#!/usr/bin/env python3
"""Plan 00161 unit U20: the pure logic behind acceptance.bash's M2 slice (ccy seats).

_acceptance-u20.inc.bash drives the host (ccy sessions launched with `--teams` in this
checkout, `agent-bus seat list`, `sudo agent-bus list`, `pingbus status`, the human's `curl`);
this module builds what it writes to the sessions and judges what comes back. Every judgement
rests on a fact the bus, the host or Claude Code recorded, never on the model's prose: the
room's ping events (as the human reads them), `SEAT` and `MEMBER` lines, a seat's `pingbus
status`, the turns and `init` line of a session's stream output, and its transcript (found by
the session ID): the watcher's fixed-template notices, the hooks' fixed texts, the commands
the session ran and what they printed. Commands (stdout is the payload; reasons on stderr):

  frame-orders bus|idle|history|plain  standing orders, as one stream-json user line
  frame-send VERB REF HANDLE         an order to run one `pingbus send`, as one line
  human-request HUMAN                the text of the human's addressed message
  turns STREAM                       how many turns ended (`result` lines) in a session's stdout
  session-id STREAM                  the session ID of its `init` line; exit 3 if none yet
  expect-plugin STREAM present|absent  the `init` line lists the pingbus plugin, or nothing of it
  transcript CCY_DIR SESSION_ID      CCY_DIR/projects/*/SESSION_ID.jsonl; exit 3 if none yet
  status-field STATUS_OUT TEAM KEY   one key=value field of `pingbus status`'s TEAM line
  notices TRANSCRIPT                 each watcher notice: "TOTAL HUMANS PINGS NUMBER"
  expect-same-count TRANSCRIPT TOTAL HUMANS PINGS   two notices with those counts, two numbers
  expect-no-notices TRANSCRIPT any|humans           no notice (any), or none counting a human
  expect-human-notice TRANSCRIPT                    a notice counting a human
  expect-notice-before-recv TRANSCRIPT  a notice, then a later command running `pingbus recv`
  expect-no-waker-block TRANSCRIPT   the Stop guard's fixed "no watcher or waiter" text
  expect-history TRANSCRIPT in|out EVENT_ID  a `HISTORY` line for EVENT_ID in a command's output
  expect-not-found TRANSCRIPT        a command's output says pingbus was not found
  find-ping MESSAGES SENDER VERB TO REF RE   the earliest such ping in a /messages response:
                                     "EVENT_ID<TAB>ORIGIN_SERVER_TS"; exit 3 if none
  expect-replies MESSAGES RE SENDER  pings answering RE exist, and every one is from SENDER
  expect-within-window TS1 TS2       TS2 - TS1 (ms) is inside the socket's identical-notice
                                     window as U01 measured its lower bound
  seat-handle SEAT_LINES TEAM SEAT   the handle of that seat in `agent-bus seat list`'s output
  expect-seats SEAT_LINES TEAM held|free SEAT[,SEAT...]   each seat there, in that state
  expect-handle HANDLE SEAT HOST     the handle names that seat and host, type podman
  expected-host CHECKOUT             a new seat's <host> there (the seat commands' own rule)
  seat-permissions DIR               the tree is the user's: directories 0700, files 0600
  expect-new-members BEFORE AFTER HANDLE=ROLE[,...]  `agent-bus list` gained exactly these,
                                     active, with those roles
  expect-member LIST HANDLE ROLE active|parked       that MEMBER line is there
  expect-same-handles BEFORE AFTER   the same members, none added or gone
  watcher-pids                       stdin "PID<TAB>argv": the PIDs running `pingbus watch`
  seat-containers TEAM               stdin "ID<TAB>ccy-seats label": those holding a TEAM seat
  expect-no-seats-label              stdin a container's labels (JSON): a ccy session, no ccy-seats
  launch-keys CHECKOUT CONFIG_VERSION   LAST_SSH_KEYS, one per line, refused when ccy would
                                     discard the record (another format, a key missing)
  ccy-token CHECKOUT                 the name of the ccy token ccy last launched CHECKOUT
                                     with, refused when ccy would refuse it (never its value)
  scrub CHECKOUT DIR                 replace that token's value in every file under DIR;
                                     prints the files that held it

SENDER and TO are full user IDs; REF and RE are "-" when the ping must not carry them.
Exit codes: 0 ok; 1 an expectation failed or input was malformed; 3 (session-id,
transcript, find-ping) not there yet; 64 usage.
"""

