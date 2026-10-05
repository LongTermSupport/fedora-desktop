"""wsi-stream's start_server() when the server it waits for dies while loading.

A server that cannot load its model (a model.bin an interrupted download never wrote,
GPU memory the model does not fit in) logs why and exits within seconds. The client
must stop waiting at once and name that error, not wait out its 45 s and report a
timeout that hides the cause (Plan 00156 Task 2.1; CLAUDE/QA.md
"ready-wait-ignores-child-exit"). That holds both for a server this client started and
for one already loading when Insert was pressed (started at login, or by another
Insert), which this client has no handle on.

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

wsi_stream = stt_stubs.load_script("wsi-stream", "wsi_stream_server_start")

SERVER_LOG = """\
[2026-01-01T00:00:00] [SERVER] [INFO] === WSI-Stream Server Starting ===
[2026-01-01T00:00:02] [SERVER] [INFO] Loading model=large-v3, language=en, device=cuda (float16)
[2026-01-01T00:00:06] [SERVER] [ERROR] Model load failed: CUDA failed with error out of memory
[2026-01-01T00:00:06] [SERVER] [ERROR] Failed to load the models - exiting
"""

# What a server that finds the lock held appends to the running server's log
# (wsi-stream-server claim_pid_file): four ERROR lines, none of them the winner's error.
GAVE_WAY = """\
[2026-01-01T00:00:01] [SERVER] [ERROR] ERROR: Server already running (PID 42)
[2026-01-01T00:00:01] [SERVER] [ERROR] PID file: /run/user/1000/wsi-stream-server.pid
[2026-01-01T00:00:01] [SERVER] [ERROR] To force restart, kill the existing server first:
[2026-01-01T00:00:01] [SERVER] [ERROR]   kill 42
"""

# An earlier session's log, left in place when a server exits before it empties it.
STALE_RUN = """\
[2025-12-31T09:00:00] [SERVER] [INFO] === WSI-Stream Server Starting ===
[2025-12-31T09:00:03] [SERVER] [ERROR] Model load failed: an error from yesterday
"""


class Server:
    """A Popen stand-in: `exit_code` None while the process runs."""

    def __init__(self, exit_code):
        self.exit_code = exit_code

    def poll(self):
        return self.exit_code


class StartServerTest(unittest.TestCase):

    def setUp(self):
        tmp = pathlib.Path(tempfile.mkdtemp())
        self.server_log = tmp / "server.log"
        self.sleeps = []
        self.spawned = []
        self.server = Server(exit_code=1)
        self.answers = iter([])
        self.pid_alive = [False]
        self.writes = None
        for name, value in {
            "LOG_FILE": tmp / "debug.log",
            "SERVER_SCRIPT": pathlib.Path(__file__),
            "is_server_running": lambda: next(self.answers, False),
            "server_pid_alive": lambda: self.pid_alive.pop(0) if len(self.pid_alive) > 1
            else self.pid_alive[0],
            "resolve_model": lambda mode, language: "large-v3",
            "read_server_idle_timeout": lambda: 20,
        }.items():
            self.patch(wsi_stream, name, value)
        # create=True: a control run against a wsi-stream that predates the server's log
        # reaches the behaviour under test instead of failing on a missing name.
        self.patch(wsi_stream, "SERVER_LOG_FILE", self.server_log, create=True)
        self.patch(wsi_stream.subprocess, "Popen", self.popen)
        self.patch(wsi_stream.time, "sleep", self.sleeps.append)

    def patch(self, target, name, value, create=False):
        patcher = mock.patch.object(target, name, value, create=create)
        patcher.start()
        self.addCleanup(patcher.stop)

    def popen(self, cmd, **kwargs):
        """The started server writes its log as the real one does: `self.writes` is
        (mode, text), "w" for a server that empties the log at its start, "a" for one
        that exits before it gets that far."""
        self.spawned.append(cmd)
        if self.writes is not None:
            mode, text = self.writes
            with self.server_log.open(mode) as log:
                log.write(text)
        return self.server

    def test_a_server_that_comes_up_is_used(self):
        self.server = Server(exit_code=None)
        self.answers = iter([False, True])
        self.assertTrue(wsi_stream.start_server("en"))
        self.assertEqual(len(self.spawned), 1)

    def test_a_server_that_exits_names_its_error_at_once(self):
        self.server_log.write_text(STALE_RUN)
        self.writes = ("w", SERVER_LOG)
        with self.assertRaisesRegex(RuntimeError, "CUDA failed with error out of memory"):
            wsi_stream.start_server("en")
        self.assertEqual(len(self.sleeps), 1, "the client kept waiting for a dead server")

    def test_a_server_that_exits_without_a_log_says_so(self):
        with self.assertRaisesRegex(RuntimeError, "cannot be read"):
            wsi_stream.start_server("en")
        self.assertEqual(len(self.sleeps), 1)

    def test_a_server_that_lost_a_start_race_waits_for_the_winner(self):
        # Two starts at once (Insert and the login unit): the second server finds the
        # lock held and exits 1, and the first comes up a moment later.
        # Answers: the check before starting, the first try, the second try.
        self.pid_alive = [False, True]
        self.answers = iter([False, False, True])
        self.assertTrue(wsi_stream.start_server("en"))
        self.assertEqual(len(self.spawned), 1)
        self.assertEqual(len(self.sleeps), 2)

    def test_the_block_a_giving_way_server_appends_is_not_the_error(self):
        # The winner's log, with a loser's four lines between its start and its error.
        start, rest = SERVER_LOG.split("\n", 1)
        self.server_log.write_text(STALE_RUN)
        self.writes = ("w", start + "\n" + GAVE_WAY + rest)
        with self.assertRaisesRegex(RuntimeError, "loading: Model load failed: CUDA"):
            wsi_stream.start_server("en")

    def test_a_server_that_exits_before_emptying_its_log_names_its_own_error(self):
        # Refused before it empties the log, it appends to an earlier session's.
        self.server_log.write_text(STALE_RUN)
        self.writes = ("a", "[2026-01-01T00:00:00] [SERVER] [ERROR] ERROR: cannot open the "
                            "PID file /run/user/1000/wsi-stream-server.pid: Permission denied\n")
        with self.assertRaisesRegex(RuntimeError, "loading: ERROR: cannot open the PID file"):
            wsi_stream.start_server("en")

    def test_a_loading_server_started_elsewhere_that_exits_names_its_error(self):
        # Alive when Insert looked (so nothing is spawned), gone at the first try. Its
        # log holds an earlier session's run and a loser's block before its own error.
        self.pid_alive = [True, False]
        start, rest = SERVER_LOG.split("\n", 1)
        self.server_log.write_text(STALE_RUN + start + "\n" + GAVE_WAY + rest)
        with self.assertRaisesRegex(RuntimeError, "CUDA failed with error out of memory"):
            wsi_stream.start_server("en")
        self.assertEqual(self.spawned, [])
        self.assertEqual(len(self.sleeps), 1, "the client kept waiting for a dead server")


if __name__ == "__main__":
    unittest.main()
