"""The rule homeserver-list-narrow-grammar (Defence Before Fix; CLAUDE/QA.md).

A list the homeserver answers with is the homeserver's, not the bus's: it can hold any room or
event the server holds, of any room version. `protocol.is_room_id` and `protocol.is_event_id`
are the bus's own grammar (room version 12 only), so applying one of them through `all()` or
`any()` to such a list refuses the whole answer when one legitimate foreign item is in it. That
is how Tuwunel's own server admin room, room version 11 (`!opaque:server`), stopped every
team's side-room discovery.

The rule reads code rather than running it: in first-party Python, a function that calls the
homeserver (a method of HOMESERVER_CALLS: `.call(`, `.sync(`, `.state(`, ...) and applies
`is_room_id` or `is_event_id` inside `all(...)` or `any(...)`, or in a loop's `if` that raises,
is a finding. Check such a list with the spec's grammar
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
#: The methods of matrix.Client and admin._Api that bring a homeserver answer back.
HOMESERVER_CALLS = frozenset({"call", "transport", "_request", "sync", "messages", "get_state", "get_event",
                              "state", "room_state", "members", "joined_rooms", "account"})
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


def _checks_bus_grammar(tree: ast.AST) -> bool:
    return any(_called_name(inner) in BUS_GRAMMAR_CHECKS for inner in _calls(tree))


def _is_homeserver_call(node: ast.AST) -> bool:
    return (isinstance(node, ast.Call) and isinstance(node.func, ast.Attribute)
            and node.func.attr in HOMESERVER_CALLS)


def _from_homeserver(tree: ast.AST, tainted: set[str]) -> bool:
    """Whether the expression holds a homeserver call or a name bound from one."""
    return any(_is_homeserver_call(n) or (isinstance(n, ast.Name) and n.id in tainted) for n in ast.walk(tree))


def _homeserver_names(func: ast.AST) -> set[str]:
    """Every local name bound, directly or through other bindings, from a homeserver answer."""
    bindings = [(t, n.value) for n in ast.walk(func) if isinstance(n, ast.Assign) for t in n.targets]
    bindings += [(n.target, n.value) for n in ast.walk(func) if isinstance(n, (ast.AnnAssign, ast.NamedExpr))
                 and n.value is not None]
    tainted: set[str] = set()
    while True:
        found = {name.id for target, value in bindings if _from_homeserver(value, tainted)
                 for name in ast.walk(target) if isinstance(name, ast.Name)}
        if found <= tainted:
            return tainted
        tainted |= found


def _raises_in_loop(loop: ast.For | ast.AsyncFor) -> int | None:
    """The line of an `if` in the loop that checks the bus grammar and raises: one item refuses all."""
    for node in ast.walk(loop):
        if (isinstance(node, ast.If) and _checks_bus_grammar(node.test)
                and any(isinstance(n, ast.Raise) for stmt in node.body for n in ast.walk(stmt))):
            return node.lineno
    return None


def findings(source: str, filename: str) -> list[int]:
    """The line of every whole-list refusal by a bus-grammar check in a homeserver-calling function:
    `all()`/`any()` over the check, or a loop whose check raises."""
    lines: list[int] = []
    for func in ast.walk(ast.parse(source, filename)):
        if not isinstance(func, (ast.FunctionDef, ast.AsyncFunctionDef)):
            continue
        tainted = _homeserver_names(func)
        for call in _calls(func):
            if (_called_name(call) in WHOLE_LIST_REDUCERS and isinstance(call.func, ast.Name)
                    and any(isinstance(arg, (ast.GeneratorExp, ast.ListComp, ast.SetComp))
                            and _checks_bus_grammar(arg.elt)
                            and any(_from_homeserver(gen.iter, tainted) for gen in arg.generators)
                            for arg in call.args)):
                lines.append(call.lineno)
        for loop in ast.walk(func):
            if (isinstance(loop, (ast.For, ast.AsyncFor)) and _from_homeserver(loop.iter, tainted)
                    and (line := _raises_in_loop(loop)) is not None):
                lines.append(line)
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

    def invites(self):
        rooms = self.client.sync(since=None)["rooms"]["invite"]
        for room_id in rooms:
            if not protocol.is_room_id(room_id):  # ruleid
                raise ValueError("bad invite")

    def invites_per_item(self):
        rooms = self.client.sync(since=None)["rooms"]["invite"]
        for room_id in rooms:
            if not protocol.is_room_id(room_id):  # ok: one item set aside, not the answer
                continue

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
        self.assertEqual(len(expected), 3)
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