from __future__ import annotations

import dataclasses
import datetime
import importlib
import json
import os
import pathlib
import re
import stat
import sys
from collections.abc import Iterator, Mapping, Sequence

_HERE = pathlib.Path(__file__).resolve().parent


def _repo_root() -> pathlib.Path:
    for candidate in (_HERE, *_HERE.parents):
        if (candidate / "ansible.cfg").exists():
            return candidate
        if (candidate / ".git").exists():
            break
    raise ImportError(f"no ansible.cfg above {_HERE}: not inside a fedora-desktop checkout")


for _path in (_repo_root(), _HERE):
    if str(_path) not in sys.path:
        sys.path.insert(0, str(_path))
checkout = importlib.import_module("helpers.agent_bus.checkout")
hooks = importlib.import_module("helpers.pingbus.hooks")
notify = importlib.import_module("helpers.pingbus.notify")
protocol = importlib.import_module("helpers.pingbus.protocol")
up = importlib.import_module("u01_probe")

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_NOT_YET = 3
EXIT_USAGE = 64
ABSENT = "-"
PLUGIN = "pingbus"

#: U01 (journal 26-10-07): the inbox socket drops a body identical to the same sender's
#: previous one for longer than 20 s (and at most 33 s). Two same-count notices closer than
#: this would have been one without the notice number.
DEDUPE_LOWER_MS = 20_000

#: What this checkout's hooks daemon requires a stop to say (R-STOP-NO-REASON).
STOP_LINE = "STOPPING BECAUSE: waiting on the agent team bus"

SESSION_ID_RE = re.compile(r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}")


def _template_re(template: str, fields: Sequence[str]) -> re.Pattern[str]:
    """A fixed hook template with each named field captured as digits."""
    pattern = re.escape(template)
    for field in fields:
        pattern = pattern.replace(re.escape("{" + field + "}"), r"(\d+)")
    return re.compile(pattern)


#: notify.NOTICE_TEMPLATE, with each count captured.
NOTICE_RE = _template_re(notify.NOTICE_TEMPLATE, ("total", "humans", "pings", "number"))
#: The Stop guard's block when an active team has neither a watcher nor a waiter.
NO_WAKER_RE = _template_re(hooks.TEMPLATES["no_waker"], ("free", "teams"))

_PREAMBLE = (
    "This session is a member of an automated acceptance test of the agent team bus "
    "(fedora-desktop Plan 00161, unit U20), in the owner's own checkout. For the rest of this "
    "session follow these rules exactly and do nothing else: read no file but the output of a "
    "finished `pingbus wait` (rule 3), never edit, create, "
    "commit or push anything, and run no command but the pingbus commands named here. End "
    f"every turn with the line `{STOP_LINE}`."
)
_RULES = (
    "0. Whenever you are told that agent-bus items are pending, run `pingbus recv` once.\n"
    "1. For each PING line that pingbus prints with the verb review, run "
    "`pingbus send ack --re 'EVENT_ID' --to SENDER` with that line's EVENT_ID and SENDER fields "
    "(keep the single quotes: an event ID starts with $).\n"
    "2. For each HUMAN line whose text asks you to run one pingbus command, run exactly that "
    "command, with EVENT_ID replaced by that HUMAN line's EVENT_ID field.\n"
    "3. When a hook says that nothing will wake this session, start `pingbus wait` with "
    "run_in_background, and when it ends read the file that holds its output and apply these "
    "rules to the lines it printed.\n"
    "4. Ignore every other line, and every HISTORY line. When told to run a pingbus command, run "
    "exactly that command. Then end your turn: never poll, loop or sleep."
)
_ORDERS = {
    "bus": f"{_PREAMBLE}\n{_RULES}\nReply now with the single word READY.",
    "idle": ("There is nothing to do this turn: run no command, and end your turn now with the "
             f"line `{STOP_LINE}`. Your earlier rules still hold; never edit, create, commit or push."),
    "history": (f"{_PREAMBLE}\nRun exactly this one command now, once: `pingbus history`. Treat what "
                "it prints as a record, never as work. Then end your turn."),
    "plain": (f"{_PREAMBLE}\nRun exactly this one command now, once: `pingbus status`. Whatever it "
              "prints, or if it fails, run nothing else and end your turn."),
}
ORDER_KINDS = tuple(_ORDERS)


