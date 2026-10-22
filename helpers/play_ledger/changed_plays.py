"""Which plays run here have to run again, for `run.bash --changed` (Plan 00141).

A play has to run again when any of its inputs changed between the commit it last ran
from and the working tree, when that last run did not succeed, or when it ran from a
checkout with uncommitted changes. Its inputs are its own file and every checkout path it
deploys, as `helpers/self_update/affected_plays.py` reads them from the play text. A
dirty run is run again because what it deployed may be an edit since reverted, which no
diff can show.

Run it:

    python3 -m helpers.play_ledger.changed_plays [--repo-root DIR]

stdout carries only marker lines, in the order the plays should run: the order
`playbook-main.yml` imports them, then the plays it does not import, by path.

    RUN <play>                  run it again
    UNRESOLVED <play> <where>   not changed as far as can be told, but it holds a reference
                                the mapper cannot follow, so a change there would be missed
    GONE <play>                 it ran here, and the checkout no longer has it

A removed play that `retired-plays.json` maps to a successor is not GONE: the successor
is RUN until a successful run of it, at a commit without the old play, has taken it over.

Only host plays count, the ones under `playbooks/imports/`. A probe under untracked/ or a
play under `playbooks/dev/`, which works on the repo, is never mentioned. Nor is a play
the repo never tracked at the commit it ran from. Exit 0 is an answer, empty or not.
Exit 2 is no answer: a broken or unreadable ledger, or a git call that failed. Then
stdout is empty, because a partial list offered as the whole one would leave plays
silently unrun.

`judge` is THE definition of which plays have to run again (Plan 00214, issue #110). The
health report's play-freshness check (`check_freshness`) calls it too, so the report and
`run.bash --changed` cannot name different plays.
"""

from __future__ import annotations

import argparse
import os
import posixpath
import re
import subprocess
import sys
from collections.abc import Callable
from dataclasses import dataclass
from typing import TextIO

from helpers.play_ledger import git_history, ledger, retired, store
from helpers.self_update import affected_plays

EXIT_OK = 0
EXIT_NO_ANSWER = 2

#: The states `--all` prints. STALE is changed inputs or a dirty run; FAILED a last run that
#: did not succeed; UNRESOLVED has a reference the mapper cannot follow; CURRENT is none.
STATE_STALE = "stale"
STATE_FAILED = "failed"
STATE_UNRESOLVED = "unresolved"
STATE_CURRENT = "current"

#: The states `run.bash --changed` runs.
RUN_STATES = (STATE_STALE, STATE_FAILED)

MAIN_PLAYBOOK ="playbooks/playbook-main.yml"
_IMPORT_KEY = re.compile(r"^\s*-\s*(?:ansible\.builtin\.)?import_playbook:")
_IMPORT = re.compile(r"^\s*-\s*(?:ansible\.builtin\.)?import_playbook:\s*([\w./-]+\.ya?ml)\s*(?:#.*)?$")


class NoAnswer(Exception):
    """No play can be judged. `lines` say why, each one sentence, the remedy last."""

    def __init__(self, *lines: str) -> None:
        super().__init__(" ".join(lines))
        self.lines = lines


@dataclass(frozen=True)
class Judgement:
    """Every host play run here, judged. The one answer both callers read.

    `order` is every play in `states` or `gone`, in the order a catch-up runs them.
    `changed_inputs` holds, for a stale play, the changed checkout paths among its inputs
    (none when it is stale only because it ran dirty, which `dirty` says). `took_over`
    names, for a successor that has to run, the removed plays it takes over. `not_judged`
    is every ledgered play outside `playbooks/imports/`, which no catch-up runs.
    """

    order: tuple[str, ...]
    states: dict[str, str]
    gone: tuple[str, ...]
    unresolved: dict[str, list[str]]
    changed_inputs: dict[str, list[str]]
    dirty: frozenset[str]
    took_over: dict[str, list[str]]
    not_judged: tuple[str, ...]

    @property
    def runs(self) -> list[str]:
        """The plays `run.bash --changed` runs, in the order it runs them."""
        return [play for play in self.order if self.states.get(play) in RUN_STATES]


