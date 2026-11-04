# 00209 research: Go (and launcher) callers of agent-bus, and where an open's time goes

Read-only survey at 442143f5. Paths are relative to the repo root. **[I]** marks an inference
(not read directly from code or measured).

## 0. Headline

- Every Go read of the bus is a **serial subprocess**. Nothing in `board`, `agentbus` or `declare`
  runs in parallel (the only goroutines in `internal/teams` are `flow/kit.go:264` and `ops/playlock.go:171,185`,
  neither of them collection).
- An interactive open (`ccy-teams` with no argument) starts Python **7 + C + T + H** times, where
  C = the distinct checkouts that have `.claude/ccy/pingbus/seats/`, T = teams whose homeserver is up,
  H = people across those teams. With C=6, T=3, H=6 that is 22 Python starts, about 10 s at 0.45 s
  each, before HTTP, sudo and runuser are counted. That is consistent with the 9-23 s you observed **[I]**.
- **"seat list runs 6 times" means six checkouts, not six passes.** `readSeats` runs one
  `agent-bus seat list` per checkout in `Snapshot.Checkouts` that has a seats directory
  (`board/collect.go:342-362`), and Collect runs once on an open. The checkouts come from four places:
  live launcher tmux sessions (`collect.go:212-214`), every ccy/cc session record in the registry,
  including stopped ones (`snapshot.go:202-206`), live desired memberships (`snapshot.go:213-217`),
  and the working directory when it is a host checkout (`collect.go:284-286`). So on your host six
  checkouts have a seats directory **[I: count not seen, but no other path calls SeatList on open]**.
- Measured in this container (`untracked/scratch/00209/time_py.py`, best of 5): bare
  `python3 -c pass` takes 33 ms. `import helpers.agent_bus.declare` takes 400 ms,
  `helpers.agent_bus.cli` 356 ms and `helpers.agent_bus.checkout` 420 ms. **The cost is the imports,
  not the interpreter.** `-X importtime` shows typing, pathlib, json, http.client, urllib and email,
  plus `pingbus.protocol`, `limits` and `forge` through `registry` and `teamfile`. `bundle.build('agent-bus')`
  alone took 183 ms.
- **[I]** On the host, the wrapper runs `/usr/bin/python3 -I agent-bus.pyz`. Imports from a zipapp
  go through zipimport, which never writes a `.pyc`, and `bundle.collect` packs only `*.py`
  (`helpers/pingbus/bundle.py:71-86`). So every start compiles every bus module from source, and the
  host figure is probably worse than the cached figures above.

## 1. Every call from src-go into the bus

Only `internal/agentbus`, `internal/declare` and `internal/teams/hot` (the host pingbus) start bus
code. `ccy-sessions` (`internal/sessions`) never calls agent-bus. `conversation.go:5` only reads a
file that a pingbus hook writes. `lxcfreeze` uses sudo for lxc-\* and one `python3 -I -c` script, not
the bus.

### 1a. `agentbus.Client` (src-go/internal/agentbus/agentbus.go)

User calls go through `userRun` (`:271`), which runs `execx.Cmd{Name:"agent-bus", Dir:dir}` on PATH,
then the bash wrapper, then `exec /usr/bin/python3 -I /usr/local/lib/agent-bus/agent-bus.pyz`
(`files/usr/local/bin/agent-bus:61-63,155-159`). Root calls go through `rootRun`/`rootShow`
(`:276-284`), then `sudo -n <abs path> …` (`sudo/sudo.go:126-163`), then bash, then
`runuser -u agent-bus -- /usr/bin/python3 -I agent-bus.pyz` (`agent-bus:55-58`).

