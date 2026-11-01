"""Command line entry point and the polling loop."""

import argparse
import asyncio
import logging
import sys
import time
from pathlib import Path
from typing import Callable
from urllib.parse import urlsplit

from . import __version__, autostart, logsetup, update
from . import convert as cv
from . import pitmenu, reader, structs as S
from .config import Config, ConfigError, load
from .link import Link
from .logsetup import parse_level
from .raw import RawWriter
from .session import SessionNames
from .spool import Spool
from .tracker import PlayerSample, Tracker

log = logging.getLogger("splash_agent")

STATUS_EVERY = 30.0  # seconds between status lines
SPOOL_WARN = 500  # unacknowledged messages that count as a backlog worth a warning
PAUSED_AFTER = 3.0  # seconds without game time progress after which the game counts as paused
PIT_INFO_RETRY = 10.0  # seconds between attempts to open the optional pit menu buffer
FIRST_UPDATE_CHECK = 300.0  # seconds after the start of the polling before the first look for a newer release


class Agent:
    """Turns buffer snapshots into messages. Polling is synchronous so it can be tested without a game."""

    def __init__(self, cfg: Config, scoring: reader.MappedBuffer, telemetry: reader.MappedBuffer,
                 link_factory: Callable[[Spool], object] | None = None,
                 pit_info: reader.MappedBuffer | None = None,
                 open_pit_info: Callable[[], reader.MappedBuffer] | None = None,
                 open_hw_control: Callable[[], reader.HWControl] | None = None,
                 updater: update.Updater | None = None):
        self.cfg, self.scoring, self.telemetry = cfg, scoring, telemetry
        self.updater = updater
        self.restart_for_update = False  # run() ended because a downloaded update is to be installed
        self._update_task: asyncio.Task | None = None
        self.pit_info, self._open_pit_info = pit_info, open_pit_info
        self._open_hw_control = open_hw_control or reader.open_hw_control
        self._hw: reader.HWControl | None = None
        self._command_task: asyncio.Task | None = None
        self.spool = Spool(cfg.spool_dir, cfg.agent_id)
        self.names = SessionNames(cfg.spool_dir)
        self.tracker = Tracker()
        self.link_factory = link_factory or (lambda spool: Link(cfg.url, cfg.token, spool, cfg.cf_client_id, cfg.cf_client_secret))
        self.link = None
        self._link_task: asyncio.Task | None = None
        self._player: tuple[int, str, float] | None = None  # slot id, vehicle name, lap distance
        self._vehicles = 0
        self._record = open(cfg.record_file, "a", encoding="utf-8") if cfg.record_file else None
        self._raw = RawWriter(cfg.raw_file) if cfg.raw_file else None
        self.frames = {"scoring": 0, "telemetry": 0}
        self._failed_reads = 0  # snapshots that could not be read consistently since the last status line
        self._rate_mark = (time.monotonic(), 0, 0)
        self._pit_info_missing_logged = False
        self._last_et = None
        self._last_et_change = time.monotonic()

    # -- polling ---------------------------------------------------------

    def poll_scoring(self) -> None:
        buf = self.scoring.read()
        if buf is None:
            self._failed_reads += 1
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
            log.debug("raw scoring buffer: %d vehicles, %d bytes", n, len(bytes(buf)))
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
            seq, line = self.spool.append(d.type, d.et, d.payload)
            log.debug("spool: %s seq %d, %d pending", d.type, seq, len(self.spool.entries))
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
            self._failed_reads += 1
            return
        if self._raw is not None:
            log.debug("raw telemetry buffer: %d vehicles, %d bytes", buf.mNumVehicles, len(bytes(buf)))
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
        log.info("session changed: %s (was %s)", sid, self.spool.session_id or "none")
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
        if self._update_task is not None:
            self._update_task.cancel()
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

    def _status_checks(self, now: float) -> None:
        """What the status line implies: warnings for problems that fixed themselves or may grow, and the sample rates."""
        if self._failed_reads:
            log.warning("%d snapshots of the plugin buffers could not be read consistently in the last %.0f s",
                        self._failed_reads, STATUS_EVERY)
            self._failed_reads = 0
        if len(self.spool.entries) >= SPOOL_WARN:
            log.warning("%d messages are waiting for the backend to acknowledge them (spool grows while it is away)",
                        len(self.spool.entries))
        t0, s0, t1 = self._rate_mark
        dt = max(now - t0, 1e-6)
        log.debug("samples per second: scoring %.1f, telemetry %.1f", (self.frames["scoring"] - s0) / dt,
                  (self.frames["telemetry"] - t1) / dt)
        self._rate_mark = (now, self.frames["scoring"], self.frames["telemetry"])

    def _update_due(self, now: float) -> bool:
        """A downloaded update is installed once the game has been idle for a while: paused, in a menu or closed."""
        return (self.updater is not None and self.updater.ready is not None and self._last_et is not None
                and now - self._last_et_change >= update.IDLE_BEFORE_RESTART)

    async def _update_loop(self) -> None:
        await asyncio.sleep(FIRST_UPDATE_CHECK)
        while True:
            try:
                await asyncio.to_thread(self.updater.check_and_download)
            except Exception as e:
                log.warning("update check failed: %s", e)
            await asyncio.sleep(self.cfg.update_check_hours * 3600)

    async def run(self) -> None:
        scoring_every, telemetry_every = 1 / self.cfg.scoring_hz, 1 / self.cfg.telemetry_hz
        next_scoring = next_telemetry = next_status = next_pit_info = time.monotonic()
        if self.updater is not None:
            self._update_task = asyncio.get_running_loop().create_task(self._update_loop())
        while True:
            now = time.monotonic()
            if self._update_due(now):
                log.info("the game has been idle for %.0f s, restarting to install agent %s",
                         now - self._last_et_change, self.updater.ready.text)
                self.restart_for_update = True
                return
            if now >= next_status:
                log.info("status: %s", self.status())
                self._status_checks(now)
                next_status = now + STATUS_EVERY
            if self.pit_info is None and self._open_pit_info is not None and now >= next_pit_info:
                next_pit_info = now + PIT_INFO_RETRY
                try:
                    self.pit_info = self._open_pit_info()
                    log.info("pit menu buffer opened")
                except reader.PluginNotRunning:
                    # older plugin or buffer unsubscribed: the dashboard just shows no pit menu
                    if not self._pit_info_missing_logged:
                        self._pit_info_missing_logged = True
                        log.info("pit menu buffer not available, trying again every %.0f s", PIT_INFO_RETRY)
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
            buffers = (reader.open_windows_buffer(S.ScoringBuffer, S.SCORING_FILE),
                       reader.open_windows_buffer(S.TelemetryBuffer, S.TELEMETRY_FILE))
            log.info("plugin buffers found" + (" (after waiting)" if waiting else ""))
            return buffers
        except reader.PluginNotRunning as e:
            if sys.platform != "win32":
                raise SystemExit(f"error: {e}") from e
            if not waiting:
                log.info("waiting for rFactor 2 and the shared memory plugin (%s)", e)
                waiting = True
            time.sleep(2)


