# Plan 00203 Task 1.1: robustness audit of the agent team system

Read-only audit of `F44` at the time of writing, plus the uncommitted work in the
`fix/watcher-survives-homeserver-restart` worktree. No code was changed and no QA was run. Names
of teams, hosts, checkouts and people are placeholders throughout.

The owner's bar (PLAN.md): "totally robust, systemd services with restart for the required
server side". This report covers every moving part. For each one it records what runs it, its
restart and boot behaviour, what each fault does, and whether recovery is automatic, needs a
human, or never happens. Then the health views, the gaps ranked, and a fix list.

Legend for recovery: **AUTO** recovers with no human; **HUMAN** needs a person; **NEVER**
silently stays broken until something unrelated happens; **N/A** the fault does not reach it.

---

## 1. Inventory: what runs each part

| Part                                     | What runs it                                                                                                                              | Restart policy                                                                                                                                                      | At boot                                                                                         |
| ---------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Homeserver (Tuwunel) per team            | system unit `agent-bus-hs@<team>.service` (`files/etc/systemd/system/`), drop-in `network.conf` rendered by `helpers/agent_bus/render.py` | `Restart=on-failure`, `RestartSec=5s`, `StartLimitIntervalSec=0` (drop-in only; the base unit has none). No `WatchdogSec`, `Type=simple`, no readiness notification | `WantedBy=multi-user.target`, enabled by `agent-bus-install team`                               |
| Homeserver ordering                      | base unit `After=/Wants=network-online.target`; drop-in `After=/Wants=` the device unit of every listen interface (`agentbus0`, …)        | -                                                                                                                                                                   | No ordering on `firewalld.service` or `NetworkManager.service` (not needed for safety, see 2.1) |
| Bus address `agentbus0`                  | NetworkManager dummy connection, `connection.autoconnect yes`, made by `agent-bus-install software --bus-address`                         | NM's own                                                                                                                                                            | NM brings it up                                                                                 |
| firewalld rich rules                     | `agent-bus-install team` writes permanent and runtime rules; record in `/var/lib/agent-bus-install/<team>/firewalld.rules`                | firewalld reloads the permanent set                                                                                                                                 | permanent rules load with firewalld                                                             |
| LXC bridge subnet in `allow_from`        | `helpers/agent_bus/bridge.py`, read live from `lxcbr0` at play time only                                                                  | -                                                                                                                                                                   | snapshot; not re-read at boot                                                                   |
| Backups                                  | `agent-bus-backup@<team>.timer` (daily, `RandomizedDelaySec=1h`, `Persistent=true`) runs a oneshot `backup-now`                           | none (oneshot); `Requisite=` the homeserver                                                                                                                         | `WantedBy=timers.target`; a missed run fires after boot                                         |
| Team setup (`team`, `remove`, bootstrap) | `play-agent-bus.yml` or `ccy-teams apply`, by a human                                                                                     | -                                                                                                                                                                   | nothing runs at boot                                                                            |
| ccy session on teams                     | `ccy --teams` in tmux; `podman run --rm`; PID 1 is `pingbus seat exec` then the supervisor then `claude`                                  | none for a crash; a self-requested restart only (`lib/restart-request.bash`)                                                                                        | `ccy-sessions-restore.service` (user manager, linger) replays the recorded launch               |
| Seat claim and seat lock                 | `pingbus seat exec` takes `flock` on each `seat.lock`, marks the fds inheritable, `execvp`s                                               | -                                                                                                                                                                   | taken again by each restored launch                                                             |
| Watcher `pingbus watch`                  | spawned detached by the plugin's SessionStart hook (`helpers/pingbus/hooks.py` `spawn_watcher`, `start_new_session=True`)                 | **none** today. The in-flight fix adds a respawn from UserPromptSubmit and Stop                                                                                     | started by SessionStart of the restored session                                                 |
| Waiter `pingbus wait`                    | the agent, with `run_in_background`, when there is no socket                                                                              | none; the Stop guard nags once per 600 s                                                                                                                            | -                                                                                               |
| Session hooks                            | Claude Code runs the plugin's `hooks.json` (`--plugin-dir`); one process per event                                                        | -                                                                                                                                                                   | -                                                                                               |
| `cc --teams` host session                | `files/var/local/claude-code/cc` + `cc-teams.bash`; the same `seat exec`, with `--root` and `--home` under a `mktemp` dir                 | none                                                                                                                                                                | restored by the same `ccy-sessions-restore`                                                     |
| LXC member                               | `agent-bus-claude` from the kit, started by a person as the agent user inside the container; IaC only installs it                         | none                                                                                                                                                                | nothing starts it                                                                               |
| ccy supervisor relay mode                | `PINGBUS_DELIVERY=relay`; pingbus side built (`relay.py`), the supervisor plugin (Plan 00194 Task 2.1) is not built                       | -                                                                                                                                                                   | nothing sets it: dormant                                                                        |
| hooks-daemon                             | project install; provides the PTY supervisor (`.claude/ccy/claude-supervise.py`) and the supervisor plugin API                            | n/a                                                                                                                                                                 | n/a                                                                                             |
| Element and human accounts               | Tuwunel password login; limits in `render.py` `LOGIN_LIMITS`                                                                              | -                                                                                                                                                                   | -                                                                                               |
| Tokens (member, admin)                   | minted through the admin API, stored in the seat bundle and `secrets/admin.token`                                                         | no expiry is configured                                                                                                                                             | -                                                                                               |

