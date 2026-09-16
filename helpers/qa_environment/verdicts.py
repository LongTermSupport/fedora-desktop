#!/usr/bin/env python3
"""Put two `qa-all.bash` runs side by side and name every stage they disagree on.

Plan 00125's subject: the same command reaches a different verdict on different machines,
and nothing surfaces that. Two failures hid behind a pass for weeks each — the `js` stage
counted ten files in a container and eight on a runner, and a gate that could not pass in
CI stopped every gate declared after it from running at all, so a stage can be *absent*
from one side rather than merely failing there.

So this compares the whole verdict LINE, not a pass/fail; it keeps EVERY line a stage
emitted rather than only the last, because an advisory dropped in favour of the pass line
below it is the same divergence hiding again; and it treats a stage present on one side
only as a difference rather than as agreement.

    python3 -m helpers.qa_environment.verdicts \\
        --here  untracked/plan-runs/.../qa-all-local.txt \\
        --there untracked/plan-runs/.../qa-all-ci.txt

Marker lines on stdout (the payload a caller keys on):

    QA-VERDICTS-COVERAGE <side> <matched> of <symbol-lines> ...
    QA-VERDICTS-DIFFERENCES <count>
    QA-VERDICTS-FAIL <reason> <side>

It renders no verdict on the differences themselves: triage establishes facts, acceptance
decides (CLAUDE/PlanTriage.md). A divergence is the finding, so it exits zero.
"""

from __future__ import annotations

import argparse
import dataclasses
import re
import sys
from typing import TextIO

#: A stage's own summary line: a status symbol, a lowercase-hyphen stage name, a colon.
#: Anchored at the start of the payload so an indented continuation line underneath a
#: finding is not read as a stage. The name is deliberately narrow — `QA FAILED:` and
#: `QA passed:` share this shape but are the RUN's verdict rather than a gate's, and
#: counting them would make every failing run differ from every passing one on a row
#: that is not a gate.
STAGE = re.compile(r"^(?P<symbol>[✓✗⚠]) (?P<name>[a-z0-9][a-z0-9-]*): (?P<detail>.*)$")

#: The run's own closing verdict. Excluded from the stage set by design — and COUNTED,
#: because a capture holding none of them never reached the end of a run.
RUN_SUMMARY = re.compile(r"^[✓✗⚠] QA (?:passed|FAILED):")

#: The OTHER way a run ends: `qa-all.bash` exits 2 on a missing tool or a gate that could
#: not produce a result, printing this to stderr and NO `QA FAILED` line. That is still a
#: terminal state — and a machine missing semgrep or ansible-playbook is exactly the
#: divergence this tool compares, so treating it as a truncated capture would refuse the
#: plan's own headline case and misreport it as a broken download.
TOOL_ABORT = re.compile(r"^ERROR: (?:Missing required tools|[a-z-]+ gate could not produce)")

#: Any line whose payload opens with a status symbol.
SYMBOL_LINE = re.compile(r"^[✓✗⚠] ")

#: The coverage DENOMINATOR. Counted with the harness prefix still ATTACHED, because a CI
#: line whose prefix stopped matching must leave the numerator without leaving the
#: denominator — otherwise coverage reads 100% with the line silently gone, a measure that
#: cannot see its own blind spot.
#:
#: Anchored, though, and that is the other half. A free `search` counts a symbol ANYWHERE,
#: which is not "this line is a verdict" but "this line mentions one": `qa-patterns.bash`
#: prints `  ✗ <file>` per failure, so a failing gate would add a phantom `unrecognised`
#: per failure — and `unrecognised` is supposed to mean the parser LOST a stage line.
#: Trading a false 100% for a false alarm is not a fix.
#:
#: So: optional BOM, any number of tab-terminated harness fields, an optional single
#: token-and-space (the timestamp), then the symbol. A timestamp whose FORMAT changed still
#: lands here; indentation and prose do not. The limit, stated rather than left to be
#: discovered: a timestamp that became TWO space-separated tokens would drop out of the
#: denominator — and out of the numerator with it, so coverage would read 100% rather than
#: wrong. No harness emits that shape today. ANSI is stripped
#: before this runs rather than tolerated inside it — numerator and denominator must agree
#: about colour or `matched + summary <= symbol_lines` stops holding.
SYMBOL_BEARING = re.compile(r"^﻿?(?:[^\t]*\t)*(?:\S+ )?[✓✗⚠] ")

