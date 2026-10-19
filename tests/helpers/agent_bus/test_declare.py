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

import hashlib
import io
import json
import os
import pathlib
import stat
import sys
import tempfile
import unittest
from collections.abc import Sequence

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[3]))

from helpers.agent_bus import bridge, declare, teamfile
from helpers.pingbus import bundle

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]

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

NEW = {"team": "beta", "humans": ["owner"], "org": "example-org", "allow_from": []}


def entry(port: int = 6168, **overrides) -> dict:
    values = dict(NEW, **overrides)
    return declare.build_entry(team=values["team"], port=port, listen=["192.0.2.10"],
                               allow_from=values["allow_from"], humans=values["humans"],
                               org=values["org"])


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
        self.assertIn("org", declare.field_error("org", "Not An Org"))
        self.assertIsNone(declare.field_error("org", "example-org"))
        self.assertIsNone(declare.field_error("allow_from", ["198.51.100.0/24"]))
        self.assertIn("allow_from", declare.field_error("allow_from", ["198.51.100.1"]))

    def test_build_entry_is_a_valid_team_file(self) -> None:
        built = entry()
        self.assertNotIn("path_prefixes", built)  # org-wide: any path (Plan 00177 T4.7)
        self.assertEqual(built["forge_api"], "https://api.github.com")
        self.assertEqual(built["org"], "example-org")
        self.assertNotIn("repos", built)
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
            '    org: "example-org"\n'
            '    forge_api: "https://api.github.com"\n'
            '\n# trailing comment\n', new)
        self.assertEqual(len(new.splitlines()), len(WITH_TEAMS.splitlines()) + 7)
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

    FLAGS = ("--team", "beta", "--human", "owner", "--org", "example-org",
             "--description", "Builds the example site")

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

    def next_steps(self, team: str) -> str:
        self.stdout = io.StringIO()
        code = declare.main(["next-steps", "--file", str(self.path), "--team", team],
                            stdout=self.stdout, stderr=self.stderr)
        self.assertEqual(code, 0, self.stderr.getvalue())
        return self.stdout.getvalue()

    def test_the_next_steps_name_each_action_in_ccy_teams(self) -> None:
        """Printed by ccy-teams once the team is installed; never a meta-deploy step (D65),
        and never an edit of localhost.yml: create sets Element up itself (Task 4.2)."""
        self.assertEqual(self.run_create(self.FLAGS), 0, self.stderr.getvalue())
        self.assertNotIn("meta-deploy", self.stderr.getvalue())
        said = self.next_steps("beta")
        for text in ("password", "Element", "Open in Element", "ccy --teams beta", "any repository of example-org"):
            self.assertIn(text, said)
        for text in ("meta-deploy", "localhost.yml", "agent_bus_element_teams", "1@beta"):
            self.assertNotIn(text, said)

    def test_repositories_instead_of_an_org(self) -> None:
        """Task 4.9: a team of one or several repositories, of any owners, each on any branch
        (no `branches`), and no org."""
        flags = ("--team", "beta", "--human", "owner", "--repo", "example-org/one", "--repo", "other/tool",
                 "--description", "Builds the site")
        self.assertEqual(self.run_create(flags), 0, self.stderr.getvalue())
        text = self.path.read_text(encoding="utf-8")
        self.assertIn('repos: [{"repo": "example-org/one"}, {"repo": "other/tool"}]', text)
        beta = declare.entry_of(next(t for t in declare.parse(text).teams if t.team == "beta"))
        self.assertNotIn("org", beta)
        self.assertEqual([r.branches for r in teamfile.parse_team_file(beta).repos], [(), ()])
        said = self.next_steps("beta")
        self.assertIn("example-org/one or other/tool", said)

    def test_the_description_is_required_and_written_as_a_json_string(self) -> None:
        """S-A5: create needs --description; the entry carries it, flow style as the other keys."""
        flags = [f for f in self.FLAGS[:6]]
        self.assertEqual(self.run_create(flags), 64)
        self.assertIn("create needs --description", self.stderr.getvalue())
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)
        self.assertEqual(self.run_create(self.FLAGS), 0, self.stderr.getvalue())
        text = self.path.read_text(encoding="utf-8")
        self.assertIn('    description: "Builds the example site"\n', text)
        beta = declare.entry_of(next(t for t in declare.parse(text).teams if t.team == "beta"))
        self.assertEqual(teamfile.parse_team_file(beta).description, "Builds the example site")

    def test_a_bad_description_is_refused_naming_the_rule(self) -> None:
        for bad in ("", " padded ", "two\nlines", "x" * 121):
            self.assertEqual(self.run_create([*self.FLAGS[:6], "--description", bad]), 64, repr(bad))
        self.assertIn("description", self.stderr.getvalue())
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_org_or_repos_is_required(self) -> None:
        self.assertEqual(self.run_create(("--team", "beta", "--human", "owner", "--description", "d")), 64)
        self.assertIn("--org or --repo", self.stderr.getvalue())

    def test_allow_from(self) -> None:
        code = self.run_create([*self.FLAGS, "--allow-from", "198.51.100.0/24"])
        self.assertEqual(code, 0, self.stderr.getvalue())
        self.assertIn('allow_from: ["198.51.100.0/24"]', self.path.read_text(encoding="utf-8"))

    def test_dry_run_prints_the_entry_and_writes_nothing(self) -> None:
        self.assertEqual(self.run_create([*self.FLAGS, "--dry-run"]), 0, self.stderr.getvalue())
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)
        self.assertIn("  - team: beta\n", self.stdout.getvalue())
        self.assertIn('org: "example-org"', self.stdout.getvalue())
        self.assertNotIn("CHANGED", self.stdout.getvalue())

    def test_every_value_is_required(self) -> None:
        for missing in ("--team", "--human", "--org", "--description"):
            flags = list(self.FLAGS)
            at = flags.index(missing)
            del flags[at:at + 2]
            self.assertEqual(self.run_create(flags), 64, missing)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_a_bad_flag_is_a_usage_error(self) -> None:
        self.assertEqual(self.run_create(["--team", "Bad_Name", *self.FLAGS[2:]]), 64)
        self.assertEqual(self.run_create(["--team", "alpha", *self.FLAGS[2:]]), 64)
        self.assertIn("already declared", self.stderr.getvalue())
        self.assertEqual(self.run_create([*self.FLAGS[:4], "--org", "Example Org"]), 64)
        self.assertEqual(self.path.read_text(encoding="utf-8"), WITH_TEAMS)

    def test_no_bus_address_is_refused_and_explained(self) -> None:
        self.path.write_text("user_login: someone\n", encoding="utf-8")
        self.assertEqual(self.run_create(self.FLAGS), 78)
        said = self.stderr.getvalue()
        self.assertIn("listen", said)
        self.assertIn("ccy-teams create", said)  # which asks for it on a terminal
        self.assertNotIn("localhost.yml", said)  # the owner is never sent to edit a file
        self.assertNotIn("Set it in", said)

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
        self.assertEqual(self.check("--org", "example-org"), (0, ""))
        self.assertEqual(self.check("--repo", "example-org/one", "--repo", "other/tool"), (0, ""))

    def test_each_bad_value_is_named(self) -> None:
        for flag, value, said in (("--team", "Bad_Name", "--team"), ("--team", "alpha", "already declared"),
                                  ("--human", "Not.Valid", "--human"),
                                  ("--allow-from", "198.51.100.1/24", "--allow-from"),
                                  ("--org", "-bad", "--org"),
                                  ("--repo", "Example-Org/one", "Example-Org/one"),
                                  ("--repo", "noslash", "noslash")):
            code, err = self.check(flag, value)
            self.assertEqual(code, 64, value)
            self.assertIn(said, err)

    def test_description_alone_is_checked(self) -> None:
        self.assertEqual(self.check("--description", "Builds the example site"), (0, ""))
        for bad in ("", " padded", "a\nb", "x" * 121):
            code, err = self.check("--description", bad)
            self.assertEqual(code, 64, repr(bad))
            self.assertIn("description", err)

    def test_too_many_repositories(self) -> None:
        flags = [arg for i in range(declare.protocol.REPOS_MAX + 1) for arg in ("--repo", f"o/r{i}")]
        code, err = self.check(*flags)
        self.assertEqual(code, 64)
        self.assertIn(str(declare.protocol.REPOS_MAX), err)


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


class SuggestTeamTest(unittest.TestCase):
    """suggest-team: a GitHub owner's or repository's name in the team-name grammar, when free."""

    def suggest(self, owner: str) -> str:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(WITH_TEAMS, encoding="utf-8")
            stdout = io.StringIO()
            code = declare.main(["suggest-team", "--file", str(path), "--name", owner],
                                stdout=stdout, stderr=io.StringIO())
        self.assertEqual(code, 0)
        return stdout.getvalue()

    def test_normalised(self) -> None:
        self.assertEqual(self.suggest("example-org"), "example-org\n")
        self.assertEqual(self.suggest("Example_Org.io"), "example-org-io\n")
        self.assertEqual(self.suggest("42-Widgets"), "widgets\n")
        self.assertEqual(self.suggest("a-very-long-organisation-name-indeed"), "a-very-long-organisation\n")

    def test_nothing_when_taken_or_empty(self) -> None:
        self.assertEqual(self.suggest("alpha"), "")
        self.assertEqual(self.suggest("123"), "")


class DeclaredTest(unittest.TestCase):
    def test_one_line_per_entry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(WITH_TEAMS, encoding="utf-8")
            stdout, stderr = io.StringIO(), io.StringIO()
            code = declare.main(["declared", "--file", str(path)], stdout=stdout, stderr=stderr)
        self.assertEqual(code, 0)
        self.assertEqual(stdout.getvalue(),
                         "TEAM\talpha\t6167\tpresent\talpha.agent-bus.internal\thttp://192.0.2.10:6167\t-\n"
                         "TEAM\told\t-\tabsent\told.agent-bus.internal\t-\t-\n")

    def test_a_missing_file_is_refused(self) -> None:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main(["declared", "--file", "/nonexistent/localhost.yml"], stdout=stdout, stderr=stderr)
        self.assertEqual(code, 78)


