import sqlite3 as sql

def truncate_teams(conn: sql.Connection):
  conn.execute("DROP TABLE IF EXISTS teams")
  conn.execute("CREATE TABLE teams (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)")
  conn.commit()

def truncate_rankings(conn: sql.Connection):
  conn.execute("DROP VIEW IF EXISTS rankings_view")
  conn.execute("DROP TABLE IF EXISTS rankings")
  conn.execute("""CREATE TABLE rankings (
    id INTEGER PRIMARY KEY AUTOINCREMENT, 
    name TEXT, 
    team INTEGER, 
    rank INTEGER,
    FOREIGN KEY (team) REFERENCES teams(id)
  )""")
  conn.execute("""CREATE VIEW rankings_view AS
    SELECT 
      t.name AS team,
      r.name AS name,
      r.rank AS rank
    FROM rankings r
    JOIN teams t ON r.team = t.id
    ORDER BY r.name, r.rank
  """)
  conn.commit()

def truncate_exclusions(conn: sql.Connection):
  conn.execute("DROP TABLE IF EXISTS exclusions")
  conn.execute("CREATE TABLE exclusions (id INTEGER PRIMARY KEY AUTOINCREMENT, name1 TEXT, name2 TEXT)")
  conn.commit()

def truncate_config(conn: sql.Connection):
  conn.execute("DROP TABLE IF EXISTS config")
  conn.execute("CREATE TABLE config (id INTEGER PRIMARY KEY AUTOINCREMENT, key TEXT, value TEXT)")
  conn.commit()

def num_teams(conn: sql.Connection) -> int:
  return conn.execute("SELECT COUNT(*) FROM teams").fetchone()[0]

def insert_rankings(conn: sql.Connection, name: str, rankings: list[int]):
  for team_index, rank in enumerate(rankings):
    conn.execute(
      "INSERT INTO rankings (name, team, rank) VALUES (?, ?, ?)",
      (name, team_index + 1, rank),
    )
  conn.commit()

def delete_rankings(conn: sql.Connection, name: str):
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

def is_rankings_valid(conn: sql.Connection) -> bool:
  """Check if all rankings are valid (no validation errors)."""
  errors = validate_rankings(conn)
  return all(len(error_list) == 0 for error_list in errors.values())

def is_already_ranked(conn: sql.Connection, name: str) -> bool:
  return conn.execute(
    "SELECT COUNT(*) FROM rankings WHERE name = ?",
    (name,),
  ).fetchone()[0] > 0

def select_top_rank(conn: sql.Connection) -> list[tuple[str, int]]:
  """Select the top rank for each name."""
  sql = """
  select name, team from (
    select
      name, team,
      row_number() over (partition by name order by rank asc) as rn
    from rankings
  )
  where rn=1;
  """
  return conn.execute(sql).fetchall()

def load_exclusions(conn: sql.Connection) -> list[tuple[str, str]]:
  return conn.execute("SELECT name1, name2 FROM exclusions").fetchall()

def insert_exclusions(conn: sql.Connection, exclusions: list[tuple[str, str]]):
  conn.executemany("INSERT INTO exclusions (name1, name2) VALUES (?, ?)", exclusions)
  conn.commit()

def load_config(conn: sql.Connection) -> dict:
  return {row[0]: row[1] for row in conn.execute("SELECT key, value FROM config").fetchall()}

def insert_config(conn: sql.Connection, key: str, value: str):
  conn.execute("INSERT INTO config (key, value) VALUES (?, ?)", (key, value))
  conn.commit()