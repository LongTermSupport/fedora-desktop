"""wsi-stream --ensure-server: the warm server is up, or the reason it is not (Plan 00179).

`stt` runs this before handing the server a recording, so the server is started the one
way an Insert starts it (start_server: the Settings' language and model, the start race
with the login unit). Exit 0 once the server answers; 1 with the reason on stderr.

Stdlib only; start_server and the Settings are stubs. Run by scripts/test-wsi-stop-grace.bash.
"""

import contextlib
import importlib.util
import io
import pathlib
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_ensure_server")


class EnsureServerTest(unittest.TestCase):

    def ensure(self, start_server, language_setting="system"):
        stderr = io.StringIO()
        with mock.patch.object(wsi_stream, "start_server", start_server) as started, \
                mock.patch.object(wsi_stream, "read_setting", lambda key: {"language": language_setting}[key]), \
                mock.patch.object(wsi_stream, "session_language", lambda setting: "en" if setting == "system" else setting), \
                contextlib.redirect_stderr(stderr):
            code = wsi_stream.run_ensure_server()
        return code, stderr.getvalue(), started

    def test_a_server_that_answers_is_exit_0_started_with_the_settings_language(self):
        code, _err, started = self.ensure(mock.Mock(return_value=True), language_setting="de")
        self.assertEqual(code, 0)
        started.assert_called_once_with("de")

    def test_a_server_that_does_not_come_up_is_exit_1_naming_the_log(self):
        code, err, _ = self.ensure(mock.Mock(return_value=False))
        self.assertEqual(code, 1)
        self.assertIn("server.log", err)

    def test_a_server_that_fails_to_load_is_exit_1_with_its_error(self):
        for failure in (RuntimeError("the server exited while loading: CUDA out of memory"),
                        wsi_stream.ModelNotDownloaded("download distil-large-v3.5 first")):
            with self.subTest(failure=failure):
                code, err, _ = self.ensure(mock.Mock(side_effect=failure))
                self.assertEqual(code, 1)
                self.assertIn(str(failure), err)


if __name__ == "__main__":
    unittest.main()
