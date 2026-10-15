#!/usr/bin/env python3
"""Plan 00201 M7: the pure logic behind acceptance.bash (the two-team acceptance leg).

_acceptance-m7.inc.bash drives the host (agent-bus-install, agent-bus, ccy-teams, two headless
ccy sessions, podman exec into them, the human's curl); this module writes what it hands them
and judges what comes back. Every judgement rests on what a tool printed or recorded, never on
the model's prose. Standard library only, and only `bus-ports` imports from this checkout
(declare.py, lazily). Commands (stdout is the payload, reasons on stderr):

  team-file TEAM PORT ADDRESS HUMAN ORG DESCRIPTION   a throwaway team file (JSON)
  host-vars TEAMFILE...              a host_vars file declaring those teams (agent_bus_teams)
  bus-ports N OWNER_HOST_VARS        N ports by declare.py's finder, clear of the owner's and
                                     of every listening port, one per line
  launch-conf-clean CONF TEAM[,TEAM]   ccy's Quick Launch record names none of the teams
  expect-own-team TEAM_JSON TEAM PREFIX   an installed team is ours: its description starts PREFIX
  say-outcome STATUS STDERR          0 sent, 3 a team record not verified yet, 1 anything else
  github-owner URL                   the lowercased owner of a github.com remote URL
  member-field FILE KEY              one string field of a member.json
  room-path ROOM_ID                  the room ID quoted for a URL path
  login-body USER                    a Matrix password login body; the password on stdin
  access-token                       the access_token of a login response on stdin
  expect-topic STATE_JSON DESCRIPTION  the room's m.room.topic is DESCRIPTION
  user-line TEXT                     one stream-json user message line
  turns STREAM                       turns that ended (`result` lines) in a session's stdout
  session-id STREAM                  the session ID of its `init` line; exit 3 while none
  expect-held SEAT_LIST SEAT@TEAM[,...]   every one is held in `agent-bus seat list`; 3 if not
  seat-containers TEAM[,TEAM...]     stdin "ID<TAB>ccy-seats label": those holding a seat of one
  expect-list-row LIST TEAM DESCRIPTION   `ccy-teams --list` (piped) shows it whole on the team row
  expect-block STDERR (SEAT TEAM DESCRIPTION)...   the launch block names exactly those seats
  status-ready STATUS TEAM...        `pingbus status`: each team trusted with an ABOUT line; 3 if not
  expect-about STATUS TEAM DESCRIPTION   that team's ABOUT line carries DESCRIPTION
  expect-sent OUT TEAM               one SENT line, for TEAM; prints its event ID
  expect-choices STDERR (TEAM DESCRIPTION)...   the "several teams are active" refusal, listing each
  expect-kit-status-line SETTINGS    the kit's settings.json makes `pingbus status-line` the status line
  expect-segment OUT TEAMS           `pingbus status-line` printed the session's own line, then | bus: TEAMS
  expect-plain LABELS_JSON ENV_JSON  a ccy session container with no seat label or PINGBUS_ variable

Exit codes: 0 ok; 1 an expectation failed or the input was malformed; 3 not there yet;
64 usage.
"""

from __future__ import annotations

import importlib
import json
import pathlib
import re
import sys
import unicodedata
import urllib.parse
from collections.abc import Callable, Sequence

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_NOT_YET = 3
EXIT_USAGE = 64

#: The bus's own forge (teamfile: members check references only through gh's GitHub login).
GITHUB_API = "https://api.github.com"
#: A documentation network (CLAUDE/ExampleValues.md): the team admits nothing real beyond loopback
#: and the bus address, as Plan 00161's acceptance team does.
ALLOW_FROM_EXAMPLE = "192.0.2.0/24"
#: helpers/pingbus/protocol.py DESCRIPTION_MAX; the installer applies the full rule again.
DESCRIPTION_MAX = 120
SESSION_ID_RE = re.compile(
    r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
)
_GITHUB_RE = re.compile(
    r"(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)"
    r"(?P<owner>[A-Za-z0-9-]+)/[A-Za-z0-9._-]+?(?:\.git)?/?"
)
_ANSI_RE = re.compile(r"\x1b\[[0-9;]*m")
#: pingbus cli.py, word for word: the refusal when several teams are active and none is inferred.
CHOICES_TEXT = "several teams are active and --to/--re fit more than one (or none): name one with --team:"


class Failed(Exception):
    """An expectation did not hold; the message says what was seen."""


