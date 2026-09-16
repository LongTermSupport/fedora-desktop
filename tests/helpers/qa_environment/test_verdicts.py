"""Unit tests for helpers/qa_environment/verdicts.py — comparing one machine's QA run
against another's.

Run from the repo root:

    python3 -m unittest tests.helpers.qa_environment.test_verdicts

The subject is Plan 00125's central claim: `./scripts/qa-all.bash` can reach a different
verdict per machine, and until the two outputs are put side by side nobody sees it. The
`js` stage counted 10 files in a container and 8 on a runner for weeks, reported as a pass
both times.
"""

from __future__ import annotations

import io
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.qa_environment import verdicts

# A CI log line carries the job name, the step name and an ISO timestamp before the
# payload, and the first line of a step also carries a UTF-8 BOM.
CI_PREFIX = "qa-all.bash\tRun full QA suite\t2026-09-15T23:17:44.5728745Z "


class TestParse(unittest.TestCase):
    def test_a_plain_stage_line(self):
        parsed = verdicts.parse("✓ bash: 258 files OK\n")
        self.assertEqual(parsed, {"bash": verdicts.Verdict("✓", "258 files OK")})

    def test_each_symbol_is_recognised(self):
        parsed = verdicts.parse(
            "✓ bash: ok\n✗ docs: 8 finding(s) across 71 files\n⚠ shellcheck: 169 issues\n"
        )
        self.assertEqual(
            {name: v.symbol for name, v in parsed.items()},
            {"bash": "✓", "docs": "✗", "shellcheck": "⚠"},
        )

    def test_a_ci_log_prefix_is_ignored(self):
        parsed = verdicts.parse(f"{CI_PREFIX}✓ js: 8 files OK\n")
        self.assertEqual(parsed["js"].detail, "8 files OK")

    def test_ansi_colour_is_stripped(self):
        parsed = verdicts.parse("\x1b[32m✓ js\x1b[0m: 8 files OK\n")
        self.assertEqual(parsed["js"], verdicts.Verdict("✓", "8 files OK"))

    def test_the_run_summary_lines_are_not_stages(self):
        # `✗ QA FAILED: …` and `✓ QA passed: …` share the stage shape but are the run's
        # own verdict. Counting them as stages would make every failing run differ from
        # every passing one on a row that is not a gate.
        parsed = verdicts.parse(
            "✓ bash: 258 files OK\n✗ QA FAILED: helper unit tests\n✓ QA passed: 905 files\n"
        )
        self.assertEqual(list(parsed), ["bash"])

    def test_indented_continuation_lines_are_not_stages(self):
        parsed = verdicts.parse(
            "✗ docs: 8 finding(s) across 71 files\n"
            "  .claude/rules/agent-docs.md:15  ../x.md  — target does not exist\n"
        )
        self.assertEqual(list(parsed), ["docs"])

    def test_prose_mentioning_a_tick_is_not_a_stage(self):
        self.assertEqual(verdicts.parse("the gate prints ✓ when it is happy\n"), {})

    def test_a_repeated_stage_keeps_the_last_line(self):
        # qa-all.bash echoes some failures to both stdout and stderr, which a CI log
        # interleaves into one stream.
        parsed = verdicts.parse("✓ js: 8 files OK\n✗ js: 1 error in 8 files\n")
        self.assertEqual(parsed["js"], verdicts.Verdict("✗", "1 error in 8 files"))

    def test_trailing_whitespace_and_carriage_returns_do_not_change_the_detail(self):
        parsed = verdicts.parse("✓ js: 8 files OK  \r\n")
        self.assertEqual(parsed["js"].detail, "8 files OK")

    def test_an_empty_input_parses_to_nothing_rather_than_failing(self):
        self.assertEqual(verdicts.parse(""), {})


