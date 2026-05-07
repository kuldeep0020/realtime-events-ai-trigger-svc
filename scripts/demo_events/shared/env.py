"""Auto-load .env.local for fish-shell and other shells that don't source files.

Fish shell cannot use `set -a; source .env.local; set +a` (bash syntax).
This module auto-discovers and loads .env.local at startup so that demo CLI
scripts work with `uv run demo_realestate.py` without manual env sourcing.

Priority:
  1. Explicit --env-file PATH (raises if file missing)
  2. scripts/demo_events/.env.local  (next to pyproject.toml)
  3. repo-root .env.local            (two levels above scripts/demo_events)
  4. Nothing loaded — rely on process env (bash users who already sourced it)

`override=False` ensures process env always beats the file, so bash users
who export vars manually are not affected.
"""
from __future__ import annotations

from pathlib import Path

from dotenv import load_dotenv

# Candidate paths searched in order when no explicit path is given.
_DEFAULT_ENV_PATHS = [
    Path(__file__).resolve().parents[1] / ".env.local",  # scripts/demo_events/.env.local
    Path(__file__).resolve().parents[3] / ".env.local",  # repo-root .env.local
]


def autoload_env(explicit: str | None = None) -> str | None:
    """Load env vars from the first .env.local found. Returns the path used or None.

    Args:
        explicit: Path supplied via --env-file flag.  If given and missing,
                  raises FileNotFoundError.

    Returns:
        The path string of the file that was loaded, or None if nothing loaded.
    """
    if explicit is not None:
        p = Path(explicit)
        if not p.is_file():
            raise FileNotFoundError(f"--env-file {explicit!r} does not exist")
        load_dotenv(p, override=False)
        return str(p)

    for p in _DEFAULT_ENV_PATHS:
        if p.is_file():
            load_dotenv(p, override=False)
            return str(p)

    return None  # nothing loaded; caller uses process env as-is
