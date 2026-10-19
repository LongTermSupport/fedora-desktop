"""Does a delivery wake a large context whose prompt cache has gone cold?

Spec: Plan 00194 SPEC-compact-wake.md C1-C6 (stage 1: the measurement, observe only).

The session's transcript says it all, and the watcher already knows where it is
(`inject.Marks.transcript`). The last main-thread assistant entry's `message.usage` is the
prompt the session sent on its last model call: input, cache writes and cache reads together.
Its timestamp is when the prompt cache was last used, and the `cache_creation` split of the
newest call that wrote the cache says which TTL the session runs on (one hour or five minutes).
A cache hit refreshes the TTL, so the cache is cold once the TTL has passed since the last call.
A `compact_boundary` entry newer than that call replaced the conversation with its summary, of
`compactMetadata.postTokens`.

The threshold is in tokens, not a percentage of the window: the cost of a cold read is in
tokens, and the transcript does not carry the window's size. Anything not known (no transcript,
no call yet, an unknown TTL, a call that asked for a tool and so a turn still running) is not a
cold wake: the delivery goes as it does today.
"""

from __future__ import annotations

import dataclasses
import datetime
import json
import os
import re

#: The one place the trigger's size is set: 30 % of a 200k-token window (decision D1).
THRESHOLD_TOKENS = 60_000
#: At most this much of the transcript's end is read for one delivery.
TAIL_MAX_BYTES = 8 << 20
TTL_1H_S = 3600.0
TTL_5M_S = 300.0
#: `stop_reason`s of a call that ended its turn; anything else (a tool call, nothing) has not.
TURN_ENDED = frozenset({"end_turn", "stop_sequence", "max_tokens"})
_USAGE_FIELDS = ("input_tokens", "cache_creation_input_tokens", "cache_read_input_tokens")
_NONCE_RE = re.compile(r"[0-9a-f]{8}")
LOG_TEMPLATE = ("cold wake: delivery {nonce} woke {tokens} tokens of context {idle} s after its last model "
                "call, past its {ttl} s prompt cache; compact-before-wake would apply (observe only)")


@dataclasses.dataclass(frozen=True)
class Context:
    """The session's context as its transcript records it."""

    tokens: int
    last_call_s: float
    ttl_s: float | None
    turn_ended: bool


@dataclasses.dataclass(frozen=True)
class Verdict:
    """A cold wake: how large, how long since the last call, and the cache's TTL."""

    tokens: int
    idle_s: float
    ttl_s: float


def _count(value: object) -> int | None:
    return value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else None


def _stamp(value: object) -> float | None:
    if not isinstance(value, str):
        return None
    try:
        return datetime.datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
    except ValueError:
        return None


def _tail(path: str, max_bytes: int) -> list[bytes]:
    with open(path, "rb") as handle:
        size = os.fstat(handle.fileno()).st_size
        start = max(0, size - max_bytes)
        handle.seek(start)
        data = handle.read(max_bytes)
    lines = data.split(b"\n")
    return lines[1:] if start > 0 else lines


def _entries_newest_first(path: str, max_bytes: int):
    for line in reversed(_tail(path, max_bytes)):
        if b'"usage"' not in line and b"compact_boundary" not in line:
            continue
        try:
            entry = json.loads(line)
        except ValueError:
            continue
        if isinstance(entry, dict) and entry.get("isSidechain") is not True:
            yield entry


def _ttl(usage: dict) -> float | None:
    split = usage.get("cache_creation")
    if not isinstance(split, dict):
        return None
    if (_count(split.get("ephemeral_1h_input_tokens")) or 0) > 0:
        return TTL_1H_S
    if (_count(split.get("ephemeral_5m_input_tokens")) or 0) > 0:
        return TTL_5M_S
    return None


def _usage(entry: dict) -> dict | None:
    message = entry.get("message")
    if entry.get("type") != "assistant" or not isinstance(message, dict):
        return None
    usage = message.get("usage")
    return usage if isinstance(usage, dict) else None


def _from_boundary(entry: dict) -> Context | None:
    meta = entry.get("compactMetadata")
    post = _count(meta.get("postTokens")) if isinstance(meta, dict) else None
    at = _stamp(entry.get("timestamp"))
    if post is None or at is None:
        return None
    return Context(tokens=post, last_call_s=at, ttl_s=None, turn_ended=True)


def read(path: str | None, *, max_bytes: int = TAIL_MAX_BYTES) -> Context | None:
    """The context of the session whose transcript is `path`, or None when it cannot be known."""
    if path is None:
        return None
    try:
        entries = _entries_newest_first(path, max_bytes)
        last: tuple[dict, dict] | None = None
        ttl = None
        for entry in entries:
            if entry.get("type") == "system" and entry.get("subtype") == "compact_boundary":
                if last is None:
                    return _from_boundary(entry)
                continue
            usage = _usage(entry)
            if usage is None:
                continue
            if last is None:
                last = (entry, usage)
            ttl = _ttl(usage)
            if ttl is not None:
                break
    except OSError:
        return None
    if last is None:
        return None
    entry, usage = last
    counts = [_count(usage.get(name)) for name in _USAGE_FIELDS]
    at = _stamp(entry.get("timestamp"))
    if at is None or any(count is None for count in counts):
        return None
    stop = entry["message"].get("stop_reason")
    return Context(tokens=sum(counts), last_call_s=at, ttl_s=ttl, turn_ended=stop in TURN_ENDED)


def assess(ctx: Context | None, *, now_s: float, threshold: int = THRESHOLD_TOKENS) -> Verdict | None:
    """A Verdict when a delivery now would wake a context of at least `threshold` tokens, idle
    at the end of a turn, whose prompt cache has expired (C2); None otherwise."""
    if ctx is None or ctx.ttl_s is None or not ctx.turn_ended or ctx.tokens < threshold:
        return None
    idle = now_s - ctx.last_call_s
    if idle < ctx.ttl_s:
        return None
    return Verdict(tokens=ctx.tokens, idle_s=idle, ttl_s=ctx.ttl_s)


def log_line(nonce: str, verdict: Verdict) -> str:
    """The one watch-log line of a cold wake (C3): the delivery's nonce and numbers only."""
    if not _NONCE_RE.fullmatch(nonce):
        raise ValueError("a delivery nonce is 8 lowercase hex digits")
    return LOG_TEMPLATE.format(nonce=nonce, tokens=verdict.tokens, idle=int(verdict.idle_s),
                               ttl=int(verdict.ttl_s))
