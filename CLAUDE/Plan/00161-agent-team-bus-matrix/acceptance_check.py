#!/usr/bin/env python3
"""Plan 00161 unit U17: the pure logic behind acceptance.bash's M1 slice.

acceptance.bash drives the host (the installer, `agent-bus`, two transient members,
the human's `curl`); this module builds what it sends and judges what comes back, so
every judgement is a tested function rather than a grep. Commands (stdout is the
payload; every reason goes to stderr):

  github-repo URL                    owner/repo of a GitHub remote, lowercased
  team-file TEAM PORT BUS_IP HUMAN REPO BRANCH PREFIX FORGE_API
                                     a test team's team file (JSON): the acceptance team's,
                                     and deploy.bash's throwaway team's
  set-limit MEMBER_JSON KEY VALUE    add one §10 limit to a bundle's member.json, in place
  member-field MEMBER_JSON KEY       one string field of member.json
  handle MEMBER_JSON                 the member's handle (its user ID's localpart)
  login-body USER                    a password login body; the password is read on stdin
  access-token                       the token from a login response on stdin
  human-message BODY USER_ID...      an m.text addressed (m.mentions) to USER_IDs only
  event-id                           the event ID from a send response on stdin
  room-path ROOM_ID                  the room ID quoted for a URL path
  sent-event OUT TEAM                the event ID of the one SENT line in OUT
  expect-ping OUT TEAM EVENT SENDER VERB REF RE     OUT is exactly that PING line
  expect-human OUT TEAM EVENT SENDER TEXT           OUT is exactly that HUMAN line
  expect-timeout OUT TEAM EVENT TARGET VERB REF     OUT is exactly that TIMEOUT line
  expect-absent OUT EVENT            no line in OUT names EVENT, and none is HUMAN
  send-outcome STATUS ERR            a `pingbus send`'s verdict from its status and stderr
  host-subnet SOURCE                 (U23) "<ifname>\\t<cidr>": the host network holding a
                                     member's SOURCE address; `ip -j -4 addr show` on stdin
  allow-from TEAM_FILE CIDR...       (U23) add each CIDR to the team file's allow_from, in place
  suggested-args OUT TYPE            (U23) `pingbus suggest-handle`'s line in OUT, one argument
                                     per line, when it names TYPE

REF and RE are "-" when absent, as on the wire (§15); FORGE_API "-" is pingbus's GitHub API. Exit codes: 0 ok; 1 an expectation
failed or input was malformed; 2 (send-outcome only) could not be established; 64 usage.

The line grammar, the limits, the handle grammar, and the forge refusal codes and message
prefix are imported from helpers/pingbus, never restated: the repo root is found by walking up to ansible.cfg, as the plan scripts do.
"""

from __future__ import annotations

import importlib
import ipaddress
import json
import pathlib
import re
import sys
import urllib.parse
from collections.abc import Sequence


def _repo_root() -> pathlib.Path:
    here = pathlib.Path(__file__).resolve().parent
    for candidate in (here, *here.parents):
        if (candidate / "ansible.cfg").exists():
            return candidate
        if (candidate / ".git").exists():
            break
    raise ImportError(f"no ansible.cfg above {here}: not inside a fedora-desktop checkout")


if str(_repo_root()) not in sys.path:
    sys.path.insert(0, str(_repo_root()))
cli = importlib.import_module("helpers.pingbus.cli")
forge = importlib.import_module("helpers.pingbus.forge")
limits = importlib.import_module("helpers.pingbus.limits")
protocol = importlib.import_module("helpers.pingbus.protocol")

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_UNKNOWN = 2
EXIT_USAGE = 64

#: TEST-NET-1 (CLAUDE/ExampleValues.md): no member connects from it; the team file needs
#: a well-formed allow_from, and every member here reaches the bus address from the host.
ALLOW_FROM_EXAMPLE = "192.0.2.0/24"
DEVICE_NAME = "agent-bus acceptance"
#: §6 send-side refusals that say the forge could not answer, not that the reference is bad.
FORGE_UNANSWERED = (forge.UNREACHABLE_REFUSAL, forge.RATE_REFUSAL)

_GITHUB_REMOTE_RE = re.compile(
    r"(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)"
    r"(?P<owner>[A-Za-z0-9-]+)/(?P<repo>[A-Za-z0-9._-]+?)(?:\.git)?/?"
)
_TOKEN_RE = re.compile(r"[\x21-\x7e]+")


