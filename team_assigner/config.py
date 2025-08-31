"""Configuration management for Team Assigner."""

from pathlib import Path
from typing import List, Optional, Set, Tuple

import yaml


class Config:
    """Configuration class for team assignment settings."""
    
    def __init__(self):
        """Initialize configuration with default values."""
        self.min_team_size: int = 4
        self.exclusions: List[Set[str]] = []
    
    def load_from_file(self, config_path: Path) -> None:
        """Load configuration from a YAML file.
        
        Args:
            config_path: Path to the YAML configuration file
            
        Raises:
            FileNotFoundError: If the config file doesn't exist
            yaml.YAMLError: If the YAML file is invalid
            ValueError: If the configuration structure is invalid
        """
        if not config_path.exists():
            raise FileNotFoundError(f"Configuration file not found: {config_path}")
        
        with open(config_path, 'r', encoding='utf-8') as f:
            config_data = yaml.safe_load(f)
        
        if not isinstance(config_data, dict):
            raise ValueError("Configuration file must contain a YAML dictionary")
        
        team_config = config_data.get('team', {})
        
        # Load minimum team size
        if 'min' in team_config:
            min_size = team_config['min']
            if not isinstance(min_size, int) or min_size < 1:
                raise ValueError("team.min must be a positive integer")
            self.min_team_size = min_size
        
        # Load exclusions
        if 'exclusions' in team_config:
            exclusions = team_config['exclusions']
            if not isinstance(exclusions, list):
                raise ValueError("team.exclusions must be a list")
            
            self.exclusions = []
            for exclusion_group in exclusions:
                if isinstance(exclusion_group, str):
                    # Split comma-separated names
                    names = {name.strip() for name in exclusion_group.split(',')}
                elif isinstance(exclusion_group, list):
                    # List of names
                    names = {str(name).strip() for name in exclusion_group}
                else:
                    raise ValueError(
                        "Each exclusion group must be a comma-separated string or list"
                    )
                
                if len(names) < 2:
                    raise ValueError(
                        "Each exclusion group must contain at least 2 people"
                    )
                
                self.exclusions.append(names)
    
    def validate_exclusions(self, people: Set[str]) -> None:
        """Validate that all people in exclusions exist in the rankings.
        
        Args:
            people: Set of people names from the rankings
            
        Raises:
            ValueError: If exclusion contains people not in rankings
        """
        all_excluded_people = set()
        for exclusion_group in self.exclusions:
            all_excluded_people.update(exclusion_group)
        
        unknown_people = all_excluded_people - people
        if unknown_people:
            raise ValueError(
                f"Exclusions contain unknown people: {sorted(unknown_people)}"
            )
    
    def are_excluded(self, person1: str, person2: str) -> bool:
        """Check if two people are excluded from being on the same team.
        
        Args:
            person1: Name of first person
            person2: Name of second person
            
        Returns:
            True if the people cannot be on the same team
        """
        for exclusion_group in self.exclusions:
            if person1 in exclusion_group and person2 in exclusion_group:
                return True
        return False
    
    def get_excluded_pairs(self) -> List[Tuple[str, str]]:
        """Get all pairs of people that cannot be on the same team.
        
        Returns:
            List of tuples representing excluded pairs
        """
        excluded_pairs = []
        
        for exclusion_group in self.exclusions:
            people_list = sorted(exclusion_group)
            for i, person1 in enumerate(people_list):
                for person2 in people_list[i + 1:]:
                    excluded_pairs.append((person1, person2))
        
        return excluded_pairs
    
    def to_dict(self) -> dict:
        """Convert configuration to dictionary format.
        
        Returns:
            Dictionary representation of the configuration
        """
        config_dict = {
            'team': {
                'min': self.min_team_size
            }
        }
        
        if self.exclusions:
            config_dict['team']['exclusions'] = [
                ','.join(sorted(exclusion_group)) 
                for exclusion_group in self.exclusions
            ]
        
        return config_dict
    
    def save_to_file(self, config_path: Path) -> None:
        """Save configuration to a YAML file.
        
        Args:
            config_path: Path where to save the configuration
        """
        config_dict = self.to_dict()
        
        with open(config_path, 'w', encoding='utf-8') as f:
            yaml.dump(config_dict, f, default_flow_style=False, sort_keys=True)
