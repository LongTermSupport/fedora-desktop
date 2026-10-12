"""Unit tests for helpers/agent_bus/declare.py: the agent_bus_teams entries in the host_vars.

Run from the repo root:

    python3 -m unittest tests.helpers.agent_bus.test_declare

`ccy-teams create` writes a team's entry into the untracked host_vars localhost.yml, a file
that may hold vault-encrypted values and that this repository has no YAML parser for (the
helpers are stdlib only). So the edit is an insertion of lines into a shape the helper
recognises, and anything else is refused: these tests hold the recognised shapes, the
refusals, the port choice, the validation (the team file's own) and the write.
"""

from __future__ import annotations

import io
import os
import pathlib
import stat
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import declare

VAULTED = """\
secret_thing: !vault |
  $ANSIBLE_VAULT;1.1;AES256
  3132333435363738393031323334353637383930
"""

WITH_TEAMS = """\
---
user_login: someone
agent_bus_address: 192.0.2.10
agent_bus_teams:
  - team: alpha
    port: 6167
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    repos:
      - repo: example-org/one
        branches: ["main"]
    path_prefixes: ["CLAUDE/Plan/", "docs/"]
    forge_api: https://api.github.com
  # a comment inside the list
  - team: "old"
    state: absent

# trailing comment
""" + VAULTED + """\
other_key: 1
"""

NEW = {"team": "beta", "humans": ["owner"], "repos": [("example-org/two", ["main", "dev"])],
       "allow_from": []}


def entry(port: int = 6168, **overrides) -> dict:
    values = dict(NEW, **overrides)
    return declare.build_entry(team=values["team"], port=port, listen=["192.0.2.10"],
                               allow_from=values["allow_from"], humans=values["humans"],
                               repos=values["repos"])


class ParseTest(unittest.TestCase):
    def test_reads_teams_ports_states_and_the_address(self) -> None:
        hv = declare.parse(WITH_TEAMS)
        self.assertEqual([(t.team, t.port, t.state) for t in hv.teams],
                         [("alpha", 6167, "present"), ("old", None, "absent")])
        self.assertEqual(hv.address, "192.0.2.10")

    def test_a_file_with_no_teams_key(self) -> None:
        hv = declare.parse("user_login: someone\n" + VAULTED)
        self.assertEqual(hv.teams, ())
        self.assertIsNone(hv.address)

    def test_a_list_at_column_zero(self) -> None:
        hv = declare.parse("agent_bus_teams:\n- team: alpha\n  port: 6200\nnext: 1\n")
        self.assertEqual([(t.team, t.port) for t in hv.teams], [("alpha", 6200)])

    def test_refusals(self) -> None:
        cases = {
            "inline list": "agent_bus_teams: []\n",
            "flow item": "agent_bus_teams:\n  - {team: alpha, port: 1}\n",
            "vaulted list": "agent_bus_teams: !vault |\n  $ANSIBLE_VAULT;1.1;AES256\n  3132\n",
            "twice": "agent_bus_teams:\n  - team: a\nagent_bus_teams:\n  - team: b\n",
            "quoted key": "\"agent_bus_teams\":\n  - team: a\n",
            "tab": "agent_bus_teams:\n\t- team: a\n",
            "item without team": "agent_bus_teams:\n  - port: 6167\n",
            "odd indent": "agent_bus_teams:\n  - team: a\n   port: 6167\n",
            "a port that is not a number": "agent_bus_teams:\n  - team: a\n    port: high\n",
            "a vaulted team name": "agent_bus_teams:\n  - team: !vault |\n      x\n",
            "a key twice in an item": "agent_bus_teams:\n  - team: a\n    team: b\n",
            "a team twice": "agent_bus_teams:\n  - team: a\n  - team: a\n",
            "an item indented differently": "agent_bus_teams:\n  - team: a\n    - team: b\n",
            "a scalar list item": "agent_bus_teams:\n  - alpha\n",
        }
        for name, text in cases.items():
            with self.subTest(name), self.assertRaises(declare.DeclareError):
                declare.parse(text)

    def test_a_vaulted_address_is_no_address(self) -> None:
        hv = declare.parse("agent_bus_address: !vault |\n  $ANSIBLE_VAULT;1.1;AES256\n  3132\n")
        self.assertIsNone(hv.address)


