"""Unit tests for helpers/agent_bus/config_restart.py: which installed teams the next
`agent-bus-install team` run restarts because this checkout renders their tuwunel.toml
differently (Plan 00201 final review N5: the pre-swap backup also covers a config-only
release).

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_config_restart
"""

from __future__ import annotations

import io
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import config_restart, render, teamfile
from tests.helpers.agent_bus.test_render import team


class ConfigRestartTest(unittest.TestCase):
    def setUp(self) -> None:
        self.root = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))

    def install(self, name: str, *, toml: str | None = None, team_json: str | None = None) -> None:
        """A team as `team` left it: its canonical team.json and the tuwunel.toml rendered from it."""
        tf = team(team=name)
        directory = self.root / name
        directory.mkdir()
        (directory / "team.json").write_text(
            teamfile.dump_team_file(tf) if team_json is None else team_json, encoding="utf-8")
        (directory / "tuwunel.toml").write_text(render.render_toml(tf) if toml is None else toml,
                                                encoding="utf-8")

    def run_main(self, *teams: str) -> tuple[int, str, str]:
        out, err = io.StringIO(), io.StringIO()
        code = config_restart.main(["--state-root", str(self.root), *teams], stdout=out, stderr=err)
        return code, out.getvalue(), err.getvalue()

    def test_a_team_rendered_the_same_is_not_restarted(self) -> None:
        self.install("team-a")
        self.assertEqual(self.run_main("team-a"), (0, "", ""))

    def test_a_team_whose_config_this_checkout_renders_differently_is_named(self) -> None:
        self.install("team-a", toml="# rendered by an older release\n")
        self.install("team-b")
        self.assertEqual(self.run_main("team-a", "team-b"), (0, "CONFIG-CHANGED team-a\n", ""))

    def test_a_team_not_installed_has_nothing_to_restart(self) -> None:
        self.assertEqual(self.run_main("team-new"), (0, "", ""))

    def test_an_installed_team_with_no_config_is_named(self) -> None:
        self.install("team-a")
        (self.root / "team-a" / "tuwunel.toml").unlink()
        self.assertEqual(self.run_main("team-a"), (0, "CONFIG-CHANGED team-a\n", ""))

    def test_an_unreadable_team_file_fails_naming_the_team(self) -> None:
        self.install("team-a", team_json="{not json")
        code, out, err = self.run_main("team-a")
        self.assertEqual((code, out), (1, ""))
        self.assertIn("team-a", err)

    def test_a_team_name_is_never_a_path(self) -> None:
        code, out, err = self.run_main("../etc")
        self.assertEqual((code, out), (1, ""))
        self.assertIn("../etc", err)


if __name__ == "__main__":
    unittest.main()
