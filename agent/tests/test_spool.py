import json

from splash_agent.spool import Spool


def spool(tmp_path):
    return Spool(tmp_path, "driver-pc", "driver")


def test_durable_messages_are_numbered_from_one(tmp_path):
    s = spool(tmp_path)
    s.open("sess")
    assert s.append("lap", 10.0, {"x": 1})[0] == 1
    assert s.append("event", 11.0, {"x": 2})[0] == 2
    assert [e[0] for e in s.entries] == [1, 2]


def test_live_samples_carry_the_last_durable_seq(tmp_path):
    s = spool(tmp_path)
    s.open("sess")
    assert json.loads(s.live("telemetry", 1.0, {}))["seq"] == 0
    s.append("lap", 10.0, {})
    live = json.loads(s.live("scoring", 11.0, {}))
    assert live["seq"] == 1 and s.last_seq == 1  # not consumed


def test_hello_has_seq_zero_and_resume_point(tmp_path):
    s = spool(tmp_path)
    s.open("sess")
    assert "resume_from_seq" not in json.loads(s.hello("1.0"))["payload"]
    s.append("lap", 1.0, {})
    s.append("lap", 2.0, {})
    s.prune(1)
    h = json.loads(s.hello("1.0"))
    assert h["seq"] == 0 and h["payload"]["resume_from_seq"] == 2


def test_reopen_continues_the_numbering(tmp_path):
    s = spool(tmp_path)
    s.open("sess")
    s.append("lap", 1.0, {})
    s.append("lap", 2.0, {})
    s.close()
    s2 = spool(tmp_path)
    s2.open("sess")
    assert [e[0] for e in s2.entries] == [1, 2]
    assert s2.append("lap", 3.0, {})[0] == 3


def test_sessions_do_not_mix(tmp_path):
    s = spool(tmp_path)
    s.open("one")
    s.append("lap", 1.0, {})
    s.open("two")
    assert s.entries == [] and s.last_seq == 0


def test_torn_last_line_is_skipped(tmp_path):
    s = spool(tmp_path)
    s.open("sess")
    s.append("lap", 1.0, {})
    s.close()
    path = next(tmp_path.iterdir())
    with path.open("a") as f:
        f.write('{"v":1,"type":"la')  # crash while writing
    s2 = spool(tmp_path)
    s2.open("sess")
    assert [e[0] for e in s2.entries] == [1]


def test_session_id_with_odd_characters_makes_a_safe_file_name(tmp_path):
    s = spool(tmp_path)
    s.open("../../etc/passwd")
    s.append("lap", 1.0, {})
    assert [p.parent for p in tmp_path.rglob("*.ndjson")] == [tmp_path]
