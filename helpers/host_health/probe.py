"""Run the post-boot health probes and report what they found (Plan 00109, Task 3.1).

The half that touches the machine. `probe_results` owns every rule about what
counts as broken; this owns the order of operations, the exit status, and the one
thing a pure classifier cannot do — surviving a command that will not run.

**A probe that could not run is a finding.** This executes at the end of a login,
so an uncaught exception here takes the whole health surface down and the user sees
nothing — which looks exactly like a healthy host, and a green report over a broken
host is the failure this plan exists for. Every route out of `run_probe` therefore
ends in a `ProbeOutcome`, never a traceback.

**Silent when clean.** No findings, no output, exit 0. A check that speaks on every
login gets muted, and a muted check is not a check.

Run it: `python3 -m helpers.host_health.probe`

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-host-health.md
"""

from __future__ import annotations

import argparse
import os
import subprocess
import sys
from collections.abc import Callable
from typing import TextIO

from helpers.host_health import probe_results

#: Clean: nothing the user must act on, and nothing printed.
EXIT_OK = 0
#: Something is wrong, or could not be checked. Both are the user's business.
#: 3 rather than 1, matching `login_report`: 1 is what Python exits for an uncaught
#: exception, so a status of 1 cannot mean "the check ran and found something".
EXIT_FINDINGS = 3

#: `--no-legend` suppresses the "N loaded units listed" footer, which would otherwise
#: parse as a unit named after the count. `--plain` drops the leading bullet and
#: `--no-pager` keeps this from blocking on a pager at login.
_FAILED_UNITS = ["systemctl", "--failed", "--no-legend", "--no-pager", "--plain"]

#: How long a login is willing to wait for one probe. `systemctl` against an
#: unreachable bus can otherwise hang, and a health check that stalls the session
#: gets removed from the session.
_TIMEOUT_SECONDS = 20

Runner = Callable[[list[str]], probe_results.ProbeOutcome]


def run_probe(argv: list[str]) -> probe_results.ProbeOutcome:
    """Run one probe as an argv list and describe the result, never raising.

    Not a shell string: these take no user input today, and building the habit of a
    shell string is how that stops being true safely.
    """
    try:
        completed = subprocess.run(
            argv, capture_output=True, text=True, check=False, timeout=_TIMEOUT_SECONDS
        )
    except FileNotFoundError:
        return probe_results.ProbeOutcome(
            ok=False,
            text="",
            error=f"{argv[0]}: command not found ({' '.join(argv)})",
            # The one failure that means "this host does not have this tool" rather than
            # "this tool did not answer". `build_report` needs the two apart to decide
            # whether an absent dkms is a gap or simply a host with no DKMS subsystem.
            missing=True,
        )
    except subprocess.TimeoutExpired:
        return probe_results.ProbeOutcome(
            ok=False, text="", error=f"{argv[0]}: no answer after {_TIMEOUT_SECONDS}s"
        )
    except OSError as error:
        return probe_results.ProbeOutcome(ok=False, text="", error=f"{argv[0]}: {error}")

    if completed.returncode != 0:
        # Collapsed to one line because each finding is one line by contract, and
        # systemctl answers an unreachable bus in two. A silent failure still has to
        # say something: "could not run: " with nothing after it tells the user
        # precisely nothing.
        detail = " ".join(completed.stderr.split()) or f"exit status {completed.returncode}"
        return probe_results.ProbeOutcome(ok=False, text="", error=f"{argv[0]}: {detail}")
    return probe_results.ProbeOutcome(ok=True, text=completed.stdout, error="")


#: Where DKMS records the modules registered on this host. Read rather than assumed,
#: because "no dkms command" and "no DKMS modules" are different facts and only the pair
#: of them licenses staying silent.
DKMS_STATE_DIR = "/var/lib/dkms"


def dkms_registered_modules(state_dir: str = DKMS_STATE_DIR) -> list[str] | None:
    """Modules registered with DKMS here, or None when that could not be established.

    **None is not the empty list.** An absent state directory means this host has no DKMS
    subsystem, which is an answer; a directory that could not be read leaves the question
    open, and by this plan's standing rule an open question is not a clean result. The
    caller renders the two differently and must be able to tell them apart.

    Deliberately reads the state directory rather than asking `dkms`: the case this exists
    for is a host where the command is not installed, so anything that shells out to it
    has already lost.
    """
    try:
        entries = os.listdir(state_dir)
    except FileNotFoundError:
        return []
    except OSError:
        return None
    return sorted(
        name for name in entries if os.path.isdir(os.path.join(state_dir, name))
    )


def running_kernel() -> str:
    """The kernel that actually booted — the only one the DKMS check cares about.

    From the kernel itself rather than from `uname -r`, because a fourth subprocess
    is a fourth thing that can fail on the one fact everything else is judged against.
    """
    return os.uname().release


def collect(
    *,
    running_kernel: str,
    runner: Runner | None = None,
    dkms_registered: list[str] | None = None,
) -> probe_results.Report:
    """Run all three probes and classify what they returned.

    `runner` is the seam the tests drive; unsupplied, the real one is used. So is
    `dkms_registered` — it is discovered here rather than in `build_report` because the
    classifier is pure and this is the half that touches the machine.

    This reports on the HOST only. Phase 2's findings join in `login_report.collect`,
    which is where each check can be guarded on its own — see `build_report`.
    """
    run = runner or run_probe
    registered = dkms_registered if dkms_registered is not None else dkms_registered_modules()
    return probe_results.build_report(
        dkms=run(["dkms", "status"]),
        failed_system=run(list(_FAILED_UNITS)),
        failed_user=run([_FAILED_UNITS[0], "--user", *_FAILED_UNITS[1:]]),
        running_kernel=running_kernel,
        dkms_registered=registered,
    )


def main(
    argv: list[str] | None = None,
    *,
    stdout: TextIO | None = None,
    runner: Runner | None = None,
    kernel: str | None = None,
) -> int:
    """Print the findings, one per line, and return the exit status."""
    parser = argparse.ArgumentParser(description="Report post-boot host health findings.")
    parser.parse_args(argv)

    out = stdout if stdout is not None else sys.stdout
    report = collect(running_kernel=kernel or running_kernel(), runner=runner)
    if report.clean:
        return EXIT_OK
    # The findings ARE the payload — a caller captures them to build one
    # notification — so they go to stdout, not to stderr with the diagnostics.
    for finding in report.findings:
        out.write(f"{finding}\n")
    return EXIT_FINDINGS


if __name__ == "__main__":
    raise SystemExit(main())
