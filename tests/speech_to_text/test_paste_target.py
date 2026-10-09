"""wsi-stream asks the panel how to paste before each paste (Plan 00148 Phase 9).

Two long dictations into GNOME Text Editor transcribed in full and never pasted: the key
was chosen once, at Insert, and every app off a short Ctrl+V list got the terminal key
Ctrl+Shift+V, which GTK 4 text views do not bind. Now the panel is asked before each
paste (PasteKey on D-Bus), and a continuous dictation can paste in chunks as it goes.
The panel answers for the window focused at Insert (Task 9.10): if focus has moved it
gives that window focus back and the paste waits for it; a closed window, or one that
never gets focus back, is not pasted into. A window given focus back must keep it for a
moment before the paste, and its Enter waits longer (Plan 00164: the text was pasted but
the Enter did not send it); the panel is then asked again before the Enter, which is not
sent into a window that was closed or would not take focus back.

Covered: the panel's reply is parsed (five fields, or three from a panel that does not
pin), and a panel that cannot answer leaves the key chosen at Insert; the wait for focus
and its two failures; the keys pressed for each answer (Ctrl+S only when asked, after the
Enter); and the chunk paster's offsets and markers. That its chunks put together are
exactly what one paste at stop pastes is checked against the real single-paste path, by
running run_server_mode both ways (test_server_client.ChunkedDictationTest).

Stdlib only. Run by scripts/test-wsi-stop-grace.bash.
"""

import importlib.util
import pathlib
import subprocess
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_paste_target")

CTRL, SHIFT, V, S, ENTER = 29, 42, 47, 31, 28


def completed(stdout="", returncode=0, stderr=""):
    return subprocess.CompletedProcess([], returncode, stdout=stdout, stderr=stderr)


class ParsePasteTargetTest(unittest.TestCase):

    def test_a_gui_app(self):
        self.assertEqual(
            wsi_stream.parse_paste_target("('org.gnome.TextEditor', false, true, true, false)\n"),
            ("org.gnome.TextEditor", False, True, True, False))

    def test_a_terminal_not_yet_focused(self):
        self.assertEqual(wsi_stream.parse_paste_target("('kitty', true, false, false, false)"),
                         ("kitty", True, False, False, False))

    def test_a_closed_window(self):
        self.assertEqual(wsi_stream.parse_paste_target("('', false, false, false, true)"),
                         ("", False, False, False, True))

    def test_a_panel_that_does_not_pin_answers_for_the_focused_window(self):
        self.assertEqual(wsi_stream.parse_paste_target("('kitty', true, false)"),
                         ("kitty", True, False, True, False))

    def test_anything_else_is_no_answer(self):
        for reply in ("", "('kitty', true)", "(true, false, false)", "Error: no such method",
                      "('kitty', true, false, true)"):
            self.assertIsNone(wsi_stream.parse_paste_target(reply), reply)


