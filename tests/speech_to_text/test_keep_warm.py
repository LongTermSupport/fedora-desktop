"""Keeping the streaming server warm (Plan 00148 Phase 8).

The server's idle timeout comes from the extension's server-idle-timeout-minutes key
(0 = never shut down), passed by wsi-stream when it starts the server. A user unit
runs `wsi-stream --server-at-login`, which becomes the server only when Settings ask
for it (streaming on, startup mode "server", server-start-at-login on) and no server
is running or loading. An exclusive flock on the server's PID file, held for the
server's lifetime, keeps an Insert, the login unit and a second Insert from ever
running two servers; a leftover file without the lock never blocks a start.

Stdlib only; nothing here loads a model, opens a microphone or needs GNOME. Run by
scripts/test-wsi-stop-grace.bash.
"""

import configparser
import contextlib
import fcntl
import importlib.util
import io
import os
import pathlib
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

_spec = importlib.util.spec_from_file_location(
    "stt_stubs", pathlib.Path(__file__).resolve().parent / "stt_stubs.py")
stt_stubs = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(stt_stubs)

server = stt_stubs.load_script("wsi-stream-server", "wsi_stream_server_keep_warm")
wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_keep_warm")

_UNIT = (stt_stubs.REPO_ROOT / "files" / "home" / ".config" / "systemd" / "user"
         / "wsi-stream-server-at-login.service")

# Settings that ask for the server at login, as wsi-setting prints them
_WANTED = {
    "streaming-mode": "true",
    "streaming-startup-mode": "server",
    "server-start-at-login": "true",
    "language": "system",
    "whisper-model": "auto",
    "server-idle-timeout-minutes": "0",
}


