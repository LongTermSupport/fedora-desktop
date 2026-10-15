"""BusTeamsHandler - the agent-bus teams this session is on, in the status line.

A ccy or cc session launched with `--teams` has `PINGBUS_TEAMS` in its environment, the comma
separated names of every team it belongs to. The status line is the one place that stays in view
for the whole session, so it names them: `bus: alpha,beta`. A session on no team shows nothing.

The value reaches a terminal, so only a well-formed team list is shown: lower-case names of the
agent-bus grammar, no duplicates. Anything else renders nothing rather than a half-trusted string.
"""

import os
import re
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import AdvisoryResult
from claude_code_hooks_daemon.core.handler_bases import StatusLineHandlerBase
from claude_code_hooks_daemon.core.segment_explanation import SegmentExplanation

ENV_TEAMS = "PINGBUS_TEAMS"
_TEAM_NAME = re.compile(r"[a-z][a-z0-9-]{0,23}")
_COLOR_CYAN = "\033[36m"
_COLOR_RESET = "\033[0m"
_GLYPH = "bus:"


def teams_from_environment() -> list[str]:
    """The session's teams in order, or [] when the variable is unset, empty or not a team list."""
    raw = os.environ.get(ENV_TEAMS, "")
    teams = raw.split(",")
    if not raw or len(set(teams)) != len(teams):
        return []
    if not all(_TEAM_NAME.fullmatch(team) for team in teams):
        return []
    return teams


class BusTeamsHandler(StatusLineHandlerBase):
    """Show `bus: alpha,beta` for the teams named in PINGBUS_TEAMS."""

    def __init__(self) -> None:
        super().__init__(name="bus-teams", priority=25, terminal=False)

    def matches(self, hook_input: dict[str, Any]) -> bool:
        return True

    def handle(self, hook_input: dict[str, Any]) -> AdvisoryResult:
        teams = teams_from_environment()
        if not teams:
            return AdvisoryResult(context=[])
        return AdvisoryResult(context=[f"| {_COLOR_CYAN}{_GLYPH} {','.join(teams)}{_COLOR_RESET}"])

    def explain_segment(self) -> SegmentExplanation:
        teams = teams_from_environment()
        current = f"Currently shows: {_GLYPH} {','.join(teams)}" if teams else (
            f"Not shown now: {ENV_TEAMS} is unset, empty or not a team list.")
        return SegmentExplanation(
            glyphs=(_GLYPH,),
            name="Bus Teams",
            what_it_is="The agent-bus teams this session is on.",
            how_to_read=(
                f"bus: A,B lists every team in ${ENV_TEAMS}, in the order the session was launched "
                "with. A session on no team shows nothing. A value that is not a list of team "
                "names is not shown."
            ),
            current_value=current,
        )

    def get_claude_md(self) -> str | None:
        return None

    def get_acceptance_tests(self) -> list:
        return []
