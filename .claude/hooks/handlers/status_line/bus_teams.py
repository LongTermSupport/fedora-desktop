"""BusTeamsHandler - the agent-bus teams THIS session is on, in the status line.

The hooks daemon is one process shared by every session of the project, started by whichever
session came first, so its environment says nothing about this session. The pingbus SessionStart
hook, which runs in the session, writes the session's teams to `<sessions dir>/<session_id>`
(helpers/pingbus/hooks.py); this handler reads that file for the `session_id` in the status
payload. No file, an unusable file or an id that is not a plain name shows nothing, never another
session's teams.

The default directory and the variable that moves it are copies of helpers/pingbus/hooks.py's
(this process cannot import that package); a pingbus test keeps them equal.
"""

import os
import re
import stat
import sys
from pathlib import Path
from typing import Any

sys.path.insert(0, str(Path(__file__).resolve().parents[3] / "hooks-daemon/src"))

from claude_code_hooks_daemon.core import AdvisoryResult
from claude_code_hooks_daemon.core.handler_bases import StatusLineHandlerBase
from claude_code_hooks_daemon.core.segment_explanation import SegmentExplanation

ENV_SESSIONS_DIR = "PINGBUS_SESSIONS_DIR"
SESSIONS_DIR_TEMPLATE = "/tmp/pingbus-sessions-{uid}"
_SESSION_ID = re.compile(r"[A-Za-z0-9_-]{1,128}")
_TEAM_NAME = re.compile(r"[a-z][a-z0-9-]{0,23}")
_MAX_RECORD_BYTES = 4096
_COLOR_CYAN = "\033[36m"
_COLOR_RESET = "\033[0m"
_GLYPH = "bus:"


def default_sessions_dir() -> Path:
    return Path(os.environ.get(ENV_SESSIONS_DIR) or SESSIONS_DIR_TEMPLATE.format(uid=os.getuid()))


def sessions_dir() -> Path:
    return default_sessions_dir()


def session_teams(session_id: object) -> list[str]:
    """The teams recorded for this session, or [] when there is no usable record: no id, an id that
    is not a plain name, no file, a file that is not this user's alone, or contents that are not a
    list of distinct team names."""
    if not isinstance(session_id, str) or not _SESSION_ID.fullmatch(session_id):
        return []
    path = sessions_dir() / session_id
    try:
        found = os.lstat(path)
        if not stat.S_ISREG(found.st_mode) or found.st_uid != os.getuid() or found.st_mode & 0o077:
            return []
        if found.st_size > _MAX_RECORD_BYTES:
            return []
        raw = path.read_text(encoding="ascii")
    except (OSError, UnicodeDecodeError):
        return []
    teams = raw.split(",")
    if len(set(teams)) != len(teams) or not all(_TEAM_NAME.fullmatch(team) for team in teams):
        return []
    return teams


class BusTeamsHandler(StatusLineHandlerBase):
    """Show `bus: alpha,beta` for the teams recorded for this session."""

    def __init__(self) -> None:
        super().__init__(name="bus-teams", priority=25, terminal=False)

    def matches(self, hook_input: dict[str, Any]) -> bool:
        return True

    def handle(self, hook_input: dict[str, Any]) -> AdvisoryResult:
        teams = session_teams(hook_input.get("session_id"))
        if not teams:
            return AdvisoryResult(context=[])
        return AdvisoryResult(context=[f"| {_COLOR_CYAN}{_GLYPH} {','.join(teams)}{_COLOR_RESET}"])

    def explain_segment(self) -> SegmentExplanation:
        return SegmentExplanation(
            glyphs=(_GLYPH,),
            name="Bus Teams",
            what_it_is="The agent-bus teams this session is on.",
            how_to_read=(
                "bus: A,B lists every team this session was launched with, from the record the "
                "pingbus SessionStart hook wrote for this session id. A session on no team, or one "
                "whose record is missing, shows nothing; it never shows another session's teams."
            ),
            current_value="Per session: shown only when this session has a record.",
        )

    def get_claude_md(self) -> str | None:
        return None

    def get_acceptance_tests(self) -> list:
        return []
