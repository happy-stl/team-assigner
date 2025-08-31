# Examples

This directory contains sample files to demonstrate how to use the Team Assigner tool.

## Files

- **`sample_rankings.csv`**: Example rankings file with 6 people and 3 preferences
- **`sample_config.yaml`**: Example configuration with team size and exclusions
- **`expected_output.csv`**: Example of assignment output in CSV format  
- **`expected_output.yaml`**: Example of assignment output in YAML format

## Usage

Try the tool with these sample files:

```bash
# Basic usage
team-assigner examples/sample_rankings.csv

# With configuration
team-assigner examples/sample_rankings.csv -c examples/sample_config.yaml

# With custom minimum team size
team-assigner examples/sample_rankings.csv -m 3

# With verbose output and custom output directory
team-assigner examples/sample_rankings.csv -c examples/sample_config.yaml -v -o output/
```

## Sample Data Explanation

The `sample_rankings.csv` contains rankings for 6 people across 3 preferences:

| Preference | John | Linda | James | Sarah | Mike | Emma |
| ---------- | ---- | ----- | ----- | ----- | ---- | ---- |
| 1          | 1    | 2     | 3     | 1     | 3    | 2    |
| 2          | 2    | 3     | 1     | 3     | 1    | 3    |
| 3          | 3    | 1     | 2     | 2     | 2    | 1    |

This means:
- John's preferences: 1st choice = Preference 1, 2nd choice = Preference 2, 3rd choice = Preference 3
- Linda's preferences: 1st choice = Preference 3, 2nd choice = Preference 1, 3rd choice = Preference 2
- And so on...

The configuration specifies:
- Minimum team size: 2 people
- Exclusions: John and Linda cannot be on the same team, Mike and Emma cannot be on the same team
