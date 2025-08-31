"""Core team assignment logic for Team Assigner."""

import random
from collections import defaultdict
from pathlib import Path
from typing import Dict, List, Set, Tuple

import pandas as pd
import yaml

from .config import Config
from .validators import validate_people_names, validate_assignment_feasibility


class TeamAssigner:
    """Main class for assigning people to teams based on preferences."""
    
    def __init__(self, config: Config):
        """Initialize the team assigner.
        
        Args:
            config: Configuration object with team settings
        """
        self.config = config
    
    def assign_teams_from_csv(self, rankings_file: Path) -> Dict[str, int]:
        """Assign teams based on rankings from a CSV file.
        
        Args:
            rankings_file: Path to CSV file with rankings
            
        Returns:
            Dictionary mapping person names to their assigned preference numbers
            
        Raises:
            ValueError: If assignment is not feasible
        """
        # Load rankings from CSV
        df = pd.read_csv(rankings_file)
        people = set(df.columns)
        num_preferences = len(df)
        
        # Validate inputs
        validate_people_names(people)
        validate_assignment_feasibility(len(people), self.config.min_team_size)
        self.config.validate_exclusions(people)
        
        # Convert rankings to preference matrix
        # preferences[person][preference_idx] = ranking
        preferences = {}
        for person in people:
            preferences[person] = df[person].tolist()
        
        # Perform team assignment
        assignments = self._assign_teams_optimized(people, preferences, num_preferences)
        
        return assignments
    
    def _assign_teams_optimized(
        self, 
        people: Set[str], 
        preferences: Dict[str, List[int]], 
        num_preferences: int
    ) -> Dict[str, int]:
        """Assign people to teams using an optimization approach.
        
        This method tries to minimize the total "dissatisfaction" by:
        1. Creating all possible team combinations that satisfy constraints
        2. Scoring each combination based on preference rankings
        3. Selecting the combination with the best overall score
        
        For larger groups, uses a greedy heuristic approach.
        
        Args:
            people: Set of all people to assign
            preferences: Dictionary of person -> list of rankings
            num_preferences: Number of preference options
            
        Returns:
            Dictionary mapping person names to their assigned preference numbers
        """
        people_list = list(people)
        
        # For small groups, try exhaustive search
        if len(people) <= 12:
            return self._assign_teams_exhaustive(people_list, preferences, num_preferences)
        else:
            return self._assign_teams_greedy(people_list, preferences, num_preferences)
    
    def _assign_teams_exhaustive(
        self, 
        people: List[str], 
        preferences: Dict[str, List[int]], 
        num_preferences: int
    ) -> Dict[str, int]:
        """Use exhaustive search for small groups to find optimal assignment."""
        from itertools import combinations
        
        best_assignment = None
        best_score = float('inf')
        
        # Try each preference as the assignment target
        for pref_idx in range(num_preferences):
            # Create teams for this preference
            teams = self._create_teams_for_preference(people, pref_idx + 1)
            
            if teams:  # Valid team configuration found
                score = self._score_assignment(teams, preferences, pref_idx)
                if score < best_score:
                    best_score = score
                    best_assignment = {person: pref_idx + 1 for team in teams for person in team}
        
        if best_assignment is None:
            # Fallback: assign people to their top preferences, ignoring team constraints
            best_assignment = self._assign_fallback(people, preferences)
        
        return best_assignment
    
    def _assign_teams_greedy(
        self, 
        people: List[str], 
        preferences: Dict[str, List[int]], 
        num_preferences: int
    ) -> Dict[str, int]:
        """Use greedy heuristic for larger groups."""
        # Sort people by how "picky" they are (sum of rankings)
        people_by_pickiness = sorted(
            people, 
            key=lambda p: sum(preferences[p])
        )
        
        assignment = {}
        remaining_people = set(people)
        
        # Try to assign teams for each preference, starting with most preferred
        for pref_idx in range(num_preferences):
            if not remaining_people:
                break
            
            # Find people who prefer this option
            candidates = [
                p for p in remaining_people 
                if preferences[p][pref_idx] <= 2  # Top 2 preferences
            ]
            
            if len(candidates) >= self.config.min_team_size:
                # Create a team from these candidates
                team = self._create_compatible_team(candidates, self.config.min_team_size)
                if team:
                    for person in team:
                        assignment[person] = pref_idx + 1
                        remaining_people.remove(person)
        
        # Assign remaining people to their best available options
        for person in remaining_people:
            best_pref = 1  # Default to first preference
            for pref_idx in range(num_preferences):
                if preferences[person][pref_idx] <= len(remaining_people):
                    best_pref = pref_idx + 1
                    break
            assignment[person] = best_pref
        
        return assignment
    
    def _create_teams_for_preference(
        self, 
        people: List[str], 
        preference_num: int
    ) -> List[List[str]]:
        """Create valid teams for a specific preference."""
        # Simple approach: try to group people into minimum-sized teams
        # This is a simplified version - a more sophisticated algorithm would
        # consider preference rankings and exclusions more carefully
        
        available_people = people.copy()
        teams = []
        
        while len(available_people) >= self.config.min_team_size:
            team = self._create_compatible_team(available_people, self.config.min_team_size)
            if not team:
                break  # Can't create more compatible teams
            
            teams.append(team)
            for person in team:
                available_people.remove(person)
        
        # Handle remaining people (assign to existing teams if possible)
        for person in available_people:
            # Find a team this person can join
            for team in teams:
                if self._can_join_team(person, team):
                    team.append(person)
                    break
            else:
                # Can't join any existing team, create a new one if we have enough people
                remaining = [p for p in available_people if p != person]
                if len(remaining) + 1 >= self.config.min_team_size:
                    new_team = [person]
                    for other in remaining[:self.config.min_team_size - 1]:
                        if self._can_join_team(other, new_team):
                            new_team.append(other)
                            available_people.remove(other)
                    if len(new_team) >= self.config.min_team_size:
                        teams.append(new_team)
                        available_people.remove(person)
        
        return teams
    
    def _create_compatible_team(self, candidates: List[str], team_size: int) -> List[str]:
        """Create a team of specified size from candidates, respecting exclusions."""
        if len(candidates) < team_size:
            return []
        
        # Simple greedy approach: start with first candidate and add compatible people
        team = [candidates[0]]
        
        for candidate in candidates[1:]:
            if len(team) >= team_size:
                break
            
            if self._can_join_team(candidate, team):
                team.append(candidate)
        
        return team if len(team) >= team_size else []
    
    def _can_join_team(self, person: str, team: List[str]) -> bool:
        """Check if a person can join a team (no exclusions)."""
        for team_member in team:
            if self.config.are_excluded(person, team_member):
                return False
        return True
    
    def _score_assignment(
        self, 
        teams: List[List[str]], 
        preferences: Dict[str, List[int]], 
        preference_idx: int
    ) -> float:
        """Score a team assignment based on preference satisfaction."""
        total_score = 0.0
        
        for team in teams:
            for person in team:
                # Lower ranking = better score, so we want to minimize
                ranking = preferences[person][preference_idx]
                total_score += ranking
        
        return total_score
    
    def _assign_fallback(self, people: List[str], preferences: Dict[str, List[int]]) -> Dict[str, int]:
        """Fallback assignment: give everyone their top preference."""
        assignment = {}
        
        for person in people:
            # Find their best preference (lowest ranking number)
            best_ranking = min(preferences[person])
            best_pref_idx = preferences[person].index(best_ranking)
            assignment[person] = best_pref_idx + 1
        
        return assignment
    
    def save_assignments_csv(
        self, 
        assignments: Dict[str, int], 
        original_csv: Path,
        output_path: Path
    ) -> None:
        """Save assignments to CSV format matching the original file structure.
        
        Args:
            assignments: Dictionary mapping person -> assigned preference number
            original_csv: Path to original rankings CSV (for header structure)
            output_path: Path where to save the assignments CSV
        """
        # Read original CSV to get the column structure
        original_df = pd.read_csv(original_csv)
        
        # Create a single row with assignments
        assignment_row = []
        for person in original_df.columns:
            assignment_row.append(assignments.get(person, 1))  # Default to 1 if not found
        
        # Create new DataFrame
        assignment_df = pd.DataFrame([assignment_row], columns=original_df.columns)
        
        # Save to CSV
        assignment_df.to_csv(output_path, index=False)
    
    def save_assignments_yaml(
        self, 
        assignments: Dict[str, int], 
        output_path: Path
    ) -> None:
        """Save assignments to YAML format with teams grouped by preference.
        
        Args:
            assignments: Dictionary mapping person -> assigned preference number
            output_path: Path where to save the assignments YAML
        """
        # Group people by their assigned preference
        teams_by_preference = defaultdict(list)
        for person, preference in assignments.items():
            teams_by_preference[preference].append(person)
        
        # Create YAML structure
        yaml_data = {'team': {}}
        for preference, people in sorted(teams_by_preference.items()):
            yaml_data['team'][preference] = sorted(people)
        
        # Save to YAML file
        with open(output_path, 'w', encoding='utf-8') as f:
            yaml.dump(yaml_data, f, default_flow_style=False, sort_keys=True)
    
    def get_assignment_summary(self, assignments: Dict[str, int]) -> Dict[str, any]:
        """Get a summary of the assignment results.
        
        Args:
            assignments: Dictionary mapping person -> assigned preference number
            
        Returns:
            Dictionary with assignment statistics
        """
        if not assignments:
            return {
                'total_people': 0,
                'teams': {},
                'team_sizes': {},
                'average_team_size': 0.0
            }
        
        # Group by preference
        teams_by_preference = defaultdict(list)
        for person, preference in assignments.items():
            teams_by_preference[preference].append(person)
        
        team_sizes = {pref: len(people) for pref, people in teams_by_preference.items()}
        average_size = sum(team_sizes.values()) / len(team_sizes) if team_sizes else 0.0
        
        return {
            'total_people': len(assignments),
            'teams': dict(teams_by_preference),
            'team_sizes': team_sizes,
            'average_team_size': round(average_size, 2)
        }
