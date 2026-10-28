"""Logging of the agent: one line per event on the console (stderr) of the open command window.

The level comes from ``log.level`` in the config file or ``--log-level`` on the command line. The handshake headers
of the WebSocket library carry the Splash token and the Cloudflare secret, so that library never logs below WARNING,
whatever level is set, and every line passes a filter that blanks known secrets and anything that looks like one.
"""

import logging
import re
import sys
from collections.abc import Iterable

LEVELS = {"DEBUG": logging.DEBUG, "INFO": logging.INFO, "WARNING": logging.WARNING, "WARN": logging.WARNING,
          "ERROR": logging.ERROR}
FORMAT = "%(asctime)s %(levelname)s %(name)s: %(message)s"
# Loggers of libraries that print request or response headers at DEBUG.
QUIET_LIBRARIES = ("websockets",)

_secrets: list[str] = []
_PATTERNS = (
    (re.compile(r"(?i)bearer\s+[A-Za-z0-9._~+/=\-]+"), "Bearer <redacted>"),
    (re.compile(r"eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]*"), "<jwt>"),
    (re.compile(r"(?i)(authorization|cf-access-client-secret|cf-access-client-id)(['\"]?\s*[:=]\s*)\S+"), r"\1\2<redacted>"),
    (re.compile(r"(?i)([a-z][a-z0-9+.\-]*://)[^/\s:@]*:[^/\s@]*@"), r"\1<redacted>@"),
)


def parse_level(name: str) -> int:
    try:
        return LEVELS[str(name).strip().upper()]
    except KeyError:
        raise ValueError(f"log level must be one of DEBUG, INFO, WARNING, ERROR, got {name!r}") from None


def register_secrets(*values: str) -> None:
    """Values that must never appear in a line, whatever the text around them (tokens, the Cloudflare secret)."""
    for v in values:
        if v and len(v) >= 4 and v not in _secrets:
            _secrets.append(v)


def scrub(text: str) -> str:
    for s in _secrets:
        text = text.replace(s, "<redacted>")
    for pattern, repl in _PATTERNS:
        text = pattern.sub(repl, text)
    return text


class Scrubber(logging.Filter):
    """Blanks secrets in the finished text of every line."""

    def filter(self, record: logging.LogRecord) -> bool:
        record.msg, record.args = scrub(record.getMessage()), None
        if record.exc_info:
            # the traceback is formatted later; keep its text but scrub it now
            text = logging.Formatter().formatException(record.exc_info)
            record.exc_info, record.exc_text = None, scrub(text)
        return True


class QuietLibraries(logging.Filter):
    """Drops everything below WARNING from the libraries, even if somebody raised their logger's level."""

    def filter(self, record: logging.LogRecord) -> bool:
        return record.levelno >= logging.WARNING or not record.name.startswith(QUIET_LIBRARIES)


def extra_handlers() -> list[logging.Handler]:
    """The place to add more outputs. There is no log file yet; when one is wanted, return a
    ``logging.FileHandler`` (or a rotating one) here and every line is written to it as well."""
    return []


def setup(level: int = logging.INFO, handlers: Iterable[logging.Handler] | None = None) -> None:
    """Sets up the root logger with the console handler. Calling it again replaces the handlers."""
    root = logging.getLogger()
    for h in list(root.handlers):
        root.removeHandler(h)
    console = logging.StreamHandler(sys.stderr)
    out = [console, *(handlers if handlers is not None else extra_handlers())]
    for h in out:
        h.setFormatter(logging.Formatter(FORMAT))
        h.addFilter(QuietLibraries())
        h.addFilter(Scrubber())
        root.addHandler(h)
    root.setLevel(level)
    for name in QUIET_LIBRARIES:
        logging.getLogger(name).setLevel(logging.WARNING)
