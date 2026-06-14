"""Unit tests for helpers/gnome/extension_state.py — pure GNOME extension verdict logic.

Run from the repo root:

    python3 -m unittest tests.helpers.gnome.test_extension_state
"""

from __future__ import annotations

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.gnome import extension_state as es

UUID = "workspace-names-overview@fedora-desktop"


class TestClassify(unittest.TestCase):
    def test_no_session_skips_and_passes(self):
        c = es.classify(
            uuid=UUID,
            session_available=False,
            live_state=None,
            shell_major=None,
            metadata_shell_versions=["50"],
        )
        self.assertEqual(c.verdict, es.Verdict.SKIP_NO_SESSION)
        self.assertFalse(c.is_failure)
        self.assertEqual(c.exit_code, 0)

    def test_active_is_ok(self):
        c = es.classify(
            uuid=UUID,
            session_available=True,
            live_state="ACTIVE",
            shell_major="50",
            metadata_shell_versions=["50"],
        )
        self.assertEqual(c.verdict, es.Verdict.OK)
        self.assertEqual(c.exit_code, 0)

    def test_error_state_is_hard_failure(self):
        c = es.classify(
            uuid=UUID,
            session_available=True,
            live_state="ERROR",
            shell_major="50",
            metadata_shell_versions=["50"],
        )
        self.assertEqual(c.verdict, es.Verdict.FAIL_ERROR)
        self.assertTrue(c.is_failure)
        self.assertEqual(c.exit_code, 1)

    def test_out_of_date_but_metadata_supports_running_shell_is_pending_reload(self):
        # On-disk metadata already lists the running major → the running Wayland
        # session is merely stale. Pass, but tell the user to log out/in.
        c = es.classify(
            uuid=UUID,
            session_available=True,
            live_state="OUT OF DATE",
            shell_major="50",
            metadata_shell_versions=["48", "49", "50"],
        )
        self.assertEqual(c.verdict, es.Verdict.PENDING_RELOAD)
        self.assertFalse(c.is_failure)
        self.assertEqual(c.exit_code, 0)
        self.assertIn("log out", c.message.lower())

    def test_out_of_date_underscore_form_is_handled(self):
        c = es.classify(
            uuid=UUID,
            session_available=True,
            live_state="OUT_OF_DATE",
            shell_major="50",
            metadata_shell_versions=["50"],
        )
        self.assertEqual(c.verdict, es.Verdict.PENDING_RELOAD)

    def test_out_of_date_and_metadata_missing_running_shell_is_version_failure(self):
        # Genuine: metadata does not cover the running shell → fix metadata.json.
        c = es.classify(
            uuid=UUID,
            session_available=True,
            live_state="OUT OF DATE",
            shell_major="51",
            metadata_shell_versions=["48", "49", "50"],
        )
        self.assertEqual(c.verdict, es.Verdict.FAIL_VERSION)
        self.assertTrue(c.is_failure)
        self.assertEqual(c.exit_code, 1)

    def test_out_of_date_with_unknown_shell_major_fails(self):
        # Cannot prove it is merely pending → treat as a genuine failure.
        c = es.classify(
            uuid=UUID,
            session_available=True,
            live_state="OUT OF DATE",
            shell_major=None,
            metadata_shell_versions=["50"],
        )
        self.assertEqual(c.verdict, es.Verdict.FAIL_VERSION)
        self.assertTrue(c.is_failure)


if __name__ == "__main__":
    unittest.main()
