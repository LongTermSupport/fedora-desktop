"""Tests for helpers/vmtest/request.py — the container-side requester and reader (§6.5, §6.6).

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_request

The requester runs INSIDE the sandbox. It reads the heartbeat before it writes,
writes a request atomically (tmp/ then rename into requests/), polls for the
response, and maps what it finds to a distinct exit code and reason. It never
verifies a signature — it cannot — and says so. The end-to-end cases drive it
as a subprocess against a fake bridge thread that answers the way the host
watcher and run scope would.
"""

from __future__ import annotations

import json
import os
import pathlib
import subprocess
import sys
import tempfile
import threading
import time
import unittest

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(REPO_ROOT))

from helpers.vmtest import request, spool, verdict

NOW = 1_800_000_000
SLUG = "home-user-Projects-fedora-desktop"
KEY = b"k" * 32


def fresh_heartbeat(now=NOW, **overrides):
    fields = {
        "now": now,
        "path_unit": {"active_state": "active", "result": "success"},
        "service_unit": {"active_state": "inactive", "result": "success"},
        "in_flight": None,
        "slug": SLUG,
    }
    fields.update(overrides)
    return verdict.heartbeat(**fields)


class TestRequestDocument(unittest.TestCase):
    def test_name_and_body_agree_and_match_the_spool_grammar(self):
        name, body = request.build_request("run-scenario", "server-fast-provision", now=NOW, nonce="0123456789abcdef")
        self.assertRegex(name, spool.REQUEST_NAME_RE)
        parsed = spool.parse_request(name, body)
        self.assertEqual(parsed.verb, "run-scenario")
        self.assertEqual(parsed.argument, "server-fast-provision")
        self.assertEqual(parsed.nonce, "0123456789abcdef")

    def test_verb_without_argument_carries_null(self):
        name, body = request.build_request("list-scenarios", None, now=NOW, nonce="0123456789abcdef")
        self.assertIsNone(spool.parse_request(name, body).argument)

    def test_unknown_verb_or_bad_argument_is_refused_before_anything_is_written(self):
        with self.assertRaises(request.Usage):
            request.build_request("exec", "ls", now=NOW, nonce="0123456789abcdef")
        with self.assertRaises(request.Usage):
            request.build_request("run-scenario", "../etc", now=NOW, nonce="0123456789abcdef")
        with self.assertRaises(request.Usage):
            request.build_request("lab-status", "extra", now=NOW, nonce="0123456789abcdef")

    def test_nonce_is_sixteen_hex_and_fresh(self):
        first, second = request.fresh_nonce(), request.fresh_nonce()
        self.assertRegex(first, r"^[0-9a-f]{16}$")
        self.assertNotEqual(first, second)