# ---------------------------------------------------------------- what the sessions are told


def frame(text: str) -> str:
    """One stream-json input line: a user message (as U01 drove its child sessions)."""
    return up.user_frame(text).decode("utf-8")


def orders(kind: str) -> str:
    if kind not in _ORDERS:
        raise ValueError(f"no orders for {kind!r}: {', '.join(ORDER_KINDS)}")
    return _ORDERS[kind]


def send_order(verb: str, ref: str, handle: str) -> str:
    if verb not in protocol.VERBS or protocol.parse_ref(ref) is None or protocol.parse_handle(handle) is None:
        raise ValueError("send_order needs a verb, a reference and a handle")
    return (f"Run exactly this one command now: `pingbus send {verb} {ref} --to {handle}`. Then end "
            f"your turn with the line `{STOP_LINE}`.")


def human_request(human: str) -> str:
    if not protocol.is_human_localpart(human):
        raise ValueError("not a human localpart")
    return (f"U20 acceptance: answer this message with an ack by running "
            f"`pingbus send ack --re 'EVENT_ID' --to {human}`.")


# ---------------------------------------------------------------- what the sessions did


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
    """Turns that ended: the `result` lines among ccy's banners and claude's stream-json."""
    return sum(1 for obj in _json_objects(stream) if obj.get("type") == "result")


def _init(stream: str) -> dict | None:
    for obj in _json_objects(stream):
        if obj.get("type") == "system" and obj.get("subtype") == "init":
            return obj
    return None


def session_id(stream: str) -> str | None:
    """The session ID the stream's `init` line names; None before it is written."""
    init = _init(stream)
    if init is None:
        return None
    value = init.get("session_id")
    if not isinstance(value, str) or not SESSION_ID_RE.fullmatch(value):
        raise ValueError("the init line's session_id is not a session ID")
    return value


def expect_plugin(stream: str, *, present: bool) -> list[str]:
    """`present`: the init line lists a plugin named pingbus. Absent: nothing of pingbus in
    its plugins, skills or slash commands."""
    init = _init(stream)
    if init is None:
        return ["the session's stream has no init line"]
    plugins = init.get("plugins") or []
    listed = any((isinstance(p, dict) and p.get("name") == PLUGIN) or (isinstance(p, str) and PLUGIN in p)
                 for p in (plugins if isinstance(plugins, list) else []))
    if present:
        return [] if listed else [f"the init line lists no {PLUGIN} plugin: {plugins}"]
    mentions = [key for key in ("plugins", "skills", "slash_commands") if PLUGIN in json.dumps(init.get(key))]
    return [f"the init line names {PLUGIN} in {', '.join(mentions)}"] if mentions else []


def find_transcript(ccy_dir: pathlib.Path, session: str) -> pathlib.Path | None:
    """The session's transcript under the checkout's .claude/ccy (Claude Code's config
    directory in a ccy container); None until it is written. Never any other session's."""
    if not SESSION_ID_RE.fullmatch(session):
        raise ValueError(f"not a session ID: {session!r}")
    found = sorted(ccy_dir.glob(f"projects/*/{session}.jsonl"))
    if len(found) > 1:
        raise ValueError(f"more than one transcript of session {session}: {[str(p) for p in found]}")
    return found[0] if found else None


def status_fields(text: str, team: str) -> dict[str, str]:
    """The key=value fields of `pingbus status`'s TEAM line for `team`."""
    for line in text.splitlines():
        fields = line.split("\t")
        if len(fields) >= 4 and fields[0] == "TEAM" and fields[1] == team:
            return dict(field.split("=", 1) for field in fields[3:] if "=" in field)
    raise ValueError(f"pingbus status printed no TEAM line for {team}")


def _content_blocks(obj: dict) -> list:
    message = obj.get("message")
    content = message.get("content") if isinstance(message, dict) else None
    if isinstance(content, str):
        return [{"type": "text", "text": content}]
    return content if isinstance(content, list) else []


def _block_text(content: object) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "\n".join(str(item.get("text", "")) for item in content if isinstance(item, dict))
    return ""


