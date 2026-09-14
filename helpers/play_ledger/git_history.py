"""The git side of the play-freshness check (Plan 00109, Task 2.1).

Answers, for one play, what `freshness.classify` needs: which commits have touched
it since the run the ledger recorded, and what the file hashes to at HEAD.

**Fetch only.** This runs at the end of a login on a machine somebody is using.
Moving their working tree is a Non-Goal of Plan 00109, so nothing here merges,
pulls, checks out, resets or rebases — and `test_git_history.py` asserts those
verbs never reach the argv.

Every call names the repo with `-C`: the caller's cwd is not the checkout.
"""

from __future__ import annotations

import hashlib
import subprocess
from collections.abc import Callable

Runner = Callable[..., subprocess.CompletedProcess]

#: Separator between short sha and subject. A unit separator cannot occur in a
#: sha and is vanishingly unlikely in a subject — and if one does contain it, the
#: split below still keeps the subject whole.
_FIELD_SEP = "\x1f"


def fetch(repo_root: str, *, run: Runner = subprocess.run) -> None:
    """Refresh remote refs. Never touches the working tree."""
    run(
        ["git", "-C", repo_root, "fetch", "--quiet"],
        check=True,
        capture_output=True,
        text=True,
    )


def changes_since(
    repo_root: str, commit: str, play: str, *, run: Runner = subprocess.run
) -> list[tuple[str, str]]:
    """`(short_sha, subject)` for each commit touching `play` in `commit..HEAD`.

    Raises if git cannot resolve the range — a ledgered commit missing from this
    clone (a dropped branch, a shallow clone) exits non-zero, and reading that as
    "no commits touched it" would report every such play fresh for ever.
    """
    result = run(
        ["git", "-C", repo_root, "log", f"--format=%h{_FIELD_SEP}%s", f"{commit}..HEAD", "--", play],
        check=True,
        capture_output=True,
        text=True,
    )
    commits: list[tuple[str, str]] = []
    for line in result.stdout.splitlines():
        if not line.strip():
            continue
        # Split once: a subject is free text and may contain anything.
        short, separator, subject = line.partition(_FIELD_SEP)
        if not separator:
            raise ValueError(
                f"git log line {line!r} has no {_FIELD_SEP!r} separator; refusing to "
                "under-report churn by skipping it"
            )
        commits.append((short, subject))
    return commits


def play_sha256_at_head(
    repo_root: str, play: str, *, run: Runner = subprocess.run
) -> str | None:
    """The play file's hash at HEAD, or None if it is not there any more.

    None is the GONE signal — distinct from a hash that merely differs, and the
    difference decides whether "re-run this play" is sane advice.

    Bytes, not text: the ledger's hash is over the file's bytes, and letting Python
    decode and re-encode would diverge on any line ending or encoding surprise.
    """
    try:
        result = run(
            ["git", "-C", repo_root, "show", f"HEAD:{play}"],
            check=True,
            capture_output=True,
        )
    except subprocess.CalledProcessError:
        return None
    return hashlib.sha256(result.stdout).hexdigest()
