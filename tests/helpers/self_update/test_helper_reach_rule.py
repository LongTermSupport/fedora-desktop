"""The rule play-helper-input-unreachable (Defence Before Fix; CLAUDE/QA.md).

`run.bash --changed` runs a play again when one of its inputs changed. A play that runs one
helper module, `python3 -m helpers.gnome.apply_enabled_extensions`, was given the whole
`helpers/gnome/` package as inputs, and then every package ANY module of that package imports.
`helpers/gnome/check_panel_contract.py` is a QA gate no play runs, and it imports
`helpers.play_ledger`, `helpers.host_health` and `helpers.containerwatch`; so a change to the
ledger's own code made play-gnome-shell-extensions, play-fedora-desktop-panel and
play-displaylink stale (Plan 00141), and the judge asked for three plays to be run again that
nothing they run had changed for.

The rule computes, with its own code and not the mapper's, the helper modules a play can
execute, and requires the helper Python files the mapper counts as inputs to be exactly those:

- the entries are the modules the play's YAML files (the play and every task, vars and play file
  the mapper walks) name on a non-comment line, as a dotted `helpers.<pkg>.<module>` (a
  `python3 -m` argv, an `ExecStart=` it writes) or as a rooted path
  `{{ root_dir }}/helpers/<pkg>/<module>.py` (a file it copies). A name or path that is a
  directory brings every module under it, because the play reaches it whole;
- the closure follows, in each module reached, every `import` and `from ... import` anywhere in
  the module (a function-level import runs too), absolute or relative, and every `helpers.x.y`
  named in a string that is not a docstring (`[sys.executable, "-m", "helpers.vmtest.bridge_run"]`
  runs that module as a child), and every checkout path under `helpers/` such a string is
  (`helpers/pingbus/bundle.py` packages `"helpers/pingbus"` and `"helpers/agent_bus"` whole
  into the zipapp a play deploys, so every module in them is deployed by that play).

A file counted that the closure does not reach is a finding (the play is judged stale for code
it cannot run); a file the closure reaches that is not counted is a finding too (a dropped input,
the play never runs again when it changes). What a module READS is play-program-read-dropped's
business, not this rule's. A helper a deployed file runs later, live from the checkout (a systemd
unit's `ExecStart`, a bash tool), is outside the play's run; the next wider rule, counting those,
was not built because such a program reads the checkout every time it runs, so no rerun is owed.
"""

from __future__ import annotations

import ast
import os
import posixpath
import re
import sys
import tempfile
import textwrap
import unittest

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "..", ".."))

from helpers.self_update import affected_plays as ap

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
RULE_ID = "play-helper-input-unreachable"

_DOTTED = re.compile(r"(?<![\w.])helpers(?:\.[A-Za-z_][A-Za-z0-9_]*)+")
_ROOTED_HELPER = re.compile(
    r"(?:\{\{\s*root_dir\s*\}\}|CONFIG_FILE'\s*\)\s*\|\s*dirname\s*\}\})/(helpers/[A-Za-z0-9_./-]*)"
    r"|\broot_dir\s*[~+]\s*['\"]/(helpers/[A-Za-z0-9_./-]*)"
)
#: A string that IS a checkout path under helpers/, as a module names one it packages.
_HELPER_LITERAL = re.compile(r"^(helpers/[A-Za-z0-9_./-]+)$")


def _modules_under(repo_root: str, rel_dir: str) -> set[str]:
    found = set()
    for dirpath, _dirs, files in os.walk(os.path.join(repo_root, rel_dir)):
        for name in files:
            if name.endswith(".py"):
                found.add(os.path.relpath(os.path.join(dirpath, name), repo_root))
    return found


def _dotted_modules(repo_root: str, dotted: str) -> set[str]:
    """The files a dotted name runs: the longest prefix that is a module, or a directory whole."""
    parts = dotted.split(".")
    for end in range(len(parts), 1, -1):
        rel = "/".join(parts[:end])
        if os.path.isfile(os.path.join(repo_root, rel + ".py")):
            return {rel + ".py"}
        if os.path.isdir(os.path.join(repo_root, rel)):
            return _modules_under(repo_root, rel)
    return set()