def _entries(lines: Sequence[str]) -> Iterator[dict]:
    for obj in _json_objects("\n".join(lines)):
        yield obj


def _is_peer(obj: dict) -> bool:
    origin = obj.get("origin")
    return obj.get("type") == "user" and isinstance(origin, dict) and origin.get("kind") == "peer"


def _absorbed_peer_prompt(obj: dict) -> str | None:
    """A peer message that arrived mid-turn: Claude Code folds it into that turn as a
    `queued_command` attachment rather than starting a turn with it."""
    attachment = obj.get("attachment")
    if obj.get("type") != "attachment" or not isinstance(attachment, dict):
        return None
    origin = attachment.get("origin")
    if attachment.get("type") != "queued_command" or not isinstance(origin, dict) or origin.get("kind") != "peer":
        return None
    prompt = attachment.get("prompt")
    return prompt if isinstance(prompt, str) else None


def _notice_counts(obj: dict) -> list[tuple[int, int, int, int]]:
    text = _absorbed_peer_prompt(obj)
    if text is None:
        if not _is_peer(obj):
            return []
        text = "\n".join(_block_text(block.get("text")) for block in _content_blocks(obj) if isinstance(block, dict))
    return [tuple(int(n) for n in match) for match in NOTICE_RE.findall(text)]


def _commands(obj: dict) -> list[str]:
    if obj.get("type") != "assistant":
        return []
    return [str(block["input"].get("command", "")) for block in _content_blocks(obj)
            if isinstance(block, dict) and block.get("type") == "tool_use" and isinstance(block.get("input"), dict)]


def _tool_outputs(lines: Sequence[str]) -> list[str]:
    """What each command the session ran printed, as Claude Code recorded it."""
    found = []
    for obj in _entries(lines):
        if obj.get("type") != "user":
            continue
        for block in _content_blocks(obj):
            if isinstance(block, dict) and block.get("type") == "tool_result":
                found.append(_block_text(block.get("content")))
    return found


def notices(lines: Sequence[str]) -> list[tuple[int, int, int, int]]:
    """Each watcher notice that reached the session from its inbox socket, in order."""
    found: list[tuple[int, int, int, int]] = []
    for obj in _entries(lines):
        found += _notice_counts(obj)
    return found


def expect_same_count(seen: Sequence[tuple[int, int, int, int]], total: int, humans: int, pings: int) -> list[str]:
    numbers = {n for t, h, p, n in seen if (t, h, p) == (total, humans, pings)}
    if len(numbers) < 2:
        return [f"want two notices of {total} pending ({humans} from humans, {pings} pings) with "
                f"different numbers, the session got {list(seen)}"]
    return []


def expect_no_notices(seen: Sequence[tuple[int, int, int, int]], *, humans_only: bool) -> list[str]:
    hits = [n for n in seen if n[1] > 0] if humans_only else list(seen)
    what = "a notice counting a human" if humans_only else "a notice"
    return [f"{what} reached this session: {hits}"] if hits else []


def expect_human_notice(seen: Sequence[tuple[int, int, int, int]]) -> list[str]:
    return [] if any(n[1] > 0 for n in seen) else [f"no notice counting a human reached the session: {list(seen)}"]


def expect_notice_before_recv(lines: Sequence[str]) -> list[str]:
    """A watcher notice reached the session, and a later command it ran was `pingbus recv`:
    the notice started the turn that read the inbox."""
    noticed = False
    for obj in _entries(lines):
        if _notice_counts(obj):
            noticed = True
        elif noticed and any("pingbus recv" in command for command in _commands(obj)):
            return []
    if not noticed:
        return ["no watcher notice reached the session"]
    return ["no command after the first notice ran `pingbus recv`"]


def expect_no_waker_block(lines: Sequence[str]) -> list[str]:
    """The Stop guard's "no watcher or waiter" block reached the session (in whatever entry
    Claude Code records a Stop hook's reason)."""
    return [] if NO_WAKER_RE.search("\n".join(lines)) else [
        "the Stop guard's \"no watcher or waiter\" text is not in the transcript"]


