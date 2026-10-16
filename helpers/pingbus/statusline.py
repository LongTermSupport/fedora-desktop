"""`pingbus status-line`: the session's status line, with the agent-bus teams it is on.

Plan 00201 S-C6. Claude Code draws a status line by running ONE command: the `statusLine` of the
highest-precedence settings that set one. The member kit's settings.json, which every bus launch
passes with `--settings` (ccy's entrypoint, `cc --teams`, agent-bus-claude), names this command,
and command-line settings outrank the project's and the user's. So it works in any checkout,
with or without a hooks daemon: it runs the status line the session would have had without the
kit (the first `statusLine` command in the project's .claude/settings.local.json, then its
.claude/settings.json, then the user's settings.json) with the same stdin, prints that, and adds
`bus: A,B` from the record the SessionStart hook wrote under the session id
(`hooks.record_session_teams`, which writes where `sessions_dir` says).

A bus session (PINGBUS_TEAMS set) with no usable record shows `bus: no record ...` in red, never
nothing, so a record that failed cannot pass for a session on no team. The status line runs as a
child of this session's claude, so PINGBUS_TEAMS here is this session's own.

It always exits 0: Claude Code blanks the whole line on a non-zero exit, which would hide the
segment and the failure alike, so a failing, hung or unreadable inner status line is shown in red
instead. The inner command runs in its own process group, killed on a timeout and when this
process is cancelled (Claude Code cancels an in-flight render when a new one starts), so nothing
it started outlives the render.

It renders on every refresh, so it imports no other pingbus module (the zipapp's __main__ runs it
without the cli); the few names it shares with hooks.py and protocol.py are copies a test keeps
equal.
"""

from __future__ import annotations

import json
import os
import pathlib
import re
import signal
import stat
import subprocess
import sys
import time
from collections.abc import Callable, Mapping

COMMAND = "pingbus status-line"
#: Set for the inner status line, so a settings file that names this command cannot loop.
INNER_VAR = "PINGBUS_STATUS_LINE_INNER"
SESSIONS_DIR_VAR = "PINGBUS_SESSIONS_DIR"
SESSIONS_DIR_TEMPLATE = "/tmp/pingbus-sessions-{uid}"
SESSION_ID_RE = re.compile(r"[A-Za-z0-9_-]{1,128}")
TEAM_NAME_RE = re.compile(r"[a-z][a-z0-9-]{0,23}")
INPUT_MAX_BYTES = 1 << 20
MAX_RECORD_BYTES = 4096
INNER_TIMEOUT_S = 10.0
#: `run_command`'s status for an inner command that was killed at its timeout.
TIMED_OUT = -1
CYAN, RED, RESET = "\033[36m", "\033[31m", "\033[0m"
NO_RECORD = "bus: no record of this session's teams (pingbus status)"
#: Plan 00203 H14: the membership files this module reads its copy of the active teams from.
MEMBERSHIP_VAR = "PINGBUS_MEMBERSHIP"
MEMBERSHIP_OFF = "off"
DESIRED_FILE = "membership.json"
APPLIED_FILE = "applied.json"
MAX_MEMBERSHIP_BYTES = 65536
NO_MEMBERSHIP = "bus: this session's membership is unreadable (pingbus status)"
#: Plan 00203 H26: `PINGBUS_HOME/down/<team>` is there while the watcher retries a team whose
#: homeserver does not answer; each retry refreshes it (its backoff caps at 30 s), so a mark a
#: killed watcher left stops counting after DOWN_FRESH_S.
DOWN_DIR = "down"
DOWN_FRESH_S = 300.0
CANCEL_SIGNALS = (signal.SIGTERM, signal.SIGINT, signal.SIGHUP)

Runner = Callable[[str, bytes, Mapping[str, str], "pathlib.Path | None"], tuple[str, int]]

#: The inner command's process while it runs, for the cancel handler.
_running: subprocess.Popen | None = None


class SettingsError(Exception):
    """A settings file that exists but cannot be read as a JSON object."""


def sessions_dir(environ: Mapping[str, str]) -> pathlib.Path:
    return pathlib.Path(environ.get(SESSIONS_DIR_VAR) or SESSIONS_DIR_TEMPLATE.format(uid=os.getuid()))


def _session_id(payload: dict) -> str | None:
    value = payload.get("session_id")
    return value if isinstance(value, str) and SESSION_ID_RE.fullmatch(value) else None


