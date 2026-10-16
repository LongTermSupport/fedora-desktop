"""Seats: several ccy sessions in one checkout, each in its own seat of each team it is in.

Spec: Plan 00161 DESIGN.md section 5.5 (D35, D36, D48, D50) and docs/agent-bus-protocol.md
§12-§15. A seat is the pair `<seat>@<team>`: a durable member of one team, whose record is
its directory `seats/<team>/<seat>/` (the bundle, pingbus's state and `seat.lock`) under a
ccy checkout's git-ignored `.claude/ccy/pingbus/`. The host creates seats (`agent-bus seat
take`, run by the launcher); this module never does.

- `parse_seat_list`: the `<seat>@<team>[,…]` list with section 5.5's rules, each refusal a
  usage error (exit 64) naming the item and the rule; `canonical` is the list as passed on.
  The launcher's `agent-bus seat check` and `seat take` and the container's claim all use it.
- `claim`, run by `pingbus seat exec`: reads the launch's seats, takes every seat's lock
  without waiting (kind `seat` and the claim time in the file; never a PID), builds the
  session home (one link per seat, D50) and checks every bundle. The locks are made
  inheritable, so the program that replaces the process, and everything it starts, hold
  the seats until the last of them exits. A ccy launch names its seats in the read-only
  desired membership file that `PINGBUS_MEMBERSHIP` points at (Plan 00203
  SPEC-hot-control.md section 3.1, `parse_membership`), possibly none; `cc --teams` still
  passes `PINGBUS_SEATS`. Under the membership the claim also writes the session's settings,
  its inner status line and the `active` flag the plugin's hooks are guarded on (H33, H34).
- `seat_lines`: the `SEAT` report lines (`pingbus status`, and `agent-bus seat list` on the
  host), read offline from each seat's lock and `member.json`.
- `session_seats`: what the SessionStart hook says about this session's seats and their
  siblings, read through the session home's links.
"""

from __future__ import annotations

import dataclasses
import json
import os
import pathlib
import re
import stat
import time
from collections.abc import Callable, Mapping, Sequence

from helpers.pingbus import config, inbox, protocol

SEATS_VAR = "PINGBUS_SEATS"
#: The directory of the session's desired membership, mounted read-only (SPEC-hot-control.md
#: D1, H2); `off` for a `ccy --no-bus` launch, which never runs `seat exec`.
MEMBERSHIP_VAR = "PINGBUS_MEMBERSHIP"
MEMBERSHIP_OFF = "off"
MEMBERSHIP_FILE = "membership.json"
_MEMBERSHIP_KEYS = frozenset({"v", "session", "kind", "checkout", "generation", "written_ms", "seats", "farewell"})
_MEMBERSHIP_KINDS = frozenset({"ccy", "cc"})
#: In the session home: present exactly while a seat is held (H33); the settings the session
#: is launched with, and the status line it would have had without the kit (H34).
ACTIVE_FLAG = "active"
SESSION_SETTINGS = "settings.json"
STATUS_LINE_INNER = "statusline-inner"
STATUS_LINE_GUARD = (f'[ -e "$PINGBUS_HOME/{ACTIVE_FLAG}" ] || exec sh "$PINGBUS_HOME/{STATUS_LINE_INNER}"; '
                     "exec pingbus status-line")
_SESSION_FILE_MODE = 0o600
#: The session home a claim builds inside the container (D50), private to the session.
SESSION_HOME = pathlib.Path("/tmp/pingbus-home")
SESSION_HOME_MODE = 0o700
#: A checkout's seats, relative to its top; inside a ccy container the checkout is /workspace.
CHECKOUT_SEATS = pathlib.PurePosixPath(".claude/ccy/pingbus/seats")
CCY_SEATS_ROOT = pathlib.Path("/workspace") / CHECKOUT_SEATS
LOCK_FILE = "seat.lock"

HELD, FREE = "held", "free"
SELF = "self"
ABSENT = "-"
LINE_KIND = "SEAT"

_AROUND_ITEM = " \t"
_SHOWABLE_RE = re.compile(r"[\x21-\x7e]{1,64}")
_FIELD_RE = re.compile(r"[\x21-\x7e]{1,255}")
_EXAMPLE = "`--teams dev1@dev-team,qa2@other-team`"


