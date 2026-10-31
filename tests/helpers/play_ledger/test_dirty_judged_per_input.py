"""Guard: a run's dirtiness is judged per input, never as one fact about the whole checkout.

Defence Before Fix, class "a whole-checkout fact used where a per-input fact is needed, so
one unrelated file stales everything". One stray untracked file under CLAUDE/Plan/, which
no play reads, made every play run for three days record `dirty: true`, and the judge read
every such run as stale whatever commit it ran at: 24 plays offered for ever.

Two shapes of the class, each found on the syntax tree of the code that reads the ledger:

1. `git status` collapsed to one boolean (`bool(<... "status" ...>)`): the checkout's
   state with the paths thrown away, so nothing downstream can ask "was it MY input?".
2. A run record's `dirty` key read directly (`record["dirty"]`, `record.get("dirty")`)
   anywhere but `ledger.recorded_dirty_paths`, the one reader that turns a record into
   the paths that were dirty, or into "cannot say" for a record that does not name them.

Run from the repo root:

    python3 -m unittest tests.helpers.play_ledger.test_dirty_judged_per_input
"""

from __future__ import annotations

import ast
import pathlib
import sys
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[3]
sys.path.insert(0, str(ROOT))

#: The code that writes or reads the ledger's runs, the health report and its collector.
SCANNED = ("helpers/play_ledger", "helpers/host_health", "callback_plugins")

#: The one function allowed to read the `dirty` key: it knows the record shapes.
ALLOWED_READER = ("helpers/play_ledger/ledger.py", "recorded_dirty_paths")


def _status_collapsed_to_bool(node: ast.AST) -> bool:
    if not (isinstance(node, ast.Call) and isinstance(node.func, ast.Name) and node.func.id == "bool"):
        return False
    return any(isinstance(inner, ast.Constant) and inner.value == "status"
               for argument in node.args for inner in ast.walk(argument))


def _reads_dirty_key(node: ast.AST) -> bool:
    if isinstance(node, ast.Subscript):
        key = node.slice
        return isinstance(key, ast.Constant) and key.value == "dirty" and isinstance(node.ctx, ast.Load)
    if isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute) and node.func.attr == "get":
        return bool(node.args) and isinstance(node.args[0], ast.Constant) and node.args[0].value == "dirty"
    return False


def findings(root: pathlib.Path, relative_paths: list[str]) -> list[str]:
    """`<path>:<line>: <what>` for each instance of the class in the given files."""
    found = []
    for relative in relative_paths:
        tree = ast.parse((root / relative).read_text(encoding="utf-8"), filename=relative)
        allowed: set[int] = set()
        for function in ast.walk(tree):
            if (isinstance(function, ast.FunctionDef)
                    and (relative, function.name) == ALLOWED_READER):
                allowed |= {id(inner) for inner in ast.walk(function)}
        for node in ast.walk(tree):
            if _status_collapsed_to_bool(node):
                found.append(f"{relative}:{node.lineno}: git status collapsed to one boolean")
            elif _reads_dirty_key(node) and id(node) not in allowed:
                found.append(f"{relative}:{node.lineno}: a run's dirty flag read as a verdict; "
                             "use ledger.recorded_dirty_paths and judge the paths per input")
    return found


def scanned_files(root: pathlib.Path) -> list[str]:
    return sorted(str(path.relative_to(root)) for top in SCANNED for path in (root / top).rglob("*.py"))


class TestTheRepository(unittest.TestCase):
    def test_no_code_judges_a_run_by_the_whole_checkout_being_dirty(self) -> None:
        files = scanned_files(ROOT)
        self.assertIn("helpers/play_ledger/changed_plays.py", files)
        self.assertIn("callback_plugins/play_ledger.py", files)
        self.assertEqual(findings(ROOT, files), [])


class TestTheGuard(unittest.TestCase):
    """The guard is proven on the two shapes it exists for, and on what it must allow."""

    def _findings(self, files: dict[str, str]) -> list[str]:
        with tempfile.TemporaryDirectory() as base:
            root = pathlib.Path(base)
            for relative, text in files.items():
                (root / relative).parent.mkdir(parents=True, exist_ok=True)
                (root / relative).write_text(text, encoding="utf-8")
            return findings(root, sorted(files))

    def test_status_collapsed_to_a_boolean_is_found(self) -> None:
        text = ('def is_dirty(root, run):\n'
                '    return bool(_git(root, ["status", "--porcelain"], run).strip())\n')
        self.assertEqual(self._findings({"helpers/play_ledger/repo.py": text}),
                         ["helpers/play_ledger/repo.py:2: git status collapsed to one boolean"])

    def test_a_dirty_key_read_as_a_verdict_is_found(self) -> None:
        text = ('def judge(record, changed):\n'
                '    if record["dirty"] or changed:\n'
                '        return "stale"\n'
                '    return {p for p in latest if latest.get(p, {}).get("dirty")}\n')
        found = self._findings({"helpers/play_ledger/changed_plays.py": text})
        self.assertEqual([line.split(": ")[0] for line in found],
                         ["helpers/play_ledger/changed_plays.py:2", "helpers/play_ledger/changed_plays.py:4"])

    def test_the_one_reader_and_writing_the_key_are_allowed(self) -> None:
        reader = ('def recorded_dirty_paths(record):\n'
                  '    return () if not record.get("dirty") else record["dirty_paths"]\n')
        writer = 'def build(dirty):\n    record = {"dirty": dirty}\n    record["dirty"] = dirty\n    return record\n'
        self.assertEqual(self._findings({"helpers/play_ledger/ledger.py": reader + writer}), [])

    def test_the_same_reader_name_elsewhere_is_not_allowed(self) -> None:
        text = 'def recorded_dirty_paths(record):\n    return record["dirty"]\n'
        self.assertEqual(len(self._findings({"helpers/play_ledger/other.py": text})), 1)


if __name__ == "__main__":
    unittest.main()
