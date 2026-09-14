"""Write the findings into a file a human can hand to Claude Code (Plan 00109, 3.3).

The session that diagnosed the 2026-09-11 failure was manual archaeology — reading
journal logs, comparing `dkms status` against a pin set months earlier, working out
which kernel had booted. This exists so that is not repeated from scratch.

Two constraints from the plan's Non-Goals, and they are why this writes a file
rather than launching anything:

1. **The handoff is offered, never automatic.** Nothing here starts a process. The
   file names the command and a human runs it.
2. **Nothing is auto-applied.** The prompt asks for a diagnosis and a discussion,
   never a fix — re-running a play is always a human decision.

And one property that is this plan's own subject: the file separates *"this is
broken"* from *"this was not looked at"*. A list that mixes them and distinguishes
neither reads like a complete picture of a machine, which is exactly how the
incident happened.
"""

from __future__ import annotations

import os

from helpers.host_health import probe_results

#: Written beside the ledger so it travels with the rest of this plan's state.
FILE_NAME = "host-health-findings.md"

def _split(
    findings: list[probe_results.Finding],
) -> tuple[list[str], list[str]]:
    """Split on what each finding SAYS it is, never on how it is worded.

    Matching the prose could not do this. Two substrings — "could not run" and "could
    not be checked" — covered seven of the messages the three checks emit and missed
    six, and every one of the six landed under *"What is wrong"*: a file whose whole
    purpose is keeping those apart, telling the reader that things nobody had looked at
    were known faults. `Finding.checked` is the producers' own answer, so a new message
    cannot be misfiled by being phrased differently.
    """
    broken = [f.text for f in findings if f.checked]
    unchecked = [f.text for f in findings if not f.checked]
    return broken, unchecked


def render(*, findings: list[probe_results.Finding], kernel: str, at: str) -> str:
    """The prompt file's content.

    Raises on an empty list: there is nothing to hand off on a healthy host, and an
    empty prompt file would be left to be found later and believed.
    """
    if not findings:
        raise ValueError("no findings, so there is nothing to hand off")

    broken, unchecked = _split(findings)
    lines = [
        "# Host health findings",
        "",
        f"Collected at **{at}** by `helpers.host_health.login_report`, running on the",
        f"kernel that actually booted: **{kernel}**.",
        "",
    ]

    if broken:
        lines += ["## What is wrong", ""]
        lines += [f"- {finding}" for finding in broken]
        lines += [""]

    if unchecked:
        lines += [
            "## What could not be checked",
            "",
            "These are **not** clean results. A check that could not run tells you nothing",
            "about the thing it was meant to look at, and this plan exists because a green",
            "report over a broken host is indistinguishable from a healthy one.",
            "",
        ]
        lines += [f"- {finding}" for finding in unchecked]
        lines += [""]

    lines += [
        "## What to do with this",
        "",
        "Read the findings above and work out what happened and why. The evidence you will",
        "want is in this repo: the play that owns each affected thing, the pin it declares,",
        "and this plan's `JOURNAL/` for the original incident.",
        "",
        "**Do not apply a fix, and do not run a playbook.** Re-running a play is always the",
        "operator's decision, and this repo is strict IaC: any change goes into a playbook",
        "and the human runs it. Diagnose, explain, and propose — then stop.",
        "",
    ]
    return "\n".join(lines)


def write(
    base: str, *, findings: list[probe_results.Finding], kernel: str, at: str
) -> str:
    """Write the prompt file and return its path.

    Overwrites: a stale handoff describing a break that is already fixed is worse
    than none, because it is a confident description of a machine that has moved on.
    Mode 0600 — it records what is broken about this host.
    """
    text = render(findings=findings, kernel=kernel, at=at)
    os.makedirs(base, exist_ok=True)
    path = os.path.join(base, FILE_NAME)
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
        handle.write(text)
    return path


def offer(path: str) -> str:
    """The line that offers the handoff. A string — it never launches anything.

    `claude`, not `ccy`: diagnosing a broken host from inside a container cannot
    see the host.
    """
    return f"To discuss this with Claude Code: claude '{path}'"
