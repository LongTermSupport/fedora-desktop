"""Play-freshness verdicts over the host ledger (Plan 00109, Task 2.1; Plan 00214).

Answers one question per play the ledger has actually seen: *does this play have to run
again here?* Which plays do is decided once, by `changed_plays.judge`, the judgement
`run.bash --changed` acts on; this module holds the verdict shape the health report and
the panel's play rows carry, and the report built from it. No I/O.

Two rules carry the plan's weight and are easy to erode:

1. **A play with no record is silent unless a file of it here shows it ran.** Reporting
   every never-run optional play as stale on day one — there are dozens — is how a health
   check trains its reader to ignore it. One with no record whose files here show it ran
   (before the ledger, or not through run.bash) is UNRECORDED, and `run.bash --changed`
   runs it (issue #323); `changed_plays` says which files show that.
2. **A known-broken ledger answers nothing at all.** While the `BROKEN` sentinel
   exists the history has holes, and a per-play verdict from an incomplete history
   is a specific false statement — worse than the general warning it replaces.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

from typing import NamedTuple

#: Nothing it uses changed since its last run here, which succeeded with its inputs committed.
FRESH = "fresh"
#: Its own file or a checkout path it deploys changed since it ran, or one was uncommitted then.
STALE = "stale"
#: Its last run here did not succeed.
FAILED = "failed"
#: Not changed as far as can be told, but it holds a reference that cannot be followed.
#: Not a fault: `run.bash --changed` does not run it either. The report's coverage names it.
UNRESOLVED = "unresolved"
#: It ran here, and the checkout does not have it.
GONE = "gone"
#: No run of it is recorded here, but a file it deploys here shows that it ran (issue #323).
UNRECORDED = "unrecorded"

#: Faults: the plays `run.bash --changed` runs, and the ones the checkout does not have.
REPORTABLE = (STALE, FAILED, UNRECORDED, GONE)


class Verdict(NamedTuple):
    play: str
    state: str
    #: The reasons, one line each: changed paths, a dirty run, a play taken over.
    detail: tuple[str, ...] = ()


class Report(NamedTuple):
    stale: tuple[Verdict, ...]
    broken_reason: str | None

    @property
    def clean(self) -> bool:
        """Nothing to say. False while the ledger is known broken, findings or not."""
        return not self.stale and self.broken_reason is None


def build_report(
    *, verdicts: list[Verdict], broken_reason: str | None
) -> Report:
    """The reportable verdicts, or none of them if the ledger is known broken."""
    if broken_reason is not None:
        # Withheld, not merely flagged: every verdict here was folded from a history
        # with an acknowledged hole in it.
        return Report((), broken_reason)
    return Report(tuple(v for v in verdicts if v.state in REPORTABLE), None)
