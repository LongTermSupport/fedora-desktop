"""`pingbus status-line`: the session's status line, with the agent-bus teams it is on.

Plan 00201 S-C6. Claude Code draws a status line by running ONE command: the `statusLine` of the
highest-precedence settings that set one. The member kit's settings.json, which every bus launch
passes with `--settings` (ccy's entrypoint, `cc --teams`, agent-bus-claude), names this command,
and command-line settings outrank the project's and the user's. So it works in any checkout,
with or without a hooks daemon: it runs the status line the session would have had without the
kit (the first `statusLine` command in the project's .claude/settings.local.json, then its
.claude/settings.json, then the user's settings.json) with the same stdin, prints that, and adds
`👥 A (role), B (role)` from the record the SessionStart hook wrote under the session id
(`hooks.record_session_teams`, which writes where `sessions_dir` says).

The teams go on rows of their own below the inner status line, never appended to its last row:
Claude Code cuts a row wider than the terminal with an ellipsis, and the hooks daemon's status
line already wraps its own rows to the width, so its last row can be full. The teams are wrapped
to COLUMNS (which Claude Code sets for a status line command) after a comma, and a red note
between words; nothing is cut. A team name wider than the terminal keeps a row of its own, whole.
With no usable COLUMNS nothing says where to break, so the segment is one unbroken row.

A bus session (PINGBUS_TEAMS set) with no usable record shows `👥 no record ...` in red, never
nothing, so a record that failed cannot pass for a session on no team. The status line runs as a
child of this session's claude, so PINGBUS_TEAMS here is this session's own.

It always exits 0: Claude Code blanks the whole line on a non-zero exit, which would hide the
segment and the failure alike, so a failing, hung or unreadable inner status line is shown in red
instead. The inner command runs in its own process group, killed on a timeout and when this
process is cancelled (Claude Code cancels an in-flight render when a new one starts), so nothing
it started outlives the render.

Each team shows this session's member in it and its role, `👥 desktop: fedora-desktop.2 (c),
other: fedora-desktop.2 (w)` (Plan 00217 SPEC-team-colours C15 to C18): the member's handle up to
its `+`, from the bundle, and the role's letter, read on every render from the team record the
hooks' role notice reads (`inbox.cached_role`), so a role change shows at the next refresh with
no restart. What cannot be read is `member?` or `(role?)`, never a guess, with one line on stderr
naming the teams and why. A team's marks such as `(down)` come after its role: the team, its
member and role, then each mark, in brackets of their own.

Each team piece is its own span of the team's colour (C10 to C12, `colours`): the override the
syncer keeps in the team's `state/colour.json`, else the colour its name gives. An override that
cannot be read is the automatic colour, the mark `(colour?)` and one more stderr line.

It renders on every refresh, so loading it imports no other pingbus module (the zipapp's
__main__ runs it without the cli): `read_roles` imports config and inbox only when there are
teams to show, and `read_colours` imports colours. The few names it shares with hooks.py and protocol.py are copies a test keeps
equal.
"""

from __future__ import annotations

import errno
import json
import os
import pathlib
import re
import signal
import stat
import subprocess
import sys
import time
import unicodedata
from collections.abc import Callable, Mapping
from typing import TextIO