STATUS_HV = """\
agent_bus_address: 192.0.2.10
agent_bus_teams:
  - team: alpha
    port: 6167
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    org: "example-org"
    path_prefixes: ["CLAUDE/Plan/", "docs/"]
    forge_api: "https://api.github.com"
  - team: beta
    port: 6168
    listen: ["192.0.2.10"]
    allow_from: ["192.0.2.0/24"]
    humans:
      - owner
      - "second"
    repos:
      - repo: example-org/one
        branches: ["main"]
    path_prefixes: ["docs/"]
    forge_api: https://api.github.com   # a comment
  - team: gamma
    state: absent
    purge: false
  - team: delta
    port: 6170
    listen: []
    allow_from: []
    humans: ["owner"]
    org: example-org
    path_prefixes: ["docs/"]
    forge_api: https://api.github.com
  - team: vaulted
    port: 6171
    listen: []
    allow_from: []
    humans: !vault |
      $ANSIBLE_VAULT;1.1;AES256
      3132
    org: example-org
    path_prefixes: ["docs/"]
    forge_api: https://api.github.com
"""


class EntryTest(unittest.TestCase):
    """The entry as the play reads it, in the subset ccy-teams reads (D65)."""

    def test_values_typed_as_yaml_types_them(self) -> None:
        teams = {t.team: t for t in declare.parse(STATUS_HV).teams}
        self.assertEqual(declare.entry_of(teams["alpha"]), {
            "team": "alpha", "port": 6167, "listen": ["192.0.2.10"], "allow_from": [], "humans": ["owner"],
            "org": "example-org", "path_prefixes": ["CLAUDE/Plan/", "docs/"],
            "forge_api": "https://api.github.com"})
        beta = declare.entry_of(teams["beta"])
        self.assertEqual(beta["humans"], ["owner", "second"])
        self.assertEqual(beta["repos"], [{"repo": "example-org/one", "branches": ["main"]}])
        self.assertEqual(beta["forge_api"], "https://api.github.com")
        self.assertIs(declare.entry_of(teams["gamma"])["purge"], False)

    def test_a_vaulted_value_is_not_read(self) -> None:
        vaulted = next(t for t in declare.parse(STATUS_HV).teams if t.team == "vaulted")
        with self.assertRaises(declare.DeclareError):
            declare.entry_of(vaulted)

    def test_the_declared_config_digest_is_the_installers(self) -> None:
        """The same function the installer's stamp uses, over the team file the play renders."""
        alpha = next(t for t in declare.parse(STATUS_HV).teams if t.team == "alpha")
        rendered = teamfile.parse_team_file(dict(declare.entry_of(alpha), state="present"))
        self.assertEqual(declare.declared_config(alpha), teamfile.config_digest(rendered))

    def test_a_hand_written_exclude_is_read_and_changes_the_digest(self) -> None:
        """D64: `exclude` is written into the entry by hand (create has no flag for it); the
        status still reads it, so a team whose entry gains one shows as out of date."""
        hv = STATUS_HV.replace('    org: "example-org"\n', '    org: "example-org"\n    exclude: ["hidden"]\n', 1)
        self.assertNotEqual(hv, STATUS_HV)
        before = next(t for t in declare.parse(STATUS_HV).teams if t.team == "alpha")
        after = next(t for t in declare.parse(hv).teams if t.team == "alpha")
        self.assertEqual(declare.entry_of(after)["exclude"], ["hidden"])
        self.assertNotEqual(declare.declared_config(before), declare.declared_config(after))
        with self.assertRaises(declare.DeclareError):
            declare.declared_config(next(t for t in declare.parse(
                hv.replace('    org: "example-org"\n', "", 1)).teams if t.team == "alpha"))


class StatusTest(unittest.TestCase):
    """Every state ccy-teams shows (D65): current, outdated, unbuilt, removed, decide."""

    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.path = self.tmp / "localhost.yml"
        self.path.write_text(STATUS_HV, encoding="utf-8")
        self.applied = self.tmp / "applied"
        self.applied.mkdir()
        self.software = declare.declared_software(REPO_ROOT)
        self.teams = {t.team: t for t in declare.parse(STATUS_HV).teams}

    def stamp(self, team: str, config: str | None = None, software: str | None = None) -> None:
        body = {"v": 1, "team": team, "config": config or declare.declared_config(self.teams[team]),
                "software": software or self.software}
        (self.applied / f"{team}.json").write_text(json.dumps(body), encoding="utf-8")

    def status(self, *installed: str) -> list[list[str]]:
        stdout, stderr = io.StringIO(), io.StringIO()
        flags = [f for team in installed for f in ("--installed", team)]
        code = declare.main(["status", "--file", str(self.path), "--source", str(REPO_ROOT),
                             "--applied", str(self.applied), *flags], stdout=stdout, stderr=stderr,
                            bridge_cidr=lambda: None)
        self.assertEqual(code, 0, stderr.getvalue())
        return [line.split("\t") for line in stdout.getvalue().splitlines()]

    def test_the_software_digest_is_the_zipapp_the_pinned_tuwunel_and_the_installer(self) -> None:
        zipapp = bundle.build("agent-bus", REPO_ROOT)
        pin = (REPO_ROOT / declare.PIN_FILE).read_text()
        version = next(line.split(":", 1)[1].strip() for line in pin.splitlines()
                       if line.startswith("tuwunel_version:"))
        installer = hashlib.sha256((REPO_ROOT / declare.INSTALLER_FILE).read_bytes()).hexdigest()
        want = hashlib.sha256(
            f"{hashlib.sha256(zipapp).hexdigest()}\n{version.lstrip('v')}\n{installer}\n".encode()).hexdigest()
        self.assertEqual(self.software, want)

    def test_all_states(self) -> None:
        self.stamp("alpha")
        self.stamp("beta", config="0" * 64)
        (self.applied / "gamma.json").write_text(json.dumps(
            {"v": 1, "team": "gamma", "config": "1" * 64, "software": self.software}), encoding="utf-8")
        (self.applied / "zeta.json").write_text(json.dumps(
            {"v": 1, "team": "zeta", "config": "2" * 64, "software": "3" * 64}), encoding="utf-8")
        rows = self.status()
        self.assertEqual(rows, [
            ["STATUS", "alpha", "current", "-"],
            ["STATUS", "beta", "outdated", "config"],
            ["STATUS", "gamma", "decide", "absent"],
            ["STATUS", "delta", "unbuilt", "-"],
            ["STATUS", "vaulted", "unreadable", rows[4][3]],
            ["STATUS", "zeta", "decide", "undeclared"],
        ])
        self.assertIn("vault", rows[4][3])

    def test_software_out_of_date_removed_and_unstamped(self) -> None:
        self.stamp("alpha", software="4" * 64)
        rows = {r[1]: r[2:] for r in self.status("delta")}
        self.assertEqual(rows["alpha"], ["outdated", "software"])
        self.assertEqual(rows["gamma"], ["removed", "-"])
        self.assertEqual(rows["delta"], ["outdated", "unstamped"])

    def test_overview_is_declared_then_status_in_one_run(self) -> None:
        self.stamp("alpha")
        argv = ["--file", str(self.path), "--source", str(REPO_ROOT), "--applied", str(self.applied),
                "--installed", "delta"]
        out = {}
        for command in ("declared", "status", "overview"):
            stdout = io.StringIO()
            args = argv if command != "declared" else argv[:2]
            self.assertEqual(declare.main([command, *args], stdout=stdout, stderr=io.StringIO(),
                                         bridge_cidr=lambda: None), 0)
            out[command] = stdout.getvalue()
        self.assertEqual(out["overview"], out["declared"] + out["status"])

    def test_a_malformed_stamp_is_refused(self) -> None:
        (self.applied / "alpha.json").write_text('{"v": 1}', encoding="utf-8")
        code = declare.main(["status", "--file", str(self.path), "--source", str(REPO_ROOT),
                             "--applied", str(self.applied)], stdout=io.StringIO(), stderr=io.StringIO(),
                            bridge_cidr=lambda: None)
        self.assertEqual(code, 78)


