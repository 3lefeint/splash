import sys

from splash_agent import main as agentmain


def _record_run(monkeypatch):
    called = []
    monkeypatch.setattr(agentmain, "cmd_run", lambda args: called.append(args) or 0)
    return called


def test_a_double_click_on_the_program_starts_the_agent(monkeypatch, tmp_path):
    called = _record_run(monkeypatch)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr(sys, "frozen", True, raising=False)
    monkeypatch.setattr(sys, "argv", ["splash-agent.exe"])
    assert agentmain.cli() == 0
    assert len(called) == 1 and called[0].cmd == "run"


def test_without_arguments_outside_the_build_the_usage_is_still_shown(monkeypatch, capsys):
    called = _record_run(monkeypatch)
    monkeypatch.delattr(sys, "frozen", raising=False)
    monkeypatch.setattr(sys, "argv", ["splash-agent"])
    try:
        agentmain.cli()
    except SystemExit as e:
        assert e.code == 2
    assert called == []
    assert "the following arguments are required" in capsys.readouterr().err


def test_explicit_arguments_are_not_changed(monkeypatch, tmp_path):
    called = _record_run(monkeypatch)
    monkeypatch.chdir(tmp_path)
    monkeypatch.setattr(sys, "frozen", True, raising=False)
    monkeypatch.setattr(sys, "argv", ["splash-agent.exe", "run", "--log-level", "DEBUG"])
    assert agentmain.cli() == 0
    assert called[0].cmd == "run"
