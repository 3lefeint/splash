import json

from splash_agent.config import Config
from splash_agent.main import Agent
from splash_agent.raw import read_frames, replay

from . import fakes

FIELD = [{"id": 0, "name": b"me", "driver": b"a", "player": True, "control": 0, "laps": 3, "dist": 10.0},
         {"id": 1, "name": b"rival", "driver": b"r", "place": 2, "laps": 3}]


def make_cfg(tmp_path, raw="", record=""):
    return Config("ws://x", "", "driver-pc", "driver", str(tmp_path / "spool"), record, 5, 2, raw)


def drive(agent, s, t):
    """A short session: a green lap, then a lap completed during a full course yellow."""
    agent.poll_scoring()
    agent.poll_telemetry()
    s.update(fakes.scoring([{**FIELD[0], "laps": 4, "last": 95.0}, FIELD[1]], et=195.0))
    t.update(fakes.telemetry([{"id": 0, "fuel": 57.0, "et": 195.0}]))
    agent.poll_scoring()
    agent.poll_telemetry()
    s.update(fakes.scoring([{**FIELD[0], "laps": 5, "last": 99.0}, FIELD[1]], et=300.0, phase=6))
    agent.poll_scoring()


def test_replay_of_a_raw_capture_gives_the_same_messages(tmp_path):
    raw, live_rec, replay_rec = tmp_path / "raw.ndjson.gz", tmp_path / "live.ndjson", tmp_path / "replay.ndjson"
    s, t = fakes.FakeBuffer(fakes.scoring(FIELD)), fakes.FakeBuffer(fakes.telemetry([{"id": 0, "fuel": 60.0}]))
    agent = Agent(make_cfg(tmp_path, str(raw), str(live_rec)), s, t, link_factory=fakes.FakeLink)
    drive(agent, s, t)
    agent.close()

    assert [k for k, _ in read_frames(str(raw))] == ["s", "t", "s", "t", "s"]
    counts = replay(str(raw), make_cfg(tmp_path / "other"), str(replay_rec))
    assert counts == {"s": 3, "t": 2}

    def laps(path):
        msgs = [json.loads(l) for l in path.read_text().splitlines()]
        return [(m["payload"]["lap"], m["payload"]["fcy"]) for m in msgs if m["type"] == "lap"]

    assert laps(live_rec) == laps(replay_rec) == [(4, False), (5, True)]


def test_a_truncated_capture_is_read_up_to_the_cut(tmp_path):
    raw = tmp_path / "raw.ndjson.gz"
    s, t = fakes.FakeBuffer(fakes.scoring(FIELD)), fakes.FakeBuffer(fakes.telemetry([{"id": 0, "fuel": 60.0}]))
    agent = Agent(make_cfg(tmp_path, str(raw)), s, t, link_factory=fakes.FakeLink)
    drive(agent, s, t)
    agent.close()
    data = raw.read_bytes()
    raw.write_bytes(data[:-10])  # cut off the gzip trailer and part of the data
    frames = [k for k, _ in read_frames(str(raw))]
    assert frames and len(frames) <= 5
