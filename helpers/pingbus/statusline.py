"""`pingbus status-line`: the session's status line, with the agent-bus teams it is on.

Plan 00201 S-C6. Claude Code draws a status line by running ONE command: the `statusLine` of the
highest-precedence settings that set one. The member kit's settings.json, which every bus launch
passes with `--settings` (ccy's entrypoint, `cc --teams`, agent-bus-claude), names this command,
and command-line settings outrank the project's and the user's. So it works in any checkout,
with or without a hooks daemon: it runs the status line the session would have had without the
kit (the first `statusLine` command in the project's .claude/settings.local.json, then its
.claude/settings.json, then the user's settings.json) with the same stdin, prints that, and adds
`bus: A,B` from the record the SessionStart hook wrote under the session id
(`hooks.record_session_teams`).

A bus session (PINGBUS_TEAMS set) with no usable record shows `bus: no record ...` in red, never
nothing, so a record that failed cannot pass for a session on no team. The status line runs as a
child of this session's claude, so PINGBUS_TEAMS here is this session's own.
"""

from __future__ import annotations

import json
import os
import pathlib
import stat
import subprocess
from collections.abc import Callable, Mapping

from helpers.pingbus import hooks, protocol

COMMAND = "pingbus status-line"
#: Set for the inner status line, so a settings file that names this command cannot loop.
INNER_VAR = "PINGBUS_STATUS_LINE_INNER"
MAX_RECORD_BYTES = 4096
CYAN, RED, RESET = "\033[36m", "\033[31m", "\033[0m"
NO_RECORD = "bus: no record of this session's teams (pingbus status)"

Runner = Callable[[str, bytes, Mapping[str, str], "pathlib.Path | None"], tuple[str, int]]


class SettingsError(Exception):
    """A settings file that exists but cannot be read as a JSON object."""


def session_teams(environ: Mapping[str, str], session_id: str | None) -> list[str] | None:
    """The teams recorded for this session, or None when there is no usable record: no id, no
    file, a file that is not this user's alone, or contents that are not distinct team names."""
    if session_id is None:
        return None
    path = hooks.sessions_dir(environ) / session_id
    try:
        found = os.lstat(path)
        if not stat.S_ISREG(found.st_mode) or found.st_uid != os.getuid() or found.st_mode & 0o077:
            return None
        if found.st_size > MAX_RECORD_BYTES:
            return None
        teams = path.read_text(encoding="ascii").split(",")
    except (OSError, UnicodeDecodeError):
        return None
    if len(set(teams)) != len(teams) or not all(protocol.is_team_name(team) for team in teams):
        return None
    return teams


def _payload(raw: bytes) -> dict:
    if len(raw) > hooks.INPUT_MAX_BYTES:
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


def run_command(command: str, raw: bytes, environ: Mapping[str, str],
                cwd: pathlib.Path | None) -> tuple[str, int]:
    """Run a status line command as Claude Code does: through the shell, the payload on stdin,
    stderr left to the caller's."""
    env = dict(environ, **{INNER_VAR: "1"})
    directory = cwd if cwd is not None and cwd.is_dir() else None
    done = subprocess.run(["/bin/sh", "-c", command], input=raw, stdout=subprocess.PIPE,
                          env=env, cwd=directory, check=False)
    return done.stdout.decode("utf-8", errors="replace"), done.returncode


def _segment(environ: Mapping[str, str], payload: dict) -> str:
    teams = session_teams(environ, hooks._session_id(payload))
    if teams is not None:
        return f"{CYAN}bus: {','.join(teams)}{RESET}"
    if environ.get("PINGBUS_TEAMS"):
        return f"{RED}{NO_RECORD}{RESET}"
    return ""


def render(raw: bytes, environ: Mapping[str, str], run: Runner = run_command) -> tuple[str, int]:
    """The status line text (no final newline) and the exit status to end with."""
    payload = _payload(raw)
    project = _project_dir(payload, environ)
    inner, status = "", 0
    if not environ.get(INNER_VAR):
        try:
            command = inner_command(project, environ)
        except SettingsError as exc:
            inner, status = f"{RED}status line: {exc}{RESET}", 1
        else:
            if command is not None:
                inner, status = run(command, raw, environ, project)
    inner = inner.rstrip("\n")
    segment = _segment(environ, payload)
    if not segment:
        return inner, status
    if not inner:
        return segment, status
    return f"{inner} | {segment}", status
