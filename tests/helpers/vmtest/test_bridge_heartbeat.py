"""Tests for helpers/vmtest/bridge_heartbeat.py — the timer-driven liveness writer (§6.5).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_bridge_heartbeat

Driven as a subprocess with a fake `systemctl` that answers `show` for the two
bridge units. The heartbeat is the most attractive symlink target in the
spool (a fixed name rewritten every interval), so the symlink cases are here.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import spool

FAKE_SYSTEMCTL = """#!/usr/bin/bash
# fake systemctl: `--user show -p ActiveState -p Result <unit>`
set -euo pipefail
unit="${@: -1}"
case "$unit" in
  *.path) printf 'ActiveState=%s\\nResult=%s\\n' "$FAKE_PATH_STATE" "$FAKE_PATH_RESULT" ;;
  *.service) printf 'ActiveState=%s\\nResult=%s\\n' "$FAKE_SERVICE_STATE" "$FAKE_SERVICE_RESULT" ;;
  *) echo "unknown unit $unit" >&2; exit 1 ;;
esac
"""


class HeartbeatCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.checkout = self.root / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.state = self.root / "state"
        self.state.mkdir()
        self.systemctl = self.root / "systemctl"
        self.systemctl.write_text(FAKE_SYSTEMCTL)
        self.systemctl.chmod(0o755)

    def run_writer(self, path_state="active", path_result="success", service_state="inactive", service_result="success"):
        return subprocess.run(
            [
                sys.executable, "-m", "helpers.vmtest.bridge_heartbeat",
                "--checkout", str(self.checkout), "--slug", "test-slug", "--state-dir", str(self.state),
                "--systemctl", str(self.systemctl), "--now", "1800000000",
            ],
            cwd=REPO_ROOT,
            env={
                **os.environ,
                "FAKE_PATH_STATE": path_state, "FAKE_PATH_RESULT": path_result,
                "FAKE_SERVICE_STATE": service_state, "FAKE_SERVICE_RESULT": service_result,
            },
            capture_output=True, text=True, check=False,
        )

    def heartbeat(self):
        return json.loads((self.bridge / "diagnostics" / "bridge-heartbeat.json").read_text())


class TestHeartbeatWriter(HeartbeatCase):
    def test_writes_units_clock_flight_and_remedy(self):
        (self.state / "in-flight").write_text("run-1\n")
        result = self.run_writer()
        self.assertEqual(result.returncode, 0, result.stderr)
        beat = self.heartbeat()
        self.assertEqual(beat["written_at"], "2027-01-15T08:00:00Z")
        self.assertEqual(beat["path_unit"], {"active_state": "active", "result": "success"})
        self.assertEqual(beat["service_unit"], {"active_state": "inactive", "result": "success"})
        self.assertEqual(beat["in_flight"], "run-1")
        self.assertIn("reset-failed vmtest-bridge@test-slug.path", beat["remedy"])

    def test_no_run_in_flight_is_null(self):
        self.run_writer()
        self.assertIsNone(self.heartbeat()["in_flight"])

    def test_reports_a_failed_unit_and_does_not_repair_it(self):
        result = self.run_writer(path_state="failed", path_result="start-limit-hit")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.heartbeat()["path_unit"]["result"], "start-limit-hit")
        self.assertNotIn("reset-failed", result.stdout)

    def test_planted_symlink_at_the_heartbeat_name_is_replaced_not_followed(self):
        victim = self.root / "victim"
        victim.write_text("precious")
        (self.bridge / "diagnostics" / "bridge-heartbeat.json").symlink_to(victim)
        result = self.run_writer()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(victim.read_text(), "precious")
        self.assertFalse((self.bridge / "diagnostics" / "bridge-heartbeat.json").is_symlink())
        self.assertEqual(self.heartbeat()["schema"], 1)

    def test_symlinked_diagnostics_dir_refuses_and_exits_non_zero(self):
        outside = self.root / "outside"
        outside.mkdir()
        (self.bridge / "diagnostics").rmdir()
        (self.bridge / "diagnostics").symlink_to(outside)
        result = self.run_writer()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sorted(p.name for p in outside.iterdir()), [])
        self.assertIn("refused", (self.state / "service.log").read_text())

    def test_systemctl_failure_is_reported_not_hidden(self):
        self.systemctl.write_text("#!/usr/bin/bash\nexit 3\n")
        result = self.run_writer()
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse((self.bridge / "diagnostics" / "bridge-heartbeat.json").exists())


if __name__ == "__main__":
    unittest.main()
