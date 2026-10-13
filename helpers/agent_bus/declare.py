"""The agent_bus_teams entries in the untracked host_vars, read and added to (Plan 00161).

`ccy-teams` lists the teams declared there and `ccy-teams create` adds one; nothing here
creates a team: play-agent-bus.yml applies the entry. The file may hold vault-encrypted
values and the helpers are stdlib only, so there is no YAML parser. The file is read as
lines, in the one shape the commented example in `localhost.yml.dist` has (a block list of
block mappings under a top-level `agent_bus_teams:`), and the new entry is inserted as
lines after the list's last item, in the list's own indentation. Any other shape is refused,
naming the line, rather than guessed at. A value is never printed, and the file is backed up
beside itself (`localhost.yml.backup.<time>`, which .gitignore covers) before it is replaced.

The entry is validated by the team file's own rules (`teamfile.parse_team_file`) before it
is written. The port is chosen, never asked: the first in AUTO_PORT_FIRST..AUTO_PORT_LAST
that no entry declares and nothing listens on (`ss -Hltn`). `listen` is the host's bus
address (`agent_bus_address`), `path_prefixes` and `forge_api` are the example's.

    python3 -m helpers.agent_bus.declare declared --file <localhost.yml>
        TEAM<TAB><team><TAB><port or -><TAB><present|absent> per entry
    python3 -m helpers.agent_bus.declare create --file <localhost.yml> --team T --human NAME...
            --repo OWNER/REPO=BRANCH[,BRANCH]... [--allow-from CIDR]... [--dry-run]
        CHANGED<TAB>team <team>, port <port>, in <file>, and the next steps on stderr;
        --dry-run prints the entry's lines instead and writes nothing
    python3 -m helpers.agent_bus.declare check --file <localhost.yml> [--team T] [--human NAME]...
            [--repo ...]... [--allow-from CIDR]... [--owner OWNER]
        nothing when every value given is valid; exit 64 naming the first that is not
    python3 -m helpers.agent_bus.declare gh-repos --owner OWNER  < gh repo list's JSON
        COUNT<TAB>n, then REPO<TAB>owner/repo<TAB>branch and EMPTY<TAB>owner/repo lines
    python3 -m helpers.agent_bus.declare github-remote --url URL
        the lower-case OWNER/REPO of a GitHub remote, or nothing

Nothing here prompts: ccy-teams asks the questions and passes the answers as flags, each
checked by `check` as it is given. Exit 0 done, 64 usage, 78 refused, 130 Ctrl-C (nothing
written: the file is replaced whole, after its backup, in one rename).
"""

from __future__ import annotations

import argparse
import datetime
import ipaddress
import json
import os
import pathlib
import re
import subprocess
import sys
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import TextIO

from helpers.agent_bus import teamfile
from helpers.pingbus import protocol

TEAMS_KEY = "agent_bus_teams"
ADDRESS_KEY = "agent_bus_address"
AUTO_PORT_FIRST = 6167
AUTO_PORT_LAST = 6266
DEFAULT_PATH_PREFIXES = ("CLAUDE/Plan/", "docs/")
DEFAULT_FORGE_API = "https://api.github.com"
DEFAULT_ITEM_INDENT = 2
EXIT_OK = 0
EXIT_USAGE = 64
EXIT_REFUSED = 78
EXIT_INTERRUPTED = 130

_KEY_LINE_RE = re.compile(r"(?P<key>[a-z_]+):(?P<rest>(?:\s.*)?)")
_TOP_KEY_RE = re.compile(rf"{TEAMS_KEY}\s*:(?P<rest>.*)")
_ADDRESS_RE = re.compile(rf"{ADDRESS_KEY}\s*:(?P<rest>.*)")

# A template team file each field is checked against, one field replaced: the team file's
# own validator then names exactly that field's rule.
_TEMPLATE = {
    "team": "template",
    "port": AUTO_PORT_FIRST,
    "listen": [],
    "allow_from": [],
    "humans": ["template"],
    "repos": [{"repo": "template/template", "branches": ["main"]}],
    "path_prefixes": list(DEFAULT_PATH_PREFIXES),
    "forge_api": DEFAULT_FORGE_API,
}


class DeclareError(Exception):
    """Refused: the message says why, and never quotes a value of the file."""


