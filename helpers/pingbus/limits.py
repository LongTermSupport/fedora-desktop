"""The agent team bus's local limits (protocol spec §10), and the checks that use them.

Pure: no I/O and no clock. Every check takes `now_ms`, wall-clock milliseconds (the unit
of Matrix `origin_server_ts`), from the caller. State that must outlive one `pingbus send`
process (the token bucket, the duplicate window) is a plain object with `to_dict` and
`from_dict`, for the outbox to persist; the receive flood counter lives only as long as
the syncer that owns it.

This module is the one home of §10's values except the two byte limits, which belong to
`protocol`'s size check. The `/sync` long-poll and server-429 values are not used here:
the client code that polls and retries imports them from this module.

Spec: docs/agent-bus-protocol.md §9, §10.
"""

from __future__ import annotations

import collections
import dataclasses
import json
from collections.abc import Iterable, Mapping

from helpers.pingbus import protocol

DEFAULTS = {
    "send_per_minute": 20,
    "send_burst": 10,
    "recv_per_sender_minute": 60,
    "ack_timeout_s": 900,
    "halt_ack_timeout_s": 300,
    "human_max_age_s": 86400,
    "wait_timeout_s": 1500,
}

#: Inclusive. A member config value outside these is refused, never clamped.
BOUNDS = {
    "send_per_minute": (1, 60),
    "send_burst": (1, 30),
    "recv_per_sender_minute": (10, 600),
    "ack_timeout_s": (60, 86400),
    "halt_ack_timeout_s": (30, 3600),
    "human_max_age_s": (3600, 604800),
    "wait_timeout_s": (1, 1790),
}

DUPLICATE_WINDOW_S = 60
SYNC_LONG_POLL_S = 30
SERVER_429_MAX_TRIES = 3
SERVER_429_DEFAULT_WAIT_S = 5

#: `halt` expects an ack like the other requests, but within its own, shorter timeout.
HALT_VERB = "halt"

_MS_PER_MINUTE = 60_000


class LimitsError(ValueError):
    """A `limits` override or bound violation: a configuration error (exit 78)."""


class RateLimited(Exception):
    """A send refused by a local limit (exit 9). `reason` is `rate` or `duplicate`."""

    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


def _is_int(value: object) -> bool:
    return isinstance(value, int) and not isinstance(value, bool)


def _check_bound(key: str, value: object) -> int:
    low, high = BOUNDS[key]
    if not _is_int(value) or not low <= value <= high:
        raise LimitsError(f"limits.{key} must be an integer from {low} to {high}")
    return value


@dataclasses.dataclass(frozen=True)
class Limits:
    send_per_minute: int = DEFAULTS["send_per_minute"]
    send_burst: int = DEFAULTS["send_burst"]
    recv_per_sender_minute: int = DEFAULTS["recv_per_sender_minute"]
    ack_timeout_s: int = DEFAULTS["ack_timeout_s"]
    halt_ack_timeout_s: int = DEFAULTS["halt_ack_timeout_s"]
    human_max_age_s: int = DEFAULTS["human_max_age_s"]
    wait_timeout_s: int = DEFAULTS["wait_timeout_s"]

    def as_dict(self) -> dict[str, int]:
        return dataclasses.asdict(self)


def parse_limits(value: object) -> Limits:
    """The member config's optional `limits` object: overridable keys only, in bounds."""
    if value is None:
        return Limits()
    if not isinstance(value, Mapping):
        raise LimitsError("limits must be an object")
    # The given keys are never echoed: one may be a value pasted into the wrong place.
    if any(key not in BOUNDS for key in value):
        raise LimitsError(f"limits may hold only these keys: {', '.join(BOUNDS)}")
    return Limits(**{key: _check_bound(key, item) for key, item in value.items()})


def check_wait_timeout(value: object) -> int:
    """A `wait --timeout` value, held to the `wait_timeout_s` bounds."""
    return _check_bound("wait_timeout_s", value)


def _check_ts(value: object) -> int:
    if not _is_int(value):
        raise ValueError("timestamp must be an integer of milliseconds")
    return value


def ack_timeout_s(verb: str, limits: Limits) -> int | None:
    """Seconds a target has to `ack` or `nack` `verb`; None when no ack is expected."""
    rule = protocol.VERBS.get(verb)
    if rule is None:
        raise ValueError(f"unknown verb: {verb!r}")
    if not rule.ack_expected:
        return None
    return limits.halt_ack_timeout_s if verb == HALT_VERB else limits.ack_timeout_s


def ack_deadline_ms(sent_ms: int, verb: str, limits: Limits) -> int | None:
    timeout = ack_timeout_s(verb, limits)
    if timeout is None:
        return None
    return _check_ts(sent_ms) + timeout * 1000


def ack_overdue(sent_ms: object, verb: str, now_ms: object, limits: Limits) -> bool:
    """True once a silent target of an ack-expected ping is owed a `TIMEOUT` line."""
    now = _check_ts(now_ms)
    deadline = ack_deadline_ms(sent_ms, verb, limits)
    return deadline is not None and now > deadline


def ping_stale(origin_ms: object, verb: str, now_ms: object, limits: Limits) -> bool:
    """§9 step 08p: older than the verb's ack timeout (`ack_timeout_s` if none expected)."""
    timeout = ack_timeout_s(verb, limits)
    if timeout is None:
        timeout = limits.ack_timeout_s
    return _check_ts(now_ms) - _check_ts(origin_ms) > timeout * 1000


