"""Unit tests for helpers/pingbus/statusline.py and `pingbus status-line`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_statusline

Plan 00201 S-C6: a session on agent-bus teams shows `bus: A,B` in its status line, in ANY
checkout. The member kit's settings.json makes `pingbus status-line` the session's status line
command; it runs the status line the session would otherwise have had and adds the teams the
SessionStart hook recorded for this session id. A bus session with no usable record says so in
red, so a failed record never looks like a session on no team.
"""

from __future__ import annotations

import io
import json
import os
import pathlib
import signal
import stat
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, config, hooks, membership, protocol, statusline

CYAN, RED, RESET = "\033[36m", "\033[31m", "\033[0m"


class StatusLineTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.sessions = self.tmp / "sessions"
        self.project = self.tmp / "project"
        self.home = self.tmp / "home"
        (self.project / ".claude").mkdir(parents=True)
        (self.home / ".claude").mkdir(parents=True)
        self.env = {hooks.SESSIONS_DIR_VAR: str(self.sessions), "HOME": str(self.home),
                    "PINGBUS_TEAMS": "alpha,beta", "PATH": os.environ.get("PATH", "/usr/bin:/bin")}

    def record(self, session_id: str, text: str, mode: int = 0o600) -> pathlib.Path:
        self.sessions.mkdir(mode=0o700, exist_ok=True)
        path = self.sessions / session_id
        path.write_text(text, encoding="ascii")
        path.chmod(mode)
        return path

    def settings(self, where: pathlib.Path, command: str | None, **extra: object) -> None:
        body: dict = dict(extra)
        if command is not None:
            body["statusLine"] = {"type": "command", "command": command}
        where.write_text(json.dumps(body), encoding="utf-8")

    def payload(self, session_id: object = "sess-1") -> bytes:
        return json.dumps({"session_id": session_id,
                           "workspace": {"project_dir": str(self.project)}}).encode()

    def render(self, session_id: object = "sess-1") -> tuple[str, int]:
        return statusline.render(self.payload(session_id), self.env)

    # ── the teams segment ─────────────────────────────────────────────────────────────

    def test_a_sessions_teams_are_shown_in_order(self) -> None:
        self.record("sess-1", "alpha,beta")
        self.assertEqual(self.render(), (f"{CYAN}bus: alpha,beta{RESET}", 0))

    def test_another_sessions_teams_are_never_shown(self) -> None:
        self.record("sess-2", "gamma")
        text, _ = self.render("sess-1")
        self.assertNotIn("gamma", text)

    def test_a_bus_session_with_no_record_says_so_in_red(self) -> None:
        text, rc = self.render()
        self.assertEqual(text, f"{RED}bus: no record of this session's teams (pingbus status){RESET}")
        self.assertEqual(rc, 0)

    def test_an_unusable_record_is_reported_never_shown(self) -> None:
        for bad in ("", "alpha,", "Al pha", "a\x1b[31m,b", "alpha,alpha", "x" * 25, ",", "alpha\nbeta"):
            with self.subTest(bad=bad):
                self.record("sess-1", bad)
                text, _ = self.render()
                self.assertIn("bus: no record", text)

    def test_a_record_others_could_write_is_not_trusted(self) -> None:
        self.record("sess-1", "alpha", mode=0o666)
        self.assertIn("bus: no record", self.render()[0])

    def test_a_symlinked_record_is_not_followed(self) -> None:
        target = self.record("elsewhere", "alpha")
        (self.sessions / "sess-1").symlink_to(target)
        self.assertIn("bus: no record", self.render()[0])

    def test_a_session_id_that_is_not_a_plain_name_reads_no_record(self) -> None:
        self.record("sess-1", "alpha")
        for bad in ("../sess-1", "a/b", "", "x" * 129, 5, None, "sess 1"):
            with self.subTest(bad=bad):
                self.assertIn("bus: no record", self.render(bad)[0])

    def test_a_session_on_no_team_shows_no_segment(self) -> None:
        del self.env["PINGBUS_TEAMS"]
        self.assertEqual(self.render(), ("", 0))

    def test_the_record_is_read_where_the_hook_writes_it(self) -> None:
        hooks.record_session_teams(self.env, "sess-1", ["alpha", "beta"])
        self.assertIn("bus: alpha,beta", self.render()[0])

    # ── the status line the session would have had ────────────────────────────────────

    def test_the_projects_status_line_runs_first_with_the_same_input(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "cat >/dev/null; printf 'model | dir'")
        self.assertEqual(self.render(), (f"model | dir | {CYAN}bus: alpha{RESET}", 0))
        echo = "python3 -c 'import json,sys; print(json.load(sys.stdin)[\"session_id\"])'"
        self.settings(self.project / ".claude/settings.json", echo)
        self.assertEqual(self.render()[0], f"sess-1 | {CYAN}bus: alpha{RESET}")

    def test_local_project_settings_win_then_project_then_user(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.home / ".claude/settings.json", "echo user")
        self.assertTrue(self.render()[0].startswith("user | "))
        self.settings(self.project / ".claude/settings.json", "echo project")
        self.assertTrue(self.render()[0].startswith("project | "))
        self.settings(self.project / ".claude/settings.local.json", "echo local")
        self.assertTrue(self.render()[0].startswith("local | "))

    def test_settings_without_a_status_line_are_passed_over(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.local.json", None, model="x")
        self.settings(self.project / ".claude/settings.json", "echo project")
        self.assertTrue(self.render()[0].startswith("project | "))

    def test_claude_config_dir_moves_the_user_settings(self) -> None:
        self.record("sess-1", "alpha")
        moved = self.tmp / "config"
        moved.mkdir()
        self.settings(moved / "settings.json", "echo moved")
        self.env["CLAUDE_CONFIG_DIR"] = str(moved)
        self.assertTrue(self.render()[0].startswith("moved | "))

    def test_the_segment_goes_on_the_last_line(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "printf 'one\\ntwo\\n'")
        self.assertEqual(self.render()[0], f"one\ntwo | {CYAN}bus: alpha{RESET}")

    def test_a_failing_inner_status_line_is_shown_in_red_and_exits_0(self) -> None:
        """Claude Code blanks the whole status line on a non-zero exit, which would hide the
        bus segment and the inner's own message: the failure is shown instead."""
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo DAEMON FAILED; exit 1")
        self.assertEqual(self.render(), (
            f"DAEMON FAILED {RED}(status line exit 1){RESET} | {CYAN}bus: alpha{RESET}", 0))

    def test_unreadable_settings_are_shown_not_skipped(self) -> None:
        self.record("sess-1", "alpha")
        (self.project / ".claude/settings.json").write_text("{not json", encoding="utf-8")
        text, rc = self.render()
        self.assertIn(f"{RED}status line: {self.project}/.claude/settings.json is not valid JSON{RESET}", text)
        self.assertIn("bus: alpha", text)
        self.assertEqual(rc, 0)

    def test_a_hung_inner_status_line_times_out_and_its_whole_group_is_killed(self) -> None:
        pidfile = self.tmp / "grandchild"
        command = f"sleep 60 & echo $! > {pidfile}; wait"
        started = time.monotonic()
        text, rc = statusline.run_command(command, b"{}", self.env, None, timeout=1.0)
        self.assertLess(time.monotonic() - started, 10)
        self.assertEqual(rc, statusline.TIMED_OUT)
        self.assertGone(int(pidfile.read_text()))

    def test_a_cancelled_pingbus_kills_the_inner_status_line_group(self) -> None:
        """Claude Code cancels an in-flight status line command when a new update comes; the
        inner command and anything it started must not outlive pingbus."""
        pidfile = self.tmp / "grandchild"
        self.settings(self.project / ".claude/settings.json", f"sleep 60 & echo $! > {pidfile}; wait")
        repo = str(pathlib.Path(__file__).resolve().parents[3])
        proc = subprocess.Popen(
            [sys.executable, "-c", "import sys; from helpers.pingbus import statusline; sys.exit(statusline.main())"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, cwd=repo, env=self.env)
        proc.stdin.write(self.payload())
        proc.stdin.close()
        deadline = time.monotonic() + 10
        while not (pidfile.exists() and pidfile.read_text().strip()) and time.monotonic() < deadline:
            self.assertIsNone(proc.poll(), "pingbus status-line ended before its inner started")
            time.sleep(0.02)
        grandchild = int(pidfile.read_text())
        proc.send_signal(signal.SIGTERM)
        self.assertNotEqual(proc.wait(timeout=10), 0)
        proc.stdout.close()
        self.assertGone(grandchild)

    def assertGone(self, pid: int) -> None:
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            try:
                state = pathlib.Path(f"/proc/{pid}/stat").read_text().rsplit(")", 1)[1].split()[0]
            except (FileNotFoundError, ProcessLookupError):
                return  # gone before the open, or reaped between the open and the read (ESRCH)
            if state in ("Z", "X"):
                return
            time.sleep(0.02)
        self.fail(f"process {pid} outlived the status line")

    def test_a_process_that_vanishes_mid_read_counts_as_gone(self) -> None:
        """The /proc race: the stat file opens, then the process is reaped before the read, and
        the read raises ESRCH. That is the process gone, never a failure."""
        for error in (ProcessLookupError, FileNotFoundError):
            with self.subTest(error=error.__name__), \
                    mock.patch.object(pathlib.Path, "read_text", side_effect=error):
                self.assertGone(os.getpid())

    def test_it_never_runs_itself(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "pingbus status-line")
        self.settings(self.home / ".claude/settings.json", "echo user")
        self.assertEqual(self.render()[0], f"user | {CYAN}bus: alpha{RESET}")

    def test_the_project_dir_falls_back_to_the_variable(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo project")
        self.env["CLAUDE_PROJECT_DIR"] = str(self.project)
        raw = json.dumps({"session_id": "sess-1"}).encode()
        self.assertTrue(statusline.render(raw, self.env)[0].startswith("project | "))

    def test_input_that_is_not_a_json_object_reads_no_record(self) -> None:
        self.record("sess-1", "alpha")
        for raw in (b"", b"[]", b"\xff", b"x" * (hooks.INPUT_MAX_BYTES + 1)):
            with self.subTest(raw=raw[:8]):
                self.assertIn("bus: no record", statusline.render(raw, self.env)[0])

    # ── the command ───────────────────────────────────────────────────────────────────

    def test_pingbus_status_line_prints_the_line_and_exits_0(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo inner")
        out, err = io.StringIO(), io.StringIO()
        rc = cli.main(["status-line"], environ=self.env, stdout=out, stderr=err,
                      stdin=io.BytesIO(self.payload()))
        self.assertEqual((out.getvalue(), rc), (f"inner | {CYAN}bus: alpha{RESET}\n", 0))

    def test_the_kit_settings_make_it_the_status_line_refreshed_every_second(self) -> None:
        """The interval is the project's (1 s): the kit's statusLine replaces the project's whole,
        and ccy --supervise reads a context sidecar the daemon's status line writes on each
        render, which goes stale in an idle session without it."""
        repo = pathlib.Path(__file__).resolve().parents[3]
        kit = repo / "files/opt/claude-yolo/optional/agent-bus"
        settings = json.loads((kit / "settings.json").read_text(encoding="utf-8"))
        self.assertEqual(settings["statusLine"],
                         {"type": "command", "command": statusline.COMMAND, "refreshInterval": 1})
        project = json.loads((repo / ".claude/settings.json").read_text(encoding="utf-8"))
        self.assertEqual(settings["statusLine"]["refreshInterval"], project["statusLine"]["refreshInterval"])

    # ── cheap to run: a render imports no other pingbus module ─────────────────────────

    def test_the_module_imports_no_other_pingbus_module(self) -> None:
        repo = str(pathlib.Path(__file__).resolve().parents[3])
        done = subprocess.run([sys.executable, "-X", "importtime", "-c", "import helpers.pingbus.statusline"],
                              cwd=repo, capture_output=True, text=True, check=True)
        loaded = {line.rsplit("|", 1)[1].strip() for line in done.stderr.splitlines() if "|" in line}
        self.assertEqual(sorted(name for name in loaded if name.startswith("helpers.pingbus.")),
                         ["helpers.pingbus.statusline"])

    def test_its_copies_agree_with_the_hooks_and_the_protocol(self) -> None:
        self.assertEqual(statusline.TEAM_NAME_RE.pattern, protocol.TEAM_NAME_PATTERN)
        self.assertEqual(statusline.SESSION_ID_RE.pattern, hooks.SESSION_ID_RE.pattern)
        self.assertEqual(statusline.INPUT_MAX_BYTES, hooks.INPUT_MAX_BYTES)
        self.assertIs(hooks.sessions_dir, statusline.sessions_dir)


class MembershipSegmentTest(unittest.TestCase):
    """Plan 00203 H14: under PINGBUS_MEMBERSHIP the segment is the seats held in the session
    home's applied file that the desired file names, read on every render, never the
    SessionStart record; the copy of the intersection is pinned equal to `config.active_teams`."""

    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.mount = self.tmp / "membership"
        self.mount.mkdir()
        self.home = self.tmp / "home"
        self.home.mkdir()
        self.env = {"PINGBUS_MEMBERSHIP": str(self.mount), "PINGBUS_HOME": str(self.home),
                    "HOME": str(self.tmp), "PINGBUS_STATUS_LINE_INNER": "1",
                    hooks.SESSIONS_DIR_VAR: str(self.tmp / "sessions")}

    def write(self, desired: list[tuple[str, str]], held: list[tuple[str, str]]) -> None:
        (self.mount / membership.DESIRED_FILE).write_text(json.dumps({
            "v": 1, "session": "ccy-x-1", "kind": "ccy", "checkout": "/srv/x", "generation": 3,
            "written_ms": 1, "seats": [{"team": t, "seat": s} for t, s in desired], "farewell": []}),
            encoding="utf-8")
        membership.write_applied(self.home / membership.APPLIED_FILE, membership.Applied(
            3, tuple(membership.AppliedSeat(t, s, "myrepo.1+local.podman") for t, s in held), ()))

    def segment(self) -> str:
        return statusline.render(json.dumps({"session_id": "sess-1"}).encode(), self.env)[0]

    def test_the_teams_held_and_desired_in_desired_order(self) -> None:
        self.write([("beta", "2"), ("alpha", "1")], [("alpha", "1"), ("beta", "2"), ("gamma", "1")])
        self.assertEqual(self.segment(), f"{CYAN}bus: beta,alpha{RESET}")

    def test_a_change_shows_on_the_next_render(self) -> None:
        self.write([("alpha", "1")], [("alpha", "1")])
        self.assertEqual(self.segment(), f"{CYAN}bus: alpha{RESET}")
        self.write([("alpha", "1"), ("beta", "2")], [("alpha", "1"), ("beta", "2")])
        self.assertEqual(self.segment(), f"{CYAN}bus: alpha,beta{RESET}")

    def test_no_team_no_segment(self) -> None:
        self.write([], [])
        self.assertEqual(self.segment(), "")

    def test_an_unreadable_membership_is_said_in_red(self) -> None:
        (self.mount / membership.DESIRED_FILE).write_text("{", encoding="utf-8")
        self.assertEqual(self.segment(), f"{RED}{statusline.NO_MEMBERSHIP}{RESET}")

    def test_the_copied_names_agree(self) -> None:
        self.assertEqual((statusline.MEMBERSHIP_VAR, statusline.MEMBERSHIP_OFF, statusline.DESIRED_FILE,
                          statusline.APPLIED_FILE, statusline.MAX_MEMBERSHIP_BYTES),
                         (membership.MEMBERSHIP_VAR, membership.MEMBERSHIP_OFF, membership.DESIRED_FILE,
                          membership.APPLIED_FILE, membership.MAX_FILE_BYTES))

    def test_a_team_whose_homeserver_is_down_says_so(self) -> None:
        """H26: the watcher keeps a team whose homeserver does not answer, and marks it down."""
        self.write([("alpha", "1"), ("beta", "2")], [("alpha", "1"), ("beta", "2")])
        statusline.set_team_down(self.home, "beta", True)
        self.assertEqual(self.segment(), f"{CYAN}bus: alpha,beta(down){RESET}")
        statusline.set_team_down(self.home, "beta", False)
        self.assertEqual(self.segment(), f"{CYAN}bus: alpha,beta{RESET}")

    def test_a_stale_down_mark_is_not_believed(self) -> None:
        """A watcher killed during an outage leaves its mark; one its retries stopped refreshing
        no longer counts."""
        self.write([("alpha", "1")], [("alpha", "1")])
        statusline.set_team_down(self.home, "alpha", True)
        old = time.time() - statusline.DOWN_FRESH_S - 1
        os.utime(self.home / statusline.DOWN_DIR / "alpha", (old, old))
        self.assertEqual(self.segment(), f"{CYAN}bus: alpha{RESET}")

    def test_marking_down_refreshes_and_clearing_is_idempotent(self) -> None:
        statusline.set_team_down(self.home, "alpha", True)
        mark = self.home / statusline.DOWN_DIR / "alpha"
        old = time.time() - 100
        os.utime(mark, (old, old))
        statusline.set_team_down(self.home, "alpha", True)
        self.assertGreater(mark.stat().st_mtime, old + 50, "a retry did not refresh the mark")
        self.assertEqual(stat.S_IMODE(mark.stat().st_mode), 0o600)
        self.assertEqual(stat.S_IMODE(mark.parent.stat().st_mode), 0o700)
        statusline.set_team_down(self.home, "alpha", False)
        statusline.set_team_down(self.home, "alpha", False)
        self.assertFalse(mark.exists())

    def test_a_down_mark_that_is_not_a_regular_file_is_ignored(self) -> None:
        self.write([("alpha", "1")], [("alpha", "1")])
        (self.home / statusline.DOWN_DIR).mkdir(mode=0o700)
        (self.home / statusline.DOWN_DIR / "alpha").symlink_to(self.home / membership.APPLIED_FILE)
        self.assertEqual(self.segment(), f"{CYAN}bus: alpha{RESET}")

    def test_the_copy_agrees_with_config_active_teams(self) -> None:
        cases = [
            ([("beta", "2"), ("alpha", "1")], [("alpha", "1"), ("beta", "2")]),
            ([("alpha", "1")], [("alpha", "9")]),
            ([("alpha", "1"), ("beta", "2")], [("beta", "2"), ("gamma", "1")]),
            ([("alpha", "1")], []),
        ]
        for desired, held in cases:
            with self.subTest(desired=desired, held=held):
                self.write(desired, held)
                try:
                    expected = list(config.active_teams(self.env))
                except config.ConfigError:
                    expected = []
                self.assertEqual(statusline.membership_teams(self.env), expected)


if __name__ == "__main__":
    unittest.main()
