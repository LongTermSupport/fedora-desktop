"""Tests for helpers.play_ledger.health_refresh — the post-play host-health refresh.

The callback plugin cannot be imported here (no `ansible`), so the decisions live in the
module under test and the subprocess is injected.
"""

from __future__ import annotations

import os
import re
import subprocess
import sys
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import health_refresh

REPO_ROOT = os.path.join(os.path.dirname(__file__), "..", "..", "..")
UNIT = os.path.join(REPO_ROOT, "files", "home", ".config", "systemd", "user",
                    "host-health-collect.service.j2")
#: Where a systemctl call that queues a job and walks away would hide.
TREES = ("helpers/play_ledger", "helpers/host_health", "callback_plugins")
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
        self.timeouts: list[object] = []

    def __call__(self, argv: list[str], **kwargs: object) -> subprocess.CompletedProcess:
        self.calls.append(argv)
        self.timeouts.append(kwargs.get("timeout"))
        assert kwargs.get("check") is False
        assert kwargs.get("timeout")
        answer = self.answers.pop(0)
        if isinstance(answer, BaseException):
            raise answer
        assert isinstance(answer, subprocess.CompletedProcess)
        return answer


class TestRequest(unittest.TestCase):
    def test_an_installed_unit_is_restarted_and_waited_for(self) -> None:
        # Waited for, so a run ends only once the document describes its own records: the
        # shell a batch hands back reads it at once, and a collection that fails is
        # reported here instead of leaving the old document standing unannounced.
        runner = _Runner(_done(stdout="loaded\n"), _done())
        self.assertIsNone(health_refresh.request(runner))
        self.assertEqual(
            runner.calls[1],
            ["systemctl", "--user", "restart", health_refresh.COLLECT_UNIT],
        )

    def test_the_wait_outlasts_the_collection_the_unit_allows(self) -> None:
        runner = _Runner(_done(stdout="loaded\n"), _done())
        health_refresh.request(runner)
        self.assertGreater(runner.timeouts[1], _unit_start_timeout())

    def test_a_collection_that_fails_is_reported(self) -> None:
        runner = _Runner(_done(stdout="loaded\n"), _done(
            returncode=1, stderr="Job for host-health-collect.service failed because the "
            "control process exited with error code.\n"))
        line = health_refresh.request(runner)
        self.assertIn(health_refresh.FAILURE_MARKER, line or "")
        self.assertIn("control process exited", line or "")

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


def _unit_start_timeout() -> int:
    with open(UNIT, encoding="utf-8") as handle:
        found = re.search(r"^TimeoutStartSec=(\d+)$", handle.read(), re.MULTILINE)
    assert found, f"{UNIT} states no TimeoutStartSec in seconds"
    return int(found.group(1))


class TestNoFireAndForgetRefresh(unittest.TestCase):
    """Guard for the class 'a refresh that returns before the work it asks for is done':
    the caller's run ends while a reader still sees the old state, and a failure of that
    work is reported to nobody. No systemctl call in the ledger or health code queues a
    job and walks away."""

    def test_no_systemctl_call_there_passes_no_block(self) -> None:
        found = []
        for tree in TREES:
            for dirpath, _dirs, files in os.walk(os.path.join(REPO_ROOT, tree)):
                for name in sorted(files):
                    if not name.endswith(".py"):
                        continue
                    path = os.path.join(dirpath, name)
                    with open(path, encoding="utf-8") as handle:
                        for number, line in enumerate(handle, start=1):
                            if re.search(r"""["']--no-block["']""", line):
                                found.append(f"{os.path.relpath(path, REPO_ROOT)}:{number}")
        self.assertEqual(found, [])


class TestUnitNameMatchesThePlaybook(unittest.TestCase):
    def test_the_play_that_deploys_the_unit_uses_the_same_name(self) -> None:
        # Two spellings of one unit name disagree silently: the refresh would find
        # `not-found`, skip quietly, and the icon would go back to stale.
        with open(PLAYBOOK, encoding="utf-8") as handle:
            text = handle.read()
        self.assertIn(f"collect_service: {health_refresh.COLLECT_UNIT}", text)


if __name__ == "__main__":
    unittest.main()
