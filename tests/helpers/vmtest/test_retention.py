"""Tests for helpers/vmtest/retention.py — the disk floor, RAM ceiling and retention sweep (T6.4).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_retention

The floor REFUSES; it never cleans up and continues. The sweep keeps the last N
passing runs and EVERY failed run, drops a failed fast-base build directory but
keeps a failed desktop one, bounds the sandbox-writable quarantine (through the
pinned spool descriptors, so a symlinked directory is refused), and records
every eviction in the off-mount retention log.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import time
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import retention, spool


class TestDecisions(unittest.TestCase):
    def test_run_floor_is_the_base_size_plus_margin_and_rebuild_is_twice(self):
        self.assertEqual(retention.needed_for_run(10_000), 10_000 + retention.MARGIN_BYTES)
        self.assertEqual(retention.needed_for_rebuild(10_000), 2 * 10_000 + retention.MARGIN_BYTES)

    def test_floor_verdict_names_both_numbers(self):
        short = retention.assess_disk(free_bytes=5, needed_bytes=10)
        self.assertFalse(short.ok)
        self.assertIn("5", short.reason)
        self.assertIn("10", short.reason)
        self.assertTrue(retention.assess_disk(free_bytes=10, needed_bytes=10).ok)

    def test_ram_ceiling_is_a_fraction_of_the_host(self):
        self.assertTrue(retention.assess_ram(guest_mib=4096, host_mib=16384).ok)
        verdict = retention.assess_ram(guest_mib=14000, host_mib=16384)
        self.assertFalse(verdict.ok)
        self.assertIn("14000", verdict.reason)

    def test_sweep_keeps_the_newest_passing_runs_and_every_failed_run(self):
        entries = [
            retention.Entry(name=f"run-{i}", mtime=1000 + i, keep_always=(i % 3 == 0)) for i in range(10)
        ]
        evicted = retention.plan_sweep(entries, keep=2)
        names = sorted(e.name for e in evicted)
        # newest two passing (run-8, run-7) stay; failed (0, 3, 6, 9) stay; the rest go
        self.assertEqual(names, ["run-1", "run-2", "run-4", "run-5"])

    def test_sweep_with_nothing_over_the_cap_evicts_nothing(self):
        entries = [retention.Entry(name="a", mtime=1, keep_always=False)]
        self.assertEqual(retention.plan_sweep(entries, keep=5), [])


class SweepCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.lab = self.root / "lab"
        (self.lab / "runs").mkdir(parents=True)
        (self.lab / "bases").mkdir()
        self.checkout = self.root / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.log = self.root / "retention.log"

    def run_dir(self, name, verdict, age):
        path = self.lab / "runs" / name
        path.mkdir()
        if verdict is not None:
            (path / "response.json").write_text(json.dumps({"state": "finished", "verdict": verdict}))
        (path / "transcript.log").write_text("x")
        stamp = time.time() - age
        os.utime(path, (stamp, stamp))
        return path

    def sweep(self, *extra):
        return subprocess.run(
            [
                sys.executable, "-m", "helpers.vmtest.retention", "sweep",
                "--lab-dir", str(self.lab), "--checkout", str(self.checkout), "--log", str(self.log),
                "--keep-runs", "2", "--keep-spool", "3", *extra,
            ],
            cwd=REPO_ROOT, capture_output=True, text=True, check=False,
        )


class TestSweep(SweepCase):
    def test_passing_runs_beyond_the_cap_go_and_failed_runs_stay(self):
        old_pass = self.run_dir("r1-pass", "pass", 5000)
        old_fail = self.run_dir("r2-fail", "fail", 4000)
        unjudged = self.run_dir("r3-none", None, 3500)
        self.run_dir("r4-pass", "pass", 3000)
        self.run_dir("r5-error", "error", 2000)
        self.run_dir("r6-pass", "pass", 1000)
        result = self.sweep()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(old_pass.exists())
        self.assertTrue(old_fail.exists())
        self.assertTrue(unjudged.exists())
        self.assertTrue((self.lab / "runs" / "r4-pass").exists())
        self.assertTrue((self.lab / "runs" / "r6-pass").exists())
        self.assertIn("VMTEST-EVICTED run r1-pass", result.stdout)
        self.assertIn("VMTEST-SWEEP-DONE evicted=1", result.stdout)
        self.assertIn("evicted run r1-pass", self.log.read_text())

    def test_failed_fast_build_dir_goes_and_failed_desktop_build_stays(self):
        (self.lab / "bases" / "server-fast-44.build").mkdir()
        (self.lab / "bases" / "desktop-44.build").mkdir()
        (self.lab / "bases" / "server-fast-44").mkdir()
        result = self.sweep()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.lab / "bases" / "server-fast-44.build").exists())
        self.assertTrue((self.lab / "bases" / "desktop-44.build").exists())
        self.assertTrue((self.lab / "bases" / "server-fast-44").exists())

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

    def test_symlinked_quarantine_is_refused_and_nothing_outside_is_touched(self):
        outside = self.root / "outside"
        outside.mkdir()
        for i in range(5):
            (outside / f"victim{i}").write_text("precious")
        (self.bridge / "quarantine").rmdir()
        (self.bridge / "quarantine").symlink_to(outside)
        result = self.sweep()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(len(list(outside.iterdir())), 5)
        self.assertIn("refused", self.log.read_text())

    def test_missing_spool_is_not_an_error_for_the_lab_sweep(self):
        # A lab without a deployed bridge still sweeps its runs.
        import shutil
        shutil.rmtree(self.checkout)
        self.run_dir("r1-pass", "pass", 3000)
        self.run_dir("r2-pass", "pass", 2000)
        self.run_dir("r3-pass", "pass", 1000)
        result = self.sweep()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.lab / "runs" / "r1-pass").exists())


class TestFloorExecutor(unittest.TestCase):
    def test_floor_prints_marker_and_exits_non_zero_when_short(self):
        with tempfile.TemporaryDirectory() as tmp:
            huge = str(1 << 60)
            result = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.retention", "floor", "--path", tmp, "--base-size", huge, "--step", "rebuild", "--guest-mib", "1024"],
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(result.returncode, 1)
            self.assertIn("VMTEST-DISK short", result.stdout)
            self.assertIn(f"need={2 * (1 << 60) + retention.MARGIN_BYTES}", result.stdout)
            ok = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.retention", "floor", "--path", tmp, "--base-size", "1", "--step", "run", "--guest-mib", "1"],
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(ok.returncode, 0, ok.stderr)
            self.assertIn("VMTEST-DISK ok", ok.stdout)
            self.assertIn("VMTEST-RAM ok", ok.stdout)
            over = subprocess.run(
                [sys.executable, "-m", "helpers.vmtest.retention", "floor", "--path", tmp, "--base-size", "1", "--step", "run", "--guest-mib", str(1 << 40)],
                cwd=REPO_ROOT, capture_output=True, text=True, check=False,
            )
            self.assertEqual(over.returncode, 1)
            self.assertIn("VMTEST-RAM over", over.stdout)


if __name__ == "__main__":
    unittest.main()
