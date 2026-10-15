"""Which installed teams the next `agent-bus-install team` run restarts for a new config.

    python3 -m helpers.agent_bus.config_restart --state-root /var/lib/agent-bus TEAM...

`team` restarts a homeserver whenever the tuwunel.toml it renders differs from the
installed one. A release that changes only how the config is rendered (render.py) swaps no
binary and no unit, so play-agent-bus.yml asks this before `software`, to take its restore
point first: each named team's installed team.json is rendered with this checkout's code and
compared with its installed tuwunel.toml.

stdout carries one `CONFIG-CHANGED <team>` line per team whose config would change; a team
with no installed team.json is not installed and is skipped. A name that is not a team name,
or a team file that cannot be read or rendered, is one stderr line and exit 1.
"""

from __future__ import annotations

import argparse
import pathlib
import sys
from collections.abc import Sequence
from typing import TextIO

from helpers.agent_bus import render, teamfile
from helpers.pingbus import protocol

MARK_CHANGED = "CONFIG-CHANGED"


def changed(state_root: pathlib.Path, team: str) -> bool:
    """Whether this checkout renders the installed team's tuwunel.toml differently."""
    if not protocol.is_team_name(team):
        raise teamfile.TeamFileError(f"{team!r} is not a team name")
    directory = state_root / team
    try:
        tf = teamfile.load_team_file(directory / "team.json")
    except FileNotFoundError:
        return False
    try:
        installed = (directory / "tuwunel.toml").read_text(encoding="utf-8")
    except FileNotFoundError:
        return True
    return render.render_toml(tf) != installed


def main(argv: Sequence[str] | None = None, *, stdout: TextIO | None = None,
         stderr: TextIO | None = None) -> int:
    out = sys.stdout if stdout is None else stdout
    err = sys.stderr if stderr is None else stderr
    parser = argparse.ArgumentParser(prog="python3 -m helpers.agent_bus.config_restart",
                                     description="Name the installed teams whose config this checkout changes.")
    parser.add_argument("--state-root", required=True, type=pathlib.Path,
                        help="the homeservers' state directory (/var/lib/agent-bus)")
    parser.add_argument("teams", nargs="*")
    args = parser.parse_args(argv)
    names: list[str] = []
    for team in args.teams:
        try:
            if changed(args.state_root, team):
                names.append(team)
        except (teamfile.TeamFileError, OSError, UnicodeDecodeError) as exc:
            err.write(f"config_restart: team {team}: {exc}\n")
            return 1
    for team in names:
        out.write(f"{MARK_CHANGED} {team}\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
