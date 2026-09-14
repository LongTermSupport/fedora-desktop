"""How long since the freshness check could last reach the remote (Plan 00109).

Settles the question DESIGN-host-health.md §7 left open and §8 records: what an
offline login should say.

The two rules this plan keeps invoking conflict head-on there. *"I could not look"
must never render as "nothing is wrong"* says report a failed fetch. *A check that
speaks on every login gets muted* says do not, because offline at login is ordinary
— a train, a hotel, a laptop that woke before the wifi did.

Neither wins, because the question was posed wrongly. "Can I reach the remote **right
now**" is a fact about the network, which this surface does not report. **"How long
is it since I last could"** is a fact about this host, and it is the one that matters:
a freshness verdict computed from refs fetched an hour ago is worth having, and the
same verdict from refs three weeks old is not.

So a successful fetch is stamped, and an offline run is silent while that stamp is
recent, a finding once it is not, and a *different* finding when there is none at all.
That last distinction is deliberate: a host that has never fetched has a different
problem from one that fetched last month, and a single message would describe neither.
"""

from __future__ import annotations

import datetime
import os

#: Beyond this, "I have not been able to check" stops being about the network and
#: starts being about this machine. Declared, not buried in a branch.
STALE_AFTER_DAYS = 7

#: Written beside the ledger, so it travels with it.
STAMP_NAME = "last-fetch"

_FORMAT = "%Y-%m-%dT%H:%M:%SZ"


def _parse(stamp: str) -> datetime.datetime | None:
    try:
        return datetime.datetime.strptime(stamp.strip(), _FORMAT)
    except (ValueError, AttributeError):
        return None


def shift(stamp: str, *, days: int) -> str:
    """`stamp` moved by `days`. Exposed because the tests need it and a private
    copy in the test file could drift from the format used here."""
    moment = _parse(stamp)
    if moment is None:
        raise ValueError(f"not a timestamp: {stamp!r}")
    return (moment + datetime.timedelta(days=days)).strftime(_FORMAT)


def record_success(base: str, *, at: str) -> None:
    """Stamp a successful fetch. Only ever called after one actually succeeded."""
    os.makedirs(base, exist_ok=True)
    with open(os.path.join(base, STAMP_NAME), "w", encoding="utf-8") as handle:
        handle.write(f"{at}\n")


def last_success(base: str) -> str | None:
    """The last stamped success, or None if there is none this can read.

    An unreadable stamp degrades to None — "never fetched", which is a finding —
    rather than to a recent time, which would be a silent pass.
    """
    try:
        with open(os.path.join(base, STAMP_NAME), encoding="utf-8") as handle:
            stamp = handle.read().strip()
    except OSError:
        return None
    return stamp if _parse(stamp) is not None else None


def offline_finding(*, last: str | None, now: str) -> str | None:
    """What to report when the fetch failed. None means stay silent."""
    if last is None:
        return (
            "play-freshness has never successfully reached the remote on this host, "
            "so no play has ever been checked for staleness here"
        )
    moment, current = _parse(last), _parse(now)
    if moment is None or current is None:
        return (
            "play-freshness cannot tell when it last reached the remote "
            f"(unreadable timestamp {last!r}), so its verdicts cannot be trusted"
        )
    elapsed = current - moment
    if elapsed < datetime.timedelta(0):
        # A clock that ran backwards would otherwise buy unlimited silence.
        return (
            "play-freshness last reached the remote in the future "
            f"({last}), so this host's clock cannot be trusted"
        )
    days = elapsed.days
    if days <= STALE_AFTER_DAYS:
        return None
    return (
        f"play-freshness has not reached the remote for {days} days, so nothing has "
        "been checked against upstream in that time"
    )