class TestCompare(unittest.TestCase):
    def _v(self, symbol, detail):
        return verdicts.Verdict(symbol, detail)

    def test_identical_runs_report_every_stage_as_agreeing(self):
        here = {"bash": self._v("✓", "258 files OK")}
        there = {"bash": self._v("✓", "258 files OK")}
        rows = verdicts.compare(here, there)
        self.assertEqual([(r.name, r.state) for r in rows], [("bash", verdicts.AGREE)])

    def test_a_differing_detail_is_a_difference_even_when_both_passed(self):
        # The js stage: a pass on both machines, over a different number of files. This
        # is the case a pass/fail comparison cannot see, and the reason this compares
        # the whole line.
        here = {"js": self._v("✓", "10 files OK")}
        there = {"js": self._v("✓", "8 files OK")}
        rows = verdicts.compare(here, there)
        self.assertEqual(rows[0].state, verdicts.DIFFERS)
        self.assertEqual(rows[0].here.detail, "10 files OK")
        self.assertEqual(rows[0].there.detail, "8 files OK")

    def test_a_differing_symbol_is_a_difference(self):
        here = {"docs": self._v("✓", "71 files OK")}
        there = {"docs": self._v("✗", "8 finding(s) across 71 files")}
        self.assertEqual(verdicts.compare(here, there)[0].state, verdicts.DIFFERS)

    def test_a_stage_only_this_machine_ran(self):
        rows = verdicts.compare({"panel-sections": self._v("✓", "passed: 17")}, {})
        self.assertEqual(rows[0].state, verdicts.ONLY_HERE)
        self.assertIsNone(rows[0].there)

    def test_a_stage_only_the_other_machine_ran(self):
        # The one that matters most: qa-all.bash exits at the first failing hard gate,
        # so a stage missing from one side may never have RUN there. Absence is not
        # agreement, and it must not be reported as one.
        rows = verdicts.compare({}, {"panel-sections": self._v("✗", "boom")})
        self.assertEqual(rows[0].state, verdicts.ONLY_THERE)
        self.assertIsNone(rows[0].here)

    def test_rows_are_sorted_by_stage_name_so_two_reports_can_be_diffed(self):
        here = {"js": self._v("✓", "x"), "bash": self._v("✓", "x"), "docs": self._v("✓", "x")}
        rows = verdicts.compare(here, dict(here))
        self.assertEqual([r.name for r in rows], ["bash", "docs", "js"])

    def test_every_stage_from_both_sides_appears_exactly_once(self):
        here = {"a": self._v("✓", "1"), "b": self._v("✓", "2")}
        there = {"b": self._v("✓", "2"), "c": self._v("✓", "3")}
        rows = verdicts.compare(here, there)
        self.assertEqual([r.name for r in rows], ["a", "b", "c"])


class TestDifferences(unittest.TestCase):
    def test_only_the_non_agreeing_rows_are_counted(self):
        here = {"a": verdicts.Verdict("✓", "1"), "b": verdicts.Verdict("✓", "2")}
        there = {"a": verdicts.Verdict("✓", "1"), "b": verdicts.Verdict("✗", "2")}
        rows = verdicts.compare(here, there)
        self.assertEqual([r.name for r in verdicts.differences(rows)], ["b"])

    def test_a_one_sided_stage_counts_as_a_difference(self):
        rows = verdicts.compare({"a": verdicts.Verdict("✓", "1")}, {})
        self.assertEqual(len(verdicts.differences(rows)), 1)


class TestMain(unittest.TestCase):
    """The entry point the triage script calls, given the same seams as the rest."""

    def _run(self, here_text, there_text, *, extra=()):
        with tempfile.TemporaryDirectory() as tmp:
            here = pathlib.Path(tmp) / "here.txt"
            there = pathlib.Path(tmp) / "there.txt"
            here.write_text(here_text, encoding="utf-8")
            there.write_text(there_text, encoding="utf-8")
            out = io.StringIO()
            code = verdicts.main(
                ["--here", str(here), "--there", str(there), *extra], stdout=out
            )
        return code, out.getvalue()

    def test_it_reports_a_marker_line_the_caller_can_key_on(self):
        code, out = self._run("✓ js: 8 files OK\n", "✓ js: 8 files OK\n")
        self.assertEqual(code, 0)
        self.assertIn("QA-VERDICTS-DIFFERENCES 0", out)

    def test_a_difference_is_counted_and_both_sides_are_printed(self):
        code, out = self._run("✓ js: 10 files OK\n", "✓ js: 8 files OK\n")
        self.assertEqual(code, 0)
        self.assertIn("QA-VERDICTS-DIFFERENCES 1", out)
        self.assertIn("10 files OK", out)
        self.assertIn("8 files OK", out)

    def test_it_exits_zero_on_a_difference_because_triage_renders_no_verdict(self):
        # CLAUDE/PlanTriage.md R9: triage establishes facts, acceptance renders the
        # verdict. A divergence is the FINDING, not a failure of the fact-finding.
        code, _out = self._run("✓ js: 10 files OK\n", "✗ js: boom\n")
        self.assertEqual(code, 0)

    def test_parsing_nothing_from_a_side_fails_rather_than_reporting_agreement(self):
        # An empty capture reads as "no divergence" — the misleading-empty-result trap
        # CLAUDE/PlanTriage.md names. A log that yielded no stages means the capture
        # is broken, not that the machines agree.
        code, out = self._run("", "✓ js: 8 files OK\n")
        self.assertNotEqual(code, 0)
        self.assertIn("QA-VERDICTS-FAIL", out)

    def test_both_sides_empty_also_fails(self):
        code, out = self._run("", "")
        self.assertNotEqual(code, 0)
        self.assertIn("QA-VERDICTS-FAIL", out)

    def test_the_labels_for_each_side_are_overridable(self):
        _code, out = self._run(
            "✓ js: 10 files OK\n",
            "✓ js: 8 files OK\n",
            extra=("--here-label", "this container", "--there-label", "CI run 123"),
        )
        self.assertIn("this container", out)
        self.assertIn("CI run 123", out)


if __name__ == "__main__":
    unittest.main()
