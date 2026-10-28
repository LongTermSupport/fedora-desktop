"""Tests for helpers/vmtest/retention.py — the bridge spool's retention sweep (T6.4).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_retention

The sweep bounds the sandbox-writable quarantine and the responses through the
pinned spool descriptors, so a symlinked directory is refused, and records every
eviction in the off-mount retention log. The lab's own retention (the floor, the
run and build sweep, the prune plan) is vmtest's, in src-go/internal/vmtest.
"""

from __future__ import annotations

import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import retention, spool


class TestDecisions(unittest.TestCase):
    def test_sweep_keeps_the_newest_entries(self):
        entries = [retention.Entry(name=f"q-{i}", mtime=1000 + i) for i in range(5)]
        evicted = retention.plan_sweep(entries, keep=2)
        self.assertEqual(sorted(e.name for e in evicted), ["q-0", "q-1", "q-2"])

    def test_sweep_with_nothing_over_the_cap_evicts_nothing(self):
        entries = [retention.Entry(name="a", mtime=1)]
        self.assertEqual(retention.plan_sweep(entries, keep=5), [])


class SweepCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.checkout = self.root / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.log = self.root / "retention.log"

    def sweep(self, *extra):
        return subprocess.run(
            [
                sys.executable, "-m", "helpers.vmtest.retention", "sweep-spool",
                "--checkout", str(self.checkout), "--log", str(self.log), "--keep-spool", "3", *extra,
            ],
            cwd=REPO_ROOT, capture_output=True, text=True, check=False,
        )


class TestSweep(SweepCase):
    def test_quarantine_and_responses_are_bounded_through_the_pinned_spool(self):
        for i in range(6):
            p = self.bridge / "quarantine" / f"q{i}.json"
            p.write_text("{}")
            os.utime(p, (1000 + i, 1000 + i))
            r = self.bridge / "responses" / f"r{i}.json.response.json"
            r.write_text("{}")
            os.utime(r, (1000 + i, 1000 + i))
        result = self.sweep()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(sorted(p.name for p in (self.bridge / "quarantine").iterdir()), ["q3.json", "q4.json", "q5.json"])
        self.assertEqual(sorted(p.name for p in (self.bridge / "responses").iterdir()), ["r3.json.response.json", "r4.json.response.json", "r5.json.response.json"])
        self.assertEqual(result.stdout.count("VMTEST-EVICTED quarantine"), 3)
        self.assertTrue(result.stdout.endswith("VMTEST-SPOOL-SWEEP-DONE evicted=6\n"), result.stdout)
        self.assertIn("evicted quarantine q0.json beyond the spool retention cap", self.log.read_text())

    def test_symlinked_quarantine_is_refused_and_nothing_outside_is_touched(self):
        outside = self.root / "outside"
        outside.mkdir()
        for i in range(5):
            (outside / f"victim{i}").write_text("precious")
        (self.bridge / "quarantine").rmdir()
        (self.bridge / "quarantine").symlink_to(outside)
        result = self.sweep()
        self.assertEqual(result.returncode, 2)
        self.assertEqual(len(list(outside.iterdir())), 5)
        self.assertIn("refused", self.log.read_text())

    def test_a_checkout_without_a_bridge_has_nothing_to_sweep(self):
        shutil.rmtree(self.checkout)
        result = self.sweep()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "VMTEST-SPOOL-SWEEP-DONE evicted=0\n")

    def test_the_lab_verbs_are_not_here(self):
        for verb in ("floor", "prune-plan", "sweep"):
            result = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.retention", verb],
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(result.returncode, 2, verb)


if __name__ == "__main__":
    unittest.main()
