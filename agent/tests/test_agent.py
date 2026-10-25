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


def test_status_line_tells_what_the_agent_sees(tmp_path):
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([{"id": 0}]))
    assert "waiting for a session" in agent.status()
    agent.poll_scoring()
    agent.poll_telemetry()
    text = agent.status()
    assert "2 cars" in text and "scoring 1, telemetry 1" in text and "not connected" in text
    agent.close()


def test_restarted_race_on_the_same_track_gets_a_new_session_id(tmp_path):
    agent, s, _ = make_agent(tmp_path, fakes.scoring(FIELD, et=1500.0), fakes.telemetry([]))
    agent.poll_scoring()
    first = agent.spool.session_id
    s.update(fakes.scoring(FIELD, et=1520.0))
    agent.poll_scoring()
    assert agent.spool.session_id == first
    s.update(fakes.scoring(FIELD, et=3.0))
    agent.poll_scoring()
    second = agent.spool.session_id
    assert second == first + "-1"
    agent.close()
    # an agent restart in the middle of the new race keeps the id
    again, s2, _ = make_agent(tmp_path, fakes.scoring(FIELD, et=90.0), fakes.telemetry([]))
    again.poll_scoring()
    assert again.spool.session_id == second
    again.close()


def test_pit_menu_row_is_attached_to_the_telemetry(tmp_path):
    from splash_agent import structs as S

    agent, s, t = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([{"id": 0, "fuel": 61.0}]))
    info = S.PitInfoBuffer()
    info.mVersionUpdateBegin = info.mVersionUpdateEnd = 2
    info.mPitMenu.mCategoryName, info.mPitMenu.mChoiceString = b"FUEL:", b"60L"
    info.mPitMenu.mChoiceIndex, info.mPitMenu.mNumChoices = 59, 120
    agent.pit_info = fakes.FakeBuffer(info)
    agent.poll_scoring()
    agent.poll_telemetry()
    sample = json.loads(agent.link.live["telemetry"])
    validate(agent.link.live["telemetry"])
    assert sample["payload"]["pit_menu"] == {"category": "FUEL:", "choice": "60L", "choice_index": 59, "choices": 120}
    # a menu the game does not show is left out
    agent.pit_info.update(S.PitInfoBuffer())
    agent.poll_telemetry()
    assert "pit_menu" not in json.loads(agent.link.live["telemetry"])["payload"]
    agent.close()


class MenuHW:
    """Stands in for the plugin: a control changes the pit menu row that the agent reads back."""

    def __init__(self, buffer):
        self.buffer, self.cat, self.choice = buffer, 0, 3
        self.cats = [("FUEL:", [f"+{n}/{n // 20}" for n in range(123)]), ("TIRES:", ["Soft", "Medium", "Hard"])]
        self.show()

    def show(self):
        from splash_agent import structs as S

        name, choices = self.cats[self.cat]
        info = S.PitInfoBuffer()
        info.mVersionUpdateBegin = info.mVersionUpdateEnd = 2
        info.mPitMenu.mCategoryIndex, info.mPitMenu.mCategoryName = self.cat, name.encode()
        info.mPitMenu.mChoiceIndex, info.mPitMenu.mChoiceString = self.choice, choices[self.choice].encode()
        info.mPitMenu.mNumChoices = len(choices)
        self.buffer.update(info)

    def send(self, name, down):
        if not down:
            return
        n = len(self.cats[self.cat][1])
        if name == "PitMenuDown":
            self.cat, self.choice = (self.cat + 1) % len(self.cats), 0
        elif name == "PitMenuIncrementValue":
            self.choice = min(self.choice + 1, n - 1)
        elif name == "PitMenuDecrementValue":
            self.choice = max(self.choice - 1, 0)
        self.show()


def test_command_sets_the_pit_menu_and_the_result_is_sent_back(tmp_path, monkeypatch):
    import asyncio

    from splash_agent import pitmenu, structs as S

    for name in ("HOLD", "GAP", "WAKE_HOLD"):
        monkeypatch.setattr(pitmenu, name, 0.0)
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([]))
    agent.pit_info = fakes.FakeBuffer(S.PitInfoBuffer())
    hw = MenuHW(agent.pit_info)
    agent._hw = hw
    agent.poll_scoring()
    cmd = {"id": "c1", "steps": [{"category": "FUEL:", "choice": "max"}, {"category": "TIRES:", "choice": "medium"}]}
    asyncio.run(agent._run_command(cmd))
    sent = json.loads(agent.link.live["command_result"])
    validate(agent.link.live["command_result"])
    assert sent["payload"]["ok"] and sent["payload"]["id"] == "c1"
    assert sent["payload"]["applied"] == [{"category": "FUEL:", "choice": "+122/6"},
                                          {"category": "TIRES:", "choice": "Medium"}]
    agent.close()


def test_command_without_plugin_buffers_answers_with_a_reason(tmp_path):
    import asyncio

    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([]))
    agent.poll_scoring()
    asyncio.run(agent._run_command({"id": "c2", "steps": [{"category": "FUEL:", "choice": "max"}]}))
    sent = json.loads(agent.link.live["command_result"])
    assert not sent["payload"]["ok"] and "pit menu buffer" in sent["payload"]["message"]
    agent.close()


