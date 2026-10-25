"""Command line entry point and the polling loop."""

import argparse
import asyncio
import logging
import sys
import time
from typing import Callable

from . import convert as cv
from . import reader, structs as S
from .config import Config, ConfigError, load
from .link import Link
from .spool import Spool
from .tracker import PlayerSample, Tracker

log = logging.getLogger("splash_agent")


class Agent:
    """Turns buffer snapshots into messages. Polling is synchronous so it can be tested without a game."""

    def __init__(self, cfg: Config, scoring: reader.MappedBuffer, telemetry: reader.MappedBuffer,
                 link_factory: Callable[[Spool], object] | None = None):
        self.cfg, self.scoring, self.telemetry = cfg, scoring, telemetry
        self.spool = Spool(cfg.spool_dir, cfg.agent_id, cfg.role)
        self.tracker = Tracker()
        self.link_factory = link_factory or (lambda spool: Link(cfg.url, cfg.token, spool))
        self.link = None
        self._link_task: asyncio.Task | None = None
        self._player: tuple[int, str, float] | None = None  # slot id, vehicle name, lap distance
        self._vehicles = 0
        self._record = open(cfg.record_file, "a", encoding="utf-8") if cfg.record_file else None

    # -- polling ---------------------------------------------------------

    def poll_scoring(self) -> None:
        buf = self.scoring.read()
        if buf is None:
            return
        info = buf.mScoringInfo
        n = min(info.mNumVehicles, S.MAX_MAPPED_VEHICLES)
        if n <= 0 or info.mGamePhase == 9:  # 9 is the plugin's pause heartbeat
            return
        sid = cv.session_id(info)
        if sid != self.spool.session_id:
            self._new_session(sid)
        self._vehicles = n
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
        line = self.spool.live("scoring", info.mCurrentET, cv.scoring_payload(info, vehicles))
        self._write_record(line)
        self.link.push_live("scoring", line)

    def poll_telemetry(self) -> None:
        if self._player is None or self.spool.session_id is None:
            return
        car_id, name, lap_dist = self._player
        buf = self.telemetry.read(prefix=reader.telemetry_prefix(max(self._vehicles, 1)))
        if buf is None:
            return
        n = min(buf.mNumVehicles, S.MAX_MAPPED_VEHICLES)
        t = next((buf.mVehicles[i] for i in range(n) if buf.mVehicles[i].mID == car_id), None)
        payload = cv.telemetry_payload(t, car_id, name, lap_dist) if t is not None else None
        if payload is None:
            self.tracker.set_player(None)
            return
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
        run = getattr(self.link, "run", None)
        if run is not None:
            self._link_task = asyncio.get_running_loop().create_task(run())

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

    async def run(self) -> None:
        scoring_every, telemetry_every = 1 / self.cfg.scoring_hz, 1 / self.cfg.telemetry_hz
        next_scoring = next_telemetry = time.monotonic()
        while True:
            now = time.monotonic()
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
    agent = Agent(cfg, scoring, telemetry)

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


def cli(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(prog="splash-agent")
    sub = ap.add_subparsers(dest="cmd", required=True)
    run = sub.add_parser("run", help="stream the shared memory data to the backend")
    run.add_argument("--config", default="agent.toml")
    run.set_defaults(fn=cmd_run)
    probe = sub.add_parser("probe", help="print the telemetry of all vehicles once")
    probe.set_defaults(fn=cmd_probe)
    args = ap.parse_args(argv)
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s: %(message)s")
    try:
        return args.fn(args)
    except ConfigError as e:
        print(f"config error: {e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(cli())
