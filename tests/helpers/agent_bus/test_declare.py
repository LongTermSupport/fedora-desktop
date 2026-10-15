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


if __name__ == "__main__":
    unittest.main()