class BridgeStatusTest(unittest.TestCase):
    """The play stamps the digest of the team file it rendered, bridge subnet included; the
    status must compute that same digest (the regression: every team read `outdated: config`)."""

    CIDR = "198.51.100.0/24"

    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.path = self.tmp / "localhost.yml"
        self.path.write_text(STATUS_HV, encoding="utf-8")
        self.applied = self.tmp / "applied"
        self.applied.mkdir()
        self.software = declare.declared_software(REPO_ROOT)
        self.alpha = next(t for t in declare.parse(STATUS_HV).teams if t.team == "alpha")

    def play_stamp(self, cidr: str | None) -> None:
        """What the installer stamps: the digest of the team file the play rendered."""
        entry = dict(declare.entry_of(self.alpha))
        entry["allow_from"] = bridge.effective(entry.get("allow_from"), cidr)
        config = teamfile.config_digest(teamfile.parse_team_file(dict(entry, state="present")))
        (self.applied / "alpha.json").write_text(json.dumps(
            {"v": 1, "team": "alpha", "config": config, "software": self.software}), encoding="utf-8")

    def alpha_row(self, cidr: str | None) -> list[str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main(["status", "--file", str(self.path), "--source", str(REPO_ROOT),
                             "--applied", str(self.applied)], stdout=stdout, stderr=stderr,
                            bridge_cidr=lambda: cidr)
        self.assertEqual(code, 0, stderr.getvalue())
        return next(line.split("\t") for line in stdout.getvalue().splitlines() if "\talpha\t" in line)

    def test_a_stamp_from_the_bridge_augmented_file_reads_back_current(self) -> None:
        self.play_stamp(self.CIDR)
        self.assertEqual(self.alpha_row(self.CIDR), ["STATUS", "alpha", "current", "-"])

    def test_a_bare_digest_is_outdated_once_the_bridge_exists(self) -> None:
        self.play_stamp(None)
        self.assertEqual(self.alpha_row(self.CIDR)[2:], ["outdated", "config"])

    def test_no_bridge_is_the_bare_entry(self) -> None:
        self.play_stamp(None)
        self.assertEqual(self.alpha_row(None), ["STATUS", "alpha", "current", "-"])
        self.assertEqual(declare.declared_config(self.alpha), declare.declared_config(self.alpha, None))

    def test_a_declared_bridge_cidr_is_not_doubled(self) -> None:
        hv = STATUS_HV.replace("allow_from: []", f'allow_from: ["{self.CIDR}"]', 1)
        self.assertNotEqual(hv, STATUS_HV)
        team = next(t for t in declare.parse(hv).teams if t.team == "alpha")
        self.assertEqual(declare.declared_config(team, self.CIDR), declare.declared_config(team))

    def test_a_broken_bridge_refuses_the_status(self) -> None:
        def broken() -> str | None:
            raise bridge.BridgeError("bridge lxcbr0 exists but has no IPv4 address: lxc-net is broken")
        stderr = io.StringIO()
        code = declare.main(["status", "--file", str(self.path), "--source", str(REPO_ROOT),
                             "--applied", str(self.applied)], stdout=io.StringIO(), stderr=stderr,
                            bridge_cidr=broken)
        self.assertEqual(code, 78)
        self.assertIn("lxc-net", stderr.getvalue())


class RemoveTest(unittest.TestCase):
    """remove declares a team absent; its data stays unless --purge (owner, 2026-10-13)."""

    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.path = self.tmp / "localhost.yml"
        self.path.write_text(STATUS_HV, encoding="utf-8")

    def remove(self, *argv: str) -> tuple[int, str]:
        stdout = io.StringIO()
        code = declare.main(["remove", "--file", str(self.path), *argv], stdout=stdout, stderr=io.StringIO())
        return code, stdout.getvalue()

    def team(self, name: str) -> dict:
        return declare.entry_of(next(t for t in declare.parse(self.path.read_text()).teams if t.team == name))

    def test_absent_and_the_data_kept_by_default(self) -> None:
        code, out = self.remove("--team", "alpha")
        self.assertEqual(code, 0)
        self.assertIn("data kept", out)
        alpha = self.team("alpha")
        self.assertEqual((alpha["state"], alpha["purge"], alpha["org"]), ("absent", False, "example-org"))
        self.assertEqual(self.team("beta")["port"], 6168)

    def test_purge_and_an_existing_state_line_replaced(self) -> None:
        self.assertEqual(self.remove("--team", "gamma", "--purge")[0], 0)
        self.assertEqual(self.team("gamma"), {"team": "gamma", "state": "absent", "purge": True})

    def test_dry_run_and_an_unknown_team(self) -> None:
        code, out = self.remove("--team", "alpha", "--dry-run")
        self.assertEqual(code, 0)
        self.assertIn("state: absent", out)
        self.assertEqual(self.path.read_text(), STATUS_HV)
        self.assertEqual(self.remove("--team", "nosuch")[0], 64)


class DescriptionFieldTest(unittest.TestCase):
    """S-A7: the seventh TEAM field is the description as a JSON string, or a bare `-`."""

    def test_json_string_or_dash(self) -> None:
        text = WITH_TEAMS.replace("    port: 6167\n", '    port: 6167\n    description: "Builds: the \\"site\\""\n', 1)
        text = text.replace("  - team: \"old\"\n", '  - team: "old"\n    description: "-"\n', 1)
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(text, encoding="utf-8")
            stdout = io.StringIO()
            self.assertEqual(declare.main(["declared", "--file", str(path)], stdout=stdout, stderr=io.StringIO()), 0)
        rows = [line.split("\t") for line in stdout.getvalue().splitlines()]
        self.assertEqual(len(rows[0]), 7)
        self.assertEqual(json.loads(rows[0][6]), 'Builds: the "site"')
        self.assertEqual(rows[1][6], '"-"')

    def test_a_hash_inside_the_quotes_is_not_a_comment(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(STATUS_HV, encoding="utf-8")
            for said in ('Fix bug #5 "now"', "Café \\ team"):
                out, err = io.StringIO(), io.StringIO()
                self.assertEqual(declare.main(["describe", "--file", str(path), "--team", "alpha",
                                               "--description", said], stdout=out, stderr=err), 0, err.getvalue())
                alpha = declare.parse(path.read_text(encoding="utf-8")).teams[0]
                self.assertEqual(declare.entry_of(alpha)["description"], said)
            path.write_text(STATUS_HV.replace("  - team: alpha\n", '  - team: alpha\n    description: "A # b"  # note\n', 1),
                            encoding="utf-8")
            self.assertEqual(declare.entry_of(declare.parse(path.read_text(encoding="utf-8")).teams[0])["description"],
                             "A # b")

    def listing(self, injected: str) -> list[list[str]]:
        text = STATUS_HV.replace("  - team: gamma\n", "  - team: gamma\n" + injected, 1)
        with tempfile.TemporaryDirectory() as tmp:
            path = pathlib.Path(tmp) / "localhost.yml"
            path.write_text(text, encoding="utf-8")
            stdout = io.StringIO()
            self.assertEqual(declare.main(["declared", "--file", str(path)], stdout=stdout, stderr=io.StringIO()), 0)
        rows = [line.split("\t") for line in stdout.getvalue().splitlines()]
        self.assertEqual(len(rows), 5)
        self.assertTrue(all(len(r) == 7 for r in rows))
        return rows

    def test_a_vaulted_description_is_marked_vaulted_not_none(self) -> None:
        rows = self.listing("    description: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      3132\n")
        self.assertEqual(rows[2][6], "!vaulted")

    def test_only_the_vault_error_is_vaulted_every_other_unreadable_value_is_invalid(self) -> None:
        for odd in ("    description: [\"a\"]\n", "    description:\n", "    description: a\n      b\n"):
            with self.subTest(odd=odd):
                self.assertEqual(self.listing(odd)[2][6], "!invalid", odd)

    def test_a_description_that_fails_the_grammar_never_reaches_the_listing(self) -> None:
        for bad in ('"a\\tb"', '"x\\ny"', '" padded"', '"' + "x" * 121 + '"', '"a\\u202eb"'):
            with self.subTest(bad=bad):
                rows = self.listing(f"    description: {bad}\n")
                self.assertEqual(rows[2][6], "!invalid")


class CommandCase(unittest.TestCase):
    TEXT = STATUS_HV

    def setUp(self) -> None:
        self.tmp = pathlib.Path(self.enterContext(tempfile.TemporaryDirectory()))
        self.path = self.tmp / "localhost.yml"
        self.path.write_text(self.TEXT, encoding="utf-8")

    def run_cmd(self, *argv: str) -> tuple[int, str, str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main([argv[0], "--file", str(self.path), *argv[1:]], stdout=stdout, stderr=stderr)
        return code, stdout.getvalue(), stderr.getvalue()


class DescribeTest(CommandCase):
    """S-A6: describe sets or replaces a present team's description, nothing else touched."""

    def test_set_where_there_was_none_and_nothing_else_changed(self) -> None:
        before = declare.entry_of(declare.parse(STATUS_HV).teams[0])
        code, out, err = self.run_cmd("describe", "--team", "alpha", "--description", "Builds the site")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "CHANGED\tteam alpha description\n")
        text = self.path.read_text()
        self.assertEqual(text, STATUS_HV.replace("  - team: alpha\n",
                                                 '  - team: alpha\n    description: "Builds the site"\n', 1))
        self.assertEqual(declare.entry_of(declare.parse(text).teams[0]), dict(before, description="Builds the site"))
        self.assertEqual(len(list(self.tmp.glob("localhost.yml.backup.*"))), 1)

    def test_replaced_in_place_and_unchanged_when_equal(self) -> None:
        self.run_cmd("describe", "--team", "beta", "--description", "First")
        self.assertEqual(self.run_cmd("describe", "--team", "beta", "--description", "First")[:2],
                         (0, "UNCHANGED\tbeta\n"))
        code, out, _ = self.run_cmd("describe", "--team", "beta", "--description", "Second")
        self.assertEqual((code, out), (0, "CHANGED\tteam beta description\n"))
        text = self.path.read_text()
        self.assertEqual(text.count("description:"), 1)
        self.assertEqual(declare.entry_of(declare.parse(text).teams[1])["description"], "Second")

    def test_refusals_write_nothing(self) -> None:
        self.assertEqual(self.run_cmd("describe", "--team", "nosuch", "--description", "d")[0], 64)
        self.assertEqual(self.run_cmd("describe", "--team", "gamma", "--description", "d")[0], 64)
        self.assertEqual(self.run_cmd("describe", "--team", "alpha", "--description", " bad")[0], 64)
        self.assertEqual(self.path.read_text(), STATUS_HV)
        self.assertEqual(list(self.tmp.glob("localhost.yml.backup.*")), [])

    def test_a_vaulted_description_is_refused_naming_the_line(self) -> None:
        vaulted = STATUS_HV.replace("  - team: alpha\n", "  - team: alpha\n    description: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      3132\n", 1)
        self.path.write_text(vaulted, encoding="utf-8")
        code, _, err = self.run_cmd("describe", "--team", "alpha", "--description", "d")
        self.assertEqual(code, 78)
        self.assertIn("line 4", err)
        self.assertEqual(self.path.read_text(), vaulted)


class UndescribedTest(CommandCase):
    """S-A15: the present teams that still have no description, with what they trust in words."""

    def test_listed_with_their_scope(self) -> None:
        org = "any repository of example-org"
        self.assertEqual(self.run_cmd("undescribed")[1],
                         f"UNDESCRIBED\talpha\t{org}\nUNDESCRIBED\tbeta\texample-org/one\n"
                         f"UNDESCRIBED\tdelta\t{org}\nUNDESCRIBED\tvaulted\t{org}\n")

    def test_a_described_team_is_left_out(self) -> None:
        self.assertEqual(self.run_cmd("describe", "--team", "beta", "--description", "Second")[0], 0)
        out = self.run_cmd("undescribed")[1]
        self.assertNotIn("\tbeta\t", out)
        self.assertEqual(out.count("UNDESCRIBED"), 3)


class SettingsTest(CommandCase):
    """S-F2: settings prints every key of a present team's entry, one SETTING line each."""

    def test_every_key_in_order_as_json_or_dash(self) -> None:
        code, out, err = self.run_cmd("settings", "--team", "alpha")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "".join(f"SETTING\t{key}\t{value}\n" for key, value in (
            ("team", '"alpha"'), ("description", "-"), ("humans", '["owner"]'), ("org", '"example-org"'),
            ("repos", "-"), ("exclude", "-"), ("path_prefixes", '["CLAUDE/Plan/", "docs/"]'),
            ("allow_from", "[]"), ("port", "6167"), ("listen", '["192.0.2.10"]'),
            ("server_name", '"alpha.agent-bus.internal"'), ("forge_api", '"https://api.github.com"'),
            ("removed_humans", "-"))))

    def test_repos_keep_their_branches_and_a_block_list_reads_as_a_list(self) -> None:
        rows = dict(line.split("\t")[1:] for line in self.run_cmd("settings", "--team", "beta")[1].splitlines())
        self.assertEqual(json.loads(rows["repos"]), [{"repo": "example-org/one", "branches": ["main"]}])
        self.assertEqual(json.loads(rows["humans"]), ["owner", "second"])

    def test_a_vaulted_value_is_marked_and_the_rest_still_shown(self) -> None:
        rows = dict(line.split("\t")[1:] for line in self.run_cmd("settings", "--team", "vaulted")[1].splitlines())
        self.assertEqual(rows["humans"], "!vaulted")
        self.assertEqual(rows["org"], '"example-org"')

    def test_a_team_not_declared_present_is_refused(self) -> None:
        self.assertEqual(self.run_cmd("settings", "--team", "gamma")[0], 64)
        self.assertEqual(self.run_cmd("settings", "--team", "nosuch")[0], 64)


class SetTest(CommandCase):
    """S-F3 to S-F6: set changes the editable keys of a present team in one write, judged
    whole by the team file's rules, every other line as it was."""

    def entry(self, index: int = 0) -> dict:
        return declare.entry_of(declare.parse(self.path.read_text(encoding="utf-8")).teams[index])

    def backups(self) -> int:
        return len(list(self.tmp.glob("localhost.yml.backup.*")))

    def test_a_flow_list_is_replaced_in_place(self) -> None:
        before = self.entry()
        code, out, err = self.run_cmd("set", "--team", "alpha", "--value", "humans", '["owner", "second"]')
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "CHANGED\tteam alpha: humans\n")
        self.assertEqual(self.path.read_text(), STATUS_HV.replace('    humans: ["owner"]\n',
                                                                  '    humans: ["owner", "second"]\n', 1))
        self.assertEqual(self.entry(), dict(before, humans=["owner", "second"]))
        self.assertEqual(self.backups(), 1)

    def test_several_keys_are_one_write_and_a_new_key_follows_the_team_line(self) -> None:
        code, _, err = self.run_cmd("set", "--team", "alpha", "--value", "description", '"Builds the site"',
                                    "--value", "exclude", '["secret"]', "--value", "allow_from", '["192.0.2.0/24"]')
        self.assertEqual(code, 0, err)
        text = self.path.read_text()
        self.assertIn('  - team: alpha\n    description: "Builds the site"\n    exclude: ["secret"]\n    port: 6167\n', text)
        self.assertIn('    allow_from: ["192.0.2.0/24"]\n', text)
        self.assertEqual(self.backups(), 1)
        self.assertEqual([t.team for t in declare.parse(text).teams], ["alpha", "beta", "gamma", "delta", "vaulted"])

    def test_a_block_list_is_replaced_whole(self) -> None:
        code, _, err = self.run_cmd("set", "--team", "beta", "--value", "humans", '["owner"]')
        self.assertEqual(code, 0, err)
        self.assertIn('    allow_from: ["192.0.2.0/24"]\n    humans: ["owner"]\n    repos:\n', self.path.read_text())
        self.assertEqual(self.entry(1)["humans"], ["owner"])

    def test_an_empty_optional_value_removes_the_key(self) -> None:
        code, _, err = self.run_cmd("set", "--team", "alpha", "--value", "path_prefixes", "[]")
        self.assertEqual(code, 0, err)
        self.assertEqual(self.path.read_text(), STATUS_HV.replace('    path_prefixes: ["CLAUDE/Plan/", "docs/"]\n', "", 1))

    def test_repos_are_given_as_names_and_keep_the_branches_they_had(self) -> None:
        code, _, err = self.run_cmd("set", "--team", "beta", "--value", "repos", '["example-org/one", "example-org/two"]')
        self.assertEqual(code, 0, err)
        self.assertEqual(self.entry(1)["repos"], [{"repo": "example-org/one", "branches": ["main"]},
                                                  {"repo": "example-org/two"}])

    def test_check_judges_the_whole_entry_and_writes_nothing(self) -> None:
        self.assertEqual(self.run_cmd("set", "--team", "alpha", "--check", "--value", "humans", '["second"]')[:2], (0, ""))
        for argv in ((("org", '""'),),  # no org and no repos
                     (("humans", "[]"),),
                     (("description", '" padded"'),),
                     (("allow_from", '["192.0.2.1"]'),),
                     (("org", '""'), ("exclude", '["x"]'))):
            flat = [part for key, value in argv for part in ("--value", key, value)]
            for check in ((), ("--check",)):
                with self.subTest(argv=argv, check=check):
                    code, _, err = self.run_cmd("set", "--team", "alpha", *check, *flat)
                    self.assertEqual(code, 64, err)
                    self.assertIn("team file:", err)
        self.assertEqual(self.path.read_text(), STATUS_HV)
        self.assertEqual(self.backups(), 0)

    def test_unchanged_when_every_value_is_already_that(self) -> None:
        self.assertEqual(self.run_cmd("set", "--team", "alpha", "--value", "humans", '["owner"]')[:2],
                         (0, "UNCHANGED\talpha\n"))
        self.assertEqual(self.backups(), 0)

    def test_a_key_that_is_not_editable_or_a_wrong_type_is_a_usage_mistake(self) -> None:
        for key, value in (("port", "6200"), ("team", '"renamed"'), ("server_name", '"x.internal"'),
                           ("humans", '"owner"'), ("org", '["a"]'), ("repos", '[{"repo": "a/b"}]'), ("humans", "not json")):
            with self.subTest(key=key, value=value):
                self.assertEqual(self.run_cmd("set", "--team", "alpha", "--value", key, value)[0], 64)
        self.assertEqual(self.run_cmd("set", "--team", "gamma", "--value", "humans", '["owner"]')[0], 64)
        self.assertEqual(self.run_cmd("set", "--team", "alpha", "--value", "humans", '["a"]', "--value", "humans", '["b"]')[0], 64)
        self.assertEqual(self.path.read_text(), STATUS_HV)

    def test_a_comment_the_edit_would_lose_is_refused(self) -> None:
        text = STATUS_HV.replace('    humans: ["owner"]\n', '    humans: ["owner"]  # the owner\n', 1)
        self.path.write_text(text, encoding="utf-8")
        code, _, err = self.run_cmd("set", "--team", "alpha", "--value", "humans", '["second"]')
        self.assertEqual(code, 78, err)
        self.assertIn("comment", err)
        self.assertEqual(self.path.read_text(), text)

    def test_a_removed_person_is_recorded_and_can_never_return(self) -> None:
        """S-F14: the play deactivates a removed person's account for good, so set records the
        name in removed_humans, and the team file's rule refuses it in humans from then on."""
        code, out, err = self.run_cmd("set", "--team", "beta", "--value", "humans", '["owner"]')
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "CHANGED\tteam beta: humans, removed_humans\n")
        self.assertEqual(self.entry(1)["removed_humans"], ["second"])
        for check in ((), ("--check",)):
            with self.subTest(check=check):
                code, _, err = self.run_cmd("set", "--team", "beta", *check, "--value", "humans", '["owner", "second"]')
                self.assertEqual(code, 64, err)
                self.assertIn("can never return", err)
        self.run_cmd("set", "--team", "beta", "--value", "humans", '["third"]')
        self.assertEqual(self.entry(1)["removed_humans"], ["second", "owner"])
        self.assertEqual(self.run_cmd("set", "--team", "beta", "--value", "removed_humans", "[]")[0], 64)

    def test_a_vaulted_entry_is_refused(self) -> None:
        self.assertEqual(self.run_cmd("set", "--team", "vaulted", "--value", "org", '"other-org"')[0], 78)
        self.assertEqual(self.path.read_text(), STATUS_HV)


