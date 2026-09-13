"""Tests for helpers/vmtest/judge_run.py — the executor that turns a run into its response.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_judge_run

`vmtest run` hands this the transcript plus the facts only the host knows
(run id, base record, commit, freshness verdict, timings) and writes what it
prints as `response.json` — the §6.6 document, minus the bridge-only fields
Phase 4 adds (request, verb, signature). Driven as a subprocess.
"""

from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from tests.helpers.vmtest.test_freshness_gate import _record
from tests.helpers.vmtest.test_transcript import GOOD

COMMIT = "0123456789abcdef0123456789abcdef01234567"


class JudgeRunCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        (self.root / "base.json").write_text(_record())

    def run_judge(self, transcript_text=GOOD, *extra):
        (self.root / "transcript.log").write_text(transcript_text)
        return subprocess.run(
            [
                sys.executable, "-m", "helpers.vmtest.judge_run",
                "--transcript", str(self.root / "transcript.log"),
                "--transcript-path", "runs/20260913T170000Z-server-fast-provision/transcript.log",
                "--base-json", str(self.root / "base.json"),
                "--scenario", "server-fast-provision",
                "--planned", "3",
                "--max-skipped", "0",
                "--run-id", "20260913T170000Z-server-fast-provision",
                "--commit", COMMIT,
                "--branch", "F44",
                "--freshness-decision", "current",
                "--freshness-degraded", "false",
                "--divergences", "throwaway-vault-password,lab-ssh-key-baked",
                "--started-at", "1800000000",
                "--finished-at", "1800001800",
                *extra,
            ],
            cwd=REPO_ROOT,
            capture_output=True,
            text=True,
            check=False,
        )

    def response(self, result):
        self.assertTrue(result.stdout.strip(), result.stderr)
        return json.loads(result.stdout)


class TestJudgeRun(JudgeRunCase):
    def test_passing_run_exits_zero_and_prints_the_response(self):
        result = self.run_judge()
        self.assertEqual(result.returncode, 0, result.stderr)
        response = self.response(result)
        self.assertEqual(response["schema"], 1)
        self.assertEqual(response["state"], "finished")
        self.assertEqual(response["verdict"], "pass")
        self.assertIsNone(response["failure"])
        self.assertEqual(response["argument"], "server-fast-provision")
        self.assertEqual(response["run_id"], "20260913T170000Z-server-fast-provision")
        self.assertEqual(response["checks"], {"planned": 3, "total": 3, "passed": 3, "failed": 0, "skipped": 0})
        self.assertEqual(response["checks_skipped"], [])
        self.assertEqual(response["started_at"], "2027-01-15T08:00:00Z")
        self.assertEqual(response["finished_at"], "2027-01-15T08:30:00Z")

    def test_evidence_binds_base_kind_and_name(self):
        # §3.5: a fast-path pass can never be read as a fresh-install pass,
        # because the response carries kind and name, not just profile.
        base = self.response(self.run_judge())["evidence"]["base"]
        self.assertEqual(base["kind"], "fast")
        self.assertEqual(base["name"], "server-fast-44")
        self.assertEqual(base["profile"], "server")
        self.assertEqual(base["compose_label"], "44-1.7")
        self.assertEqual(base["built_from"], ["Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2"])
        self.assertEqual(base["freshness"], "current")
        self.assertFalse(base["freshness_degraded"])
        self.assertEqual(base["refresh_state"], "complete")
        self.assertEqual(base["last_upgraded_revision"], 1789172543)

    def test_evidence_carries_guest_repo_recap_and_divergences(self):
        evidence = self.response(self.run_judge())["evidence"]
        self.assertEqual(evidence["guest"]["boot_id"], "6f1c2a7e-3c4b-4d5e-8f90-1a2b3c4d5e6f")
        self.assertEqual(evidence["guest"]["kernel"], "6.19.0-200.fc44.x86_64")
        self.assertEqual(evidence["repo"], {"commit": COMMIT, "branch": "F44"})
        self.assertEqual(evidence["playbook_recap"], {"ok": 312, "changed": 87, "unreachable": 0, "failed": 0, "skipped": 41, "rescued": 0, "ignored": 0})
        self.assertEqual(evidence["divergences"], ["lab-ssh-key-baked", "throwaway-vault-password"])
        self.assertEqual(evidence["transcript"], "runs/20260913T170000Z-server-fast-provision/transcript.log")
        self.assertEqual(len(evidence["transcript_sha256"]), 64)
        self.assertEqual(evidence["overrides"], [])

    def test_freshness_divergences_merge_with_the_scenarios(self):
        result = self.run_judge(GOOD, "--freshness-divergences", "package-revision-unreadable", "--freshness-degraded", "true")
        evidence = self.response(result)["evidence"]
        self.assertIn("package-revision-unreadable", evidence["divergences"])
        self.assertTrue(evidence["base"]["freshness_degraded"])

    def test_failing_run_exits_non_zero_with_stage_and_reason(self):
        result = self.run_judge(GOOD.replace("RUN-BASH-EXIT 0", "RUN-BASH-EXIT 1"))
        self.assertNotEqual(result.returncode, 0)
        response = self.response(result)
        self.assertEqual(response["verdict"], "fail")
        self.assertEqual(response["failure"]["stage"], "provision")
        self.assertIn("exit 1", response["failure"]["reason"])

    def test_harness_error_is_error_not_fail(self):
        result = self.run_judge(GOOD.replace("VMTEST-CHECKS-DONE total=3 passed=3 failed=0 skipped=0\n", ""))
        self.assertNotEqual(result.returncode, 0)
        response = self.response(result)
        self.assertEqual(response["verdict"], "error")
        self.assertEqual(response["failure"]["stage"], "assert")

    def test_overrides_are_recorded_verbatim(self):
        response = self.response(self.run_judge(GOOD, "--override=--accept-stale-base"))
        self.assertEqual(response["evidence"]["overrides"], ["--accept-stale-base"])

    def test_malformed_transcript_marker_is_a_hard_error(self):
        result = self.run_judge(GOOD + "VMTEST-CHECK maybe thing\n")
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")
        self.assertIn("transcript", result.stderr)

    def test_unreadable_base_record_is_a_hard_error(self):
        (self.root / "base.json").write_text("{}")
        result = self.run_judge()
        self.assertEqual(result.returncode, 2)
        self.assertEqual(result.stdout, "")


if __name__ == "__main__":
    unittest.main()