class PortTest(unittest.TestCase):
    def test_first_free_in_the_range(self) -> None:
        self.assertEqual(declare.choose_port(set()), declare.AUTO_PORT_FIRST)
        self.assertEqual(declare.choose_port({declare.AUTO_PORT_FIRST, declare.AUTO_PORT_FIRST + 1}),
                         declare.AUTO_PORT_FIRST + 2)

    def test_none_free_is_refused(self) -> None:
        used = set(range(declare.AUTO_PORT_FIRST, declare.AUTO_PORT_LAST + 1))
        with self.assertRaises(declare.DeclareError):
            declare.choose_port(used)

    def test_listening_ports_from_ss(self) -> None:
        out = ("LISTEN 0 4096 127.0.0.1:631 0.0.0.0:*\n"
               "LISTEN 0 128 [::]:22 [::]:*\n"
               "LISTEN 0 128 *:6167 *:*\n"
               "LISTEN 0 128 192.0.2.10%agentbus0:6170 0.0.0.0:*\n")
        self.assertEqual(declare.listening_ports(out), {631, 22, 6167, 6170})

    def test_unreadable_ss_output_is_refused(self) -> None:
        with self.assertRaises(declare.DeclareError):
            declare.listening_ports("LISTEN garbage\n")


class ValidateTest(unittest.TestCase):
    def test_each_field_by_the_team_file_rules(self) -> None:
        self.assertIsNone(declare.field_error("team", "beta-2"))
        self.assertIn("team", declare.field_error("team", "Bad_Name"))
        self.assertIsNone(declare.field_error("humans", ["owner"]))
        self.assertIn("humans", declare.field_error("humans", []))
        self.assertIn("repos", declare.field_error("repos", [{"repo": "Not A Repo", "branches": ["main"]}]))
        self.assertIsNone(declare.field_error("allow_from", ["198.51.100.0/24"]))
        self.assertIn("allow_from", declare.field_error("allow_from", ["198.51.100.1"]))

    def test_build_entry_is_a_valid_team_file(self) -> None:
        built = entry()
        self.assertEqual(built["path_prefixes"], ["CLAUDE/Plan/", "docs/"])
        self.assertEqual(built["forge_api"], "https://api.github.com")
        self.assertEqual(built["repos"], [{"repo": "example-org/two", "branches": ["main", "dev"]}])
        with self.assertRaises(declare.DeclareError):
            entry(humans=["Bad Human"])