class ElementTest(CommandCase):
    """Task 4.2: create declares the team's Element profile itself, in
    agent_bus_element_teams, so nobody edits localhost.yml for it."""

    def test_added_once_with_the_teams_url(self) -> None:
        code, out, err = self.run_cmd("element", "--team", "alpha")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"CHANGED\tElement profile for alpha, in {self.path}\n")
        text = self.path.read_text(encoding="utf-8")
        self.assertTrue(text.startswith(STATUS_HV))
        self.assertIn('agent_bus_element_teams:\n  - team: alpha\n    base_url: "http://192.0.2.10:6167"\n', text)
        self.assertEqual(self.run_cmd("element", "--team", "alpha")[:2], (0, "UNCHANGED\talpha\n"))
        self.assertEqual(self.run_cmd("element", "--team", "beta")[0], 0)
        code, out, _ = self.run_cmd("element-teams")
        self.assertEqual(out, "ELEMENT\talpha\thttp://192.0.2.10:6167\nELEMENT\tbeta\thttp://192.0.2.10:6168\n")
        # Both read back as two entries of the one list, the teams list untouched.
        self.assertEqual([t.team for t in declare.parse(self.path.read_text(), declare.ELEMENT_KEY).teams],
                         ["alpha", "beta"])
        self.assertEqual(len(declare.parse(self.path.read_text()).teams), 5)

    def test_a_changed_url_is_updated(self) -> None:
        self.path.write_text(STATUS_HV + "agent_bus_element_teams:\n  - team: alpha\n"
                             "    base_url: http://192.0.2.99:6000\n  - team: other\n"
                             "    base_url: http://192.0.2.98:6001\n", encoding="utf-8")
        code, out, err = self.run_cmd("element", "--team", "alpha")
        self.assertEqual(code, 0, err)
        self.assertIn("CHANGED", out)
        text = self.path.read_text()
        self.assertIn('  - team: alpha\n    base_url: "http://192.0.2.10:6167"\n  - team: other\n', text)

    def test_a_team_not_declared_present_is_refused(self) -> None:
        self.assertEqual(self.run_cmd("element", "--team", "nosuch")[0], 64)
        self.assertEqual(self.run_cmd("element", "--team", "gamma")[0], 64)
        self.assertEqual(self.path.read_text(), STATUS_HV)