#: A CI log line carries `<job>\t<step>\t<ISO timestamp> ` before the payload, and the
#: first line of a step carries a BOM. The timestamp is REQUIRED rather than optional:
#: without it the tab-delimited part alone also matches a tab inside a stage's own detail
#: text, and silently deletes the line (`scenarios=8\trunnable=8` parsed as nothing).
CI_LOG_PREFIX = re.compile(r"^﻿?(?:[^\t]*\t)+\d{4}-\d{2}-\d{2}T[\d:.]+Z ")

ANSI = re.compile(r"\x1b\[[0-9;]*m")

AGREE = "agree"
DIFFERS = "differs"
ONLY_HERE = "only-here"
ONLY_THERE = "only-there"


@dataclasses.dataclass(frozen=True)
class Verdict:
    """One line a stage emitted: the status symbol and the text after the colon."""

    symbol: str
    detail: str


@dataclasses.dataclass(frozen=True)
class Parsed:
    """One side's stages, with enough counted to tell a thin capture from a quiet run."""

    stages: dict[str, list[Verdict]]
    symbol_lines: int
    matched_lines: int
    summary_lines: int
    #: The abort MESSAGES, not a tally of them. For an instrument whose subject is machine
    #: dependence, which tool was missing is the finding — a count leaves a reader a table
    #: of `only-there` rows with no reason for any of them.
    aborts: tuple[str, ...]

    @property
    def abort_lines(self) -> int:
        return len(self.aborts)

    @property
    def terminal_lines(self) -> int:
        """Lines proving the run ENDED, however it ended."""
        return self.summary_lines + self.abort_lines


@dataclasses.dataclass(frozen=True)
class Row:
    """One stage, as each side reported it. An empty list means it did not run there."""

    name: str
    state: str
    here: list[Verdict]
    there: list[Verdict]


def parse(text: str) -> Parsed:
    """Every stage verdict in a captured `qa-all.bash` run, keyed by stage name.

    Tolerates a CI log's per-line harness prefix and ANSI colour, so one function reads a
    local capture and a downloaded workflow log. A stage that emitted more than one line
    keeps all of them, in order.

    Keeping every line replaced a "last wins" rule whose stated premise was that
    `qa-all.bash` echoes some failures to both stdout and stderr and a CI log interleaves
    the two. Under sequence comparison that premise would matter — a duplicate or a
    reordering on one side alone would now read as a difference — so it was checked rather
    than assumed: on a real CI log the only stage emitting more than one line is
    `patterns`, with the same two lines in the same order on both machines.
    """
    stages: dict[str, list[Verdict]] = {}
    aborts: list[str] = []
    symbol_lines = matched_lines = summary_lines = 0
    for raw in text.splitlines():
        uncoloured = ANSI.sub("", raw)
        bears_symbol = SYMBOL_BEARING.match(uncoloured) is not None
        line = CI_LOG_PREFIX.sub("", uncoloured).rstrip()
        if TOOL_ABORT.match(line):
            aborts.append(line)
            continue
        if bears_symbol:
            symbol_lines += 1
        if not SYMBOL_LINE.match(line):
            continue
        if RUN_SUMMARY.match(line):
            summary_lines += 1
            continue
        match = STAGE.match(line)
        if match is None:
            continue
        matched_lines += 1
        stages.setdefault(match["name"], []).append(
            Verdict(match["symbol"], match["detail"].strip())
        )
    return Parsed(
        stages=stages,
        symbol_lines=symbol_lines,
        matched_lines=matched_lines,
        summary_lines=summary_lines,
        aborts=tuple(aborts),
    )


def compare(here: dict[str, list[Verdict]], there: dict[str, list[Verdict]]) -> list[Row]:
    """Every stage either side ran, sorted by name so two reports can be diffed."""
    rows: list[Row] = []
    for name in sorted(set(here) | set(there)):
        mine, theirs = here.get(name, []), there.get(name, [])
        if not mine:
            state = ONLY_THERE
        elif not theirs:
            state = ONLY_HERE
        elif mine == theirs:
            state = AGREE
        else:
            state = DIFFERS
        rows.append(Row(name=name, state=state, here=mine, there=theirs))
    return rows


