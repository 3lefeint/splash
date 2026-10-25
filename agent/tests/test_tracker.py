import math

from splash_agent.tracker import PlayerSample, Tracker

from . import fakes


def step(tr, vehicles, et, **info):
    s = fakes.scoring(vehicles, et=et, **info)
    return tr.on_scoring(s.mScoringInfo, [s.mVehicles[i] for i in range(len(vehicles))])


def car(laps, pits=False, driver=b"a", **kw):
    return {"id": 0, "name": b"me", "driver": driver, "laps": laps, "pits": pits, **kw}


def kinds(out):
    return [(d.type, d.payload.get("kind")) for d in out]


def test_first_sight_emits_nothing():
    assert step(Tracker(), [car(5)], 100) == []


def test_lap_completion_converts_sectors():
    tr = Tracker()
    step(tr, [car(3)], 100)
    out = step(tr, [car(4, last=98.4, s1=30.0, s12=62.0)], 110)
    assert [d.type for d in out] == ["lap"]
    p = out[0].payload
    assert p["lap"] == 4 and p["lap_time"] == 98.4 and p["s1"] == 30.0
    assert math.isclose(p["s2"], 32.0) and math.isclose(p["s3"], 36.4)
    assert p["driver"] == "a" and not (p["in_lap"] or p["out_lap"] or p["fcy"])
    assert "fuel_start" not in p  # no telemetry sample


def test_in_and_out_lap_flags_and_pit_events():
    tr = Tracker()
    step(tr, [car(3)], 100)
    assert kinds(step(tr, [car(3, pits=True)], 110)) == [("event", "pit_entry")]
    out = step(tr, [car(4, pits=True, last=99.0)], 115)  # line crossed in the pit lane
    assert out[0].payload["in_lap"] and not out[0].payload["out_lap"]
    assert kinds(step(tr, [car(4, pits=False)], 140)) == [("event", "pit_exit")]
    out = step(tr, [car(5, last=120.0)], 200)
    assert out[0].payload["out_lap"] and not out[0].payload["in_lap"]
    out = step(tr, [car(6, last=98.0)], 300)
    assert not (out[0].payload["in_lap"] or out[0].payload["out_lap"])


def test_fuel_and_wear_only_for_the_player_car():
    tr = Tracker()
    tr.set_player(PlayerSample(0, 60.0, [0.9] * 4))
    step(tr, [car(3), {"id": 1, "name": b"rival", "laps": 3}], 100)
    tr.set_player(PlayerSample(0, 57.0, [0.88] * 4))
    out = step(tr, [car(4, last=98.0), {"id": 1, "name": b"rival", "laps": 4, "last": 99.0}], 110)
    mine = next(d.payload for d in out if d.payload["car_id"] == 0)
    rival = next(d.payload for d in out if d.payload["car_id"] == 1)
    assert mine["fuel_start"] == 60.0 and mine["fuel_end"] == 57.0 and mine["wear"] == [0.88] * 4
    assert "fuel_start" not in rival and "wear" not in rival
    # the next lap starts from the fuel level at the line
    tr.set_player(PlayerSample(0, 54.0, [0.86] * 4))
    out = step(tr, [car(5, last=98.0)], 210)
    assert out[0].payload["fuel_start"] == 57.0 and out[0].payload["fuel_end"] == 54.0


def test_pit_exit_reports_fuel_added_and_tyre_change():
    tr = Tracker()
    tr.set_player(PlayerSample(0, 30.0, [0.2] * 4))
    step(tr, [car(5)], 100)
    step(tr, [car(5, pits=True)], 110)
    tr.set_player(PlayerSample(0, 85.5, [0.95] * 4))  # refuelled and fresh tyres (direction does not matter)
    out = step(tr, [car(5, pits=False)], 150)
    assert out[0].payload == {"kind": "pit_exit", "car_id": 0, "vehicle_name": "me", "fuel_added": 55.5,
                              "tyres_changed": True}
    # a stop without a tyre change
    step(tr, [car(6, pits=True)], 400)
    tr.set_player(PlayerSample(0, 88.0, [0.94] * 4))
    out = step(tr, [car(6, pits=False)], 430)
    assert out[0].payload["tyres_changed"] is False and out[0].payload["fuel_added"] == 2.5


def test_pit_exit_of_an_opponent_has_no_fuel_data():
    tr = Tracker()
    rival = {"id": 1, "name": b"rival", "laps": 3}
    step(tr, [rival], 100)
    step(tr, [{**rival, "pits": True}], 110)
    out = step(tr, [{**rival, "pits": False}], 150)
    assert out[0].payload == {"kind": "pit_exit", "car_id": 1, "vehicle_name": "rival"}


def test_driver_swap_event_and_lap_driver():
    tr = Tracker()
    step(tr, [car(3, driver=b"a")], 100)
    step(tr, [car(3, pits=True, driver=b"a")], 110)
    out = step(tr, [car(3, pits=True, driver=b"b")], 130)
    assert out[0].payload == {"kind": "driver_swap", "car_id": 0, "vehicle_name": "me", "driver": "b"}
    out = step(tr, [car(4, pits=True, driver=b"b", last=100.0)], 140)
    assert out[0].payload["driver"] == "a"  # a started this lap
    step(tr, [car(4, pits=False, driver=b"b")], 150)
    out = step(tr, [car(5, driver=b"b", last=110.0)], 250)
    assert out[0].payload["driver"] == "b"


def test_yellow_marks_only_the_lap_it_was_seen_in():
    tr = Tracker()
    step(tr, [car(3)], 100)
    step(tr, [car(3)], 110, yellow=1)
    out = step(tr, [car(4, last=130.0)], 200)
    assert out[0].payload["fcy"] is True
    out = step(tr, [car(5, last=98.0)], 300)
    assert out[0].payload["fcy"] is False


def test_full_course_yellow_by_game_phase_counts():
    tr = Tracker()
    step(tr, [car(3)], 100)
    step(tr, [car(3)], 110, phase=6)
    out = step(tr, [car(4, last=98.0)], 200)
    assert out[0].payload["fcy"] is True


def test_sector_flags_and_pit_lane_states_do_not_count():
    tr = Tracker()
    step(tr, [car(3)], 100)
    step(tr, [car(3)], 110, mSectorFlag=(1, 11, 11))
    step(tr, [car(3)], 120, yellow=4)
    step(tr, [car(3)], 130, yellow=5)
    out = step(tr, [car(4, last=98.0)], 200)
    assert out[0].payload["fcy"] is False


def test_session_restart_resets_without_emitting():
    tr = Tracker()
    step(tr, [car(10)], 100)
    assert step(tr, [car(0)], 5) == []  # lap counter went back
    out = step(tr, [car(1, last=99.0)], 100)
    assert [d.type for d in out] == ["lap"]


def test_unscored_slots_are_ignored():
    assert step(Tracker(), [car(3, place=0)], 100) == []