class TeamsForTest(CommandCase):
    """Task 4.4: the teams a checkout's repository belongs to (its owner's org, or listed)."""

    def test_by_org_and_by_listing(self) -> None:
        self.assertEqual(self.run_cmd("teams-for", "--repo", "example-org/two")[1],
                         "TEAM\talpha\nTEAM\tdelta\nTEAM\tvaulted\n")
        self.assertEqual(self.run_cmd("teams-for", "--repo", "example-org/one")[1],
                         "TEAM\talpha\nTEAM\tbeta\nTEAM\tdelta\nTEAM\tvaulted\n")
        self.assertEqual(self.run_cmd("teams-for", "--repo", "other/tool")[1], "")

    def test_a_repository_that_is_not_owner_slash_repo_is_refused(self) -> None:
        for bad in ("example-org", "Example-Org/two", "example-org/two/x", ""):
            code, out, err = self.run_cmd("teams-for", "--repo", bad)
            self.assertEqual((code, out), (64, ""), bad)
            self.assertIn(repr(bad), err)

    def test_an_org_team_does_not_cover_a_repository_it_excludes(self) -> None:
        self.path.write_text(STATUS_HV.replace('    org: "example-org"\n',
                                               '    org: "example-org"\n    exclude: ["two"]\n', 1))
        # alpha trusts the org but excludes example-org/two: the bus would refuse a ping naming it.
        self.assertEqual(self.run_cmd("teams-for", "--repo", "example-org/two")[1],
                         "TEAM\tdelta\nTEAM\tvaulted\n")
        self.assertEqual(self.run_cmd("teams-for", "--repo", "example-org/other")[1],
                         "TEAM\talpha\nTEAM\tdelta\nTEAM\tvaulted\n")
        # A team that lists the repository (beta) is unaffected by another team's exclude.
        self.assertEqual(self.run_cmd("teams-for", "--repo", "example-org/one")[1],
                         "TEAM\talpha\nTEAM\tbeta\nTEAM\tdelta\nTEAM\tvaulted\n")


class ChoicesTest(CommandCase):
    """S-B4: every present team with how it stands to a repository: covers, widen (a scope
    edit would make it cover), or excluded (it refuses the repository on purpose)."""

    def test_each_present_team_with_its_standing(self) -> None:
        self.assertEqual(self.run_cmd("choices", "--repo", "example-org/two")[1],
                         "CHOICE\talpha\tcovers\t-\nCHOICE\tbeta\twiden\t-\n"
                         "CHOICE\tdelta\tcovers\t-\nCHOICE\tvaulted\tcovers\t-\n")
        self.assertEqual(self.run_cmd("choices", "--repo", "other/tool")[1],
                         "CHOICE\talpha\twiden\t-\nCHOICE\tbeta\twiden\t-\n"
                         "CHOICE\tdelta\twiden\t-\nCHOICE\tvaulted\twiden\t-\n")

    def test_a_listed_repository_covers(self) -> None:
        self.assertIn("CHOICE\tbeta\tcovers\t-\n", self.run_cmd("choices", "--repo", "example-org/one")[1])

    def test_an_org_team_that_excludes_the_repository_is_excluded_not_covering(self) -> None:
        self.path.write_text(STATUS_HV.replace('    org: "example-org"\n',
                                               '    org: "example-org"\n    exclude: ["two"]\n', 1))
        out = self.run_cmd("choices", "--repo", "example-org/two")[1]
        self.assertIn("CHOICE\talpha\texcluded\t-\n", out)
        self.assertIn("CHOICE\tdelta\tcovers\t-\n", out)
        # teams-for and choices agree on who covers.
        self.assertNotIn("alpha", self.run_cmd("teams-for", "--repo", "example-org/two")[1])

    def test_the_description_is_a_json_string_as_declared_prints_it(self) -> None:
        self.run_cmd("describe", "--team", "beta", "--description", "Builds the site")
        self.assertIn('CHOICE\tbeta\twiden\t"Builds the site"\n', self.run_cmd("choices", "--repo", "other/tool")[1])

    def test_a_team_whose_scope_cannot_be_read_is_unreadable_and_does_not_end_the_listing(self) -> None:
        self.path.write_text(STATUS_HV.replace("    org: example-org\n    path_prefixes: [\"docs/\"]\n"
                                               "    forge_api: https://api.github.com\n  - team: vaulted",
                                               "    org: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      3132\n"
                                               "    forge_api: https://api.github.com\n  - team: vaulted", 1))
        out = self.run_cmd("choices", "--repo", "example-org/two")[1]
        self.assertIn("CHOICE\tdelta\tunreadable\t-\n", out)
        self.assertIn("CHOICE\talpha\tcovers\t-\n", out)

    def test_a_repository_that_is_not_owner_slash_repo_is_refused(self) -> None:
        code, out, err = self.run_cmd("choices", "--repo", "Example-Org/two")
        self.assertEqual((code, out), (64, ""))
        self.assertIn("--repo", err)


class AddRepoTest(CommandCase):
    """S-B8: add-repo appends one repository to a present team's `repos`, every other line as it
    was, read back or refused."""

    def test_made_where_the_team_had_no_repos_and_nothing_else_changed(self) -> None:
        before = declare.entry_of(declare.parse(STATUS_HV).teams[0])
        code, out, err = self.run_cmd("add-repo", "--team", "alpha", "--repo", "other/tool")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "CHANGED\tteam alpha trusts other/tool\n")
        text = self.path.read_text()
        self.assertEqual(text, STATUS_HV.replace("  - team: alpha\n",
                                                 '  - team: alpha\n    repos: [{"repo": "other/tool"}]\n', 1))
        self.assertEqual(declare.entry_of(declare.parse(text).teams[0]),
                         dict(before, repos=[{"repo": "other/tool"}]))
        self.assertEqual(len(list(self.tmp.glob("localhost.yml.backup.*"))), 1)
        self.assertIn("CHOICE\talpha\tcovers\t-\n", self.run_cmd("choices", "--repo", "other/tool")[1])

    def test_appended_to_a_one_line_list(self) -> None:
        self.run_cmd("add-repo", "--team", "alpha", "--repo", "other/tool")
        self.assertEqual(self.run_cmd("add-repo", "--team", "alpha", "--repo", "other/more")[0], 0)
        self.assertIn('    repos: [{"repo": "other/tool"}, {"repo": "other/more"}]\n', self.path.read_text())

    def test_appended_to_a_block_list_after_its_last_item(self) -> None:
        code, out, err = self.run_cmd("add-repo", "--team", "beta", "--repo", "other/tool")
        self.assertEqual(code, 0, err)
        text = self.path.read_text()
        self.assertEqual(text, STATUS_HV.replace('        branches: ["main"]\n',
                                                 '        branches: ["main"]\n      - repo: "other/tool"\n', 1))
        self.assertEqual(declare.entry_of(declare.parse(text).teams[1])["repos"],
                         [{"repo": "example-org/one", "branches": ["main"]}, {"repo": "other/tool"}])

    def test_already_trusted_is_a_usage_error_and_writes_nothing(self) -> None:
        for team, repo in (("beta", "example-org/one"), ("alpha", "example-org/two")):
            code, _, err = self.run_cmd("add-repo", "--team", team, "--repo", repo)
            self.assertEqual(code, 64, err)
            self.assertIn("already trusts", err)
        self.assertEqual(self.path.read_text(), STATUS_HV)

    def test_a_repository_the_team_excludes_is_refused_naming_the_exclude(self) -> None:
        self.path.write_text(STATUS_HV.replace('    org: "example-org"\n',
                                               '    org: "example-org"\n    exclude: ["two"]\n', 1))
        before = self.path.read_text()
        code, _, err = self.run_cmd("add-repo", "--team", "alpha", "--repo", "example-org/two")
        self.assertEqual(code, 78)
        self.assertIn("exclude", err)
        self.assertEqual(self.path.read_text(), before)

    def test_the_repos_limit_is_refused_with_78(self) -> None:
        many = ", ".join('{"repo": "o/r%d"}' % i for i in range(declare.protocol.REPOS_MAX))
        self.path.write_text(STATUS_HV.replace("  - team: alpha\n", f"  - team: alpha\n    repos: [{many}]\n", 1))
        before = self.path.read_text()
        code, _, err = self.run_cmd("add-repo", "--team", "alpha", "--repo", "other/tool")
        self.assertEqual(code, 78)
        self.assertIn(str(declare.protocol.REPOS_MAX), err)
        self.assertEqual(self.path.read_text(), before)

    def test_a_vaulted_scope_is_refused_naming_the_line(self) -> None:
        vaulted = STATUS_HV.replace("  - team: alpha\n", "  - team: alpha\n    repos: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      3132\n", 1)
        self.path.write_text(vaulted)
        code, _, err = self.run_cmd("add-repo", "--team", "alpha", "--repo", "other/tool")
        self.assertEqual(code, 78)
        self.assertIn("line 4", err)
        self.assertEqual(self.path.read_text(), vaulted)

    def test_a_list_line_with_a_comment_is_refused_not_rewritten(self) -> None:
        text = STATUS_HV.replace("  - team: alpha\n", '  - team: alpha\n    repos: [{"repo": "o/r"}]   # mine\n', 1)
        self.path.write_text(text)
        code, _, err = self.run_cmd("add-repo", "--team", "alpha", "--repo", "other/tool")
        self.assertEqual(code, 78)
        self.assertIn("comment", err)
        self.assertEqual(self.path.read_text(), text)

    def test_usage_refusals_write_nothing(self) -> None:
        self.assertEqual(self.run_cmd("add-repo", "--team", "nosuch", "--repo", "other/tool")[0], 64)
        self.assertEqual(self.run_cmd("add-repo", "--team", "gamma", "--repo", "other/tool")[0], 64)
        self.assertEqual(self.run_cmd("add-repo", "--team", "alpha", "--repo", "Other/Tool")[0], 64)
        self.assertEqual(self.path.read_text(), STATUS_HV)
        self.assertEqual(list(self.tmp.glob("localhost.yml.backup.*")), [])


