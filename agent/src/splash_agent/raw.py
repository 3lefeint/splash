"""Raw capture of the plugin buffers and offline replay through the agent."""

import base64
import gzip
import json
import tempfile
from pathlib import Path
from typing import IO, Iterator

from . import reader, structs as S


class RawWriter:
    """Appends the used part of every polled buffer to an NDJSON file (gzip if the name ends in .gz)."""

    def __init__(self, path: str):
        if path.endswith(".gz"):
            self._fh: IO[str] = gzip.open(path, "at", encoding="utf-8", compresslevel=3)
        else:
            self._fh = open(path, "a", encoding="utf-8")

    def write(self, kind: str, buf, prefix: int) -> None:
        data = bytes(buf)[:prefix]
        self._fh.write(json.dumps({"k": kind, "b": base64.b64encode(data).decode("ascii")}) + "\n")
        self._fh.flush()

    def close(self) -> None:
        self._fh.close()


def read_frames(path: str) -> Iterator[tuple[str, bytes]]:
    """Yield the captured frames. A capture cut off by a killed agent ends at its last complete frame."""
    opener = gzip.open if path.endswith(".gz") else open
    with opener(path, "rt", encoding="utf-8") as f:
        try:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    d = json.loads(line)
                    yield d["k"], base64.b64decode(d["b"])
                except (ValueError, KeyError):
                    return  # torn last line
        except EOFError:
            return  # gzip stream without its end marker


class _Link:
    """Stands in for the WebSocket link: only the notifications the agent makes."""

    def notify(self) -> None:
        pass

    def push_live(self, type_: str, line: str) -> None:
        pass


def replay(path: str, cfg, out_file: str) -> dict:
    """Feed a raw capture through the agent and return counts of what it produced."""
    from .config import Config
    from .main import Agent

    scoring_src, telemetry_src = bytearray(C_SCORING), bytearray(C_TELEMETRY)
    scoring = reader.MappedBuffer(S.ScoringBuffer, scoring_src)
    telemetry = reader.MappedBuffer(S.TelemetryBuffer, telemetry_src)
    with tempfile.TemporaryDirectory() as spool_dir:
        cfg = Config(**{**cfg.__dict__, "spool_dir": spool_dir, "record_file": out_file, "raw_file": ""})
        agent = Agent(cfg, scoring, telemetry, link_factory=lambda spool: _Link())
        counts: dict[str, int] = {}
        try:
            for kind, data in read_frames(path):
                if kind == "s":
                    scoring_src[:len(data)] = data
                    agent.poll_scoring()
                else:
                    telemetry_src[:len(data)] = data
                    agent.poll_telemetry()
                counts[kind] = counts.get(kind, 0) + 1
        finally:
            agent.close()
    return counts


import ctypes as _C

C_SCORING = _C.sizeof(S.ScoringBuffer)
C_TELEMETRY = _C.sizeof(S.TelemetryBuffer)