class PasteTargetNowTest(unittest.TestCase):
    """The panel's answers before one paste (Task 9.10: the window pinned at Insert)."""

    def setUp(self):
        self.waits = []
        for name, value in {"log": lambda *a, **k: None}.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        patcher = mock.patch.object(wsi_stream.time, "sleep", self.waits.append)
        patcher.start()
        self.addCleanup(patcher.stop)

    def ask(self, *results, for_enter=False):
        """paste_target_now against the panel answering each of `results` in turn
        (the last repeats); the number of times it was asked is self.asked."""
        replies = list(results)

        def run(*args, **kwargs):
            self.asked += 1
            result = replies.pop(0) if len(replies) > 1 else replies[0]
            if isinstance(result, Exception):
                raise result
            return result

        self.asked = 0
        with mock.patch.object(wsi_stream.subprocess, "run", run):
            return wsi_stream.paste_target_now(fallback_with_shift=True, for_enter=for_enter)

    def test_the_panel_decides(self):
        self.assertEqual(self.ask(completed("('org.gnome.TextEditor', false, true, true, false)")),
                         (False, True, False))
        self.assertEqual(self.asked, 1)
        self.assertEqual(self.waits, [], "a window that kept focus is pasted into at once")

    def test_a_panel_that_does_not_pin_still_decides(self):
        self.assertEqual(self.ask(completed("('org.gnome.TextEditor', false, true)")),
                         (False, True, False))

    def test_a_panel_without_paste_key_leaves_the_insert_choice_and_no_save(self):
        self.assertEqual(self.ask(completed("", 1, "No such interface")), (True, False, False))

    def test_a_panel_that_does_not_answer_leaves_the_insert_choice(self):
        self.assertEqual(self.ask(subprocess.TimeoutExpired(["gdbus"], 2)), (True, False, False))

    def test_the_pinned_window_given_focus_back_is_waited_for_and_left_to_settle(self):
        """Plan 00164: the panel sees focus move the moment it activates the window; the
        app takes it a moment later. The paste waits until the window has kept focus for
        PASTE_FOCUS_SETTLE_SECONDS, asking at each poll, and says focus was given back."""
        unfocused = completed("('org.gnome.TextEditor', false, true, false, false)")
        focused = completed("('org.gnome.TextEditor', false, true, true, false)")
        settle_polls = round(wsi_stream.PASTE_FOCUS_SETTLE_SECONDS / wsi_stream.PASTE_FOCUS_POLL_SECONDS)
        self.assertGreater(settle_polls, 0)
        self.assertEqual(self.ask(unfocused, unfocused, focused), (False, True, True))
        self.assertEqual(self.asked, 3 + settle_polls,
                         "not asked until focus was back, then at each poll while it settled")
        self.assertTrue(self.waits and all(0 < s <= 0.1 for s in self.waits), self.waits)
        self.assertAlmostEqual(sum(self.waits[2:]), wsi_stream.PASTE_FOCUS_SETTLE_SECONDS, delta=0.05)

    def test_focus_lost_again_while_settling_starts_the_settle_again(self):
        unfocused = completed("('org.gnome.TextEditor', false, true, false, false)")
        focused = completed("('org.gnome.TextEditor', false, true, true, false)")
        settle_polls = round(wsi_stream.PASTE_FOCUS_SETTLE_SECONDS / wsi_stream.PASTE_FOCUS_POLL_SECONDS)
        self.assertEqual(self.ask(unfocused, focused, focused, unfocused, focused), (False, True, True))
        self.assertEqual(self.asked, 4 + 1 + settle_polls)

    def test_a_pinned_window_that_never_takes_focus_is_not_pasted_into(self):
        unfocused = completed("('org.gnome.TextEditor', false, true, false, false)")
        with self.assertRaises(wsi_stream.PasteTargetUnavailable) as raised:
            self.ask(unfocused)
        self.assertIn("focus", str(raised.exception))
        self.assertAlmostEqual(sum(self.waits), wsi_stream.PASTE_FOCUS_WAIT_SECONDS, delta=0.2)

    def test_a_closed_pinned_window_is_not_pasted_into(self):
        with self.assertRaises(wsi_stream.PasteTargetUnavailable) as raised:
            self.ask(completed("('', false, false, false, true)"))
        self.assertIn("closed", str(raised.exception))
        self.assertEqual(self.asked, 1)

    def test_before_the_enter_a_window_that_kept_focus_is_asked_once(self):
        focused = completed("('org.gnome.Ptyxis', true, false, true, false)")
        self.assertEqual(self.ask(focused, for_enter=True), (True, False, False))
        self.assertEqual(self.asked, 1)
        self.assertEqual(self.waits, [])

    def test_before_the_enter_focus_lost_again_is_given_back_and_left_to_settle(self):
        """Plan 00164 (H2): focus moved again after the paste, so the Enter went to
        another window. It is given back and settles, as before the paste."""
        unfocused = completed("('org.gnome.Ptyxis', true, false, false, false)")
        focused = completed("('org.gnome.Ptyxis', true, false, true, false)")
        settle_polls = round(wsi_stream.PASTE_FOCUS_SETTLE_SECONDS / wsi_stream.PASTE_FOCUS_POLL_SECONDS)
        self.assertEqual(self.ask(unfocused, focused, for_enter=True), (True, False, True))
        self.assertEqual(self.asked, 2 + settle_polls)

    def test_before_the_enter_a_closed_window_takes_no_enter(self):
        with self.assertRaises(wsi_stream.PasteTargetUnavailable) as raised:
            self.ask(completed("('', false, false, false, true)"), for_enter=True)
        self.assertIn("closed", str(raised.exception))


