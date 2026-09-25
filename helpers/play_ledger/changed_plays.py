"""Which plays run here have to run again, for `run.bash --changed` (Plan 00141).

A play has to run again when any of its inputs changed between the commit it last ran
from and the working tree, when that last run did not succeed, or when it ran from a
checkout with uncommitted changes. Its inputs are its own file and every checkout path it
deploys, as `helpers/self_update/affected_plays.py` reads them from the play text. The
ledger's own freshness check watches only the play file, so a change to a lib or a
template a play copies would leave that play reading as current. A dirty run is run again
because what it deployed may be an edit since reverted, which no diff can show.

Run it:

    python3 -m helpers.play_ledger.changed_plays [--repo-root DIR]

stdout carries only marker lines, in the order the plays should run: the order
`playbook-main.yml` imports them, then the plays it does not import, by path.

    RUN <play>                  run it again
    UNRESOLVED <play> <where>   not changed as far as can be told, but it holds a reference
                                the mapper cannot follow, so a change there would be missed
    GONE <play>                 it ran here, and the checkout no longer has it

Only host plays count, the ones under `playbooks/imports/`. A probe under untracked/ or a
play under `playbooks/dev/`, which works on the repo, is never mentioned. Nor is a play
the repo never tracked at the commit it ran from. Exit 0 is an answer, empty or not.
Exit 2 is no answer: a broken or unreadable ledger, or a git call that failed. Then
stdout is empty, because a partial list offered as the whole one would leave plays
silently unrun.
"""

from __future__ import annotations

import argparse
import os
import posixpath
import re
import subprocess
import sys
from collections.abc import Callable
from typing import TextIO

from helpers.play_ledger import git_history, ledger, store
from helpers.self_update import affected_plays

EXIT_OK = 0
EXIT_NO_ANSWER = 2

MAIN_PLAYBOOK = "playbooks/playbook-main.yml"
_IMPORT = re.compile(r"^-\s*import_playbook:\s*(\S+)\s*$")


def main_order(repo_root: str) -> list[str]:
    """The plays `playbook-main.yml` imports, in its order, as repo-relative paths.

    That order carries real dependencies (play-ZZ-repo-cleanup runs after the plays that
    add COPRs), so a catch-up run follows it. A missing file raises.
    """
    with open(os.path.join(repo_root, MAIN_PLAYBOOK), encoding="utf-8") as handle:
        text = handle.read()
    here = posixpath.dirname(MAIN_PLAYBOOK)
    return [
        posixpath.normpath(posixpath.join(here, match.group(1)))
        for match in map(_IMPORT.match, text.splitlines())
        if match
    ]


def changed_since(repo_root: str, commit: str) -> list[str]:
    """Paths that differ between `commit` and the working tree, committed or not.

    `--no-renames`, so a moved file names both ends. Untracked files are not seen: a
    play only deploys what the checkout tracks.
    """
    if not commit or commit.startswith("-"):
        raise ValueError(f"{commit!r} is not a commit")
    result = subprocess.run(
        ["git", "-C", repo_root, "diff", "--name-only", "--no-renames", "-z", commit, "--"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise ValueError(f"git diff {commit} failed: {result.stderr.strip()}")
    return [path for path in result.stdout.split("\0") if path]


def run(
    *,
    base: str,
    repo_root: str,
    stdout: TextIO,
    stderr: TextIO,
    changed_since: Callable[[str, str], list[str]] = changed_since,
    exists_now: Callable[[str, str], bool] | None = None,
    tracked_at: Callable[[str, str, str], bool] | None = None,
    inputs_of: Callable[[str, str], affected_plays.Inputs] | None = None,
    main_order: Callable[[str], list[str]] = main_order,
) -> int:
    """Print the marker lines and return the exit status. The callables are test seams."""
    exists_now = exists_now or (lambda root, play: os.path.isfile(os.path.join(root, play)))
    tracked_at = tracked_at or (
        lambda root, commit, play: git_history.path_exists_at(root, commit, play)
    )
    inputs_of = inputs_of or affected_plays.play_inputs

    broken = store.broken_reason(base)
    if broken is not None:
        stderr.write(f"changed-plays: the play ledger has a recorded hole, so it cannot say what ran: {broken}\n")
        return EXIT_NO_ANSWER
    try:
        latest = ledger.fold_latest(store.read_lines(base))
    except ValueError as error:
        stderr.write(f"changed-plays: the ledger cannot be read: {error}\n")
        return EXIT_NO_ANSWER

    try:
        position = {play: index for index, play in enumerate(main_order(repo_root))}
    except OSError as error:
        stderr.write(f"changed-plays: cannot read the play order from {MAIN_PLAYBOOK}: {error}\n")
        return EXIT_NO_ANSWER
    host_plays = [play for play in latest if play.startswith(affected_plays.CANDIDATE_DIR + "/")]
    host_plays.sort(key=lambda play: (play not in position, position.get(play, 0), play))

    lines: list[str] = []
    diffs: dict[str, list[str]] = {}
    for play in host_plays:
        record = latest[play]
        commit = record["commit"]
        try:
            if not exists_now(repo_root, play):
                if tracked_at(repo_root, commit, play):
                    lines.append(f"GONE {play}")
                continue
            if commit not in diffs:
                diffs[commit] = changed_since(repo_root, commit)
            inputs = inputs_of(repo_root, play)
        except Exception as error:
            stderr.write(
                f"changed-plays: cannot judge {play}, which last ran from {commit}: {error}\n"
                f"changed-plays: run it by name instead: ./run.bash {play}\n"
            )
            return EXIT_NO_ANSWER
        if (
            record["outcome"] != "ok"
            or record["dirty"]
            or any(affected_plays.affects(inputs, path) for path in diffs[commit])
        ):
            lines.append(f"RUN {play}")
            continue
        lines.extend(f"UNRESOLVED {play} {where}" for where in inputs.unresolved)

    for line in lines:
        stdout.write(f"{line}\n")
    return EXIT_OK


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/play_ledger/changed_plays.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="List the plays run here whose inputs changed since (run.bash --changed)."
    )
    parser.add_argument("--repo-root", default=_repo_root_default())
    arguments = parser.parse_args(argv)
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
    return run(base=base, repo_root=arguments.repo_root, stdout=sys.stdout, stderr=sys.stderr)


if __name__ == "__main__":
    sys.exit(main())
