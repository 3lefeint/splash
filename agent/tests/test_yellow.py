import json

from splash_agent import convert as cv

from . import fakes
from .test_agent import make_agent, validate


def cars(n=3, **extra):
    return [{"id": i, "name": f"car{i}".encode(), "driver": b"d", "place": i + 1, "dist": 100.0 * i,
             "player": i == 0, "sector": (1, 2, 0)[i % 3], **extra} for i in range(n)]


def payload(flags, vehicles, **info):
    b = fakes.scoring(vehicles, **info)
    for k, f in enumerate(flags):
        b.mScoringInfo.mSectorFlag[k] = f
    vs = [b.mVehicles[i] for i in range(len(vehicles))]
    return cv.scoring_payload(b.mScoringInfo, vs)


def test_a_green_session_adds_only_the_sector_of_our_own_car():
    for flags in ([0, 0, 0], [11, 11, 11]):  # the plugin reports 11 for a clear sector
        p = payload(flags, cars())
        assert p["sector_flags"] == flags
        for v in p["vehicles"]:
            assert ("sector" in v) == v["is_player"]  # the backend learns the sector borders from our car
            assert "flag" not in v and "under_yellow" not in v


def test_with_a_sector_flag_up_every_car_says_which_sector_it_is_in():
    p = payload([1, 11, 11], cars(3))
    # the plugin's mSector 1, 2, 0 are the sectors 1, 2, 3
    assert [v["sector"] for v in p["vehicles"]] == [1, 2, 3]
    assert p["sector_flags"] == [1, 11, 11]


def test_flag_and_under_yellow_are_sent_only_when_not_at_rest():
    vs = cars(2)
    vs[1].update(flag=1, under_yellow=True)
    p = payload([11, 11, 11], vs)
    assert "flag" not in p["vehicles"][0] and "under_yellow" not in p["vehicles"][0]
    assert p["vehicles"][1]["flag"] == 1 and p["vehicles"][1]["under_yellow"] is True


def test_messages_with_the_new_fields_match_the_schema(tmp_path):
    vs = cars(3)
    vs[2].update(flag=1, under_yellow=True)
    agent, _, _ = make_agent(tmp_path, fakes.scoring(vs), fakes.telemetry([{"id": 0}]))
    b = fakes.scoring(vs)
    b.mScoringInfo.mSectorFlag[0] = 1
    agent.scoring.update(b)
    agent.poll_scoring()
    msg = validate(agent.link.live["scoring"])
    assert msg["payload"]["sector_flags"] == [1, 0, 0]
    assert [v["sector"] for v in msg["payload"]["vehicles"]] == [1, 2, 3]
    assert msg["payload"]["vehicles"][2]["under_yellow"] is True
    agent.close()


def test_an_old_message_without_the_fields_is_still_valid(tmp_path):
    agent, _, _ = make_agent(tmp_path, fakes.scoring(cars(2)), fakes.telemetry([{"id": 0}]))
    agent.poll_scoring()
    msg = validate(agent.link.live["scoring"])
    assert all("sector" not in v for v in msg["payload"]["vehicles"] if not v["is_player"])
    agent.close()


def test_the_added_bytes_per_scoring_message():
    n = 40
    vehicles = lambda flags: len(json.dumps(payload(flags, cars(n))["vehicles"], separators=(",", ":")))  # noqa: E731
    green, yellow = vehicles([11, 11, 11]), vehicles([1, 11, 11])
    assert vehicles([0, 0, 0]) == green  # the same in any green session
    assert (yellow - green) == 11 * (n - 1)  # while a flag is up: ',"sector":N' (11 bytes) for each car but ours
    plain = len(json.dumps([{k: x for k, x in v.items() if k != "sector"} for v in payload([11, 11, 11], cars(n))["vehicles"]], separators=(",", ":")))
    assert green - plain == 11  # a green session costs the 11 bytes for the sector of our own car
    # the sector_flags array itself is the same three numbers whether the sector is flagged or not
    assert len(json.dumps(payload([1, 11, 11], cars(1))["sector_flags"])) == len(json.dumps([1, 11, 11]))


class CountingBuffer(fakes.FakeBuffer):
    reads = 0

    def read(self, prefix=None):
        CountingBuffer.reads += 1
        return super().read(prefix)


def test_a_yellow_flag_does_not_cause_any_additional_read(tmp_path):
    def reads(flags):
        CountingBuffer.reads = 0
        agent, _, _ = make_agent(tmp_path, fakes.scoring(cars(3)), fakes.telemetry([{"id": 0}]))
        agent.scoring = CountingBuffer(fakes.scoring(cars(3)))
        agent.telemetry = CountingBuffer(fakes.telemetry([{"id": 0}]))
        b = fakes.scoring(cars(3))
        for k, f in enumerate(flags):
            b.mScoringInfo.mSectorFlag[k] = f
        agent.scoring.update(b)
        agent.poll_scoring()
        agent.close()
        return CountingBuffer.reads

    assert reads([11, 11, 11]) == reads([1, 11, 1])
