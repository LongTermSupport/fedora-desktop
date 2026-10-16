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

| Step | What passes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | Where                         |
| ---- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------- |
| (a)  | `ccy --teams` works as before: the seats are held, the launch block, `pingbus status`, delivery and the goodbye.                                                                                                                                                                                                                                                                                                                                                                               | Plan 00201 M7, Plan 00161 U20 |
| (H1) | The plain session's membership bind asks for `Z`, and its directory and file carry `container_file_t` at the container's own MCS level, not `user_tmp_t`. Enforcing: the container reads them. Permissive: one line says enforcement was not exercised.                                                                                                                                                                                                                                        | `_acceptance-m2.inc.bash`     |
| (b)  | The plain session reads `/run/ccy/membership/membership.json` (no seat). A `touch`, a write and a remount there all fail (H3). It has no `active` flag, and its status line is the guard around its own. Its claude was launched in bypass mode with `--settings` naming the session's settings, which say `crossSessionInbound: refuse`, and no managed settings override it (Decision 1). Claude Code's own inbox socket is there in every session, so its presence is reported, not judged. | `_acceptance-m2.inc.bash`     |
| (c)  | A prompt's hook ends in the shell (no `python3`, no `pingbus`), at most one `pingbus` process runs, and idle CPU over a minute matches the `--no-bus` session.                                                                                                                                                                                                                                                                                                                                 | `_acceptance-m2.inc.bash`     |
| (d)  | The session image is the launcher's `REQUIRED_CONTAINER_VERSION` (2.71), with the M2 entrypoint.                                                                                                                                                                                                                                                                                                                                                                                               | `_acceptance-m2.inc.bash`     |
| (e)  | `--no-bus`: `PINGBUS_MEMBERSHIP=off`, no mount, no `pingbus`, no plugin, no settings.                                                                                                                                                                                                                                                                                                                                                                                                          | `_acceptance-m2.inc.bash`     |
| (f)  | The spike's result is in the spec (H34). Claude Code does not re-read `--settings` mid-session, so a session on no team is launched with no inbound, and its first join relaunches it once.                                                                                                                                                                                                                                                                                                    | `_acceptance-m2.inc.bash`     |

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

