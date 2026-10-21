"""Existing LXC containers joined to agent teams by IaC: `agent_bus_lxc_members`.

Plan 00161. play-agent-bus.yml runs this on the homeserver host, as root: `--phase absent`
before any team is removed, `--phase present` after the teams are installed. Each entry is
one agent user in one existing LXC container (never made here), in one team hosted on this
host. A present entry gets README.lxc's steps (files/opt/claude-yolo/optional/agent-bus/),
in its order, after README.lxc's requirement is checked for every container before any change
(gh installed and logged in to github.com for the agent user; never provided here):

  1. the kit, /usr/local/share/agent-bus/kit, copied into the container (only what differs
     is written) and its `pingbus` and `agent-bus-claude` linked into /usr/local/bin;
  2. the container's source address towards the bus address must be on a bridge of this
     host and inside the team's `allow_from` (the play adds the default LXC bridge's subnet
     to every team): refused otherwise, naming the subnet to add;
  3. `HOOKS_DAEMON_HOSTNAME=<host> pingbus suggest-handle`, as the agent user in its
     checkout, gives `--repo` and `--host`, and must say `--type=lxc`;
  4. `agent-bus add-member ... --seat=<seat> --out=-`, the bundle placed by the agent user
     as ~/.config/pingbus/<team>/ (directory 0700, files 0600);
  5. ~/.config/pingbus/env lists the team in PINGBUS_TEAMS and sets HOOKS_DAEMON_HOSTNAME;
  6. `pingbus config check` as the agent user, on every run;
  7. with `service: true` (Plan 00203 gap 9), the session supervised inside the container: the
     system unit `agent-bus-claude-<user>.service` (render_member_unit), enabled in the
     container's multi-user.target, runs `agent-bus-claude` as the agent user in its checkout
     under tmux (socket `agent-bus-claude`, so a person can attach) and restarts it on any exit.
     It needs the container to start at host boot (`lxc.start.auto = 1`), systemd as its init
     and tmux installed, each checked before any change. Without it, or once the user is in no
     team, the unit is stopped and removed.

The handle `<repo>.<seat>+<host>.lxc` is derived from the repository as ccy derives a podman
seat's (derive.py): `<host>` and `<seat>` are overrides an entry may carry, and are otherwise
read from the checkout, which this play reads from the host's side as plain files, never by
running anything in it. An entry written before they were derived keeps its handle: its
bundle's seat, or the seat its container's name gave, is kept. The handle is known before
anything is made, so a member whose bundle names it, holds a token, matches the
address and `human_text`, and is active in the registry is left alone: no new token per
run. Otherwise the stale bundle goes, an active handle is parked, and `add-member --seat`
returns (or adds) it with a new token. An absent entry parks the handle when its team
runs here, deletes the bundle, and takes the team out of PINGBUS_TEAMS.

A container that is not running is never entered and never fails the play (Plan 00203 Task
2.6, SPEC-lxc-stopped-members.md): after a reboot most are stopped. A present member whose handle
this host knows (the entry's host, seat and repo, or the member record below) and which is an
active member is skipped; any other is deferred to the first run with its container up. An
absent one has its known handle parked from the host's side, and the rest is deferred. Only a
container that does not exist, or a failure in one that runs, fails.

What each phase found is kept in the member record, `<applied>/lxc/<team>.json` (L8), which
ccy-teams reads to say what was deferred, and which gives a stopped member its known handle.

Streams: stdout carries `CHANGED<TAB><what>` lines, then a `SKIPPED` or `DEFERRED<TAB><team>
<TAB><user>@<container><TAB><why>` line for each member held back and one `LXC-SUMMARY<TAB>
<text>` line when there is any; diagnostics go to stderr. Exit 0 done, 64 usage, 78 refused
(`MemberError`).
"""

from __future__ import annotations

import argparse
import dataclasses
import grp
import io
import ipaddress
import json
import pathlib
import re
import subprocess
import sys
import tarfile
import time
from collections.abc import Callable, Collection, Mapping, Sequence
from typing import TextIO, TypeVar

from helpers.agent_bus import checkout, registry, teamfile
from helpers.agent_bus_lxc import applied_record, derive
from helpers.pingbus import protocol

T = TypeVar("T")
PROG = "agent-bus-lxc-members"
MEMBER_TYPE = "lxc"
STATES = ("present", "absent")
DEFAULT_ROLE = "worker"
KIT_PARENT = "/usr/local/share/agent-bus"
GUEST_BIN = "/usr/local/bin"
GUEST_PATH = "/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
#: Under the agent user's home (README.lxc steps 4 and 5).
PINGBUS_DIR = ".config/pingbus"
ENV_PATH = f"~/{PINGBUS_DIR}/env"
TEAMS_VAR = "PINGBUS_TEAMS"
ROLE_VAR = checkout.ROLE_VAR
WRAPPER = checkout.WRAPPER
STATE_ROOT = pathlib.Path("/var/lib/agent-bus")
TEAM_FILE = "team.json"
VARS_FILE = "environment/localhost/host_vars/localhost.yml"
LXC_PLAY = "playbooks/imports/play-lxc-install-config.yml"
EXIT_USAGE = 64
EXIT_REFUSED = 78
RUNNING = "RUNNING"
#: The member record (L8): `<team>.json` here, beside the applied stamps, readable by ccy-teams.
RECORD_DIR = checkout.APPLIED_DIR / "lxc"
RECORD_VERSION = 1
STAMP_GROUP_FILE = pathlib.Path("/etc/agent-bus/stamp-group")
#: A record item's status: joined this run; joined, its container not running (skipped);
#: held back to a later run (deferred); taken off the team.
JOINED, STOPPED, DEFERRED, LEFT = "joined", "stopped", "deferred", "left"

REQUIRED_KEYS = frozenset({"team", "container", "user"})
#: `host` and `seat` are overrides: left out, they are derived from the checkout as ccy
#: derives them (derive.py).
OPTIONAL_KEYS = frozenset({"host", "checkout", "seat", "role", "human_text", "repo", "state", "service"})
GUEST_UNIT_DIR = "/etc/systemd/system"
#: The tmux socket (`tmux -L`) and session the supervised session runs in.
SERVICE_TMUX = "agent-bus-claude"
#: What a rendered unit value may hold: no space, `%` (a systemd specifier), quote or backslash.
_UNIT_VALUE_RE = re.compile(r"/[A-Za-z0-9._/@+-]*")
_CONTAINER_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}")
_USER_RE = re.compile(r"[a-z_][a-z0-9_-]{0,31}")
_ENV_VALUE = r"[A-Za-z0-9,._-]*"
_SUGGEST_RE = re.compile(r"--repo=(\S+) --host=(\S+) --type=(\S+)")

# ── what runs inside the container (`python3 -I -c SCRIPT ARGS...`) ─────────────────────
#
# Each script that creates anything sets its own umask first: lxc-attach hands on whatever
# umask its caller had (077 has been seen, which made the kit untraversable), and a umask also
# governs the parents `os.makedirs` makes, which an explicit mode or chmod does not reach.
# Root's scripts (the kit, the unit) use 022, the agent's (bundle, env file) use 077, and each
# file and leaf directory still names its mode.

#: The member's own source address towards the bus address (no packet is sent).
SOURCE_PY = ("import socket, sys; s = socket.socket(socket.AF_INET6 if ':' in sys.argv[1] else "
             "socket.AF_INET, socket.SOCK_DGRAM); s.connect((sys.argv[1], 9)); print(s.getsockname()[0])")

