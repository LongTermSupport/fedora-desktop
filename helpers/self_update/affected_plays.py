"""Which plays a set of changed paths touches, split by the unattended allowlist (Plan 00137).

A play's own file is not all it deploys: a commit that changes the ccy lib or the `cc`
launcher changes what `play-claude-yolo.yml` puts on the host. This maps changed paths to
the plays that deploy them, from the play text itself. `changed_plays.judge`, the one
judgement of which plays have to run again (for `run.bash --changed` and the health
report), reads its inputs from here.

**No YAML parser.** Helpers are stdlib-only (helpers/CLAUDE.md), so references are read
with regexes over the play text. The idioms recognised are the ones this repo uses:

- `{{ root_dir }}/<path>` anywhere on a non-comment line, and the same with root_dir's own
  definition, `{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}`, and the
  Jinja joins `root_dir ~ '/<path>'` and `root_dir + '/<path>'`. A literal path is an exact
  input, or a directory input when it ends in `/` or is a directory in the checkout. A
  rooted path whose tail is one template and a literal suffix is the paths the play text
  fills it in with (Plan 00229, Task 1.5): `{{ item }}` or `{{ item.<key> }}` under the
  task's `loop:` / `with_items:` of literals, given inline or as a list variable a walked
  file sets, and `{{ var }}` a walked file sets to a literal. Any other template or glob in
  the tail (a loop built by a filter, a renamed loop_var, `.../*.bash`, a join that goes on
  `~ var`) is an input on its literal prefix: conservative, because a regex cannot
  evaluate it.
- A reference to a Go module root (a directory holding `go.mod`, src-go) is a Go build:
  `tasks/build-go-command.yml` builds `<module>/cmd/<go_command_name>`. The play's inputs
  are then every package that command imports, transitively, within the module and from
  its vendor tree (each package's own files, test files aside, plus any directory it
  embeds), and the module's go.mod, go.sum and vendor/modules.txt. A `go_command_name`
  written `{{ var }}` is resolved through the `var:` the walked files set. A command
  built from a pinned external module (`go_command_module`) brings nothing from src-go.
- The files those references name are followed transitively when they are YAML (task
  files, vars files, imported plays). So an `include_tasks` of a task file makes that
  file's own `src:` paths inputs of the play.
- `import_playbook: <relative>`, relative to the importing file.
- `helpers.<pkg>` where `helpers/<pkg>/` exists (`python3 -m helpers.<pkg>.x` in an
  argv): the whole package, plus every `helpers.<other>` package its modules import.
- What the programs a play runs read from the checkout (rule play-program-read-dropped,
  `tests/helpers/self_update/test_program_read_rule.py`). In a helper package: a path built
  from `__file__` and literal segments (`Path(__file__).parents[2] / "vars" / "x.yml"`, or
  `os.path.join` of a `dirname` climb), and a string literal naming an existing checkout path
  outside the package (a path with a `/`, or a root dot-file), unless `NOT_PLAY_INPUTS` lists it
  for that module. A bare climb (a `--repo-root` default) is not a read. In a bash file that
  is an input: the file it sources beside itself, and the Go command it builds and runs from
  the checkout (`run_go_tool` / `run_go_command` of scripts/lib/go-tool.bash), whose packages
  are inputs and whose non-test sources' literals naming checkout paths are too. A path read
  this way is an input as content: a YAML file read is not walked as a play.

**A reference it cannot follow is reported, never dropped.** A `src:`, `file:`,
`include_*` or `vars_files` value that is not rooted, not a host path or URL, not a loop
`item`, not a variable the file defines as rooted or as a host path, and not a registered
result, is UNRESOLVED. So is a join with no literal path (`root_dir ~ '/' ~ var`), a Go
module built with no `go_command_name`, a name no walked file sets, a command the module
does not have, and a `__file__` form, a sourced file or a run Go command a program names
that cannot be read. The rule play-input-dropped (CLAUDE/QA.md,
`tests/helpers/self_update/test_dropped_input_rule.py`) holds every host play to this for
the play text, and play-program-read-dropped for the programs it runs. A missed dependency is a play that silently never re-runs, which
is the defect this helper exists to remove.

Run it:
  python3 -m helpers.self_update.affected_plays --old SHA --new SHA
  python3 -m helpers.self_update.affected_plays --changed PATH [PATH ...]

stdout carries only marker lines: `RUN <play>`, `SKIP-NOT-ALLOWED <play>`,
`UNRESOLVED <play> <where>: <value>`. Exit: 0 answered; 1 answered, but an allowlisted
play has a reference that could not be followed; 2 no answer (bad allowlist, git failed).
"""

from __future__ import annotations

import argparse
import json
import os
import posixpath
import re
import subprocess
import sys
from dataclasses import dataclass, field
from typing import TextIO

#: Repo-relative, next to the only code that reads it, as helpers/play_ledger does with
#: retired-plays.json: vars/ is Ansible's YAML, which this stdlib-only helper cannot parse.
ALLOWLIST_PATH = "helpers/self_update/unattended-plays.json"
CANDIDATE_DIR = "playbooks/imports"
#: Per helper module, the checkout-looking literals it names that no play running it depends
#: on, each with why. Kept in data beside this module, not in the modules (some are baked into
#: the ccy image, where an edit costs a version bump) nor in this file (whose literals it scans).
NOT_PLAY_INPUTS_FILE = os.path.join(os.path.dirname(os.path.realpath(__file__)), "not-play-inputs.json")