class AutoPasteKeysTest(unittest.TestCase):
    """The keys auto_paste presses, for each answer the panel can give."""

    def setUp(self):
        self.pressed = []
        self.timeline = []  # presses and sleeps, in order
        for name, value in {
            "log": lambda *a, **k: None,
            "copy_to_clipboard": lambda text, use_clipboard=False: True,
            "press": lambda env, *keys: self.pressed.append(keys) or self.timeline.append(keys),
        }.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        patcher = mock.patch.object(wsi_stream.time, "sleep", self.timeline.append)
        patcher.start()
        self.addCleanup(patcher.stop)

    def target(self, before_paste, before_enter=None):
        """A paste_target_now answering `before_paste`, and `before_enter` (default the
        same) when asked again before the Enter; an exception is raised. Its asks are
        self.asks, True for the one before the Enter, and are in self.timeline."""
        self.asks = []

        def paste_target_now(fallback, for_enter=False):
            self.asks.append(for_enter)
            self.timeline.append("asked")
            answer = before_enter if for_enter and before_enter is not None else before_paste
            if isinstance(answer, Exception):
                raise answer
            return answer
        return paste_target_now

    def paste(self, target, text="hello", skip_enter=False):
        with mock.patch.object(wsi_stream, "paste_target_now", self.target(target)):
            self.assertTrue(wsi_stream.auto_paste(text, skip_enter=skip_enter))
        return self.pressed

    def wait_before_enter(self):
        """The wait after the paste: before the Enter, and before any ask for it"""
        paste = next(i for i, step in enumerate(self.timeline) if step in ((CTRL, V), (CTRL, SHIFT, V)))
        return self.timeline[paste + 1]

    def test_a_gui_app_gets_ctrl_v_then_enter(self):
        self.assertEqual(self.paste((False, False, False)), [(CTRL, V), (ENTER,)])

    def test_a_terminal_gets_ctrl_shift_v(self):
        self.assertEqual(self.paste((True, False, False)), [(CTRL, SHIFT, V), (ENTER,)])

    def test_save_follows_the_enter(self):
        self.assertEqual(self.paste((False, True, False)), [(CTRL, V), (ENTER,), (CTRL, S)])

    def test_a_chunk_has_no_enter_but_is_saved(self):
        self.assertEqual(self.paste((False, True, False), skip_enter=True), [(CTRL, V), (CTRL, S)])

    def test_nothing_left_to_paste_sends_only_the_enter(self):
        self.assertEqual(self.paste((False, False, False), text=""), [(ENTER,)])

    def test_nothing_to_paste_and_no_enter_presses_nothing(self):
        self.assertEqual(self.paste((False, True, False), text="", skip_enter=True), [])

    def test_a_short_paste_into_a_window_that_kept_focus_waits_the_usual_time_for_enter(self):
        self.paste((True, False, False), text="hello")
        self.assertAlmostEqual(self.wait_before_enter(), 0.3 + len("hello") * 0.002)

    def test_a_paste_into_a_window_given_focus_back_waits_longer_for_enter(self):
        """Plan 00164: pasted, but the Enter did not send it, when focus had been given back."""
        self.assertEqual(self.paste((True, False, True), text="hello"), [(CTRL, SHIFT, V), (ENTER,)])
        self.assertEqual(self.wait_before_enter(), wsi_stream.PASTE_ENTER_DELAY_AFTER_REFOCUS_SECONDS)
        self.assertGreater(wsi_stream.PASTE_ENTER_DELAY_AFTER_REFOCUS_SECONDS, 0.3 + len("hello") * 0.002)

    def test_a_window_that_kept_focus_is_not_asked_again_before_the_enter(self):
        self.paste((True, False, False))
        self.assertEqual(self.asks, [False])

    def test_after_a_retake_the_panel_is_asked_again_just_before_the_enter(self):
        """Plan 00164 (H2): focus can move away again after the paste. The panel is asked
        after the wait, so a window that lost it again is given it back before the Enter."""
        self.assertEqual(self.paste((True, False, True)), [(CTRL, SHIFT, V), (ENTER,)])
        self.assertEqual(self.asks, [False, True])
        enter = self.timeline.index((ENTER,))
        self.assertEqual(self.timeline[enter - 2:enter],
                         [wsi_stream.PASTE_ENTER_DELAY_AFTER_REFOCUS_SECONDS, "asked"])

    def test_a_retake_with_no_enter_due_asks_once(self):
        self.assertEqual(self.paste((False, True, True), skip_enter=True), [(CTRL, V), (CTRL, S)])
        self.assertEqual(self.asks, [False])

    def test_a_window_closed_after_the_paste_takes_no_enter_and_no_save(self):
        closed = wsi_stream.PasteTargetUnavailable("the window this dictation started in was closed")
        with mock.patch.object(wsi_stream, "paste_target_now", self.target((False, True, True), closed)), \
                self.assertRaises(wsi_stream.EnterNotSent) as raised:
            wsi_stream.auto_paste("hello")
        self.assertEqual(self.pressed, [(CTRL, V)], "pasted, then nothing pressed into another window")
        self.assertIn("closed", str(raised.exception))

    def test_no_paste_target_presses_nothing_and_copies_nothing(self):
        copied = []

        def unavailable(fallback):
            raise wsi_stream.PasteTargetUnavailable("the window this dictation started in was closed")

        with mock.patch.object(wsi_stream, "paste_target_now", unavailable), \
                mock.patch.object(wsi_stream, "copy_to_clipboard",
                                  lambda text, use_clipboard=False: copied.append(text) or True), \
                self.assertRaises(wsi_stream.PasteTargetUnavailable):
            wsi_stream.auto_paste("hello")
        self.assertEqual(self.pressed, [])
        self.assertEqual(copied, [], "the caller says where the text goes")


