"""The login-time health surface (Plan 00109, Tasks 3.1 and 3.2).

Runs the four checks this plan built — post-boot health, ledger presence, play
freshness, installed-vs-pinned — plus Plan 00137's self-update result, merges their
findings into **one** report, and notifies only if there is something to say.

It runs at the **end of a login** rather than at boot, because the point is that
somebody is present to read it. The unit is `After=graphical-session.target`.

Four rules, each the answer to a way a health surface stops being one:

1. **Silent when clean.** No findings, no notification, no output, exit 0. A check
   that speaks on every login gets muted, and a muted check is not a check.
2. **One notification, not three.** A host with a stale play *and* a failed unit
   *and* a drifted pin gets a single message counting three things and saying where
   to read them. Three messages is the other way this ends up ignored.
3. **Merged, not chained.** Each check is called independently and a raising one
   becomes a finding naming itself, so one broken check can never suppress
   another's findings.
4. **The notification is not the only channel.** A broken notifier must not turn a
   broken host into a silent one: stdout still carries every finding and the exit
   status still says there were some.

Run it: `python3 -m helpers.host_health.login_report`

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-host-health.md
"""

from __future__ import annotations

import argparse
import os
import shlex
import subprocess
import sys
from collections.abc import Callable
from typing import TextIO

from helpers.host_health import (
    handoff,
    login_message,
    play_runner,
    probe,
    probe_results,
    self_update_check,
    status_document,
)
from helpers.play_ledger import (
    check_freshness,
    freshness,
    ledger,
    ledger_presence,
    plugin_support,
    repo,
    store,
)
from helpers.self_update import published
from helpers.version_pins import check_pins

#: Clean: nothing the user must act on, and nothing shown.
EXIT_OK = 0
#: Something is wrong, or could not be checked. Both are the user's business.
#:
#: **3, not 1, and that is load-bearing.** The unit declares this status a success, so
#: that a drifted host does not also register as a broken service. Python exits **1**
#: for any uncaught exception — a wrong `WorkingDirectory`, a renamed helper, a typo in
#: a module — so at 1 the unit would declare a permanently dead health surface a
#: success, deliver nothing, and leave `is-failed` silent. That is this plan's own
#: incident, rebuilt inside the code written to prevent it. 1 and 2 stay failures.
EXIT_FINDINGS = 3

#: Section ids, shared by the notification's guard messages and the status document's
#: keys — which are the panel registry's lookup keys, so these are interface, not
#: labels. Renaming one silently makes the panel's section render `unavailable`.
#: Read from `status_document` rather than spelled again: a consumer keys on this id to
#: recognise the one boot-scoped section, and two spellings of it would let the producer
#: and that consumer disagree without anything saying so.
HEALTH = status_document.BOOT_SCOPED_SECTION
LEDGER = "play-ledger"
FRESHNESS = "play-freshness"
PINS = "installed-vs-pinned"
#: Plan 00137: the unattended cycle's last result. Always present, and clean on a host
#: without self-update, so the section set does not depend on the host.
SELF_UPDATE = "self-update"

_SUMMARY = "fedora-desktop: this machine needs attention"
_TIMEOUT_SECONDS = 15


def message(findings: list[probe_results.Finding]) -> str:
    """The notification body. Raises on an empty list rather than sending nothing.

    A headline and where to read the rest, never the findings themselves. A
    notification cannot be selected or copied, and one finding carrying a diagnostic
    and its remedy made it an unreadable wall. The two kinds are counted apart for the
    reason `login_message.item_counts` gives.
    """
    if not findings:
        raise ValueError("no findings, so there is no notification to send")
    faults = sum(1 for finding in findings if finding.checked)
    unchecked = len(findings) - faults
    parts = []
    if faults:
        parts.append(f"{faults} {'finding' if faults == 1 else 'findings'}")
    if unchecked:
        parts.append(f"{unchecked} {'item' if unchecked == 1 else 'items'} not checked")
    return (
        f"{' and '.join(parts)}. Read them in the Fedora Desktop panel, "
        f"or run {login_message.ON_DEMAND_COMMAND} in a terminal."
    )


