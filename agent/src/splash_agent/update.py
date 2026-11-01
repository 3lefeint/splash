"""Updates the single-file Windows build from the GitHub releases of the agent.

Releases with a tag ``agent-vX.Y.Z`` carry ``splash-agent.exe`` and ``splash-agent.exe.sha256``. The newest one that is
newer than the running version is downloaded next to the program, checked against its checksum and swapped in: Windows
does not let a running program be overwritten, but it can be renamed, so the old file becomes ``splash-agent.exe.old``
and is removed at the next start. The swap and restart happen at a start of the agent, or later while the game is idle
(paused, in a menu or closed), never in the middle of a session.

The checksum comes from the same release as the program and so protects against a damaged download, not against a
release that was replaced by somebody else. Only an https address below the configured repository is used.
"""

import hashlib
import json
import logging
import os
import re
import subprocess
import sys
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Callable

log = logging.getLogger("splash_agent")

DEFAULT_REPO = "3lefeint/splash"
TAG_PREFIX = "agent-v"
EXE_NAME = "splash-agent.exe"
SUM_NAME = EXE_NAME + ".sha256"
RELEASES_URL = "https://api.github.com/repos/{repo}/releases?per_page=30"
HTTP_TIMEOUT = 15.0
MAX_EXE_BYTES = 200 * 1024 * 1024
MAX_JSON_BYTES = 4 * 1024 * 1024
IDLE_BEFORE_RESTART = 120.0  # seconds the game time must stand still before the agent restarts for an update
REPO_PATTERN = re.compile(r"^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$")

Fetch = Callable[[str, int], bytes]


def valid_repo(repo: str) -> bool:
    """``owner/name`` as GitHub spells it; no part may be only dots, so the name cannot climb out of /repos/."""
    return bool(REPO_PATTERN.match(repo)) and not any(set(part) == {"."} for part in repo.split("/"))


class UpdateError(Exception):
    pass


@dataclass(frozen=True)
class Release:
    version: tuple[int, ...]
    tag: str
    exe_url: str
    sum_url: str

    @property
    def text(self) -> str:
        return ".".join(str(n) for n in self.version)


def parse_version(text: str) -> tuple[int, ...] | None:
    """``0.2.1``, ``v0.2.1`` or ``agent-v0.2.1`` as numbers; None for anything else (``0.2.1-sim`` included)."""
    text = text.strip()
    for prefix in (TAG_PREFIX, "v"):
        if text.startswith(prefix):
            text = text[len(prefix):]
            break
    if not re.fullmatch(r"\d+(\.\d+)*", text):
        return None
    return tuple(int(p) for p in text.split("."))


def newer(a: tuple[int, ...], b: tuple[int, ...]) -> bool:
    """Whether version a is newer than b; missing parts count as 0 (1.0 equals 1.0.0)."""
    n = max(len(a), len(b))
    return a + (0,) * (n - len(a)) > b + (0,) * (n - len(b))


def pick(releases: list[dict], repo: str) -> Release | None:
    """The release with the highest version among the published ones of the agent series that carry both files."""
    base = f"https://github.com/{repo}/releases/download/"
    best: Release | None = None
    for r in releases:
        tag = str(r.get("tag_name", ""))
        if r.get("draft") or r.get("prerelease") or not tag.startswith(TAG_PREFIX):
            continue
        version = parse_version(tag)
        if version is None:
            continue
        urls = {a.get("name"): str(a.get("browser_download_url", "")) for a in r.get("assets", []) if isinstance(a, dict)}
        exe, digest = urls.get(EXE_NAME, ""), urls.get(SUM_NAME, "")
        if not (exe.startswith(f"{base}{tag}/") and digest.startswith(f"{base}{tag}/")):
            continue
        if best is None or newer(version, best.version):
            best = Release(version, tag, exe, digest)
    return best


def fetch(url: str, limit: int) -> bytes:
    """GETs an https address; refuses anything larger than limit bytes."""
    if not url.startswith("https://"):
        raise UpdateError("only https addresses are used")
    req = urllib.request.Request(url, headers={"User-Agent": "splash-agent", "Accept": "application/vnd.github+json"})
    with urllib.request.urlopen(req, timeout=HTTP_TIMEOUT) as resp:
        data = resp.read(limit + 1)
    if len(data) > limit:
        raise UpdateError(f"the download is larger than {limit} bytes")
    return data