class PasteAndReportTest(unittest.TestCase):
    """What the end of a dictation reports when its Enter could not be sent"""

    def test_pasted_but_not_sent_is_said_and_fails(self):
        notified, signals = [], []
        args = mock.Mock(no_auto_enter=False, paste_with_shift=True)

        def not_sent(*args, **kwargs):
            raise wsi_stream.EnterNotSent("the window this dictation started in was closed")

        with mock.patch.object(wsi_stream, "auto_paste", not_sent), \
                mock.patch.object(wsi_stream, "log", lambda *a, **k: None), \
                mock.patch.object(wsi_stream, "emit_dbus_text", lambda *a: signals.append(a)), \
                mock.patch.object(wsi_stream, "emit_dbus_signal", lambda *a: signals.append(a)), \
                mock.patch.object(wsi_stream, "desktop_notification",
                                  lambda message, timeout: notified.append((message, timeout))), \
                mock.patch.object(wsi_stream.sys, "stderr"):
            self.assertEqual(wsi_stream.paste_and_report("hello", args, "hello"), 1)
        self.assertEqual(len(notified), 1)
        message, timeout = notified[0]
        self.assertIn("pasted but not sent", message)
        self.assertIn("closed", message)
        self.assertEqual(timeout, 0, "the notification stays")
        self.assertIn(("StateChanged", "ERROR"), signals)


class ChunkPasterTest(unittest.TestCase):

    def run_dictation(self, snapshots, final, wrap_marker, fail_at=()):
        """The chunks pasted for each `text_so_far` snapshot, then the last one at stop."""
        chunks = wsi_stream.ChunkPaster(wrap_marker)
        pasted = []
        for i, text in enumerate(snapshots):
            pending = chunks.next_chunk(text)
            if pending is None:
                continue
            chunk, after = pending
            if i in fail_at:
                continue  # the paste failed: not counted
            pasted.append(chunk)
            chunks.mark_pasted(after)
        if chunks.started:
            pasted.append(chunks.last_chunk(final))
        return pasted

    def test_chunks_join_to_the_single_paste(self):
        final = "so my first experience was llama cpp which ran a model locally"
        snapshots = ["so my first experience", "so my first experience was llama cpp",
                     "so my first experience was llama cpp"]
        pasted = self.run_dictation(snapshots, final, wrap_marker=True)
        self.assertEqual("".join(pasted),
                         'speech-to-text:"So my first experience was llama cpp which ran a model locally"')
        self.assertEqual(pasted[1], " was llama cpp", "a chunk carries the space before it")

    def test_no_text_yet_means_no_chunk(self):
        chunks = wsi_stream.ChunkPaster(False)
        self.assertIsNone(chunks.next_chunk(""))
        self.assertFalse(chunks.started)

    def test_a_failed_chunk_goes_out_with_the_next(self):
        final = "one two three"
        pasted = self.run_dictation(["one", "one two", "one two three"], final,
                                    wrap_marker=False, fail_at={1})
        self.assertEqual(pasted, ["One", " two three", ""])

    def test_nothing_new_at_stop_leaves_only_the_closing_marker(self):
        pasted = self.run_dictation(["all of it"], "all of it", wrap_marker=True)
        self.assertEqual(pasted, ['speech-to-text:"All of it', '"'])


if __name__ == "__main__":
    unittest.main()