COMMAND = "pingbus status-line"
#: The Go port of this command (src-go/cmd/pingbus-status-line, Plan 00213), which the member
#: kit and a seated session run; this module is its parity oracle. A settings file naming
#: either is passed over.
KIT_COMMAND = "pingbus-status-line"
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
RED, RESET = "\033[31m", "\033[0m"
#: The segment's label, the team counterpart of the hooks daemon's 👤 account segment.
LABEL = "👥"
COLUMNS_VAR = "COLUMNS"
COLUMNS_RE = re.compile(r"[0-9]{1,6}")
#: A colour (SGR/CSI) escape, which takes no column.
ANSI_ESCAPE_RE = re.compile("\x1b\\[[0-9;]*[A-Za-z]")
NO_RECORD = "no record of this session's teams (pingbus status)"
#: Plan 00203 H14: the membership files this module reads its copy of the active teams from.
MEMBERSHIP_VAR = "PINGBUS_MEMBERSHIP"
MEMBERSHIP_OFF = "off"
DESIRED_FILE = "membership.json"
APPLIED_FILE = "applied.json"
MAX_MEMBERSHIP_BYTES = 65536
NO_MEMBERSHIP = "this session's membership is unreadable (pingbus status)"
#: Plan 00203 H26: `PINGBUS_HOME/down/<team>` is there while the watcher retries a team whose
#: homeserver does not answer; each retry refreshes it (its backoff caps at 30 s), so a mark a
#: killed watcher left stops counting after DOWN_FRESH_S.
DOWN_DIR = "down"
DOWN_FRESH_S = 300.0
CANCEL_SIGNALS = (signal.SIGTERM, signal.SIGINT, signal.SIGHUP)
#: Shown in place of a role, and of this member's short name, that cannot be read (C16).
ROLE_UNKNOWN = "role?"
MEMBER_UNKNOWN = "member?"
#: The role's letter on the status line (C15).
ROLE_LETTERS = {"coordinator": "c", "worker": "w"}
#: Plan 00217 C12: the override the syncer keeps (`inbox.COLOUR_FILE`), in each team's state.
COLOUR_FILE = "colour.json"
MAX_COLOUR_BYTES = 4096
#: The mark of a team whose override cannot be read.
COLOUR_UNKNOWN = "colour?"

Runner = Callable[[str, bytes, Mapping[str, str], "pathlib.Path | None"], tuple[str, int]]
#: (this member's short name, its role), either None where unreadable.
MemberRole = tuple["str | None", "str | None"]
#: Teams -> (each team's MemberRole; one diagnostic line, None when all read).
RoleReader = Callable[[Mapping[str, str], list[str]], tuple[dict[str, MemberRole], "str | None"]]

#: The inner command's process while it runs, for the cancel handler.
_running: subprocess.Popen | None = None
#: True while Popen is starting the inner command, and a cancel that landed meanwhile.
_starting = False
_cancel_pending: int | None = None


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
        if isinstance(command, str) and command.strip() and command.strip() not in (COMMAND, KIT_COMMAND):
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
    global _running, _starting, _cancel_pending
    env = dict(environ, **{INNER_VAR: "1"})
    directory = cwd if cwd is not None and cwd.is_dir() else None
    # The inner command runs before Popen returns, so a cancel can land while the handler cannot
    # name its process yet: the handler leaves it pending and it is carried out here, once known.
    _starting = True
    try:
        proc = subprocess.Popen(["/bin/sh", "-c", command], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                env=env, cwd=directory, start_new_session=True)
        _running = proc
    finally:
        _starting = False
        pending, _cancel_pending = _cancel_pending, None
    if pending is not None:
        _kill_group(proc)
        _running = None
        raise SystemExit(128 + pending)
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


def read_roles(environ: Mapping[str, str], teams: list[str]) -> tuple[dict[str, MemberRole], str | None]:
    """This member's short name and role in each team: the name from each bundle as
    `config.load_active` validates it, the role from its cached team record by
    `inbox.cached_role` (what the role notice reads), None where either cannot be read; and one
    diagnostic line naming every team whose role was not read and why, None when every role was
    read. The bus modules are imported here, not with this module, so a render with no teams
    stays cheap."""
    from helpers.pingbus import config, inbox, protocol

    roles: dict[str, MemberRole] = dict.fromkeys(teams, (None, None))
    failed: list[str] = []
    try:
        home = config.resolve_home(environ)
    except config.ConfigError as exc:
        return roles, f"pingbus status-line: no team's role can be read: {exc}"
    for team in teams:
        try:
            member = config.load_bundle(home, team)
        except (config.ConfigError, protocol.Untrusted) as exc:
            failed.append(f"{team}: {exc}")
            continue
        short = member.handle.split(protocol.HANDLE_SEP, 1)[0]
        try:
            roles[team] = (short, inbox.cached_role(member))
        except (config.ConfigError, protocol.Untrusted) as exc:
            roles[team] = (short, None)
            failed.append(f"{team}: {exc}")
    if not failed:
        return roles, None
    return roles, f"pingbus status-line: the role cannot be read ({'; '.join(failed)})"


