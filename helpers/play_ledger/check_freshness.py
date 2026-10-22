"""Report the plays that have to run again here (Plan 00109, Task 2.1; Plan 00214).

Wires the ledger to the one judgement of which plays have to run again,
`changed_plays.judge`, and prints the findings. That is the judgement `run.bash --changed`
acts on, so the plays this reports as changed or failed are exactly the plays that command
runs, and running it clears them. What lives here is the order of operations, the wording
and the exit status.

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
    changed_plays,
    fetch_clock,
    freshness,
    git_history,
    ledger,
    plugin_support,
    repo,
    store,
)

#: Clean: nothing the user must act on.
EXIT_OK = 0
#: Plays have to run again here. Not an error — a finding.
EXIT_FINDINGS = 1
#: No answer could be given. Distinct from EXIT_FINDINGS on purpose.
EXIT_UNTRUSTWORTHY = 2

_STATE_LABEL = {
    freshness.STALE: "changed since it was run here",
    freshness.FAILED: "its last run here did not succeed",
    freshness.GONE: "no longer in the checkout",
}

_VERDICT_STATE = {
    changed_plays.STATE_STALE: freshness.STALE,
    changed_plays.STATE_FAILED: freshness.FAILED,
    changed_plays.STATE_UNRESOLVED: freshness.UNRESOLVED,
    changed_plays.STATE_CURRENT: freshness.FRESH,
}

#: How many changed paths a finding names before it counts the rest.
_PATHS_SHOWN = 3


def run(
    *,
    base: str,
    repo_root: str,
    stdout: TextIO,
    stderr: TextIO,
    unchecked: TextIO | None = None,
    fetch: Callable[[str], None] | None = None,
    judge: Callable[[dict[str, dict], str], changed_plays.Judgement] | None = None,
    judged: list[freshness.Verdict] | None = None,
    coverage: list[str] | None = None,
) -> int:
    """Print the findings and return the exit status.

    `fetch` and `judge` are seams for testing; unsupplied, the real ones are used.

    `judged`, when given, receives every verdict of a COMPLETE judgement, fresh ones
    included — the panel's play runner lists each play with its state. It stays empty
    on every path that answers untrustworthy, so a partial list is never offered as
    the whole one.

    `coverage`, when given, receives one sentence saying what was judged and what was not:
    the plays outside `playbooks/imports/`, and the plays holding a reference nobody can
    follow. Neither is a fault, and `run.bash --changed` runs neither.

    `unchecked` is where findings that mean *"nothing was checked against upstream"* go
    — the refs-are-old answer, which is a real finding but not a fault anybody has
    established. It defaults to `stdout`, so run by hand this prints exactly as before;
    the login surface passes a separate sink so it can label them, because a consumer
    that has to tell them apart by wording gets it wrong.
    """
    fetch = fetch or git_history.fetch
    judge = judge or (lambda latest, root: changed_plays.judge(latest, repo_root=root))

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

    if not latest:
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

    try:
        judgement = judge(latest, repo_root)
    except changed_plays.NoAnswer as no_answer:
        # An unjudgeable play must never read as "nothing changed".
        stderr.writelines(f"play-freshness: {line}\n" for line in no_answer.lines)
        return EXIT_UNTRUSTWORTHY

    verdicts = [_verdict(judgement, play) for play in judgement.order]
    if judged is not None:
        judged.extend(verdicts)
    if coverage is not None:
        coverage.append(coverage_sentence(judgement))

    status = _emit(freshness.build_report(verdicts=verdicts, broken_reason=None), stdout, stderr)
    if offline is not None:
        # An offline run still judged, on the refs it had. What is reported is the
        # age of those refs, and only once it is past the bound. It goes to the
        # `unchecked` sink because that is what it means — nothing was compared
        # against upstream — and not to a fault this host has been shown to have.
        (unchecked or stdout).write(f"{offline}\n")
        return EXIT_FINDINGS
    return status


def _verdict(judgement: changed_plays.Judgement, play: str) -> freshness.Verdict:
    """One play's verdict, with the reasons a reader acts on as its detail lines."""
    if play in judgement.gone:
        return freshness.Verdict(play, freshness.GONE)
    state = _VERDICT_STATE[judgement.states[play]]
    if state != freshness.STALE:
        return freshness.Verdict(play, state)
    detail = [
        f"it takes over {old}, which the checkout removed"
        for old in judgement.took_over.get(play, [])
    ]
    paths = judgement.changed_inputs.get(play, [])
    if paths:
        shown = ", ".join(paths[:_PATHS_SHOWN])
        more = len(paths) - _PATHS_SHOWN
        detail.append(f"changed: {shown}" + (f" and {more} more" if more > 0 else ""))
    if play in judgement.dirty:
        detail.append("it last ran from a checkout with uncommitted changes")
    return freshness.Verdict(play, state, tuple(detail))