def collect_sections(
    *,
    health: Callable[[], probe_results.Report],
    ledger_present: Callable[[], list[probe_results.Finding]],
    freshness: Callable[[], list[probe_results.Finding]],
    pins: Callable[[], list[probe_results.Finding]],
    self_update: Callable[[], list[probe_results.Finding]],
) -> dict[str, list[probe_results.Finding]]:
    """Every check's findings, kept under the id of the check that produced them.

    Each check is called independently and separately guarded by
    `status_document.collect`, which is the **only** copy of that guard. Chaining them
    — or letting one exception escape — would let a single broken check hide the
    others, which is the failure mode this whole plan is about one level up.

    Host health comes first, and dict order carries it through to `collect`: something
    broken on this machine now outranks something that has merely drifted. The ledger's
    own emptiness sits second for the same reason — it is a fault here and now, and it
    is also the explanation for anything the two ledger-reading checks below it fail to
    say, so a user reading top to bottom meets the cause before the silence.

    The section ids are the panel's registry keys, so they are part of the interface
    and not just labels. They read as check names because a guard failure quotes them
    back to the user.
    """
    return status_document.collect(
        {
            HEALTH: lambda: list(health().findings),
            LEDGER: ledger_present,
            FRESHNESS: freshness,
            PINS: pins,
            SELF_UPDATE: self_update,
        }
    )


def collect(
    *,
    health: Callable[[], probe_results.Report],
    ledger_present: Callable[[], list[probe_results.Finding]],
    freshness: Callable[[], list[probe_results.Finding]],
    pins: Callable[[], list[probe_results.Finding]],
    self_update: Callable[[], list[probe_results.Finding]],
) -> list[probe_results.Finding]:
    """The same findings flattened, for the notification and for stdout.

    Derived from `collect_sections` rather than collected again, so the notification
    and the status document cannot disagree about which checks ran.
    """
    sections = collect_sections(
        health=health, ledger_present=ledger_present, freshness=freshness, pins=pins,
        self_update=self_update,
    )
    return [finding for group in sections.values() for finding in group]


def publish(
    base: str,
    *,
    sections: dict[str, list[probe_results.Finding]],
    kernel: str,
    at: str,
    # `handoff_path`, not `handoff`: this module imports the `handoff` module, and a
    # parameter of that name shadows it inside the function body.
    handoff_path: str = "",
    coverage: dict[str, str] | None = None,
    plays: list[dict[str, str]] | None = None,
) -> str:
    """Write the machine-readable document, and return where it went.

    `coverage` carries each section's statement of the population it examined, so a
    consumer asserts on a number instead of inferring one from the absence of a
    complaint.

    Written on **every** run, clean or not. A document that only appears when
    something is wrong makes a healthy host look exactly like a host nothing has ever
    checked — this plan's own defect, moved into the file format. `ok` is a result.

    (The handoff file is the opposite case and correctly conditional: it exists to be
    handed to Claude Code, and a healthy host has nothing to diagnose.)

    `handoff` is the path of that file when one was written, `""` otherwise. It is
    passed in rather than derived here, because only the caller knows whether the write
    actually succeeded — and a path this function computed would name a file that may
    not exist.

    A named function because this is the seam between two separately tested modules,
    which is where this repo's defects live. Inline in `main` it would have no tests.
    """
    path = status_document.path(base)
    status_document.write_atomic(
        path,
        status_document.build(
            sections=sections, kernel=kernel, at=at, handoff=handoff_path,
            coverage=coverage, plays=plays,
        ),
    )
    return path


def record_host_state(
    *,
    ledger_base: str,
    state_base: str,
    sections: dict[str, list[probe_results.Finding]],
    findings: list[probe_results.Finding],
    kernel: str,
    at: str,
    out: Callable[[str], object],
    diagnostics: Callable[[str], object],
    coverage: dict[str, str] | None = None,
    plays: list[dict[str, str]] | None = None,
) -> str:
    """Write the handoff and the status document, in that order, and return the path.

    **The order is the point, which is why this is a function.** The document NAMES the
    handoff file, and the panel turns that name into a button (Task 3.3). A path
    recorded before the write is a path that may have no file behind it — a button that
    fails in the user's hands, on a surface whose entire job is to be trustworthy about
    what is and is not known. Writing first and recording the RESULT makes that
    unrepresentable rather than merely discouraged.

    Inline in `main` this ordering would have no test, and `main` runs four real probes.
    `publish` is a named function for exactly the same reason.

    Both writes are guarded, and both guards are deliberate: the findings have already
    been reported by the time this runs, so losing either file is a degradation worth
    naming and not a reason to fail a login. They are named on different streams because
    they are different kinds of thing — the handoff is part of the report the user is
    reading, and the document is machinery.

    Returns the handoff path, or `""` when there are no findings or the write failed.
    """
    handoff_path = ""
    if findings:
        # Written, named, and NOT launched — the handoff is offered, always. Only when
        # there is something to diagnose: a healthy host has nothing to hand over.
        try:
            handoff_path = handoff.write(
                ledger_base, findings=findings, kernel=kernel, at=at
            )
        except Exception as error:
            out(f"the handoff file could not be written: {error}\n")

    # Unconditional: a clean host must be distinguishable from one nothing has checked.
    try:
        publish(
            state_base,
            sections=sections,
            kernel=kernel,
            at=at,
            handoff_path=handoff_path,
            coverage=coverage,
            plays=plays,
        )
    except Exception as error:
        diagnostics(f"the host status document could not be written: {error}\n")

    # LAST, so the offer stays the final line of the report whichever way the two writes
    # above went — and only when the file exists, because `offer` names a path and
    # naming one that was never written sends the reader to a file that is not there.
    if handoff_path:
        out(f"{handoff.offer(handoff_path)}\n")
    return handoff_path