class SeatListError(config.UsageError):
    """A `<seat>@<team>` list that breaks a rule of section 5.5 (exit 64)."""


class SeatHeld(inbox.Busy):
    """A seat another live session holds (exit 75)."""

    def __init__(self, ref: SeatRef) -> None:
        Exception.__init__(self, f"seat `{ref.text}` is held by another session")
        self.holder = inbox.SEAT_KIND
        self.ref = ref


@dataclasses.dataclass(frozen=True)
class SeatRef:
    """One `<seat>@<team>`: both names already passed their grammar."""

    seat: str
    team: str

    @property
    def text(self) -> str:
        return f"{self.seat}@{self.team}"


@dataclasses.dataclass(frozen=True)
class TeamRef:
    """A team named alone (`--teams <team>`): its seat is chosen on a terminal (Plan 00177
    Task 4.8) before anything is claimed, so it never reaches `take` or a claim."""

    team: str

    @property
    def text(self) -> str:
        return self.team


# ── the list ─────────────────────────────────────────────────────────────────────────────


def _show(item: str) -> str:
    """An item is echoed only when it is short printable ASCII, never a control character."""
    return f" ({item})" if _SHOWABLE_RE.fullmatch(item) else ""


def _team_case_hint(team: str) -> str:
    """For a team name refused only for its capitals: the lower-case spellings it may mean
    (`LongTermSupport`: `longtermsupport` or `long-term-support`); else nothing."""
    if team == team.lower():
        return ""
    kebab = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", "-", team).lower()
    found = [name for name in dict.fromkeys((team.lower(), kebab)) if protocol.is_team_name(name)]
    if not found:
        return ""
    return "; team names are lower-case: did you mean " + " or ".join(f"`{name}`" for name in found) + "?"


def _team_refusal(where: str, team: str) -> SeatListError:
    return SeatListError(
        f"{where}: the team is not a team name (a lowercase letter followed by at most 23 "
        f"lowercase letters, digits or -){_team_case_hint(team)}"
    )


def parse_seat_list(text: str, *, one_per_team: bool = True,
                    allow_bare: bool = False) -> tuple[SeatRef | TeamRef, ...]:
    """The seats of a `<seat>@<team>[,…]` list, in the order given. Spaces and tabs around
    an item are dropped; everything else that is not one seat of one team per item is
    refused, and so is a seat named twice. A list one session claims also refuses a team
    named twice (`one_per_team`); a list of seats to remove does not. With `allow_bare` an
    item may be a team alone (a `TeamRef`), whose seat is still to be chosen."""
    if not isinstance(text, str) or not text.strip(_AROUND_ITEM):
        raise SeatListError("the seat list is empty: give <seat>@<team>[,<seat>@<team>...]")
    items = text.split(",")
    refs: list[SeatRef | TeamRef] = []
    by_team: dict[str, SeatRef | TeamRef] = {}
    for position, raw in enumerate(items, start=1):
        item = raw.strip(_AROUND_ITEM)
        if not item:
            hint = ""
            if position == len(items):
                hint = (f"; an unquoted list with a space after a comma reaches ccy cut short, so "
                        f"write it with no space after a comma ({_EXAMPLE}), or quote it")
            raise SeatListError(f"item {position} is empty (a leading, doubled or trailing comma){hint}")
        where = f"item {position}{_show(item)}"
        ref: SeatRef | TeamRef
        if allow_bare and "@" not in item:
            if not protocol.is_team_name(item):
                raise _team_refusal(where, item)
            ref = TeamRef(item)
            team = item
        else:
            if item.count("@") != 1:
                hint = ""
                if "@" not in item and protocol.is_team_name(item):
                    hint = (f"; a team alone (`ccy --teams {item}`) is answered on a terminal, "
                            "where the seat is chosen")
                raise SeatListError(f"{where}: an item is <seat>@<team>, with exactly one @{hint}")
            name, team = item.split("@")
            if not protocol.is_seat_name(name):
                raise SeatListError(
                    f"{where}: the seat is not a seat name (a number 1-999999, or a lowercase "
                    "letter followed by at most 11 lowercase letters or digits)"
                )
            if not protocol.is_team_name(team):
                raise _team_refusal(where, team)
            ref = SeatRef(name, team)
        if not one_per_team and ref in refs:
            raise SeatListError(f"`{ref.text}` is named twice")
        first = by_team.setdefault(team, ref)
        if one_per_team and first is not ref:
            raise SeatListError(
                f"one seat per team per session: `{first.text}` and `{ref.text}` both name team `{team}`"
            )
        refs.append(ref)
    return tuple(refs)


