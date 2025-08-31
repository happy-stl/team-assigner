"""Tests for the validators module."""

import tempfile
from pathlib import Path

import pandas as pd
import pytest

from team_assigner.validators import (
    validate_rankings_csv,
    validate_people_names,
    validate_assignment_feasibility,
)


class TestValidateRankingsCSV:
    """Test cases for CSV ranking validation."""
    
    def test_valid_csv(self):
        """Test validation of a valid rankings CSV."""
        data = {
            'John': [1, 2, 3],
            'Linda': [2, 3, 1],
            'James': [3, 1, 2]
        }
        df = pd.DataFrame(data)
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.csv', delete=False) as f:
            df.to_csv(f.name, index=False)
            csv_path = Path(f.name)
        
        try:
            validate_rankings_csv(csv_path)  # Should not raise
        finally:
            csv_path.unlink()
    
    def test_missing_rankings(self):
        """Test validation with missing ranking values."""
        data = {
            'John': [1, 2, 3],
            'Linda': [2, None, 1],  # Missing value
            'James': [3, 1, 2]
        }
        df = pd.DataFrame(data)
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.csv', delete=False) as f:
            df.to_csv(f.name, index=False)
            csv_path = Path(f.name)
        
        try:
            with pytest.raises(ValueError, match="missing values"):
                validate_rankings_csv(csv_path)
        finally:
            csv_path.unlink()
    
    def test_duplicate_rankings(self):
        """Test validation with duplicate rankings in a column."""
        data = {
            'John': [1, 1, 3],  # Duplicate ranking
            'Linda': [2, 3, 1],
            'James': [3, 2, 2]   # Duplicate ranking
        }
        df = pd.DataFrame(data)
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.csv', delete=False) as f:
            df.to_csv(f.name, index=False)
            csv_path = Path(f.name)
        
        try:
            with pytest.raises(ValueError, match="missing rankings"):
                validate_rankings_csv(csv_path)
        finally:
            csv_path.unlink()
    
    def test_invalid_ranking_range(self):
        """Test validation with rankings outside valid range."""
        data = {
            'John': [1, 2, 4],    # 4 is invalid for 3 preferences
            'Linda': [2, 3, 1],
            'James': [0, 1, 2]    # 0 is invalid
        }
        df = pd.DataFrame(data)
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.csv', delete=False) as f:
            df.to_csv(f.name, index=False)
            csv_path = Path(f.name)
        
        try:
            with pytest.raises(ValueError, match="invalid rankings"):
                validate_rankings_csv(csv_path)
        finally:
            csv_path.unlink()
    
    def test_too_few_columns(self):
        """Test validation with insufficient columns."""
        data = {
            'John': [1, 2, 3]  # Only one person
        }
        df = pd.DataFrame(data)
        
        with tempfile.NamedTemporaryFile(mode='w', suffix='.csv', delete=False) as f:
            df.to_csv(f.name, index=False)
            csv_path = Path(f.name)
        
        try:
            with pytest.raises(ValueError, match="at least 2 people"):
                validate_rankings_csv(csv_path)
        finally:
            csv_path.unlink()
    
    def test_empty_csv(self):
        """Test validation with empty CSV."""
        with tempfile.NamedTemporaryFile(mode='w', suffix='.csv', delete=False) as f:
            f.write("")  # Empty file
            csv_path = Path(f.name)
        
        try:
            with pytest.raises(ValueError, match="empty"):
                validate_rankings_csv(csv_path)
        finally:
            csv_path.unlink()


class TestValidatePeopleNames:
    """Test cases for people name validation."""
    
    def test_valid_names(self):
        """Test validation with valid people names."""
        people = {'Alice', 'Bob', 'Charlie'}
        validate_people_names(people)  # Should not raise
    
    def test_empty_names_set(self):
        """Test validation with empty set of names."""
        people = set()
        
        with pytest.raises(ValueError, match="No people found"):
            validate_people_names(people)
    
    def test_empty_name_string(self):
        """Test validation with empty name strings."""
        people = {'Alice', '', 'Bob'}
        
        with pytest.raises(ValueError, match="cannot be empty"):
            validate_people_names(people)
    
    def test_whitespace_only_name(self):
        """Test validation with whitespace-only names."""
        people = {'Alice', '   ', 'Bob'}
        
        with pytest.raises(ValueError, match="cannot be empty"):
            validate_people_names(people)


class TestValidateAssignmentFeasibility:
    """Test cases for assignment feasibility validation."""
    
    def test_feasible_assignment(self):
        """Test validation with feasible assignment parameters."""
        validate_assignment_feasibility(6, 2)  # 6 people, min team size 2
        validate_assignment_feasibility(10, 3)  # 10 people, min team size 3
    
    def test_insufficient_people(self):
        """Test validation when there aren't enough people for minimum team size."""
        with pytest.raises(ValueError, match="Cannot create teams"):
            validate_assignment_feasibility(1, 2)  # 1 person, need min 2
        
        with pytest.raises(ValueError, match="Insufficient people"):
            validate_assignment_feasibility(2, 3)  # 2 people, need min 3