class PathLimitTest(CommandCase):
    """Task 4.7's follow-up: an entry written with the old path_prefixes default is offered
    its removal, which ccy-teams applies through drop-limit."""

    def test_listed(self) -> None:
        code, out, _ = self.run_cmd("limited")
        self.assertEqual(code, 0)
        org = "any repository of example-org"
        self.assertEqual(out, f"LIMITED\talpha\tCLAUDE/Plan/, docs/\t{org}\n"
                              "LIMITED\tbeta\tdocs/\texample-org/one\n"
                              f"LIMITED\tdelta\tdocs/\t{org}\nLIMITED\tvaulted\tdocs/\t{org}\n")

    def test_dropped_and_nothing_else_changed(self) -> None:
        before = declare.entry_of(declare.parse(STATUS_HV).teams[0])
        code, out, err = self.run_cmd("drop-limit", "--team", "alpha")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"CHANGED\tteam alpha may name any path, in {self.path}\n")
        text = self.path.read_text()
        self.assertEqual(text, STATUS_HV.replace('    path_prefixes: ["CLAUDE/Plan/", "docs/"]\n', "", 1))
        after = declare.entry_of(declare.parse(text).teams[0])
        self.assertEqual(after, {k: v for k, v in before.items() if k != "path_prefixes"})
        self.assertEqual(self.run_cmd("drop-limit", "--team", "alpha")[0], 64)

    def test_a_block_list_is_dropped_whole(self) -> None:
        self.path.write_text(STATUS_HV.replace('    path_prefixes: ["CLAUDE/Plan/", "docs/"]\n',
                                               "    path_prefixes:\n      - CLAUDE/Plan/\n      - docs/\n", 1))
        self.assertEqual(self.run_cmd("drop-limit", "--team", "alpha")[0], 0)
        self.assertEqual(self.path.read_text(), STATUS_HV.replace('    path_prefixes: ["CLAUDE/Plan/", "docs/"]\n', "", 1))


class AddressTest(CommandCase):
    """The bus address is a one-off fact about the machine: ccy-teams create asks for it and
    sets it through declare.py, so nobody edits the host_vars for it."""
    TEXT = "user_login: someone\n"

    FLAGS = ("--team", "beta", "--human", "owner", "--org", "example-org", "--description", "Builds the site")

    def create(self, *extra: str) -> tuple[int, str, str]:
        stdout, stderr = io.StringIO(), io.StringIO()
        code = declare.main(["create", "--file", str(self.path), *self.FLAGS, *extra],
                            ss_output=lambda: "", stdout=stdout, stderr=stderr)
        return code, stdout.getvalue(), stderr.getvalue()

    def test_create_declares_it_with_the_first_team_in_one_write(self) -> None:
        code, out, err = self.create("--address", "192.0.2.10")
        self.assertEqual(code, 0, err)
        text = self.path.read_text()
        self.assertTrue(text.startswith("user_login: someone\nagent_bus_address: 192.0.2.10\n"), text)
        hv = declare.parse(text)
        self.assertEqual((hv.address, [t.team for t in hv.teams]), ("192.0.2.10", ["beta"]))
        self.assertEqual(len(list(self.tmp.glob("localhost.yml.backup.*"))), 1)
        self.assertEqual(self.run_cmd("address")[1], "192.0.2.10\n")

    def test_a_dry_run_writes_nothing(self) -> None:
        code, out, err = self.create("--address", "192.0.2.10", "--dry-run")
        self.assertEqual(code, 0, err)
        self.assertIn('listen: ["192.0.2.10"]', out)
        self.assertEqual(self.path.read_text(), self.TEXT)

    def test_no_address_reads_as_nothing(self) -> None:
        self.assertEqual(self.run_cmd("address"), (0, "", ""))

    def test_a_bad_address_is_a_usage_error_and_nothing_is_written(self) -> None:
        for bad in ("not-an-ip", "192.0.2.0/24", "", "0.0.0.0", "127.0.0.1"):
            self.assertEqual(self.create("--address", bad)[0], 64, bad)
            self.assertEqual(declare.main(["check-address", "--address", bad], stderr=io.StringIO()), 64, bad)
        self.assertEqual(declare.main(["check-address", "--address", "192.0.2.10"]), 0)
        self.assertEqual(self.path.read_text(), self.TEXT)

    def test_one_already_set_is_never_replaced(self) -> None:
        self.path.write_text("agent_bus_address: 192.0.2.10\n")
        self.assertEqual(self.create("--address", "192.0.2.11")[0], 64)
        self.assertEqual(self.path.read_text(), "agent_bus_address: 192.0.2.10\n")


LXC_HV = STATUS_HV + """\
agent_bus_lxc_members:
  - team: alpha
    container: container-a
    user: agent
    host: worker
    checkout: /srv/agent/one
    role: orchestrator
  - team: alpha
    container: container-b
    user: agent
    host: other
    checkout: /srv/agent/two
    state: absent
  # a comment inside the list
  - team: beta
    container: container-a
    user: agent
    host: worker
    checkout: /srv/agent/one
    role: coordinator
    state: absent
other_key: 1
"""

#: What a hand-written entry needs: host and seat are derived from the checkout.
LXC_FLAGS = ("--team", "alpha", "--container", "container-c", "--user", "agent",
             "--checkout", "/srv/agent/three")


