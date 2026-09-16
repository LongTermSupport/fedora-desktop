#!/usr/bin/env python3
"""Put two `qa-all.bash` runs side by side and name every stage they disagree on.

Plan 00125's subject: the same command reaches a different verdict on different machines,
and nothing surfaces that. Two failures hid behind a pass for weeks each — the `js` stage
counted ten files in a container and eight on a runner, and a gate that could not pass in
CI stopped every gate declared after it from running at all, so a stage can be *absent*
from one side rather than merely failing there.

So this compares the whole verdict LINE, not a pass/fail, and it treats a stage present on
one side only as a difference rather than as agreement.

    python3 -m helpers.qa_environment.verdicts \\
        --here  untracked/plan-runs/.../qa-all-local.txt \\
        --there untracked/plan-runs/.../qa-all-ci.txt

Marker lines on stdout (the payload a caller keys on):

    QA-VERDICTS-DIFFERENCES <count>
    QA-VERDICTS-FAIL <reason>

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

#: A CI log line carries `<job>\t<step>\t<ISO timestamp> ` before the payload, and the
#: first line of a step carries a BOM. The timestamp is separated from the payload by a
#: SPACE rather than a tab, so consuming tab-delimited fields alone leaves it behind.
CI_LOG_PREFIX = re.compile(r"^﻿?(?:[^\t]*\t)+(?:\d{4}-\d{2}-\d{2}T[\d:.]+Z )?")

ANSI = re.compile(r"\x1b\[[0-9;]*m")

AGREE = "agree"
DIFFERS = "differs"
ONLY_HERE = "only-here"
ONLY_THERE = "only-there"


@dataclasses.dataclass(frozen=True)
class Verdict:
    """One stage's reported outcome: the status symbol and the text after the colon."""

    symbol: str
    detail: str


@dataclasses.dataclass(frozen=True)
class Row:
    """One stage, as each side reported it. `here`/`there` are None where it did not run."""

    name: str
    state: str
    here: Verdict | None
    there: Verdict | None


def parse(text: str) -> dict[str, Verdict]:
    """Every stage verdict in a captured `qa-all.bash` run, keyed by stage name.

    Tolerates a CI log's per-line harness prefix and ANSI colour so the same function
    reads a local capture and a downloaded workflow log.
    """
    found: dict[str, Verdict] = {}
    for raw in text.splitlines():
        line = ANSI.sub("", CI_LOG_PREFIX.sub("", raw)).rstrip()
        match = STAGE.match(line)
        if match is None:
            continue
        # Last wins: qa-all.bash echoes some failures to both stdout and stderr, and a
        # CI log interleaves the two into one stream.
        found[match["name"]] = Verdict(match["symbol"], match["detail"].strip())
    return found


def compare(here: dict[str, Verdict], there: dict[str, Verdict]) -> list[Row]:
    """Every stage either side ran, sorted by name so two reports can be diffed."""
    rows: list[Row] = []
    for name in sorted(set(here) | set(there)):
        mine, theirs = here.get(name), there.get(name)
        if mine is None:
            state = ONLY_THERE
        elif theirs is None:
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


def _render(row: Verdict | None) -> str:
    return "(did not run)" if row is None else f"{row.symbol} {row.detail}"


def _report(rows: list[Row], here_label: str, there_label: str) -> list[str]:
    lines = [
        f"{'stage':<32} {'state':<11} {here_label}",
        f"{'':<32} {'':<11} {there_label}",
        "-" * 100,
    ]
    for row in rows:
        lines.append(f"{row.name:<32} {row.state:<11} {_render(row.here)}")
        if row.state != AGREE:
            lines.append(f"{'':<32} {'':<11} {_render(row.there)}")
    return lines


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

    # An empty capture reads as "no divergence", which is the misleading-empty-result
    # trap CLAUDE/PlanTriage.md names: it would report agreement having compared nothing.
    for label, stages in sides.items():
        if not stages:
            print(f"QA-VERDICTS-FAIL no-stages-parsed {label}", file=out)
            print(
                f"parsed 0 stage verdicts from {label} — the capture is broken, not the "
                "machines agreeing. Check it holds a qa-all.bash run.",
                file=sys.stderr,
            )
            return 1

    rows = compare(sides["--here"], sides["--there"])
    for line in _report(rows, args.here_label, args.there_label):
        print(line, file=out)

    print(f"\nQA-VERDICTS-DIFFERENCES {len(differences(rows))}", file=out)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
