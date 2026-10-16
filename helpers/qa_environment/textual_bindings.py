#!/usr/bin/env python3
"""The Python half of the rule project_hidden_bindings: a Textual key binding whose action
no visible control on the screen offers.

Every action is a visible, named choice; a key is only a shortcut to one. In a Textual app the
visible choice is a Button that runs the same action (Button("Download (d)",
action="app.download")), so a binding is judged by its action: it must be navigation (an action
named focus*, scroll* or cursor*, which moves focus, the cursor or the view) or the action of a
Button in the same file. The namespace before the last dot ("app.", "screen.") is not compared.

Read from the syntax tree, never run: a BINDINGS list in a class body (Binding(...) calls and
(key, action, ...) tuples) and every .bind(key, action) call, in a file that imports textual. A
binding or a BINDINGS value that is not a literal cannot be judged and is a finding.

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


def _button_actions(tree: ast.Module) -> set[str]:
    actions = set()
    for node in ast.walk(tree):
        if isinstance(node, ast.Call) and _name(node.func) == "Button":
            text = _text(
                next((k.value for k in node.keywords if k.arg == "action"), None)
            )
            if text:
                actions.add(_action_name(text))
    return actions


def _bindings(tree: ast.Module) -> list[tuple[int, ast.expr | None, ast.expr | None]]:
    """Every (line, key, action) the file binds; None where a binding cannot be read."""
    found: list[tuple[int, ast.expr | None, ast.expr | None]] = []
    for cls in (n for n in ast.walk(tree) if isinstance(n, ast.ClassDef)):
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
                    found.append(
                        (elt.lineno, _arg(elt, 0, "key"), _arg(elt, 1, "action"))
                    )
                elif isinstance(elt, ast.Tuple | ast.List) and len(elt.elts) >= 2:
                    found.append((elt.lineno, elt.elts[0], elt.elts[1]))
                else:
                    found.append((elt.lineno, None, None))
    for node in ast.walk(tree):
        if (
            isinstance(node, ast.Call)
            and isinstance(node.func, ast.Attribute)
            and node.func.attr == "bind"
        ):
            found.append((node.lineno, _arg(node, 0, "keys"), _arg(node, 1, "action")))
    return sorted(found, key=lambda b: b[0])


def judge_source(source: str, path: str) -> list[Finding]:
    """The findings of one file's source; SyntaxError when it does not parse."""
    tree = ast.parse(source, filename=path)
    if not _imports_textual(tree):
        return []
    buttons = _button_actions(tree)
    findings = []
    for line, key_node, action_node in _bindings(tree):
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
                f'add a Button whose action= runs {name} on its screen [{RULE}]',
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
