"""The panel's play runner: which plays it lists, and the gate a requested play passes
(Plan 00109, Task 4.3).

**The list** is the plays under `playbooks/imports/` that `changed_plays.judge` judges, each
with the state `check_freshness` reports for it — fresh, stale, failed, unresolved, or
unrecorded (no ledger record here, but a file it deploys here shows it ran; issue #323).
It is derived from that check's own verdicts, never recomputed: a freshness rule
reimplemented here, or in the panel's JavaScript, would be a second check that drifts from
the one under test (DESIGN-panel.md §8). A GONE play is left out, because there is nothing
to run and the health section already reports it, and so are the plays the judgement does
not run (NEVER, UNJUDGED).

**The gate** is `validate`. The play name reaches the on-demand command from a state
file the panel read, so it is judged as untrusted input: a repo-relative path under
`playbooks/`, spelled canonically, one the list offers (`offered_plays`, the same judgement
made again now), present in this checkout, executable, and not a symlink out of the
playbooks tree. Anything else is refused with the reason, and no path is printed.

The command then runs the play through its own shebang, which hands it to `run.bash`.
Nothing here runs a play: this module answers which, and whether.

Run it: `python3 -m helpers.host_health.play_runner --validate <playbooks/...yml>`
"""

from __future__ import annotations

import argparse
import os
import posixpath
import re
import sys
from collections.abc import Callable, Iterable, Mapping
from typing import TextIO

from helpers.play_ledger import changed_plays, check_freshness, freshness, ledger, store

#: The only tree the runner launches from.
PLAYBOOKS_DIR = "playbooks"

#: Refused: the name failed the gate. 64 is the command's own usage status.
EXIT_REFUSED = 64
#: The ledger could not be read or the plays judged, so no name can be shown to be one the
#: panel offers.
EXIT_LEDGER_UNREADABLE = 2

#: Path segments of letters, digits, `.`, `_` and `-`, ending in `.yml`. An allowlist,
#: because a denylist of shell metacharacters is the list that misses one.
_PLAY_RE = re.compile(r"^playbooks/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+\.yml$")


class PlayRefused(ValueError):
    """The requested play did not pass the gate. The message says which rule."""


def runnable(verdicts: Iterable[freshness.Verdict]) -> list[dict[str, str]]:
    """The runner's rows: `{"play", "state"}` per judged play under `playbooks/` (which the
    judgement limits to `playbooks/imports/`), GONE ones left out, sorted by path so two
    documents diff cleanly."""
    return sorted(
        (
            {"play": verdict.play, "state": verdict.state}
            for verdict in verdicts
            if verdict.state != freshness.GONE
            and verdict.play.startswith(f"{PLAYBOOKS_DIR}/")
        ),
        key=lambda row: row["play"],
    )


def offered_plays(base: str, repo_root: str, *,
                  judge: Callable[..., changed_plays.Judgement] = changed_plays.judge) -> set[str]:
    """The plays `runnable` offers for this host's ledger at `base`, judged now.

    The same judgement and verdicts as the list (`check_freshness`), so the gate accepts
    exactly what the panel can have offered. Raises `ValueError` for a ledger that cannot be
    read or has a recorded hole (the panel offers nothing then), and `changed_plays.NoAnswer`
    when the plays cannot be judged. `judge` is the test seam.
    """
    broken = store.broken_reason(base)
    if broken is not None:
        raise ValueError(f"the play ledger has a recorded hole: {broken}")
    lines = store.read_lines(base)
    judgement = judge(ledger.fold_latest_full(lines), repo_root=repo_root,
                      recorded=ledger.recorded_plays(lines))
    return {row["play"] for row in runnable(check_freshness.verdicts(judgement))}


def validate(play: str, *, repo_root: str, offered: set[str]) -> str:
    """The play's absolute path, or `PlayRefused` naming the rule it broke."""
    if not isinstance(play, str) or not _PLAY_RE.match(play):
        raise PlayRefused(
            f"{play!r} is not a repo-relative playbook path under {PLAYBOOKS_DIR}/ "
            "ending in .yml"
        )
    if posixpath.normpath(play) != play or ".." in play.split("/"):
        raise PlayRefused(f"{play!r} is not spelled canonically")
    if play not in offered:
        raise PlayRefused(
            f"the panel does not offer {play} on this host (as run.bash --rerun judges "
            "the plays now), so it cannot be run from there"
        )

    tree = os.path.join(os.path.realpath(repo_root), PLAYBOOKS_DIR)
    resolved = os.path.realpath(os.path.join(repo_root, play))
    if os.path.commonpath([tree, resolved]) != tree:
        raise PlayRefused(f"{play} resolves outside {tree}")
    if not os.path.isfile(resolved):
        raise PlayRefused(f"{play} is not in the checkout at {repo_root}")
    if not os.access(resolved, os.X_OK):
        raise PlayRefused(
            f"{play} is not executable, and a play runs through its own shebang; "
            "scripts/make-playbooks-executable.bash sets the bit"
        )
    return resolved


def _repo_root_default() -> str:
    """This file is `<repo>/helpers/host_health/play_runner.py`."""
    return os.path.dirname(os.path.dirname(os.path.dirname(os.path.realpath(__file__))))


def main(
    argv: list[str] | None = None,
    *,
    environ: Mapping[str, str] | None = None,
    home: str | None = None,
    stdout: TextIO | None = None,
    stderr: TextIO | None = None,
    judge: Callable[..., changed_plays.Judgement] = changed_plays.judge,
) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=_repo_root_default())
    parser.add_argument("--validate", required=True, metavar="PLAY")
    arguments = parser.parse_args(argv)
    out = stdout if stdout is not None else sys.stdout
    err = stderr if stderr is not None else sys.stderr
    environment = environ if environ is not None else os.environ
    base = ledger.ledger_dir(environment, home if home is not None else os.path.expanduser("~"))

    try:
        offered = offered_plays(base, arguments.repo_root, judge=judge)
    except (OSError, ValueError) as error:
        err.write(f"the play ledger cannot be read, so no play can be run from it: {error}\n")
        return EXIT_LEDGER_UNREADABLE
    except changed_plays.NoAnswer as no_answer:
        err.writelines(f"{line}\n" for line in ("the plays cannot be judged, so none is run "
                                                "from the panel:", *no_answer.lines))
        return EXIT_LEDGER_UNREADABLE

    try:
        path = validate(arguments.validate, repo_root=arguments.repo_root, offered=offered)
    except PlayRefused as error:
        err.write(f"refused: {error}\n")
        return EXIT_REFUSED
    out.write(f"{path}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
