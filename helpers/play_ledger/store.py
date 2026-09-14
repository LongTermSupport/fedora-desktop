"""The play ledger's filesystem half (Plan 00109, Task 1.2).

Creation, appends, and the `BROKEN` sentinel. Every function here raises on
failure — the caller that cannot afford to raise is the Ansible callback plugin,
and its job is to catch and call `mark_broken`, because Ansible swallows an
exception raised inside a callback and would otherwise let the run finish green
with nothing written.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
Pure record logic: `ledger.py`.
"""

from __future__ import annotations

import os
from typing import Any

from helpers.play_ledger import ledger

DIR_MODE = 0o700
FILE_MODE = 0o600


def ensure_ledger(base: str, *, commit: str, at: str) -> None:
    """Create the ledger if absent, writing the genesis record exactly once.

    The genesis record is what lets a reader tell "no record because this play was
    never run here" from "no record because the ledger is younger than the run".
    Writing a second one would reset the ledger's recorded age and make every
    silence ambiguous again, so an existing runs file is left alone.
    """
    os.makedirs(base, mode=DIR_MODE, exist_ok=True)
    runs = ledger.runs_path(base)
    if os.path.exists(runs):
        return

    # O_EXCL so two concurrent first runs cannot both write a genesis record.
    try:
        handle = os.open(runs, os.O_WRONLY | os.O_CREAT | os.O_EXCL, FILE_MODE)
    except FileExistsError:
        return
    with os.fdopen(handle, "w", encoding="utf-8") as fh:
        fh.write(ledger.serialise(ledger.genesis_record(commit=commit, at=at)))


def append_record(base: str, record: dict[str, Any]) -> None:
    """Append one record. Raises if the ledger does not exist.

    Creating it here instead would lose the genesis record, and with it the
    ability to tell "never run" from "run before the ledger existed".

    Serialisation happens BEFORE the file is opened, so a record that cannot be
    written leaves the file untouched rather than half-written.
    """
    line = ledger.serialise(record)
    runs = ledger.runs_path(base)
    if not os.path.exists(runs):
        raise OSError(
            f"{runs} does not exist — call ensure_ledger() first; creating it here "
            "would lose the genesis record the reader needs to date its silences"
        )
    # O_APPEND: concurrent runs interleave whole lines rather than corrupting one.
    handle = os.open(runs, os.O_WRONLY | os.O_APPEND)
    with os.fdopen(handle, "a", encoding="utf-8") as fh:
        fh.write(line)


def mark_broken(base: str, *, error: str, at: str) -> None:
    """Record that the ledger has a hole, keeping the FIRST reason.

    Deliberately independent of the ledger existing: "could not create the ledger
    at all" is exactly a failure that must be recordable.

    The first failure is the one that explains the hole; later ones are usually
    consequences of it, and overwriting would lose the cause.
    """
    os.makedirs(base, mode=DIR_MODE, exist_ok=True)
    sentinel = ledger.sentinel_path(base)
    try:
        handle = os.open(sentinel, os.O_WRONLY | os.O_CREAT | os.O_EXCL, FILE_MODE)
    except FileExistsError:
        return
    with os.fdopen(handle, "w", encoding="utf-8") as fh:
        fh.write(f"{at} {error}\n")


def broken_reason(base: str) -> str | None:
    """Why the ledger cannot be trusted, or None. Phase 2 reads this FIRST."""
    sentinel = ledger.sentinel_path(base)
    if not os.path.exists(sentinel):
        return None
    with open(sentinel, encoding="utf-8") as fh:
        return fh.read().strip()


def clear_broken(base: str) -> None:
    """Forget a recorded hole. Deliberate and explicit — never automatic."""
    sentinel = ledger.sentinel_path(base)
    if os.path.exists(sentinel):
        os.unlink(sentinel)


def read_lines(base: str) -> list[str]:
    """Every line in the ledger, genesis included. An absent ledger reads as none.

    "No ledger yet" is a state a reader must handle — it means nothing has been
    run here since it would have been created — and is not an error.
    """
    runs = ledger.runs_path(base)
    if not os.path.exists(runs):
        return []
    with open(runs, encoding="utf-8") as fh:
        return fh.readlines()
