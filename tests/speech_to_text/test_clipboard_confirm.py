"""wsi-stream pastes only once the clipboard reads back as this dictation's text (Plan 00208).

A dictation pasted the previous dictation's text: its wl-copy never got the selection from
the compositor (it was still running days later, stdin on the pipe), and wsi-stream slept
0.1 s and 0.3 s, read the clipboard back, logged only its length, and pressed Ctrl+Shift+V.

Now each copy is read back with wl-paste until it holds the text (one trailing newline
allowed). A copy that has not taken within a short attempt is killed and copied again; past
a hard deadline nothing is pasted and the failure is said. The copies killed are those this
run started, by their process handle, never by name.

The fakes below are real executables on PATH: a wl-copy that takes the selection at once,
late, never (it stays running, as the stuck one did), or only on its second run; and a
wl-paste that prints the selection, which starts as the previous dictation's text.

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import os
import pathlib
import subprocess
import tempfile
import time
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_clipboard_confirm")

PREVIOUS = "The previous dictation, which must never be pasted again.\n"
TEXT = "This dictation."
CTRL, SHIFT, V = 29, 42, 47

FAKE_WL_COPY = r"""#!/usr/bin/bash
set -euo pipefail
echo "$$" >> "$FAKE_CLIP/pids"
runs=$(( $(cat "$FAKE_CLIP/runs" 2>/dev/null || echo 0) + 1 ))
echo "$runs" > "$FAKE_CLIP/runs"
data=$(cat; echo x)
data=${data%x}
mode=$(cat "$FAKE_CLIP/mode")
case "$mode:$runs" in
    never:* | first-never:1) exec sleep 600 ;;
    late:*) sleep 0.3 ;;
esac
if [ "$mode" = newline ]; then data="$data"$'\n'; fi
printf '%s' "$data" > "$FAKE_CLIP/selection.tmp"
mv "$FAKE_CLIP/selection.tmp" "$FAKE_CLIP/selection"
"""

FAKE_WL_PASTE = r"""#!/usr/bin/bash
set -euo pipefail
if [ ! -f "$FAKE_CLIP/selection" ]; then
    echo "Nothing is copied" >&2
    exit 1
