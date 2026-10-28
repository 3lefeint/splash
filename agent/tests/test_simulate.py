import collections
import functools

from splash_agent import simulate as sim

from .test_agent import VALIDATOR


@functools.lru_cache(maxsize=None)
def run(minutes=120, cars=10, seed=1, sample=25):
    race = sim.Race(minutes=minutes, cars=cars, seed=seed)
    seen, kinds, states = [], collections.Counter(), []
    VALIDATOR.validate(race.hello())
    step = 0
    while not race.finished():
        msgs = race.step()
        step += 1
        for m in msgs:
            if m["type"] in ("lap", "event") or step % sample == 0:
                VALIDATOR.validate(m)
            if m["type"] == "event":
                kinds[m["payload"]["kind"]] += 1
            seen.append(m)
        states.append((race.rain, race.wetness, race.fcy))
    return race, seen, kinds, states


def test_every_message_follows_the_schema_and_the_race_has_what_the_dashboard_needs():
    race, seen, kinds, states = run()
    assert kinds["pit_entry"] >= 10 and kinds["pit_exit"] >= 10 and kinds["driver_swap"] >= 2
    assert kinds["fcy_start"] == 1 and kinds["fcy_end"] == 1
    assert max(s[0] for s in states) > 0.5 and max(s[1] for s in states) > 0.3  # the rain comes
    assert race.ours.penalties == 1 and race.ours.dent == 1
    scoring = [m for m in seen if m["type"] == "scoring"]
    last = scoring[-1]["payload"]["vehicles"]
    assert [v["place"] for v in last] == list(range(1, 11))
    assert all(v["laps"] > 30 for v in last), "a car did too few laps for two hours"
    assert any(v["path_lateral"] > v["track_edge"] for m in scoring for v in m["payload"]["vehicles"]), "no car off the track"
    assert any(m["payload"]["game_phase"] == 6 for m in scoring)


def test_laps_are_numbered_in_order_with_plausible_times_and_pit_flags():
    _, seen, _, _ = run()
    laps = collections.defaultdict(list)
    for m in seen:
        if m["type"] == "lap":
            laps[m["payload"]["car_id"]].append(m["payload"])
    assert len(laps) == 10
    for car, ls in laps.items():
        assert [l["lap"] for l in ls] == list(range(1, len(ls) + 1)), car
        normal = [l["lap_time"] for l in ls if not (l["in_lap"] or l["out_lap"] or l["fcy"])]
        assert normal and 80 < min(normal) and max(normal) < 125, (car, min(normal), max(normal))
        assert any(l["in_lap"] for l in ls) and any(l["out_lap"] for l in ls)
    ours = laps[0]
    assert all("fuel_start" in l and "wear" in l for l in ours) and "fuel_start" not in laps[1][0]


def test_our_fuel_never_runs_dry_and_a_stop_refuels_and_changes_tyres():
    race = sim.Race(minutes=120, cars=6, seed=2)
    lowest, wears = 99.0, []
    while not race.finished():
        race.step()
        lowest = min(lowest, race.ours.fuel)
        wears.append(min(race.ours.wear))
    assert lowest > 0.5, lowest
    assert min(wears) < 0.7 and race.ours.pits >= 3  # the tyres wear and are changed
    assert race.ours.driver_index == race.ours.pits  # one driver change per stop


def test_commands_are_answered_and_a_pit_request_brings_the_car_in():
    race = sim.Race(minutes=60, cars=6, seed=3)
    for _ in range(300):
        race.step()
    res = race.command({"type": "command", "id": "c1", "kind": "pit_request"})
    VALIDATOR.validate(res)
    assert res["payload"]["ok"] and res["payload"]["message"] == "pit request on" and race.ours.requested
    res = race.command({"type": "command", "id": "c2", "steps": [{"category": "FUEL:", "choice": "max"}, {"category": "TIRES:", "choice": "55"}]})
    VALIDATOR.validate(res)
    assert [a["category"] for a in res["payload"]["applied"]] == ["FUEL:", "TIRES:"]
    assert race.menu["category"] == "TIRES:"
    race.command({"type": "command", "id": "c3", "kind": "pit_request"})
    assert not race.ours.requested  # a second request cancels
    race.command({"type": "command", "id": "c4", "kind": "pit_request"})
    pits = race.ours.pits
    for _ in range(600):
        race.step()
    assert race.ours.pits == pits + 1, "the requested stop did not happen"


def test_same_seed_same_race():
    a = [m["payload"] for m in run(minutes=20, cars=4, seed=7, sample=10**9)[1] if m["type"] == "lap"]
    b = [m["payload"] for m in run(minutes=20, cars=4, seed=7, sample=10**9)[1] if m["type"] == "lap"]
    assert a == b and a


def test_a_real_track_replaces_the_invented_one(tmp_path, monkeypatch):
    for name in ("TRACK", "LENGTH", "_TABLE", "CORNERS", "_INV_MEAN", "TIME_SCALE", "CAPACITY", "PIT_ENTRY", "PIT_BOX", "PIT_EXIT"):
        monkeypatch.setattr(sim, name, getattr(sim, name))  # put the invented track back after the test
    ring = {"track": "Test Ring", "length": 20000.0, "points": [[0, 0], [1000, 0], [1000, 1000], [0, 1000]],
            "corners": [{"from": 100, "to": 300}, {"from": 19900, "to": 100}, {"from": 5000, "to": None}]}
    path = tmp_path / "ring.json"
    path.write_text(__import__("json").dumps(ring))
    sim.load_track(str(path))
    assert (sim.TRACK, sim.LENGTH) == ("Test Ring", 20000.0)
    assert sim.CORNERS == [200.0, 0.0]  # a corner across the start line sits in the middle of it; unfinished ones are skipped
    assert sim.position(0) == (0, 0)
    x, z = sim.position(5000)  # a quarter of the lap is the first corner of the square
    assert (round(x), round(z)) == (1000, 0)
    assert sim.TIME_SCALE > 1 and sim.PIT_ENTRY > 0.99
    race = sim.Race(minutes=10, cars=3)
    scoring = next(m for step in (race.step() for _ in range(3)) for m in step if m["type"] == "scoring")
    assert scoring["payload"]["track"] == "Test Ring" and scoring["payload"]["track_length"] == 20000.0