def emit(
    findings: list[probe_results.Finding],
    *,
    notify: Callable[[str], None],
    # `object`, not `None`: the real caller is `sys.stdout.write`, which returns an
    # int, and a `-> None` annotation would make the production call site the one
    # shape the type checker rejects.
    write: Callable[[str], object],
) -> int:
    """Report the findings on both channels and return the exit status."""
    if not findings:
        return EXIT_OK
    for finding in findings:
        write(f"{finding.text}\n")
    try:
        notify(message(findings))
    except Exception as error:
        # The findings are already on stdout, so this is a degraded report rather
        # than a lost one — and the degradation itself is worth saying out loud.
        write(f"the notification could not be sent: {error}\n")
    return EXIT_FINDINGS


def dkms_text(runner: Callable[[list[str]], probe_results.ProbeOutcome]) -> str:
    """`dkms status` output, raising if the probe could not run.

    `ProbeOutcome.text` is `""` on failure, and empty `dkms status` output is a
    legitimate healthy state — a host with no DKMS modules. Passing the text
    straight through would therefore turn "dkms is not installed" into
    "pinned 1.15.0, nothing installed": a confident claim about this host that
    nothing measured. `check_pins` turns the raise into "could not be checked",
    which is the honest answer.

    A command the OS could not find raises `NotInstalled`, its own type: with no DKMS
    state directory as well, that is a host with no DKMS subsystem, and `check_pins`
    reads it as an answer by type, never by message.
    """
    outcome = runner(["dkms", "status"])
    if not outcome.ok:
        if outcome.missing:
            raise check_pins.NotInstalled(outcome.error)
        raise check_pins.ResolutionError(outcome.error)
    return outcome.text


def _notify_send(body: str) -> None:
    subprocess.run(
        ["notify-send", "--app-name=fedora-desktop", "--urgency=normal", _SUMMARY, body],
        check=True,
        capture_output=True,
        text=True,
        timeout=_TIMEOUT_SECONDS,
    )


def plays_run_here(base: str) -> set[str] | None:
    """Which plays this host has a ledger record for, or None when it cannot be read.

    **None is not the empty set.** An empty ledger means no play has been recorded here,
    which is an answer — Task 1.3 settled that a play with no record has never been run
    here, and silence is correct for it. A ledger that could not be read leaves the
    question open, and `check_pins` treats None by keeping every pin applicable, because
    an open question must not buy silence on a whole drift axis.

    Guarded rather than raising: the ledger's own brokenness is `ledger_presence`'s
    finding, reported once and in its own section, so letting it also take down the pin
    check would be two voices on one fact — and this runs at login, where an exception
    costs the user the report entirely.

    **The BROKEN sentinel answers None too**, and that is the one case the "it is
    reported elsewhere" argument does not cover: `ledger_presence.findings` returns
    nothing while the sentinel exists, deliberately, because `check_freshness` already
    prints the reason. So in the single state where this repo has declared the ledger
    incomplete, a set read from it anyway would silently suppress every `ABSENT` verdict
    whose row is in the hole, with nothing saying so. The sentinel IS the declaration
    that the question is open, and an open question must not buy silence.

    **So does the CLEARED marker**, and for the identical reason. `--clear-broken`
    removes the sentinel, and once it did only that, this function went straight from
    None to a PARTIAL set — turning the open question into a confident wrong answer and
    skipping exactly the pins whose rows were in the hole. The missing rows cannot be
    recovered, so the set stays a lower bound for good: `ledger.cleared_path` carries
    the argument, and the cost only ever runs in the direction of reporting more.
    """
    if os.path.exists(ledger.sentinel_path(base)):
        return None
    if os.path.exists(ledger.cleared_path(base)):
        return None
    try:
        return set(ledger.fold_latest(store.read_lines(base)))
    except (OSError, ValueError):
        return None


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/host_health/login_report.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


