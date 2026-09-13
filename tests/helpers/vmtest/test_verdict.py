"""Unit tests for helpers/vmtest/verdict.py — response state machine, HMAC signing, heartbeat.

Run from the repo root with no third-party deps:

    python3 -m unittest tests.helpers.vmtest.test_verdict

DESIGN.md §6.5 (liveness) and §6.6 (the response contract). Everything here is
pure: documents in, documents out. The watcher, the run scope and the
heartbeat timer are the executors that write what these functions return.
"""

from __future__ import annotations

import json
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.vmtest import spool, verdict

KEY = b"k" * 32
OTHER_KEY = b"j" * 32
NOW = 1_800_000_000
REQUEST = spool.Request(
    name="20260913T114500Z-run-scenario-0123456789abcdef.json",
    timestamp="20260913T114500Z",
    verb="run-scenario",
    argument="server-fast-provision",
    nonce="0123456789abcdef",
)


def _finished_response():
    # The shape judge_run.py prints (schema 1, state finished).
    return {
        "schema": 1,
        "verb": "run-scenario",
        "argument": "server-fast-provision",
        "state": "finished",
        "verdict": "pass",
        "run_id": "20260913T114501Z-server-fast-provision",
        "started_at": "2027-01-15T08:00:00Z",
        "finished_at": "2027-01-15T08:30:00Z",
        "checks": {"planned": 13, "total": 13, "passed": 13, "failed": 0, "skipped": 0},
        "checks_skipped": [],
        "evidence": {"transcript": "x", "base": {"kind": "fast"}, "divergences": [], "overrides": []},
        "failure": None,
    }


class TestSigning(unittest.TestCase):
    def test_sign_then_verify_round_trips(self):
        document = {"a": 1, "b": [1, 2]}
        text = verdict.sign(document, KEY, nonce="0123456789abcdef")
        self.assertEqual(verdict.verify(text, KEY), {"a": 1, "b": [1, 2], "signature": json.loads(text)["signature"]})

    def test_signature_covers_the_nonce_so_a_response_cannot_be_replayed(self):
        text = verdict.sign({"a": 1}, KEY, nonce="0123456789abcdef")
        replayed = json.loads(text)
        replayed["signature"]["nonce"] = "fedcba9876543210"
        with self.assertRaises(verdict.SignatureError):
            verdict.verify(json.dumps(replayed), KEY)

    def test_tampered_body_fails_verification(self):
        text = verdict.sign({"verdict": "fail"}, KEY, nonce="0123456789abcdef")
        tampered = json.loads(text)
        tampered["verdict"] = "pass"
        with self.assertRaises(verdict.SignatureError):
            verdict.verify(json.dumps(tampered), KEY)

    def test_wrong_key_fails_verification(self):
        text = verdict.sign({"a": 1}, KEY, nonce="0123456789abcdef")
        with self.assertRaises(verdict.SignatureError):
            verdict.verify(text, OTHER_KEY)

    def test_missing_or_malformed_signature_fails(self):
        for text in (json.dumps({"a": 1}), json.dumps({"a": 1, "signature": "abc"}), "{not json", json.dumps([])):
            with self.subTest(text=text):
                with self.assertRaises(verdict.SignatureError):
                    verdict.verify(text, KEY)

    def test_key_must_be_long_enough(self):
        with self.assertRaises(verdict.SignatureError):
            verdict.sign({"a": 1}, b"short", nonce="0123456789abcdef")

    def test_signing_is_canonical_over_key_order(self):
        one = json.loads(verdict.sign({"a": 1, "b": 2}, KEY, nonce="0123456789abcdef"))
        two = json.loads(verdict.sign({"b": 2, "a": 1}, KEY, nonce="0123456789abcdef"))
        self.assertEqual(one["signature"]["value"], two["signature"]["value"])

    def test_verify_does_not_need_the_nonce_from_outside(self):
        # The nonce is inside the signed payload, so verification is a pure
        # function of the document and the key.
        text = verdict.sign({"a": 1}, KEY, nonce="0123456789abcdef")
        self.assertEqual(verdict.verify(text, KEY)["signature"]["nonce"], "0123456789abcdef")