def session_teams(environ: Mapping[str, str], session_id: str | None) -> list[str] | None:
    """The teams recorded for this session, or None when there is no usable record: no id, no
    file, a file that is not this user's alone, or contents that are not distinct team names."""
    if session_id is None:
        return None
    path = sessions_dir(environ) / session_id
    try:
        found = os.lstat(path)
        if not stat.S_ISREG(found.st_mode) or found.st_uid != os.getuid() or found.st_mode & 0o077:
            return None
        if found.st_size > MAX_RECORD_BYTES:
            return None
        teams = path.read_text(encoding="ascii").split(",")
    except (OSError, UnicodeDecodeError):
        return None
    if len(set(teams)) != len(teams) or not all(TEAM_NAME_RE.fullmatch(team) for team in teams):
        return None
    return teams


def _payload(raw: bytes) -> dict:
    if len(raw) > INPUT_MAX_BYTES:
        return {}
    try:
        data = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return {}
    return data if isinstance(data, dict) else {}


def _project_dir(payload: dict, environ: Mapping[str, str]) -> pathlib.Path | None:
    workspace = payload.get("workspace")
    candidate = workspace.get("project_dir") if isinstance(workspace, dict) else None
    for value in (candidate, environ.get("CLAUDE_PROJECT_DIR"), payload.get("cwd")):
        if isinstance(value, str) and value.startswith("/"):
            return pathlib.Path(value)
    return None


def _settings_files(project: pathlib.Path | None, environ: Mapping[str, str]) -> list[pathlib.Path]:
    files = [] if project is None else [project / ".claude/settings.local.json",
                                        project / ".claude/settings.json"]
    user = environ.get("CLAUDE_CONFIG_DIR") or (
        f"{environ['HOME']}/.claude" if environ.get("HOME", "").startswith("/") else None)
    if user is not None:
        files.append(pathlib.Path(user) / "settings.json")
    return files


def inner_command(project: pathlib.Path | None, environ: Mapping[str, str]) -> str | None:
    """The status line command the session would run without the kit, or None. This command
    itself is passed over. SettingsError for a file that exists but is not a JSON object."""
    for path in _settings_files(project, environ):
        try:
            text = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            continue
        except (OSError, UnicodeDecodeError) as exc:
            raise SettingsError(f"{path} cannot be read ({type(exc).__name__})") from exc
        try:
            settings = json.loads(text)
        except ValueError as exc:
            raise SettingsError(f"{path} is not valid JSON") from exc
        line = settings.get("statusLine") if isinstance(settings, dict) else None
        command = line.get("command") if isinstance(line, dict) else None
        if isinstance(command, str) and command.strip() and command.strip() != COMMAND:
            return command
    return None


def _kill_group(proc: subprocess.Popen) -> None:
    try:
        os.killpg(proc.pid, signal.SIGKILL)
    except ProcessLookupError:
        return


def run_command(command: str, raw: bytes, environ: Mapping[str, str], cwd: pathlib.Path | None,
                *, timeout: float = INNER_TIMEOUT_S) -> tuple[str, int]:
    """Run a status line command as Claude Code does: through the shell, the payload on stdin,
    stderr left to the caller's. In its own process group, killed whole at `timeout` (status
    TIMED_OUT) or when this process is cancelled (`main`)."""
    global _running
    env = dict(environ, **{INNER_VAR: "1"})
    directory = cwd if cwd is not None and cwd.is_dir() else None
    proc = subprocess.Popen(["/bin/sh", "-c", command], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                            env=env, cwd=directory, start_new_session=True)
    _running = proc
    try:
        out, _ = proc.communicate(raw, timeout=timeout)
    except subprocess.TimeoutExpired:
        _kill_group(proc)
        proc.communicate()
        return "", TIMED_OUT
    finally:
        _running = None
    return out.decode("utf-8", errors="replace"), proc.returncode


