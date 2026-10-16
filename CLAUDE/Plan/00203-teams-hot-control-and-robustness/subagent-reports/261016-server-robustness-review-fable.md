# Plan 00203 Phase 2: adversarial pre-merge review of `fix/teams-server-robustness`

Reviewed `git diff F44...origin/fix/teams-server-robustness` at head `9e449dea`, against local `F44`
(`4cdd2d3b`, which holds the Plan 00194/00203 M0 watcher fix, CCY 3.98.12, container 2.67).
Read-only; no `qa-all`. Targeted checks run in a throwaway worktree of the branch: the branch's
agent_bus and agent_bus_lxc Python suites (311 tests, OK), `go test ./internal/teams/...` (all
ok), `scripts/qa-bash-limits.bash` (OK), ruff on the touched helpers and tests (clean), shellcheck
on `acceptance.bash` (clean). Upstream facts were checked at source (Tuwunel `v1.9.4`, sd-notify
0.5.0, systemd v258).

**Verdict: FIX-FIRST.** Two blocking findings (B1, B2); the design itself holds.

## Blocking

### B1. The merge conflicts with F44 in `meta-deploy.list` and `PLAN.md`, and the queue it would leave is wrong

`git merge-tree F44 origin/fix/teams-server-robustness` conflicts in two files:

- `CLAUDE/Plan/meta-deploy.list`: F44 queues Plan 00194's `play-claude-yolo.yml` then
  `play-agent-bus.yml`; the branch queues `play-agent-bus.yml`, `play-claude-yolo.yml`, then the
  plan folder. Resolving by keeping both gives each play twice. The resolved queue should run each
  play once, `play-agent-bus.yml` first (the homeserver unit, installer, zipapp, health units) then
  `play-claude-yolo.yml` (ccy-teams board, launcher 3.98.12), then `00203-teams-hot-control-and-robustness`,
  with one merged description per entry. The branch's description also says "a running team
  session's watcher still drops that team on a restart (the watcher fix is a separate branch)";
  on F44 that is no longer true and must go.
- `CLAUDE/Plan/00203-teams-hot-control-and-robustness/PLAN.md`: F44 added a `## Decisions` section
  where the branch added `### Phase 2`. Both belong; order them.

Also stale after the merge: `subagent-reports/261016-server-robustness-opus.md` "Left open" still
says gaps 1, 2 and 5 are "the other branch's; until it lands". A one-line note that it landed is
enough.

No conflict in `src-go/internal/teams`: `board/collect.go` and `collect_test.go` auto-merge
cleanly, and the Go suites pass on the branch.

### B2. Every LXC member entry now fails the play when its container does not run systemd as init, even with `service` left out

`run_present` calls `_drop_service` for every (container, user) group that has no `service: true`,
and `run_phase("absent")` does the same for a user leaving its last team. `_drop_service` runs
`systemctl show ... agent-bus-claude-<user>.service` in the container through `guest.root`, which
raises `MemberError` on any non-zero exit. In a container whose PID 1 is not systemd, `systemctl`
exits non-zero ("System has not been booted with systemd as init system"), so a member that never
asked for a unit now fails the whole play. Nothing in `docs/agent-bus-lxc-members.md` or the kit's
`README.lxc` requires systemd as the container's init (the table requires `systemd-detect-virt`,
which is a binary, not an init).

