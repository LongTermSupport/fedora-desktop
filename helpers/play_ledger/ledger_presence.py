"""Does this host have any record of a play having run? (Plan 00109, Task 4.2.)

`check_freshness` answers "has any recorded play drifted since it ran". On a host whose
ledger is empty it correctly answers "no" — there are no plays to have drifted — and
publishes `play-freshness: ok`. That verdict is indistinguishable from the one a fully
provisioned, fully current host produces, and the two mean opposite things. This plan
exists because a green tick meant nothing was wrong on an axis nothing was watching.

Reinterpreting freshness would be the wrong fix: its `EXIT_OK` on an empty ledger is
correct for the question it asks, it is tested twice with reasoning, and it has other
callers. So this is its own check with its own section, asking the one question nobody
was asking.

**Emptiness is a fault, not an unknown.** `run.bash` ledgers every play, and a play is
what deploys the unit that runs the login report, so by the time anything here reads the
ledger at least one record must exist. Nothing is an answer that cannot be honestly
arrived at: it means the ledger was lost, or was never being written, and either way
every drift check downstream is answering from an empty set while looking healthy.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import os

from helpers.host_health import probe_results
from helpers.play_ledger import ledger


def _has_record(path: str) -> bool:
    """True when the file holds at least one non-blank line.

    Line-wise rather than by file size: a ledger of newlines is as empty as a ledger of
    nothing, and a size check would call it populated.
    """
    with open(path, encoding="utf-8") as handle:
        return any(line.strip() for line in handle)


def findings(base: str) -> list[probe_results.Finding]:
    """`base` is the ledger directory. One finding, or none.

    Silent while the BROKEN sentinel exists. That marker says the ledger has a hole and
    must not be trusted, and `check_freshness` already refuses to answer while it is
    there and prints the reason. Adding "and it is also empty" describes one absence
    twice, and two voices on one fact read to a user as two problems.
    """
    if os.path.exists(ledger.sentinel_path(base)):
        return []

    path = ledger.runs_path(base)
    try:
        populated = _has_record(path)
    except FileNotFoundError:
        populated = False
    except OSError as error:
        # `unchecked`, deliberately, and this is the one branch here that is not a
        # fault: a ledger that could not be read has not been shown to be empty. Calling
        # it empty would report a fault nobody has established, which is the mirror of
        # the defect this module exists for.
        return [
            probe_results.unchecked(
                f"the play ledger could not be read, so it is not known whether this "
                f"host has recorded any play run: {error}"
            )
        ]

    if populated:
        return []
    return [
        probe_results.broken(
            "no play run has ever been recorded on this host, so the ledger is empty — "
            "every check that reads it is answering from nothing while reporting as "
            "though it had looked"
        )
    ]