class _Sink:
    """Collects one channel's output. Two of these, never one shared between both."""

    def __init__(self) -> None:
        self._chunks: list[str] = []

    def write(self, text: str) -> None:
        self._chunks.append(text)

    def lines(self) -> list[str]:
        return [line for line in "".join(self._chunks).splitlines() if line.strip()]


def _fold_detail_lines(lines: list[str]) -> list[str]:
    """One finding per line, with each finding's indented detail folded into it.

    `check_freshness` writes a stale play as a headline followed by indented commit
    lines. Returned as peers they became findings in their own right: the count said
    four problems where there was one, and the notification listed commits as though
    each were something to fix.

    An indented line with nothing above it is kept as its own finding. Dropping it
    would be the worse failure — this is the layer that must not lose anything.
    """
    findings: list[str] = []
    for line in lines:
        if line.startswith((" ", "\t")) and findings:
            findings[-1] = f"{findings[-1]}; {line.strip()}"
        else:
            findings.append(line.strip())
    return findings


_FRESHNESS_TAG = "play-freshness: "


def _diagnostic_sentence(lines: list[str], repo_root: str) -> str:
    """`check_freshness`'s multi-line diagnostic as one readable line, remedy intact.

    A finding is one line by contract (`emit`, `item_counts`), so the structure has to
    survive as punctuation. Joining the raw lines with `"; "` kept their indentation and
    produced `.;   reason:` and `itself:;     cd`, which is what the panel showed as an
    unbroken wall. Each line is stripped; a line that introduces the next (ends in `:`)
    runs on into it, the rest are separated. The first line's own `play-freshness:` tag
    is dropped because the finding already names the check.

    The clear command's checkout placeholder becomes this checkout, so the one
    actionable part is copyable as it stands rather than a template to fill in.
    """
    text = ""
    for index, raw in enumerate(lines):
        line = raw.strip()
        if index == 0 and line.startswith(_FRESHNESS_TAG):
            line = line[len(_FRESHNESS_TAG):]
        if not text:
            text = line
        elif text.endswith(":"):
            text = f"{text} {line}"
        else:
            text = f"{text.rstrip('.')}; {line}"
    return text.replace(plugin_support.CHECKOUT_PLACEHOLDER, shlex.quote(repo_root))


def freshness_findings(
    base: str,
    repo_root: str,
    *,
    stderr: TextIO,
    run: Callable[..., int] = check_freshness.run,
    judged: list[freshness.Verdict] | None = None,
) -> list[probe_results.Finding]:
    """The freshness check's findings, via its real entry point.

    `judged` is handed straight to the check, so the panel's play runner rows come from
    the very run whose findings the freshness section shows.

    Its two channels are kept apart deliberately. Sharing one sink between them made
    every diagnostic a user-facing finding: "git fetch failed" was reported as a
    problem with this host on any login that had any other finding — which is
    precisely what [DESIGN-host-health.md](../../CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-host-health.md)
    §8 decided against — and in the long-gap case it appeared twice, once as the raw
    exception and once as `offline_finding`'s sentence.

    `run` is injected for the same reason `dkms_text`'s runner is: this seam is where
    the defects were, and it is only testable if it can be driven.
    """
    # Three sinks, one per meaning: findings, diagnostics, and findings that mean
    # nothing was checked. The check knows which is which; a consumer reading the
    # wording does not.
    out, diagnostics, not_checked = _Sink(), _Sink(), _Sink()
    status = run(
        base=base,
        repo_root=repo_root,
        stdout=out,
        stderr=diagnostics,
        unchecked=not_checked,
        judged=judged,
    )

    # Diagnostics are not findings, and they are not discarded either — they go where
    # diagnostics go (CLAUDE/StderrHygiene.md).
    for line in diagnostics.lines():
        stderr.write(f"{line}\n")

    if status == check_freshness.EXIT_UNTRUSTWORTHY:
        # "I cannot tell you" is a finding. It is the state the BROKEN sentinel and
        # an unresolvable ledgered commit produce, and reporting it as clean is the
        # exact defect this plan exists for. It carries the REASON: the sentinel
        # exists to say why, and a finding that pointed at "its stderr output above"
        # sent the reader looking for something never shown to them.
        reason = _diagnostic_sentence(diagnostics.lines(), repo_root) or "it gave no reason"
        return [
            probe_results.unchecked(
                f"play-freshness could not give an answer, so no play was judged: {reason}"
            )
        ]
    if status == check_freshness.EXIT_OK:
        return []
    return [
        probe_results.broken(text) for text in _fold_detail_lines(out.lines())
    ] + [probe_results.unchecked(text) for text in not_checked.lines()]


