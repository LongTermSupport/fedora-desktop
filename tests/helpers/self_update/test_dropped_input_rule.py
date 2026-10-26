"""The rule play-input-dropped (Defence Before Fix; CLAUDE/QA.md).

`run.bash --changed` runs a play again when one of its inputs changed, and its inputs are
what `helpers/self_update/affected_plays.play_inputs` reads from the play text. A checkout
path the play builds, reads or deploys that the mapper neither lists as an input nor reports
as UNRESOLVED is silently dropped: the play never runs again when that path changes. Plan
00192 Task 3.6 found every play that builds a Go binary dropping its whole Go source, because
`tasks/build-go-command.yml` reaches src-go as `root_dir ~ '/src-go'`, a form the mapper did
not read.

The rule reads play text rather than running it, with its own patterns, not the mapper's. A
finding is a line of a YAML file a host play walks (the play and every task, vars or play
file it reaches) holding a checkout reference, `{{ root_dir }}/<path>` (or root_dir's own
config lookup) or a Jinja concatenation `root_dir ~ '/<path>'` / `root_dir + '/<path>'`,
whose path is not an input of that play and is not named by an UNRESOLVED entry for that
line. A reference to a Go module root (a directory holding go.mod) is accounted for when the
mapper took it as that module's Go builds.

The bare checkout root (`chdir: "{{ root_dir }}"`, an argv element, `-X main.RootDir=`) is
not judged. The next wider rule, any checkout path handed to a program that reads what it
likes, was not built: a bare root is mostly the working directory of `python3 -m helpers.x`,
whose package the mapper follows already, and what a program reads from the root is not in
the play text for a reader of text to judge.

Fix a finding in the mapper, so it follows the form (or reports it UNRESOLVED), never by
rewording the play to hide the reference.
"""

from __future__ import annotations

import os
import posixpath
import re
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import affected_plays as ap

RULE_ID = "play-input-dropped"

_ROOT = (
    r"(?:\{\{\s*root_dir\s*\}\}"
    r"|\{\{\s*lookup\(\s*'ansible\.builtin\.config'\s*,\s*'CONFIG_FILE'\s*\)\s*\|\s*dirname\s*\}\})"
)
_REFERENCE = re.compile(
    _ROOT + r"/(?P<rooted>[^\s\"'{}(),]*)"
    + r"|\broot_dir\s*[~+]\s*(?P<quote>['\"])/(?P<joined>[^'\"]*)(?P=quote)"
)
_GLOB = re.compile(r"[*?\[]")


def _reference_path(literal: str) -> str:
    """The checkout path a reference names: its literal part up to any glob, normalised."""
    glob = _GLOB.search(literal)
    if glob:
        literal = literal[: glob.start()]
    literal = literal.rstrip("/")
    return posixpath.normpath(literal) if literal else ""


def _accounted(inputs: ap.Inputs, path: str) -> bool:
    if not path:
        return False
    if path in getattr(inputs, "go_modules", set()):
        return True
    if ap.affects(inputs, path) or ap.affects(inputs, path + "/"):
        return True
    return any(prefix.startswith(path + "/") for prefix in inputs.prefixes)


def dropped(repo_root: str, inputs: ap.Inputs) -> list[str]:
    """Every checkout reference in the YAML files of `inputs` that `inputs` drops."""
    findings = []
    for rel in sorted(path for path in inputs.exact if path.endswith((".yml", ".yaml"))):
        full = os.path.join(repo_root, rel)
        if not os.path.isfile(full):
            continue
        with open(full, encoding="utf-8") as handle:
            lines = handle.read().splitlines()
        for number, line in enumerate(lines, start=1):
            if line.lstrip().startswith("#"):
                continue
            where = f"{rel}:{number}:"
            if any(entry.startswith(where) for entry in inputs.unresolved):
                continue
            for match in _REFERENCE.finditer(line):
                literal = match.group("rooted")
                if literal is None:
                    literal = match.group("joined")
                path = _reference_path(literal)
                if not _accounted(inputs, path):
                    findings.append(f"{where} {match.group(0)}")
    return findings