| Method (line)                           | argv                                                                    | Callers (func, file:line), flow                                                                                                                                                                                                                                                                                                                                                           | Cached?                                                                                                                                                                   |
| --------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SeatList (:110)                         | `agent-bus seat list` (user, Dir=checkout)                              | board `readSeats` collect.go:349 (open, redraw, --list, login, shutdown, restart); choose `describer.handle` choose.go:404 (describe-seats); hot `pickSeat` add.go:144, `seatHeld` hot.go:618, `checkoutSeats` list.go:240 (Tab sessions view, `session list`), `running` role.go:126; restart `heldSeats` jobs.go:406; shutdown `heldSeats` stop.go:294; seats `NewSession` seats.go:174 | hot `listCache.seats` per checkout per list (list.go:230-240); describer loads once per run (choose.go:401-410); **board: not cached, once per checkout per Collect**     |
| SeatChooseAs/In (:134,:152)             | `agent-bus seat choose TEAM [--type=]` (user)                           | choose `PickSeat` choose.go:89 (launcher choose-seat/choose-teams); chooser `handles` consent.go:174 (one per ref); hot `chooseSeat` add.go:199 (session add); picker `askSeat` sessions.go:319 (Tab view, add to team)                                                                                                                                                                   | no                                                                                                                                                                        |
| SeatTake/SeatTakeAllocated (:141,:147)  | `agent-bus seat take LIST --type=T [--no-prompt] [--alloc-held]` (user) | hot `makeSeat` add.go:280 (session add)                                                                                                                                                                                                                                                                                                                                                   | n/a (it writes)                                                                                                                                                           |
| SeatRemove (:169)                       | `agent-bus seat remove REF` (user)                                      | hot `farewell` remove.go:143; seats `removeSeat` seats.go:259                                                                                                                                                                                                                                                                                                                             | n/a                                                                                                                                                                       |
| List (:175)                             | `sudo -n /usr/local/bin/agent-bus list TEAM`                            | board `collectMembers` collect.go:292 (**per team that is up**, every Collect); choose `offerRole` choose.go:267, `describer.roleWords` choose.go:382; hot `teamUp` add.go:111, `coordinatorSaid` move.go:50, `setRoleTo` role.go:26, `leaveRole` role.go:285; seats `ChangeRole` seats/role.go:26; settings `readRoster` settings.go:100                                                 | describer per team per run (`d.rosters`); **board: not cached**                                                                                                           |
| HumanDevices (:203)                     | `sudo -n …/agent-bus human devices TEAM NAME`                           | board `collectMembers` collect.go:313 (per person of each up team)                                                                                                                                                                                                                                                                                                                        | **yes**: `Collector.devices` keyed `team\|name` for the process's life (collect.go:57-59,308-311), dropped by `ForgetDevices` (collect.go:102); a failed read is not kept |
| HumanPassword (:217)                    | `sudo -n …/agent-bus human password TEAM NAME`                          | login `IssuePassword` login.go:216                                                                                                                                                                                                                                                                                                                                                        | n/a                                                                                                                                                                       |
| ParkMember (:188)                       | `sudo -n …/agent-bus park-member TEAM HANDLE` (streams)                 | seats `park` seats.go:280                                                                                                                                                                                                                                                                                                                                                                 | n/a                                                                                                                                                                       |
| SetRole (:194)                          | `sudo -n …/agent-bus set-role TEAM HANDLE --role=R`                     | roles `Setter.step` roles.go:229,235 (with rollback)                                                                                                                                                                                                                                                                                                                                      | n/a                                                                                                                                                                       |
| Backups (:230)                          | `sudo -n /usr/local/sbin/agent-bus-install backups --team T`            | ops `chooseBackup` takedown.go:376                                                                                                                                                                                                                                                                                                                                                        | n/a                                                                                                                                                                       |
| BackupNow/Restore/RemoveTeam (:243-259) | `agent-bus-install backup-now / restore --backup ID / remove [--purge]` | ops `backupFirst` takedown.go:181, `Backup` :296, `Restore` :334, `takeDownUndeclared` :222                                                                                                                                                                                                                                                                                               | n/a                                                                                                                                                                       |

The sudoers NOPASSWD rule (`files/etc/sudoers.d/agent-bus-read-only.j2:9-10`) covers only
`list TEAM` and `human devices TEAM NAME`. Every other root call needs `Sudo.Ready` first.

### 1b. `declare.Client` (src-go/internal/declare/declare.go:581)

