import argparse
import asyncio
import json
import logging

import pytest
from websockets.asyncio.server import serve

from splash_agent import logsetup, main as agentmain
from splash_agent.config import ConfigError, load
from splash_agent.link import Link
from splash_agent.logsetup import parse_level, scrub, setup

from . import fakes
from .test_agent import make_agent
from .test_config import VALID, write
from .test_link import Backend, make_spool, until

SPLASH_TOKEN = "splash-token-0123456789"
CF_SECRET = "cf-secret-value-abcdef"
CF_ID = "cf-client-id.access"


@pytest.fixture(autouse=True)
def clean_logging():
    saved = (list(logging.getLogger().handlers), logging.getLogger().level, logging.getLogger("websockets").level,
             list(logsetup._secrets))
    yield
    root = logging.getLogger()
    for h in list(root.handlers):
        root.removeHandler(h)
    for h in saved[0]:
        root.addHandler(h)
    root.setLevel(saved[1])
    logging.getLogger("websockets").setLevel(saved[2])
    logsetup._secrets[:] = saved[3]


# -- level -----------------------------------------------------------------


def test_level_names():
    assert parse_level("debug") == logging.DEBUG and parse_level(" Warning ") == logging.WARNING
    assert parse_level("WARN") == logging.WARNING and parse_level("ERROR") == logging.ERROR
    with pytest.raises(ValueError):
        parse_level("loud")


def test_config_key_is_read_and_checked(tmp_path):
    assert load(write(tmp_path, VALID)).log_level == "INFO"
    assert load(write(tmp_path, VALID + '[log]\nlevel = "debug"\n')).log_level == "debug"
    with pytest.raises(ConfigError, match="log.level"):
        load(write(tmp_path, VALID + '[log]\nlevel = "loud"\n'))


def test_command_line_beats_config_and_default(tmp_path):
    path = write(tmp_path, VALID + '[log]\nlevel = "warning"\n')
    level, cfg = agentmain._levels(argparse.Namespace(config=str(path)))
    assert level == logging.WARNING and cfg.log_level == "warning"
    level, _ = agentmain._levels(argparse.Namespace(config=str(path), log_level="debug"))
    assert level == logging.DEBUG
    level, cfg = agentmain._levels(argparse.Namespace())  # a command without a config file
    assert level == logging.INFO and cfg is None
    level, _ = agentmain._levels(argparse.Namespace(config=str(tmp_path / "missing.toml")))
    assert level == logging.INFO  # the command itself reports the missing file


def test_cli_accepts_the_option_before_and_after_the_command(tmp_path, monkeypatch, capsys):
    path = write(tmp_path, VALID)
    seen = []
    monkeypatch.setattr(agentmain, "_open_buffers", lambda: seen.append(logging.getLogger().level) or (_ for _ in ()).throw(SystemExit(0)))
    for argv in (["--log-level", "debug", "run", "--config", str(path)], ["run", "--log-level", "debug", "--config", str(path)]):
        with pytest.raises(SystemExit):
            agentmain.cli(argv)
    assert seen == [logging.DEBUG, logging.DEBUG]
    assert agentmain.cli(["run", "--log-level", "loud", "--config", str(path)]) == 2


def test_the_level_decides_what_is_written(capsys):
    setup(logging.WARNING)
    logging.getLogger("splash_agent").info("an info line")
    logging.getLogger("splash_agent").warning("a warning line")
    err = capsys.readouterr().err
    assert "a warning line" in err and "an info line" not in err


# -- start line ------------------------------------------------------------


