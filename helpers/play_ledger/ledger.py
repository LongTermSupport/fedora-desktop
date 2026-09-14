"""Pure logic for the host play-run ledger (Plan 00109, Task 1.2).

The ledger answers the one question the repo cannot answer about itself: which
plays have actually been run on this host, and against which version of
themselves. Every drift check in Phase 2 is a comparison against that answer, so
a ledger that is silently wrong makes every check downstream silently wrong —
which is why nothing here skips a malformed record, and why a reader refuses a
schema it does not understand rather than misreading it.

Design and the reasoning behind each field:
CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md

No I/O. The side-effecting half — the append, the sentinel, the callback plugin
that drives them — is `store.py`.
"""

from __future__ import annotations

import json
import os
import re
from collections.abc import Iterable
from typing import Any

SCHEMA = 1

#: What a play run can have ended as. Anything else is a caller bug, not a state.
OUTCOMES = frozenset({"ok", "failed", "unreachable"})

_COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")
_TIMESTAMP_RE = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$")
_SHA256_RE = re.compile(r"^[0-9a-f]{64}$")

_RUN_FIELDS = (
    "schema",
    "play",
    "name",
    "commit",
    "dirty",
    "play_sha256",
    "outcome",
    "changed",
    "started",
    "finished",
)


#: The directory name under `$XDG_STATE_HOME`. Named rather than inline because the panel
#: is a second process in a second language computing the same path from `GLib`, so this
#: is interface, and `helpers/gnome/check_panel_contract.py` compares the two halves. A
#: disagreement here does not announce itself: the panel would read an absent document and
#: report `unavailable` for ever, which by design reads exactly like a producer that has
#: never run.
STATE_DIR_NAME = "fedora-desktop"


def state_dir(environ: dict[str, str] | Any, home: str) -> str:
    """This host's `fedora-desktop` state directory: `$XDG_STATE_HOME/fedora-desktop`.

    Host state, never repo state — it must not be committable, and it must
    survive a re-clone, because a re-clone must not reset the host's memory of
    what has been run on it.

    The ledger is one thing kept here; `host_health.status_document` is another. Both
    resolve the location through this function rather than each spelling out the XDG
    rule, so a host cannot end up with two state trees because one of them fell back
    differently.
    """
    state_home = (environ.get("XDG_STATE_HOME") or "").strip()
    if state_home and not state_home.startswith("/"):
        raise ValueError(
            f"XDG_STATE_HOME={state_home!r} is not absolute; this host's state location "
            "would then depend on the cwd of whoever ran the play"
        )
    if not state_home:
        state_home = os.path.join(home, ".local", "state")
    return os.path.join(state_home, STATE_DIR_NAME)


def ledger_dir(environ: dict[str, str] | Any, home: str) -> str:
    """The ledger directory: `$XDG_STATE_HOME/fedora-desktop/play-ledger`."""
    return os.path.join(state_dir(environ, home), "play-ledger")


def runs_path(base: str) -> str:
    """The append-only record file."""
    return os.path.join(base, "runs.jsonl")


def sentinel_path(base: str) -> str:
    """The marker that says the ledger has a hole and must not be trusted.

    A callback cannot fail an Ansible run — Ansible catches exceptions raised
    inside one and carries on — so a write failure is RECORDED here instead, and
    Phase 2's checks refuse to answer while it exists. That turns an unfailable
    hook into a failable check.
    """
    return os.path.join(base, "BROKEN")


def _require(condition: bool, message: str) -> None:
    if not condition:
        raise ValueError(message)


def build_record(
    *,
    play: str,
    name: str,
    commit: str,
    dirty: bool,
    play_sha256: str,
    outcome: str,
    changed: int,
    started: str,
    finished: str,
) -> dict[str, Any]:
    """One validated run record. Every constraint here is a thing Phase 2 relies on."""
    _require(bool(play) and not play.startswith("/"), f"play must be repo-relative, got {play!r}")
    _require(outcome in OUTCOMES, f"unknown outcome {outcome!r}; expected one of {sorted(OUTCOMES)}")
    _require(bool(_COMMIT_RE.match(commit)), f"commit must be a 40-hex sha, got {commit!r}")
    _require(bool(_SHA256_RE.match(play_sha256)), f"play_sha256 must be a 64-hex sha, got {play_sha256!r}")
    # A string here is truthy, so every run would record as dirty and the reason
    # play_sha256 exists at all would become unreadable.
    _require(isinstance(dirty, bool), f"dirty must be a bool, got {type(dirty).__name__}")
    _require(isinstance(changed, int) and not isinstance(changed, bool), "changed must be an int")
    for label, value in (("started", started), ("finished", finished)):
        _require(bool(_TIMESTAMP_RE.match(value)), f"{label} must be UTC ISO 8601, got {value!r}")

    return {
        "schema": SCHEMA,
        "play": play,
        "name": name,
        "commit": commit,
        "dirty": dirty,
        "play_sha256": play_sha256,
        "outcome": outcome,
        "changed": changed,
        "started": started,
        "finished": finished,
    }


def genesis_record(*, commit: str, at: str) -> dict[str, Any]:
    """The record written when the ledger is created.

    Nothing is backfilled: a play with no record has never been run here, and
    silence is the correct output for it. This record is what makes that silence
    unambiguous — without it, "no record" cannot be told apart from "run before
    the ledger existed", and every silence would be a guess.
    """
    _require(bool(_COMMIT_RE.match(commit)), f"commit must be a 40-hex sha, got {commit!r}")
    _require(bool(_TIMESTAMP_RE.match(at)), f"at must be UTC ISO 8601, got {at!r}")
    return {"schema": SCHEMA, "kind": "genesis", "commit": commit, "at": at}


def serialise(record: dict[str, Any]) -> str:
    """One record as one line. Keys sorted so two runs diff cleanly."""
    return json.dumps(record, sort_keys=True, separators=(",", ":")) + "\n"


def fold_latest(lines: Iterable[str]) -> dict[str, dict[str, Any]]:
    """The most recent run record per play.

    Decided by `finished`, not by position: the file is appended to by concurrent
    runs, so its order is not run order.

    A corrupt line or an unknown schema raises. Skipping either would let the
    fold answer from a history it knows is incomplete, which is precisely the
    silently-wrong-check failure this ledger exists to prevent.
    """
    latest: dict[str, dict[str, Any]] = {}
    for number, line in enumerate(lines, start=1):
        stripped = line.strip()
        if not stripped:
            continue
        try:
            record = json.loads(stripped)
        except json.JSONDecodeError as error:
            raise ValueError(
                f"ledger line {number} is not valid JSON ({error}); the ledger has a "
                "hole and cannot be read as complete"
            ) from error

        schema = record.get("schema")
        if schema != SCHEMA:
            raise ValueError(
                f"ledger line {number} declares schema {schema!r}, this reader "
                f"understands {SCHEMA}; refusing to misread it"
            )
        if record.get("kind") == "genesis":
            continue

        play = record.get("play")
        if not isinstance(play, str) or not play:
            raise ValueError(f"ledger line {number} has no 'play'")
        seen = latest.get(play)
        if seen is None or record.get("finished", "") > seen.get("finished", ""):
            latest[play] = record
    return latest


def run_fields() -> tuple[str, ...]:
    """The field names of a run record, for callers asserting the shape."""
    return _RUN_FIELDS