def canonical(refs: tuple[SeatRef | TeamRef, ...]) -> str:
    """The list as the launcher passes, labels and restores it: the items in the order
    given, joined by `,`."""
    return ",".join(ref.text for ref in refs)


# ── a checkout's seats ───────────────────────────────────────────────────────────────────


def checkout_seats_root(checkout: pathlib.Path) -> pathlib.Path:
    return pathlib.Path(checkout) / CHECKOUT_SEATS


def seat_dir(root: pathlib.Path, ref: SeatRef) -> pathlib.Path:
    return root / ref.team / ref.seat


def lock_path(root: pathlib.Path, ref: SeatRef) -> pathlib.Path:
    return seat_dir(root, ref) / LOCK_FILE


def _real_dir(path: pathlib.Path) -> None:
    if not stat.S_ISDIR(os.lstat(path).st_mode):
        raise inbox.StateError(f"{path} is not a directory (a symlink is refused)")


def list_seats(root: pathlib.Path, *, team: str | None = None) -> tuple[SeatRef, ...]:
    """Every seat under `root` (`seats/<team>/<seat>/`), by team then seat; only `team`'s
    when given. No `root` is no seats. Anything else in the tree is refused: the tree is
    the record, so an entry that is not a seat means it is not what the host wrote."""
    try:
        _real_dir(root)
    except FileNotFoundError:
        return ()
    found: list[SeatRef] = []
    for team_dir in sorted(root.iterdir()):
        if not protocol.is_team_name(team_dir.name):
            raise inbox.StateError(f"{team_dir} is not a team's seats (not a team name)")
        _real_dir(team_dir)
        for directory in sorted(team_dir.iterdir()):
            if not protocol.is_seat_name(directory.name):
                raise inbox.StateError(f"{directory} is not a seat (not a seat name)")
            _real_dir(directory)
            found.append(SeatRef(directory.name, team_dir.name))
    return tuple(ref for ref in found if team is None or ref.team == team)


def seat_state(root: pathlib.Path, ref: SeatRef) -> str:
    """`held` while a live session holds the seat's lock, else `free`."""
    return FREE if inbox.probe_lock_at(lock_path(root, ref)) is None else HELD


def is_own(home: pathlib.Path | None, root: pathlib.Path, ref: SeatRef) -> bool:
    """Whether the session home links this seat's team to this seat."""
    if home is None:
        return False
    link = home / ref.team
    return link.is_symlink() and os.path.realpath(link) == os.path.realpath(seat_dir(root, ref))


def _report_line(*fields: str) -> str:
    for value in fields:
        if not isinstance(value, str) or not _FIELD_RE.fullmatch(value):
            raise ValueError(f"{LINE_KIND}: a field is not printable")
    return "\t".join((LINE_KIND, *fields))


def seat_lines(root: pathlib.Path, home: pathlib.Path | None) -> tuple[list[str], list[str]]:
    """One `SEAT` line per seat (team, seat, `held`/`free`, `self`/`-`, handle), and a
    message for each seat whose bundle cannot be read; the others still print."""
    lines: list[str] = []
    failures: list[str] = []
    for ref in list_seats(root):
        try:
            handle = config.read_member(ref.team, seat_dir(root, ref)).handle
            state = seat_state(root, ref)
        except (config.ConfigError, inbox.StateError) as exc:
            failures.append(f"seat {ref.text}: {exc}")
            continue
        own = SELF if is_own(home, root, ref) else ABSENT
        lines.append(_report_line(ref.team, ref.seat, state, own, handle))
    return lines, failures


# ── the claim ────────────────────────────────────────────────────────────────────────────

#: The locks a claim handed on to the program that replaces the process: kept open.
_HELD: list[inbox.Lock] = []


def held_locks() -> tuple[inbox.Lock, ...]:
    return tuple(_HELD)


def forget_held_locks() -> None:
    _HELD.clear()


@dataclasses.dataclass(frozen=True)
class Claimed:
    refs: tuple[SeatRef, ...]
    environ: dict[str, str]
    members: tuple[config.Member, ...]