class TestOutcomes(unittest.TestCase):
    def outcome(self, response, now=NOW + 10, heartbeat_max_age=300):
        return request.classify(response, now=now, heartbeat_max_age=heartbeat_max_age)

    def stub(self, **overrides):
        req = spool.Request(name="20260913T114500Z-run-scenario-0123456789abcdef.json", timestamp="20260913T114500Z", verb="run-scenario", argument="server-fast-provision", nonce="0123456789abcdef")
        document = verdict.accepted(req, run_id="20260913T114500Z-server-fast-provision", now=NOW, planned=13)
        document.update(overrides)
        return document

    def test_finished_pass_is_the_only_zero(self):
        judged = {"state": "finished", "verdict": "pass", "verb": "run-scenario", "argument": "server-fast-provision", "run_id": "20260913T114500Z-server-fast-provision", "finished_at": "2027-01-15T08:20:00Z", "checks": {"planned": 13, "total": 13, "passed": 13, "failed": 0, "skipped": 0}, "failure": None}
        outcome = self.outcome(verdict.finished(self.stub(), judged))
        self.assertEqual(outcome.exit_code, 0)
        self.assertEqual(outcome.kind, "pass")

    def test_fail_error_and_rejected_are_distinct_non_zero(self):
        judged = {"state": "finished", "verdict": "fail", "verb": "run-scenario", "argument": "server-fast-provision", "run_id": "20260913T114500Z-server-fast-provision", "finished_at": "2027-01-15T08:20:00Z", "checks": {"planned": 13, "total": 13, "passed": 12, "failed": 1, "skipped": 0}, "failure": {"stage": "assert", "reason": "1 check failed"}}
        failed = self.outcome(verdict.finished(self.stub(), judged))
        errored = self.outcome(verdict.errored(self.stub(), now=NOW + 5, stage="ssh", reason="no answer"))
        rejected = self.outcome(verdict.rejected("x.json", code="policy-deny", reason="MODE_run-scenario is deny", now=NOW))
        self.assertEqual((failed.kind, errored.kind, rejected.kind), ("fail", "error", "rejected"))
        self.assertEqual(len({failed.exit_code, errored.exit_code, rejected.exit_code, 0}), 4)
        self.assertIn("ssh", errored.reason)
        self.assertIn("policy-deny", rejected.reason)

    def test_running_with_a_fresh_heartbeat_is_still_waiting(self):
        outcome = self.outcome(verdict.running(self.stub(), now=NOW + 5), now=NOW + 60)
        self.assertEqual(outcome.kind, "waiting")
        self.assertIsNone(outcome.exit_code)

    def test_running_with_a_stale_heartbeat_means_the_host_process_died(self):
        outcome = self.outcome(verdict.running(self.stub(), now=NOW + 5), now=NOW + 5 + 301)
        self.assertEqual(outcome.kind, "died")
        self.assertNotEqual(outcome.exit_code, 0)
        self.assertIn("heartbeat", outcome.reason)

    def test_a_naive_verdict_read_of_an_open_response_is_never_pass(self):
        for document in (self.stub(), verdict.running(self.stub(), now=NOW)):
            self.assertIsNone(document["verdict"])
            self.assertNotEqual(self.outcome(document).kind, "pass")

    def test_malformed_response_is_reported_not_trusted(self):
        outcome = self.outcome({"state": "finished", "verdict": "pass"})
        self.assertEqual(outcome.kind, "malformed")
        self.assertNotEqual(outcome.exit_code, 0)


