"""Agent configuration from a TOML file."""

import socket
import tomllib
from dataclasses import dataclass, field
from pathlib import Path

from .logsetup import parse_level
from .update import valid_repo


class ConfigError(ValueError):
    pass


@dataclass(frozen=True)
class Config:
    url: str
    token: str
    agent_id: str
    spool_dir: str
    record_file: str
    telemetry_hz: float
    scoring_hz: float
    raw_file: str = ""
    # Cloudflare Access service token of this agent. Both or none; the secret is left out of repr() so a config dump
    # cannot show it.
    cf_client_id: str = ""
    cf_client_secret: str = field(default="", repr=False)
    log_level: str = "INFO"
    # Self-update from the GitHub releases of the agent, see update.py. Only the single-file Windows build uses it.
    update_enabled: bool = True
    update_repo: str = "3lefeint/splash"
    update_check_hours: float = 6.0


def load(path: str | Path) -> Config:
    try:
        with open(path, "rb") as f:
            raw = tomllib.load(f)
    except FileNotFoundError:
        raise ConfigError(f"config file not found: {path}") from None
    except tomllib.TOMLDecodeError as e:
        raise ConfigError(f"{path}: {e}") from e
    server, agent, poll = raw.get("server", {}), raw.get("agent", {}), raw.get("poll", {})
    base = Path(path).resolve().parent  # relative file names count from the folder of the configuration, not from the
    # folder the program happens to be started in (Windows starts programs of the Run key somewhere else)

    def place(name: str, default: str) -> str:
        value = str(agent.get(name, default))
        return str(base / value) if value and not Path(value).is_absolute() else value

    log_section, update = raw.get("log", {}), raw.get("update", {})
    cfg = Config(
        url=str(server.get("url", "")),
        token=str(server.get("token", "")),
        agent_id=str(agent.get("id", "")).strip() or socket.gethostname(),
        spool_dir=place("spool_dir", "spool"),
        record_file=place("record_file", ""),
        telemetry_hz=float(poll.get("telemetry_hz", 5)),
        scoring_hz=float(poll.get("scoring_hz", 2)),
        raw_file=place("raw_file", ""),
        cf_client_id=str(server.get("cf_client_id", "")),
        cf_client_secret=str(server.get("cf_client_secret", "")),
        log_level=str(log_section.get("level", "INFO")),
        update_enabled=bool(update.get("enabled", True)),
        update_repo=str(update.get("repo", "3lefeint/splash")),
        update_check_hours=float(update.get("check_hours", 6)),
    )
    if not valid_repo(cfg.update_repo):
        raise ConfigError("update.repo must look like owner/name")
    if not 1 <= cfg.update_check_hours <= 168:
        raise ConfigError("update.check_hours must be between 1 and 168")
    if not cfg.url.startswith(("ws://", "wss://")):
        raise ConfigError("server.url must start with ws:// or wss://")
    try:
        parse_level(cfg.log_level)
    except ValueError as e:
        raise ConfigError(f"log.level: {e}") from None
    if bool(cfg.cf_client_id) != bool(cfg.cf_client_secret):
        raise ConfigError("server.cf_client_id and server.cf_client_secret must be set together")
    for name, hz in (("telemetry_hz", cfg.telemetry_hz), ("scoring_hz", cfg.scoring_hz)):
        if not 0.1 <= hz <= 10:
            raise ConfigError(f"poll.{name} must be between 0.1 and 10 (the game must not be disturbed)")
    return cfg
