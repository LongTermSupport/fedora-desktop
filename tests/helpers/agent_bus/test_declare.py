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
import json
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
            "the address twice": "agent_bus_address: 192.0.2.10\nagent_bus_address: 192.0.2.11\n",
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


class CreateTest(unittest.TestCase):
    """create takes every value as a flag and prompts for nothing: ccy-teams asks the
    questions (with gum) and calls it with the answers."""

    FLAGS = ("--team", "beta", "--human", "owner", "--repo", "example-org/two=main,dev")

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "localhost.yml"
        self.path.write_text(WITH_TEAMS, encoding="utf-8")
        self.stdout = io.StringIO()
        self.stderr = io.StringIO()

    def run_create(self, argv: list[str] | tuple[str, ...], *, listening: str = "") -> int:
        return declare.main(["create", "--file", str(self.path), *argv],
                            ss_output=lambda: listening, stdout=self.stdout, stderr=self.stderr)

    def test_flags_write_the_entry(self) -> None:
        code = self.run_create(self.FLAGS, listening="LISTEN 0 1 *:6168 *:*\n")
        self.assertEqual(code, 0, self.stderr.getvalue())
        teams = declare.parse(self.path.read_text(encoding="utf-8")).teams
        # 6167 is alpha's, 6168 has a listener: the next one is chosen.
        self.assertEqual([(t.team, t.port) for t in teams][-1], ("beta", 6169))
        self.assertEqual(self.stdout.getvalue(), f"CHANGED\tteam beta, port 6169, in {self.path}\n")

    def test_the_next_steps_are_numbered_and_name_each_action(self) -> None:
        self.assertEqual(self.run_create(self.FLAGS), 0, self.stderr.getvalue())
        said = self.stderr.getvalue()
        for text in ("1.", "play-agent-bus.yml", "meta-deploy", "2.", "agent_bus_element_teams",
                     "http://192.0.2.10:6168", "3.", "password", "4.", "ccy --teams 1@beta"):
            self.assertIn(text, said)

    def test_allow_from(self) -> None:
        code = self.run_create([*self.FLAGS, "--allow-from", "198.51.100.0/24"])
        self.assertEqual(code, 0, self.stderr.getvalue())
        self.assertIn('allow_from: ["198.51.100.0/24"]', self.path.read_text(encoding="utf-8"))

    def test_dry_run_prints_the_entry_and_writes_nothing(self) -> None:
        self.assertEqual(self.run_create([*self.FLAGS, "--dry-run"]), 0, self.stderr.getvalue())
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)
        self.assertIn("  - team: beta\n", self.stdout.getvalue())
        self.assertIn('branches: ["main", "dev"]', self.stdout.getvalue())
        self.assertNotIn("CHANGED", self.stdout.getvalue())

    def test_every_value_is_required(self) -> None:
        for missing in ("--team", "--human", "--repo"):
            flags = list(self.FLAGS)
            at = flags.index(missing)
            del flags[at:at + 2]
            self.assertEqual(self.run_create(flags), 64, missing)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_a_bad_flag_is_a_usage_error(self) -> None:
        self.assertEqual(self.run_create(["--team", "Bad_Name", *self.FLAGS[2:]]), 64)
        self.assertEqual(self.run_create(["--team", "alpha", *self.FLAGS[2:]]), 64)
        self.assertIn("already declared", self.stderr.getvalue())
        self.assertEqual(self.run_create([*self.FLAGS[:4], "--repo", "example-org/two"]), 64)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_no_bus_address_is_refused_and_explained(self) -> None:
        self.path.write_text("user_login: someone\n", encoding="utf-8")
        self.assertEqual(self.run_create(self.FLAGS), 78)
        said = self.stderr.getvalue()
        self.assertIn("agent_bus_address", said)
        self.assertIn("192.0.2.", said)  # an example, from the documentation range
        self.assertIn("listen", said)

    def test_an_unexpected_shape_is_refused(self) -> None:
        self.path.write_text("agent_bus_address: 192.0.2.10\nagent_bus_teams: []\n", encoding="utf-8")
        self.assertEqual(self.run_create(self.FLAGS), 78)

    def test_the_file_is_never_printed(self) -> None:
        self.run_create(self.FLAGS)
        self.assertNotIn("ANSIBLE_VAULT", self.stdout.getvalue() + self.stderr.getvalue())
        self.assertNotIn("someone", self.stdout.getvalue() + self.stderr.getvalue())

    def test_ctrl_c_is_one_line_exit_130_and_no_write(self) -> None:
        def interrupted() -> str:
            raise KeyboardInterrupt
        code = declare.main(["create", "--file", str(self.path), *self.FLAGS],
                            ss_output=interrupted, stdout=self.stdout, stderr=self.stderr)
        self.assertEqual(code, 130)
        self.assertEqual(self.stderr.getvalue(), "Cancelled; nothing was changed.\n")
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)