class LxcMemberTest(CommandCase):
    """ccy-teams lxc-add writes an agent_bus_lxc_members entry through declare.py, by the rules
    helpers/agent_bus_lxc/members.py applies to the same entry (Plan 00177 Phase 8)."""

    TEXT = LXC_HV

    def members(self) -> tuple:
        from helpers.agent_bus_lxc import members
        entries = [declare.entry_of(m) for m in declare.parse(self.path.read_text(), declare.LXC_KEY).teams]
        return members.parse_members(entries)

    def test_added_at_the_end_of_the_list_and_the_rest_kept(self) -> None:
        code, out, err = self.run_cmd("lxc-add", *LXC_FLAGS)
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"CHANGED\tLXC member agent@container-c in team alpha, in {self.path}\n")
        text = self.path.read_text(encoding="utf-8")
        old, after_list = LXC_HV.split("other_key: 1\n")[0], "other_key: 1\n"
        self.assertTrue(text.startswith(old), text)
        self.assertTrue(text.endswith(
            '  - team: alpha\n    container: "container-c"\n    user: "agent"\n'
            '    checkout: "/srv/agent/three"\n' + after_list))
        found = self.members()
        self.assertEqual([(m.team, m.container, m.role) for m in found][-1], ("alpha", "container-c", "worker"))
        self.assertEqual(len(found), 4)
        self.assertEqual(len(list(self.tmp.glob("localhost.yml.backup.*"))), 1)
        self.assertIn("kept at", err)

    def test_the_list_made_when_there_is_none(self) -> None:
        self.path.write_text(STATUS_HV, encoding="utf-8")
        self.assertEqual(self.run_cmd("lxc-add", *LXC_FLAGS)[0], 0)
        text = self.path.read_text()
        self.assertTrue(text.startswith(STATUS_HV))
        self.assertIn("agent_bus_lxc_members:\n  - team: alpha\n", text)
        self.assertEqual(len(self.members()), 1)

    def test_a_dry_run_prints_the_entry_and_writes_nothing(self) -> None:
        code, out, err = self.run_cmd("lxc-add", *LXC_FLAGS, "--dry-run")
        self.assertEqual((code, err), (0, ""))
        self.assertIn('    container: "container-c"\n', out)
        self.assertEqual(self.path.read_text(), LXC_HV)
        self.assertEqual(list(self.tmp.glob("localhost.yml.backup.*")), [])

    def test_role_host_and_seat_are_written_only_when_given_and_orchestrator_is_a_coordinator(self) -> None:
        self.path.write_text(STATUS_HV, encoding="utf-8")
        code, _, err = self.run_cmd("lxc-add", *LXC_FLAGS, "--role", "orchestrator", "--seat", "boxc",
                                    "--host", "worker")
        self.assertEqual(code, 0, err)
        self.assertIn('    host: "worker"\n    checkout: "/srv/agent/three"\n    role: "coordinator"\n'
                      '    seat: "boxc"\n', self.path.read_text())
        self.assertEqual([(m.role, m.host, m.seat) for m in self.members()], [("coordinator", "worker", "boxc")])

    def test_an_entry_without_host_or_seat_is_what_a_hand_written_one_needs(self) -> None:
        self.path.write_text(STATUS_HV, encoding="utf-8")
        self.assertEqual(self.run_cmd("lxc-add", *LXC_FLAGS)[0], 0)
        self.assertNotIn("host:", self.path.read_text().split("agent_bus_lxc_members:")[1])
        self.assertNotIn("seat:", self.path.read_text().split("agent_bus_lxc_members:")[1])
        self.assertEqual([(m.host, m.seat) for m in self.members()], [(None, None)])
        # lxc-add needs only the four a person must say
        code, _, err = self.run_cmd("lxc-add", "--team", "alpha", "--container", "container-c", "--user", "agent")
        self.assertEqual(code, 64)
        self.assertIn("--checkout", err)
        self.assertNotIn("--host", err)

    def test_a_second_coordinator_is_refused_but_an_absent_one_does_not_count(self) -> None:
        # alpha's container-a is a present coordinator (read as orchestrator): a second is refused.
        code, _, err = self.run_cmd("lxc-add", *LXC_FLAGS, "--role", "coordinator")
        self.assertEqual(code, 64)
        self.assertIn("coordinator", err)
        self.assertEqual(self.path.read_text(), LXC_HV)
        # beta's only coordinator entry is absent, so beta can have one.
        beta = ("--team", "beta", "--container", "container-d", "--user", "agent",
                "--checkout", "/srv/agent/four", "--role", "coordinator")
        self.assertEqual(self.run_cmd("lxc-add", *beta)[0], 0)

    def test_refusals_write_nothing(self) -> None:
        base = dict(zip(LXC_FLAGS[::2], LXC_FLAGS[1::2], strict=True))

        def flags(**changes: str) -> list[str]:
            merged = dict(base, **{f"--{k}": v for k, v in changes.items()})
            return [part for pair in merged.items() for part in pair]

        cases = {
            "duplicate": flags(container="container-a"),
            "unknown team": flags(team="nosuch"),
            "team declared absent": flags(team="gamma"),
            "bad role": flags(role="boss"),
            "bad user": flags(user="Not A User"),
            "bad container": flags(container="-bad"),
            "host would be rewritten": flags(host="My Host"),
            "relative checkout": flags(checkout="home/agent/three"),
            "checkout the file could not hold": flags(checkout="/srv/agent/a #b"),
            "bad seat": flags(seat="!"),
        }
        for name, argv in cases.items():
            code, out, err = self.run_cmd("lxc-add", *argv)
            self.assertEqual((code, out), (64, ""), f"{name}: {err}")
            self.assertTrue(err.startswith("declare: "), f"{name}: {err}")
            self.assertEqual(self.path.read_text(), LXC_HV, name)
        missing = self.run_cmd("lxc-add", "--team", "alpha")
        self.assertEqual(missing[0], 64)

    def test_a_second_user_or_container_of_the_team_is_allowed(self) -> None:
        self.assertEqual(self.run_cmd("lxc-add", *LXC_FLAGS)[0], 0)
        other_user = ("--team", "alpha", "--container", "container-c", "--user", "other",
                      "--checkout", "/srv/other/x")
        self.assertEqual(self.run_cmd("lxc-add", *other_user)[0], 0)
        self.assertEqual(len(self.members()), 5)

    def test_an_entry_in_a_form_that_is_not_read_is_refused_and_nothing_is_written(self) -> None:
        vaulted = STATUS_HV + ("agent_bus_lxc_members:\n  - team: alpha\n    container: container-a\n"
                               "    user: agent\n    host: !vault |\n      $ANSIBLE_VAULT;1.1;AES256\n      3132\n")
        self.path.write_text(vaulted, encoding="utf-8")
        code, _, err = self.run_cmd("lxc-add", *LXC_FLAGS)
        self.assertEqual(code, 78, err)
        self.assertEqual(self.path.read_text(), vaulted)

    def test_check_judges_what_it_is_given(self) -> None:
        for argv in (("--role", "worker"), ("--role", "orchestrator"), ("--host", "worker"),
                     ("--user", "agent"), ("--checkout", "/srv/agent/x"), ("--container", "container-z"),
                     ("--team", "alpha"), LXC_FLAGS):
            self.assertEqual(self.run_cmd("lxc-check", *argv)[:2], (0, ""), argv)
        for argv in (("--role", "boss"), ("--host", "My Host"), ("--user", "A"), ("--checkout", "x"),
                     ("--container", "-x"), ("--team", "nosuch"), ("--team", "gamma"),
                     ("--team", "alpha", "--container", "container-a", "--user", "agent"),
                     ("--team", "alpha", "--role", "coordinator", "--container", "container-z", "--user", "agent")):
            self.assertEqual(self.run_cmd("lxc-check", *argv)[0], 64, argv)
        self.assertEqual(self.path.read_text(), LXC_HV)

    def test_listed_with_the_role_as_the_play_reads_it(self) -> None:
        code, out, _ = self.run_cmd("lxc-members")
        self.assertEqual(code, 0)
        self.assertEqual(out, "LXCMEMBER\talpha\tcontainer-a\tagent\tworker\tcoordinator\tpresent\n"
                              "LXCMEMBER\talpha\tcontainer-b\tagent\tother\tworker\tabsent\n"
                              "LXCMEMBER\tbeta\tcontainer-a\tagent\tworker\tcoordinator\tabsent\n")
        self.path.write_text(STATUS_HV, encoding="utf-8")
        self.assertEqual(self.run_cmd("lxc-members")[:2], (0, ""))

    def test_an_entry_leaving_the_host_out_is_listed_with_a_dash(self) -> None:
        self.path.write_text(STATUS_HV + "agent_bus_lxc_members:\n  - team: alpha\n    container: container-a\n"
                             "    user: agent\n    checkout: /srv/agent/one\n", encoding="utf-8")
        self.assertEqual(self.run_cmd("lxc-members")[:2],
                         (0, "LXCMEMBER\talpha\tcontainer-a\tagent\t-\tworker\tpresent\n"))

    def test_a_user_already_in_one_team_is_checkable_for_a_second(self) -> None:
        """Review F1: the partial check took the template's host and called it a second host."""
        self.path.write_text(STATUS_HV + "agent_bus_lxc_members:\n  - team: alpha\n    container: container-a\n"
                             "    user: agent\n    host: worker\n    checkout: /srv/agent/one\n", encoding="utf-8")
        probe = ("--team", "beta", "--container", "container-a", "--user", "agent")
        self.assertEqual(self.run_cmd("lxc-check", *probe)[:2], (0, ""))
        self.assertEqual(self.run_cmd("lxc-check", *probe, "--host", "worker")[:2], (0, ""))
        code, _, err = self.run_cmd("lxc-check", *probe, "--host", "other")
        self.assertEqual(code, 64)
        self.assertIn("two hosts", err)
        add = (*probe, "--checkout", "/srv/agent/two")
        self.assertEqual(self.run_cmd("lxc-add", *add)[0], 0)
        self.assertEqual(len(self.members()), 2)

    def test_a_seat_that_two_containers_would_hold_is_refused_when_both_are_given(self) -> None:
        self.path.write_text(STATUS_HV + "agent_bus_lxc_members:\n  - team: alpha\n"
                             "    container: container-one\n    user: agent\n    host: worker\n    seat: dev\n"
                             "    checkout: /srv/agent/one\n", encoding="utf-8")
        flags = ("--team", "alpha", "--container", "container-two", "--user", "agent", "--host", "worker",
                 "--checkout", "/srv/agent/two")
        code, _, err = self.run_cmd("lxc-add", *flags, "--seat", "dev")
        self.assertEqual(code, 64)
        self.assertIn("seat dev of team alpha", err)

    def test_the_questions_about_a_host_word_and_a_seat_are_gone(self) -> None:
        code, out, err = self.run_cmd("lxc-suggest-seat", "--team", "alpha", "--container", "c")
        self.assertEqual((code, out), (64, ""))
        self.assertIn("invalid choice", err)


class LxcDeriveTest(CommandCase):
    """The handle `ccy-teams lxc-add` shows before the yes: the rule the play applies, through
    helpers/agent_bus_lxc/derive.py, over what the caller read from the container."""

    TEXT = LXC_HV

    GIT_CONFIG = '[remote "origin"]\n\turl = https://github.com/Example-Org/Proj.git\n'

    def probe(self, *, env_local: str | None = "export HOOKS_DAEMON_HOSTNAME=worker\n",
              git_config: str = GIT_CONFIG, path: str = "/srv/agent/three") -> str:
        return json.dumps({"path": path, "uid": 1000, "git_config": git_config,
                           "env_uid": None if env_local is None else 1000, "env_local": env_local})

    def derive(self, probe: str, *, roster: str = "", container: str = "container-c", team: str = "alpha",
               extra: Sequence[str] = ()) -> tuple[int, str, str]:
        return self.run_cmd("lxc-derive", "--team", team, "--container", container, "--user", "agent",
                            "--probe", probe, "--roster", roster, *extra)

    def test_the_repo_host_and_seat_the_play_would_give(self) -> None:
        code, out, err = self.derive(self.probe())
        self.assertEqual((code, err), (0, ""))
        self.assertEqual(out, "DERIVED\tproj\tworker\t1\tproj.1+worker.lxc\n")

    def test_no_role_in_the_checkout_is_local(self) -> None:
        self.assertEqual(self.derive(self.probe(env_local=None))[1], "DERIVED\tproj\tlocal\t1\tproj.1+local.lxc\n")

    def test_the_lowest_seat_no_active_member_holds_and_a_parked_one_returns(self) -> None:
        active = "proj.1+worker.lxc=active,proj.2+worker.lxc=active"
        self.assertEqual(self.derive(self.probe(), roster=active)[1], "DERIVED\tproj\tworker\t3\tproj.3+worker.lxc\n")
        parked = "proj.1+worker.lxc=parked,proj.2+worker.lxc=active"
        self.assertEqual(self.derive(self.probe(), roster=parked)[1], "DERIVED\tproj\tworker\t1\tproj.1+worker.lxc\n")

    def test_a_seat_a_declared_entry_holds_is_not_given_again(self) -> None:
        self.path.write_text(STATUS_HV + "agent_bus_lxc_members:\n  - team: alpha\n    container: container-a\n"
                             "    user: agent\n    host: worker\n    seat: \"1\"\n    checkout: /srv/agent/one\n",
                             encoding="utf-8")
        self.assertEqual(self.derive(self.probe())[1], "DERIVED\tproj\tworker\t2\tproj.2+worker.lxc\n")

    def test_the_entrys_own_overrides_are_applied_as_the_play_applies_them(self) -> None:
        out = self.derive(self.probe(), extra=("--host", "desk", "--seat", "mine"))[1]
        self.assertEqual(out, "DERIVED\tproj\tdesk\tmine\tproj.mine+desk.lxc\n")

    def test_a_user_whose_other_entry_has_another_host_is_refused_as_the_play_would(self) -> None:
        # alpha's container-b entry says host "other"; this checkout assigns "worker".
        code, _, err = self.derive(self.probe(), container="container-b", team="beta")
        self.assertEqual(code, 64)
        self.assertIn("two hosts", err)

    def test_what_the_container_printed_is_judged(self) -> None:
        for label, probe in {"not json": "{", "a control character": self.probe(path="/x\x1b[31m"),
                             "a url with an escaped quote": self.probe(git_config='[remote "origin"]\n\turl = "a\\"b"\n'),
                             "a url rewrite": self.probe(git_config=self.GIT_CONFIG + '[url "ssh://h/"]\n\tinsteadOf = https://h/\n'),
                             "a role needing a shell": self.probe(env_local="export HOOKS_DAEMON_HOSTNAME=$(id)\n")}.items():
            code, out, err = self.derive(probe)
            self.assertEqual((code, out), (64, ""), label)
            self.assertTrue(err.startswith("declare: "), f"{label}: {err}")

    def test_a_roster_that_is_not_handles_is_refused(self) -> None:
        for roster in ("nonsense", "proj.1+worker.lxc=maybe", "proj.1+worker.lxc"):
            self.assertEqual(self.derive(self.probe(), roster=roster)[0], 64, roster)

    def test_the_file_is_not_changed(self) -> None:
        self.derive(self.probe())
        self.assertEqual(self.path.read_text(), LXC_HV)


