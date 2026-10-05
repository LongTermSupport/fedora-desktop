"""wsi-stream pastes into the window focused at the moment of each paste (Plan 00148 Phase 9).

Two long dictations into GNOME Text Editor transcribed in full and never pasted: the key
was chosen once, at Insert, and every app off a short Ctrl+V list got the terminal key
Ctrl+Shift+V, which GTK 4 text views do not bind. Now the panel is asked before each
paste (PasteKey on D-Bus), and a continuous dictation can paste in chunks as it goes.

Covered: the panel's reply is parsed, and a panel that cannot answer leaves the key
chosen at Insert; the keys pressed for each answer (Ctrl+S only when asked, after the
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
        self.assertEqual(wsi_stream.parse_paste_target("('org.gnome.TextEditor', false, true)\n"),
                         ("org.gnome.TextEditor", False, True))

    def test_a_terminal(self):
        self.assertEqual(wsi_stream.parse_paste_target("('kitty', true, false)"),
                         ("kitty", True, False))

    def test_anything_else_is_no_answer(self):
        for reply in ("", "('kitty', true)", "(true, false, false)", "Error: no such method"):
            self.assertIsNone(wsi_stream.parse_paste_target(reply), reply)


class PasteTargetNowTest(unittest.TestCase):

    def setUp(self):
        for name, value in {"log": lambda *a, **k: None}.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    def ask(self, result):
        run = mock.Mock(side_effect=result) if isinstance(result, Exception) else mock.Mock(return_value=result)
        with mock.patch.object(wsi_stream.subprocess, "run", run):
            return wsi_stream.paste_target_now(fallback_with_shift=True)

    def test_the_panel_decides(self):
        self.assertEqual(self.ask(completed("('org.gnome.TextEditor', false, true)")), (False, True))

    def test_a_panel_without_paste_key_leaves_the_insert_choice_and_no_save(self):
        self.assertEqual(self.ask(completed("", 1, "No such interface")), (True, False))

    def test_a_panel_that_does_not_answer_leaves_the_insert_choice(self):
        self.assertEqual(self.ask(subprocess.TimeoutExpired(["gdbus"], 2)), (True, False))


class AutoPasteKeysTest(unittest.TestCase):
    """The keys auto_paste presses, for each answer the panel can give."""

    def setUp(self):
        self.pressed = []
        for name, value in {
            "log": lambda *a, **k: None,
            "copy_to_clipboard": lambda text, use_clipboard=False: True,
            "press": lambda env, *keys: self.pressed.append(keys),
        }.items():
            patcher = mock.patch.object(wsi_stream, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        patcher = mock.patch.object(wsi_stream.time, "sleep", lambda s: None)
        patcher.start()
        self.addCleanup(patcher.stop)

    def paste(self, target, text="hello", skip_enter=False):
        with mock.patch.object(wsi_stream, "paste_target_now", lambda fallback: target):
            self.assertTrue(wsi_stream.auto_paste(text, skip_enter=skip_enter))
        return self.pressed

    def test_a_gui_app_gets_ctrl_v_then_enter(self):
        self.assertEqual(self.paste((False, False)), [(CTRL, V), (ENTER,)])

    def test_a_terminal_gets_ctrl_shift_v(self):
        self.assertEqual(self.paste((True, False)), [(CTRL, SHIFT, V), (ENTER,)])

    def test_save_follows_the_enter(self):
        self.assertEqual(self.paste((False, True)), [(CTRL, V), (ENTER,), (CTRL, S)])

    def test_a_chunk_has_no_enter_but_is_saved(self):
        self.assertEqual(self.paste((False, True), skip_enter=True), [(CTRL, V), (CTRL, S)])

    def test_nothing_left_to_paste_sends_only_the_enter(self):
        self.assertEqual(self.paste((False, False), text=""), [(ENTER,)])

    def test_nothing_to_paste_and_no_enter_presses_nothing(self):
        self.assertEqual(self.paste((False, True), text="", skip_enter=True), [])


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
