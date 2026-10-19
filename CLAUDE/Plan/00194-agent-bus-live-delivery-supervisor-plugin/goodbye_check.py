"""goodbye_check.py — the judgements of Plan 00194's goodbye acceptance (acceptance.bash).

The run reads each throwaway team's room as its human reads it in Element: the client API's
/messages pages, newest first. From those it counts what one seat's member posted since a
scenario started, and judges the count. Stdlib only; every command reads files or arguments
and prints a value or a verdict, so test_goodbye_check.py covers it without a homeserver.

    handle <user_id>                              the seat's handle (the user ID's localpart)
    tally <sender> <handle> <since-ms> <page>...  the seat's texts since <since-ms>, as JSON:
                                                  {"goodbye": [...], "disconnected": [...], "other": [...]}
    expect <tally> <handle> <goodbyes> <disconnects> <reason>
                                                  exactly that many of each, in the exact wording
                                                  (the goodbye with <reason>), and never both
    next-from <page>                              the next page's `from` token; nothing at the end
    last-token <last-launch.conf>                 the token name a Quick Launch saved
    pane-clean <pane.log>                         no traceback, no failed disconnect notice
    fake-page <end|''> [<ts> <sender> <text>]...  a fixture page for `acceptance.bash --fake`

Exit 0 OK, 1 FAIL (the reason on stderr), 64 usage.
"""

from __future__ import annotations

import json
import re
import sys
from collections.abc import Callable, Sequence

EXIT_OK = 0
EXIT_FAIL = 1
EXIT_USAGE = 64

#: The pingbus content key of an agent's text (helpers/pingbus/protocol.py TEXT_KEY).
TEXT_KEY = "agent_bus.text"
#: The session-end goodbye (helpers/pingbus/hooks.py, template `goodbye`).
GOODBYE_TEXT = "{handle} left the team: session ended ({reason})"
#: What the host says of a session that ended without one (helpers/pingbus/cli.py).
DISCONNECT_TEXT = "{handle} disconnected: session ended without a goodbye"
#: What the launcher prints when its disconnect notice fails (lib/agent-bus-membership.bash),
#: and the first line of a Python traceback.
PANE_FAILURES = (
    "Traceback (most recent call last)",
    "pingbus seat disconnected failed",
    "pingbus seat disconnected was stopped",
)
_USER_ID = re.compile(r"@([^:@\s]+):(\S+)")
_TOKEN_LINE = re.compile(r'LAST_TOKEN=(?:"([^"]*)"|(\S*))')
_TOKEN_NAME = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]*")


class Failed(Exception):
    """A verdict: the room, the pane or the saved launch is not as the acceptance requires."""


def handle_of(user_id: str) -> str:
    match = _USER_ID.fullmatch(user_id)
    if match is None:
        raise ValueError(f"{user_id!r} is not a Matrix user ID")
    return match.group(1)


def _chunk(page: object) -> list[object]:
    chunk = page.get("chunk") if isinstance(page, dict) else None
    if not isinstance(chunk, list):
        raise ValueError("a /messages page has no chunk list")
    return chunk


def seat_texts(pages: Sequence[object], sender: str, since_ms: int) -> list[str]:
    """The agent texts `sender` posted at or after `since_ms`, oldest first. Pages come newest
    first (dir=b), so the order is reversed."""
    texts: list[tuple[int, str]] = []
    for page in pages:
        for item in _chunk(page):
            if not isinstance(item, dict) or item.get("type") != "m.room.message":
                continue
            if item.get("sender") != sender:
                continue
            ts = item.get("origin_server_ts")
            if type(ts) is not int or ts < since_ms:
                continue
            content = item.get("content")
            obj = content.get(TEXT_KEY) if isinstance(content, dict) else None
            text = obj.get("text") if isinstance(obj, dict) else None
            if isinstance(text, str):
                texts.append((ts, text))
    return [text for _, text in sorted(texts, key=lambda pair: pair[0])]


def tally(texts: Sequence[str], handle: str) -> dict[str, list[str]]:
    result: dict[str, list[str]] = {"goodbye": [], "disconnected": [], "other": []}
    for text in texts:
        if text.startswith(f"{handle} left the team: "):
            result["goodbye"].append(text)
        elif text.startswith(f"{handle} disconnected: "):
            result["disconnected"].append(text)
        else:
            result["other"].append(text)
    return result


