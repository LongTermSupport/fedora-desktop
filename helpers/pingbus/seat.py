"""Seats: several ccy sessions in one checkout, each in its own seat of each team it is in.

Spec: Plan 00161 DESIGN.md section 5.5 (D35, D36, D48, D50) and docs/agent-bus-protocol.md
§12-§15. A seat is the pair `<seat>@<team>`: a durable member of one team, whose record is
its directory `seats/<team>/<seat>/` (the bundle, pingbus's state and `seat.lock`) under a
ccy checkout's git-ignored `.claude/ccy/pingbus/`. The host creates seats (`agent-bus seat
take`, run by the launcher); this module never does.

- `parse_seat_list`: the `<seat>@<team>[,…]` list with section 5.5's rules, each refusal a
  usage error (exit 64) naming the item and the rule; `canonical` is the list as passed on.
  The launcher's `agent-bus seat check` and `seat take` and the container's claim all use it.
- `Keeper`, run as `pingbus seat keep` (Plan 00203 D4, H9): the only process that holds a
  seat lock, for every seat of the session, launch seats included. `pingbus seat exec`
  starts it before it replaces itself with the session. It makes the session home (even
  with no seat), takes each desired seat's lock without waiting (kind `seat` and the claim
  time in the file; never a PID), links the seat into the home (D50), checks its bundle,
  and writes the applied membership, the host's report and the flag file. It follows the
  desired file `PINGBUS_MEMBERSHIP` names as it changes. Every lock is close-on-exec, so the
  session's processes hold none and a detached one cannot keep a seat (audit F5); release is
  an explicit `LOCK_UN`. A ccy launch names its seats (possibly none) in the read-only
  desired file (membership.py); so does `cc --teams` or `cc --bus`, in a directory of its own
  on the host (Plan 00203 M6).
- `session_settings` and its helpers: under the desired membership, `seat exec` writes the
  session's settings and its inner status line into the home (H34); the keeper keeps the
  `active` flag the plugin's hooks are guarded on (H33).
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
import secrets
import stat
import time
from collections.abc import Callable, Mapping, Sequence
from typing import TextIO

from helpers.pingbus import config, inbox, inject, membership, notify, protocol

#: The keeper's lock in the session home: held while it runs (H10).
KEEPER_LOCK = "keeper.lock"
#: Where the keeper's diagnostics go once it is running, in the session home.
KEEPER_LOG = "keeper.log"
#: The watcher's log in the session home; the hooks append to the same file.
WATCH_LOG = "watch.log"
#: Beside a checkout's seats: the keeper's report for the host, per session (3.3).
SESSIONS_DIR = "sessions"
#: How often the keeper looks at the desired file and at its parent (H9: within 2 s).
KEEP_TICK_S = 0.5
#: Kept here for the names other modules and tests use; membership.py owns them.
MEMBERSHIP_VAR = membership.MEMBERSHIP_VAR
MEMBERSHIP_OFF = membership.MEMBERSHIP_OFF
MEMBERSHIP_FILE = membership.DESIRED_FILE
#: In the session home: present exactly while a seat is held (H33), kept by the keeper; the
#: settings the session is launched with, and the status line it would have had without the
#: kit (H34), written by `seat exec`.
ACTIVE_FLAG = membership.FLAG_FILE
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


# ── what the keeper follows ──────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Want:
    """What the keeper should hold: the generation, the seats in order, and the session's
    registry name when the host wrote one (where the report goes)."""

    generation: int
    refs: tuple[SeatRef, ...]
    session: str | None = None


class MembershipFile:
    """The host's desired file (3.1) in `directory`, read again whenever its inode, size or
    modification time changes (H9)."""

    def __init__(self, directory: pathlib.Path) -> None:
        self.directory = pathlib.Path(directory)

    def stamp(self) -> object:
        try:
            info = os.stat(membership.desired_path(self.directory), follow_symlinks=False)
        except FileNotFoundError:
            return None
        return (info.st_ino, info.st_mtime_ns, info.st_size)

    def read(self) -> Want:
        try:
            desired = membership.read_desired(self.directory)
        except membership.MembershipError as exc:
            raise config.ConfigError(str(exc)) from None
        return Want(desired.generation, tuple(SeatRef(e.seat, e.team) for e in desired.seats),
                    desired.session)


def desired_source(environ: Mapping[str, str]) -> MembershipFile:
    """The desired membership `PINGBUS_MEMBERSHIP` names, which every launcher sets (ccy,
    `cc --teams`, `cc --bus`)."""
    marker = environ.get(membership.MEMBERSHIP_VAR)
    if marker is None:
        raise config.ConfigError(f"{membership.MEMBERSHIP_VAR} is not set: the launcher names the session's "
                                 "desired membership (ccy, cc --teams, cc --bus)")
    try:
        return MembershipFile(membership.mount_dir(marker))
    except membership.MembershipError as exc:
        raise config.ConfigError(str(exc)) from None


class SeatRefused(Exception):
    """A desired seat the keeper did not take: its class (3.2 `why`), the kind of the lock's
    holder for `held-elsewhere`, and the error a launch reports for it."""

    def __init__(self, ref: SeatRef, why: str, error: Exception,
                 holder: str = membership.NO_HOLDER) -> None:
        super().__init__(str(error))
        self.ref, self.why, self.error, self.holder = ref, why, error, holder


class KeeperBusy(inbox.Busy):
    """Another keeper holds this session home (exit 75)."""

    def __init__(self, home: pathlib.Path) -> None:
        Exception.__init__(self, f"another seat keeper is running for the session home {home}")
        self.holder = inbox.KEEPER_KIND


def check_seat_dir(root: pathlib.Path, ref: SeatRef) -> None:
    """The seat's directory is a real directory holding a `member.json`: `SeatRefused`,
    `no-bundle` or `bundle-invalid`, naming the host's launch that makes it, if not."""
    directory = seat_dir(root, ref)
    try:
        _real_dir(directory)
    except FileNotFoundError:
        raise SeatRefused(ref, "no-bundle", config.ConfigError(
            f"seat {ref.text} has no directory under {root}: seats are created on the host, "
            f"by the launch `ccy --teams {ref.text}`")) from None
    except inbox.StateError as exc:
        raise SeatRefused(ref, "bundle-invalid", config.ConfigError(str(exc))) from None
    if not os.path.lexists(directory / config.MEMBER_FILE):
        raise SeatRefused(ref, "no-bundle", config.ConfigError(
            f"seat {ref.text}: no bundle (member.json is missing); the launch on the host "
            f"creates it: end this session and run `ccy --teams {ref.text}` again"))


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


