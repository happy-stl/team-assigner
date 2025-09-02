import sqlite3 as sql
from typing import Iterable

def truncate_teams(conn: sql.Connection) -> None:
  """Truncate the teams table."""
  conn.execute("DROP TABLE IF EXISTS teams")
  conn.execute("CREATE TABLE teams (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, CONSTRAINT teams_name_unique UNIQUE (name))")
  conn.commit()

def truncate_rankings(conn: sql.Connection) -> None:
  """Truncate the rankings table."""
  conn.execute("DROP TABLE IF EXISTS temp_rankings")
  conn.execute("DROP VIEW IF EXISTS rankings_view")
  conn.execute("DROP TABLE IF EXISTS rankings")
  conn.execute("DROP TABLE IF EXISTS people_sections")
  conn.execute("""CREATE TABLE people_sections (
    name TEXT PRIMARY KEY,
    section INTEGER
  )""")
  conn.execute("""CREATE TABLE rankings (
    id INTEGER PRIMARY KEY AUTOINCREMENT, 
    name TEXT, 
    team INTEGER, 
    rank INTEGER,
    FOREIGN KEY (team) REFERENCES teams(id)
    FOREIGN KEY (name) REFERENCES people_sections(name)
  )""")
  conn.execute("""CREATE VIEW rankings_view AS
    SELECT 
      r.name AS name,
      ps.section AS section,
      t.name AS team,
      r.rank AS rank
    FROM rankings r
    JOIN teams t ON r.team = t.id
    JOIN people_sections ps ON r.name = ps.name
    ORDER BY r.name, ps.section, r.rank
  """)
  conn.commit()

def truncate_exclusions(conn: sql.Connection) -> None:
  conn.execute("DROP VIEW IF EXISTS exclusion_pairs")
  conn.execute("DROP TABLE IF EXISTS exclusions")
  conn.execute("CREATE TABLE exclusions (id INTEGER PRIMARY KEY AUTOINCREMENT, name1 TEXT, name2 TEXT)")
  conn.execute("CREATE VIEW exclusion_pairs AS SELECT name1, name2 FROM exclusions UNION ALL SELECT name2, name1 FROM exclusions")
  conn.commit()

def truncate_config(conn: sql.Connection) -> None:
  conn.execute("DROP TABLE IF EXISTS config")
  conn.execute(
    """
    CREATE TABLE config (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      key TEXT,
      value TEXT,
      CONSTRAINT config_key_unique UNIQUE (key)
    )
    """
  )
  conn.commit()

def num_teams(conn: sql.Connection) -> int:
  """Get the number of teams in the database."""
  return conn.execute("SELECT COUNT(*) FROM teams").fetchone()[0]

def insert_rankings(conn: sql.Connection, name: str, rankings: Iterable[int]) -> None:
  for team_index, rank in enumerate(rankings):
    conn.execute(
      "INSERT INTO rankings (name, team, rank) VALUES (?, ?, ?)",
      (name, team_index + 1, rank),
    )
  conn.commit()

def delete_rankings(conn: sql.Connection, name: str) -> None:
  conn.execute("DELETE FROM rankings WHERE name = ?", (name,))
  conn.commit()

def validate_rankings(conn: sql.Connection) -> dict[str, list[str]]:
  """Validate that each person has rankings from 1..max(teams.id) with no gaps, repeats, or invalid values.
  
  Returns:
    Dictionary with validation errors by category:
    - 'missing_ranks': People missing some rank values
    - 'duplicate_ranks': People with duplicate rank values  
    - 'invalid_ranks': People with ranks outside valid range
    - 'incomplete_rankings': People who haven't ranked all teams
  """
  errors = {
    'missing_people': [],
    'missing_ranks': [],
    'duplicate_ranks': [],
    'invalid_ranks': [],
    'incomplete_rankings': []
  }
  
  # Get the maximum team ID (number of teams)
  max_team_id = conn.execute("SELECT MAX(id) FROM teams").fetchone()[0]
  if max_team_id is None:
    return errors
  
  # Get all people who have submitted rankings
  people = conn.execute("SELECT DISTINCT name FROM rankings").fetchall()
  required_people = set(conn.execute("SELECT DISTINCT name FROM people_sections").fetchall())

  if set(people) != required_people:
    missing_people = [missing[0] for missing in required_people - set(people)]
    errors['missing_people'].append(
      f"People missing from rankings: {sorted(missing_people)}"
    )
  
  for (person,) in people:
    # Get all ranks for this person
    ranks = conn.execute(
      "SELECT rank FROM rankings WHERE name = ? ORDER BY rank", 
      (person,)
    ).fetchall()
    rank_values = [r[0] for r in ranks]
    
    # Check if person has ranked all teams
    expected_count = max_team_id
    actual_count = len(rank_values)
    if actual_count != expected_count:
      errors['incomplete_rankings'].append(
        f"{person}: has {actual_count} rankings, expected {expected_count}"
      )
    
    # Check for invalid rank values (outside 1..max_team_id range)
    invalid_ranks = [r for r in rank_values if r < 1 or r > max_team_id]
    if invalid_ranks:
      errors['invalid_ranks'].append(
        f"{person}: invalid ranks {invalid_ranks} (valid range: 1-{max_team_id})"
      )
    
    # Check for duplicate ranks
    if len(rank_values) != len(set(rank_values)):
      duplicates = []
      seen = set()
      for rank in rank_values:
        if rank in seen:
          duplicates.append(rank)
        seen.add(rank)
      errors['duplicate_ranks'].append(
        f"{person}: duplicate ranks {list(set(duplicates))}"
      )
    
    # Check for missing ranks in the valid range
    valid_rank_values = [r for r in rank_values if 1 <= r <= max_team_id]
    expected_ranks = set(range(1, max_team_id + 1))
    actual_ranks = set(valid_rank_values)
    missing_ranks = expected_ranks - actual_ranks
    if missing_ranks:
      errors['missing_ranks'].append(
        f"{person}: missing ranks {sorted(missing_ranks)}"
      )
  
  return errors

