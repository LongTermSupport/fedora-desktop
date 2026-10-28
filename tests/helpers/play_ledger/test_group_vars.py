"""Tests for helpers.play_ledger.group_vars — which variables of the group_vars file changed,
and which of them a play reads (Plan 00192 Task 3.11).

A change to `environment/localhost/group_vars/desktop.yml` is not a change to any one play's
inputs: every play loads the file, so counting it would rerun every play. A play is to run
again for it when a variable that changed is one the play reads, by name, in its own files.
"""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import group_vars
from helpers.self_update.affected_plays import Inputs

BASE = (
    "---\n"
    "# The profile.\n"
    "_target: \"{{ lookup('ansible.builtin.pipe', 'systemctl get-default') }}\"\n"
    "profile: \"{{ 'desktop' if _target == 'graphical.target' else 'server' }}\"\n"
    "\n"
    "# Who uses it.\n"
    "audience: developer\n"
    "packages:\n"
    "  - git\n"
    "  - vim\n"
)


def _isolated_git_env() -> dict[str, str]:
    env = {key: value for key, value in os.environ.items()
           if key not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
    env["GIT_CONFIG_GLOBAL"] = os.devnull
    env["GIT_CONFIG_NOSYSTEM"] = "1"
    return env


def _git(root: str, *args: str) -> str:
    return subprocess.run(["git", "-C", root, *args], check=True, capture_output=True,
                          text=True, env=_isolated_git_env()).stdout


def _write(root: str, path: str, text: str | bytes) -> None:
    full = os.path.join(root, path)
    os.makedirs(os.path.dirname(full), exist_ok=True)
    mode = "wb" if isinstance(text, bytes) else "w"
    with open(full, mode, **({} if isinstance(text, bytes) else {"encoding": "utf-8"})) as handle:
        handle.write(text)


class TestTopLevel(unittest.TestCase):
    def test_each_top_level_key_is_read_with_its_block(self) -> None:
        found = group_vars.top_level(BASE)
        self.assertEqual(sorted(found), ["_target", "audience", "packages", "profile"])
        self.assertIn("vim", found["packages"])

    def test_a_line_it_cannot_read_raises_rather_than_drop_a_variable(self) -> None:
        with self.assertRaises(ValueError) as caught:
            group_vars.top_level("audience: developer\n{{ odd }}\n")
        self.assertIn(":2:", str(caught.exception))

    def test_a_key_set_twice_raises(self) -> None:
        with self.assertRaises(ValueError):
            group_vars.top_level("audience: a\naudience: b\n")


class TestChangedNames(unittest.TestCase):
    def test_one_changed_value_names_that_variable(self) -> None:
        self.assertEqual(group_vars.changed_names(BASE, BASE.replace("developer", "consumer")),
                         {"audience"})

    def test_a_change_inside_a_block_names_its_key(self) -> None:
        self.assertEqual(group_vars.changed_names(BASE, BASE.replace("  - vim\n", "")), {"packages"})

    def test_a_comment_or_blank_line_names_nothing(self) -> None:
        edited = BASE.replace("# Who uses it.\n", "# Who uses the machine.\n\n")
        self.assertEqual(group_vars.changed_names(BASE, edited), set())

    def test_an_added_variable_is_named(self) -> None:
        self.assertEqual(group_vars.changed_names(BASE, BASE + "extra: 1\n"), {"extra"})

    def test_a_removed_variable_is_named(self) -> None:
        self.assertEqual(group_vars.changed_names(BASE, BASE.replace("audience: developer\n", "")),
                         {"audience"})

    def test_a_variable_whose_value_uses_a_changed_one_is_named_too(self) -> None:
        """`profile` is computed from `_target`: a play reading `profile` reads the change."""
        edited = BASE.replace("systemctl get-default", "systemctl --user get-default")
        self.assertEqual(group_vars.changed_names(BASE, edited), {"_target", "profile"})

    def test_a_file_added_or_removed_names_every_variable(self) -> None:
        every = {"_target", "profile", "audience", "packages"}
        self.assertEqual(group_vars.changed_names(None, BASE), every)
        self.assertEqual(group_vars.changed_names(BASE, None), every)


class TestNamesRead(unittest.TestCase):
    def test_a_name_is_read_where_it_stands_as_a_whole_word(self) -> None:
        text = "when: audience == 'developer'\nx: \"{{ hostvars['localhost']['profile'] }}\"\n"
        self.assertEqual(group_vars.names_read({"audience", "profile", "packages"}, text),
                         ["audience", "profile"])

    def test_a_longer_name_holding_it_is_not_a_read(self) -> None:
        self.assertEqual(group_vars.names_read({"audience"}, "install_audience_extra: 1\naudiences\n"), [])


class TestPlayText(unittest.TestCase):
    def test_reads_its_exact_files_and_the_files_under_its_directories(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            _write(root, "playbooks/imports/play-a.yml", "when: audience\n")
            _write(root, "files/etc/a.conf.j2", "{{ profile }}\n")
            _write(root, "files/blob.bin", b"\xff\xfe\x00 packages")
            inputs = Inputs(exact={"playbooks/imports/play-a.yml", "files/gone.txt"},
                            prefixes={"files/"})
            text = group_vars.play_text(root, inputs)
            self.assertIn("audience", text)
            self.assertIn("profile", text)
            self.assertNotIn("packages", text)


class TestTextAt(unittest.TestCase):
    def test_the_file_at_a_commit_or_none_where_it_is_absent(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            _git(root, "init", "-q")
            _git(root, "config", "user.email", "test@example.com")
            _git(root, "config", "user.name", "test")
            _git(root, "config", "commit.gpgsign", "false")
            _write(root, "README", "x\n")
            _git(root, "add", ".")
            _git(root, "commit", "-q", "-m", "first")
            first = _git(root, "rev-parse", "HEAD").strip()
            _write(root, group_vars.GROUP_VARS, BASE)
            _git(root, "add", ".")
            _git(root, "commit", "-q", "-m", "second")
            second = _git(root, "rev-parse", "HEAD").strip()
            env = _isolated_git_env()
            self.assertIsNone(group_vars.text_at(root, first, env=env))
            self.assertEqual(group_vars.text_at(root, second, env=env), BASE)
            self.assertEqual(group_vars.text_now(root), BASE)
            os.remove(os.path.join(root, group_vars.GROUP_VARS))
            self.assertIsNone(group_vars.text_now(root))

    def test_a_commit_git_cannot_read_raises(self) -> None:
        with tempfile.TemporaryDirectory() as root:
            _git(root, "init", "-q")
            with self.assertRaises(ValueError):
                group_vars.text_at(root, "c" * 40, env=_isolated_git_env())


if __name__ == "__main__":
    unittest.main()
