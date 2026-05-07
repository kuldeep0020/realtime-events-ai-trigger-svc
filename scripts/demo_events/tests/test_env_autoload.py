"""Tests for shared.env.autoload_env and argparse default ordering."""
from __future__ import annotations

import os
import textwrap
from pathlib import Path

import pytest

from shared.env import autoload_env


class TestAutoloadEnv:
    """Unit tests for autoload_env."""

    def test_returns_none_when_no_env_file_and_no_token(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        """With no .env.local on disk and PULSAR_JWT_TOKEN absent, autoload_env returns None."""
        # Patch _DEFAULT_ENV_PATHS to point at non-existent files inside tmp_path.
        monkeypatch.setattr(
            "shared.env._DEFAULT_ENV_PATHS",
            [tmp_path / ".env.local.nonexistent"],
        )
        monkeypatch.delenv("PULSAR_JWT_TOKEN", raising=False)

        result = autoload_env(None)

        assert result is None
        assert "PULSAR_JWT_TOKEN" not in os.environ

    def test_loads_explicit_env_file(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        """With a temp .env.local containing FOO=bar, autoload_env loads it."""
        env_file = tmp_path / ".env.local"
        env_file.write_text("FOO=bar\n")

        monkeypatch.delenv("FOO", raising=False)

        result = autoload_env(str(env_file))

        assert result == str(env_file)
        assert os.environ.get("FOO") == "bar"

        # Cleanup — don't pollute other tests.
        monkeypatch.delenv("FOO", raising=False)

    def test_process_env_not_overwritten(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        """Process env takes priority over .env.local (override=False)."""
        env_file = tmp_path / ".env.local"
        env_file.write_text("MY_VAR=from_file\n")

        monkeypatch.setenv("MY_VAR", "from_process")

        autoload_env(str(env_file))

        # Process env value must survive.
        assert os.environ.get("MY_VAR") == "from_process"

        monkeypatch.delenv("MY_VAR", raising=False)

    def test_raises_for_missing_explicit_path(self, tmp_path: Path) -> None:
        """FileNotFoundError raised when --env-file path does not exist."""
        missing = str(tmp_path / "does_not_exist.env")
        with pytest.raises(FileNotFoundError, match="does_not_exist.env"):
            autoload_env(missing)

    def test_auto_detects_default_path(self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
        """Auto-detection picks the first existing path from _DEFAULT_ENV_PATHS."""
        env_file = tmp_path / ".env.local"
        env_file.write_text("AUTO_DETECT_VAR=detected\n")

        monkeypatch.setattr("shared.env._DEFAULT_ENV_PATHS", [env_file])
        monkeypatch.delenv("AUTO_DETECT_VAR", raising=False)

        result = autoload_env(None)

        assert result == str(env_file)
        assert os.environ.get("AUTO_DETECT_VAR") == "detected"

        monkeypatch.delenv("AUTO_DETECT_VAR", raising=False)


class TestArgparseDefaultsResolveAfterAutoload:
    """Prove that argparse defaults in each demo CLI see env-file values.

    The fix (two-phase parse) ensures autoload_env() is called before
    parse_args(), so os.environ.get(...) inside parse_args() resolves
    against the freshly-loaded file rather than the pre-load process env.
    """

    def _make_env_file(self, tmp_path: Path, contents: str) -> str:
        f = tmp_path / ".env.local"
        f.write_text(contents)
        return str(f)

    def test_realestate_write_key_from_env_file(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """demo_realestate: --write-key default picks up WRITE_KEY_REALESTATE from env file."""
        env_path = self._make_env_file(tmp_path, "WRITE_KEY_REALESTATE=key_from_file_re\n")
        monkeypatch.delenv("WRITE_KEY_REALESTATE", raising=False)

        # Import inside test so module-level state is isolated.
        import importlib
        import demo_realestate as mod
        importlib.reload(mod)

        env_file, _ = mod._early_parse_env_file(["--env-file", env_path])
        autoload_env(env_file)
        args = mod.parse_args(["--env-file", env_path])

        assert args.write_key == "key_from_file_re"
        monkeypatch.delenv("WRITE_KEY_REALESTATE", raising=False)

    def test_rs_self_write_key_from_env_file(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """demo_rs_self: --write-key default picks up WRITE_KEY_RS_SELF from env file."""
        env_path = self._make_env_file(tmp_path, "WRITE_KEY_RS_SELF=key_from_file_rs\n")
        monkeypatch.delenv("WRITE_KEY_RS_SELF", raising=False)

        import importlib
        import demo_rs_self as mod
        importlib.reload(mod)

        env_file, _ = mod._early_parse_env_file(["--env-file", env_path])
        autoload_env(env_file)
        args = mod.parse_args(["--env-file", env_path])

        assert args.write_key == "key_from_file_rs"
        monkeypatch.delenv("WRITE_KEY_RS_SELF", raising=False)

    def test_combined_write_keys_from_env_file(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """demo_combined: both --write-key-re/rs defaults pick up env-file values."""
        env_path = self._make_env_file(
            tmp_path,
            "WRITE_KEY_REALESTATE=re_from_file\nWRITE_KEY_RS_SELF=rs_from_file\n",
        )
        monkeypatch.delenv("WRITE_KEY_REALESTATE", raising=False)
        monkeypatch.delenv("WRITE_KEY_RS_SELF", raising=False)

        import importlib
        import demo_combined as mod
        importlib.reload(mod)

        env_file, _ = mod._early_parse_env_file(["--env-file", env_path])
        autoload_env(env_file)
        args = mod.parse_args(["--env-file", env_path])

        assert args.write_key_re == "re_from_file"
        assert args.write_key_rs == "rs_from_file"
        monkeypatch.delenv("WRITE_KEY_REALESTATE", raising=False)
        monkeypatch.delenv("WRITE_KEY_RS_SELF", raising=False)

    def test_ingestion_url_from_env_file(
        self, tmp_path: Path, monkeypatch: pytest.MonkeyPatch
    ) -> None:
        """INGESTION_URL in env file is picked up by parse_args default."""
        env_path = self._make_env_file(tmp_path, "INGESTION_URL=https://override.example.com\n")
        monkeypatch.delenv("INGESTION_URL", raising=False)

        import importlib
        import demo_realestate as mod
        importlib.reload(mod)

        env_file, _ = mod._early_parse_env_file(["--env-file", env_path])
        autoload_env(env_file)
        args = mod.parse_args(["--env-file", env_path])

        assert args.ingestion_url == "https://override.example.com"
        monkeypatch.delenv("INGESTION_URL", raising=False)
