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

## M2: the bus kit in every ccy session

On an SELinux host, Enforcing or Permissive, after `meta-deploy.bash` has run
`play-claude-yolo.yml` for this plan. The relabel (H1) is proved by labels, which hold in both
modes; on a Permissive host the run says in one line that enforcement was not exercised. The first launch after the deploy rebuilds the image
once (container 2.71). The automated part needs ccy launched interactively in this checkout
once, so a headless launch has Quick Launch's token and key.

### What runs by itself

`_acceptance-m2.inc.bash` starts two throwaway headless sessions in ccy's tmux, in this checkout:
a plain `ccy` and a `ccy --no-bus`. Their stdin is held open and no turn is sent, so they stay
idle. It removes them on the way out, with their containers, records, membership directories
and any transcript. Your own sessions are never touched. Then meta-deploy runs the Plan 00201
and Plan 00161 acceptances, ported to M2. They launch `ccy --teams` sessions on throwaway teams,
and that is check (a).

| Step | What passes                                                                                                                                                                                                                                                                | Where                         |
| ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| (a)  | `ccy --teams` works as before: the seats are held, the launch block, `pingbus status`, delivery and the goodbye.                                                                                                                                                           | Plan 00201 M7, Plan 00161 U20 |
| (H1) | The plain session's membership bind asks for `Z`, and its directory and file carry `container_file_t` at the container's own MCS level, not `user_tmp_t`. Enforcing: the container reads them. Permissive: one line says enforcement was not exercised.                    | `_acceptance-m2.inc.bash`     |
| (b)  | The plain session reads `/run/ccy/membership/membership.json` (no seat). A `touch`, a write and a remount there all fail (H3). It has no `active` flag, and its status line is the guard around its own. It has no `crossSessionInbound` and no inbox socket (Decision 1). | `_acceptance-m2.inc.bash`     |
| (c)  | A prompt's hook ends in the shell (no `python3`, no `pingbus`), at most one `pingbus` process runs, and idle CPU over a minute matches the `--no-bus` session.                                                                                                             | `_acceptance-m2.inc.bash`     |
| (d)  | The session image is the launcher's `REQUIRED_CONTAINER_VERSION` (2.71), with the M2 entrypoint.                                                                                                                                                                           | `_acceptance-m2.inc.bash`     |
| (e)  | `--no-bus`: `PINGBUS_MEMBERSHIP=off`, no mount, no `pingbus`, no plugin, no settings.                                                                                                                                                                                      | `_acceptance-m2.inc.bash`     |
| (f)  | The spike's result is in the spec (H34). Claude Code does not re-read `--settings` mid-session, so a session on no team is launched with no inbound, and its first join relaunches it once.                                                                                | `_acceptance-m2.inc.bash`     |

### What to look at yourself

