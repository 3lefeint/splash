"""Conversion of raw plugin structs into wire format payloads."""

import hashlib
import math

KELVIN = 273.15

SESSION_TYPES = [
    (0, 0, "test"),
    (1, 4, "practice"),
    (5, 8, "qualifying"),
    (9, 9, "warmup"),
    (10, 13, "race"),
]


def text(raw: bytes) -> str:
    """Decode a NUL-terminated char array; the game mixes UTF-8 and Windows-1252."""
    raw = raw.split(b"\0", 1)[0]
    try:
        return raw.decode("utf-8")
    except UnicodeDecodeError:
        return raw.decode("cp1252", errors="replace")


def num(v: float) -> float | None:
    """A JSON-safe number: NaN and infinity become None."""
    return v if math.isfinite(v) else None


def positive(v: float) -> float | None:
    """Lap and sector times use 0 or -1 for "not set"."""
    return v if math.isfinite(v) and v > 0 else None


def nonneg(v: float) -> float | None:
    return v if math.isfinite(v) and v >= 0 else None


def session_type(session: int) -> str:
    for lo, hi, name in SESSION_TYPES:
        if lo <= session <= hi:
            return name
    return f"session-{session}"


def session_id(info) -> str:
    """Identify the game session identically on every PC that joined it.

    The id is built from values the server shares with all clients. A restart of
    the same session on the same track therefore keeps the id.
    """
    key = "|".join([
        text(info.mTrackName),
        str(info.mSession),
        f"{info.mStartET:.0f}",
        text(info.mServerName),
    ])
    return hashlib.sha1(key.encode()).hexdigest()[:12]


def car_state(t) -> dict:
    """Fuel and mean tyre wear of any car from its telemetry slot; values the game does not deliver are left out."""
    out = {}
    if math.isfinite(t.mFuel) and t.mFuel >= 0 and math.isfinite(t.mFuelCapacity) and t.mFuelCapacity > 0:
        out["fuel"], out["fuel_capacity"] = t.mFuel, t.mFuelCapacity
    wear = [t.mWheels[i].mWear for i in range(4)]
    if all(math.isfinite(x) and 0 <= x <= 1 for x in wear):
        out["wear"] = wear
    return out


def scoring_vehicle(v) -> dict | None:
    """Wire vehicle for scoring, or None for a slot that is not scored yet."""
    name = text(v.mVehicleName)
    if v.mPlace < 1 or not name:
        return None
    s1 = positive(v.mLastSector1)
    s2 = positive(v.mLastSector2)
    return {
        "car_id": v.mID,
        "vehicle_name": name,
        "driver": text(v.mDriverName),
        "vehicle_class": text(v.mVehicleClass),
        "is_player": bool(v.mIsPlayer),
        "control": v.mControl if -1 <= v.mControl <= 2 else None,
        "place": v.mPlace,
        "laps": max(v.mTotalLaps, 0),
        "last_lap": positive(v.mLastLapTime),
        "best_lap": positive(v.mBestLapTime),
        "last_s1": s1,
        # The plugin reports sector 2 as S1+S2.
        "last_s2": s2 - s1 if s1 is not None and s2 is not None and s2 > s1 else None,
        "in_pits": bool(v.mInPits),
        "lap_dist": nonneg(v.mLapDist),
        "x": num(v.mPos.x),
        "z": num(v.mPos.z),
        "pit_state": v.mPitState,
        "num_pitstops": max(v.mNumPitstops, 0),
        "time_behind_next": num(v.mTimeBehindNext),
        "laps_behind_leader": max(v.mLapsBehindLeader, 0),
        "penalties": max(v.mNumPenalties, 0),
        "path_lateral": num(v.mPathLateral),
        "track_edge": num(v.mTrackEdge),
    }


