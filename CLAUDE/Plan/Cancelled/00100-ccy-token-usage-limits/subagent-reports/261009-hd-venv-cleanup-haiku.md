# Why stale hooks-daemon venvs survived ~10 upgrades (read-only investigation)

Scope: read-only. Nothing edited, deleted, restarted, upgraded or approved. Paths are under the
container checkout `<repo>/.claude/hooks-daemon/` (`DD` below). The host view is referred to as
`<host-project>`.

## Summary

Root cause: the upgrade always takes the "already at target" fast path, which never calls
`eager_cleanup_stale_venvs`. The only call site is in the slow path, after Step 15. Layer 1
(`scripts/upgrade.sh`) checks out or resets the target before delegating to Layer 2, so Layer 2
always sees HEAD = target and takes the fast path. The cleanup has therefore never run on a normal
client upgrade since it was added in v3.9.0 (Plan 00100 Task 3.9, commit 8f70fae7a).

Confidence: high on the code path (single call site, explicit early `exit 0` before it). I did not
run an upgrade to reproduce it.

## 1. Every route that changes the daemon version

| Route                                                                                                         | Moves the checkout?                                                                                  | Builds venv                                                                          | Calls eager cleanup?                                                                         | Evidence                                             |
| ------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| Layer 1 `scripts/upgrade.sh` (skill shim fetches and `exec`s it, `skills/hooks-daemon/scripts/upgrade.sh:60`) | Yes: `git reset --hard TARGET` (non-self-install), `checkout` (self-install) at `upgrade.sh:731-733` | Via Layer 2                                                                          | Only if Layer 2 reaches its slow path (see below), which Layer 1 never does                  | `upgrade.sh:688-738`, `upgrade.sh:762-890`           |
| Layer 2 fast path ("already at target")                                                                       | No                                                                                                   | `ensure_venv` at `upgrade_version.sh:~1010`                                          | **No**: `exit 0` at `upgrade_version.sh:1055`, before any cleanup                            | `upgrade_version.sh:870`, `:1055`                    |
| Layer 2 slow path (Steps 6-17)                                                                                | Yes (`checkout` at `upgrade_version.sh:1223`)                                                        | `ensure_venv` at `:1265`                                                             | Yes: `upgrade_version.sh:1564`, after `restart_daemon_verified` at `:1533-1562`              | `upgrade_version.sh:1564`                            |
| Layer 2 second-pass re-exec (target ships a different upgrade script)                                         | No                                                                                                   | Re-enters Layer 2 with HEAD = target, so the fast path                               | No (fast path)                                                                               | `upgrade_version.sh:1718-1748`                       |
| Pre-deploy gate stop (`abort_before_deploy`, gate exits 3/4 and timeout/other)                                | Restores the installed version with `git reset --hard` (`upgrade_version.sh:366`)                    | No new venv                                                                          | Not needed; nothing new was built                                                            | `upgrade_version.sh:~520-545`, `:340-390`            |
| Fast-path restart failure (`fail_fast`), Step 15 restart failure (`exit 1`)                                   | Yes                                                                                                  | Yes (new venv built)                                                                 | No, intentionally, for rollback safety, so the old venv is left behind until a later success | `upgrade_version.sh:1553`                            |
| `install_version.sh` (fresh install via `init.sh`/`provision.sh`)                                             | No (clone already present)                                                                           | `ensure_venv` at `install_version.sh:364`; `rm -rf` only the legacy `venv/` (`:379`) | No                                                                                           | `install_version.sh:336-380`                         |
| `provision.sh` (project `.claude/provision.sh`, fresh checkout)                                               | Via init.sh                                                                                          | Via `install_version.sh`                                                             | No                                                                                           | `provision.sh` (fresh checkout only, per its header) |
| `repair` (`cli.py` ~2440-2470)                                                                                | No                                                                                                   | Syncs `venv-<fp>` keyed on daemon dir                                                | No                                                                                           | `cli.py:2451-2461`                                   |
| Lazy rebuild in `ensure_venv` (`venv.sh:1117-1260`)                                                           | No                                                                                                   | Rebuilds only its own target path; `rm -rf "$venv_path"` at `venv.sh:1239`           | No, never touches siblings                                                                   | `venv.sh:1239`                                       |
| Daemon start (`cli.py:615`)                                                                                   | No                                                                                                   | No build; the resolver only selects                                                  | No                                                                                           | resolver `paths.py:1057-1068`                        |
| Rollback `install/rollback.sh`                                                                                | Yes (`git checkout "$git_ref"` at `rollback.sh:278`)                                                 | No                                                                                   | No                                                                                           | `rollback.sh:278`                                    |
| Manual `prune-venvs --all-except-current --force` (`cli.py` `cmd_prune_venvs`)                                | No                                                                                                   | No                                                                                   | This is the manual cleanup, and the docstring says so                                        | `cli.py` `cmd_prune_venvs` docstring                 |

