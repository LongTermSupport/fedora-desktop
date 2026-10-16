#!/usr/bin/env python3
"""The Python half of the rule project_hidden_bindings: a Textual key binding whose action
no visible control on the screen offers.

Every action is a visible, named choice; a key is only a shortcut to one. In a Textual app the
visible choice is a Button that runs the same action (Button("Download (d)",
action="app.download")), so a binding is judged by its action: it must be navigation (an action
named focus*, scroll* or cursor*, which moves focus, the cursor or the view) or the action of a
Button on the same screen. The same screen is the class that binds the key: the Button is built
in that class's compose(), and its action runs there (no namespace, "app." in an App class,
"screen." in any other). A Button built anywhere else, or one compose() reaches only through
another method, pairs nothing.

Read from the syntax tree, never run: a BINDINGS list in a class body (Binding(...) calls and
(key, action, ...) tuples) and every .bind(key, action) call (judged against the class whose
method makes it; outside a class, nothing pairs it), in a file that imports textual. A binding
or a BINDINGS value that is not a literal cannot be judged and is a finding.

    python3 -m helpers.qa_environment.textual_bindings FILE...

stdout is one JSON object, {"files": N, "findings": [{"path", "line", "message"}]}; the Go gate
qa-hidden-bindings reads it. Exit 0 when every file was judged (with or without findings), 2
when there is no file or one cannot be read or parsed (the reason on stderr).
"""

from __future__ import annotations

import ast
import dataclasses
import json
import sys

RULE = "project_hidden_bindings"
NAVIGATION_PREFIXES = ("focus", "scroll", "cursor")
EXIT_CANNOT_RUN = 2


@dataclasses.dataclass(frozen=True)
class Finding:
    line: int
    message: str


def _name(func: ast.expr) -> str:
    if isinstance(func, ast.Name):
        return func.id
    if isinstance(func, ast.Attribute):
        return func.attr
    return ""


def _text(node: ast.expr | None) -> str | None:
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return node.value
    return None


def _arg(call: ast.Call, index: int, keyword: str) -> ast.expr | None:
    if len(call.args) > index:
        return call.args[index]
    return next((k.value for k in call.keywords if k.arg == keyword), None)


def _action_name(action: str) -> str:
    return action.split("(", 1)[0].rsplit(".", 1)[-1]


def _action_namespace(action: str) -> str:
    return action.split("(", 1)[0].rpartition(".")[0]


def _own_nodes(node: ast.AST):
    """Every node under node, not descending into a class defined inside it."""
    for child in ast.iter_child_nodes(node):
        if isinstance(child, ast.ClassDef):
            continue
        yield child
        yield from _own_nodes(child)


def _imports_textual(tree: ast.Module) -> bool:
    for node in ast.walk(tree):
        if isinstance(node, ast.Import) and any(
            a.name.split(".")[0] == "textual" for a in node.names
        ):
            return True
        if (
            isinstance(node, ast.ImportFrom)
            and (node.module or "").split(".")[0] == "textual"
        ):
            return True
    return False


def _button_actions(cls: ast.ClassDef) -> set[str]:
    """The actions of the Buttons cls's compose() builds that run on cls's own screen."""
    is_app = any(_name(b).endswith("App") for b in cls.bases)
    runs_here = {"", "app"} if is_app else {"", "screen"}
    actions = set()
    for stmt in cls.body:
        if not (
            isinstance(stmt, ast.FunctionDef | ast.AsyncFunctionDef)
            and stmt.name == "compose"
        ):
            continue
        for node in _own_nodes(stmt):
            if isinstance(node, ast.Call) and _name(node.func) == "Button":
                text = _text(
                    next((k.value for k in node.keywords if k.arg == "action"), None)
                )
                if text and _action_namespace(text) in runs_here:
                    actions.add(_action_name(text))
    return actions


def _bind_calls(node: ast.AST) -> list[tuple[int, ast.expr | None, ast.expr | None]]:
    return [
        (n.lineno, _arg(n, 0, "keys"), _arg(n, 1, "action"))
        for n in _own_nodes(node)
        if isinstance(n, ast.Call)
        and isinstance(n.func, ast.Attribute)
        and n.func.attr == "bind"
    ]


def _class_bindings(
    cls: ast.ClassDef,
) -> list[tuple[int, ast.expr | None, ast.expr | None]]:
    """Every (line, key, action) cls binds; None where a binding cannot be read."""
    found = _bind_calls(cls)
    for stmt in cls.body:
        targets = (
            stmt.targets
            if isinstance(stmt, ast.Assign)
            else [stmt.target]
            if isinstance(stmt, ast.AnnAssign)
            else []
        )
        if not any(isinstance(t, ast.Name) and t.id == "BINDINGS" for t in targets):
            continue
        value = stmt.value
        if not isinstance(value, ast.List | ast.Tuple):
            found.append((stmt.lineno, None, None))
            continue
        for elt in value.elts:
            if isinstance(elt, ast.Call) and _name(elt.func) == "Binding":
                found.append((elt.lineno, _arg(elt, 0, "key"), _arg(elt, 1, "action")))
            elif isinstance(elt, ast.Tuple | ast.List) and len(elt.elts) >= 2:
                found.append((elt.lineno, elt.elts[0], elt.elts[1]))
            else:
                found.append((elt.lineno, None, None))
    return found


def _screens(tree: ast.Module):
    """Each (bindings, button actions) pair of the file: one per class, and the bind calls made
    outside any class, which no Button pairs."""
    yield _bind_calls(tree), set()
    for cls in (n for n in ast.walk(tree) if isinstance(n, ast.ClassDef)):
        yield _class_bindings(cls), _button_actions(cls)


def judge_source(source: str, path: str) -> list[Finding]:
    """The findings of one file's source; SyntaxError when it does not parse."""
    tree = ast.parse(source, filename=path)
    if not _imports_textual(tree):
        return []
    judged = [
        (line, key, action, buttons)
        for bindings, buttons in _screens(tree)
        for line, key, action in bindings
    ]
    findings = []
    for line, key_node, action_node, buttons in sorted(judged, key=lambda b: b[0]):
        key, action = _text(key_node), _text(action_node)
        if key is None or action is None:
            findings.append(
                Finding(
                    line,
                    f"Textual binding cannot be read: spell the key and action as literals [{RULE}]",
                )
            )
            continue
        name = _action_name(action)
        if name.startswith(NAVIGATION_PREFIXES) or name in buttons:
            continue
        findings.append(
            Finding(
                line,
                f"Textual binding {key} -> {name}: a key with no visible control; "
                f"add a Button whose action= runs {name} on its screen [{RULE}]",
            )
        )
    return findings


def main(argv: list[str]) -> int:
    if not argv:
        print("ERROR: no file to judge", file=sys.stderr)
        return EXIT_CANNOT_RUN
    findings = []
    for path in argv:
        try:
            with open(path, encoding="utf-8") as f:
                source = f.read()
            found = judge_source(source, path)
        except (OSError, UnicodeDecodeError, SyntaxError, ValueError) as e:
            print(f"ERROR: {path}: {e}", file=sys.stderr)
            return EXIT_CANNOT_RUN
        findings.extend(
            {"path": path, "line": f.line, "message": f.message} for f in found
        )
    print(json.dumps({"files": len(argv), "findings": findings}))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