def expected_hash(text: str) -> str:
    """The SHA-256 from the first line of a ``sha256sum`` file (``<hex>  <name>``, with an optional ``*`` before the name)."""
    first = text.strip().splitlines()[0].split() if text.strip() else []
    if not first or not re.fullmatch(r"[0-9a-fA-F]{64}", first[0]):
        raise UpdateError("the checksum file is not a SHA-256 checksum")
    return first[0].lower()


def download(release: Release, target: Path, fetch_fn: Fetch = fetch) -> None:
    """Fetches the program, checks it against its checksum and only then puts it at target."""
    want = expected_hash(fetch_fn(release.sum_url, 4096).decode("ascii", "replace"))
    data = fetch_fn(release.exe_url, MAX_EXE_BYTES)
    got = hashlib.sha256(data).hexdigest()
    if got != want:
        raise UpdateError(f"the download does not match its checksum ({got[:12]}... instead of {want[:12]}...)")
    part = target.with_name(target.name + ".part")
    part.write_bytes(data)
    os.replace(part, target)


def install(exe: Path, new: Path) -> None:
    """Swaps the new program in. The running file is renamed, not overwritten, which Windows allows."""
    old = exe.with_name(exe.name + ".old")
    try:
        old.unlink(missing_ok=True)
    except OSError:
        pass  # still in use by a process that has not ended yet; the rename below then fails and says so
    os.replace(exe, old)
    try:
        os.replace(new, exe)
    except OSError:
        os.replace(old, exe)  # put the program back; an agent without a program cannot start
        raise


def cleanup(exe: Path) -> None:
    """Removes what an earlier update left behind."""
    for suffix in (".old", ".part"):
        try:
            exe.with_name(exe.name + suffix).unlink(missing_ok=True)
        except OSError:
            pass


def supported() -> bool:
    """Only the single-file Windows build updates itself; a source install is updated with pip."""
    return bool(getattr(sys, "frozen", False)) and sys.platform == "win32"


class Updater:
    def __init__(self, repo: str, current: str, exe: Path | None = None, fetch_fn: Fetch = fetch):
        if not valid_repo(repo):
            raise ValueError(f"update repository must look like owner/name, got {repo!r}")
        self.repo, self.fetch_fn = repo, fetch_fn
        self.exe = exe or Path(sys.executable)
        self.current = parse_version(current)
        self.ready: Release | None = None  # downloaded and checked, waiting to be installed

    @property
    def staged(self) -> Path:
        return self.exe.with_name(self.exe.name + ".new")

    def check(self) -> Release | None:
        """The newest release if it is newer than the running version, else None."""
        if self.current is None:
            log.info("update check skipped: version of this build is not a release version")
            return None
        data = json.loads(self.fetch_fn(RELEASES_URL.format(repo=self.repo), MAX_JSON_BYTES))
        if not isinstance(data, list):
            raise UpdateError("unexpected answer from the release list")
        rel = pick(data, self.repo)
        return rel if rel is not None and newer(rel.version, self.current) else None

    def check_and_download(self) -> Release | None:
        """Looks for a newer release and stages it. Safe to call again: a release that is staged is not fetched twice."""
        rel = self.check()
        if rel is None:
            log.info("update check: no newer agent release than %s", ".".join(map(str, self.current or ())) or "this build")
            return None
        if self.ready is not None and self.ready.version == rel.version and self.staged.exists():
            return rel
        log.info("update check: agent %s is available, downloading", rel.text)
        download(rel, self.staged, self.fetch_fn)
        self.ready = rel
        log.info("agent %s downloaded and checked; it is installed when the game is idle or at the next start", rel.text)
        return rel

    def install_and_restart(self, argv: list[str]) -> None:
        """Swaps the staged program in and starts it with the same arguments. The caller ends the process afterwards."""
        if self.ready is None:
            raise UpdateError("no update is staged")
        install(self.exe, self.staged)
        log.info("agent %s installed, starting it", self.ready.text)
        env = dict(os.environ, PYINSTALLER_RESET_ENVIRONMENT="1")  # a new top-level instance, not a child of this one
        flags = getattr(subprocess, "CREATE_NEW_CONSOLE", 0)
        subprocess.Popen([str(self.exe), *argv], env=env, creationflags=flags, close_fds=True)

    def update_at_start(self, argv: list[str]) -> bool:
        """The check at program start. True when a new program was installed and started: the caller must exit."""
        cleanup(self.exe)
        try:
            if self.check_and_download() is None:
                return False
            self.install_and_restart(argv)
            return True
        except Exception as e:
            log.warning("update at start failed, going on with this version: %s", e)
            return False