class UsageError(Exception):
    """A flag is wrong: exit 64."""


@dataclass(frozen=True)
class Declared:
    team: str
    port: int | None
    state: str


@dataclass(frozen=True)
class HostVars:
    lines: tuple[str, ...]
    teams: tuple[Declared, ...]
    address: str | None
    key_line: int | None  # the index of `agent_bus_teams:`, or None
    insert_at: int  # where a new entry's lines go
    item_indent: int
    key_indent: int


def _scalar(rest: str, where: str) -> str:
    """A plain or quoted one-line scalar, its comment removed; a tag is refused."""
    value = re.sub(r"(^|\s)#.*$", "", rest).strip()
    if value.startswith("!"):
        raise DeclareError(f"{where} is tagged (vault-encrypted?); ccy-teams reads only plain values")
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        value = value[1:-1]
    return value


def _address(lines: Sequence[str]) -> str | None:
    found = [m for m in (_ADDRESS_RE.fullmatch(line.rstrip("\r\n")) for line in lines) if m]
    if len(found) > 1:
        raise DeclareError(f"{ADDRESS_KEY} is set more than once")
    if not found:
        return None
    try:
        return str(ipaddress.ip_address(_scalar(found[0]["rest"], ADDRESS_KEY)))
    except (DeclareError, ValueError):
        return None


def parse(text: str) -> HostVars:
    """The declared teams and the bus address; DeclareError for any shape not recognised."""
    lines = tuple(text.splitlines(keepends=True))
    keys = []
    for index, line in enumerate(lines):
        bare = line.rstrip("\r\n")
        if TEAMS_KEY in bare and not bare.lstrip().startswith("#"):
            if not _TOP_KEY_RE.fullmatch(bare):
                raise DeclareError(f"line {index + 1} names {TEAMS_KEY} in a form ccy-teams does not edit")
            keys.append(index)
    if len(keys) > 1:
        raise DeclareError(f"{TEAMS_KEY} is set more than once (lines {', '.join(str(k + 1) for k in keys)})")
    address = _address(lines)
    if not keys:
        return HostVars(lines, (), address, None, len(lines), DEFAULT_ITEM_INDENT, DEFAULT_ITEM_INDENT + 2)
    key = keys[0]
    rest = _scalar(_TOP_KEY_RE.fullmatch(lines[key].rstrip("\r\n"))["rest"], TEAMS_KEY)
    if rest:
        raise DeclareError(f"{TEAMS_KEY} on line {key + 1} is written inline; ccy-teams adds only to a "
                           "block list (as in localhost.yml.dist)")
    content: list[int] = []
    index = key + 1
    while index < len(lines):
        bare = lines[index].rstrip("\r\n")
        if "\t" in bare[: len(bare) - len(bare.lstrip())]:
            raise DeclareError(f"line {index + 1} is indented with a tab")
        if not bare.strip() or bare.lstrip().startswith("#"):
            index += 1
            continue
        if bare[0] == " " or bare.startswith("- "):
            content.append(index)
            index += 1
            continue
        break
    if not content:
        return HostVars(lines, (), address, key, key + 1, DEFAULT_ITEM_INDENT, DEFAULT_ITEM_INDENT + 2)
    teams, item_indent, key_indent = _items(lines, content)
    return HostVars(lines, teams, address, key, content[-1] + 1, item_indent, key_indent)


def _items(lines: Sequence[str], content: Sequence[int]) -> tuple[tuple[Declared, ...], int, int]:
    first = lines[content[0]].rstrip("\r\n")
    item_indent = len(first) - len(first.lstrip(" "))
    key_indent = -1
    items: list[dict[str, tuple[str, int]]] = []
    for index in content:
        bare = lines[index].rstrip("\r\n")
        indent = len(bare) - len(bare.lstrip(" "))
        body = bare[indent:]
        where = f"line {index + 1}"
        if indent == item_indent and body.startswith("- "):
            after = body[2:]
            pad = len(after) - len(after.lstrip(" "))
            this_key_indent = item_indent + 2 + pad
            if key_indent not in (-1, this_key_indent):
                raise DeclareError(f"{where}: the {TEAMS_KEY} items are not indented alike")
            key_indent = this_key_indent
            items.append({})
            body = after.lstrip(" ")
        elif indent == key_indent and items:
            pass
        elif indent > key_indent >= 0 and items:
            continue
        else:
            raise DeclareError(f"{where}: not the {TEAMS_KEY} shape ccy-teams edits (a block list of mappings)")
        m = _KEY_LINE_RE.fullmatch(body)
        if not m:
            raise DeclareError(f"{where}: each {TEAMS_KEY} item must be a block mapping of key: value lines")
        if m["key"] in items[-1]:
            raise DeclareError(f"{where}: {m['key']} is set twice in one {TEAMS_KEY} item")
        items[-1][m["key"]] = (m["rest"], index)
    return tuple(_declared(item) for item in _unique(items)), item_indent, key_indent


