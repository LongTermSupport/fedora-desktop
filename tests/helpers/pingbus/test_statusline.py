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
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.pingbus import cli, hooks, statusline

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

    def test_the_inner_exit_status_is_kept(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo DAEMON FAILED; exit 1")
        self.assertEqual(self.render(), (f"DAEMON FAILED | {CYAN}bus: alpha{RESET}", 1))

    def test_unreadable_settings_are_shown_not_skipped(self) -> None:
        self.record("sess-1", "alpha")
        (self.project / ".claude/settings.json").write_text("{not json", encoding="utf-8")
        text, rc = self.render()
        self.assertIn(f"{RED}status line: {self.project}/.claude/settings.json is not valid JSON{RESET}", text)
        self.assertIn("bus: alpha", text)
        self.assertEqual(rc, 1)

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

    def test_pingbus_status_line_prints_the_line_and_returns_its_status(self) -> None:
        self.record("sess-1", "alpha")
        self.settings(self.project / ".claude/settings.json", "echo inner; exit 3")
        out, err = io.StringIO(), io.StringIO()
        rc = cli.main(["status-line"], environ=self.env, stdout=out, stderr=err,
                      stdin=io.BytesIO(self.payload()))
        self.assertEqual((out.getvalue(), rc), (f"inner | {CYAN}bus: alpha{RESET}\n", 3))

    def test_the_kit_settings_make_it_the_status_line(self) -> None:
        kit = pathlib.Path(__file__).resolve().parents[3] / "files/opt/claude-yolo/optional/agent-bus"
        settings = json.loads((kit / "settings.json").read_text(encoding="utf-8"))
        self.assertEqual(settings["statusLine"], {"type": "command", "command": statusline.COMMAND})


if __name__ == "__main__":
    unittest.main()
