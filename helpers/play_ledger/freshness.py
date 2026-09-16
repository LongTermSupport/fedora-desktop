"""Play-freshness verdicts over the host ledger (Plan 00109, Task 2.1).

Answers one question per play the ledger has actually seen: *has this play changed
since it was last run here?* No I/O — git and the ledger are read by the executor
and handed in, so every rule below is unit-testable.

Two rules carry the plan's weight and are easy to erode:

1. **A play with no record is silent.** It has never been run here, and the correct
   output for it is nothing. Reporting every never-run optional play as stale on day
   one — there are dozens — is how a health check trains its reader to ignore it.
2. **A known-broken ledger answers nothing at all.** While the `BROKEN` sentinel
   exists the history has holes, and a per-play verdict from an incomplete history
   is a specific false statement — worse than the general warning it replaces.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

from typing import Any, NamedTuple

#: Unchanged since the ledgered run.
FRESH = "fresh"
#: Changed since the ledgered run — git says so, or a dirty run's bytes no longer match.
STALE = "stale"
#: Ledgered, but no longer present at HEAD.
GONE = "gone"
#: The bytes differ and no commit explains it. Reported, never guessed at.
UNEXPLAINED = "unexplained"

#: Everything a reader needs to act on. FRESH is deliberately absent.
REPORTABLE = (STALE, GONE, UNEXPLAINED)


class Verdict(NamedTuple):
    play: str
    state: str
    #: `(short_sha, subject)` for each commit touching this play since the run.
    changes: tuple[tuple[str, str], ...]


class Report(NamedTuple):
    stale: tuple[Verdict, ...]
    broken_reason: str | None

    @property
    def clean(self) -> bool:
        """Nothing to say. False while the ledger is known broken, findings or not."""
        return not self.stale and self.broken_reason is None


def plays_to_query(latest: dict[str, dict[str, Any]]) -> list[str]:
    """The plays git should be asked about — exactly those the ledger has seen.

    Sorted so two runs of the report diff cleanly rather than by dict order.
    """
    return sorted(latest)


def classify(
    *,
    record: dict[str, Any],
    changes: list[tuple[str, str]],
    head_sha256: str | None,
) -> Verdict:
    """One play's verdict.

    `changes` is every commit touching this play *since the ledgered run*;
    `head_sha256` is the play file's hash at HEAD, or None if it is no longer there.

    **Git history is the authority, the hash is only the dirty-tree guard**
    (DESIGN §1). So a commit that reverts the play to identical bytes still counts
    as churn since the run — the play's history moved even though its content did
    not, and a reader deciding whether to re-run wants to know.
    """
    play = record["play"]

    if head_sha256 is None:
        # Deleted, not merely changed. "Re-run this play" would be wrong advice, so
        # this outranks STALE however many commits touched it on the way out.
        return Verdict(play, GONE, tuple(changes))

    if changes:
        return Verdict(play, STALE, tuple(changes))

    if record["play_sha256"] == head_sha256:
        return Verdict(play, FRESH, ())

    # The bytes differ with no commit to explain it. If the run was dirty, that IS
    # the explanation — this play was among the uncommitted edits, which is exactly
    # what play_sha256 was added to catch, and what ran here is not what HEAD has.
    if record["dirty"]:
        return Verdict(play, STALE, ())

    # A clean tree, no commits, and different bytes: the ledger and the repo
    # disagree and nothing here can say which is right. Guessing is how a check
    # starts lying, so the disagreement itself is the finding.
    return Verdict(play, UNEXPLAINED, ())


def build_report(
    *, verdicts: list[Verdict], broken_reason: str | None
) -> Report:
    """The reportable verdicts, or none of them if the ledger is known broken."""
    if broken_reason is not None:
        # Withheld, not merely flagged: every verdict here was folded from a history
        # with an acknowledged hole in it.
        return Report((), broken_reason)
    return Report(tuple(v for v in verdicts if v.state in REPORTABLE), None)
