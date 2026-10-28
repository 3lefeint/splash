"""Command line entry point and the polling loop."""

import argparse
import asyncio
import logging
import sys
import time
from typing import Callable

from . import convert as cv
from . import pitmenu, reader, structs as S
from .config import Config, ConfigError, load
from .link import Link
from .raw import RawWriter
from .session import SessionNames
from .spool import Spool
from .tracker import PlayerSample, Tracker

log = logging.getLogger("splash_agent")

STATUS_EVERY = 30.0  # seconds between status lines
PAUSED_AFTER = 3.0  # seconds without game time progress after which the game counts as paused
PIT_INFO_RETRY = 10.0  # seconds between attempts to open the optional pit menu buffer


class Agent:
    """Turns buffer snapshots into messages. Polling is synchronous so it can be tested without a game."""

    def __init__(self, cfg: Config, scoring: reader.MappedBuffer, telemetry: reader.MappedBuffer,
                 link_factory: Callable[[Spool], object] | None = None,
                 pit_info: reader.MappedBuffer | None = None,
                 open_pit_info: Callable[[], reader.MappedBuffer] | None = None,
                 open_hw_control: Callable[[], reader.HWControl] | None = None):
        self.cfg, self.scoring, self.telemetry = cfg, scoring, telemetry
        self.pit_info, self._open_pit_info = pit_info, open_pit_info
        self._open_hw_control = open_hw_control or reader.open_hw_control
        self._hw: reader.HWControl | None = None
        self._command_task: asyncio.Task | None = None
        self.spool = Spool(cfg.spool_dir, cfg.agent_id, cfg.role)
        self.names = SessionNames(cfg.spool_dir)
        self.tracker = Tracker()
        self.link_factory = link_factory or (lambda spool: Link(cfg.url, cfg.token, spool))
        self.link = None
        self._link_task: asyncio.Task | None = None
        self._player: tuple[int, str, float] | None = None  # slot id, vehicle name, lap distance
        self._vehicles = 0
        self._record = open(cfg.record_file, "a", encoding="utf-8") if cfg.record_file else None
        self._raw = RawWriter(cfg.raw_file) if cfg.raw_file else None
        self.frames = {"scoring": 0, "telemetry": 0}
        self._last_et = None
        self._last_et_change = time.monotonic()

    # -- polling ---------------------------------------------------------

    def poll_scoring(self) -> None:
        buf = self.scoring.read()
        if buf is None:
            return
        info = buf.mScoringInfo
        n = min(info.mNumVehicles, S.MAX_MAPPED_VEHICLES)
        if info.mCurrentET != self._last_et:
            self._last_et, self._last_et_change = info.mCurrentET, time.monotonic()
        if n <= 0 or info.mGamePhase == 9:  # 9 is the plugin's pause heartbeat
            return
        sid = self.names.name(cv.session_id(info), info.mCurrentET)
        if sid != self.spool.session_id:
            self._new_session(sid)
        self._vehicles = n
        if self._raw is not None:
            self._raw.write("s", buf, reader.scoring_prefix(n))
        vehicles = [buf.mVehicles[i] for i in range(n)]

        self._player = None
        if info.mInRealtime:
            for v in vehicles:
                if v.mIsPlayer and v.mPlace >= 1:
                    self._player = (v.mID, cv.text(v.mVehicleName), v.mLapDist)
        if self._player is None:
            self.tracker.set_player(None)

        for d in self.tracker.on_scoring(info, vehicles):
            _, line = self.spool.append(d.type, d.et, d.payload)
            self._write_record(line)
        self.link.notify()
        self.frames["scoring"] += 1
        line = self.spool.live("scoring", info.mCurrentET, cv.scoring_payload(info, vehicles, self._car_states(n)))
        self._write_record(line)
        self.link.push_live("scoring", line)

    def _car_states(self, n: int) -> dict:
        """Fuel and tyre wear of every car in the telemetry buffer, by car id."""
        buf = self.telemetry.read(prefix=reader.telemetry_prefix(max(n, 1)))
        if buf is None:
            return {}
        out = {}
        for i in range(min(buf.mNumVehicles, S.MAX_MAPPED_VEHICLES)):
            t = buf.mVehicles[i]
            state = cv.car_state(t)
            if state:
                out[t.mID] = state
        return out

    def poll_telemetry(self) -> None:
        if self._player is None or self.spool.session_id is None:
            return
        car_id, name, lap_dist = self._player
        buf = self.telemetry.read(prefix=reader.telemetry_prefix(max(self._vehicles, 1)))
        if buf is None:
            return
        if self._raw is not None:
            self._raw.write("t", buf, reader.telemetry_prefix(max(self._vehicles, 1)))
        n = min(buf.mNumVehicles, S.MAX_MAPPED_VEHICLES)
        t = next((buf.mVehicles[i] for i in range(n) if buf.mVehicles[i].mID == car_id), None)
        payload = cv.telemetry_payload(t, car_id, name, lap_dist) if t is not None else None
        if payload is None:
            self.tracker.set_player(None)
            return
        menu = self.pit_info.read() if self.pit_info is not None else None
        menu = cv.pit_menu(menu.mPitMenu) if menu is not None else None
        if menu is not None:
            payload["pit_menu"] = menu
        self.frames["telemetry"] += 1
        self.tracker.set_player(PlayerSample(car_id, payload["fuel"], payload["wear"]))
        line = self.spool.live("telemetry", t.mElapsedTime, payload)
        self._write_record(line)
        self.link.push_live("telemetry", line)

    # -- session and output ----------------------------------------------

    def _new_session(self, sid: str) -> None:
        log.info("session %s", sid)
        self._stop_link()
        self.tracker.reset()
        self.spool.open(sid)
        self.link = self.link_factory(self.spool)
        self.link.on_command = self._on_command
        run = getattr(self.link, "run", None)
        if run is not None:
            self._link_task = asyncio.get_running_loop().create_task(run())

    # -- commands --------------------------------------------------------

    def _on_command(self, msg: dict) -> None:
        if self._command_task is not None and not self._command_task.done():
            self._answer(msg.get("id", ""), pitmenu.Result(False, "another command is still running"))
            return
        self._command_task = asyncio.get_running_loop().create_task(self._run_command(msg))

    async def _run_command(self, msg: dict) -> None:
        cid, steps = str(msg.get("id", "")), msg.get("steps") or []
        log.info("command %s: %s", cid, ", ".join(f"{s.get('category')} {s.get('choice')}" for s in steps))
        if msg.get("kind") == "pit_request":
            result = await asyncio.to_thread(self._toggle_pit_request)
        else:
            result = await asyncio.to_thread(self._apply_steps, steps)
        log.info("command %s: %s", cid, result.message)
        self._answer(cid, result)

    def _apply_steps(self, steps: list[dict]) -> pitmenu.Result:
        if self.pit_info is None:
            return pitmenu.Result(False, "the pit menu buffer of the plugin is not available")
        if time.monotonic() - self._last_et_change > PAUSED_AFTER:
            return pitmenu.Result(False, "the game is paused or in a menu; the controls only work while it runs")
        if self._hw is None:
            try:
                self._hw = self._open_hw_control()
            except reader.PluginNotRunning as e:
                return pitmenu.Result(False, f"the plugin's HWControl buffer is not available ({e})")

        def read() -> pitmenu.Row | None:
            buf = self.pit_info.read()
            m = cv.pit_menu(buf.mPitMenu) if buf is not None else None
            return None if m is None else pitmenu.Row(buf.mPitMenu.mCategoryIndex, m["category"], m["choice_index"],
                                                      m["choice"], m["choices"])

        def press(name: str, hold: float) -> None:
            self._hw.send(name, True)
            time.sleep(hold)
            self._hw.send(name, False)
            time.sleep(pitmenu.GAP)

        return pitmenu.PitMenuController(read, press).apply(steps)

    def _player_pit_state(self) -> int | None:
        """The pit state of the player's car straight from the scoring buffer, or None."""
        buf = self.scoring.read()
        if buf is None:
            return None
        n = min(buf.mScoringInfo.mNumVehicles, S.MAX_MAPPED_VEHICLES)
        for i in range(n):
            v = buf.mVehicles[i]
            if v.mIsPlayer and v.mPlace >= 1:
                return int(v.mPitState)
        return None

    def _toggle_pit_request(self) -> pitmenu.Result:
        """Presses the pit request control once and waits until the pit state of the car reflects it."""
        if time.monotonic() - self._last_et_change > PAUSED_AFTER:
            return pitmenu.Result(False, "the game is paused or in a menu; the controls only work while it runs")
        before = self._player_pit_state()
        if before is None:
            return pitmenu.Result(False, "the game shows no car of the player")
        if before > 1:
            return pitmenu.Result(False, "the car is already in the pit lane")
        if self._hw is None:
            try:
                self._hw = self._open_hw_control()
            except reader.PluginNotRunning as e:
                return pitmenu.Result(False, f"the plugin's HWControl buffer is not available ({e})")
        for hold in (pitmenu.WAKE_HOLD, pitmenu.WAKE_HOLD):
            self._hw.send("PitRequest", True)
            time.sleep(hold)
            self._hw.send("PitRequest", False)
            deadline = time.monotonic() + pitmenu.REACT_TIMEOUT
            while time.monotonic() < deadline:
                now = self._player_pit_state()
                if now is not None and now != before:
                    return pitmenu.Result(True, "pit request on" if now == 1 else "pit request off")
                time.sleep(0.05)
        return pitmenu.Result(False, "the pit state did not change")

    def _answer(self, cid: str, result: pitmenu.Result) -> None:
        if self.spool.session_id is None or self.link is None:
            return
        payload = {"id": cid, "ok": result.ok, "message": result.message, "applied": result.applied}
        self.link.push_live("command_result", self.spool.live("command_result", self._last_et or 0, payload))

    def _stop_link(self) -> None:
        if self._link_task is not None:
            self._link_task.cancel()
            self._link_task = None

    def _write_record(self, line: str) -> None:
        if self._record is not None:
            self._record.write(line + "\n")

    def close(self) -> None:
        self._stop_link()
        self.spool.close()
        if self._record is not None:
            self._record.close()
        if self._raw is not None:
            self._raw.close()

    def status(self) -> str:
        """One line saying what the agent sees and sends, for the log."""
        connected = getattr(self.link, "connected", None)
        idle = time.monotonic() - self._last_et_change
        state = "waiting for a session" if self.spool.session_id is None else f"session {self.spool.session_id}"
        text = (f"{state}, {self._vehicles} cars, scoring {self.frames['scoring']}, telemetry {self.frames['telemetry']}, "
                f"backend {'connected' if connected else 'not connected'}, pending {len(self.spool.entries)}")
        if idle > 15 and self._last_et is not None:
            text += f", game time not moving for {idle:.0f} s (paused, in a menu or the game closed)"
        return text

    async def run(self) -> None:
        scoring_every, telemetry_every = 1 / self.cfg.scoring_hz, 1 / self.cfg.telemetry_hz
        next_scoring = next_telemetry = next_status = next_pit_info = time.monotonic()
        while True:
            now = time.monotonic()
            if now >= next_status:
                log.info("status: %s", self.status())
                next_status = now + STATUS_EVERY
            if self.pit_info is None and self._open_pit_info is not None and now >= next_pit_info:
                next_pit_info = now + PIT_INFO_RETRY
                try:
                    self.pit_info = self._open_pit_info()
                    log.info("pit menu buffer opened")
                except reader.PluginNotRunning:
                    pass  # older plugin or buffer unsubscribed: the dashboard just shows no pit menu
            if now >= next_scoring:
                self.poll_scoring()
                next_scoring = max(next_scoring + scoring_every, now)
            if now >= next_telemetry:
                self.poll_telemetry()
                next_telemetry = max(next_telemetry + telemetry_every, now)
            await asyncio.sleep(max(min(next_scoring, next_telemetry) - time.monotonic(), 0.005))