#: As root: the kit tar on stdin made the container's KIT_PARENT/kit (only what differs is
#: written, what the tar lacks is removed), its two commands linked into BIN. Prints one
#: line per change; a kit directory it repairs to 0755 is also said on stderr.
KIT_SYNC_PY = r'''
import os, stat, sys, tarfile
os.umask(0o022)
if sys.version_info < (3, 11):
    sys.exit("pingbus needs Python 3.11 or later; this container has " + sys.version.split()[0])
parent, bindir, owner = sys.argv[1], sys.argv[2], int(sys.argv[3])
kit = os.path.join(parent, "kit")
changed = []
def directory(path, st):
    # Root replaces files in these directories, so nobody else may change what they hold.
    if not stat.S_ISDIR(st.st_mode):
        sys.exit(path + " is not a directory (a symlink or file there is never followed or "
                 "replaced): remove it, and the play makes the kit")
    mode = stat.S_IMODE(st.st_mode)
    if st.st_uid != owner or mode & 0o022:
        sys.exit(path + " is not owned by uid " + str(owner) + " or is writable by others: a kit "
                 "copied by hand must go before the play installs it (remove " + parent + ")")
    if mode != 0o755:
        # Ours and closed to others (made under a tight umask): the agent must traverse it.
        fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fchmod(fd, 0o755)
        finally:
            os.close(fd)
        note = "repaired " + path + " from " + format(mode, "04o") + " to 0755"
        print(note, file=sys.stderr)
        changed.append(note)
want = {}
with tarfile.open(fileobj=sys.stdin.buffer, mode="r|") as tar:
    for m in tar:
        parts = m.name.split("/")
        if parts[0] != "kit" or "" in parts or ".." in parts or "." in parts:
            sys.exit("the kit tar names " + repr(m.name))
        if m.isdir():
            want[m.name] = None
        elif m.isreg():
            want[m.name] = (tar.extractfile(m).read(), m.mode & 0o777)
        else:
            sys.exit("the kit tar holds " + repr(m.name) + ", not a file or directory")
try:
    st = os.lstat(parent)
except FileNotFoundError:
    os.makedirs(parent, mode=0o755)
    st = os.lstat(parent)
directory(parent, st)
for name in sorted(want):
    path = os.path.join(parent, name)
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        st = None
    if want[name] is None:
        if st is None:
            os.mkdir(path, 0o755)
            os.chmod(path, 0o755)
            changed.append(name)
        else:
            directory(path, st)
        continue
    data, mode = want[name]
    if st is not None and not stat.S_ISREG(st.st_mode):
        sys.exit(path + " is not a regular file")
    if st is not None and stat.S_IMODE(st.st_mode) == mode:
        with open(path, "rb") as f:
            if f.read() == data:
                continue
    tmp = path + ".agent-bus-new"
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC | os.O_NOFOLLOW, mode)
    with os.fdopen(fd, "wb") as f:
        f.write(data)
    os.chmod(tmp, mode)
    os.replace(tmp, path)
    changed.append(name)
for top, dirs, files in os.walk(kit, topdown=False):
    for entry in files + dirs:
        path = os.path.join(top, entry)
        name = os.path.relpath(path, parent)
        if name not in want:
            if os.path.isdir(path) and not os.path.islink(path):
                os.rmdir(path)
            else:
                os.unlink(path)
            changed.append("removed " + name)
for command in ("pingbus", "agent-bus-claude"):
    link, target = os.path.join(bindir, command), os.path.join(kit, command)
    if os.path.islink(link) and os.readlink(link) == target:
        continue
    if os.path.lexists(link):
        sys.exit(link + " exists and is not a link to " + target + "; README.lxc step 1 links it")
    os.symlink(target, link)
    changed.append(link)
for name in changed:
    print(name)
'''

#: As the agent: what its pingbus directory holds for one team, as JSON (never the token).
OBSERVE_PY = r'''
import json, os, stat, sys
base, team = sys.argv[1], sys.argv[2]
def text(path):
    try:
        st = os.lstat(path)
    except FileNotFoundError:
        return None
    if not stat.S_ISREG(st.st_mode) or st.st_size > 65536:
        sys.exit(path + " is not a regular file of at most 65536 bytes")
    with open(path, encoding="utf-8") as f:
        return f.read()
team_dir = os.path.join(base, team)
try:
    st = os.lstat(team_dir)
except FileNotFoundError:
    st = None
if st is not None and not (stat.S_ISDIR(st.st_mode) or stat.S_ISLNK(st.st_mode)):
    sys.exit(team_dir + " is not a directory")
real = st is not None and stat.S_ISDIR(st.st_mode)
member = text(os.path.join(team_dir, "member.json")) if real else None
try:
    token = real and os.lstat(os.path.join(team_dir, "token")).st_size > 0
except FileNotFoundError:
    token = False
print(json.dumps({"dir": st is not None, "member": member, "token": token,
                  "env": text(os.path.join(base, "env"))}))
'''

#: As the agent: the bundle tar on stdin written as BASE/TEAM (0700), each file new, 0600.
PLACE_PY = r'''
import os, sys, tarfile
os.umask(0o077)
base, team = sys.argv[1], sys.argv[2]
names =("token", "README", "member.json")
files = {}
with tarfile.open(fileobj=sys.stdin.buffer, mode="r|") as tar:
    for m in tar:
        if m.name not in names or not m.isreg():
            sys.exit("the bundle holds " + repr(m.name))
        files[m.name] = tar.extractfile(m).read()
if sorted(files) != sorted(names):
    sys.exit("the bundle lacks one of " + ", ".join(names))
os.makedirs(base, mode=0o700, exist_ok=True)
team_dir = os.path.join(base, team)
os.mkdir(team_dir, 0o700)
os.chmod(team_dir, 0o700)
for name in names:
    fd = os.open(os.path.join(team_dir, name), os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    with os.fdopen(fd, "wb") as f:
        f.write(files[name])
'''

#: As the agent: BASE/TEAM gone (a symlink there is removed, never followed).
REMOVE_PY = r'''
import os, shutil, sys
path = os.path.join(sys.argv[1], sys.argv[2])
if os.path.isdir(path) and not os.path.islink(path):
    shutil.rmtree(path)
elif os.path.lexists(path):
    os.unlink(path)
'''

#: As the agent: stdin written to PATH (0600) by a rename, or PATH removed when stdin is empty.
WRITE_ENV_PY = r'''
import os, sys
os.umask(0o077)
path = sys.argv[1]
data = sys.stdin.buffer.read()
if not data:
    if os.path.lexists(path):
        os.unlink(path)
    sys.exit(0)
os.makedirs(os.path.dirname(path), mode=0o700, exist_ok=True)
tmp = path + ".agent-bus-new"
if os.path.lexists(tmp):
    os.unlink(tmp)
fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
with os.fdopen(fd, "wb") as f:
    f.write(data)
os.replace(tmp, path)
'''

#: As root: stdin written to PATH (0644) by a rename when it differs; prints `changed` when it did.
UNIT_WRITE_PY = r'''
import os, sys
os.umask(0o022)
path = sys.argv[1]
data = sys.stdin.buffer.read()
try:
    with open(path, "rb") as f:
        same = f.read() == data and (os.stat(path).st_mode & 0o7777) == 0o644
except FileNotFoundError:
    same = False
if not same:
    os.makedirs(os.path.dirname(path), mode=0o755, exist_ok=True)
    tmp = path + ".agent-bus-new"
    if os.path.lexists(tmp):
        os.unlink(tmp)
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o644)
    with os.fdopen(fd, "wb") as f:
        f.write(data)
    os.chmod(tmp, 0o644)
    os.replace(tmp, path)
    print("changed")
'''

#: As root: PATH removed when it is there; prints `removed` when it was.
UNIT_REMOVE_PY = r'''
import os, sys
if os.path.lexists(sys.argv[1]):
    os.unlink(sys.argv[1])
    print("removed")
'''

#: As root: prints `systemd` when systemd is the container's init (sd_booted's own test).
SYSTEMD_PROBE_PY = r'''
import os
if os.path.isdir("/run/systemd/system"):
    print("systemd")
'''

#: As root, before any change for a `service: true` entry: systemd is the container's init, and
#: tmux's path.
SERVICE_NEEDS_SH = ('test -d /run/systemd/system || { echo "systemd is not the init of this container" >&2; '
                    'exit 3; }; command -v tmux || { echo "tmux is not installed" >&2; exit 4; }')

#: As the agent: README.lxc step 6, with the env file read as agent-bus-claude reads it.
CONFIG_CHECK_SH = f'set -a; . "$HOME/{PINGBUS_DIR}/env"; set +a; exec pingbus config check'
#: As the agent, before any change: README.lxc's requirement, gh logged in to github.com
#: (pingbus's one forge credential, DESIGN.md D66), with the env file read when there is one,
#: as config check reads it (a GH_TOKEN there counts). The token stays inside the container.
GH_CHECK_SH = (f'if [ -f "$HOME/{PINGBUS_DIR}/env" ]; then set -a; . "$HOME/{PINGBUS_DIR}/env"; set +a; fi; '
               "gh auth token --hostname github.com >/dev/null")


class MemberError(Exception):
    """A declared member cannot be brought to its state (exit 78)."""


# ── the entries (pure) ───────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Member:
    team: str
    container: str
    user: str
    #: HOOKS_DAEMON_HOSTNAME inside the container: the install's role, the handle's <host>.
    #: None until resolve_members derives it from the checkout.
    host: str | None
    checkout: str | None
    #: None until resolve_members derives it.
    seat: str | None
    role: str
    human_text: bool
    repo: str | None
    state: str
    #: The session is supervised by a unit inside the container (step 7).
    service: bool = False

    @property
    def label(self) -> str:
        return f"{self.user}@{self.container}"

    def handle(self, repo: str) -> str:
        if self.host is None or self.seat is None:
            raise MemberError(f"{self.label} in team {self.team}: its host and seat are derived from "
                              "the checkout, which has not been read")
        try:
            return registry.build_handle(repo, self.seat, self.host, MEMBER_TYPE)
        except registry.HandleError as exc:
            raise MemberError(f"{self.label} in team {self.team}: {exc}") from None


