# Plan 00203: host acceptance

What proves each milestone on the host, one section per milestone. `acceptance.bash` is a
dispatcher: each milestone's checks live in its own `_acceptance-<milestone>.inc.bash`, and it
runs them in turn through `meta-deploy.bash`, after `play-claude-yolo.yml` and
`play-agent-bus.yml`.

## Server-side robustness (`_acceptance-robust.inc.bash`)

`acceptance.bash` runs R1-R9 by itself on a throwaway team (`acc-robust`, loopback only,
removed on the way out). The reboot steps (B1, B2) need a reboot, so they are the owner's.

The gaps are those of `subagent-reports/261016-robustness-audit-opus.md` section 5; the design
and its evidence are in `subagent-reports/261016-server-robustness-opus.md`.

### Run by acceptance.bash

| Check | Gap | What it does                                                                                                                                          | Passes when                                                                                                                      |
| ----- | --- | ----------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| R1    | 4   | Reads the homeserver unit's `Type`, `WatchdogUSec` and `Restart`, then waits 75 s                                                                     | `notify`, `30s`, `always`; same main PID, `NRestarts` 0 and the client API answering after 75 s (READY=1 and the pings arrive)   |
| R2    | 10  | Starts `agent-bus-hs-health@acc-robust.service`; runs `agent-bus health status acc-robust` as the desktop user                                        | the check succeeds and the status exits 0                                                                                        |
| R3    | 4   | `systemctl kill --kill-whom=main --signal=SIGKILL`                                                                                                    | a new main PID is active and answering within 60 s; `NRestarts` rose                                                             |
| R4    | 4   | `systemctl kill --kill-whom=main --signal=SIGTERM` (Tuwunel exits 0; no `systemctl stop`)                                                             | back within 90 s (`Restart=always`)                                                                                              |
| R5    | 4   | `systemctl kill --kill-whom=main --signal=SIGSTOP` (a hang)                                                                                           | back within 150 s, and the journal says `Watchdog timeout`                                                                       |
| R6    | 10  | Runs the health check again                                                                                                                           | the status exits 1 with `PROBLEM systemd restarted the homeserver ...` and what to do                                            |
| R7    | 10  | `agent-bus-install backup-now`, then the newest state tar made three days old, then the health check                                                  | the status says `PROBLEM the newest backup is 3 d ...`, and the team's row of `ccy-teams --list` says `health: ...` and the same |
| R8    | 3   | A runtime drop-in delays the homeserver's start by 25 s; `systemctl restart --no-block`; `agent-bus seat check 1@acc-robust` while it is `activating` | exit 0 after at least 15 s, its stderr saying `... is starting ...` and `... is up after N s`                                    |
| R9    | 9   | Every running LXC container with systemd: each `agent-bus-claude-*.service` in it                                                                     | enabled, active, `Restart=always`, and the container's `lxc.start.auto = 1`; a host with none declared says so in the report     |

### The owner's, after a reboot

- **B1 (gap 3).** With at least one ccy team session recorded for restore, reboot. Every recorded
  team session is up (`ccy-sessions`), and none of their tmux windows shows a `seat check`
  refusal. A homeserver that was still starting shows the wait instead:
  `agent-bus: agent-bus-hs@<team>.service is starting (activating); waiting up to 180 s`.
  `agent-bus health status` exits 0, or names only what it says.
- **B2 (gap 9).** With an `agent_bus_lxc_members` entry that has `service: true` and the play
  applied, reboot. The container is running, and in it
  `systemctl is-active agent-bus-claude-<agent-user>.service` says `active`; `tmux -L agent-bus-claude attach`
  as the agent user shows the session. Then end the session (`/exit` in it): it is running
  again within a minute.

### Not provable on the host

A per-account sign-in lockout leaves nothing a tool can read (Tuwunel 1.9.4 keeps the
buckets in memory and logs a refusal only at a level the release build compiles out). What is
built is the text: `sudo agent-bus human password` and `agent-bus health status` both state the
limits, what Element shows and that the wait clears by itself. No reset is offered.
