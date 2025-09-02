"""Joiner module for Team Assigner."""

from collections import defaultdict
import click
import sys
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

def rerank_invalid_rankings(rankings: list[int], max_team_id: int) -> list[int]:
  """Update invalid rankings to the next valid ranking."""
  if len(rankings) > max_team_id:
    click.secho(f"Rankings contain more than {max_team_id} rankings; pruning...", fg="yellow")
    rankings = rankings[:max_team_id]

  if len(rankings) < max_team_id:
    click.secho(f"Rankings contain less than {max_team_id} rankings; padding...", fg="yellow")
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
def init(db_file: Path):
  """Initialize the database."""
  if db_file.exists():
    db_file.unlink()

  with sql.connect(db_file) as conn:
    db.truncate_teams(conn)
    db.truncate_rankings(conn)
    db.truncate_config(conn)
    db.truncate_exclusions(conn)

    conn.commit()

    click.secho(f"Initialized database {db_file}", fg="green")

@cli.command()
@click.argument("db_file", type=click.Path(path_type=Path))
@click.argument("config_file", type=click.Path(path_type=Path))
def config(db_file: Path, config_file: Path):
  """Initialize the database with the given config file."""
  if not config_file.exists():
    click.secho(f"Error: Config file {config_file} does not exist", fg="red")
    sys.exit(1)

  with open(config_file, "r") as f:
    config = yaml.safe_load(f)

  with sql.connect(db_file) as conn:
    db.insert_teams(conn, [(int(team_id), name) for team_id, name in config["teams"]["names"].items()])

    db.insert_config(conn, "teams.min.size", config["teams"]["size"]["min"])
    for section, names in config["people"]["sections"].items():
      db.insert_people_sections(conn, names, int(section))
    exclusion_pairs = []
    for person, exclusions in config["teams"].get("match_exclusions", {}).items():
      for exclusion in exclusions:
        exclusion_pairs.append((person, exclusion))
    db.insert_exclusions(conn, exclusion_pairs)

    conn.commit()

    click.secho(f"Loaded config from {config_file} into {db_file}", fg="green")

@cli.command()
@click.argument("db_file", type=click.Path(path_type=Path))
def truncate(db_file: Path):
  """Truncate the rankings database."""
  with sql.connect(db_file) as conn:
    db.truncate_rankings(conn)
    conn.commit()
    click.secho(f"Truncated database {db_file} successfully.", fg="green")

@cli.command()
@click.argument("db_file", type=click.Path(path_type=Path))
@click.option("--input", "input_files", type=click.Path(exists=True, path_type=Path), 
              multiple=True, help="Input rankings files")
def store(db_file: Path, input_files: list[Path]):
  """Process rankings files and store in database."""
  if not input_files:
    click.secho("Error: At least one input file must be specified with --input", fg="red")
    sys.exit(1)
  
  db_file_exists = db_file.exists()
  with sql.connect(db_file) as conn:
    if not db_file_exists:
      db.truncate_rankings(conn)
      click.secho(f"Initialized database {db_file}", fg="green")

    max_team_id = db.num_teams(conn)
    click.secho(f"Max team ID: {max_team_id}", fg="blue")
    for path in input_files:
      rankings = normalize_rankings(path)
      click.secho(f"{path.stem} rankings: {rankings}", fg="blue")
      updated = rerank_invalid_rankings(rankings, max_team_id)
      if rankings != updated:
        click.secho(f"Updated rankings for {path.stem}: {rankings} -> {updated}", fg="yellow")
      rankings = updated

      if db.is_already_ranked(conn, path.stem):
        if click.confirm(f"{path.stem} already ranked; overwrite?", default=False):
          db.delete_rankings(conn, path.stem)
          click.secho(f"Deleted rankings for {path.stem}", fg="yellow")
        else:
          click.secho(f"Skipping {path.stem}", fg="green")
          continue

      db.insert_rankings(conn, path.stem, rankings)
      click.secho(f"Inserted rankings from {path} into {db_file}", fg="blue")
    click.secho(f"Processed {len(input_files)} input files into {db_file}", fg="green")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
def assign(db_file: Path):
  """Assign teams based on rankings."""
  min_team_size = int(db.fetch_config(conn, "teams.min.size"))

  def num_teams(conn: sql.Connection) -> dict[int, float]:
    sections = db.fetch_num_people_per_section(conn)
    return {
      section: num_people / min_team_size for section, num_people in sections.items()
    }

  def team_sizes_expanded(conn: sql.Connection) -> dict[int, int]:
    sections = num_teams(conn)
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
    return teams

  with sql.connect(db_file) as conn:
    sections = num_teams(conn)
    team_sizes = team_sizes_expanded(conn)
    teams_assigned = {section: [] for section in sections}

    click.secho(f"People sections: {sections}", fg="blue")
    click.secho(f"Team sizes: {team_sizes}", fg="blue")

    db.create_temp_rankings(conn)

    # {section: {team: set(person)}}
    section_teams: dict[int, dict[int, set[str]]] = {section: defaultdict(set) for section in sections}
    while rankings := db.select_temp_top_rank(conn):
      click.secho(f"Top rankings: {rankings}", fg="blue")
      choice = random.choice(rankings)
      click.secho(f"Chosen assignment: {choice}", fg="blue")
      person, section, team = choice
      assigned_team = section_teams[section][team]
      if db.is_excluded(conn, person, list(assigned_team)):
        click.secho(f"{person} excluded by rule with somebody already in {{section: {section}, team {team}}}; skipping...", fg="yellow")
        continue
      assigned_team.add(person)
      if len(assigned_team) == team_sizes[section][0]:
        team_sizes[section].pop(0)
        db.delete_temp_rankings_for_team(conn, team)
        teams_assigned[section].append(assigned_team)
      db.delete_temp_rankings(conn, person)

    click.secho(f"Teams assigned: {teams_assigned}", fg="green")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
def validate(db_file: Path):
  """Validate rankings data for completeness and correctness."""
  with sql.connect(db_file) as conn:
    errors = db.validate_rankings(conn)
    
    for error_type, error_list in errors.items():
      if error_list:
        click.secho(f"\n{error_type.replace('_', ' ').title()}:", fg="red")
        for error in error_list:
          click.secho(f"  • {error}", fg="red")
    
    if not errors:
      click.secho("✅ All rankings are valid!", fg="green")
    else:
      click.secho(f"\n❌ Found validation errors in {db_file}", fg="red")

if __name__ == "__main__":
  cli()