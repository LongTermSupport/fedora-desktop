#!/usr/bin/env python3
"""Plan 00201 M7: the pure logic behind acceptance.bash (the two-team acceptance leg).

_acceptance-m7.inc.bash drives the host (agent-bus-install, agent-bus, ccy-teams, two headless
ccy sessions, podman exec into them, the human's curl); this module writes what it hands them
and judges what comes back. Every judgement rests on what a tool printed or recorded, never on
the model's prose. Standard library only; the host-side commands read the values the product owns
from this checkout, lazily, never a copy: `bus-ports` (declare.py), `team-file` (protocol.py),
`session-paths` and `expect-session-settings` (seat.py, membership.py, and the kit's settings.json), `expect-list-row` (the board's
table.go), `hot-marks` (hot.go), and `status-line-names` and `expect-segment` (statusline.go and
statusline.py). Commands
(stdout is the payload, reasons on stderr):

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
  seat-containers TEAM[,TEAM...]     stdin "ID<TAB>seats" (the session's desired seats): those holding a seat of one
  expect-list-row LIST TEAM DESCRIPTION   `ccy-teams --list` (piped) shows it whole on the team row
  expect-block STDERR (SEAT TEAM DESCRIPTION)...   the launch block names exactly those seats
  status-ready STATUS TEAM...        `pingbus status`: each team trusted with an ABOUT line; 3 if not
  expect-about STATUS TEAM DESCRIPTION   that team's ABOUT line carries DESCRIPTION
  expect-sent OUT TEAM               one SENT line, for TEAM; prints its event ID
  expect-choices STDERR (TEAM DESCRIPTION)...   the "several teams are active" refusal, listing each
  session-paths                      seat.py's session home and its settings file, membership.py's mount and desired file, one per line
  hot-marks                          the done and failed step marks of ccy-teams session add/remove (hot.go), one per line
  status-line-names                  the Go status line's teams row head, then its inner-only variable (statusline.go)
  expect-session-settings SETTINGS   a seated ccy session's own settings.json: inbound accepted, the seated status line this checkout's seat.py writes
  session-status-line PROC           IN THE SESSION'S CONTAINER: the status line its claude draws, the payload on stdin
  expect-segment OUT TEAMS           the status line printed the session's own line, then 👥 TEAMS on rows of its own
  expect-plain LABELS_JSON ENV_JSON  a ccy session container with no seat label or PINGBUS_ variable but PINGBUS_MEMBERSHIP=off

Exit codes: 0 ok; 1 an expectation failed or the input was malformed; 3 not there yet;
64 usage.
"""

from __future__ import annotations

import importlib
import json
import os
import pathlib
import re
import subprocess
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
SESSION_ID_RE = re.compile(
    r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}"
)
_GITHUB_RE = re.compile(
    r"(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/)"
    r"(?P<owner>[A-Za-z0-9-]+)/[A-Za-z0-9._-]+?(?:\.git)?/?"
)
#: The Go `pingbus-status-line` that draws the segment, in this checkout; its `label` constant
#: is read from the source, not copied (expect-segment runs on the host).
STATUS_LINE_GO = pathlib.PurePosixPath("src-go/internal/pingbus/statusline/statusline.go")
_GO_LABEL_RE = re.compile(r'^\s*label\s*=\s*"([^"\\]+)"\s*$', re.MULTILINE)
#: The Go table `ccy-teams --list` prints; its `headings` and the team row's `KindTeam` are read
#: from the source, not copied (expect-list-row runs on the host).
BOARD_TABLE_GO = pathlib.PurePosixPath("src-go/internal/teams/board/table.go")
_GO_HEADINGS_RE = re.compile(r"^var headings = \[columns\]string\{([^}]*)\}\s*$", re.MULTILINE)
_GO_KIND_TEAM_RE = re.compile(r'^\s*KindTeam\s+Kind\s*=\s*"([^"\\]+)"\s*$', re.MULTILINE)
#: Stand-ins for the team and the member when segment_piece_re has statusline.py draw a piece:
#: control characters, which no team name, handle or fixed text of the piece holds.
_PIECE_TEAM, _PIECE_MEMBER, _PIECE_LETTER = "\x00\x01\x00", "\x00\x02\x00", "\x00\x03\x00"
#: A mark after a team's role, such as `(down)`; a mark of something unreadable (`(colour?)`)
#: has a `?` and is not one of these.
_SEGMENT_MARKS = r"(?: \([a-z]+\))*"
#: pingbus cli.py, word for word: the refusal when several teams are active and none is inferred.
CHOICES_TEXT = "several teams are active and --to/--re fit more than one (or none): name one with --team:"


