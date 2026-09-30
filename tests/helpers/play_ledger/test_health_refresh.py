"""Tests for helpers.play_ledger.health_refresh — the post-play host-health refresh.

The callback plugin cannot be imported here (no `ansible`), so the decisions live in the
module under test and the subprocess is injected.
"""

from __future__ import annotations

import os
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import health_refresh

REPO_ROOT = os.path.join(os.path.dirname(__file__), "..", "..", "..")
PLAYBOOK = os.path.join(
    REPO_ROOT,
    "playbooks", "imports", "optional", "common", "play-host-health-login-report.yml",
)


def _done(returncode: int = 0, stdout: str = "", stderr: str = "") -> subprocess.CompletedProcess:
    return subprocess.CompletedProcess([], returncode, stdout=stdout, stderr=stderr)


class _Runner:
    """Answers each systemctl call in order and records every argv it was given."""

    def __init__(self, *answers: object) -> None:
        self.answers = list(answers)
        self.calls: list[list[str]] = []

    def __call__(self, argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
        self.calls.append(argv)
        assert kwargs.get("check") is False
        assert kwargs.get("timeout")
        answer = self.answers.pop(0)
        if isinstance(answer, BaseException):
            raise answer
        assert isinstance(answer, subprocess.CompletedProcess)
        return answer


class TestRequest(unittest.TestCase):
    def test_an_installed_unit_is_restarted_without_blocking(self) -> None:
        runner = _Runner(_done(stdout="loaded\n"), _done())
        self.assertIsNone(health_refresh.request(runner))
        self.assertEqual(
            runner.calls[1],
            ["systemctl", "--user", "restart", "--no-block", health_refresh.COLLECT_UNIT],
        )

    def test_restart_not_start_so_a_run_already_in_flight_is_redone(self) -> None:
        # A collector the timer started before the ledger write has read the old ledger;
        # `start` would leave it running and the document would stay stale for an hour.
        runner = _Runner(_done(stdout="loaded\n"), _done())
        health_refresh.request(runner)
        self.assertIn("restart", runner.calls[1])
        self.assertNotIn("start", runner.calls[1])

    def test_a_host_without_the_unit_is_skipped_silently(self) -> None:
        # The report is an optional play. No unit means no document to go stale, and
        # warning on every playbook run of a host that never asked for it is noise.
        runner = _Runner(_done(stdout="not-found\n"))
        self.assertIsNone(health_refresh.request(runner))
        self.assertEqual(len(runner.calls), 1)

    def test_a_failed_probe_is_reported_not_swallowed(self) -> None:
        runner = _Runner(_done(returncode=1, stderr="Failed to connect to bus\n"))
        line = health_refresh.request(runner)
        self.assertIsNotNone(line)
        self.assertIn(health_refresh.FAILURE_MARKER, line or "")
        self.assertIn("Failed to connect to bus", line or "")
        self.assertEqual(len(runner.calls), 1, "no restart after a failed probe")

    def test_a_failed_restart_is_reported(self) -> None:
        runner = _Runner(_done(stdout="loaded\n"), _done(returncode=1, stderr="boom\n"))
        line = health_refresh.request(runner)
        self.assertIn(health_refresh.FAILURE_MARKER, line or "")
        self.assertIn("boom", line or "")

    def test_a_missing_systemctl_is_reported(self) -> None:
        line = health_refresh.request(_Runner(FileNotFoundError("systemctl")))
        self.assertIn(health_refresh.FAILURE_MARKER, line or "")

    def test_a_hung_systemctl_is_reported(self) -> None:
        line = health_refresh.request(_Runner(subprocess.TimeoutExpired("systemctl", 10)))
        self.assertIn(health_refresh.FAILURE_MARKER, line or "")

    def test_a_silent_failure_still_says_something(self) -> None:
        line = health_refresh.request(_Runner(_done(returncode=4)))
        self.assertIn("exit status 4", line or "")

    def test_the_report_is_one_line(self) -> None:
        line = health_refresh.request(_Runner(_done(returncode=1, stderr="a\nb\n")))
        self.assertNotIn("\n", line or "")


class TestUnitNameMatchesThePlaybook(unittest.TestCase):
    def test_the_play_that_deploys_the_unit_uses_the_same_name(self) -> None:
        # Two spellings of one unit name disagree silently: the refresh would find
        # `not-found`, skip quietly, and the icon would go back to stale.
        with open(PLAYBOOK, encoding="utf-8") as handle:
            text = handle.read()
        self.assertIn(f"collect_service: {health_refresh.COLLECT_UNIT}", text)


if __name__ == "__main__":
    unittest.main()