Each call runs `env PYTHONPATH=<RootDir> python3 -P -m helpers.agent_bus.declare SUB [--file <host_vars>] …`
as the user, from the checkout, not the pyz. Subcommands and callers:

- overview (`Overview` :260), which runs `declared` + `status`: board `collectTeams` collect.go:169,
  through `Refresh` (picker.go:127, offers.go:318, ops.go:142,395, takedown.go:73,271,
  login.go:233) and `Collect`.
- declared (`chooser.go:316`, `choose.go:202`, `lxcadd/adder.go:265`); limited (`offers.go:108`);
  undescribed (`offers.go:221`); github-remote (`offers.go:354`, `chooser.go:286`, `find.go:76`,
  `wizard.go:272`); teams-for (`offers.go:381`, `find.go:40`, `wizard.go:1011`); choices
  (`chooser.go:314`); element-teams (`ops.go:304`); settings, set and check-set (settings.go);
  create, check, suggest-team, address, check-address and next-steps (create wizard); describe,
  drop-limit, add-repo, element and remove; the lxc-\* family (`lxc.go:58-179`, lxcadd and roles).
- No caching anywhere.
- `status` is heavy inside one process. It parses host_vars, runs `ip -j -4 addr show dev <bridge>`
  (`bridge.py:46-60`), reads every stamp, and rebuilds the whole agent-bus zipapp in memory to hash
  it (`declare.py:461-470`, `bundle.build`, measured 183 ms) **on every overview**, so on every open
  and every redraw.

### 1c. The host pingbus and in-container pingbus (internal/teams/hot)

- `Hot.pingbus` (`remove.go:156`) runs `execx.Cmd{Name: cfg.Pingbus}`, default
  `/usr/local/share/agent-bus/kit/pingbus` (`config.go:45`). It is used for `seat retire` and
  `seat farewell` (`remove.go:120,125`, hot remove) and `reach --team T --root R` (`cc.go:120`,
  cc hot add).
- `sessprobe.ExecIn(container, "pingbus", "reach", "--team", T)` runs in `add.go:302` (podman hot add).
- `ExecDetached(container, "pingbus", "seat", "keep")` runs in `list.go:398` (hot repair).
- Keeper liveness is a process grep (`cc.go:113-115`), not a bus call.

### 1d. Count per flow

**Interactive open** (`picker.go:123-149` then the first `once` at :235; no offer fires, sudo not
needed, cwd is a host checkout with a GitHub origin):

| #   | Step                                        | Processes                                                                                                                                                                                           |
| --- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `Board.Refresh` picker.go:127               | systemctl list-units; **py declare overview**                                                                                                                                                       |
| 2   | `offerPathLimits` offers.go:108             | **py declare limited**                                                                                                                                                                              |
| 3   | `offerDescriptions` offers.go:221           | **py declare undescribed**                                                                                                                                                                          |
| 4   | `offerUpdate` offers.go:318                 | systemctl; **py declare overview** (a second Refresh)                                                                                                                                               |
| 5   | `pickerFrame` picker.go:182                 | git ×2 (ProjectHere, TopLevel)                                                                                                                                                                      |
| 6   | `collectWithSudo` → `Collect` picker.go:356 | systemctl; **py overview**; tmux list; registry and desired files; git ×2 (workTree, asked once per process); **py seat list ×C**; **sudo→bash→runuser→py list ×T**; **sudo→…→py human devices ×H** |
| 7   | `offerJoinHere` offers.go:359               | git ×2 (Find again); git remote; **py github-remote**; **py teams-for**                                                                                                                             |

That is 7 declare runs, of which overview runs 3 times (steps 1, 4 and 6 read the same
declaration), plus C + T + H.

- If `NeedSudo` is set (no sudoers rule, or a cold credential), `sudo -n -v` and `sudo -n true` run,
  then the **whole Collect runs again** (picker.go:363-371).
- Each **redraw** (after every action or Esc back to the list, `loop` picker.go:225) costs
  1 overview + C seat lists + T lists, with devices cached. With C=6 and T=3 that is 10 Python starts,
  about 4.5 s **[I]**.