def _parse_entry(position: int, entry: object) -> Member:
    where = f"agent_bus_lxc_members entry {position}"
    if not isinstance(entry, dict):
        raise MemberError(f"{where} must be a mapping")
    missing = sorted(REQUIRED_KEYS - set(entry))
    unknown = sorted(set(entry) - REQUIRED_KEYS - OPTIONAL_KEYS)
    if missing or unknown:
        raise MemberError(f"{where}: missing {', '.join(missing) or 'nothing'}; unknown "
                          f"{', '.join(map(str, unknown)) or 'nothing'}")
    state = entry.get("state", "present")
    if state not in STATES:
        raise MemberError(f"{where}: state must be present or absent")
    team, container, user = entry["team"], entry["container"], entry["user"]
    if not protocol.is_team_name(team):
        raise MemberError(f"{where}: team must match {protocol.TEAM_NAME_PATTERN}")
    if not isinstance(container, str) or not _CONTAINER_RE.fullmatch(container):
        raise MemberError(f"{where}: container must be an LXC container name")
    if not isinstance(user, str) or not _USER_RE.fullmatch(user):
        raise MemberError(f"{where}: user must be a user name")
    host = entry.get("host")
    if host is not None:
        if not isinstance(host, str):
            raise MemberError(f"{where}: host must be a string")
        try:
            host = registry.resolve_host(host, None)
        except registry.HandleError as exc:
            raise MemberError(f"{where}: host: {exc}") from None
    checkout_path = entry.get("checkout")
    if checkout_path is not None and (not isinstance(checkout_path, str) or not checkout_path.startswith("/")):
        raise MemberError(f"{where}: checkout must be an absolute path inside the container")
    repo = entry.get("repo")
    if repo is not None:
        if not isinstance(repo, str):
            raise MemberError(f"{where}: repo must be a string")
        try:
            repo = registry.normalise_repo(repo)
        except registry.HandleError as exc:
            raise MemberError(f"{where}: repo: {exc}") from None
    if state == "present" and checkout_path is None:
        raise MemberError(f"{where}: a present member needs checkout, where suggest-handle runs")
    if state == "absent" and checkout_path is None and repo is None:
        raise MemberError(f"{where}: an absent member needs checkout or repo, to know its handle")
    seat = entry.get("seat")
    if type(seat) is int:
        seat = str(seat)
    if seat is not None and (not isinstance(seat, str) or not protocol.is_seat_name(seat)):
        raise MemberError(f"{where}: seat must match {protocol.HANDLE_SEAT_PATTERN}")
    role = protocol.normalise_role(entry.get("role", DEFAULT_ROLE))
    if role not in registry.ROLES:
        raise MemberError(f"{where}: role must be one of {', '.join(registry.ROLES)}")
    human_text = entry.get("human_text", True)
    if not isinstance(human_text, bool):
        raise MemberError(f"{where}: human_text must be true or false")
    if state == "present" and role == protocol.ROLE_COORDINATOR and not human_text:
        # Every message the questions room delivers is a human's (Plan 00211 SPEC 4.5).
        raise MemberError(f"{where}: a coordinator must take human text: drop human_text: false")
    service = entry.get("service", False)
    if not isinstance(service, bool):
        raise MemberError(f"{where}: service must be true or false")
    return Member(team, container, user, host, checkout_path, seat, role, human_text, repo, state, service)


def parse_members(data: object) -> tuple[Member, ...]:
    """The entries, each checked, and the rules between them: one entry per user, container
    and team; one host per user and container (they share one env file); one container per
    team, host and seat (else two containers would claim one handle)."""
    if not isinstance(data, list):
        raise MemberError("agent_bus_lxc_members must be a list")
    found = tuple(_parse_entry(position, entry) for position, entry in enumerate(data, start=1))
    _check_between(found)
    return found


def _check_between(found: Sequence[Member]) -> None:
    """The rules between entries. A host or seat not yet derived (None) is not judged; it is,
    once resolve_members has given it a value. A seat is numbered within its `<repo>` and
    `<host>` (the handle is `<repo>.<seat>+<host>.lxc`, as derive_seat counts it), so two
    containers clash on a seat only in one repo, or when either entry's repo is not known."""
    entries: dict[tuple[str, str, str], int] = {}
    hosts: dict[tuple[str, str], str] = {}
    seats: dict[tuple[str, str, str], list[Member]] = {}
    coordinators: dict[str, int] = {}
    sessions: dict[tuple[str, str], Member] = {}
    for position, member in enumerate(found, start=1):
        if member.state == "present":
            first = sessions.setdefault((member.container, member.user), member)
            if first.service != member.service:
                raise MemberError(f"agent_bus_lxc_members: the entries of {member.label} differ on service; "
                                  "the user runs one session for all its teams, so set it alike on each")
            if member.service and first.checkout != member.checkout:
                raise MemberError(f"agent_bus_lxc_members: the entries of {member.label} with service: true "
                                  "name two checkouts; its one supervised session runs in one checkout")
        key = (member.container, member.user, member.team)
        if key in entries:
            raise MemberError(f"agent_bus_lxc_members entries {entries[key]} and {position} both "
                              f"declare {member.label} in team {member.team}")
        entries[key] = position
        if member.host is not None and hosts.setdefault((member.container, member.user), member.host) != member.host:
            raise MemberError(f"agent_bus_lxc_members gives {member.label} two hosts; its env file "
                              f"sets one {ROLE_VAR}")
        if member.host is not None and member.seat is not None:
            holders = seats.setdefault((member.team, member.host, member.seat), [])
            if any(other.container != member.container
                   and (other.repo is None or member.repo is None or other.repo == member.repo)
                   for other in holders):
                raise MemberError(f"agent_bus_lxc_members gives seat {member.seat} of team {member.team} "
                                  f"to two containers; set seat on one")
            holders.append(member)
        if member.state == "present" and member.role == protocol.ROLE_COORDINATOR:
            if coordinators.setdefault(member.team, position) != position:
                raise MemberError(f"agent_bus_lxc_members entries {coordinators[member.team]} and {position} "
                                  f"both declare the coordinator of team {member.team}; a team has one")


def resolve_members(found: Sequence[Member], *, identify: Callable[[Member], derive.Identity],
                    roster: Callable[[str], Mapping[str, bool]],
                    bundle: Callable[[Member], str | None],
                    unheld_ok: bool = False, unheld_teams: Collection[str] = ()) -> tuple[Member, ...]:
    """The present entries with the host and seat they leave out derived, in file order, by
    derive.py's rule. `identify` reads the member's checkout (asked only for an entry that
    leaves one out), `roster` is a team's members (handle -> parked) and `bundle` the handle
    the member's bundle in its container names. An absent entry is left as it is: its bundle
    names its handle. An entry that gives both is untouched, so a run of it changes nothing.

    A derived entry comes back with `repo` set to the checkout's: the one `<repo>` of its
    handle, which `_suggest` then holds the container's own claim to. `unheld_ok` is derive_seat's;
    `unheld_teams` turns it on for those teams alone: each has an entry whose container is not
    running and whose handle is not known here, so an active seat no entry names may be its (L4)."""
    def derived(member: Member, step: Callable[[], T]) -> T:
        try:
            return step()
        except derive.DeriveError as exc:
            raise MemberError(f"{member.label} in team {member.team}: {exc}") from None

    needy = [m for m in found if m.state == "present" and (m.host is None or m.seat is None)]
    identities: dict[int, derive.Identity] = {}
    for member in needy:
        read = derived(member, lambda member=member: identify(member))
        if member.repo is not None and member.repo != read.repo:
            raise MemberError(f"{member.label} in team {member.team}: the entry says repo: {member.repo}, "
                              f"and its checkout's origin gives {read.repo}")
        identities[id(member)] = derive.Identity(read.repo, member.host or read.host)
    # What other entries of a team already hold: explicit seats first, then the seat each derived
    # entry's bundle names (whatever its place in the file), then each seat derived below.
    claimed: list[tuple[str, str, str | None, str]] = []
    for member in found:
        if member.state == "present" and member.seat is not None:
            known = identities.get(id(member))
            claimed.append((member.team, known.host if known else str(member.host),
                            known.repo if known else member.repo, member.seat))
    bundles: dict[int, str | None] = {}
    for member in needy:
        if member.seat is None:
            bundles[id(member)] = bundle(member)
            held = protocol.parse_handle(bundles[id(member)] or "")
            identity = identities[id(member)]
            if held is not None and (held.repo, held.host, held.type) == (identity.repo, identity.host, MEMBER_TYPE):
                claimed.append((member.team, identity.host, identity.repo, held.seat))
    resolved = []
    for member in found:
        identity = identities.get(id(member))
        if identity is None:
            resolved.append(member)
            continue
        seat = member.seat
        if seat is None:
            taken = {s for team, h, repo, s in claimed
                     if team == member.team and h == identity.host and repo in (None, identity.repo)}
            seat = derived(member, lambda member=member, identity=identity, taken=taken: derive.derive_seat(
                identity, member.container, roster(member.team), bundles[id(member)], taken,
                unheld_ok=unheld_ok or member.team in unheld_teams))
            claimed.append((member.team, identity.host, identity.repo, seat))
        resolved.append(dataclasses.replace(member, host=identity.host, seat=seat, repo=identity.repo))
    result = tuple(resolved)
    _check_between(result)
    return result


# ── the decisions (pure) ─────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Found:
    """What the container holds for one team: its directory, member.json (parsed), a token."""

    exists: bool
    member: Mapping | None
    token: bool


@dataclasses.dataclass(frozen=True)
class Plan:
    remove_bundle: bool = False
    park: bool = False
    add: bool = False
    set_role: bool = False


def bundle_handle(member: Mapping) -> str | None:
    """The handle a bundle's member.json names, or None when it names none."""
    user_id = member.get("user_id")
    if not isinstance(user_id, str) or not user_id.startswith("@") or ":" not in user_id:
        return None
    handle = user_id[1:].split(":", 1)[0]
    return handle if protocol.parse_handle(handle) is not None else None