def _load_not_inputs(path: str) -> dict[str, dict[str, str]]:
    with open(path, encoding="utf-8") as handle:
        data = json.load(handle)
    if not isinstance(data, dict) or not all(
        isinstance(literals, dict) and all(isinstance(why, str) and why for why in literals.values())
        for literals in data.values()
    ):
        raise ValueError(f"{path} must map each module to {{literal: why}}")
    return data


NOT_PLAY_INPUTS = _load_not_inputs(NOT_PLAY_INPUTS_FILE)

EXIT_OK = 0
EXIT_UNRESOLVED = 1
EXIT_ERROR = 2

_ROOT_EXPR = (
    r"(?:\{\{\s*root_dir\s*\}\}"
    r"|\{\{\s*lookup\(\s*'ansible\.builtin\.config'\s*,\s*'CONFIG_FILE'\s*\)\s*\|\s*dirname\s*\}\})"
)
_ROOTED = re.compile(_ROOT_EXPR + r"/([A-Za-z0-9_.@+\-/]*)(\{\{)?")
_ROOTED_AT_START = re.compile(_ROOT_EXPR + "/")
_JOINED = re.compile(r"\broot_dir\s*[~+]\s*(['\"])/([^'\"]*)\1(\s*[~+])?")
_GLOB = re.compile(r"[*?\[]")
_GO_COMMAND_NAME = re.compile(r"^\s*(?:-\s+)?go_command_name\s*:\s*(.+)$")
_GO_COMMAND_MODULE = re.compile(r"^\s*(?:-\s+)?go_command_module\s*:\s*\S")
_GO_MODULE_LINE = re.compile(r"^module\s+(\S+)", re.MULTILINE)
_GO_IMPORT_BLOCK = re.compile(r"^import\s*\((.*?)^\)", re.MULTILINE | re.DOTALL)
_GO_IMPORT_ONE = re.compile(r'^import\s+(?:[\w.]+\s+)?"([^"]+)"', re.MULTILINE)
_GO_QUOTED = re.compile(r'"([^"]+)"')
_GO_EMBED = re.compile(r"^\s*//go:embed\s+(.+)$", re.MULTILINE)
_LONE_TEMPLATE = re.compile(r"^\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}$")
_KEY = re.compile(
    r"^(\s*)(?:-\s+)?(?:ansible\.builtin\.)?"
    r"(src|file|include_tasks|import_tasks|include_vars|import_playbook|vars_files)\s*:\s*(.*)$"
)
_LIST_ITEM = re.compile(r"^(\s*)-\s+(.*)$")
_HELPER_TOKEN = re.compile(r"\bhelpers\.([a-z_][a-z0-9_]*)")
_PY_IMPORT = re.compile(r"^\s*(?:from|import)\s+helpers\.([a-z_][a-z0-9_]*)", re.MULTILINE)
_PY_FROM_HELPERS = re.compile(r"^\s*from\s+helpers\s+import\s+\(?([^)\n]+)", re.MULTILINE)
_TEMPLATE_HEAD = re.compile(r"^\{\{\s*([A-Za-z_][A-Za-z0-9_]*)")
_URL = re.compile(r"^[a-z][a-z0-9+.-]*://")
_REGISTER = re.compile(r"^\s*register:\s*([A-Za-z_][A-Za-z0-9_]*)\s*$", re.MULTILINE)
_PY_PATH_LITERAL = re.compile(r"""(['"])([A-Za-z0-9_.@+\-/]+)\1""")
_PATH_CLIMB = re.compile(
    r"Path\(__file__\)(?:\.resolve\(\)|\.absolute\(\))?(?:((?:\.parent\b)+)|\.parents\[(\d+)\])"
    r"((?:\s*/\s*['\"][^'\"]+['\"])*)"
)
_OS_CLIMB = re.compile(
    r"((?:os\.path\.dirname\(\s*)+)os\.path\.(?:realpath|abspath)\(__file__\)[\s)]*"
    r"((?:,\s*['\"][^'\"]+['\"]\s*)*)"
)
_QUOTED = re.compile(r"['\"]([^'\"]+)['\"]")
#: A quoted or backticked span: `__file__` there is text (a docstring, a pattern), not code.
_QUOTED_SPAN = re.compile(r"(['\"`]).*?\1")
_SHEBANG = re.compile(r"^#!\s*(?:/\S*/)?(?:env\s+)?(?:bash|sh)\b")
_BASH_SOURCE = re.compile(
    r"^\s*(?:source|\.)\s+\"?"
    r"(?:\$SCRIPT_DIR|\$\{SCRIPT_DIR\}|\$\(dirname \"\$\{BASH_SOURCE\[0\]\}\"\)|\$\{BASH_SOURCE\[0\]%/\*\})"
    r"/([^\"\s]+)"
)
#: The functions of scripts/lib/go-tool.bash and the module each builds `cmd/<name>` of.
_GO_RUNNERS = {"run_go_tool": "src-go/qa", "run_go_command": "src-go"}
_BASH_GO_RUN = re.compile(r"\b(" + "|".join(_GO_RUNNERS) + r")\s+([A-Za-z0-9_-]+)")
_GO_STRING = re.compile(r'"((?:[^"\\\n]|\\.)*)"')
#: A templated tail: one template, then the literal rest of the path, then no more template.
_TAIL = re.compile(
    r"\{\{\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\s*\}\}([A-Za-z0-9_.@+\-/]*)(?!\{)"
)
_PATH_VALUE = re.compile(r"^[A-Za-z0-9_.@+\-/]+$")
_LIST_VAR = re.compile(r"^\{\{\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\}\}$")
_MAPPING_LINE = re.compile(r"^([A-Za-z_][A-Za-z0-9_-]*)\s*:\s*(.*)$")
_DASH = re.compile(r"^(\s*)-(?:\s+(.*))?$")