# ---------------------------------------------------------------- the throwaway teams


def is_description(value: str) -> bool:
    return (
        1 <= len(value) <= DESCRIPTION_MAX
        and value == value.strip()
        and not any(unicodedata.category(ch) in ("Cc", "Cf") for ch in value)
    )


def team_file(
    team: str, port: int, address: str, human: str, org: str, description: str
) -> dict:
    """A team on the bus address, trusting the checkout's GitHub owner, with one human and a
    description. The installer validates it again; this refuses only what would make the run
    meaningless."""
    if not is_description(description):
        raise ValueError(
            f"description must be 1 to {DESCRIPTION_MAX} characters, stripped, "
            "with no control or format character"
        )
    return {
        "team": team,
        "port": port,
        "listen": [address],
        "allow_from": [ALLOW_FROM_EXAMPLE],
        "humans": [human],
        "org": org,
        "forge_api": GITHUB_API,
        "description": description,
    }


def host_vars(teams: Sequence[dict]) -> str:
    """The teams as `agent_bus_teams` entries in the subset ccy-teams reads (each value a JSON
    flow value), and the bus address, so ccy-teams pointed at this file sees exactly them."""
    if not teams:
        raise ValueError("host-vars needs a team file")
    lines = [
        "# Plan 00201 M7 acceptance: the throwaway teams only, for CCY_TEAMS_HOST_VARS.",
        f"agent_bus_address: {teams[0]['listen'][0]}",
        "agent_bus_teams:",
    ]
    for team in teams:
        first = True
        for key, value in team.items():
            lines.append(
                f"  {'- ' if first else '  '}{key}: {json.dumps(value, ensure_ascii=True)}"
            )
            first = False
    return "\n".join(lines) + "\n"


def bus_ports(
    count: int,
    declared: set[int],
    listening: set[int],
    choose: Callable[[set[int]], int],
) -> list[int]:
    """`count` ports by the bus's own finder (declare.choose_port: its range, never a port the
    owner declares or one listening now), each chosen port counted as used for the next."""
    used = set(declared) | set(listening)
    ports = []
    for _ in range(count):
        port = choose(used)
        ports.append(port)
        used.add(port)
    return ports


def _declare() -> object:
    """helpers.agent_bus.declare from this checkout, imported only by the host-side commands
    that need it."""
    here = pathlib.Path(__file__).resolve().parent
    for candidate in (here, *here.parents):
        if (candidate / "ansible.cfg").exists():
            if str(candidate) not in sys.path:
                sys.path.insert(0, str(candidate))
            return importlib.import_module("helpers.agent_bus.declare")
        if (candidate / ".git").exists():
            break
    raise ValueError(
        f"no ansible.cfg above {here}: not inside a fedora-desktop checkout"
    )


def owner_bus_ports(count: int, host_vars_path: str) -> list[int]:
    """Ports for the throwaway teams, clear of every port the owner's host_vars declare (an
    absent file declares none) and of every port listening now (ss -Hltn)."""
    declare = _declare()
    path = pathlib.Path(host_vars_path)
    teams = (
        declare.parse(path.read_text(encoding="utf-8")).teams if path.exists() else ()
    )
    declared = {team.port for team in teams if team.port is not None}
    try:
        listening = declare.listening_ports(declare._ss())
        return bus_ports(count, declared, listening, declare.choose_port)
    except declare.DeclareError as exc:
        raise ValueError(str(exc)) from None


def launch_conf_clean(conf: str, teams: Sequence[str]) -> None:
    """F1: ccy's Quick Launch record names none of `teams` in LAST_TEAMS."""
    for line in conf.splitlines():
        match = re.fullmatch(r'LAST_TEAMS="?([^"]*)"?\s*', line.strip())
        if match is None:
            continue
        named = [item.strip() for item in match.group(1).split(",") if item.strip()]
        found = [item for item in named if item.rpartition("@")[2] in teams]
        if found:
            raise Failed(
                f"the Quick Launch record names the acceptance seats {found} (LAST_TEAMS)"
            )


def expect_own_team(installed: object, team: str, prefix: str) -> None:
    """F2: an installed team of this name is the acceptance's own: its description says so."""
    if not isinstance(installed, dict) or installed.get("team") != team:
        raise Failed(f"the installed team file is not team {team}'s")
    description = installed.get("description")
    if not isinstance(description, str) or not description.startswith(prefix):
        raise Failed(
            f"team {team} is installed with the description {description!r}, not one starting "
            f"{prefix!r}: it is not this acceptance's, so it is not purged"
        )


