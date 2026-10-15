"""BusTeamsHandler - the status line names the agent-bus teams THIS session is on (Plan 00201 S-C6).

The hooks daemon is one process shared by sessions, so the handler never reads the environment: it
reads the record the pingbus SessionStart hook wrote for the session id in the status payload.
"""

import os
import sys
import unittest
import unittest.mock
from pathlib import Path
from tempfile import TemporaryDirectory

sys.path.insert(0, str(Path(__file__).parent.parent))

import bus_teams
from bus_teams import BusTeamsHandler


class BusTeamsTest(unittest.TestCase):
    def setUp(self):
        self.dir = Path(self.enterContext(TemporaryDirectory()))
        self.enterContext(unittest.mock.patch.object(bus_teams, "sessions_dir", lambda: self.dir))
        self.handler = BusTeamsHandler()

    def record(self, session_id, text, mode=0o600):
        path = self.dir / session_id
        path.write_text(text, encoding="ascii")
        path.chmod(mode)

    def segment(self, session_id="sess-1"):
        payload = {} if session_id is None else {"session_id": session_id}
        return self.handler.handle(payload).context

    def test_name_and_priority(self):
        self.assertEqual(self.handler.name, "bus-teams")
        self.assertEqual(self.handler.priority, 25)

    def test_it_always_matches(self):
        self.assertTrue(self.handler.matches({}))

    def test_a_sessions_teams_are_shown_in_order(self):
        self.record("sess-1", "alpha,beta")
        (text,) = self.segment()
        self.assertTrue(text.startswith("| "))
        self.assertIn("bus: alpha,beta", text)

    def test_one_team_is_shown(self):
        self.record("sess-1", "alpha")
        (text,) = self.segment()
        self.assertIn("bus: alpha", text)

    def test_another_sessions_teams_are_never_shown(self):
        self.record("sess-2", "alpha,beta")
        self.assertEqual(self.segment("sess-1"), [])

    def test_the_environment_is_never_read(self):
        with unittest.mock.patch.dict(os.environ, {"PINGBUS_TEAMS": "alpha"}):
            self.assertEqual(self.segment("sess-1"), [])

    def test_no_session_id_shows_nothing(self):
        self.record("sess-1", "alpha")
        self.assertEqual(self.segment(None), [])

    def test_a_session_id_that_is_not_a_plain_name_shows_nothing(self):
        self.record("sess-1", "alpha")
        for bad in ("../sess-1", "a/b", "", "x" * 129, 5, None, "sess 1"):
            with self.subTest(bad=bad):
                self.assertEqual(self.handler.handle({"session_id": bad}).context, [])

    def test_a_missing_record_shows_nothing(self):
        self.assertEqual(self.segment(), [])

    def test_a_record_that_is_not_a_team_list_shows_nothing(self):
        for bad in ("", "alpha,", "Al pha", "a\x1b[31m,b", "alpha,alpha", "x" * 25, ",", "alpha,,beta", "alpha\nbeta"):
            with self.subTest(bad=bad):
                self.record("sess-1", bad)
                self.assertEqual(self.segment(), [])

    def test_a_record_others_could_write_is_not_trusted(self):
        self.record("sess-1", "alpha", mode=0o666)
        self.assertEqual(self.segment(), [])

    def test_a_symlink_is_not_followed(self):
        target = self.dir / "elsewhere"
        target.write_text("alpha", encoding="ascii")
        target.chmod(0o600)
        (self.dir / "sess-1").symlink_to(target)
        self.assertEqual(self.segment(), [])

    def test_the_default_place_is_per_user_and_can_be_moved_by_the_variable(self):
        with unittest.mock.patch.dict(os.environ, {bus_teams.ENV_SESSIONS_DIR: "/somewhere"}):
            self.assertEqual(bus_teams.default_sessions_dir(), Path("/somewhere"))
        with unittest.mock.patch.dict(os.environ, {}, clear=True):
            self.assertEqual(bus_teams.default_sessions_dir(), Path(f"/tmp/pingbus-sessions-{os.getuid()}"))

    def test_it_explains_itself(self):
        explained = self.handler.explain_segment()
        self.assertIn("bus:", explained.glyphs)
        self.assertIn("session", explained.how_to_read)


if __name__ == "__main__":
    unittest.main()
