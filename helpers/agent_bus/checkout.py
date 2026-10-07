"""A ccy checkout's seats, on the host: `agent-bus seat check|take|list|remove`.

Spec: Plan 00161 DESIGN.md section 5.6 ("Seat commands"), with section 5.5's list rules,
layout and reuse, and section 5.2's `<host>` rule (D49). Run by the checkout's owner as
themselves, in the checkout (the working directory's git top level); the `agent-bus`
wrapper refuses them as root, since root never writes in a user's checkout. The only way
to the root side is `sudo agent-bus add-member|park-member`. `add-member` is asked for the
bundle on stdout (`--out=-`, D61): a sibling seat's session can rewrite the checkout, so
`take` places the files itself, as the user, through directories it opened without
following a symlink (`claim_seat_dir`, `SeatClaim.place`).

Pure: `role_from_env_local` (the `HOOKS_DAEMON_HOSTNAME` that `ccy.env.local` assigns,
read by parsing, never by sourcing), `seat_host` (that role normalised, else `local`),
`parse_bus_address` (`agentbus0`'s one address from `ip -j addr`), and `take_actions` /
`remove_actions` (the `agent-bus` calls an observed state needs). The rest is the thin
executor: `System` carries the host's side (which teams run here, the bus address,
`sudo agent-bus`), and the four commands observe the checkout and run the actions.

Streams: stdout is the payload (the canonical list, `CHANGED` lines, `SEAT` lines);
diagnostics go to stderr through `say`. Exit codes come from the exceptions: 64 for a
malformed list (`seat.SeatListError`), 75 for a held seat (`seat.SeatHeld`), 78 for
everything this host or checkout cannot do (`CheckoutError`).
"""

from __future__ import annotations

import dataclasses
import io
import json
import os
import pathlib
import re
import shutil
import stat
import subprocess
import tarfile
import time
from collections.abc import Callable, Sequence

from helpers.agent_bus import registry
from helpers.pingbus import config, inbox, seat

ROLE_VAR = "HOOKS_DAEMON_HOSTNAME"
#: A seat's `<host>` when the checkout assigns no role (D49): names no machine.
DEFAULT_HOST = "local"
MEMBER_TYPE = "podman"
MEMBER_ROLE = "worker"
BUS_IFACE = "agentbus0"
#: The root wrapper, by path: sudo's own PATH is not the caller's.
WRAPPER = "/usr/local/bin/agent-bus"
DIR_MODE = 0o700

CCY_DIR = pathlib.PurePosixPath(".claude/ccy")
ENV_LOCAL = CCY_DIR / "ccy.env.local"
PINGBUS_DIR = CCY_DIR / "pingbus"

_VALUE = r"[A-Za-z0-9._-]*"
_ASSIGN_RE = re.compile(
    rf"[ \t]*(?:export[ \t]+)?{ROLE_VAR}="
    rf"(?:(?P<bare>{_VALUE})|\"(?P<dq>{_VALUE})\"|'(?P<sq>{_VALUE})')"
    r"(?:[ \t]*|[ \t]+#.*)"
)
_MENTION_RE = re.compile(rf"\b{ROLE_VAR}\b")


class CheckoutError(Exception):
    """This host or checkout cannot do what was asked (exit 78)."""

    EXIT_CODE = 78


# ── the role and the bus address (pure) ──────────────────────────────────────────────────


def role_from_env_local(text: str) -> str | None:
    """The value `ccy.env.local` assigns `HOOKS_DAEMON_HOSTNAME`, or None (no assignment, or
    an empty one). Only a plain assignment of a literal is read; any other non-comment line
    naming the variable, which only running the file could resolve, is refused, and so is a
    second assignment."""
    found: list[tuple[int, str]] = []
    for number, line in enumerate(text.splitlines(), start=1):
        if line.lstrip(" \t").startswith("#") or not _MENTION_RE.search(line):
            continue
        match = _ASSIGN_RE.fullmatch(line)
        if match is None:
            raise CheckoutError(
                f"{ENV_LOCAL} line {number} names {ROLE_VAR} in a form that only running the file "
                f"could read; write it as `export {ROLE_VAR}=<role>`"
            )
        found.append((number, match.group("bare") or match.group("dq") or match.group("sq") or ""))
    if len(found) > 1:
        raise CheckoutError(
            f"{ENV_LOCAL} assigns {ROLE_VAR} twice (lines {found[0][0]} and {found[1][0]}); keep one"
        )
    if not found:
        return None
    return found[0][1] or None


