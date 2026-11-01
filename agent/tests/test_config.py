import pytest

from splash_agent.config import ConfigError, load

VALID = """
[server]
url = "ws://host:8080/v1/agent/ws"
token = "t"
[agent]
id = "driver-pc"
"""


def write(tmp_path, text):
    p = tmp_path / "agent.toml"
    p.write_text(text)
    return p


def test_valid_with_defaults(tmp_path):
    c = load(write(tmp_path, VALID))
    assert (c.url, c.token, c.agent_id) == ("ws://host:8080/v1/agent/ws", "t", "driver-pc")
    assert (c.telemetry_hz, c.scoring_hz, c.spool_dir, c.record_file) == (5, 2, str(tmp_path / "spool"), "")


@pytest.mark.parametrize("text,msg", [
    (VALID.replace("ws://", "http://"), "ws://"),
    (VALID + "[poll]\ntelemetry_hz = 60\n", "telemetry_hz"),
    (VALID + "[poll]\nscoring_hz = 0\n", "scoring_hz"),
    ("not toml [", "agent.toml"),
])
def test_invalid(tmp_path, text, msg):
    with pytest.raises(ConfigError, match=msg):
        load(write(tmp_path, text))


def test_the_agent_id_defaults_to_the_name_of_the_pc(tmp_path, monkeypatch):
    monkeypatch.setattr("socket.gethostname", lambda: "sim-pc")
    assert load(write(tmp_path, VALID.replace('id = "driver-pc"', 'id = ""'))).agent_id == "sim-pc"
    assert load(write(tmp_path, VALID.replace('id = "driver-pc"', ""))).agent_id == "sim-pc"


def test_a_role_in_an_old_config_is_ignored(tmp_path):
    assert load(write(tmp_path, VALID + 'role = "spotter"\n')).agent_id == "driver-pc"


def test_relative_file_names_count_from_the_folder_of_the_configuration(tmp_path, monkeypatch):
    elsewhere = tmp_path / "elsewhere"
    elsewhere.mkdir()
    monkeypatch.chdir(elsewhere)  # Windows starts autostart entries in some other folder
    text = VALID + 'spool_dir = "data/spool"\nrecord_file = "rec.ndjson"\nraw_file = "' + str(tmp_path / "abs.gz").replace("\\", "/") + '"\n'
    c = load(write(tmp_path, text))
    assert c.spool_dir == str(tmp_path / "data" / "spool")
    assert c.record_file == str(tmp_path / "rec.ndjson")
    assert c.raw_file == str(tmp_path / "abs.gz")  # an absolute name is kept


def test_missing_file(tmp_path):
    with pytest.raises(ConfigError, match="not found"):
        load(tmp_path / "nope.toml")


def test_access_service_token_is_optional_and_kept_out_of_repr(tmp_path):
    c = load(write(tmp_path, VALID))
    assert (c.cf_client_id, c.cf_client_secret) == ("", "")
    c = load(write(tmp_path, VALID.replace('token = "t"', 'token = "t"\ncf_client_id = "id.access"\ncf_client_secret = "cf-secret-value"')))
    assert (c.cf_client_id, c.cf_client_secret) == ("id.access", "cf-secret-value")
    assert "cf-secret-value" not in repr(c) and "cf-secret-value" not in str(c)


@pytest.mark.parametrize("line", ['cf_client_id = "id.access"', 'cf_client_secret = "x"'])
def test_access_service_token_needs_both_values(tmp_path, line):
    with pytest.raises(ConfigError, match="together"):
        load(write(tmp_path, VALID.replace('token = "t"', 'token = "t"\n' + line)))