def present_plan(handle: str, url: str, member: Member, found: Found,
                 registered: tuple[str, bool] | None) -> Plan:
    """Nothing for a current member but a role to correct; else a new bundle for `handle`."""
    if found.member is not None:
        held = bundle_handle(found.member)
        if held != handle:
            parsed = protocol.parse_handle(held) if held else None
            hint = (f"set seat: {parsed.seat} on the entry to keep that member"
                    if parsed is not None and registry.prefix_of(held) == registry.prefix_of(handle)
                    else "put back the seat or host that made it")
            raise MemberError(
                f"{member.label}: its bundle for team {member.team} names {held or 'no handle'}, not "
                f"{handle}: {hint}, or set state: absent and run the play before declaring the "
                "member again")
    active = registered is not None and not registered[1]
    if active and found.member is None and protocol.parse_handle(handle).n is not None:
        raise MemberError(
            f"{handle} is an active member of team {member.team}, and {member.label} holds no bundle "
            f"for it: a numbered seat may be another container's, joined by hand, so it is not "
            "parked here. If it is this container's, set the entry's state: absent and run the play "
            "(which parks it), then declare it present again; otherwise set another seat")
    role_differs = registered is not None and registered[0] != member.role
    current = (found.member is not None and found.token and active
               and found.member.get("base_url") == url
               and found.member.get("human_text") is member.human_text)
    if current:
        return Plan(set_role=role_differs)
    return Plan(remove_bundle=found.exists, park=active, add=True, set_role=role_differs)


def absent_plan(found: Found, registered: tuple[str, bool] | None, team_runs: bool) -> Plan:
    return Plan(remove_bundle=found.exists,
                park=team_runs and registered is not None and not registered[1])


@dataclasses.dataclass(frozen=True)
class Seen:
    """What a phase found for one entry: a member record item's status, handle and why."""

    member: Member
    status: str
    handle: str | None
    why: str = ""


@dataclasses.dataclass(frozen=True)
class Report:
    """A member held back because its container is not running: SKIPPED or DEFERRED."""

    kind: str
    team: str
    label: str
    why: str


def report_of(seen: Seen) -> Report | None:
    kinds = {STOPPED: "SKIPPED", DEFERRED: "DEFERRED"}
    if seen.status not in kinds:
        return None
    return Report(kinds[seen.status], seen.member.team, seen.member.label, seen.why)


def summary(reports: Sequence[Report]) -> str:
    """The one line after the members' lines that says what was held back (L7)."""
    words = {"SKIPPED": "skipped", "DEFERRED": "deferred"}
    named = ", ".join(f"{r.label} ({r.team}, {words[r.kind]})" for r in reports)
    return (f"{len(reports)} LXC member(s) not reached, their containers not running: {named}. Nothing "
            "failed; the first run with each container up brings it in line")


def record_item(record: Mapping | None, member: Member) -> Mapping | None:
    """The member record's item for the entry's container and user, if there is one."""
    items = record.get("members") if isinstance(record, Mapping) else None
    for item in items if isinstance(items, list) else ():
        if isinstance(item, Mapping) and (item.get("container"), item.get("user")) == (member.container, member.user):
            return item
    return None


def known_handle(member: Member, record: Mapping | None) -> str | None:
    """The member's handle as this host knows it without entering the container: from the entry
    when it gives host, seat and repo; else from the member record, when the record is for the
    entry's checkout and agrees with whatever host, seat or repo the entry gives."""
    if member.host is not None and member.seat is not None and member.repo is not None:
        return member.handle(member.repo)
    item = record_item(record, member)
    if item is None or (member.checkout is not None and item.get("checkout") != member.checkout):
        return None
    held = protocol.parse_handle(item.get("handle"))
    if held is None or held.type != MEMBER_TYPE:
        return None
    for given, found in ((member.host, held.host), (member.seat, held.seat), (member.repo, held.repo)):
        if given is not None and given != found:
            return None
    return item["handle"]


def with_handle(member: Member, handle: str) -> Member:
    """The entry with the host, seat and repo its known handle gives: a seat that is claimed."""
    held = protocol.parse_handle(handle)
    return dataclasses.replace(member, host=held.host, seat=held.seat, repo=held.repo)


def skipped_why(member: Member, state: str, handle: str) -> str:
    return (f"LXC container {member.container} is {state}: {handle} is already a member of team "
            f"{member.team}, so it is left as it is; the first run with the container up refreshes its kit, "
            "env file and config check")


def deferred_why(member: Member, state: str, handle: str | None) -> str:
    known = (f"and this host has no record that it joined team {member.team}" if handle is None else
             f"and its handle {handle} is not an active member of team {member.team}")
    return (f"LXC container {member.container} is {state}, {known}: start it, then run the play again "
            f"(ccy-teams apply {member.team}) to join it, or choose not to declare it yet")


def merge_record(previous: Mapping | None, team: str, declared: Sequence[Member], seen: Sequence[Seen],
                 now: float) -> dict:
    """The team's member record (L8): one item per entry of the team, in file order; an entry
    this phase saw gets what it found, any other keeps its item while its state is unchanged.
    `since` is kept while an item's status and handle stay."""
    fresh = {(s.member.container, s.member.user): s for s in seen}
    items = []
    for member in declared:
        before = record_item(previous, member)
        found = fresh.get((member.container, member.user))
        if found is None:
            if before is not None and before.get("state") == member.state:
                items.append(dict(before))
            continue
        kept = before is not None and (before.get("status"), before.get("handle")) == (found.status, found.handle)
        items.append({"container": member.container, "user": member.user, "state": member.state,
                      "status": found.status, "handle": found.handle, "checkout": member.checkout,
                      "why": found.why, "since": before["since"] if kept else int(now)})
    return {"version": RECORD_VERSION, "team": team, "members": items}


def parse_record(text: str | None, where: str) -> dict | None:
    """A member record as written here, or None when there is none."""
    if text is None:
        return None
    try:
        record = json.loads(text)
    except ValueError as exc:
        raise MemberError(f"{where} is not JSON ({exc}): the play writes it; remove it and run the play "
                          "again") from None
    if not isinstance(record, dict) or record.get("version") != RECORD_VERSION or not isinstance(
            record.get("members"), list):
        raise MemberError(f"{where} is not a member record of version {RECORD_VERSION}: remove it and run "
                          "the play again")
    return record


def merge_env(text: str | None, *, where: str, add: Sequence[str] = (), remove: Sequence[str] = (),
              host: str | None = None) -> str | None:
    """The env file with `add` appended to PINGBUS_TEAMS, `remove` taken out, and
    HOOKS_DAEMON_HOSTNAME set to `host` (when given); every other line kept. Both lines go
    when no team is left, and None means the file goes too. A role that differs while
    other teams stay listed is refused: their handles were made with it."""
    lines = [] if text is None else text.splitlines()
    index: dict[str, tuple[int, str]] = {}
    for number, line in enumerate(lines):
        if line.lstrip().startswith("#"):
            continue
        for var in (TEAMS_VAR, ROLE_VAR):
            if not re.search(rf"\b{var}\b", line):
                continue
            match = re.fullmatch(rf"{var}=({_ENV_VALUE})", line)
            if match is None:
                raise MemberError(f"{where} line {number + 1} sets {var} in a form this play does not "
                                  f"read; write it as {var}=<value>")
            if var in index:
                raise MemberError(f"{where} sets {var} twice")
            index[var] = (number, match.group(1))
    teams = [team for team in index.get(TEAMS_VAR, (0, ""))[1].split(",") if team and team not in remove]
    teams += [team for team in add if team not in teams]
    current = index.get(ROLE_VAR, (0, ""))[1]
    if host is not None and current and current != host:
        others = [team for team in teams if team not in add]
        if others:
            raise MemberError(f"{where} sets {ROLE_VAR}={current} for team(s) {', '.join(others)}, and "
                              f"the entries say {host}: one role per user")
    values = {TEAMS_VAR: ",".join(teams), ROLE_VAR: host or current} if teams else {}
    out: list[str | None] = list(lines)
    for var in (TEAMS_VAR, ROLE_VAR):
        if var in index:
            out[index[var][0]] = f"{var}={values[var]}" if values.get(var) else None
    kept = [line for line in out if line is not None]
    kept += [f"{var}={values[var]}" for var in (TEAMS_VAR, ROLE_VAR) if var not in index and values.get(var)]
    if not any(line.strip() for line in kept):
        return None
    return "\n".join(kept) + "\n"


def host_network(source: str, ip_json: str) -> tuple[str, str]:
    """The interface and network (CIDR) of this host that hold `source`."""
    try:
        address = ipaddress.ip_address(source)
        for link in json.loads(ip_json):
            for info in link.get("addr_info", ()):
                network = ipaddress.ip_interface(f"{info['local']}/{info['prefixlen']}").network
                if address in network and network.num_addresses > 1:
                    return link["ifname"], str(network)
    except (ValueError, TypeError, KeyError, AttributeError) as exc:
        raise MemberError(f"`ip -j addr show` or the source address {source!r} is unreadable ({exc})") from None
    raise MemberError(f"{source} is on no network of this host: README.lxc step 2 routes the "
                      "container through an LXC bridge here")


