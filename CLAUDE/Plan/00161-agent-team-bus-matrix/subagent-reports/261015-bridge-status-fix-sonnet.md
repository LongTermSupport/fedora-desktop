# Bridge subnet in the applied-state digest (Plan 00161 defect)

Branch `agent-a86eb821d758e26d9-1f76fb77` from F44.

## Cause

The play stamped the config digest of the team file rendered with the LXC bridge's subnet
appended to `allow_from`; `declare.py status` digested the bare host_vars entry. Wherever
lxcbr0 exists the two never matched.

## Changes

- `helpers/agent_bus/bridge.py`: owns `BRIDGE = "lxcbr0"` and `live_subnet(run)`, which runs
  `ip -j -4 addr show dev lxcbr0` (argument list, injectable runner). Device absent
  ("does not exist" on stderr) gives None; any other exit, a missing `ip`, unparseable JSON or
  a bridge with no inet address is a `BridgeError`. `apply(cidr, teams)` and `main(--teams)`
  use it. The `--bridge` and `--facts` arguments and `subnet()` are gone.
- `helpers/agent_bus/declare.py`: `declared_config(team, bridge_cidr=None)` digests
  `bridge.effective(allow_from, cidr)`; `team_states(..., bridge_cidr)` stays pure; `status`
  and `overview` probe once per call (injectable via `main(bridge_cidr=...)`); a `BridgeError`
  becomes a `DeclareError` (exit 78).
- `playbooks/imports/play-agent-bus.yml`: bridge task without `--facts`; the fact refresh task
  and `agent_bus_lxc_bridge` are removed. Failure behaviour is unchanged (non-zero exit fails
  the task with the reason on stderr).
- `helpers/agent_bus_lxc/members.py`, `docs/agent-bus.md`, `docs/agent-bus-lxc-members.md`,
  `localhost.yml.dist`: no longer mention `agent_bus_lxc_bridge`.
- `tests/helpers/agent_bus/ccy_teams_play.py`: the stand-in play stamps with the live bridge.
- Go (separate commit): `src-go/internal/teams/create/wizard.go` failure line now says
  "declared ... but not applied as declared" and "applies it"; the Go test and the label in
  `scripts/test-ccy-teams-command.bash` follow. `ApplyTeams`' error does not say whether the
  play ran, so the wording is neutral rather than split.
- `src-go` needs no change for the digest: `statusArgs` only shells `declare.py status`.

## Evidence

Red: with the new tests in place and the old code, `test_bridge` and the new
`BridgeStatusTest` errored/failed (18 errors). Green: `test_bridge`, `test_declare`,
`agent_bus_lxc.test_members` 144 tests OK. Go: the pinned-text test failed (1 FAIL) before
the wording change; `go test ./internal/teams/...` passes after.

## Deviations

- The stand-in play (`ccy_teams_play.py`) and `members.py`'s message needed edits the brief
  did not list.
- Dropping `agent_bus_lxc_bridge` means a host with a renamed bridge now uses `allow_from`,
  as the updated docs say.