| Step | What passes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | `session role A acc-m4a coordinator --yes` exits 0 with `✓ role: ... set to coordinator` and `✓ session: told it is acc-m4a's coordinator`; the roster shows it. The same command again changes no role, record or membership.                                                                                                                                                                                                                                                                                                                                        |
| (b)  | `session remove A acc-m4a --yes` exits 64 (A is the coordinator, no `--coordinator`) and changes nothing. `session move A acc-m4a acc-m4b --coordinator=hand-to=2 --yes` exits 0: A desires only acc-m4b, B's seat is acc-m4a's coordinator, seat 1 is free. The move again is a no-op.                                                                                                                                                                                                                                                                               |
| (c)  | Two `session add A acc-m4a` at once. A's keeper is killed first, so the first add commits and then holds the change lock through its 10 s wait. The second, run in that window, exits 75, "another change to it is in progress: ccy-teams session add ...". The first is rolled back (exit 1); after `session repair A` the add is made for real (H20).                                                                                                                                                                                                               |
| (d)  | With acc-m4b's homeserver stopped: `pingbus status-line`, run under `podman exec` in A's container with the session home given by hand, shows `acc-m4b(down)` within 60 s (H26; this proves the status line's code path over the watcher's real mark, not claude's own rendering); `session add B acc-m4b` and `session role A acc-m4b coordinator` exit 1 and change nothing (H26); `session remove A acc-m4b` exits 1 with "goodbye and status not sent: homeserver down" and the seat free. With it started again, the same remove exits 0 with the goodbye (H19). |
| (e)  | B's seat keeper killed: `session list` says `unguarded: ccy-teams session repair B`; `session repair B --yes` exits 0, `✓ keeper: running again, 1 seat held`; the list says `ok`; then `session remove B acc-m4a --coordinator=demote --yes` takes out its launch seat, which reads free.                                                                                                                                                                                                                                                                            |
| (f)  | A's keeper killed, `session add A acc-m4b` started, and A's container killed while the add waits for it: the add exits 1, "session ended during the add", `✓ membership: removed`; A's record exists and names no acc-m4b, and no desired membership is left (H28).                                                                                                                                                                                                                                                                                                   |

### The owner's steps

| Step | What to do                                                                                                                                                                                                                                             | What passes                                                                                                                                            |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| (g)  | Two checkouts of team repositories on this machine, both of one team. In each, in ccy's tmux, start a plain `ccy`. From another terminal: `ccy-teams session add <session-1> <team>/1 --yes`, then `ccy-teams session add <session-2> <team>/1 --yes`. | Both exit 0: seat 1 of one checkout is not seat 1 of the other (H21). `ccy-teams session list` shows each session in `<team> (1, worker)`.             |
| (h)  | On a terminal, with a session that is a team's coordinator: `ccy-teams session remove <session> <team>` (no `--yes`, no `--coordinator`).                                                                                                              | It asks first whether the seat becomes a worker as it leaves (yes by default), then the remove's own question. Answering no to either changes nothing. |
| (i)  | `ccy-teams session list` on a terminal.                                                                                                                                                                                                                | A table (SESSION, CHECKOUT, TEAMS (seat, role), STATE) and one line saying only sessions started through ccy's tmux can be changed.                    |

## M5: hot control from the picker

After `meta-deploy.bash` has run `play-claude-yolo.yml`, which rebuilds `ccy-teams`. M5 changes
only the picker, which needs a person at a terminal, so every step is the owner's. Each change
the picker makes is the `ccy-teams session` command of M3 and M4 (SPEC-hot-control.md, M5 as
built), and `acceptance.bash` proves that command's data path again through M4's (e). There is
no `_acceptance-m5.inc.bash`.

### The owner's steps

Use one plain `ccy` and one `ccy --teams 1@<team>` session, both started in ccy's tmux in a
checkout of a team on this machine, and a terminal outside them.

| Step | What to do                                                                                                                                                                | What passes                                                                                                                                                                                                                     |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | `ccy-teams`, then Tab.                                                                                                                                                    | The Running sessions view: both sessions, their checkout, teams (seat, role) and state, and the line saying only sessions started through ccy's tmux can be changed. Tab, or Esc, goes back to the teams.                       |
| (b)  | On the plain session, Ctrl-A: choose the team, the lowest free seat, worker. Answer yes.                                                                                  | The add's own question and its steps, ending in the one relaunch (it started on no team), as M3 (g). Back on the list, the session shows `<team> (<seat>, worker)`. A team whose homeserver is down is refused and asked again. |
| (c)  | On the same session, Ctrl-O: "Make it the coordinator (<team> has none)", or, if the team has a coordinator, "Replace the coordinator, ...".                              | The two paths of the next section. The role's steps end in `✓ session: told it is <team>'s coordinator`, and the row shows `coordinator`.                                                                                       |
| (d)  | Enter on it, Move to another team, choose a second team (needs a second team on this machine; skip it otherwise).                                                         | It asks what becomes of the coordinator role; "The role goes to no one" gives `--coordinator=demote`. The move's steps end in "Moved", and the row shows only the second team.                                                  |
| (e)  | On it, Ctrl-X. Answer yes.                                                                                                                                                | The remove's steps, with `✓ re-baseline` and the goodbye; the row shows `-` for teams. The seat reads `free` in `agent-bus seat list`.                                                                                          |
| (f)  | In the seated session's container, stop the seat keeper (`podman exec <container> pkill -f 'pingbus seat keep'`). Then, in the view, Enter on it: Repair the seat keeper. | The menu has Repair only while the state is `unguarded`. The repair's steps end in `✓ keeper: running again, 1 seat held`, and the state is `ok` again.                                                                         |

## M6: cc sessions

After `meta-deploy.bash` has run `play-claude-yolo.yml` (CCY 3.99.12.3, container 2.77.2; it
deploys cc, `cc-teams.bash`, `ccy-teams` and the host's pingbus kit). M6 changes nothing
`play-agent-bus.yml` deploys, so that play need only have run once, as for M3 and M4. M6 runs on
the host only: a cc session has no container. It needs the Element
consent record (`~/.config/pingbus/element-accepted`); with none, it stops at once and says to
run `cc --teams` once from a terminal and accept.

### What runs by itself (`_acceptance-m6.inc.bash`)

After M4, `acceptance.bash` builds two throwaway teams, `acc-m6a` and `acc-m6b` (human
`tester`), declared only in a host_vars file in the run directory, and starts throwaway cc
sessions in ccy's tmux, each with a record (`restore=no`) as cc's own tmux start writes one.
Each pane runs the deployed `cc-teams.bash` from its seats on (`cc_teams_command`), so the
membership is written by `ccy-teams internal membership-init --kind=cc`, and `pingbus seat exec`
and its keeper are the production ones. Two steps are not cc's: the seats are taken from this
terminal, where sudo is primed, and `claude` is a stand-in that records its pid and sleeps. It
removes the sessions, their records, membership directories, reports and private homes
(`cc-teams.XXXXXX`, which a tmux kill ends the driver before it removes), both teams and their
seat directories on the way out, however the run ends.

| Step | What passes                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | `session move A acc-m6a acc-m6b --yes` on cc session A exits 0 with `✓ route: this host reaches acc-m6b's homeserver` and `Moved: ...`; A desires one seat of acc-m6b only, its record names it, seat `1@acc-m6a` reads free, and `session list` shows A as kind `cc`, `ok` (H31).                                                                                                                                                                                                                                                                                                                                                                                                    |
| (b)  | Audit F5: A's stand-in claude started a detached `setsid sleep 600`. With A's claude ended, its seat reads free within 5 s while the sleep still runs; B, started at once on the same seat, holds it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| (c)  | `cc --bus` (Decision 7): C starts on no team with `crossSessionInbound: refuse` in its settings, and its keeper reports no inbound. `session add C acc-m6a --yes` exits 1, saying C is in acc-m6a but cc must be ended and started again from `ccy-sessions`; no relaunch is asked, and the desired seat and the record stay. The same add again says it again. Started again on its record's seat, C accepts (settings and keeper), and the add is "Nothing to do". With C's keeper killed, the list says `unguarded: end it ...` and `session repair C --yes` exits 78 (a cc keeper cannot be started from outside). `session remove C acc-m6a --yes` leaves the record on `--bus`. |
| (d)  | A reused name (Fable's B1): D runs a plain cc's stand-in under a name with a cc membership left on `1@acc-m6a`, and D's record, as its own tmux start writes it, has neither `--teams` nor `--bus`. `session list` does not show D; `session add D acc-m6b --yes` exits 78, "it was started without --teams or --bus"; and `session add B acc-m6a/1 --yes` exits 0: the membership left under D's name reserves no seat.                                                                                                                                                                                                                                                              |

### The owner's steps

| Step | What to do                                                                                                                                                                              | What passes                                                                                                                                                   |
| ---- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (d)  | In a team repository, in ccy's tmux, start `cc --teams <team>` and pick a token. From another terminal: `ccy-teams session move cc-<id> <team> <other-team>`, answering the question.   | It exits 0; the session is told it moved, and `pingbus status` in it shows only `<other-team>`. Its seat in `<team>` reads free.                              |
| (e)  | Start `cc --bus` in ccy's tmux. It asks the consent alone (no team list). Then `ccy-teams session add cc-<id> <team> --yes`; then `/exit` in cc and start it again from `ccy-sessions`. | The add exits 1 and says plainly that cc must be started again. Started again, cc is on `<team>` without being asked, and a message from the team reaches it. |
| (f)  | End a `cc --teams` session with `/exit`, then restart it from `ccy-sessions`.                                                                                                           | It starts on the seats its record names, the ones the last hot change left, not its first `--teams`.                                                          |

## The coordinator's two paths (owner feedback on M5)

After `meta-deploy.bash` has run `play-claude-yolo.yml`. A team has one coordinator, so making
a member the coordinator is one of two different questions: a team with none is offered one
plainly; a team that has one is a takeover, which names the current coordinator, says whether
it is running, says it loses the role, and starts on No (SPEC-hot-control.md H25). Every place
that can make a coordinator asks it that way. Use a team with no coordinator, two sessions on
it, and a terminal outside them.

| Step | What to do                                                                                                   | What passes                                                                                                                                                                                                                                                                                                                                                                              |
| ---- | ------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (a)  | Restart a session from ccy-sessions (or `ccy --teams <team>`) and choose its existing seat.                  | The role question reads "... <team> has no coordinator." and offers "Keep as is: worker" (selected) and "Make it the coordinator (<team> has none)". Choosing it asks "<team> has no coordinator. Make <handle> its coordinator?". Say yes.                                                                                                                                              |
| (b)  | Restart the other session the same way.                                                                      | The question reads "<team>'s coordinator is <handle> (running), and a team has one" and offers "Replace the coordinator, <handle> (running): it becomes a worker...". Choosing it asks "<team>'s coordinator is <handle> (running). A team has one coordinator. Remove <handle> as coordinator and make <handle-2> the coordinator instead?", starting on No; Esc or No changes nothing. |
| (c)  | `ccy-teams`, Enter on a worker seat of the team.                                                             | The menu lists "Replace the coordinator, <handle> (running): ..." last, after "Remove seat N from <team>". Never a plain "Make coordinator".                                                                                                                                                                                                                                             |
| (d)  | `ccy-teams`, Tab, Ctrl-O on the worker session.                                                              | "worker (now)" first, then "Replace the coordinator, ...". Choosing it runs `session role ... --demote-current`, whose one question is the takeover, starting on No.                                                                                                                                                                                                                     |
| (e)  | `ccy-teams session role <worker-session> <team> coordinator --yes`.                                          | Exit 64: "<team>'s coordinator is <handle> (running). A team has one coordinator: to remove <handle> as coordinator and make <session> (seat N, <handle-2>) the coordinator instead, add --demote-current. Nothing was changed."                                                                                                                                                         |
| (e2) | Only with a running LXC container to add: `ccy-teams lxc-add <team>` on the team that now has a coordinator. | The Role step names "<team>'s coordinator is <handle> (...), and a team has one" and offers "worker" (selected) and "Replace the coordinator, <handle> (...): it becomes a worker...". Choosing it asks the takeover, on No; No says the member joins as a worker. It never makes the member a worker without asking.                                                                    |

## Which team a launch question is about (owner feedback on Plan 00201 S-C1)

| Step | What to do                                                                                           | What passes                                                                                                                                                                                                                                                                                                                                    |
| ---- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| (i)  | In a checkout, `ccy --teams`, mark two or three teams (one that does not cover the checkout), Enter. | Each question is headed by its team and place: "Team 2 of 3: <team> · scope: ...", "Team 1 of 3: <team> · seat: which seat does this session take?", "Team 1 of 3: <team> · role of seat N: ...". Each team's lines (the "references external" note, "Taking seat ...") come right after that team's heading line, never under another team's. |

## The picker's offers: now, not now, or never (owner feedback on Plan 00201 S-A15)

| Step | What to do                                                                                     | What passes                                                                                                                                                                                                                                      |
| ---- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| (f)  | `ccy-teams` with a team that has no description (one answered No under the old prompt counts). | "An earlier no to describing <team> ... is taken as not now", then "Team <team> (...) has no description." with "Describe <team> now", "Not now (ask again next time)" and "Never ask about <team>". Describing asks "What is team <team> for?". |
| (g)  | Answer "Not now" (or Esc), leave, start `ccy-teams` again.                                     | It asks again.                                                                                                                                                                                                                                   |
| (h)  | Answer "Never ask about <team>", leave, start again; then Enter on the team row.               | Not asked; the team row's menu has "Ask me to describe <team> again", after which the next start asks.                                                                                                                                           |
