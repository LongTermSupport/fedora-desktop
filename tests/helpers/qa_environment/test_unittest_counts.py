"""Unit tests for helpers/qa_environment/unittest_counts.py — reporting a unittest run's
size and skip count as DATA rather than as prose to be scraped back.

Run from the repo root:

    python3 -m unittest tests.helpers.qa_environment.test_unittest_counts

WHY THIS MODULE EXISTS AT ALL. `unittest` counts a SKIPPED test inside `testsRun`, so
`Ran 1464 tests` is byte-identical whether a test asserted or skipped itself, and two
machines running different subsets of the same suite printed the same sentence for weeks.
That is Plan 00125's subject. The skip count is the only thing that separates them.

Four attempts were made to read that count out of the captured text, and all four were
wrong, each in a way a hand-check could not see:

  1. `\\(skipped=[0-9]+\\)` missed `OK (skipped=1, expected failures=1)` — reported 0.
  2. The match ran over the whole capture and bash `=~` takes the FIRST hit, so any
     earlier `skipped=<digits>` won. This repo's own fixtures contain that text.
  3. "Last match wins" over a merged `2>&1` capture: Python BLOCK-BUFFERS stdout to a
     pipe, so a small decoy flushes at exit and lands AFTER unittest's summary while a
     decoy padded past 8KB flushes early and lands BEFORE it. Both reachable, so no
     first-or-last rule over a merged stream can be right.
  4. "Last match wins over STDERR alone", on the claim that unittest's summary is always
     last there. It is not: a test that registers an `atexit` handler printing to stderr
     puts its text after the summary, and the readers then answered `Ran 3 tests` / `99`
     for a run whose truth was `Ran 1 test` / `0`.

The counts are not in the text. They are in the `TestResult` object, exactly, and this
module reads them from there and writes them to a file the caller names. No stream a test
can write to is parsed, so there is no fifth ordering to get wrong.
"""

from __future__ import annotations

import contextlib
import io
import os
import pathlib
import subprocess
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.qa_environment import unittest_counts


class FakeResult:
    """Only the attributes `counts_text` is allowed to read.

    `testsRun` keeps unittest's own spelling because that is the attribute name the
    production code reads; renaming it here would let a rename in unittest pass.
    """

    def __init__(self, tests_run, skipped):
        self.testsRun = tests_run
        self.skipped = skipped


class TestCountsText(unittest.TestCase):
    def test_the_counts_are_rendered_as_key_value_lines(self):
        self.assertEqual(
            unittest_counts.counts_text(FakeResult(1464, [("t", "why")]), 64, "n0nce"),
            "token=n0nce\ntests=1464\nskipped=1\nmodules=64\n",
        )

    def test_a_run_with_no_skips_says_zero_rather_than_omitting_the_key(self):
        # An absent key and a zero must not look alike to the reader: one is a clean run,
        # the other is a reader that has gone blind. The bash side refuses a missing key.
        self.assertEqual(
            unittest_counts.counts_text(FakeResult(3, []), 2, "t"),
            "token=t\ntests=3\nskipped=0\nmodules=2\n",
        )

    def test_the_skip_count_is_the_length_of_the_skipped_list(self):
        # Not scraped from anywhere, and not derived from testsRun.
        result = FakeResult(10, [("a", "r"), ("b", "r"), ("c", "r")])
        self.assertEqual(
            unittest_counts.counts_text(result, 1, "t"), "token=t\ntests=10\nskipped=3\nmodules=1\n"
        )

    def test_the_token_line_is_omitted_when_no_token_was_asked_for(self):
        # A standalone run has no caller to prove the file's provenance to.
        self.assertEqual(
            unittest_counts.counts_text(FakeResult(3, []), 1, None), "tests=3\nskipped=0\nmodules=1\n"
        )

    def test_a_skip_count_above_the_test_count_is_rendered_faithfully(self):
        # NOT commensurable, and the file must not "correct" it. `testsRun` counts test
        # METHODS; `skipped` counts skip EVENTS, and one method can register several via
        # subTest. unittest's own summary says the same thing, so clamping here would make
        # the counts file disagree with the run it describes.
        self.assertEqual(
            unittest_counts.counts_text(FakeResult(1, [("a", "r"), ("b", "r"), ("c", "r")]), 1, "t"),
            "token=t\ntests=1\nskipped=3\nmodules=1\n",
        )


