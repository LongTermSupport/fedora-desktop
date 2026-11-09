"""The rule play-input-listed-past-git (Defence Before Fix; CLAUDE/QA.md; issue #198).

A play's inputs are the files git would publish from the checkout: tracked, and untracked but
not ignored (a new task file not yet committed still counts). An ignored file is a local
artefact (`__pycache__/*.pyc`, build output, scratch): it changes with whatever last ran on this
machine, and git never reports a change to it. A reader of play inputs that lists a directory
from the filesystem counts those artefacts as inputs, so the change and dirty judgement of a
play depends on the machine. `play_ledger.group_vars.play_text` read every `.pyc` under four
plays' helper trees.

The rule has two halves:

- **The code.** No module of the play-input packages (`helpers/play_ledger/`,
  `helpers/self_update/`) lists a directory itself: no call named `walk`, `fwalk`, `scandir`,
  `listdir`, `glob`, `iglob`, `rglob` or `iterdir` (bare or as an attribute, `ast.walk` aside),
  and no `find` argv. A directory's files come from git's own listing
  (`affected_plays.checkout_files`). It reads the source with `ast`, never runs it.
- **The hazard.** Over a fixture repository with ignored files planted where each reader looks,
  `play_text`, `play_inputs` and `candidate_plays` see none of them.

Fix a finding by listing through `affected_plays.checkout_files`; never by catching only the
names seen so far (`__pycache__`), which leaves every other ignored file an input.
"""

from __future__ import annotations

import ast
import os
import subprocess
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.play_ledger import group_vars as gv
from helpers.self_update import affected_plays as ap

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
RULE_ID = "play-input-listed-past-git"
SCOPE = ("helpers/play_ledger/", "helpers/self_update/")
LISTING_NAMES = frozenset({"walk", "fwalk", "scandir", "listdir", "glob", "iglob", "rglob", "iterdir"})
IGNORED_MARK = "IGNORED_ARTEFACT_MARK"


def _isolated_git_env() -> dict[str, str]:
    env = {key: value for key, value in os.environ.items()
           if key not in ("GIT_CONFIG_COUNT", "GIT_CONFIG_PARAMETERS")}
    env["GIT_CONFIG_GLOBAL"] = os.devnull
    env["GIT_CONFIG_NOSYSTEM"] = "1"
    return env