def seat_host(role: str | None) -> str:
    """A new seat's `<host>`: the role normalised as protocol §3 says, else `local`."""
    if not role:
        return DEFAULT_HOST
    try:
        return registry.resolve_host(None, role)
    except registry.HandleError as exc:
        raise CheckoutError(f"{ENV_LOCAL}: {ROLE_VAR}: {exc}") from None


def parse_bus_address(ip_json: str) -> str:
    """The one address (not link-scoped) that `ip -j addr show dev agentbus0` lists."""
    try:
        data = json.loads(ip_json)
        found = [info["local"] for link in data for info in link.get("addr_info", ())
                 if info.get("scope") != "link"]
    except (ValueError, TypeError, KeyError, AttributeError):
        raise CheckoutError(f"`ip -j addr show dev {BUS_IFACE}` printed something unreadable") from None
    if len(found) != 1:
        raise CheckoutError(
            f"{BUS_IFACE} carries {len(found)} addresses ({', '.join(found) or 'none'}); the bus "
            "address is exactly one, set by play-agent-bus.yml (agent_bus_address)"
        )
    return found[0]


# ── the actions for an observed state (pure) ─────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Observed:
    """One named seat as found: its directory, its lock, its bundle's handle."""

    ref: seat.SeatRef
    exists: bool
    held: bool
    handle: str | None


@dataclasses.dataclass(frozen=True)
class Identity:
    """What a seat's handle is built from besides its name."""

    repo: str
    host: str

    def handle(self, ref: seat.SeatRef) -> str:
        try:
            return registry.build_handle(self.repo, ref.seat, self.host, MEMBER_TYPE)
        except registry.HandleError as exc:
            raise CheckoutError(f"seat {ref.text}: {exc}") from None


@dataclasses.dataclass(frozen=True)
class AddMember:
    ref: seat.SeatRef
    handle: str
    argv: tuple[str, ...]


@dataclasses.dataclass(frozen=True)
class ParkMember:
    ref: seat.SeatRef
    handle: str
    argv: tuple[str, ...]
    #: Whether the seat's directory is there to delete once the handle is parked.
    delete: bool


def _refuse_held(observed: Sequence[Observed]) -> None:
    for seen in observed:
        if seen.held:
            raise seat.SeatHeld(seen.ref)


def take_actions(observed: Sequence[Observed],
                 needs: Callable[[], tuple[Identity, str]]) -> tuple[AddMember, ...]:
    """One `add-member` per named seat with no directory (a new member, or a parked one
    returning: the root side tells them apart), none for a seat that exists, and none at
    all when one is held. `needs` gives the identity and the bus address, asked only when
    a seat is to be created. Each call asks for the bundle on stdout (`--out=-`), which
    `take` places itself: root never writes in the checkout (D61)."""
    _refuse_held(observed)
    for seen in observed:
        if seen.exists and seen.handle is None:
            raise CheckoutError(
                f"seat {seen.ref.text} has a directory but no bundle (member.json): run "
                f"`agent-bus seat remove {seen.ref.text}`, then launch again"
            )
    missing = [seen.ref for seen in observed if not seen.exists]
    if not missing:
        return ()
    identity, address = needs()
    return tuple(
        AddMember(ref, identity.handle(ref), (
            "add-member", ref.team, f"--repo={identity.repo}", f"--seat={ref.seat}",
            f"--host={identity.host}", f"--type={MEMBER_TYPE}", f"--role={MEMBER_ROLE}",
            f"--address={address}", "--out=-"))
        for ref in missing
    )


# ── placing a bundle, as the user, by open directories (D61) ─────────────────────────────

