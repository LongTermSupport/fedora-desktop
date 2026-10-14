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
is written. It trusts its `org`, every repository of that GitHub owner on any of its own
branches (Plan 00161 DESIGN.md D64), or the repositories it lists under `repos`, of any
owners, each on any branch (no `branches`; Plan 00177 Task 4.9). The team's Element profile
is an `agent_bus_element_teams` entry, added the same way (`element`). The port is chosen,
never asked: the first in AUTO_PORT_FIRST..AUTO_PORT_LAST
that no entry declares and nothing listens on (`ss -Hltn`). `listen` is the host's bus
address (`agent_bus_address`) and `forge_api` the example's. No `path_prefixes` is written: a
trusted reference may name any path in the team's repositories (a team narrows it by hand).

    python3 -m helpers.agent_bus.declare declared --file <localhost.yml>
        TEAM<TAB><team><TAB><port or -><TAB><present|absent><TAB><server_name><TAB><url or -> per
        entry; the URL is http://<first listen address, or agent_bus_address>:<port>
    python3 -m helpers.agent_bus.declare create --file <localhost.yml> --team T --human NAME...
            (--org ORG | --repo OWNER/REPO...) [--allow-from CIDR]... [--dry-run]
        CHANGED<TAB>team <team>, port <port>, in <file>, and the next steps on stderr;
        --dry-run prints the entry's lines instead and writes nothing; ccy-teams then installs it
    python3 -m helpers.agent_bus.declare next-steps --file <localhost.yml> --team T
        what is left once the team is installed (sign in, Element, ccy --teams), in ccy-teams
    python3 -m helpers.agent_bus.declare check --file <localhost.yml> [--team T] [--human NAME]...
            [--org ORG] [--repo OWNER/REPO]... [--allow-from CIDR]...
        nothing when every value given is valid; exit 64 naming the first that is not
    python3 -m helpers.agent_bus.declare suggest-team --file <localhost.yml> --name NAME
        a GitHub owner's or repository's name as a free team name, or nothing
    python3 -m helpers.agent_bus.declare element --file <localhost.yml> --team T
        adds the team's agent_bus_element_teams entry (base_url its homeserver), or brings
        its base_url up to date: CHANGED<TAB>...; UNCHANGED<TAB><team> when already right
    python3 -m helpers.agent_bus.declare element-teams --file <localhost.yml>
        ELEMENT<TAB><team><TAB><base_url> per agent_bus_element_teams entry
    python3 -m helpers.agent_bus.declare teams-for --file <localhost.yml> --repo OWNER/REPO
        TEAM<TAB><team> per present team whose org owns the repository, or that lists it
    python3 -m helpers.agent_bus.declare limited --file <localhost.yml>
        LIMITED<TAB><team><TAB><prefixes> per present team that sets path_prefixes
    python3 -m helpers.agent_bus.declare drop-limit --file <localhost.yml> --team T
        removes the team's path_prefixes, every other line as it was: CHANGED<TAB>...
    python3 -m helpers.agent_bus.declare github-remote --url URL
        the lower-case OWNER/REPO of a GitHub remote, or nothing
    python3 -m helpers.agent_bus.declare status --file <localhost.yml> --source <repo>
            --applied <dir> [--installed TEAM]...
        STATUS<TAB><team><TAB><state><TAB><detail or -> per team declared or applied (D65):
        current, outdated (config, software, both, or unstamped), unbuilt, removed,
        decide (absent: declared absent but still applied; undeclared: applied, not
        declared) or unreadable (an entry ccy-teams cannot read; the reason)
    python3 -m helpers.agent_bus.declare overview --file <localhost.yml> --source <repo>
            --applied <dir> [--installed TEAM]...
        what `declared` prints, then what `status` prints, from one run (ccy-teams redraws the
        list on every key and starts one process instead of two)
    python3 -m helpers.agent_bus.declare remove --file <localhost.yml> --team T [--purge] [--dry-run]
        sets the team's state: absent and purge: false (true with --purge); CHANGED<TAB>...

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

from helpers.agent_bus import installer_digest, teamfile
from helpers.pingbus import bundle, protocol

TEAMS_KEY = "agent_bus_teams"
#: The teams this desktop's human watches in Element (play-agent-bus-element.yml).
ELEMENT_KEY = "agent_bus_element_teams"
ADDRESS_KEY = "agent_bus_address"
AUTO_PORT_FIRST = 6167
AUTO_PORT_LAST = 6266
DEFAULT_FORGE_API = "https://api.github.com"
DEFAULT_ITEM_INDENT = 2
EXIT_OK = 0
EXIT_USAGE = 64
EXIT_REFUSED = 78
EXIT_INTERRUPTED = 130