def _server_summary(url: str) -> str:
    """Host and port of the backend; the path, the query and any credentials in the URL stay out of the log."""
    parts = urlsplit(url)
    return f"{parts.scheme}://{parts.hostname or '?'}" + (f":{parts.port}" if parts.port else "")


def cmd_run(args) -> int:
    cfg = load(args.config)
    log.info("splash-agent %s starting: server=%s agent=%s cloudflare_client_id=%s log_level=%s spool=%s",
             __version__, _server_summary(cfg.url), cfg.agent_id, "set" if cfg.cf_client_id else "not set",
             cfg.log_level.upper(), cfg.spool_dir)
    autostart.offer(cfg.spool_dir, args.config)
    updater = _updater(cfg)
    if updater is not None and updater.update_at_start(sys.argv[1:]):
        return 0  # the new version runs in its own window
    scoring, telemetry = _open_buffers()
    agent = Agent(cfg, scoring, telemetry, updater=updater,
                  open_pit_info=lambda: reader.open_windows_buffer(S.PitInfoBuffer, S.PIT_INFO_FILE))

    async def main():
        try:
            await agent.run()
        finally:
            agent.close()

    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        log.info("shutting down (interrupted)")
    if agent.restart_for_update:
        try:
            updater.install_and_restart(sys.argv[1:])
        except Exception as e:
            log.error("the update could not be installed, this version stays: %s", e)
    log.info("stopped")
    return 0


def _updater(cfg: Config) -> update.Updater | None:
    """The updater of this build, or None when it does not update itself (source install, switched off)."""
    if not cfg.update_enabled or not update.supported():
        return None
    return update.Updater(cfg.update_repo, __version__)


def cmd_update(args) -> int:
    """Look for a newer agent release and, with ``install``, install it now."""
    cfg = load(args.config)
    updater = update.Updater(cfg.update_repo, __version__)
    rel = updater.check_and_download() if args.action == "install" else updater.check()
    if rel is None:
        print(f"agent {__version__} is up to date")
        return 0
    if args.action == "check":
        print(f"agent {rel.text} is available (this is {__version__}); install it with: splash-agent update install")
        return 0
    if not update.supported():
        print(f"agent {rel.text} is downloaded, but only the Windows program updates itself; replace {updater.exe} by hand")
        return 1
    updater.install_and_restart(["run", "--config", str(Path(args.config).resolve())])
    print(f"agent {rel.text} installed; this window can be closed")
    return 0


