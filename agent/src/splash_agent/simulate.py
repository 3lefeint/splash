"""A made-up endurance race for looking at the dashboard without the game.

The simulator plays a field of cars on an invented track and sends the same messages as the agent: scoring for all
cars, telemetry for the driven car, laps and pit events. It also answers the commands of the backend (pit menu,
pit request). Time can run faster than real time. Nothing here depends on the game.
"""

import asyncio
import json
import logging
import math
import random
from dataclasses import dataclass, field
from typing import Iterator

from . import __version__
from .spool import envelope

log = logging.getLogger("splash_agent.simulate")

TRACK = "Demo Ring"
LENGTH = 3200.0
CLASS = "GT3"
EDGE = 7.0  # distance from the centre line to the track edge in metres
PIT_ENTRY, PIT_BOX, PIT_EXIT = 0.950, 0.995, 0.040  # lap share of the pit lane
PIT_SPEED = 16.0  # m/s in the pit lane
PIT_OFFSET = 14.0  # metres the pit lane lies beside the track
RESERVE = 3.0  # litres a car keeps when it stops
DT = 0.5  # seconds of game time per step, the scoring rate of the agent
DRIVERS = ["Demo Driver A", "Demo Driver B", "Demo Driver C"]
CORNERS = [250, 700, 1250, 1650, 2250, 2800]  # distance of the slow sections


def _table(n: int = 2000) -> list[tuple[float, float, float]]:
    """x, z and the distance from the start of an invented closed track, evenly spaced in the angle."""
    pts = []
    for i in range(n + 1):
        a = 2 * math.pi * i / n
        r = 520 * (1 + 0.22 * math.cos(3 * a))
        pts.append((r * math.cos(a), r * 0.62 * (1 + 0.18 * math.sin(2 * a)) * math.sin(a)))
    out, total = [], 0.0
    for i, (x, z) in enumerate(pts):
        if i:
            total += math.hypot(x - pts[i - 1][0], z - pts[i - 1][1])
        out.append((x, z, total))
    scale = LENGTH / total
    return [(x * scale, z * scale, s * scale) for x, z, s in out]


_TABLE = _table()


def position(dist: float, offset: float = 0.0) -> tuple[float, float]:
    """World position at a distance along the lap, shifted sideways by offset metres."""
    dist %= LENGTH
    lo, hi = 0, len(_TABLE) - 1
    while hi - lo > 1:
        mid = (lo + hi) // 2
        if _TABLE[mid][2] <= dist:
            lo = mid
        else:
            hi = mid
    x0, z0, s0 = _TABLE[lo]
    x1, z1, s1 = _TABLE[hi]
    f = 0.0 if s1 == s0 else (dist - s0) / (s1 - s0)
    x, z = x0 + (x1 - x0) * f, z0 + (z1 - z0) * f
    if offset:
        length = math.hypot(x1 - x0, z1 - z0) or 1.0
        x, z = x + (z1 - z0) / length * offset, z - (x1 - x0) / length * offset
    return x, z


def shape(dist: float) -> float:
    """Relative speed along the lap: 1 on the straights, down to about 0.55 in the corners."""
    dip = sum(math.exp(-(((dist - c + LENGTH / 2) % LENGTH - LENGTH / 2) / 90.0) ** 2) for c in CORNERS)
    return 1 - 0.45 * min(dip, 1.0)


def _inv_mean() -> float:
    return sum(1 / shape(i * LENGTH / 400) for i in range(400)) / 400  # mean of 1/shape: time per metre at speed 1


_INV_MEAN = _inv_mean()
TIME_SCALE = 1.0  # lap times are the invented ones times this
CAPACITY = 60.0
PIT_LANE = (160.0, 16.0, 128.0)  # metres before the line where the lane starts, before the line to the box, after the line to the exit