_KEY_LINE_RE = re.compile(r"(?P<key>[a-z_]+):(?P<rest>(?:\s.*)?)")
_ADDRESS_RE = re.compile(rf"{ADDRESS_KEY}\s*:(?P<rest>.*)")

# A template team file each field is checked against, one field replaced: the team file's
# own validator then names exactly that field's rule.
_TEMPLATE = {
    "team": "template",
    "port": AUTO_PORT_FIRST,
    "listen": [],
    "allow_from": [],
    "humans": ["template"],
    "org": "template",
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
    server_name: str = ""
    listen: str | None = None  # the first listen address, when written as a flow list
    #: Each key as written: (key, the rest of its line, its line, the lines nested under it).
    raw: tuple[tuple[str, str, int, tuple[str, ...]], ...] = ()


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


def parse(text: str, list_key: str = TEAMS_KEY) -> HostVars:
    """The entries of one top-level list (the declared teams, by default; or the Element
    profiles, ELEMENT_KEY) and the bus address; DeclareError for any shape not recognised."""
    lines = tuple(text.splitlines(keepends=True))
    top_key_re = re.compile(rf"{list_key}\s*:(?P<rest>.*)")
    keys = []
    for index, line in enumerate(lines):
        bare = line.rstrip("\r\n")
        if list_key in bare and not bare.lstrip().startswith("#"):
            if not top_key_re.fullmatch(bare):
                raise DeclareError(f"line {index + 1} names {list_key} in a form ccy-teams does not edit")
            keys.append(index)
    if len(keys) > 1:
        raise DeclareError(f"{list_key} is set more than once (lines {', '.join(str(k + 1) for k in keys)})")
    address = _address(lines)
    if not keys:
        return HostVars(lines, (), address, None, len(lines), DEFAULT_ITEM_INDENT, DEFAULT_ITEM_INDENT + 2)
    key = keys[0]
    rest = _scalar(top_key_re.fullmatch(lines[key].rstrip("\r\n"))["rest"], list_key)
    if rest:
        raise DeclareError(f"{list_key} on line {key + 1} is written inline; ccy-teams adds only to a "
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
    teams, item_indent, key_indent = _items(lines, content, list_key)
    return HostVars(lines, teams, address, key, content[-1] + 1, item_indent, key_indent)


def _items(lines: Sequence[str], content: Sequence[int],
           list_key: str) -> tuple[tuple[Declared, ...], int, int]:
    first = lines[content[0]].rstrip("\r\n")
    item_indent = len(first) - len(first.lstrip(" "))
    key_indent = -1
    items: list[dict[str, tuple[str, int]]] = []
    nested: list[dict[str, list[str]]] = []
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
                raise DeclareError(f"{where}: the {list_key} items are not indented alike")
            key_indent = this_key_indent
            items.append({})
            nested.append({})
            body = after.lstrip(" ")
        elif indent == key_indent and items:
            pass
        elif indent > key_indent >= 0 and items:
            if not items[-1]:
                raise DeclareError(f"{where}: an indented line before any key of the item")
            nested[-1][list(items[-1])[-1]].append(bare)
            continue
        else:
            raise DeclareError(f"{where}: not the {list_key} shape ccy-teams edits (a block list of mappings)")
        m = _KEY_LINE_RE.fullmatch(body)
        if not m:
            raise DeclareError(f"{where}: each {list_key} item must be a block mapping of key: value lines")
        if m["key"] in items[-1]:
            raise DeclareError(f"{where}: {m['key']} is set twice in one {list_key} item")
        items[-1][m["key"]] = (m["rest"], index)
        nested[-1][m["key"]] = []
    return (tuple(_declared(item, more) for item, more in zip(_unique(items, list_key), nested, strict=True)),
            item_indent, key_indent)


def _unique(items: list[dict[str, tuple[str, int]]], list_key: str) -> list[dict[str, tuple[str, int]]]:
    seen: set[str] = set()
    for item in items:
        if "team" not in item:
            line = min(index for _, index in item.values()) + 1
            raise DeclareError(f"the {list_key} item at line {line} has no team")
        name = _scalar(item["team"][0], f"the team on line {item['team'][1] + 1}")
        if name in seen:
            raise DeclareError(f"team {name} is declared twice in {list_key}")
        seen.add(name)
    return items


def _declared(item: dict[str, tuple[str, int]], nested: dict[str, list[str]]) -> Declared:
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
    server_name = team + teamfile.SERVER_NAME_SUFFIX
    if "server_name" in item:
        rest, index = item["server_name"]
        server_name = _scalar(rest, f"the server_name on line {index + 1}")
    raw = tuple((key, rest, index, tuple(nested[key])) for key, (rest, index) in item.items())
    return Declared(team, port, state, server_name, _first_listen(item), raw)


def _first_listen(item: dict[str, tuple[str, int]]) -> str | None:
    """The first listen address when the list is written in flow style, as create writes it."""
    if "listen" not in item:
        return None
    try:
        value = json.loads(_scalar(item["listen"][0], "listen"))
    except (DeclareError, json.JSONDecodeError):
        return None
    if isinstance(value, list) and value and isinstance(value[0], str):
        return value[0]
    return None


# ---------------------------------------------------------------- an entry's values

_INT_RE = re.compile(r"-?[0-9]+")


def _value(rest: str, nested: Sequence[str], where: str) -> object:
    """One value as the play reads it, in the subset ccy-teams writes and the example shows:
    a plain or quoted scalar (`true`/`false` and unquoted integers typed), a flow list or
    mapping written as JSON, or a block list of scalars or of one-line mappings."""
    plain = re.sub(r"(^|\s)#.*$", "", rest).strip()
    text = _scalar(rest, where)
    if plain == "":
        if not nested:
            raise DeclareError(f"{where} has no value")
        return _block_list(nested, where)
    if nested:
        raise DeclareError(f"{where} has a value and lines nested under it")
    if plain[0] in "[{":
        try:
            return json.loads(plain)
        except json.JSONDecodeError:
            raise DeclareError(f"{where} is a flow value ccy-teams cannot read (write it as JSON)") from None
    if plain[0] in "\"'":
        return text
    if text in ("true", "false"):
        return text == "true"
    if _INT_RE.fullmatch(text):
        return int(text)
    return text


def _block_list(lines: Sequence[str], where: str) -> list:
    first = lines[0]
    indent = len(first) - len(first.lstrip(" "))
    out: list = []
    pending: dict[str, tuple[str, list[str]]] | None = None
    for line in lines:
        body = line.strip()
        if not body or body.startswith("#"):
            continue
        this = len(line) - len(line.lstrip(" "))
        if this == indent and body.startswith("- "):
            item = body[2:].strip()
            km = _KEY_LINE_RE.fullmatch(item)
            if km is None:
                out.append(_value(item, (), where))
                pending = None
                continue
            pending = {km["key"]: (km["rest"], [])}
            out.append(pending)
        elif this > indent and pending is not None:
            km = _KEY_LINE_RE.fullmatch(body)
            if km is None or km["key"] in pending:
                raise DeclareError(f"{where} holds a list item ccy-teams cannot read")
            pending[km["key"]] = (km["rest"], [])
        else:
            raise DeclareError(f"{where} is not a block list ccy-teams can read")
    return [{key: _value(rest, more, f"{where}: {key}") for key, (rest, more) in item.items()}
            if isinstance(item, dict) else item for item in out]


def entry_of(team: Declared) -> dict:
    """The entry as the play hands it to the installer; DeclareError when it is written in
    a form ccy-teams does not read (a vault-tagged value, YAML beyond the subset above)."""
    return {key: _value(rest, more, f"{team.team}'s {key} on line {index + 1}")
            for key, rest, index, more in team.raw}


# ---------------------------------------------------------------- applied or not (D65)

PIN_FILE = "files/usr/local/share/agent-bus/tuwunel.pin"
CURRENT, OUTDATED, UNBUILT, REMOVED, DECIDE, UNREADABLE = (
    "current", "outdated", "unbuilt", "removed", "decide", "unreadable")


def declared_software(source: pathlib.Path) -> str:
    """The software digest the play would stamp now: the admin zipapp built from this
    checkout (the same reproducible build the installer runs), the pinned Tuwunel and the
    digest of this checkout's installer source (helpers/agent_bus/installer_digest.py), which
    the play bakes into the installer binary it builds."""
    versions = [line.partition(":")[2].strip() for line in (source / PIN_FILE).read_text().splitlines()
                if line.startswith("tuwunel_version:")]
    if len(versions) != 1:
        raise DeclareError(f"{PIN_FILE} names no single tuwunel_version")
    try:
        installer = installer_digest.source_digest(source)
    except installer_digest.DigestError as exc:
        raise DeclareError(str(exc)) from None
    return teamfile.software_digest(bundle.build("agent-bus", source), versions[0], installer)


def declared_config(team: Declared) -> str:
    """The config digest of the team file the play would render from this entry."""
    entry = {k: v for k, v in entry_of(team).items() if k != "purge"}
    try:
        return teamfile.config_digest(teamfile.parse_team_file(dict(entry, state="present")))
    except teamfile.TeamFileError as exc:
        raise DeclareError(str(exc)) from None


def read_stamps(applied: pathlib.Path) -> dict[str, dict]:
    """Every `<team>.json` stamp the installer left; a malformed one is refused, naming it."""
    stamps: dict[str, dict] = {}
    if not applied.is_dir():
        return stamps
    for path in sorted(applied.glob("*.json")):
        try:
            found = teamfile.parse_stamp(path.read_text(encoding="utf-8"))
        except (ValueError, UnicodeDecodeError):
            raise DeclareError(f"{path} is not an applied-team stamp") from None
        if f"{found['team']}.json" != path.name:
            raise DeclareError(f"{path} names team {found['team']}")
        stamps[found["team"]] = found
    return stamps


def team_states(hv: HostVars, stamps: dict[str, dict], software: str,
                installed: Sequence[str]) -> list[tuple[str, str, str]]:
    """(team, state, detail) per declared team, then per applied team no longer declared."""
    rows = []
    for team in hv.teams:
        stamp = stamps.get(team.team)
        if team.state == "absent":
            rows.append((team.team, DECIDE, "absent") if stamp else (team.team, REMOVED, "-"))
            continue
        try:
            config = declared_config(team)
        except DeclareError as exc:
            rows.append((team.team, UNREADABLE, str(exc).replace("\t", " ")))
            continue
        if stamp is None:
            rows.append((team.team, OUTDATED, "unstamped") if team.team in installed else (team.team, UNBUILT, "-"))
            continue
        differ = [what for what, now in (("config", config), ("software", software)) if stamp[what] != now]
        rows.append((team.team, OUTDATED, ",".join(differ)) if differ else (team.team, CURRENT, "-"))
    declared = {t.team for t in hv.teams}
    rows += [(name, DECIDE, "undeclared") for name in sorted(stamps) if name not in declared]
    return rows


def status(args: argparse.Namespace, out: TextIO) -> int:
    hv = parse(_read(pathlib.Path(args.file)))
    rows = team_states(hv, read_stamps(pathlib.Path(args.applied)), declared_software(pathlib.Path(args.source)),
                       args.installed or [])
    out.writelines(f"STATUS\t{team}\t{state}\t{detail}\n" for team, state, detail in rows)
    return EXIT_OK


def overview(args: argparse.Namespace, out: TextIO) -> int:
    declared(args, out)
    return status(args, out)


def mark_absent(hv: HostVars, name: str, purge: bool) -> str:
    """The file's text with the team's `state: absent` and `purge:` set, every other line as
    it was; the play then takes the team down, keeping its data unless `purge`."""
    team = next((t for t in hv.teams if t.team == name), None)
    if team is None:
        raise UsageError(f"--team: team {name} is not declared in {TEAMS_KEY}")
    lines = list(hv.lines)
    keys = {key: index for key, _, index, _ in team.raw}
    pad = " " * hv.key_indent
    for key, value in (("state", "absent"), ("purge", "true" if purge else "false")):
        line = f"{pad}{key}: {value}\n"
        if key in keys:
            if any(k == key and more for k, _, _, more in team.raw):
                raise DeclareError(f"team {name}'s {key} has nested lines; set it by hand")
            lines[keys[key]] = line
        else:
            at = keys["team"] + 1
            lines.insert(at, line)
            keys = {k: (i + 1 if i >= at else i) for k, i in keys.items()}
            keys[key] = at
    new = "".join(lines)
    after = next(t for t in parse(new).teams if t.team == name)
    if after.state != "absent" or entry_of(after).get("purge") is not purge:
        raise DeclareError("the edited file does not read back with the team absent; nothing was written")
    return new


def remove(args: argparse.Namespace, out: TextIO, err: TextIO) -> int:
    path = pathlib.Path(args.file)
    new = mark_absent(parse(_read(path)), args.team, args.purge)
    if args.dry_run:
        out.write(new)
        return EXIT_OK
    backup = write(path, new)
    what = "absent, its data deleted (purge: true)" if args.purge else "absent, its data kept (purge: false)"
    out.write(f"CHANGED\tteam {args.team} {what}, in {path}\n")
    err.write(f"The file as it was is kept at {backup}.\n")
    return EXIT_OK


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


def _repos_value(repos: Sequence[str]) -> list[dict]:
    """Each repository on any of its branches: no `branches` (spec §11, Plan 00177 Task 4.9)."""
    return [{"repo": repo} for repo in repos]


def build_entry(*, team: str, port: int, listen: Sequence[str], allow_from: Sequence[str],
                humans: Sequence[str], org: str | None = None, repos: Sequence[str] = ()) -> dict:
    """The new agent_bus_teams entry, checked by the team file's validator: it trusts the
    whole GitHub owner `org`, or the repositories `repos` (any owners), or both."""
    entry = {
        "team": team,
        "port": port,
        "listen": list(listen),
        "allow_from": list(allow_from),
        "humans": list(humans),
        **({"org": org} if org else {}),
        **({"repos": _repos_value(repos)} if repos else {}),
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
    for key in ("listen", "allow_from", "humans", "org", "repos", "forge_api"):
        if key in entry:
            lines.append(f"{key_pad}{key}: {json.dumps(entry[key])}\n")
    return lines


def _append(hv: HostVars, list_key: str, added: list[str]) -> str:
    """The file's text with an item's lines added at the end of `list_key` (the key made at
    the end of the file when there is none); the rest of it byte for byte as it was."""
    lines = list(hv.lines)
    if lines and not lines[-1].endswith("\n"):
        lines[-1] += "\n"
    if hv.key_line is None:
        added = ([] if not lines else ["\n"]) + [f"{list_key}:\n"] + added
        at = len(lines)
    else:
        at = hv.insert_at
    return "".join(lines[:at] + added + lines[at:])


def insert(hv: HostVars, entry: dict) -> str:
    """The file's text with the entry added; the rest of it byte for byte as it was."""
    if any(t.team == entry["team"] for t in hv.teams):
        raise DeclareError(f"team {entry['team']} is already declared in {TEAMS_KEY}")
    if any(t.port == entry["port"] for t in hv.teams):
        raise DeclareError(f"port {entry['port']} is already declared in {TEAMS_KEY}")
    new = _append(hv, TEAMS_KEY, render_entry(entry, hv.item_indent, hv.key_indent))
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
    "this machine has no bus address yet. It is the address that every team's homeserver "
    "listens on, and it is used for nothing else: an IP of this machine that the team's agents "
    "and your Element can reach, such as its WireGuard address, or one added for the bus. "
    "ccy-teams create asks for it on a terminal and keeps it; run it there"
)


def bus_address_error(text: str) -> str | None:
    """Why `text` cannot be the bus address, or None when it can."""
    try:
        address = ipaddress.ip_address(text.strip())
    except ValueError:
        return f"'{text}' is not an IP address (for example 192.0.2.10)"
    if address.is_unspecified or address.is_loopback or address.is_multicast:
        return f"{address} cannot be reached from the team's agents; give an address of this machine on a network"
    return None


def set_address(text: str, address: str) -> str:
    """The file's text with the bus address added at its end. A file that already has the key
    (even one declared in a form this tool cannot read) is never changed: DeclareError."""
    problem = bus_address_error(address)
    if problem:
        raise UsageError(f"--address: {problem}")
    if any(_ADDRESS_RE.fullmatch(line.rstrip("\r\n")) for line in text.splitlines(keepends=True)):
        raise DeclareError(f"{ADDRESS_KEY} is already set; ccy-teams does not replace it")
    lead = "" if not text or text.endswith("\n") else "\n"
    return f"{text}{lead}{ADDRESS_KEY}: {ipaddress.ip_address(address.strip())}\n"


def _split(text: str) -> list[str]:
    return [part.strip() for part in text.split(",") if part.strip()]


def _check_flags(hv: HostVars, args: argparse.Namespace) -> None:
    """Every value the flags give, by the team file's rules; UsageError names the first bad one."""
    if args.team is not None:
        if any(t.team == args.team for t in hv.teams):
            raise UsageError(f"--team: team {args.team} is already declared in {TEAMS_KEY}")
        problem = field_error("team", args.team)
        if problem:
            raise UsageError(f"--team: {problem}")
    for flag, key, value in (("--human", "humans", args.human), ("--allow-from", "allow_from", args.allow_from),
                             ("--org", "org", args.org),
                             ("--repo", "repos", _repos_value(args.repo) if args.repo else None)):
        if value:
            problem = field_error(key, value)
            if problem:
                raise UsageError(f"{flag}: {problem}")


def check(args: argparse.Namespace) -> int:
    _check_flags(parse(_read(pathlib.Path(args.file))), args)
    return EXIT_OK


def create(args: argparse.Namespace, ss_output: Callable[[], str], out: TextIO, err: TextIO) -> int:
    path = pathlib.Path(args.file)
    text = _read(path)
    hv = parse(text)
    if hv.address is None:
        if args.address is None:
            raise DeclareError(NO_ADDRESS)
        # The machine's one-off bus address is declared with the first team, in one write.
        hv = parse(set_address(text, args.address))
    elif args.address is not None:
        raise UsageError(f"--address: {ADDRESS_KEY} is already set; ccy-teams does not replace it")
    missing = [flag for flag, value in (("--team", args.team), ("--human", args.human),
                                        ("--org or --repo", args.org or args.repo)) if not value]
    if missing:
        raise UsageError(f"create needs {', '.join(missing)} (ccy-teams create asks for them on a terminal)")
    _check_flags(hv, args)
    port = choose_port({t.port for t in hv.teams if t.port is not None} | listening_ports(ss_output()))
    entry = build_entry(team=args.team, port=port, listen=[hv.address], allow_from=args.allow_from or [],
                        humans=args.human, org=args.org, repos=args.repo or [])
    new = insert(hv, entry)
    if args.dry_run:
        out.writelines(render_entry(entry, hv.item_indent, hv.key_indent))
        return EXIT_OK
    backup = write(path, new)
    out.write(f"CHANGED\tteam {args.team}, port {port}, in {path}\n")
    err.write(f"Team {args.team} is declared in {path}, on port {port}; the file as it was is kept at {backup}.\n")
    return EXIT_OK


def _present(hv: HostVars, name: str) -> Declared:
    team = next((t for t in hv.teams if t.team == name and t.state == "present"), None)
    if team is None or team.port is None:
        raise UsageError(f"--team: team {name} is not declared present, with a port, in {TEAMS_KEY}")
    return team


def _raw_value(team: Declared, key: str) -> object:
    """One key's value as the play reads it, or None when the entry does not set it; the
    rest of the entry is not read (a vaulted value elsewhere in it does not matter)."""
    for raw_key, rest, index, more in team.raw:
        if raw_key == key:
            return _value(rest, more, f"{team.team}'s {key} on line {index + 1}")
    return None


def scope_words(team: Declared) -> str:
    """What the team trusts, as a person would say it."""
    org = _raw_value(team, "org")
    repos = [r["repo"] for r in _raw_value(team, "repos") or [] if isinstance(r, dict) and "repo" in r]
    parts = ([f"any repository of {org}"] if org else []) + repos
    return " or ".join(parts) if parts else "no repository"


def next_steps(hv: HostVars, name: str) -> str:
    """What is left to do once the team is installed, all of it in ccy-teams or ccy."""
    team = _present(hv, name)
    return (
        f"Team {name} is up. Later, in ccy-teams:\n"
        "  Sign in      Enter on your name under the team sets a new Element password and shows it.\n"
        "  Element      Ctrl-E on the team opens it in Element.\n"
        f"  Agents       in a checkout of {scope_words(team)}, run ccy --teams {name}\n"
        "               (ccy-teams run there offers it; Ctrl-N on the team does it too).\n"
    )


def base_url(hv: HostVars, team: Declared) -> str:
    """How this desktop reaches the team's homeserver: its first listen address, else the
    bus address, at its port."""
    host = team.listen or hv.address
    if host is None:
        raise DeclareError(f"team {team.team} has no listen address and {ADDRESS_KEY} is not set")
    return f"http://{_url_host(host)}:{team.port}"


def _as_written(hv: HostVars) -> list[tuple[str, list[tuple[str, str, tuple[str, ...]]]]]:
    """Each entry's keys and values as written, whatever line they are on."""
    return [(t.team, [(key, rest, more) for key, rest, _, more in t.raw]) for t in hv.teams]


def set_element(text: str, name: str) -> str | None:
    """The file's text with the team's agent_bus_element_teams entry added, or its base_url
    brought up to date; None when it is already right."""
    team = _present(parse(text), name)
    url = base_url(parse(text), team)
    profiles = parse(text, ELEMENT_KEY)
    profile = next((p for p in profiles.teams if p.team == name), None)
    line = f'{" " * profiles.key_indent}base_url: {json.dumps(url)}\n'
    if profile is None:
        new = _append(profiles, ELEMENT_KEY, [f"{' ' * profiles.item_indent}- team: {name}\n", line])
    else:
        if _raw_value(profile, "base_url") == url:
            return None
        found = [(index, more) for key, _, index, more in profile.raw if key == "base_url"]
        if not found or found[0][1]:
            raise DeclareError(f"team {name}'s Element entry has no one-line base_url to update")
        lines = list(profiles.lines)
        lines[found[0][0]] = line
        new = "".join(lines)
    after = next((p for p in parse(new, ELEMENT_KEY).teams if p.team == name), None)
    if after is None or _raw_value(after, "base_url") != url or _as_written(parse(new)) != _as_written(parse(text)):
        raise DeclareError("the edited file does not read back with the Element entry; nothing was written")
    return new


def teams_for(hv: HostVars, repo: str) -> list[str]:
    """The teams declared present whose trust covers the repository: its owner is their
    org, or they list it."""
    owner = repo.partition("/")[0]
    out = []
    for team in hv.teams:
        if team.state != "present":
            continue
        listed = [r.get("repo") for r in _raw_value(team, "repos") or [] if isinstance(r, dict)]
        if _raw_value(team, "org") == owner or repo in listed:
            out.append(team.team)
    return out


def drop_limit(text: str, name: str) -> str:
    """The file's text with the team's path_prefixes line (and any lines nested under it)
    removed, every other line as it was; Plan 00177 Task 4.7's follow-up."""
    team = _present(parse(text), name)
    found = [(index, more) for key, _, index, more in team.raw if key == "path_prefixes"]
    if not found:
        raise UsageError(f"--team: team {name} sets no path_prefixes")
    index, more = found[0]
    lines = list(parse(text).lines)
    end, left = index + 1, len(more)
    while left:
        if lines[end].strip() and not lines[end].lstrip().startswith("#"):
            left -= 1
        end += 1
    new = "".join(lines[:index] + lines[end:])
    after = _present(parse(new), name)
    if any(key == "path_prefixes" for key, *_ in after.raw) or (
            {k: v for k, v in entry_of(team).items() if k != "path_prefixes"} != entry_of(after)):
        raise DeclareError("the edited file does not read back with only path_prefixes gone; nothing was written")
    return new


def suggest_team(hv: HostVars, source: str) -> str | None:
    """A GitHub owner's or repository's name in the team-name grammar, or None when nothing
    valid is left of it or a team has the name already."""
    name = re.sub(r"[^a-z0-9-]+", "-", source.lower())
    name = re.sub(r"-+", "-", name).lstrip("-0123456789")[:24].rstrip("-")
    if not name or field_error("team", name) or any(t.team == name for t in hv.teams):
        return None
    return name


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


def _url_host(address: str) -> str:
    return f"[{address}]" if ":" in address else address


def _read(path: pathlib.Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except FileNotFoundError:
        raise DeclareError(f"{path} does not exist (copy localhost.yml.dist to make it)") from None


def declared(args: argparse.Namespace, out: TextIO) -> int:
    hv = parse(_read(pathlib.Path(args.file)))
    for team in hv.teams:
        host = team.listen or hv.address
        url = f"http://{_url_host(host)}:{team.port}" if host and team.port is not None else "-"
        out.write(f"TEAM\t{team.team}\t{team.port if team.port is not None else '-'}\t{team.state}"
                  f"\t{team.server_name}\t{url}\n")
    return EXIT_OK


def _listing_or_edit(args: argparse.Namespace, out: TextIO, err: TextIO) -> int:
    """element, drop-limit (edits, each backed up), element-teams, limited, teams-for."""
    path = pathlib.Path(args.file)
    text = _read(path)
    if args.command == "element-teams":
        for profile in parse(text, ELEMENT_KEY).teams:
            out.write(f"ELEMENT\t{profile.team}\t{_raw_value(profile, 'base_url')}\n")
        return EXIT_OK
    if args.command == "limited":
        for team in parse(text).teams:
            prefixes = _raw_value(team, "path_prefixes") if team.state == "present" else None
            if prefixes:
                out.write(f"LIMITED\t{team.team}\t{', '.join(str(p) for p in prefixes)}"
                          f"\t{scope_words(team)}\n")
        return EXIT_OK
    if args.command == "teams-for":
        out.writelines(f"TEAM\t{name}\n" for name in teams_for(parse(text), args.repo))
        return EXIT_OK
    if args.command == "element":
        new = set_element(text, args.team)
        if new is None:
            out.write(f"UNCHANGED\t{args.team}\n")
            return EXIT_OK
        said = f"Element profile for {args.team}"
    else:
        new = drop_limit(text, args.team)
        said = f"team {args.team} may name any path"
    backup = write(path, new)
    out.write(f"CHANGED\t{said}, in {path}\n")
    err.write(f"The file as it was is kept at {backup}.\n")
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
        sub.add_argument("--org", help="the GitHub owner (an org or a user) whose repositories the team trusts")
        sub.add_argument("--repo", action="append", metavar="OWNER/REPO",
                         help="a repository the team trusts, on any branch (instead of, or as well as, --org)")
        sub.add_argument("--allow-from", action="append", dest="allow_from", metavar="CIDR")
        sub.add_argument("--address", help="this machine's bus address, declared with the first team")
        if name == "create":
            sub.add_argument("--dry-run", action="store_true", help="print the entry; write nothing")
    suggest = commands.add_parser("suggest-team", help="a free team name made from a GitHub owner's or repo's")
    suggest.add_argument("--file", required=True)
    suggest.add_argument("--name", required=True)
    for name, help_text in (("element", "add or update the team's agent_bus_element_teams entry"),
                            ("drop-limit", "remove the team's path_prefixes")):
        sub = commands.add_parser(name, help=help_text)
        sub.add_argument("--file", required=True)
        sub.add_argument("--team", required=True)
    for name, help_text in (("element-teams", "one ELEMENT line per agent_bus_element_teams entry"),
                            ("limited", "one LIMITED line per present team that sets path_prefixes")):
        commands.add_parser(name, help=help_text).add_argument("--file", required=True)
    covering = commands.add_parser("teams-for", help="the present teams a repository belongs to")
    covering.add_argument("--file", required=True)
    covering.add_argument("--repo", required=True)
    remote = commands.add_parser("github-remote", help="OWNER/REPO of a GitHub remote URL, or nothing")
    remote.add_argument("--url", required=True)
    for name, help_text in (("status", "each team: applied as declared or not (D65)"),
                            ("overview", "the declared teams, then each team's applied state")):
        states = commands.add_parser(name, help=help_text)
        states.add_argument("--file", required=True)
        states.add_argument("--source", required=True, help="this checkout, whose software the play installs")
        states.add_argument("--applied", required=True, help="the installer's applied-stamp directory")
        states.add_argument("--installed", action="append", metavar="TEAM", help="a team whose homeserver exists")
    commands.add_parser("address", help="the bus address, or nothing").add_argument("--file", required=True)
    check_address = commands.add_parser("check-address", help="validate a bus address")
    check_address.add_argument("--address", required=True)
    steps = commands.add_parser("next-steps", help="what to do once a team is installed")
    steps.add_argument("--file", required=True)
    steps.add_argument("--team", required=True)
    down = commands.add_parser("remove", help="declare a team absent, its data kept unless --purge")
    down.add_argument("--file", required=True)
    down.add_argument("--team", required=True)
    down.add_argument("--purge", action="store_true")
    down.add_argument("--dry-run", action="store_true", help="print the edited file; write nothing")
    return parser


def main(argv: Sequence[str] | None = None, *, ss_output: Callable[[], str] = _ss,
         stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    try:
        args = build_parser().parse_args(argv)
        if args.command == "declared":
            return declared(args, out)
        if args.command == "check":
            return check(args)
        if args.command == "suggest-team":
            name = suggest_team(parse(_read(pathlib.Path(args.file))), args.name)
            if name:
                out.write(f"{name}\n")
            return EXIT_OK
        if args.command == "check-address":
            problem = bus_address_error(args.address)
            if problem:
                raise UsageError(f"--address: {problem}")
            return EXIT_OK
        if args.command == "address":
            found = parse(_read(pathlib.Path(args.file))).address
            if found:
                out.write(f"{found}\n")
            return EXIT_OK
        if args.command == "next-steps":
            out.write(next_steps(parse(_read(pathlib.Path(args.file))), args.team))
            return EXIT_OK
        if args.command in ("element", "drop-limit", "element-teams", "limited", "teams-for"):
            return _listing_or_edit(args, out, err)
        if args.command == "status":
            return status(args, out)
        if args.command == "overview":
            return overview(args, out)
        if args.command == "remove":
            return remove(args, out, err)
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