def _unique(items: list[dict[str, tuple[str, int]]]) -> list[dict[str, tuple[str, int]]]:
    seen: set[str] = set()
    for item in items:
        if "team" not in item:
            line = min(index for _, index in item.values()) + 1
            raise DeclareError(f"the {TEAMS_KEY} item at line {line} has no team")
        name = _scalar(item["team"][0], f"the team on line {item['team'][1] + 1}")
        if name in seen:
            raise DeclareError(f"team {name} is declared twice in {TEAMS_KEY}")
        seen.add(name)
    return items


def _declared(item: dict[str, tuple[str, int]]) -> Declared:
    rest, index = item["team"]
    team = _scalar(rest, f"the team on line {index + 1}")
    if not protocol.is_team_name(team):
        raise DeclareError(f"line {index + 1}: the team name does not match {protocol.TEAM_NAME_PATTERN}")
    port = None
    if "port" in item:
        rest, index = item["port"]
        value = _scalar(rest, f"the port on line {index + 1}")
        if not value.isdigit():
            raise DeclareError(f"line {index + 1}: the port is not a number")
        port = int(value)
    state = teamfile.DEFAULT_STATE
    if "state" in item:
        rest, index = item["state"]
        state = _scalar(rest, f"the state on line {index + 1}")
        if state not in teamfile.STATES:
            raise DeclareError(f"line {index + 1}: state must be one of {', '.join(teamfile.STATES)}")
    return Declared(team, port, state)


# ---------------------------------------------------------------- the port


def listening_ports(ss_output: str) -> set[int]:
    """The local TCP ports `ss -Hltn` shows listening."""
    ports: set[int] = set()
    for line in ss_output.splitlines():
        fields = line.split()
        if not fields:
            continue
        if len(fields) < 4 or ":" not in fields[3] or not fields[3].rsplit(":", 1)[1].isdigit():
            raise DeclareError("ss -Hltn printed a line that is not a listening socket, so the free ports are unknown")
        ports.add(int(fields[3].rsplit(":", 1)[1]))
    return ports


def choose_port(used: set[int]) -> int:
    for port in range(AUTO_PORT_FIRST, AUTO_PORT_LAST + 1):
        if port not in used:
            return port
    raise DeclareError(f"every port from {AUTO_PORT_FIRST} to {AUTO_PORT_LAST} is declared or listening")


def _ss() -> str:
    try:
        return subprocess.run(["ss", "-Hltn"], check=True, capture_output=True, text=True).stdout
    except FileNotFoundError:
        raise DeclareError("ss (iproute) is not installed, so the free ports are unknown") from None
    except subprocess.CalledProcessError as exc:
        raise DeclareError(f"ss -Hltn failed ({exc.stderr.strip()}), so the free ports are unknown") from None


# ---------------------------------------------------------------- the entry


def field_error(key: str, value: object) -> str | None:
    """Why the team file's validator refuses this one value, or None when it accepts it."""
    data = dict(_TEMPLATE, **{key: value})
    try:
        teamfile.parse_team_file(data)
    except teamfile.TeamFileError as exc:
        return str(exc)
    return None


def build_entry(*, team: str, port: int, listen: Sequence[str], allow_from: Sequence[str],
                humans: Sequence[str], repos: Sequence[tuple[str, Sequence[str]]]) -> dict:
    """The new agent_bus_teams entry, checked by the team file's validator."""
    entry = {
        "team": team,
        "port": port,
        "listen": list(listen),
        "allow_from": list(allow_from),
        "humans": list(humans),
        "repos": [{"repo": repo, "branches": list(branches)} for repo, branches in repos],
        "path_prefixes": list(DEFAULT_PATH_PREFIXES),
        "forge_api": DEFAULT_FORGE_API,
    }
    try:
        teamfile.parse_team_file(entry)
    except teamfile.TeamFileError as exc:
        raise DeclareError(str(exc)) from None
    return entry