def _membership_error(why: str) -> config.ConfigError:
    return config.ConfigError(f"the desired membership ({MEMBERSHIP_VAR}) {why}")


def _membership_entries(value: object, keys: frozenset[str], what: str) -> list[dict[str, object]]:
    if not isinstance(value, list):
        raise _membership_error(f"is refused: `{what}` is not a list")
    for entry in value:
        if not isinstance(entry, dict) or set(entry) != keys:
            raise _membership_error(f"is refused: a `{what}` entry is not an object of exactly "
                                    f"{', '.join(sorted(keys))}")
    return value


def parse_membership(text: str) -> tuple[SeatRef, ...]:
    """The seats of a desired membership file (SPEC-hot-control.md section 3.1), in its order.
    Anything that is not exactly version 1 is refused (exit 78): an unknown or missing key, a
    value of the wrong type, a seat or team outside the grammar, a team named twice. The
    farewell entries are checked and read past: a claim only ever takes the seats."""
    try:
        data = json.loads(text)
    except json.JSONDecodeError as exc:
        raise _membership_error(f"is not JSON: {exc.msg}") from None
    if not isinstance(data, dict):
        raise _membership_error("is not a JSON object")
    if set(data) != _MEMBERSHIP_KEYS:
        unknown = sorted(set(data) - _MEMBERSHIP_KEYS)
        missing = sorted(_MEMBERSHIP_KEYS - set(data))
        raise _membership_error(f"is refused: unknown keys {unknown}, missing keys {missing}")
    if type(data["v"]) is not int or data["v"] != 1:
        raise _membership_error(f"is version {data['v']!r}; this pingbus reads version 1")
    if not isinstance(data["session"], str) or not data["session"]:
        raise _membership_error("is refused: `session` is not a name")
    if data["kind"] not in _MEMBERSHIP_KINDS:
        raise _membership_error(f"is refused: `kind` is not one of {sorted(_MEMBERSHIP_KINDS)}")
    if not isinstance(data["checkout"], str) or not os.path.isabs(data["checkout"]):
        raise _membership_error("is refused: `checkout` is not an absolute path")
    for key, least in (("generation", 1), ("written_ms", 0)):
        if type(data[key]) is not int or data[key] < least:
            raise _membership_error(f"is refused: `{key}` is not a whole number of at least {least}")
    _membership_entries(data["farewell"], frozenset({"team", "seat", "reason"}), "farewell")
    refs: list[SeatRef] = []
    for entry in _membership_entries(data["seats"], frozenset({"team", "seat"}), "seats"):
        team, name = entry["team"], entry["seat"]
        if not protocol.is_team_name(team) or not protocol.is_seat_name(name):
            raise _membership_error(f"is refused: {_show(f'{name}@{team}')} is not <seat>@<team>")
        if any(ref.team == team for ref in refs):
            raise _membership_error(f"is refused: team {team} is named twice (one seat per team)")
        refs.append(SeatRef(name, team))
    return tuple(refs)


def _read_membership(value: str) -> tuple[SeatRef, ...]:
    if value == MEMBERSHIP_OFF:
        raise _membership_error("is off: a `ccy --no-bus` launch has no seats to claim")
    if not os.path.isabs(value):
        raise _membership_error(f"is not an absolute directory: {_show(value)}")
    path = pathlib.Path(value) / MEMBERSHIP_FILE
    try:
        text = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise _membership_error(f"cannot be read at {path}: {exc.strerror or exc}") from None
    return parse_membership(text)


def _launch_seats(environ: Mapping[str, str]) -> tuple[tuple[SeatRef, ...], bool]:
    """The launch's seats, and whether they came from the desired membership."""
    if MEMBERSHIP_VAR in environ:
        if SEATS_VAR in environ:
            raise _membership_error(f"and {SEATS_VAR} are both set: ccy passes only the first since "
                                    "Plan 00203; a launcher and an image of different versions met")
        return _read_membership(environ[MEMBERSHIP_VAR]), True
    raw = environ.get(SEATS_VAR, "")
    if not raw:
        raise config.ConfigError(
            f"{SEATS_VAR} is not set: the launch named no seat (ccy --teams <seat>@<team>[,...])"
        )
    try:
        return parse_seat_list(raw), False
    except SeatListError as exc:
        raise config.ConfigError(f"{SEATS_VAR} is malformed: {exc}") from None