def cmd_autostart(args) -> int:
    """Turn the start with Windows on or off, or say whether it is on."""
    if sys.platform != "win32":
        print("starting with Windows is only available on Windows")
        return 1
    registry = autostart.WindowsRegistry()
    if args.action == "on":
        load(args.config)  # a configuration that cannot be read would make the entry useless
        autostart.enable(registry, args.config)
        print("the agent will start when you sign in to Windows")
    elif args.action == "off":
        autostart.disable(registry)
        print("the agent will no longer start with Windows")
    else:
        print("on" if autostart.is_enabled(registry) else "off")
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


def _levels(args) -> tuple[int, Config | None]:
    """The log level: the command line wins over log.level in the config file, the default is INFO."""
    cfg = None
    if getattr(args, "config", None):
        try:
            cfg = load(args.config)
        except ConfigError:
            cfg = None  # the command reports it itself, after the logging is up
    name = getattr(args, "log_level", None) or (cfg.log_level if cfg else "INFO")
    return parse_level(name), cfg


def find_config(explicit: str | None) -> str:
    """The configuration file: the one given, else agent.toml in the working folder, else next to the program (the
    single-file build, started by a double click or from another folder)."""
    if explicit:
        return explicit
    here = Path("agent.toml")
    if here.exists():
        return str(here)
    if getattr(sys, "frozen", False):
        beside = Path(sys.executable).parent / "agent.toml"
        if beside.exists():
            return str(beside)
    return str(here)


MISSING_CONFIG_HINT = (
    "There is no configuration yet. Sign in to Splash, open the administration page, press \"Download the configuration\" "
    "under \"My agent\" and save the file as agent.toml next to this program. Then start it again."
)


def _wait_for_enter() -> None:
    """Keeps the window open after an error when somebody started the program by a double click."""
    try:
        if sys.stdin is not None and sys.stdin.isatty():
            input("Press Enter to close this window. ")
    except (EOFError, KeyboardInterrupt, OSError):
        pass


def cli(argv: list[str] | None = None) -> int:
    common = argparse.ArgumentParser(add_help=False)
    common.add_argument("--log-level", default=argparse.SUPPRESS, metavar="LEVEL",
                        help="DEBUG, INFO, WARNING or ERROR (default: log.level of the config file, else INFO)")
    ap = argparse.ArgumentParser(prog="splash-agent", parents=[common])
    sub = ap.add_subparsers(dest="cmd", required=True)
    run = sub.add_parser("run", parents=[common], help="stream the shared memory data to the backend")
    run.add_argument("--config", default=None, help="default: agent.toml in the working folder or next to the program")
    run.set_defaults(fn=cmd_run)
    upd = sub.add_parser("update", parents=[common], help="look for a newer agent release, or install it")
    upd.add_argument("action", choices=["check", "install"])
    upd.add_argument("--config", default=None)
    upd.set_defaults(fn=cmd_update)
    auto = sub.add_parser("autostart", parents=[common], help="start the agent when you sign in to Windows")
    auto.add_argument("action", choices=["on", "off", "status"])
    auto.add_argument("--config", default=None)
    auto.set_defaults(fn=cmd_autostart)
    probe = sub.add_parser("probe", parents=[common], help="print the telemetry of all vehicles once")
    probe.set_defaults(fn=cmd_probe)
    sim = sub.add_parser("simulate", parents=[common], help="play a made-up race against a backend, no game needed")
    sim.add_argument("--url", default="ws://localhost:8080/v1/agent/ws")
    sim.add_argument("--token", default="dev")
    sim.add_argument("--minutes", type=float, default=120, help="length of the race in game minutes")
    sim.add_argument("--speed", type=float, default=20, help="1 is real time")
    sim.add_argument("--cars", type=int, default=16)
    sim.add_argument("--seed", type=int, default=1)
    sim.add_argument("--track-file", help="JSON with the outline (and corners) of a real track to play on instead of the invented one")
    sim.set_defaults(fn=cmd_simulate)
    rr = sub.add_parser("replay-raw", parents=[common], help="run a raw capture (agent.raw_file) through the agent offline")
    rr.add_argument("file")
    rr.add_argument("--config", default=None)
    rr.add_argument("--out", default="replay-out.ndjson")
    rr.set_defaults(fn=cmd_replay_raw)
    args = ap.parse_args(argv)
    if hasattr(args, "config"):
        args.config = find_config(args.config)
    try:
        level, cfg = _levels(args)
    except ValueError as e:
        print(f"config error: {e}", file=sys.stderr)
        return 2
    logsetup.setup(level)
    if cfg is not None:
        logsetup.register_secrets(cfg.token, cfg.cf_client_secret)
    try:
        return args.fn(args)
    except ConfigError as e:
        print(f"config error: {e}", file=sys.stderr)
        if hasattr(args, "config") and not Path(args.config).exists():
            print(MISSING_CONFIG_HINT, file=sys.stderr)
        _wait_for_enter()
        return 2
    except Exception:
        log.error("unrecoverable failure, the agent stops", exc_info=True)
        _wait_for_enter()
        return 1


if __name__ == "__main__":
    sys.exit(cli())
