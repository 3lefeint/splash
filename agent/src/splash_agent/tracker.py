"""Derives laps and pit events from successive scoring updates."""

from dataclasses import dataclass, field

from . import convert as cv

# A tyre set counts as changed when the wear of any tyre moved by more than this.
# The direction of mWear (new = 0 or 1) does not matter.
TYRE_CHANGE_DELTA = 0.15
# Fuel added is reported rounded to this many decimals.
FUEL_DECIMALS = 2


@dataclass
class Durable:
    """A message that is spooled and acknowledged: a lap or an event."""

    type: str  # "lap" or "event"
    et: float
    payload: dict


@dataclass
class PlayerSample:
    car_id: int
    fuel: float
    wear: list[float]


@dataclass
class CarState:
    laps: int
    in_pits: bool
    driver: str
    lap_driver: str
    started_in_pits: bool
    saw_entry: bool = False
    saw_exit: bool = False
    yellow_seen: bool = False
    fuel_at_lap_start: float | None = None
    fuel_at_entry: float | None = None
    wear_at_entry: list[float] | None = None


@dataclass
class Tracker:
    cars: dict = field(default_factory=dict)
    player: PlayerSample | None = None

    def reset(self) -> None:
        self.cars.clear()
        self.player = None

    def set_player(self, sample: PlayerSample | None) -> None:
        self.player = sample

    def _sample(self, car_id: int) -> PlayerSample | None:
        p = self.player
        return p if p is not None and p.car_id == car_id else None

    def on_scoring(self, info, vehicles) -> list[Durable]:
        """Process one scoring update and return the laps and events it completed."""
        et = info.mCurrentET
        yellow = info.mYellowFlagState > 0 or any(info.mSectorFlag)
        out: list[Durable] = []
        for v in vehicles:
            w = cv.scoring_vehicle(v)
            if w is None:
                continue
            key = (v.mID, w["vehicle_name"])
            driver = w["driver"]
            sample = self._sample(v.mID)
            st = self.cars.get(key)
            if st is None or v.mTotalLaps < st.laps:  # first sight or session restart
                self.cars[key] = CarState(
                    laps=v.mTotalLaps, in_pits=bool(v.mInPits), driver=driver, lap_driver=driver,
                    started_in_pits=bool(v.mInPits), yellow_seen=yellow,
                    fuel_at_lap_start=sample.fuel if sample else None)
                continue
            if yellow:
                st.yellow_seen = True

            if v.mInPits and not st.in_pits:
                st.saw_entry = True
                st.fuel_at_entry = sample.fuel if sample else None
                st.wear_at_entry = list(sample.wear) if sample else None
                out.append(Durable("event", et, _event("pit_entry", w)))
            elif not v.mInPits and st.in_pits:
                st.saw_exit = True
                out.append(Durable("event", et, _exit_event(w, st, sample)))
            st.in_pits = bool(v.mInPits)

            if driver and driver != st.driver:
                out.append(Durable("event", et, _event("driver_swap", w, driver=driver)))
                st.driver = driver

            if v.mTotalLaps > st.laps:
                out.append(Durable("lap", et, _lap(w, v, st, sample)))
                st.laps = v.mTotalLaps
                st.lap_driver = st.driver
                st.started_in_pits = st.in_pits
                st.saw_entry = st.saw_exit = False
                st.yellow_seen = yellow
                st.fuel_at_lap_start = sample.fuel if sample else None
        return out


def _event(kind: str, w: dict, **extra) -> dict:
    p = {"kind": kind, "car_id": w["car_id"], "vehicle_name": w["vehicle_name"]}
    p.update(extra)
    return p


def _exit_event(w: dict, st: CarState, sample: PlayerSample | None) -> dict:
    extra = {}
    if sample is not None and st.fuel_at_entry is not None:
        extra["fuel_added"] = round(max(sample.fuel - st.fuel_at_entry, 0.0), FUEL_DECIMALS)
    if sample is not None and st.wear_at_entry is not None:
        extra["tyres_changed"] = any(
            abs(a - b) > TYRE_CHANGE_DELTA for a, b in zip(sample.wear, st.wear_at_entry))
    return _event("pit_exit", w, **extra)


def _lap(w: dict, v, st: CarState, sample: PlayerSample | None) -> dict:
    lap_time = cv.positive(v.mLastLapTime)
    s1 = cv.positive(v.mLastSector1)
    s1_s2 = cv.positive(v.mLastSector2)  # cumulative in the plugin
    p = {
        "car_id": w["car_id"],
        "vehicle_name": w["vehicle_name"],
        "lap": v.mTotalLaps,
        "lap_time": lap_time,
        "s1": s1,
        "s2": s1_s2 - s1 if s1 is not None and s1_s2 is not None and s1_s2 > s1 else None,
        "s3": lap_time - s1_s2 if lap_time is not None and s1_s2 is not None and lap_time > s1_s2 else None,
        "fcy": st.yellow_seen,
        "in_lap": st.saw_entry,
        "out_lap": st.started_in_pits or st.saw_exit,
    }
    if st.lap_driver:
        p["driver"] = st.lap_driver
    if sample is not None:
        p["fuel_start"] = st.fuel_at_lap_start
        p["fuel_end"] = sample.fuel
        p["wear"] = list(sample.wear)
    return p
