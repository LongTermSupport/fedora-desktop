"""Ask the host-health collector to run again, once a play run has been ledgered.

The Fedora Desktop panel and the terminal report both read one document, written only
by `helpers.host_health.login_report`. Its `play-freshness` section compares the ledger
against git, so a play run that fixes a finding leaves the document — and the icon —
describing the host as it was before the fix until something regenerates it. The ledger
write is the moment that fact changes, so this is the trigger, called by the callback
plugin after the records are on disk. A timer is the backstop for drift that no play run
announces (a `git pull`).

**It cannot fail a play, and that is a decision rather than a dodge.** Ansible discards an
exception raised inside a callback, so a raise here would be silence, not a failed run.
And the refresh is reporting-only: nothing it does changes the host, and a missed trigger
costs bounded staleness (the timer's next run, with the document's `generated_at`
showing its age), not a wrong answer. So failure is a returned line the plugin prints to
stderr — loud, named, never swallowed — and never an exception.

Stdlib only; `subprocess` is injected so the decisions are testable.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md §4
"""

from __future__ import annotations

import subprocess
from collections.abc import Callable

#: The systemd --user unit that regenerates the status document. Deployed by
#: `play-host-health-login-report.yml` (its `collect_service` var), on both profiles; a
#: test pins the two spellings together.
COLLECT_UNIT = "host-health-collect.service"

#: Printed to stderr when the refresh could not be requested.
FAILURE_MARKER = "HEALTH-REFRESH-FAILED"

#: `systemctl` against an unreachable bus can hang, and this runs inside the operator's
#: playbook run.
_TIMEOUT_SECONDS = 10

Runner = Callable[..., subprocess.CompletedProcess]


def _failure(detail: str) -> str:
    return (
        f"{FAILURE_MARKER}: {detail} — the host status document was not refreshed after "
        f"this run, so the Fedora Desktop panel may show a stale result until the next "
        f"scheduled collection"
    )


def _systemctl(run: Runner, *args: str) -> subprocess.CompletedProcess | str:
    """One `systemctl --user` call; the completed process, or the failure line."""
    argv = ["systemctl", "--user", *args]
    try:
        completed = run(
            argv, capture_output=True, text=True, check=False, timeout=_TIMEOUT_SECONDS
        )
    except FileNotFoundError:
        return _failure("systemctl: command not found")
    except subprocess.TimeoutExpired:
        return _failure(f"systemctl: no answer after {_TIMEOUT_SECONDS}s")
    except OSError as error:
        return _failure(f"systemctl: {error}")
    if completed.returncode != 0:
        detail = " ".join(completed.stderr.split()) or f"exit status {completed.returncode}"
        return _failure(f"{' '.join(argv)}: {detail}")
    return completed


def request(run: Runner = subprocess.run) -> str | None:
    """Restart the collector without waiting for it; the failure line, or None.

    `restart`, not `start`: a collection the timer began before the ledger write has
    already read the old ledger, and `start` on a running unit does nothing — the
    document would keep the pre-run answer for another interval.

    A host that never deployed the unit is skipped silently. The report is an optional
    play, and with no unit there is no document this run could have left stale.
    """
    probed = _systemctl(run, "show", "--property=LoadState", "--value", COLLECT_UNIT)
    if isinstance(probed, str):
        return probed
    if probed.stdout.strip() == "not-found":
        return None
    restarted = _systemctl(run, "restart", "--no-block", COLLECT_UNIT)
    return restarted if isinstance(restarted, str) else None