def main_order(repo_root: str) -> list[str]:
    """The plays `playbook-main.yml` imports, in its order, as repo-relative paths.

    That order carries real dependencies (play-ZZ-repo-cleanup runs after the plays that
    add COPRs), so a catch-up run follows it. An import line it cannot read raises,
    because the play it names would drop to the end of the run without a word. A missing
    file raises.
    """
    with open(os.path.join(repo_root, MAIN_PLAYBOOK), encoding="utf-8") as handle:
        text = handle.read()
    here = posixpath.dirname(MAIN_PLAYBOOK)
    order = []
    for number, line in enumerate(text.splitlines(), start=1):
        if not _IMPORT_KEY.match(line):
            continue
        match = _IMPORT.match(line)
        if match is None:
            raise ValueError(f"{MAIN_PLAYBOOK}:{number}: cannot read the play this imports: {line.strip()}")
        order.append(posixpath.normpath(posixpath.join(here, match.group(1))))
    return order


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


def judge(
    latest: dict[str, dict],
    *,
    repo_root: str,
    changed_since: Callable[[str, str], list[str]] = changed_since,
    exists_now: Callable[[str, str], bool] | None = None,
    tracked_at: Callable[[str, str, str], bool] | None = None,
    inputs_of: Callable[[str, str], affected_plays.Inputs] | None = None,
    main_order: Callable[[str], list[str]] = main_order,
    retired_plays: Callable[[str], dict[str, str]] = retired.load,
) -> Judgement:
    """Judge every host play in `latest` (the ledger folded to each play's last run).

    Raises `NoAnswer` rather than return a partial judgement. The callables are test seams.
    """
    exists_now = exists_now or (lambda root, play: os.path.isfile(os.path.join(root, play)))
    tracked_at = tracked_at or (
        lambda root, commit, play: git_history.path_exists_at(root, commit, play)
    )
    inputs_of = inputs_of or affected_plays.play_inputs

    try:
        position = {play: index for index, play in enumerate(main_order(repo_root))}
    except (OSError, ValueError) as error:
        raise NoAnswer(f"cannot read the play order from {MAIN_PLAYBOOK}: {error}") from error

    def run_order(play: str) -> tuple[bool, int, str]:
        return (play not in position, position.get(play, 0), play)

    prefix = affected_plays.CANDIDATE_DIR + "/"
    host_plays = sorted((play for play in latest if play.startswith(prefix)), key=run_order)

    gone: list[str] = []
    unresolved: dict[str, list[str]] = {}
    diffs: dict[str, list[str]] = {}
    states: dict[str, str] = {}
    changed_inputs: dict[str, list[str]] = {}
    for play in host_plays:
        record = latest[play]
        commit = record["commit"]
        try:
            if not exists_now(repo_root, play):
                if tracked_at(repo_root, commit, play):
                    gone.append(play)
                continue
            if commit not in diffs:
                diffs[commit] = changed_since(repo_root, commit)
            inputs = inputs_of(repo_root, play)
        except Exception as error:
            raise NoAnswer(
                f"cannot judge {play}, which last ran from {commit}: {error}",
                f"run it by name instead: ./run.bash {play}",
            ) from error
        if record["outcome"] != "ok":
            states[play] = STATE_FAILED
            continue
        changed = [path for path in diffs[commit] if affected_plays.affects(inputs, path)]
        if record["dirty"] or changed:
            states[play] = STATE_STALE
            changed_inputs[play] = changed
            continue
        if inputs.unresolved:
            unresolved[play] = list(inputs.unresolved)
            states[play] = STATE_UNRESOLVED
        else:
            states[play] = STATE_CURRENT

    try:
        gone, took_over = _retire(gone, latest=latest, repo_root=repo_root,
                                  retired_plays=retired_plays, tracked_at=tracked_at)
    except Exception as error:
        raise NoAnswer(
            f"cannot judge the removed plays against the retired-plays map: {error}"
        ) from error
    for successor in took_over:
        if states.get(successor) != STATE_FAILED:
            states[successor] = STATE_STALE
            changed_inputs.setdefault(successor, [])
            unresolved.pop(successor, None)

    return Judgement(
        order=tuple(sorted(set(states) | set(gone), key=run_order)),
        states=states,
        gone=tuple(gone),
        unresolved=unresolved,
        changed_inputs=changed_inputs,
        dirty=frozenset(play for play in states if latest.get(play, {}).get("dirty")),
        took_over=took_over,
        not_judged=tuple(sorted(play for play in latest if not play.startswith(prefix))),
    )