# -- commands ------------------------------------------------------------


def _open_buffers() -> tuple[reader.MappedBuffer, reader.MappedBuffer]:
    waiting = False
    while True:
        try:
            return (reader.open_windows_buffer(S.ScoringBuffer, S.SCORING_FILE),
                    reader.open_windows_buffer(S.TelemetryBuffer, S.TELEMETRY_FILE))
        except reader.PluginNotRunning as e:
            if sys.platform != "win32":
                raise SystemExit(f"error: {e}") from e
            if not waiting:
                log.info("waiting for rFactor 2 and the shared memory plugin (%s)", e)
                waiting = True
            time.sleep(2)


def cmd_run(args) -> int:
    cfg = load(args.config)
    scoring, telemetry = _open_buffers()
    agent = Agent(cfg, scoring, telemetry,
                  open_pit_info=lambda: reader.open_windows_buffer(S.PitInfoBuffer, S.PIT_INFO_FILE))

    async def main():
        try:
            await agent.run()
        finally:
            agent.close()

    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
    return 0


def cmd_probe(args) -> int:
    """Print what the telemetry buffer holds for every vehicle, e.g. to see whether remote cars have fuel data."""
    scoring, telemetry = _open_buffers()
    s = scoring.read()
    t = telemetry.read()
    if s is None or t is None:
        print("no consistent snapshot, try again")
        return 1
    names = {}
    for i in range(min(s.mScoringInfo.mNumVehicles, S.MAX_MAPPED_VEHICLES)):
        v = s.mVehicles[i]
        names[v.mID] = (cv.text(v.mDriverName), v.mIsPlayer, v.mControl)
    print(f"track {cv.text(s.mScoringInfo.mTrackName)}, in realtime: {bool(s.mScoringInfo.mInRealtime)}, "
          f"scoring vehicles {s.mScoringInfo.mNumVehicles}, telemetry vehicles {t.mNumVehicles}")
    print(f"{'id':>4} {'driver':<20} {'player':<6} {'ctrl':>4} {'fuel':>8} {'capacity':>8} {'wear fl/fr/rl/rr'}")
    for i in range(min(t.mNumVehicles, S.MAX_MAPPED_VEHICLES)):
        v = t.mVehicles[i]
        driver, is_player, control = names.get(v.mID, ("?", False, "?"))
        wear = "/".join(f"{v.mWheels[k].mWear:.2f}" for k in range(4))
        print(f"{v.mID:>4} {driver[:20]:<20} {str(is_player):<6} {control!s:>4} {v.mFuel:>8.2f} "
              f"{v.mFuelCapacity:>8.1f} {wear}")
    return 0


