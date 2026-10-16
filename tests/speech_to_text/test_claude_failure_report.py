"""A Claude post-processing failure in wsi-stream is shown with notifications off (Plan 00208).

The panel passes --no-notify unless Show Notifications is on, and that setting is off by
default. report_claude_failure sent its message through desktop_notification, which
--no-notify silences, so a failed Ctrl+Insert or Alt+Insert dictation ended with nothing
pasted and nothing said: the icon flashed ERROR for two seconds, and the reason was only in
the debug log and the first 30 characters of the menu's status line. Eight other failure
notifications that stay (wsi-stream and wsi-article: nothing pasted, not sent, the final
transcription or the dictation lost) were silenced the same way, so the rule is in
desktop_notification itself: with --no-notify, a notification that stays is still shown.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import pathlib
import tempfile
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_claude_failure_report")


class ClaudeFailureReportTest(unittest.TestCase):

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        cache = pathlib.Path(tmp.name)
        self.shown = []
        self.signals = []
        for name, value in {
            "no_notify": True,
            "CACHE_DIR": cache,
            "TRANSCRIPTION_FILE": cache / "last-transcription.txt",
            "copy_to_clipboard": lambda text, use_clipboard=False: True,
            "show_notification": lambda message, expire_ms: self.shown.append((message, expire_ms)),
            "emit_dbus_signal": lambda name, value: self.signals.append((name, value)),
            "emit_dbus_text": lambda name, text: self.signals.append((name, text)),
            "log": lambda message, level="INFO": None,
        }.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    def test_the_failure_is_shown_and_stays_with_notifications_off(self):
        wsi_stream.report_claude_failure("the raw words", wsi_stream.ClaudeProcessingFailed("Not logged in"))
        self.assertEqual(len(self.shown), 1, "a failed post-processing was not shown with --no-notify")
        message, expire_ms = self.shown[0]
        self.assertIn("Claude post-processing failed: Not logged in", message)
        self.assertIn("Nothing was pasted", message)
        self.assertEqual(expire_ms, 0, "it must stay until dismissed")
        self.assertIn(("StateChanged", "ERROR"), self.signals)
        self.assertEqual(wsi_stream.TRANSCRIPTION_FILE.read_text(), "the raw words")

    def test_with_notifications_off_only_a_notification_that_stays_is_shown(self):
        # The one gate every caller goes through (wsi-article's too): a passing status
        # stays quiet, a failure that stays is never dropped
        wsi_stream.desktop_notification("Transcribed", wsi_stream.NOTIFY_MS)
        wsi_stream.desktop_notification("The dictation was lost", 0)
        self.assertEqual(self.shown, [("The dictation was lost", 0)])


if __name__ == "__main__":
    unittest.main()
