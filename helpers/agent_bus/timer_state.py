"""The timer state of agent-bus teams that are not installed here, cleared (play-agent-bus.yml).

`agent-bus-backup@<team>.timer` is Persistent=true, so systemd keeps its last run in
`/var/lib/systemd/timers/stamp-<unit>`. `agent-bus-install remove` clears it; this clears any
an earlier removal left, for every agent-bus timer whose team has no applied stamp, so a new
team of that name does not inherit a run it never made. Prints `CHANGED\t...` per stamp cleared.

    python3 -m helpers.agent_bus.timer_state [--stamp-dir DIR] [--applied-dir DIR]
"""

from __future__ import annotations

import argparse
import pathlib
import subprocess
import sys
from collections.abc import Callable, Iterator, Sequence
from typing import TextIO

from helpers.pingbus import protocol

STAMP_DIR = pathlib.Path("/var/lib/systemd/timers")
APPLIED_DIR = pathlib.Path("/var/lib/agent-bus-applied")
_PREFIX = "stamp-agent-bus-"
_SUFFIX = ".timer"


class TimerStateError(Exception):
    """A stamp could not be cleared."""


def stale_timers(stamp_dir: pathlib.Path, applied_dir: pathlib.Path) -> Iterator[tuple[str, str]]:
    """(unit, team) of each agent-bus timer stamp whose team has no applied stamp."""
    if not stamp_dir.is_dir():
        return
    for path in sorted(stamp_dir.glob(f"{_PREFIX}*@*{_SUFFIX}")):
        unit = path.name.removeprefix("stamp-")
        team = unit.removesuffix(_SUFFIX).rpartition("@")[2]
        if protocol.is_team_name(team) and not (applied_dir / f"{team}.json").exists():
            yield unit, team


def _clean(unit: str) -> None:
    run = subprocess.run(["systemctl", "clean", "--what=state", unit], capture_output=True, text=True, check=False)
    if run.returncode != 0:
        raise TimerStateError(f"systemctl clean --what=state {unit} failed (exit {run.returncode}): "
                              f"{run.stderr.strip()}")


def main(argv: Sequence[str] | None = None, clean: Callable[[str], None] = _clean,
         out: TextIO = sys.stdout) -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--stamp-dir", type=pathlib.Path, default=STAMP_DIR)
    parser.add_argument("--applied-dir", type=pathlib.Path, default=APPLIED_DIR)
    args = parser.parse_args(argv)
    for unit, team in list(stale_timers(args.stamp_dir, args.applied_dir)):
        clean(unit)
        print(f"CHANGED\tcleared the last-run time of {unit} (team {team} is not here)", file=out)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except TimerStateError as exc:
        print(f"timer_state: {exc}", file=sys.stderr)
        sys.exit(1)