def say_outcome(status: int, stderr: str) -> int:
    """F7: 0 sent; 3 not yet, while a team's record is not verified (inference waits for it);
    1 anything else."""
    if status == 0:
        return EXIT_OK
    if (
        status == 64
        and CHOICES_TEXT in stderr
        and "no verified team record yet" in stderr
    ):
        return EXIT_NOT_YET
    return EXIT_FAIL


def github_owner(url: str) -> str:
    match = _GITHUB_RE.fullmatch(url.strip())
    if match is None:
        raise ValueError("the remote is not a github.com repository URL")
    return match["owner"].lower()


def member_field(member: object, key: str) -> str:
    value = member.get(key) if isinstance(member, dict) else None
    if not isinstance(value, str) or not value:
        raise ValueError(f"member.json has no {key}")
    return value


def login_body(user: str, password: str) -> dict:
    if not password:
        raise ValueError("no password on stdin")
    return {
        "type": "m.login.password",
        "identifier": {"type": "m.id.user", "user": user},
        "password": password,
    }


def access_token(response: object) -> str:
    token = response.get("access_token") if isinstance(response, dict) else None
    if not isinstance(token, str) or not token:
        raise ValueError("the login response has no access_token")
    return token


def expect_topic(state: object, description: str) -> None:
    topics = [
        event.get("content", {}).get("topic")
        for event in (state if isinstance(state, list) else [])
        if isinstance(event, dict)
        and event.get("type") == "m.room.topic"
        and event.get("state_key") == ""
    ]
    if topics != [description]:
        raise Failed(f"the room's topic is {topics!r}, want [{description!r}]")


# ---------------------------------------------------------------- the sessions


def user_line(text: str) -> str:
    if not text:
        raise ValueError("a user line needs text")
    return (
        json.dumps({"type": "user", "message": {"role": "user", "content": text}})
        + "\n"
    )


def _json_objects(text: str) -> list[dict]:
    found = []
    for line in text.splitlines():
        if not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except ValueError:
            continue
        if isinstance(obj, dict):
            found.append(obj)
    return found


def turns(stream: str) -> int:
    return sum(1 for obj in _json_objects(stream) if obj.get("type") == "result")


def session_id(stream: str) -> str | None:
    for obj in _json_objects(stream):
        if obj.get("type") == "system" and obj.get("subtype") == "init":
            value = obj.get("session_id")
            if not isinstance(value, str) or not SESSION_ID_RE.fullmatch(value):
                raise ValueError("the init line's session_id is not a session ID")
            return value
    return None


def _items(seats: str) -> list[tuple[str, str]]:
    pairs = [tuple(item.split("@", 1)) for item in seats.split(",")]
    if not pairs or any(len(pair) != 2 or not all(pair) for pair in pairs):
        raise ValueError(f"want SEAT@TEAM[,SEAT@TEAM...], got {seats!r}")
    return [(seat, team) for seat, team in pairs]


def all_held(seat_list: str, seats: str) -> bool:
    """`agent-bus seat list`'s SEAT lines (SEAT, team, seat, held|free, self|-, handle)."""
    states = {}
    for line in seat_list.splitlines():
        fields = line.split("\t")
        if len(fields) == 6 and fields[0] == "SEAT":
            states[(fields[2], fields[1])] = fields[3]
    return all(states.get(item) == "held" for item in _items(seats))


def seat_containers(listing: str, teams: Sequence[str]) -> list[str]:
    found = []
    for line in listing.splitlines():
        container, _, label = line.partition("\t")
        items = [item.strip() for item in label.split(",")]
        if container and any(
            item.endswith(f"@{team}") for item in items for team in teams
        ):
            found.append(container)
    return found


def expect_list_row(listing: str, team: str, description: str) -> None:
    """The team row of `ccy-teams --list`, piped: TEAM padded to 24, then the MEMBER cell, which
    is the description printed whole (padded to 36 when shorter), then the KIND `team`."""
    row = re.compile(re.escape(f"{team:<24}  {description}") + r" *  team  ")
    rows = [
        line
        for line in listing.splitlines()
        if line.startswith(f"{team} ") or line == team
    ]
    if not any(row.match(line) for line in rows):
        raise Failed(
            f"ccy-teams --list shows no team row for {team} with its description whole; "
            f"its rows: {rows!r}"
        )


