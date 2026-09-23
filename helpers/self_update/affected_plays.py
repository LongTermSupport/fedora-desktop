"""Which plays a set of changed paths touches, split by the unattended allowlist (Plan 00137).

The play ledger watches only each play's OWN file, so a commit that changes the ccy lib or
the `cc` launcher leaves `play-claude-yolo.yml` reading as up to date. This maps changed
paths to the plays that deploy them, from the play text itself.

**No YAML parser.** Helpers are stdlib-only (helpers/CLAUDE.md), so references are read
with regexes over the play text. The idioms recognised are the ones this repo uses:

- `{{ root_dir }}/<path>` anywhere on a non-comment line, and the same with root_dir's own
  definition, `{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}`. A literal
  path is an exact input, or a directory input when it ends in `/` or is a directory in
  the checkout. A path with a template in its tail (`.../bin/{{ item }}`) is an input on
  its literal prefix. That is conservative, because the loop that fills it in is not
  something a regex can evaluate.
- The files those references name are followed transitively when they are YAML (task
  files, vars files, imported plays). So an `include_tasks` of a task file makes that
  file's own `src:` paths inputs of the play.
- `import_playbook: <relative>`, relative to the importing file.
- `helpers.<pkg>` where `helpers/<pkg>/` exists (`python3 -m helpers.<pkg>.x` in an
  argv): the whole package, plus every `helpers.<other>` package its modules import.

**A reference it cannot follow is reported, never dropped.** A `src:`, `file:`,
`include_*` or `vars_files` value that is not rooted, not a host path or URL, not a loop
`item`, not a variable the file defines as rooted or as a host path, and not a registered
result, is UNRESOLVED. A missed dependency is a play that silently never re-runs, which
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

EXIT_OK = 0
EXIT_UNRESOLVED = 1
EXIT_ERROR = 2

_ROOT_EXPR = (
    r"(?:\{\{\s*root_dir\s*\}\}"
    r"|\{\{\s*lookup\(\s*'ansible\.builtin\.config'\s*,\s*'CONFIG_FILE'\s*\)\s*\|\s*dirname\s*\}\})"
)
_ROOTED = re.compile(_ROOT_EXPR + r"/([A-Za-z0-9_.@+\-/]*)(\{\{)?")
_ROOTED_AT_START = re.compile(_ROOT_EXPR + "/")
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


@dataclass
class Inputs:
    """What a play deploys from the checkout: exact paths, path prefixes, and the
    references that could not be followed."""

    exact: set[str] = field(default_factory=set)
    prefixes: set[str] = field(default_factory=set)
    unresolved: list[str] = field(default_factory=list)


@dataclass
class Report:
    run: list[str]
    skipped: list[str]
    unresolved: list[tuple[str, str]]


def affects(inputs: Inputs, path: str) -> bool:
    return path in inputs.exact or any(path.startswith(prefix) for prefix in inputs.prefixes)


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
        self._maybe_helper_path(rel)

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
                with open(os.path.join(dirpath, name), encoding="utf-8") as handle:
                    source = handle.read()
                for imported in _PY_IMPORT.findall(source):
                    self._add_package(imported)
                for group in _PY_FROM_HELPERS.findall(source):
                    for imported in re.findall(r"[a-z_][a-z0-9_]*", group):
                        self._add_package(imported)

    def _rooted(self, rel_file: str, line_no: int, line: str) -> None:
        for match in _ROOTED.finditer(line):
            literal, templated = match.group(1), match.group(2)
            if templated:
                prefix = _safe_relpath(literal.rstrip("/") or "") if literal else None
                if prefix is None:
                    self.inputs.unresolved.append(f"{rel_file}:{line_no}: {match.group(0)}")
                    continue
                self.inputs.prefixes.add(literal if literal.endswith("/") else prefix)
                self._maybe_helper_path(prefix)
                continue
            rel = _safe_relpath(literal)
            if rel is None or rel == ".":
                self.inputs.unresolved.append(f"{rel_file}:{line_no}: {match.group(0)}")
                continue
            self._add_path(rel, as_dir=literal.endswith("/"))

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
        context = context + "\n" + text
        list_key: tuple[str, int] | None = None
        for line_no, line in _non_comment_lines(text):
            self._rooted(rel_file, line_no, line)
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
    stdout = stdout or sys.stdout
    stderr = stderr or sys.stderr
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n", 1)[0])
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
        stderr.write(f"affected-plays: {error}\n")
        return EXIT_ERROR
    for play in report.run:
        stdout.write(f"RUN {play}\n")
    for play in report.skipped:
        stdout.write(f"SKIP-NOT-ALLOWED {play}\n")
    for play, where in report.unresolved:
        stdout.write(f"UNRESOLVED {play} {where}\n")
    allowed = set(load_allowlist(args.repo_root))
    if any(play in allowed for play, _ in report.unresolved):
        stderr.write(
            "affected-plays: an allowlisted play has a reference this helper cannot follow, "
            "so a change to it could be missed; see the UNRESOLVED lines\n"
        )
        return EXIT_UNRESOLVED
    return EXIT_OK


if __name__ == "__main__":
    sys.exit(main())