class SettingsUnreadable(config.ConfigError):
    """A settings file whose status line cannot be read; the claim names it and leaves the
    status line to Claude Code, so a broken file never stops a session starting."""


def inner_status_line(sources: Sequence[pathlib.Path]) -> dict[str, object] | None:
    """The status line the session would have had without the kit: the `statusLine` of the
    first source that has one, when it is a command; None when there is none. A source that
    cannot be read or parsed raises `SettingsUnreadable`: guessing would hide the owner's
    status line."""
    for path in sources:
        try:
            text = path.read_text(encoding="utf-8")
        except FileNotFoundError:
            continue
        except OSError as exc:
            raise SettingsUnreadable(f"cannot read {path}: {exc.strerror or exc}") from None
        try:
            data = json.loads(text)
        except json.JSONDecodeError as exc:
            raise SettingsUnreadable(f"{path} is not JSON ({exc.msg})") from None
        if not isinstance(data, dict) or "statusLine" not in data:
            continue
        line = data["statusLine"]
        if (isinstance(line, dict) and line.get("type") == "command"
                and isinstance(line.get("command"), str) and line["command"].strip()):
            return line
        return None
    return None


def session_settings(inner: Mapping[str, object] | None, *, seated: bool, guard: bool = True) -> dict[str, object]:
    """The settings the session is launched with (H33b, H34). Claude Code reads a `--settings`
    file once, at start (the M2 spike), so inbound is decided at launch: a session launched
    with a seat accepts (the watcher, which runs only while a seat is held, is the only writer
    the kit starts); a session launched on no team refuses (Decision 1). Claude Code binds its
    inbox socket in every session whatever this says, so `refuse` is what keeps a message on
    it from becoming a turn or a review for the human; a `--settings` value outranks the
    user's settings, which the session can write. Its first join relaunches it once. The status line is a
    shell guard: the inner one with no `active` flag, `pingbus status-line` with it. A seated
    session refreshes each second as before; a plain one keeps the inner line's own interval
    and padding. Without `guard` (a settings file could not be read) no status line is set,
    so Claude Code's own applies and there is no bus segment."""
    settings: dict[str, object] = {"crossSessionInbound": "accept" if seated else "refuse"}
    if not guard:
        return settings
    line: dict[str, object] = {"type": "command", "command": STATUS_LINE_GUARD}
    if seated:
        line["refreshInterval"] = 1
    elif inner is not None and "refreshInterval" in inner:
        line["refreshInterval"] = inner["refreshInterval"]
    if inner is not None and "padding" in inner:
        line["padding"] = inner["padding"]
    settings["statusLine"] = line
    return settings


