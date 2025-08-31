# Team Assigner

A simple tool to assign people to teams based on their preferences (e.g. project preference). What those preferences mean will be something external to this tool, but as long as the preferences can be mapped to integers `[1,N]`, then this tool will serve well.

## Rankings

There should be one file that contains the rankings, and it should be in CSV format. Each column should be an identifier of the person choosing their preferences. Each row will represent a preference. Each cell will represent an identifier's (e.g. person's) ranking of a preference.

The number of rows in the CSV, not including the header, indicates the number of preferences. The file will be auto-checked when the program is run to ensure validity.

### Example CSV

Three people voting on three preferences might have a CSV like this:

| John | Linda | James |
| ---- | ----- | ----- |
| 1    | 2     | 3     |
| 2    | 3     | 1     |
| 3    | 1     | 2     |

## Configuration

A YAML configuration file may also be used.

### Exclusions

Sometimes, people refuse to be teamed together. For these situations, add an `exclusions` section:

    ```yaml
    team:
      exclusions:
        - person_a,person_b,person_c
        - person_b,person_f
        - person_c,person_f
    ```

### Team Size

A team needs to have a minimum size. It can be passed via the CLI parameter `--min-team-size` or `-m`, but it can also be saved in the configuration:

    ```yaml
    team:
      min: 2
    ```

## Assignments

When the application has finished assigning members to teams, it will generate a new CSV file. The header will be identical to the rankings CSV file. There will be a single data row with each cell representing the preference (e.g. project preference) they were assigned.

There will also be a yaml file generated with a more human-readable team assignment list. For example, its contents might look like:

    ```yaml
    team:
      1:
        - John
        - Linda
      2:
        - James
    ```