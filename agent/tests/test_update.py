import hashlib
import json
import time

import pytest

from splash_agent import update
from splash_agent.config import Config, ConfigError, load

REPO = "owner/splash"
BASE = f"https://github.com/{REPO}/releases/download"


def release(tag, **kw):
    r = {"tag_name": tag, "draft": False, "prerelease": False, "assets": [
        {"name": update.EXE_NAME, "browser_download_url": f"{BASE}/{tag}/{update.EXE_NAME}"},
        {"name": update.SUM_NAME, "browser_download_url": f"{BASE}/{tag}/{update.SUM_NAME}"},
    ]}
    r.update(kw)
    return r


def test_versions():
    p = update.parse_version
    assert p("0.2.1") == (0, 2, 1) and p("v1.0") == (1, 0) and p("agent-v0.10.0") == (0, 10, 0)
    assert p("0.2.1-sim") is None and p("dev") is None and p("") is None and p("1..2") is None
    assert update.newer((0, 10, 0), (0, 9, 0)) and not update.newer((1, 0), (1, 0, 0)) and update.newer((1, 0, 1), (1, 0))


def test_pick_takes_the_highest_published_release_of_the_agent_series():
    got = update.pick([
        release("agent-v0.9.0"), release("agent-v0.10.0"), release("agent-v0.11.0", draft=True),
        release("agent-v0.12.0", prerelease=True), release("v9.0.0"), release("agent-vx"),
    ], REPO)
    assert got.tag == "agent-v0.10.0" and got.version == (0, 10, 0)


def test_pick_ignores_releases_without_both_files_or_with_foreign_addresses():
    only_exe = release("agent-v1.0.0")
    only_exe["assets"] = only_exe["assets"][:1]
    foreign = release("agent-v2.0.0")
    foreign["assets"][0]["browser_download_url"] = "https://example.com/splash-agent.exe"
    plain = release("agent-v3.0.0")
    plain["assets"][1]["browser_download_url"] = plain["assets"][1]["browser_download_url"].replace("https://", "http://")
    other_repo = release("agent-v4.0.0")
    other_repo["assets"][0]["browser_download_url"] = other_repo["assets"][0]["browser_download_url"].replace(REPO, "evil/repo")
    ok = release("agent-v0.5.0")
    assert update.pick([only_exe, foreign, plain, other_repo, ok], REPO).tag == "agent-v0.5.0"
    assert update.pick([only_exe, foreign], REPO) is None
    assert update.pick([], REPO) is None


def test_checksum_files():
    digest = "a" * 64
    assert update.expected_hash(f"{digest}  splash-agent.exe\n") == digest
    assert update.expected_hash(f"{digest.upper()} *splash-agent.exe") == digest
    for bad in ("", "nothex  file", "abc  file"):
        with pytest.raises(update.UpdateError):
            update.expected_hash(bad)


class Server:
    """Answers the release list and the two files of a release."""

    def __init__(self, tag="agent-v0.3.0", payload=b"new program", digest=None):
        self.payload = payload
        self.digest = digest or hashlib.sha256(payload).hexdigest()
        self.tag = tag
        self.calls = []

    def __call__(self, url, limit):
        self.calls.append(url)
        if url.startswith("https://api.github.com/"):
            return json.dumps([release(self.tag)]).encode()
        if url.endswith(update.SUM_NAME):
            return f"{self.digest}  {update.EXE_NAME}\n".encode()
        return self.payload


def make(tmp_path, server, current="0.2.0"):
    exe = tmp_path / "splash-agent.exe"
    exe.write_bytes(b"old program")
    return update.Updater(REPO, current, exe=exe, fetch_fn=server)


def test_a_newer_release_is_downloaded_checked_and_staged(tmp_path):
    srv = Server()
    u = make(tmp_path, srv)
    rel = u.check_and_download()
    assert rel.text == "0.3.0" and u.ready == rel
    assert u.staged.read_bytes() == b"new program"
    assert (tmp_path / "splash-agent.exe").read_bytes() == b"old program"  # nothing installed yet
    n = len(srv.calls)
    u.check_and_download()  # a staged release is not fetched twice
    assert [c for c in srv.calls[n:] if "download" in c] == []


