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

**This is a VOCABULARY check, and it must not be read as a behaviour one.** It proves
both sides use the same words. It cannot prove either side asks a question with them,
and extending it to try would be a category error. The live example: `kernel` is one of
the document keys demanded here and the gate passes, while the panel's only uses of it
are a prose comment and a `kernel: ''` default that is compared to nothing — so the panel
renders boot-scoped findings as current faults whichever boot produced them
(`status_document.is_boot_stale` is the predicate it does not ask). What proves a
behaviour is a test that renders a document through the panel's own section code; adding
another required mention here would only make the gate's silence more convincing.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md
"""

from __future__ import annotations

import inspect
import os
import re
import sys

from helpers.host_health import handoff, login_report, probe_results, status_document
from helpers.play_ledger import ledger

#: The JavaScript half of the contract: the file declaring the shared constants.
PANEL_JS = os.path.join(
    "extensions", "fedora-desktop@fedora-desktop", "statusDocument.js"
)

#: Every panel source that reads the document. The document's keys and the section ids
#: are not all declared in one file — the ids live with the section that registers them —
#: so a name is looked for across the panel rather than in `PANEL_JS` alone.
PANEL_SOURCES = (
    PANEL_JS,
    os.path.join("extensions", "fedora-desktop@fedora-desktop", "sections", "health.js"),
    os.path.join("extensions", "fedora-desktop@fedora-desktop", "extension.js"),
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
        # Both readers demote this section's findings after a reboot, and each has to
        # demote the SAME one. A panel that disagreed with the login report about which
        # section is boot-scoped would present the previous boot's faults as current on
        # exactly the machine the other surface had already corrected.
        "BOOT_SCOPED_SECTION": status_document.BOOT_SCOPED_SECTION,
        "OK": status_document.OK,
        "FINDINGS": status_document.FINDINGS,
        "UNAVAILABLE": status_document.UNAVAILABLE,
        "STATE_DIR_NAME": ledger.STATE_DIR_NAME,
        # Both surfaces offer the same command for the same file. The panel's copy is
        # the one nobody rereads, and a panel offering a program the login report no
        # longer names would put a command that does not exist under a button.
        "HANDOFF_COMMAND": handoff.COMMAND,
    }


def document_keys() -> set[str]:
    """Every key a real document carries — built, not listed.

    A hand-written list of names covers what its author thought of, and then keeps
    passing while the document grows a key the panel never learned to read. Building a
    document with the producer means the set cannot fall behind the producer: add a key
    there and this gate immediately demands the panel mention it.

    Both levels, because the panel reads both: the top-level envelope and the per-section
    shape inside `sections`.
    """
    document = status_document.build(
        sections={login_report.HEALTH: [probe_results.broken("a finding")]},
        kernel="7.2.4-200.fc44.x86_64",
        at="2026-01-01T00:00:00Z",
    )
    keys = set(document)
    for section in document["sections"].values():
        keys.update(section)
    return keys


class _NoFindings(list):
    """A clean answer in either shape a section callable can return.

    `collect_sections` takes one callable per section and they do not agree on a return
    type — one yields a `Report`, the rest yield lists — so a stub has to satisfy both.
    Being an empty list that reports itself as its own `findings` does.
    """

    @property
    def findings(self) -> _NoFindings:
        return self


def section_ids() -> list[str]:
    """The document's section ids, from the seam that names them.

    `sections/health.js` says of exactly these: "These are the document's keys, so they
    are interface: rename one here and the section silently reports unavailable for
    ever." Taken from `collect_sections` rather than re-listed, so the gate reads the
    same names the producer writes.

    The stubs are built by inspecting the signature rather than spelled out. Naming them
    here would couple this gate to the seam's parameter list, so adding a section would
    stop the gate with a TypeError instead of producing the finding that a new section
    the panel does not register is exactly what this exists to report — a gate that
    crashes on the change it is meant to judge.
    """
    parameters = inspect.signature(login_report.collect_sections).parameters
    stubs = {name: (lambda: _NoFindings()) for name in parameters}
    return list(login_report.collect_sections(**stubs))


def unmentioned(javascript: str, names: set[str] | list[str]) -> list[str]:
    """Names that appear nowhere in the JavaScript, as whole words.

    A weaker test than the constant comparison — it asks only that the panel mentions
    the name, not what it does with it — and deliberately so. The strong form would need
    a JS runtime; this catches the failure that actually happens, which is one side
    renaming a key and the other never hearing about it.
    """
    return sorted(
        name
        for name in names
        if not re.search(r"\b" + re.escape(name) + r"\b", javascript)
    )


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


def panel_javascript(root: str) -> str:
    """Every panel source concatenated, for the whole-word name search."""
    chunks: list[str] = []
    for relative in PANEL_SOURCES:
        with open(os.path.join(root, relative), encoding="utf-8") as handle:
            chunks.append(handle.read())
    return "\n".join(chunks)


def check(root: str) -> list[str]:
    """The gate: read the panel's JavaScript and compare it against Python.

    Three comparisons, because the two halves share three kinds of name: the declared
    constants (compared by value, against the file that declares them), the document's
    keys, and the section ids (both only asked to appear, across the whole panel).
    """
    with open(os.path.join(root, PANEL_JS), encoding="utf-8") as handle:
        findings = mismatches(handle.read(), expected())

    javascript = panel_javascript(root)
    for name in unmentioned(javascript, document_keys()):
        findings.append(
            f"PANEL-CONTRACT-FAIL document key {name!r}: written by "
            f"status_document.build and mentioned nowhere in the panel, so the panel "
            f"cannot be reading it. A key the panel does not read is a section that "
            f"renders as though the producer said nothing about it."
        )
    for name in unmentioned(javascript, section_ids()):
        findings.append(
            f"PANEL-CONTRACT-FAIL section id {name!r}: produced by "
            f"login_report.collect_sections and mentioned nowhere in the panel, so that "
            f"section would report `unavailable` for ever — which reads as a host "
            f"nothing has checked."
        )
    return findings


def main(argv: list[str] | None = None) -> int:
    root = (argv or [os.getcwd()])[0]
    findings = check(root)
    for finding in findings:
        sys.stderr.write(f"{finding}\n")
    if findings:
        return 1
    wanted = expected()
    keys, ids = document_keys(), section_ids()
    # Each population named, not just totalled. A bare count cannot show which names a
    # gate compared, so a gap in its coverage would be invisible in a passing run — and
    # the coverage gap is the way a text-matching gate fails.
    print(
        f"PANEL-CONTRACT-OK {len(wanted)} constant(s) agree between "
        f"status_document.py and the panel: {', '.join(sorted(wanted))}; "
        f"{len(keys)} document key(s) present: {', '.join(sorted(keys))}; "
        f"{len(ids)} section id(s) present: {', '.join(sorted(ids))}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
