"""Agent configuration from a TOML file."""

import tomllib
from dataclasses import dataclass
from pathlib import Path


class ConfigError(ValueError):
    pass


@dataclass(frozen=True)
class Config:
    url: str
    token: str
    agent_id: str
    role: str
    spool_dir: str
    record_file: str
    telemetry_hz: float
    scoring_hz: float
    raw_file: str = ""


def load(path: str | Path) -> Config:
    try:
        with open(path, "rb") as f:
            raw = tomllib.load(f)
    except FileNotFoundError:
        raise ConfigError(f"config file not found: {path}") from None
    except tomllib.TOMLDecodeError as e:
        raise ConfigError(f"{path}: {e}") from e
    server, agent, poll = raw.get("server", {}), raw.get("agent", {}), raw.get("poll", {})
    cfg = Config(
        url=str(server.get("url", "")),
        token=str(server.get("token", "")),
        agent_id=str(agent.get("id", "")),
        role=str(agent.get("role", "driver")),
        spool_dir=str(agent.get("spool_dir", "spool")),
        record_file=str(agent.get("record_file", "")),
        telemetry_hz=float(poll.get("telemetry_hz", 5)),
        scoring_hz=float(poll.get("scoring_hz", 2)),
        raw_file=str(agent.get("raw_file", "")),
    )
    if not cfg.url.startswith(("ws://", "wss://")):
        raise ConfigError("server.url must start with ws:// or wss://")
    if not cfg.agent_id:
        raise ConfigError("agent.id is required")
    if cfg.role not in ("driver", "spotter"):
        raise ConfigError("agent.role must be driver or spotter")
    for name, hz in (("telemetry_hz", cfg.telemetry_hz), ("scoring_hz", cfg.scoring_hz)):
        if not 0.1 <= hz <= 10:
            raise ConfigError(f"poll.{name} must be between 0.1 and 10 (the game must not be disturbed)")
    return cfg
