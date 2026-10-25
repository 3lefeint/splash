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
        "pit_state": v.mPitState,
        "num_pitstops": max(v.mNumPitstops, 0),
        "time_behind_next": num(v.mTimeBehindNext),
        "laps_behind_leader": max(v.mLapsBehindLeader, 0),
    }


def scoring_payload(info, vehicles) -> dict:
    """Payload of a scoring message; vehicles is the list of raw slots in use."""
    out = []
    for v in vehicles:
        w = scoring_vehicle(v)
        if w is not None:
            # None means "unknown" and is dropped, the schema has no null for these.
            out.append({k: x for k, x in w.items() if x is not None or k in _NULLABLE})
    return {
        "track": text(info.mTrackName),
        "session_type": session_type(info.mSession),
        "end_et": info.mEndET,
        "game_phase": info.mGamePhase,
        "yellow_state": info.mYellowFlagState,
        "sector_flags": list(info.mSectorFlag),
        "weather": {
            "ambient_temp": info.mAmbientTemp,
            "track_temp": info.mTrackTemp,
            "rain": min(max(info.mRaining, 0.0), 1.0),
        },
        "vehicles": out,
    }


_NULLABLE = {"last_lap", "best_lap", "last_s1", "last_s2", "time_behind_next"}


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
    return payload