def _message(play: str, findings: list[str]) -> str:
    return (
        f"[{RULE_ID}] {play} drops checkout references the mapper neither follows nor reports;"
        f" see CLAUDE/QA.md#{RULE_ID}:\n  " + "\n  ".join(findings)
    )


class TestRuleFires(unittest.TestCase):
    """The rule proved against fixtures, with stand-in inputs, before it judges the repo."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name

    def _play(self, body: str) -> str:
        rel = "playbooks/imports/play-a.yml"
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path))
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(textwrap.dedent(body))
        return rel

    def test_a_joined_reference_the_inputs_lack_is_a_finding(self) -> None:
        """The originating shape: tasks/build-go-command.yml's `root_dir ~ '/src-go'`."""
        play = self._play("""
            - ansible.builtin.command:
                argv: [go, build]
                chdir: "{{ root_dir ~ '/src-go' }}"
        """)
        found = dropped(self.root, ap.Inputs(exact={play}))
        self.assertEqual(len(found), 1)
        self.assertIn(f"{play}:4:", found[0])

    def test_plus_joins_and_globs_are_references_too(self) -> None:
        play = self._play("""
            - ansible.builtin.set_fact:
                a: "{{ lookup('file', root_dir + '/files/conf/' + item) }}"
                b: "{{ lookup('fileglob', root_dir ~ '/helpers/tool/*.py', wantlist=true) }}"
        """)
        self.assertEqual(len(dropped(self.root, ap.Inputs(exact={play}))), 2)

    def test_a_rooted_reference_the_inputs_lack_is_a_finding(self) -> None:
        play = self._play("""
            - ansible.builtin.copy:
                src: "{{ root_dir }}/files/bin/tool"
        """)
        self.assertEqual(len(dropped(self.root, ap.Inputs(exact={play}))), 1)

    def test_a_reference_that_is_an_input_is_not_a_finding(self) -> None:
        play = self._play("""
            - ansible.builtin.set_fact:
                a: "{{ lookup('file', root_dir ~ '/files/conf/x') }}"
                b: "{{ lookup('fileglob', root_dir ~ '/helpers/tool/*.py') }}"
                c: "{{ root_dir }}/files/bin/{{ item }}"
        """)
        inputs = ap.Inputs(exact={play, "files/conf/x"}, prefixes={"helpers/tool/", "files/bin/"})
        self.assertEqual(dropped(self.root, inputs), [])

    def test_a_reference_reported_unresolved_is_not_a_finding(self) -> None:
        play = self._play("""
            - ansible.builtin.set_fact:
                a: "{{ root_dir ~ '/' ~ some_path }}"
        """)
        inputs = ap.Inputs(exact={play}, unresolved=[f"{play}:3: root_dir ~ '/' ~ some_path"])
        self.assertEqual(dropped(self.root, inputs), [])

    def test_a_joined_root_with_no_literal_path_is_a_finding_unless_reported(self) -> None:
        play = self._play("""
            - ansible.builtin.set_fact:
                a: "{{ root_dir ~ '/' ~ some_path }}"
        """)
        self.assertEqual(len(dropped(self.root, ap.Inputs(exact={play}))), 1)

    def test_the_bare_root_and_comments_are_not_judged(self) -> None:
        play = self._play("""
            # formerly {{ root_dir }}/files/old and root_dir ~ '/files/older'
            - ansible.builtin.command:
                argv: [python3, -m, helpers.x]
                chdir: "{{ root_dir }}"
        """)
        self.assertEqual(dropped(self.root, ap.Inputs(exact={play})), [])

    def test_the_message_carries_the_rule_id(self) -> None:
        self.assertIn(f"[{RULE_ID}]", _message("p", ["f:1: x"]))
        self.assertIn(f"CLAUDE/QA.md#{RULE_ID}", _message("p", ["f:1: x"]))


if __name__ == "__main__":
    unittest.main()