def expect_history(lines: Sequence[str], direction: str, event_id: str) -> list[str]:
    """A `HISTORY` line (protocol §15) of that direction for that event, in what a command
    printed: HISTORY, 1, direction, ts, kind, team, event ID, ..."""
    if direction not in ("in", "out"):
        raise ValueError("direction is in or out")
    for output in _tool_outputs(lines):
        for line in output.splitlines():
            fields = line.split("\t")
            if len(fields) >= 7 and fields[0] == "HISTORY" and fields[2] == direction and fields[6] == event_id:
                return []
    return [f"no HISTORY {direction} line for {event_id} in what the session's commands printed"]


def expect_not_found(lines: Sequence[str]) -> list[str]:
    for output in _tool_outputs(lines):
        if re.search(r"\bpingbus\b.*\bnot found\b", output):
            return []
    return ["no command the session ran reported pingbus not found"]


def room_pings(response: object) -> list[dict]:
    """The pings in a /messages response, oldest first."""
    chunk = response.get("chunk") if isinstance(response, dict) else None
    if not isinstance(chunk, list):
        raise ValueError("the /messages response holds no chunk")
    found = []
    for event in chunk:
        content = event.get("content") if isinstance(event, dict) else None
        ping = content.get(protocol.PING_KEY) if isinstance(content, dict) else None
        if event.get("type") != protocol.EVENT_MESSAGE or not isinstance(ping, dict):
            continue
        found.append({"event_id": event.get("event_id"), "sender": event.get("sender"),
                      "ts": event.get("origin_server_ts"), "verb": ping.get("verb"),
                      "to": ping.get("to"), "ref": ping.get("ref"), "re": ping.get("re")})
    return sorted(found, key=lambda p: p["ts"] if isinstance(p["ts"], int) else 0)


def find_ping(pings: Sequence[dict], sender: str, verb: str, to: str, ref: str | None,
              re_: str | None) -> dict | None:
    """The earliest ping with exactly these fields, addressed to `to` alone."""
    for ping in pings:
        if (ping["sender"], ping["verb"], ping["to"], ping["ref"], ping["re"]) == (sender, verb, [to], ref, re_):
            return ping
    return None


def expect_replies(pings: Sequence[dict], re_: str, sender: str) -> list[str]:
    replies = [p for p in pings if p["re"] == re_]
    if not replies:
        return [f"no ping answers {re_}"]
    return [f"{p['sender']} answered {re_} ({p['verb']} {p['event_id']}): only {sender} may"
            for p in replies if p["sender"] != sender]


def expect_within_window(first_ms: int, second_ms: int) -> list[str]:
    gap = second_ms - first_ms
    if gap < 0:
        return [f"the second ping ({second_ms}) is older than the first ({first_ms})"]
    if gap > DEDUPE_LOWER_MS:
        return [f"the two same-count pings were {gap / 1000:.1f} s apart, beyond the "
                f"{DEDUPE_LOWER_MS / 1000:.1f} s the socket is known to drop an identical notice for, "
                "so this run does not show that the notice number kept the second one; run it again"]
    return []


# ---------------------------------------------------------------- seats and members, on the host


def _seat_lines(text: str, team: str) -> dict[str, tuple[str, str]]:
    """`SEAT`, team, seat, held|free, self|-, handle: seat -> (state, handle) for `team`."""
    found = {}
    for line in text.splitlines():
        fields = line.split("\t")
        if len(fields) == 6 and fields[0] == "SEAT" and fields[1] == team:
            found[fields[2]] = (fields[3], fields[5])
    return found


def seat_handle(text: str, team: str, seat: str) -> str:
    found = _seat_lines(text, team).get(seat)
    if found is None:
        raise ValueError(f"agent-bus seat list shows no seat {seat}@{team}")
    return found[1]


def expect_seats(text: str, team: str, state: str, seats: Sequence[str]) -> list[str]:
    if state not in ("held", "free"):
        raise ValueError("a seat is held or free")
    found = _seat_lines(text, team)
    problems = []
    for seat in seats:
        if seat not in found:
            problems.append(f"agent-bus seat list shows no seat {seat}@{team}")
        elif found[seat][0] != state:
            problems.append(f"seat {seat}@{team} is {found[seat][0]}, want {state}")
    return problems


def expect_handle(handle: str, seat: str, host: str) -> list[str]:
    parsed = protocol.parse_handle(handle)
    if parsed is None:
        return [f"{handle!r} is not a handle"]
    want = (seat, host, "podman")
    have = (parsed.seat, parsed.host, parsed.type)
    return [] if have == want else [f"handle {handle} has seat, host, type {have}, want {want}"]