def render_entry(entry: dict, item_indent: int, key_indent: int) -> list[str]:
    """The entry as YAML lines in the list's indentation; lists in flow (JSON) style."""
    pad, key_pad = " " * item_indent, " " * key_indent
    lines = [f"{pad}- team: {entry['team']}\n", f"{key_pad}port: {entry['port']}\n"]
    for key in ("listen", "allow_from", "humans"):
        lines.append(f"{key_pad}{key}: {json.dumps(entry[key])}\n")
    lines.append(f"{key_pad}repos:\n")
    for repo in entry["repos"]:
        lines.append(f"{key_pad}  - repo: {json.dumps(repo['repo'])}\n")
        lines.append(f"{key_pad}    branches: {json.dumps(repo['branches'])}\n")
    lines.append(f"{key_pad}path_prefixes: {json.dumps(entry['path_prefixes'])}\n")
    lines.append(f"{key_pad}forge_api: {json.dumps(entry['forge_api'])}\n")
    return lines


def insert(hv: HostVars, entry: dict) -> str:
    """The file's text with the entry added; the rest of it byte for byte as it was."""
    if any(t.team == entry["team"] for t in hv.teams):
        raise DeclareError(f"team {entry['team']} is already declared in {TEAMS_KEY}")
    if any(t.port == entry["port"] for t in hv.teams):
        raise DeclareError(f"port {entry['port']} is already declared in {TEAMS_KEY}")
    lines = list(hv.lines)
    if lines and not lines[-1].endswith("\n"):
        lines[-1] += "\n"
    added = render_entry(entry, hv.item_indent, hv.key_indent)
    if hv.key_line is None:
        added = ([] if not lines else ["\n"]) + [f"{TEAMS_KEY}:\n"] + added
        at = len(lines)
    else:
        at = hv.insert_at
    new = "".join(lines[:at] + added + lines[at:])
    after = parse(new)
    if [t.team for t in after.teams] != [*(t.team for t in hv.teams), entry["team"]]:
        raise DeclareError("the edited file does not read back with the new team last; nothing was written")
    return new


def write(path: pathlib.Path, text: str) -> pathlib.Path:
    """Back the file up beside itself, then replace it whole; the mode is kept. Returns the
    backup's path."""
    mode = path.stat().st_mode & 0o7777
    stamp = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%dT%H%M%S%fZ")
    backup = path.with_name(f"{path.name}.backup.{stamp}")
    original = path.read_bytes()
    _write_new(backup, original, mode)
    tmp = path.with_name(f".{path.name}.ccy-teams.{os.getpid()}")
    try:
        _write_new(tmp, text.encode("utf-8"), mode)
        os.replace(tmp, path)
    finally:
        if tmp.exists():
            tmp.unlink()
    return backup


def _write_new(path: pathlib.Path, data: bytes, mode: int) -> None:
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    with os.fdopen(fd, "wb") as handle:
        handle.write(data)
        handle.flush()
        os.fsync(handle.fileno())
    os.chmod(path, mode)


# ---------------------------------------------------------------- the commands


NO_ADDRESS = (
    "{path} sets no {key}. It is the address on this machine that every team's homeserver "
    "listens on, and it is used for nothing else: an IP of this machine that the team's agents "
    "and your Element can reach, such as its WireGuard address, or one added for the bus. Set it "
    "in {path}, for example `{key}: 192.0.2.10` (see localhost.yml.dist), and run "
    "playbooks/imports/play-agent-bus.yml once to bring it up; then run ccy-teams create again"
)


def _split(text: str) -> list[str]:
    return [part.strip() for part in text.split(",") if part.strip()]


def _parse_repo_flag(value: str) -> tuple[str, list[str]]:
    repo, sep, branches = value.partition("=")
    if not sep:
        raise UsageError(f"--repo {value}: give it as OWNER/REPO=BRANCH[,BRANCH]")
    problem = field_error("repos", [{"repo": repo, "branches": _split(branches)}])
    if problem:
        raise UsageError(f"--repo {value}: {problem}")
    return repo, _split(branches)


