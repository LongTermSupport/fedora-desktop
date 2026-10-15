"""A stand-in for play-agent-bus.yml in scripts/test-ccy-teams-command.bash (D65).

    python3 -m tests.helpers.agent_bus.ccy_teams_play '<extra vars JSON>' <localhost.yml> <applied dir>

For each team the extra vars' `agent_bus_only_teams` names, it does what the play and the
installer leave behind: a team declared present gets the stamp `agent-bus-install team`
writes (its digests by the same functions, `declare.declared_config` over the entry plus the live
bridge subnet, and
`declare.declared_software` over this checkout); a team declared absent loses its stamp.
"""

from __future__ import annotations

import json
import pathlib
import sys

from helpers.agent_bus import bridge, declare, teamfile

REPO_ROOT = pathlib.Path(__file__).resolve().parents[3]


def main(argv: list[str]) -> int:
    extra, host_vars, applied = json.loads(argv[0]), pathlib.Path(argv[1]), pathlib.Path(argv[2])
    teams = {t.team: t for t in declare.parse(host_vars.read_text(encoding="utf-8")).teams}
    software = declare.declared_software(REPO_ROOT)
    for name in extra["agent_bus_only_teams"]:
        team = teams[name]
        stamp = applied / f"{name}.json"
        if team.state == "absent":
            stamp.unlink(missing_ok=True)
            continue
        body = {"v": teamfile.STAMP_VERSION, "team": name, "config": declare.declared_config(team, bridge.live_subnet()),
                "software": software}
        stamp.write_text(json.dumps(body, sort_keys=True) + "\n", encoding="utf-8")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
