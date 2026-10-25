import pytest
import math

from splash_agent import convert as cv

from . import fakes


def test_text_decoding():
    assert cv.text(b"abc\0junk") == "abc"
    assert cv.text("Müller".encode("utf-8") + b"\0") == "Müller"
    assert cv.text("Müller".encode("cp1252") + b"\0") == "Müller"  # not valid UTF-8


def test_session_type():
    assert [cv.session_type(n) for n in (0, 3, 6, 9, 12)] == ["test", "practice", "qualifying", "warmup", "race"]


def test_session_id_is_stable_and_differs_per_session():
    a = fakes.scoring().mScoringInfo
    b = fakes.scoring(et=5000.0).mScoringInfo  # only the time differs
    c = fakes.scoring(start_et=40000.0).mScoringInfo
    assert cv.session_id(a) == cv.session_id(b) != cv.session_id(c)


def test_scoring_vehicle_converts_sectors_and_skips_unscored():
    s = fakes.scoring([{"last": 98.4, "s1": 30.0, "s12": 62.0, "gap": 3.5}, {"place": 0}])
    w = cv.scoring_vehicle(s.mVehicles[0])
    assert w["last_s1"] == 30.0 and math.isclose(w["last_s2"], 32.0)  # plugin reports S1+S2
    assert w["last_lap"] == 98.4 and w["time_behind_next"] == 3.5
    assert cv.scoring_vehicle(s.mVehicles[1]) is None


def test_scoring_vehicle_unset_times_are_null():
    s = fakes.scoring([{}])  # lap and sector times are -1
    w = cv.scoring_vehicle(s.mVehicles[0])
    assert w["last_lap"] is None and w["last_s1"] is None and w["last_s2"] is None


def test_scoring_payload_drops_unscored_vehicles():
    s = fakes.scoring([{}, {"place": 0}])
    p = cv.scoring_payload(s.mScoringInfo, [s.mVehicles[0], s.mVehicles[1]])
    assert len(p["vehicles"]) == 1 and p["session_type"] == "race" and p["sector_flags"] == [0, 0, 0]


def test_telemetry_payload_kelvin_to_celsius():
    t = fakes.telemetry([{"fuel": 40.0, "wear": [0.1, 0.2, 0.3, 0.4]}])
    p = cv.telemetry_payload(t.mVehicles[0], 0, "car", 123.0)
    assert p["fuel"] == 40.0 and p["wear"] == [0.1, 0.2, 0.3, 0.4]
    assert all(math.isclose(a, b) for a, b in zip(p["tyre_carcass_temp"], [80, 81, 82, 83]))
    assert p["lap_dist"] == 123.0


def test_telemetry_payload_rejects_unusable_values():
    assert cv.telemetry_payload(fakes.telemetry([{"cap": 0.0}]).mVehicles[0], 0, "car", None) is None
    assert cv.telemetry_payload(fakes.telemetry([{"fuel": -1.0}]).mVehicles[0], 0, "car", None) is None
    assert cv.telemetry_payload(fakes.telemetry([{"wear": [math.nan] * 4}]).mVehicles[0], 0, "car", None) is None


def test_scoring_payload_carries_positions_and_track_length():
    s = fakes.scoring([{"dist": 321.5}], mLapDist=2000.0)
    veh = s.mVehicles[0]
    veh.mPos.x, veh.mPos.z = 12.5, -40.25
    p = cv.scoring_payload(s.mScoringInfo, [veh])
    assert p["track_length"] == 2000.0
    assert (p["vehicles"][0]["lap_dist"], p["vehicles"][0]["x"], p["vehicles"][0]["z"]) == (321.5, 12.5, -40.25)


def test_telemetry_payload_carries_tyre_brake_and_engine_values():
    t = fakes.telemetry([{"fuel": 40.0}])
    p = cv.telemetry_payload(t.mVehicles[0], 0, "car", 1.0)
    assert p["tyre_surface_temp"][1] == [pytest.approx(90.0), pytest.approx(91.0), pytest.approx(92.0)]
    assert p["tyre_inner_temp"][0][0] == pytest.approx(70.0)
    assert p["tyre_pressure"] == [180.0, 181.0, 182.0, 183.0] and p["brake_temp"][3] == 403.0
    assert p["engine_water_temp"] == 90.0 and p["engine_oil_temp"] == 100.0 and p["compound_front"] == "Medium"
    assert "compound_rear" not in p


def test_missing_game_values_are_left_out():
    t = fakes.telemetry([{"fuel": 40.0}])
    t.mVehicles[0].mWheels[2].mPressure = 0.0
    t.mVehicles[0].mEngineWaterTemp = float("nan")
    p = cv.telemetry_payload(t.mVehicles[0], 0, "car", None)
    assert "tyre_pressure" not in p and "engine_water_temp" not in p and "tyre_surface_temp" in p


def test_weather_carries_wetness():
    s = fakes.scoring([{}], mAvgPathWetness=0.4, mDarkCloud=0.25)
    w = cv.scoring_payload(s.mScoringInfo, [s.mVehicles[0]])["weather"]
    assert w["wetness"] == 0.4 and w["dark_cloud"] == 0.25
