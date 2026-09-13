"""Unit tests for helpers/vmtest/transcript.py — reading a run transcript and judging it.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_transcript

A transcript is the guest's stdout/stderr as the host captured it: run.bash's
output (with its PLAY RECAP blocks), a `RUN-BASH-EXIT <rc>` line the host
appends, then the guest acceptance script's `VMTEST-*` marker lines. The parser
is pure; the judge applies DESIGN.md §6.6 on top of scenarios.judge_checks and
names a stage for every non-pass.
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import transcript

RECAP = """\
PLAY RECAP *********************************************************************
localhost                  : ok=312  changed=87   unreachable=0    failed=0    skipped=41   rescued=0    ignored=0
"""

GOOD = (
    "Headless: checkout pinned at commit 0123456789ab (detached, RUN_BASH_GIT_REF)\n"
    + RECAP
    + "Headless provisioning complete! Reboot when convenient\n"
    "RUN-BASH-EXIT 0\n"
    "VMTEST-CHECK-PLANNED 3\n"
    "VMTEST-CHECK pass repo-cloned-at-pinned-commit\n"
    "VMTEST-CHECK pass podman-rootless-works rootless=true\n"
    "VMTEST-CHECK pass sshd-active\n"
    "VMTEST-EVIDENCE boot_id=6f1c2a7e-3c4b-4d5e-8f90-1a2b3c4d5e6f\n"
    "VMTEST-EVIDENCE machine_id=0123456789abcdef0123456789abcdef\n"
    "VMTEST-EVIDENCE os_release=Fedora Linux 44 (Cloud Edition)\n"
    "VMTEST-EVIDENCE kernel=6.19.0-200.fc44.x86_64\n"
    "VMTEST-CHECKS-DONE total=3 passed=3 failed=0 skipped=0\n"
)


def judge(text=GOOD, planned=3, max_skipped=0):
    return transcript.judge(transcript.parse(text), planned=planned, max_skipped=max_skipped)


class TestParse(unittest.TestCase):
    def test_reads_checks_counters_recap_evidence_and_exit(self):
        parsed = transcript.parse(GOOD)
        self.assertEqual(parsed.planned, 3)
        self.assertEqual([c.name for c in parsed.checks], ["repo-cloned-at-pinned-commit", "podman-rootless-works", "sshd-active"])
        self.assertEqual(parsed.checks[1].detail, "rootless=true")
        self.assertEqual(parsed.done, {"total": 3, "passed": 3, "failed": 0, "skipped": 0})
        self.assertEqual(parsed.run_bash_exit, 0)
        self.assertEqual(parsed.evidence["boot_id"], "6f1c2a7e-3c4b-4d5e-8f90-1a2b3c4d5e6f")
        self.assertEqual(parsed.evidence["os_release"], "Fedora Linux 44 (Cloud Edition)")
        self.assertEqual(len(parsed.recaps), 1)
        self.assertEqual(parsed.recaps[0], {"ok": 312, "changed": 87, "unreachable": 0, "failed": 0, "skipped": 41, "rescued": 0, "ignored": 0})

    def test_multiple_recaps_are_all_kept_in_order(self):
        text = GOOD.replace(RECAP, RECAP + RECAP.replace("ok=312", "ok=7"))
        parsed = transcript.parse(text)
        self.assertEqual([r["ok"] for r in parsed.recaps], [312, 7])

    def test_missing_pieces_are_none_not_zero(self):
        parsed = transcript.parse("nothing here\n")
        self.assertIsNone(parsed.planned)
        self.assertIsNone(parsed.done)
        self.assertIsNone(parsed.run_bash_exit)
        self.assertEqual(parsed.checks, ())
        self.assertEqual(parsed.recaps, ())
        self.assertEqual(parsed.evidence, {})

    def test_malformed_marker_lines_are_a_parse_error(self):
        for bad in ("VMTEST-CHECK maybe thing\n", "VMTEST-CHECKS-DONE total=x\n", "VMTEST-CHECK-PLANNED many\n", "RUN-BASH-EXIT ok\n"):
            with self.subTest(line=bad):
                with self.assertRaises(transcript.TranscriptError):
                    transcript.parse(GOOD + bad)

    def test_check_status_must_be_one_of_three(self):
        parsed = transcript.parse(GOOD.replace("VMTEST-CHECK pass sshd-active", "VMTEST-CHECK skip sshd-active not-applicable"))
        self.assertEqual(parsed.checks[2].status, "skip")
        self.assertEqual(parsed.checks[2].detail, "not-applicable")


class TestJudge(unittest.TestCase):
    def test_good_run_passes_with_no_stage(self):
        judgement = judge()
        self.assertEqual(judgement.verdict, "pass")
        self.assertIsNone(judgement.stage)

    def test_run_bash_non_zero_is_fail_at_provision(self):
        # The negative scenarios (T3.4) must land here: the product ran and
        # failed, which is `fail`, never `error`.
        text = GOOD.replace("RUN-BASH-EXIT 0", "RUN-BASH-EXIT 1")
        judgement = judge(text)
        self.assertEqual(judgement.verdict, "fail")
        self.assertEqual(judgement.stage, "provision")
        self.assertIn("exit 1", judgement.reason)

    def test_missing_run_bash_exit_is_error_at_provision(self):
        text = GOOD.replace("RUN-BASH-EXIT 0\n", "")
        judgement = judge(text)
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "provision")

    def test_recap_failure_with_zero_exit_is_error_not_pass(self):
        # A failed play must propagate out of run.bash; if the recap says failed
        # and the exit says success, the harness cannot trust either.
        text = GOOD.replace("failed=0    skipped=41", "failed=1    skipped=41")
        judgement = judge(text)
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "provision")
        self.assertIn("disagree", judgement.reason)

    def test_unreachable_in_recap_is_treated_like_failed(self):
        text = GOOD.replace("unreachable=0    failed=0", "unreachable=1    failed=0")
        self.assertEqual(judge(text).verdict, "error")

    def test_no_recap_is_error(self):
        # §6.6 rule 05: a pass with no PLAY RECAP could not have provisioned.
        judgement = judge(GOOD.replace(RECAP, ""))
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "provision")

    def test_recap_with_zero_ok_is_error(self):
        judgement = judge(GOOD.replace("ok=312", "ok=0"))
        self.assertEqual(judgement.verdict, "error")

    def test_missing_planned_declaration_is_error_at_assert(self):
        judgement = judge(GOOD.replace("VMTEST-CHECK-PLANNED 3\n", ""))
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "assert")

    def test_planned_disagreeing_with_manifest_is_error(self):
        # The manifest and the guest script both declare the count; they must
        # agree, or one of them is stale.
        judgement = judge(planned=4)
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "assert")
        self.assertIn("manifest", judgement.reason)

    def test_missing_done_line_is_error_at_assert(self):
        # The script died before its summary: 3 of 3 checks printed pass, but
        # nothing certified the list was complete.
        judgement = judge(GOOD.replace("VMTEST-CHECKS-DONE total=3 passed=3 failed=0 skipped=0\n", ""))
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "assert")

    def test_done_counters_disagreeing_with_check_lines_is_error(self):
        judgement = judge(GOOD.replace("VMTEST-CHECKS-DONE total=3 passed=3", "VMTEST-CHECKS-DONE total=3 passed=2"))
        self.assertEqual(judgement.verdict, "error")
        self.assertIn("disagree", judgement.reason)

    def test_failed_check_is_fail_at_assert_and_names_it(self):
        judgement = judge(GOOD.replace("VMTEST-CHECK pass sshd-active", "VMTEST-CHECK fail sshd-active inactive").replace(
            "VMTEST-CHECKS-DONE total=3 passed=3 failed=0", "VMTEST-CHECKS-DONE total=3 passed=2 failed=1"))
        self.assertEqual(judgement.verdict, "fail")
        self.assertEqual(judgement.stage, "assert")
        self.assertIn("sshd-active", judgement.reason)

    def test_harness_that_died_early_is_error(self):
        # Two of three checks ran, both green, no DONE — rule 02.
        text = GOOD.replace("VMTEST-CHECK pass sshd-active\n", "").replace(
            "VMTEST-CHECKS-DONE total=3 passed=3 failed=0 skipped=0\n", "")
        judgement = judge(text)
        self.assertEqual(judgement.verdict, "error")

    def test_skipped_over_cap_is_error(self):
        text = GOOD.replace("VMTEST-CHECK pass sshd-active", "VMTEST-CHECK skip sshd-active n/a").replace(
            "VMTEST-CHECKS-DONE total=3 passed=3 failed=0 skipped=0", "VMTEST-CHECKS-DONE total=3 passed=2 failed=0 skipped=1")
        self.assertEqual(judge(text, max_skipped=0).verdict, "error")
        self.assertEqual(judge(text, max_skipped=1).verdict, "pass")

    def test_missing_boot_id_is_error_at_collect(self):
        # §6.6 rule 04: a pass with no boot id could not have booted anything.
        judgement = judge(GOOD.replace("VMTEST-EVIDENCE boot_id=6f1c2a7e-3c4b-4d5e-8f90-1a2b3c4d5e6f\n", ""))
        self.assertEqual(judgement.verdict, "error")
        self.assertEqual(judgement.stage, "collect")

    def test_fail_outranks_every_error_condition(self):
        # Product failure plus a harness defect is still reported as the
        # product failure; that is the more informative verdict.
        text = GOOD.replace("RUN-BASH-EXIT 0", "RUN-BASH-EXIT 2").replace("VMTEST-EVIDENCE boot_id=6f1c2a7e-3c4b-4d5e-8f90-1a2b3c4d5e6f\n", "")
        self.assertEqual(judge(text).verdict, "fail")

    def test_checks_summary_carries_planned_and_counters(self):
        judgement = judge()
        self.assertEqual(judgement.checks, {"planned": 3, "total": 3, "passed": 3, "failed": 0, "skipped": 0})
        self.assertEqual(judgement.skipped_names, ())
        text = GOOD.replace("VMTEST-CHECK pass sshd-active", "VMTEST-CHECK skip sshd-active n/a").replace(
            "VMTEST-CHECKS-DONE total=3 passed=3 failed=0 skipped=0", "VMTEST-CHECKS-DONE total=3 passed=2 failed=0 skipped=1")
        self.assertEqual(judge(text, max_skipped=1).skipped_names, ("sshd-active",))

    def test_unfinished_run_reports_null_counters(self):
        judgement = judge("RUN-BASH-EXIT 0\n" + RECAP)
        self.assertEqual(judgement.checks["total"], None)
        self.assertEqual(judgement.checks["planned"], 3)


if __name__ == "__main__":
    unittest.main()