class TestStateMachine(unittest.TestCase):
    def test_accepted_stub_has_null_verdict_and_names_the_request(self):
        stub = verdict.accepted(REQUEST, run_id="20260913T114501Z-server-fast-provision", now=NOW)
        self.assertEqual(stub["state"], "accepted")
        self.assertIsNone(stub["verdict"])
        self.assertEqual(stub["request"], REQUEST.name)
        self.assertEqual(stub["verb"], "run-scenario")
        self.assertEqual(stub["argument"], "server-fast-provision")
        self.assertEqual(stub["run_id"], "20260913T114501Z-server-fast-provision")
        self.assertEqual(stub["accepted_at"], "2027-01-15T08:00:00Z")
        self.assertIsNone(stub["started_at"])
        self.assertIsNone(stub["finished_at"])
        self.assertEqual(stub["checks"]["planned"], None)
        self.assertIsNone(stub["failure"])

    def test_accepted_with_planned_count(self):
        stub = verdict.accepted(REQUEST, run_id="r", now=NOW, planned=13)
        self.assertEqual(stub["checks"], {"planned": 13, "total": None, "passed": None, "failed": None, "skipped": None})

    def test_rejected_is_a_terminal_response_with_the_reason(self):
        response = verdict.rejected(REQUEST.name, code="unknown-argument", reason="not in the deployed allowlist", now=NOW)
        self.assertEqual(response["state"], "rejected")
        self.assertIsNone(response["verdict"])
        self.assertEqual(response["failure"], {"stage": "allowlist", "reason": "unknown-argument: not in the deployed allowlist"})
        self.assertEqual(response["request"], REQUEST.name)

    def test_rejection_codes_map_to_stages(self):
        for code, stage in (
            ("bad-filename", "allowlist"),
            ("denylisted-verb", "allowlist"),
            ("unknown-verb", "allowlist"),
            ("verb-mismatch", "allowlist"),
            ("nonce-mismatch", "allowlist"),
            ("bad-argument", "allowlist"),
            ("bad-body", "allowlist"),
            ("unknown-argument", "allowlist"),
            ("allowlist-stale", "allowlist"),
            ("policy-deny", "allowlist"),
            ("rate-limited", "allowlist"),
            ("in-flight", "allowlist"),
        ):
            with self.subTest(code=code):
                self.assertEqual(verdict.rejected(REQUEST.name, code=code, reason="x", now=NOW)["failure"]["stage"], stage)

    def test_unknown_rejection_code_is_a_programming_error(self):
        with self.assertRaises(ValueError):
            verdict.rejected(REQUEST.name, code="made-up", reason="x", now=NOW)

    def test_running_marks_started_and_refreshes_the_heartbeat(self):
        stub = verdict.accepted(REQUEST, run_id="r", now=NOW)
        running = verdict.running(stub, now=NOW + 5)
        self.assertEqual(running["state"], "running")
        self.assertEqual(running["started_at"], "2027-01-15T08:00:05Z")
        self.assertEqual(running["heartbeat_at"], "2027-01-15T08:00:05Z")
        later = verdict.running(running, now=NOW + 65)
        self.assertEqual(later["started_at"], "2027-01-15T08:00:05Z")
        self.assertEqual(later["heartbeat_at"], "2027-01-15T08:01:05Z")
        self.assertIsNone(later["verdict"])

    def test_finished_merges_the_judged_response_into_the_stub(self):
        stub = verdict.running(verdict.accepted(REQUEST, run_id="20260913T114501Z-server-fast-provision", now=NOW), now=NOW + 5)
        final = verdict.finished(stub, _finished_response())
        self.assertEqual(final["state"], "finished")
        self.assertEqual(final["verdict"], "pass")
        self.assertEqual(final["request"], REQUEST.name)
        self.assertEqual(final["accepted_at"], "2027-01-15T08:00:00Z")
        self.assertEqual(final["checks"]["passed"], 13)
        self.assertEqual(final["evidence"]["base"]["kind"], "fast")

    def test_finished_refuses_a_response_for_a_different_run_or_argument(self):
        stub = verdict.accepted(REQUEST, run_id="other-run", now=NOW)
        with self.assertRaises(ValueError):
            verdict.finished(stub, _finished_response())
        stub = verdict.accepted(REQUEST, run_id="20260913T114501Z-server-fast-provision", now=NOW)
        judged = _finished_response()
        judged["argument"] = "desktop-fresh-install"
        with self.assertRaises(ValueError):
            verdict.finished(stub, judged)

    def test_finished_refuses_a_verdict_outside_the_three(self):
        stub = verdict.accepted(REQUEST, run_id="20260913T114501Z-server-fast-provision", now=NOW)
        judged = _finished_response()
        judged["verdict"] = "forged"
        with self.assertRaises(ValueError):
            verdict.finished(stub, judged)

    def test_transitions_only_move_forward(self):
        stub = verdict.accepted(REQUEST, run_id="20260913T114501Z-server-fast-provision", now=NOW)
        final = verdict.finished(stub, _finished_response())
        with self.assertRaises(ValueError):
            verdict.running(final, now=NOW + 99)
        with self.assertRaises(ValueError):
            verdict.finished(final, _finished_response())
        rejected = verdict.rejected(REQUEST.name, code="bad-body", reason="x", now=NOW)
        with self.assertRaises(ValueError):
            verdict.running(rejected, now=NOW + 1)

    def test_aborted_is_a_finished_error_at_stage_aborted(self):
        stub = verdict.running(verdict.accepted(REQUEST, run_id="r", now=NOW), now=NOW + 5)
        final = verdict.aborted(stub, now=NOW + 60, reason="abort-run requested")
        self.assertEqual(final["state"], "finished")
        self.assertEqual(final["verdict"], "error")
        self.assertEqual(final["failure"], {"stage": "aborted", "reason": "abort-run requested"})
        self.assertEqual(final["finished_at"], "2027-01-15T08:01:00Z")

    def test_harness_error_is_a_finished_error_with_the_named_stage(self):
        stub = verdict.running(verdict.accepted(REQUEST, run_id="r", now=NOW), now=NOW + 5)
        final = verdict.errored(stub, now=NOW + 60, stage="boot", reason="guest did not answer SSH within 600s")
        self.assertEqual((final["state"], final["verdict"]), ("finished", "error"))
        self.assertEqual(final["failure"]["stage"], "boot")
        with self.assertRaises(ValueError):
            verdict.errored(stub, now=NOW + 60, stage="made-up", reason="x")