def expect_tally(found: dict[str, list[str]], handle: str, goodbyes: int, disconnects: int,
                 reason: str) -> None:
    said, gone = found.get("goodbye", []), found.get("disconnected", [])
    if said and gone:
        raise Failed(f"{handle} posted both a goodbye and a disconnected: {said + gone!r}")
    if len(said) != goodbyes:
        raise Failed(f"{handle} posted {len(said)} goodbye(s), want {goodbyes}: {said!r}")
    if len(gone) != disconnects:
        raise Failed(f"{handle} posted {len(gone)} disconnected notice(s), want {disconnects}: {gone!r}")
    want_goodbye = GOODBYE_TEXT.format(handle=handle, reason=reason)
    for text in said:
        if text != want_goodbye:
            raise Failed(f"the goodbye reads {text!r}, want {want_goodbye!r} (another reason?)")
    want_gone = DISCONNECT_TEXT.format(handle=handle)
    for text in gone:
        if text != want_gone:
            raise Failed(f"the disconnected notice reads {text!r}, want {want_gone!r} (a new wording?)")


def next_from(page: object) -> str | None:
    if not _chunk(page):
        return None
    end = page.get("end") if isinstance(page, dict) else None
    if not isinstance(end, str) or not end or end == page.get("start"):
        return None
    return end


def last_token(text: str) -> str:
    name = None
    for line in text.splitlines():
        match = _TOKEN_LINE.fullmatch(line.strip())
        if match is not None:
            name = match.group(1) if match.group(1) is not None else match.group(2)
    if not name:
        raise Failed("the saved Quick Launch choices name no token")
    if _TOKEN_NAME.fullmatch(name) is None:
        raise Failed(f"the saved token name {name!r} is not a token name")
    return name


def pane_clean(text: str) -> None:
    for failure in PANE_FAILURES:
        if failure in text:
            raise Failed(f"the session's terminal shows {failure!r}")


def fake_page(end: str, triples: Sequence[str]) -> dict:
    if len(triples) % 3:
        raise UsageError("fake-page takes <ts> <sender> <text> triples")
    events = []
    for i in range(0, len(triples), 3):
        ts, sender, text = int(triples[i]), triples[i + 1], triples[i + 2]
        events.append({
            "type": "m.room.message", "sender": sender, "origin_server_ts": ts, "event_id": f"$fake{ts}",
            "content": {"msgtype": "m.notice", "body": f"[agent-bus] text -> @tester\n{text}",
                        TEXT_KEY: {"v": 1, "to": ["@tester"], "text": text}},
        })
    page: dict = {"chunk": sorted(events, key=lambda e: -e["origin_server_ts"]), "start": "s0"}
    if end:
        page["end"] = end
    return page


class UsageError(Exception):
    pass


def _read(path: str) -> str:
    with open(path, encoding="utf-8") as handle:
        return handle.read()


def _json(path: str) -> object:
    return json.loads(_read(path))


def _count(value: str) -> int:
    if not value.isdigit():
        raise UsageError(f"{value!r} is not a count")
    return int(value)


def _print(value: str | None) -> None:
    if value is not None:
        print(value)


def _tally(args: Sequence[str]) -> None:
    pages = [_json(path) for path in args[3:]]
    print(json.dumps(tally(seat_texts(pages, args[0], _count(args[2])), args[1]), indent=2))


def _expect(args: Sequence[str]) -> None:
    goodbyes, disconnects = _count(args[2]), _count(args[3])
    found = _json(args[0])
    if not isinstance(found, dict):
        raise ValueError(f"{args[0]} is not a tally")
    expect_tally(found, args[1], goodbyes, disconnects, args[4])


#: name: (argument count, or -N for at least N; the command)
COMMANDS: dict[str, tuple[int, Callable[[Sequence[str]], None]]] = {
    "handle": (1, lambda a: _print(handle_of(a[0]))),
    "tally": (-4, _tally),
    "expect": (5, _expect),
    "next-from": (1, lambda a: _print(next_from(_json(a[0])))),
    "last-token": (1, lambda a: _print(last_token(_read(a[0])))),
    "pane-clean": (1, lambda a: pane_clean(_read(a[0]))),
    "fake-page": (-1, lambda a: print(json.dumps(fake_page(a[0], a[1:])))),
}


def main(argv: Sequence[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__, file=sys.stderr)
        return EXIT_USAGE
    command, args = argv[0], list(argv[1:])
    count, run = COMMANDS[command]
    if (count >= 0 and len(args) != count) or (count < 0 and len(args) < -count):
        print(f"goodbye_check {command}: wrong number of arguments ({len(args)})", file=sys.stderr)
        return EXIT_USAGE
    try:
        run(args)
    except UsageError as exc:
        print(f"goodbye_check {command}: {exc}", file=sys.stderr)
        return EXIT_USAGE
    except (Failed, ValueError, OSError) as exc:
        print(f"goodbye_check {command}: {exc}", file=sys.stderr)
        return EXIT_FAIL
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
