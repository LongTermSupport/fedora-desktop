# Acceptance run 1: the three check-design fixes

Branch `fix/00232-acceptance-design`. Run 1 (at `1f03b8275`): triage before and after, and
deploy, passed. The acceptance was REJECTED on `blocked`, `connect` and `bus`. All three came
from how the checks were built, not from the policy (PLAN.md F41 to F44).

## blocked

`host-other` was the first global host address off the outbound interface. The unpoliced
control could not reach it either (curl 7), because pasta copies that address into the rootless
namespace. The check now probes every such address (the bus address is left out) from the
control. It sends to the session only the addresses the control reaches, over http and ssh,
and prints a NOTE for each address it leaves out. If the control reaches none, the check prints
COULD NOT ESTABLISH and exits 2. `acceptance.bash`'s `check` now treats a 2 as its own
COULD NOT ESTABLISH (overall exit 2 when nothing failed).

## connect

np232c's Quick Launch record had `LAST_NETWORK=""`. A headless replay of an empty saved network
sets `NO_NETWORK_MODE=true` (`claude-yolo:1311-1312`), so np232c ran on pasta. It now replays
`LAST_NETWORK="podman"`, the network a plain launch joins.

When ccy --connect reached a pasta session, it used to print podman's `"pasta" is not supported`,
then "This is unusual. The container may need to be restarted". `_container_can_join_networks` (in `lib/network-policy.bash`, since
`network-management.bash` is on the bash-limits ratchet) now checks `.HostConfig.NetworkMode`
first. The single-container failure diagnostic was cut down to `_container_networks`, which
keeps that file under its record (tightened with `--update`). It says the session
was started without a network, cannot join one, and should be relaunched with
`--network <net>`. On the several-container path, the other containers are still connected.
The host agent noticed that a real headless relaunch with no saved network also landed on
pasta. That line (`claude-yolo` Quick Launch block, from Plan 00161 U31) existed only to keep a
headless launch out of the network picker. An interactive yes to the same record joins
`podman`, and an empty `LAST_NETWORK` is also what a launch onto the default network saves. So
the headless replay now sets `CCY_NO_PROJECT_NETWORK=true`, not `NO_NETWORK_MODE=true`. It takes
the restart's "no project network" branch (no detection, no offers) and lands on `podman`.
`scripts/test-ccy-teams.bash` and `scripts/test-ccy-restart-request.bash` were each seen red
first. The difftest parity passes. CCY 3.101.1, with changelog and `docs/ccy.md` entries,
covers both changes.

## bus

`pingbus inbox` lists only pending items and never prints their text (`cmd_inbox`), so the
nonce could never have matched. The woken receiver had consumed the delivery anyway. The check
now takes the event id from `say`'s `SENT` line. It then polls the receiver's
`pingbus history --limit 50` for a line `HISTORY .. in .. PEER .. <event id>` that carries the
nonce. That line is read from the room through the receiver's own seat.

The throwaways take turns of their own on the membership notice and on the probes. This is
harmless and is now recorded in the `acceptance.bash` header and as F44.

## Tests, red first

- `test-acceptance-checks.bash` (new; fakes for podman, ip and sleep). Against the old checks,
  10 cases failed, including the three host failures. All pass after the fixes.
- `scripts/test-ccy-network-disconnect.bash`. The stub now answers the network mode and refuses
  a pasta connect as the engine does. Against the old connect, 8 new cases failed. Now 146
  pass and 0 fail. `scripts/test-ccy-agent-choice.bash`: 75 pass, 0 fail.

## The host rerun

`meta-deploy.list` now has two entries:

1. `00232-.../deploy.bash`, which runs `play-claude-yolo.yml` again for the launcher at 3.101.1.
2. `00232-.../acceptance.bash`.

Triage is not run again. The `changed-plays` entry below them is unchanged. Its reclaim run
already PASSED in run 1, so the coordinator decides whether it stays.