#: The bundle's files, in the order they are written: `member.json` last, so a seat whose
#: placement stopped part way has no bundle and `take` names the way back.
BUNDLE_ORDER = (config.TOKEN_FILE, "README", config.MEMBER_FILE)
BUNDLE_FILE_MAX_BYTES = config.MEMBER_FILE_MAX_BYTES
FILE_MODE = 0o600
_DIR_FLAGS = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC
_NEW_FILE_FLAGS = os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC


def read_bundle(data: bytes) -> dict[str, bytes]:
    """The bundle `add-member --out=-` printed: a tar of exactly `member.json`, `token` and
    `README`, each a regular file of bounded size; anything else is refused."""
    try:
        with tarfile.open(fileobj=io.BytesIO(data), mode="r:") as tar:
            members = tar.getmembers()
            names = sorted(member.name for member in members)
            if names != sorted(BUNDLE_ORDER):
                raise CheckoutError(f"the bundle holds {', '.join(names) or 'nothing'}, not "
                                    f"{', '.join(sorted(BUNDLE_ORDER))}")
            files = {}
            for member in members:
                if not member.isreg() or member.size > BUNDLE_FILE_MAX_BYTES:
                    raise CheckoutError(f"the bundle's {member.name} is not a regular file of at most "
                                        f"{BUNDLE_FILE_MAX_BYTES} bytes")
                handle = tar.extractfile(member)
                if handle is None:
                    raise CheckoutError(f"the bundle's {member.name} cannot be read")
                files[member.name] = handle.read()
            return files
    except tarfile.TarError as exc:
        raise CheckoutError(f"the bundle is not a readable tar ({exc})") from None


def _open_dir(name: str, parent_fd: int | None, uid: int, *, create: bool) -> int:
    """The directory `name` under `parent_fd` (an absolute path when None), opened without
    following a symlink and refused unless owned by `uid`; made (0700) first when `create`
    and missing."""
    made = False
    if create:
        try:
            os.mkdir(name, DIR_MODE, dir_fd=parent_fd)
            made = True
        except FileExistsError:
            made = False
    try:
        fd = os.open(name, _DIR_FLAGS, dir_fd=parent_fd)
    except FileNotFoundError:
        raise CheckoutError(f"{name} is missing from the seats' tree") from None
    except OSError as exc:  # ELOOP: a symlink; ENOTDIR: not a directory
        raise CheckoutError(f"{name} is a symlink or not a directory ({exc.strerror}); the seats' "
                            "tree must be real directories") from None
    info = os.fstat(fd)
    if info.st_uid != uid:
        os.close(fd)
        raise CheckoutError(f"{name} is owned by uid {info.st_uid}, not {uid}; the seats' tree must be yours")
    if made:
        os.fchmod(fd, DIR_MODE)
    return fd


@dataclasses.dataclass
class SeatClaim:
    """A seat's directory, newly made by this `take` and held open with its parent."""

    ref: seat.SeatRef
    parent_fd: int
    fd: int

    def place(self, data: bytes) -> None:
        """Write the bundle into the claimed directory: each file new, 0600, by the open
        directory, never by a path another session could re-point."""
        files = read_bundle(data)
        for name in BUNDLE_ORDER:
            fd = os.open(name, _NEW_FILE_FLAGS, FILE_MODE, dir_fd=self.fd)
            try:
                os.fchmod(fd, FILE_MODE)
                view = memoryview(files[name])
                while view:
                    view = view[os.write(fd, view):]
                os.fsync(fd)
            finally:
                os.close(fd)

    def abandon(self) -> None:
        """Remove the directory again (empty: nothing was placed)."""
        os.rmdir(self.ref.seat, dir_fd=self.parent_fd)

    def close(self) -> None:
        os.close(self.fd)
        os.close(self.parent_fd)


