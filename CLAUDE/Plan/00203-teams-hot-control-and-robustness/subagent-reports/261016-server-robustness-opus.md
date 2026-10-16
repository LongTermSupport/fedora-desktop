# Plan 00203 Phase 2: server-side robustness (audit gaps 3, 4, 9, 10)

Branch `fix/teams-server-robustness`. What was built for each gap, why it is built that way,
what was found on the way, and what is left. The host proof is [ACCEPTANCE.md](../ACCEPTANCE.md).

## A correction to the audit

The audit (section 1 and F3) says Tuwunel 1.9.4 sends no `sd_notify`. It does. The pinned
source (`v1.9.4`, `src/router/run.rs`) sends `READY=1` once its services are built (the
database is open), `EXTEND_TIMEOUT_USEC` every 15 s while a migration runs, `STOPPING=1` on a
stop, `RELOADING=1` on an admin in-place restart, and `WATCHDOG=1` every `WatchdogSec/2` from a
task of its own runtime. The `systemd` cargo feature is a default one, and the release asset is
`release-all` (every feature). The Debian unit upstream ships is `Type=notify` with
`WatchdogSec=30`. Plan 00161's probe H3 tried `Type=notify` but its result was never recorded,
so the unit stayed `Type=simple`.

`PrivatePIDs=yes` makes Tuwunel PID 1 of its own namespace. The watchdog only arms when
`WATCHDOG_PID` equals the process's own PID (`sd-notify` 0.5.0, `watchdog_enabled`). systemd
258 builds the environment after its fork into the new namespace (`exec-invoke.c`,
`setup_private_pids` then `build_environment`), with a fresh PID cache, so `WATCHDOG_PID=1`
and the check holds. That is read from source; acceptance R1 proves it on the host (no
restart over two and a half watchdog periods, which would loop every 30 s if the pings did not
arrive).

## Gap 4: homeserver liveness

- **Base unit** (`files/etc/systemd/system/agent-bus-hs@.service`): `Type=notify`,
  `NotifyAccess=main`, `WatchdogSec=30s`, `TimeoutAbortSec=30s`. A watchdog timeout sends
  `SIGABRT` (a core for a real deadlock); a stopped process holds that pending, so the abort
  timeout's `SIGKILL` ends it 30 s later instead of after `TimeoutStopSec` (330 s).
  `Type=notify` also makes `active` mean ready, which gap 3 relies on.
- **Drop-in** (`render.render_dropin`): `Restart=always`, `RestartSec=5s`, `RestartSteps=5`,
  `RestartMaxDelaySec=60s`, `StartLimitIntervalSec=0`. Why `always`: Tuwunel exits 0 on
  `SIGTERM` and on an admin `server shutdown`, and `on-failure` left those down for good; only
  `systemctl stop` is a deliberate stop, and systemd never restarts that. Why no start limit:
  a limit that trips leaves the unit `failed` after the cause (a late `agentbus0`, a full
  disk) has cleared, which is the "never" class; the backoff to 60 s bounds what the retries
  cost.
- **Health check** (`helpers/agent_bus/health.py`, `agent-bus-hs-health@.timer` every 60 s,
  its oneshot service as root, sandboxed: read-write only on `/var/lib/agent-bus-applied`,
  loopback only, `CAP_DAC_READ_SEARCH CAP_CHOWN`). The watchdog cannot see a homeserver whose
  runtime still ticks while its client API has stopped answering (a stalled handler, a
  RocksDB stall on a blocking thread), so the check probes `/_matrix/client/versions` on
  loopback, three times 10 s apart with a 5 s timeout, once the unit has been active for 60 s.
  When every probe misses and the unit is still the same invocation and `active`, it logs why
  and restarts it. A restart that fails fails the check (fail fast). It cannot race the
  watchdog: three misses span 35 s, and by then a frozen process's watchdog has fired and the
  unit is `deactivating`, which the check leaves alone.
- The installer (`agent-bus-install`, on the ratchet) wires the two new units with no growth:
  `UNIT_FILES`, one loop that enables the unit and enables and starts both timers after the
  homeserver (a Persistent backup timer fires a missed run at once and needs it up), `remove`
  disabling the health timer, and `unstamp` removing the health record. Its record went from
  991 code lines and 1157 statements to 988 and 1156.

## Gap 3: reboot ordering

- **No systemd ordering is possible.** A user unit cannot depend on or be ordered against a
  system unit. Waiting for the system to finish booting from `ccy-sessions-restore.service`
  (`systemctl is-system-running --wait`) was considered and refused: that unit holds the user
  manager's `default.target`, and a lingering user's `user@.service` is itself a job of the
  boot transaction, so the wait would hold the user manager's start (and a desktop login) on a
  boot that is waiting for it.
- **The wait is in `seat check` and `seat take`** (`checkout._wait_while_starting`): a
  homeserver whose `ActiveState` is `activating`, `deactivating`, `reloading` or `refreshing`
  (starting up to `READY=1`, or in auto-restart while its interface is late) is polled every
  2 s for up to 180 s, saying so on stderr at the start and every 20 s, and once it is up.
  `active` passes; `inactive` and `failed` are refused at once as before (exit 78); still
  starting at 180 s is refused naming `journalctl -u ... -n 40 --no-pager`. With `Type=notify`,
  `active` is ready, so the "active but not answering yet" window the audit names is gone.
  `checkout.System` gained `team_state`, `sleep`, `monotonic` and `progress` (fakes in tests).

