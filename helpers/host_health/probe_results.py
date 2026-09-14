"""Classify what a post-boot health probe collected (Plan 00109, Task 3.1).

Pure logic. The executor runs `dkms status` and `systemctl --failed` (system and
user) and hands the raw text in; nothing here shells out, so every rule below is
unit-testable.

Two rules the plan turns on:

1. **Silent when clean.** No findings means no output, and Task 3.2's notification
   never fires. A health check that speaks on every login gets muted, and a muted
   check is not a check.
2. **A probe that could not run is a finding, not a pass.** `dkms` missing,
   `systemctl` unavailable, output this cannot read — each becomes a finding. The
   failure this plan was written after was a green report over a broken host, so
   "I could not look" must never render as "nothing is wrong".

Deliberately NOT re-implemented here: kernel-version enumeration (Plan 00086's
subject) and the boot preflight's probing (Plan 00074). This reports what `dkms`
says about the running kernel and nothing cleverer.
"""

from __future__ import annotations

import re
from typing import NamedTuple

# `evdi/1.15.0, 7.2.4-200.fc44.x86_64, x86_64: installed`
# `evdi/1.14.16: added`  — the shape a failed autoinstall leaves behind
_DKMS_RE = re.compile(
    r"^(?P<module>[^/\s]+)/(?P<version>[^,:\s]+)"
    r"(?:,\s*(?P<kernel>[^,]+),\s*(?P<arch>[^:]+))?"
    r":\s*(?P<state>[a-z ]+)"
)


class DkmsEntry(NamedTuple):
    module: str
    version: str
    #: None when the module was added but never built for any kernel.
    kernel: str | None
    state: str


class ProbeOutcome(NamedTuple):
    ok: bool
    text: str
    error: str


class Report(NamedTuple):
    findings: tuple[str, ...]

    @property
    def clean(self) -> bool:
        return not self.findings


def parse_dkms(text: str) -> list[DkmsEntry]:
    """`dkms status` output as entries. Empty output is no modules, not an error.

    An unreadable line raises: skipping it would drop a module from the population
    silently, and a module nobody looks at reports as healthy.
    """
    entries: list[DkmsEntry] = []
    for line in text.splitlines():
        stripped = line.strip()
        if not stripped:
            continue
        match = _DKMS_RE.match(stripped)
        if match is None:
            raise ValueError(f"cannot read dkms status line {stripped!r}")
        kernel = match.group("kernel")
        entries.append(DkmsEntry(
            module=match.group("module"),
            version=match.group("version"),
            kernel=kernel.strip() if kernel else None,
            state=match.group("state").strip(),
        ))
    return entries


def dkms_findings(entries: list[DkmsEntry], *, running_kernel: str) -> list[str]:
    """Modules with no `installed` build for the kernel that actually booted.

    Per module, not per entry: a module built for several kernels is healthy as
    long as the running one is among them, and the stale entries beside it are
    normal. Reporting those would be the noise that gets the whole check muted.
    """
    healthy: set[str] = set()
    seen: set[str] = set()
    for entry in entries:
        seen.add(entry.module)
        if entry.state == "installed" and entry.kernel == running_kernel:
            healthy.add(entry.module)
    return [
        f"{module}: no DKMS module installed for the running kernel {running_kernel}"
        for module in sorted(seen - healthy)
    ]


def failed_unit_findings(text: str, *, scope: str) -> list[str]:
    """One finding per failed systemd unit, naming its scope."""
    findings: list[str] = []
    for line in text.splitlines():
        stripped = line.strip().lstrip("●").strip()
        if not stripped:
            continue
        unit = stripped.split()[0]
        findings.append(f"{unit}: failed ({scope} scope)")
    return findings


def build_report(
    *,
    dkms: ProbeOutcome,
    failed_system: str,
    failed_user: str,
    running_kernel: str,
    extra: list[str] | None = None,
) -> Report:
    """Every finding, from every source, in one report.

    `extra` is where Phase 2's play-freshness and installed-vs-pinned findings join,
    so a broken host produces one notification rather than three.
    """
    findings: list[str] = []

    if not dkms.ok:
        findings.append(f"the dkms probe could not run: {dkms.error}")
    else:
        try:
            findings.extend(dkms_findings(parse_dkms(dkms.text), running_kernel=running_kernel))
        except ValueError as error:
            # A shape this cannot read is a finding, not an exception that takes the
            # whole login-time probe down and reports nothing at all.
            findings.append(f"the dkms probe output could not be read: {error}")

    findings.extend(failed_unit_findings(failed_system, scope="system"))
    findings.extend(failed_unit_findings(failed_user, scope="user"))
    findings.extend(extra or [])
    return Report(tuple(findings))