def test_command_is_refused_while_the_game_is_paused(tmp_path):
    import asyncio
    import time as _time

    from splash_agent import structs as S

    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([]))
    agent.pit_info = fakes.FakeBuffer(S.PitInfoBuffer())
    agent.poll_scoring()
    agent._last_et_change = _time.monotonic() - 10
    asyncio.run(agent._run_command({"id": "c3", "steps": [{"category": "FUEL:", "choice": "max"}]}))
    sent = json.loads(agent.link.live["command_result"])
    assert not sent["payload"]["ok"] and "paused" in sent["payload"]["message"]
    agent.close()


def test_every_car_carries_its_fuel_and_tyre_wear_in_the_scoring_message(tmp_path):
    field = [{**FIELD[0]}, {**FIELD[1]}]
    tel = fakes.telemetry([{"id": 0, "fuel": 61.0, "cap": 120.0}, {"id": 1, "fuel": 40.5, "cap": 100.0, "wear": [0.8, 0.8, 0.6, 0.6]}])
    agent, _, _ = make_agent(tmp_path, fakes.scoring(field), tel)
    agent.poll_scoring()
    line = agent.link.live["scoring"]
    validate(line)
    cars = {v["car_id"]: v for v in json.loads(line)["payload"]["vehicles"]}
    assert cars[0]["fuel"] == 61.0 and cars[0]["fuel_capacity"] == 120.0
    assert cars[1]["fuel"] == 40.5 and cars[1]["wear"] == [0.8, 0.8, 0.6, 0.6]
    agent.close()


def test_cars_without_usable_telemetry_get_no_fuel_fields(tmp_path):
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([{"id": 0, "fuel": 10.0, "cap": 0.0}]))
    agent.poll_scoring()
    validate(agent.link.live["scoring"])
    assert "fuel" not in json.loads(agent.link.live["scoring"])["payload"]["vehicles"][0]
    agent.close()


class PitRequestHW:
    """The game side of the pit request: the control toggles the pit state of the player's car."""

    def __init__(self, buffer, field):
        self.buffer, self.field, self.requested = buffer, field, False

    def send(self, name, down):
        if name == "PitRequest" and down:
            self.requested = not self.requested
            field = [{**FIELD[0], "pit_state": 1 if self.requested else 0}, FIELD[1]]
            self.buffer.update(fakes.scoring(field))


def test_pit_request_toggles_and_reports_the_new_state(tmp_path, monkeypatch):
    import asyncio

    from splash_agent import pitmenu

    monkeypatch.setattr(pitmenu, "WAKE_HOLD", 0.0)
    agent, s, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([]))
    agent.poll_scoring()
    agent._hw = PitRequestHW(s, FIELD)
    asyncio.run(agent._run_command({"id": "p1", "kind": "pit_request"}))
    first = json.loads(agent.link.live["command_result"])["payload"]
    assert first["ok"] and first["message"] == "pit request on"
    validate(agent.link.live["command_result"])
    asyncio.run(agent._run_command({"id": "p2", "kind": "pit_request"}))
    assert json.loads(agent.link.live["command_result"])["payload"]["message"] == "pit request off"
    agent.close()


def test_pit_request_fails_when_the_game_ignores_it(tmp_path, monkeypatch):
    import asyncio

    from splash_agent import pitmenu

    monkeypatch.setattr(pitmenu, "WAKE_HOLD", 0.0)
    monkeypatch.setattr(pitmenu, "REACT_TIMEOUT", 0.1)
    agent, _, _ = make_agent(tmp_path, fakes.scoring(FIELD), fakes.telemetry([]))
    agent.poll_scoring()
    agent._hw = type("Dead", (), {"send": lambda self, name, down: None})()
    asyncio.run(agent._run_command({"id": "p3", "kind": "pit_request"}))
    r = json.loads(agent.link.live["command_result"])["payload"]
    assert not r["ok"] and "did not change" in r["message"]
    agent.close()


def test_damage_and_penalties_reach_the_messages(tmp_path):
    field = [{**FIELD[0], "penalties": 2}, FIELD[1]]
    tel = fakes.telemetry([{"id": 0, "fuel": 50.0}])
    tel.mVehicles[0].mDentSeverity[3] = 2
    tel.mVehicles[0].mDentSeverity[5] = 1
    tel.mVehicles[0].mOverheating = True
    agent, _, _ = make_agent(tmp_path, fakes.scoring(field), tel)
    agent.poll_scoring()
    agent.poll_telemetry()
    validate(agent.link.live["scoring"])
    validate(agent.link.live["telemetry"])
    assert json.loads(agent.link.live["scoring"])["payload"]["vehicles"][0]["penalties"] == 2
    t = json.loads(agent.link.live["telemetry"])["payload"]
    assert t["dent_max"] == 2 and t["overheating"] is True and "detached" not in t
    agent.close()
