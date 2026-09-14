"""Every decision the play-ledger callback plugin defers to a tested module.

`ansible` is not importable by the interpreter that runs this repo's tests, so
logic left inside `callback_plugins/play_ledger.py` would be logic with no tests
at all — in the component every Phase 2 drift check trusts. The plugin is
therefore a pure adapter: it translates Ansible's callback events into calls on
`RunCollector` and the functions here, and decides nothing itself.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import os
from typing import Any

from helpers.play_ledger import store

#: Printed to stderr when the ledger cannot be written. Ansible swallows an
#: exception raised inside a callback, so this line and the BROKEN sentinel are
#: the only two ways the failure ever surfaces.
FAILURE_MARKER = "LEDGER-WRITE-FAILED"

#: CLI flags that mean the run applied nothing. Recording one of these would tell
#: Phase 2 the play is fresh on a host that never received it — which is the
#: precise failure this whole plan was written to catch.
_NO_OP_FLAGS = ("check", "listtasks", "listhosts", "listtags", "syntax")


def should_record(cliargs: dict[str, Any]) -> bool:
    """Whether this invocation actually applied anything worth ledgering."""
    return not any(bool(cliargs.get(flag)) for flag in _NO_OP_FLAGS)


def play_source(position: Any) -> str:
    """The play's source file, from Ansible's `(file, line, column)` position.

    Refuses to invent a path. A record with no `play` cannot be joined to
    anything, so a missing position must become a recorded hole rather than an
    unusable row nobody notices.
    """
    if not position:
        raise ValueError(
            "the play carries no source position, so its file cannot be named; "
            "the ledger will not record a play it cannot identify"
        )
    filename = position[0]
    if not isinstance(filename, str) or not filename:
        raise ValueError(f"play source position {position!r} has no usable filename")
    return filename


def repo_root_from(plugin_file: str) -> str:
    """The checkout root, derived from the plugin's own location.

    Never from the cwd: a callback runs wherever the operator happened to be, and
    `playbooks/` may be reached by an absolute path from anywhere.
    """
    return os.path.dirname(os.path.dirname(os.path.realpath(plugin_file)))


def write_records(
    base: str, records: list[dict[str, Any]], *, commit: str, at: str
) -> None:
    """Create the ledger if needed, then append each record. Raises on any failure.

    The ledger is created even when there are no records: that write is what dates
    it, and without a date a later silence cannot be told from "the ledger did not
    exist when that play ran".

    A failure part-way through leaves the earlier records written, deliberately.
    They are true, and the caller's `record_failure` sentinel is what tells Phase 2
    the run is incomplete — a partial history plus a known hole beats discarding
    rows that actually happened.
    """
    store.ensure_ledger(base, commit=commit, at=at)
    for record in records:
        store.append_record(base, record)


def record_failure(base: str, *, error: str, at: str) -> str:
    """Record a ledger hole and return the line the plugin must print to stderr.

    This is the last resort, so it does not raise — a raise here would be
    swallowed by Ansible exactly like the failure it is reporting, and the
    operator would see nothing at all. If even the sentinel cannot be written,
    the returned line says so and carries the original cause.
    """
    try:
        store.mark_broken(base, error=error, at=at)
    except OSError as sentinel_error:
        return (
            f"{FAILURE_MARKER}: {error} — and the BROKEN sentinel at {base} could not "
            f"be written either ({sentinel_error}), so nothing on disk records this"
        )
    return f"{FAILURE_MARKER}: {error} — recorded in {store.ledger.sentinel_path(base)}"