def _indent(line: str) -> int:
    return len(line) - len(line.lstrip(" "))


def _skip(line: str) -> bool:
    stripped = line.strip()
    return not stripped or stripped.startswith("#")


def _literal(value: str) -> str | None:
    """A plain path-shaped scalar, unquoted, or None for anything else."""
    value = _unquote(value)
    return value if _PATH_VALUE.match(value) else None


#: A list item: a literal, or a mapping of keys to literals (None where a value is not one).
#: None for the whole list is a list that cannot be read.
Item = str | dict[str, str | None] | None


def _flow_items(value: str) -> list[Item] | None:
    value = value.strip()
    if not (value.startswith("[") and value.endswith("]")):
        return None
    items: list[Item] = [_literal(part) for part in value[1:-1].split(",") if part.strip()]
    return None if None in items else items


def _block_items(lines: list[str], start: int, parent: int) -> list[Item] | None:
    """The block list starting at `lines[start]`, under a key indented `parent`."""
    items: list[Item] = []
    dash = None
    for line in lines[start:]:
        if _skip(line):
            continue
        indent = _indent(line)
        if indent <= parent:
            break
        item = _DASH.match(line)
        if dash is None or (item and indent == dash):
            if item is None or (dash is not None and indent != dash):
                return None
            dash = indent
            content = (item.group(2) or "").split(" #", 1)[0].strip()
            keyed = _MAPPING_LINE.match(content)
            if keyed and not content.startswith(("'", '"')):
                items.append({keyed.group(1): _literal(keyed.group(2))})
            else:
                items.append(_literal(content))
            continue
        current = items[-1]
        if indent < dash + 2 or not isinstance(current, dict):
            return None
        if indent > dash + 2:
            continue
        keyed = _MAPPING_LINE.match(line.strip())
        if keyed is None:
            return None
        current[keyed.group(1)] = _literal(keyed.group(2))
    return items if dash is not None else None


def _list_in(lines: list[str], path: list[str], start: int = 0, parent: int = -1) -> list[list[Item] | None]:
    """Every list the dotted `path` names in `lines`, each key inside the block of the last."""
    found: list[list[Item] | None] = []
    key = re.compile(rf"^(\s*)(?:-\s+)?{re.escape(path[0])}\s*:\s*(.*)$")
    for index in range(start, len(lines)):
        line = lines[index]
        if _skip(line):
            continue
        if _indent(line) <= parent:
            break
        match = key.match(line)
        if match is None:
            continue
        indent, value = len(match.group(1)), match.group(2).split(" #", 1)[0].strip()
        if len(path) > 1:
            found.extend(_list_in(lines, path[1:], index + 1, indent))
        elif not value:
            found.append(_block_items(lines, index + 1, indent))
        else:
            found.append(_flow_items(value))
    return found


@dataclass
class Inputs:
    """What a play deploys from the checkout: exact paths, path prefixes, the Go package
    directories it builds from, and the references that could not be followed.

    A Go package directory counts its own files, not its subdirectories (each is a package
    of its own) nor its `_test.go` files. `go_modules` are the Go module roots the play
    builds from and `go_commands` the command directories it builds. `go_builders` names,
    for each package, the commands built from it, which is how a reason names a change.
    `filled` names, for each `<file>:<line>` holding a templated rooted reference the play
    text fills in, the paths it was filled in with (rule play-input-dropped reads it).
    """

    exact: set[str] = field(default_factory=set)
    prefixes: set[str] = field(default_factory=set)
    unresolved: list[str] = field(default_factory=list)
    go_packages: set[str] = field(default_factory=set)
    go_modules: set[str] = field(default_factory=set)
    go_commands: set[str] = field(default_factory=set)
    go_builders: dict[str, set[str]] = field(default_factory=dict)
    filled: dict[str, list[str]] = field(default_factory=dict)


@dataclass
class Report:
    run: list[str]
    skipped: list[str]
    unresolved: list[tuple[str, str]]


def affects(inputs: Inputs, path: str) -> bool:
    if path in inputs.exact or any(path.startswith(prefix) for prefix in inputs.prefixes):
        return True
    return _go_package_of(inputs, path) is not None


def _go_package_of(inputs: Inputs, path: str) -> str | None:
    """The Go package the play builds that `path` is a file of, or None."""
    package = posixpath.dirname(path)
    if package in inputs.go_packages and not path.endswith("_test.go"):
        return package
    return None


def go_changes(inputs: Inputs, paths: list[str]) -> dict[str, set[str]]:
    """The packages among `paths` the play builds, in the order first seen, each with the
    names of the commands built from it."""
    changes: dict[str, set[str]] = {}
    for path in paths:
        package = _go_package_of(inputs, path)
        if package is not None and package not in changes:
            changes[package] = set(inputs.go_builders.get(package, ()))
    return changes


def _non_comment_lines(text: str) -> list[tuple[int, str]]:
    return [(n, line) for n, line in enumerate(text.splitlines(), 1) if not line.lstrip().startswith("#")]


