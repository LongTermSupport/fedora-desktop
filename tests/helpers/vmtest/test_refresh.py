"""Tests for helpers/vmtest/refresh.py — the run-as-probe refresh outcome and base certification (§4.4a, T6.2/T6.2a).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_refresh

The two traps §4.4a was built from, each asserted to go red without its guard:

- a guest whose mirror lags the probe must produce `incomplete`, never
  "checked and current" (B7) — a zero-package transaction against a stale
  mirror proves nothing;
- the run certifies the base current ONLY on a passing run whose upgrade
  changed nothing and whose guest had caught up with the probe.
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

from helpers.vmtest import basejson, refresh, transcript

RECORD_FIELDS = {
    "fedora_version": 44,
    "profile": "server",
    "kind": "fast",
    "name": "server-fast-44",
    "compose_id": "Fedora-44-20260422.1",
    "compose_label": "44-1.7",
    "artefacts": [{"name": "Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2", "sha256": "a" * 64}],
    "treeinfo_checksums": None,
    "recipe_digest": "b" * 64,
    "installed_at": 1_800_000_000,
    "last_upgraded_at": 1_800_000_100,
    "last_upgraded_revision": 1_789_000_000,
    "last_upgraded_mirror": "https://mirror.example.com/fedora/updates/44/x86_64/",
    "refresh_state": "complete",
    "base_sha256": "c" * 64,
    "base_size": 123_456_789,
    "base_mtime": 1_800_000_200,
}


class TestAfterRun(unittest.TestCase):
    def test_caught_up_and_unchanged_is_current(self):
        outcome = refresh.after_run(upgrade_changed=False, guest_seen=1_789_000_500, probe_seen=1_789_000_500)
        self.assertEqual(outcome.state, "current")

    def test_caught_up_and_changed_means_the_base_is_stale(self):
        outcome = refresh.after_run(upgrade_changed=True, guest_seen=1_789_000_500, probe_seen=1_789_000_400)
        self.assertEqual(outcome.state, "stale")
        self.assertIn("refresh-base", outcome.reason)

    def test_lagging_guest_is_incomplete_never_current(self):
        # B7: the trap. Nothing changed, but the guest's mirror was behind the
        # probe, so the base was NOT checked against the current revision.
        for changed in (False, True):
            with self.subTest(changed=changed):
                outcome = refresh.after_run(upgrade_changed=changed, guest_seen=1_789_000_100, probe_seen=1_789_000_500)
                self.assertEqual(outcome.state, "incomplete")
                self.assertNotEqual(outcome.state, "current")
                self.assertIn("behind", outcome.reason)

    def test_missing_evidence_is_unknown(self):
        self.assertEqual(refresh.after_run(upgrade_changed=None, guest_seen=1, probe_seen=1).state, "unknown")
        self.assertEqual(refresh.after_run(upgrade_changed=False, guest_seen=None, probe_seen=1).state, "unknown")
        self.assertEqual(refresh.after_run(upgrade_changed=False, guest_seen=1, probe_seen=None).state, "unknown")


class TestTranscriptUpgradeSignal(unittest.TestCase):
    def test_ok_after_the_upgrade_task_means_unchanged(self):
        text = "TASK [Upgrade all packages to latest available] ****\nok: [localhost]\nRUN-BASH-EXIT 0\n"
        self.assertIs(transcript.parse(text).upgrade_changed, False)

    def test_changed_after_the_upgrade_task_means_changed(self):
        text = "TASK [Upgrade all packages to latest available] ****\nchanged: [localhost]\nRUN-BASH-EXIT 0\n"
        self.assertIs(transcript.parse(text).upgrade_changed, True)

    def test_no_upgrade_task_means_none(self):
        text = "TASK [Something else] ****\nchanged: [localhost]\nRUN-BASH-EXIT 0\n"
        self.assertIsNone(transcript.parse(text).upgrade_changed)

    def test_a_status_for_another_task_is_not_attributed(self):
        text = "TASK [Upgrade all packages to latest available] ****\nTASK [Next] ****\nchanged: [localhost]\n"
        self.assertIsNone(transcript.parse(text).upgrade_changed)


class CertifyCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.base_json = self.root / "base.json"
        self.base_json.write_text(basejson.render_record(basejson.build_record(**RECORD_FIELDS)))
        self.response = self.root / "response.json"

    def write_response(self, verdict="pass", state="current", guest_seen=1_789_000_500, mirror="https://other.example.com/x/"):
        self.response.write_text(json.dumps({
            "state": "finished", "verdict": verdict,
            "evidence": {
                "refresh": {"state": state, "guest_seen_revision": guest_seen, "probe_revision": 1_789_000_500, "upgrade_changed": False},
                "guest": {"updates_revision": str(guest_seen) if guest_seen else None, "updates_mirror": mirror},
            },
        }))

    def certify(self):
        return subprocess.run(
            [sys.executable, "-m", "helpers.vmtest.refresh", "certify", "--response", str(self.response), "--base-json", str(self.base_json), "--now", "1800001000"],
            cwd=REPO_ROOT, capture_output=True, text=True, check=False,
        )


class TestCertify(CertifyCase):
    def test_a_passing_current_run_advances_the_record_to_the_guest_seen_revision(self):
        self.write_response()
        result = self.certify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-BASE-CERTIFIED server-fast-44 revision=1789000500", result.stdout)
        record = basejson.parse_record(self.base_json.read_text())
        self.assertEqual(record.last_upgraded_revision, 1_789_000_500)
        self.assertEqual(record.last_upgraded_at, 1_800_001_000)
        self.assertEqual(record.last_upgraded_mirror, "https://other.example.com/x/")
        self.assertEqual(record.refresh_state, "complete")
        # Nothing else moved: the identity and the disk facts are untouched.
        self.assertEqual(record.base_sha256, "c" * 64)
        self.assertEqual(record.installed_at, 1_800_000_000)

    def test_a_failing_run_certifies_nothing(self):
        self.write_response(verdict="fail")
        result = self.certify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-BASE-NOT-CERTIFIED", result.stdout)
        self.assertEqual(basejson.parse_record(self.base_json.read_text()).last_upgraded_revision, 1_789_000_000)

    def test_incomplete_or_stale_certifies_nothing(self):
        for state in ("incomplete", "stale", "unknown"):
            with self.subTest(state=state):
                self.write_response(state=state)
                result = self.certify()
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn("VMTEST-BASE-NOT-CERTIFIED", result.stdout)
                self.assertEqual(basejson.parse_record(self.base_json.read_text()).last_upgraded_revision, 1_789_000_000)

    def test_a_revision_that_would_go_backwards_is_refused(self):
        self.write_response(guest_seen=1_788_000_000)
        result = self.certify()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("VMTEST-BASE-NOT-CERTIFIED", result.stdout)
        self.assertIn("backwards", result.stdout)

    def test_unusable_inputs_exit_two(self):
        self.response.write_text("not json")
        self.assertEqual(self.certify().returncode, 2)


if __name__ == "__main__":
    unittest.main()