def test_start_line_has_a_summary_without_secrets(tmp_path, monkeypatch, capsys):
    path = write(tmp_path, f'''[server]
url = "wss://user:urlpassword@splash.example.com:8443/v1/agent/ws?key=querysecret"
token = "{SPLASH_TOKEN}"
cf_client_id = "{CF_ID}"
cf_client_secret = "{CF_SECRET}"
[agent]
id = "driver-pc"
role = "driver"
[log]
level = "DEBUG"
''')
    monkeypatch.setattr(agentmain, "_open_buffers", lambda: (_ for _ in ()).throw(SystemExit(0)))
    with pytest.raises(SystemExit):
        agentmain.cli(["run", "--config", str(path)])
    err = capsys.readouterr().err
    assert "starting: server=wss://splash.example.com:8443 agent=driver-pc role=driver cloudflare_client_id=set log_level=DEBUG" in err
    for secret in (SPLASH_TOKEN, CF_SECRET, "urlpassword", "querysecret", "/v1/agent/ws"):
        assert secret not in err


def test_server_summary():
    assert agentmain._server_summary("ws://localhost:8080/v1/agent/ws") == "ws://localhost:8080"
    assert agentmain._server_summary("wss://u:p@host.example.com/x") == "wss://host.example.com"


# -- secrets ---------------------------------------------------------------


def test_scrub_removes_registered_secrets_and_lookalikes():
    logsetup.register_secrets(SPLASH_TOKEN, CF_SECRET)
    text = scrub(f"token {SPLASH_TOKEN} and {CF_SECRET}; Authorization: Bearer abc.def; CF-Access-Client-Secret: xyz; "
                 "eyJhbGciOiJSUzI1NiJ9.eyJhIjoxfQ.c2ln; postgres://u:pw@host/db")
    for gone in (SPLASH_TOKEN, CF_SECRET, "abc.def", "xyz", "c2ln", ":pw@"):
        assert gone not in text


def test_a_secret_in_a_message_or_a_traceback_never_reaches_the_console(capsys):
    logsetup.register_secrets(SPLASH_TOKEN)
    setup(logging.DEBUG)
    log = logging.getLogger("splash_agent")
    log.info("connecting with %s", SPLASH_TOKEN)
    try:
        raise RuntimeError(f"failed with {SPLASH_TOKEN}")
    except RuntimeError:
        log.error("boom", exc_info=True)
    assert SPLASH_TOKEN not in capsys.readouterr().err


# -- the websockets library -------------------------------------------------


def _connect_with_secrets(tmp_path):
    backend = Backend()

    async def scenario(link):
        await until(lambda: link.connected)

    async def main():
        async with serve(backend.handler, "127.0.0.1", 0) as server:
            port = server.sockets[0].getsockname()[1]
            link = Link(f"ws://127.0.0.1:{port}", SPLASH_TOKEN, make_spool(tmp_path), CF_ID, CF_SECRET)
            task = asyncio.create_task(link.run())
            try:
                await scenario(link)
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)

    asyncio.run(main())


def test_the_websockets_handshake_headers_never_reach_the_console(tmp_path, capsys):
    logsetup.register_secrets(SPLASH_TOKEN, CF_SECRET)
    setup(logging.DEBUG)
    # somebody tries to turn the library's debug output on, which prints every header
    logging.getLogger("websockets").setLevel(logging.DEBUG)
    logging.getLogger("websockets.client").setLevel(logging.DEBUG)
    logging.getLogger("websockets.server").setLevel(logging.DEBUG)
    _connect_with_secrets(tmp_path)
    err = capsys.readouterr().err
    for secret in (SPLASH_TOKEN, CF_SECRET, CF_ID, "Authorization", "CF-Access-Client", "Sec-WebSocket-Key"):
        assert secret not in err, secret
    assert "handshake ok" in err  # the agent's own DEBUG line is there: the test is not passing on silence


def test_without_the_guard_the_library_would_have_printed_them(tmp_path):
    """Why the guard exists: a plain DEBUG setup does print the secret."""
    records = []

    class Collect(logging.Handler):
        def emit(self, record):
            records.append(record.getMessage())

    root = logging.getLogger()
    root.addHandler(Collect())
    root.setLevel(logging.DEBUG)
    logging.getLogger("websockets").setLevel(logging.DEBUG)
    _connect_with_secrets(tmp_path)
    assert any(CF_SECRET in r or SPLASH_TOKEN in r for r in records)


