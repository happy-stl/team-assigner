"""Validation utilities for Team Assigner."""

from pathlib import Path
from typing import Set

import pandas as pd


def validate_rankings_csv(csv_path: Path) -> None:
    """Validate a rankings CSV file.
    
    Ensures the CSV file has the correct structure for team assignment:
    - Has at least 2 columns (people)
    - Has at least 1 row of data (preferences)
    - All values are positive integers
    - Each column contains rankings from 1 to N (where N is number of preferences)
    - No duplicate rankings within a column
    
    Args:
        csv_path: Path to the CSV file to validate
        
    Raises:
        FileNotFoundError: If the CSV file doesn't exist
        ValueError: If the CSV structure or content is invalid
        pd.errors.EmptyDataError: If the CSV file is empty
    """
    if not csv_path.exists():
        raise FileNotFoundError(f"Rankings file not found: {csv_path}")
    
    try:
        # Read CSV file
        df = pd.read_csv(csv_path)
    except pd.errors.EmptyDataError:
        raise ValueError("Rankings CSV file is empty")
    except Exception as e:
        raise ValueError(f"Failed to read CSV file: {e}")
    
    # Check basic structure
    if df.shape[0] == 0:
        raise ValueError("Rankings CSV must contain at least 1 preference row")
    
    if df.shape[1] < 2:
        raise ValueError("Rankings CSV must contain at least 2 people (columns)")
    
    num_preferences = df.shape[0]
    expected_rankings = set(range(1, num_preferences + 1))
    
    # Validate each person's rankings
    for column in df.columns:
        column_data = df[column]
        
        # Check for missing values
        if column_data.isna().any():
            raise ValueError(f"Column '{column}' contains missing values")
        
        # Check that all values are integers
        try:
            rankings = column_data.astype(int)
        except (ValueError, TypeError):
            raise ValueError(f"Column '{column}' contains non-integer values")
        
        # Check that all values are positive
        if (rankings <= 0).any():
            raise ValueError(f"Column '{column}' contains non-positive values")
        
        # Check that rankings are in valid range
        rankings_set = set(rankings)
        if rankings_set != expected_rankings:
            missing = expected_rankings - rankings_set
            extra = rankings_set - expected_rankings
            
            error_parts = []
            if missing:
                error_parts.append(f"missing rankings: {sorted(missing)}")
            if extra:
                error_parts.append(f"invalid rankings: {sorted(extra)}")
            
            raise ValueError(
                f"Column '{column}' has {', '.join(error_parts)}. "
                f"Expected rankings from 1 to {num_preferences}"
            )
        
        # Check for duplicate rankings (shouldn't happen if above checks pass, but be safe)
        if len(rankings) != len(rankings_set):
            raise ValueError(f"Column '{column}' contains duplicate rankings")


def validate_people_names(people: Set[str]) -> None:
    """Validate people names from the rankings.
    
    Args:
        people: Set of people names to validate
        
    Raises:
        ValueError: If people names are invalid
    """
    if not people:
        raise ValueError("No people found in rankings")
    
    # Check for empty or whitespace-only names
    for person in people:
        if not person or not person.strip():
            raise ValueError("People names cannot be empty or whitespace-only")
    
    # Check for very long names (likely data issue)
    for person in people:
        if len(person) > 100:
            raise ValueError(f"Person name too long (max 100 chars): '{person[:50]}...'")


def validate_assignment_feasibility(num_people: int, min_team_size: int) -> None:
    """Validate that team assignment is feasible given constraints.
    
    Args:
        num_people: Total number of people to assign
        min_team_size: Minimum size for each team
        
    Raises:
        ValueError: If assignment is not feasible
    """
    if num_people < min_team_size:
        raise ValueError(
            f"Cannot create teams: {num_people} people < minimum team size {min_team_size}"
        )
    
    # Check if we can form at least one complete team
    if num_people < min_team_size:
        raise ValueError(
            f"Insufficient people for minimum team size: "
            f"{num_people} people, {min_team_size} minimum per team"
        )