class Failed(Exception):
    """An expectation did not hold; the message says what was seen."""


# ---------------------------------------------------------------- the throwaway teams


def description_max() -> int:
    """The longest description, from this checkout's helpers/pingbus/protocol.py; the
    installer applies the full rule again."""
    return _checkout_module("helpers.pingbus.protocol").DESCRIPTION_MAX


def is_description(value: str) -> bool:
    return (
        1 <= len(value) <= description_max()
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
            f"description must be 1 to {description_max()} characters, stripped, "
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


def _checkout_root() -> pathlib.Path:
    """This checkout's top: the first directory above this file holding ansible.cfg."""
    here = pathlib.Path(__file__).resolve().parent
    for candidate in (here, *here.parents):
        if (candidate / "ansible.cfg").exists():
            return candidate
        if (candidate / ".git").exists():
            break
    raise ValueError(
        f"no ansible.cfg above {here}: not inside a fedora-desktop checkout"
    )


def _checkout_module(name: str) -> object:
    """A module of this checkout's helpers, imported only by the host-side commands that
    need it."""
    root = str(_checkout_root())
    if root not in sys.path:
        sys.path.insert(0, root)
    return importlib.import_module(name)


def _declare() -> object:
    return _checkout_module("helpers.agent_bus.declare")


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


def board_table(source: str | None = None) -> tuple[tuple[str, str, str, str], str]:
    """The board's first four column headings (TEAM, MEMBER, KIND, STATE: colTeam to colState)
    and the KIND a team row shows, read from `source`, default this checkout's table.go. Each
    must be declared exactly once, or this fails rather than guess."""
    if source is None:
        source = (_checkout_root() / BOARD_TABLE_GO).read_text(encoding="utf-8")
    found = _GO_HEADINGS_RE.findall(source)
    if len(found) != 1:
        raise Failed(f"want exactly one `var headings = [columns]string{{...}}` in {BOARD_TABLE_GO}, found {found!r}")
    names = re.findall(r'"([^"\\]+)"', found[0])
    kinds = _GO_KIND_TEAM_RE.findall(source)
    if len(names) < 4 or len(kinds) != 1:
        raise Failed(f"want four or more headings and one `KindTeam Kind = \"...\"` in {BOARD_TABLE_GO}, "
                     f"found {names!r} and {kinds!r}")
    return (names[0], names[1], names[2], names[3]), kinds[0]


def expect_list_row(
    listing: str, team: str, description: str,
    table: tuple[tuple[str, str, str, str], str] | None = None,
) -> None:
    """The team row of `ccy-teams --list`, piped: the TEAM cell, the MEMBER cell, which is the
    description printed whole, then the KIND of a team row, each cell starting exactly where its
    heading does. The board sizes each column to its longest value (board.measure), so the
    widths are read from the heading line, never assumed. `table` is (headings, team kind),
    default board_table()."""
    (first, *rest), team_kind = board_table() if table is None else table
    lines = listing.splitlines()
    heading = lines[0] if lines else ""
    starts = [heading.find(f"  {name}  ") + 2 for name in rest]
    if not heading.startswith(f"{first} ") or min(starts) < 2 or starts != sorted(starts):
        raise Failed(f"ccy-teams --list did not start with its {first} {' '.join(rest)} heading: {heading!r}")
    member, kind, state = starts
    row = (team.ljust(member - 2) + "  " + description.ljust(kind - member - 2) + "  "
           + team_kind.ljust(state - kind - 2) + "  ")
    rows = [line for line in lines[1:] if line.startswith(f"{team} ") or line == team]
    if not any(line[:state] == row for line in rows):
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


#: The member kit's settings in this checkout, whose status line command a seated session's
#: guard runs (the ccy image's /opt/claude-yolo/optional/agent-bus/settings.json).
KIT_SETTINGS = pathlib.PurePosixPath("files/opt/claude-yolo/optional/agent-bus/settings.json")
#: Each status line command is given this long before the leg calls it hung.
STATUS_LINE_TIMEOUT_S = 30


def seated_status_line() -> dict:
    """The status line `pingbus seat exec` writes for a seated session, from this checkout's
    own helpers/pingbus/seat.py (Plan 00203 H34): never a copy, so it cannot drift from the kit."""
    seat = _checkout_module("helpers.pingbus.seat")
    return seat.session_settings(None, seated=True)["statusLine"]


def session_paths() -> list[str]:
    """Where a ccy session keeps what the legs read, from this checkout's seat.py and
    membership.py, never a copy: the session home `pingbus seat exec` builds, the settings it
    writes there, the membership mount's destination, and the desired membership's file name."""
    seat = _checkout_module("helpers.pingbus.seat")
    membership = _checkout_module("helpers.pingbus.membership")
    home = pathlib.PurePosixPath(seat.SESSION_HOME)
    paths = [str(home), str(home / seat.SESSION_SETTINGS), membership.CCY_MOUNT, membership.DESIRED_FILE]
    if not all(isinstance(path, str) and path and "\n" not in path for path in paths):
        raise Failed(f"seat.py and membership.py name no usable session paths: {paths!r}")
    return paths


def kit_command() -> str:
    """The status line command of this checkout's member kit settings."""
    path = _checkout_root() / KIT_SETTINGS
    line = json.loads(path.read_text(encoding="utf-8")).get("statusLine")
    command = line.get("command") if isinstance(line, dict) else None
    if not isinstance(command, str) or not command.strip():
        raise Failed(f"{path} names no status line command: {line!r}")
    return command


def expect_session_settings(settings: object, want: dict | None = None, kit: str | None = None) -> None:
    """The settings `pingbus seat exec` wrote for a seated ccy session (H34): inbound accepted,
    and the status line exactly what this checkout's seat.py writes for a seated session (each
    key it sets; Claude Code's own padding may be added), a shell guard that ends by running
    the kit's status line command. `want` and `kit` default to this checkout's."""
    want = seated_status_line() if want is None else want
    kit = kit_command() if kit is None else kit
    guard = want.get("command")
    if not isinstance(guard, str) or not guard.startswith("[ -e ") or not guard.endswith(f"exec {kit}"):
        raise Failed(f"this checkout's seat.py guard {guard!r} does not end by running the kit's "
                     f"status line command {kit!r} ({KIT_SETTINGS})")
    line = settings.get("statusLine") if isinstance(settings, dict) else None
    problems = []
    if not isinstance(settings, dict) or settings.get("crossSessionInbound") != "accept":
        problems.append("no crossSessionInbound accept")
    if not isinstance(line, dict) or any(line.get(key) != value for key, value in want.items()):
        problems.append(f"the status line is {line!r}, not the seated guard this checkout writes, {want!r}")
    if problems:
        raise Failed(f"the session's settings: {'; '.join(problems)}")


def _claude_processes(proc: pathlib.Path) -> list[tuple[pathlib.Path, list[str]]]:
    found = []
    for entry in sorted(proc.iterdir()):
        if not entry.name.isdigit():
            continue
        try:
            argv = [part.decode("utf-8", "replace") for part in (entry / "cmdline").read_bytes().split(b"\0")[:-1]]
        except OSError:
            continue
        if argv and pathlib.PurePath(argv[0]).name in ("claude", "claude.exe") and "--settings" in argv:
            found.append((entry, argv))
    return found


def session_status_line(proc: pathlib.Path, payload: bytes) -> str:
    """Run in the session's container: the status line its claude draws. The command is the
    `statusLine` of the settings file claude was launched with (`--settings`), run as Claude
    Code runs it (through the shell, the payload on stdin) in claude's own environment and
    working directory, read from /proc. Its stdout is returned, never claude's environment."""
    found = _claude_processes(proc)
    if len(found) != 1:
        raise Failed(f"want exactly one claude process launched with --settings, found {[argv for _, argv in found]!r}")
    entry, argv = found[0]
    settings_path = pathlib.Path(argv[argv.index("--settings") + 1]) if argv.index("--settings") + 1 < len(argv) else None
    if settings_path is None or not settings_path.is_absolute():
        raise Failed(f"claude's --settings is not a file path: {argv!r}")
    settings = json.loads(settings_path.read_text(encoding="utf-8"))
    line = settings.get("statusLine") if isinstance(settings, dict) else None
    command = line.get("command") if isinstance(line, dict) else None
    if not isinstance(command, str) or not command.strip():
        raise Failed(f"{settings_path} names no status line command: {line!r}")
    env = dict(item.split("=", 1) for item in (entry / "environ").read_bytes().decode("utf-8", "replace").split("\0") if "=" in item)
    result = subprocess.run(["/bin/sh", "-c", command], input=payload, capture_output=True, env=env,
                            cwd=os.readlink(entry / "cwd"), timeout=STATUS_LINE_TIMEOUT_S, check=False)
    text = result.stdout.decode("utf-8", "replace")
    if result.returncode != 0:
        raise Failed(f"the status line command {command!r} exited {result.returncode}: {text!r} "
                     f"{result.stderr.decode('utf-8', 'replace')!r}")
    print(f"m7_check session-status-line: {settings_path}: {command}", file=sys.stderr)
    return text.rstrip("\n")


#: ccy-teams' hot add/remove, whose step marks are read from its const block.
HOT_GO = pathlib.PurePosixPath("src-go/internal/teams/hot/hot.go")


def go_string_const(source: str, name: str, where: object) -> str:
    """The value of the one plain `name = "..."` line (a Go const block's string entry) in
    `source`; zero or several such lines fail rather than guess."""
    found = re.findall(rf'^\s*{re.escape(name)}\s*=\s*"([^"\\]+)"\s*$', source, re.MULTILINE)
    if len(found) != 1:
        raise Failed(f"want exactly one `{name} = \"...\"` constant in {where}, found {found!r}")
    return found[0]


def hot_marks(source: str | None = None) -> list[str]:
    """The marks `ccy-teams session add/remove` starts a done and a failed step with (hot.go's
    markDone and markFailed)."""
    if source is None:
        source = (_checkout_root() / HOT_GO).read_text(encoding="utf-8")
    return [go_string_const(source, "markDone", HOT_GO), go_string_const(source, "markFailed", HOT_GO)]


def status_line_names(source: str | None = None) -> list[str]:
    """The Go status line's teams row head (segment_label) and the variable that has it draw
    only the bus segment (statusline.go's innerVar)."""
    if source is None:
        source = (_checkout_root() / STATUS_LINE_GO).read_text(encoding="utf-8")
    return [segment_label(source)[0], go_string_const(source, "innerVar", STATUS_LINE_GO)]


def segment_label(source: str | None = None) -> tuple[str, str]:
    """How the status line starts its teams row and indents a wrapped one under it: the Go
    drawer's `label` constant (read from `source`, default this checkout's statusline.go) and a
    space, and as many spaces as that takes columns, by helpers/pingbus/statusline.py's
    display_width, the reference Go's displayWidth is the port of."""
    if source is None:
        source = (_checkout_root() / STATUS_LINE_GO).read_text(encoding="utf-8")
    found = _GO_LABEL_RE.findall(source)
    if len(found) != 1:
        raise Failed(f"want exactly one `label = \"...\"` constant in {STATUS_LINE_GO}, found {found!r}")
    head = f"{found[0]} "
    return head, " " * _checkout_module("helpers.pingbus.statusline").display_width(head)


def _statusline() -> object:
    return _checkout_module("helpers.pingbus.statusline")


def segment_piece(statusline: object | None = None) -> str:
    """How the status line draws one team, with stand-ins for the team, the member and the role's
    letter: this checkout's helpers/pingbus/statusline.py `_team_piece` (Plan 00217 C15; the Go
    drawer's teamsText is its port, both held to the same goldens), drawn once for each role in
    its ROLE_LETTERS. The separators are the product's; a drawer whose pieces differ by more than
    the letter, or that does not show each stand-in once, fails rather than guess."""
    statusline = _statusline() if statusline is None else statusline
    shapes = set()
    for role, letter in statusline.ROLE_LETTERS.items():
        piece = statusline._team_piece({}, _PIECE_TEAM, (_PIECE_MEMBER, role), False)
        if any(piece.count(part) != 1 for part in (_PIECE_TEAM, _PIECE_MEMBER, letter)):
            raise Failed(f"statusline.py drew {piece!r} for role {role!r}: want the team, the member "
                         f"and the letter {letter!r} once each")
        shapes.add(piece.replace(letter, _PIECE_LETTER))
    if len(shapes) != 1:
        raise Failed(f"statusline.py draws its roles in different shapes: {sorted(shapes)!r}")
    return shapes.pop()


def segment_piece_re(statusline: object | None = None) -> re.Pattern[str]:
    """One team of the segment: segment_piece with the team a team name (protocol.py's
    TEAM_NAME_PATTERN), the member one word and the letter of a role that was read (one of
    ROLE_LETTERS, never ROLE_UNKNOWN), then any marks such as `(down)`."""
    statusline = _statusline() if statusline is None else statusline
    letters = "|".join(re.escape(letter) for letter in statusline.ROLE_LETTERS.values())
    groups = {
        _PIECE_TEAM: f"(?P<team>{_checkout_module('helpers.pingbus.protocol').TEAM_NAME_PATTERN})",
        _PIECE_MEMBER: r"(?P<member>[^\s,()]+)",
        _PIECE_LETTER: f"(?:{letters})",
    }
    parts = re.split(f"({'|'.join(map(re.escape, groups))})", segment_piece(statusline))
    return re.compile("".join(groups.get(part, re.escape(part)) for part in parts) + _SEGMENT_MARKS)


def _coloured_spans(row: str, statusline: object) -> list[str] | None:
    """C10: what follows a row's plain lead is spans joined by plain spaces, each a colour (an
    escape that is not the reset), its text, the reset. The spans' texts, or None."""
    reset = statusline.RESET
    if not row.endswith(reset):
        return None
    texts = []
    for span in row.removesuffix(reset).split(f"{reset} "):
        colour = statusline.ANSI_ESCAPE_RE.match(span)
        if colour is None or colour[0] == reset or statusline.ANSI_ESCAPE_RE.search(span, colour.end()):
            return None
        texts.append(span[colour.end():])
    return texts


def _segment_teams(raw_rows: list[str], leads: list[str], piece_re: re.Pattern[str],
                   statusline: object) -> str | None:
    """The team names the segment's rows show, comma-separated, if every row is its lead then
    spans of one team piece each, every piece but the last ending in a comma, each a team whose
    role and member were read; else None."""
    texts: list[str] = []
    for row, lead in zip(raw_rows, leads, strict=True):
        found = _coloured_spans(row.removeprefix(lead), statusline) if row.startswith(lead) else None
        if found is None:
            return None
        texts.extend(found)
    if not all(text.endswith(",") for text in texts[:-1]):
        return None
    pieces = [piece_re.fullmatch(text.removesuffix(",")) for text in texts[:-1]] + [piece_re.fullmatch(texts[-1])]
    if not all(pieces) or any(match["member"] == statusline.MEMBER_UNKNOWN for match in pieces):
        return None
    return ",".join(match["team"] for match in pieces)


def expect_segment(text: str, teams: str, label: tuple[str, str] | None = None) -> None:
    """`pingbus status-line` prints the session's own status line, then `👥 TEAMS` on rows of its
    own: the first row starts with the label, later rows are indented under it (it wraps after a
    comma at COLUMNS). Each team is drawn as segment_piece_re: `team: member (letter)`, then any
    marks, its role one that was read (Plan 00203 Task 3.8) and its member one that was read
    (Plan 00217 C16), each team in its own colour span (C10). TEAMS is the team names alone,
    comma-separated. `label` is (head, indent), default segment_label()."""
    head, indent = segment_label() if label is None else label
    statusline = _statusline()
    piece_re = segment_piece_re(statusline)
    raw = text.strip("\n").splitlines()
    lines = [statusline.ANSI_ESCAPE_RE.sub("", line) for line in raw]
    starts = [index for index, line in enumerate(lines) if line.startswith(head)]
    first = starts[-1] if starts else len(lines)
    inner = lines[:first]
    leads = [head, *(indent for _ in raw[first + 1:])]
    shown = _segment_teams(raw[first:], leads, piece_re, statusline) if first < len(raw) else None
    if not any(line.strip() for line in inner) or shown != teams:
        letters = "|".join(statusline.ROLE_LETTERS.values())
        want = ", ".join(segment_piece(statusline).replace(_PIECE_TEAM, team)
                         .replace(_PIECE_MEMBER, "<member>").replace(_PIECE_LETTER, letters)
                         for team in teams.split(","))
        raise Failed(f"the status line is {raw!r}, want the session's own line then {head}{want}, "
                     "each team in its own colour")


def expect_plain(labels: object, env: object) -> None:
    if not isinstance(labels, dict) or labels.get("ccy") != "true":
        raise Failed(f"not a ccy session's labels: {labels!r}")
    problems = []
    if "ccy-seats" in labels:
        problems.append(f"the container carries ccy-seats={labels['ccy-seats']!r}")
    # Plan 00203 M2: the one bus variable a plain launch carries is the marker saying it has no
    # kit, PINGBUS_MEMBERSHIP=off (a headless launch outside ccy's tmux, no --teams).
    bus = [
        item.split("=", 1)[0]
        for item in (env if isinstance(env, list) else [])
        if isinstance(item, str) and item.startswith("PINGBUS_") and item != "PINGBUS_MEMBERSHIP=off"
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
    "session-paths": (0, lambda a: _lines(session_paths())),
    "hot-marks": (0, lambda a: _lines(hot_marks())),
    "status-line-names": (0, lambda a: _lines(status_line_names())),
    "expect-session-settings": (1, lambda a: expect_session_settings(_json_file(a[0]))),
    "session-status-line": (
        1,
        lambda a: _print(session_status_line(pathlib.Path(a[0]), sys.stdin.read().encode("utf-8"))),
    ),
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
