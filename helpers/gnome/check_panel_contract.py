"""The panel's contract with the status document, checked across two languages.

`helpers/host_health/status_document.py` writes the document and
`extensions/fedora-desktop@fedora-desktop/statusDocument.js` reads it, so the file name,
the schema number and the three state strings are each declared twice. Nothing at
runtime notices when the two copies disagree: a panel looking for the wrong name, or
refusing a schema number it was not told about, renders `unavailable` for ever — and
`unavailable` is by design indistinguishable from a producer that never ran. The panel
would be confidently reporting that nothing is known about a host it simply cannot find
the file for.

That is this plan's own defect with the languages swapped, so the agreement is a gate
rather than a comment asking someone to remember.

**It reads the JavaScript as text.** There is no JS runtime in the QA path, and adding
one to compare six strings is not worth it. The risk a text matcher carries is that its
pattern silently stops matching and the gate then compares nothing — so a constant it
cannot find in the JavaScript is a **finding**, never treated as agreement.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md
"""

from __future__ import annotations

import os
import re
import sys

from helpers.host_health import status_document

#: The JavaScript half of the contract.
PANEL_JS = os.path.join(
    "extensions", "fedora-desktop@fedora-desktop", "statusDocument.js"
)


def expected() -> dict[str, str]:
    """Every constant that must agree, taken from Python as the source of truth.

    Python is the authority because it writes the document. Values are compared as
    strings so a number and a quoted string are checked the same way — what matters is
    that the two halves mean the same thing, not how each language spells it.
    """
    return {
        "FILE_NAME": status_document.FILE_NAME,
        "SCHEMA_VERSION": str(status_document.SCHEMA_VERSION),
        "SELF_SECTION": status_document.SELF_SECTION,
        "OK": status_document.OK,
        "FINDINGS": status_document.FINDINGS,
        "UNAVAILABLE": status_document.UNAVAILABLE,
    }


def _declared(javascript: str, name: str) -> str | None:
    """The value the JavaScript gives `name`, or None if it does not declare it.

    Matches both `const NAME = 'text';` and `const NAME = 7;`, with or without
    `export`, because the panel exports the states and keeps the file name private.
    """
    pattern = (
        r"(?:export\s+)?const\s+" + re.escape(name) + r"\s*=\s*(?:'([^']*)'|(\d+))\s*;"
    )
    match = re.search(pattern, javascript)
    if match is None:
        return None
    return match.group(1) if match.group(1) is not None else match.group(2)


def mismatches(javascript: str, wanted: dict[str, str]) -> list[str]:
    """Every disagreement, as a line naming the constant and both sides.

    Both sides, because "these disagree" sends the reader to diff two files by hand,
    and the whole value of a gate is that its output is the diagnosis.
    """
    findings: list[str] = []
    for name, value in wanted.items():
        found = _declared(javascript, name)
        if found is None:
            findings.append(
                f"PANEL-CONTRACT-FAIL {name}: not declared in {PANEL_JS}, so the panel "
                f"and the producer cannot be shown to agree on it (Python says "
                f"{value!r})"
            )
        elif found != value:
            findings.append(
                f"PANEL-CONTRACT-FAIL {name}: the panel says {found!r}, Python says "
                f"{value!r}. A mismatch here makes the panel report `unavailable` for "
                f"ever, which reads as a host nothing has checked."
            )
    return findings


def check(root: str) -> list[str]:
    """The gate: read the panel's JavaScript and compare it against Python."""
    path = os.path.join(root, PANEL_JS)
    with open(path, encoding="utf-8") as handle:
        return mismatches(handle.read(), expected())


def main(argv: list[str] | None = None) -> int:
    root = (argv or [os.getcwd()])[0]
    findings = check(root)
    for finding in findings:
        sys.stderr.write(f"{finding}\n")
    if findings:
        return 1
    wanted = expected()
    print(
        f"PANEL-CONTRACT-OK {len(wanted)} constant(s) agree between "
        f"status_document.py and the panel: {', '.join(sorted(wanted))}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