def scoring_payload(info, vehicles, states: dict | None = None) -> dict:
    """Payload of a scoring message; vehicles is the list of raw slots in use.

    ``states`` maps a car id to ``car_state`` of its telemetry slot, so every car carries fuel and tyre wear.
    """
    out = []
    for v in vehicles:
        w = scoring_vehicle(v)
        if w is not None:
            w.update((states or {}).get(v.mID, {}))
            # None means "unknown" and is dropped, the schema has no null for these.
            out.append({k: x for k, x in w.items() if x is not None or k in _NULLABLE})
    return {
        "track": text(info.mTrackName),
        "session_type": session_type(info.mSession),
        "end_et": info.mEndET,
        **({"track_length": info.mLapDist} if math.isfinite(info.mLapDist) and info.mLapDist > 0 else {}),
        "game_phase": info.mGamePhase,
        "yellow_state": info.mYellowFlagState,
        "sector_flags": list(info.mSectorFlag),
        "weather": {
            "ambient_temp": info.mAmbientTemp,
            "track_temp": info.mTrackTemp,
            "rain": min(max(info.mRaining, 0.0), 1.0),
            **_unit("wetness", info.mAvgPathWetness),
            **_unit("dark_cloud", info.mDarkCloud),
        },
        "vehicles": out,
    }


_NULLABLE = {"last_lap", "best_lap", "last_s1", "last_s2", "time_behind_next"}


def _unit(key: str, v: float) -> dict:
    """A 0..1 value as a one-entry dict, empty when the game delivers nothing usable."""
    return {key: min(max(v, 0.0), 1.0)} if math.isfinite(v) else {}


def _celsius(kelvin: float) -> float | None:
    return kelvin - KELVIN if math.isfinite(kelvin) and kelvin > 0 else None


def tyre_details(t) -> dict:
    """Optional tyre, brake and engine values; a value the game does not deliver is left out."""
    out: dict = {}
    surface = [[_celsius(t.mWheels[i].mTemperature[j]) for j in range(3)] for i in range(4)]
    inner = [[_celsius(t.mWheels[i].mTireInnerLayerTemperature[j]) for j in range(3)] for i in range(4)]
    if all(v is not None for row in surface for v in row):
        out["tyre_surface_temp"] = surface
    if all(v is not None for row in inner for v in row):
        out["tyre_inner_temp"] = inner
    pressure = [t.mWheels[i].mPressure for i in range(4)]
    if all(math.isfinite(v) and v > 0 for v in pressure):
        out["tyre_pressure"] = pressure
    brakes = [t.mWheels[i].mBrakeTemp for i in range(4)]
    if all(math.isfinite(v) and v >= 0 for v in brakes):
        out["brake_temp"] = brakes
    for key, v in (("engine_water_temp", t.mEngineWaterTemp), ("engine_oil_temp", t.mEngineOilTemp)):
        if math.isfinite(v) and v > 0:
            out[key] = v
    front, rear = text(t.mFrontTireCompoundName), text(t.mRearTireCompoundName)
    if front:
        out["compound_front"] = front
    if rear:
        out["compound_rear"] = rear
    return out


def pit_menu(menu) -> dict | None:
    """The selected row of the pit menu, or None while the game shows none."""
    category = text(menu.mCategoryName)
    if not category or menu.mNumChoices <= 0:
        return None
    return {"category": category, "choice": text(menu.mChoiceString), "choice_index": menu.mChoiceIndex,
            "choices": menu.mNumChoices}


def telemetry_payload(t, car_id: int, vehicle_name: str, lap_dist: float | None) -> dict | None:
    """Payload of a telemetry message, or None if the values are not usable."""
    if not (t.mFuelCapacity > 0 and math.isfinite(t.mFuel) and t.mFuel >= 0):
        return None
    wear = [t.mWheels[i].mWear for i in range(4)]
    temps = [t.mWheels[i].mTireCarcassTemperature - KELVIN for i in range(4)]
    if not all(math.isfinite(x) for x in wear + temps):
        return None
    payload = {
        "car_id": car_id,
        "vehicle_name": vehicle_name,
        "lap": max(t.mLapNumber, 0),
        "fuel": t.mFuel,
        "fuel_capacity": t.mFuelCapacity,
        "wear": wear,
        "tyre_carcass_temp": temps,
    }
    if lap_dist is not None and math.isfinite(lap_dist) and lap_dist >= 0:
        payload["lap_dist"] = lap_dist
    payload.update(tyre_details(t))
    if math.isfinite(t.mUnfilteredBrake):
        payload["brake"] = min(max(t.mUnfilteredBrake, 0.0), 1.0)
    payload["dent_max"] = min(max(max(t.mDentSeverity), 0), 2)
    if t.mOverheating:
        payload["overheating"] = True
    if t.mDetached:
        payload["detached"] = True
    return payload