class InsertTest(unittest.TestCase):
    def test_appended_to_the_list_and_nothing_else_changes(self) -> None:
        new = declare.insert(declare.parse(WITH_TEAMS), entry())
        hv = declare.parse(new)
        self.assertEqual([(t.team, t.port) for t in hv.teams], [("alpha", 6167), ("old", None), ("beta", 6168)])
        self.assertIn(
            '    state: absent\n'
            '  - team: beta\n'
            '    port: 6168\n'
            '    listen: ["192.0.2.10"]\n'
            '    allow_from: []\n'
            '    humans: ["owner"]\n'
            '    repos:\n'
            '      - repo: "example-org/two"\n'
            '        branches: ["main", "dev"]\n'
            '    path_prefixes: ["CLAUDE/Plan/", "docs/"]\n'
            '    forge_api: "https://api.github.com"\n'
            '\n# trailing comment\n', new)
        self.assertEqual(len(new.splitlines()), len(WITH_TEAMS.splitlines()) + 10)
        self.assertTrue(new.startswith(WITH_TEAMS.split("  # a comment")[0]))
        self.assertTrue(new.endswith("\n# trailing comment\n" + VAULTED + "other_key: 1\n"))
        # The new entry comes after the last item's lines, before the trailing comment.
        self.assertLess(new.index("state: absent"), new.index("team: beta"))
        self.assertLess(new.index("team: beta"), new.index("# trailing comment"))

    def test_the_lists_own_indentation_is_kept(self) -> None:
        new = declare.insert(declare.parse("agent_bus_teams:\n- team: alpha\n  port: 6200\n"), entry())
        self.assertIn("\n- team: beta\n  port: 6168\n", new)

    def test_a_file_without_the_key_gets_one_at_the_end(self) -> None:
        text = "user_login: someone\n" + VAULTED.rstrip("\n")
        new = declare.insert(declare.parse(text), entry())
        self.assertTrue(new.startswith(text + "\n"))
        self.assertEqual([t.team for t in declare.parse(new).teams], ["beta"])
        self.assertIn("\nagent_bus_teams:\n  - team: beta\n", new)

    def test_an_empty_key_takes_the_entry(self) -> None:
        new = declare.insert(declare.parse("agent_bus_teams:\nnext: 1\n"), entry())
        self.assertTrue(new.startswith("agent_bus_teams:\n  - team: beta\n"))
        self.assertTrue(new.endswith("next: 1\n"))

    def test_a_team_already_declared_is_refused(self) -> None:
        with self.assertRaises(declare.DeclareError):
            declare.insert(declare.parse(WITH_TEAMS), entry(team="old"))

    def test_a_port_already_declared_is_refused(self) -> None:
        with self.assertRaises(declare.DeclareError):
            declare.insert(declare.parse(WITH_TEAMS), entry(port=6167))