def github_repo(url: str) -> str:
    """`owner/repo` of a GitHub remote URL (https, scp-like or ssh), lowercased."""
    match = _GITHUB_REMOTE_RE.fullmatch(url)
    if match is None:
        raise ValueError("the remote is not a github.com repository URL")
    return f"{match['owner']}/{match['repo']}".lower()


def team_file(team: str, port: int, bus_address: str, human: str, repo: str, branch: str,
              prefix: str, forge_api: str) -> dict:
    """DESIGN.md section 3.4's team file for a test team: listening on the bus address, one
    human, one trusted repository and branch, one forge. The only place its shape is
    written: deploy.bash's throwaway team and the acceptance team both come from here."""
    return {
        "team": team, "port": port, "listen": [bus_address], "allow_from": [ALLOW_FROM_EXAMPLE],
        "humans": [human], "repos": [{"repo": repo, "branches": [branch]}],
        "path_prefixes": [prefix], "forge_api": forge_api,
    }


def with_limit(member: dict, key: str, value: int) -> dict:
    """member.json with one more `limits` entry, refused unless pingbus accepts it (§10)."""
    wanted = {**member.get("limits", {}), key: value}
    limits.parse_limits(wanted)
    return {**member, "limits": wanted}


def handle_of(user_id: str) -> str:
    """The handle an agent's user ID carries as its localpart."""
    local, colon, server = user_id.removeprefix("@").partition(":")
    if not user_id.startswith("@") or not colon or not server or protocol.parse_handle(local) is None:
        raise ValueError("not an agent user ID")
    return local


def login_body(user: str, password: str) -> dict:
    return {"type": "m.login.password", "identifier": {"type": "m.id.user", "user": user},
            "password": password, "initial_device_display_name": DEVICE_NAME}


def access_token(response: object) -> str:
    token = response.get("access_token") if isinstance(response, dict) else None
    if not isinstance(token, str) or _TOKEN_RE.fullmatch(token) is None:
        raise ValueError("the login response holds no access token")
    return token


def human_message(body: str, user_ids: Sequence[str]) -> dict:
    """What Element sends for a message whose mention pills name `user_ids` (§7)."""
    return {"msgtype": "m.text", "body": body, "m.mentions": {"user_ids": list(user_ids)}}


def event_id_of(response: object) -> str:
    event_id = response.get("event_id") if isinstance(response, dict) else None
    if not protocol.is_event_id(event_id):
        raise ValueError("the send response holds no event ID")
    return event_id


def room_path(room_id: str) -> str:
    if not protocol.is_room_id(room_id):
        raise ValueError("not a room ID")
    return urllib.parse.quote(room_id, safe="")


# ---------------------------------------------------------------- §15 lines


def parse_lines(text: str) -> list[tuple[str, tuple[str, ...]]]:
    """Every stdout line as (kind, fields after the version); malformed: ValueError."""
    parsed = []
    for raw in text.splitlines():
        kind, *rest = raw.split("\t")
        if kind not in cli.LINES or cli.LINES[kind][0] != cli.STDOUT:
            raise ValueError(f"not a stdout line kind: {kind!r}")
        if len(rest) != 1 + len(cli.LINES[kind][1]) or rest[0] != str(protocol.PROTOCOL_VERSION):
            raise ValueError(f"a {kind} line has the wrong version or number of fields")
        parsed.append((kind, tuple(rest[1:])))
    return parsed


def sent_event(text: str, team: str) -> str:
    lines = parse_lines(text)
    if len(lines) != 1 or lines[0][0] != "SENT" or lines[0][1][0] != team:
        raise ValueError(f"expected exactly one SENT line for team {team}")
    return lines[0][1][1]


def _only(text: str, kind: str) -> tuple[tuple[str, ...] | None, list[str]]:
    try:
        lines = parse_lines(text)
    except ValueError as exc:
        return None, [str(exc)]
    kinds = [k for k, _ in lines]
    if kinds != [kind]:
        return None, [f"expected exactly one {kind} line, got {kinds or 'none'}"]
    return lines[0][1], []


def _compare(kind: str, got: Sequence[str], want: Sequence[str | None]) -> list[str]:
    names = cli.LINES[kind][1]
    return [f"{kind} {name}: got {g!r}, want {w!r}"
            for name, g, w in zip(names, got, want, strict=True) if w is not None and g != w]