So the only automatic cleanup is `upgrade_version.sh:1564`, and it is unreachable on a Layer 1 upgrade.

## 2. Why the fast path is always taken (the root cause)

- `upgrade_version.sh:846`: `ROLLBACK_REF=$(git describe --tags --exact-match ...)` (the current tag).
- `upgrade_version.sh:870`: `if [ "$ROLLBACK_REF" = "$TARGET_VERSION" ] || [ "$TARGET_COMMIT" = "$HEAD_COMMIT" ]`.
- Layer 1 has already moved HEAD onto `TARGET_VERSION` (`upgrade.sh:733` or `:738`, before
  delegation at `:762+`), so both conditions are true.
- The fast path runs `ensure_venv` (`:883`-ish, under the fast-path block) and then `exit 0` at
  `:1055`. The cleanup at `:1564` is never reached.

History: the `ROLLBACK_REF = TARGET_VERSION` check dates from `5c6f4b748` (Plan 00041, 2026-02-10).
Plan 00291 (`af8dc45347`, 2026-09-09) added the commit comparison and says "every tag takes this"
fast path. The cleanup call (`8f70fae7a`, Plan 00100 Task 3.9, 2026-04-24, in v3.9.0) was added to
the slow path only, so the slow path is the only place it can run.

Tests do not catch this. `tests/integration/test_upgrade_eager_cleanup.py` calls the bash function
directly (`_run_bash`, tests at lines ~50-142). Nothing drives `upgrade_version.sh` down the
Layer 1 route and asserts that a stale venv is removed.

## 3. Fingerprint and current-venv accounting

- `paths.py:178-212` `python_venv_fingerprint(root)`: `md5(sys.version | sys.base_prefix | platform.machine())[:8]`,
  prefixed with `project_path_slug(root)`. The digest does **not** depend on `root`. It depends on
  `sys.version`, which includes the **interpreter build timestamp**.

- Evidence on this checkout: `/usr/bin/python3.11` mtime is `2026-10-07 12:35:07`, and its
  `sys.version` reads `(main, Oct  7 2026, 12:35:07)`. The venv `...-py311-81c29529` was built
  and stamped on 2026-10-03 (`.daemon-metadata.json` `written_at` `2026-10-03T22:15:45Z`). A
  python rebuild between those dates changed the digest from `81c29529` to `2a519198`. Running the
  venv's own interpreter now gives `workspace_claude_hooks-daemon-py311-2a519198` (daemon-dir slug)
  and `workspace-py311-2a519198` (project-root slug). The rebuild, not a logic error, explains the
  different digests. The earlier py311 digests (`28fb230b`, `66bbc57c`) are the same effect over
  earlier interpreter builds. Each interpreter rebuild orphans one venv, which is exactly what
  eager cleanup is meant to remove.

- The legacy `venv-py311-28fb230b` (v3.19) and the slugged `venv-workspace_claude_hooks-daemon-py311-28fb230b`
  (v3.38) share a suffix because they were built on the same interpreter. The suffix alone does not
  identify the slug. This is not a bug in itself.

- **Slug mismatch (real bug, latent)**: venv creation keys on the daemon dir
  (`venv.sh:1213` `python_venv_fingerprint "$python_bin" "$daemon_dir"`; `cli.py:2456` `repair`),
  but the listing and prune code keys on the project root:

  - `cli.py:2890` (`_enumerate_venvs`): `current_fp = python_venv_fingerprint(project_root)`
  - `cli.py:3196` (`cmd_list_venvs` display): same
  - `cli.py:3256` (`cmd_prune_venvs`): same

  So the fingerprint predicate in `_enumerate_venvs` (`is_current`) never matches a slugged venv
  name. The real venv is protected only by the resolver-active realpath check
  (`_resolver_active_venv_realpath`, `cli.py:~2843`). Consequence: a venv that is fingerprint-current
  but not resolver-active would be pruned by `prune-venvs --all-except-current`.

- The running daemon does **not** follow the name fingerprint. Resolver step 2 (`paths.py:1153-1200`)
  is metadata-authoritative: it takes the first venv-\* whose `lock_hash` matches the project lock
  hash and whose slug is eligible. The old `81c29529` venv still matches and is still the one in use.
  Once `eager_cleanup_stale_venvs` finally runs, it deletes by path, so it removes `81c29529` and
  keeps only the venv `ensure_venv` just built (`2a519198`). The daemon then restarts on that venv.
  That ordering is safe. The one thing to avoid is deleting the resolver's active venv before the
  replacement is verified.

## 4. Host view vs container view

- Both views share one `untracked/` directory through the bind mount. Venv names differ by slug:
  container `workspace_claude_hooks-daemon-py..`, host `home_<slug>-py314-..`.
- `eager_cleanup_stale_venvs` (`scripts/install/venv.sh:1613-1650`) is **not slug-aware**. It deletes
  every `untracked/venv*` whose absolute path differs from `$current_venv`. Run from either view it
  would delete the other view's venv. Run from the host, it would evict the container's venv, and
  the reverse.
