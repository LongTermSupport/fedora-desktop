"""Read a run transcript and judge it (Plan 00110, DESIGN.md §6.6).

Pure. The transcript is what the host captured from the guest: run.bash's own
output including its `PLAY RECAP` blocks, a `RUN-BASH-EXIT <rc>` line the host
appends when run.bash returns, and the guest acceptance script's marker lines:

    VMTEST-CHECK-PLANNED <n>                     declared before the first check
    VMTEST-CHECK <pass|fail|skip> <name> [detail]
    VMTEST-EVIDENCE <key>=<value>
    VMTEST-CHECKS-DONE total=N passed=N failed=N skipped=N

`judge` turns that into the three-valued verdict with a stage for every
non-pass. `fail` means the product ran and did not hold (run.bash non-zero,
or an assertion failed); it outranks every `error`, which covers the harness
not completing: no exit line, no recap, the acceptance script dying before its
summary, counters that disagree, or missing boot evidence.
"""

from __future__ import annotations

import re
from dataclasses import dataclass

from helpers.vmtest import scenarios

STAGE_PROVISION = "provision"
STAGE_ASSERT = "assert"
STAGE_COLLECT = "collect"

_PLANNED_RE = re.compile(r"^VMTEST-CHECK-PLANNED (\d+)$")
_CHECK_RE = re.compile(r"^VMTEST-CHECK (pass|fail|skip) (\S+)(?: (.*))?$")
_EVIDENCE_RE = re.compile(r"^VMTEST-EVIDENCE ([A-Za-z0-9_]+)=(.*)$")
_DONE_RE = re.compile(r"^VMTEST-CHECKS-DONE total=(\d+) passed=(\d+) failed=(\d+) skipped=(\d+)$")
_EXIT_RE = re.compile(r"^RUN-BASH-EXIT (\d+)$")
_RECAP_RE = re.compile(
    r"^\S+\s+:\s+ok=(\d+)\s+changed=(\d+)\s+unreachable=(\d+)\s+failed=(\d+)\s+"
    r"skipped=(\d+)\s+rescued=(\d+)\s+ignored=(\d+)\s*$"
)
_RECAP_KEYS = ("ok", "changed", "unreachable", "failed", "skipped", "rescued", "ignored")


class TranscriptError(ValueError):
    """A marker line the guest printed is not of the shape the contract defines."""


@dataclass(frozen=True)
class Check:
    status: str
    name: str
    detail: str


@dataclass(frozen=True)
class Transcript:
    planned: int | None
    checks: tuple[Check, ...]
    done: dict[str, int] | None
    run_bash_exit: int | None
    recaps: tuple[dict[str, int], ...]
    evidence: dict[str, str]


@dataclass(frozen=True)
class Judgement:
    verdict: str
    stage: str | None
    reason: str
    checks: dict[str, int | None]
    skipped_names: tuple[str, ...]


def parse(text: str) -> Transcript:
    planned = None
    checks: list[Check] = []
    done = None
    run_bash_exit = None
    recaps: list[dict[str, int]] = []
    evidence: dict[str, str] = {}
    for raw in text.splitlines():
        line = raw.rstrip("\r")
        if line.startswith("VMTEST-CHECK-PLANNED"):
            match = _PLANNED_RE.match(line)
            if not match:
                raise TranscriptError(f"malformed planned line: {line!r}")
            planned = int(match.group(1))
        elif line.startswith("VMTEST-CHECKS-DONE"):
            match = _DONE_RE.match(line)
            if not match:
                raise TranscriptError(f"malformed checks-done line: {line!r}")
            done = dict(zip(("total", "passed", "failed", "skipped"), map(int, match.groups())))
        elif line.startswith("VMTEST-CHECK "):
            match = _CHECK_RE.match(line)
            if not match:
                raise TranscriptError(f"malformed check line: {line!r}")
            checks.append(Check(status=match.group(1), name=match.group(2), detail=match.group(3) or ""))
        elif line.startswith("VMTEST-EVIDENCE"):
            match = _EVIDENCE_RE.match(line)
            if not match:
                raise TranscriptError(f"malformed evidence line: {line!r}")
            evidence[match.group(1)] = match.group(2)
        elif line.startswith("RUN-BASH-EXIT"):
            match = _EXIT_RE.match(line)
            if not match:
                raise TranscriptError(f"malformed run.bash exit line: {line!r}")
            run_bash_exit = int(match.group(1))
        else:
            match = _RECAP_RE.match(line)
            if match:
                recaps.append(dict(zip(_RECAP_KEYS, map(int, match.groups()))))
    return Transcript(
        planned=planned,
        checks=tuple(checks),
        done=done,
        run_bash_exit=run_bash_exit,
        recaps=tuple(recaps),
        evidence=evidence,
    )


