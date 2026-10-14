"""The sent log and the `SENT` echo: what this session sent, shown to the human.

Spec: Plan 00194 DESIGN.md section 2.8 and decision E; docs/agent-bus-protocol.md §12
(`PINGBUS_HOME/sent.jsonl`) and §15.

In relay mode `send` and `say` append one JSON line per send to `PINGBUS_HOME/sent.jsonl`,
after the homeserver answered with an event ID (a refused send is never recorded). The
PostToolUse and Stop hooks read the lines past a saved offset and return them as the
hook's `systemMessage`, in an envelope the sender's text cannot close or imitate: the text
is `json.dumps(..., ensure_ascii=True)`, one quoted line, under a nonce chosen after the
text was written. The log is the member's own text from a container-private file, so the
echo opens no new injection path; every line is still validated on the way back out.
"""

from __future__ import annotations

import datetime
import json
import os
import pathlib
from collections.abc import Mapping, Sequence

from helpers.pingbus import inbox, protocol, relay

SENT_LOG = "sent.jsonl"
OFFSET_FILE = "sent-offset"
VERSION = 1

#: At most this many entries, and this many bytes, in one echo; the rest follows next time.
ECHO_MAX_ENTRIES = 8
ECHO_MAX_BYTES = 12 * 1024
READ_MAX_BYTES = 1 << 20
TO_MAX = 20

SAY_KEYS = frozenset({"v", "team", "id", "ms", "kind", "to", "text"})
PING_KEYS = frozenset({"v", "team", "id", "ms", "kind", "to", "verb", "ref", "re"})
HEADER = "agent-bus SENT {nonce} — recorded by pingbus from the homeserver's answer."


def say_entry(team: str, event_id: str, ms: int, to: Sequence[str], text: str) -> dict:
    return {"v": VERSION, "team": team, "id": event_id, "ms": ms, "kind": "say",
            "to": list(to), "text": text}


def ping_entry(team: str, event_id: str, ms: int, to: Sequence[str], verb: str,
               ref: str | None, re_: str | None) -> dict:
    return {"v": VERSION, "team": team, "id": event_id, "ms": ms, "kind": "ping",
            "to": list(to), "verb": verb, "ref": ref, "re": re_}


def _localpart_ok(value: object) -> bool:
    return isinstance(value, str) and (
        protocol.parse_handle(value) is not None or protocol.is_human_localpart(value))


def validate(entry: object) -> None:
    """`ValueError` unless `entry` is exactly a sent-log entry whose every field passed
    its grammar."""
    if not isinstance(entry, dict):
        raise ValueError("not an object")
    kind = entry.get("kind")
    if set(entry) != (SAY_KEYS if kind == "say" else PING_KEYS if kind == "ping" else None):
        raise ValueError("unexpected keys")
    if entry["v"] != VERSION:
        raise ValueError("unknown version")
    if not protocol.is_team_name(entry["team"]) or not protocol.is_event_id(entry["id"]):
        raise ValueError("team or event ID")
    ms = entry["ms"]
    if type(ms) is not int or ms < 0:
        raise ValueError("time")
    to = entry["to"]
    if not isinstance(to, list) or not 1 <= len(to) <= TO_MAX or not all(_localpart_ok(n) for n in to):
        raise ValueError("addressees")
    if kind == "say":
        if not isinstance(entry["text"], str):
            raise ValueError("text")
        return
    if entry["verb"] not in protocol.VERBS:
        raise ValueError("verb")
    if entry["ref"] is not None and protocol.parse_ref(entry["ref"]) is None:
        raise ValueError("reference")
    if entry["re"] is not None and not protocol.is_event_id(entry["re"]):
        raise ValueError("re")


def append(home: pathlib.Path, entry: dict) -> None:
    """Record one send. Refused (`ValueError`) unless the entry is valid."""
    validate(entry)
    data = json.dumps(entry, ensure_ascii=True, separators=(",", ":")).encode("ascii") + b"\n"
    fd = os.open(pathlib.Path(home) / SENT_LOG,
                 os.O_WRONLY | os.O_APPEND | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, relay.FILE_MODE)
    try:
        os.write(fd, data)
        os.fsync(fd)
    finally:
        os.close(fd)


# ── reading back ─────────────────────────────────────────────────────────────────────────