def claim_seat_dir(top: pathlib.Path, ref: seat.SeatRef, uid: int) -> SeatClaim:
    """Make `ref`'s directory, walking from the checkout one open directory at a time
    (no symlink followed, each owned by `uid`) and making the seats' tree below
    `.claude/ccy/` (0700) on the way; refused when the seat's directory is already there."""
    parts = (*seat.CHECKOUT_SEATS.parts, ref.team)
    fd = _open_dir(str(top), None, uid, create=False)
    try:
        for index, part in enumerate(parts):
            child = _open_dir(part, fd, uid, create=index >= len(CCY_DIR.parts))
            os.close(fd)
            fd = child
        try:
            os.mkdir(ref.seat, DIR_MODE, dir_fd=fd)
        except FileExistsError:
            raise CheckoutError(f"seat {ref.text}'s directory appeared while it was being taken "
                                "(another launch?): launch again") from None
        seat_fd = _open_dir(ref.seat, fd, uid, create=False)
        os.fchmod(seat_fd, DIR_MODE)
    except BaseException:
        os.close(fd)
        raise
    return SeatClaim(ref, fd, seat_fd)


def remove_actions(observed: Sequence[Observed], needs: Callable[[], Identity]) -> tuple[ParkMember, ...]:
    """One `park-member` per named seat, none when one is held. The handle is the one the
    seat's bundle names; a seat whose directory or bundle is lost parks the handle the
    checkout's rule builds, so a lost bundle is recovered by removing the seat and
    launching again (section 5.5)."""
    _refuse_held(observed)
    actions = []
    for seen in observed:
        handle = seen.handle or needs().handle(seen.ref)
        actions.append(ParkMember(seen.ref, handle, ("park-member", seen.ref.team, handle), seen.exists))
    return tuple(actions)


# ── the executor ─────────────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class System:
    """Where the command runs and the host's side of it."""

    cwd: pathlib.Path
    uid: int
    team_active: Callable[[str], bool]
    bus_address: Callable[[], str]
    #: `sudo agent-bus ARGV` (`sudo -n` when no prompt is allowed): its exit code and stdout
    #: (bytes: `add-member --out=-` prints the bundle's tar).
    agent_bus: Callable[[Sequence[str], bool], tuple[int, bytes]]
    clock_ms: Callable[[], int]


def hs_unit(team: str) -> str:
    return f"agent-bus-hs@{team}.service"


def _systemctl_active(team: str) -> bool:
    try:
        run = subprocess.run(["systemctl", "is-active", "--quiet", hs_unit(team)], check=False)
    except FileNotFoundError:
        raise CheckoutError("systemctl is not installed: a team's homeserver cannot be checked") from None
    return run.returncode == 0


def _ip_bus_address() -> str:
    try:
        run = subprocess.run(["ip", "-j", "addr", "show", "dev", BUS_IFACE],
                             capture_output=True, text=True, check=False)
    except FileNotFoundError:
        raise CheckoutError("ip is not installed: the bus address cannot be read") from None
    if run.returncode != 0:
        raise CheckoutError(
            f"{BUS_IFACE} is absent, so there is no bus address for a ccy seat: set "
            "agent_bus_address and run play-agent-bus.yml"
        )
    return parse_bus_address(run.stdout)


def _sudo_agent_bus(argv: Sequence[str], no_prompt: bool) -> tuple[int, bytes]:
    command = ["sudo", *(["-n"] if no_prompt else []), WRAPPER, *argv]
    try:
        run = subprocess.run(command, stdout=subprocess.PIPE, check=False)
    except FileNotFoundError:
        raise CheckoutError("sudo is not installed") from None
    return run.returncode, run.stdout