---

## 2. Server side, fault by fault

### 2.1 Homeserver unit

| Fault                                                   | What happens                                                                                                                                                                                                                                            | Recovery                                                                        |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Process killed (SIGKILL, OOM, crash with non-zero exit) | `on-failure` restarts it after 5 s, with no start limit                                                                                                                                                                                                 | AUTO                                                                            |
| Process exits 0 by itself                               | `on-failure` does not restart a clean exit. Tuwunel exits 0 on SIGTERM and on an admin `server shutdown`/`restart` command                                                                                                                              | **NEVER** (stays inactive; board says down)                                     |
| Process hangs (deadlock, RocksDB stall), still running  | No `WatchdogSec`, no liveness probe. Unit stays `active`; every client sees timeouts                                                                                                                                                                    | **NEVER**, and the board says `up`                                              |
| A human runs `systemctl stop`                           | stays stopped, as intended                                                                                                                                                                                                                              | HUMAN (intended)                                                                |
| Host reboot                                             | enabled in `multi-user.target`; clean stop (`TimeoutStopSec=330`). At boot it starts after `network-online.target`; if `agentbus0` is late the bind fails, Tuwunel exits non-zero and restarts every 5 s until it binds                                 | AUTO (journal noise; seat check refuses while it is `activating`, see 3.4)      |
| Network blip / `agentbus0` taken down                   | an already-bound listen socket survives an address that goes and comes back; a restart while the address is absent loops on 5 s until it returns                                                                                                        | AUTO                                                                            |
| Disk full                                               | RocksDB writes fail. Tuwunel either keeps running and answers 5xx, or exits non-zero and loops on 5 s with no limit. Nothing tells the owner                                                                                                            | AUTO when space returns; **silent** meanwhile                                   |
| firewalld restarted or reloaded                         | the rules are permanent and runtime both, so a reload keeps them. A `firewall-cmd --complete-reload` or a zone change by another play is not detected. The unit's own `IPAddressAllow` still holds                                                      | AUTO for reload; a lost rule is seen only by `agent-bus-install check`          |
| Tuwunel upgrade or changed config (play run)            | play backs up first, then `team` restarts each changed homeserver. Every session's watcher dies (2.x bug, section 3.2)                                                                                                                                  | server AUTO; sessions **NEVER** today                                           |
| Restore from backup (`ccy-teams restore`)               | the database goes back to the backup. Tokens minted after it (any seat taken or returned since) no longer exist: those watchers get 401 and drop the team for good. A seat's stored sync token may be ahead of the restored stream (behaviour unproven) | **HUMAN** per affected seat (`agent-bus seat remove`, relaunch); nobody is told |

Note on the backup signal: the homeserver runs with `PrivatePIDs=yes`, so Tuwunel is PID 1 in its
namespace and a SIGUSR2 that arrives before its handler is installed is dropped, not fatal (the
installer comment says so). A backup timer firing at boot therefore cannot kill a starting
homeserver; it can only fail with "no new backup".

### 2.2 Team setup: bridge, firewalld, units

- **Runs only when a human applies.** Nothing reconciles at boot. If the LXC bridge's subnet
  changes (lxc-net reconfigured), the drop-in's `IPAddressAllow` and the firewall rule keep the
  old subnet; LXC members are refused until the play runs. `ccy-teams` shows the team out of date
  (the declare digest reads the live subnet), so this is **surfaced, HUMAN**.
- **`remove` then data on disk** is fine; a half-failed `team` leaves the firewalld record
  covering every rule it may have added (good).
- **`require_running firewalld NetworkManager`** fails a `team` run rather than guessing (good).

### 2.3 Backups