fi
cat "$FAKE_CLIP/selection"
"""


def alive(pid):
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    return True


class FakeClipboardCase(unittest.TestCase):

    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.dir = pathlib.Path(tmp.name)
        bin_dir = self.dir / "bin"
        bin_dir.mkdir()
        for name, body in (("wl-copy", FAKE_WL_COPY), ("wl-paste", FAKE_WL_PASTE)):
            (bin_dir / name).write_text(body)
            (bin_dir / name).chmod(0o755)
        (self.dir / "selection").write_text(PREVIOUS)
        self.logged = []
        for target, value in (
            (os.environ, {"PATH": f"{bin_dir}:{os.environ['PATH']}", "XDG_SESSION_TYPE": "wayland",
                          "FAKE_CLIP": str(self.dir)}),
        ):
            patcher = mock.patch.dict(target, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        for name, value in {
            "log": lambda message, level="INFO": self.logged.append((level, message)),
            "CLIPBOARD_CONFIRM_SECONDS": 1.5,
            "CLIPBOARD_COPY_ATTEMPT_SECONDS": 0.6,
        }.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        self.addCleanup(wsi_stream.stop_copies)

    def mode(self, mode):
        (self.dir / "mode").write_text(mode)

    def pids(self):
        path = self.dir / "pids"
        return [int(p) for p in path.read_text().split()] if path.exists() else []

    def selection(self):
        return (self.dir / "selection").read_text()


class CopyConfirmedTest(FakeClipboardCase):

    def test_a_copy_that_takes_at_once_is_confirmed(self):
        self.mode("takes")
        wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        self.assertEqual(self.selection(), TEXT)
        self.assertEqual(len(self.pids()), 1)

    def test_a_copy_that_takes_late_is_waited_for(self):
        self.mode("late")
        started = time.monotonic()
        wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        self.assertGreaterEqual(time.monotonic() - started, 0.3)
        self.assertEqual(self.selection(), TEXT)
        self.assertEqual(len(self.pids()), 1, "one copy, read back until it took")

    def test_one_trailing_newline_is_allowed(self):
        self.mode("newline")
        wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        self.assertEqual(self.selection(), TEXT + "\n")

    def test_a_copy_that_never_takes_is_killed_and_copied_again(self):
        self.mode("first-never")
        wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        first, second = self.pids()
        self.assertFalse(alive(first), "the stuck copy this run started is killed")
        self.assertEqual(self.selection(), TEXT)
        self.assertTrue(any("copying again" in m for _, m in self.logged), self.logged)

    def test_a_selection_that_never_takes_the_text_raises_and_leaves_no_copy_running(self):
        self.mode("never")
        started = time.monotonic()
        with self.assertRaises(wsi_stream.ClipboardNotTaken) as raised:
            wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        self.assertLess(time.monotonic() - started, 1.5 + 1.0, "the deadline is hard")
        self.assertGreaterEqual(len(self.pids()), 2, "copied again before giving up")
        self.assertEqual([p for p in self.pids() if alive(p)], [], "every copy it started is killed")
        self.assertIn("still held other text", str(raised.exception))
        self.assertEqual(self.selection(), PREVIOUS)

    def test_an_earlier_copy_still_running_is_killed_before_the_next(self):
        earlier = subprocess.Popen(["sleep", "600"])
        wsi_stream._copies.append(earlier)
        self.mode("takes")
        wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        self.assertIsNotNone(earlier.poll(), "killed by its handle, not matched by name")

    def test_a_copy_that_cannot_start_fails_at_once_and_is_not_retried(self):
        """Review B1: wl-copy missing spun the retry loop with no sleep (32,316 starts in 3 s)."""
        (self.dir / "bin" / "wl-copy").unlink()
        starts = []
        real_popen = subprocess.Popen

        def counting_popen(*args, **kwargs):
            starts.append(args[0])
            return real_popen(*args, **kwargs)

        started = time.monotonic()
        with mock.patch.object(wsi_stream.subprocess, "Popen", counting_popen), \
                mock.patch.dict(os.environ, {"PATH": str(self.dir / "bin")}), \
                self.assertRaises(wsi_stream.ClipboardNotTaken) as raised:
            wsi_stream.copy_confirmed(TEXT, use_clipboard=True)
        self.assertLess(time.monotonic() - started, 0.5, "no wait for the deadline")
        self.assertEqual(len(starts), 1, "one start, no retry")
        self.assertIn("could not run", str(raised.exception))
        self.assertFalse(any("copying again" in m for _, m in self.logged), self.logged)

    def test_copy_to_clipboard_is_false_when_the_copy_never_takes(self):
        self.mode("never")
        self.assertFalse(wsi_stream.copy_to_clipboard(TEXT, use_clipboard=True))
        self.assertTrue(any(level == "ERROR" for level, _ in self.logged), self.logged)


class AutoPasteGateTest(FakeClipboardCase):
    """auto_paste presses nothing until the clipboard holds the text."""

    def setUp(self):
        super().setUp()
        self.timeline = []
        for name, value in {
            "press": lambda env, *keys: self.timeline.append(("press", keys, self.selection())),
            "paste_target_now": lambda fallback, for_enter=False: (True, False, False),
        }.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    def test_the_paste_key_sees_this_dictations_text_on_the_clipboard(self):
        self.mode("late")
        self.assertTrue(wsi_stream.auto_paste(TEXT, skip_enter=True))
        self.assertEqual(self.timeline, [("press", (CTRL, SHIFT, V), TEXT)])

    def test_a_clipboard_that_never_takes_the_text_is_never_pasted(self):
        self.mode("never")
        with self.assertRaises(wsi_stream.ClipboardNotTaken):
            wsi_stream.auto_paste(TEXT)
        self.assertEqual(self.timeline, [], "no paste of the previous text, and no Enter")


class ClipboardNotTakenReportTest(unittest.TestCase):

    def run_report(self, words_pasted=0):
        notified, signals = [], []
        args = mock.Mock(no_auto_enter=False, paste_with_shift=True)

        def not_taken(*args, **kwargs):
            raise wsi_stream.ClipboardNotTaken("after 3 copies in 3 s the CLIPBOARD selection still held other text (785 characters)")

        with mock.patch.object(wsi_stream, "auto_paste", not_taken), \
                mock.patch.object(wsi_stream, "log", lambda *a, **k: None), \
                mock.patch.object(wsi_stream, "emit_dbus_text", lambda *a: signals.append(a)), \
                mock.patch.object(wsi_stream, "emit_dbus_signal", lambda *a: signals.append(a)), \
                mock.patch.object(wsi_stream, "no_notify", True), \
                mock.patch.object(wsi_stream, "show_notification",
                                  lambda message, timeout: notified.append((message, timeout))), \
                mock.patch.object(wsi_stream.sys, "stderr"):
            rc = wsi_stream.paste_and_report(TEXT, args, TEXT, words_pasted)
        return rc, notified, signals

    def test_nothing_pasted_is_said_and_fails(self):
        rc, notified, signals = self.run_report()
        self.assertEqual(rc, 1)
        self.assertEqual(len(notified), 1)
        message, timeout = notified[0]
        self.assertIn("Nothing was pasted", message)
        self.assertIn("785 characters", message)
        self.assertIn("Copy Last Transcription", message)
        self.assertEqual(timeout, 0, "the notification stays, even with --no-notify (review N4)")
        self.assertIn(("StateChanged", "ERROR"), signals)

    def test_words_already_pasted_in_chunks_are_said(self):
        _, notified, _ = self.run_report(words_pasted=12)
        self.assertIn("first 12 words were already pasted", notified[0][0])


class ChunkGateTest(unittest.TestCase):

    def test_a_chunk_the_clipboard_never_took_is_not_counted(self):
        chunks = wsi_stream.ChunkPaster(wrap_marker=False)

        def not_taken(*args, **kwargs):
            raise wsi_stream.ClipboardNotTaken("still held other text")

        with mock.patch.object(wsi_stream, "auto_paste", not_taken), \
                mock.patch.object(wsi_stream, "log", lambda *a, **k: None), \
                mock.patch.object(wsi_stream, "send_server_command") as told:
            wsi_stream.paste_chunk(chunks, "some words")
        self.assertEqual(chunks.pasted, 0, "it goes out with the next chunk")
        told.assert_not_called()


if __name__ == "__main__":
    unittest.main()
