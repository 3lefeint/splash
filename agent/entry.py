"""Entry point for the PyInstaller build (the package's __main__ uses relative imports)."""
import sys

from splash_agent.main import cli

sys.exit(cli())
