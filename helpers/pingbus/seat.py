"""Seats: several ccy sessions in one checkout, each in its own seat of each team it is in.

Spec: Plan 00161 DESIGN.md section 5.5 (D35, D36, D48, D50) and docs/agent-bus-protocol.md
§12-§15. A seat is the pair `<seat>@<team>`: a durable member of one team, whose record is
its directory `seats/<team>/<seat>/` (the bundle, pingbus's state and `seat.lock`) under a
ccy checkout's git-ignored `.claude/ccy/pingbus/`. The host creates seats (`agent-bus seat
take`, run by the launcher); this module never does.

- `parse_seat_list`: the `<seat>@<team>[,…]` list with section 5.5's rules, each refusal a
  usage error (exit 64) naming the item and the rule; `canonical` is the list as passed on.
  The launcher's `agent-bus seat check` and `seat take` and the container's claim all use it.
- `claim`, run by `pingbus seat exec`: reads `PINGBUS_SEATS`, takes every seat's lock
  without waiting (kind `seat` and the claim time in the file; never a PID), builds the
  session home (one link per seat, D50) and checks every bundle. The locks are made
  inheritable, so the program that replaces the process, and everything it starts, hold
  the seats until the last of them exits.
- `seat_lines`: the `SEAT` report lines (`pingbus status`, and `agent-bus seat list` on the
  host), read offline from each seat's lock and `member.json`.
- `session_seats`: what the SessionStart hook says about this session's seats and their
  siblings, read through the session home's links.
"""

from __future__ import annotations

import dataclasses
import os
import pathlib
import re
import stat
import time
from collections.abc import Callable, Mapping

from helpers.pingbus import config, inbox, protocol

SEATS_VAR = "PINGBUS_SEATS"
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


# ── the list ─────────────────────────────────────────────────────────────────────────────


def _show(item: str) -> str:
    """An item is echoed only when it is short printable ASCII, never a control character."""
    return f" ({item})" if _SHOWABLE_RE.fullmatch(item) else ""


def parse_seat_list(text: str, *, one_per_team: bool = True) -> tuple[SeatRef, ...]:
    """The seats of a `<seat>@<team>[,…]` list, in the order given. Spaces and tabs around
    an item are dropped; everything else that is not one seat of one team per item is
    refused, and so is a seat named twice. A list one session claims also refuses a team
    named twice (`one_per_team`); a list of seats to remove does not."""
    if not isinstance(text, str) or not text.strip(_AROUND_ITEM):
        raise SeatListError("the seat list is empty: give <seat>@<team>[,<seat>@<team>...]")
    items = text.split(",")
    refs: list[SeatRef] = []
    by_team: dict[str, SeatRef] = {}
    for position, raw in enumerate(items, start=1):
        item = raw.strip(_AROUND_ITEM)
        if not item:
            hint = ""
            if position == len(items):
                hint = (f"; an unquoted list with a space after a comma reaches ccy cut short, so "
                        f"write it with no space after a comma ({_EXAMPLE}), or quote it")
            raise SeatListError(f"item {position} is empty (a leading, doubled or trailing comma){hint}")
        where = f"item {position}{_show(item)}"
        if item.count("@") != 1:
            raise SeatListError(f"{where}: an item is <seat>@<team>, with exactly one @")
        name, team = item.split("@")
        if not protocol.is_seat_name(name):
            raise SeatListError(
                f"{where}: the seat is not a seat name (a number 1-999999, or a lowercase "
                "letter followed by at most 11 lowercase letters or digits)"
            )
        if not protocol.is_team_name(team):
            raise SeatListError(
                f"{where}: the team is not a team name (a lowercase letter followed by at most "
                "23 lowercase letters, digits or -)"
            )
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


def canonical(refs: tuple[SeatRef, ...]) -> str:
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


def _launch_seats(environ: Mapping[str, str]) -> tuple[SeatRef, ...]:
    raw = environ.get(SEATS_VAR, "")
    if not raw:
        raise config.ConfigError(
            f"{SEATS_VAR} is not set: the launch named no seat (ccy --teams <seat>@<team>[,...])"
        )
    try:
        return parse_seat_list(raw)
    except SeatListError as exc:
        raise config.ConfigError(f"{SEATS_VAR} is malformed: {exc}") from None


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
    home.rmdir()


def claim(
    environ: Mapping[str, str],
    *,
    root: pathlib.Path = CCY_SEATS_ROOT,
    home: pathlib.Path = SESSION_HOME,
    clock_ms: Callable[[], int],
    sleep: Callable[[float], object] = time.sleep,
) -> Claimed:
    """Claim every seat `PINGBUS_SEATS` names and build the session home; return the
    environment for the program that follows (`PINGBUS_HOME`, `PINGBUS_TEAMS` set) and
    each seat's checked member. Any failure releases what was taken and removes the home."""
    refs = _launch_seats(environ)
    for ref in refs:
        _check_seat_dir(root, ref)
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
        child_env["PINGBUS_TEAMS"] = ",".join(ref.team for ref in refs)
        members = config.load_active(child_env)
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