class TestRunEndToEnd(unittest.TestCase):
    """These drive `main` against a real suite in a temporary directory, because the
    property under test — that the numbers survive a test trying to forge them — is a
    property of the production path and not of a rendering function."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        sys.path.insert(0, str(self.tmp))
        self.addCleanup(lambda: sys.path.remove(str(self.tmp)))
        self.stream = io.StringIO()

    def write_module(self, name, body):
        (self.tmp / f"{name}.py").write_text(textwrap.dedent(body), encoding="utf-8")
        return name

    def run_main(self, *modules):
        counts_file = self.tmp / "counts"
        code = unittest_counts.main(
            ["--counts-file", str(counts_file), *modules], stream=self.stream
        )
        return code, counts_file

    def test_a_passing_suite_reports_its_size_and_exits_zero(self):
        module = self.write_module(
            "t_pass",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.assertTrue(True)

                def test_two(self):
                    self.assertTrue(True)
            """,
        )
        code, counts_file = self.run_main(module)
        self.assertEqual(code, 0)
        self.assertEqual(counts_file.read_text(encoding="utf-8"), "tests=2\nskipped=0\nmodules=1\n")

    def test_a_skip_is_counted_where_the_text_summary_would_hide_it(self):
        # The defect this plan exists to remove, stated as a test: `Ran 2 tests` is what
        # unittest prints for BOTH of these suites. Only the skip count separates them.
        module = self.write_module(
            "t_skip",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.assertTrue(True)

                @unittest.skip("no hardware here")
                def test_two(self):
                    pass
            """,
        )
        code, counts_file = self.run_main(module)
        self.assertEqual(code, 0)
        self.assertEqual(counts_file.read_text(encoding="utf-8"), "tests=2\nskipped=1\nmodules=1\n")

    def test_a_runtime_skip_counts_the_same_as_a_decorated_one(self):
        module = self.write_module(
            "t_runtime_skip",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.skipTest("no DP connector")
            """,
        )
        _, counts_file = self.run_main(module)
        self.assertEqual(counts_file.read_text(encoding="utf-8"), "tests=1\nskipped=1\nmodules=1\n")

    def test_a_failing_suite_exits_non_zero_and_still_reports_its_counts(self):
        module = self.write_module(
            "t_fail",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.fail("deliberate")

                @unittest.skip("why not")
                def test_two(self):
                    pass
            """,
        )
        code, counts_file = self.run_main(module)
        self.assertEqual(code, 1)
        self.assertEqual(counts_file.read_text(encoding="utf-8"), "tests=2\nskipped=1\nmodules=1\n")

    def test_an_unexpected_success_fails_the_run_as_unittest_itself_would(self):
        module = self.write_module(
            "t_unexpected",
            """
            import unittest

            class T(unittest.TestCase):
                @unittest.expectedFailure
                def test_one(self):
                    pass
            """,
        )
        code, _ = self.run_main(module)
        self.assertEqual(code, 1)

    def test_the_human_readable_run_still_reaches_the_stream(self):
        # The counts file replaces the SCRAPE, not the operator's output. A failing run
        # must still print its traceback where a person can read it.
        module = self.write_module(
            "t_traceback",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.fail("the distinguishing message")
            """,
        )
        self.run_main(module)
        self.assertIn("the distinguishing message", self.stream.getvalue())

    def test_a_module_that_cannot_be_imported_is_a_failure_not_an_empty_run(self):
        # `loadTestsFromNames` turns an import error into a synthetic failing test rather
        # than raising, so the guard is that the run is NOT reported as a clean zero.
        code, counts_file = self.run_main("t_does_not_exist")
        self.assertEqual(code, 1)
        self.assertNotEqual(counts_file.read_text(encoding="utf-8"), "tests=0\nskipped=0\nmodules=1\n")