- The resolver's slug filter (`_venv_slug_eligible`, Plan 00313, `paths.py:237`) is the safe rule
  and is not used by the cleanup. This is the hazard to address before enabling the cleanup on the
  fast path. Enabling it unchanged would start deleting host-view venvs from container upgrades,
  which is what today's manual deletion of the host-view venvs would have done.
- The bash path comparison also works across views only because `current_abs` is the path as seen by
  the caller. Two views never share a venv name, so path-equality cleanup removes the other view's
  venv every time.

## 5. The `reset:` vs `checkout:` reflog entries

- `checkout: moving from ... to vX` (v3.19 ... v3.51, to 2026-08-09) came from the older Layer 1,
  which used `git checkout "$TARGET_VERSION"`. The `reset: moving to vX` entries (v3.54 onward,
  2026-08-24 onward) come from the current Layer 1 `git reset --hard --quiet "$TARGET_VERSION"`
  (`upgrade.sh:733`, introduced in `bf7457898`, 2026-08-10). `upgrade_version.sh:366` (the
  pre-deploy restore) also emits `reset:`.
- One entry is anomalous: `checkout: moving from e55c2ea... to refs/tags/v3.68.0` at
  `2026-10-03 22:15:01`. The `refs/tags/` spelling matches no script in the daemon tree (grep for
  `refs/tags` finds only `version_check.py:165` parsing and `branch_safety.py:484`). It was probably a
  manual `git checkout refs/tags/...` followed by a Layer 2 run, which also takes the fast path. I
  could not attribute it. The venv was written 44 seconds later, consistent with `ensure_venv` on the
  fast path.
- So no route other than Layer 1 / the restore path moves the daemon version in normal operation,
  apart from the unattributed manual checkout above and `rollback.sh:278`.

## Root cause(s), ranked

1. **High**: the fast path (`upgrade_version.sh:870` to `:1055`) returns before the only cleanup call
   (`:1564`). Layer 1 always puts HEAD on the target first, so every client upgrade takes the fast
   path. Cleanup has never run on a normal client upgrade since v3.9.0.
2. **High (for the survival)**: no other route cleans up. `ensure_venv` removes only its own target
   path (`venv.sh:1239`), and `repair`, `install_version.sh` and daemon start never prune.
3. **Medium (latent)**: fingerprint-driven "current" accounting uses the project-root slug in
   `cli.py:2890/3196/3256`, but venvs are keyed on the daemon-dir slug (`venv.sh:1213`,
   `cli.py:2456`). Fingerprint-based `is_current` never matches, so the only protection is the
   resolver realpath.
4. **Medium (cause of orphaning)**: `python_venv_fingerprint` includes `sys.version`, including the
   interpreter build timestamp. Each system-Python rebuild (for example `/usr/bin/python3.11` on
   2026-10-07) gives a new fingerprint and orphans the old venv.

## Suggested upstream fix

1. Run the cleanup on **every successful** upgrade, not only the slow path. Factor it into one tail
   step after `restart_daemon_verified` and call it from the fast path before `exit 0` at
   `upgrade_version.sh:1055`. Keep it after the verified restart so rollback safety holds.
2. Make the cleanup slug-aware before enabling it on the fast path. Only remove `venv-*` entries
   whose slug equals `project_path_slug(daemon_dir)` (or that are the legacy `venv/`), using the same
   eligibility as `_venv_slug_eligible` (`paths.py:237`). Leave other views' venvs alone. Alternatively
   route through `hooks-daemon prune-venvs --all-except-current` with the slug filter applied.
3. Make `cli.py` `current_fp` use `daemon_dir`, as `venv.sh:1213` and `cli.py:2456` do (lines
   2890, 3196, 3256). Add a test that the listed current fingerprint equals the `ensure_venv` name.
4. Add an integration test that drives `upgrade_version.sh` with HEAD already on the target (the
   Layer 1 shape) and asserts a stale same-slug venv is removed and a foreign-slug venv is kept.
   The current test calls only the bash function.
5. Optional, separate change: decouple the fingerprint from the interpreter build timestamp
   (for example `sys.version_info`, `sys.base_prefix` and `platform.machine()` without the build
   date). This would stop a same-version Python rebuild from orphaning the venv. It changes every
   existing fingerprint, so it needs a migration (or a one-time adoption of the existing venv by
   lock hash). Do not do it casually.

## Immediate observations (no action taken)

- The current venv `venv-workspace_claude_hooks-daemon-py311-81c29529` is still the resolver's
  active venv (lock hash matches). The next `ensure_venv` will create `...-2a519198` as a new
  sibling. Until then the running daemon is correct.
- Any fix that enables the existing cleanup unchanged on the fast path would delete the host-view
  venvs in the shared `untracked/`. Do not ship that without the slug filter.
