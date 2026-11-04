# Review: fix/teams-security-infra (tip fd409d30) — Plans 00161/00201 infrastructure fixes

Reviewer: Fable 5.1, read-only, 2026-10-15. Diff: `git diff F44...origin/fix/teams-security-infra`.
Builder report: `untracked/agent-reports/auto/261015-192144-general-purpose-a6f728cf2961642b7.md`.

## Verdict: MERGE

Nothing found that would stop a homeserver from starting after the deploy. Every new systemd
directive and every new Tuwunel config key was checked against the shipped versions' own
sources; the tests on the branch pass here (Python: OK; installer suite: 291 passed, 0 failed,
run in a detached worktree of fd409d30). Two Medium items are deploy-hygiene, not defects.

## Evidence by question

### 1. systemd directives

- **Fedora 44 ships systemd 259.9** (`rpms/systemd` f44 branch spec, `Version: 259.9`).
  `PrivatePIDs=` arrived in 257, `TemporaryFileSystem=`/`BindPaths=` are old. The CCY
  container has systemd 252 (Debian), so `systemd-analyze verify` cannot be run here; the
  checks below are against systemd v259 source and systemd.exec(5) from main.
- **Context validity.** Unit is `Type=simple`, `User=agent-bus` (no `DynamicUser=`),
  `ProtectSystem=strict`, `ReadWritePaths=/var/lib/agent-bus/%i`. systemd.exec(5):
  `PrivatePIDs=` is "only supported for service units", "not supported with `Type=forking`",
  implies `MountAPIVFS=yes`, and the unprivileged-user caveat is about per-user managers, not
  system services with `User=`. The `TemporaryFileSystem=/var:ro` + `BindReadOnlyPaths=`
  pattern is the man page's own example; `BindPaths=` says the destination is created when
  systemd can create it, which it can inside the tmpfs.
- **Main PID under PrivatePIDs** (`src/core/exec-invoke.c` v259 `setup_private_pids`): the
  executor forks with `FORK_NEW_PIDNS`, the parent sends the child's pidref to the manager
  over `pidref_transport_fd` and `_exit`s; the function "returns in the child process only".
  So the manager's main PID is Tuwunel itself, PID 1 of the namespace, with no extra
  supervisor process. `systemctl kill --kill-whom=main --signal=SIGUSR2` (backup-now) and
  the stop SIGTERM therefore go straight to Tuwunel.
- **Signals to PID 1 of a namespace** are delivered from the parent namespace only when a
  handler is installed (plus SIGKILL/SIGSTOP). Tuwunel 1.9.4 `src/main/signals.rs` installs
  handlers for SIGQUIT, SIGTERM, SIGUSR1, SIGUSR2 and SIGINT, so stop and backups work.
  Consequence: a SIGUSR2 sent before the handler is installed is now dropped rather than
  fatal (the installer's comment at `cmd_backup_now` still says it "kills"; the retry loop
  there already covers both outcomes).
- **Same path in `BindPaths=` and `ReadWritePaths=`** (`src/core/namespace.c` v259):
  `MountMode` is ordered by priority with `MOUNT_BIND` before `MOUNT_READ_WRITE`;
  `mount_path_compare` sorts equal paths by mode; `drop_duplicates` keeps the first (the
  bind) and ORs the other's `read_only`/`exec` flags in (both false). `make_read_only` for
  that entry has `flags_mask == 0` and returns without remounting; it also "never turns off"
  RDONLY, but the bind is a fresh mount created before `/` is remounted read-only by
  `ProtectSystem=strict`, so the team directory is **read-write**, the tmpfs over
  `/var/lib/agent-bus` is read-only (`:ro` applied in the final pass), and prefixes are
  mounted before their children (path order). Tuwunel's `database_path` and
  `database_backup_path` are under `/var/lib/agent-bus/<team>/` (render.py), so nothing it
  needs is hidden.
- **Does it block a sibling's `admin.token`?** Sibling directories are not in the tmpfs at
  all (only `%i` is bound back): no path exists to open. The same-uid escapes —
  `/proc/<sibling pid><home>`, `/proc/<pid>/cwd`, `/proc/<pid>/fd`, `ptrace` — need the
  sibling's PID to be nameable, and `PrivatePIDs=` puts each homeserver in its own PID
  namespace with its own `/proc`. `PrivateTmp`, `ProtectHome` and the unchanged
  `IPAddressDeny`/`SocketBindDeny` close the remaining shared places (`/run` has no
  agent-bus-owned path; `RuntimeDirectory=` is not used). I find no remaining route from one
  team's homeserver to another's secrets as the shared `agent-bus` user.
