"""The panel's play runner: which plays it lists, and the gate a requested play passes
(Plan 00109, Task 4.3).

**The list** is the plays this host's ledger has seen, each with the state
`check_freshness` judged for it — fresh, stale, or unexplained. It is derived from that
check's own verdicts, never recomputed: a freshness rule reimplemented here, or in the
panel's JavaScript, would be a second check that drifts from the one under test
(DESIGN-panel.md §8). A GONE play is left out, because there is nothing to run and the
health section already reports it.

**The gate** is `validate`. The play name reaches the on-demand command from a state
file the panel read, so it is judged as untrusted input: a repo-relative path under
`playbooks/`, spelled canonically, one the ledger lists, present in this checkout,
executable, and not a symlink out of the playbooks tree. Anything else is refused with
the reason, and no path is printed.

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
from collections.abc import Iterable, Mapping
from typing import TextIO

from helpers.play_ledger import freshness, ledger, store

#: The only tree the runner launches from.
PLAYBOOKS_DIR = "playbooks"

#: Refused: the name failed the gate. 64 is the command's own usage status.
EXIT_REFUSED = 64
#: The ledger could not be read, so no name can be shown to be one it lists.
EXIT_LEDGER_UNREADABLE = 2

#: Path segments of letters, digits, `.`, `_` and `-`, ending in `.yml`. An allowlist,
#: because a denylist of shell metacharacters is the list that misses one.
_PLAY_RE = re.compile(r"^playbooks/(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+\.yml$")


class PlayRefused(ValueError):
    """The requested play did not pass the gate. The message says which rule."""


def runnable(verdicts: Iterable[freshness.Verdict]) -> list[dict[str, str]]:
    """The runner's rows: `{"play", "state"}` per ledgered play under `playbooks/`,
    GONE ones left out, sorted by path so two documents diff cleanly."""
    return sorted(
        (
            {"play": verdict.play, "state": verdict.state}
            for verdict in verdicts
            if verdict.state != freshness.GONE
            and verdict.play.startswith(f"{PLAYBOOKS_DIR}/")
        ),
        key=lambda row: row["play"],
    )


def validate(play: str, *, repo_root: str, ledgered: set[str]) -> str:
    """The play's absolute path, or `PlayRefused` naming the rule it broke."""
    if not isinstance(play, str) or not _PLAY_RE.match(play):
        raise PlayRefused(
            f"{play!r} is not a repo-relative playbook path under {PLAYBOOKS_DIR}/ "
            "ending in .yml"
        )
    if posixpath.normpath(play) != play or ".." in play.split("/"):
        raise PlayRefused(f"{play!r} is not spelled canonically")
    if play not in ledgered:
        raise PlayRefused(
            f"{play} is not in this host's play ledger, so the panel cannot have "
            "offered it"
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
        ledgered = set(ledger.fold_latest(store.read_lines(base)))
    except (OSError, ValueError) as error:
        err.write(f"the play ledger cannot be read, so no play can be run from it: {error}\n")
        return EXIT_LEDGER_UNREADABLE

    try:
        path = validate(arguments.validate, repo_root=arguments.repo_root, ledgered=ledgered)
    except PlayRefused as error:
        err.write(f"refused: {error}\n")
        return EXIT_REFUSED
    out.write(f"{path}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