- `TestARedrawStartsFewProcessesAndTheFirstDrawReadsEachPersonOnce`
  (`board/redraw_cost_test.go:116-138`) pins Collect at 3 teams × 3 people, one checkout: 17
  processes on the first draw and 7 on a redraw. It covers Collect only, not the offers or the open's
  extra Refreshes.

**`--list`**: one Collect, which is step 6 alone.

**Tab (sessions view)**: `Hot.Sessions` (list.go:55) runs podman ps, the cc pane scan, and seat list
once per session checkout (cached per list) plus `HomeserverState` per team (cached).

**Launcher `ccy --teams`**: covered in section 3. Inside `ccy-teams choose-teams`: github-remote,
then choices or declared; per team `seat choose`, which itself starts systemctl, git ×2 and a nested
`sudo -n agent-bus list` (`checkout.py:902-917,920-949`), so 2 Python starts; and `list` for the role
question (`choose.go:267`).

## 2. Marker contracts Go parses

`marker.Fields(out, M, n)` (`internal/marker/marker.go`) takes lines equal to `M` or starting `M\t`,
requires exactly n tab fields, and skips other lines. `DashEmpty` turns `-` into "".

| Producer          | Marker (fields)                                                                                                                                                                                                                                                                                                | Go uses                                                                                              |
| ----------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| seat list         | `SEAT team seat held\|free self\|- handle` (6)                                                                                                                                                                                                                                                                 | all five; Self is parsed but the board uses Team/Seat/Held/Handle (parse.go:140-165)                 |
| list              | `HUMAN name join\|invite\|-` (3); `MEMBER handle role membership active\|parked` (5)                                                                                                                                                                                                                           | name, membership; handle, role (`orchestrator` → coordinator), membership, parked (parse.go:168-196) |
| human devices     | `DEVICE id last_seen_ms\|-` (3)                                                                                                                                                                                                                                                                                | **only the count** (`len(devices)`, collect.go:316)                                                  |
| seat choose       | `CHOICE seat free\|held\|new handle words` (5), `DEFAULT seat` (2)                                                                                                                                                                                                                                             | strict: any other line is an error, and DEFAULT must be a row (parse.go:116-137)                     |
| installer backups | `BACKUP id time` (3)                                                                                                                                                                                                                                                                                           | both                                                                                                 |
| human password    | first stdout line = password                                                                                                                                                                                                                                                                                   | agentbus.go:222                                                                                      |
| declare overview  | `TEAM name port\|- present\|absent server_name url\|- desc-json\|-\|!vaulted\|!invalid` (7) + `STATUS team state detail\|-` (4)                                                                                                                                                                                | all (declare.go:200-292)                                                                             |
| declare (others)  | `LIMITED team prefixes trusts` (4), `UNDESCRIBED team trusts` (3), `ELEMENT team base_url` (3), `TEAM name` (2, teams-for), `CHOICE team standing desc` (4), `SETTING key json\|-\|!vaulted\|!invalid` (3), `CHANGED\|UNCHANGED\ttext`; github-remote/address/suggest-team as one plain line; lxc-\* in lxc.go | all                                                                                                  |

Exit codes are contracts too:

- agent-bus: 64 usage, 69 unreachable, 70 refused, 75 busy, 77 not root, 78 config
  (agentbus.go:24-52). `seat list` exiting 78 means "partial output, some seat unreadable", not
  failure (:110-124).
- `sudo -n` asking for a password is detected by the exact stderr line `sudo: a password is required`
  (sudo.go:147-153).
- declare: 64, 78, 130 (declare.go:22-60).

## 3. The ccy launcher's bash side (files/var/local/claude-yolo)

All of these run on the host, before the container starts or after it ends:

- `claude-yolo:854` runs `ccy-teams choose-teams --type=podman LIST` (Go; section 1d).
- `claude-yolo:858` runs `ccy_seats_check` (`lib/agent-bus-seats.bash:36-61`):
  `agent-bus seat check LIST [--resolved]`, one Python start, plus git inside.