| Fault                                    | What happens                                                                 | Recovery                                          |
| ---------------------------------------- | ---------------------------------------------------------------------------- | ------------------------------------------------- |
| Homeserver down at the timer's run       | `Requisite=` fails the backup unit                                           | the next day's run; a failed unit nobody looks at |
| Disk full                                | Tuwunel's backup fails, or the state tar fails; the unit fails               | **silent**                                        |
| Missed run (host off)                    | `Persistent=true` runs it after boot, with up to an hour's random delay      | AUTO                                              |
| Backups on the same disk as the database | a disk loss takes both (owner's decision not to copy off-host is documented) | n/a, documented                                   |
| Backup age                               | nothing checks that the newest backup is recent                              | **NEVER** surfaced                                |

### 2.4 Bus address `agentbus0`

NM dummy with autoconnect. Survives NM restart and reboot. If deleted (`nmcli con delete`),
`seat take` refuses (`agentbus0 is absent`) and the homeserver loops on a failed bind; recovery is
a play run (**HUMAN**, surfaced by the refusal text). Nothing in the board names `agentbus0`.

---

## 3. Session side, fault by fault

### 3.1 Seat take, seat lock and stale seats

- **Lock primitive**: `flock` on `seats/<team>/<seat>/seat.lock`, kind and claim time written in,
  never a PID. A dead holder's lock is released by the kernel.
- **ccy session dies uncleanly** (OOM, `podman kill`, host crash): `--rm` takes every process,
  the lock goes with them. **AUTO.** The session itself does not come back until the next reboot
  restore or a human (no crash restart).
- **Container restart**: there is no `podman restart` of a `--rm` container; a new container is a
  new launch, which re-takes the seats. **AUTO** for the lock.
- **podman restarted / conmon killed / user manager restarted**: every container of the user dies;
  locks go; sessions do not come back until reboot restore or a human. **HUMAN.**
- **Frozen container (podfreeze)**: the lock stays held, correctly; peers see the seat's
  `listening` status go `stale` after 600 s.
- **`cc --teams` host session: a stale seat that never frees.** `seat exec` marks the seat fds
  inheritable so that "everything it starts" holds them (`helpers/pingbus/seat.py`). In a
  container the container's end bounds that. On the host nothing does: any process `claude`
  started that outlives it (a `setsid`/`nohup` child from a Bash tool call, a dev server, an MCP
  server that does not exit) keeps the seat locked after the session is gone. The next
  `cc --teams` for that seat is refused with 75 "held by another session", and the lock file
  names no holder. **HUMAN, and hard to diagnose.** The watcher itself is spawned with
  `close_fds=True` and does not hold the seat lock.
- **Seat directory deleted by hand**: `seat take` sees the account as an unparked member and
  refuses; the fix is `agent-bus seat remove` then relaunch (documented). **HUMAN.**

### 3.2 The watcher (`pingbus watch`)

Today (`helpers/pingbus/cli.py` `cmd_watch`, `_watch_poll`):

| Fault                                            | What happens today                                                                                                                                                                                                        | Recovery                                                                |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| Homeserver restarts mid-session                  | the team's poll thread gets `Unreachable` or a 5xx, the team is "no longer watched"; with one team the watcher exits                                                                                                      | **NEVER** (the known bug)                                               |
| Homeserver not answering when the watcher starts | the first `sync_once(0)` and `publish_status` run in the main thread before any poll thread, so the exception leaves `cmd_watch` and the watcher exits                                                                    | **NEVER**                                                               |
| Network blip                                     | same as a homeserver restart (socket timeout gives `Unreachable`)                                                                                                                                                         | **NEVER**                                                               |
| Disk full                                        | an `OSError` (ENOSPC) from a state write is not in `FAILURES`; the main loop re-raises it and the whole watcher dies, every team at once, with a traceback in `watch.log`                                                 | **NEVER**                                                               |
| Token revoked (401)                              | team dropped, exit 8                                                                                                                                                                                                      | **NEVER** (correctly not retried; but silent)                           |
| Watcher killed (OOM, `kill`)                     | nothing restarts it; the Stop guard tells the agent once per 600 s to run `pingbus wait`, but only when it stops a turn                                                                                                   | HUMAN or agent                                                          |
| Session socket gone (session ended)              | the watcher exits 0, as designed                                                                                                                                                                                          | n/a                                                                     |
| Host session killed with SIGKILL                 | the socket file stays on disk, `socket_present` is a `stat`, so the watcher stays alive holding the team sync locks until it tries a delivery and gets ECONNREFUSED. A new session for the same seat sees the team "held" | AUTO only at the next message; meanwhile the new session has no watcher |
| Host reboot                                      | dies with its container; the restored session's SessionStart starts a new one (but see the start race above)                                                                                                              | depends on 3.4                                                          |

**The in-flight fix (`fix/watcher-survives-homeserver-restart`).** At the time of this audit the
worktree holds only red tests (`tests/helpers/pingbus/test_matrix.py`, `test_notify.py`,
`test_hooks.py`); no implementation is committed. What the tests pin:

1. `MatrixError.transient`: no connection, 5xx (including a proxy's HTML 502), and a 429 past
   its retries are transient; 401, 403, 404 and a malformed answer are not.
2. The watcher retries a transient failure per team with a 1 s doubling to 30 s backoff from
   its stored sync token, logs once when the outage starts and once when it ends, and delivers
   each message once. A 401 still drops the team.
3. A per-session watch lock, so a second watcher of the same session exits busy even on a free team.
4. UserPromptSubmit and Stop respawn a missing watcher (with a start grace and a respawn
   interval, and a `watcher_failed` report when it dies again soon after a start). The "no
   waker, run `pingbus wait`" Stop block is kept only for a session with no socket.

**What the fix does not cover** (from the tests; check against the implementation when it lands):

- **The start path.** Every outage test calls `self.joined()` and `start_watch()` first, then
  injects faults. The first `sync_once(0)` and `publish_status` in `cmd_watch`'s main thread are
  outside the per-team retry, so a homeserver that is not answering yet when the watcher starts
  (the reboot race, a Tuwunel restart during `/clear`) still kills it.
- **An idle session.** The respawn runs only on UserPromptSubmit and Stop. A session parked at its
  prompt has no hook events, and a dead watcher is the only thing that would wake it. So a
  watcher that dies while the session is idle stays dead until a human types. That is the normal
  state of a restored session after `set-going`.
- **Non-`FAILURES` exceptions** (disk full, a permission change) still kill the whole watcher.
- **The waiter.** `_long_poll` (`pingbus wait`) has the same per-team drop on a transient failure;
  no test covers it.
- **Relay mode.** It shares `_watch_poll`, so it gets the retry for free, but nothing starts a
  relay watcher (the plugin is not built).
- **The orphaned host watcher** (stale socket file) above: the new per-session lock is per socket
  path, but the team sync lock is per seat, so the new session's watcher still exits busy.
- **Spec numbering clash.** The tests cite "SPEC-inject E20" and "E21", but
  `CLAUDE/Plan/00194-.../SPEC-inject.md` already has E20 (Several teams) and E21 (Timestamps are
  bounded). The fix needs new numbers, or the citations point at the wrong expectations.

### 3.3 Hooks

Each hook is a short process run by Claude Code, offline except SessionEnd's goodbye (4 s cap).
Faults inside a hook become one fixed template and exit 0. They do not need restarting. Their
one robustness duty is starting the watcher, covered above. The Stop guard's memory
(`stop-guard.json`) not being writable means the guard stops blocking. That is reported on the
hook's stderr only, so it is in effect silent.

### 3.4 ccy and cc sessions across a host reboot

`ccy-sessions-restore.service` is a **user** unit (`WantedBy=default.target`, linger). A user unit
cannot be ordered after a system unit, so nothing orders it after `agent-bus-hs@<team>.service`.
The restored launch then runs:

1. `agent-bus seat check`: `systemctl is-active --quiet agent-bus-hs@<team>`. A homeserver in
   auto-restart (e.g. `agentbus0` not up yet) is `activating`, so the check refuses with 78 and
   **that session is not started**. There is no retry. `ccy-sessions restore` counts it only if
   the tmux start itself failed; the refusal sits in the tmux window.
2. A homeserver that is `active` but not yet answering passes the check; the session starts; its
   watcher's first sync fails and the watcher exits (3.2). The session looks healthy and is deaf.
3. `seat take --no-prompt` runs no sudo when every seat exists (the normal case), so it does not
   need a password.
4. `set-going` types `continue` or `/compact`, then the session goes idle. No hook fires after
   that, so the in-flight respawn never runs.

**Net: a reboot brings back every homeserver, every unit and the sessions' tmux windows with no
human step. It does not reliably bring back a working wake path for any team session.**

### 3.5 LXC members and `agent-bus-claude`

The play installs the kit and bundle into an existing container (`helpers/agent_bus_lxc/members.py`).
The session itself is started by hand (`sudo -iu <agent-user> agent-bus-claude` or inside the
container). Nothing restarts it after a crash, a container restart or a host reboot (**HUMAN**).
It claims no seat (v1); its busy rule is the team sync lock. Its watcher has every fault in 3.2.
The kit's `pingbus` is updated only when the play's member step runs.

### 3.6 ccy supervisor relay mode

Dormant: nothing sets `PINGBUS_DELIVERY=relay`. If it were set, `session-start` would start no
watcher ("the plugin owns it") and no plugin exists to start one. The Stop guard does not nag in
relay mode except on a stall. So a relay session today would have no wake path and no nag.
Turning it on before Plan 00194 Task 2.1 ships would be a silent break.

### 3.7 hooks-daemon dependency

The bus does not depend on the daemon for delivery. The pingbus hooks are plugin hooks that
Claude Code runs directly, the inbox socket is Claude Code's, and the status line works without
a daemon. The dependencies are:

- **The PTY supervisor** (`claude-supervise.py`, from the project's daemon install) sits between
  `seat exec` and `claude`. A supervisor that does not parse refuses the launch (good). One that
  dies mid-session ends `claude` and with it the container.
- **Relay mode** (future) depends on the daemon's supervisor plugin API.
- **Stop interplay**: the pingbus Stop guard blocks at most once per window so it cannot loop
  against the daemon's own Stop handlers.
- **`HOOKS_DAEMON_HOSTNAME`** labels a new seat's `<host>`; if it changes, the next launch makes a
  new member under a new handle and the old one stays parked (documented).

A hooks-daemon outage does not stop the bus. A daemon upgrade that changes the supervisor
affects only new launches.

### 3.8 Element and human accounts

- **Login rate limit** (`render.py` `LOGIN_LIMITS`): per account, three failed passwords then one
  per five minutes, and five successful sign-ins then one per about five and a half minutes. A
  person signing in on several devices after `human password` (which logs out every device), or
  an Element client retrying, can hit the successful-login bucket. They then see
  `M_LIMIT_EXCEEDED` in Element. **Nothing in `ccy-teams` says so**, and there is no reset action.
  The bucket is probably in memory, so a homeserver restart would clear it. That is unverified:
  prove it before relying on it.
- **Human deactivated by a team-file change**: by design; cannot return (documented).

### 3.9 Tokens and credentials

- No token expiry is configured, and Tuwunel issues no refresh tokens for admin-minted member
  tokens. Member tokens, the admin token and Element sessions last until revoked.
- **Revocations that break a running session without telling anyone:** `rotate-token` and
  `park-member` on a held seat (`seat remove` refuses a held seat, but `sudo agent-bus rotate-token`
  does not check the lock), and a restore from an older backup (2.1). Each one gives the watcher
  a 401: the team is dropped and one line goes to `watch.log`. The peers see the seat go `stale`
  after 600 s. The owner is not told.
- `admin.token` lost: restore from backup (documented; `rotate-admin` deferred).

### 3.10 What a host reboot brings back

| Comes back with no human step                       | Does not come back, or not reliably                                                      |
| --------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| every enabled homeserver (after `agentbus0`)        | a working watcher in a restored session (start race, idle session: 3.2 and 3.4)          |
| `agentbus0`, firewalld rules, the drop-ins          | a restored session whose homeserver was `activating` at `seat check` (refused, no retry) |
| backup timers (a missed run fires)                  | LXC members' Claude sessions (started by hand)                                           |
| ccy and cc sessions' tmux windows, with their seats | any session that was not running at shutdown (by design)                                 |
| the seat locks, taken again by each launch          | a human locked out by the login limit (it clears on its own in minutes)                  |

---

## 4. The health views and what they do not say

| View                                     | Shows                                                                                                                                                                                                                                                                                             | Does not show                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ccy-teams` board and `ccy-teams --list` | team STATE from `systemctl list-units` ACTIVE (`up`, or `down (<word>)`); APPLIED from the stamps; members and roles (via the read-only sudoers rule); seats `held`/`free`; SESSION from tmux and the registry, `(stopped)` when the tmux session is gone; RESTORE yes/no; Element session counts | whether a seat's **watcher is running or `listening`** (a seat row's STATE is only the account's `active`/`parked`); a homeserver that is `active` but not answering; `NRestarts` or a crash loop; the backup timer's last result and the newest backup's age; `agentbus0` up; firewalld rules present (`agent-bus-install check` knows, the board does not ask); a seat held with no session record (shown `held`, with no holder named); a login lockout |
| `pingbus status` (inside the session)    | per team: trust, \`wake=watcher                                                                                                                                                                                                                                                                   | waiter                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `pingbus status-line`                    | `bus: A,B` (the teams), red `no record` when the SessionStart record failed                                                                                                                                                                                                                       | anything about the wake path: a session with no watcher shows the same green team list                                                                                                                                                                                                                                                                                                                                                                     |
| SessionStart / Stop templates            | `watcher_started`, `watcher_failed`, `no_waker`, failure classes                                                                                                                                                                                                                                  | they speak to the agent, not the owner; once per 600 s                                                                                                                                                                                                                                                                                                                                                                                                     |
| `agent-bus-install check --team` (root)  | `CHECK` lines: unit file state, sandbox properties, firewalld rules recorded vs permanent                                                                                                                                                                                                         | not run by anything routinely; not shown on the board                                                                                                                                                                                                                                                                                                                                                                                                      |
| `agent-bus list` / `agent-bus seat list` | roster and room membership; seats held/free                                                                                                                                                                                                                                                       | liveness                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `ccy-sessions` / verify-restore          | which sessions restored and reached a prompt                                                                                                                                                                                                                                                      | whether a restored team session's seats and watcher work                                                                                                                                                                                                                                                                                                                                                                                                   |

There is no `agent-bus status` command.

---

## 5. Top gaps, ranked

01. **The watcher has no supervisor.** Restart relies on hook events (the in-flight fix). An idle
    session, which is the normal state after a reboot restore, never gets one, so a watcher that
    dies leaves the session deaf until a human types.
02. **The watcher's start path is outside the in-flight retry.** The first sync and status publish
    run before the per-team threads, so a homeserver not yet answering at session start kills it.
    This is exactly what the reboot race produces.
03. **Reboot ordering.** The user-manager restore has no ordering against the system homeservers,
    and `seat check` refuses an `activating` homeserver with 78 and no retry. Restored team
    sessions can fail to start, and the reason is visible only in a tmux window.
04. **The homeserver's liveness is not checked.** `Restart=on-failure` with no `WatchdogSec` or
    health probe: a hung Tuwunel, or one that exits 0, stays broken, and the board says `up` for
    the hung one.
05. **Any unexpected exception kills the whole watcher.** Disk full, or any `OSError` outside
    `FAILURES`, is re-raised in the main loop and every team stops at once.
06. **The owner cannot see session health.** The board's seat STATE is the account state, not
    the watcher's. The status line shows teams only, `pingbus status` has no last-sync time,
    and nothing shows backup age, restart count, `agentbus0`, or an outage.
07. **Host (`cc --teams`) seats can stay held for good.** Inherited seat fds outlive `claude` in
    detached children (refused with 75, no holder named). An orphan watcher behind a stale socket
    file holds the team sync locks.
08. **Permanent drops are silent.** A 401 after `rotate-token` on a held seat, or a restore from an
    older backup, drops the team with one `watch.log` line. The seat needs `seat remove` and a
    relaunch, and nobody is told.
09. **LXC members have no unit.** Nothing restarts `agent-bus-claude` after a crash or reboot.
10. **Backup failures and the login lockout are invisible.** A failed timer shows only in
    `systemctl --failed`, the newest backup's age is never checked, and `M_LIMIT_EXCEEDED`
    reaches only Element.

Also noted: relay mode is dormant and would be a silent break if enabled early (3.6), and the
in-flight fix's SPEC-inject E20/E21 citations clash with existing expectations (3.2).

---

## 6. Prioritised fix list

Each fix names the file, the change and the acceptance on the host. All acceptance steps go into
a plan-local `acceptance.bash` run through `meta-deploy.bash`. **S** = server side,
**Se** = session side, **T** = tooling/health.

### F1 (Se). A watcher that never gives up on a transient fault, including at start

- `helpers/pingbus/cli.py`: move the first `sync_once(0)` and `publish_status` of each team into
  its `_watch_poll` thread, under the same transient backoff the in-flight fix adds. Then a team
  whose homeserver is down at start is "waiting for the homeserver", not a failure. `cmd_watch`
  exits only when the socket goes, or when every team has a permanent refusal (401, 403,
  untrusted).
- Treat `OSError` from state writes per team: report it, back off, retry. Never re-raise it in
  the main loop. Keep re-raising truly unexpected exceptions only after logging them, and let F2
  restart.
- Apply the same transient rule to `_long_poll` (`pingbus wait`).
- Renumber the fix's expectations in `SPEC-inject.md` (new E-numbers, not E20/E21).
- **Acceptance:** with a team session idle, run
  `sudo systemctl stop agent-bus-hs@<team>`. Start a new team session (watcher start with
  the homeserver down). Wait 60 s, then `sudo systemctl start agent-bus-hs@<team>`. Post from
  Element to both sessions. Both are woken within 35 s with no keypress, and `watch.log` shows
  one outage line and one recovered line each. Repeat with `systemctl restart` five times in a
  row. Fill a loop-mounted tmpfs seat directory to ENOSPC in a test checkout; the watcher logs it
  and survives, and delivers once space is freed.

### F2 (Se). A watcher supervisor that does not depend on hook events

- `helpers/pingbus/cli.py`, `helpers/pingbus/hooks.py`: SessionStart spawns
  `pingbus watch --supervise` instead of `pingbus watch`. The supervising parent (tiny, no
  network) runs the watcher as its child. While `socket_present` holds, it restarts the child
  after any exit other than "socket gone" or "every team permanently refused", with a 1 s to
  60 s backoff. Each restart is logged to `watch.log`, and a `restarts` count is written to
  `PINGBUS_HOME/watch-state.json` for status (F6). It holds the per-session watch lock from the
  in-flight fix, so the hook respawn becomes the backstop for a killed supervisor only.
- The supervisor should also confirm the socket answers (a connect, not a `stat`), so an orphan
  on the host exits when its session is gone (closes part of gap 7).
- **Acceptance:** in an idle team session, `pkill -KILL -f '[p]ingbus watch$'` (the child only).
  A message from Element wakes the session within 65 s with no keypress, and `watch.log` names
  the restart. Then kill the supervisor too; the next prompt respawns it (the in-flight fix's
  path). On the host, SIGKILL a `cc --teams` claude; within 60 s no `pingbus watch` process of
  that session remains (`pgrep -af '[p]ingbus watch'`).

### F3 (S). The homeserver restarts on every exit and is health-checked

- `helpers/agent_bus/render.py` `render_dropin`: `Restart=always` (a deliberate `systemctl stop`
  is still not restarted). Update `tests/helpers/agent_bus/test_render.py` and DESIGN.md section
  3.5.
- A liveness check, because Tuwunel 1.9.4 sends no `sd_notify`. Add a per-team
  `agent-bus-hs-health@<team>.timer` + oneshot service in `files/etc/systemd/system/`, installed
  by `agent-bus-install software` (`UNIT_FILES`) and enabled by `team`, alongside the backup
  timer. Every minute it curls `http://127.0.0.1:<port>/_tuwunel/server_version`
  (`--max-time 5`). After three consecutive failures while the unit is `active`, it runs
  `systemctl restart agent-bus-hs@<team>` and logs the reason. The probe logic goes in a new
  `agent-bus-install health --team` subcommand, or better a small Go or Python helper: the
  installer is already on the bash ratchet allowlist and must not grow.
  `agent_bus_swap_paths` in `play-agent-bus.yml` gains the new units.
- **Acceptance:** `sudo systemctl kill --signal=SIGSTOP agent-bus-hs@<team>` (a hang): within 4
  minutes the unit has restarted (`systemctl show -P NRestarts` increased) and a team session
  is woken by an Element post with no keypress (F1). Then make the process exit 0 (e.g.
  `systemctl kill --signal=SIGTERM` on the main PID without `systemctl stop`): it is running
  again within 10 s.

### F4 (Se). A restored team session waits for its homeserver instead of being refused

- `helpers/agent_bus/checkout.py` `seat check`/`seat take`: when unattended (`--resolved` /
  `--no-prompt`), a homeserver that is `activating`, or `active` but not answering on
  `127.0.0.1:<port>`, is waited for (poll every 2 s, up to a bound such as 180 s), saying so on
  stderr, before 78. Attended launches keep the immediate refusal.
- `files/home/.config/systemd/user/ccy-sessions-restore.service`: no system ordering is
  possible, so the wait above is the fix. Also make `ccy-sessions restore` report a team
  session whose launch exited non-zero in its summary and the boot manifest, so verify-restore
  shows it.
- **Acceptance:** add a temporary drop-in that delays `agent-bus-hs@<team>` start by 60 s
  (`ExecStartPre=/usr/bin/sleep 60`, through a plan-local play, removed after). Reboot. Every
  recorded team session is up, `pingbus status` in each shows `wake=watcher`, and an Element post
  wakes each with no keypress.

### F5 (Se/S). Host seats cannot stay held by stray children; an orphan watcher frees the seat's sync locks

- `helpers/pingbus/seat.py`: for a host launch (`--root`/`--home` given), keep the seat fds out
  of children. Hold the locks in a small `seat exec` parent that stays alive, waits for
  `claude`, and exits when it does, instead of exec-ing into `claude` with inheritable fds.
  The container path can stay as it is.
- Record the holder's PID and start time in `seat.lock` beside the kind, for diagnosis only
  (liveness stays the `flock`). `agent-bus seat list` and the board then name the holder of a
  held seat with no session record.
- **Acceptance:** in a `cc --teams` session run `setsid sleep 3600 &` from a Bash tool call, then
  quit claude. `cc --teams <seat>@<team>` again in the same checkout starts at once. A seat held
  by a live session shows its holder PID in `agent-bus seat list`.

### F6 (T). Health the owner can read: the board, the status line, `pingbus status`

- `pingbus status`: add `watch=` (`running`, `restarting (n)`, `waiting for homeserver since <t>`, `stopped: <reason class>`) from F2's `watch-state.json`, and `last_sync=<age>` per team
  from the syncer (store the time of the last successful sync with the sync token).
- `helpers/pingbus/statusline.py`: append `no watcher` in red when the session has the socket
  and a team's lock probes free (a cheap non-blocking `flock`; the module imports nothing, so
  copy the probe and pin it equal in a test), and `homeserver down` from `watch-state.json`.
- `src-go/internal/teams/board`: a seat row's STATE shows the watcher, from the seat's own lock
  kind (`watch`/`wait`/none, readable as the user) and its `listening` status in the room view.
  The team row adds `NRestarts` when non-zero, `not answering` from a 2 s loopback probe, the
  newest backup's age (via `agent-bus-install backups`, already offered to `ccy-teams restore`)
  with a warning past 36 h, and the backup timer's last result. Add a one-line banner when
  `agentbus0` is absent.
- Feed the same facts to the existing host-health report (`host-health-collect` user timer), so
  a broken team is reported without opening `ccy-teams`.
- **Acceptance:** stop a homeserver: within one board refresh the team row says down, each of its
  seats says `no watcher`/`waiting`, and each session's status line turns red. Kill a watcher
  with F2 disabled: the seat row and status line say so within 5 s. Make a backup fail (stop the
  homeserver before `systemctl start agent-bus-backup@<team>`): the board shows the failed
  backup.

### F7 (Se/T). Permanent drops are told to the owner, and can be repaired from `ccy-teams`

- `helpers/pingbus/cli.py`: on a 401 the watcher writes the drop to `watch-state.json` (shown by
  F6) and, through the socket, tells the session once: "team X: this seat's token was revoked;
  ask the owner to run ccy-teams …".
- `helpers/agent_bus/cli.py` `rotate-token`/`park-member`: refuse a seat whose `seat.lock` is held
  unless `--force`, as `seat remove` already does.
- `ccy-teams restore`: after a restore, list the seats whose bundles were minted after the backup
  (bundle time against backup time) and offer to re-mint them. This is a natural hot-control
  verb for Plan 00203: re-mint the token of a seat held by a running session and deliver it to
  that session.
- **Acceptance:** `sudo agent-bus rotate-token <team> <handle>` on a held seat is refused. With
  `--force` the session shows the revoked-token message within 35 s and the board flags the
  seat. Take a backup, take a new seat, restore: `ccy-teams restore` names the new seat and
  re-mints it, and the session is woken by an Element post.

### F8 (Se). LXC members as user services

- `helpers/agent_bus_lxc/members.py` (README.lxc step 7): install a systemd user unit in the
  container for the agent user. It runs `agent-bus-claude` headless under tmux, or the member's
  own driver, with `Restart=on-failure` and linger enabled for that user. The F1 and F2
  watcher fixes cover its wake path. Make this opt-in per entry (`service: true`), since some
  LXC members are interactive.
- **Acceptance:** reboot the host. The LXC container starts, the member's unit is active
  (`lxc-attach -n <container> -- systemctl --user -M <agent-user>@ is-active <unit>`), and a
  ping to its handle is answered.

### F9 (S/T). Login lockout is visible and resettable

- Prove on the host whether a homeserver restart clears Tuwunel's login buckets. If it does,
  add `ccy-teams sign-in-reset <team>` (restart, with the one-line warning that it interrupts
  the team for a few seconds, which F1 makes harmless). If it does not, document the wait.
- `ccy-teams` sign-in flow: after `human password`, tell the person they have five sign-ins
  before a five-minute wait.
- **Acceptance:** four wrong passwords for a test human, then the right one: refused with
  `M_LIMIT_EXCEEDED`. Run the reset; the right password is accepted at once (or the documented
  wait is confirmed).

### F10 (Se). Relay mode guarded until its plugin exists

- `helpers/pingbus/relay.py` `enabled`: refuse `PINGBUS_DELIVERY=relay` unless the relay plugin
  file is present, naming Plan 00194 Task 2.1. Without that, a relay session starts with no
  wake path and no nag.
- **Acceptance:** a test in `tests/helpers/pingbus/test_relay.py`; on the host, a launch with
  the variable set and no plugin is refused with the message.

### Order

F1 and F2 first: they close gaps 1, 2 and 5, and Plan 00203's hot control needs them, because a
membership change takes effect by restarting the watcher. Then F4 (the reboot), F3 (homeserver
liveness), F6 (health), F5, F7, F8, F9, F10.