@pytest.mark.parametrize("current", ["0.3.0", "0.4.0", "0.2.0-sim", "dev"])
def test_nothing_to_do_when_this_version_is_current_or_unknown(tmp_path, current):
    u = make(tmp_path, Server(), current=current)
    assert u.check_and_download() is None and u.ready is None and not u.staged.exists()


def test_a_download_that_does_not_match_its_checksum_is_refused(tmp_path):
    u = make(tmp_path, Server(digest="0" * 64))
    with pytest.raises(update.UpdateError, match="checksum"):
        u.check_and_download()
    assert not u.staged.exists() and not list(tmp_path.glob("*.part")) and u.ready is None


def test_the_swap_keeps_the_old_program_until_the_next_start(tmp_path):
    exe, new = tmp_path / "splash-agent.exe", tmp_path / "splash-agent.exe.new"
    exe.write_bytes(b"old")
    new.write_bytes(b"new")
    update.install(exe, new)
    assert exe.read_bytes() == b"new" and (tmp_path / "splash-agent.exe.old").read_bytes() == b"old" and not new.exists()
    update.cleanup(exe)
    assert not (tmp_path / "splash-agent.exe.old").exists()


def test_a_failed_swap_puts_the_old_program_back(tmp_path):
    exe = tmp_path / "splash-agent.exe"
    exe.write_bytes(b"old")
    with pytest.raises(OSError):
        update.install(exe, tmp_path / "missing.new")
    assert exe.read_bytes() == b"old"


def test_install_and_restart_starts_the_new_program_as_a_fresh_instance(tmp_path, monkeypatch):
    u = make(tmp_path, Server())
    u.check_and_download()
    started = {}

    def popen(cmd, **kw):
        started.update(cmd=cmd, **kw)

    monkeypatch.setattr(update.subprocess, "Popen", popen)
    u.install_and_restart(["run", "--config", "agent.toml"])
    assert started["cmd"] == [str(tmp_path / "splash-agent.exe"), "run", "--config", "agent.toml"]
    assert started["env"]["PYINSTALLER_RESET_ENVIRONMENT"] == "1"
    assert (tmp_path / "splash-agent.exe").read_bytes() == b"new program"


def test_update_at_start_installs_and_asks_the_caller_to_exit(tmp_path, monkeypatch):
    u = make(tmp_path, Server())
    monkeypatch.setattr(update.subprocess, "Popen", lambda *a, **k: None)
    assert u.update_at_start(["run"]) is True
    assert make(tmp_path, Server(), current="0.3.0").update_at_start(["run"]) is False


def test_a_failing_update_at_start_never_stops_the_agent(tmp_path):
    def broken(url, limit):
        raise OSError("no network")

    assert make(tmp_path, broken).update_at_start(["run"]) is False


def test_only_https_is_fetched():
    with pytest.raises(update.UpdateError):
        update.fetch("http://github.com/x", 10)
    with pytest.raises(update.UpdateError):
        update.fetch("file:///etc/passwd", 10)


def test_the_repository_must_look_like_owner_and_name(tmp_path):
    for bad in ("", "owner", "a/b/c", "a b/c", "../x"):
        with pytest.raises(ValueError):
            update.Updater(bad, "0.1.0")


def test_this_build_does_not_update_itself_unless_it_is_the_windows_program():
    assert update.supported() is False  # tests run from source


CFG = """
[server]
url = "ws://host/v1/agent/ws"
token = "t"
"""


def test_update_settings(tmp_path):
    p = tmp_path / "agent.toml"
    p.write_text(CFG)
    c = load(p)
    assert (c.update_enabled, c.update_repo, c.update_check_hours) == (True, "3lefeint/splash", 6.0)
    p.write_text(CFG + '[update]\nenabled = false\nrepo = "a/b"\ncheck_hours = 12\n')
    c = load(p)
    assert (c.update_enabled, c.update_repo, c.update_check_hours) == (False, "a/b", 12.0)
    for bad in ('repo = "nope"', "check_hours = 0", "check_hours = 500"):
        p.write_text(CFG + "[update]\n" + bad + "\n")
        with pytest.raises(ConfigError):
            load(p)