class LxcCheckoutsTest(CommandCase):
    """The checkouts offered for the checkout question: those in the user's home with a GitHub
    remote, from what the host-side reader printed."""

    def scan(self, *items: tuple[str, str | None], ccy: tuple[str, ...] = (), cut: bool = False,
             skipped: tuple[str, ...] = ()) -> str:
        return json.dumps({"checkouts": [{"path": path, "ccy": path in ccy, "git_config": None if url is None else
                                          f'[remote "origin"]\n\turl = {url}\n'} for path, url in items],
                           "partial": cut, "skipped": list(skipped)})

    def test_a_search_root_that_is_a_symbolic_link_is_named(self) -> None:
        scan = self.scan(("/srv/agent/one", "git@github.com:example-org/one.git"), skipped=("/var/www",))
        code, out, _ = self.run_cmd("lxc-checkouts", "--scan", scan)
        self.assertEqual((code, out), (0, "CHECKOUT\t/srv/agent/one\texample-org/one\t-\nSKIPPED\t/var/www\n"))

    def test_only_checkouts_with_a_github_remote_are_listed_with_their_repository(self) -> None:
        scan = self.scan(("/srv/agent/one", "git@github.com:Example-Org/One.git"),
                         ("/srv/agent/two", "https://github.com/example-org/two"),
                         ("/srv/agent/other", "https://forge.example.com/example-org/other.git"),
                         ("/srv/agent/none", None))
        code, out, err = self.run_cmd("lxc-checkouts", "--scan", scan)
        self.assertEqual((code, err), (0, ""))
        self.assertEqual(out, "CHECKOUT\t/srv/agent/one\texample-org/one\t-\nCHECKOUT\t/srv/agent/two\texample-org/two\t-\n")

    def test_a_ccy_project_comes_first_and_is_marked(self) -> None:
        scan = self.scan(("/srv/agent/one", "git@github.com:example-org/one.git"),
                         ("/srv/agent/two", "git@github.com:example-org/two.git"), ccy=("/srv/agent/two",))
        code, out, _ = self.run_cmd("lxc-checkouts", "--scan", scan)
        self.assertEqual((code, out), (0, "CHECKOUT\t/srv/agent/two\texample-org/two\tccy\n"
                                          "CHECKOUT\t/srv/agent/one\texample-org/one\t-\n"))

    def test_a_search_that_stopped_at_a_limit_says_it_is_partial(self) -> None:
        scan = self.scan(("/srv/agent/one", "git@github.com:example-org/one.git"), cut=True)
        code, out, _ = self.run_cmd("lxc-checkouts", "--scan", scan)
        self.assertEqual((code, out), (0, "CHECKOUT\t/srv/agent/one\texample-org/one\t-\nPARTIAL\n"))

    def test_nothing_found_prints_nothing(self) -> None:
        self.assertEqual(self.run_cmd("lxc-checkouts", "--scan", self.scan())[:2], (0, ""))

    def test_what_the_container_printed_is_judged(self) -> None:
        for scan in ("{", "[]", self.scan(("relative", None)), self.scan(("/srv/\x1b[31m", None))):
            code, out, err = self.run_cmd("lxc-checkouts", "--scan", scan)
            self.assertEqual((code, out), (64, ""), scan)
            self.assertTrue(err.startswith("declare: "), err)


class LxcScriptTest(unittest.TestCase):
    """The script that reads a container's checkout from the host exists once, in derive.py; the
    Go tool asks for it here instead of carrying a copy."""

    def test_it_prints_the_script_the_play_runs(self) -> None:
        from helpers.agent_bus_lxc import derive
        out = io.StringIO()
        self.assertEqual(declare.main(["lxc-script", "--name", "root-read"], stdout=out), 0)
        self.assertEqual(out.getvalue(), derive.ROOT_READ_PY)

    def test_an_unknown_script_is_refused(self) -> None:
        err = io.StringIO()
        self.assertEqual(declare.main(["lxc-script", "--name", "nosuch"], stdout=io.StringIO(), stderr=err), 64)
        self.assertIn("invalid choice", err.getvalue())


class LxcMemberRemoveTest(CommandCase):
    TEXT = LXC_HV

    def test_declared_absent_and_every_other_line_kept(self) -> None:
        code, out, err = self.run_cmd("lxc-remove", "--team", "alpha", "--container", "container-a",
                                      "--user", "agent")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"CHANGED\tLXC member agent@container-a in team alpha declared absent, in {self.path}\n")
        text = self.path.read_text()
        # The state line goes under the entry's team line, where `remove` puts a team's.
        self.assertEqual(text, LXC_HV.replace("  - team: alpha\n    container: container-a\n",
                                              "  - team: alpha\n    state: absent\n    container: container-a\n", 1))
        self.assertEqual(len(list(self.tmp.glob("localhost.yml.backup.*"))), 1)

    def test_an_entry_already_absent_is_unchanged(self) -> None:
        argv = ("--team", "alpha", "--container", "container-b", "--user", "agent")
        self.assertEqual(self.run_cmd("lxc-remove", *argv)[:2], (0, "UNCHANGED\tagent@container-b in team alpha\n"))
        self.assertEqual(self.path.read_text(), LXC_HV)

    def test_dry_run_and_unknown_entries(self) -> None:
        code, out, _ = self.run_cmd("lxc-remove", "--team", "alpha", "--container", "container-a",
                                    "--user", "agent", "--dry-run")
        self.assertEqual(code, 0)
        self.assertIn("state: absent", out)
        self.assertEqual(self.path.read_text(), LXC_HV)
        for argv in (("--team", "alpha", "--container", "nosuch", "--user", "agent"),
                     ("--team", "nosuch", "--container", "container-a", "--user", "agent")):
            self.assertEqual(self.run_cmd("lxc-remove", *argv)[0], 64, argv)


class LxcMemberRoleTest(CommandCase):
    """lxc-role: the takeover of a declared LXC coordinator makes its entry a worker first
    (Plan 00203 SPEC-hot-control.md H25), every other line kept."""

    TEXT = LXC_HV
    A = ("--team", "alpha", "--container", "container-a", "--user", "agent")

    def test_the_role_line_is_replaced_and_every_other_line_kept(self) -> None:
        code, out, err = self.run_cmd("lxc-role", *self.A, "--role", "worker")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, f"CHANGED\tLXC member agent@container-a in team alpha is now worker, in {self.path}\n")
        self.assertEqual(self.path.read_text(), LXC_HV.replace("    role: orchestrator\n", '    role: "worker"\n', 1))
        self.assertEqual(len(list(self.tmp.glob("localhost.yml.backup.*"))), 1)

    def test_a_role_line_is_added_when_there_is_none(self) -> None:
        self.path.write_text(LXC_HV.replace("    role: orchestrator\n", ""), encoding="utf-8")
        self.assertEqual(self.run_cmd("lxc-role", *self.A, "--role", "coordinator")[0], 0)
        self.assertIn('  - team: alpha\n    role: "coordinator"\n    container: container-a\n', self.path.read_text())

    def test_the_same_role_is_unchanged_and_bad_input_refused(self) -> None:
        self.path.write_text(LXC_HV.replace("role: orchestrator", "role: worker"), encoding="utf-8")
        before = self.path.read_text()
        self.assertEqual(self.run_cmd("lxc-role", *self.A, "--role", "worker")[:2],
                         (0, "UNCHANGED\tagent@container-a in team alpha\n"))
        self.assertEqual(self.path.read_text(), before)
        for argv in ((*self.A, "--role", "boss"),
                     ("--team", "alpha", "--container", "nosuch", "--user", "agent", "--role", "worker")):
            self.assertEqual(self.run_cmd("lxc-role", *argv)[0], 64, argv)
        self.assertEqual(self.path.read_text(), before)

    def test_dry_run_writes_nothing(self) -> None:
        code, out, _ = self.run_cmd("lxc-role", *self.A, "--role", "worker", "--dry-run")
        self.assertEqual(code, 0)
        self.assertIn('role: "worker"', out)
        self.assertEqual(self.path.read_text(), LXC_HV)


if __name__ == "__main__":
    unittest.main()
