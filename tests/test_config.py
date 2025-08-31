"""Tests for the config module."""

import tempfile
from pathlib import Path

import pytest
import yaml

from team_assigner.config import Config


class TestConfig:
    """Test cases for the Config class."""
    
    def test_default_initialization(self):
        """Test that Config initializes with correct defaults."""
        config = Config()
        assert config.min_team_size == 2
        assert config.exclusions == []
    
    def test_load_minimum_team_size(self):
        """Test loading minimum team size from YAML."""
        config_data = {
            'team': {
                'min': 3
            }
        }
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            yaml.dump(config_data, f)
            config_path = Path(f.name)
        
        try:
            config = Config()
            config.load_from_file(config_path)
            assert config.min_team_size == 3
        finally:
            config_path.unlink()
    
    def test_load_exclusions_string_format(self):
        """Test loading exclusions in comma-separated string format."""
        config_data = {
            'team': {
                'exclusions': [
                    'Alice,Bob',
                    'Charlie,David,Eve'
                ]
            }
        }
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            yaml.dump(config_data, f)
            config_path = Path(f.name)
        
        try:
            config = Config()
            config.load_from_file(config_path)
            
            assert len(config.exclusions) == 2
            assert {'Alice', 'Bob'} in config.exclusions
            assert {'Charlie', 'David', 'Eve'} in config.exclusions
        finally:
            config_path.unlink()
    
    def test_are_excluded(self):
        """Test checking if two people are excluded from same team."""
        config = Config()
        config.exclusions = [
            {'Alice', 'Bob'},
            {'Charlie', 'David'}
        ]
        
        assert config.are_excluded('Alice', 'Bob')
        assert config.are_excluded('Bob', 'Alice')
        assert config.are_excluded('Charlie', 'David')
        assert not config.are_excluded('Alice', 'Charlie')
        assert not config.are_excluded('Bob', 'David')
    
    def test_validate_exclusions_success(self):
        """Test successful exclusion validation."""
        config = Config()
        config.exclusions = [{'Alice', 'Bob'}, {'Charlie', 'David'}]
        
        people = {'Alice', 'Bob', 'Charlie', 'David', 'Eve'}
        config.validate_exclusions(people)  # Should not raise
    
    def test_validate_exclusions_failure(self):
        """Test exclusion validation with unknown people."""
        config = Config()
        config.exclusions = [{'Alice', 'Bob'}, {'Charlie', 'Unknown'}]
        
        people = {'Alice', 'Bob', 'Charlie', 'David'}
        
        with pytest.raises(ValueError, match="unknown people"):
            config.validate_exclusions(people)
    
    def test_invalid_config_structure(self):
        """Test handling of invalid configuration structures."""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.yaml', delete=False) as f:
            f.write("invalid: yaml: structure: [")
            config_path = Path(f.name)
        
        try:
            config = Config()
            with pytest.raises(yaml.YAMLError):
                config.load_from_file(config_path)
        finally:
            config_path.unlink()
    
    def test_nonexistent_config_file(self):
        """Test handling of nonexistent configuration file."""
        config = Config()
        nonexistent_path = Path('/nonexistent/config.yaml')
        
        with pytest.raises(FileNotFoundError):
            config.load_from_file(nonexistent_path)