def _offset_path(home: pathlib.Path) -> pathlib.Path:
    return pathlib.Path(home) / relay.RELAY_DIR / OFFSET_FILE


def _load_offset(home: pathlib.Path) -> int:
    data = inbox.read_json_file(_offset_path(home))
    if data is None:
        return 0
    if (not isinstance(data, dict) or set(data) != {"v", "offset"} or data["v"] != 1
            or type(data["offset"]) is not int or data["offset"] < 0):
        raise inbox.StateError(f"{_offset_path(home)} is not a sent-log offset file")
    return data["offset"]


def _save_offset(home: pathlib.Path, offset: int) -> None:
    os.makedirs(pathlib.Path(home) / relay.RELAY_DIR, mode=relay.DIR_MODE, exist_ok=True)
    inbox.write_json_file(_offset_path(home), {"v": 1, "offset": offset})


def _read_from(home: pathlib.Path, offset: int) -> tuple[bytes, int]:
    """(the log's bytes from `offset`, the offset actually used): a log shorter than the
    saved offset was replaced, so it is read from its start."""
    try:
        fd = os.open(pathlib.Path(home) / SENT_LOG, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
    except FileNotFoundError:
        return b"", offset
    with os.fdopen(fd, "rb") as handle:
        if offset > os.fstat(handle.fileno()).st_size:
            offset = 0
        handle.seek(offset)
        return handle.read(READ_MAX_BYTES), offset


def _block(entry: dict, handle: str, index: int, count: int, nonce: str) -> list[str]:
    lines = [f"<<agent-bus {nonce} {index}/{count}>>",
             f"kind: SENT (this session, as {handle})",
             f"to: {', '.join(entry['to'])}",
             f"team: {entry['team']}",
             "sent: " + datetime.datetime.fromtimestamp(entry["ms"] / 1000, datetime.UTC)
             .strftime("%Y-%m-%dT%H:%M:%SZ"),
             f"id: {entry['id']}"]
    if entry["kind"] == "say":
        shown, cut = relay.cap_text(entry["text"])
        lines.append("body: " + json.dumps(shown, ensure_ascii=True))
        if cut != relay.CUT_NONE:
            done, total = cut.split("/")
            lines.append(f"truncated: {done} of {total} bytes shown.")
    else:
        lines += [f"verb: {entry['verb']}", f"ref: {entry['ref'] or '-'}", f"re: {entry['re'] or '-'}"]
    lines.append(f"<<end agent-bus {nonce} {index}/{count}>>")
    return lines


def drain(home: pathlib.Path, handles: Mapping[str, str], nonce: str,
          problems: list[str] | None = None) -> str | None:
    """The echo of every send not yet shown, or None; what it shows is not shown again.
    `handles` maps each active team to this member's handle there; an entry for another
    team is refused. A line that fails validation is skipped and described in `problems`
    (never with its content)."""
    offset = _load_offset(home)
    raw, offset = _read_from(home, offset)
    chosen: list[tuple[dict, str]] = []
    size = len(HEADER.format(nonce=nonce)) + 128
    consumed = 0
    while True:
        end = raw.find(b"\n", consumed)
        if end < 0 or len(chosen) == ECHO_MAX_ENTRIES:
            break
        try:
            entry = json.loads(raw[consumed:end].decode("utf-8"))
            validate(entry)
            if entry["team"] not in handles:
                raise ValueError("a team this session does not hold")
        except ValueError as exc:  # UnicodeDecodeError and JSONDecodeError are ValueErrors
            if problems is not None:
                problems.append(f"{SENT_LOG}: a line was skipped: {exc}")
            consumed = end + 1
            continue
        block = "\n".join(_block(entry, handles[entry["team"]], 1, ECHO_MAX_ENTRIES, nonce))
        if chosen and size + len(block) + 1 > ECHO_MAX_BYTES:
            break
        chosen.append((entry, handles[entry["team"]]))
        size += len(block) + 1
        consumed = end + 1
    more = raw.count(b"\n", consumed)
    if consumed:
        _save_offset(home, offset + consumed)
    if not chosen:
        return None
    lines = [HEADER.format(nonce=nonce)]
    for index, (entry, handle) in enumerate(chosen, 1):
        lines += _block(entry, handle, index, len(chosen), nonce)
    if more:
        lines.append(f"{more} more sent: they follow in the next echo")
    return "\n".join(lines)