def differences(rows: list[Row]) -> list[Row]:
    """The rows that are not agreement — including a stage only one side ran."""
    return [row for row in rows if row.state != AGREE]


def _render(verdicts: list[Verdict]) -> list[str]:
    if not verdicts:
        return ["(did not run)"]
    return [f"{verdict.symbol} {verdict.detail}" for verdict in verdicts]


def _report(rows: list[Row], here_label: str, there_label: str) -> list[str]:
    lines = [
        f"{'stage':<32} {'state':<11} {here_label}",
        f"{'':<32} {'':<11} {there_label}",
        "-" * 100,
    ]
    for row in rows:
        for rendered in _render(row.here):
            lines.append(f"{row.name:<32} {row.state:<11} {rendered}")
        if row.state != AGREE:
            for rendered in _render(row.there):
                lines.append(f"{'':<32} {'':<11} {rendered}")
    return lines


def coverage_line(label: str, parsed: Parsed) -> str:
    """How much of a capture this parser accounted for, named per side.

    Printed BEFORE the table so a thin capture is visible rather than inferred from a
    wall of "did not run" rows.
    """
    unrecognised = parsed.symbol_lines - parsed.matched_lines - parsed.summary_lines
    return (
        f"QA-VERDICTS-COVERAGE {label} {parsed.matched_lines} of "
        f"{parsed.symbol_lines} symbol-prefixed line(s) "
        f"({parsed.summary_lines} run summary, {parsed.abort_lines} tool abort, "
        f"{unrecognised} unrecognised) "
        f"-> {len(parsed.stages)} stage(s)"
    )


def capture_problem(parsed: Parsed) -> str:
    """Why this capture cannot support a comparison, or "" when it can.

    An empty or truncated capture otherwise reads as agreement, or as a confident set of
    "that machine never ran this stage" rows — the misleading empty result
    CLAUDE/PlanTriage.md names, and the exact finding class this tool exists to produce.
    """
    if not parsed.stages:
        return "no-stages-parsed"
    if parsed.terminal_lines == 0:
        return "no-terminal-line"
    return ""


_PROBLEM_DETAIL = {
    "no-stages-parsed": (
        "parsed 0 stage verdicts — the capture is broken, not the machines agreeing. "
        "Check it holds a qa-all.bash run."
    ),
    "no-terminal-line": (
        "stages were parsed but nothing marks the end of the run — no closing "
        "'QA passed'/'QA FAILED' line and no 'ERROR: Missing required tools' abort — so "
        "the run did not finish or the capture is truncated. Every 'did not run' row "
        "below it would be an artefact of the truncation, not a fact about that machine."
    ),
}


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--here", required=True, help="Captured qa-all.bash output, side A.")
    parser.add_argument("--there", required=True, help="Captured qa-all.bash output, side B.")
    parser.add_argument("--here-label", default="here", help="How to name side A.")
    parser.add_argument("--there-label", default="there", help="How to name side B.")
    args = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout

    sides = {}
    for label, path in (("--here", args.here), ("--there", args.there)):
        with open(path, encoding="utf-8", errors="replace") as handle:
            sides[label] = parse(handle.read())

    for label, parsed in sides.items():
        print(coverage_line(label, parsed), file=out)
        # Named, not tallied. A side that aborted shows every later stage as absent, and
        # without this the reader has a column of `only-there` rows and no cause for them.
        for abort in parsed.aborts:
            print(f"QA-VERDICTS-ABORT {label} {abort}", file=out)

    for label, parsed in sides.items():
        problem = capture_problem(parsed)
        if problem:
            print(f"QA-VERDICTS-FAIL {problem} {label}", file=out)
            print(f"{label}: {_PROBLEM_DETAIL[problem]}", file=sys.stderr)
            return 1

    rows = compare(sides["--here"].stages, sides["--there"].stages)
    print("", file=out)
    for line in _report(rows, args.here_label, args.there_label):
        print(line, file=out)

    print(f"\nQA-VERDICTS-DIFFERENCES {len(differences(rows))}", file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