def _path_modules(repo_root: str, rel: str) -> set[str]:
    rel = posixpath.normpath(rel.rstrip("/"))
    if rel.endswith(".py") and os.path.isfile(os.path.join(repo_root, rel)):
        return {rel}
    if os.path.isdir(os.path.join(repo_root, rel)):
        return _modules_under(repo_root, rel)
    return set()


def entries(repo_root: str, yaml_files: set[str]) -> set[str]:
    found: set[str] = set()
    for rel in sorted(yaml_files):
        with open(os.path.join(repo_root, rel), encoding="utf-8") as handle:
            text = handle.read()
        for line in text.splitlines():
            if line.lstrip().startswith("#"):
                continue
            for dotted in _DOTTED.findall(line):
                found |= _dotted_modules(repo_root, dotted)
            for match in _ROOTED_HELPER.finditer(line):
                found |= _path_modules(repo_root, match.group(1) or match.group(2))
    return found


def _docstrings(tree: ast.Module) -> set[int]:
    ids = set()
    for node in ast.walk(tree):
        if isinstance(node, (ast.Module, ast.ClassDef, ast.FunctionDef, ast.AsyncFunctionDef)):
            body = node.body
            if body and isinstance(body[0], ast.Expr) and isinstance(body[0].value, ast.Constant):
                ids.add(id(body[0].value))
    return ids


def imported(repo_root: str, rel: str) -> set[str]:
    """The helper files a module can run: what it imports, and the modules its strings name."""
    with open(os.path.join(repo_root, rel), encoding="utf-8") as handle:
        tree = ast.parse(handle.read(), filename=rel)
    package = rel[: -len(".py")].replace("/", ".").rsplit(".", 1)[0]
    names: set[str] = set()
    paths: set[str] = set()
    skip = _docstrings(tree)
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            names.update(alias.name for alias in node.names)
        elif isinstance(node, ast.ImportFrom):
            base = node.module or ""
            if node.level:
                anchor = package.split(".")[: len(package.split(".")) - (node.level - 1)]
                base = ".".join(anchor + ([base] if base else []))
            for alias in node.names:
                names.add(f"{base}.{alias.name}" if alias.name != "*" else base)
        elif isinstance(node, ast.Constant) and isinstance(node.value, str) and id(node) not in skip:
            names.update(_DOTTED.findall(node.value))
            paths.update(_HELPER_LITERAL.findall(node.value))
    found: set[str] = set()
    for name in names:
        if name.split(".")[0] == "helpers":
            found |= _dotted_modules(repo_root, name)
    for path in paths:
        found |= _path_modules(repo_root, path)
    return found


def reachable(repo_root: str, yaml_files: set[str]) -> set[str]:
    pending = sorted(entries(repo_root, yaml_files))
    seen: set[str] = set()
    while pending:
        rel = pending.pop()
        if rel in seen:
            continue
        seen.add(rel)
        pending.extend(sorted(imported(repo_root, rel) - seen))
    return seen


def findings(repo_root: str, inputs: ap.Inputs) -> list[str]:
    yaml_files = {rel for rel in inputs.exact if rel.endswith((".yml", ".yaml"))
                  and os.path.isfile(os.path.join(repo_root, rel))}
    reach = reachable(repo_root, yaml_files)
    counted = {rel for rel in _modules_under(repo_root, "helpers") if ap.affects(inputs, rel)}
    return [f"counted, but nothing the play runs imports it: {rel}" for rel in sorted(counted - reach)] + [
        f"run by the play, but not counted: {rel}" for rel in sorted(reach - counted)
    ]


def _message(play: str, found: list[str]) -> str:
    return (
        f"[{RULE_ID}] {play}: the helper inputs are not the modules the play runs;"
        f" see CLAUDE/QA.md#{RULE_ID}:\n  " + "\n  ".join(found)
    )