# ── the session's settings (H34) ─────────────────────────────────────────────────────────


def settings_sources(environ: Mapping[str, str], project: pathlib.Path) -> tuple[pathlib.Path, ...]:
    """The settings files a Claude Code session in `project` reads, highest precedence first:
    the project's local file, the project's file, the user's (`CLAUDE_CONFIG_DIR`, else
    `~/.claude`). A flag file (`--settings`) outranks all three; it is the one written here."""
    sources = [project / ".claude" / "settings.local.json", project / ".claude" / "settings.json"]
    user_dir = environ.get("CLAUDE_CONFIG_DIR") or (
        os.path.join(environ["HOME"], ".claude") if environ.get("HOME") else "")
    if user_dir:
        sources.append(pathlib.Path(user_dir) / "settings.json")
    return tuple(sources)


def inner_status_line(sources: Sequence[pathlib.Path]) -> dict[str, object] | None:
    """The status line the session would have had without the kit: the `statusLine` of the
    first source that has one, when it is a command; None when there is none. A source that
    cannot be read or parsed is refused (78): guessing would hide the owner's status line."""
    for path in sources:
        try:
            text = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            continue
        except OSError as exc:
            raise config.ConfigError(f"cannot read {path}: {exc.strerror or exc}") from None
        try:
            data = json.loads(text)
        except json.JSONDecodeError as exc:
            raise config.ConfigError(f"{path} is not JSON ({exc.msg}), so the status line it "
                                     "sets cannot be kept beside the agent team bus kit") from None
        if not isinstance(data, dict) or "statusLine" not in data:
            continue
        line = data["statusLine"]
        if (isinstance(line, dict) and line.get("type") == "command"
                and isinstance(line.get("command"), str) and line["command"].strip()):
            return line
        return None
    return None


def session_settings(inner: Mapping[str, object] | None, *, seated: bool) -> dict[str, object]:
    """The settings the session is launched with (H34). Claude Code reads a `--settings` file
    once, at start (the M2 spike), so inbound is accepted from the start and the watcher, which
    runs only while a seat is held, is its only writer. The status line is a shell guard: the
    inner one with no `active` flag, `pingbus status-line` with it. A seated session refreshes
    each second as before; a plain one keeps the inner line's own interval and padding."""
    line: dict[str, object] = {"type": "command", "command": STATUS_LINE_GUARD}
    if seated:
        line["refreshInterval"] = 1
    elif inner is not None and "refreshInterval" in inner:
        line["refreshInterval"] = inner["refreshInterval"]
    if inner is not None and "padding" in inner:
        line["padding"] = inner["padding"]
    return {"crossSessionInbound": "accept", "statusLine": line}


def _write_private(path: pathlib.Path, text: str) -> None:
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, _SESSION_FILE_MODE)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        handle.write(text)


def _write_session_files(home: pathlib.Path, inner: Mapping[str, object] | None, *, seated: bool) -> None:
    settings = session_settings(inner, seated=seated)
    _write_private(home / SESSION_SETTINGS, json.dumps(settings, indent=2) + "\n")
    _write_private(home / STATUS_LINE_INNER, "" if inner is None else f"{inner['command']}\n")
    if seated:
        _write_private(home / ACTIVE_FLAG, "")


def _check_seat_dir(root: pathlib.Path, ref: SeatRef) -> None:
    directory = seat_dir(root, ref)
    try:
        _real_dir(directory)
    except FileNotFoundError:
        raise config.ConfigError(
            f"seat {ref.text} has no directory under {root}: seats are created on the host, "
            f"by the launch `ccy --teams {ref.text}`"
        ) from None
    except inbox.StateError as exc:
        raise config.ConfigError(str(exc)) from None
    if not os.path.lexists(directory / config.MEMBER_FILE):
        raise config.ConfigError(
            f"seat {ref.text}: no bundle (member.json is missing); the launch on the host "
            f"creates it: end this session and run `ccy --teams {ref.text}` again"
        )


def _make_home(home: pathlib.Path, root: pathlib.Path, refs: tuple[SeatRef, ...]) -> None:
    try:
        os.mkdir(home, SESSION_HOME_MODE)
    except FileExistsError:
        raise config.ConfigError(
            f"the session home {home} already exists: a session claims its seats once"
        ) from None
    os.chmod(home, SESSION_HOME_MODE)
    for ref in refs:
        os.symlink(seat_dir(root, ref), home / ref.team)


