import sys

import pytest

from splash_agent import main as agentmain


def test_a_missing_configuration_says_where_it_comes_from(tmp_path, monkeypatch, capsys):
    monkeypatch.chdir(tmp_path)
    assert agentmain.cli(["run"]) == 2
    err = capsys.readouterr().err
    assert "config file not found" in err
    assert "Download the configuration" in err and "agent.toml next to this program" in err


def test_a_broken_configuration_gets_no_download_hint(tmp_path, monkeypatch, capsys):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "agent.toml").write_text("not toml [")
    assert agentmain.cli(["run"]) == 2
    err = capsys.readouterr().err
    assert "config error" in err and "Download the configuration" not in err


def test_the_window_stays_open_for_a_person_only(monkeypatch):
    asked = []
    monkeypatch.setattr("builtins.input", lambda prompt: asked.append(prompt) or "")
    monkeypatch.setattr(sys, "stdin", type("Tty", (), {"isatty": lambda self: True})())
    agentmain._wait_for_enter()
    assert len(asked) == 1 and "Enter" in asked[0]
    monkeypatch.setattr(sys, "stdin", type("Pipe", (), {"isatty": lambda self: False})())
    agentmain._wait_for_enter()
    assert len(asked) == 1  # a script is never held up


def test_the_window_survives_a_closed_input(monkeypatch):
    def closed(prompt):
        raise EOFError

    monkeypatch.setattr("builtins.input", closed)
    monkeypatch.setattr(sys, "stdin", type("Tty", (), {"isatty": lambda self: True})())
    agentmain._wait_for_enter()  # must not raise


def test_the_configuration_is_found_in_the_working_folder_or_next_to_the_program(tmp_path, monkeypatch):
    program = tmp_path / "program"
    program.mkdir()
    work = tmp_path / "work"
    work.mkdir()
    monkeypatch.chdir(work)
    monkeypatch.setattr(sys, "frozen", True, raising=False)
    monkeypatch.setattr(sys, "executable", str(program / "splash-agent.exe"))
    assert agentmain.find_config("given.toml") == "given.toml"
    assert agentmain.find_config(None) == "agent.toml"  # nothing anywhere: the error then names the default
    (program / "agent.toml").write_text("")
    assert agentmain.find_config(None) == str(program / "agent.toml")
    (work / "agent.toml").write_text("")
    assert agentmain.find_config(None) == "agent.toml"  # the working folder wins
    monkeypatch.setattr(sys, "frozen", False, raising=False)
    (work / "agent.toml").unlink()
    assert agentmain.find_config(None) == "agent.toml"  # a source install does not look beside python
