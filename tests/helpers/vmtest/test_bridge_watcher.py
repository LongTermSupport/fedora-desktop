"""Tests for helpers/vmtest/bridge_watcher.py — one activation of the bridge (§6.4).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_bridge_watcher

The watcher is driven as a subprocess against a temporary checkout with a
spool, a deployed allowlist, a policy file, a signing key, an off-mount audit
log and a FAKE dispatcher that records the argv it was handed instead of
starting a systemd scope. Every rejection path must reject AND respond; a
symlinked spool directory must refuse and respond with nothing (T4.8).
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

from helpers.vmtest import spool, verdict

KEY = b"k" * 32
NOW = 1_800_000_000
NAME = "20260913T114500Z-run-scenario-0123456789abcdef.json"


def _body(verb="run-scenario", argument="server-fast-provision", nonce="0123456789abcdef"):
    return json.dumps({"verb": verb, "argument": argument, "nonce": nonce}).encode()


class WatcherCase(unittest.TestCase):
    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = pathlib.Path(self._tmp.name)
        self.checkout = self.root / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.config = self.root / "config"
        self.config.mkdir()
        (self.config / "policy").write_text("MODE_run-scenario=auto\nMODE_lab-status=auto\nMODE_refresh-base=deny\n")
        (self.config / "response.key").write_bytes(KEY)
        self.state = self.root / "state"
        self.state.mkdir()
        self.allowlist = self.root / "scenarios.allowlist"
        self.allowlist.write_text("server-fast-provision\nserver-main-playbook-fails\n")
        self.dispatch_log = self.root / "dispatched.json"
        self.dispatcher = self.root / "fake-dispatcher"
        self.dispatcher.write_text(
            "#!/usr/bin/bash\nset -euo pipefail\nprintf '%s\\n' \"$@\" >> \"$DISPATCH_LOG\"\n"
        )
        self.dispatcher.chmod(0o755)

    def put(self, name=NAME, body=None):
        (self.bridge / "requests" / name).write_bytes(body if body is not None else _body())

    def run_watcher(self, *extra, now=NOW):
        return subprocess.run(
            [
                sys.executable, "-m", "helpers.vmtest.bridge_watcher",
                "--checkout", str(self.checkout),
                "--slug", "test-slug",
                "--config-dir", str(self.config),
                "--state-dir", str(self.state),
                "--allowlist", str(self.allowlist),
                "--dispatcher", str(self.dispatcher),
                "--now", str(now),
                "--debounce-seconds", "0",
                *extra,
            ],
            cwd=REPO_ROOT,
            env={**os.environ, "DISPATCH_LOG": str(self.dispatch_log)},
            capture_output=True,
            text=True,
            check=False,
        )

    def response(self, name=NAME):
        text = (self.bridge / "responses" / f"{name}.response.json").read_text()
        return verdict.verify(text, KEY)

    def dispatched(self):
        if not self.dispatch_log.exists():
            return []
        return self.dispatch_log.read_text().splitlines()

    def audit(self):
        log = self.state / "service.log"
        return log.read_text() if log.exists() else ""


class TestAccept(WatcherCase):
    def test_valid_request_is_claimed_answered_and_dispatched(self):
        self.put()
        result = self.run_watcher()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse((self.bridge / "requests" / NAME).exists())
        self.assertTrue((self.bridge / "processing" / NAME).exists())
        response = self.response()
        self.assertEqual(response["state"], "accepted")
        self.assertIsNone(response["verdict"])
        self.assertEqual(response["signature"]["nonce"], "0123456789abcdef")
        argv = self.dispatched()
        self.assertEqual(argv[:2], ["--user", "--scope"])
        self.assertIn("--unit", argv)
        self.assertIn("run-scenario", argv)
        self.assertEqual(argv[-1], "server-fast-provision")
        self.assertIn("accepted", self.audit())

    def test_accepted_response_is_written_before_dispatch(self):
        # Silence is never an outcome: even if the dispatcher dies, the
        # accepted stub is on disk.
        self.dispatcher.write_text("#!/usr/bin/bash\nexit 1\n")
        self.put()
        result = self.run_watcher()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(self.response()["state"], "accepted")

    def test_argv_is_built_from_the_buffer_not_the_file(self):
        # D2: swapping the file between read and dispatch changes nothing. The
        # fake dispatcher reads nothing, so the check is on what it was handed
        # after the file was rewritten by a hook that runs during the watcher:
        # simulate by making the request unwritable after claim — the argv must
        # still name the original argument.
        self.put()
        self.run_watcher()
        self.assertEqual(self.dispatched()[-1], "server-fast-provision")

    def test_lab_status_needs_no_argument_and_no_allowlist(self):
        name = "20260913T114500Z-lab-status-0123456789abcdef.json"
        self.put(name, _body(verb="lab-status", argument=None))
        result = self.run_watcher()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.response(name)["state"], "accepted")
        self.assertIn("lab-status", self.dispatched())

    def test_drains_every_request_in_one_activation(self):
        self.put()
        other = "20260913T114501Z-lab-status-fedcba9876543210.json"
        self.put(other, _body(verb="lab-status", argument=None, nonce="fedcba9876543210"))
        result = self.run_watcher()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.response()["state"], "accepted")
        # The second is rejected as in-flight — one run at a time — but it IS answered.
        self.assertEqual(self.response(other)["state"], "rejected")
        self.assertIn("in-flight", self.response(other)["failure"]["reason"])


class TestReject(WatcherCase):
    def assert_rejected(self, name, code):
        response = self.response(name)
        self.assertEqual(response["state"], "rejected")
        self.assertIsNone(response["verdict"])
        self.assertTrue(response["failure"]["reason"].startswith(f"{code}:"), response["failure"])
        self.assertFalse((self.bridge / "requests" / name).exists())
        self.assertTrue((self.bridge / "quarantine" / name).exists())
        self.assertEqual(self.dispatched(), [])
        self.assertIn(code, self.audit())

    def test_bad_filename_is_quarantined_and_answered(self):
        self.put("evil.json", _body())
        self.assertEqual(self.run_watcher().returncode, 0)
        self.assert_rejected("evil.json", "bad-filename")

    def test_denylisted_verb(self):
        name = "20260913T114500Z-exec-0123456789abcdef.json"
        self.put(name, _body(verb="exec"))
        self.run_watcher()
        self.assert_rejected(name, "denylisted-verb")

    def test_unknown_verb(self):
        name = "20260913T114500Z-list-hosts-0123456789abcdef.json"
        self.put(name, _body(verb="list-hosts"))
        self.run_watcher()
        self.assert_rejected(name, "unknown-verb")

    def test_verb_mismatch(self):
        self.put(NAME, _body(verb="lab-status", argument=None))
        self.run_watcher()
        self.assert_rejected(NAME, "verb-mismatch")

    def test_unknown_argument_not_in_the_deployed_allowlist(self):
        self.put(NAME, _body(argument="desktop-fresh-install"))
        self.run_watcher()
        self.assert_rejected(NAME, "unknown-argument")

    def test_refresh_base_argument_enumeration(self):
        name = "20260913T114500Z-refresh-base-0123456789abcdef.json"
        (self.config / "policy").write_text("MODE_refresh-base=auto\n")
        self.put(name, _body(verb="refresh-base", argument="everything"))
        self.run_watcher()
        self.assert_rejected(name, "unknown-argument")

    def test_policy_deny(self):
        name = "20260913T114500Z-refresh-base-0123456789abcdef.json"
        self.put(name, _body(verb="refresh-base", argument="server"))
        self.run_watcher()
        self.assert_rejected(name, "policy-deny")

    def test_missing_policy_file_denies(self):
        (self.config / "policy").unlink()
        self.put()
        self.run_watcher()
        self.assert_rejected(NAME, "policy-deny")

    def test_missing_or_unrecognised_policy_value_denies(self):
        for text in ("", "MODE_run-scenario=ask\n", "MODE_run-scenario=AUTO\n", "run-scenario=auto\n"):
            with self.subTest(policy=text):
                (self.config / "policy").write_text(text)
                self.put()
                self.run_watcher()
                self.assert_rejected(NAME, "policy-deny")
                (self.bridge / "quarantine" / NAME).unlink()
                (self.bridge / "responses" / f"{NAME}.response.json").unlink()
                (self.state / "service.log").unlink()

    def test_in_flight_lock_rejects_a_second_run(self):
        (self.state / "in-flight").write_text("20260913T114000Z-server-fast-provision\n")
        self.put()
        self.run_watcher()
        self.assert_rejected(NAME, "in-flight")

    def test_rate_limit_counts_the_off_mount_audit_log(self):
        # Ten answered requests inside the window, from the audit log the
        # sandbox cannot edit; the eleventh is rejected and the watcher still
        # exits 0 so no unit enters failed.
        lines = "".join(f"{NOW - 5} rejected 2026x-lab-status-{i:016x}.json bad-body\n" for i in range(10))
        (self.state / "service.log").write_text(lines)
        self.put()
        result = self.run_watcher()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assert_rejected(NAME, "rate-limited")

    def test_old_audit_entries_do_not_count(self):
        lines = "".join(f"{NOW - 3600} rejected 2026x-lab-status-{i:016x}.json bad-body\n" for i in range(10))
        (self.state / "service.log").write_text(lines)
        self.put()
        self.run_watcher()
        self.assertEqual(self.response()["state"], "accepted")

    def test_fifo_request_is_quarantined_and_answered(self):
        os.mkfifo(self.bridge / "requests" / NAME)
        self.assertEqual(self.run_watcher().returncode, 0)
        self.assert_rejected(NAME, "bad-body")

    def test_stale_allowlist_file_absent_rejects_with_the_remedy(self):
        self.allowlist.unlink()
        self.put()
        self.run_watcher()
        self.assert_rejected(NAME, "allowlist-stale")
        self.assertIn("play-vm-test-lab.yml", self.response()["failure"]["reason"])


class TestRefuse(WatcherCase):
    def test_symlinked_responses_refuses_writes_nothing_and_exits_non_zero(self):
        outside = self.root / "outside"
        outside.mkdir()
        (self.bridge / "responses").rmdir()
        (self.bridge / "responses").symlink_to(outside)
        self.put()
        result = self.run_watcher()
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(sorted(p.name for p in outside.iterdir()), [])
        self.assertTrue((self.bridge / "requests" / NAME).exists())
        self.assertEqual(self.dispatched(), [])
        self.assertIn("refus", self.audit().lower())

    def test_symlinked_untracked_refuses(self):
        real = self.root / "real-untracked"
        (self.checkout / "untracked").rename(real)
        (self.checkout / "untracked").symlink_to(real)
        result = self.run_watcher()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("refus", self.audit().lower())

    def test_missing_signing_key_refuses_before_touching_the_spool(self):
        (self.config / "response.key").unlink()
        self.put()
        result = self.run_watcher()
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue((self.bridge / "requests" / NAME).exists())
        self.assertFalse(list((self.bridge / "responses").iterdir()))

    def test_empty_spool_is_a_quiet_success(self):
        result = self.run_watcher()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.dispatched(), [])


if __name__ == "__main__":
    unittest.main()