def load_track(path: str) -> None:
    """Plays on a real track instead of the invented one.

    The file holds the outline as the backend stores it ({"track", "length", "points": [[x, z], ...]}) and optionally
    the corners ({"corners": [{"from", "to"}, ...]}), which become the slow sections. Lap times, fuel use and the pit lane
    are scaled to the length of the track.
    """
    global TRACK, LENGTH, _TABLE, CORNERS, _INV_MEAN, TIME_SCALE, CAPACITY, PIT_ENTRY, PIT_BOX, PIT_EXIT
    with open(path, encoding="utf-8") as f:
        data = json.load(f)
    pts = [(float(x), float(z)) for x, z in data["points"]]
    pts.append(pts[0])
    cum, total = [0.0], 0.0
    for a, b in zip(pts, pts[1:]):
        total += math.hypot(b[0] - a[0], b[1] - a[1])
        cum.append(total)
    TRACK, LENGTH = data["track"], float(data["length"])
    scale = LENGTH / total
    _TABLE = [(x, z, s * scale) for (x, z), s in zip(pts, cum)]
    corners = [c for c in data.get("corners", []) if c.get("from") is not None and c.get("to") is not None]
    CORNERS = [((c["from"] + c["to"]) / 2 if c["from"] <= c["to"] else ((c["from"] + c["to"] + LENGTH) / 2) % LENGTH) for c in corners] or [
        LENGTH * k / 12 for k in range(12)
    ]
    _INV_MEAN = _inv_mean()
    TIME_SCALE = LENGTH / 3200.0 * 0.7  # about 0.7 times the pace per metre of the invented track: 8 to 9 minutes on the Nordschleife
    CAPACITY = 120.0
    PIT_ENTRY, PIT_BOX, PIT_EXIT = 1 - PIT_LANE[0] / LENGTH, 1 - PIT_LANE[1] / LENGTH, PIT_LANE[2] / LENGTH


@dataclass
class Car:
    car_id: int
    name: str
    driver_index: int
    base: float  # lap time on an empty tank and new tyres
    fuel_use: float  # litres per lap
    capacity: float
    skill: tuple[float, float, float]  # amplitude, waves per lap, phase of the speed along the lap
    ours: bool = False
    fuel: float = 0.0
    wear: list[float] = field(default_factory=lambda: [1.0, 1.0, 1.0, 1.0])
    age: int = 0  # laps on this set of tyres
    dist: float = 0.0
    laps: int = 0
    state: str = "race"  # race, pit_in, stopped, pit_out
    stop_left: float = 0.0
    refuel_to: float = 0.0
    pits: int = 0
    penalties: int = 0
    dent: int = 0
    wet_tyres: bool = False
    # the lap in progress
    lap_start: float = 0.0
    lap_target: float = 0.0
    lap_fuel_start: float = 0.0
    splits: list[float] = field(default_factory=list)
    in_lap: bool = False
    out_lap: bool = False
    fcy_lap: bool = False
    last_lap: float | None = None
    best_lap: float | None = None
    last_s1: float | None = None
    last_s2: float | None = None
    speed: float = 0.0
    requested: bool = False
    fuel_in: float = 0.0  # fuel when the car stopped
    fresh_tyres: bool = True
    off_track_until: float = 0.0

    @property
    def total(self) -> float:
        return self.laps * LENGTH + self.dist

    @property
    def vehicle_name(self) -> str:
        return self.name

    @property
    def driver(self) -> str:
        return DRIVERS[self.driver_index % len(DRIVERS)] if self.ours else f"Rival {self.car_id}"