def scope_modules(repo_root: str) -> list[str]:
    """The Python files git would publish under the play-input packages, by git's own listing
    (not the code under test's)."""
    listed = subprocess.run(
        ["git", "-C", repo_root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", *SCOPE],
        capture_output=True, text=True, check=True, env=_isolated_git_env(),
    )
    return sorted(path for path in listed.stdout.split("\0") if path.endswith(".py"))


def listings(source: str, rel: str) -> list[str]:
    """Every directory listing in `source`, as `<rel>:<line>: <call>`."""
    findings = []
    for node in ast.walk(ast.parse(source, filename=rel)):
        if not isinstance(node, ast.Call):
            continue
        func = node.func
        if isinstance(func, ast.Attribute):
            receiver = func.value
            is_syntax_walk = isinstance(receiver, ast.Name) and receiver.id == "ast"
            if func.attr in LISTING_NAMES and not is_syntax_walk:
                findings.append(f"{rel}:{node.lineno}: {ast.unparse(func)}(...)")
                continue
        elif isinstance(func, ast.Name) and func.id in LISTING_NAMES:
            findings.append(f"{rel}:{node.lineno}: {func.id}(...)")
            continue
        first = node.args[0] if node.args else None
        if (isinstance(first, (ast.List, ast.Tuple)) and first.elts and isinstance(first.elts[0], ast.Constant)
                and first.elts[0].value == "find"):
            findings.append(f"{rel}:{node.lineno}: a find argv")
    return findings


def scope_findings(repo_root: str) -> list[str]:
    findings = []
    for rel in scope_modules(repo_root):
        with open(os.path.join(repo_root, rel), encoding="utf-8") as handle:
            findings.extend(listings(handle.read(), rel))
    return findings


def _message(findings: list[str]) -> str:
    return (f"[{RULE_ID}] a play-input reader lists a directory past git, so ignored files become "
            f"inputs (see CLAUDE/QA.md#{RULE_ID}):\n  " + "\n  ".join(findings))


def _write(root: str, rel: str, text: str) -> None:
    path = os.path.join(root, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w", encoding="utf-8") as handle:
        handle.write(text)


class TestTheCodeHalf(unittest.TestCase):
    def test_every_listing_form_is_a_finding(self) -> None:
        source = textwrap.dedent("""
            import glob, os
            from os import walk
            from pathlib import Path
            os.walk(top)
            os.fwalk(top)
            os.scandir(top)
            os.listdir(top)
            glob.glob(pattern)
            glob.iglob(pattern)
            Path(top).rglob("*")
            Path(top).glob("*")
            Path(top).iterdir()
            Path(top).walk()
            walk(top)
            subprocess.run(["find", top, "-type", "f"], check=True)
        """)
        self.assertEqual(len(listings(source, "m.py")), 12)

    def test_a_syntax_tree_walk_and_git_listing_are_not_findings(self) -> None:
        source = textwrap.dedent("""
            import ast
            for node in ast.walk(tree):
                pass
            subprocess.run(["git", "ls-files", "-z"], check=True)
            os.path.isfile(path)
        """)
        self.assertEqual(listings(source, "m.py"), [])

    def test_the_originating_instance_is_a_finding(self) -> None:
        source = textwrap.dedent("""
            def play_text(repo_root, inputs):
                for prefix in inputs.prefixes:
                    for dirpath, _dirs, files in os.walk(os.path.join(repo_root, prefix)):
                        pass
        """)
        self.assertEqual(listings(source, "group_vars.py"), ["group_vars.py:4: os.walk(...)"])

    def test_the_message_carries_the_rule_id(self) -> None:
        self.assertIn(f"[{RULE_ID}]", _message(["x"]))
        self.assertIn(f"CLAUDE/QA.md#{RULE_ID}", _message(["x"]))

    def test_no_play_input_module_lists_a_directory_past_git(self) -> None:
        modules = scope_modules(REPO_ROOT)
        self.assertIn("helpers/play_ledger/group_vars.py", modules)
        self.assertIn("helpers/self_update/affected_plays.py", modules)
        findings = scope_findings(REPO_ROOT)
        self.assertEqual(findings, [], _message(findings))


class TestTheHazardHalf(unittest.TestCase):
    """Ignored files planted where each reader looks: under a directory a play copies, in a
    helper package a play runs, and among the plays themselves."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name
        play = textwrap.dedent("""
            - hosts: desktop
              tasks:
                - ansible.builtin.copy:
                    src: "{{ root_dir }}/files/conf/"
                    dest: /etc/conf/
                - ansible.builtin.command:
                    argv: [python3, -m, helpers.tool]
        """)
        for rel, text in (
            (".gitignore", "__pycache__/\n*.pyc\ngenerated_*.py\nlocal-*.yml\n"),
            ("playbooks/imports/play-a.yml", play),
            ("playbooks/imports/local-scratch.yml", f"# {IGNORED_MARK}\n"),
            ("files/conf/a.conf", "a\n"),
            ("files/conf/__pycache__/m.cpython-313.pyc", f"{IGNORED_MARK}\n"),
            ("files/conf/stray.pyc", f"{IGNORED_MARK}\n"),
            ("helpers/tool/main.py", "print('tool')\n"),
            ("helpers/tool/generated_extra.py", f"# {IGNORED_MARK}\n"),
        ):
            _write(self.root, rel, text)
        for args in (("init", "-q"), ("add", "-A")):
            subprocess.run(["git", "-C", self.root, *args], check=True, capture_output=True, env=_isolated_git_env())

    def _ignored(self, paths: set[str]) -> set[str]:
        checked = subprocess.run(["git", "-C", self.root, "check-ignore", "-z", "--stdin"], input="\0".join(sorted(paths)),
                                 capture_output=True, text=True, check=False, env=_isolated_git_env())
        if checked.returncode not in (0, 1):
            self.fail(f"git check-ignore failed: {checked.stderr}")
        return {path for path in checked.stdout.split("\0") if path}

    def test_play_text_reads_no_ignored_file(self) -> None:
        inputs = ap.play_inputs(self.root, "playbooks/imports/play-a.yml")
        self.assertIn("files/conf/", inputs.prefixes)
        self.assertNotIn(IGNORED_MARK, gv.play_text(self.root, inputs), f"[{RULE_ID}] play_text read an ignored file")

    def test_play_inputs_holds_no_ignored_module(self) -> None:
        inputs = ap.play_inputs(self.root, "playbooks/imports/play-a.yml")
        self.assertIn("helpers/tool/main.py", inputs.exact)
        self.assertEqual(self._ignored(inputs.exact), set(), f"[{RULE_ID}] play_inputs counted an ignored file")

    def test_no_ignored_file_is_a_candidate_play(self) -> None:
        plays = ap.candidate_plays(self.root)
        self.assertIn("playbooks/imports/play-a.yml", plays)
        self.assertEqual(self._ignored(set(plays)), set(), f"[{RULE_ID}] an ignored file is a candidate play")


if __name__ == "__main__":
    unittest.main()
