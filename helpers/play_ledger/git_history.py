"""The git side of the play-freshness check (Plan 00109, Task 2.1).

Refreshes the remote refs (whose age the offline finding reports) and answers whether a
path is in a commit's tree, which the retired-plays rule and `changed_plays` ask.

**Fetch only.** This runs at the end of a login on a machine somebody is using.
Moving their working tree is a Non-Goal of Plan 00109, so nothing here merges,
pulls, checks out, resets or rebases — and `test_git_history.py` asserts those
verbs never reach the argv.

Every call names the repo with `-C`: the caller's cwd is not the checkout.
"""

from __future__ import annotations

import os
import subprocess
from collections.abc import Callable

Runner = Callable[..., subprocess.CompletedProcess]


#: The one genuinely network-bound call on the login path, so the one that needs a
#: bound — and there is no systemd backstop underneath it. `src/core/service.c`
#: carries the comment "Oneshot services have disabled start timeout by default" and
#: sets `timeout_start_usec = USEC_INFINITY` unless the unit names one, so an
#: unbounded hung fetch would hold `graphical-session.target` in `activating` with
#: nothing ever ending it. The unit also sets `TimeoutStartSec=` explicitly, so this
#: does not rest on that default either way.
_FETCH_TIMEOUT_SECONDS = 20


def fetch(repo_root: str, *, run: Runner = subprocess.run) -> None:
    """Refresh remote refs. Never touches the working tree, and never blocks on input.

    `GIT_TERMINAL_PROMPT=0` because a credential prompt on a login path has no
    terminal to appear on: git would wait for an answer nobody can give, and the
    timeout below would then be the only thing that ended the login's last step.
    """
    run(
        ["git", "-C", repo_root, "fetch", "--quiet"],
        check=True,
        capture_output=True,
        text=True,
        timeout=_FETCH_TIMEOUT_SECONDS,
        env={**os.environ, "GIT_TERMINAL_PROMPT": "0", "GIT_ASKPASS": ""},
    )


def path_exists_at(
    repo_root: str, commit: str, path: str, *, run: Runner = subprocess.run
) -> bool:
    """Whether `path` is in the tree of `commit`.

    `ls-tree` prints nothing for an absent path and exits non-zero only when the
    commit itself cannot be read — so "absent" and "git failed" are told apart here,
    and a failure raises. The retired-plays rule
    retires a finding on an absent answer, so a dead clone must not produce one.
    """
    result = run(
        ["git", "-C", repo_root, "ls-tree", "--full-tree", "--name-only", commit, "--", path],
        check=True,
        capture_output=True,
        text=True,
    )
    return path in result.stdout.splitlines()