def _write_private(path: pathlib.Path, text: str) -> None:
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, _SESSION_FILE_MODE)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        handle.write(text)


def write_session_files(home: pathlib.Path, inner: Mapping[str, object] | None, *, seated: bool,
                        guard: bool) -> None:
    """The session's settings and its inner status line, in the home the keeper made. The
    `active` flag is the keeper's (H33)."""
    settings = session_settings(inner, seated=seated, guard=guard)
    _write_private(home / SESSION_SETTINGS, json.dumps(settings, indent=2) + "\n")
    _write_private(home / STATUS_LINE_INNER, "" if inner is None else f"{inner['command']}\n")


# ── the keeper ───────────────────────────────────────────────────────────────────────────


class Keeper:
    """The session's only seat lock holder (D4). `start` makes or adopts the session home
    and applies what the source wants; `poll` applies it again when it changes (H9);
    `release_all` lets every lock go, as the keeper's death would."""

    def __init__(self, source: MembershipFile, *, root: pathlib.Path,
                 home: pathlib.Path, clock_ms: Callable[[], int], err: TextIO,
                 sleep: Callable[[float], object] = time.sleep, uid: int | None = None,
                 user_home: pathlib.Path | None = None, environ: Mapping[str, str] | None = None,
                 spawn_watcher: Callable[[Mapping[str, str], pathlib.Path], object] | None = None) -> None:
        self.source, self.root, self.home = source, pathlib.Path(root), pathlib.Path(home)
        self._clock_ms, self._sleep, self._err = clock_ms, sleep, err
        self._uid, self._user_home = uid, user_home
        self._environ, self._spawn_watcher = environ, spawn_watcher
        self._held: dict[SeatRef, tuple[inbox.Lock, config.Member]] = {}
        self._keeper_lock: inbox.Lock | None = None
        self._stamp: object = None
        self._told_stamp: object = None
        self._want: Want | None = None
        self._inbound: bool | None = None

    # The life of the keeper.

    def start(self, *, launch: bool) -> None:
        """Take the session home and the keeper lock, then the seats. A launch (`seat
        exec`'s keeper) makes a new home and is all or nothing: the first refusal is raised
        and nothing is kept. Otherwise (Repair) the home may exist, and a refusal is
        recorded while the other seats are still taken."""
        made = self._make_home(launch)
        try:
            try:
                self._keeper_lock = inbox.acquire_lock_at(self.home / KEEPER_LOCK, inbox.KEEPER_KIND,
                                                          sleep=self._sleep)
            except inbox.Busy:
                raise KeeperBusy(self.home) from None
            self._stamp = self.source.stamp()
            self._told_stamp = _stamp_of(self.home / membership.TOLD_FILE)
            if not launch:
                self._inbound = self._settings_inbound()
            self._apply(self.source.read(), strict=launch)
        except BaseException:
            self.release_all()
            if made:
                self._remove_home()
            raise

    def poll(self) -> bool:
        """Apply the desired membership again if it changed since it was last read; whether
        anything was applied. A file that is refused leaves the seats as they are."""
        told = _stamp_of(self.home / membership.TOLD_FILE)
        if told != self._told_stamp and self._want is not None:
            self._told_stamp = told
            self._apply(self._want, strict=False)
            return True
        stamp = self.source.stamp()
        if stamp == self._stamp:
            return False
        self._stamp = stamp
        try:
            want = self.source.read()
        except config.ConfigError as exc:
            self._say(f"the desired membership is refused, so the seats stay as they are: {exc}")
            return False
        if want == self._want:
            return False
        self._apply(want, strict=False)
        return True

    def release_all(self) -> None:
        """Let every lock go, the keeper's own last. The links stay, as after a death."""
        for lock, _ in self._held.values():
            lock.release()
        self._held.clear()
        if self._keeper_lock is not None:
            self._keeper_lock.release()
            self._keeper_lock = None

    # Applying a membership.

    def _apply(self, want: Want, *, strict: bool) -> None:
        wanted = {ref.team: ref for ref in want.refs}
        for ref in list(self._held):
            if wanted.get(ref.team) != ref:
                self._drop(ref)
        refused: list[membership.Refused] = []
        taken: list[SeatRef] = []
        try:
            for ref in want.refs:
                if ref in self._held:
                    continue
                try:
                    self._take(ref)
                except SeatRefused as refusal:
                    if strict:
                        raise refusal.error from None
                    self._say(f"seat {ref.text} not taken ({refusal.why}): {refusal.error}")
                    refused.append(membership.Refused(ref.team, ref.seat, refusal.why, refusal.holder))
                    continue
                taken.append(ref)
            if strict:
                config.check_distinct(tuple(member for _, member in self._held.values()), uid=self._uid)
        except BaseException:
            for ref in taken:
                self._drop(ref)
            raise
        self._clear_stray_links()
        self._want = want
        if strict and self._inbound is None:
            self._inbound = bool(self._held)
        seats = tuple(membership.AppliedSeat(ref.team, ref.seat, self._held[ref][1].handle)
                      for ref in want.refs if ref in self._held)
        self._publish(membership.Applied(want.generation, seats, tuple(refused), self._told(),
                                         self._inbound), want.session, strict)
        if seats and not strict:
            self._wake()

    def _take(self, ref: SeatRef) -> None:
        check_seat_dir(self.root, ref)
        try:
            lock = inbox.acquire_lock_at(lock_path(self.root, ref), inbox.SEAT_KIND,
                                         claimed_ms=self._clock_ms(), sleep=self._sleep)
        except inbox.Busy as exc:
            raise SeatRefused(ref, "held-elsewhere", SeatHeld(ref), exc.holder) from None
        except inbox.StateError as exc:
            raise SeatRefused(ref, "bundle-invalid", config.ConfigError(str(exc))) from None
        try:
            self._link(ref)
            member = config.load_bundle(self.home, ref.team, uid=self._uid, user_home=self._user_home)
        except (config.ConfigError, OSError) as exc:
            self._unlink(ref)
            lock.release()
            error = exc if isinstance(exc, config.ConfigError) else config.ConfigError(
                f"seat {ref.text}: the session home's link was not made: {exc.strerror or exc}")
            raise SeatRefused(ref, "bundle-invalid", error) from None
        self._held[ref] = (lock, member)

    def _drop(self, ref: SeatRef) -> None:
        lock, _ = self._held.pop(ref)
        self._unlink(ref)
        lock.release()

    def _link(self, ref: SeatRef) -> None:
        """The home's link for the seat's team, made atomically: a temporary link, then a rename."""
        temp = self.home / f".{ref.team}.{secrets.token_hex(8)}.tmp"
        os.symlink(seat_dir(self.root, ref), temp)
        try:
            os.replace(temp, self.home / ref.team)
        except BaseException:
            os.unlink(temp)
            raise

    def _unlink(self, ref: SeatRef) -> None:
        link = self.home / ref.team
        if link.is_symlink() and os.readlink(link) == str(seat_dir(self.root, ref)):
            link.unlink()

    def _clear_stray_links(self) -> None:
        """A team link no held seat backs (left by a keeper that died) goes."""
        teams = {ref.team for ref in self._held}
        for entry in os.scandir(self.home):
            if entry.is_symlink() and protocol.is_team_name(entry.name) and entry.name not in teams:
                os.unlink(entry.path)

    def _told(self) -> membership.Told | None:
        """`told` is not the keeper's (H12, H13): the watcher and the prompt hook write
        `told.json`, and the keeper copies the roles it says into what it publishes."""
        try:
            current = membership.read_told(self.home)
        except membership.MembershipError as exc:
            self._say(f"what the session was told is unreadable: {exc}")
            return None
        return None if current is None else current.said()

    def _settings_inbound(self) -> bool | None:
        """For a keeper that adopts a home (Repair): whether the session's settings, written
        once at launch, accept team messages; None when they cannot be read."""
        try:
            data = json.loads((self.home / SESSION_SETTINGS).read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return None
        return isinstance(data, dict) and data.get("crossSessionInbound") == "accept"

    def _wake(self) -> None:
        """H11: a seat is applied, so start the session's watcher when it has a socket (the
        SessionStart hook recorded it) and no watcher holds its watch lock. The hooks' revive
        stays the backstop; a failure here is one log line."""
        if self._spawn_watcher is None:
            return
        session = notify.recorded_socket(self.home)
        if session is None or not notify.socket_present(session[0]):
            return
        marks = inject.Marks(self.home, session[0])
        try:
            if inbox.probe_lock_at(marks.watch_lock_path()) is not None:
                return
            env = dict(self._environ or {})
            env.update({"PINGBUS_HOME": str(self.home), notify.SOCKET_ENV: session[0],
                        notify.TOKEN_ENV: session[1]})
            self._spawn_watcher(env, self.home / WATCH_LOG)
            marks.set_spawned(self._clock_ms())
        except (OSError, inbox.StateError) as exc:
            self._say(f"the watcher was not started: {exc}")

    def _publish(self, applied: membership.Applied, session: str | None, strict: bool) -> None:
        """The applied file and the flag file; then, when the host named the session, the
        report beside the checkout's seats (3.3), which is never authority."""
        try:
            membership.write_applied(self.home / membership.APPLIED_FILE, applied)
            self._flag(bool(applied.seats))
        except OSError as exc:
            if strict:
                raise config.ConfigError(f"the session home {self.home}: {exc.strerror or exc}") from None
            self._say(f"the applied membership was not written: {exc.strerror or exc}")
        if session is None:
            return
        report = self.root.parent / SESSIONS_DIR / session
        try:
            os.makedirs(report, mode=SESSION_HOME_MODE, exist_ok=True)
            membership.write_applied(report / membership.APPLIED_FILE, applied)
        except OSError as exc:
            self._say(f"the report for the host was not written: {exc.strerror or exc}")

    def _flag(self, on: bool) -> None:
        path = self.home / membership.FLAG_FILE
        if on:
            os.close(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600))
        elif os.path.lexists(path):
            os.unlink(path)

    # The home.

    def _make_home(self, launch: bool) -> bool:
        """Whether this keeper made the home. A launch must make it; Repair adopts it."""
        try:
            os.mkdir(self.home, SESSION_HOME_MODE)
        except FileExistsError:
            if launch:
                raise config.ConfigError(
                    f"the session home {self.home} already exists: a session claims its seats once"
                ) from None
            _real_dir(self.home)
            return False
        os.chmod(self.home, SESSION_HOME_MODE)
        return True

    def _remove_home(self) -> None:
        for name in (KEEPER_LOCK, membership.APPLIED_FILE, membership.FLAG_FILE):
            if os.path.lexists(self.home / name):
                os.unlink(self.home / name)
        self.home.rmdir()

    def _say(self, text: str) -> None:
        self._err.write(f"pingbus seat keep: {text}\n")
        self._err.flush()


def _stamp_of(path: pathlib.Path) -> object:
    """A file's inode, modification time and size: what tells a rewrite; None when absent."""
    try:
        info = os.stat(path, follow_symlinks=False)
    except FileNotFoundError:
        return None
    return (info.st_ino, info.st_mtime_ns, info.st_size)


def keep(keeper: Keeper, *, parent: int | None, getppid: Callable[[], int] = os.getppid,
         sleep: Callable[[float], object] = time.sleep, tick_s: float = KEEP_TICK_S) -> int:
    """Run a started keeper until its parent changes (with `parent`; else until it is
    killed, which in a container is the container's end), then let every seat go."""
    try:
        while parent is None or getppid() == parent:
            keeper.poll()
            sleep(tick_s)
    finally:
        keeper.release_all()
    return 0


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
