"""Joiner module for Team Assigner."""

from collections import defaultdict
import click
import sys
import sqlite3 as sql
import re
import random
from pathlib import Path
import yaml
import math

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
  if db_file.exists() and click.confirm(f"Database file {db_file} already exists; overwrite?", default=False):
    db_file.unlink()
  else:
    click.secho(f"Database file {db_file} already exists; not overwriting", fg="red")
    sys.exit(1)

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
  if not db_file.exists():
    click.secho(f"Error: Database file {db_file} does not exist", fg="red")
    sys.exit(1)

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
  if not db_file.exists():
    click.secho(f"Error: Database file {db_file} does not exist", fg="red")
    sys.exit(1)

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
  if not db_file.exists():
    click.secho(f"Error: Database file {db_file} does not exist", fg="red")
    sys.exit(1)

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
      click.secho(f"{path.stem}'s rankings: {rankings}", fg="blue")
      updated = rerank_invalid_rankings(rankings, max_team_id)
      if rankings != updated:
        click.secho(f"Updated {path.stem}'s rankings: {rankings} -> {updated}", fg="yellow")
      rankings = updated

      if db.is_already_ranked(conn, path.stem):
        if click.confirm(f"{path.stem}'s rankings already exist; overwrite?", default=False):
          db.delete_rankings(conn, path.stem)
          click.secho(f"Deleted {path.stem}'s rankings", fg="yellow")
        else:
          click.secho(f"Skipping {path.stem}'s rankings", fg="green")
          continue

      db.insert_rankings(conn, path.stem, rankings)
      click.secho(f"Inserted {path.stem}'s rankings into {db_file}", fg="blue")
    click.secho(f"Processed {len(input_files)} input files into {db_file}", fg="green")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
@click.option("--debug", "is_debug", is_flag=True, help="Debug mode")
def assign(db_file: Path, is_debug: bool):
  """Assign teams based on rankings."""
  if not db_file.exists():
    click.secho(f"Error: Database file {db_file} does not exist", fg="red")
    sys.exit(1)

  with sql.connect(db_file) as conn:
    min_team_size = int(db.fetch_config(conn, "teams.min.size"))

  def num_teams_by_section(conn: sql.Connection) -> dict[int, float]:
    sections = db.fetch_num_people_per_section(conn)
    return {
      section: num_people / min_team_size for section, num_people in sections.items()
    }

  def team_sizes_expanded(conn: sql.Connection) -> dict[int, list[int]]:
    num_teams_per_section = num_teams_by_section(conn)
    teams = {}
    for section, num_teams in num_teams_per_section.items():
      teams[section] = [min_team_size] * int(num_teams) if int(num_teams) > 0 else [0]
      rem = int(num_teams * min_team_size) % min_team_size
      idx = 0
      while rem > 0:
        teams[section][idx] += 1
        rem -= 1
        idx += 1
        idx %= len(teams[section])
    return teams

  def print_teams_assigned(teams_assigned: dict[int, dict[int, set[str]]], fg: str) -> None:
    sections = sorted(teams_assigned.keys())
    for section in sections:
      click.secho(f"  Section {section}:", fg=fg)
      teams = sorted(teams_assigned[section].keys())
      for team in teams:
        people = teams_assigned[section][team]
        click.secho(f"    Team {team}: {people}", fg=fg)

  if validate.callback(db_file):
    click.secho(f"Validation errors in {db_file}", fg="red")
    sys.exit(1)

  with sql.connect(db_file) as conn:
    sections = num_teams_by_section(conn)
    team_sizes = team_sizes_expanded(conn)
    teams_assigned = {section: {} for section in sections}

    is_debug and click.secho(f"People sections: {sections}", fg="blue")
    click.secho(f"Team sizes: {team_sizes}", fg="green")

    db.create_temp_rankings(conn)

    # {section: {team: set(person)}}
    section_teams: dict[int, dict[int, set[str]]] = {section: defaultdict(set) for section in sections}
    while top_rankings := db.fetch_temp_top_rank(conn):
      if not top_rankings:
        click.secho("No more rankings to assign; stopping...", fg="red")
        break
      is_debug and click.secho(f"Selecting target team from {top_rankings}...", fg="blue")
      target_team = db.fetch_temp_most_popular_team(conn)
      is_debug and click.secho(f"Target team: {target_team}", fg="blue")
      rankings = db.fetch_temp_top_rank_for_team(conn, target_team)
      is_debug and click.secho(f"Rankings for target team: {rankings}", fg="blue")
      if not any(team_sizes[section] for section in sections):
        click.secho("No more teams to assign; stopping...", fg="red")
        break
      skip_count = 0
      while rankings:
        person, section, team = rankings.pop(0)
        assigned_team = section_teams[section][team]
        if db.is_excluded(conn, person, list(assigned_team)):
          click.secho(f"{person} is excluded from team {team} in section {section}; skipping...", fg="yellow")
          skip_count += 1
          if skip_count == len(rankings):
            click.secho("All remaining rankings are excluded from matching; stopping...", fg="red")
            click.secho("Please check the exclusions and try again; team assignments could not be completed.", fg="red")
            click.secho("These are the teams that were assigned:", fg="red")
            print_teams_assigned(teams_assigned, "red")
            sys.exit(1)
          continue
        skip_count = 0
        is_debug and click.secho(f"Assigning {person} to team {team} in section {section}", fg="blue")
        assigned_team.add(person)
        is_debug and click.secho(f"Assigned team: {assigned_team}; len: {len(assigned_team)}; has team sizes: {bool(team_sizes[section])}", fg="blue")
        if team_sizes[section]:
          if len(assigned_team) == team_sizes[section][0]:  # first team to the finish
            team_sizes[section].pop(0)
            teams_assigned[section][team] = assigned_team
            db.ignore_temp_rankings_for_team(conn, team)
            db.ignore_temp_rankings_for_names(conn, assigned_team)
            break
      if is_debug and not click.confirm(f"Continue assigning teams?", default=True):
        break

    click.secho("Teams assigned:", fg="green")
    print_teams_assigned(teams_assigned, "green")

@cli.command()
@click.argument("db_file", type=click.Path(exists=True, path_type=Path))
def validate(db_file: Path) -> bool:
  """Validate rankings data for completeness and correctness."""
  if not db_file.exists():
    click.secho(f"Error: Database file {db_file} does not exist", fg="red")
    sys.exit(1)

  has_errors = False
  with sql.connect(db_file) as conn:
    errors = db.validate_rankings(conn)
    
    for error_type, error_list in errors.items():
      if error_list:
        has_errors = True
        click.secho(f"\n{error_type.replace('_', ' ').title()}:", fg="red")
        for error in error_list:
          click.secho(f"  • {error}", fg="red")
    
  if has_errors:
    click.secho(f"\n❌ Found validation errors in {db_file}", fg="red")
  else:
    click.secho("✅ All rankings are valid!", fg="green")
  return has_errors

if __name__ == "__main__":
  cli()