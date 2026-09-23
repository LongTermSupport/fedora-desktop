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

from helpers.play_ledger import (
    fetch_clock,
    freshness,
    git_history,
    ledger,
    plugin_support,
    repo,
    retired,
    store,
)

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
    retired_plays: Callable[[str], dict[str, str]] | None = None,
    path_exists_at: Callable[[str, str, str], bool] | None = None,
    judged: list[freshness.Verdict] | None = None,
) -> int:
    """Print the freshness findings and return the exit status.

    The git callables and the retired-plays loader are seams for testing; unsupplied,
    the real ones are used.

    `judged`, when given, receives every verdict of a COMPLETE judgement, fresh ones
    included — the panel's play runner lists each play with its state. It stays empty
    on every path that answers untrustworthy, so a partial list is never offered as
    the whole one.

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

    try:
        verdicts = _apply_retirements(
            verdicts,
            latest=latest,
            repo_root=repo_root,
            retired_plays=retired_plays or retired.load,
            path_exists_at=path_exists_at or (
                lambda root, commit, path: git_history.path_exists_at(root, commit, path)
            ),
        )
    except Exception as error:
        # A map that cannot be applied would leave a GONE finding standing, or drop
        # one, with nothing saying which. Neither is an answer.
        stderr.write(f"play-freshness: the retired-plays map cannot be applied: {error}\n")
        return EXIT_UNTRUSTWORTHY

    if judged is not None:
        judged.extend(verdicts)

    status = _emit(freshness.build_report(verdicts=verdicts, broken_reason=None), stdout, stderr)
    if offline is not None:
        # An offline run still judged, on the refs it had. What is reported is the
        # age of those refs, and only once it is past the bound. It goes to the
        # `unchecked` sink because that is what it means — nothing was compared
        # against upstream — and not to a fault this host has been shown to have.
        (unchecked or stdout).write(f"{offline}\n")
        return EXIT_FINDINGS
    return status


def _apply_retirements(
    verdicts: list[freshness.Verdict],
    *,
    latest: dict[str, dict],
    repo_root: str,
    retired_plays: Callable[[str], dict[str, str]],
    path_exists_at: Callable[[str, str, str], bool],
) -> list[freshness.Verdict]:
    """Name the successor of each mapped GONE play, and drop those it has absorbed.

    The map is read only when something is GONE: it can change no other answer, so a
    login with nothing removed costs no extra git call. When it is read it is checked
    against HEAD in full, not just the entries this host happens to need.
    """
    if not any(verdict.state == freshness.GONE for verdict in verdicts):
        return verdicts
    mapping = retired_plays(repo_root)
    retired.validate(mapping, exists_at_head=lambda path: path_exists_at(repo_root, "HEAD", path))

    kept: list[freshness.Verdict] = []
    for verdict in verdicts:
        successor = mapping.get(verdict.play) if verdict.state == freshness.GONE else None
        if successor is None:
            kept.append(verdict)
            continue
        # The successor's LATEST run, and only a SUCCESSFUL one: a run that failed at
        # its first task never reached what it absorbed. If that commit lacks the old
        # play, the run carried everything the old play used to deploy — which holds
        # only if the removal and the merge land in ONE commit (see retired.py).
        record = latest.get(successor)
        ran_after = (
            record is not None
            and record["outcome"] == "ok"
            and not path_exists_at(repo_root, record["commit"], verdict.play)
        )
        remaining = freshness.retire(verdict, successor=successor, successor_ran_after_removal=ran_after)
        if remaining is not None:
            kept.append(remaining)
    return kept


def _label(verdict: freshness.Verdict) -> str:
    if verdict.successor is not None:
        return (
            f"{_STATE_LABEL[verdict.state]}; merged into {verdict.successor} — "
            f"run {verdict.successor} to retire this"
        )
    return _STATE_LABEL[verdict.state]


def _emit(report: freshness.Report, stdout: TextIO, stderr: TextIO) -> int:
    if report.broken_reason is not None:
        # The remedy belongs HERE as much as in record_failure's line. That one is
        # printed during an ansible-playbook run and stops the moment the cause is
        # fixed; this is the surface an operator meets at every login until the hole
        # is cleared, and it used to say the sentinel "never clears itself" without
        # ever naming what does.
        stderr.write(
            "play-freshness: the ledger is marked BROKEN and cannot be trusted, so no "
            f"play was judged.\n  reason: {report.broken_reason}\n"
            "  clear it deliberately once the cause is fixed; it never clears itself:\n"
            f"    {plugin_support.CLEAR_COMMAND}\n"
        )
        return EXIT_UNTRUSTWORTHY

    if report.clean:
        return EXIT_OK

    for verdict in report.stale:
        stdout.write(f"{verdict.play} — {_label(verdict)}\n")
        for short, subject in verdict.changes:
            stdout.write(f"    {short}  {subject}\n")
    return EXIT_FINDINGS


def clear_broken(*, base: str, stdout: TextIO) -> int:
    """Forget a recorded ledger hole, so recording can resume. The operator's route.

    `store.clear_broken` existed from the start and had **no caller** — no CLI, no
    script, nothing. So a sentinel, once written, made the ledger permanently
    untrustworthy with no documented way out, and every check downstream refused for
    ever. A fail-safe with no reset is a fail-stop; issue #46 put two hosts in exactly
    that state.

    What this does NOT do is recover the missing rows. The plays that ran while the
    sentinel existed were never recorded and cannot be reconstructed, so the ledger
    stays incomplete — it just stops being *known-broken*, which is a weaker and
    honest claim. Reporting that difference is why this prints rather than being
    silent, and why it is a deliberate flag rather than something `run` does for you.
    """
    sentinel = ledger.sentinel_path(base)
    if not os.path.exists(sentinel):
        stdout.write("play-ledger: no recorded hole to clear.\n")
        return EXIT_OK
    reason = ""
    try:
        with open(sentinel, encoding="utf-8") as handle:
            reason = handle.read().strip()
    except OSError as error:
        # Reported, not fatal: the operator asked to clear the sentinel, and failing
        # to quote it back is no reason to leave it in place.
        reason = f"(the reason could not be read: {error})"
    # Dated, because the CLEARED marker's whole job is to say the record set is a lower
    # bound FROM SOME POINT ON. An undated marker says a hole was cleared and not when,
    # which is the half of the fact a reader would act on.
    store.clear_broken(base, at=repo.utc_now())
    stdout.write(f"play-ledger: cleared the recorded hole — {reason}\n")
    stdout.write(
        "The plays that ran while it existed were never recorded and are NOT recovered; "
        "the ledger is incomplete, it is simply no longer known-broken.\n"
    )
    return EXIT_OK


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/play_ledger/check_freshness.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=_repo_root_default())
    parser.add_argument(
        "--clear-broken",
        action="store_true",
        help=(
            "forget a recorded ledger hole and exit. The hole is REAL — plays that ran "
            "while it existed were not recorded, and clearing does not recover them — so "
            "this only says the CAUSE is fixed and recording may resume."
        ),
    )
    arguments = parser.parse_args(argv)
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
    if arguments.clear_broken:
        return clear_broken(base=base, stdout=sys.stdout)
    return run(base=base, repo_root=arguments.repo_root, stdout=sys.stdout, stderr=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