def cmd_simulate(args) -> int:
    """Play a made-up race against a backend, to look at the dashboard without the game."""
    from . import simulate

    if args.track_file:
        simulate.load_track(args.track_file)
    try:
        asyncio.run(simulate.run(args.url, args.token, args.minutes, args.speed, args.cars, args.seed))
    except KeyboardInterrupt:
        pass
    return 0


def cmd_replay_raw(args) -> int:
    """Run a raw capture through the agent logic without a game or a backend."""
    from .raw import replay

    cfg = load(args.config)
    counts = replay(args.file, cfg, args.out)
    print(f"replayed {counts.get('s', 0)} scoring and {counts.get('t', 0)} telemetry frames, output in {args.out}")
    return 0


def cli(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="splash-agent")
    sub = ap.add_subparsers(dest="cmd", required=True)
    run = sub.add_parser("run", help="stream the shared memory data to the backend")
    run.add_argument("--config", default="agent.toml")
    run.set_defaults(fn=cmd_run)
    probe = sub.add_parser("probe", help="print the telemetry of all vehicles once")
    probe.set_defaults(fn=cmd_probe)
    sim = sub.add_parser("simulate", help="play a made-up race against a backend, no game needed")
    sim.add_argument("--url", default="ws://localhost:8080/v1/agent/ws")
    sim.add_argument("--token", default="dev")
    sim.add_argument("--minutes", type=float, default=120, help="length of the race in game minutes")
    sim.add_argument("--speed", type=float, default=20, help="1 is real time")
    sim.add_argument("--cars", type=int, default=16)
    sim.add_argument("--seed", type=int, default=1)
    sim.add_argument("--track-file", help="JSON with the outline (and corners) of a real track to play on instead of the invented one")
    sim.set_defaults(fn=cmd_simulate)
    rr = sub.add_parser("replay-raw", help="run a raw capture (agent.raw_file) through the agent offline")
    rr.add_argument("file")
    rr.add_argument("--config", default="agent.toml")
    rr.add_argument("--out", default="replay-out.ndjson")
    rr.set_defaults(fn=cmd_replay_raw)
    args = ap.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    try:
        return args.fn(args)
    except ConfigError as e:
        print(f"config error: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(cli())
