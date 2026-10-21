"""Unit tests for helpers/pingbus/statusline.py and `pingbus status-line`.

Run from the repo root:

    python3 -m unittest tests.helpers.pingbus.test_statusline

Plan 00201 S-C6: a session on agent-bus teams shows `👥 A,B` in its status line, in ANY
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
import shutil
import signal
import stat
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, config, hooks, inbox, membership, protocol, seat, statusline

CYAN, RED, RESET = "\033[36m", "\033[31m", "\033[0m"


def worker_roles(environ: object, teams: list[str]) -> tuple[dict[str, str | None], str | None]:
    """A role reader that finds this member a worker in every team: the layout tests' stand-in
    for the team records RoleSegmentTest reads for real."""
    return dict.fromkeys(teams, "worker"), None


class StatusLineTest(unittest.TestCase):
    def setUp(self) -> None:
        self.enterContext(mock.patch.object(statusline, "read_roles", worker_roles))
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
        self.assertEqual(self.render(), (f"{CYAN}👥 alpha (worker), beta (worker){RESET}", 0))

    def test_another_sessions_teams_are_never_shown(self) -> None:
        self.record("sess-2", "gamma")
        text, _ = self.render("sess-1")
        self.assertNotIn("gamma", text)

    def test_a_bus_session_with_no_record_says_so_in_red(self) -> None:
        text, rc = self.render()
        self.assertEqual(text, f"{RED}👥 no record of this session's teams (pingbus status){RESET}")
        self.assertEqual(rc, 0)

    def test_an_unusable_record_is_reported_never_shown(self) -> None:
        for bad in ("", "alpha,", "Al pha", "a\x1b[31m,b", "alpha,alpha", "x" * 25, ",", "alpha\nbeta"):
            with self.subTest(bad=bad):
                self.record("sess-1", bad)
                text, _ = self.render()
                self.assertIn("👥 no record", text)

    def test_a_record_others_could_write_is_not_trusted(self) -> None:
        self.record("sess-1", "alpha", mode=0o666)
        self.assertIn("👥 no record", self.render()[0])

    def test_a_symlinked_record_is_not_followed(self) -> None:
        target = self.record("elsewhere", "alpha")
        (self.sessions / "sess-1").symlink_to(target)
        self.assertIn("👥 no record", self.render()[0])

    def test_a_session_id_that_is_not_a_plain_name_reads_no_record(self) -> None:
        self.record("sess-1", "alpha")
        for bad in ("../sess-1", "a/b", "", "x" * 129, 5, None, "sess 1"):
            with self.subTest(bad=bad):
                self.assertIn("👥 no record", self.render(bad)[0])

    def test_a_session_on_no_team_shows_no_segment(self) -> None:
        del self.env["PINGBUS_TEAMS"]
        self.assertEqual(self.render(), ("", 0))

    def test_the_record_is_read_where_the_hook_writes_it(self) -> None:
        hooks.record_session_teams(self.env, "sess-1", ["alpha", "beta"])
        self.assertIn("👥 alpha (worker), beta (worker)", self.render()[0])

    # ── the status line the session would have had ────────────────────────────────────

    def test_the_projects_status_line_runs_first_with_the_same_input(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "cat >/dev/null; printf 'model | dir'")
        self.assertEqual(self.render(), (f"model | dir\n{CYAN}👥 alpha (worker){RESET}", 0))
        echo = "python3 -c 'import json,sys; print(json.load(sys.stdin)[\"session_id\"])'"
        self.settings(self.project / ".claude/settings.json", echo)
        self.assertEqual(self.render()[0], f"sess-1\n{CYAN}👥 alpha (worker){RESET}")

    def test_local_project_settings_win_then_project_then_user(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.home / ".claude/settings.json", "echo user")
        self.assertTrue(self.render()[0].startswith("user\n"))
        self.settings(self.project / ".claude/settings.json", "echo project")
        self.assertTrue(self.render()[0].startswith("project\n"))
        self.settings(self.project / ".claude/settings.local.json", "echo local")
        self.assertTrue(self.render()[0].startswith("local\n"))

    def test_settings_without_a_status_line_are_passed_over(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.local.json", None, model="x")
        self.settings(self.project / ".claude/settings.json", "echo project")
        self.assertTrue(self.render()[0].startswith("project\n"))

    def test_claude_config_dir_moves_the_user_settings(self) -> None:
        self.record("sess-1", "alpha")
        moved = self.tmp / "config"
        moved.mkdir()
        self.settings(moved / "settings.json", "echo moved")
        self.env["CLAUDE_CONFIG_DIR"] = str(moved)
        self.assertTrue(self.render()[0].startswith("moved\n"))

    def test_the_segment_goes_on_its_own_row(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "printf 'one\\ntwo\\n'")
        self.assertEqual(self.render()[0], f"one\ntwo\n{CYAN}👥 alpha (worker){RESET}")

    def test_no_row_is_wider_than_the_terminal_after_a_wrapped_inner_status_line(self) -> None:
        """The owner's `bus: de…`: the hooks daemon's status line wraps its own segments to
        COLUMNS, so its last row can already be the full width; Claude Code cuts a row wider than
        the terminal with an ellipsis, and the bus segment must never be what is cut."""
        self.record("sess-1", "alpha,beta")
        self.env["COLUMNS"] = "40"
        self.settings(self.project / ".claude/settings.json", "printf '%s\\n%s\\n' 'a | b' " + "'" + "x" * 40 + "'")
        text, _ = self.render()
        for row in text.split("\n"):
            with self.subTest(row=row):
                self.assertLessEqual(statusline.display_width(row), 40)
        self.assertIn("alpha", text)
        self.assertIn("beta", text)

    def test_the_teams_wrap_at_the_width_never_cut(self) -> None:
        """Every team is shown whole, with its role: the list breaks after a comma onto rows
        indented under the first, each its own colour span, and nothing is ellipsised. A team
        and its role are never split across rows."""
        self.record("sess-1", "alpha,beta,gamma")
        self.env["COLUMNS"] = "20"
        self.assertEqual(self.render()[0], f"{CYAN}👥 alpha (worker),{RESET}\n{CYAN}   beta (worker),{RESET}\n"
                                           f"{CYAN}   gamma (worker){RESET}")
        self.env["COLUMNS"] = "34"
        self.assertEqual(self.render()[0], f"{CYAN}👥 alpha (worker), beta (worker),{RESET}\n"
                                           f"{CYAN}   gamma (worker){RESET}")
        self.env["COLUMNS"] = "80"
        self.assertEqual(self.render()[0], f"{CYAN}👥 alpha (worker), beta (worker), gamma (worker){RESET}")

    def test_a_team_wider_than_the_width_keeps_its_own_row_whole(self) -> None:
        self.record("sess-1", "alpha,a-very-long-team-name")
        self.env["COLUMNS"] = "10"
        self.assertEqual(self.render()[0], f"{CYAN}👥 alpha (worker),{RESET}\n"
                                           f"{CYAN}   a-very-long-team-name (worker){RESET}")

    def test_the_red_notes_wrap_between_words(self) -> None:
        self.env["COLUMNS"] = "30"
        rows = self.render()[0].split("\n")
        self.assertEqual(rows[0], f"{RED}👥 no record of this session's{RESET}")
        self.assertEqual(" ".join(row.removeprefix(RED).removesuffix(RESET).strip() for row in rows),
                         f"👥 {statusline.NO_RECORD}")
        for row in rows:
            self.assertLessEqual(statusline.display_width(row), 30)

    def test_an_unknown_width_puts_the_segment_on_one_row_of_its_own(self) -> None:
        """No COLUMNS (or one that is not a positive number): nothing says where to break, so
        the segment is one unbroken row, still below the inner status line rather than
        appended to its last row."""
        self.record("sess-1", "alpha,beta,gamma")
        self.settings(self.project / ".claude/settings.json", "echo inner")
        for columns in (None, "", "0", "abc", "-5", "²"):
            with self.subTest(columns=columns):
                self.env.pop("COLUMNS", None)
                if columns is not None:
                    self.env["COLUMNS"] = columns
                self.assertEqual(self.render()[0],
                                 f"inner\n{CYAN}👥 alpha (worker), beta (worker), gamma (worker){RESET}")

    def test_the_measured_text_is_ascii_and_the_label(self) -> None:
        """display_width only ever measures the segment: the label, team names (TEAM_NAME_RE,
        plus a role and `(down)`) and the fixed notes. Pinned to ASCII and the label, the one
        wide code point, so the heuristic's approximations never meet text they could get wrong."""
        self.assertEqual(statusline.display_width(statusline.LABEL), 2)
        for text in (statusline.NO_RECORD, statusline.NO_MEMBERSHIP, statusline.TEAM_NAME_RE.pattern,
                     statusline.ROLE_UNKNOWN, *protocol.ROLES):
            with self.subTest(text=text):
                self.assertTrue(text.isascii())
        self.record("sess-1", "alpha,beta")
        self.env["COLUMNS"] = "8"
        for row in self.render()[0].split("\n"):
            plain = statusline.ANSI_ESCAPE_RE.sub("", row).replace(statusline.LABEL, "", 1)
            self.assertTrue(plain.isascii(), row)

    def test_the_display_width_skips_colour_and_counts_the_icon_as_two(self) -> None:
        self.assertEqual(statusline.display_width(f"{CYAN}👥 ab{RESET}"), 5)
        self.assertEqual(statusline.display_width("é"), 1)

    def test_a_failing_inner_status_line_is_shown_in_red_and_exits_0(self) -> None:
        """Claude Code blanks the whole status line on a non-zero exit, which would hide the
        bus segment and the inner's own message: the failure is shown instead."""
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo DAEMON FAILED; exit 1")
        self.assertEqual(self.render(), (
            f"DAEMON FAILED {RED}(status line exit 1){RESET}\n{CYAN}👥 alpha (worker){RESET}", 0))

    def test_unreadable_settings_are_shown_not_skipped(self) -> None:
        self.record("sess-1", "alpha")
        (self.project / ".claude/settings.json").write_text("{not json", encoding="utf-8")
        text, rc = self.render()
        self.assertIn(f"{RED}status line: {self.project}/.claude/settings.json is not valid JSON{RESET}", text)
        self.assertIn("👥 alpha (worker)", text)
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

    def test_a_cancel_that_lands_while_the_inner_is_starting_still_kills_its_group(self) -> None:
        """The window the test above can only hit under load, forced: the inner command has
        started (and started its own child) but Popen has not yet returned, so the cancel
        handler cannot know the process yet. The cancel must still kill the whole group and
        end pingbus, never leave the inner running."""
        pidfile = self.tmp / "grandchild"
        real_popen = subprocess.Popen
        started: list[subprocess.Popen] = []

        def popen_then_cancel(*args: object, **kwargs: object) -> subprocess.Popen:
            proc = real_popen(*args, **kwargs)
            started.append(proc)
            self.addCleanup(self.kill_group_if_alive, proc)
            deadline = time.monotonic() + 10
            while not (pidfile.exists() and pidfile.read_text().strip()) and time.monotonic() < deadline:
                time.sleep(0.02)
            statusline._cancelled(signal.SIGTERM, None)  # what the signal runs, landing here
            return proc

        with mock.patch.object(statusline.subprocess, "Popen", popen_then_cancel), \
                self.assertRaises(SystemExit) as raised:
            statusline.run_command(f"sleep 60 & echo $! > {pidfile}; wait", b"{}", self.env, None)
        self.assertEqual(raised.exception.code, 128 + signal.SIGTERM)
        self.assertEqual(len(started), 1)
        self.assertGone(int(pidfile.read_text()))
        started[0].wait(timeout=10)

    def test_a_cancel_with_no_inner_running_ends_pingbus_at_once(self) -> None:
        with self.assertRaises(SystemExit) as raised:
            statusline._cancelled(signal.SIGTERM, None)
        self.assertEqual(raised.exception.code, 128 + signal.SIGTERM)

    @staticmethod
    def kill_group_if_alive(proc: subprocess.Popen) -> None:
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=10)
        for stream in (proc.stdin, proc.stdout):
            if stream is not None:
                stream.close()

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
        self.assertEqual(self.render()[0], f"user\n{CYAN}👥 alpha (worker){RESET}")

    def test_the_project_dir_falls_back_to_the_variable(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo project")
        self.env["CLAUDE_PROJECT_DIR"] = str(self.project)
        raw = json.dumps({"session_id": "sess-1"}).encode()
        self.assertTrue(statusline.render(raw, self.env)[0].startswith("project\n"))

    def test_input_that_is_not_a_json_object_reads_no_record(self) -> None:
        self.record("sess-1", "alpha")
        for raw in (b"", b"[]", b"\xff", b"x" * (hooks.INPUT_MAX_BYTES + 1)):
            with self.subTest(raw=raw[:8]):
                self.assertIn("👥 no record", statusline.render(raw, self.env)[0])

    # ── the command ───────────────────────────────────────────────────────────────────

    def test_pingbus_status_line_prints_the_line_and_exits_0(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo inner")
        out, err = io.StringIO(), io.StringIO()
        rc = cli.main(["status-line"], environ=self.env, stdout=out, stderr=err,
                      stdin=io.BytesIO(self.payload()))
        self.assertEqual((out.getvalue(), rc), (f"inner\n{CYAN}👥 alpha (worker){RESET}\n", 0))

    def test_the_kit_settings_make_it_the_status_line_at_the_seated_interval(self) -> None:
        """Plan 00213 Task 1.3: the kit and a ccy seated session (seat.py) refresh at one
        interval, under the 30 s after which ccy --supervise holds the context sidecar the inner
        daemon line writes on each render to be stale (an idle session renders only on the timer)."""
        repo = pathlib.Path(__file__).resolve().parents[3]
        kit = repo / "files/opt/claude-yolo/optional/agent-bus"
        settings = json.loads((kit / "settings.json").read_text(encoding="utf-8"))
        self.assertEqual(settings["statusLine"],
                         {"type": "command", "command": statusline.COMMAND,
                          "refreshInterval": seat.STATUS_LINE_REFRESH_S})
        self.assertLess(seat.STATUS_LINE_REFRESH_S, 30)

    # ── cheap to run: only a segment with teams imports the role reader's modules ──────

    def test_the_module_imports_no_other_pingbus_module(self) -> None:
        """A render outside a bus session imports nothing more; `read_roles` imports config and
        inbox when there are teams to show (RoleSegmentTest), which is still not the cli."""
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
        self.enterContext(mock.patch.object(statusline, "read_roles", worker_roles))
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
        self.assertEqual(self.segment(), f"{CYAN}👥 beta (worker), alpha (worker){RESET}")

    def test_a_change_shows_on_the_next_render(self) -> None:
        self.write([("alpha", "1")], [("alpha", "1")])
        self.assertEqual(self.segment(), f"{CYAN}👥 alpha (worker){RESET}")
        self.write([("alpha", "1"), ("beta", "2")], [("alpha", "1"), ("beta", "2")])
        self.assertEqual(self.segment(), f"{CYAN}👥 alpha (worker), beta (worker){RESET}")

    def test_no_team_no_segment(self) -> None:
        self.write([], [])
        self.assertEqual(self.segment(), "")

    def test_an_unreadable_membership_is_said_in_red(self) -> None:
        (self.mount / membership.DESIRED_FILE).write_text("{", encoding="utf-8")
        self.assertEqual(self.segment(), f"{RED}👥 {statusline.NO_MEMBERSHIP}{RESET}")

    def test_the_copied_names_agree(self) -> None:
        self.assertEqual((statusline.MEMBERSHIP_VAR, statusline.MEMBERSHIP_OFF, statusline.DESIRED_FILE,
                          statusline.APPLIED_FILE, statusline.MAX_MEMBERSHIP_BYTES),
                         (membership.MEMBERSHIP_VAR, membership.MEMBERSHIP_OFF, membership.DESIRED_FILE,
                          membership.APPLIED_FILE, membership.MAX_FILE_BYTES))

    def test_a_team_whose_homeserver_is_down_says_so(self) -> None:
        """H26: the watcher keeps a team whose homeserver does not answer, and marks it down."""
        self.write([("alpha", "1"), ("beta", "2")], [("alpha", "1"), ("beta", "2")])
        statusline.set_team_down(self.home, "beta", True)
        self.assertEqual(self.segment(), f"{CYAN}👥 alpha (worker), beta (worker) (down){RESET}")
        statusline.set_team_down(self.home, "beta", False)
        self.assertEqual(self.segment(), f"{CYAN}👥 alpha (worker), beta (worker){RESET}")

    def test_a_stale_down_mark_is_not_believed(self) -> None:
        """A watcher killed during an outage leaves its mark; one its retries stopped refreshing
        no longer counts."""
        self.write([("alpha", "1")], [("alpha", "1")])
        statusline.set_team_down(self.home, "alpha", True)
        old = time.time() - statusline.DOWN_FRESH_S - 1
        os.utime(self.home / statusline.DOWN_DIR / "alpha", (old, old))
        self.assertEqual(self.segment(), f"{CYAN}👥 alpha (worker){RESET}")

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
        self.assertEqual(self.segment(), f"{CYAN}👥 alpha (worker){RESET}")

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


class RoleSegmentTest(unittest.TestCase):
    """Plan 00203 Task 3.8, owner feedback: each team in the segment shows this member's role in
    it, `👥 alpha (coordinator), beta (worker)`, read on every render from the team record the
    role notice reads (`inbox.cached_role`), so a role change shows at the next refresh with no
    restart. A role that cannot be read is `(role?)` with one stderr line, never a guess.
    Order: the team, its role, then its marks such as `(down)`, each in its own brackets."""

    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.home = self.tmp / "pingbus"
        self.home.mkdir()
        self.sessions = self.tmp / "sessions"
        self.sessions.mkdir(mode=0o700)
        self.env = {"PINGBUS_HOME": str(self.home), "PINGBUS_TEAMS": "alpha,beta",
                    "HOME": str(self.tmp), "PINGBUS_STATUS_LINE_INNER": "1",
                    hooks.SESSIONS_DIR_VAR: str(self.sessions)}
        record = self.sessions / "sess-1"
        record.write_text("alpha,beta", encoding="ascii")
        record.chmod(0o600)

    @staticmethod
    def me(team: str) -> str:
        return f"@myrepo.1+workstation.podman:{team}.agent-bus.internal"

    def bundle(self, team: str) -> None:
        directory = self.home / team
        directory.mkdir()
        sn = f"{team}.agent-bus.internal"
        (directory / "member.json").write_text(json.dumps({
            "protocol": 1, "team": team, "user_id": self.me(team), "server_name": sn,
            "base_url": "http://127.0.0.1:1", "plain_http_hosts": ["127.0.0.1"],
            "token_file": "token", "admin": f"@admin:{sn}", "room": "!" + "R" * 43}), encoding="utf-8")
        token = directory / "token"
        token.write_text(f"syt_member_token_for_{team}", encoding="ascii")
        token.chmod(0o600)

    def roles(self, team: str, roles: dict[str, str]) -> None:
        """Team `team`'s cached record, as the watcher saves it, giving `roles` by user id."""
        sn = f"{team}.agent-bus.internal"
        inbox.TeamState(self.home / team / "state").save_team_record({
            "v": 1, "team": team, "humans": [f"@alice:{sn}"], "roles": roles,
            "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
            "path_prefixes": ["docs/"], "forge_api": "https://api.github.com"})

    def make_me(self, team: str, role: str) -> None:
        other = "worker" if role == "coordinator" else "coordinator"
        self.roles(team, {self.me(team): role, f"@orch.1+workstation.podman:{team}.agent-bus.internal": other})

    def segment(self) -> tuple[str, str]:
        err = io.StringIO()
        text, _ = statusline.render(json.dumps({"session_id": "sess-1"}).encode(), self.env, err=err)
        return text, err.getvalue()

    def test_each_team_shows_this_members_role_in_it(self) -> None:
        for team in ("alpha", "beta"):
            self.bundle(team)
        self.make_me("alpha", "coordinator")
        self.make_me("beta", "worker")
        self.assertEqual(self.segment(), (f"{CYAN}👥 alpha (coordinator), beta (worker){RESET}", ""))

    def test_a_role_change_shows_on_the_next_render_with_no_restart(self) -> None:
        for team in ("alpha", "beta"):
            self.bundle(team)
            self.make_me(team, "worker")
        self.assertEqual(self.segment()[0], f"{CYAN}👥 alpha (worker), beta (worker){RESET}")
        self.make_me("beta", "coordinator")
        self.assertEqual(self.segment()[0], f"{CYAN}👥 alpha (worker), beta (coordinator){RESET}")

    def test_the_role_is_the_one_the_role_notice_reads(self) -> None:
        for team in ("alpha", "beta"):
            self.bundle(team)
        for alpha, beta in (("coordinator", "worker"), ("worker", "coordinator"), ("worker", "worker")):
            with self.subTest(alpha=alpha, beta=beta):
                self.make_me("alpha", alpha)
                self.make_me("beta", beta)
                told = {view.team: view.role for view in hooks.survey(self.env).teams}
                self.assertEqual(statusline.read_roles(self.env, ["alpha", "beta"]), (told, None))

    def test_an_unreadable_role_is_role_unknown_and_one_stderr_line_never_a_guess(self) -> None:
        self.bundle("alpha")
        self.make_me("alpha", "worker")
        for broken in ("no bundle", "no record", "not json", "no role for this member"):
            with self.subTest(broken=broken):
                shutil.rmtree(self.home / "beta", ignore_errors=True)
                if broken != "no bundle":
                    self.bundle("beta")
                if broken == "not json":
                    (self.home / "beta/state").mkdir(mode=0o700)
                    (self.home / "beta/state" / inbox.TEAM_FILE).write_text("{", encoding="utf-8")
                if broken == "no role for this member":
                    self.roles("beta", {"@orch.1+workstation.podman:beta.agent-bus.internal": "coordinator"})
                text, err = self.segment()
                self.assertEqual(text, f"{CYAN}👥 alpha (worker), beta (role?){RESET}")
                self.assertEqual(len(err.splitlines()), 1, err)
                self.assertIn("beta", err)
                self.assertNotIn("alpha", err)

    def test_the_marks_come_after_the_role(self) -> None:
        self.bundle("alpha")
        self.make_me("alpha", "coordinator")
        statusline.set_team_down(self.home, "alpha", True)
        statusline.set_team_down(self.home, "beta", True)
        text, err = self.segment()
        self.assertEqual(text, f"{CYAN}👥 alpha (coordinator) (down), beta (role?) (down){RESET}")
        self.assertEqual(len(err.splitlines()), 1)

    def test_pingbus_status_line_writes_the_diagnostic_to_its_stderr(self) -> None:
        self.bundle("alpha")
        self.make_me("alpha", "worker")
        out, err = io.StringIO(), io.StringIO()
        rc = cli.main(["status-line"], environ=self.env, stdout=out, stderr=err,
                      stdin=io.BytesIO(json.dumps({"session_id": "sess-1"}).encode()))
        self.assertEqual((out.getvalue(), rc), (f"{CYAN}👥 alpha (worker), beta (role?){RESET}\n", 0))
        self.assertEqual(len(err.getvalue().splitlines()), 1, err.getvalue())


if __name__ == "__main__":
    unittest.main()