def expect_block(stderr: str, seats: Sequence[tuple[str, str, str]]) -> None:
    """S-C4: "This session is on N teams:" then one line per seat, team, seat, role, description."""
    lines = stderr.splitlines()
    noun = "team" if len(seats) == 1 else "teams"
    head = f"This session is on {len(seats)} {noun}:"
    if head not in lines:
        raise Failed(f"no launch block line {head!r}")
    start = lines.index(head) + 1
    width = max(len(team) for _, team, _ in seats)
    problems = []
    for offset, (seat, team, description) in enumerate(seats):
        got = lines[start + offset] if start + offset < len(lines) else ""
        want = re.compile(
            re.escape(f"  {team:<{width}}  seat {seat} (")
            + r"[^()]+"
            + re.escape(f")  {description}")
        )
        if not want.fullmatch(got):
            problems.append(
                f"block line {offset + 1} is {got!r}, want {team} seat {seat} (role)  {description}"
            )
    if problems:
        raise Failed("; ".join(problems))


def _status(text: str) -> tuple[dict[str, str], dict[str, str]]:
    trust, about = {}, {}
    for line in text.splitlines():
        fields = line.split("\t")
        if fields[0] == "TEAM" and len(fields) >= 4:
            trust[fields[1]] = fields[3]
        elif fields[0] == "ABOUT" and len(fields) == 4:
            about[fields[2]] = json.loads(fields[3])
    return trust, about


def status_ready(text: str, teams: Sequence[str]) -> bool:
    if not teams:
        raise UsageError("status-ready needs the teams")
    trust, about = _status(text)
    return all(trust.get(team) == "trust=trusted" and team in about for team in teams)


def expect_about(text: str, team: str, description: str) -> None:
    about = _status(text)[1].get(team)
    if about != description:
        raise Failed(
            f"pingbus status ABOUT for {team} is {about!r}, want {description!r}"
        )


def expect_sent(text: str, team: str) -> str:
    lines = [line.split("\t") for line in text.splitlines() if line]
    if len(lines) != 1 or len(lines[0]) != 4 or lines[0][:3] != ["SENT", "1", team]:
        raise Failed(f"want exactly one SENT line for team {team}, got {text!r}")
    return lines[0][3]


def expect_choices(stderr: str, teams: Sequence[tuple[str, str]]) -> None:
    if CHOICES_TEXT not in stderr:
        raise Failed(f"the refusal does not say {CHOICES_TEXT!r}: {stderr!r}")
    lines = stderr.splitlines()
    missing = [
        f"  {team}  {description}"
        for team, description in teams
        if f"  {team}  {description}" not in lines
    ]
    if missing:
        raise Failed(f"the refusal does not list {missing!r}: {stderr!r}")


#: The status line command the member kit's settings.json names (helpers/pingbus/statusline.py).
KIT_STATUS_LINE = "pingbus status-line"


def expect_kit_status_line(settings: object) -> None:
    line = settings.get("statusLine") if isinstance(settings, dict) else None
    command = line.get("command") if isinstance(line, dict) else None
    if command != KIT_STATUS_LINE:
        raise Failed(f"the kit's settings.json status line is {line!r}, want the command {KIT_STATUS_LINE!r}")


def expect_segment(text: str, teams: str) -> None:
    """`pingbus status-line`'s last line is the session's own status line, then `| bus: TEAMS`."""
    lines = _ANSI_RE.sub("", text).strip().splitlines()
    last = lines[-1] if lines else ""
    inner, sep, segment = last.rpartition(" | ")
    if not sep or not inner.strip() or segment != f"bus: {teams}":
        raise Failed(f"the status line ends {last!r}, want the session's own line then | bus: {teams}")


def expect_plain(labels: object, env: object) -> None:
    if not isinstance(labels, dict) or labels.get("ccy") != "true":
        raise Failed(f"not a ccy session's labels: {labels!r}")
    problems = []
    if "ccy-seats" in labels:
        problems.append(f"the container carries ccy-seats={labels['ccy-seats']!r}")
    bus = [
        item.split("=", 1)[0]
        for item in (env if isinstance(env, list) else [])
        if isinstance(item, str) and item.startswith("PINGBUS_")
    ]
    if bus:
        problems.append(f"the container's environment sets {bus}")
    if problems:
        raise Failed("; ".join(problems))


# ---------------------------------------------------------------- command line


def _read(path: str) -> str:
    with open(path, encoding="utf-8") as handle:
        return handle.read()


def _json_file(path: str) -> object:
    return json.loads(_read(path))


