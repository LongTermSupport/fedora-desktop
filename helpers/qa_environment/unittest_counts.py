#!/usr/bin/env python3
"""Run named unittest modules and report how many tests ran and how many skipped —
as DATA in a file, not as prose for the caller to scrape back out of a stream.

Plan 00125's subject is a check whose clean result is indistinguishable from a blind
one. `unittest` counts a SKIPPED test inside `testsRun`, so `Ran 1464 tests` is
byte-identical whether a test asserted or skipped itself. Two machines running
different subsets of the same suite printed the same sentence for weeks, and the skip
count is the only thing that separates them.

WHY THE COUNTS ARE NOT SCRAPED FROM THE OUTPUT. Four readers were written against the
captured text and all four were wrong. The last of them parsed stderr alone, on the
claim that unittest's summary is always last there; a test that registers an `atexit`
handler printing to stderr disproves it, and that reader answered `Ran 3 tests` / `99`
for a run whose truth was `Ran 1 test` / `0`. A test can write anything to either
stream, in any order, so the text cannot be the source of truth — but the
`TestResult` object holds both numbers exactly, and nothing a test prints can alter
it. This writes those numbers to a path the CALLER names, so the payload never shares
a channel with anything a test can reach.

    python3 -m helpers.qa_environment.unittest_counts \\
        --counts-file /tmp/counts --counts-token <nonce> \\
        tests.helpers.pyenv.test_resolver ...

The counts file, whose whole format this is:

    token=<whatever --counts-token was given, omitted if it was not>
    tests=1464
    skipped=1
    modules=64

Every key is always present: an absent key and a zero must not look alike, because one
means a clean run and the other means the reader has gone blind. The human-readable run
goes to stderr exactly as `python3 -m unittest` writes it, so an operator reading a
failure sees the same tracebacks as before.

THE FILE IS NOT UNREACHABLE, only much harder to reach by accident. Its path travels in
`argv`, so a test that reads `sys.argv` can find and overwrite it — and this module's own
tests are themselves collected by the runner they test. `--counts-token` is why the reader
can tell: the caller passes a value only it knows, requires it back, and a clobbered file
then FAILS the gate instead of reporting a number nobody checked. That is a guard against
accident, not against a test that is actively trying; the streams, by contrast, could be
hit with an ordinary `print`.

Read by `helper_counts_summary` in `scripts/lib/qa-helper-summary.bash`, which refuses
a file that is missing, short a key, or not all digits.
"""

from __future__ import annotations

import argparse
import pathlib
import sys
import unittest


def counts_text(result, module_count, token):
    """Render a `TestResult`'s authoritative numbers as the counts file's content.

    `testsRun` INCLUDES skipped tests — that is the whole reason the skip count has to
    be reported alongside it rather than inferred from it.

    The two are NOT commensurable and neither is clamped to the other: `testsRun`
    counts test METHODS while `skipped` counts skip EVENTS, and one method registering
    several `subTest` skips reports more skips than tests. unittest's own summary says
    the same, so adjusting either here would make this file disagree with the run.

    `modules` is carried because a machine that COLLECTED a different set is the same
    defect one level up, and the test count alone cannot show it.

    `token` is whatever the caller asked to have echoed back, or None.
    """
    lines = []
    if token is not None:
        lines.append(f"token={token}")
    lines.append(f"tests={result.testsRun}")
    lines.append(f"skipped={len(result.skipped)}")
    lines.append(f"modules={module_count}")
    return "".join(f"{line}\n" for line in lines)


def main(argv=None, stream=None):
    """Run the named modules, write the counts file, return unittest's own verdict."""
    parser = argparse.ArgumentParser(
        prog="unittest_counts",
        description="Run unittest modules and record the run's size and skip count.",
    )
    parser.add_argument(
        "--counts-file",
        required=True,
        help="path to write the counts to, after the run completes",
    )
    parser.add_argument(
        "--counts-token",
        default=None,
        help="opaque value to echo back as `token=`, so the caller can tell its own "
        "counts file from one a test overwrote",
    )
    parser.add_argument(
        "modules",
        nargs="+",
        help="dotted module names, e.g. tests.helpers.pyenv.test_resolver",
    )
    # argparse raises SystemExit(2) on a missing --counts-file or an empty module list.
    # That is the intended behaviour: `Ran 0 tests ... OK` exiting 0 is the false pass
    # helpers/CLAUDE.md warns about, so an empty invocation must never look like a run.
    args = parser.parse_args(argv)

    suite = unittest.TestLoader().loadTestsFromNames(args.modules)
    runner = unittest.TextTestRunner(stream=stream if stream is not None else sys.stderr)
    result = runner.run(suite)

    # `wasSuccessful()` rather than `failures or errors`: an unexpected success alone
    # leaves both empty while unittest itself reports FAILED, so checking the lists
    # directly would turn a red suite green.
    pathlib.Path(args.counts_file).write_text(
        counts_text(result, len(args.modules), args.counts_token), encoding="utf-8"
    )

    return 0 if result.wasSuccessful() else 1


if __name__ == "__main__":
    sys.exit(main())
