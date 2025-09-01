"""Joiner module for Team Assigner."""

from collections import defaultdict
import click
import sqlite3 as sql
import re
import random
from pathlib import Path
import yaml

import team_assigner.db as db


SEPARATOR_RE = re.compile(r"[, ]+")

def normalize_rankings(rankings_file: Path) -> list[int]:
  """Normalize the rankings for a given file.

  The file may be a comma-separated, newline-separated, space-separated, or mixed list of rankings.
  It will be returned as a list of integers.
  """
  with open(rankings_file, "r") as f:
    rankings = []
    for line in f.readlines():
      line = line.strip()
      if not line:
        continue
      text = SEPARATOR_RE.sub(",", line)
      rankings.extend([int(x) for x in text.split(",")])
    return rankings

def update_invalid_rankings(rankings: list[int], max_team_id: int) -> list[int]:
  """Update invalid rankings to the next valid ranking."""
  if len(rankings) > max_team_id:
    click.echo(f"Rankings contain more than {max_team_id} rankings; pruning...")
    rankings = rankings[:max_team_id]

  if len(rankings) < max_team_id:
    click.echo(f"Rankings contain less than {max_team_id} rankings; padding...")
    rankings.extend([rankings[0]] * (max_team_id - len(rankings)))

  rankings = rankings.copy()
  current = set(rankings)
  missing = set(range(1, max_team_id + 1)) - current
  seen = set()
  if missing:  # either duplicates or missing ranks
    for idx, rank in enumerate(rankings):
      if rank < 1 or rank > max_team_id or rank in seen:
        choices = list(missing)
        rankings[idx] = random.choice(choices)
        missing.remove(rankings[idx])
      else:
        seen.add(rank)

  return rankings
  
@click.group()
def cli():
  """Team Assigner CLI for managing rankings database."""
  pass

@cli.command()
@click.argument("db_file", type=click.Path(path_type=Path))
@click.argument("config_file", type=click.Path(path_type=Path))
def init(db_file: Path, config_file: Path):
  """Initialize the database."""
  if not config_file.exists():
    click.echo(f"Error: Teams file {config_file} does not exist")
    return

  if db_file.exists():
    db_file.unlink()

  with open(config_file, "r") as f:
    config = yaml.safe_load(f)

  with sql.connect(db_file) as conn:
    db.truncate_teams(conn)
    db.truncate_rankings(conn)
    db.truncate_config(conn)
    db.truncate_exclusions(conn)

    for line in config["teams"]["names"]:
      line = line.strip()
      if not line:
        continue
      conn.execute("INSERT INTO teams (name) VALUES (?)", (line,))

    db.insert_config(conn, "teams_min_size", config["teams"]["size"]["min"])
    exclusions = []
    for exclusion in config["teams"]["exclusions"]:
      exclusions.append((exclusion[0], exclusion[1]))
    db.insert_exclusions(conn, exclusions)

    conn.commit()

    click.echo(f"Initialized database {db_file}")

@cli.command()
@click.argument("db_file", type=click.Path(path_type=Path))
def truncate(db_file: Path):
  """Truncate the rankings database."""
  with sql.connect(db_file) as conn:
    db.truncate_rankings(conn)
    conn.commit()
    click.echo(f"Truncated database {db_file} successfully.")

@cli.command()
@click.argument("db_file", type=click.Path(path_type=Path))
@click.option("--input", "input_files", type=click.Path(exists=True, path_type=Path), 
              multiple=True, help="Input rankings files")
def store(db_file: Path, input_files: list[Path]):
  """Process rankings files and store in database."""
  if not input_files:
    click.echo("Error: At least one input file must be specified with --input")
    return
  
  db_file_exists = db_file.exists()
  with sql.connect(db_file) as conn:
    if not db_file_exists:
      db.truncate_rankings(conn)
      click.echo(f"Initialized database {db_file}")

    max_team_id = db.num_teams(conn)
    click.echo(f"Max team ID: {max_team_id}")
    for path in input_files:
      rankings = normalize_rankings(path)
      click.echo(f"{path.stem} rankings: {rankings}")
      updated = update_invalid_rankings(rankings, max_team_id)
      if rankings != updated:
        click.echo(f"Updated rankings for {path.stem}: {rankings} -> {updated}")
      rankings = updated

      if db.is_already_ranked(conn, path.stem):
        if click.confirm(f"{path.stem} already ranked; overwrite?"):
          db.delete_rankings(conn, path.stem)
          click.echo(f"Deleted rankings for {path.stem}")
        else:
          click.echo(f"Skipping {path.stem}")
          continue

      db.insert_rankings(conn, path.stem, rankings)
      click.echo(f"Inserted rankings from {path} into {db_file}")
    click.echo(f"Processed {len(input_files)} input files into {db_file}")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
def assign(db_file: Path):
  """Assign teams based on rankings."""
  exclusion_list = db.load_exclusions(db_file)
  click.echo(f"Exclusions: {exclusion_list}")

  exclusions = defaultdict(set)
  for exclusion in exclusion_list:
    exclusions[exclusion[0]].add(exclusion[1])
    exclusions[exclusion[1]].add(exclusion[0])
  exclusions = dict(exclusions)
  click.echo(f"Exclusions: {exclusions}")

  with sql.connect(db_file) as conn:
    rankings = db.select_top_rank(conn)
    click.echo(f"Top rankings: {rankings}")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
def validate(db_file: Path):
  """Validate rankings data for completeness and correctness."""
  with sql.connect(db_file) as conn:
    errors = db.validate_rankings(conn)
    
    has_errors = False
    for error_type, error_list in errors.items():
      if error_list:
        has_errors = True
        click.echo(f"\n{error_type.replace('_', ' ').title()}:")
        for error in error_list:
          click.echo(f"  • {error}")
    
    if not has_errors:
      click.echo("✅ All rankings are valid!")
    else:
      click.echo(f"\n❌ Found validation errors in {db_file}")

if __name__ == "__main__":
  cli()