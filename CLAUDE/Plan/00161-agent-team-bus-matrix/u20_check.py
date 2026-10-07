#!/usr/bin/env python3
"""Plan 00161 unit U20: the pure logic behind acceptance.bash's M2 slice (ccy members).

_acceptance-u20.inc.bash drives the host (ccy sessions, `pingbus status`, the human's
`curl`); this module builds what it writes to the sessions and judges what comes back. Every
judgement rests on a fact the bus or Claude Code recorded, never on the model's prose: the
room's ping events (as the human reads them), a member's `pingbus status`, the turns in a
session's stream output, and the watcher's fixed-template notices in a session's transcript.
Commands (stdout is the payload; every reason goes to stderr):

  frame-orders socket|wait           the standing orders, as one stream-json user line
  frame-send VERB REF HANDLE         an order to run one `pingbus send`, as one line
  human-request HUMAN                the text of the human's addressed message
  turns STREAM                       how many turns ended (`result` lines) in a session's stdout
  status-field STATUS_OUT TEAM KEY   one key=value field of `pingbus status`'s TEAM line
  notices STATE_DIR                  each watcher notice in the transcripts under a ccy
                                     checkout's .claude/ccy: "TOTAL HUMANS PINGS NUMBER"
  expect-same-count STATE_DIR TOTAL HUMANS PINGS   two notices with those counts, two numbers
  expect-no-notices STATE_DIR any|humans           no notice (any), or none counting a human
  expect-human-notice STATE_DIR                    a notice counting a human
  find-ping MESSAGES SENDER VERB TO REF RE         the earliest such ping in a /messages
                                     response: "EVENT_ID<TAB>ORIGIN_SERVER_TS"; exit 3 if none
  expect-replies MESSAGES RE SENDER  pings answering RE exist, and every one is from SENDER
  expect-within-window TS1 TS2       TS2 - TS1 (ms) is inside the socket's identical-notice
                                     window as U01 measured its lower bound
  ccy-token CHECKOUT                 the name of the ccy token ccy last launched CHECKOUT
                                     with, refused when ccy would refuse it (never its value)
  scrub CHECKOUT DIR                 replace that token's value in every file under DIR;
                                     prints the files that held it

SENDER and TO are full user IDs; REF and RE are "-" when the ping must not carry them.
Exit codes: 0 ok; 1 an expectation failed or input was malformed; 3 (find-ping) not there
yet; 64 usage.
"""

from __future__ import annotations

import dataclasses
import datetime
import importlib
import json
import pathlib
import re
import sys
from collections.abc import Sequence

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
notify = importlib.import_module("helpers.pingbus.notify")
protocol = importlib.import_module("helpers.pingbus.protocol")
up = importlib.import_module("u01_probe")

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_NOT_YET = 3
EXIT_USAGE = 64
ABSENT = "-"

#: U01 (journal 26-10-07): the inbox socket drops a body identical to the same sender's
#: previous one for longer than 20 s (and at most 33 s). Two same-count notices closer than
#: this would have been one without the notice number.
DEDUPE_LOWER_MS = 20_000

#: notify.NOTICE_TEMPLATE, with each count captured.
NOTICE_RE = re.compile(
    re.escape(notify.NOTICE_TEMPLATE)
    .replace(re.escape("{total}"), r"(\d+)")
    .replace(re.escape("{humans}"), r"(\d+)")
    .replace(re.escape("{pings}"), r"(\d+)")
    .replace(re.escape("{number}"), r"(\d+)")
)

_PREAMBLE = (
    "This session is a member of an automated acceptance test of the agent team bus "
    "(fedora-desktop Plan 00161, unit U20). For the rest of this session follow these rules "
    "exactly and do nothing else: read no file, change nothing, and run no command but the "
    "pingbus commands named here."
)
_RULES = (
    "1. For each PING line that pingbus prints with the verb review, "
    "run `pingbus send ack --re EVENT_ID --to SENDER` with that line's EVENT_ID and SENDER fields.\n"
    "2. For each HUMAN line whose text asks you to run one pingbus command, run exactly that "
    "command, with EVENT_ID replaced by that HUMAN line's EVENT_ID field.\n"
    "3. Ignore every other line. When told to run a pingbus command, run exactly that command. "
    "Then end your turn: never poll, loop or sleep."
)
_ORDERS = {
    "socket": (
        f"{_PREAMBLE}\n0. Whenever you are told that agent-bus items are pending, run `pingbus recv` "
        f"once.\n{_RULES}\nReply now with the single word READY."
    ),
    "wait": (
        f"{_PREAMBLE}\n0. This session has no inbox socket. Now, and every time it ends, start "
        "`pingbus wait` with run_in_background; when it ends, apply the rules below to the lines it "
        f"printed, then start it again.\n{_RULES}\nStart the waiter now, then reply with the single "
        "word READY."
    ),
}


# ---------------------------------------------------------------- what the sessions are told