def _unquote(value: str) -> str:
    value = value.strip()
    if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
        return value[1:-1]
    return value.split(" #", 1)[0].strip()


def _safe_relpath(candidate: str) -> str | None:
    """A normalised repo-relative path, or None if it would leave the checkout."""
    if not candidate or candidate.startswith("/"):
        return None
    normal = posixpath.normpath(candidate)
    if normal == ".." or normal.startswith("../"):
        return None
    return normal


def _defined_vars(text: str) -> set[str]:
    """Names the file defines as a rooted path, a host path or a URL: their values are
    already inputs through the rooted scan, or are not checkout files at all."""
    names = set()
    # [ \t], not \s: under MULTILINE \s crosses the newline, and `vars:` would swallow the
    # line below it as its value.
    for match in re.finditer(r"^[ \t]*([A-Za-z_][A-Za-z0-9_]*)[ \t]*:[ \t]*[\"']?(.*)$", text, re.MULTILINE):
        value = match.group(2)
        if _ROOTED_AT_START.match(value) or value.startswith("/") or _URL.match(value):
            names.add(match.group(1))
    return names


class _Walker:
    def __init__(self, repo_root: str) -> None:
        self.root = repo_root
        self.inputs = Inputs()
        self._seen_files: set[str] = set()
        self._seen_packages: set[str] = set()
        self._texts: dict[str, str] = {}
        self._module_refs: list[tuple[str, str]] = []
        self._go_names: list[tuple[str, str]] = []
        self._external_go_files: set[str] = set()
        self._go_import_cache: dict[str, set[str]] = {}
        #: Templated rooted references, resolved once every file is walked: (file, line, literal, tail, shown).
        self._templated: list[tuple[str, int, str, str, str]] = []

    def _is_dir(self, rel: str) -> bool:
        return os.path.isdir(os.path.join(self.root, rel))

    def _is_file(self, rel: str) -> bool:
        return os.path.isfile(os.path.join(self.root, rel))

    def _add_path(self, rel: str, *, as_dir: bool) -> None:
        if as_dir or self._is_dir(rel):
            self.inputs.prefixes.add(rel.rstrip("/") + "/")
        else:
            self.inputs.exact.add(rel)
            if rel.endswith((".yml", ".yaml")) and self._is_file(rel):
                self.walk_yaml(rel, context="")
            elif self._is_bash(rel):
                self._walk_bash(rel)
        self._maybe_helper_path(rel)

    def _add_read(self, rel: str) -> None:
        """A checkout path a program reads: its content is the input, so a YAML file is not
        walked (a helper that reads a play does not deploy what the play deploys)."""
        if self._is_dir(rel):
            self.inputs.prefixes.add(rel.rstrip("/") + "/")
        elif rel not in self.inputs.exact:
            self.inputs.exact.add(rel)
            if self._is_bash(rel):
                self._walk_bash(rel)
        self._maybe_helper_path(rel)

    def _checkout_literal(self, literal: str) -> str | None:
        """The checkout path a string literal in a program names, if it names one: an existing
        path with a `/`, or a dot-file of the root (`.qa-versions`). A lone dot-directory or
        `.git` is a name under some other directory (`<project>/.claude`)."""
        if literal == ".git":
            return None
        rel = _safe_relpath(literal)
        if rel is None or rel == ".":
            return None
        if "/" in literal:
            return rel if self._is_file(rel) or self._is_dir(rel) else None
        return rel if literal.startswith(".") and self._is_file(rel) else None

    def _is_bash(self, rel: str) -> bool:
        if not self._is_file(rel):
            return False
        if rel.endswith((".bash", ".sh")):
            return True
        try:
            with open(os.path.join(self.root, rel), encoding="utf-8") as handle:
                return bool(_SHEBANG.match(handle.readline()))
        except UnicodeDecodeError:
            return False

    def _walk_bash(self, rel_file: str) -> None:
        """A bash file's library beside it, and the Go command it builds and runs from the
        checkout (scripts/lib/go-tool.bash) with the checkout files that command names."""
        with open(os.path.join(self.root, rel_file), encoding="utf-8") as handle:
            text = handle.read()
        for line_no, line in _non_comment_lines(text):
            sourced = _BASH_SOURCE.match(line)
            if sourced:
                rel = _safe_relpath(posixpath.join(posixpath.dirname(rel_file), sourced.group(1)))
                if rel is not None and self._is_file(rel):
                    self._add_read(rel)
                else:
                    self.inputs.unresolved.append(f"{rel_file}:{line_no}: {line.strip()}")
            for runner, name in _BASH_GO_RUN.findall(line):
                self._script_go_command(f"{rel_file}:{line_no}", _GO_RUNNERS[runner], name)

    def _script_go_command(self, where: str, module: str, name: str) -> None:
        """A Go command a script builds and runs as the play runs: its packages, and the
        checkout files its non-test sources name, are inputs."""
        command = posixpath.join(module, "cmd", name)
        if command in self.inputs.go_commands:
            return
        if not self._is_go_module(module) or not self._is_dir(command):
            self.inputs.unresolved.append(f"{where}: runs Go command {name}, but there is no {command}")
            return
        self.inputs.go_modules.add(module)
        self.inputs.go_commands.add(command)
        self._go_dependencies(module, command)
        for package, builders in sorted(self.inputs.go_builders.items()):
            if name not in builders:
                continue
            directory = os.path.join(self.root, package)
            for file_name in sorted(os.listdir(directory)):
                if not file_name.endswith(".go") or file_name.endswith("_test.go"):
                    continue
                with open(os.path.join(directory, file_name), encoding="utf-8") as handle:
                    source = handle.read()
                for literal in _GO_STRING.findall(source):
                    rel = self._checkout_literal(literal)
                    if rel is not None:
                        self._add_read(rel)

    def _python_reads(self, rel_file: str, source: str, package_dir: str) -> None:
        """The checkout paths a helper module names outside its package: a path built from
        `__file__` and literal segments, or a literal path. A bare climb from `__file__` (a
        `--repo-root` default) is the root a program is handed, not a read; a `__file__` form
        not recognised is UNRESOLVED; a literal NOT_PLAY_INPUTS lists for the module is not a read
        a play depends on."""
        exempt = NOT_PLAY_INPUTS.get(rel_file, {})
        for line_no, line in _non_comment_lines(source):
            if "__file__" in _QUOTED_SPAN.sub("", line):
                self._file_read(f"{rel_file}:{line_no}", rel_file, line, package_dir)
                continue
            for _quote, literal in _PY_PATH_LITERAL.findall(line):
                rel = self._checkout_literal(literal)
                if rel is not None and literal not in exempt and not (rel + "/").startswith(package_dir):
                    self._add_read(rel)

    def _file_read(self, where: str, rel_file: str, line: str, package_dir: str) -> None:
        climbed = _PATH_CLIMB.search(line)
        if climbed:
            levels = len(climbed.group(1).split(".parent")) - 1 if climbed.group(1) else int(climbed.group(2)) + 1
            segments = _QUOTED.findall(climbed.group(3))
        else:
            climbed = _OS_CLIMB.search(line)
            if climbed is None:
                self.inputs.unresolved.append(f"{where}: {line.strip()}")
                return
            levels = climbed.group(1).count("dirname(")
            segments = _QUOTED.findall(climbed.group(2))
        if not segments:
            return
        base = rel_file
        for _ in range(levels):
            base = posixpath.dirname(base)
        rel = _safe_relpath(posixpath.join(base, *segments))
        if rel is not None and (rel + "/").startswith(package_dir):
            return
        if rel is None or not (self._is_file(rel) or self._is_dir(rel)):
            self.inputs.unresolved.append(f"{where}: {line.strip()}")
        else:
            self._add_read(rel)

    def _maybe_helper_path(self, rel: str) -> None:
        parts = rel.split("/")
        if len(parts) >= 2 and parts[0] == "helpers" and parts[1]:
            self._add_package(parts[1])

    def _add_package(self, package: str) -> None:
        if package in self._seen_packages or not self._is_dir(f"helpers/{package}"):
            return
        self._seen_packages.add(package)
        self.inputs.prefixes.add(f"helpers/{package}/")
        for dirpath, _dirs, files in os.walk(os.path.join(self.root, "helpers", package)):
            for name in sorted(files):
                if not name.endswith(".py"):
                    continue
                path = os.path.join(dirpath, name)
                with open(path, encoding="utf-8") as handle:
                    source = handle.read()
                self._python_reads(os.path.relpath(path, self.root), source, f"helpers/{package}/")
                for imported in _PY_IMPORT.findall(source):
                    self._add_package(imported)
                for group in _PY_FROM_HELPERS.findall(source):
                    for imported in re.findall(r"[a-z_][a-z0-9_]*", group):
                        self._add_package(imported)

    def _rooted(self, rel_file: str, line_no: int, line: str) -> None:
        where = f"{rel_file}:{line_no}"
        for match in _ROOTED.finditer(line):
            if match.group(2):
                self._templated.append((rel_file, line_no, match.group(1), line[match.start(2):], match.group(0)))
                continue
            self._reference(where, match.group(1), False, match.group(0))
        for match in _JOINED.finditer(line):
            self._reference(where, match.group(2), bool(match.group(3)), match.group(0))

    def _reference(self, where: str, literal: str, templated: bool, shown: str) -> None:
        """One checkout reference: `literal` is its path, `templated` when more follows it."""
        glob = _GLOB.search(literal)
        if glob:
            literal, templated = literal[: glob.start()], True
        if templated:
            prefix = _safe_relpath(literal.rstrip("/")) if literal.rstrip("/") else None
            if prefix is None:
                self.inputs.unresolved.append(f"{where}: {shown}")
            elif self._is_go_module(prefix):
                self._module_refs.append((where, prefix))
            else:
                self.inputs.prefixes.add(literal if literal.endswith("/") else prefix)
                self._maybe_helper_path(prefix)
            return
        rel = _safe_relpath(literal)
        if rel is None or rel == ".":
            self.inputs.unresolved.append(f"{where}: {shown}")
        elif self._is_go_module(rel):
            self._module_refs.append((where, rel))
        else:
            self._add_path(rel, as_dir=literal.endswith("/"))

    def finish_templates(self) -> None:
        """Each templated rooted reference: the paths the play text fills it in with, or its
        literal prefix when it cannot say. A resolved path may walk a file with more."""
        while self._templated:
            rel_file, line_no, literal, tail, shown = self._templated.pop(0)
            paths = self._filled_in(rel_file, line_no, literal, tail)
            if paths is None:
                self._reference(f"{rel_file}:{line_no}", literal, True, shown)
                continue
            filled = self.inputs.filled.setdefault(f"{rel_file}:{line_no}", [])
            for rel, as_dir in paths:
                filled.append(rel)
                self._add_path(rel, as_dir=as_dir)

    def _filled_in(self, rel_file: str, line_no: int, literal: str, tail: str) -> list[tuple[str, bool]] | None:
        """The checkout paths `literal` + `tail` stands for, each with whether it is a
        directory, or None when the template is not one the play text fills with literals."""
        tail_match = _TAIL.match(tail)
        prefix = _safe_relpath(literal.rstrip("/")) if literal.rstrip("/") else None
        if tail_match is None or prefix is None or self._is_go_module(prefix):
            return None
        name, suffix = tail_match.group(1), tail_match.group(2)
        if name == "item" or name.startswith("item."):
            items = self._loop_items(rel_file, line_no)
            key = name.partition(".")[2]
            if items is None:
                values = None
            elif key:
                values = [item.get(key) if isinstance(item, dict) else None for item in items]
            else:
                values = items
        elif "." in name:
            return None
        else:
            values = self._scalar_values(name)
        if not values or any(not isinstance(value, str) for value in values):
            return None
        paths = []
        for value in values:
            rel = _safe_relpath(literal + value + suffix)
            if rel is None or not (rel + "/").startswith(prefix + "/"):
                return None
            paths.append((rel, suffix.endswith("/")))
        return paths

    def _loop_items(self, rel_file: str, line_no: int) -> list[Item] | None:
        """The items of the `loop:` / `with_items:` of the task holding line `line_no`."""
        lines = self._texts[rel_file].splitlines()
        index = line_no - 1
        if _DASH.match(lines[index]):
            return None
        own = _indent(lines[index])
        start = next((j for j in range(index - 1, -1, -1)
                      if not _skip(lines[j]) and _DASH.match(lines[j]) and _indent(lines[j]) < own), None)
        if start is None:
            return None
        task = _indent(lines[start])
        key_indent = task + 2
        found: list[list[Item] | None] = []
        for j in range(start + 1, len(lines)):
            line = lines[j]
            if _skip(line):
                continue
            if _indent(line) <= task:
                break
            if _indent(line) != key_indent:
                continue
            keyed = _MAPPING_LINE.match(line.strip())
            if keyed is None:
                continue
            key, value = keyed.group(1), keyed.group(2).split(" #", 1)[0].strip()
            if key == "loop_control" and self._renames_item(lines, j + 1, key_indent):
                return None
            if key not in ("loop", "with_items"):
                continue
            if not value:
                found.append(_block_items(lines, j + 1, key_indent))
            elif value.startswith("["):
                found.append(_flow_items(value))
            else:
                named = _LIST_VAR.match(_unquote(value))
                found.append(self._list_values(named.group(1)) if named else None)
        if len(found) != 1:
            return None
        return found[0]

    @staticmethod
    def _renames_item(lines: list[str], start: int, parent: int) -> bool:
        """Whether the `loop_control:` block starting at `lines[start]` sets a `loop_var`."""
        for line in lines[start:]:
            if _skip(line):
                continue
            if _indent(line) <= parent:
                return False
            if line.strip().startswith("loop_var:"):
                return True
        return False

    def _list_values(self, dotted: str) -> list[Item] | None:
        """Every item of the list variable `dotted`, from every walked file that sets it."""
        lists = [found for text in self._texts.values()
                 for found in _list_in(text.splitlines(), dotted.split("."))]
        if not lists or any(found is None for found in lists):
            return None
        return [item for found in lists for item in found]

    def _scalar_values(self, name: str) -> list[str | None] | None:
        """Every value a walked file sets `name` to; None in place of one that is not a literal."""
        setting = re.compile(rf"^\s*(?:-\s+)?{re.escape(name)}\s*:\s*(.*)$")
        values: list[str | None] = []
        for text in self._texts.values():
            for _line_no, line in _non_comment_lines(text):
                found = setting.match(line)
                if found:
                    values.append(_literal(found.group(1)))
        return values or None

    def _is_go_module(self, rel: str) -> bool:
        return self._is_file(posixpath.join(rel, "go.mod"))

    def _go_line(self, rel_file: str, line_no: int, line: str) -> None:
        named = _GO_COMMAND_NAME.match(line)
        if named:
            self._go_names.append((f"{rel_file}:{line_no}", _unquote(named.group(1))))
        if _GO_COMMAND_MODULE.match(line):
            self._external_go_files.add(rel_file)

    def finish_go(self) -> None:
        """Turn each Go module reference into the packages of the commands built from it."""
        for where, module in self._module_refs:
            if not self._go_names:
                self.inputs.unresolved.append(
                    f"{where}: the Go module {module} is built from, but no go_command_name says what"
                )
                continue
            self.inputs.go_modules.add(module)
            for name_at, value in self._go_names:
                for name in self._command_names(name_at, value):
                    self._go_command(name_at, module, name)

    def _command_names(self, where: str, value: str) -> list[str]:
        """The command names a `go_command_name` value stands for: itself, or every literal
        a walked file sets the one variable it is to."""
        if "{{" not in value:
            return [value]
        lone = _LONE_TEMPLATE.match(value)
        names: list[str] = []
        if lone:
            setting = re.compile(
                rf"^\s*(?:-\s+)?{lone.group(1)}\s*:\s*[\"']?([A-Za-z0-9_.-]+)[\"']?\s*(?:#.*)?$"
            )
            for text in self._texts.values():
                for _line_no, line in _non_comment_lines(text):
                    found = setting.match(line)
                    if found and found.group(1) not in names:
                        names.append(found.group(1))
        if not names:
            self.inputs.unresolved.append(f"{where}: go_command_name {value}, which no walked file sets")
        return names

    def _go_command(self, where: str, module: str, name: str) -> None:
        command = posixpath.join(module, "cmd", name)
        if self._is_dir(command):
            self.inputs.go_commands.add(command)
            self._go_dependencies(module, command)
            return
        if where.rsplit(":", 1)[0] in self._external_go_files:
            return
        self.inputs.unresolved.append(f"{where}: go_command_name {name}, but there is no {command}")

    def _go_dependencies(self, module: str, command: str) -> None:
        """Every package `command` imports, transitively, within `module` and its vendor tree.

        Read from the source rather than asked of the Go toolchain, so an answer needs no
        toolchain. Every non-test file counts, whatever its build constraint: a superset of
        one build, never less (the real-repo test holds it to `go list -deps`).
        """
        with open(os.path.join(self.root, module, "go.mod"), encoding="utf-8") as handle:
            declared = _GO_MODULE_LINE.search(handle.read())
        if declared is None:
            raise ValueError(f"{module}/go.mod declares no module")
        module_path = declared.group(1)
        for meta in ("go.mod", "go.sum", "vendor/modules.txt"):
            if self._is_file(posixpath.join(module, meta)):
                self.inputs.exact.add(posixpath.join(module, meta))
        name = posixpath.basename(command)
        pending = [command]
        while pending:
            package = pending.pop()
            builders = self.inputs.go_builders.setdefault(package, set())
            if name in builders:
                continue
            builders.add(name)
            self.inputs.go_packages.add(package)
            if package not in self._go_import_cache:
                self._go_import_cache[package] = self._go_imports(package)
            for imported in self._go_import_cache[package]:
                if imported == module_path or imported.startswith(module_path + "/"):
                    pending.append(module + imported[len(module_path):])
                elif self._is_dir(posixpath.join(module, "vendor", imported)):
                    pending.append(posixpath.join(module, "vendor", imported))

    def _go_imports(self, package: str) -> set[str]:
        """The import paths of a package's non-test files. The directories they embed become
        prefixes as they are read."""
        imports: set[str] = set()
        directory = os.path.join(self.root, package)
        for name in sorted(os.listdir(directory)):
            path = os.path.join(directory, name)
            if not name.endswith(".go") or name.endswith("_test.go") or not os.path.isfile(path):
                continue
            with open(path, encoding="utf-8") as handle:
                source = handle.read()
            for block in _GO_IMPORT_BLOCK.findall(source):
                for line in block.splitlines():
                    imports.update(_GO_QUOTED.findall(line.split("//", 1)[0]))
            imports.update(_GO_IMPORT_ONE.findall(source))
            for patterns in _GO_EMBED.findall(source):
                for pattern in patterns.split():
                    self._go_embed(package, pattern.strip("\"`").removeprefix("all:"))
        return imports

    def _go_embed(self, package: str, pattern: str) -> None:
        """An embedded directory, or a glob that can match one, is a prefix input. A plain
        file of the package's own directory is counted already."""
        glob = _GLOB.search(pattern)
        literal = pattern[: glob.start()] if glob else pattern
        if glob:
            directory = literal.rstrip("/") if literal.endswith("/") else posixpath.dirname(literal)
        elif self._is_dir(posixpath.join(package, literal)):
            directory = literal.rstrip("/")
        else:
            directory = posixpath.dirname(literal)
            if not directory:
                return
        rel = _safe_relpath(posixpath.join(package, directory) if directory else package)
        if rel is None or not rel.startswith(package):
            raise ValueError(f"{package}: //go:embed {pattern} names a path outside the package")
        self.inputs.prefixes.add(rel + "/")

    def _classify(self, rel_file: str, line_no: int, key: str, value: str, context: str) -> None:
        value = _unquote(value)
        if not value or _ROOTED_AT_START.match(value) or value.startswith("/") or _URL.match(value):
            return
        where = f"{rel_file}:{line_no}"
        head = _TEMPLATE_HEAD.match(value)
        if head:
            name = head.group(1)
            if name == "item" or name in _defined_vars(context) or name in _REGISTER.findall(context):
                return
            self.inputs.unresolved.append(f"{where}: {value}")
            return
        if "{{" in value:
            self.inputs.unresolved.append(f"{where}: {value}")
            return
        base = posixpath.dirname(rel_file)
        places = [base] if key == "import_playbook" else [base, f"{base}/files", f"{base}/templates"]
        for place in places:
            rel = _safe_relpath(posixpath.join(place, value))
            if rel is not None and (self._is_file(rel) or self._is_dir(rel)):
                self._add_path(rel, as_dir=False)
                return
        self.inputs.unresolved.append(f"{where}: {value}")

    def walk_yaml(self, rel_file: str, *, context: str) -> None:
        if rel_file in self._seen_files:
            return
        self._seen_files.add(rel_file)
        self.inputs.exact.add(rel_file)
        with open(os.path.join(self.root, rel_file), encoding="utf-8") as handle:
            text = handle.read()
        self._texts[rel_file] = text
        context = context + "\n" + text
        list_key: tuple[str, int] | None = None
        for line_no, line in _non_comment_lines(text):
            self._rooted(rel_file, line_no, line)
            self._go_line(rel_file, line_no, line)
            for package in _HELPER_TOKEN.findall(line):
                self._add_package(package)
            if list_key is not None:
                item = _LIST_ITEM.match(line)
                if item and len(item.group(1)) > list_key[1]:
                    self._classify(rel_file, line_no, list_key[0], item.group(2), context)
                    continue
                if line.strip():
                    list_key = None
            keyed = _KEY.match(line)
            if not keyed:
                continue
            indent, key, value = keyed.group(1), keyed.group(2), keyed.group(3)
            if key == "vars_files" and not value.strip():
                list_key = (key, len(indent))
                continue
            self._classify(rel_file, line_no, key, value, context)