## Gap 10: backups and sign-in lockouts

- **Backups** are judged by the same root check: the backup service failed (`ActiveState failed` or a `Result` other than success), the newest usable backup (a database backup with
  its state tar, what `agent-bus-install backups` lists) older than 36 h, none at all once the
  daily timer has run, or the timer itself stopped. A run at boot while the homeserver was down
  fails the backup's start on `Requisite=` without entering the unit, which only the age
  catches; that is why age is the main signal.
- **Restarts** systemd made itself (`NRestarts` rose since the last record: a watchdog abort,
  a crash, an exit) and the check's own restarts stay on the board for 24 h with the cause and
  a `journalctl --since` to read.
- **The record**, `/var/lib/agent-bus-applied/health/<team>.json`, 0640 in a 0750 directory,
  root and the stamps' group (the applied stamps' rules, no secret), holds the problems with
  what to do, and `expires` (checked + 5 min), so a check that stops running is reported by
  both readers. Its keys are single words so the Go reader needs no lint exception.
- **The board**: `ccy-teams` appends `health: <problem>: <what to do>; ...` to an installed
  team's row (`board/health.go`, about 70 lines, plus a `Now` clock on the collector), and says
  `health: never checked` or that the check stopped. `teamtest.Populated` writes a healthy
  record so the other packages' row tests are unchanged.
- **The status command**: `agent-bus health status [TEAM...]`, run as yourself (the wrapper
  now runs `health`, like `seat`, as the caller and refuses root, because the record is the
  stamps' group's and the `agent-bus` user is not in it): the live homeserver state, the
  record's problems with `do:` lines, the newest backup, and the sign-in limits. Exit 1 when a
  team has a problem.
- **Sign-in lockouts cannot be read.** Tuwunel 1.9.4 keeps both per-account buckets in memory
  (`src/service/login_ratelimit`) and logs a refused request only through `debug_error!`, which
  `release_max_log_level` compiles out of the release build. There is no admin command to read
  or reset a bucket. A restart empties every account's buckets (they are built fresh), which
  would lift the limit for every account, attacker's guesses included, so no reset is offered.
  What is built: `sudo agent-bus human password` states the limits on stderr, and so does
  `agent-bus health status`: the numbers come from `render.LOGIN_LIMITS`, with what Element
  shows and that the wait clears by itself and is not lengthened by retrying. ccy-teams'
  sign-in card (`login.showLogin`; its sudo runner shows a root command's stderr only on a
  failure) says the same without the numbers, pointing at `agent-bus health status`.

## Gap 9: LXC members

`agent_bus_lxc_members` entries take `service: true` (opt-in: a member someone starts by hand
must not get a second session fighting it for the team sync lock). The play
(`members.py`, the step 7 its docstring adds after README.lxc's six) then writes `agent-bus-claude-<user>.service` into the
container: `Type=forking` over `tmux -L agent-bus-claude new-session -d ... agent-bus-claude`
as the agent user in its checkout, `Restart=always` with a 10 s to 5 min backoff and no start
limit, enabled in the container's `multi-user.target`. When `claude` exits the tmux session and
server end with it, so systemd sees the main process go and restarts it. Before any change it
requires `lxc.start.auto = 1` (`lxc-info -c`), systemd as the container's init and tmux
installed, and every entry of one user to agree on `service` and, when on, on the checkout.
Idempotent: the unit is written only when it differs (and the session restarted then), enabled
and started only when it is not. Turning it off, or the user leaving its last team, stops and
removes it. The kit README in the ccy image was left unchanged so the image is not rebuilt for
a doc line; `docs/agent-bus-lxc-members.md` and `localhost.yml.dist` document the key.

## Tests

Each new test was seen failing first: `test_render` (restart policy, base unit, health
units), `test_checkout` (the wait: success after transitions, progress throttled, the bound,
failed while starting, stopped not waited for), `test_health` (26: liveness, systemd restarts
kept a day, backups, record modes and expiry, status lines and exit, sign-in words),
`test_cli` (password says the limits), `test_wrapper` (health as the caller, refused as root),
`test_members` (`ServiceTest`, 11, and `lxc-info` parsing), the board's `health_test.go` (5), `login/password_test.go` (the sign-in card),
and `test-agent-bus-install.bash` (health units installed, enabled and started after the
homeserver, disabled on remove, the record removed with the stamp).

## Left open

- Host acceptance (meta-deploy) and the reboot steps B1-B2.
- Gaps 1, 2, 5 (the watcher) are the other branch's; until it lands, the play's one restart of
  every homeserver still drops each running session's watcher for that team.
- `config_restart` decides a pre-restart backup from `tuwunel.toml` only; a release that
  changes only the rendered drop-in restarts the homeserver without the play's backup first.
  This release also changes the base unit, which is in `agent_bus_swap_paths`, so it is covered
  here; a future drop-in-only change is not.
