"""Team Assigner - A tool to assign people to teams based on their preferences."""

__version__ = "0.1.0"
__author__ = "Your Name"
__email__ = "your.email@example.com"

from .assigner import TeamAssigner
from .config import Config

__all__ = ["TeamAssigner", "Config"]