def _print(value: object) -> None:
    print(value)


def _lines(values: Sequence[object]) -> None:
    for value in values:
        print(value)


def _maybe(value: str | None) -> int:
    if value is None:
        return EXIT_NOT_YET
    print(value)
    return EXIT_OK


def _groups(args: Sequence[str], size: int) -> list[tuple[str, ...]]:
    if not args or len(args) % size:
        raise UsageError(f"takes arguments in groups of {size}")
    return [tuple(args[i : i + size]) for i in range(0, len(args), size)]


class UsageError(Exception):
    pass


def _team_file(a: Sequence[str]) -> None:
    print(json.dumps(team_file(a[0], int(a[1]), a[2], a[3], a[4], a[5]), indent=1))


#: name -> (argument count, or -1 for one or more, and the function).
COMMANDS: dict[str, tuple[int, Callable[[Sequence[str]], int | None]]] = {
    "team-file": (6, _team_file),
    "host-vars": (-1, lambda a: print(host_vars([_json_file(p) for p in a]), end="")),
    "bus-ports": (2, lambda a: _lines(owner_bus_ports(int(a[0]), a[1]))),
    "launch-conf-clean": (2, lambda a: launch_conf_clean(_read(a[0]), a[1].split(","))),
    "expect-own-team": (3, lambda a: expect_own_team(_json_file(a[0]), a[1], a[2])),
    "say-outcome": (2, lambda a: say_outcome(int(a[0]), _read(a[1]))),
    "github-owner": (1, lambda a: _print(github_owner(a[0]))),
    "member-field": (2, lambda a: _print(member_field(_json_file(a[0]), a[1]))),
    "room-path": (1, lambda a: _print(urllib.parse.quote(a[0], safe=""))),
    "login-body": (
        1,
        lambda a: _print(
            json.dumps(login_body(a[0], sys.stdin.read().removesuffix("\n")))
        ),
    ),
    "access-token": (0, lambda a: _print(access_token(json.loads(sys.stdin.read())))),
    "expect-topic": (2, lambda a: expect_topic(_json_file(a[0]), a[1])),
    "user-line": (1, lambda a: print(user_line(a[0]), end="")),
    "turns": (1, lambda a: _print(turns(_read(a[0])))),
    "session-id": (1, lambda a: _maybe(session_id(_read(a[0])))),
    "expect-held": (
        2,
        lambda a: EXIT_OK if all_held(_read(a[0]), a[1]) else EXIT_NOT_YET,
    ),
    "seat-containers": (
        1,
        lambda a: _lines(seat_containers(sys.stdin.read(), a[0].split(","))),
    ),
    "expect-list-row": (3, lambda a: expect_list_row(_read(a[0]), a[1], a[2])),
    "expect-block": (-1, lambda a: expect_block(_read(a[0]), _groups(a[1:], 3))),
    "status-ready": (
        -1,
        lambda a: EXIT_OK if status_ready(_read(a[0]), a[1:]) else EXIT_NOT_YET,
    ),
    "expect-about": (3, lambda a: expect_about(_read(a[0]), a[1], a[2])),
    "expect-sent": (2, lambda a: _print(expect_sent(_read(a[0]), a[1]))),
    "expect-choices": (-1, lambda a: expect_choices(_read(a[0]), _groups(a[1:], 2))),
    "expect-kit-status-line": (1, lambda a: expect_kit_status_line(_json_file(a[0]))),
    "expect-segment": (2, lambda a: expect_segment(_read(a[0]), a[1])),
    "expect-plain": (2, lambda a: expect_plain(_json_file(a[0]), _json_file(a[1]))),
}


def main(argv: Sequence[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__, file=sys.stderr)
        return EXIT_USAGE
    command, args = argv[0], list(argv[1:])
    count, run = COMMANDS[command]
    if (count >= 0 and len(args) != count) or (count < 0 and not args):
        print(
            f"m7_check {command}: takes {count if count >= 0 else 'one or more'} argument(s), "
            f"got {len(args)}",
            file=sys.stderr,
        )
        return EXIT_USAGE
    try:
        status = run(args)
    except UsageError as exc:
        print(f"m7_check {command}: {exc}", file=sys.stderr)
        return EXIT_USAGE
    except (Failed, ValueError, KeyError, OSError) as exc:
        print(f"m7_check {command}: {exc}", file=sys.stderr)
        return EXIT_FAIL
    return EXIT_OK if status is None else status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