def covered(source: str, allow_from: Sequence[str]) -> bool:
    address = ipaddress.ip_address(source)
    return any(address in ipaddress.ip_network(cidr, strict=False) for cidr in allow_from)


def base_url(bus: str, port: int) -> str:
    """The bundle's `base_url` for the bus address (as agent-bus add-member writes it)."""
    host = f"[{bus}]" if ipaddress.ip_address(bus).version == 6 else bus
    return f"http://{host}:{port}"


def parse_suggest(text: str, label: str) -> tuple[str, str]:
    """`--repo` and `--host` from `pingbus suggest-handle`, whose `--type` must be lxc."""
    match = _SUGGEST_RE.fullmatch(text.strip())
    if match is None:
        raise MemberError(f"{label}: pingbus suggest-handle printed {text.strip()!r}")
    repo, host, type_ = match.groups()
    if type_ != MEMBER_TYPE:
        raise MemberError(f"{label}: pingbus suggest-handle says --type={type_}, not {MEMBER_TYPE}: "
                          "the agent does not run in an LXC container")
    return repo, host


def parse_list(text: str) -> dict[str, tuple[str, bool]]:
    """`agent-bus list`'s MEMBER lines: handle -> (role, parked)."""
    found = {}
    for line in text.splitlines():
        fields = line.split("\t")
        if fields[0] != "MEMBER":
            continue
        if len(fields) != 5:
            raise MemberError(f"agent-bus list printed an unreadable line: {line!r}")
        found[fields[1]] = (fields[2], fields[4] == "parked")
    return found


def service_unit(user: str) -> str:
    return f"agent-bus-claude-{user}.service"


def render_member_unit(user: str, home: str, checkout_path: str, tmux: str) -> str:
    """Step 7's unit: `agent-bus-claude` as `user` in its checkout, under tmux, restarted on any
    exit (the tmux server ends with the session, and with it the unit's main process), with a
    delay growing from 10 s to 5 min and no start limit, so a session that cannot start yet (a
    homeserver still starting, Claude logged out) is retried, slowly, until it can."""
    for what, value in (("checkout", checkout_path), ("home", home), ("tmux", tmux)):
        if not _UNIT_VALUE_RE.fullmatch(value):
            raise MemberError(f"{user}'s {what} {value!r} is not an absolute path of letters, digits and "
                              "._/@+-, which a systemd unit takes as written")
    if not _USER_RE.fullmatch(user):
        raise MemberError(f"{user!r} is not a user name")
    tmux_args = f"-L {SERVICE_TMUX}"
    return "\n".join([
        "# Rendered by play-agent-bus.yml (helpers/agent_bus_lxc/members.py, agent_bus_lxc_members",
        f"# service: true); do not edit. Attach: runuser -u {user} -- tmux {tmux_args} attach",
        "[Unit]",
        f"Description=Agent team member session of {user} (agent-bus-claude under tmux)",
        "After=network-online.target",
        "Wants=network-online.target",
        "StartLimitIntervalSec=0",
        "",
        "[Service]",
        "Type=forking",
        f"User={user}",
        f"WorkingDirectory={checkout_path}",
        f"Environment=HOME={home}",
        f"Environment=PATH={home}/.local/bin:{GUEST_PATH}",
        f"ExecStart={tmux} {tmux_args} new-session -d -s {SERVICE_TMUX} {GUEST_BIN}/agent-bus-claude",
        f"ExecStop={tmux} {tmux_args} kill-server",
        "Restart=always",
        "RestartSec=10s",
        "RestartSteps=6",
        "RestartMaxDelaySec=5min",
        "",
        "[Install]",
        "WantedBy=multi-user.target",
    ]) + "\n"


# ── the executor ─────────────────────────────────────────────────────────────────────────


@dataclasses.dataclass(frozen=True)
class Host:
    """The homeserver host's side, and a way into each container."""

    #: RUNNING, STOPPED, ... or None when no such container exists.
    container_state: Callable[[str], str | None]
    #: `agent-bus ARGV` as root: its stdout; a failure raises MemberError.
    agent_bus: Callable[[Sequence[str]], bytes]
    ip_json: Callable[[], str]
    is_bridge: Callable[[str], bool]
    team_active: Callable[[str], bool]
    bus_address: Callable[[], str]
    team_file: Callable[[str], teamfile.TeamFile]
    kit_tar: Callable[[], bytes]
    guest: Callable[[str], object]
    #: `derive.ROOT_READ_PY probe` for (container, user, checkout) as root, from the host's
    #: side: what it printed. A failure raises MemberError.
    read_checkout: Callable[[str, str, str], str]
    #: Whether lxc-autostart starts the container at host boot (`lxc.start.auto = 1`).
    autostarts: Callable[[str], bool]
    #: The team's member record (L8), parsed, or None when there is none.
    read_record: Callable[[str], Mapping | None]
    #: The team's member record written (None: removed); whether that changed anything.
    write_record: Callable[[str, Mapping | None], bool]
    #: The teams that have a member record on this host.
    record_teams: Callable[[], Sequence[str]]
    now: Callable[[], float]


def _python(script: str, *args: str) -> list[str]:
    return ["python3", "-I", "-c", script, *args]


def _existing_state(host: Host, container: str) -> str:
    """The container's state; one that does not exist is a wrong entry, and fails (L5)."""
    state = host.container_state(container)
    if state is None:
        raise MemberError(f"LXC container {container} does not exist: this play joins existing "
                          "containers, it never makes one")
    return state


def _home(guest, member: Member) -> str:
    home = guest.home(member.user)
    if home is None:
        raise MemberError(f"LXC container {member.container} has no user {member.user}: make the "
                          "agent user in the container first")
    return home


def _observe(guest, member: Member, home: str) -> tuple[Found, str | None]:
    out = guest.agent(member.user, home, home, _python(OBSERVE_PY, f"{home}/{PINGBUS_DIR}", member.team),
                      f"{member.label}: reading its bundle for team {member.team}")
    data = json.loads(out)
    parsed = None
    if data["member"] is not None:
        try:
            parsed = json.loads(data["member"])
        except ValueError:
            parsed = None
    if not isinstance(parsed, dict):
        parsed = None
    return Found(data["dir"], parsed, data["token"]), data["env"]


def _suggest(guest, member: Member, home: str) -> str:
    out = guest.agent(member.user, home, member.checkout, ["pingbus", "suggest-handle"],
                      f"{member.label}: pingbus suggest-handle in {member.checkout}",
                      env=[f"{ROLE_VAR}={member.host}"])
    repo, suggested_host = parse_suggest(out.decode(), member.label)
    if suggested_host != member.host:
        raise MemberError(f"{member.label}: suggest-handle gives --host={suggested_host}, not {member.host}")
    if member.repo is not None and member.repo != registry.normalise_repo(repo):
        raise MemberError(f"{member.label}: the checkout {member.checkout} is repo {repo}, and the "
                          f"entry says {member.repo}")
    return repo


def _write_env(guest, member: Member, home: str, text: str | None) -> None:
    guest.agent(member.user, home, home, _python(WRITE_ENV_PY, f"{home}/{PINGBUS_DIR}/env"),
                f"{member.label}: writing {ENV_PATH}", stdin=(text or "").encode())


def _remove_bundle(guest, member: Member, home: str) -> None:
    guest.agent(member.user, home, home, _python(REMOVE_PY, f"{home}/{PINGBUS_DIR}", member.team),
                f"{member.label}: removing its bundle for team {member.team}")


def _check_teams(members: Sequence[Member], teams_present: Sequence[str], host: Host) -> None:
    for member in members:
        if member.team not in teams_present:
            raise MemberError(f"{member.label}: team {member.team} is not declared present in "
                              f"agent_bus_teams ({VARS_FILE}): a member joins a team hosted here")
        if not host.team_active(member.team):
            raise MemberError(f"team {member.team}'s homeserver ({checkout.hs_unit(member.team)}) is not "
                              "active; run play-agent-bus.yml with the team declared present")


def _check_gh(guest, member: Member, home: str) -> None:
    """README.lxc's requirement, met by whoever set the container up and never by this play:
    gh installed and logged in to github.com for the agent user."""
    guest.agent(member.user, home, home, ["sh", "-c", GH_CHECK_SH],
                f"LXC container {member.container}: gh is required and must be logged in for "
                f"{member.user}: install gh in the container and run `gh auth login --hostname "
                f"github.com` as {member.user}. The check, gh's token for github.com,")


def _sync_kit(host: Host, guest, container: str, emit: Callable[[str], None],
              say: Callable[[str], None]) -> None:
    out = guest.root(_python(KIT_SYNC_PY, guest.kit_parent, guest.bin_dir, str(guest.kit_owner)),
                     f"container {container}: the member kit (README.lxc step 1)", stdin=host.kit_tar())
    for line in out.decode().splitlines():
        emit(f"container {container}: kit {line}")
        if line.startswith("repaired "):
            say(f"container {container}: kit {line}")