def frame(text: str) -> str:
    """One stream-json input line: a user message (as U01 drove its child sessions)."""
    return up.user_frame(text).decode("utf-8")


def orders(kind: str) -> str:
    if kind not in _ORDERS:
        raise ValueError(f"no orders for {kind!r}: socket or wait")
    return _ORDERS[kind]


def send_order(verb: str, ref: str, handle: str) -> str:
    if verb not in protocol.VERBS or protocol.parse_ref(ref) is None or protocol.parse_handle(handle) is None:
        raise ValueError("send_order needs a verb, a reference and a handle")
    return f"Run exactly this one command now, then end your turn: `pingbus send {verb} {ref} --to {handle}`"


def human_request(human: str) -> str:
    if not protocol.is_human_localpart(human):
        raise ValueError("not a human localpart")
    return (f"U20 acceptance: answer this message with an ack by running "
            f"`pingbus send ack --re EVENT_ID --to {human}`.")


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


def status_fields(text: str, team: str) -> dict[str, str]:
    """The key=value fields of `pingbus status`'s TEAM line for `team`."""
    for line in text.splitlines():
        fields = line.split("\t")
        if len(fields) >= 4 and fields[0] == "TEAM" and fields[1] == team:
            return dict(field.split("=", 1) for field in fields[3:] if "=" in field)
    raise ValueError(f"pingbus status printed no TEAM line for {team}")


def transcript_lines(state_dir: pathlib.Path) -> list[str]:
    """Every line of the session transcripts in a ccy checkout's .claude/ccy (none yet: [])."""
    lines: list[str] = []
    for path in sorted(state_dir.glob("projects/*/*.jsonl")):
        lines += path.read_text(encoding="utf-8", errors="replace").splitlines()
    return lines


def notices(lines: Sequence[str]) -> list[tuple[int, int, int, int]]:
    """Each watcher notice that reached the session from its inbox socket, in order."""
    found = []
    for entry in up.peer_entries(list(lines)):
        found += [tuple(int(n) for n in match) for match in NOTICE_RE.findall(entry)]
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


# ---------------------------------------------------------------- the ccy token


@dataclasses.dataclass(frozen=True)
class CcyToken:
    name: str
    expires: str
    value: str = dataclasses.field(repr=False)


def ccy_token(checkout: pathlib.Path, home: pathlib.Path, today: datetime.date) -> CcyToken:
    """The token ccy last launched `checkout` with, by U01's rules (the same as ccy's)."""
    token = up.load_ccy_token(checkout, home, today)
    return CcyToken(token.name, token.expires, token.value)


def scrub(root: pathlib.Path, secret: str) -> list[str]:
    return up.redact_secret(root, secret)


# ---------------------------------------------------------------- command line


def _read(path: str) -> str:
    return pathlib.Path(path).read_text(encoding="utf-8")


def _absent(value: str) -> str | None:
    return None if value == ABSENT else value


def _verdict(problems: list[str]) -> int:
    for problem in problems:
        print(f"[FAIL] {problem}", file=sys.stderr)
    return EXIT_FAIL if problems else EXIT_OK


def _notices_of(state_dir: str) -> list[tuple[int, int, int, int]]:
    return notices(transcript_lines(pathlib.Path(state_dir)))


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


def _token(checkout: str) -> CcyToken:
    return ccy_token(pathlib.Path(checkout), pathlib.Path.home(), datetime.date.today())


def _print_lines(lines: Sequence[str]) -> None:
    for line in lines:
        print(line)


COMMANDS = {
    "frame-orders": (1, lambda a: print(frame(orders(a[0])), end="")),
    "frame-send": (3, lambda a: print(frame(send_order(*a)), end="")),
    "human-request": (1, lambda a: print(human_request(a[0]))),
    "turns": (1, lambda a: print(turns(_read(a[0])))),
    "status-field": (3, lambda a: _status_field(*a)),
    "notices": (1, lambda a: _print_lines([" ".join(map(str, n)) for n in _notices_of(a[0])])),
    "expect-same-count": (4, lambda a: _verdict(expect_same_count(_notices_of(a[0]), *map(int, a[1:])))),
    "expect-no-notices": (2, lambda a: _verdict(expect_no_notices(
        _notices_of(a[0]), humans_only={"any": False, "humans": True}[a[1]]))),
    "expect-human-notice": (1, lambda a: _verdict(expect_human_notice(_notices_of(a[0])))),
    "find-ping": (6, _find_ping),
    "expect-replies": (3, lambda a: _verdict(expect_replies(room_pings(json.loads(_read(a[0]))), a[1], a[2]))),
    "expect-within-window": (2, lambda a: _verdict(expect_within_window(int(a[0]), int(a[1])))),
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
    except (ValueError, KeyError, OSError, up.tp.ProbeError) as exc:
        print(f"u20_check {command}: {exc}", file=sys.stderr)
        return EXIT_FAIL
    return EXIT_OK if status is None else status


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
