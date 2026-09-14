"""Report plays that have changed since they were last run here (Plan 00109, Task 2.1).

Wires the ledger to git and prints the findings. Every decision it makes is in
`freshness.py`; every git answer comes from `git_history.py`. What lives here is
the order of operations and the exit status.

**Silent when clean.** Nothing reaches stdout unless there is something to act on.
A health check that speaks on every login gets muted, and a muted check is not a
check.

**Three outcomes, not two.** "Nothing is stale" and "I cannot tell you whether
anything is stale" are different answers, and a caller that cannot distinguish
them will treat the second as the first — which is this plan's whole subject.

Run it: `python3 -m helpers.play_ledger.check_freshness [--repo-root PATH]`

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import argparse
import os
import sys
from collections.abc import Callable
from typing import TextIO

from helpers.play_ledger import fetch_clock, freshness, git_history, ledger, repo, store

#: Clean: nothing the user must act on.
EXIT_OK = 0
#: Plays have changed since they were run here. Not an error — a finding.
EXIT_FINDINGS = 1
#: No answer could be given. Distinct from EXIT_FINDINGS on purpose.
EXIT_UNTRUSTWORTHY = 2

_STATE_LABEL = {
    freshness.STALE: "changed since it was run here",
    freshness.GONE: "no longer exists at HEAD",
    freshness.UNEXPLAINED: "differs from HEAD and no commit explains it",
}


def run(
    *,
    base: str,
    repo_root: str,
    stdout: TextIO,
    stderr: TextIO,
    unchecked: TextIO | None = None,
    fetch: Callable[[str], None] | None = None,
    changes_since: Callable[[str, str, str], list[tuple[str, str]]] | None = None,
    play_sha256_at_head: Callable[[str, str], str | None] | None = None,
) -> int:
    """Print the freshness findings and return the exit status.

    The three git callables are seams for testing; unsupplied, the real ones are used.

    `unchecked` is where findings that mean *"nothing was checked against upstream"* go
    — the refs-are-old answer, which is a real finding but not a fault anybody has
    established. It defaults to `stdout`, so run by hand this prints exactly as before;
    the login surface passes a separate sink so it can label them, because a consumer
    that has to tell them apart by wording gets it wrong.
    """
    fetch = fetch or git_history.fetch
    changes_since = changes_since or (
        lambda root, commit, play: git_history.changes_since(root, commit, play)
    )
    play_sha256_at_head = play_sha256_at_head or (
        lambda root, play: git_history.play_sha256_at_head(root, play)
    )

    broken = store.broken_reason(base)
    if broken is not None:
        # Answer nothing, and do nothing to find out — a fetch at login costs the
        # user time for a result that would be discarded unread.
        report = freshness.build_report(verdicts=[], broken_reason=broken)
        return _emit(report, stdout, stderr)

    try:
        latest = ledger.fold_latest(store.read_lines(base))
    except ValueError as error:
        # fold_latest refuses a corrupt line rather than skipping it. Softening that
        # into a clean report here would undo the only reason it raises.
        stderr.write(f"play-freshness: the ledger cannot be read: {error}\n")
        return EXIT_UNTRUSTWORTHY

    plays = freshness.plays_to_query(latest)
    if not plays:
        # Nothing has ever been run here. Silence is the correct output.
        return EXIT_OK

    offline: str | None = None
    reached_remote = False
    try:
        fetch(repo_root)
        reached_remote = True
    except Exception as error:
        # Offline at login is ordinary, so a failed fetch is NOT untrustworthy on its
        # own. What matters is how long it has been — a fact about this host rather
        # than about the network. DESIGN-host-health.md §8.
        stderr.write(f"play-freshness: git fetch failed, judging on the refs on hand: {error}\n")
        offline = fetch_clock.offline_finding(
            last=fetch_clock.last_success(base), now=repo.utc_now()
        )

    if reached_remote:
        # Stamped in its OWN try. Inside the fetch's, a stamp that cannot be written —
        # a full disk, a read-only state directory — was reported as a failed fetch,
        # and `offline_finding` then read the absent stamp and said the remote had
        # never been reached. Two confident statements, both false, about a fetch that
        # had just succeeded.
        try:
            fetch_clock.record_success(base, at=repo.utc_now())
        except Exception as error:
            stderr.write(
                "play-freshness: the fetch succeeded, but recording its timestamp "
                f"failed, so the next run will judge the gap from an older stamp: {error}\n"
            )

    verdicts: list[freshness.Verdict] = []
    for play in plays:
        record = latest[play]
        try:
            changes = changes_since(repo_root, record["commit"], play)
            head = play_sha256_at_head(repo_root, play)
        except Exception as error:
            # An unresolvable ledgered commit must never read as "nothing changed".
            stderr.write(f"play-freshness: cannot judge {play}: {error}\n")
            return EXIT_UNTRUSTWORTHY
        verdicts.append(freshness.classify(record=record, changes=changes, head_sha256=head))

    status = _emit(freshness.build_report(verdicts=verdicts, broken_reason=None), stdout, stderr)
    if offline is not None:
        # An offline run still judged, on the refs it had. What is reported is the
        # age of those refs, and only once it is past the bound. It goes to the
        # `unchecked` sink because that is what it means — nothing was compared
        # against upstream — and not to a fault this host has been shown to have.
        (unchecked or stdout).write(f"{offline}\n")
        return EXIT_FINDINGS
    return status


def _emit(report: freshness.Report, stdout: TextIO, stderr: TextIO) -> int:
    if report.broken_reason is not None:
        stderr.write(
            "play-freshness: the ledger is marked BROKEN and cannot be trusted, so no "
            f"play was judged.\n  reason: {report.broken_reason}\n"
            "  clear it deliberately once the cause is fixed; it never clears itself.\n"
        )
        return EXIT_UNTRUSTWORTHY

    if report.clean:
        return EXIT_OK

    for verdict in report.stale:
        stdout.write(f"{verdict.play} — {_STATE_LABEL[verdict.state]}\n")
        for short, subject in verdict.changes:
            stdout.write(f"    {short}  {subject}\n")
    return EXIT_FINDINGS


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/play_ledger/check_freshness.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=_repo_root_default())
    arguments = parser.parse_args(argv)
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
    return run(base=base, repo_root=arguments.repo_root, stdout=sys.stdout, stderr=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