class Race:
    """The state of the made-up race. ``step`` advances it by DT and returns the messages to send."""

    def __init__(self, minutes: float = 120, cars: int = 16, seed: int = 1, agent_id: str = "demo", session: str = "demo-race"):
        self.rnd = random.Random(seed)
        self.agent_id, self.session = agent_id, session
        self.end_et = minutes * 60
        self.et = 0.0
        self.seq = 0
        self.cars: list[Car] = []
        for i in range(max(cars, 2)):
            ours = i == 0
            self.cars.append(Car(
                car_id=i, name="#1 DEMO TEAM" if ours else f"#{i + 1} Rival {i}", driver_index=0,
                base=(92 + self.rnd.uniform(-2.5, 3.5)) * TIME_SCALE, fuel_use=(2.6 + self.rnd.uniform(-0.35, 0.35)) * LENGTH / 3200.0, capacity=CAPACITY,
                skill=(self.rnd.uniform(0.01, 0.04), self.rnd.randint(2, 7), self.rnd.uniform(0, 6.28)), ours=ours))
        self.cars[0].base = 92.0 * TIME_SCALE
        self.cars[0].fuel_use = 2.6 * LENGTH / 3200.0
        for k, c in enumerate(self.cars):
            c.fuel = c.capacity - self.rnd.uniform(0, 8)
            c.dist = LENGTH - 14.0 * (k + 1)  # a grid, one car every 14 m behind the line
            c.lap_fuel_start = c.fuel
        self.rain = 0.0
        self.wetness = 0.0
        self.fcy = False
        self.menu = {"category": "FUEL:", "choice": "+20/8", "choice_index": 20, "choices": 123}
        self.wet_from = self.end_et * 0.45
        self.pending: list[dict] = []  # results of commands waiting to be sent
        for c in self.cars:
            c.laps = -1  # the grid is behind the line: the first crossing starts lap 1
            c.lap_target = self._lap_target(c)

    # -- helpers ---------------------------------------------------------

    def _env(self, type_: str, payload: dict, durable: bool = False) -> dict:
        if durable:
            self.seq += 1
        return envelope(self.agent_id, "driver", self.session, type_, self.seq, round(self.et, 3), payload)

    def hello(self) -> dict:
        return envelope(self.agent_id, "driver", self.session, "hello", 0, 0.0, {"agent_version": f"{__version__}-sim"})

    @property
    def ours(self) -> Car:
        return self.cars[0]

    def _lap_target(self, c: Car) -> float:
        t = c.base + (0.03 * c.fuel + 0.05 * c.age + self.rnd.gauss(0, 0.25)) * TIME_SCALE
        if self.wetness > 0.35 and not c.wet_tyres:
            t += 8 * self.wetness * TIME_SCALE
        return max(t, 60.0)

    def _speed(self, c: Car) -> float:
        amp, waves, phase = c.skill
        v = _INV_MEAN * LENGTH / c.lap_target * shape(c.dist)
        v *= 1 + amp * math.sin(2 * math.pi * waves * c.dist / LENGTH + phase)
        return v * (0.5 if self.fcy else 1.0)

    # -- one step --------------------------------------------------------

    def step(self) -> list[dict]:
        self.et += DT
        out: list[dict] = []
        self._weather()
        out.extend(self._flags())
        for c in self.cars:
            out.extend(self._advance(c))
        out.append(self._scoring())
        out.append(self._telemetry())
        out.extend(self.pending)
        self.pending = []
        return out

    def finished(self) -> bool:
        return self.et >= self.end_et

    def _weather(self) -> None:
        if self.et > self.wet_from and self.et < self.end_et * 0.75:
            self.rain = min(self.rain + DT / 600, 0.7)
        else:
            self.rain = max(self.rain - DT / 400, 0.0)
        if self.rain > 0.1:
            self.wetness = min(self.wetness + DT * 0.002, 1.0)
        else:
            self.wetness = max(self.wetness - DT * 0.0007, 0.0)

    def _flags(self) -> list[dict]:
        out = []
        start, end = self.end_et * 0.30, self.end_et * 0.30 + 150
        fcy = start <= self.et < end
        if fcy != self.fcy:
            self.fcy = fcy
            out.append(self._env("event", {"kind": "fcy_start" if fcy else "fcy_end", "car_id": 0, "vehicle_name": self.ours.name}, True))
        if self.ours.penalties == 0 and self.et > self.end_et * 0.40:
            self.ours.penalties = 1
        if self.ours.dent == 0 and self.et > self.end_et * 0.55:
            self.ours.dent = 1
        return out

    def _needs_pit(self, c: Car) -> bool:
        if c.requested:
            return True
        return c.fuel < c.fuel_use * 1.6 + RESERVE

    def _advance(self, c: Car) -> list[dict]:
        out: list[dict] = []
        if c.state == "stopped":
            c.speed = 0.0
            c.stop_left -= DT
            if c.fuel < c.refuel_to:
                c.fuel = min(c.fuel + 3.4 * DT, c.refuel_to)
            if c.stop_left <= 0 and c.fuel >= c.refuel_to - 1e-6:
                c.state, c.requested = "pit_out", False
            return out
        if c.state in ("pit_in", "pit_out"):
            c.speed = PIT_SPEED
            before = c.dist
            c.dist += PIT_SPEED * DT
            c.fuel -= c.fuel_use * PIT_SPEED * DT / LENGTH
            if c.state == "pit_in" and c.dist >= PIT_BOX * LENGTH:
                c.dist, c.state = PIT_BOX * LENGTH, "stopped"
                c.pits += 1
                self._start_stop(c)
                if c.ours:
                    c.driver_index += 1
                    out.append(self._env("event", {"kind": "driver_swap", "car_id": c.car_id, "vehicle_name": c.name, "driver": c.driver}, True))
                return out
            if c.dist >= LENGTH:
                c.dist -= LENGTH
                out.extend(self._line(c))
            if c.state == "pit_out" and PIT_EXIT * LENGTH <= c.dist < LENGTH / 2:  # past the line and out of the lane
                c.state = "race"
                out.append(self._env("event", {"kind": "pit_exit", "car_id": c.car_id, "vehicle_name": c.name,
                                               "fuel_added": round(max(c.fuel - c.fuel_in, 0.0), 2), "tyres_changed": c.fresh_tyres,
                                               "driver": c.driver}, True))
            return out
        v = self._speed(c)
        c.speed = v
        before = c.dist
        c.dist += v * DT
        c.fuel = max(c.fuel - c.fuel_use * v * DT / LENGTH, 0.0)
        for k, mark in enumerate((LENGTH / 3, 2 * LENGTH / 3)):
            if before < mark <= c.dist and len(c.splits) == k:
                c.splits.append(self.et - c.lap_start)
        if self.fcy:
            c.fcy_lap = True
        if before < PIT_ENTRY * LENGTH <= c.dist and self._needs_pit(c):
            c.state, c.in_lap = "pit_in", True
            out.append(self._env("event", {"kind": "pit_entry", "car_id": c.car_id, "vehicle_name": c.name}, True))
        if c.dist >= LENGTH:
            c.dist -= LENGTH
            out.extend(self._line(c))
        return out

    def _start_stop(self, c: Car) -> None:
        c.fuel_in = c.fuel
        c.refuel_to = c.capacity - self.rnd.uniform(0, 18 if c.ours else 12)
        c.fresh_tyres = self.rnd.random() < 0.65 or self.wetness > 0.3 and not c.wet_tyres
        if c.ours and c.pits == 2:
            c.fresh_tyres = False  # one stop on the old tyres, as a team does to save time
        tyres = (7.0 + self.rnd.uniform(0, 2.0)) if c.fresh_tyres else 0.0
        refuel = max(c.refuel_to - c.fuel, 0) / 3.4
        c.stop_left = max(tyres, refuel) + (6.0 if c.ours else 0.0)
        if c.fresh_tyres:
            c.wear, c.age = [1.0, 1.0, 1.0, 1.0], 0
        if self.wetness > 0.3:
            c.wet_tyres = c.wet_tyres or c.fresh_tyres
        elif self.wetness < 0.1:
            c.wet_tyres = c.wet_tyres and not c.fresh_tyres

    def _line(self, c: Car) -> list[dict]:
        """The car crossed the start line: the lap is complete."""
        if c.laps < 0:  # the first crossing only starts the race
            c.laps, c.lap_start, c.lap_fuel_start, c.splits = 0, self.et, c.fuel, []
            return []
        out = []
        lap_time = self.et - c.lap_start
        s1 = c.splits[0] if len(c.splits) > 0 else None
        s2 = c.splits[1] - c.splits[0] if len(c.splits) > 1 else None
        c.laps += 1
        c.last_lap, c.last_s1, c.last_s2 = lap_time, s1, s2
        if not (c.in_lap or c.out_lap or c.fcy_lap):
            c.best_lap = lap_time if c.best_lap is None else min(c.best_lap, lap_time)
        payload = {"car_id": c.car_id, "vehicle_name": c.name, "lap": c.laps, "driver": c.driver,
                   "lap_time": round(lap_time, 3), "s1": s1, "s2": s2, "s3": None if s2 is None or s1 is None else round(lap_time - s1 - s2, 3),
                   "fcy": c.fcy_lap, "in_lap": c.in_lap, "out_lap": c.out_lap}
        if c.ours:
            payload.update(fuel_start=round(c.lap_fuel_start, 2), fuel_end=round(c.fuel, 2), tyre_age=c.age + 1,
                           wear=[round(w, 4) for w in c.wear])
        out.append(self._env("lap", payload, True))
        for i in range(4):
            c.wear[i] = max(c.wear[i] - (0.018 + 0.004 * (i < 2) + self.rnd.uniform(0, 0.003)), 0.05)
        c.age += 1
        c.out_lap = c.in_lap
        c.in_lap = c.fcy_lap = False
        c.splits, c.lap_start, c.lap_fuel_start = [], self.et, c.fuel
        c.lap_target = self._lap_target(c)
        return out

    # -- messages --------------------------------------------------------

    def _order(self) -> list[Car]:
        return sorted(self.cars, key=lambda c: -c.total)

    def _vehicle(self, c: Car, place: int, gap: float | None, leader: Car) -> dict:
        offset = PIT_OFFSET if c.state != "race" else 0.0
        off_track = c.car_id == 3 and self.end_et * 0.2 <= self.et < self.end_et * 0.2 + 20
        x, z = position(c.dist, offset if not off_track else EDGE + 3.5)
        pit_state = {"race": 0, "pit_in": 2, "stopped": 3, "pit_out": 4}[c.state]
        if c.requested and c.state == "race":
            pit_state = 1
        v = {"car_id": c.car_id, "vehicle_name": c.name, "driver": c.driver, "vehicle_class": CLASS, "is_player": c.ours,
             "control": 0 if c.ours else 2, "place": place, "laps": max(c.laps, 0), "in_pits": c.state != "race",
             "lap_dist": round(c.dist, 2), "x": round(x, 2), "z": round(z, 2), "pit_state": pit_state, "num_pitstops": c.pits,
             "laps_behind_leader": max(leader.laps - c.laps - (1 if c.dist > leader.dist else 0), 0) if c.laps >= 0 else 0,
             "fuel": round(c.fuel, 2), "fuel_capacity": c.capacity, "wear": [round(w, 4) for w in c.wear],
             "penalties": c.penalties, "path_lateral": round(EDGE + 3.5 if off_track else 0.5, 2), "track_edge": EDGE}
        if c.last_lap is not None:
            v["last_lap"] = round(c.last_lap, 3)
        if c.best_lap is not None:
            v["best_lap"] = round(c.best_lap, 3)
        if c.last_s1 is not None:
            v["last_s1"] = round(c.last_s1, 3)
        if c.last_s2 is not None:
            v["last_s2"] = round(c.last_s2, 3)
        if gap is not None:
            v["time_behind_next"] = round(gap, 3)
        return v

    def _scoring(self) -> dict:
        order = self._order()
        vehicles = []
        for i, c in enumerate(order):
            gap = None
            if i > 0:
                gap = max((order[i - 1].total - c.total) / max(c.speed, 12.0), 0.0)
            vehicles.append(self._vehicle(c, i + 1, gap, order[0]))
        payload = {"track": TRACK, "session_type": "race", "end_et": self.end_et, "track_length": LENGTH,
                   "game_phase": 6 if self.fcy else 5, "yellow_state": 1 if self.fcy else 0, "sector_flags": [0, 0, 0],
                   "weather": {"ambient_temp": 22.0 - 3 * self.et / self.end_et, "track_temp": 31.0 - 8 * self.rain - 4 * self.et / self.end_et,
                               "rain": round(self.rain, 3), "wetness": round(self.wetness, 3), "dark_cloud": round(min(self.rain * 1.3, 1.0), 3)},
                   "vehicles": vehicles}
        return self._env("scoring", payload)

    def _telemetry(self) -> dict:
        c = self.ours
        load = 1 - shape(c.dist)  # warmer in the corners
        cool = 28 if c.wet_tyres else 0  # wet tyres run cooler
        surf = [[round(80 - cool + 14 * load + 3 * (i < 2) + j * 2 + math.sin(self.et / 40 + i) * 2, 1) for j in range(3)] for i in range(4)]
        compound = "Wet" if c.wet_tyres else "Medium"
        payload = {"car_id": c.car_id, "vehicle_name": c.name, "lap": max(c.laps, 0), "lap_dist": round(c.dist, 2), "fuel": round(c.fuel, 2),
                   "fuel_capacity": c.capacity, "wear": [round(w, 4) for w in c.wear],
                   "tyre_carcass_temp": [round(s[1] - 6, 1) for s in surf], "tyre_surface_temp": surf,
                   "tyre_inner_temp": [[round(s[j] - 8, 1) for j in range(3)] for s in surf],
                   "tyre_pressure": [round(165 + 12 * load + i, 1) for i in range(4)],
                   "brake_temp": [round(300 + 400 * load, 0)] * 4, "engine_water_temp": 90.0, "engine_oil_temp": 104.0,
                   "compound_front": compound, "compound_rear": compound,
                   "brake": round(min(max((0.8 - shape(c.dist)) * 3, 0.0), 1.0), 3), "dent_max": c.dent,
                   "pit_menu": self.menu}
        return self._env("telemetry", payload)

    # -- commands --------------------------------------------------------

    def command(self, msg: dict) -> dict:
        """Answers a command of the backend the way the agent does."""
        kind = msg.get("kind", "pit_menu")
        if kind == "pit_request":
            self.ours.requested = not self.ours.requested
            text = "pit request on" if self.ours.requested else "pit request off"
            return self._env("command_result", {"id": msg["id"], "ok": True, "message": text, "applied": []})
        applied = []
        for step in msg.get("steps", []):
            choice = str(step.get("choice"))
            self.menu = {"category": step["category"].upper(), "choice": choice if not choice.isdigit() else f"+{choice}/{int(choice) // 3}",
                         "choice_index": int(choice) if choice.isdigit() else 0, "choices": 123}
            applied.append({"category": step["category"], "choice": self.menu["choice"]})
        return self._env("command_result", {"id": msg["id"], "ok": True, "message": "ok", "applied": applied})