The test that claims otherwise, `test_an_entry_without_service_asks_nothing_of_the_container`,
sets `guest.systemd = False`, but `FakeGuest._systemctl` ignores that flag (only the
`SERVICE_NEEDS_SH` path reads it), so the test passes against behaviour the real guest does not
have. Fix: in the no-service path, probe `/run/systemd/system` (or the unit file's existence)
first and return without running `systemctl` when there is nothing to remove; make the fake fail
`systemctl` when `systemd` is `False`, and see the test go red then green.

## Checked and sound

### Tuwunel 1.9.4 really sends READY and WATCHDOG

- `src/router/run.rs` at tag `v1.9.4`, behind `cfg(all(feature = "systemd", target_os = "linux"))`:
  `start()` sends `NotifyState::Ready`; `start_systemd_watchdog()` is spawned on the runtime and
  sends `NotifyState::Watchdog` every `watchdog_enabled()/2` (ticker with `MissedTickBehavior::Skip`);
  `stop()` sends `Stopping` (or `Reloading` + a watchdog ping on an in-place exec restart);
  startup progress sends `ExtendTimeoutUsec`.
- `src/main/Cargo.toml` `default` features include `systemd` (and `release_max_log_level`, which
  is what compiles out the login-ratelimit debug log the report relies on). The installer downloads
  the `release-all` asset (`files/usr/local/share/agent-bus/tuwunel.pin`), so the feature is on.
- `PrivatePIDs=yes` does not break the arming check. sd-notify 0.5.0 `watchdog_enabled()` returns
  `None` when `WATCHDOG_PID` differs from `process::id()`. systemd v258 `build_environment()` sets
  `WATCHDOG_PID` from `getpid_cached()`; the PrivatePIDs child is made by `raw_clone()` with
  `CLONE_NEWPID`, and `raw_clone()` calls `reset_cached_pid()` in the child (`src/basic/raw-clone.c`),
  so the environment is built after the fork with the namespace PID (1), which is what Tuwunel's
  `process::id()` returns. The 30 s watchdog arms and is fed. R1 proves it on the host.

A residual risk the owner should know: `play-agent-bus.yml` restarts every live homeserver onto
the new unit before `acceptance.bash` runs R1. If the reading above were wrong, every team would
abort every 30 s (and `Restart=always` would keep it looping) until the unit was changed back. The
source reading says it will not happen; the order cannot be reversed because R1 needs the unit
installed. Say so in the meta-deploy description.

### Restart storm, races

- No storm: the drop-in has `StartLimitIntervalSec=0` with `RestartSec=5s`, `RestartSteps=5`,
  `RestartMaxDelaySec=60s`; the retries cost at most one start a minute. The health check restarts
  at most once per minute-run, only after `START_GRACE` (60 s active) and three misses over 25 s,
  and only if the `InvocationID` is unchanged and the unit still `active`; a frozen process is
  already aborted by the watchdog (30 s) before the third miss, so the check leaves it.
- A deliberate `systemctl stop`: the re-read shows `deactivating`/`inactive`, left alone. A
  software swap or `ccy-teams apply` restart: new `InvocationID`, left alone. A backup: SIGUSR2
  does not stop the client API, and the backup unit is `Requisite=` on the homeserver so a restart
  mid-backup fails the backup, which the next check reports.
- `restart_events` treats a lower `NRestarts` as a restart by hand, so the play's restart does
  not count. Systemd's own restarts stay a `PROBLEM` (exit 1 for `health status`) for 24 h after a
  successful recovery; that is a deliberate choice and is documented in the report, not a defect.
- `Type=notify` makes `systemctl restart` in the installer block until READY, bounded by the
  default `TimeoutStartSec` (90 s); Tuwunel extends it only during migrations. A very slow RocksDB
  open would now fail the start where it used to pass. Low risk at this database size; noting it.

### The 180 s wait

- Only `activating`, `deactivating`, `reloading`, `refreshing` are waited for; `inactive` and
  `failed` are refused at once as before (tests cover `activating` then `failed`, and a stopped
  unit never sleeping). At the bound it refuses with the `journalctl` command.
- Progress goes to stderr at the start, every 20 s, and when up. Both callers
  (`files/var/local/claude-yolo/lib/agent-bus-seats.bash`, `files/var/local/claude-code/cc-teams.bash`)
  capture stdout only, so the person sees the lines. The audit asked for the wait only when
  unattended; the branch waits attended too. That is defensible (an attended launch during a boot
  gets the same late homeserver) and the feedback makes it acceptable.

### Health status truthfulness

- It cannot fail open: no record is "never checked", a record past `expires` (checked + 300 s,
  check every 60 s) is "the check last ran N ago", an unparseable record is said, and the Go board
  reads the same fields with the same three outcomes (tests for each). Every problem carries a
  `do:` line. `check` writes the record only after a successful run, so a failing check goes stale
  rather than lying. A homeserver stopped by hand is a `PROBLEM` in `status` (live state), which is
  right for a tool whose exit code means "something needs you".
- The service sandbox is consistent with what it does: `ReadWritePaths=/var/lib/agent-bus-applied`,
  `CAP_DAC_READ_SEARCH CAP_CHOWN` for the 0700 team directory and the group chown, `AF_UNIX` for
  systemd's private socket (a socket inode is exempt from the read-only mount check, so
  `ProtectSystem=strict` does not block `systemctl restart`), loopback-only IP for the probe,
  `ProxyHandler({})` so a proxy environment cannot divert it.
