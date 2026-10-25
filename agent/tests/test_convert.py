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
