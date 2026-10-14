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
  6. `pingbus config check` as the agent user, on every run.

The seat (default: the container's name) makes the handle `<repo>.<seat>+<host>.lxc` known
before anything is made, so a member whose bundle names it, holds a token, matches the
address and `human_text`, and is active in the registry is left alone: no new token per
run. Otherwise the stale bundle goes, an active handle is parked, and `add-member --seat`
returns (or adds) it with a new token. An absent entry parks the handle when its team
runs here, deletes the bundle, and takes the team out of PINGBUS_TEAMS.

Streams: stdout carries only `CHANGED<TAB><what>` lines; diagnostics go to stderr. Exit 0
done, 64 usage, 78 refused (`MemberError`).
"""

from __future__ import annotations

import argparse
import dataclasses
import io
import ipaddress
import json
import pathlib
import re
import subprocess
import sys
import tarfile
from collections.abc import Callable, Mapping, Sequence
from typing import TextIO

from helpers.agent_bus import checkout, registry, teamfile
from helpers.pingbus import protocol

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

REQUIRED_KEYS = frozenset({"team", "container", "user", "host"})
OPTIONAL_KEYS = frozenset({"checkout", "seat", "role", "human_text", "repo", "state"})
_CONTAINER_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,63}")
_USER_RE = re.compile(r"[a-z_][a-z0-9_-]{0,31}")
_ENV_VALUE = r"[A-Za-z0-9,._-]*"
_SUGGEST_RE = re.compile(r"--repo=(\S+) --host=(\S+) --type=(\S+)")

# ── what runs inside the container (`python3 -I -c SCRIPT ARGS...`) ─────────────────────

#: The member's own source address towards the bus address (no packet is sent).
SOURCE_PY = ("import socket, sys; s = socket.socket(socket.AF_INET6 if ':' in sys.argv[1] else "
             "socket.AF_INET, socket.SOCK_DGRAM); s.connect((sys.argv[1], 9)); print(s.getsockname()[0])")

#: As root: the kit tar on stdin made the container's KIT_PARENT/kit (only what differs is
#: written, what the tar lacks is removed), its two commands linked into BIN. Prints one
#: line per change.
KIT_SYNC_PY = r'''
import os, stat, sys, tarfile
if sys.version_info < (3, 11):
    sys.exit("pingbus needs Python 3.11 or later; this container has " + sys.version.split()[0])
parent, bindir, owner = sys.argv[1], sys.argv[2], int(sys.argv[3])
kit = os.path.join(parent, "kit")
def guarded(path, st):
    # Root replaces files in these directories, so nobody else may change what they hold.
    if st.st_uid != owner or stat.S_IMODE(st.st_mode) & 0o022:
        sys.exit(path + " is not owned by uid " + str(owner) + " or is writable by others: a kit "
                 "copied by hand must go before the play installs it (remove " + parent + ")")
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
changed = []
os.makedirs(parent, mode=0o755, exist_ok=True)
guarded(parent, os.lstat(parent))
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
        elif not stat.S_ISDIR(st.st_mode):
            sys.exit(path + " is not a directory")
        else:
            guarded(path, st)
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
base, team = sys.argv[1], sys.argv[2]
names = ("token", "README", "member.json")
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
    host: str
    checkout: str | None
    seat: str
    role: str
    human_text: bool
    repo: str | None
    state: str

    @property
    def label(self) -> str:
        return f"{self.user}@{self.container}"

    def handle(self, repo: str) -> str:
        try:
            return registry.build_handle(repo, self.seat, self.host, MEMBER_TYPE)
        except registry.HandleError as exc:
            raise MemberError(f"{self.label} in team {self.team}: {exc}") from None


def default_seat(container: str) -> str | None:
    """The container's name as a seat: lower case, letters and digits, at most 12. Only a
    named seat (a letter first): a number is the registry counter's, which a member joined
    by hand may hold."""
    seat = re.sub(r"[^a-z0-9]", "", container.lower())[:12]
    return seat if seat[:1].isalpha() and protocol.is_seat_name(seat) else None


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
    if not isinstance(entry["host"], str):
        raise MemberError(f"{where}: host must be a string")
    try:
        host = registry.resolve_host(entry["host"], None)
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
    if seat is None:
        seat = default_seat(container)
        if seat is None:
            raise MemberError(f"{where}: container {container} gives no seat name "
                              f"({protocol.HANDLE_SEAT_PATTERN}); set seat")
    elif type(seat) is int:
        seat = str(seat)
    if not isinstance(seat, str) or not protocol.is_seat_name(seat):
        raise MemberError(f"{where}: seat must match {protocol.HANDLE_SEAT_PATTERN}")
    role = entry.get("role", DEFAULT_ROLE)
    if role not in registry.ROLES:
        raise MemberError(f"{where}: role must be one of {', '.join(registry.ROLES)}")
    human_text = entry.get("human_text", True)
    if not isinstance(human_text, bool):
        raise MemberError(f"{where}: human_text must be true or false")
    return Member(team, container, user, host, checkout_path, seat, role, human_text, repo, state)


def parse_members(data: object) -> tuple[Member, ...]:
    """The entries, each checked, and the rules between them: one entry per user, container
    and team; one host per user and container (they share one env file); one container per
    team, host and seat (else two containers would claim one handle)."""
    if not isinstance(data, list):
        raise MemberError("agent_bus_lxc_members must be a list")
    found = tuple(_parse_entry(position, entry) for position, entry in enumerate(data, start=1))
    entries: dict[tuple[str, str, str], int] = {}
    hosts: dict[tuple[str, str], str] = {}
    seats: dict[tuple[str, str, str], str] = {}
    for position, member in enumerate(found, start=1):
        key = (member.container, member.user, member.team)
        if key in entries:
            raise MemberError(f"agent_bus_lxc_members entries {entries[key]} and {position} both "
                              f"declare {member.label} in team {member.team}")
        entries[key] = position
        if hosts.setdefault((member.container, member.user), member.host) != member.host:
            raise MemberError(f"agent_bus_lxc_members gives {member.label} two hosts; its env file "
                              f"sets one {ROLE_VAR}")
        if seats.setdefault((member.team, member.host, member.seat), member.container) != member.container:
            raise MemberError(f"agent_bus_lxc_members gives seat {member.seat} of team {member.team} "
                              f"to two containers; set seat on one")
    return found


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


def _python(script: str, *args: str) -> list[str]:
    return ["python3", "-I", "-c", script, *args]


def _guest_running(host: Host, container: str):
    state = host.container_state(container)
    if state is None:
        raise MemberError(f"LXC container {container} does not exist: this play joins existing "
                          "containers, it never makes one")
    if state != "RUNNING":
        raise MemberError(f"LXC container {container} is {state}: start it, then run the play again")
    return host.guest(container)


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


def _sync_kit(host: Host, guest, container: str, emit: Callable[[str], None]) -> None:
    out = guest.root(_python(KIT_SYNC_PY, guest.kit_parent, guest.bin_dir, str(guest.kit_owner)),
                     f"container {container}: the member kit (README.lxc step 1)", stdin=host.kit_tar())
    for line in out.decode().splitlines():
        emit(f"container {container}: kit {line}")


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
                f"container {container} at {source} on bridge {dev}. The play admits the bridge named by "
                f"agent_bus_lxc_bridge (lxcbr0 by default) itself; {dev} is another bridge, so either set "
                f"agent_bus_lxc_bridge to {dev}, or add \"{cidr}\" to allow_from of "
                f"team {member.team} in agent_bus_teams ({VARS_FILE}), and run the play again")


def _join(host: Host, guest, member: Member, home: str, bus: str, emit: Callable[[str], None]) -> None:
    repo = _suggest(guest, member, home)
    handle = member.handle(registry.normalise_repo(repo))
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


def _groups(members: Sequence[Member]) -> dict[tuple[str, str], list[Member]]:
    groups: dict[tuple[str, str], list[Member]] = {}
    for member in members:
        groups.setdefault((member.container, member.user), []).append(member)
    return groups


def run_present(members: Sequence[Member], teams_present: Sequence[str], host: Host,
                emit: Callable[[str], None], say: Callable[[str], None]) -> None:
    # Everything that can be checked without the kit, for every container, before any change:
    # suggest-handle needs the kit's pingbus, so its --type check comes after the kit sync.
    _check_teams(members, teams_present, host)
    bus = host.bus_address()
    ready = []
    for (container, user), group in _groups(members).items():
        guest = _guest_running(host, container)
        home = _home(guest, group[0])
        _check_gh(guest, group[0], home)
        _check_route(host, guest, group, bus)
        ready.append((container, user, group, guest, home))
    synced: set[str] = set()
    for container, user, group, guest, home in ready:
        first = group[0]
        if container not in synced:
            _sync_kit(host, guest, container, emit)
            synced.add(container)
        for member in group:
            say(f"{member.label}: team {member.team}")
            _join(host, guest, member, home, bus, emit)
        _, env = _observe(guest, first, home)
        teams = [member.team for member in group]
        merged = merge_env(env, where=f"{first.label}: {ENV_PATH}", add=teams, host=first.host)
        if merged != env:
            _write_env(guest, first, home, merged)
            emit(f"{first.label}: {ENV_PATH} lists {', '.join(teams)}")
        guest.agent(user, home, home, ["sh", "-c", CONFIG_CHECK_SH],
                    f"{first.label}: pingbus config check (README.lxc step 6)")


def _absent_one(host: Host, member: Member, teams_present: Sequence[str],
                emit: Callable[[str], None]) -> None:
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
    else:
        guest = _guest_running(host, member.container)
        home = guest.home(member.user)
        if home is not None:
            found, env = _observe(guest, member, home)
    if team_runs:
        held = bundle_handle(found.member) if found.member is not None else None
        if held is None:
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


def run_phase(phase: str, members: Sequence[Member], teams_present: Sequence[str], host: Host,
              *, emit: Callable[[str], None], say: Callable[[str], None]) -> None:
    chosen = [member for member in members if member.state == phase]
    if not chosen:
        return
    if phase == "present":
        run_present(chosen, teams_present, host, emit, say)
        return
    for member in chosen:
        say(f"{member.label}: leaving team {member.team}")
        _absent_one(host, member, teams_present, emit)


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


def real_host() -> Host:
    system = checkout.real_system()
    return Host(container_state=_container_state, agent_bus=_agent_bus, ip_json=_ip_json,
                is_bridge=lambda dev: pathlib.Path(f"/sys/class/net/{dev}/bridge").is_dir(),
                team_active=system.team_active, bus_address=system.bus_address, team_file=_team_file,
                kit_tar=lambda: kit_tar(pathlib.Path(KIT_PARENT)), guest=LxcGuest)


def main(argv: Sequence[str] | None = None, *, host_factory: Callable[[], Host] = real_host,
         stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    parser = argparse.ArgumentParser(prog=PROG, description="Join existing LXC containers to agent teams.")
    parser.add_argument("--phase", choices=STATES, required=True)
    parser.add_argument("--members", required=True, help="agent_bus_lxc_members, as JSON")
    parser.add_argument("--teams-present", required=True,
                        help="the names of the agent_bus_teams declared present, as a JSON list")
    args = parser.parse_args(argv)

    def emit(what: str) -> None:
        out.write(f"CHANGED\t{what}\n")
        out.flush()

    def say(text: str) -> None:
        err.write(f"{PROG}: {text}\n")

    try:
        data, teams = json.loads(args.members), json.loads(args.teams_present)
    except ValueError as exc:
        say(f"--members and --teams-present must be JSON: {exc}")
        return EXIT_USAGE
    try:
        if not isinstance(teams, list) or not all(isinstance(team, str) for team in teams):
            raise MemberError("--teams-present must be a JSON list of team names")
        members = parse_members(data)
        if any(member.state == args.phase for member in members):
            run_phase(args.phase, members, teams, host_factory(), emit=emit, say=say)
    except (MemberError, checkout.CheckoutError, teamfile.TeamFileError) as exc:
        say(str(exc))
        return EXIT_REFUSED
    return 0


if __name__ == "__main__":
    sys.exit(main())