def _colour_override(path: pathlib.Path) -> tuple[str | None, str | None]:
    """C12: the override in `path` as #rrggbb, or None; and why it cannot be read, None when it
    was read or is absent."""
    from helpers.pingbus import colours

    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    except FileNotFoundError:
        return None, None
    except OSError as exc:
        return None, "the colour file is a symlink" if exc.errno == errno.ELOOP else "the colour file cannot be read"
    # fstat before fdopen: fdopen itself refuses a directory, with an exception of its own.
    if not stat.S_ISREG(os.fstat(fd).st_mode):
        os.close(fd)
        return None, "the colour file is not a regular file"
    with os.fdopen(fd, "rb") as handle:
        try:
            raw = handle.read(MAX_COLOUR_BYTES + 1)
        except OSError:
            return None, "the colour file cannot be read"
    if len(raw) > MAX_COLOUR_BYTES:
        return None, "the colour file is too large"
    try:
        data = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None, "the colour file is not JSON"
    if (not isinstance(data, dict) or data.keys() != {"v", "colour"} or type(data["v"]) is not int
            or data["v"] != 1 or not colours.is_stored_colour(data["colour"])):
        return None, 'the colour file is not {"v": 1, "colour": "#rrggbb"}'
    return data["colour"], None


def read_colours(environ: Mapping[str, str], teams: list[str]) -> tuple[dict[str, str], set[str], str | None]:
    """C12: each team's colour as #rrggbb, its override or else its automatic colour; the teams
    whose override cannot be read; and one diagnostic line naming them, None when there are none.
    With no home every team is automatic: the role line already says why."""
    from helpers.pingbus import colours, config

    shown = {team: colours.auto_colour(team) for team in teams}
    try:
        home = config.resolve_home(environ)
    except config.ConfigError:
        return shown, set(), None
    unknown: set[str] = set()
    failed: list[str] = []
    for team in teams:
        override, reason = _colour_override(home / team / "state" / COLOUR_FILE)
        if reason is not None:
            unknown.add(team)
            failed.append(f"{team}: {reason}")
        shown[team] = override or shown[team]
    if not failed:
        return shown, unknown, None
    return shown, unknown, f"pingbus status-line: the colour cannot be read ({'; '.join(failed)})"


def display_width(text: str) -> int:
    """The terminal columns `text` takes, by the hooks daemon's measure: colour escapes take
    none, wide and fullwidth code points (most emoji) two, combining and format code points none.
    Its input is this module's own segment text (LABEL, team names, the fixed notes), never the
    inner status line, so the heuristic is exact for everything it measures."""
    width = 0
    for char in ANSI_ESCAPE_RE.sub("", text):
        if unicodedata.combining(char) or unicodedata.category(char) == "Cf":
            continue
        width += 2 if unicodedata.east_asian_width(char) in ("W", "F") else 1
    return width


def terminal_columns(environ: Mapping[str, str]) -> int | None:
    """The terminal's width from COLUMNS, or None when it is not a positive number."""
    value = environ.get(COLUMNS_VAR, "")
    return int(value) if COLUMNS_RE.fullmatch(value) and int(value) > 0 else None


def _rows(pieces: list[str], columns: int | None) -> list[list[int]]:
    """The pieces' indices on each row of `LABEL pieces`, no row wider than `columns` (unbroken
    when None), measured on the plain texts. A row breaks only between pieces, joined by one
    space; a later row is indented under the first, as wide as `LABEL `."""
    rows = [[0]]
    width = display_width(f"{LABEL} {pieces[0]}")
    for index, piece in enumerate(pieces[1:], start=1):
        candidate = width + 1 + display_width(piece)
        if columns is None or candidate <= columns:
            rows[-1].append(index)
            width = candidate
        else:
            rows.append([index])
            width = display_width(f"{LABEL} {piece}")
    return rows