class CheckTest(unittest.TestCase):
    """check validates what it is given, one field or several, for ccy-teams' re-prompts."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "localhost.yml"
        self.path.write_text(WITH_TEAMS, encoding="utf-8")

    def check(self, *argv: str) -> tuple[int, str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main(["check", "--file", str(self.path), *argv], stdout=stdout, stderr=stderr)
        self.assertEqual(stdout.getvalue(), "")
        return code, stderr.getvalue()

    def test_good_values_pass_silently(self) -> None:
        self.assertEqual(self.check("--team", "beta"), (0, ""))
        self.assertEqual(self.check("--human", "owner", "--human", "second"), (0, ""))
        self.assertEqual(self.check("--allow-from", "198.51.100.0/24"), (0, ""))
        self.assertEqual(self.check("--owner", "example-org"), (0, ""))
        self.assertEqual(self.check("--repo", "example-org/two=main"), (0, ""))

    def test_each_bad_value_is_named(self) -> None:
        for flag, value, said in (("--team", "Bad_Name", "--team"), ("--team", "alpha", "already declared"),
                                  ("--human", "Not.Valid", "--human"),
                                  ("--allow-from", "198.51.100.1/24", "--allow-from"),
                                  ("--owner", "-bad", "--owner"), ("--repo", "example-org/two", "--repo")):
            code, err = self.check(flag, value)
            self.assertEqual(code, 64, value)
            self.assertIn(said, err)


class GhReposTest(unittest.TestCase):
    """gh-repos turns `gh repo list --json name,defaultBranchRef,isArchived` into lines."""

    def gh_repos(self, data: object) -> tuple[int, str, str]:
        stdin, stdout, stderr = io.StringIO(json.dumps(data)), io.StringIO(), io.StringIO()
        code = declare.main(["gh-repos", "--owner", "Example-Org"], stdin=stdin, stdout=stdout, stderr=stderr)
        return code, stdout.getvalue(), stderr.getvalue()

    def test_lines_lower_cased_archived_skipped_empty_marked(self) -> None:
        code, out, _ = self.gh_repos([
            {"name": "Two", "defaultBranchRef": {"name": "main"}, "isArchived": False},
            {"name": "old", "defaultBranchRef": {"name": "master"}, "isArchived": True},
            {"name": "empty", "defaultBranchRef": None, "isArchived": False},
            {"name": "blank", "defaultBranchRef": {"name": ""}, "isArchived": False},
            {"name": "one", "defaultBranchRef": {"name": "trunk"}, "isArchived": False},
        ])
        self.assertEqual(code, 0)
        self.assertEqual(out, "COUNT\t5\n"
                              "REPO\texample-org/one\ttrunk\n"
                              "REPO\texample-org/two\tmain\n"
                              "EMPTY\texample-org/blank\n"
                              "EMPTY\texample-org/empty\n")

    def test_not_the_expected_json_is_refused(self) -> None:
        code, out, err = self.gh_repos({"message": "Not Found"})
        self.assertEqual(code, 78)
        self.assertEqual(out, "")
        self.assertIn("gh repo list", err)


class GithubRemoteTest(unittest.TestCase):
    def remote(self, url: str) -> str:
        stdout = io.StringIO()
        self.assertEqual(declare.main(["github-remote", "--url", url], stdout=stdout, stderr=io.StringIO()), 0)
        return stdout.getvalue()

    def test_the_forms_github_remotes_take(self) -> None:
        for url in ("git@github.com:Example-Org/Two.git", "https://github.com/example-org/two",
                    "https://github.com/example-org/two.git", "ssh://git@github.com/example-org/two.git",
                    "git@github.com-work:example-org/two.git"):
            self.assertEqual(self.remote(url), "example-org/two\n", url)

    def test_anything_else_prints_nothing(self) -> None:
        for url in ("https://gitlab.example.com/example-org/two.git", "/srv/git/two", ""):
            self.assertEqual(self.remote(url), "", url)


class AddRepoTest(unittest.TestCase):
    """add-repo adds repositories to a declared team's repos, in that list's own layout."""

    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = pathlib.Path(self.tmp.name) / "localhost.yml"
        self.path.write_text(WITH_TEAMS, encoding="utf-8")
        self.stdout = io.StringIO()
        self.stderr = io.StringIO()

    def call(self, *argv: str) -> int:
        return declare.main([argv[0], "--file", str(self.path), *argv[1:]], stdout=self.stdout, stderr=self.stderr)

    def test_repos_lists_a_teams_repositories(self) -> None:
        self.assertEqual(self.call("repos", "--team", "alpha"), 0, self.stderr.getvalue())
        self.assertEqual(self.stdout.getvalue(), "REPO\texample-org/one\tmain\n")

    def test_added_after_the_teams_last_repository_and_nothing_else_changes(self) -> None:
        code = self.call("add-repo", "--team", "alpha", "--repo", "example-org/two=main,dev",
                        "--repo", "example-org/three=trunk")
        self.assertEqual(code, 0, self.stderr.getvalue())
        want = WITH_TEAMS.replace(
            '        branches: ["main"]\n',
            '        branches: ["main"]\n'
            '      - repo: "example-org/two"\n'
            '        branches: ["main", "dev"]\n'
            '      - repo: "example-org/three"\n'
            '        branches: ["trunk"]\n', 1)
        self.assertEqual(self.path.read_text(encoding="utf-8"), want)
        self.assertEqual(self.stdout.getvalue(),
                         f"CHANGED\tteam alpha: example-org/two, example-org/three added, in {self.path}\n")
        self.assertIn("play-agent-bus.yml", self.stderr.getvalue())
        self.stdout = io.StringIO()
        self.call("repos", "--team", "alpha")
        self.assertEqual(self.stdout.getvalue().count("REPO\t"), 3)

    def test_dry_run_prints_the_lines_and_writes_nothing(self) -> None:
        self.assertEqual(self.call("add-repo", "--team", "alpha", "--repo", "example-org/two=main", "--dry-run"), 0)
        self.assertEqual(self.stdout.getvalue(), '      - repo: "example-org/two"\n        branches: ["main"]\n')
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_refusals_write_nothing(self) -> None:
        self.assertEqual(self.call("add-repo", "--team", "alpha", "--repo", "example-org/one=main"), 64)
        self.assertIn("already", self.stderr.getvalue())
        self.assertEqual(self.call("add-repo", "--team", "nosuch", "--repo", "example-org/two=main"), 64)
        self.assertEqual(self.call("add-repo", "--team", "alpha"), 64)
        self.assertEqual(self.call("add-repo", "--team", "alpha", "--repo", "example-org/two"), 64)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_an_inline_repos_list_is_refused(self) -> None:
        inline = WITH_TEAMS.replace(
            '    repos:\n      - repo: example-org/one\n        branches: ["main"]\n',
            '    repos: [{repo: example-org/one, branches: [main]}]\n')
        self.path.write_text(inline, encoding="utf-8")
        self.assertEqual(self.call("add-repo", "--team", "alpha", "--repo", "example-org/two=main"), 78)
        self.assertIn("inline", self.stderr.getvalue())
        self.assertEqual(self.path.read_text(encoding="utf-8"), inline)


class DeclaredTest(unittest.TestCase):
    def test_one_line_per_entry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(WITH_TEAMS, encoding="utf-8")
            stdout, stderr = io.StringIO(), io.StringIO()
            code = declare.main(["declared", "--file", str(path)], stdout=stdout, stderr=stderr)
        self.assertEqual(code, 0)
        self.assertEqual(stdout.getvalue(),
                         "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\n"
                         "TEAM\told\t-\tabsent\told.agent-bus.internal\t-\n")

    def test_a_missing_file_is_refused(self) -> None:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main(["declared", "--file", "/nonexistent/localhost.yml"], stdout=stdout, stderr=stderr)
        self.assertEqual(code, 78)


if __name__ == "__main__":
    unittest.main()