def is_already_ranked(conn: sql.Connection, name: str) -> bool:
  return conn.execute(
    "SELECT COUNT(*) FROM rankings WHERE name = ?",
    (name,),
  ).fetchone()[0] > 0

def create_temp_rankings(conn: sql.Connection) -> None:
  conn.execute(
    """
    CREATE TEMPORARY TABLE IF NOT EXISTS temp_rankings AS
    SELECT r.name, ps.section, r.team, r.rank, false as excluded
    FROM rankings r
    JOIN people_sections ps ON r.name = ps.name
    """
  )
  conn.commit()

def select_temp_most_popular_team(conn: sql.Connection) -> int:
  query = """
  SELECT team as count FROM (
    SELECT
      name, section, team,
      row_number() OVER (PARTITION BY name, section ORDER BY rank ASC) AS rn
    FROM temp_rankings
    WHERE excluded = false
  )
  WHERE rn=1
  GROUP BY team ORDER BY count(*) DESC LIMIT 1;
  """
  return conn.execute(query).fetchone()[0]

def select_temp_top_rank_for_team(conn: sql.Connection, team: int) -> list[tuple[str, int, int]]:
  query = """
  SELECT name, section, team FROM (
    SELECT
      name, section, team,
      row_number() OVER (PARTITION BY rank ORDER BY RANDOM()) AS rn
    FROM temp_rankings
    WHERE excluded = false
      AND team = ?
    ORDER BY rank ASC, rn ASC
  )
  """
  return conn.execute(query, (team,)).fetchall()

def select_temp_top_rank(conn: sql.Connection) -> list[tuple[str, int, int]]:
  """Select the top rank for each name."""
  query = """
  SELECT name, section, team FROM (
    SELECT
      name, section, team,
      row_number() OVER (PARTITION BY name, section ORDER BY rank ASC) AS rn
    FROM temp_rankings
    WHERE excluded = false
  )
  WHERE rn=1;
  """
  return conn.execute(query).fetchall()

def ignore_temp_rankings_for_name_team(conn: sql.Connection, name: str, team: int) -> None:
  conn.execute("UPDATE temp_rankings SET excluded = true WHERE name = ? AND team = ?", (name, team))
  conn.commit()

def ignore_temp_rankings_for_names(conn: sql.Connection, names: Iterable[str]) -> None:
  conn.executemany("UPDATE temp_rankings SET excluded = true WHERE name = ?", [(name,) for name in names])
  conn.commit()

def randomize_temp_ranking(conn: sql.Connection, name: str, team: int, num_teams: int) -> None:
  conn.execute("UPDATE temp_rankings SET rank = (abs(random()) % ?) + 1 WHERE name = ? AND team = ?", (num_teams, name, team))
  conn.commit()

def ignore_temp_rankings_for_team(conn: sql.Connection, team: int) -> None:
  conn.execute("UPDATE temp_rankings SET excluded = true WHERE team = ?", (team,))
  conn.commit()

def is_excluded(conn: sql.Connection, root: str, targets: Iterable[str]) -> bool:
  targets_list = list(targets)
  if not targets_list:
    return False
  
  placeholders = ','.join('?' * len(targets_list))
  params = [root] + targets_list + targets_list + [root]
  
  result = conn.execute(
    f"""
    SELECT COUNT(*)
    FROM exclusions
    WHERE (name1 = ? AND name2 IN ({placeholders}))
       OR (name1 IN ({placeholders}) AND name2 = ?)
    """,
    params
  ).fetchone()
  
  return result[0] > 0

def insert_people_sections(conn: sql.Connection, names: Iterable[str], section: int) -> None:
  conn.executemany(
    """INSERT INTO people_sections (name, section) VALUES (?, ?)
    ON CONFLICT(name) DO UPDATE
    SET section = excluded.section""",
    [(name, section) for name in names]
  )
  conn.commit()

def insert_teams(conn: sql.Connection, values: Iterable[tuple[int, str]]) -> None:
  conn.executemany(
    """INSERT INTO teams (id, name) VALUES (?, ?)
    ON CONFLICT(id) DO UPDATE
    SET name = excluded.name""",
    values
  )
  conn.commit()

def fetch_num_people_per_section(conn: sql.Connection) -> dict[int, int]:
  return {row[0]: row[1] for row in conn.execute("SELECT section, COUNT(*) FROM people_sections GROUP BY section").fetchall()}

def insert_exclusions(conn: sql.Connection, exclusions: Iterable[tuple[str, str]]) -> None:
  conn.executemany("INSERT INTO exclusions (name1, name2) VALUES (?, ?)", exclusions)
  conn.commit()

def load_config(conn: sql.Connection) -> dict[str, str]:
  return {row[0]: row[1] for row in conn.execute("SELECT key, value FROM config").fetchall()}

def fetch_config(conn: sql.Connection, key: str) -> str:
  return conn.execute("SELECT value FROM config WHERE key = ?", (key,)).fetchone()[0]

def fetch_config_like(conn: sql.Connection, key: str) -> dict[str, str]:
  return dict(conn.execute("SELECT key, value FROM config WHERE key LIKE ?", (key,)).fetchall())

def insert_config(conn: sql.Connection, key: str, value: str) -> None:
  conn.execute(
    """INSERT INTO config (key, value) VALUES (?, ?)
    ON CONFLICT(key) DO UPDATE
    SET value = excluded.value""",
    (key, value)
  )
  conn.commit()