def _check_route(host: Host, guest, members: Sequence[Member], bus: str) -> None:
    container = members[0].container
    source = guest.root(_python(SOURCE_PY, bus), f"container {container}: its source address").decode().strip()
    dev, cidr = host_network(source, host.ip_json())
    if not host.is_bridge(dev):
        raise MemberError(f"LXC container {container} ({source}) reaches {bus} through {dev}, which is "
                          "not a bridge of this host; README.lxc routes it through the LXC bridge")
    for member in members:
        allow = host.team_file(member.team).allow_from
        if not covered(source, allow):
            raise MemberError(
                f"team {member.team}'s allow_from ({', '.join(allow) or 'empty'}) does not cover LXC "
                f"container {container} at {source} on bridge {dev}. The play admits the lxcbr0 "
                f"bridge itself; {dev} is another bridge, so add \"{cidr}\" to allow_from of "
                f"team {member.team} in agent_bus_teams ({VARS_FILE}), and run the play again")


def _join(host: Host, guest, member: Member, home: str, bus: str, emit: Callable[[str], None]) -> str:
    """README.lxc steps 3 and 4 for one entry; the member's handle."""
    suggested = _suggest(guest, member, home)
    # A derived entry carries the repo the host read from the checkout; the container's own
    # claim was only held to it (_suggest), so the handle is never minted from what it says.
    repo = member.repo if member.repo is not None else registry.normalise_repo(suggested)
    handle = member.handle(repo)
    found, _ = _observe(guest, member, home)
    registered = parse_list(host.agent_bus(["list", member.team]).decode()).get(handle)
    plan = present_plan(handle, base_url(bus, host.team_file(member.team).port), member, found, registered)
    if plan.remove_bundle:
        _remove_bundle(guest, member, home)
        emit(f"removed the stale bundle of {member.label} for team {member.team}")
    if plan.park:
        host.agent_bus(["park-member", member.team, handle])
        emit(f"parked {handle} in team {member.team}, to issue a new bundle")
    if plan.add:
        bundle = host.agent_bus(["add-member", member.team, f"--repo={repo}", f"--seat={member.seat}",
                                 f"--host={member.host}", f"--type={MEMBER_TYPE}", f"--role={member.role}",
                                 f"--address={bus}", *([] if member.human_text else ["--no-human-text"]),
                                 "--out=-"])
        try:
            checkout.read_bundle(bundle)
        except checkout.CheckoutError as exc:
            raise MemberError(f"{handle}: agent-bus add-member printed no usable bundle: {exc}") from None
        guest.agent(member.user, home, home, _python(PLACE_PY, f"{home}/{PINGBUS_DIR}", member.team),
                    f"{member.label}: placing its bundle for team {member.team}", stdin=bundle)
        emit(f"{handle} joined team {member.team} ({member.label})")
    if plan.set_role:
        host.agent_bus(["set-role", member.team, handle, f"--role={member.role}"])
        emit(f"{handle} is now {member.role} in team {member.team}")
    return handle


def _check_service(host: Host, guest, container: str) -> str:
    """Step 7's requirements, before any change: the container starts at host boot, runs
    systemd and has tmux. Returns tmux's path in the container."""
    if not host.autostarts(container):
        raise MemberError(f"LXC container {container} does not start at host boot, so its supervised session "
                          "(service: true) would not come back after a reboot: set lxc.start.auto = 1 in "
                          "its config, then run the play again")
    out = guest.root(["sh", "-c", SERVICE_NEEDS_SH],
                     f"LXC container {container}: service: true needs systemd as the container's init "
                     "and tmux installed in it (install tmux there); the check")
    return out.decode().strip()


def _service_state(guest, container: str, unit: str) -> dict[str, str]:
    out = guest.root(["systemctl", "show", "--property=LoadState", "--property=UnitFileState",
                      "--property=ActiveState", unit], f"container {container}: systemctl show {unit}")
    found = dict(line.split("=", 1) for line in out.decode().splitlines() if "=" in line)
    if {"LoadState", "UnitFileState", "ActiveState"} - set(found):
        raise MemberError(f"container {container}: systemctl show {unit} printed {out.decode().strip()!r}")
    return found


def _systemctl(guest, container: str, *args: str) -> None:
    guest.root(["systemctl", *args], f"container {container}: systemctl {' '.join(args)}")


def _ensure_service(guest, member: Member, home: str, tmux: str, emit: Callable[[str], None]) -> None:
    """Step 7: the unit as rendered, enabled and running; restarted when it changed."""
    unit = service_unit(member.user)
    label = f"container {member.container}: {unit}"
    state = _service_state(guest, member.container, unit)
    text = render_member_unit(member.user, home, str(member.checkout), tmux)
    wrote = guest.root(_python(UNIT_WRITE_PY, f"{guest.unit_dir}/{unit}"), f"{label}: writing it",
                       stdin=text.encode()).strip() == b"changed"
    if wrote:
        _systemctl(guest, member.container, "daemon-reload")
        emit(f"{label} {'installed' if state['LoadState'] == 'not-found' else 'updated'}")
    if state["UnitFileState"] != "enabled":
        _systemctl(guest, member.container, "enable", unit)
        emit(f"{label} enabled")
    if wrote and state["ActiveState"] == "active":
        _systemctl(guest, member.container, "restart", unit)
        emit(f"{label} restarted (its unit changed)")
    elif state["ActiveState"] != "active":
        _systemctl(guest, member.container, "start", unit)
        emit(f"{label} started")


def _drop_service(guest, container: str, user: str, emit: Callable[[str], None]) -> None:
    """No supervised session for `user`: its unit stopped, disabled and removed, if there is one."""
    unit = service_unit(user)
    path = f"{guest.unit_dir}/{unit}"
    if guest.root(_python(SYSTEMD_PROBE_PY), f"container {container}: is systemd its init?").strip() != b"systemd":
        # No systemd, so no unit can be loaded or running: only a stray file can be left.
        if guest.root(_python(UNIT_REMOVE_PY, path), f"container {container}: removing {unit}").strip() == b"removed":
            emit(f"container {container}: {unit} removed")
        return
    loaded = _service_state(guest, container, unit)["LoadState"] != "not-found"
    if loaded:
        _systemctl(guest, container, "disable", "--now", unit)
    removed = guest.root(_python(UNIT_REMOVE_PY, path),
                         f"container {container}: removing {unit}").strip() == b"removed"
    if loaded or removed:
        _systemctl(guest, container, "daemon-reload")
        emit(f"container {container}: {unit} stopped and removed")


def _groups(members: Sequence[Member]) -> dict[tuple[str, str], list[Member]]:
    groups: dict[tuple[str, str], list[Member]] = {}
    for member in members:
        groups.setdefault((member.container, member.user), []).append(member)
    return groups


def _derive_hosts_and_seats(found: Sequence[Member], host: Host,
                            guests: Mapping[tuple[str, str], tuple[object, str]],
                            unheld_teams: Collection[str]) -> tuple[Member, ...]:
    """resolve_members over the real surroundings: the checkout read from the host's side,
    the team's roster from `agent-bus list`, the bundle from the agent's own directory."""
    rosters: dict[str, dict[str, bool]] = {}

    def identify(member: Member) -> derive.Identity:
        return derive.identity_of(derive.parse_probe(host.read_checkout(member.container, member.user,
                                                                        member.checkout)))

    def roster(team: str) -> dict[str, bool]:
        if team not in rosters:
            listed = parse_list(host.agent_bus(["list", team]).decode())
            rosters[team] = {handle: parked for handle, (_role, parked) in listed.items()}
        return rosters[team]

    def bundle(member: Member) -> str | None:
        guest, home = guests[(member.container, member.user)]
        seen, _ = _observe(guest, member, home)
        return bundle_handle(seen.member) if seen.member is not None else None

    return resolve_members(found, identify=identify, roster=roster, bundle=bundle, unheld_teams=unheld_teams)


def _hold_back(member: Member, state: str, record: Mapping | None,
               listed: Callable[[str], Mapping[str, tuple[str, bool]]]) -> tuple[Seen, str | None]:
    """A present entry whose container is not running (L1, L2): skipped when its known handle
    is an active member, else deferred. Also its known handle, which keeps its seat claimed."""
    handle = known_handle(member, record)
    registered = listed(member.team).get(handle) if handle is not None else None
    if registered is not None and not registered[1]:
        return Seen(member, STOPPED, handle, skipped_why(member, state, handle)), handle
    return Seen(member, DEFERRED, handle, deferred_why(member, state, handle)), handle