def messages(race: Race) -> Iterator[list[dict]]:
    """The messages of every step until the race is over."""
    yield [race.hello()]
    while not race.finished():
        yield race.step()


async def run(url: str, token: str, minutes: float, speed: float, cars: int, seed: int) -> None:
    from websockets.asyncio.client import connect

    race = Race(minutes=minutes, cars=cars, seed=seed)
    headers = {"Authorization": f"Bearer {token}"} if token else {}
    async with connect(url, additional_headers=headers, max_size=None) as ws:
        await ws.send(json.dumps(race.hello()))
        await ws.recv()  # the first ack

        async def listen() -> None:
            async for raw in ws:
                msg = json.loads(raw)
                if msg.get("type") == "command":
                    race.pending.append(race.command(msg))

        reader = asyncio.create_task(listen())
        try:
            log.info("simulating %.0f minutes of race with %d cars at %.0fx", minutes, cars, speed)
            last = 0
            while not race.finished():
                for m in race.step():
                    await ws.send(json.dumps(m))
                if int(race.et // 300) != last:
                    last = int(race.et // 300)
                    log.info("race time %d min, our place %d, lap %d", race.et // 60, [c.car_id for c in race._order()].index(0) + 1, race.ours.laps)
                await asyncio.sleep(DT / speed)
            log.info("race over")
        finally:
            reader.cancel()