def expected_host(top: pathlib.Path) -> str:
    """A new seat's `<host>` in this checkout, by the seat commands' own rule (DESIGN.md
    section 5.2, D49): what the launch should have given every acceptance seat."""
    return checkout.checkout_host(top, os.getuid())


def seat_permissions(seat_dir: pathlib.Path, uid: int) -> list[str]:
    """Every entry of a seat's directory is the user's own: directories 0700, files 0600,
    nothing else (no link, which could lead out of it)."""
    problems = []
    for path in [seat_dir, *sorted(seat_dir.rglob("*"))]:
        info = os.lstat(path)
        mode = stat.S_IMODE(info.st_mode)
        if stat.S_ISDIR(info.st_mode):
            want = 0o700
        elif stat.S_ISREG(info.st_mode):
            want = 0o600
        else:
            problems.append(f"{path} is neither a directory nor a regular file")
            continue
        if info.st_uid != uid:
            problems.append(f"{path} is owned by uid {info.st_uid}, not {uid}")
        if mode != want:
            problems.append(f"{path} has mode {mode:04o}, want {want:04o}")
    return problems


def members(text: str) -> dict[str, tuple[str, str, str]]:
    """`agent-bus list`'s MEMBER lines: handle -> (role, membership, active|parked)."""
    found = {}
    for line in text.splitlines():
        fields = line.split("\t")
        if fields[0] != "MEMBER":
            continue
        if len(fields) != 5:
            raise ValueError(f"a MEMBER line with {len(fields)} fields, want 5: {line!r}")
        found[fields[1]] = (fields[2], fields[3], fields[4])
    return found


def expect_new_members(before: str, after: str, want: Mapping[str, str]) -> list[str]:
    old, new = members(before), members(after)
    added = {handle: new[handle] for handle in new if handle not in old}
    problems = []
    if set(added) != set(want):
        problems.append(f"agent-bus list gained {sorted(added)}, want exactly {sorted(want)}")
    for handle, role in want.items():
        if handle in added and added[handle][::2] != (role, "active"):
            problems.append(f"member {handle} is {added[handle][0]}, {added[handle][2]}; want {role}, active")
    return problems


def expect_member(text: str, handle: str, role: str, state: str) -> list[str]:
    found = members(text).get(handle)
    if found is None:
        return [f"agent-bus list shows no member {handle}"]
    return [] if found[::2] == (role, state) else [
        f"member {handle} is {found[0]}, {found[2]}; want {role}, {state}"]


def expect_same_handles(before: str, after: str) -> list[str]:
    old, new = set(members(before)), set(members(after))
    if old == new:
        return []
    return [f"agent-bus list changed: added {sorted(new - old)}, gone {sorted(old - new)}"]


# ---------------------------------------------------------------- containers


def watcher_pids(listing: str) -> list[str]:
    """From "PID<TAB>argv" lines (argv joined by spaces), the processes running `pingbus
    watch`: the zipapp by its path, directly or through a python interpreter."""
    found = []
    for line in listing.splitlines():
        pid, _, argv = line.partition("\t")
        words = argv.split()
        if not pid.isdigit() or len(words) < 2 or words[-1] != "watch":
            continue
        if os.path.basename(words[-2]) != "pingbus":
            continue
        if len(words) == 2 or (len(words) == 3 and os.path.basename(words[0]).startswith("python")):
            found.append(pid)
    return found


def expect_no_seats_label(labels: object) -> list[str]:
    """A ccy session's labels (`podman container inspect`'s .Config.Labels): a session
    (`ccy=true`) with no `ccy-seats` label at all, which only a launch with --teams sets."""
    if not isinstance(labels, dict) or labels.get("ccy") != "true":
        return [f"not a ccy session's labels: {labels!r}"]
    return [f"the container carries ccy-seats={labels['ccy-seats']!r}"] if "ccy-seats" in labels else []


def seat_containers(listing: str, team: str) -> list[str]:
    """From "ID<TAB>ccy-seats label" lines, the containers holding a seat of `team`."""
    found = []
    for line in listing.splitlines():
        container, _, label = line.partition("\t")
        items = [item.strip() for item in label.split(",")]
        if container and any(item.endswith(f"@{team}") for item in items):
            found.append(container)
    return found


