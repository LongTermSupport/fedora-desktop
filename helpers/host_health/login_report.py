"""The login-time health surface (Plan 00109, Tasks 3.1 and 3.2).

Runs the three checks this plan built — post-boot health, play freshness,
installed-vs-pinned — merges their findings into **one** report, and notifies only
if there is something to say.

It runs at the **end of a login** rather than at boot, because the point is that
somebody is present to read it. The unit is `After=graphical-session.target`.

Four rules, each the answer to a way a health surface stops being one:

1. **Silent when clean.** No findings, no notification, no output, exit 0. A check
   that speaks on every login gets muted, and a muted check is not a check.
2. **One notification, not three.** A host with a stale play *and* a failed unit
   *and* a drifted pin gets a single message listing three things. Three messages
   is the other way this ends up ignored.
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
import subprocess
import sys
from collections.abc import Callable
from typing import TextIO

from helpers.host_health import handoff, probe, probe_results, status_document
from helpers.play_ledger import check_freshness, ledger, ledger_presence, repo, store
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

_SUMMARY = "fedora-desktop: this machine needs attention"
_TIMEOUT_SECONDS = 15


def message(findings: list[probe_results.Finding]) -> str:
    """The notification body. Raises on an empty list rather than sending nothing."""
    if not findings:
        raise ValueError("no findings, so there is no notification to send")
    count = len(findings)
    noun = "finding" if count == 1 else "findings"
    body = "\n".join(f"• {finding.text}" for finding in findings)
    return f"{count} {noun}:\n{body}"


def collect_sections(
    *,
    health: Callable[[], probe_results.Report],
    ledger_present: Callable[[], list[probe_results.Finding]],
    freshness: Callable[[], list[probe_results.Finding]],
    pins: Callable[[], list[probe_results.Finding]],
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
        }
    )


def collect(
    *,
    health: Callable[[], probe_results.Report],
    ledger_present: Callable[[], list[probe_results.Finding]],
    freshness: Callable[[], list[probe_results.Finding]],
    pins: Callable[[], list[probe_results.Finding]],
) -> list[probe_results.Finding]:
    """The same findings flattened, for the notification and for stdout.

    Derived from `collect_sections` rather than collected again, so the notification
    and the status document cannot disagree about which checks ran.
    """
    sections = collect_sections(
        health=health, ledger_present=ledger_present, freshness=freshness, pins=pins
    )
    return [finding for group in sections.values() for finding in group]


def publish(
    base: str,
    *,
    sections: dict[str, list[probe_results.Finding]],
    kernel: str,
    at: str,
) -> str:
    """Write the machine-readable document, and return where it went.

    Written on **every** run, clean or not. A document that only appears when
    something is wrong makes a healthy host look exactly like a host nothing has ever
    checked — this plan's own defect, moved into the file format. `ok` is a result.

    (The handoff file is the opposite case and correctly conditional: it exists to be
    handed to Claude Code, and a healthy host has nothing to diagnose.)

    A named function because this is the seam between two separately tested modules,
    which is where this repo's defects live. Inline in `main` it would have no tests.
    """
    path = status_document.path(base)
    status_document.write_atomic(
        path, status_document.build(sections=sections, kernel=kernel, at=at)
    )
    return path


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
    """
    outcome = runner(["dkms", "status"])
    if not outcome.ok:
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
    """
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


def freshness_findings(
    base: str,
    repo_root: str,
    *,
    stderr: TextIO,
    run: Callable[..., int] = check_freshness.run,
) -> list[probe_results.Finding]:
    """The freshness check's findings, via its real entry point.

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
        reason = "; ".join(diagnostics.lines()) or "it gave no reason"
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

    sections = collect_sections(
        health=lambda: probe.collect(running_kernel=probe.running_kernel()),
        ledger_present=lambda: ledger_presence.findings(base),
        freshness=lambda: freshness_findings(base, arguments.repo_root, stderr=diagnostics),
        pins=lambda: check_pins.check(
            pins=check_pins.declared_pins(arguments.repo_root),
            playbook_text=lambda relative: _read(arguments.repo_root, relative),
            dkms_status=lambda: dkms_text(probe.run_probe),
            # A pin belongs to a play, and a play this host has never run installs
            # nothing here for the pin to be about. Without this, a server reports
            # "evdi_version: pinned 1.15.0, nothing installed" at every single login.
            ran_plays=plays_run_here(base),
        ),
    )
    findings = [finding for group in sections.values() for finding in group]
    notifier: Callable[[str], None] = (lambda _: None) if arguments.no_notify else _notify_send
    status = emit(findings, notify=notifier, write=out.write)

    # Unconditional: a clean host must be distinguishable from one nothing has checked.
    # Guarded because the report above has already been delivered — losing the panel's
    # copy is a degradation, and one worth naming, but not a reason to fail the login.
    if not arguments.no_handoff:
        try:
            publish(
                state_base,
                sections=sections,
                kernel=probe.running_kernel(),
                at=repo.utc_now(),
            )
        except Exception as error:
            diagnostics.write(f"the host status document could not be written: {error}\n")

    if findings and not arguments.no_handoff:
        # Task 3.3: the handoff file, so diagnosing a break is not archaeology from
        # scratch. Written, named, and NOT launched — the handoff is offered, always.
        # Guarded because a failure to write it must not lose the findings above,
        # which have already been reported by this point.
        try:
            path = handoff.write(
                base,
                findings=findings,
                kernel=probe.running_kernel(),
                at=repo.utc_now(),
            )
            out.write(f"{handoff.offer(path)}\n")
        except Exception as error:
            out.write(f"the handoff file could not be written: {error}\n")
    return status


def _read(root: str, relative: str) -> str:
    with open(os.path.join(root, relative), encoding="utf-8") as handle:
        return handle.read()


if __name__ == "__main__":
    raise SystemExit(main())