def _counted(checks: tuple[Check, ...]) -> dict[str, int]:
    return {
        "total": len(checks),
        "passed": sum(1 for c in checks if c.status == "pass"),
        "failed": sum(1 for c in checks if c.status == "fail"),
        "skipped": sum(1 for c in checks if c.status == "skip"),
    }


def judge(transcript: Transcript, *, planned: int, max_skipped: int) -> Judgement:
    """§6.6: every non-pass names a stage; fail outranks error; nothing passes by absence."""
    counted = _counted(transcript.checks)
    summary: dict[str, int | None] = {"planned": planned}
    if transcript.done is not None:
        summary.update(transcript.done)
    else:
        summary.update({"total": None, "passed": None, "failed": None, "skipped": None})
    skipped_names = tuple(c.name for c in transcript.checks if c.status == "skip")

    def result(verdict: str, stage: str | None, reason: str) -> Judgement:
        return Judgement(verdict=verdict, stage=stage, reason=reason, checks=summary, skipped_names=skipped_names)

    # --- product failures first: they are the more informative verdict ---------------
    if transcript.run_bash_exit is not None and transcript.run_bash_exit != 0:
        return result(
            scenarios.VERDICT_FAIL,
            STAGE_PROVISION,
            f"run.bash exit {transcript.run_bash_exit}: provisioning failed in the guest",
        )
    failed_checks = [c for c in transcript.checks if c.status == "fail"]
    if failed_checks:
        names = ", ".join(f"{c.name}{' (' + c.detail + ')' if c.detail else ''}" for c in failed_checks)
        return result(scenarios.VERDICT_FAIL, STAGE_ASSERT, f"{len(failed_checks)} check(s) failed: {names}")

    # --- the harness must have completed provisioning ---------------------------------
    if transcript.run_bash_exit is None:
        return result(scenarios.VERDICT_ERROR, STAGE_PROVISION, "run.bash never reported an exit status")
    if not transcript.recaps:
        return result(scenarios.VERDICT_ERROR, STAGE_PROVISION, "no PLAY RECAP in the transcript; nothing was provisioned")
    broken = [r for r in transcript.recaps if r["failed"] > 0 or r["unreachable"] > 0]
    if broken:
        return result(
            scenarios.VERDICT_ERROR,
            STAGE_PROVISION,
            "a PLAY RECAP reports failed or unreachable hosts but run.bash exited 0; the two disagree",
        )
    if sum(r["ok"] for r in transcript.recaps) == 0:
        return result(scenarios.VERDICT_ERROR, STAGE_PROVISION, "every PLAY RECAP has ok=0; nothing ran")

    # --- the acceptance script must have declared, run and summarised -----------------
    if transcript.planned is None:
        return result(scenarios.VERDICT_ERROR, STAGE_ASSERT, "the acceptance script never declared its planned check count")
    if transcript.planned != planned:
        return result(
            scenarios.VERDICT_ERROR,
            STAGE_ASSERT,
            f"the guest script declares {transcript.planned} planned checks but the manifest says {planned}; one is stale",
        )
    if transcript.done is None:
        return result(
            scenarios.VERDICT_ERROR,
            STAGE_ASSERT,
            f"the acceptance script did not finish: {counted['total']} of {planned} checks printed, no summary",
        )
    if transcript.done != counted:
        return result(
            scenarios.VERDICT_ERROR,
            STAGE_ASSERT,
            f"summary {transcript.done} and the check lines {counted} disagree",
        )

    # --- evidence that a guest actually booted ----------------------------------------
    if not transcript.evidence.get("boot_id"):
        return result(scenarios.VERDICT_ERROR, STAGE_COLLECT, "no boot_id in the evidence; a run that booted nothing cannot pass")

    judgement = scenarios.judge_checks(
        planned=planned,
        total=transcript.done["total"],
        passed=transcript.done["passed"],
        failed=transcript.done["failed"],
        skipped=transcript.done["skipped"],
        max_skipped=max_skipped,
    )
    if judgement.verdict == scenarios.VERDICT_PASS:
        return result(scenarios.VERDICT_PASS, None, judgement.reason)
    if judgement.verdict == scenarios.VERDICT_FAIL:
        return result(scenarios.VERDICT_FAIL, STAGE_ASSERT, judgement.reason)
    return result(scenarios.VERDICT_ERROR, STAGE_ASSERT, judgement.reason)
