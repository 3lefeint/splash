"""Starts the agent when the user signs in to Windows.

The entry lives in the user's own Run key (HKCU), so it needs no administrator rights and the agent runs in the
user's session, which it needs to see the plugin buffers of the game. It is offered once, at the first interactive
start. The answer is remembered in the spool folder; ``splash-agent autostart on|off|status`` changes it later.
"""

import json
import logging
import sys
from pathlib import Path
from typing import Callable, Protocol

log = logging.getLogger("splash_agent")

RUN_KEY = r"Software\Microsoft\Windows\CurrentVersion\Run"
VALUE_NAME = "SplashAgent"
MARKER = "autostart.json"
QUESTION = "Start the Splash agent automatically when you sign in to Windows? [y/N] "


class Registry(Protocol):
    def get(self, name: str) -> str | None: ...
    def set(self, name: str, value: str) -> None: ...
    def delete(self, name: str) -> None: ...


class WindowsRegistry:
    """The Run key of the current user."""

    def get(self, name: str) -> str | None:
        import winreg

        try:
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY) as key:
                value, _ = winreg.QueryValueEx(key, name)
                return str(value)
        except FileNotFoundError:
            return None

    def set(self, name: str, value: str) -> None:
        import winreg

        with winreg.CreateKey(winreg.HKEY_CURRENT_USER, RUN_KEY) as key:
            winreg.SetValueEx(key, name, 0, winreg.REG_SZ, value)

    def delete(self, name: str) -> None:
        import winreg

        try:
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, RUN_KEY, 0, winreg.KEY_SET_VALUE) as key:
                winreg.DeleteValue(key, name)
        except FileNotFoundError:
            pass


def command_line(config: Path) -> str:
    """The command the entry runs: this program with the configuration it was started with, all paths absolute because
    the folder Windows starts it in is not the agent's folder."""
    config = Path(config).resolve()
    if getattr(sys, "frozen", False):  # the single-file build
        return f'"{sys.executable}" run --config "{config}"'
    return f'"{sys.executable}" -m splash_agent run --config "{config}"'


def is_enabled(registry: Registry) -> bool:
    return registry.get(VALUE_NAME) is not None


def enable(registry: Registry, config: Path) -> None:
    registry.set(VALUE_NAME, command_line(config))


def disable(registry: Registry) -> None:
    registry.delete(VALUE_NAME)


def _marker(spool_dir: str | Path) -> Path:
    return Path(spool_dir) / MARKER


def asked(spool_dir: str | Path) -> bool:
    try:
        return bool(json.loads(_marker(spool_dir).read_text(encoding="utf-8")).get("asked"))
    except (OSError, ValueError, AttributeError):
        return False


def _remember(spool_dir: str | Path, enabled: bool) -> None:
    try:
        path = _marker(spool_dir)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json.dumps({"asked": True, "enabled": enabled}), encoding="utf-8")
    except OSError as e:
        log.warning("could not remember the answer about starting with Windows: %s", e)


def offer(spool_dir: str | Path, config: Path, *, registry: Registry | None = None,
          ask: Callable[[str], str] = input, interactive: bool | None = None, platform: str | None = None) -> None:
    """Asks once whether to start with Windows. Never raises: this is a convenience and must not stop the agent."""
    try:
        if (platform or sys.platform) != "win32":
            return
        registry = registry or WindowsRegistry()
        if is_enabled(registry):
            # Keep the entry pointing at this program and this configuration (the folder may have been moved).
            wanted = command_line(config)
            if registry.get(VALUE_NAME) != wanted:
                registry.set(VALUE_NAME, wanted)
                log.info("start with Windows: entry updated to %s", wanted)
            return
        if asked(spool_dir):
            return
        if interactive is None:
            interactive = sys.stdin is not None and sys.stdin.isatty()
        if not interactive:
            return
        try:
            answer = ask(QUESTION).strip().lower()
        except (EOFError, KeyboardInterrupt):
            return  # not answered: ask again next time
        if answer in ("y", "yes", "j", "ja"):
            enable(registry, config)
            _remember(spool_dir, True)
            log.info("the agent will start when you sign in to Windows (turn off with: splash-agent autostart off)")
        else:
            _remember(spool_dir, False)
            log.info("the agent will not start with Windows (turn on with: splash-agent autostart on)")
    except Exception:
        log.warning("start with Windows could not be set up", exc_info=True)
