"""Durable messages kept on disk until the backend acknowledged them."""

import json
import re
from pathlib import Path


def envelope(agent_id: str, role: str, session_id: str, type_: str, seq: int, et: float, payload: dict) -> dict:
    return {"v": 1, "type": type_, "agent_id": agent_id, "role": role, "session_id": session_id,
            "seq": seq, "et": et, "payload": payload}


class Spool:
    """Append-only NDJSON file per agent and session.

    The file keeps every durable message; ``entries`` holds those the backend has
    not acknowledged yet. After a restart all messages of the session are loaded
    again and the backend's ``ack`` decides what is resent.
    """

    def __init__(self, directory: str | Path, agent_id: str, role: str):
        self.directory = Path(directory)
        self.agent_id, self.role = agent_id, role
        self.session_id: str | None = None
        self.last_seq = 0
        self.entries: list[tuple[int, str]] = []
        self._fh = None

    def open(self, session_id: str) -> None:
        self.close()
        self.session_id = session_id
        self.directory.mkdir(parents=True, exist_ok=True)
        name = re.sub(r"[^A-Za-z0-9_.-]", "_", f"{self.agent_id}-{session_id}") + ".ndjson"
        path = self.directory / name
        self.entries, self.last_seq = [], 0
        if path.exists():
            with path.open("r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        seq = json.loads(line)["seq"]
                    except (ValueError, KeyError, TypeError):
                        continue  # a torn last line after a crash
                    self.entries.append((seq, line))
                    self.last_seq = max(self.last_seq, seq)
        self._fh = path.open("a", encoding="utf-8")

    def append(self, type_: str, et: float, payload: dict) -> tuple[int, str]:
        """Number a durable message, write it to disk and queue it for sending."""
        assert self._fh is not None and self.session_id is not None
        self.last_seq += 1
        line = json.dumps(envelope(self.agent_id, self.role, self.session_id, type_, self.last_seq, et, payload),
                          separators=(",", ":"))
        self._fh.write(line + "\n")
        self._fh.flush()
        self.entries.append((self.last_seq, line))
        return self.last_seq, line

    def live(self, type_: str, et: float, payload: dict) -> str:
        """A live sample: not sequenced, carries the last durable seq."""
        assert self.session_id is not None
        return json.dumps(envelope(self.agent_id, self.role, self.session_id, type_, self.last_seq, et, payload),
                          separators=(",", ":"))

    def hello(self, agent_version: str) -> str:
        assert self.session_id is not None
        payload = {"agent_version": agent_version}
        if self.entries:
            payload["resume_from_seq"] = self.entries[0][0]
        return json.dumps(envelope(self.agent_id, self.role, self.session_id, "hello", 0, 0, payload),
                          separators=(",", ":"))

    def prune(self, acked: int) -> None:
        self.entries = [e for e in self.entries if e[0] > acked]

    def close(self) -> None:
        if self._fh is not None:
            self._fh.close()
            self._fh = None