- `claude-yolo:1248` runs `ccy-teams describe-seats --prefix`. Go starts declare declared,
  seat list ×1 and list ×T.
- `claude-yolo:1278` runs `ccy-teams record-seats` and `:3786` runs `ccy-teams internal restart-args`
  (both Go).
- `claude-yolo:3882` runs `ccy_seats_take` (`agent-bus-seats.bash:78-90`). That is
  `ccy-teams internal membership-init` (Go, `agent-bus-membership.bash:62`), then
  `agent-bus seat take CANON [--no-prompt]` (Python; it can nest `sudo agent-bus add-member`, and so
  runuser and Python, `checkout.py:504-511`), then `ccy-teams describe-seats --block`.
- `claude-yolo:2413` runs `ccy_bus_disconnected` (`agent-bus-membership.bash:81-93`):
  `timeout 15+ /usr/local/share/agent-bus/kit/pingbus seat disconnected --root … --session … --since …`.

**[I]** One `ccy --teams` launch therefore starts roughly 6-10 bus Python processes on the host.

## 4. Existing timing and performance notes

- `CLAUDE/Plan/00192-…/JOURNAL/00192-Journal-26-10-14.md:106,119-127`: the "per-redraw process fan-out"
  was cut from 21 to 7 processes for 3 teams × 3 humans; `declare overview` was added to merge
  declared and status. It took no wall-clock figure.
- `CLAUDE/Plan/00192-…/subagent-reports/261014-sessions-perf-sonnet.md` covers ccy-sessions only:
  - The CPU column's 1 s sleep per redraw became a /proc diff.
  - The transcript poll went from 401 ms to µs through an incremental cache.
  - `TestOneDrawRunsNoCommandTwice` pins that no argv runs twice in a draw.
  - It does not cover the bus.
- `src-go/internal/teams/board/redraw_cost_test.go` counts processes; it does not measure time.
- Nothing in any plan, journal or report records the 9-23 s open, the 0.45 s per start, or "6×".
  Plan 00209's PLAN.md and journal are still the scaffold.

## 5. Latency budget for an open (C=6, T=3, H=6; [I] estimates from the 0.40-0.45 s start)

| Read                                                      | Count | Each                                                                                       | Total   | Serial? | What it needs                                                                                                                                                                                                    |
| --------------------------------------------------------- | ----- | ------------------------------------------------------------------------------------------ | ------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| declare overview                                          | 3     | ~0.45 + 0.18 zipapp build + `ip`                                                           | ~1.9 s  | yes     | **files only**: host_vars (user-readable), `/var/lib/agent-bus-applied/*.json` stamps (group-readable), source tree hash, the bridge address. In-process in Go; the software digest needs a new definition in v2 |
| declare limited / undescribed / github-remote / teams-for | 4     | ~0.45                                                                                      | ~1.8 s  | yes     | files only (host_vars), and github-remote is a pure string function                                                                                                                                              |
| seat list                                                 | C=6   | ~0.45 + git rev-parse                                                                      | ~2.8 s  | yes     | **user files**: `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/member.json` and the seat lock state (`pingbus/seat.py:295-309`). In-process, no privilege                                                   |
| sudo list                                                 | T=3   | sudo + bash + runuser (PAM) + ~0.45 + HTTP: whoami + 1 membership per human and per member | ~2 s    | yes     | **privilege** (admin token in `/var/lib/agent-bus/<team>/secrets`, 0700 agent-bus; the registry there too) **and homeserver HTTP**                                                                               |
| sudo human devices                                        | H=6   | sudo + runuser + ~0.45 + HTTP whoami + account + devices                                   | ~3.5 s  | yes     | **privilege and HTTP** (admin API); only the count is used                                                                                                                                                       |
| systemctl ×2-3, git ×6-7, tmux                            | ~10   | 5-20 ms                                                                                    | ~0.15 s | yes     | unprivileged; the systemd unit state could also be read over D-Bus                                                                                                                                               |

The sum is about 12 s, which is in the observed range. The Python starts alone are about 22 × 0.45,
roughly 10 s.

**Path to about 1 s \[I\]:**