def _check_flags(hv: HostVars, args: argparse.Namespace) -> list[tuple[str, list[str]]]:
    """Every value the flags give, by the team file's rules; UsageError names the first bad one.
    Returns the repos, parsed."""
    if args.team is not None:
        if any(t.team == args.team for t in hv.teams):
            raise UsageError(f"--team: team {args.team} is already declared in {TEAMS_KEY}")
        problem = field_error("team", args.team)
        if problem:
            raise UsageError(f"--team: {problem}")
    for flag, key, value in (("--human", "humans", args.human), ("--allow-from", "allow_from", args.allow_from)):
        if value:
            problem = field_error(key, value)
            if problem:
                raise UsageError(f"{flag}: {problem}")
    owner = getattr(args, "owner", None)
    if owner is not None and not protocol.is_owner_repo(f"{owner}/x"):
        raise UsageError(f"--owner: {owner!r} must be a lower-case GitHub owner (an org or a user)")
    return [_parse_repo_flag(value) for value in args.repo or []]


def check(args: argparse.Namespace) -> int:
    _check_flags(parse(_read(pathlib.Path(args.file))), args)
    return EXIT_OK


def create(args: argparse.Namespace, ss_output: Callable[[], str], out: TextIO, err: TextIO) -> int:
    path = pathlib.Path(args.file)
    hv = parse(_read(path))
    if hv.address is None:
        raise DeclareError(NO_ADDRESS.format(path=path, key=ADDRESS_KEY))
    missing = [flag for flag, value in (("--team", args.team), ("--human", args.human), ("--repo", args.repo))
               if not value]
    if missing:
        raise UsageError(f"create needs {', '.join(missing)} (ccy-teams create asks for them on a terminal)")
    repos = _check_flags(hv, args)
    port = choose_port({t.port for t in hv.teams if t.port is not None} | listening_ports(ss_output()))
    entry = build_entry(team=args.team, port=port, listen=[hv.address], allow_from=args.allow_from or [],
                        humans=args.human, repos=repos)
    new = insert(hv, entry)
    if args.dry_run:
        out.writelines(render_entry(entry, hv.item_indent, hv.key_indent))
        return EXIT_OK
    backup = write(path, new)
    out.write(f"CHANGED\tteam {args.team}, port {port}, in {path}\n")
    err.write(next_steps(path, backup, args.team, port, hv.address))
    return EXIT_OK


def next_steps(path: pathlib.Path, backup: pathlib.Path, team: str, port: int, address: str) -> str:
    """What happens now, in order: nothing is installed until the play runs."""
    return (
        f"\nTeam {team} is declared in {path}, on port {port}.\n"
        f"The file as it was is kept at {backup}.\n\n"
        "Next:\n"
        "  1. Install it. Nothing exists until play-agent-bus.yml runs: add\n"
        "     playbooks/imports/play-agent-bus.yml to PLANS in CLAUDE/Plan/meta-deploy.bash and run\n"
        "     ./CLAUDE/Plan/meta-deploy.bash. It creates the homeserver, the room and the humans' accounts.\n"
        f"  2. Watch it in Element: add this item under agent_bus_element_teams in {path.name},\n"
        f"     {{team: {team}, base_url: \"http://{_url_host(address)}:{port}\"}}\n"
        "     and run playbooks/imports/optional/common/play-agent-bus-element.yml.\n"
        f"  3. Sign in: in ccy-teams, Ctrl-E on {team} offers each human a new Element password,\n"
        "     shown once.\n"
        f"  4. Bring in agents: in a checkout of one of the team's repositories, run ccy --teams 1@{team}\n"
        "     (or Ctrl-N on the team in ccy-teams).\n"
    )


# ---------------------------------------------------------------- GitHub


_GITHUB_REMOTE_RE = re.compile(
    r"(?:git@github\.com[^:/]*:|ssh://git@github\.com[^/]*/|https://github\.com/)"
    r"(?P<owner>[^/]+)/(?P<repo>[^/]+?)(?:\.git)?/?")


def github_remote(url: str) -> str | None:
    """The lower-case OWNER/REPO a GitHub remote URL names (an alias host such as
    github.com-work included), or None."""
    m = _GITHUB_REMOTE_RE.fullmatch(url.strip())
    if m is None:
        return None
    name = f"{m['owner']}/{m['repo']}".lower()
    return name if protocol.is_owner_repo(name) else None


