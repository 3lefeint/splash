"""Sets rows of the game's pit menu by sending the same controls a player presses.

The plugin reports only the selected row, so every change is a loop: press, wait until the
reported row changes, compare with the wanted value.
"""

import re
import time
from dataclasses import dataclass, field
from typing import Callable

HOLD = 0.04  # seconds a control stays down while the plugin scans quickly
GAP = 0.03  # seconds between two controls
WAKE_HOLD = 0.3  # the plugin scans at 5 per second until it gets a control, so the first one is held longer
BOOST_WINDOW = 0.08  # seconds after a control during which the plugin still scans quickly
REACT_TIMEOUT = 1.0  # seconds to wait for the reported row to change after a control
MAX_STEPS = 400


@dataclass
class Row:
    category_index: int
    category: str
    choice_index: int
    choice: str
    choices: int

    def key(self) -> tuple:
        return (self.category_index, self.choice_index, self.choices)


@dataclass
class Result:
    ok: bool
    message: str
    applied: list[dict] = field(default_factory=list)


class Failed(Exception):
    pass


class PitMenuController:
    """``read`` returns the selected row (or None), ``press(control, hold)`` sends one control as down and up."""

    def __init__(self, read: Callable[[], Row | None], press: Callable[[str, float], None],
                 sleep: Callable[[float], None] = time.sleep, clock: Callable[[], float] = time.monotonic):
        self._read, self._press, self._sleep, self._clock = read, press, sleep, clock
        self._last_press = -1e9

    def apply(self, steps: list[dict]) -> Result:
        applied: list[dict] = []
        try:
            for step in steps:
                self._category(step["category"])
                self._choice(step["choice"])
                applied.append({"category": step["category"], "choice": self._row().choice})
        except Failed as e:
            return Result(False, str(e), applied)
        return Result(True, "ok", applied)

    # -- helpers ---------------------------------------------------------

    def _row(self) -> Row:
        row = self._read()
        if row is None:
            raise Failed("the game shows no pit menu")
        return row

    def _move(self, control: str) -> Row:
        """Send a control and return the row after it changed; the row is unchanged when the menu did not react.

        A control the plugin did not notice is sent once more, held long enough for its slow scan.
        """
        before = self._row()
        for attempt in range(2):
            quick = attempt == 0 and self._clock() - self._last_press < BOOST_WINDOW
            self._press(control, HOLD if quick else WAKE_HOLD)
            self._last_press = self._clock()
            deadline = self._last_press + REACT_TIMEOUT
            while self._clock() < deadline:
                row = self._row()
                if row.key() != before.key() or row.choice != before.choice:
                    return row
                self._sleep(0.01)
        return before

    def _category(self, wanted: str) -> None:
        want = wanted.strip().upper().rstrip(":")
        seen: set[int] = set()
        row = self._row()
        while row.category.strip().upper().rstrip(":") != want:
            if row.category_index in seen:
                raise Failed(f"pit menu row {wanted} not found")
            seen.add(row.category_index)
            nxt = self._move("PitMenuDown")
            if nxt.key() == row.key():
                raise Failed("the pit menu does not react to the controls (HWControl disabled or menu closed?)")
            row = nxt

    def _choice(self, wanted: str) -> None:
        w = str(wanted).strip()
        if w.lower() == "max":
            self._to_end("PitMenuIncrementValue", lambda r: r.choice_index >= r.choices - 1)
        elif w.lower() == "min":
            self._to_end("PitMenuDecrementValue", lambda r: r.choice_index <= 0)
        elif re.fullmatch(r"\d+", w):
            self._number(int(w))
        else:
            self._text(w)

    def _to_end(self, control: str, done: Callable[[Row], bool]) -> Row:
        row = self._row()
        for _ in range(MAX_STEPS):
            if done(row):
                return row
            nxt = self._move(control)
            if nxt.key() == row.key():
                return row  # the end of the list
            row = nxt
        raise Failed("too many steps")

    def _number(self, wanted: int) -> None:
        row = self._row()
        for _ in range(MAX_STEPS):
            m = re.search(r"\d+", row.choice)
            if m is None:
                raise Failed(f"the row {row.category} has no number: {row.choice!r}")
            current = int(m.group())
            if current == wanted:
                return
            nxt = self._move("PitMenuIncrementValue" if current < wanted else "PitMenuDecrementValue")
            if nxt.key() == row.key() and nxt.choice == row.choice:
                raise Failed(f"cannot reach {wanted} in {row.category}, stopped at {row.choice}")
            row = nxt
        raise Failed("too many steps")

    def _text(self, wanted: str) -> None:
        low = wanted.lower()
        row = self._to_end("PitMenuDecrementValue", lambda r: r.choice_index <= 0)
        for _ in range(MAX_STEPS):
            if low in row.choice.lower():
                return
            nxt = self._move("PitMenuIncrementValue")
            if nxt.key() == row.key():
                raise Failed(f"{wanted!r} is not a choice of {row.category}")
            row = nxt
        raise Failed("too many steps")
