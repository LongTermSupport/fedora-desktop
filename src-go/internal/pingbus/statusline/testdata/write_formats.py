"""Write a status-line world with the real Python writers, for writers_test.go (SPEC section 8).

Usage: python3 -I write_formats.py <checkout> <root>

Under <root>: the session record sess-1 (alpha, beta) in sessions/, the bundles of alpha and beta
in pingbus/ (member.json, token and README as the admin's bundle holds them), each team's
team.json (alpha: coordinator, beta: worker), a fresh down mark for beta, and applied.json
holding seat 1 of alpha. Each file is written by the function that writes it in use.
"""

import pathlib
import sys
import types

HANDLE = "myrepo.1+workstation.podman"
ROOM = "!" + "R" * 43
ADDRESS = "127.0.0.1"
ROLES = {"alpha": "coordinator", "beta": "worker"}


def team_of(name: str) -> types.SimpleNamespace:
    """The few parts of agent_bus.admin.Team that the bundle reads."""
    server = f"{name}.example.org"
    teamfile = types.SimpleNamespace(port=8448, listen_addresses=lambda: [ADDRESS])
    return types.SimpleNamespace(
        name=name, server_name=server, admin_id=f"@admin:{server}", file=teamfile,
        user_id=lambda localpart: f"@{localpart}:{server}",
    )


def main(checkout: str, root: pathlib.Path) -> None:
    sys.path.insert(0, checkout)
    from helpers.agent_bus import admin, checkout as seats
    from helpers.pingbus import hooks, inbox, membership, protocol, statusline

    hooks.record_session_teams({"PINGBUS_SESSIONS_DIR": str(root / "sessions")}, "sess-1", ["alpha", "beta"])
    home = root / "pingbus"
    home.mkdir(mode=0o700)
    (root / "home").mkdir(mode=0o700)
    for name, role in ROLES.items():
        team = team_of(name)
        bundle = home / name
        bundle.mkdir(mode=0o700)
        files = seats.read_bundle(admin._bundle(team, ROOM, HANDLE, "podman", f"syt_{name}", ADDRESS, True))
        for file_name, data in files.items():
            path = bundle / file_name
            path.write_bytes(data)
            path.chmod(seats.FILE_MODE)
        other = "worker" if role == "coordinator" else "coordinator"
        record = {
            "v": 1, "team": name, "humans": [f"@alice:{team.server_name}"],
            "roles": {team.user_id(HANDLE): role, team.user_id("orch.1+workstation.podman"): other},
            "repos": [{"repo": "example-org/myrepo", "branches": ["main"]}],
            "forge_api": "https://api.github.com",
        }
        protocol.parse_team_record(record, team.server_name, name)
        inbox.TeamState(bundle / "state").save_team_record(record)
    statusline.set_team_down(home, "beta", True)
    seat = membership.AppliedSeat(team="alpha", seat="1", handle="myrepo.1+local.podman")
    membership.write_applied(home / membership.APPLIED_FILE, membership.Applied(generation=3, seats=(seat,), refused=()))


if __name__ == "__main__":
    main(sys.argv[1], pathlib.Path(sys.argv[2]))