def gh_repos(owner: str, data: object) -> list[str]:
    """`gh repo list <owner> --json name,defaultBranchRef,isArchived` as lines: COUNT (every
    repository gh listed, so a caller can tell a list cut at its limit), then REPO
    <owner/repo> <default branch> per live repository and EMPTY <owner/repo> per one with no
    default branch (no commits), each group sorted. Archived repositories are left out."""
    if not isinstance(data, list) or not all(isinstance(item, dict) and isinstance(item.get("name"), str)
                                             for item in data):
        raise DeclareError("gh repo list did not print the expected JSON list of repositories")
    repos, empty = [], []
    for item in data:
        if item.get("isArchived") is True:
            continue
        name = f"{owner}/{item['name']}".lower()
        ref = item.get("defaultBranchRef")
        branch = ref.get("name") if isinstance(ref, dict) else None
        if isinstance(branch, str) and protocol.is_branch_name(branch):
            repos.append(f"REPO\t{name}\t{branch}")
        else:
            empty.append(f"EMPTY\t{name}")
    return [f"COUNT\t{len(data)}", *sorted(repos), *sorted(empty)]


def _url_host(address: str) -> str:
    return f"[{address}]" if ":" in address else address


def _read(path: pathlib.Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except FileNotFoundError:
        raise DeclareError(f"{path} does not exist (copy localhost.yml.dist to make it)") from None


def declared(args: argparse.Namespace, out: TextIO) -> int:
    for team in parse(_read(pathlib.Path(args.file))).teams:
        out.write(f"TEAM\t{team.team}\t{team.port if team.port is not None else '-'}\t{team.state}\n")
    return EXIT_OK


def build_parser() -> argparse.ArgumentParser:
    class Parser(argparse.ArgumentParser):
        def error(self, message: str) -> None:
            raise UsageError(message)

    parser = Parser(prog="declare", description="agent_bus_teams in the host_vars")
    commands = parser.add_subparsers(dest="command", required=True)
    listing = commands.add_parser("declared", help="one TEAM line per entry")
    listing.add_argument("--file", required=True)
    for name, help_text in (("create", "add a team's entry"), ("check", "validate the values given")):
        sub = commands.add_parser(name, help=help_text)
        sub.add_argument("--file", required=True)
        sub.add_argument("--team")
        sub.add_argument("--human", action="append")
        sub.add_argument("--repo", action="append", metavar="OWNER/REPO=BRANCH[,BRANCH]")
        sub.add_argument("--allow-from", action="append", dest="allow_from", metavar="CIDR")
        if name == "create":
            sub.add_argument("--dry-run", action="store_true", help="print the entry; write nothing")
        else:
            sub.add_argument("--owner", help="a GitHub owner (an org or a user)")
    repos = commands.add_parser("gh-repos", help="gh repo list's JSON (stdin) as REPO lines")
    repos.add_argument("--owner", required=True)
    remote = commands.add_parser("github-remote", help="OWNER/REPO of a GitHub remote URL, or nothing")
    remote.add_argument("--url", required=True)
    return parser


def main(argv: Sequence[str] | None = None, *, ss_output: Callable[[], str] = _ss,
         stdin: TextIO | None = None, stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    try:
        args = build_parser().parse_args(argv)
        if args.command == "declared":
            return declared(args, out)
        if args.command == "check":
            return check(args)
        if args.command == "gh-repos":
            try:
                data = json.load(sys.stdin if stdin is None else stdin)
            except json.JSONDecodeError:
                raise DeclareError("gh repo list did not print JSON") from None
            out.writelines(f"{line}\n" for line in gh_repos(args.owner, data))
            return EXIT_OK
        if args.command == "github-remote":
            name = github_remote(args.url)
            if name:
                out.write(f"{name}\n")
            return EXIT_OK
        return create(args, ss_output, out, err)
    except KeyboardInterrupt:
        err.write("Cancelled; nothing was changed.\n")
        return EXIT_INTERRUPTED
    except UsageError as exc:
        err.write(f"declare: {exc}\n")
        return EXIT_USAGE
    except DeclareError as exc:
        err.write(f"declare: {exc}\n")
        return EXIT_REFUSED


if __name__ == "__main__":
    sys.exit(main())
