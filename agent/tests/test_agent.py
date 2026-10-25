import json
from pathlib import Path

import jsonschema
import pytest

from splash_agent.config import Config
from splash_agent.main import Agent

from . import fakes

SCHEMA = json.loads((Path(__file__).parents[2] / "schema" / "messages.schema.json").read_text())
VALIDATOR = jsonschema.Draft202012Validator(SCHEMA)


def validate(line):
    msg = json.loads(line)
    VALIDATOR.validate(msg)
    return msg


def make_agent(tmp_path, scoring, telemetry):
    cfg = Config(url="ws://x", token="", agent_id="driver-pc", role="driver", spool_dir=str(tmp_path / "spool"),
                 record_file="", telemetry_hz=5, scoring_hz=2)
    s, t = fakes.FakeBuffer(scoring), fakes.FakeBuffer(telemetry)
    agent = Agent(cfg, s, t, link_factory=fakes.FakeLink)
    return agent, s, t


FIELD = [{"id": 0, "name": b"me", "driver": b"a", "player": True, "control": 0, "laps": 3, "dist": 1234.5},
         {"id": 1, "name": b"rival", "driver": b"r", "place": 2, "laps": 3}]


def test_scoring_and_telemetry_messages_match_the_schema(tmp_path):
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([{"id": 0, "fuel": 61.0}]))
    agent.poll_scoring()
    agent.poll_telemetry()
    live = agent.link.live
    sc, tel = validate(live["scoring"]), validate(live["telemetry"])
    assert sc["payload"]["vehicles"][1]["vehicle_name"] == "rival" and sc["seq"] == 0 and sc["et"] == 100.0
    assert tel["payload"]["car_id"] == 0 and tel["payload"]["fuel"] == 61.0 and tel["payload"]["lap_dist"] == 1234.5
    agent.close()


def test_telemetry_vehicle_name_comes_from_scoring(tmp_path):
    # the telemetry buffer may spell the name differently; the identity is the scoring name
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([{"id": 0, "name": b"Team Car #7"}]))
    agent.poll_scoring()
    agent.poll_telemetry()
    assert json.loads(agent.link.live["telemetry"])["payload"]["vehicle_name"] == "me"
    agent.close()


def test_no_telemetry_when_not_in_a_car(tmp_path):
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD, realtime=False), fakes.telemetry([{"id": 0}]))
    agent.poll_scoring()
    agent.poll_telemetry()
    assert "scoring" in agent.link.live and "telemetry" not in agent.link.live
    agent.close()


def test_laps_and_events_are_spooled_and_valid(tmp_path):
    agent, s, t = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([{"id": 0, "fuel": 61.0}]))
    agent.poll_scoring()
    agent.poll_telemetry()
    field = [{**FIELD[0], "laps": 4, "last": 98.4, "s1": 30.0, "s12": 62.0}, {**FIELD[1], "laps": 3}]
    s.update(fakes.scoring(field, et=200.0))
    t.update(fakes.telemetry([{"id": 0, "fuel": 58.0}]))
    agent.poll_telemetry()
    agent.poll_scoring()
    lines = [line for _, line in agent.spool.entries]
    assert len(lines) == 1
    lap = validate(lines[0])
    assert lap["type"] == "lap" and lap["seq"] == 1 and lap["payload"]["fuel_end"] == 58.0
    assert agent.link.notified >= 2
    # the scoring sample after the lap carries the durable seq 1
    assert json.loads(agent.link.live["scoring"])["seq"] == 1
    agent.close()


def test_pause_heartbeat_and_empty_field_are_ignored(tmp_path):
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD, phase=9), fakes.telemetry([]))
    agent.poll_scoring()
    assert agent.spool.session_id is None
    agent.scoring.update(fakes.scoring([]))
    agent.poll_scoring()
    assert agent.spool.session_id is None
    agent.close()


def test_new_session_starts_a_fresh_spool(tmp_path):
    agent, s, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([]))
    agent.poll_scoring()
    first = agent.spool.session_id
    s.update(fakes.scoring(FIELD, session=5))
    agent.poll_scoring()
    assert agent.spool.session_id != first and agent.spool.last_seq == 0
    agent.close()


def test_record_file_gets_every_message(tmp_path):
    rec = tmp_path / "rec.ndjson"
    cfg = Config("ws://x", "", "driver-pc", "driver", str(tmp_path / "spool"), str(rec), 5, 2)
    agent = Agent(cfg, fakes.FakeBuffer(fakes.scoring(FIELD)), fakes.FakeBuffer(fakes.telemetry([{"id": 0}])),
                  link_factory=fakes.FakeLink)
    agent.poll_scoring()
    agent.poll_telemetry()
    agent.close()
    lines = rec.read_text().splitlines()
    assert [validate(l)["type"] for l in lines] == ["scoring", "telemetry"]