def main(
    argv: list[str] | None = None,
    *,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=_repo_root_default())
    parser.add_argument(
        "--no-notify",
        action="store_true",
        help="report on stdout only; for running it by hand",
    )
    parser.add_argument(
        "--no-handoff",
        action="store_true",
        help="write no host state — neither the handoff file nor the status document; "
        "for a triage run that must not clobber what the last real login left",
    )
    arguments = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    # The findings are the payload and go to stdout; the checks' own diagnostics are
    # not findings and go here. One stream for both is what made a failed fetch read
    # as a problem with the host.
    diagnostics = stderr if stderr is not None else sys.stderr
    home = os.path.expanduser("~")
    base = ledger.ledger_dir(os.environ, home)
    # The status document is host-health state, not ledger state, so it sits one level
    # up beside the ledger rather than inside it. `GLib.get_user_state_dir()` applies
    # the identical XDG rule, which is how the panel finds the same file.
    state_base = ledger.state_dir(os.environ, home)

    # The pin axis states the population it compared, and the document carries it even
    # when the axis is clean. Captured here because the producer runs inside
    # `status_document.collect`'s per-section guard and returns only findings — and a
    # producer that raised has no coverage to state, which is why the map stays empty
    # rather than gaining a zero.
    stated_coverage: dict[str, str] = {}

    def pins_findings() -> list[probe_results.Finding]:
        result = check_pins.check_with_coverage(
            pins=check_pins.declared_pins(arguments.repo_root),
            playbook_text=lambda relative: _read(arguments.repo_root, relative),
            dkms_status=lambda: dkms_text(probe.run_probe),
            # The two things this host knows about itself. `registry` decides whether a
            # DKMS-resolved pin is answerable here at all; `ran_plays` disambiguates one
            # verdict, "nothing installed", which means something different on a host
            # that ran the play and one that never did.
            registry=probe.dkms_registry(),
            ran_plays=plays_run_here(base),
        )
        stated_coverage[PINS] = result.coverage.sentence()
        return result.findings

    # Filled by the freshness check itself, and left empty by every path on which it
    # could not judge — so the panel's play runner offers nothing it did not judge.
    judged: list[freshness.Verdict] = []

    sections = collect_sections(
        health=lambda: probe.collect(running_kernel=probe.running_kernel()),
        ledger_present=lambda: ledger_presence.findings(base),
        freshness=lambda: freshness_findings(
            base, arguments.repo_root, stderr=diagnostics, judged=judged
        ),
        pins=pins_findings,
        self_update=lambda: self_update_check.findings(
            published.DIRECTORY,
            now=repo.utc_now(),
            boot_id=self_update_check.read_boot_id(),
            uptime_seconds=self_update_check.read_uptime_seconds(),
        ),
    )
    findings = [finding for group in sections.values() for finding in group]
    notifier: Callable[[str], None] = (lambda _: None) if arguments.no_notify else _notify_send
    status = emit(findings, notify=notifier, write=out.write)

    if not arguments.no_handoff:
        record_host_state(
            ledger_base=base,
            state_base=state_base,
            sections=sections,
            findings=findings,
            kernel=probe.running_kernel(),
            at=repo.utc_now(),
            out=out.write,
            diagnostics=diagnostics.write,
            coverage=stated_coverage,
            plays=play_runner.runnable(judged),
        )
    return status


def _read(root: str, relative: str) -> str:
    with open(os.path.join(root, relative), encoding="utf-8") as handle:
        return handle.read()


if __name__ == "__main__":
    raise SystemExit(main())