def _lead(row: int) -> str:
    """The label before the first row, an indent as wide before each later one."""
    return f"{LABEL} " if row == 0 else " " * display_width(f"{LABEL} ")


def _wrapped(colour: str, pieces: list[str], columns: int | None) -> str:
    """The pieces wrapped (`_rows`), each row one colour span, label included."""
    return "\n".join(f"{colour}{_lead(n)}{' '.join(pieces[i] for i in row)}{RESET}"
                     for n, row in enumerate(_rows(pieces, columns)))


def _team_piece(environ: Mapping[str, str], team: str, found: MemberRole, colour_unknown: bool) -> str:
    """`team: member (r)`, then each of its marks: the one order every team is shown in."""
    short, role = found
    letter = ROLE_LETTERS.get(role, ROLE_UNKNOWN) if role is not None else ROLE_UNKNOWN
    marks = (" (down)" if _is_down(environ, team) else "") + (f" ({COLOUR_UNKNOWN})" if colour_unknown else "")
    return f"{team}: {short or MEMBER_UNKNOWN} ({letter}){marks}"


def _teams_text(environ: Mapping[str, str], teams: list[str], read: RoleReader, err: TextIO) -> str:
    """C10: the label and the indent plain, each team's piece (with its comma) in its own span
    of the team's colour, the pieces joined by plain spaces."""
    from helpers.pingbus import colours

    roles, problem = read(environ, teams)
    shown, unknown, colour_problem = read_colours(environ, teams)
    for line in (problem, colour_problem):
        if line is not None:
            err.write(line + "\n")
    texts = [_team_piece(environ, team, roles.get(team, (None, None)), team in unknown) for team in teams]
    pieces = [f"{text}," for text in texts[:-1]] + texts[-1:]
    spans = [f"{colours.sgr(shown[team])}{piece}{RESET}" for team, piece in zip(teams, pieces, strict=True)]
    return "\n".join(_lead(n) + " ".join(spans[i] for i in row)
                     for n, row in enumerate(_rows(pieces, terminal_columns(environ))))


def _note(environ: Mapping[str, str], text: str) -> str:
    return _wrapped(RED, text.split(" "), terminal_columns(environ))


def _segment(environ: Mapping[str, str], payload: dict, read: RoleReader, err: TextIO) -> str:
    marker = environ.get(MEMBERSHIP_VAR)
    if marker is not None and marker != MEMBERSHIP_OFF and marker.startswith("/"):
        found = membership_teams(environ)
        if found is None:
            return _note(environ, NO_MEMBERSHIP)
        return _teams_text(environ, found, read, err) if found else ""
    teams = session_teams(environ, _session_id(payload))
    if teams is not None:
        return _teams_text(environ, teams, read, err)
    if environ.get("PINGBUS_TEAMS"):
        return _note(environ, NO_RECORD)
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


def render(raw: bytes, environ: Mapping[str, str], run: Runner = run_command, *,
           err: TextIO | None = None) -> tuple[str, int]:
    """The status line text (no final newline) and the exit status, always 0. The segment
    starts a row of its own: appended to the inner's last row it could push that row past the
    terminal's width, where Claude Code cuts it. A role that cannot be read is said on `err`
    (default stderr)."""
    payload = _payload(raw)
    inner = "" if environ.get(INNER_VAR) else _inner(raw, payload, environ, run)
    segment = _segment(environ, payload, read_roles, sys.stderr if err is None else err)
    if not segment:
        return inner, 0
    if not inner:
        return segment, 0
    return f"{inner}\n{segment}", 0


def _cancelled(signum: int, _frame: object) -> None:
    global _cancel_pending
    if _starting:
        _cancel_pending = signum  # run_command carries it out once Popen has returned
        return
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