class WriteTest(unittest.TestCase):
    def test_backup_then_replace_keeping_the_mode(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(WITH_TEAMS, encoding="utf-8")
            os.chmod(path, 0o600)
            backup = declare.write(path, "new text\n")
            self.assertEqual(path.read_text(encoding="utf-8"), "new text\n")
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
            self.assertTrue(backup.name.startswith("localhost.yml.backup."))
            self.assertEqual(backup.read_text(encoding="utf-8"), WITH_TEAMS)
            self.assertEqual(stat.S_IMODE(backup.stat().st_mode), 0o600)
            self.assertEqual(sorted(p.name for p in pathlib.Path(tmp).iterdir()),
                             sorted(["localhost.yml", backup.name]))


class Answers:
    """A queue of answers for the prompts; None is end of input."""

    def __init__(self, *answers: str | None) -> None:
        self.answers = list(answers)
        self.prompts: list[str] = []

    def __call__(self, prompt: str) -> str | None:
        self.prompts.append(prompt)
        if not self.answers:
            raise AssertionError(f"no answer queued for {prompt!r}")
        return self.answers.pop(0)


class CreateTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "localhost.yml"
        self.path.write_text(WITH_TEAMS, encoding="utf-8")
        self.stdout = io.StringIO()
        self.stderr = io.StringIO()

    def run_create(self, argv: list[str], answers: Answers, *, listening: str = "") -> int:
        return declare.main(["create", "--file", str(self.path), *argv], ask=answers,
                            ss_output=lambda: listening, stdout=self.stdout, stderr=self.stderr)

    def test_prompts_validate_and_write(self) -> None:
        answers = Answers("Bad_Name", "beta", "owner", "example-org/two", "main,dev", "",
                          "", "y")
        code = self.run_create([], answers, listening="LISTEN 0 1 *:6168 *:*\n")
        self.assertEqual(code, 0, self.stderr.getvalue())
        teams = declare.parse(self.path.read_text(encoding="utf-8")).teams
        # 6167 is alpha's, 6168 has a listener: the next one is chosen.
        self.assertEqual([(t.team, t.port) for t in teams][-1], ("beta", 6169))
        self.assertIn("team", self.stderr.getvalue())  # the bad name was explained
        self.assertEqual(len([p for p in answers.prompts if p.startswith("Team name")]), 2)
        self.assertEqual(self.stdout.getvalue(),
                         f"CHANGED\tteam beta, port 6169, in {self.path}\n")
        self.assertIn("play-agent-bus.yml", self.stderr.getvalue())
        self.assertIn("meta-deploy", self.stderr.getvalue())

    def test_a_team_already_declared_is_asked_again(self) -> None:
        answers = Answers("alpha", "beta", "owner", "example-org/two", "main", "", "", "y")
        self.assertEqual(self.run_create([], answers), 0, self.stderr.getvalue())
        self.assertIn("already declared", self.stderr.getvalue())

    def test_no_writes_without_a_yes(self) -> None:
        answers = Answers("beta", "owner", "example-org/two", "main", "", "", "")
        self.assertEqual(self.run_create([], answers), 1)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)
        self.assertEqual(self.stdout.getvalue(), "")

    def test_end_of_input_cancels(self) -> None:
        self.assertEqual(self.run_create([], Answers("beta", None)), 1)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)
        self.assertIn("No changes", self.stderr.getvalue())

    def test_three_wrong_answers_give_up(self) -> None:
        self.assertEqual(self.run_create([], Answers("A", "B", "C")), 1)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_flags_need_no_prompt(self) -> None:
        code = self.run_create(["--team", "beta", "--human", "owner", "--repo", "example-org/two=main",
                                "--allow-from", "198.51.100.0/24", "--yes"], Answers())
        self.assertEqual(code, 0, self.stderr.getvalue())
        self.assertIn('allow_from: ["198.51.100.0/24"]', self.path.read_text(encoding="utf-8"))

    def test_a_bad_flag_is_a_usage_error(self) -> None:
        code = self.run_create(["--team", "Bad_Name", "--human", "owner", "--repo", "example-org/two=main",
                                "--yes"], Answers())
        self.assertEqual(code, 64)
        code = self.run_create(["--team", "beta", "--human", "owner", "--repo", "example-org/two",
                                "--yes"], Answers())
        self.assertEqual(code, 64)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_no_bus_address_is_refused_before_any_prompt(self) -> None:
        self.path.write_text("user_login: someone\n", encoding="utf-8")
        self.assertEqual(self.run_create([], Answers()), 78)
        self.assertIn("agent_bus_address", self.stderr.getvalue())

    def test_an_unexpected_shape_is_refused_before_any_prompt(self) -> None:
        self.path.write_text("agent_bus_address: 192.0.2.10\nagent_bus_teams: []\n", encoding="utf-8")
        self.assertEqual(self.run_create([], Answers()), 78)

    def test_the_file_is_never_printed(self) -> None:
        answers = Answers("beta", "owner", "example-org/two", "main", "", "", "y")
        self.run_create([], answers)
        self.assertNotIn("ANSIBLE_VAULT", self.stdout.getvalue() + self.stderr.getvalue())
        self.assertNotIn("someone", self.stdout.getvalue() + self.stderr.getvalue())


class DeclaredTest(unittest.TestCase):
    def test_one_line_per_entry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(WITH_TEAMS, encoding="utf-8")
            stdout, stderr = io.StringIO(), io.StringIO()
            code = declare.main(["declared", "--file", str(path)], stdout=stdout, stderr=stderr)
        self.assertEqual(code, 0)
        self.assertEqual(stdout.getvalue(), "TEAM\talpha\t6167\tpresent\nTEAM\told\t-\tabsent\n")

    def test_a_missing_file_is_refused(self) -> None:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main(["declared", "--file", "/nonexistent/localhost.yml"], stdout=stdout, stderr=stderr)
        self.assertEqual(code, 78)


if __name__ == "__main__":
    unittest.main()
