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

from helpers.host_health import probe, probe_results
from helpers.play_ledger import check_freshness, ledger
from helpers.version_pins import check_pins

#: Clean: nothing the user must act on, and nothing shown.
EXIT_OK = 0
#: Something is wrong, or could not be checked. Both are the user's business.
EXIT_FINDINGS = 1

_SUMMARY = "fedora-desktop: this machine needs attention"
_TIMEOUT_SECONDS = 15


def message(findings: list[str]) -> str:
    """The notification body. Raises on an empty list rather than sending nothing."""
    if not findings:
        raise ValueError("no findings, so there is no notification to send")
    count = len(findings)
    noun = "finding" if count == 1 else "findings"
    body = "\n".join(f"• {finding}" for finding in findings)
    return f"{count} {noun}:\n{body}"


def collect(
    *,
    health: Callable[[], probe_results.Report],
    freshness: Callable[[], list[str]],
    pins: Callable[[], list[str]],
) -> list[str]:
    """Every finding from every check, host health first.

    Each check is called independently and separately guarded. Chaining them — or
    letting one exception escape — would let a single broken check hide the others,
    which is the failure mode this whole plan is about one level up.

    Host-health findings come first: something broken on this machine now outranks
    something that has merely drifted.
    """
    findings: list[str] = []

    try:
        findings.extend(health().findings)
    except Exception as error:
        findings.append(f"the post-boot health probe could not run: {error}")

    for label, check in (("play-freshness", freshness), ("installed-vs-pinned", pins)):
        try:
            findings.extend(check())
        except Exception as error:
            # Named, so the user learns WHICH check stopped working rather than
            # that something, somewhere, did.
            findings.append(f"the {label} check could not run: {error}")

    return findings


def emit(
    findings: list[str],
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
        write(f"{finding}\n")
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


def _freshness_findings(base: str, repo_root: str) -> list[str]:
    """The freshness check's own output, as lines, via its real entry point."""
    captured: list[str] = []

    class _Sink:
        def write(self, text: str) -> None:
            captured.append(text)

    status = check_freshness.run(
        base=base, repo_root=repo_root, stdout=_Sink(), stderr=_Sink()
    )
    if status == check_freshness.EXIT_UNTRUSTWORTHY:
        # "I cannot tell you" is a finding. It is the state the BROKEN sentinel and
        # an unresolvable ledgered commit produce, and reporting it as clean is the
        # exact defect this plan exists for.
        return [
            "play-freshness could not give an answer, so no play was judged "
            "(see its stderr output above)"
        ]
    if status == check_freshness.EXIT_OK:
        return []
    return [line.rstrip("\n") for line in "".join(captured).splitlines() if line.strip()]


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=_repo_root_default())
    parser.add_argument(
        "--no-notify",
        action="store_true",
        help="report on stdout only; for running it by hand",
    )
    arguments = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))

    findings = collect(
        health=lambda: probe.collect(running_kernel=probe.running_kernel()),
        freshness=lambda: _freshness_findings(base, arguments.repo_root),
        pins=lambda: check_pins.check(
            pins=_declared_pins(arguments.repo_root),
            playbook_text=lambda relative: _read(arguments.repo_root, relative),
            dkms_status=lambda: dkms_text(probe.run_probe),
        ),
    )
    notifier: Callable[[str], None] = (lambda _: None) if arguments.no_notify else _notify_send
    return emit(findings, notify=notifier, write=out.write)


def _read(root: str, relative: str) -> str:
    with open(os.path.join(root, relative), encoding="utf-8") as handle:
        return handle.read()


def _declared_pins(root: str):
    """The pin manifest, converted outside the stdlib-only helpers as ever."""
    import json

    from helpers.version_pins import manifest

    decoded = json.loads(
        subprocess.run(
            [
                sys.executable, "-c",
                "import json,sys,yaml; json.dump(yaml.safe_load(open(sys.argv[1])), sys.stdout)",
                os.path.join(root, "vars", "version-pins.yml"),
            ],
            check=True, capture_output=True, text=True, timeout=_TIMEOUT_SECONDS,
        ).stdout
    )
    return manifest.parse(decoded, require_installed=True)


if __name__ == "__main__":
    raise SystemExit(main())