def run_present(members: Sequence[Member], teams_present: Sequence[str], host: Host,
                emit: Callable[[str], None], say: Callable[[str], None],
                records: Mapping[str, Mapping | None] | None = None) -> list[Seen]:
    # Everything that can be checked without the kit, for every container, before any change:
    # suggest-handle needs the kit's pingbus, so its --type check comes after the kit sync.
    _check_teams(members, teams_present, host)
    records = records or {}
    bus = host.bus_address()
    lists: dict[str, dict[str, tuple[str, bool]]] = {}

    def listed(team: str) -> dict[str, tuple[str, bool]]:
        if team not in lists:
            lists[team] = parse_list(host.agent_bus(["list", team]).decode())
        return lists[team]

    ready = []
    tmux_of: dict[tuple[str, str], str] = {}
    held: list[Seen] = []
    claimed: dict[int, Member] = {}
    unheld_teams: set[str] = set()
    for (container, user), group in _groups(members).items():
        state = _existing_state(host, container)
        if state != RUNNING:
            for member in group:
                seen, handle = _hold_back(member, state, records.get(member.team), listed)
                held.append(seen)
                if handle is not None:
                    claimed[id(member)] = with_handle(member, handle)
                else:
                    unheld_teams.add(member.team)
            continue
        guest = host.guest(container)
        home = _home(guest, group[0])
        _check_gh(guest, group[0], home)
        _check_route(host, guest, group, bus)
        if group[0].service:
            tmux_of[(container, user)] = _check_service(host, guest, container)
        ready.append((container, user, group, guest, home))
    reachable = {(c, u) for c, u, *_ in ready}
    # A held-back entry takes part in the derivation only through its known handle (L4).
    found = [claimed.get(id(m), m) for m in members
             if (m.container, m.user) in reachable or id(m) in claimed]
    derived = _derive_hosts_and_seats(found, host, {(c, u): (guest, home) for c, u, _, guest, home in ready},
                                      unheld_teams)
    members = [m for m in derived if (m.container, m.user) in reachable]
    ready = [(c, u, group, *next((g, h) for cc, uu, _, g, h in ready if (cc, uu) == (c, u)))
             for (c, u), group in _groups(members).items()]
    joined: list[Seen] = []
    synced: set[str] = set()
    for container, user, group, guest, home in ready:
        if container not in synced:
            _sync_kit(host, guest, container, emit, say)
            synced.add(container)
    # Every demotion, in every container, lands before any promotion: a team has one coordinator.
    for coordinators in (False, True):
        for _container, _user, group, guest, home in ready:
            for member in group:
                if (member.role == protocol.ROLE_COORDINATOR) == coordinators:
                    say(f"{member.label}: team {member.team}")
                    joined.append(Seen(member, JOINED, _join(host, guest, member, home, bus, emit)))
    for container, user, group, guest, home in ready:
        first = group[0]
        _, env = _observe(guest, first, home)
        teams = [member.team for member in group]
        merged = merge_env(env, where=f"{first.label}: {ENV_PATH}", add=teams, host=first.host)
        if merged != env:
            _write_env(guest, first, home, merged)
            emit(f"{first.label}: {ENV_PATH} lists {', '.join(teams)}")
        guest.agent(user, home, home, ["sh", "-c", CONFIG_CHECK_SH],
                    f"{first.label}: pingbus config check (README.lxc step 6)")
        if (container, user) in tmux_of:
            _ensure_service(guest, first, home, tmux_of[(container, user)], emit)
        else:
            _drop_service(guest, container, user, emit)
    return held + joined


def _absent_held_back(host: Host, member: Member, state: str, team_runs: bool, record: Mapping | None,
                      emit: Callable[[str], None]) -> Seen:
    """An absent entry whose container is not running (L6): its known handle parked from this
    host's side, the bundle and env file inside left for the first run with the container up."""
    where = f"LXC container {member.container} is {state}"
    later = "the first run with it up removes its bundle and env file"
    if not team_runs:
        return Seen(member, DEFERRED, None, f"{where}: {later}")
    held = known_handle(member, record)
    if held is None:
        return Seen(member, DEFERRED, None, f"{where}, and this host has no record of its handle in team "
                                            f"{member.team}, so it is not parked yet: {later} and parks it")
    registered = parse_list(host.agent_bus(["list", member.team]).decode()).get(held)
    if registered is not None and not registered[1]:
        host.agent_bus(["park-member", member.team, held])
        emit(f"parked {held} in team {member.team}")
    return Seen(member, DEFERRED, held, f"{where}: {held} is parked in team {member.team}; {later}")


def _absent_one(host: Host, member: Member, teams_present: Sequence[str],
                emit: Callable[[str], None], record: Mapping | None = None) -> Seen:
    team_runs = host.team_active(member.team)
    if member.team in teams_present and not team_runs:
        raise MemberError(f"team {member.team}'s homeserver ({checkout.hs_unit(member.team)}) is not "
                          f"active, so {member.label}'s handle cannot be parked")
    state = host.container_state(member.container)
    guest = home = env = None
    found = Found(False, None, False)
    if state is None:
        if member.repo is None and team_runs:
            raise MemberError(f"LXC container {member.container} does not exist, so {member.label}'s "
                              "handle cannot be read from it: add repo: <repo> to the entry, and the play "
                              "parks it")
    elif state != RUNNING:
        return _absent_held_back(host, member, state, team_runs, record, emit)
    else:
        guest = host.guest(member.container)
        home = guest.home(member.user)
        if home is not None:
            found, env = _observe(guest, member, home)
    if team_runs:
        held = bundle_handle(found.member) if found.member is not None else None
        if held is None:
            if member.host is None or member.seat is None:
                raise MemberError(f"{member.label}: no bundle in the container names its handle, and the entry "
                                  "leaves out its host or seat: set both on the entry to park it")
            if member.repo is None and home is None:
                raise MemberError(f"{member.label}: no user {member.user} in the container to run "
                                  "suggest-handle as: add repo: <repo> to the entry")
            held = member.handle(member.repo if member.repo is not None else
                                 registry.normalise_repo(_suggest(guest, member, home)))
        registered = parse_list(host.agent_bus(["list", member.team]).decode()).get(held)
    else:
        held, registered = None, None
    plan = absent_plan(found, registered, team_runs)
    if plan.park:
        host.agent_bus(["park-member", member.team, held])
        emit(f"parked {held} in team {member.team}")
    if plan.remove_bundle:
        _remove_bundle(guest, member, home)
        emit(f"removed the bundle of {member.label} for team {member.team}")
    if home is not None:
        merged = merge_env(env, where=f"{member.label}: {ENV_PATH}", remove=[member.team])
        if merged != env:
            _write_env(guest, member, home, merged)
            emit(f"{member.label}: {ENV_PATH} no longer lists {member.team}")
    return Seen(member, LEFT, held)


def _write_records(members: Sequence[Member], seen: Sequence[Seen], teams_present: Sequence[str],
                   records: Mapping[str, Mapping | None], host: Host, emit: Callable[[str], None]) -> None:
    """Each team's member record, after the phase succeeded (L8); a team not declared present
    keeps none."""
    for team in dict.fromkeys(member.team for member in members):
        record = None
        if team in teams_present:
            record = merge_record(records.get(team), team, [m for m in members if m.team == team],
                                  [s for s in seen if s.member.team == team], host.now())
        if host.write_record(team, record):
            emit(f"the LXC member record of team {team}")


def sweep_records(members: Sequence[Member], only_teams: Collection[str] | None, host: Host,
                  emit: Callable[[str], None]) -> None:
    """The record of each team no entry names any more removed (L8), so a member nobody declares
    is never shown as waiting for its container. With `only_teams` (agent_bus_only_teams) the
    entries are only those teams', so only those teams' records may go."""
    named = {member.team for member in members}
    for team in host.record_teams():
        if team not in named and (only_teams is None or team in only_teams) and host.write_record(team, None):
            emit(f"the LXC member record of team {team}")


def run_phase(phase: str, members: Sequence[Member], teams_present: Sequence[str], host: Host,
              *, emit: Callable[[str], None], say: Callable[[str], None]) -> list[Report]:
    """The phase's entries brought to their state; the members held back, in entry order."""
    chosen = [member for member in members if member.state == phase]
    if not chosen:
        return []
    records = {team: host.read_record(team) for team in dict.fromkeys(member.team for member in members)}
    if phase == "present":
        seen = run_present(chosen, teams_present, host, emit, say, records)
    else:
        seen = []
        for member in chosen:
            say(f"{member.label}: leaving team {member.team}")
            seen.append(_absent_one(host, member, teams_present, emit, records.get(member.team)))
        # A user left in no team keeps no supervised session (step 7); one still declared present
        # in another team is the present phase's to keep. A container not running keeps its unit
        # until the first run with it up (L6).
        staying = {(member.container, member.user) for member in members if member.state == "present"}
        for container, user in dict.fromkeys((member.container, member.user) for member in chosen):
            if (container, user) not in staying and host.container_state(container) == RUNNING:
                _drop_service(host.guest(container), container, user, emit)
    _write_records(members, seen, teams_present, records, host, emit)
    order = {(m.container, m.user, m.team): n for n, m in enumerate(members)}
    seen = sorted(seen, key=lambda s: order[(s.member.container, s.member.user, s.member.team)])
    return [report for report in map(report_of, seen) if report is not None]


# ── the real host ────────────────────────────────────────────────────────────────────────


def _run(argv: Sequence[str], stdin: bytes = b"") -> tuple[int, bytes, bytes]:
    try:
        run = subprocess.run(list(argv), input=stdin, capture_output=True, check=False)
    except FileNotFoundError:
        hint = f": run {LXC_PLAY}" if argv[0].startswith("lxc-") else ""
        raise MemberError(f"{argv[0]} is not installed{hint}") from None
    return run.returncode, run.stdout, run.stderr


