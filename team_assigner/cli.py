"""Joiner module for Team Assigner."""

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

    db.insert_config(conn, "teams.min.size", config["teams"]["size"]["min"])
    for section, names in config["people"]["sections"].items():
      db.insert_people_sections(conn, names, int(section))
    for section, size in config["teams"]["sections"].items():
      db.insert_config(conn, f"teams.sections.{int(section)}", size)
    exclusion_pairs = []
    for person, exclusions in config["teams"].get("match_exclusions", {}).items():
      for exclusion in exclusions:
        exclusion_pairs.append((person, exclusion))
    db.insert_exclusions(conn, exclusion_pairs)

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
  def num_teams(conn: sql.Connection) -> dict[int, float]:
    sections = db.fetch_config_like(conn, "teams.sections.%")
    min_team_size = int(db.fetch_config(conn, "teams.min.size"))
    return {
      int(section.split(".")[-1]): int(num_people) / min_team_size for section, num_people in sections.items()
    }

  def team_sizes_expanded(conn: sql.Connection) -> dict[int, int]:
    sections = num_teams(conn)
    min_team_size = int(db.fetch_config(conn, "teams.min.size"))
    teams = {}
    for section, size in sections.items():
      teams[section] = [int(size)] * int(size)
      rem = size % min_team_size
      idx = 0
      while rem > 0:
        teams[section][idx] += 1
        rem -= 1
        idx += 1
        idx %= len(teams[section])
    click.echo(f"Teams: {teams}")
    return teams

  with sql.connect(db_file) as conn:
    sections = num_teams(conn)
    click.echo(f"People sections: {sections}")
    team_sizes_expanded(conn)
    db.create_temp_rankings(conn)
    rankings = db.select_temp_top_rank(conn)
    choice = random.choice(rankings)
    click.echo(f"Chosen ranking: {choice}")
    # TODO delete all rankings for this name and section
    db.delete_temp_top_rank(conn, choice[0], choice[1], choice[2])
    click.echo(f"Top rankings: {rankings}")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
def validate(db_file: Path):
  """Validate rankings data for completeness and correctness."""
  with sql.connect(db_file) as conn:
    errors = db.validate_rankings(conn)
    
    for error_type, error_list in errors.items():
      if error_list:
        click.echo(f"\n{error_type.replace('_', ' ').title()}:")
        for error in error_list:
          click.echo(f"  • {error}")
    
    if not errors:
      click.echo("✅ All rankings are valid!")
    else:
      click.echo(f"\n❌ Found validation errors in {db_file}")

if __name__ == "__main__":
  cli()