class ServerIdleTimeoutTest(unittest.TestCase):
    def setUp(self):
        patcher = mock.patch.multiple(server, recording_active=False,
                                      last_activity_time=server.time.time() - 10_000)
        patcher.start()
        self.addCleanup(patcher.stop)

    def test_zero_never_shuts_down(self):
        with mock.patch.object(server, "idle_timeout", 0):
            self.assertFalse(server.check_idle_timeout())

    def test_a_timeout_shuts_down_once_exceeded(self):
        with mock.patch.object(server, "idle_timeout", 1200):
            self.assertTrue(server.check_idle_timeout())

    def test_the_default_is_twenty_minutes(self):
        self.assertEqual(server.DEFAULT_IDLE_TIMEOUT, 1200)

    def test_help_states_the_real_default_and_zero(self):
        result = subprocess.run([sys.executable, str(stt_stubs.BIN / "wsi-stream-server"), "--help"],
                                capture_output=True, text=True, timeout=30, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        help_text = " ".join(result.stdout.split())
        self.assertIn("(default: 1200)", help_text)
        self.assertIn("0 = never", help_text)
        self.assertNotIn("300", help_text)

    def test_a_negative_timeout_is_refused(self):
        result = subprocess.run([sys.executable, str(stt_stubs.BIN / "wsi-stream-server"),
                                 "--timeout", "-5"],
                                capture_output=True, text=True, timeout=30, check=False)
        self.assertEqual(result.returncode, 2)
        self.assertIn("--timeout", result.stderr)


class ServerPidLockTest(unittest.TestCase):
    """The PID file's flock is the server lock; the PID written in it is information."""

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        tmp = pathlib.Path(self.tmp.name)
        self.pid_file = tmp / "wsi-stream-server.pid"
        patcher = mock.patch.multiple(server, PID_FILE=self.pid_file, LOG_DIR=tmp,
                                      LOG_FILE=tmp / "server.log", pid_lock_fd=None)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(server.release_pid_file)

    def server_process(self, body, *args):
        """Run `body` in a fresh Python with the server script loaded as `s`."""
        code = (
            "import pathlib, sys, time\n"
            f"sys.path.insert(0, {str(pathlib.Path(__file__).resolve().parent)!r})\n"
            "import stt_stubs\n"
            "s = stt_stubs.load_script('wsi-stream-server', 'srv')\n"
            f"s.PID_FILE = pathlib.Path({str(self.pid_file)!r})\n"
            "s.LOG_DIR = s.PID_FILE.parent; s.LOG_FILE = s.LOG_DIR / 'server.log'\n"
            + body)
        return subprocess.Popen([sys.executable, "-c", code, *args], stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, text=True)

    def race(self, starts=6):
        """Start servers that all claim at the same moment; return how many won."""
        go = pathlib.Path(self.tmp.name) / "go"
        body = (
            "go = pathlib.Path(sys.argv[1])\n"
            "while not go.exists(): time.sleep(0.001)\n"
            "won = s.claim_pid_file()\n"
            "print('won' if won else 'lost', flush=True)\n"
            "time.sleep(1.0)\n")  # hold the lock while the others try
        procs = [self.server_process(body, str(go)) for _ in range(starts)]
        time.sleep(0.5)  # let every process load the script and reach the wait
        go.touch()
        results = []
        for proc in procs:
            out, err = proc.communicate(timeout=30)
            self.assertEqual(proc.returncode, 0, err)
            results.append(out.strip())
        return results.count("won")

    def test_no_pid_file_is_claimed_for_this_process(self):
        self.assertTrue(server.claim_pid_file())
        self.assertEqual(self.pid_file.read_text(), str(os.getpid()))

    def test_racing_starts_give_exactly_one_server(self):
        self.assertEqual(self.race(), 1)

    def test_racing_starts_over_a_stale_file_give_exactly_one_server(self):
        # The stale file names a live process (a reused PID) and holds no lock
        self.pid_file.write_text(str(os.getpid()))
        self.assertEqual(self.race(), 1)

    def test_a_reused_pid_without_the_lock_does_not_block_a_start(self):
        self.pid_file.write_text(str(os.getppid()))
        self.assertTrue(server.claim_pid_file())
        self.assertEqual(self.pid_file.read_text(), str(os.getpid()))

    def test_a_pid_file_without_a_number_is_taken_over(self):
        self.pid_file.write_text("")
        self.assertTrue(server.claim_pid_file())
        self.assertEqual(self.pid_file.read_text(), str(os.getpid()))

    def test_a_running_server_keeps_its_lock_and_its_pid_file(self):
        self.assertTrue(server.claim_pid_file())
        proc = self.server_process("sys.exit(0 if s.claim_pid_file() else 3)\n")
        _out, err = proc.communicate(timeout=30)
        self.assertEqual(proc.returncode, 3, err)
        self.assertEqual(self.pid_file.read_text(), str(os.getpid()))

    def test_a_start_during_wsi_streams_brief_probe_still_claims(self):
        # wsi-stream's server_pid_alive() takes a shared lock for an instant; a server
        # starting inside that instant must wait it out, not refuse to start
        self.pid_file.write_text("")
        probe_fd = os.open(self.pid_file, os.O_RDONLY)
        self.addCleanup(os.close, probe_fd)
        fcntl.flock(probe_fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
        releaser = threading.Timer(0.05, fcntl.flock, args=(probe_fd, fcntl.LOCK_UN))
        releaser.start()
        self.addCleanup(releaser.cancel)
        self.assertTrue(server.claim_pid_file())
        self.assertEqual(self.pid_file.read_text(), str(os.getpid()))

    def test_a_lock_held_past_the_grace_still_refuses(self):
        self.pid_file.write_text("")
        holder_fd = os.open(self.pid_file, os.O_RDONLY)
        self.addCleanup(os.close, holder_fd)
        fcntl.flock(holder_fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
        self.assertFalse(server.claim_pid_file())

    def test_release_removes_the_file_and_frees_the_lock(self):
        self.assertTrue(server.claim_pid_file())
        server.release_pid_file()
        self.assertFalse(self.pid_file.exists())
        proc = self.server_process("sys.exit(0 if s.claim_pid_file() else 3)\n")
        _out, err = proc.communicate(timeout=30)
        self.assertEqual(proc.returncode, 0, err)

    def test_a_model_that_fails_to_load_leaves_no_pid_file(self):
        body = (
            "s.SOCKET_PATH = s.PID_FILE.with_name('test.socket')\n"
            "s.initialize_recorder = lambda *a: False\n"
            "sys.argv = ['wsi-stream-server']\n"
            "sys.exit(s.main())\n")
        proc = self.server_process(body)
        _out, err = proc.communicate(timeout=30)
        self.assertEqual(proc.returncode, 1, err)
        self.assertFalse(self.pid_file.exists())

    def test_wsi_stream_sees_a_server_by_its_lock_not_its_pid(self):
        with mock.patch.object(wsi_stream, "SERVER_PID_FILE", self.pid_file):
            self.assertFalse(wsi_stream.server_pid_alive())  # no file
            self.pid_file.write_text(str(os.getpid()))
            self.assertFalse(wsi_stream.server_pid_alive())  # live PID, no lock
            self.pid_file.unlink()
            self.assertTrue(server.claim_pid_file())
            self.assertTrue(wsi_stream.server_pid_alive())  # locked


class IdleTimeoutSettingTest(unittest.TestCase):
    def read(self, value):
        with mock.patch.object(wsi_stream, "read_setting", return_value=value):
            return wsi_stream.read_server_idle_timeout()

    def test_minutes_become_seconds(self):
        self.assertEqual(self.read("20"), 1200)

    def test_zero_stays_zero(self):
        self.assertEqual(self.read("0"), 0)

    def test_anything_but_a_whole_number_fails(self):
        for bad in ("", "-1", "1.5", "uint32 3"):
            with self.assertRaises(RuntimeError, msg=bad):
                self.read(bad)

    def test_the_server_command_carries_model_language_and_timeout(self):
        with mock.patch.object(wsi_stream, "debug_mode", False):
            cmd = wsi_stream.server_command("large-v3-turbo", "de", 0)
        self.assertEqual(cmd[1:], ["--model", "large-v3-turbo", "--language", "de", "--timeout", "0"])
        self.assertTrue(cmd[0].endswith("/.local/bin/wsi-stream-server"))


class StartServerOnInsertTest(unittest.TestCase):
    """start_server(), the lazy start an Insert makes in server mode."""

    def setUp(self):
        self.popen = mock.Mock()
        self.resolve = mock.Mock(return_value="base")
        for patcher in (
                mock.patch.multiple(wsi_stream, resolve_model=self.resolve,
                                    read_server_idle_timeout=lambda: 1200, debug_mode=False,
                                    SERVER_SCRIPT=stt_stubs.BIN / "wsi-stream-server"),
                mock.patch.object(wsi_stream.subprocess, "Popen", self.popen),
                mock.patch.object(wsi_stream.time, "sleep", lambda seconds: None)):
            patcher.start()
            self.addCleanup(patcher.stop)

    def test_a_server_still_loading_is_waited_for_not_doubled(self):
        answers = iter([False, False, True])
        with mock.patch.object(wsi_stream, "is_server_running", lambda: next(answers)), \
                mock.patch.object(wsi_stream, "server_pid_alive", return_value=True):
            self.assertTrue(wsi_stream.start_server("en"))
        self.popen.assert_not_called()
        self.resolve.assert_not_called()

    def test_no_server_starts_one_with_the_idle_timeout(self):
        answers = iter([False, True])
        with mock.patch.object(wsi_stream, "is_server_running", lambda: next(answers)), \
                mock.patch.object(wsi_stream, "server_pid_alive", return_value=False):
            self.assertTrue(wsi_stream.start_server("en"))
        cmd = self.popen.call_args.args[0]
        self.assertEqual(cmd[cmd.index("--timeout") + 1], "1200")
        self.assertEqual(cmd[cmd.index("--model") + 1], "base")
        self.resolve.assert_called_once_with("streaming", "en")


class ServerAtLoginTest(unittest.TestCase):
    """run_server_at_login(), the ExecStart of the start-at-login unit."""

    def setUp(self):
        self.settings = dict(_WANTED)
        self.execv = mock.Mock()
        self.resolve = mock.Mock(return_value="distil-whisper/distil-large-v3.5-ct2")
        patcher = mock.patch.multiple(
            wsi_stream, read_setting=lambda key: self.settings[key], resolve_model=self.resolve,
            is_server_running=mock.Mock(return_value=False),
            server_pid_alive=mock.Mock(return_value=False), debug_mode=False)
        patcher.start()
        self.addCleanup(patcher.stop)
        execv = mock.patch.object(wsi_stream.os, "execv", self.execv)
        execv.start()
        self.addCleanup(execv.stop)
        env = mock.patch.dict(os.environ, {"LANG": "en_GB.UTF-8"})
        env.start()
        self.addCleanup(env.stop)

    def run_unit(self):
        self.stderr = io.StringIO()
        with contextlib.redirect_stderr(self.stderr):
            return wsi_stream.run_server_at_login()

    def test_wanted_becomes_the_server_with_the_settings(self):
        self.settings["whisper-model"] = "small"
        self.run_unit()
        path, cmd = self.execv.call_args.args
        self.assertEqual(path, cmd[0])
        self.assertTrue(cmd[0].endswith("/.local/bin/wsi-stream-server"))
        self.assertEqual(cmd[1:], ["--model", "distil-whisper/distil-large-v3.5-ct2",
                                   "--language", "en", "--timeout", "0"])
        self.resolve.assert_called_once_with("streaming", "en", setting="small")

    def test_the_system_language_is_the_locales(self):
        os.environ["LANG"] = "de_DE.UTF-8"
        self.run_unit()
        cmd = self.execv.call_args.args[1]
        self.assertEqual(cmd[cmd.index("--language") + 1], "de")

    def test_each_setting_off_starts_nothing(self):
        for key, off in (("streaming-mode", "false"), ("streaming-startup-mode", "pre-buffer"),
                         ("streaming-startup-mode", "standard"), ("server-start-at-login", "false")):
            with self.subTest(key=key, value=off):
                self.settings = dict(_WANTED, **{key: off})
                self.assertEqual(self.run_unit(), 0)
                self.execv.assert_not_called()
                self.resolve.assert_not_called()
                self.assertIn("not starting the server", self.stderr.getvalue())

    def test_a_running_server_is_not_doubled(self):
        wsi_stream.is_server_running.return_value = True
        self.assertEqual(self.run_unit(), 0)
        self.execv.assert_not_called()

    def test_a_loading_server_is_not_doubled(self):
        wsi_stream.server_pid_alive.return_value = True
        self.assertEqual(self.run_unit(), 0)
        self.execv.assert_not_called()

    def test_unreadable_settings_fail_loudly(self):
        def broken(key):
            raise RuntimeError("wsi-setting failed")
        wsi_stream.read_setting = broken
        with self.assertRaises(RuntimeError):
            self.run_unit()
        self.execv.assert_not_called()

    def test_main_routes_the_flag_before_the_recorder_cleanup(self):
        registered = mock.Mock()
        with mock.patch.object(wsi_stream, "run_server_at_login", return_value=0) as run, \
                mock.patch.object(wsi_stream.atexit, "register", registered), \
                mock.patch.object(sys, "argv", ["wsi-stream", "--server-at-login"]):
            self.assertEqual(wsi_stream.main(), 0)
        run.assert_called_once_with()
        registered.assert_not_called()


class LoginUnitTest(unittest.TestCase):
    def setUp(self):
        self.unit = configparser.ConfigParser(strict=False, interpolation=None)
        self.unit.optionxform = str
        self.unit.read(_UNIT)

    def test_it_runs_wsi_stream_at_login(self):
        self.assertEqual(self.unit["Service"]["ExecStart"], "%h/.local/bin/wsi-stream --server-at-login")
        self.assertEqual(self.unit["Install"]["WantedBy"], "graphical-session.target")
        self.assertEqual(self.unit["Unit"]["PartOf"], "graphical-session.target")

    def test_it_never_restarts_the_server(self):
        self.assertEqual(self.unit["Service"]["Restart"], "no")

    def test_its_main_process_is_the_server(self):
        self.assertEqual(self.unit["Service"]["Type"], "simple")


if __name__ == "__main__":
    unittest.main()