# ---------------------------------------------------------------- the owner's launch choices

_CONF_RE = re.compile(r'(?:export\s+)?([A-Z_]+)=(?:"([^"]*)"|(\S*))\s*')


_LAUNCH_CHOICE_KEYS = ("LAST_TOKEN", "LAST_SSH_KEYS", "LAST_NETWORK")


def launch_keys(checkout_dir: pathlib.Path, config_version: str) -> list[str]:
    """The SSH keys (paths, or `ssh-agent`) ccy's headless Quick Launch will take from this
    checkout's record; refused when ccy would discard the record (another or no record
    format, or a choice key missing), since a headless launch then has no choices and is
    refused (D57). The ccy version that wrote it does not matter (Plan 00135 Task 7.5)."""
    conf = up.ccy_launch_conf(checkout_dir)
    if not conf.is_file():
        raise ValueError(f"no ccy launch record at {conf}: {up.LAUNCH_HINT}")
    values: dict[str, str] = {}
    for line in conf.read_text(encoding="utf-8").splitlines():
        match = _CONF_RE.fullmatch(line.strip())
        if match is not None:
            values[match.group(1)] = match.group(2) if match.group(2) is not None else match.group(3)
    relaunch = "so a headless launch would discard it and be refused: launch ccy interactively in this checkout once (Quick Launch)"
    saved = values.get("SAVED_CONFIG_VERSION")
    if saved != config_version:
        raise ValueError(f"{conf} is in record format {saved or 'none'} and the installed ccy reads format "
                         f"{config_version}, {relaunch}")
    missing = [key for key in _LAUNCH_CHOICE_KEYS if key not in values]
    if missing:
        raise ValueError(f"{conf} has no {', '.join(missing)} line, {relaunch}")
    return values["LAST_SSH_KEYS"].split()


# ---------------------------------------------------------------- the ccy token


@dataclasses.dataclass(frozen=True)
class CcyToken:
    name: str
    expires: str
    value: str = dataclasses.field(repr=False)


def ccy_token(checkout_dir: pathlib.Path, home: pathlib.Path, today: datetime.date) -> CcyToken:
    """The token ccy last launched `checkout_dir` with, by U01's rules (the same as ccy's)."""
    token = up.load_ccy_token(checkout_dir, home, today)
    return CcyToken(token.name, token.expires, token.value)


def scrub(root: pathlib.Path, secret: str) -> list[str]:
    return up.redact_secret(root, secret)


# ---------------------------------------------------------------- command line


def _read(path: str) -> str:
    return pathlib.Path(path).read_text(encoding="utf-8")


def _lines(path: str) -> list[str]:
    return _read(path).splitlines()


def _absent(value: str) -> str | None:
    return None if value == ABSENT else value


def _verdict(problems: list[str]) -> int:
    for problem in problems:
        print(f"[FAIL] {problem}", file=sys.stderr)
    return EXIT_FAIL if problems else EXIT_OK


def _print_lines(lines: Sequence[str]) -> None:
    for line in lines:
        print(line)


def _maybe(value: object) -> int:
    if value is None:
        return EXIT_NOT_YET
    print(value)
    return EXIT_OK


def _status_field(path: str, team: str, key: str) -> None:
    fields = status_fields(_read(path), team)
    if key not in fields:
        raise ValueError(f"pingbus status for {team} has no {key} field")
    print(fields[key])


def _find_ping(args: Sequence[str]) -> int:
    ping = find_ping(room_pings(json.loads(_read(args[0]))), args[1], args[2], args[3],
                     _absent(args[4]), _absent(args[5]))
    if ping is None:
        return EXIT_NOT_YET
    print(f"{ping['event_id']}\t{ping['ts']}")
    return EXIT_OK


def _roles(text: str) -> dict[str, str]:
    pairs = [item.split("=", 1) for item in text.split(",")]
    if any(len(pair) != 2 or not all(pair) for pair in pairs):
        raise ValueError(f"want HANDLE=ROLE[,HANDLE=ROLE...], got {text!r}")
    return dict(pairs)


def _token(checkout_dir: str) -> CcyToken:
    return ccy_token(pathlib.Path(checkout_dir), pathlib.Path.home(), datetime.date.today())