class TestRuleFires(unittest.TestCase):
    """The rule proved against fixtures before it judges the repo."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name

    def _file(self, rel: str, text: str = "X = 1\n") -> str:
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(textwrap.dedent(text))
        return rel

    def _gnome_shape(self) -> str:
        """The originating shape: the play runs one module; a sibling QA module imports more."""
        self._file("helpers/gnome/apply.py", "from helpers.gnome import bus\n")
        self._file("helpers/gnome/bus.py")
        self._file("helpers/gnome/contract.py", "from helpers.ledger import store\n")
        self._file("helpers/ledger/store.py")
        return self._file("playbooks/imports/play-a.yml", """
            - hosts: desktop
              tasks:
                - ansible.builtin.command:
                    argv: [python3, -m, helpers.gnome.apply]
        """)

    def test_a_sibling_module_and_its_imports_counted_are_findings(self) -> None:
        play = self._gnome_shape()
        inputs = ap.Inputs(exact={play}, prefixes={"helpers/gnome/", "helpers/ledger/"})
        self.assertEqual(findings(self.root, inputs), [
            "counted, but nothing the play runs imports it: helpers/gnome/contract.py",
            "counted, but nothing the play runs imports it: helpers/ledger/store.py",
        ])

    def test_exactly_the_run_module_and_its_imports_is_clean(self) -> None:
        play = self._gnome_shape()
        inputs = ap.Inputs(exact={play, "helpers/gnome/apply.py", "helpers/gnome/bus.py"})
        self.assertEqual(findings(self.root, inputs), [])

    def test_an_import_left_out_is_a_finding(self) -> None:
        play = self._gnome_shape()
        inputs = ap.Inputs(exact={play, "helpers/gnome/apply.py"})
        self.assertEqual(findings(self.root, inputs), ["run by the play, but not counted: helpers/gnome/bus.py"])

    def test_function_level_relative_and_child_process_runs_are_reached(self) -> None:
        self._file("helpers/tool/cli.py", '''
            """Runs helpers.tool.never in prose only."""
            import sys


            def main():
                from . import near
                from helpers.other import far
                return [sys.executable, "-m", "helpers.tool.child"]
        ''')
        for name in ("near", "child", "never"):
            self._file(f"helpers/tool/{name}.py")
        self._file("helpers/other/far.py")
        play = self._file("playbooks/imports/play-a.yml", "- command: python3 -m helpers.tool.cli\n")
        self.assertEqual(reachable(self.root, {play}), {
            "helpers/tool/cli.py", "helpers/tool/near.py", "helpers/tool/child.py", "helpers/other/far.py",
        })

    def test_a_copied_file_and_a_copied_directory_are_entries(self) -> None:
        self._file("helpers/rec/one.py")
        self._file("helpers/rec/two.py")
        self._file("helpers/whole/a.py")
        play = self._file("playbooks/imports/play-a.yml", """
            - copy:
                src: "{{ root_dir }}/helpers/rec/one.py"
                dest: /usr/local/lib/x/helpers/rec/two.py
            - copy:
                src: "{{ root_dir }}/helpers/whole/"
            # - copy: "{{ root_dir }}/helpers/rec/two.py"
        """)
        self.assertEqual(entries(self.root, {play}), {"helpers/rec/one.py", "helpers/whole/a.py"})

    def test_the_message_carries_the_rule_id(self) -> None:
        self.assertIn(f"[{RULE_ID}]", _message("p", ["x"]))
        self.assertIn(f"CLAUDE/QA.md#{RULE_ID}", _message("p", ["x"]))


class TestEveryPlayCountsTheHelpersItRuns(unittest.TestCase):
    """The rule over every host play of this checkout."""

    def test_no_host_play_counts_a_helper_it_cannot_run_or_drops_one_it_runs(self) -> None:
        plays = ap.candidate_plays(REPO_ROOT)
        self.assertGreater(len(plays), 30)
        for play in plays:
            with self.subTest(play=play):
                found = findings(REPO_ROOT, ap.play_inputs(REPO_ROOT, play))
                self.assertEqual(found, [], _message(play, found))


if __name__ == "__main__":
    unittest.main()