Start a plain `ccy` in ccy's tmux and look at it. Its status line is the checkout's own (the
hooks daemon's, here), with no `bus:` segment, and a prompt is answered as in any session. A
`ccy --no-bus` session looks the same. A `ccy --teams 1@<team>` session shows `bus: <team>`.

### What is not in M2

With M1 merged, a plain session runs exactly one `pingbus` process, the seat keeper. Adding a
running session to a team is M3. For a session that started on no team, the first join
relaunches it once: the keeper takes a seat added later, but inbound stays as it was launched.

## M1: the seat keeper and the membership reader

After `meta-deploy.bash` has run `play-claude-yolo.yml` and `play-agent-bus.yml` (the same
runs as M2: one deploy covers both). The first ccy launch after it rebuilds the image once.

### What runs by itself (`_acceptance-m1.inc.bash`)

After M2's checks, `acceptance.bash` runs (a) itself, and (b) on a throwaway plain headless
session it starts and removes as M2's are: exactly one `pingbus` process,
`pingbus seat keep --launch --parent ...`, holding `keeper.lock`, and no other process holding a
lock. The seated half of (b) needs a team, so it is yours, below.

### Steps

| Step | What to do                                                                                                                                                                                                                                                                                                                                                                                                                      | What passes                                                                                                                                                                                                                                               |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | Nothing to do: `acceptance.bash` runs `./scripts/qa-helper-tests.bash`                                                                                                                                                                                                                                                                                                                                                          | It ends green.                                                                                                                                                                                                                                            |
| (b)  | Inside ccy's tmux, in a checkout of a team on this machine, start `ccy --teams 1@<team>` and leave it idle. Then, from another terminal: `agent-bus seat list \| cat`, and `podman exec <container> sh -c 'for p in /proc/[0-9]*; do ls -l "$p/fd" 2>/dev/null \| grep -q seat.lock && cat "$p/cmdline" \| tr "\0" " " && echo; done'` (the container from `podman ps --filter label=ccy-session --format '{{.Names}}' \| cat`) | The launch block says `✓ agent team bus: 1@<team> (...)` as before; `agent-bus seat list` shows the seat `held`; the loop prints exactly one process, and it is `... pingbus seat keep --launch --parent ...`: claude and its children hold no seat lock. |

The audit F5 case (a detached process keeping a seat) matters only on the host, where no
container ends with the session; it is M6's acceptance (b), for `cc --teams`.

## M3: hot add and remove

After `meta-deploy.bash` has run `play-claude-yolo.yml` and `play-agent-bus.yml` (container
2.73, CCY 3.99.8). The first ccy launch after it rebuilds the image once.

### What runs by itself (`_acceptance-m3.inc.bash`)

After M1, `acceptance.bash` builds two throwaway teams, `acc-m3a` and `acc-m3b` (human
`tester`), declared only in a host_vars file in the run directory, and starts one throwaway
headless session on `acc-m3a` in ccy's tmux. It removes the session, both teams and their seat
directories on the way out, however the run ends. Each check is judged from what a tool printed
or recorded (the session's transcript, `agent-bus seat list`, the room), never from the
model's prose.

| Step | What passes                                                                                                                                                                                                                      |
| ---- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | `ccy-teams session add <session> acc-m3b --yes` exits 0, every step `✓` or `-`, and no relaunch (the session was launched on a team).                                                                                            |
| (b)  | The transcript has the `kind: MEMBERSHIP` notice, "you joined team acc-m3b"; the status line says `bus: acc-m3a,acc-m3b`.                                                                                                        |
| (c)  | A message from the human in `acc-m3b` reaches the conversation as a `kind: HUMAN` delivery.                                                                                                                                      |
| (d)  | `session remove <session> acc-m3b --yes` exits 0 with `✓ re-baseline`; the transcript says "you left team acc-m3b"; the room has the goodbye; the seat is `free`; `pingbus --team acc-m3b say` exits 64, "removed by the owner". |
| (e)  | A message posted while the seat is out is not delivered when it is added again (H23a).                                                                                                                                           |
| (f)  | The same add again changes neither the record nor the membership (every step `-`).                                                                                                                                               |

### The owner's step

| Step | What to do                                                                                                                                                                                        | What passes                                                                                                                                                                                                                      |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (g)  | In ccy's tmux, in a checkout of a team on this machine, start a plain `ccy` and leave it idle. From another terminal: `ccy-teams session add <session> <team> --yes`. Then post to it in Element. | The add prints its own line, "relaunched once to accept team messages (it started on no team)", and exits 0. The session resumes the same conversation, says it joined, shows `bus: <team>`, and the Element message reaches it. |

A headless acceptance session runs without the supervisor, so it cannot exercise the relaunch:
that is why (g) is yours. After the relaunch the session is told its teams by the session-start
context, not by a "you joined" notice: that is what "says it joined" means here.

With M4, a second `ccy-teams session` command on the same session while one runs is refused
(exit 75), naming the first.

## M4: role, move, locks and edge cases

After `meta-deploy.bash` has run `play-claude-yolo.yml` and `play-agent-bus.yml` (CCY 3.99.9,
container 2.74). The first ccy launch after it rebuilds the image once.

### What runs by itself (`_acceptance-m4.inc.bash`)

After M3, `acceptance.bash` builds two throwaway teams, `acc-m4a` and `acc-m4b` (human
`tester`), declared only in a host_vars file in the run directory, and starts two throwaway
headless sessions in ccy's tmux: A on `1@acc-m4a` and B on `2@acc-m4a`. It removes both
sessions, both teams and their seat directories on the way out, however the run ends. Each
check is judged from what `ccy-teams` printed and exited, the membership files, the records,
`agent-bus seat list` and the team's roster, never from the model's prose.

| Step | What passes                                                                                                                                                                                                                                                                                                           |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | `session role A acc-m4a coordinator --yes` exits 0 with `✓ role: ... set to coordinator` and `✓ session: told it is acc-m4a's coordinator`; the roster shows it. The same command again changes no role, record or membership.                                                                                        |
| (b)  | `session remove A acc-m4a --yes` exits 64 (A is the coordinator, no `--coordinator`) and changes nothing. `session move A acc-m4a acc-m4b --coordinator=hand-to=2 --yes` exits 0: A desires only acc-m4b, B's seat is acc-m4a's coordinator, seat 1 is free. The move again is a no-op.                               |
| (c)  | Two `session add A acc-m4a` at once: the second exits 75, "another change to it is in progress: ccy-teams session add ...", and the first completes (H20).                                                                                                                                                            |
| (d)  | With acc-m4b's homeserver stopped: `session add B acc-m4b` and `session role A acc-m4b coordinator` exit 1 and change nothing (H26); `session remove A acc-m4b` exits 1 with "goodbye and status not sent: homeserver down" and the seat free. With it started again, the same remove exits 0 with the goodbye (H19). |
| (e)  | B's seat keeper killed: `session list` says `unguarded: ccy-teams session repair B`; `session repair B --yes` exits 0, `✓ keeper: running again, 1 seat held`; the list says `ok`; then `session remove B acc-m4a --coordinator=demote --yes` takes out its launch seat, which reads free.                            |
| (f)  | A's keeper killed, `session add A acc-m4b` started, and A's container killed while the add waits for it: the add exits 1, "session ended during the add", `✓ membership: removed`; A's record names no acc-m4b and no desired membership is left (H28).                                                               |

### The owner's steps

| Step | What to do                                                                                                                                                                                                                                             | What passes                                                                                                                                            |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| (g)  | Two checkouts of team repositories on this machine, both of one team. In each, in ccy's tmux, start a plain `ccy`. From another terminal: `ccy-teams session add <session-1> <team>/1 --yes`, then `ccy-teams session add <session-2> <team>/1 --yes`. | Both exit 0: seat 1 of one checkout is not seat 1 of the other (H21). `ccy-teams session list` shows each session in `<team> (1, worker)`.             |
| (h)  | On a terminal, with a session that is a team's coordinator: `ccy-teams session remove <session> <team>` (no `--yes`, no `--coordinator`).                                                                                                              | It asks first whether the seat becomes a worker as it leaves (yes by default), then the remove's own question. Answering no to either changes nothing. |
| (i)  | `ccy-teams session list` on a terminal.                                                                                                                                                                                                                | A table (SESSION, CHECKOUT, TEAMS (seat, role), STATE) and one line saying only sessions started through ccy's tmux can be changed.                    |
