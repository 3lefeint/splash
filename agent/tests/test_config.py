import pytest

from splash_agent.config import ConfigError, load

VALID = """
[server]
url = "ws://host:8080/v1/agent/ws"
token = "t"
[agent]
id = "driver-pc"
role = "spotter"
"""


def write(tmp_path, text):
    p = tmp_path / "agent.toml"
    p.write_text(text)
    return p


def test_valid_with_defaults(tmp_path):
    c = load(write(tmp_path, VALID))
    assert (c.url, c.token, c.agent_id, c.role) == ("ws://host:8080/v1/agent/ws", "t", "driver-pc", "spotter")
    assert (c.telemetry_hz, c.scoring_hz, c.spool_dir, c.record_file) == (5, 2, "spool", "")


@pytest.mark.parametrize("text,msg", [
    (VALID.replace("ws://", "http://"), "ws://"),
    (VALID.replace('id = "driver-pc"', ""), "agent.id"),
    (VALID.replace("spotter", "pilot"), "role"),
    (VALID + "[poll]\ntelemetry_hz = 60\n", "telemetry_hz"),
    (VALID + "[poll]\nscoring_hz = 0\n", "scoring_hz"),
    ("not toml [", "agent.toml"),
])
def test_invalid(tmp_path, text, msg):
    with pytest.raises(ConfigError, match=msg):
        load(write(tmp_path, text))


def test_missing_file(tmp_path):
    with pytest.raises(ConfigError, match="not found"):
        load(tmp_path / "nope.toml")