1. Move declare into Go: drops 7 starts, about 3.7 s.
2. Read seats in-process: drops 6 starts, about 2.8 s.
3. Make one privileged call for **all teams**, as a Go binary under sudo (about 20-40 ms of
   start-up), with the per-team and per-person HTTP calls done in parallel (one connection to each
   localhost homeserver, about 1-5 ms per call). This replaces T + H processes, about 5.5 s,
   with about 0.1 s.
4. Read the declaration once per open, not three times.
5. Only a long-running privileged daemon or socket would remove sudo and runuser too. One batched
   sudo call is probably enough for 1 s.

## 6. What changes in the Go adapters if agent-bus is Go in the same module

- **`internal/agentbus/agentbus.go`**:
  - The exec layer (`userRun`, `rootRun`, `rootShow`, `classify`, `CommandError`, `kindOf`) goes
    away for the user-side reads (seat list, seat choose, seat check). They become direct calls that
    return typed values and errors.
  - The exit codes 64/69/70/75/77/78 become sentinel errors. `ErrConfig` meaning "seat list was
    partial" becomes an explicit `[]failure` return.
  - Root calls still cross a process boundary because of the privilege, but into a Go entry point
    (the same module's command run through sudo). The marker lines could then become one versioned
    JSON or gob value, or stay as the stable CLI contract for the bash launcher.
- **`parse.go`** (SEAT/HUMAN/MEMBER/DEVICE/CHOICE/BACKUP) goes away for in-process calls. The
  `orchestrator` → coordinator legacy mapping (parse.go:37-39,183-185) can go once nothing old
  prints it.
- **`internal/marker`** is kept only while any Python producer or bash consumer remains.
- **`internal/declare`**: the whole `env PYTHONPATH=… python3 -P -m helpers.agent_bus.declare`
  client (declare.go:581-616) and its parsers become a package. `Overview` no longer needs to be
  "two answers from one run" (:258-259). The software digest that builds the Python zipapp
  (`declare.py:461-470`) must be redefined against the Go build **[I]**.
- **`internal/sudo`**:
  - The constants `AgentBus` and `InstallTool` (sudo.go:20-23) stay, and their meaning changes if
    the wrapper becomes Go. The sudoers rule matches the exact argv, so the regex lines in
    `agent-bus-read-only.j2` must match the new batched read-only command.
  - `files/usr/local/bin/agent-bus`'s bash `runuser` hop, and the `place_bundle` tar handling for
    add-member and rotate-token, are wrapper logic that v2 has to keep or replace.
- **`board.Collector.devices` cache and `ForgetDevices`** (collect.go:57-59,99-112, called from
  ops.go via `teams.Wire` app.go:107) exist only because each read costs a sudo process. They become
  optional with a batched call.
- So do these caches: `hot.listCache.seats`, `choose.describer` handles/rosters, and
  `workTree.asked` (git).
- **Test seams**:
  - `teamtest/world.go:35-41,113,141,165-173` keys fakes on argv strings ("agent-bus (user) seat
    list", `SudoBus`, "env PYTHONPATH=… declare"); these keys need rewriting.
  - `redraw_cost_test.go` counts processes and would count calls instead.
  - The `qa/rules` command-ownership table (`table.go:51,186-206`, the "sudo is owned by
    internal/sudo" rule, the Hot.pingbus exemption) needs entries for any new exec.
  - `qa/deployeddrift/run.go:170-189` tracks the deployed wrapper, installer and plugin files.
- **Nested Python-in-Python goes away**: `seat choose` and `seat take` run a nested
  `sudo -n agent-bus list` (checkout.py:902-917). In Go, the user-side chooser would call the one
  privileged reader, or share its result with the board **[I]**.
- **Unchanged unless pingbus is also ported** (the kit pingbus, and pingbus in the container):
  `Hot.pingbus` (retire, farewell, reach) and `ExecIn`/`ExecDetached` pingbus.
- **Unchanged unless the launcher calls a Go binary**: the launcher's `agent-bus seat check` and
  `seat take` and `pingbus seat disconnected`.