def coverage_sentence(judgement: changed_plays.Judgement) -> str:
    """What the play-freshness section judged, and what it leaves out, in one sentence."""
    judged_count = len(judgement.states) + len(judgement.gone)
    sentence = (
        f"{judged_count} plays run here under playbooks/imports/, judged as run.bash --changed "
        "judges them: their own file and every checkout path they deploy, their last run's "
        "outcome, and whether that run was from a dirty checkout"
    )
    if judgement.unresolved:
        sentence += (
            f"; {len(judgement.unresolved)} of them hold a reference that cannot be followed, "
            f"so a change there is not seen: {', '.join(sorted(judgement.unresolved))}"
        )
    if judgement.not_judged:
        sentence += (
            f"; {len(judgement.not_judged)} other ledgered plays are not host plays and are "
            f"not judged: {', '.join(judgement.not_judged)}"
        )
    return sentence


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
        stdout.write(f"{verdict.play} — {_STATE_LABEL[verdict.state]}\n")
        stdout.writelines(f"    {line}\n" for line in verdict.detail)
    return EXIT_FINDINGS


def clear_broken(
    *,
    base: str,
    stdout: TextIO,
    stderr: TextIO = sys.stderr,
    only_if_reason: str | None = None,
) -> int:
    """Forget a recorded ledger hole, so recording can resume. The operator's route.

    Without this route a sentinel, once written, would leave the ledger permanently
    untrustworthy with no documented way out, and every check downstream would refuse
    for ever. A fail-safe with no reset is a fail-stop.

    What this does NOT do is recover the missing rows. The plays that ran while the
    sentinel existed were never recorded and cannot be reconstructed, so the ledger
    stays incomplete — it just stops being *known-broken*, which is a weaker and
    honest claim. Reporting that difference is why this prints rather than being
    silent, and why it is a deliberate flag rather than something `run` does for you.

    `only_if_reason` is for an unattended clear (a meta-deploy script) written for one
    known cause: the sentinel is cleared only if its reason contains that text, and is
    otherwise left in place with a [FATAL] line, because a hole with any other reason
    has a cause nobody has said is fixed.
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
        if only_if_reason is not None:
            stderr.write(
                f"[FATAL] play-ledger: the reason in {sentinel} could not be read ({error}), "
                f"so it cannot be shown to be {only_if_reason!r}; left in place.\n"
            )
            return EXIT_UNTRUSTWORTHY
        # Reported, not fatal: the operator asked to clear the sentinel, and failing
        # to quote it back is no reason to leave it in place.
        reason = f"(the reason could not be read: {error})"
    if only_if_reason is not None and only_if_reason not in reason:
        stderr.write(
            f"[FATAL] play-ledger: the recorded hole is not the one this clear was written "
            f"for ({only_if_reason!r}), so it is left in place: {reason}\n"
            "Clear it by hand once its own cause is fixed.\n"
        )
        return EXIT_UNTRUSTWORTHY
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
    parser.add_argument(
        "--only-if-reason",
        metavar="TEXT",
        help=(
            "with --clear-broken: clear only a hole whose recorded reason contains TEXT, "
            "else leave it and exit 2. For an unattended clear written for one known cause."
        ),
    )
    arguments = parser.parse_args(argv)
    if arguments.only_if_reason is not None and not arguments.clear_broken:
        parser.error("--only-if-reason needs --clear-broken")
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
    if arguments.clear_broken:
        return clear_broken(
            base=base,
            stdout=sys.stdout,
            stderr=sys.stderr,
            only_if_reason=arguments.only_if_reason,
        )
    return run(base=base, repo_root=arguments.repo_root, stdout=sys.stdout, stderr=sys.stderr)


if __name__ == "__main__":
    raise SystemExit(main())