- **Restore unit** (`sandbox_properties`) parses the installed unit's `[Service]` lines bar
  `ExecStart`, substitutes `%i`, and dies on any other specifier; the three new lines carry
  no other specifier, and the test checks each lands as a `--property=` of `systemd-run`.

### 2. Tuwunel 1.9.4

- **Release exists**: `v1.9.4`, published 2026-10-10 (GitHub release API).
- **Digests match the pin exactly**: `v1.9.4-release-all-x86_64-v1-linux-gnu-tuwunel.zst`
  `78628e6d…edd5a3` and `…aarch64-v8-linux-gnu-tuwunel.zst` `67295bb0…c97f7`, from the
  release API's per-asset `digest`, as the pin's comment says.
- **Example config hash** `6510ebc7…453fd7` matches the test fixture's `EXAMPLE_CONFIG_SHA256`
  (fetched `tuwunel-example.toml` at `v1.9.4`, sha256sum here).
- **Config keys are real** (`src/core/config/mod.rs` v1.9.4): `max_request_size: usize`
  (default `24 * 1024 * 1024`); `enable_set_displayname: bool` (#608); `rate_limiting: RateLimits` → `login.failed { per_second: f64, burst_count: u32 }` defaults 0.17/3 and
  `login.account` defaults 0.003/5 (#597). The rendered `account` values equal the defaults.
  `check.rs` refuses `max_request_size < 10_000_000`, so 10 MB is the floor as stated.
  `rate_limiting` is in the `ignore` list the unknown-option check uses, so
  `error_on_unknown_config_opts = true` accepts the tables.
- **Bare keys plus tables** (`Config::load_file`): a file whose Default profile holds any
  non-table value is "headerless" and is loaded with `provider.profile(Profile::Global)`, so
  `[rate_limiting.login.failed]` is `global.rate_limiting.login.failed`; mixing bare keys
  with a `[global]` header is refused, which the renderer never does. The renderer's
  scalars-then-tables order is what keeps `[rate_limiting...]` from swallowing later bare
  keys; the test `test_scalars_come_before_the_tables` pins it.
- **Display names set by bootstrap**: the gate is `may_set_displayname` in
  `src/api/client/utils.rs` (`config.enable_set_displayname || appservice || is_admin`),
  called only from `src/api/client/profile.rs`. Bootstrap uses
  `PUT /_synapse/admin/v2/users/<id>` (admin.py `create_account`), a path the gate is not on,
  and the caller is a server admin anyway. Per-room `m.room.member` names are not gated —
  documented in Limits, and pingbus reads the account.
- **Upgrade path 1.9.3 → 1.9.4**: `src/service/migrations/mod.rs` has `DATABASE_VERSION = 17`
  in **both** versions; 1.9.4 adds two run-once passes (`rebuild_thread_summaries`,
  `redactions::restore_redactions`) stamped as keys in the `global` column, and a
  startup repair for short-id residue. `src/database/maps.rs` gains two column families
  (`roomtseventid_prevwalk`, `servershortroomid_park`).
- **Rollback**: both 1.9.3 and 1.9.4 `src/database/engine/open.rs` open "undescribed"
  existing columns as `descriptor::IGNORED` with a debug warning, and the schema version is
  unchanged, so reverting the pin to 1.9.3 and re-running `software` reopens a database 1.9.4
  has touched. The clean route is `agent-bus-install restore --team <t> --backup <id>`, whose
  state tar carries `secrets/` (so `admin.token`), but see M1: nothing in this change takes a
  backup before the swap.

### 3. L3 — deleting `admin.password`

- With a stored token, `_admin_api` calls `_session` first; `_session` raises on a rejected
  token (`whoami` 401/403) or a wrong admin (`_check_admin`) **before** `_remove_admin_password`
  runs, so an invalid token at that moment leaves the password in place. It also changes
  nothing in practice: the pre-change code never used the password once a token file
  existed, so the password was never a recovery path for a bad token.
- A run that died after registering and before storing the token: `stored` is true →
  `M_USER_IN_USE` → login with the stored password → token written → password removed.
  Tested (`test_recovers_from_a_run_that_died_before_storing_the_token`).
- Token file gone later, password gone: `M_USER_IN_USE` with `stored` false → the fresh
  password file is unlinked and the error names `agent-bus-install restore`. Tested. The
  state tar includes `secrets/`, and `cmd_restore` replaces `secrets/` wholesale, so a
  restore does bring the token back.
- `admin.password` has no other reader (`helpers/`, installer, `src-go/`, `docs/`: only
  admin.py). The 1.9.4 restart does not invalidate access tokens (they live in the DB; no
  token-related migration).

### 4. Stamps 0640 / 0750, group

- Readers: the only consumer outside the installer is `ccy-teams`' board
  (`src-go/internal/teams/board/collect.go` `os.ReadDir(c.AppliedDir)`), run on the host as
  the desktop user by the `ccy` launcher; LXC members and the CCY container never read
  `/var/lib/agent-bus-applied` (no reference in `files/var/local/claude-yolo/` or the
  entrypoint). The installer reads as root. So one group suffices.
- The play passes `AGENT_BUS_STAMP_GROUP: "{{ user_login }}"`: the user's primary group on a
  Fedora install (UPG). `ensure_dir` re-chowns/chmods an existing 0755 directory (its stat
  comparison includes group), and `put_file` reinstalls a stamp whose mode or group differ,
  so existing installs converge. A missing group fails the run loudly (tested). See M2.
- The board's error path on an unreadable directory is `PrintError`, not a crash.

### 5. L6 — status cleanup

- `_empty_stray_status` touches only `type == agent_bus.status`, `state_key` a string not
  starting with `@`, content non-empty. Protocol: `parse_status_event` requires
  `state_key == sender` (a user ID, always `@…`), and the empty state key belongs to
  `EVENT_TEAM`, a different type. Matrix auth rules hold `@`-keys to their sender, so a member
  can never own a stray key. Departed members' `@` keys are left alone. No legitimate state is
  reachable. Writes happen only when something is stray (tested: no non-GET call otherwise).
- Residual (documented by the builder): the number of keys is unbounded; a member can keep
  writing fresh keys up to `max_request_size` each between bootstraps. Same class as before
  the change; not a regression.

### 6. Deploy entry, docs, tests, names

- `meta-deploy.list`: appended to the already-queued `play-agent-bus.yml` entry, not queued
  twice; its sentences match what the code does (3 then 1/5 min, display names refused,
  10 MB, password deleted, stamps 0640/0750). One imprecision, L1.
- `docs/agent-bus.md` Limits: "limits only password sign-in", "no per-account send limit",
  per-room display name residual — all true per the 1.9.4 changelog and `RateLimits`.
- Tests exercise the production paths: the installer suite runs the real `agent-bus-install`
  under `PREFIX` with a fake `systemd-run` log and checks every sandbox property; the Python
  suite drives the real `bootstrap` against the recorded-behaviour fake, including the new
  whole-room-state route. The fixture key list is regenerated from 1.9.4's example config
  and pinned by hash.
- Install-specific names: grep of every added line under `CLAUDE/` for home paths, emails,
  IPs, `.lan`/`.local` hostnames found only design vocabulary (`local` literal, `acceptance`
  team). None found.

## Findings

### Medium

- **M1 — No restore point is taken before the binary/unit swap.** The only backups are the
  daily timer's (`OnCalendar=daily`, up to ~25 h old); `cmd_team`/`converge_units` restart
  without one. Rollback by reverting the pin works (schema 17 unchanged, unknown columns
  ignored), but a same-minute `agent-bus-install backup-now --team <t>` for each team ahead
  of `play-agent-bus.yml` in `meta-deploy.list` would make the worst case a known restore.
  Non-blocking; a deploy-queue line, or `software` taking a backup before a binary change.
- **M2 — `AGENT_BUS_STAMP_GROUP: "{{ user_login }}"` assumes a group named after the login.**
  True on Fedora's user-private-group default; where it is not, the stamp step dies after the
  homeserver has already been restarted, so the play fails with a running homeserver and no
  stamp (ccy-teams then shows the team as not applied). Fail-fast and tested, so acceptable;
  `{{ user_group | default(user_login) }}`-style sourcing from the user's actual primary
  group would remove the assumption.

### Low

- **L1 — Wording.** `meta-deploy.list` says status events "under a key that is no member's";
  the code empties keys that are not user IDs (a departed member's `@` key stays). The
  builder report says `login.account` is "left at its default" but the renderer pins it
  explicitly (same values); DESIGN says "Tuwunel's default", which is correct today.
- **L2 — Stale comment.** `cmd_backup_now`'s comment says SIGUSR2 "kills a Tuwunel that has
  not yet installed its handler"; under `PrivatePIDs=` an unhandled signal to PID 1 from the
  host is dropped instead. The retry loop handles both.
- **L3 — Residual, documented.** Stray `agent_bus.status` keys are bounded in size, not count.

## Not verified here

- `systemd-analyze verify` on a systemd ≥ 257 (container has 252); reasoning is from v259
  source and the man page. The installer's `wait_ready` fails the run if the unit does not
  come up, so a surprise surfaces at deploy, not silently.
