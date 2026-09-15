"""The status document rendered for a login shell (Plan 00109, Task 3.2 server route).

The second consumer of `status_document`, and the one a **server** gets. The desktop
delivery is `notify-send` from a `graphical-session.target` unit: there is no session bus
to reach on a server and that target never activates, so `play-host-health-login-report.yml`
is `scope: gnome` and ends its play there. Without this, a server — where unattended drift
goes unnoticed longest, because nobody logs in to see a notification — got nothing.

What makes this affordable is that the checks no longer run here. They run on their own
schedule and leave the document behind; this prints what they left. A `git fetch` at every
SSH login would add latency to every login and can hang on an unreachable remote.

Two rules, and they pull against each other:

* **Silent when clean**, because a message on every login gets ignored and then it is not
  a report.
* **Silent is a claim.** A clean document nobody has updated for a month describes a host
  as it was a month ago. The worst outcome available is a host that quietly stops being
  checked, which is this plan's subject in its purest form.

Both are kept by treating the document's own age as a finding past a declared bound —
the same shape `DESIGN-host-health.md` §8 settled for the fetch clock, for the same
reason.

Age is not the only way a fresh document can fail to describe this host. The producer
runs on a timer here, so a document can outlive a **reboot**: collected minutes ago,
`ok`, and entirely about the kernel that is no longer running. A kernel mismatch is
therefore reported in its own right, independently of age.

**Nothing here raises.** It is called from a login shell, so a traceback costs the user
their prompt, and that is worse than any report it might have printed. Every branch ends
in a string.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md
"""

from __future__ import annotations

import argparse
import datetime
import os
import sys
from typing import TextIO

from helpers.host_health import probe, status_document
from helpers.play_ledger import ledger, repo

#: How old the document may be before its age is itself reported. Declared, not buried in
#: a branch, because it is a judgement about how often a host is expected to be checked
#: and a reader has to be able to find it. Generous: the desktop producer runs once per
#: graphical login, so a laptop left shut over a long weekend must not cry stale.
STALE_AFTER_DAYS = 14

#: How far ahead of now the document's stamp may sit before the clock itself is reported.
#: Deliberately not zero. `(now - then).days` floors, so any stamp between one second and
#: one day ahead reads as -1 — and the producer and consumer are the same host, so an NTP
#: correction of a few seconds landing between the write and the read would otherwise cry
#: "wrong clock" at every login on a perfectly healthy machine. A stamp more than a full
#: day ahead cannot be ordinary skew. Declared next to the staleness bound because it is
#: the same kind of judgement, and a reader comparing the two has to find both.
FUTURE_TOLERANCE_DAYS = 1

_HEADER = "fedora-desktop: this machine needs attention"
_NOT_CHECKED = "Not checked — these are NOT clean results, nothing is known about them:"


def _age_days(generated_at: object, now: str) -> int | None:
    """Whole days between the two stamps, or None if either cannot be read.

    None means *unknown*, and by this plan's standing rule an unknown age is not a fresh
    one — `render` reports it rather than assuming the document is current. A negative
    answer means the stamp is in the future, which `render` reports too.

    A stamp carrying no timezone designator counts as unreadable. `fromisoformat` accepts
    naive and aware forms alike, so mixing the two raises `TypeError` on the subtraction
    — out of the one function whose entire purpose is that a login shell never sees a
    traceback, and past an `except ValueError` that cannot catch it. The producer always
    writes `Z`, so a naive stamp came from another version, a hand-edit or a truncation;
    assuming UTC for it would invent an age rather than admit to not knowing one.

    `fetch_clock._parse` is immune to this by using a strict `strptime` format, which
    yields a naive datetime on both sides and so cannot mix them.
    """
    if not isinstance(generated_at, str) or not generated_at:
        return None
    try:
        then = datetime.datetime.fromisoformat(generated_at.replace("Z", "+00:00"))
        current = datetime.datetime.fromisoformat(now.replace("Z", "+00:00"))
        if then.tzinfo is None or current.tzinfo is None:
            return None
        return (current - then).days
    except (TypeError, ValueError):
        return None


def _texts(section: object, key: str) -> list[str]:
    """One group's lines, defensively.

    The document is read off disk and may have been written by a different version, or
    truncated, or hand-edited. A wrong type here must degrade to "nothing in this group"
    rather than take down the login shell.
    """
    if not isinstance(section, dict):
        return []
    lines = section.get(key)
    if not isinstance(lines, list):
        return []
    return [line for line in lines if isinstance(line, str)]