def play_inputs(repo_root: str, play: str) -> Inputs:
    """Every checkout path `play` deploys, and every reference it could not follow."""
    walker = _Walker(repo_root)
    walker.walk_yaml(play, context="")
    walker.finish_templates()
    walker.finish_go()
    return walker.inputs


def candidate_plays(repo_root: str) -> list[str]:
    """Every play file under playbooks/imports/, nested optional plays included.
    playbook-main.yml is an aggregator of these, and playbooks/dev/ operates on the repo
    rather than the host, so neither is a candidate."""
    found = []
    for dirpath, _dirs, files in os.walk(os.path.join(repo_root, CANDIDATE_DIR)):
        for name in files:
            if name.endswith((".yml", ".yaml")):
                found.append(os.path.relpath(os.path.join(dirpath, name), repo_root))
    return sorted(found)


def load_allowlist(repo_root: str) -> list[str]:
    """The plays the timer may run unattended (D2). Refused, never read around: a bad
    entry would either run a play nobody allowed or silently drop one that was."""
    path = os.path.join(repo_root, ALLOWLIST_PATH)
    try:
        with open(path, encoding="utf-8") as handle:
            data = json.load(handle)
    except (OSError, json.JSONDecodeError) as error:
        raise ValueError(f"the unattended allowlist {ALLOWLIST_PATH} cannot be read: {error}") from error
    if not isinstance(data, list):
        raise ValueError(f"the unattended allowlist {ALLOWLIST_PATH} must be a JSON list of play paths")
    plays: list[str] = []
    for entry in data:
        if not isinstance(entry, str) or _safe_relpath(entry) != entry:
            raise ValueError(f"allowlist entry {entry!r} is not a normalised repo-relative path")
        if not entry.startswith(CANDIDATE_DIR + "/") or not entry.endswith((".yml", ".yaml")):
            raise ValueError(f"allowlist entry {entry!r} is not a play under {CANDIDATE_DIR}/")
        if not os.path.isfile(os.path.join(repo_root, entry)):
            raise ValueError(f"allowlist entry {entry!r} does not exist in the checkout")
        if entry in plays:
            raise ValueError(f"allowlist names {entry!r} twice")
        plays.append(entry)
    return plays