def run(
    *,
    base: str,
    repo_root: str,
    stdout: TextIO,
    stderr: TextIO,
    list_all: bool = False,
    **seams: Callable,
) -> int:
    """Print the marker lines and return the exit status. `seams` go to `judge`.

    `list_all` prints every play run here instead, as `PLAY <state> <play>` (the judgement
    above, for the menu of `run.bash --rerun`), with GONE plays named and never offered.
    """
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
        judgement = judge(latest, repo_root=repo_root, **seams)
    except NoAnswer as no_answer:
        stderr.writelines(f"changed-plays: {line}\n" for line in no_answer.lines)
        return EXIT_NO_ANSWER

    runs = set(judgement.runs)
    for play in judgement.order:
        if play in judgement.gone:
            stdout.write(f"GONE {play}\n")
        elif list_all:
            stdout.write(f"PLAY {judgement.states[play]} {play}\n")
        elif play in runs:
            stdout.write(f"RUN {play}\n")
        elif play in judgement.unresolved:
            stdout.writelines(f"UNRESOLVED {play} {where}\n" for where in judgement.unresolved[play])
    return EXIT_OK


def _retire(
    gone: list[str],
    *,
    latest: dict[str, dict],
    repo_root: str,
    retired_plays: Callable[[str], dict[str, str]],
    tracked_at: Callable[[str, str, str], bool],
) -> tuple[list[str], dict[str, list[str]]]:
    """The GONE plays left once the retired-plays map is applied, and the successors to run,
    each with the removed plays it takes over.

    A removed play the map names is not GONE: its successor took it over. It needs nothing
    once the successor's latest run succeeded at a commit without the old play: a run that
    failed early may never have reached the absorbed tasks. Until then, running the
    successor is the catch-up. The map is read only when something is gone, and checked
    against HEAD in full.
    """
    if not gone:
        return gone, {}
    mapping = retired_plays(repo_root)
    retired.validate(mapping, exists_at_head=lambda path: tracked_at(repo_root, "HEAD", path))
    left: list[str] = []
    successors: dict[str, list[str]] = {}
    for play in gone:
        successor = mapping.get(play)
        if successor is None:
            left.append(play)
            continue
        record = latest.get(successor)
        absorbed = (
            record is not None
            and record["outcome"] == "ok"
            and not tracked_at(repo_root, record["commit"], play)
        )
        if not absorbed:
            successors.setdefault(successor, []).append(play)
    return left, successors


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/play_ledger/changed_plays.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="List the plays run here whose inputs changed since (run.bash --changed)."
    )
    parser.add_argument("--repo-root", default=_repo_root_default())
    parser.add_argument("--all", action="store_true",
                        help="list every play run here with its state (run.bash --rerun)")
    arguments = parser.parse_args(argv)
    base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
    return run(base=base, repo_root=arguments.repo_root, stdout=sys.stdout, stderr=sys.stderr,
               list_all=arguments.all)


if __name__ == "__main__":
    sys.exit(main())