# -- events ----------------------------------------------------------------


def test_reconnect_and_resend_lines_at_info(tmp_path, capsys, monkeypatch):
    from splash_agent import link as linkmod

    monkeypatch.setattr(linkmod, "MIN_BACKOFF", 0.02)
    setup(logging.INFO)
    from .test_link import DroppingBackend, _run_with_delays

    backend = DroppingBackend(drops=1)

    async def scenario(link):
        await until(lambda: backend.connections >= 2)
        await asyncio.sleep(0.05)

    _run_with_delays(backend, make_spool(tmp_path, 2), scenario)
    err = capsys.readouterr().err
    assert "connected, backend is at seq 0, 2 messages to resend" in err
    assert "connection closed; reconnecting in" in err or "connection lost" in err
    assert "attempt 1" in err


def test_backlog_progress_line(tmp_path, capsys):
    setup(logging.INFO)
    backend = Backend()

    async def scenario(link):
        await until(lambda: backend.last == 3)
        await until(lambda: not link.spool.entries)
        await asyncio.sleep(0.05)

    from .test_link import run_link

    run_link(backend, make_spool(tmp_path, 3), scenario=scenario)
    assert "backlog resent: 3 messages up to seq 3, all acknowledged" in capsys.readouterr().err


def test_every_message_type_sent_is_a_debug_line(tmp_path, capsys):
    setup(logging.DEBUG)
    backend = Backend()

    async def scenario(link):
        await until(lambda: link.connected)
        link.push_live("telemetry", link.spool.live("telemetry", 1.0, {"fuel": 50}))
        await until(lambda: any(m["type"] == "telemetry" for m in backend.received))

    from .test_link import run_link

    run_link(backend, make_spool(tmp_path), scenario=scenario)
    err = capsys.readouterr().err
    assert "DEBUG splash_agent.link: sent hello seq 0" in err and "sent telemetry seq 0" in err


def test_failed_reads_and_a_growing_spool_are_warnings(tmp_path, capsys, monkeypatch):
    monkeypatch.setattr(agentmain, "SPOOL_WARN", 3)
    setup(logging.INFO)
    agent, scoring, _ = make_agent(tmp_path, fakes.scoring([{"id": 0, "name": b"me", "driver": b"a", "player": True}]), fakes.telemetry([{"id": 0}]))
    agent.spool.open("s1")
    for i in range(3):
        agent.spool.append("lap", float(i), {"car_id": 0, "vehicle_name": "v", "lap": i + 1})
    agent._failed_reads = 7
    agent._status_checks(1000.0)
    err = capsys.readouterr().err
    assert "WARNING splash_agent: 7 snapshots of the plugin buffers could not be read consistently" in err
    assert "WARNING splash_agent: 3 messages are waiting for the backend" in err
    assert agent._failed_reads == 0
    agent.close()


def test_session_change_line(tmp_path, capsys):
    setup(logging.INFO)
    agent, _, _ = make_agent(tmp_path, fakes.scoring([{"id": 0, "name": b"me", "driver": b"a", "player": True, "laps": 1}]), fakes.telemetry([{"id": 0}]))
    agent.poll_scoring()
    err = capsys.readouterr().err
    assert "INFO splash_agent: session changed: " in err and "(was none)" in err
    agent.close()


def test_setup_keeps_the_library_logger_at_warning():
    logging.getLogger("websockets").setLevel(logging.DEBUG)
    setup(logging.DEBUG)
    assert logging.getLogger("websockets").level == logging.WARNING


def test_the_file_handler_hook_exists():
    assert logsetup.extra_handlers() == []
    extra = logging.NullHandler()
    setup(logging.INFO, handlers=[extra])
    assert extra in logging.getLogger().handlers