def decide(repo_root: str, changed: list[str]) -> Report:
    allowed = load_allowlist(repo_root)
    run, skipped, unresolved = [], [], []
    for play in candidate_plays(repo_root):
        inputs = play_inputs(repo_root, play)
        hit = any(affects(inputs, path) for path in changed)
        if hit:
            (run if play in allowed else skipped).append(play)
        if hit or play in allowed:
            unresolved.extend((play, where) for where in inputs.unresolved)
    return Report(run=run, skipped=skipped, unresolved=unresolved)


def changed_between(repo_root: str, old: str, new: str) -> list[str]:
    """Paths changed from `old` to `new`. `--no-renames`, so a moved file names both ends."""
    for ref in (old, new):
        if not ref or ref.startswith("-"):
            raise ValueError(f"{ref!r} is not a commit")
    result = subprocess.run(
        ["git", "-C", repo_root, "diff", "--name-only", "--no-renames", "-z", old, new, "--"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise ValueError(f"git diff {old} {new} failed: {result.stderr.strip()}")
    return [path for path in result.stdout.split("\0") if path]


def _repo_root_default() -> str:
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None, *, stdout: TextIO | None = None, stderr: TextIO | None = None) -> int:
    out: TextIO = stdout if stdout is not None else sys.stdout
    err: TextIO = stderr if stderr is not None else sys.stderr
    parser = argparse.ArgumentParser(description=(__doc__ or "").split("\n\n", 1)[0])
    parser.add_argument("--repo-root", default=_repo_root_default())
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--changed", nargs="+", metavar="PATH")
    source.add_argument("--old", metavar="SHA")
    parser.add_argument("--new", metavar="SHA")
    args = parser.parse_args(argv)
    if (args.old is None) != (args.new is None):
        parser.error("--old and --new go together")
    try:
        changed = args.changed or changed_between(args.repo_root, args.old, args.new)
        report = decide(args.repo_root, changed)
    except ValueError as error:
        err.write(f"affected-plays: {error}\n")
        return EXIT_ERROR
    for play in report.run:
        out.write(f"RUN {play}\n")
    for play in report.skipped:
        out.write(f"SKIP-NOT-ALLOWED {play}\n")
    for play, where in report.unresolved:
        out.write(f"UNRESOLVED {play} {where}\n")
    allowed = set(load_allowlist(args.repo_root))
    if any(play in allowed for play, _ in report.unresolved):
        err.write(
            "affected-plays: an allowlisted play has a reference this helper cannot follow, "
            "so a change to it could be missed; see the UNRESOLVED lines\n"
        )
        return EXIT_UNRESOLVED
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
