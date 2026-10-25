"""Tells restarts of the same game session apart.

The id built from the track, session type and start time is identical for every
offline race on one track. When the elapsed time jumps back the race was
restarted, so the id gets a counter. The last state is kept on disk so an agent
restart in the middle of a race keeps its id.
"""

import json
from pathlib import Path

BACKWARDS = 60.0  # seconds the elapsed time may fall before it counts as a restart
SAVE_EVERY = 10.0  # seconds of game time between saves of the elapsed time


class SessionNames:
    def __init__(self, directory: str | Path):
        self.path = Path(directory) / "session-state.json"
        self.base, self.restarts, self.top, self._saved = None, 0, 0.0, 0.0
        try:
            state = json.loads(self.path.read_text(encoding="utf-8"))
            self.base, self.restarts, self.top = state["base"], int(state["restarts"]), float(state["et"])
            self._saved = self.top
        except (OSError, ValueError, KeyError, TypeError):
            pass

    def name(self, base: str, et: float) -> str:
        if base != self.base:
            self.base, self.restarts, self.top = base, 0, et
            self._save(et)
        elif et < self.top - BACKWARDS:
            self.restarts, self.top = self.restarts + 1, et
            self._save(et)
        elif et >= self.top + SAVE_EVERY:
            self.top = et
            self._save(et)
        else:
            self.top = max(self.top, et)
        return base if self.restarts == 0 else f"{base}-{self.restarts}"

    def _save(self, et: float) -> None:
        self._saved = et
        try:
            self.path.parent.mkdir(parents=True, exist_ok=True)
            self.path.write_text(json.dumps({"base": self.base, "restarts": self.restarts, "et": et}),
                                 encoding="utf-8")
        except OSError:
            pass