def _failed(what: str, code: int, err: bytes) -> MemberError:
    return MemberError(f"{what} failed (exit {code}): {err.decode(errors='replace').strip()}")


@dataclasses.dataclass(frozen=True)
class LxcGuest:
    """Commands in one container through lxc-attach, with a cleared environment."""

    container: str
    kit_parent: str = KIT_PARENT
    bin_dir: str = GUEST_BIN
    unit_dir: str = GUEST_UNIT_DIR
    #: Who must own the kit's directories in the container: its root.
    kit_owner: int = 0

    def _attach(self, inner: Sequence[str], stdin: bytes) -> tuple[int, bytes, bytes]:
        return _run(["lxc-attach", "-n", self.container, "--clear-env", "--", "env", "-i",
                     f"PATH={GUEST_PATH}", "HOME=/root", *inner], stdin)

    def home(self, user: str) -> str | None:
        code, out, err = self._attach(["getent", "passwd", user], b"")
        if code == 2:  # getent: no such key
            return None
        if code != 0:
            raise _failed(f"container {self.container}: getent passwd {user}", code, err)
        fields = out.decode().strip().split(":")
        if len(fields) != 7 or not fields[5].startswith("/"):
            raise MemberError(f"container {self.container}: getent passwd {user} printed no home")
        return fields[5]

    def root(self, argv: Sequence[str], what: str, stdin: bytes = b"") -> bytes:
        code, out, err = self._attach(argv, stdin)
        if code != 0:
            raise _failed(what, code, err)
        return out

    def agent(self, user: str, home: str, cwd: str, argv: Sequence[str], what: str,
              stdin: bytes = b"", env: Sequence[str] = ()) -> bytes:
        return self.root(["runuser", "-u", user, "--", "env", "-i", "-C", cwd, f"PATH={GUEST_PATH}",
                          f"HOME={home}", *env, *argv], what, stdin)


def _container_state(name: str) -> str | None:
    code, out, err = _run(["lxc-ls", "-1"])
    if code != 0:
        raise _failed("lxc-ls", code, err)
    if name not in out.decode().split():
        return None
    code, out, err = _run(["lxc-info", "-n", name, "-sH"])
    if code != 0:
        raise _failed(f"lxc-info -n {name}", code, err)
    return out.decode().strip()


def _autostarts(name: str) -> bool:
    """`lxc-info -c lxc.start.auto`: `lxc.start.auto = 1` when lxc-autostart starts it at boot."""
    code, out, err = _run(["lxc-info", "-n", name, "-c", "lxc.start.auto"])
    if code != 0:
        raise _failed(f"lxc-info -n {name} -c lxc.start.auto", code, err)
    _key, sep, value = out.decode().partition("=")
    if not sep:
        raise MemberError(f"lxc-info -n {name} -c lxc.start.auto printed {out.decode().strip()!r}")
    return value.strip() == "1"


def _agent_bus(argv: Sequence[str]) -> bytes:
    code, out, err = _run([WRAPPER, *argv])
    if code != 0:
        raise _failed(f"agent-bus {' '.join(argv[:2])}", code, err)
    return out


def _ip_json() -> str:
    code, out, err = _run(["ip", "-j", "addr", "show"])
    if code != 0:
        raise _failed("ip -j addr show", code, err)
    return out.decode()


def _team_file(team: str) -> teamfile.TeamFile:
    try:
        return teamfile.load_team_file(STATE_ROOT / team / TEAM_FILE)
    except FileNotFoundError:
        raise MemberError(f"team {team} has no {STATE_ROOT / team / TEAM_FILE}: run play-agent-bus.yml") from None


def read_checkout(container: str, user: str, checkout_path: str) -> str:
    """What `derive.ROOT_READ_PY probe` prints for the checkout: read from this host as root,
    through the container's LXC config, never by running anything the container controls."""
    code, out, err = _run(list(derive.LXC_PATH_COMMAND))
    lxc_path = out.decode().strip()
    if code != 0 or not lxc_path.startswith("/") or not lxc_path.isprintable():
        raise _failed(" ".join(derive.LXC_PATH_COMMAND), code, err)
    code, out, err = _run(["python3", "-I", "-c", derive.ROOT_READ_PY, "probe", lxc_path, container, user,
                           checkout_path])
    if code != 0:
        raise _failed(f"reading {checkout_path} of {user}@{container} from the host", code, err)
    return out.decode()


def kit_tar(parent: pathlib.Path) -> bytes:
    """`parent`/kit as a tar of `kit/...`, owned by root; a symlink in it is refused."""
    kit = pathlib.Path(parent) / "kit"
    if not kit.is_dir():
        raise MemberError(f"{kit} is missing: run play-agent-bus.yml (agent-bus-install software)")
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode="w", format=tarfile.PAX_FORMAT) as tar:
        for path in sorted([kit, *kit.rglob("*")]):
            if path.is_symlink() or not (path.is_dir() or path.is_file()):
                raise MemberError(f"{path} in the member kit is not a plain file or directory")
            info = tar.gettarinfo(str(path), arcname=str(path.relative_to(parent)))
            info.uid = info.gid = 0
            info.uname = info.gname = "root"
            info.mtime = 0
            if path.is_file():
                with path.open("rb") as handle:
                    tar.addfile(info, handle)
            else:
                tar.addfile(info)
    return buffer.getvalue()


def _read_record(team: str) -> dict | None:
    path = RECORD_DIR / f"{team}.json"
    return parse_record(applied_record.read(path), str(path))


def _write_record(team: str, record: Mapping | None) -> bool:
    text = None if record is None else json.dumps(record, sort_keys=True, indent=1) + "\n"
    try:
        group = teamfile.read_stamp_group(STAMP_GROUP_FILE, "root")
        try:
            gid = grp.getgrnam(group).gr_gid
        except KeyError:
            raise MemberError(f"the LXC member record of team {team}: the stamps' group {group} named in "
                              f"{STAMP_GROUP_FILE} does not exist on this host (it went while the play ran?): "
                              "play-agent-bus.yml records that group; run the play again") from None
        return applied_record.write(RECORD_DIR, f"{team}.json", text, gid)
    except (teamfile.StampGroupError, applied_record.RecordError) as exc:
        raise MemberError(f"the LXC member record of team {team}: {exc}") from None


def _record_teams() -> list[str]:
    try:
        return applied_record.names(RECORD_DIR)
    except (OSError, applied_record.RecordError) as exc:
        raise MemberError(f"the LXC member records: {exc}") from None


def real_host() -> Host:
    system = checkout.real_system()
    return Host(container_state=_container_state, agent_bus=_agent_bus, ip_json=_ip_json,
                is_bridge=lambda dev: pathlib.Path(f"/sys/class/net/{dev}/bridge").is_dir(),
                team_active=system.team_active, bus_address=system.bus_address, team_file=_team_file,
                kit_tar=lambda: kit_tar(pathlib.Path(KIT_PARENT)), guest=LxcGuest,
                read_checkout=read_checkout, autostarts=_autostarts,
                read_record=_read_record, write_record=_write_record, record_teams=_record_teams,
                now=time.time)


def main(argv: Sequence[str] | None = None, *, host_factory: Callable[[], Host] = real_host,
         stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    parser = argparse.ArgumentParser(prog=PROG, description="Join existing LXC containers to agent teams.")
    parser.add_argument("--phase", choices=STATES, required=True)
    parser.add_argument("--members", required=True, help="agent_bus_lxc_members, as JSON")
    parser.add_argument("--teams-present", required=True,
                        help="the names of the agent_bus_teams declared present, as a JSON list")
    parser.add_argument("--only-teams", help="agent_bus_only_teams, as a JSON list, when the run is scoped: "
                        "--members then holds only those teams' entries")
    args = parser.parse_args(argv)

    def emit(what: str) -> None:
        out.write(f"CHANGED\t{what}\n")
        out.flush()

    def say(text: str) -> None:
        err.write(f"{PROG}: {text}\n")

    try:
        data, teams = json.loads(args.members), json.loads(args.teams_present)
        only = None if args.only_teams is None else json.loads(args.only_teams)
    except ValueError as exc:
        say(f"--members, --teams-present and --only-teams must be JSON: {exc}")
        return EXIT_USAGE
    try:
        for name, value in (("--teams-present", teams), ("--only-teams", [] if only is None else only)):
            if not isinstance(value, list) or not all(isinstance(team, str) for team in value):
                raise MemberError(f"{name} must be a JSON list of team names")
        members = parse_members(data)
        reports = []
        # The present phase runs on every play run, with entries or none, to sweep the records
        # of teams that have no entry left (L8).
        if args.phase == "present" or any(member.state == args.phase for member in members):
            host = host_factory()
            reports = run_phase(args.phase, members, teams, host, emit=emit, say=say)
            if args.phase == "present":
                sweep_records(members, only, host, emit)
    except (MemberError, checkout.CheckoutError, teamfile.TeamFileError) as exc:
        say(str(exc))
        return EXIT_REFUSED
    for report in reports:
        out.write(f"{report.kind}\t{report.team}\t{report.label}\t{report.why}\n")
    if reports:
        out.write(f"LXC-SUMMARY\t{summary(reports)}\n")
    out.flush()
    return 0


if __name__ == "__main__":
    sys.exit(main())
