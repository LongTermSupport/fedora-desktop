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

from helpers.host_health import handoff, probe, probe_results
from helpers.play_ledger import check_freshness, ledger, repo
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


def collect(
    *,
    health: Callable[[], probe_results.Report],
    freshness: Callable[[], list[probe_results.Finding]],
    pins: Callable[[], list[probe_results.Finding]],
) -> list[probe_results.Finding]:
    """Every finding from every check, host health first.

    Each check is called independently and separately guarded. Chaining them — or
    letting one exception escape — would let a single broken check hide the others,
    which is the failure mode this whole plan is about one level up.

    Host-health findings come first: something broken on this machine now outranks
    something that has merely drifted.
    """
    findings: list[probe_results.Finding] = []

    try:
        findings.extend(health().findings)
    except Exception as error:
        findings.append(
            probe_results.unchecked(f"the post-boot health probe could not run: {error}")
        )

    for label, check in (("play-freshness", freshness), ("installed-vs-pinned", pins)):
        try:
            findings.extend(check())
        except Exception as error:
            # Named, so the user learns WHICH check stopped working rather than
            # that something, somewhere, did. Every guard here produces an
            # `unchecked` finding by construction: reaching this line means the
            # check did not complete, so nothing is known about its subject.
            findings.append(
                probe_results.unchecked(f"the {label} check could not run: {error}")
            )

    return findings


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
        help="do not write the handoff file; for a triage run that must not clobber it",
    )
    arguments = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    # The findings are the payload and go to stdout; the checks' own diagnostics are
    # not findings and go here. One stream for both is what made a failed fetch read
    # as a problem with the host.
    diagnostics = stderr if stderr is not None else sys.stderr
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))

    findings = collect(
        health=lambda: probe.collect(running_kernel=probe.running_kernel()),
        freshness=lambda: freshness_findings(base, arguments.repo_root, stderr=diagnostics),
        pins=lambda: check_pins.check(
            pins=check_pins.declared_pins(arguments.repo_root),
            playbook_text=lambda relative: _read(arguments.repo_root, relative),
            dkms_status=lambda: dkms_text(probe.run_probe),
        ),
    )
    notifier: Callable[[str], None] = (lambda _: None) if arguments.no_notify else _notify_send
    status = emit(findings, notify=notifier, write=out.write)

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
