"""Tests for helpers.self_update.become_file — is the root-only become password file usable (Plan 00137 Task 6.2).

play-self-update.yml asks for `self_update_become_password` only when the root-only
`/etc/fedora-desktop/self-update.become` is not one the cycle would accept. So "usable" here
must be exactly what the cycle's own `read_private(..., secret=True)` accepts, plus the one
line run.bash reads as the password. The helper never prints the content.
"""

from __future__ import annotations

import io
import json
import os
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import become_file, cycle

PASSWORD = "correct horse"


class FileCase(unittest.TestCase):
    def setUp(self) -> None:
        self._dir = tempfile.TemporaryDirectory()
        self.addCleanup(self._dir.cleanup)
        self.path = os.path.join(self._dir.name, "self-update.become")

    def write(self, text: str, mode: int = 0o600) -> None:
        with open(self.path, "w", encoding="utf-8") as handle:
            handle.write(text)
        os.chmod(self.path, mode)


class TestInspect(FileCase):
    def test_a_file_as_the_play_writes_it_is_present(self) -> None:
        self.write(PASSWORD + "\n")
        self.assertEqual(become_file.inspect(self.path), ("present", ""))

    def test_a_file_without_the_final_newline_is_present(self) -> None:
        self.write(PASSWORD)
        self.assertEqual(become_file.inspect(self.path), ("present", ""))

    def test_no_file_is_missing(self) -> None:
        state, reason = become_file.inspect(self.path)
        self.assertEqual(state, "missing")
        self.assertIn(self.path, reason)

    def test_a_file_others_can_read_is_unusable(self) -> None:
        self.write(PASSWORD + "\n", mode=0o644)
        state, reason = become_file.inspect(self.path)
        self.assertEqual(state, "unusable")
        self.assertIn("0600", reason)

    def test_an_empty_file_is_unusable(self) -> None:
        self.write("")
        self.assertEqual(become_file.inspect(self.path)[0], "unusable")

    def test_a_blank_line_is_unusable(self) -> None:
        self.write("\n")
        self.assertEqual(become_file.inspect(self.path)[0], "unusable")

    def test_two_lines_are_unusable(self) -> None:
        self.write(PASSWORD + "\nsecond\n")
        self.assertEqual(become_file.inspect(self.path)[0], "unusable")

    def test_a_carriage_return_is_unusable(self) -> None:
        self.write(PASSWORD + "\r\n")
        self.assertEqual(become_file.inspect(self.path)[0], "unusable")

    def test_a_symlink_is_unusable_not_followed(self) -> None:
        target = os.path.join(self._dir.name, "elsewhere")
        with open(target, "w", encoding="utf-8") as handle:
            handle.write(PASSWORD + "\n")
        os.chmod(target, 0o600)
        os.symlink(target, self.path)
        state, reason = become_file.inspect(self.path)
        self.assertEqual(state, "unusable")
        self.assertIn("regular file", reason)

    def test_a_directory_is_unusable(self) -> None:
        os.mkdir(self.path, 0o700)
        self.assertEqual(become_file.inspect(self.path)[0], "unusable")

    def test_a_reason_never_carries_the_content(self) -> None:
        for text in (PASSWORD + "\nsecond\n", PASSWORD + "\r\n"):
            self.write(text)
            self.assertNotIn(PASSWORD, become_file.inspect(self.path)[1])
        self.write(PASSWORD + "\n", mode=0o640)
        self.assertNotIn(PASSWORD, become_file.inspect(self.path)[1])

    def test_it_agrees_with_the_cycle_on_permissions(self) -> None:
        """A mode the cycle refuses is never reported present, so the play cannot skip the ask."""
        for mode in (0o604, 0o640, 0o660, 0o622, 0o666):
            self.write(PASSWORD + "\n", mode=mode)
            self.assertEqual(become_file.inspect(self.path)[0], "unusable", oct(mode))


class TestMain(FileCase):
    def run_main(self) -> tuple[int, dict[str, str], str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = become_file.main([self.path], stdout=stdout, stderr=stderr)
        return code, json.loads(stdout.getvalue()), stderr.getvalue()

    def test_present_prints_its_state_and_exits_zero(self) -> None:
        self.write(PASSWORD + "\n")
        code, result, _ = self.run_main()
        self.assertEqual((code, result), (0, {"state": "present", "reason": ""}))

    def test_missing_and_unusable_are_results_not_errors(self) -> None:
        code, result, _ = self.run_main()
        self.assertEqual((code, result["state"]), (0, "missing"))
        self.write(PASSWORD + "\n", mode=0o644)
        code, result, _ = self.run_main()
        self.assertEqual((code, result["state"]), (0, "unusable"))

    def test_the_content_is_never_printed(self) -> None:
        for text, mode in ((PASSWORD + "\n", 0o600), (PASSWORD + "\nx\n", 0o600), (PASSWORD, 0o644)):
            self.write(text, mode=mode)
            stdout, stderr = io.StringIO(), io.StringIO()
            become_file.main([self.path], stdout=stdout, stderr=stderr)
            self.assertNotIn(PASSWORD, stdout.getvalue() + stderr.getvalue())

    def test_a_wrong_argument_count_is_a_usage_error(self) -> None:
        stdout, stderr = io.StringIO(), io.StringIO()
        self.assertEqual(become_file.main([], stdout=stdout, stderr=stderr), 2)
        self.assertEqual(stdout.getvalue(), "")
        self.assertIn("usage", stderr.getvalue())

    def test_a_relative_path_is_a_usage_error(self) -> None:
        stdout, stderr = io.StringIO(), io.StringIO()
        self.assertEqual(become_file.main(["self-update.become"], stdout=stdout, stderr=stderr), 2)
        self.assertEqual(stdout.getvalue(), "")


class TestOneRule(unittest.TestCase):
    def test_the_permission_rule_is_the_cycles_own(self) -> None:
        """The cycle's read_private decides, not a second copy of its checks that could drift."""
        self.assertIs(become_file.read_private, cycle.read_private)


if __name__ == "__main__":
    unittest.main()