class TestHeartbeat(unittest.TestCase):
    def heartbeat(self, **overrides):
        fields = {
            "now": NOW,
            "path_unit": {"active_state": "active", "result": "success"},
            "service_unit": {"active_state": "inactive", "result": "success"},
            "in_flight": None,
            "slug": "home-user-Projects-fedora-desktop",
        }
        fields.update(overrides)
        return verdict.heartbeat(**fields)

    def test_carries_clock_units_flight_and_the_remedy_as_a_literal(self):
        beat = self.heartbeat(in_flight="20260913T114501Z-server-fast-provision")
        self.assertEqual(beat["written_at"], "2027-01-15T08:00:00Z")
        self.assertEqual(beat["path_unit"]["active_state"], "active")
        self.assertEqual(beat["in_flight"], "20260913T114501Z-server-fast-provision")
        self.assertIn("systemctl --user reset-failed vmtest-bridge@home-user-Projects-fedora-desktop.path", beat["remedy"])
        # The container cannot compute the slug (it sees /workspace, not the host
        # path), so the heartbeat carries it and the off-mount audit-log path.
        self.assertEqual(beat["slug"], "home-user-Projects-fedora-desktop")
        self.assertEqual(beat["audit_log"], "~/.local/state/vmtest-bridge/home-user-Projects-fedora-desktop/service.log")

    def test_fresh_and_healthy_is_ok(self):
        assessment = verdict.assess_heartbeat(self.heartbeat(), now=NOW + 30, max_age=120)
        self.assertEqual(assessment.state, "ok")

    def test_stale_heartbeat_means_the_bridge_is_not_running(self):
        assessment = verdict.assess_heartbeat(self.heartbeat(), now=NOW + 121, max_age=120)
        self.assertEqual(assessment.state, "stale")
        self.assertIn("not running", assessment.reason)

    def test_failed_path_unit_means_wedged_with_the_remedy(self):
        beat = self.heartbeat(path_unit={"active_state": "failed", "result": "start-limit-hit"})
        assessment = verdict.assess_heartbeat(beat, now=NOW + 1, max_age=120)
        self.assertEqual(assessment.state, "wedged")
        self.assertIn("reset-failed", assessment.reason)

    def test_failed_service_unit_is_wedged_too(self):
        beat = self.heartbeat(service_unit={"active_state": "failed", "result": "exit-code"})
        self.assertEqual(verdict.assess_heartbeat(beat, now=NOW + 1, max_age=120).state, "wedged")

    def test_absent_heartbeat_is_never_installed(self):
        assessment = verdict.assess_heartbeat(None, now=NOW, max_age=120)
        self.assertEqual(assessment.state, "absent")

    def test_malformed_heartbeat_is_absent_not_ok(self):
        for beat in ({}, {"written_at": "yesterday"}, {"written_at": "2027-01-15T08:00:00Z"}):
            with self.subTest(beat=beat):
                self.assertEqual(verdict.assess_heartbeat(beat, now=NOW, max_age=120).state, "absent")

    def test_a_future_dated_heartbeat_is_not_fresh(self):
        # A clock that went backwards must not read as a bridge that is alive.
        beat = self.heartbeat(now=NOW + 600)
        self.assertEqual(verdict.assess_heartbeat(beat, now=NOW, max_age=120).state, "stale")


if __name__ == "__main__":
    unittest.main()