def render(document: object, *, now: str, running_kernel: str) -> str:
    """The message, or `""` to say nothing.

    Known faults first — something broken now outranks something merely unknown — then
    the not-checked group under a heading that says in terms that these are not clean
    results. Keeping those two apart is the whole value of the report; a list that mixes
    them and distinguishes neither reads like a complete picture of a machine, which is
    how this plan's incident happened.
    """
    sections = document.get("sections") if isinstance(document, dict) else None
    if not isinstance(sections, dict):
        sections = {}

    broken: list[str] = []
    unchecked: list[str] = []
    for section in sections.values():
        broken.extend(_texts(section, "findings"))
        unchecked.extend(_texts(section, "unchecked"))

    age = _age_days(
        document.get("generated_at") if isinstance(document, dict) else None, now
    )
    # A future stamp past the tolerance is reported unconditionally, unlike an unknown
    # one. A negative age can never reach STALE_AFTER_DAYS, so without this branch a
    # clock that ran backwards buys unlimited silence — and a wrong clock invalidates
    # every age in the document, which is worth saying even alongside real findings.
    # `fetch_clock` rejects the same condition on the sibling clock for the same reason.
    #
    # No day count in the message: `.days` floors, so the number here is a lower bound
    # rather than a measurement, and this report does not print figures it cannot stand
    # behind.
    if age is not None and age < -FUTURE_TOLERANCE_DAYS:
        unchecked.append(
            "the host status is stamped in the future, so this host's clock cannot be "
            "trusted and the age of these results is unknown"
        )
    # An unknown age is reported only when the document is otherwise silent. A document
    # that already says `unavailable` has explained itself — `status_document.read` puts
    # the reason in the self section — and adding "its age cannot be read" to that would
    # describe the same absence twice.
    elif age is None and not broken and not unchecked:
        unchecked.append(
            "the host status file gives no readable collection time, so it is not known "
            "how old these results are"
        )
    elif age is not None and age >= STALE_AFTER_DAYS:
        unchecked.append(
            f"the host status was last collected {age} days ago, so nothing here "
            f"describes this machine as it is now"
        )

    # A document can outlive a reboot on the SERVER route, where the producer runs on a
    # timer rather than at every login. A document collected under the previous kernel is
    # still fresh and can still say `ok` while every DKMS module on the box is unbuilt for
    # the kernel that actually booted — this plan's founding incident, and staleness does
    # not cover it: the document can be minutes old and still be about a different kernel.
    #
    # Both sides must be known before this is a finding. The `unavailable` shape carries
    # `kernel: ""`, and an empty running kernel means "could not tell"; neither is evidence
    # of a mismatch, and manufacturing one out of ignorance is the inverse of this plan's
    # rule and just as wrong. Read defensively for the same reason every other field here
    # is: the document may have been written by another version, or truncated.
    collected_under = document.get("kernel") if isinstance(document, dict) else None
    if (
        isinstance(collected_under, str)
        and collected_under
        and running_kernel
        and collected_under != running_kernel
    ):
        unchecked.append(
            f"these results were collected under kernel {collected_under} and this host "
            f"is now running {running_kernel}, so nothing here describes the running "
            "kernel"
        )

    if not broken and not unchecked:
        return ""

    lines = [_HEADER]
    lines.extend(f"  - {text}" for text in broken)
    if unchecked:
        lines.append(f"  {_NOT_CHECKED}")
        lines.extend(f"  - {text}" for text in unchecked)
    return "\n".join(lines)


def read_and_render(path: str, *, now: str, running_kernel: str) -> str:
    """The whole server-side job: read the document, render it.

    `status_document.read` already turns absent, unparseable and unknown-schema into an
    `unavailable` document rather than an empty one, so this cannot accidentally report a
    missing file as a healthy host.
    """
    return render(status_document.read(path), now=now, running_kernel=running_kernel)


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None) -> int:
    """What the login shell runs. **Always exits 0.**

    That is not laziness about error reporting — it is the contract. A non-zero status
    from a snippet sourced by a profile can trip `set -e` in the surrounding shell, and
    a health reporter that costs the user the login to the host it reports on is worse
    than no reporter. Everything it has to say, it says on stdout.

    The whole text is the payload here, which is the documented exception to
    CLAUDE/StderrHygiene.md: nothing captures this in a `$(...)`, a human reads it.
    """
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--state-dir",
        default=None,
        help="this host's fedora-desktop state directory; resolved from XDG by default",
    )
    arguments = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout

    state_dir = arguments.state_dir or ledger.state_dir(
        os.environ, os.path.expanduser("~")
    )
    message = read_and_render(
        status_document.path(state_dir),
        now=repo.utc_now(),
        # One definition of "the running kernel", shared with the producer, rather than a
        # second `os.uname()` here that could drift from it.
        running_kernel=probe.running_kernel(),
    )
    if message:
        out.write(f"{message}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
