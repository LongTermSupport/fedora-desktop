"""Git and filesystem facts for one play-ledger record (Plan 00109, Task 1.2).

Three of a record's fields describe the checkout the play ran from rather than
the play itself: `commit`, `dirty` and `play_sha256`. They are gathered here so
the Ansible callback plugin stays a pure adapter — `ansible` is not importable
by the interpreter that runs the tests, so anything left in the plugin is
untested by construction.

Every git call names the repo root with `-C`. A callback's working directory is
wherever the operator happened to be, and asking git about that would answer
about a different repository or none.

Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-play-ledger.md
"""

from __future__ import annotations

import datetime
import hashlib
import re
import subprocess
from collections.abc import Callable

#: Read size for hashing. A play file is small; this just avoids assuming so.
CHUNK_BYTES = 65536

_COMMIT_RE = re.compile(r"^[0-9a-f]{40}$")

Runner = Callable[..., subprocess.CompletedProcess]


def _git(repo_root: str, arguments: list[str], run: Runner) -> str:
    result = run(
        ["git", "-C", repo_root, *arguments],
        check=True,
        capture_output=True,
        text=True,
    )
    return result.stdout


def head_commit(repo_root: str, *, run: Runner = subprocess.run) -> str:
    """The full 40-hex HEAD sha.

    Abbreviated output is refused rather than accepted: Phase 2 joins records to
    commits by exact match, and a short sha would fail every one of those joins
    while looking like a valid value.
    """
    commit = _git(repo_root, ["rev-parse", "HEAD"], run).strip()
    if not _COMMIT_RE.match(commit):
        raise ValueError(f"git rev-parse HEAD in {repo_root!r} gave {commit!r}, not a 40-hex sha")
    return commit


def is_dirty(repo_root: str, *, run: Runner = subprocess.run) -> bool:
    """Whether the checkout has uncommitted changes, untracked files included.

    Untracked counts deliberately: a play file that exists only in the working
    tree is exactly the case `play_sha256` was added to catch, and calling that
    checkout clean would let the record claim the commit describes it.
    """
    return bool(_git(repo_root, ["status", "--porcelain"], run).strip())


def sha256_file(path: str) -> str:
    """The play file's hash, as executed. A missing file raises."""
    digest = hashlib.sha256()
    with open(path, "rb") as handle:
        while chunk := handle.read(CHUNK_BYTES):
            digest.update(chunk)
    return digest.hexdigest()


def utc_now() -> str:
    """Now, in the one timestamp format `ledger.build_record` accepts."""
    return datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