class EndToEnd(unittest.TestCase):
    """The requester as a subprocess, against a fake bridge thread in this process."""

    def setUp(self):
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.checkout = pathlib.Path(self._tmp.name) / "checkout"
        self.bridge = self.checkout / "untracked" / "vmtest-bridge"
        for name in spool.SPOOL_DIRS:
            (self.bridge / name).mkdir(parents=True)
        self.write_heartbeat(fresh_heartbeat(now=int(time.time())))

    def write_heartbeat(self, document):
        (self.bridge / "diagnostics" / "bridge-heartbeat.json").write_text(json.dumps(document))

    def run_requester(self, *args, timeout="10"):
        return subprocess.run(
            [sys.executable, "-m", "helpers.vmtest.request", "--checkout", str(self.checkout), "--poll-seconds", "0.1",
             "--accept-timeout", "3", "--timeout", timeout, *args],
            cwd=REPO_ROOT, env={**os.environ}, capture_output=True, text=True, check=False,
        )

    def fake_bridge(self, answer, delay=0.2):
        """Wait for a request to appear, claim it, and write `answer(request)` as the response."""

        def body():
            deadline = time.time() + 5
            while time.time() < deadline:
                names = sorted(p.name for p in (self.bridge / "requests").iterdir())
                if names:
                    name = names[0]
                    (self.bridge / "requests" / name).rename(self.bridge / "processing" / name)
                    parsed = spool.parse_request(name, (self.bridge / "processing" / name).read_bytes())
                    time.sleep(delay)
                    for document in answer(parsed):
                        (self.bridge / "responses" / f"{name}.response.json").write_text(verdict.sign(document, KEY, nonce=parsed.nonce))
                        time.sleep(delay)
                    return
                time.sleep(0.05)

        thread = threading.Thread(target=body, daemon=True)
        thread.start()
        return thread

    def test_pass_end_to_end_exits_zero_and_states_what_it_cannot_verify(self):
        now = int(time.time())

        def answer(parsed):
            stub = verdict.accepted(parsed, run_id=f"{parsed.timestamp}-{parsed.argument}", now=now, planned=13)
            running = verdict.running(stub, now=now + 1)
            judged = {"state": "finished", "verdict": "pass", "verb": parsed.verb, "argument": parsed.argument, "run_id": stub["run_id"], "finished_at": verdict._iso(now + 2), "checks": {"planned": 13, "total": 13, "passed": 13, "failed": 0, "skipped": 0}, "failure": None, "evidence": {"transcript": f"untracked/vmtest-bridge/archive/{stub['run_id']}/transcript.log"}}
            return [stub, running, verdict.finished(running, judged)]

        thread = self.fake_bridge(answer)
        result = self.run_requester("run-scenario", "server-fast-provision")
        thread.join(timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertRegex(result.stdout, r"^VMTEST-REQUEST \S+ state=finished verdict=pass ", )
        self.assertIn("not verifiable from inside the sandbox", result.stderr)
        self.assertIn("vmtest verify ", result.stderr)
        self.assertIn(f"~/.local/state/vmtest-bridge/{SLUG}/service.log", result.stderr)
        self.assertEqual(sorted(p.name for p in (self.bridge / "tmp").iterdir()), [])

    def test_rejected_is_its_own_exit_and_names_the_code(self):
        thread = self.fake_bridge(lambda parsed: [verdict.rejected(parsed.name, code="unknown-argument", reason="not in the deployed allowlist", now=int(time.time()))])
        result = self.run_requester("run-scenario", "server-fast-provision")
        thread.join(timeout=5)
        self.assertEqual(result.returncode, request.EXIT_REJECTED)
        self.assertIn("unknown-argument", result.stderr)
        self.assertIn("state=rejected", result.stdout)

    def test_no_response_at_all_is_unknown_not_a_timeout_and_not_a_fail(self):
        result = self.run_requester("list-scenarios")
        self.assertEqual(result.returncode, request.EXIT_UNKNOWN)
        self.assertIn("watcher never answered", result.stderr)
        self.assertIn("state=unknown", result.stdout)

    def test_stale_heartbeat_refuses_to_write_a_request(self):
        self.write_heartbeat(fresh_heartbeat(now=int(time.time()) - 3600))
        result = self.run_requester("list-scenarios")
        self.assertEqual(result.returncode, request.EXIT_BRIDGE_DOWN)
        self.assertIn("not running", result.stderr)
        self.assertEqual(sorted(p.name for p in (self.bridge / "requests").iterdir()), [])

    def test_wedged_bridge_prints_the_remedy_and_writes_nothing(self):
        self.write_heartbeat(fresh_heartbeat(now=int(time.time()), path_unit={"active_state": "failed", "result": "start-limit-hit"}))
        result = self.run_requester("list-scenarios")
        self.assertEqual(result.returncode, request.EXIT_BRIDGE_WEDGED)
        self.assertIn("wedged", result.stderr)
        self.assertIn(f"systemctl --user reset-failed vmtest-bridge@{SLUG}.path", result.stderr)
        self.assertEqual(sorted(p.name for p in (self.bridge / "requests").iterdir()), [])

    def test_absent_heartbeat_means_not_installed(self):
        (self.bridge / "diagnostics" / "bridge-heartbeat.json").unlink()
        result = self.run_requester("list-scenarios")
        self.assertEqual(result.returncode, request.EXIT_BRIDGE_DOWN)
        self.assertIn("not installed", result.stderr)

    def test_usage_errors_exit_64_and_write_nothing(self):
        result = self.run_requester("exec", "ls")
        self.assertEqual(result.returncode, 64)
        self.assertEqual(sorted(p.name for p in (self.bridge / "requests").iterdir()), [])


if __name__ == "__main__":
    unittest.main()
