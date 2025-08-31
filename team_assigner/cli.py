"""Command-line interface for Team Assigner."""

import sys
from pathlib import Path
from typing import Optional

import click

from .assigner import TeamAssigner
from .config import Config
from .validators import validate_rankings_csv


@click.command()
@click.argument("rankings_file", type=click.Path(exists=True, path_type=Path))
@click.option(
    "-c",
    "--config",
    type=click.Path(exists=True, path_type=Path),
    help="YAML configuration file path",
)
@click.option(
    "-m",
    "--min-team-size",
    type=int,
    default=2,
    help="Minimum team size (default: 2)",
)
@click.option(
    "-o",
    "--output-dir",
    type=click.Path(path_type=Path),
    default=Path("."),
    help="Output directory for assignment files (default: current directory)",
)
@click.option(
    "--output-prefix",
    type=str,
    default="team_assignments",
    help="Prefix for output files (default: team_assignments)",
)
@click.option(
    "-v",
    "--verbose",
    is_flag=True,
    help="Enable verbose output",
)
def main(
    rankings_file: Path,
    config: Optional[Path],
    min_team_size: int,
    output_dir: Path,
    output_prefix: str,
    verbose: bool,
) -> None:
    """Assign people to teams based on their preferences from RANKINGS_FILE.
    
    RANKINGS_FILE should be a CSV file where:
    - Each column represents a person
    - Each row represents a preference option
    - Each cell contains the person's ranking for that preference (1=most preferred)
    
    Example:
        team-assigner rankings.csv -c config.yaml -m 3 -o output/
    """
    try:
        # Validate the rankings CSV file
        if verbose:
            click.echo(f"Validating rankings file: {rankings_file}")
        
        validate_rankings_csv(rankings_file)
        
        # Load configuration
        if verbose:
            click.echo(f"Loading configuration...")
        
        team_config = Config()
        if config:
            team_config.load_from_file(config)
        
        # Override min team size if provided via CLI
        team_config.min_team_size = min_team_size
        
        if verbose:
            click.echo(f"Minimum team size: {team_config.min_team_size}")
            if team_config.exclusions:
                click.echo(f"Found {len(team_config.exclusions)} exclusion groups")
        
        # Initialize team assigner
        assigner = TeamAssigner(team_config)
        
        # Load rankings and perform assignment
        if verbose:
            click.echo("Loading rankings and performing team assignment...")
        
        assignments = assigner.assign_teams_from_csv(rankings_file)
        
        # Create output directory if it doesn't exist
        output_dir.mkdir(parents=True, exist_ok=True)
        
        # Generate output files
        csv_output = output_dir / f"{output_prefix}.csv"
        yaml_output = output_dir / f"{output_prefix}.yaml"
        
        if verbose:
            click.echo(f"Writing assignments to: {csv_output}")
            click.echo(f"Writing team summary to: {yaml_output}")
        
        assigner.save_assignments_csv(assignments, rankings_file, csv_output)
        assigner.save_assignments_yaml(assignments, yaml_output)
        
        click.echo("✅ Team assignment completed successfully!")
        click.echo(f"📊 Assignments saved to: {csv_output}")
        click.echo(f"📋 Team summary saved to: {yaml_output}")
        
    except Exception as e:
        click.echo(f"❌ Error: {e}", err=True)
        if verbose:
            import traceback
            click.echo(traceback.format_exc(), err=True)
        sys.exit(1)


if __name__ == "__main__":
    main()
