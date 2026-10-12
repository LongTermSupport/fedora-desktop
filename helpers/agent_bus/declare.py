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
    python3 -m helpers.agent_bus.declare create --file <localhost.yml> [--team T]
            [--human NAME]... [--repo OWNER/REPO=BRANCH[,BRANCH]]... [--allow-from CIDR]... [--yes]
        prompts for what the flags leave out, then CHANGED<TAB>team <team>, port <port>, in <file>;
        with --yes it asks nothing more, and allow_from is empty unless --allow-from names one

Exit 0 done, 1 cancelled (nothing written), 64 usage, 78 refused.
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
MAX_TRIES = 3

EXIT_OK = 0
EXIT_CANCELLED = 1
EXIT_USAGE = 64
EXIT_REFUSED = 78

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
    for line in lines:
        m = _ADDRESS_RE.fullmatch(line.rstrip("\r\n"))
        if m:
            try:
                value = _scalar(m["rest"], ADDRESS_KEY)
                return str(ipaddress.ip_address(value))
            except (DeclareError, ValueError):
                return None
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


# ---------------------------------------------------------------- the prompts


Ask = Callable[[str], "str | None"]


class Cancelled(Exception):
    pass


def _terminal_ask(prompt: str) -> str | None:
    sys.stderr.write(prompt)
    sys.stderr.flush()
    line = sys.stdin.readline()
    if not line:
        sys.stderr.write("\n")
        return None
    return line.rstrip("\n")


def _split(text: str) -> list[str]:
    return [part.strip() for part in text.split(",") if part.strip()]


def _prompt(ask: Ask, err: TextIO, prompt: str, check: Callable[[str], str | None]) -> str:
    """Ask until check accepts the answer (None), MAX_TRIES at most."""
    for _ in range(MAX_TRIES):
        answer = ask(prompt)
        if answer is None:
            raise Cancelled("no input")
        problem = check(answer.strip())
        if problem is None:
            return answer.strip()
        err.write(f"  {problem}. Try again.\n")
    raise Cancelled(f"giving up after {MAX_TRIES} tries")


def _team_check(hv: HostVars) -> Callable[[str], str | None]:
    def check(value: str) -> str | None:
        if any(t.team == value for t in hv.teams):
            return f"team {value} is already declared in {TEAMS_KEY}"
        return field_error("team", value)
    return check


def _repo_check(value: str) -> str | None:
    return field_error("repos", [{"repo": value, "branches": ["main"]}])


def _ask_repos(ask: Ask, err: TextIO) -> list[tuple[str, list[str]]]:
    repos: list[tuple[str, list[str]]] = []
    while len(repos) < protocol.REPOS_MAX:
        def repo_check(value: str) -> str | None:
            if not value:
                return None if repos else "a team needs at least one repository"
            if any(value == known for known, _ in repos):
                return f"{value} is listed already"
            return _repo_check(value)
        repo = _prompt(ask, err, "Repository the team works on, OWNER/REPO (empty when done): ", repo_check)
        if not repo:
            break

        def branches_check(value: str, repo: str = repo) -> str | None:
            return field_error("repos", [{"repo": repo, "branches": _split(value)}])
        branches = _prompt(ask, err, f"Branches of {repo} pings may name, comma-separated: ", branches_check)
        repos.append((repo, _split(branches)))
    return repos


def _parse_repo_flag(value: str) -> tuple[str, list[str]]:
    repo, sep, branches = value.partition("=")
    if not sep:
        raise UsageError(f"--repo {value}: give it as OWNER/REPO=BRANCH[,BRANCH]")
    problem = field_error("repos", [{"repo": repo, "branches": _split(branches)}])
    if problem:
        raise UsageError(f"--repo {value}: {problem}")
    return repo, _split(branches)


def create(args: argparse.Namespace, ask: Ask, ss_output: Callable[[], str], out: TextIO, err: TextIO) -> int:
    path = pathlib.Path(args.file)
    hv = parse(_read(path))
    if hv.address is None:
        raise DeclareError(f"{path} sets no plain {ADDRESS_KEY}, which a team listens on: set it "
                           "(see localhost.yml.dist) and run play-agent-bus.yml first")
    for key, value in (("team", args.team), ("humans", args.human or None), ("allow_from", args.allow_from)):
        if value is not None:
            problem = _team_check(hv)(value) if key == "team" else field_error(key, value)
            if problem:
                raise UsageError(f"--{key.replace('_', '-').rstrip('s')}: {problem}")
    repos = [_parse_repo_flag(value) for value in args.repo or []]
    try:
        team = args.team or _prompt(ask, err, "Team name (lower-case letters, digits and -, up to 24): ",
                                    _team_check(hv))
        humans = args.human or _split(_prompt(
            ask, err, "Humans of the team, comma-separated (their Matrix account names): ",
            lambda v: field_error("humans", _split(v))))
        repos = repos or _ask_repos(ask, err)
        allow_from = args.allow_from
        if allow_from is None and args.yes:
            allow_from = []
        if allow_from is None:
            allow_from = _split(_prompt(
                ask, err, "Subnets that may connect besides this machine, CIDR, comma-separated "
                "(empty for none): ", lambda v: field_error("allow_from", _split(v))))
        port = choose_port({t.port for t in hv.teams if t.port is not None} | listening_ports(ss_output()))
        entry = build_entry(team=team, port=port, listen=[hv.address], allow_from=allow_from,
                            humans=humans, repos=repos)
        new = insert(hv, entry)
        err.write(f"\nThe entry for {path}:\n\n")
        err.writelines(render_entry(entry, hv.item_indent, hv.key_indent))
        err.write("\n")
        if not args.yes:
            answer = ask(f"Add it to {path}? [y/N] ")
            if answer is None or answer.strip().lower() not in ("y", "yes"):
                raise Cancelled("not confirmed")
    except Cancelled as why:
        err.write(f"Cancelled ({why}). No changes made.\n")
        return EXIT_CANCELLED
    backup = write(path, new)
    out.write(f"CHANGED\tteam {team}, port {port}, in {path}\n")
    err.write(
        f"The file as it was is kept at {backup}.\n"
        "Nothing exists until play-agent-bus.yml applies it: add playbooks/imports/play-agent-bus.yml "
        "to PLANS in CLAUDE/Plan/meta-deploy.bash and run ./CLAUDE/Plan/meta-deploy.bash.\n"
        f"To watch it in Element, add {{team: {team}, base_url: http://{_url_host(hv.address)}:{port}}} to "
        "agent_bus_element_teams and run playbooks/imports/optional/common/play-agent-bus-element.yml.\n")
    return EXIT_OK


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
    make = commands.add_parser("create", help="add a team's entry")
    make.add_argument("--file", required=True)
    make.add_argument("--team")
    make.add_argument("--human", action="append")
    make.add_argument("--repo", action="append", metavar="OWNER/REPO=BRANCH[,BRANCH]")
    make.add_argument("--allow-from", action="append", dest="allow_from", metavar="CIDR")
    make.add_argument("--yes", action="store_true")
    return parser


def main(argv: Sequence[str] | None = None, *, ask: Ask = _terminal_ask,
         ss_output: Callable[[], str] = _ss, stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    try:
        args = build_parser().parse_args(argv)
        if args.command == "declared":
            return declared(args, out)
        return create(args, ask, ss_output, out, err)
    except UsageError as exc:
        err.write(f"declare: {exc}\n")
        return EXIT_USAGE
    except DeclareError as exc:
        err.write(f"declare: {exc}\n")
        return EXIT_REFUSED


if __name__ == "__main__":
    sys.exit(main())
