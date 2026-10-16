"""Everything the play-ledger callback plugin decides (Plan 00109, Task 1.2).

The Ansible callback plugin is a thin adapter over this class: it translates
callback events into `on_play_start` / `on_result` / `on_end` and writes what it
gets back. Nothing in the plugin decides anything.

That split is not decoration. Helpers here are stdlib-only, and `ansible` is not
importable by the interpreter that runs the tests, so logic left in the plugin
would be logic with no tests at all — in a component whose whole job is to be
trusted by every drift check downstream.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import os
from collections.abc import Callable
from typing import Any, NamedTuple

from helpers.play_ledger import ledger


class ResolvedPlay(NamedTuple):
    """A play path resolved against the repo root.

    `tracked` is False for a play outside the root; `relative` is then empty.
    """

    absolute: str
    relative: str
    tracked: bool

#: Worst-wins. `unreachable` outranks `failed` because a host that could not be
#: reached did not run the play at all, and recording "failed" would claim it ran
#: and did not work.
_SEVERITY = {"ok": 0, "failed": 1, "unreachable": 2}

#: Opens the stderr line for a play skipped because it is outside the repo root.
NOT_TRACKED_MARKER = "LEDGER-PLAY-NOT-TRACKED"


class RunCollector:
    """Accumulates per-play state across one `ansible-playbook` run."""

    def __init__(
        self,
        *,
        commit: str,
        dirty: bool,
        hash_play: Callable[[str], str],
        repo_root: str,
    ) -> None:
        self._commit = commit
        self._dirty = dirty
        self._hash_play = hash_play
        self._repo_root = os.path.realpath(repo_root)
        self._records: list[dict[str, Any]] = []
        self._open: dict[str, Any] | None = None

    def on_play_start(self, *, play_path: str, name: str, at: str) -> str | None:
        """Close the play in progress, if any, and open a new one.

        One record per PLAY, never per playbook run: `playbook-main.yml` imports
        many plays, and a single record saying "everything ran" would lose exactly
        the axis Phase 2 needs.

        Returns None, or the one line the caller must print to stderr when the play
        is outside the repo root and so is not tracked (see `_resolve`). Such a play
        opens nothing, so its results fold into no record.
        """
        self._close(at)
        play = self._resolve(play_path)
        if not play.tracked:
            return (
                f"{NOT_TRACKED_MARKER}: play {play.absolute!r} is outside the repo root "
                f"{self._repo_root!r}, so it is not tracked and the ledger records nothing for it"
            )
        self._open = {
            "absolute": play.absolute,
            "relative": play.relative,
            "name": name,
            "started": at,
            "outcome": "ok",
            "changed": 0,
        }
        return None

    def on_result(self, *, outcome: str, changed: bool) -> None:
        """Fold one task result into the play in progress."""
        if outcome not in _SEVERITY:
            raise ValueError(
                f"unknown result state {outcome!r}; expected one of {sorted(_SEVERITY)}"
            )
        if self._open is None:
            return
        if _SEVERITY[outcome] > _SEVERITY[self._open["outcome"]]:
            self._open["outcome"] = outcome
        if changed:
            self._open["changed"] += 1

    def on_end(self, *, at: str) -> list[dict[str, Any]]:
        """Close the last play and return every record, then forget them.

        Idempotent on purpose: the plugin may see both a stats event and an
        explicit close, and emitting the final play twice would double-count it in
        every freshness report thereafter.
        """
        self._close(at)
        records, self._records = self._records, []
        return records

    def _close(self, at: str) -> None:
        if self._open is None:
            return
        play = self._open
        self._open = None
        self._records.append(
            ledger.build_record(
                play=play["relative"],
                name=play["name"],
                commit=self._commit,
                dirty=self._dirty,
                play_sha256=self._hash_play(play["absolute"]),
                outcome=play["outcome"],
                changed=play["changed"],
                started=play["started"],
                finished=at,
            )
        )

    def _resolve(self, play_path: str) -> ResolvedPlay:
        """The play's real path, and whether it is tracked, from Ansible's `<file>:<line>` form.

        A play outside the root comes back with `tracked=False`: not an error, a play
        the ledger does not track. It cannot be compared against this repo's
        HEAD, so a row for it would be unanswerable — the same reasoning as
        `check_freshness._drop_never_tracked`. A private play run from a linked
        worktree is the usual case: it lives in the main checkout's untracked/.

        Inside the root the play is in scope even when git does not track it (a
        private play under the main checkout's untracked/): commit, dirty and
        play_sha256 still describe it, and check_freshness drops it as never
        tracked. The test is on the real path, so a private directory that is a
        symlink to somewhere outside the root is outside too. Every other failure
        here propagates, so the plugin marks BROKEN.
        """
        path = play_path
        head, separator, tail = path.rpartition(":")
        if separator and tail.isdigit():
            path = head

        absolute = os.path.realpath(path)
        prefix = self._repo_root + os.sep
        if not absolute.startswith(prefix):
            return ResolvedPlay(absolute=absolute, relative="", tracked=False)
        return ResolvedPlay(absolute=absolute, relative=absolute[len(prefix) :], tracked=True)