def expect_ping(text: str, team: str, event: str, sender: str, verb: str, ref: str | None,
                re_: str | None) -> list[str]:
    fields, problems = _only(text, "PING")
    if fields is None:
        return problems
    return _compare("PING", fields, (team, event, sender, verb, ref or cli.ABSENT, re_ or cli.ABSENT))


def expect_human(text: str, team: str, event: str, sender: str, body: str) -> list[str]:
    fields, problems = _only(text, "HUMAN")
    if fields is None:
        return problems
    problems = _compare("HUMAN", fields, (team, event, sender, None, None))
    if not fields[3].isdigit():
        problems.append(f"HUMAN origin_server_ts {fields[3]!r} is not a non-negative integer")
    try:
        delivered = json.loads(fields[4])
    except ValueError:
        delivered = None
    if not isinstance(delivered, str) or delivered != body:
        problems.append(f"HUMAN text {fields[4]!r} is not the message sent ({body!r})")
    return problems


def expect_timeout(text: str, team: str, event: str, target: str, verb: str, ref: str) -> list[str]:
    fields, problems = _only(text, "TIMEOUT")
    if fields is None:
        return problems
    return _compare("TIMEOUT", fields, (team, event, target, verb, ref))


def expect_absent(text: str, event: str) -> list[str]:
    try:
        lines = parse_lines(text)
    except ValueError as exc:
        return [str(exc)]
    return [f"a {kind} line was delivered here: {fields}" for kind, fields in lines
            if kind == "HUMAN" or event in fields]


def send_outcome(status: int, stderr: str) -> int:
    """0 sent; 2 when the forge did not answer (a fact about GitHub, not the bus); 1 else."""
    if status == 0:
        return EXIT_OK
    if any(f"{cli.FORGE_REFUSED}: {code}:" in stderr for code in FORGE_UNANSWERED):
        return EXIT_UNKNOWN
    return EXIT_FAIL


# ---------------------------------------------------------------- U23: other encapsulations


def host_subnet(addrs: object, source: str) -> tuple[str, str]:
    """The host interface whose IPv4 network holds `source` (an LXC, docker or VM member's
    own address towards the bus), and that network as a CIDR. `addrs` is `ip -j -4 addr
    show`. The caller requires the interface to be a bridge: that is the route the READMEs
    give, and that network is what the team's allow_from must list."""
    address = ipaddress.ip_address(source)
    if address.version != 4:
        raise ValueError(f"{source} is not an IPv4 address")
    if not isinstance(addrs, list):
        raise ValueError("the address listing is not a list of interfaces")
    found = []
    for link in addrs:
        if not isinstance(link, dict) or not isinstance(link.get("ifname"), str) \
                or not isinstance(link.get("addr_info"), list):
            raise ValueError("an interface in the address listing has no ifname or addr_info")
        for info in link["addr_info"]:
            if not isinstance(info, dict) or "local" not in info or "prefixlen" not in info:
                raise ValueError(f"an address of {link['ifname']} has no local or prefixlen")
            network = ipaddress.ip_interface(f"{info['local']}/{info['prefixlen']}").network
            if address in network:
                found.append((link["ifname"], str(network)))
    if len(found) != 1:
        raise ValueError(f"{source} is on {len(found)} host networks, not exactly one: {found}")
    return found[0]


def with_allow_from(team: dict, cidrs: Sequence[str]) -> dict:
    """The team file with each network in `cidrs` added to `allow_from` once, after the
    networks already there. A malformed network, or one with host bits set, is refused."""
    wanted = list(team["allow_from"])
    for cidr in cidrs:
        network = str(ipaddress.ip_network(cidr, strict=True))
        if "/" not in cidr:
            raise ValueError(f"{cidr!r} is an address, not a network")
        if network not in wanted:
            wanted.append(network)
    return {**team, "allow_from": wanted}


_SUGGESTED_RE = re.compile(r"--repo=(?P<repo>\S+) --host=(?P<host>\S+) --type=(?P<type>\S+)\n")


def suggested_args(text: str, member_type: str) -> list[str]:
    """`pingbus suggest-handle`'s one line, as add-member arguments, when it names the
    encapsulation the member really is in (its READMEs' step 3)."""
    match = _SUGGESTED_RE.fullmatch(text)
    if match is None:
        raise ValueError(f"suggest-handle printed {text!r}, not one --repo --host --type line")
    if match["type"] != member_type:
        raise ValueError(f"suggest-handle says type {match['type']!r}, the member is {member_type!r}")
    return [f"--repo={match['repo']}", f"--host={match['host']}", f"--type={member_type}"]


