"""The unattended self-update's last result, as report findings (Plan 00137 Task 4.3).

The cycle runs as root at night on a server nobody is watching. It alerts, but an alert
is one message at 03:30. This puts the same outcome into the host-health report, so the
next person to log in, or to run `fedora-desktop-health`, meets it.

It reads `helpers.self_update.published`, the one copy of the result the user may read.
The rules, each answering a way this check could lie:

1. **No status directory, no finding.** The play creates the directory only where
   self-update is enabled, so a host without it says nothing at all.
2. **A failed cycle is a fault**, naming when, the phase it stopped at, and why.
3. **Silence is a claim.** The cycle records a result every night, including "nothing to
   do". A newest result older than `STALE_AFTER_DAYS` means the timer stopped or every
   run since failed before it could record, so the age is itself a fault. An enabled
   host with no result at all is judged from when the directory was created.
4. **A reboot whose post-boot check never ran is a fault**, once `VERIFY_GRACE_SECONDS`
   of this boot have passed, because the check may still be waiting for the sessions.
5. **What this cannot read is not checked**, never clean.
6. **An alert that did not arrive is a fault**, whatever the cycle's outcome: the sink
   is the channel someone relies on, and a clean-looking silence there proves nothing.

Contract: CLAUDE/Plan/00137-unattended-server-self-update/DESIGN-cycle.md.
"""

from __future__ import annotations

import datetime
import os

from helpers.host_health import probe_results
from helpers.self_update import published

#: The cycle's timer fires nightly (03:30 plus up to 30 minutes, `Persistent=true`), and
#: every run that gets as far as the update records a result. One night lost to the play
#: lock, or one failed start, is a single miss. Three days is two whole missed nights of
#: margin, and a cycle that has really stopped is still reported inside the week.
STALE_AFTER_DAYS = 3

#: `fedora-desktop-self-update-verify.service` starts after the user's manager and may
#: take up to its `TimeoutStartSec=30min`. Until then an owed check is not yet late.
VERIFY_GRACE_SECONDS = 35 * 60

#: A stamp this far in the future is a clock fault, not skew (the same bound, and the
#: same reason, as `login_message.FUTURE_TOLERANCE_DAYS`).
FUTURE_TOLERANCE_DAYS = 1

_UPTIME = "/proc/uptime"
_BOOT_ID = "/proc/sys/kernel/random/boot_id"


def read_uptime_seconds(proc_uptime: str = _UPTIME) -> float:
    """Seconds since boot, or -1 for "unknown". Unknown never buys silence (rule 4)."""
    try:
        with open(proc_uptime, encoding="utf-8") as handle:
            return float(handle.read().split()[0])
    except (OSError, ValueError, IndexError):
        return -1.0


def read_boot_id(proc_boot_id: str = _BOOT_ID) -> str:
    """This boot's id, or "" when it cannot be read."""
    try:
        with open(proc_boot_id, encoding="utf-8") as handle:
            return handle.read().strip()
    except OSError:
        return ""


def _parse_stamp(stamp: str) -> datetime.datetime | None:
    try:
        parsed = datetime.datetime.strptime(stamp, "%Y-%m-%dT%H:%M:%SZ")
    except ValueError:
        return None
    return parsed.replace(tzinfo=datetime.timezone.utc)


_STOPPED = ("so its nightly timer has stopped, or every run since has failed before it "
            "could record anything")


def _age_findings(since: datetime.datetime, now: datetime.datetime, *, recorded: bool) -> list[probe_results.Finding]:
    """The finding for a newest result (or, with none, the enabling) this old, if any."""
    age = now - since
    if age < -datetime.timedelta(days=FUTURE_TOLERANCE_DAYS):
        return [probe_results.unchecked(
            "the unattended self-update's result is stamped in the future, so this host's "
            "clock cannot be trusted and how long ago the cycle last ran is unknown"
        )]
    if age <= datetime.timedelta(days=STALE_AFTER_DAYS):
        return []
    if recorded:
        return [probe_results.broken(
            f"the unattended self-update last recorded a result {age.days} days ago, {_STOPPED}"
        )]
    return [probe_results.broken(
        f"unattended self-update was set up here {age.days} days ago and no cycle has "
        f"recorded a result since, {_STOPPED}"
    )]


def findings(directory: str, *, now: str, boot_id: str, uptime_seconds: float) -> list[probe_results.Finding]:
    """Every finding about the self-update cycle on this host, `[]` when it is healthy
    or not enabled."""
    if not os.path.isdir(directory):
        return []
    current = _parse_stamp(now)
    if current is None:
        return [probe_results.unchecked(
            f"the current time {now!r} could not be read, so the self-update result was not judged"
        )]
    try:
        record = published.read(directory)
        created = os.stat(directory).st_mtime
    except (OSError, ValueError) as error:
        return [probe_results.unchecked(f"the unattended self-update's result could not be read: {error}")]

    if record is None:
        since = datetime.datetime.fromtimestamp(created, tz=datetime.timezone.utc)
        return _age_findings(since, current, recorded=False)

    result: list[probe_results.Finding] = []
    outcome = record["outcome"]
    if outcome in published.FAILED_OUTCOMES:
        result.append(probe_results.broken(
            f"the last unattended self-update, at {record['at']}, stopped at {record['phase']} "
            f"({outcome}): {record['detail']}"
        ))
    elif outcome not in published.OK_OUTCOMES | published.IN_PROGRESS_OUTCOMES:
        result.append(probe_results.unchecked(
            f"the unattended self-update recorded the outcome {outcome!r}, which this report "
            "does not know, so whether it succeeded is unknown"
        ))

    if record["alert"]:
        result.append(probe_results.broken(
            f"the unattended self-update's alert for its {record['at']} result could not be "
            f"delivered ({record['alert']}), so nobody was told about it through that channel "
            "(journalctl -u fedora-desktop-self-update.service --no-pager | cat)"
        ))

    stamp = _parse_stamp(record["at"])
    if stamp is None:
        result.append(probe_results.unchecked(
            f"the unattended self-update's result has no readable time ({record['at']!r}), "
            "so how long ago it last ran is unknown"
        ))
    else:
        result.extend(_age_findings(stamp, current, recorded=True))

    owed = record["owed_boot"]
    if owed:
        if not boot_id:
            result.append(probe_results.unchecked(
                "a post-boot check is owed after a self-update reboot, and this boot's id "
                "could not be read, so whether that reboot has happened is unknown"
            ))
        elif owed != boot_id and not 0 <= uptime_seconds < VERIFY_GRACE_SECONDS:
            result.append(probe_results.broken(
                "a self-update reboot happened, but the post-boot check that the ccy "
                "sessions came back has not been recorded "
                "(journalctl -u fedora-desktop-self-update-verify.service -b --no-pager | cat)"
            ))
    return result
