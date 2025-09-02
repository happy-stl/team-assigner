# Team Assigner

A database-driven tool to assign people to teams based on their preferences and rankings. The tool uses a SQLite database to store configuration, rankings, and manage the assignment process efficiently.

## Installation

Install the package using pip:

```bash
pip install team-assigner
```

Or install from source:

```bash
git clone https://github.com/grimwm/team-assigner
cd team-assigner
pip install -e .
```

## Quick Start

1. **Initialize a database:**
   ```bash
   team-assigner init my_project.db
   ```

2. **Load configuration:**
   ```bash
   team-assigner config my_project.db config.yaml
   ```

3. **Store individual rankings:**
   ```bash
   team-assigner store my_project.db --input person1.txt --input person2.txt
   ```

4. **Validate data:**
   ```bash
   team-assigner validate my_project.db
   ```

5. **Assign teams:**
   ```bash
   team-assigner assign my_project.db
   ```

## Configuration

The tool uses a comprehensive YAML configuration file that defines teams, people sections, team sizes, and exclusions.

### Complete Configuration Structure

```yaml
# People organization by sections
people:
  sections:
    001:  # Section ID
      - John
      - Linda
      - James
    002:
      - Sarah
      - Mike
      - Emma

# Team configuration
teams:
  size:
    min: 2  # Minimum team size

  # Team names (ID: Name mapping)
  names:
    1: "Development Team"
    2: "Design Team" 
    3: "Marketing Team"
    4: "Research Team"

  # People that cannot be on the same team
  # Note: Exclusions are bidirectional
  match_exclusions:
    John:
      - Linda  # John and Linda cannot be together
    Sarah:
      - Mike
      - Emma   # Sarah cannot be with Mike or Emma
```

### Simplified Configuration (Legacy Format)

For simpler use cases, you can use the legacy format:

```yaml
team:
  min: 2
  exclusions:
    - John,Linda
    - Mike,Emma
```

### Configuration Options

- **`people.sections`**: Organize people into sections (useful for different departments, skill levels, etc.)
- **`teams.size.min`**: Minimum number of people per team
- **`teams.names`**: Human-readable names for each team (mapped by team ID)
- **`teams.match_exclusions`**: Define people who cannot be placed on the same team

## Rankings

Individual rankings are stored in simple text files. Each person should have their own file containing their team preferences ranked from most preferred (1) to least preferred.

### Rankings File Format

Rankings can be provided in various formats:
- Comma-separated: `1,3,2,4`
- Space-separated: `1 3 2 4`
- Newline-separated:
  ```
  1
  3
  2
  4
  ```
- Mixed format: `1, 3 2,4`

### Example Rankings

If there are 4 teams, a person's rankings file might contain:
```
2  # First choice: Team 2
1  # Second choice: Team 1  
4  # Third choice: Team 4
3  # Fourth choice: Team 3
```

## CLI Commands

### `init <database>`
Initialize a new SQLite database for the project.

```bash
team-assigner init my_project.db
```

### `config <database> <config_file>`
Load configuration from a YAML file into the database.

```bash
team-assigner config my_project.db config.yaml
```

### `store <database> --input <file> [--input <file2> ...]`
Store individual ranking files into the database.

```bash
team-assigner store my_project.db --input john.txt --input linda.txt --input james.txt
```

### `validate <database>`
Validate that all rankings data is complete and correct.

```bash
team-assigner validate my_project.db
```

### `assign <database> [--debug]`
Run the team assignment algorithm.

```bash
team-assigner assign my_project.db
# or with debug output
team-assigner assign my_project.db --debug
```

### `truncate <database>`
Clear all rankings data (keeps configuration).

```bash
team-assigner truncate my_project.db
```

## Complete Workflow Example

Here's a complete example of using the tool:

```bash
# 1. Initialize database
team-assigner init company_hackathon.db

# 2. Create and load configuration
cat > config.yaml << EOF
people:
  sections:
    001:  # Engineering
      - Alice
      - Bob
      - Charlie
    002:  # Design  
      - Diana
      - Eve
      - Frank

teams:
  size:
    min: 2
  names:
    1: "Web App Team"
    2: "Mobile Team"
    3: "AI/ML Team"
  match_exclusions:
    Alice:
      - Bob  # Alice and Bob had conflicts before
EOF

team-assigner config company_hackathon.db config.yaml

# 3. Collect individual rankings
echo "1,3,2" > alice_rankings.txt    # Alice prefers: Web App, AI/ML, Mobile
echo "2,1,3" > bob_rankings.txt      # Bob prefers: Mobile, Web App, AI/ML
echo "3,2,1" > charlie_rankings.txt  # Charlie prefers: AI/ML, Mobile, Web App
echo "1,2,3" > diana_rankings.txt    # Diana prefers: Web App, Mobile, AI/ML
echo "2,3,1" > eve_rankings.txt      # Eve prefers: Mobile, AI/ML, Web App
echo "3,1,2" > frank_rankings.txt    # Frank prefers: AI/ML, Web App, Mobile

# 4. Store rankings
team-assigner store company_hackathon.db \
  --input alice_rankings.txt \
  --input bob_rankings.txt \
  --input charlie_rankings.txt \
  --input diana_rankings.txt \
  --input eve_rankings.txt \
  --input frank_rankings.txt

# 5. Validate data
team-assigner validate company_hackathon.db

# 6. Assign teams
team-assigner assign company_hackathon.db
```

## Team Assignment Output

The assignment algorithm will output the final team assignments to the console, showing which people are assigned to which teams within their sections:

```
Teams assigned:
  Section 1:
    Team 1 (Web App Team):
      - Alice
      - Charlie
    Team 2 (Mobile Team):
      - Bob
  Section 2:
    Team 1 (Web App Team):
      - Diana
    Team 2 (Mobile Team):
      - Eve
      - Frank
```

## Algorithm

The team assignment algorithm works by:

1. **Creating temporary rankings** from the stored data
2. **Finding the most popular team** among all top preferences
3. **Selecting people** who ranked that team highest
4. **Checking exclusions** to ensure incompatible people aren't grouped
5. **Filling teams** to the minimum size requirement
6. **Repeating** until all people are assigned

The algorithm respects:
- Individual preferences (higher ranked preferences are prioritized)
- Exclusion rules (people who cannot work together)
- Minimum team size requirements
- Section boundaries (people are assigned within their sections)