- `agent-bus health status` as the desktop user reads a 0640 record in a 0750 directory owned by
  the stamps' group, the same rule as the stamps; the wrapper refuses root for `health` as it does
  for `seat`.

### The LXC unit

- Idempotent: the unit is written through a compare-then-rename as root, enabled only when
  `UnitFileState` is not `enabled`, restarted only when its text changed, started only when not
  active; a second run issues one `systemctl show` (tested). Removal on `service` turned off or on
  the user's last team leaving (tested).
- Does not fight a hand-started session by design: opt-in per entry, and the entries of one user
  must agree. If a person starts a second session anyway, the team sync lock makes one exit busy
  and the unit retries with a backoff to 5 min, bounded by `RestartMaxDelaySec`. Documented.
- Safe: unit values are refused unless they match `/[A-Za-z0-9._/@+-]*` (no space, `%`, quote or
  backslash; tested), the user name is validated, the file is 0644 under `/etc/systemd/system` in
  the container, and `lxc.start.auto = 1`, systemd-as-init and tmux are checked before any change.
  `Type=forking` over `tmux new-session -d` relies on systemd's main-PID guess, which is the tmux
  server; it ends with the session, so a `/exit` is seen and restarted (B2 in ACCEPTANCE.md proves
  it after a reboot).

### acceptance.bash and ACCEPTANCE.md

- Follows the plan-script shape: `_planlib.inc.bash` sourced through the ansible.cfg boundary
  walk, `plan_init`, `plan_mode deploy`, `plan_parse_common_flags`, `plan_require_host`,
  `plan_prime_sudo`, `plan_start_log auto`, legs through `plan_deploy_leg`, `plan_on_cleanup`,
  `plan_finish`; `--check` is refused with 64 and a reason, which is the right answer for a script
  that only has meaning against a real team. Steps live in `_acceptance-steps.inc.bash` so
  ShellCheck sees them without suppression. shellcheck is clean.
- Throwaway team: `acc-robust` on an ephemeral loopback port, built with the real installer
  (`team --team-file`, every key `teamfile.REQUIRED_KEYS` wants), torn down first (an interrupted
  run) and on every exit (`remove --purge`, which also removes the health record and the root-side
  backups), plus the `/run` start-delay drop-in. Every wait is bounded (longest 150 s).
- Each claim is exercised on the production path: real unit properties and a 75 s quiet period
  (R1), the real health service and `agent-bus health status` as the user (R2, R6, R7),
  `systemctl kill` with SIGKILL/SIGTERM/SIGSTOP and `NRestarts` (R3-R5; R5 also greps the journal
  for the watchdog), a backup aged by `touch` (R7, also read off `ccy-teams --list`'s team row,
  which `s.Loaded[team]` admits because the team has a unit), a real `activating` homeserver timed
  against `seat check` (R8). R9 proves existing member units and says so when none is declared.
  B1-B2 are the owner's reboot steps and are written down.

### Limits, dependencies, names

- Bash limits: gate passes on the branch; the installer shrank (991/1157 to 988/1156) and the
  allowlist row follows; the `agent-bus` wrapper stays at its row.
- Python: stdlib only in `health.py`, `checkout.py`, `members.py` and the tests.
- No install-specific names: the only hostname-like literal is the test placeholder `box`; teams
  and containers are `alpha`, `devbox`, `acc-robust`.
- Docs and DESIGN.md 3.5/3.7 updated with the new units and policy; `localhost.yml.dist`
  documents `service`; the meta-deploy entries tell the owner the homeservers are restarted.

## Non-blocking notes

1. `agent-bus-hs-health@.timer` has no `Persistent=`; not needed (`OnActiveSec=30s` after boot).
2. `health.py` `_write_record` chowns the directory on every run; harmless, one syscall.
3. After the merge, re-run the branch's `test-agent-bus-install.bash` and the Go board tests on
   the merge result before pushing; the auto-merge in `collect.go` is clean but was not executed
   here.