# ---------------------------------------------------------------- command line


def _read(path: str) -> str:
    return pathlib.Path(path).read_text(encoding="utf-8")


def _json_stdin() -> object:
    return json.loads(sys.stdin.read())


def _print_json(value: object) -> None:
    print(json.dumps(value))


def _verdict(problems: list[str]) -> int:
    for problem in problems:
        print(f"[FAIL] {problem}", file=sys.stderr)
    return EXIT_FAIL if problems else EXIT_OK


def _absent(value: str) -> str | None:
    return None if value == cli.ABSENT else value


def _set_limit(path: str, key: str, value: str) -> int:
    target = pathlib.Path(path)
    member = json.loads(target.read_text(encoding="utf-8"))
    target.write_text(json.dumps(with_limit(member, key, int(value)), indent=2) + "\n", encoding="utf-8")
    return EXIT_OK


def _field(path: str, key: str) -> str:
    value = json.loads(_read(path)).get(key)
    if not isinstance(value, str):
        raise ValueError(f"member.json has no string {key!r}")
    return value


COMMANDS = {
    "github-repo": (1, lambda a: print(github_repo(a[0]))),
    "team-file": (8, lambda a: _print_json(team_file(a[0], int(a[1]), *a[2:7],
                                                     _absent(a[7]) or forge.GITHUB_API))),
    "set-limit": (3, lambda a: _set_limit(*a)),
    "member-field": (2, lambda a: print(_field(a[0], a[1]))),
    "handle": (1, lambda a: print(handle_of(_field(a[0], "user_id")))),
    "login-body": (1, lambda a: _print_json(login_body(a[0], sys.stdin.read().removesuffix("\n")))),
    "access-token": (0, lambda a: print(access_token(_json_stdin()))),
    "event-id": (0, lambda a: print(event_id_of(_json_stdin()))),
    "room-path": (1, lambda a: print(room_path(a[0]))),
    "sent-event": (2, lambda a: print(sent_event(_read(a[0]), a[1]))),
    "expect-ping": (7, lambda a: _verdict(expect_ping(_read(a[0]), a[1], a[2], a[3], a[4],
                                                      _absent(a[5]), _absent(a[6])))),
    "expect-human": (5, lambda a: _verdict(expect_human(_read(a[0]), *a[1:]))),
    "expect-timeout": (6, lambda a: _verdict(expect_timeout(_read(a[0]), *a[1:]))),
    "expect-absent": (2, lambda a: _verdict(expect_absent(_read(a[0]), a[1]))),
    "send-outcome": (2, lambda a: send_outcome(int(a[0]), _read(a[1]))),
    "host-subnet": (1, lambda a: print("\t".join(host_subnet(_json_stdin(), a[0])))),
    "suggested-args": (2, lambda a: print("\n".join(suggested_args(_read(a[0]), a[1])))),
}


def _allow_from(path: str, cidrs: Sequence[str]) -> int:
    target = pathlib.Path(path)
    team = json.loads(target.read_text(encoding="utf-8"))
    target.write_text(json.dumps(with_allow_from(team, cidrs), indent=1) + "\n", encoding="utf-8")
    return EXIT_OK


def main(argv: Sequence[str]) -> int:
    if not argv or (argv[0] not in COMMANDS and argv[0] not in ("human-message", "allow-from")):
        print(__doc__, file=sys.stderr)
        return EXIT_USAGE
    command, args = argv[0], list(argv[1:])
    try:
        if command == "allow-from":
            if len(args) < 2:
                print(f"{command}: TEAM_FILE and at least one CIDR", file=sys.stderr)
                return EXIT_USAGE
            return _allow_from(args[0], args[1:])
        if command == "human-message":
            if len(args) < 2:
                print(f"{command}: BODY and at least one USER_ID", file=sys.stderr)
                return EXIT_USAGE
            _print_json(human_message(args[0], args[1:]))
            return EXIT_OK
        count, run = COMMANDS[command]
        if len(args) != count:
            print(f"{command}: takes {count} argument(s), got {len(args)}", file=sys.stderr)
            return EXIT_USAGE
        status = run(args)
    except (ValueError, OSError) as exc:
        print(f"acceptance_check {command}: {exc}", file=sys.stderr)
        return EXIT_FAIL
    return EXIT_OK if status is None else status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
