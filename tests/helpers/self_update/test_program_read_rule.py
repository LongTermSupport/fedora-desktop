"""The rule play-program-read-dropped (Defence Before Fix; CLAUDE/QA.md).

`run.bash --changed` runs a play again when one of its inputs changed. play-input-dropped holds
the mapper (`helpers/self_update/affected_plays.play_inputs`) to every checkout reference in the
play text, but a play also runs programs from the checkout, and what those programs read is not
in the play text. Plan 00192 Task 3.7: `helpers/github_scopes/cli.py` reads
`vars/github-required-scopes.yml` through `__file__`, so `play-github-cli-multi.yml` never ran
again when the required scopes changed; `scripts/qa-toolchain.bash`, run by `play-python.yml`,
sources `scripts/lib/go-tool.bash` and builds and runs `src-go/qa/cmd/qa-toolchain`, which reads
`src-go/go.mod`, and none of them was an input.

The rule reads the programs' source, with its own patterns, not the mapper's. A finding is:

- in a helper package that is an input of a play (`helpers/<pkg>/`), a non-comment line with a
  path built from `__file__` and literal segments (`Path(__file__).parents[2] / "vars" / "x"`),
  or a quoted literal that names an existing checkout path outside the package (a path with a
  `/`, or a dot-file of the root such as `.qa-versions`), when the path is not an input. A
  literal `affected_plays.NOT_PLAY_INPUTS` lists for its module is not judged: it is named in a
  message, read again on every run, or relative to another directory. A bare climb to a directory (`--repo-root`'s default, a
  `sys.path` entry) is not judged: what a program reads under a root it is handed is decided
  as it runs, as play-input-dropped leaves the bare `{{ root_dir }}` alone. Any other
  `__file__` line is a finding unless an UNRESOLVED entry names it;
- in a bash file that is an exact input, a `source` / `.` of a path beside the script
  (`$SCRIPT_DIR/...`, `$(dirname "${BASH_SOURCE[0]}")/...`, `${BASH_SOURCE[0]%/*}/...`) that is
  not an input, or a `run_go_tool <name>` / `run_go_command <name>` (scripts/lib/go-tool.bash)
  whose command the inputs do not build;
- in a Go package of a command such a script builds and runs, a string literal that names an
  existing checkout path, in the same sense, that is not an input.

A source of a host path or of a path held in a runtime variable is not judged: it is not a
checkout file, or is not known before the program runs. A Go command a play installs reads the
checkout, if at all, when it runs later, live, so its literals are not judged.

Fix a finding in the mapper, so it counts the read (or reports it UNRESOLVED), or, for a literal
that is not a read, by listing it under its module in NOT_PLAY_INPUTS with the reason.
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

REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", ".."))
RULE_ID = "play-program-read-dropped"

_HELPER_PREFIX = re.compile(r"^helpers/([a-z_][a-z0-9_]*)/$")
_PY_LITERAL = re.compile(r"""(['"])([A-Za-z0-9_.@+\-/]+)\1""")
#: Text in quotes or backticks, where `__file__` is prose or a pattern, not the module's path.
_TEXT_SPAN = re.compile(r"""(["'`])(?:(?!\1).)*\1""")
_PARENTS = re.compile(r"\.parents\[(\d+)\]")
_PARENT = re.compile(r"\.parent\b")
_DIRNAME = re.compile(r"\bdirname\(")
_SHEBANG = re.compile(r"^#!\s*(?:/\S*/)?(?:env\s+)?(?:bash|sh)\b")
_BASH_SOURCE = re.compile(
    r"^\s*(?:source|\.)\s+\"?"
    r"(?:\$SCRIPT_DIR|\$\{SCRIPT_DIR\}|\$\(dirname \"\$\{BASH_SOURCE\[0\]\}\"\)|\$\{BASH_SOURCE\[0\]%/\*\})"
    r"/([^\"\s]+)"
)
_BASH_GO = re.compile(r"\brun_go_(tool|command)\s+([A-Za-z0-9_-]+)")
_GO_MODULE_OF = {"tool": "src-go/qa", "command": "src-go"}
_GO_LITERAL = re.compile(r'"((?:[^"\\\n]|\\.)*)"')


def _accounted(inputs: ap.Inputs, path: str) -> bool:
    if ap.affects(inputs, path) or ap.affects(inputs, path + "/"):
        return True
    return any(prefix.startswith(path + "/") for prefix in inputs.prefixes)


def _named(inputs: ap.Inputs, where: str) -> bool:
    return any(entry.startswith(where) for entry in inputs.unresolved)


def _checkout_path(repo_root: str, literal: str) -> str | None:
    """The repo-relative path a literal names, when it looks like one and exists: a path with a
    `/`, or a dot-file of the checkout root (`.qa-versions`). A lone dot-directory or `.git`
    (`<project>/.claude`, `<repo>/.git`) is a name under some other directory."""
    if literal.startswith("/") or literal == ".git":
        return None
    normal = posixpath.normpath(literal)
    if normal in (".", "..") or normal.startswith("../"):
        return None
    full = os.path.join(repo_root, normal)
    if "/" in literal:
        return normal if os.path.exists(full) else None
    return normal if literal.startswith(".") and os.path.isfile(full) else None


def _read(repo_root: str, rel: str) -> str:
    with open(os.path.join(repo_root, rel), encoding="utf-8") as handle:
        return handle.read()


def _file_path(rel: str, line: str) -> str | None:
    """The checkout path a `__file__` line builds, or None when the rule cannot read it."""
    climbs = _PARENTS.search(line)
    if climbs:
        levels = int(climbs.group(1)) + 1
    else:
        levels = len(_PARENT.findall(line)) or len(_DIRNAME.findall(line))
    if not levels:
        return None
    base = rel
    for _ in range(levels):
        base = posixpath.dirname(base)
    segments = [match.group(2) for match in _PY_LITERAL.finditer(line)]
    return posixpath.normpath(posixpath.join(base, *segments)) if base or segments else None


def _python_findings(repo_root: str, inputs: ap.Inputs, not_inputs: dict[str, dict[str, str]]) -> list[str]:
    findings = []
    for prefix in sorted(inputs.prefixes):
        package = _HELPER_PREFIX.match(prefix)
        if not package or not os.path.isdir(os.path.join(repo_root, prefix)):
            continue
        for dirpath, _dirs, files in os.walk(os.path.join(repo_root, prefix)):
            for name in sorted(files):
                if not name.endswith(".py"):
                    continue
                rel = os.path.relpath(os.path.join(dirpath, name), repo_root)
                text = _read(repo_root, rel)
                exempt = not_inputs.get(rel, {})
                for number, line in enumerate(text.splitlines(), start=1):
                    if line.lstrip().startswith("#"):
                        continue
                    where = f"{rel}:{number}:"
                    if "__file__" in _TEXT_SPAN.sub("", line):
                        if not _PY_LITERAL.search(line) and "/" not in line.split("__file__", 1)[1]:
                            continue
                        path = _file_path(rel, line)
                        if path is None or (not path.startswith(prefix) and not _accounted(inputs, path)):
                            if not _named(inputs, where):
                                findings.append(f"{where} {line.strip()}")
                        continue
                    for match in _PY_LITERAL.finditer(line):
                        literal = match.group(2)
                        path = _checkout_path(repo_root, literal)
                        if path is None or literal in exempt or (path + "/").startswith(prefix):
                            continue
                        if not _accounted(inputs, path):
                            findings.append(f"{where} {literal}")
    return findings


def _is_bash(repo_root: str, rel: str) -> bool:
    full = os.path.join(repo_root, rel)
    if not os.path.isfile(full):
        return False
    if rel.endswith((".bash", ".sh")):
        return True
    try:
        with open(full, encoding="utf-8") as handle:
            return bool(_SHEBANG.match(handle.readline()))
    except UnicodeDecodeError:
        return False


def _go_findings(repo_root: str, inputs: ap.Inputs, name: str, where: str) -> list[str]:
    findings = []
    for package in sorted(p for p, names in inputs.go_builders.items() if name in names):
        directory = os.path.join(repo_root, package)
        for file_name in sorted(os.listdir(directory)):
            if not file_name.endswith(".go") or file_name.endswith("_test.go"):
                continue
            rel = posixpath.join(package, file_name)
            for match in _GO_LITERAL.finditer(_read(repo_root, rel)):
                path = _checkout_path(repo_root, match.group(1))
                if path is not None and not _accounted(inputs, path):
                    findings.append(f"{where} {name} reads {path} ({rel})")
    return findings


def _bash_findings(repo_root: str, inputs: ap.Inputs) -> list[str]:
    findings = []
    for rel in sorted(path for path in inputs.exact if _is_bash(repo_root, path)):
        for number, line in enumerate(_read(repo_root, rel).splitlines(), start=1):
            if line.lstrip().startswith("#"):
                continue
            where = f"{rel}:{number}:"
            sourced = _BASH_SOURCE.match(line)
            if sourced:
                path = posixpath.normpath(posixpath.join(posixpath.dirname(rel), sourced.group(1)))
                if not _accounted(inputs, path) and not _named(inputs, where):
                    findings.append(f"{where} {line.strip()}")
            for kind, name in _BASH_GO.findall(line):
                command = posixpath.join(_GO_MODULE_OF[kind], "cmd", name)
                if command not in inputs.go_commands:
                    if not _named(inputs, where):
                        findings.append(f"{where} {line.strip()}")
                    continue
                findings.extend(_go_findings(repo_root, inputs, name, where))
    return findings


def dropped_reads(
    repo_root: str, inputs: ap.Inputs, not_inputs: dict[str, dict[str, str]] | None = None
) -> list[str]:
    """Every checkout read by a program the play runs that `inputs` does not count."""
    declared = ap.NOT_PLAY_INPUTS if not_inputs is None else not_inputs
    return _python_findings(repo_root, inputs, declared) + _bash_findings(repo_root, inputs)


def _message(play: str, findings: list[str]) -> str:
    return (
        f"[{RULE_ID}] {play} runs programs that read checkout paths the mapper does not count;"
        f" see CLAUDE/QA.md#{RULE_ID}:\n  " + "\n  ".join(findings)
    )


class TestRuleFires(unittest.TestCase):
    """The rule proved against fixtures, with stand-in inputs, before it judges the repo."""

    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self._tmp.cleanup)
        self.root = self._tmp.name

    def _file(self, rel: str, text: str = "x\n") -> str:
        path = os.path.join(self.root, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as handle:
            handle.write(textwrap.dedent(text))
        return rel

    def _scopes_helper(self) -> None:
        """The originating shape: helpers/github_scopes/cli.py."""
        self._file("vars/required.yml")
        self._file("helpers/tool/cli.py", """
            import pathlib

            DEFAULT = pathlib.Path(__file__).resolve().parents[2] / "vars" / "required.yml"
        """)

    def test_a_file_path_to_a_checkout_file_the_inputs_lack_is_a_finding(self) -> None:
        self._scopes_helper()
        found = dropped_reads(self.root, ap.Inputs(prefixes={"helpers/tool/"}))
        self.assertEqual(len(found), 1)
        self.assertIn("helpers/tool/cli.py:4:", found[0])

    def test_a_file_path_that_is_an_input_is_not_a_finding(self) -> None:
        self._scopes_helper()
        inputs = ap.Inputs(prefixes={"helpers/tool/"}, exact={"vars/required.yml"})
        self.assertEqual(dropped_reads(self.root, inputs), [])

    def test_a_package_no_play_reaches_is_not_judged(self) -> None:
        self._scopes_helper()
        self.assertEqual(dropped_reads(self.root, ap.Inputs(prefixes={"helpers/other/"})), [])

    def test_bare_climbs_and_paths_inside_the_package_are_not_judged(self) -> None:
        self._file("helpers/tool/data.json")
        self._file("helpers/tool/cli.py", """
            import os
            import pathlib

            ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))
            TOP = pathlib.Path(__file__).resolve().parents[2]
            DATA = pathlib.Path(__file__).parent / "data.json"
        """)
        self.assertEqual(dropped_reads(self.root, ap.Inputs(prefixes={"helpers/tool/"})), [])

    def test_file_in_prose_or_a_pattern_is_not_judged(self) -> None:
        self._file("helpers/tool/cli.py", """
            '''A path built from `__file__` and "vars"/"x".'''
            PATTERN = r"Path\\(__file__\\)\\.parent / 'vars'"
        """)
        self.assertEqual(dropped_reads(self.root, ap.Inputs(prefixes={"helpers/tool/"})), [])

    def test_a_file_use_the_rule_cannot_read_is_a_finding_unless_reported(self) -> None:
        self._file("helpers/tool/cli.py", 'X = str(__file__) + "/../../vars"\n')
        self.assertEqual(len(dropped_reads(self.root, ap.Inputs(prefixes={"helpers/tool/"}))), 1)
        inputs = ap.Inputs(prefixes={"helpers/tool/"}, unresolved=["helpers/tool/cli.py:1: __file__"])
        self.assertEqual(dropped_reads(self.root, inputs), [])

    def test_a_literal_checkout_path_outside_the_package_is_a_finding(self) -> None:
        self._file("files/share/pin.txt")
        self._file("helpers/tool/cli.py", 'PIN = "files/share/pin.txt"\n# old: "files/share/pin.txt"\n')
        found = dropped_reads(self.root, ap.Inputs(prefixes={"helpers/tool/"}))
        self.assertEqual(found, ["helpers/tool/cli.py:1: files/share/pin.txt"])

    def test_literals_that_are_inputs_absent_or_declared_are_not_findings(self) -> None:
        self._file("files/share/pin.txt")
        self._file("docs/x.md")
        self._file("helpers/tool/cli.py", """
            PIN = "files/share/pin.txt"
            GONE = "files/never/there"
            HINT = "docs/x.md"
        """)
        inputs = ap.Inputs(prefixes={"helpers/tool/"}, exact={"files/share/pin.txt"})
        self.assertEqual(len(dropped_reads(self.root, inputs, {})), 1)
        declared = {"helpers/tool/cli.py": {"docs/x.md": "named in a message"}}
        self.assertEqual(dropped_reads(self.root, inputs, declared), [])

    def test_a_root_dot_file_is_a_path_and_a_lone_dot_directory_is_not(self) -> None:
        self._file(".pins")
        self._file(".claude/settings.json")
        self._file(".git")
        self._file("helpers/tool/cli.py", 'A = ".pins"\nB = ".claude"\nC = ".git"\n')
        found = dropped_reads(self.root, ap.Inputs(prefixes={"helpers/tool/"}))
        self.assertEqual(found, ["helpers/tool/cli.py:1: .pins"])

    def test_a_sourced_sibling_the_inputs_lack_is_a_finding(self) -> None:
        self._file("scripts/lib/lib.bash")
        script = self._file("scripts/run.bash", """
            #!/usr/bin/env bash
            source "$SCRIPT_DIR/lib/lib.bash"
            source /usr/local/lib/host.bash
            source "$CONF"
        """)
        found = dropped_reads(self.root, ap.Inputs(exact={script}))
        self.assertEqual(len(found), 1)
        self.assertIn("scripts/run.bash:3:", found[0])
        self.assertEqual(dropped_reads(self.root, ap.Inputs(exact={script, "scripts/lib/lib.bash"})), [])

    def test_a_go_tool_the_inputs_do_not_build_is_a_finding(self) -> None:
        """The qa-toolchain shape: the script builds src-go/qa/cmd/<name> and runs it."""
        script = self._file("scripts/qa-x.bash", "#!/usr/bin/env bash\nrun_go_tool qa-x --scripts here\n")
        found = dropped_reads(self.root, ap.Inputs(exact={script}))
        self.assertEqual(len(found), 1)
        self.assertIn("scripts/qa-x.bash:2:", found[0])

    def test_a_checkout_file_the_go_tool_names_must_be_an_input(self) -> None:
        self._file("src-go/go.mod", "module m\n")
        self._file("src-go/qa/x/pins.go", 'package x\n\nvar files = []string{"src-go/go.mod", "no/such"}\n')
        script = self._file("scripts/qa-x.bash", "#!/usr/bin/env bash\nrun_go_tool qa-x\n")
        built = {
            "exact": {script},
            "go_commands": {"src-go/qa/cmd/qa-x"},
            "go_packages": {"src-go/qa/x"},
            "go_builders": {"src-go/qa/x": {"qa-x"}},
        }
        found = dropped_reads(self.root, ap.Inputs(**built))
        self.assertEqual(found, ["scripts/qa-x.bash:2: qa-x reads src-go/go.mod (src-go/qa/x/pins.go)"])
        built["exact"] = {script, "src-go/go.mod"}
        self.assertEqual(dropped_reads(self.root, ap.Inputs(**built)), [])

    def test_the_message_carries_the_rule_id(self) -> None:
        self.assertIn(f"[{RULE_ID}]", _message("p", ["f:1: x"]))
        self.assertIn(f"CLAUDE/QA.md#{RULE_ID}", _message("p", ["f:1: x"]))


class TestNoPlayDropsAProgramRead(unittest.TestCase):
    """The rule over every host play of this checkout."""

    def test_no_host_play_drops_a_read_of_a_program_it_runs(self) -> None:
        plays = ap.candidate_plays(REPO_ROOT)
        self.assertGreater(len(plays), 30)
        for play in plays:
            with self.subTest(play=play):
                findings = dropped_reads(REPO_ROOT, ap.play_inputs(REPO_ROOT, play))
                self.assertEqual(findings, [], _message(play, findings))

    def test_every_declared_non_input_is_still_named_by_its_module(self) -> None:
        """An entry outlives its literal silently otherwise, and would excuse a later read."""
        for module, literals in ap.NOT_PLAY_INPUTS.items():
            text = _read(REPO_ROOT, module)
            for literal, why in literals.items():
                with self.subTest(module=module, literal=literal):
                    self.assertTrue(why.strip())
                    self.assertRegex(text, rf"""["']{re.escape(literal)}["']""")


if __name__ == "__main__":
    unittest.main()