def real_system(cwd: pathlib.Path | None = None) -> System:
    return System(cwd=pathlib.Path.cwd() if cwd is None else cwd, uid=os.getuid(),
                  team_active=_systemctl_active, bus_address=_ip_bus_address,
                  agent_bus=_sudo_agent_bus, clock_ms=lambda: time.time_ns() // 1_000_000)


def _git(cwd: pathlib.Path, *args: str) -> subprocess.CompletedProcess:
    try:
        return subprocess.run(["git", "-C", str(cwd), *args], capture_output=True, text=True,
                              check=False, env={**os.environ, "LC_ALL": "C"})
    except FileNotFoundError:
        raise CheckoutError("git is not installed: a checkout cannot be found") from None


def _real_dir(path: pathlib.Path, uid: int, what: str) -> bool:
    """False when `path` is missing; refused unless a real directory owned by `uid`."""
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        return False
    if stat.S_ISLNK(info.st_mode):
        raise CheckoutError(f"{path} is a symlink; {what} must be a real directory")
    if not stat.S_ISDIR(info.st_mode):
        raise CheckoutError(f"{path} is not a directory; {what} must be one")
    if info.st_uid != uid:
        raise CheckoutError(f"{path} is owned by uid {info.st_uid}, not {uid}; {what} must be yours")
    return True


def find_checkout(system: System) -> pathlib.Path:
    """The working directory's git top level, owned by the user, with ccy's `.claude/ccy/`."""
    top = _git(system.cwd, "rev-parse", "--show-toplevel")
    if top.returncode != 0:
        raise CheckoutError(
            f"{system.cwd} is not in a git checkout: run agent-bus seat in the ccy checkout "
            f"({top.stderr.strip()})"
        )
    path = pathlib.Path(top.stdout.strip())
    _real_dir(path, system.uid, "the checkout")
    if not _real_dir(path / CCY_DIR, system.uid, "ccy's directory"):
        raise CheckoutError(f"{path} has no {CCY_DIR}/: launch ccy in it once, then again with --teams")
    return path


def _check_ignored(top: pathlib.Path) -> None:
    """P8's rule: the bundles' tree is ignored by git before a token can land in it."""
    run = _git(top, "check-ignore", "-q", f"{PINGBUS_DIR}/")
    if run.returncode == 1:
        raise CheckoutError(
            f"git check-ignore says {PINGBUS_DIR}/ is not ignored in {top}, so a token written there "
            "could be committed: ccy's .claude/ccy/.gitignore must ignore it"
        )
    if run.returncode != 0:
        raise CheckoutError(f"git check-ignore failed: {run.stderr.strip()}")


def _seat_tree(top: pathlib.Path, uid: int, teams: Sequence[str]) -> list[pathlib.Path]:
    """The tree a seat's directory sits in, top down; each present one checked."""
    root = seat.checkout_seats_root(top)
    tree = [top / PINGBUS_DIR, root, *dict.fromkeys(root / team for team in teams)]
    for path in tree:
        _real_dir(path, uid, "the seats' tree")
    return tree


def _repo(top: pathlib.Path) -> str:
    remote = _git(top, "remote", "get-url", "origin")
    if remote.returncode not in (0, 2):  # git: 2 is "no such remote"
        raise CheckoutError(f"git remote get-url origin failed: {remote.stderr.strip()}")
    try:
        return registry.repo_from_remote(remote.stdout.strip() if remote.returncode == 0 else None,
                                         str(top))
    except registry.HandleError as exc:
        raise CheckoutError(str(exc)) from None


def checkout_host(top: pathlib.Path, uid: int) -> str:
    """The `<host>` of a seat created in `top`: from its `ccy.env.local` (a regular file
    owned by the user, never a symlink), else `local`."""
    path = top / ENV_LOCAL
    try:
        info = os.lstat(path)
    except FileNotFoundError:
        return DEFAULT_HOST
    if stat.S_ISLNK(info.st_mode):
        raise CheckoutError(f"{path} is a symlink; it must be a regular file")
    if not stat.S_ISREG(info.st_mode):
        raise CheckoutError(f"{path} is not a regular file")
    if info.st_uid != uid:
        raise CheckoutError(f"{path} is owned by uid {info.st_uid}, not {uid}")
    try:
        text = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        raise CheckoutError(f"{path} is not UTF-8 text") from None
    return seat_host(role_from_env_local(text))


def _observe(root: pathlib.Path, refs: Sequence[seat.SeatRef], uid: int) -> tuple[Observed, ...]:
    found = []
    for ref in refs:
        directory = seat.seat_dir(root, ref)
        exists = _real_dir(directory, uid, "a seat")
        handle = None
        if exists and os.path.lexists(directory / config.MEMBER_FILE):
            handle = config.read_member(ref.team, directory).handle
        found.append(Observed(ref, exists, exists and seat.seat_state(root, ref) == seat.HELD, handle))
    return tuple(found)


def _parse_and_check_teams(text: str, system: System) -> tuple[seat.SeatRef, ...]:
    refs = seat.parse_seat_list(text)
    for ref in refs:
        if not system.team_active(ref.team):
            raise CheckoutError(
                f"team `{ref.team}` has no homeserver running on this host ({hs_unit(ref.team)} is "
                "not active): a launch names only a team hosted here; create it with "
                "play-agent-bus.yml (agent_bus_teams) or sudo agent-bus-install team --team-file <file>"
            )
    return refs


def check(text: str, system: System) -> str:
    """`seat check`: the canonical list, when every team runs here. Changes nothing."""
    return seat.canonical(_parse_and_check_teams(text, system))


def take(text: str, system: System, *, no_prompt: bool, say: Callable[[str], None],
         emit: Callable[[str], None]) -> None:
    """`seat take`: create every named seat that has no directory; refuse a held one."""
    refs = _parse_and_check_teams(text, system)
    top = find_checkout(system)
    _seat_tree(top, system.uid, [ref.team for ref in refs])
    _check_ignored(top)
    root = seat.checkout_seats_root(top)
    observed = _observe(root, refs, system.uid)

    def needs() -> tuple[Identity, str]:
        return Identity(_repo(top), checkout_host(top, system.uid)), system.bus_address()

    actions = take_actions(observed, needs)
    for action in actions:
        claim = claim_seat_dir(top, action.ref, system.uid)
        try:
            say(f"creating seat {action.ref.text} as {action.handle} in team {action.ref.team}")
            code, bundle = system.agent_bus(action.argv, no_prompt)
            if code != 0:
                claim.abandon()
                hint = ("; a launch with no prompt uses only sudo's cached credential: run `sudo -v`, "
                        "then launch again") if no_prompt else ""
                raise CheckoutError(
                    f"seat {action.ref.text} could not be created: sudo agent-bus add-member exited "
                    f"{code}, for the reason above{hint}"
                )
            try:
                claim.place(bundle)
            except (CheckoutError, OSError) as exc:
                raise CheckoutError(
                    f"seat {action.ref.text} ({action.handle}) was added but its bundle could not be "
                    f"placed: {exc}; run `agent-bus seat remove {action.ref.text}`, then launch again"
                ) from None
        finally:
            claim.close()
        emit(f"CHANGED\tseat {action.ref.text} {action.handle}")


def seat_list(system: System) -> tuple[list[str], list[str]]:
    """`seat list`: a `SEAT` line per seat of the checkout, and the seats that cannot be read."""
    top = find_checkout(system)
    return seat.seat_lines(seat.checkout_seats_root(top), None)


def _prune(tree: Sequence[pathlib.Path]) -> None:
    """Remove each directory of the seats' tree left empty, deepest first."""
    for path in reversed(tree):
        if path.is_dir() and not any(path.iterdir()):
            path.rmdir()


def remove(text: str, system: System, *, say: Callable[[str], None], emit: Callable[[str], None]) -> None:
    """`seat remove`: park each named seat's handle, delete its directory, and remove what
    is left empty up to `.claude/ccy/pingbus/`. Each seat's lock is held while it goes."""
    refs = _parse_and_check_teams(text, system)
    top = find_checkout(system)
    tree = _seat_tree(top, system.uid, [ref.team for ref in refs])
    root = seat.checkout_seats_root(top)
    observed = _observe(root, refs, system.uid)
    actions = remove_actions(observed, lambda: Identity(_repo(top), checkout_host(top, system.uid)))
    locks: list[inbox.Lock] = []
    try:
        for action in actions:
            if action.delete:
                try:
                    locks.append(inbox.acquire_lock_at(seat.lock_path(root, action.ref), inbox.SEAT_KIND,
                                                       claimed_ms=system.clock_ms()))
                except inbox.Busy:
                    raise seat.SeatHeld(action.ref) from None
        for action in actions:
            say(f"parking {action.handle} in team {action.ref.team}")
            code, _ = system.agent_bus(action.argv, False)
            if code != 0:
                raise CheckoutError(
                    f"seat {action.ref.text} was not removed: sudo agent-bus park-member exited "
                    f"{code}, for the reason above"
                )
            if action.delete:
                shutil.rmtree(seat.seat_dir(root, action.ref))
            emit(f"CHANGED\tseat {action.ref.text} {action.handle}")
    finally:
        for lock in locks:
            lock.release()
    _prune(tree)