def _read_object(path: pathlib.Path) -> dict | None:
    """A JSON object from a regular file never followed through a symlink; None otherwise."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except OSError:
        return None
    with os.fdopen(fd, "rb") as handle:
        if not stat.S_ISREG(os.fstat(handle.fileno()).st_mode):
            return None
        raw = handle.read(MAX_MEMBERSHIP_BYTES + 1)
    try:
        data = json.loads(raw.decode("utf-8")) if len(raw) <= MAX_MEMBERSHIP_BYTES else None
    except (UnicodeDecodeError, ValueError):
        return None
    return data if isinstance(data, dict) else None


def _pairs(data: dict | None, held_only: bool) -> list[tuple[str, str]] | None:
    seats = None if data is None or data.get("v") != 1 else data.get("seats")
    if not isinstance(seats, list):
        return None
    pairs = []
    for seat in seats:
        if not isinstance(seat, dict) or not isinstance(seat.get("team"), str) or not isinstance(seat.get("seat"), str):
            return None
        if not TEAM_NAME_RE.fullmatch(seat["team"]):
            return None
        if not held_only or seat.get("state") == "held":
            pairs.append((seat["team"], seat["seat"]))
    return pairs


def membership_teams(environ: Mapping[str, str]) -> list[str] | None:
    """Plan 00203 H14, this module's copy of `config.active_teams` under PINGBUS_MEMBERSHIP:
    the seats held in the session home's applied file that the desired file names, in desired
    order. None when a file cannot be read; [] before the keeper wrote the applied file."""
    desired = _pairs(_read_object(pathlib.Path(environ[MEMBERSHIP_VAR]) / DESIRED_FILE), held_only=False)
    home = environ.get("PINGBUS_HOME", "")
    if desired is None or not home.startswith("/"):
        return None
    applied_path = pathlib.Path(home) / APPLIED_FILE
    if not os.path.lexists(applied_path):
        return []
    held = _pairs(_read_object(applied_path), held_only=True)
    if held is None:
        return None
    return [team for team, seat in desired if (team, seat) in set(held)]


def set_team_down(home: pathlib.Path, team: str, down: bool) -> None:
    """The watcher's H26 mark for `team`: created or refreshed when `down`, removed otherwise.
    The directory is made mode 0700 and the mark 0600, never followed through a symlink."""
    path = pathlib.Path(home) / DOWN_DIR / team
    if not down:
        path.unlink(missing_ok=True)
        return
    path.parent.mkdir(mode=0o700, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        os.utime(fd)
    finally:
        os.close(fd)


def _is_down(environ: Mapping[str, str], team: str) -> bool:
    home = environ.get("PINGBUS_HOME", "")
    if not home.startswith("/"):
        return False
    try:
        found = os.lstat(pathlib.Path(home) / DOWN_DIR / team)
    except OSError:
        return False
    return stat.S_ISREG(found.st_mode) and time.time() - found.st_mtime < DOWN_FRESH_S


def _teams_text(environ: Mapping[str, str], teams: list[str]) -> str:
    shown = (f"{team}(down)" if _is_down(environ, team) else team for team in teams)
    return f"{CYAN}bus: {','.join(shown)}{RESET}"


def _segment(environ: Mapping[str, str], payload: dict) -> str:
    marker = environ.get(MEMBERSHIP_VAR)
    if marker is not None and marker != MEMBERSHIP_OFF and marker.startswith("/"):
        found = membership_teams(environ)
        if found is None:
            return f"{RED}{NO_MEMBERSHIP}{RESET}"
        return _teams_text(environ, found) if found else ""
    teams = session_teams(environ, _session_id(payload))
    if teams is not None:
        return _teams_text(environ, teams)
    if environ.get("PINGBUS_TEAMS"):
        return f"{RED}{NO_RECORD}{RESET}"
    return ""


def _inner(raw: bytes, payload: dict, environ: Mapping[str, str], run: Runner) -> str:
    """The inner status line's text, with a red note when it could not be had cleanly."""
    try:
        command = inner_command(_project_dir(payload, environ), environ)
    except SettingsError as exc:
        return f"{RED}status line: {exc}{RESET}"
    if command is None:
        return ""
    text, status = run(command, raw, environ, _project_dir(payload, environ))
    text = text.rstrip("\n")
    if status == 0:
        return text
    note = (f"(status line timed out after {INNER_TIMEOUT_S:g} s)" if status == TIMED_OUT
            else f"(status line exit {status})")
    return f"{text} {RED}{note}{RESET}" if text else f"{RED}{note}{RESET}"


def render(raw: bytes, environ: Mapping[str, str], run: Runner = run_command) -> tuple[str, int]:
    """The status line text (no final newline) and the exit status, always 0."""
    payload = _payload(raw)
    inner = "" if environ.get(INNER_VAR) else _inner(raw, payload, environ, run)
    segment = _segment(environ, payload)
    if not segment:
        return inner, 0
    if not inner:
        return segment, 0
    return f"{inner} | {segment}", 0


def _cancelled(signum: int, _frame: object) -> None:
    if _running is not None:
        _kill_group(_running)
    raise SystemExit(128 + signum)


def main() -> int:
    """The zipapp's entry for `pingbus status-line` (bundle.py), without the cli."""
    for signum in CANCEL_SIGNALS:
        signal.signal(signum, _cancelled)
    text, status = render(sys.stdin.buffer.read(INPUT_MAX_BYTES + 1), os.environ)
    sys.stdout.write(text + "\n")
    return status