class TestAgainstARealSubprocess(unittest.TestCase):
    """The decoy scenario has to be a real process, not an in-process run.

    The three properties that defeated the previous readers only exist in one: Python
    block-buffers stdout when it is a PIPE (so a padded decoy flushes early and an
    unpadded one flushes at exit), and `atexit` handlers run at INTERPRETER shutdown,
    after unittest's summary — neither is reproducible inside an already-running test.
    It also exercises `__main__` and the real exit status rather than `main`'s return.
    """

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.tmp = pathlib.Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        self.repo_root = pathlib.Path(__file__).resolve().parents[3]

    def run_module_in_subprocess(self, name, body, token=None):
        (self.tmp / f"{name}.py").write_text(textwrap.dedent(body), encoding="utf-8")
        counts_file = self.tmp / "counts"
        env = dict(os.environ, PYTHONPATH=f"{self.repo_root}:{self.tmp}")
        token_args = ["--counts-token", token] if token is not None else []
        completed = subprocess.run(
            [
                sys.executable,
                "-m",
                "helpers.qa_environment.unittest_counts",
                "--counts-file",
                str(counts_file),
                *token_args,
                name,
            ],
            cwd=self.repo_root,
            env=env,
            capture_output=True,
            text=True,
            check=False,  # the exit status IS the assertion in some cases below
        )
        return completed, counts_file

    def test_a_test_forging_unittest_output_on_both_streams_cannot_move_the_numbers(self):
        # Every previous reader was defeated by some part of this one module. The stdout
        # copy is padded past the 8KB pipe buffer so it flushes EARLY (revision 3's
        # defeat); the atexit handler writes a result line to stderr AFTER unittest's
        # summary (revision 4's defeat); and there is a plain stderr decoy mid-run too.
        completed, counts_file = self.run_module_in_subprocess(
            "t_decoy",
            """
            import atexit
            import sys
            import unittest

            def _late():
                print("Ran 3 tests in 9.999s", file=sys.stderr)
                print("OK (skipped=99)", file=sys.stderr)

            class T(unittest.TestCase):
                def test_one(self):
                    atexit.register(_late)
                    print("Ran 7 tests in 0.1s")
                    print("OK (skipped=42)")
                    print("X" * 9000)
                    print("Ran 5 tests in 0.2s", file=sys.stderr)
                    print("FAILED (skipped=13)", file=sys.stderr)
                    self.assertTrue(True)
            """,
        )
        self.assertEqual(completed.returncode, 0)
        self.assertEqual(counts_file.read_text(encoding="utf-8"), "tests=1\nskipped=0\nmodules=1\n")

    def test_the_forged_lines_really_do_reach_the_streams(self):
        # Without this the test above could pass because the decoy never ran. It asserts
        # the attack is live: the text IS in the output, and the counts are right anyway.
        completed, _ = self.run_module_in_subprocess(
            "t_decoy_live",
            """
            import atexit
            import sys
            import unittest

            def _late():
                print("OK (skipped=99)", file=sys.stderr)

            class T(unittest.TestCase):
                def test_one(self):
                    atexit.register(_late)
                    print("OK (skipped=42)")
                    self.assertTrue(True)
            """,
        )
        self.assertIn("OK (skipped=42)", completed.stdout)
        # After unittest's own summary, which is the ordering that broke revision 4.
        self.assertTrue(completed.stderr.rstrip().endswith("OK (skipped=99)"), completed.stderr)

    def test_the_token_is_echoed_back_so_a_clobbered_file_can_be_detected(self):
        # The counts path travels in argv, so a test CAN read it and write over the file.
        # That is a much smaller target than "any test that prints", but it is not nothing,
        # and this module's own tests run inside the suite they measure. The token turns a
        # clobber into a hard failure at the reader instead of a wrong number.
        completed, counts_file = self.run_module_in_subprocess(
            "t_token",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.assertTrue(True)
            """,
            token="abc123",
        )
        self.assertEqual(completed.returncode, 0)
        self.assertEqual(
            counts_file.read_text(encoding="utf-8"),
            "token=abc123\ntests=1\nskipped=0\nmodules=1\n",
        )

    def test_the_real_entry_point_exits_non_zero_on_a_failing_suite(self):
        completed, counts_file = self.run_module_in_subprocess(
            "t_subprocess_fail",
            """
            import unittest

            class T(unittest.TestCase):
                def test_one(self):
                    self.fail("deliberate")
            """,
        )
        self.assertEqual(completed.returncode, 1)
        self.assertEqual(counts_file.read_text(encoding="utf-8"), "tests=1\nskipped=0\nmodules=1\n")


class TestArgumentHandling(unittest.TestCase):
    """argparse writes its usage message to the process's stderr, which would otherwise
    land in the middle of the suite's own output and read as a failure. It is redirected
    here because it is expected, not because it is unwanted — the message is asserted."""

    def refuses(self, argv):
        captured = io.StringIO()
        with contextlib.redirect_stderr(captured), self.assertRaises(SystemExit) as raised:
            unittest_counts.main(argv, stream=io.StringIO())
        self.assertNotEqual(raised.exception.code, 0)
        return captured.getvalue()

    def test_no_modules_is_refused_rather_than_reported_as_a_clean_zero_test_run(self):
        # `Ran 0 tests ... OK` exiting 0 is the false pass helpers/CLAUDE.md warns about.
        with tempfile.TemporaryDirectory() as tmp:
            counts_file = pathlib.Path(tmp) / "counts"
            message = self.refuses(["--counts-file", str(counts_file)])
            self.assertIn("modules", message)
            self.assertFalse(counts_file.exists())

    def test_a_missing_counts_file_argument_is_refused(self):
        self.assertIn("--counts-file", self.refuses(["some.module"]))


if __name__ == "__main__":
    unittest.main()
