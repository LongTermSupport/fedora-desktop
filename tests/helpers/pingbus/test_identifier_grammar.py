"""The rule homeserver-list-narrow-grammar (Defence Before Fix; CLAUDE/QA.md).

A list the homeserver answers with is the homeserver's, not the bus's: it can hold any room or
event the server holds, of any room version. `protocol.is_room_id` and `protocol.is_event_id`
are the bus's own grammar (room version 12 only), so applying one of them through `all()` or
`any()` to such a list refuses the whole answer when one legitimate foreign item is in it. That
is how Tuwunel's own server admin room, room version 11 (`!opaque:server`), stopped every
team's side-room discovery.

The rule reads code rather than running it: in first-party Python, a function that calls the
homeserver (`.call(`, `.transport(`, `._request(`) and applies `is_room_id` or `is_event_id`
inside `all(...)` or `any(...)` is a finding. Check such a list with the spec's grammar
(`protocol.is_any_room_id`) and then select what the bus can use, naming what it set aside.
"""

from __future__ import annotations

import ast
import os
import pathlib
import textwrap
import unittest

RULE_ID = "homeserver-list-narrow-grammar"
REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]
#: First-party Python that can hold a homeserver client: the helpers and the plan scripts.
SCAN_ROOTS = ("helpers", "CLAUDE/Plan")
HOMESERVER_CALLS = frozenset({"call", "transport", "_request"})
BUS_GRAMMAR_CHECKS = frozenset({"is_room_id", "is_event_id"})
WHOLE_LIST_REDUCERS = frozenset({"all", "any"})


def _called_name(node: ast.Call) -> str | None:
    func = node.func
    if isinstance(func, ast.Name):
        return func.id
    if isinstance(func, ast.Attribute):
        return func.attr
    return None


def _calls(tree: ast.AST) -> list[ast.Call]:
    return [n for n in ast.walk(tree) if isinstance(n, ast.Call)]


def findings(source: str, filename: str) -> list[int]:
    """The line of every `all()`/`any()` over a bus-grammar check in a homeserver-calling function."""
    lines: list[int] = []
    for func in ast.walk(ast.parse(source, filename)):
        if not isinstance(func, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        calls = _calls(func)
        if not any(_called_name(c) in HOMESERVER_CALLS and isinstance(c.func, ast.Attribute) for c in calls):
            continue
        for call in calls:
            if (_called_name(call) in WHOLE_LIST_REDUCERS and isinstance(call.func, ast.Name)
                    and any(_called_name(inner) in BUS_GRAMMAR_CHECKS
                            for arg in call.args for inner in _calls(arg))):
                lines.append(call.lineno)
    return sorted(set(lines))


def scanned_files() -> list[pathlib.Path]:
    files: list[pathlib.Path] = []
    for root in SCAN_ROOTS:
        for dirpath, dirnames, filenames in os.walk(REPO_ROOT / root):
            dirnames[:] = [d for d in dirnames if d not in ("__pycache__", ".venv", "venv")]
            files.extend(pathlib.Path(dirpath) / f for f in filenames if f.endswith(".py"))
    return sorted(files)


FIXTURE = textwrap.dedent('''
    def joined_rooms(self):
        _, obj = self.call("list the admin's rooms", "GET", "/joined_rooms")
        rooms = obj.get("joined_rooms")
        if not isinstance(rooms, list) or not all(protocol.is_room_id(r) for r in rooms):  # ruleid
            raise AdminError("no list of room IDs")
        return rooms

    def event_ids(self):
        status, events = self.transport("GET", "/messages", None, None, self.token)
        if any(not is_event_id(e.get("event_id")) for e in events):  # ruleid
            raise AdminError("bad page")

    def joined_rooms_by_spec(self):
        _, obj = self.call("list the admin's rooms", "GET", "/joined_rooms")
        rooms = obj.get("joined_rooms")
        if not isinstance(rooms, list) or not all(protocol.is_any_room_id(r) for r in rooms):  # ok
            raise AdminError("no list of room IDs")
        return [r for r in rooms if protocol.is_room_id(r)]  # ok: selects, refuses nothing

    def check_index(rooms, main_room):
        # ok: no homeserver call here; the bus's own index content is the bus's grammar
        return all(is_room_id(room) and room != main_room for room in rooms.values())

    def one_answer(self):
        _, obj = self.call("create the room", "POST", "/createRoom")
        if not protocol.is_room_id(obj.get("room_id")):  # ok: a room the bus asked to be v12
            raise AdminError("no room ID")
''')


def marked(source: str, marker: str) -> list[int]:
    return [n for n, line in enumerate(source.splitlines(), 1) if line.rstrip().endswith(f"# {marker}")]


class RuleProof(unittest.TestCase):
    """The rule fires on the fixture's `ruleid` lines and on nothing else."""

    def test_the_rule_fires_on_exactly_the_marked_lines(self) -> None:
        expected = marked(FIXTURE, "ruleid")
        self.assertEqual(len(expected), 2)
        self.assertEqual(findings(FIXTURE, "<fixture>"), expected)


class Sweep(unittest.TestCase):
    """No first-party Python checks a homeserver list with the bus's narrower grammar."""

    def test_the_scan_finds_the_homeserver_clients(self) -> None:
        names = {p.relative_to(REPO_ROOT).as_posix() for p in scanned_files()}
        for client in ("helpers/agent_bus/admin.py", "helpers/pingbus/matrix.py", "helpers/pingbus/syncer.py"):
            self.assertIn(client, names)

    def test_no_finding(self) -> None:
        found = [f"{p.relative_to(REPO_ROOT)}:{line}: {RULE_ID}: a homeserver list checked with the "
                 "bus's own identifier grammar refuses the whole answer for one foreign item; "
                 f"see CLAUDE/QA.md \"{RULE_ID}\""
                 for p in scanned_files() for line in findings(p.read_text(encoding="utf-8"), str(p))]
        self.assertEqual(found, [], "\n" + "\n".join(found))


if __name__ == "__main__":
    unittest.main()