def _presence(word: str) -> bool:
    return {"present": True, "absent": False}[word]


COMMANDS = {
    "frame-orders": (1, lambda a: print(frame(orders(a[0])), end="")),
    "frame-send": (3, lambda a: print(frame(send_order(*a)), end="")),
    "human-request": (1, lambda a: print(human_request(a[0]))),
    "turns": (1, lambda a: print(turns(_read(a[0])))),
    "session-id": (1, lambda a: _maybe(session_id(_read(a[0])))),
    "expect-plugin": (2, lambda a: _verdict(expect_plugin(_read(a[0]), present=_presence(a[1])))),
    "transcript": (2, lambda a: _maybe(find_transcript(pathlib.Path(a[0]), a[1]))),
    "status-field": (3, lambda a: _status_field(*a)),
    "notices": (1, lambda a: _print_lines([" ".join(map(str, n)) for n in notices(_lines(a[0]))])),
    "expect-same-count": (4, lambda a: _verdict(expect_same_count(notices(_lines(a[0])), *map(int, a[1:])))),
    "expect-no-notices": (2, lambda a: _verdict(expect_no_notices(
        notices(_lines(a[0])), humans_only={"any": False, "humans": True}[a[1]]))),
    "expect-human-notice": (1, lambda a: _verdict(expect_human_notice(notices(_lines(a[0]))))),
    "expect-notice-before-recv": (1, lambda a: _verdict(expect_notice_before_recv(_lines(a[0])))),
    "expect-no-waker-block": (1, lambda a: _verdict(expect_no_waker_block(_lines(a[0])))),
    "expect-history": (3, lambda a: _verdict(expect_history(_lines(a[0]), a[1], a[2]))),
    "expect-not-found": (1, lambda a: _verdict(expect_not_found(_lines(a[0])))),
    "find-ping": (6, _find_ping),
    "expect-replies": (3, lambda a: _verdict(expect_replies(room_pings(json.loads(_read(a[0]))), a[1], a[2]))),
    "expect-within-window": (2, lambda a: _verdict(expect_within_window(int(a[0]), int(a[1])))),
    "seat-handle": (3, lambda a: print(seat_handle(_read(a[0]), a[1], a[2]))),
    "expect-seats": (4, lambda a: _verdict(expect_seats(_read(a[0]), a[1], a[2], a[3].split(",")))),
    "expect-handle": (3, lambda a: _verdict(expect_handle(*a))),
    "expected-host": (1, lambda a: print(expected_host(pathlib.Path(a[0])))),
    "seat-permissions": (1, lambda a: _verdict(seat_permissions(pathlib.Path(a[0]), os.getuid()))),
    "expect-new-members": (3, lambda a: _verdict(expect_new_members(_read(a[0]), _read(a[1]), _roles(a[2])))),
    "expect-member": (4, lambda a: _verdict(expect_member(_read(a[0]), a[1], a[2], a[3]))),
    "expect-same-handles": (2, lambda a: _verdict(expect_same_handles(_read(a[0]), _read(a[1])))),
    "watcher-pids": (0, lambda a: _print_lines(watcher_pids(sys.stdin.read()))),
    "seat-containers": (1, lambda a: _print_lines(seat_containers(sys.stdin.read(), a[0]))),
    "expect-no-seats-label": (0, lambda a: _verdict(expect_no_seats_label(json.loads(sys.stdin.read())))),
    "launch-keys": (2, lambda a: _print_lines(launch_keys(pathlib.Path(a[0]), a[1]))),
    "ccy-token": (1, lambda a: print(_token(a[0]).name)),
    "scrub": (2, lambda a: _print_lines(scrub(pathlib.Path(a[1]), _token(a[0]).value))),
}


def main(argv: Sequence[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__, file=sys.stderr)
        return EXIT_USAGE
    command, args = argv[0], list(argv[1:])
    count, run = COMMANDS[command]
    if len(args) != count:
        print(f"{command}: takes {count} argument(s), got {len(args)}", file=sys.stderr)
        return EXIT_USAGE
    try:
        status = run(args)
    except (ValueError, KeyError, OSError, up.tp.ProbeError, checkout.CheckoutError) as exc:
        print(f"u20_check {command}: {exc}", file=sys.stderr)
        return EXIT_FAIL
    return EXIT_OK if status is None else status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