def human_stale(origin_ms: object, now_ms: object, limits: Limits) -> bool:
    """§7: a human message older than `human_max_age_s` when first seen."""
    return _check_ts(now_ms) - _check_ts(origin_ms) > limits.human_max_age_s * 1000


@dataclasses.dataclass
class TokenBucket:
    """The send token bucket, in integer credit: one token is `_MS_PER_MINUTE` credit,
    and `send_per_minute` credit accrues per millisecond, so refill is exact."""

    credit: int
    at_ms: int
    limits: Limits = dataclasses.field(compare=False, repr=False)

    @classmethod
    def full(cls, limits: Limits, now_ms: int) -> TokenBucket:
        return cls(limits.send_burst * _MS_PER_MINUTE, now_ms, limits)

    def _capacity(self) -> int:
        return self.limits.send_burst * _MS_PER_MINUTE

    def take(self, now_ms: int) -> bool:
        elapsed = max(0, now_ms - self.at_ms)
        self.at_ms = max(self.at_ms, now_ms)
        self.credit = min(self._capacity(), self.credit + elapsed * self.limits.send_per_minute)
        if self.credit < _MS_PER_MINUTE:
            return False
        self.credit -= _MS_PER_MINUTE
        return True

    def to_dict(self) -> dict[str, int]:
        return {"credit": self.credit, "at_ms": self.at_ms}

    @classmethod
    def from_dict(cls, state: object, limits: Limits) -> TokenBucket:
        if not isinstance(state, Mapping) or set(state) != {"credit", "at_ms"}:
            raise ValueError("token bucket state must be {credit, at_ms}")
        credit, at_ms = state["credit"], state["at_ms"]
        if not _is_int(credit) or credit < 0 or not _is_int(at_ms):
            raise ValueError("token bucket state has a bad value")
        bucket = cls(credit, at_ms, limits)
        bucket.credit = min(credit, bucket._capacity())
        return bucket


def duplicate_key(verb: str, ref: str | None, re_: str | None, to: Iterable[str]) -> str:
    """Same verb, ref, re and set of `to`: the identity the duplicate window compares."""
    return json.dumps([verb, ref, re_, sorted(set(to))], separators=(",", ":"))


@dataclasses.dataclass
class DuplicateWindow:
    """Keys of pings sent in the last `DUPLICATE_WINDOW_S`, with when."""

    sent: dict[str, int] = dataclasses.field(default_factory=dict)

    def seen(self, key: str, now_ms: int) -> bool:
        at = self.sent.get(key)
        return at is not None and now_ms - at < DUPLICATE_WINDOW_S * 1000

    def record(self, key: str, now_ms: int) -> None:
        self.sent = {k: at for k, at in self.sent.items() if self.seen(k, now_ms)}
        self.sent[key] = now_ms

    def to_dict(self) -> dict[str, int]:
        return dict(self.sent)

    @classmethod
    def from_dict(cls, state: object) -> DuplicateWindow:
        if not isinstance(state, Mapping):
            raise ValueError("duplicate window state must be an object")
        for key, at in state.items():
            if not isinstance(key, str) or not _is_int(at):
                raise ValueError("duplicate window state has a bad entry")
        return cls(dict(state))


@dataclasses.dataclass
class SendGate:
    """§9 send step 5: the duplicate window, then the token bucket."""

    bucket: TokenBucket
    recent: DuplicateWindow

    @classmethod
    def fresh(cls, limits: Limits, now_ms: int) -> SendGate:
        return cls(TokenBucket.full(limits, now_ms), DuplicateWindow())

    def admit(self, verb: str, ref: str | None, re_: str | None, to: Iterable[str], now_ms: int) -> None:
        """Record the send, or raise `RateLimited` having recorded nothing."""
        key = duplicate_key(verb, ref, re_, to)
        if self.recent.seen(key, now_ms):
            raise RateLimited("duplicate")
        if not self.bucket.take(now_ms):
            raise RateLimited("rate")
        self.recent.record(key, now_ms)

    def to_dict(self) -> dict[str, object]:
        return {"bucket": self.bucket.to_dict(), "recent": self.recent.to_dict()}

    @classmethod
    def from_dict(cls, state: object, limits: Limits) -> SendGate:
        if not isinstance(state, Mapping) or set(state) != {"bucket", "recent"}:
            raise ValueError("send gate state must be {bucket, recent}")
        return cls(TokenBucket.from_dict(state["bucket"], limits), DuplicateWindow.from_dict(state["recent"]))


class ReceiveFlood:
    """§9 steps 10h and 09p: at most `recv_per_sender_minute` admitted per sender in any
    sliding minute. Dropped events are not counted, so a flood cannot extend its own
    window."""

    def __init__(self, limits: Limits) -> None:
        self._limit = limits.recv_per_sender_minute
        self._admitted: dict[str, collections.deque[int]] = {}

    def admit(self, sender: str, now_ms: int) -> bool:
        times = self._admitted.setdefault(sender, collections.deque())
        while times and now_ms - times[0] >= _MS_PER_MINUTE:
            times.popleft()
        if len(times) >= self._limit:
            return False
        times.append(now_ms)
        return True
