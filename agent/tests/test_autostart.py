import sys
from pathlib import Path

from splash_agent import autostart


class FakeRegistry:
    def __init__(self, **values):
        self.values = dict(values)

    def get(self, name):
        return self.values.get(name)

    def set(self, name, value):
        self.values[name] = value

    def delete(self, name):
        self.values.pop(name, None)


def answers(*texts):
    it = iter(texts)

    def ask(prompt):
        assert "automatically" in prompt
        return next(it)

    return ask


def offer(tmp_path, registry, ask, **kw):
    kw.setdefault("interactive", True)
    kw.setdefault("platform", "win32")
    autostart.offer(tmp_path / "spool", tmp_path / "agent.toml", registry=registry, ask=ask, **kw)


def test_the_command_runs_this_program_with_an_absolute_configuration(tmp_path, monkeypatch):
    monkeypatch.setattr(sys, "frozen", True, raising=False)
    monkeypatch.setattr(sys, "executable", "C:/Splash/splash-agent.exe")
    cmd = autostart.command_line(Path("agent.toml"))
    assert cmd.startswith('"C:/Splash/splash-agent.exe" run --config "')
    assert Path(cmd.split('--config "')[1].rstrip('"')).is_absolute()
    monkeypatch.setattr(sys, "frozen", False, raising=False)
    assert ' -m splash_agent run --config "' in autostart.command_line(Path("agent.toml"))


def test_yes_turns_it_on_and_is_remembered(tmp_path):
    reg = FakeRegistry()
    offer(tmp_path, reg, answers("y"))
    assert autostart.is_enabled(reg)
    assert str(tmp_path / "agent.toml") in reg.values[autostart.VALUE_NAME]
    assert autostart.asked(tmp_path / "spool")


def test_no_is_remembered_and_not_asked_again(tmp_path):
    reg = FakeRegistry()
    offer(tmp_path, reg, answers("n"))
    assert not autostart.is_enabled(reg) and autostart.asked(tmp_path / "spool")

    def never(prompt):
        raise AssertionError("asked again")

    offer(tmp_path, reg, never)
    assert not autostart.is_enabled(reg)


def test_anything_but_yes_means_no(tmp_path):
    reg = FakeRegistry()
    offer(tmp_path, reg, answers(""))
    assert not autostart.is_enabled(reg)


def test_no_question_without_a_person_to_answer(tmp_path):
    reg = FakeRegistry()

    def never(prompt):
        raise AssertionError("asked")

    offer(tmp_path, reg, never, interactive=False)
    assert not autostart.asked(tmp_path / "spool")


def test_an_unanswered_question_is_asked_next_time(tmp_path):
    def interrupted(prompt):
        raise KeyboardInterrupt

    offer(tmp_path, FakeRegistry(), interrupted)
    assert not autostart.asked(tmp_path / "spool")


def test_nothing_happens_outside_windows(tmp_path):
    reg = FakeRegistry()

    def never(prompt):
        raise AssertionError("asked")

    offer(tmp_path, reg, never, platform="linux")
    assert reg.values == {}


def test_an_entry_that_points_somewhere_else_is_corrected(tmp_path):
    reg = FakeRegistry(**{autostart.VALUE_NAME: '"C:/old/splash-agent.exe" run --config "C:/old/agent.toml"'})
    offer(tmp_path, reg, answers())
    assert str(tmp_path / "agent.toml") in reg.values[autostart.VALUE_NAME]


def test_a_failing_registry_does_not_stop_the_agent(tmp_path):
    class Broken(FakeRegistry):
        def get(self, name):
            raise OSError("access denied")

    offer(tmp_path, Broken(), answers("y"))  # must not raise


def test_off_removes_the_entry(tmp_path):
    reg = FakeRegistry()
    autostart.enable(reg, tmp_path / "agent.toml")
    autostart.disable(reg)
    autostart.disable(reg)  # twice is fine
    assert not autostart.is_enabled(reg)