def _remove_home(home: pathlib.Path, refs: tuple[SeatRef, ...]) -> None:
    for ref in refs:
        link = home / ref.team
        if link.is_symlink():
            link.unlink()
    for name in (SESSION_SETTINGS, STATUS_LINE_INNER, ACTIVE_FLAG):
        (home / name).unlink(missing_ok=True)
    home.rmdir()


def claim(
    environ: Mapping[str, str],
    *,
    root: pathlib.Path = CCY_SEATS_ROOT,
    home: pathlib.Path = SESSION_HOME,
    clock_ms: Callable[[], int],
    sleep: Callable[[float], object] = time.sleep,
    settings_from: Sequence[pathlib.Path] = (),
    before_seats: Callable[[], object] = lambda: None,
) -> Claimed:
    """Claim every seat of the launch and build the session home; return the environment for
    the program that follows (`PINGBUS_HOME`, and `PINGBUS_TEAMS` when a seat is held) and each
    seat's checked member. `before_seats` runs once the launch is known to hold a seat, before
    any lock (the forge credential check). Under the desired membership the home also gets the
    session's settings, built from `settings_from`, and the `active` flag. Any failure releases
    what was taken and removes the home."""
    refs, from_membership = _launch_seats(environ)
    if not refs and not from_membership:
        raise config.ConfigError(f"{SEATS_VAR} names no seat")
    for ref in refs:
        _check_seat_dir(root, ref)
    inner = inner_status_line(settings_from) if from_membership else None
    if refs:
        before_seats()
    locks: list[inbox.Lock] = []
    made_home = False
    try:
        for ref in refs:
            try:
                locks.append(inbox.acquire_lock_at(lock_path(root, ref), inbox.SEAT_KIND,
                                                   claimed_ms=clock_ms(), sleep=sleep))
            except inbox.Busy:
                raise SeatHeld(ref) from None
        _make_home(home, root, refs)
        made_home = True
        child_env = dict(environ)
        child_env["PINGBUS_HOME"] = str(home)
        child_env.pop("PINGBUS_TEAMS", None)
        members: tuple[config.Member, ...] = ()
        if refs:
            child_env["PINGBUS_TEAMS"] = ",".join(ref.team for ref in refs)
            members = config.load_active(child_env)
        if from_membership:
            _write_session_files(home, inner, seated=bool(refs))
    except BaseException:
        for lock in locks:
            lock.release()
        if made_home:
            _remove_home(home, refs)
        raise
    for lock in locks:
        lock.set_inheritable()
    _HELD.extend(locks)
    return Claimed(refs, child_env, members)


# ── what the SessionStart hook says ──────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class SeatView:
    ref: SeatRef
    handle: str
    state: str
    own: bool
    history: bool


def _has_traffic(directory: pathlib.Path) -> bool:
    """Whether the seat ever received an item (`consumed/`) or sent one (`outbox.json`)."""
    state = directory / config.STATE_DIR
    consumed = state / inbox.CONSUMED_DIR
    if consumed.is_dir() and any(consumed.iterdir()):
        return True
    return os.path.lexists(state / inbox.OUTBOX_FILE)


def session_seats(home: pathlib.Path, teams: tuple[str, ...]) -> tuple[SeatView, ...]:
    """For each active team whose home entry links to a seat (`seats/<team>/<seat>`): that
    seat, then the team's other seats in the checkout, each with its handle and lock state.
    A team whose entry is a plain bundle (not a ccy seat) gives nothing."""
    views: list[SeatView] = []
    for team in teams:
        link = home / team
        if not link.is_symlink():
            continue
        target = pathlib.Path(os.path.realpath(link))
        if target.parent.name != team or not protocol.is_seat_name(target.name):
            continue
        root = target.parent.parent
        own = SeatRef(target.name, team)
        others = [ref for ref in list_seats(root, team=team) if ref != own]
        for ref in (own, *others):
            directory = seat_dir(root, ref)
            handle = config.read_member(team, directory).handle
            views.append(SeatView(ref, handle, seat_state(root, ref), ref == own,
                                  ref == own and _has_traffic(directory)))
    return tuple(views)
