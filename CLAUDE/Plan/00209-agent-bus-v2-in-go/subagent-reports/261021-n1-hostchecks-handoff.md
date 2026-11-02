# N1 Task 1.4 (hostchecks): handoff

Superseded by decisions 1-3 of [261021-n1-hostchecks.md](261021-n1-hostchecks.md): no Element
mode, no host script and no meta-deploy entry, the probes kept as Python in the plan folder. What
follows is the handoff as it was written.

## Constraints from the lead, after the WIP commits (they override what follows)

1. **No new Python tooling.** The delivered probe is Go: a Go test behind a build tag (as M44 is) or
   a small Go program, driven by the plan script. `hca_server.py`, `hca_probe.py` and `test_hca.py`
   (committed in b8c3fcb3 and 3b7be112, not pushed) are the reference to port, then `git rm`. The
   findings and API answers below stay valid: they are Tuwunel's, not the language's.
2. **Gate A.** On the host the throwaway Tuwunel uses no `teamchat` name in any unit or path: a
   mktemp directory, a plain process (no unit), a random free port, nothing left behind; the script
   refuses anything else, or else targets the VM lab. Rename the server name `hca.teamchat.internal`
   to a neutral one (for example `hca.invalid`), and the Element profile `Element-teamchat-hca` must
   not carry the name either (or the Element mode is dropped). Move the work directory out of the
   run directory into `mktemp -d`; keep only the report in `PLAN_RUN_DIR`.

Worktree `untracked/worktrees/bus-v2-n1-hostchecks`, branch `feat/bus-v2-n1-hostchecks`, WIP commit
b8c3fcb3 on db3c8350. Read `untracked/scratch/n1-common.txt` (common rules) first.

## Done

- `hca_server.py`: the pinned Tuwunel run as the invoking user on a free loopback port, a team's config
  (v1's key set, C 3.2.4), and A 4.1-4.2's rooms as data (main, reports restricted to main, questions
  open and closed); requests, sync, the worker filter of A 6.10.
- `hca_probe.py <repo> <work> <report> [--binary P] [--extra K=JSON]`: the server checks HC-A7, HC-A4,
  H-C12, HC-A2 (server half: push rules and unread counts), HC-A6 (with `!admin server show-config`
  saved as show-config.txt beside the report), HC-A1 (a second short-lived server with 10 s timers for
  the timeout), H-C13, H-C14 (restore simulated by a cold copy of the stopped database). Exit 0 / 1 / 2;
  a missing or wrong binary is `[FATAL]`, exit 1, naming `playbooks/imports/play-agent-bus.yml`.
- `test_hca.py`: pure-part tests, written first. It imports `hca_element`, which does not exist yet,
  so it does not run until that module is written.

## Harness decision

The 00211 spike's shape (`spike.bash` + `spike_probe.py`): the installed pinned binary
`/usr/local/lib/agent-bus/tuwunel-<version>` with its `.asset-sha256` record checked against this
checkout's `files/usr/local/share/agent-bus/tuwunel.pin`, plus `--version`. No unit, no root, no VM:
simplest and touches nothing but its run directory. Not the VM lab (needs a VM for a user process);
not the M44 Go test (the matrix client is not on this branch, and section 14 makes the tagged Go test
the permanent regression form after the matrix client merges).

## Run in the container against the real v1.9.4 binary

The pinned `.zst` asset (sha256 matches the pin) is under `untracked/scratch/tuwunel-dl/`, decompressed
with `untracked/scratch/zstd-tool/unzstd.py` (ctypes over libzstd; no zstd binary here). The
container needs `--extra db_pool_workers=4` (the default pool hits EAGAIN under the container's pids
limit). Command, from the worktree:

```
python3 CLAUDE/Plan/00209-agent-bus-v2-in-go/hca_probe.py . untracked/scratch/hca-run/work \
  untracked/scratch/hca-run/report.md --binary untracked/scratch/tuwunel-dl/tuwunel-1.9.4 \
  --extra db_pool_workers=4
```

(remove `untracked/scratch/hca-run` first). Last result: FAIL (exit 1), two real findings below;
HC-A2 (server) was NOT JUDGED only because of a probe bug fixed after the run (Tuwunel omits
`unread_notifications` from a batch that did not change them), not yet re-run.

## Findings on Tuwunel 1.9.4 (design-text fixes to propose)

1. HC-A1 FAILS on one fact: a sync with `set_presence=offline` SETS offline (A 6.6 assumes no
   change). Online by the default sync, PUT offline, and the timers all hold. Timers: unavailable after
   `presence_idle_timeout_s`, offline after a further `presence_offline_timeout_s`; defaults 300 and
   1800, so a dead seat shows online up to 5 min and is offline after 35 min. Propose: A 6.6 says other
   requests must not sync at all (or sync without `set_presence`, which marks online), and C 3.2.4 sets
   both timers explicitly.
2. H-C13 FAILS on one fact: a second password login with the same `device_id` does NOT revoke the
   first token (both stay valid until logout; one logout ends both, as they share the device). Logout,
   UIA on password change and self-deactivation, get_token refused, the admin read of devices and
   `deactivated`, and the fallback (DELETE /devices/<id> with UIA by password) all hold. No config key
   refuses a member's own password change (only `login_with_password`, which would break DC16).
   Propose: DC16 (c') takes the logout-before-login order, or the fallback.
3. H-C14: a since ahead of the database WAITS for the timeout and answers next_batch at the server's
   (lower) position; a post after the restore is NOT delivered to a member holding the old since. A
   roster state read answers at once. Propose: C 3.12.2 / B make every member drop its since after a
   restore (`restore_epoch`), which C47 may already say; check.
4. HC-A6: the service account is `@conduit` (v1's reserved name holds).
5. HC-A4, HC-A7, H-C12: hold as designed (create event in the state list with sender, event ID = `$` +
   room ID without `!`; power levels read back exactly).

## Left

1. `hca_element.py`: owner Element checks (HC-A2 display, HC-A3 shapes, HC-A5), run by
   `acceptance-hca.bash --element`, refusing `PLAN_ASSUME_YES=1` (meta-deploy answers every question
   yes) and needing a terminal; profile `~/.var/app/im.riot.Riot/config/Element-teamchat-hca/` with a
   locked config.json (v1's keys in `helpers/agent_bus/element.py` `config_json`, copied as data, not
   imported), refused if it exists, removed on cleanup; `judge_reply`, `judge_thread`,
   `judge_thread_quote`, `judge_edit`, `parse_answer` as `test_hca.py` specifies. Or drop the Element
   mode and its tests and leave those checks to the owner's steps (A 14's Element row): decide.
2. `acceptance-hca.bash` per PlanScriptStandards (bootstrap, `plan_mode deploy`, `plan_require_host`,
   `plan_start_log auto`, `plan_on_cleanup` killing `work/tuwunel.pid` and `work/timers/tuwunel.pid`
   and removing the work dir; exit the probe's status, 2 included, after `plan_list_reports`).
3. `meta-deploy.list` entry `00209-agent-bus-v2-in-go/acceptance-hca.bash`. NOTE: meta-deploy maps
   exit 2 to COULD NOT ESTABLISH only for a folder's `acceptance.bash`; a script entry shows
   `FAIL (exit 2)`, and meta-deploy.bash is on the bash-limits ratchet. Say so in the entry.
4. Run `python3 -m unittest test_hca` (from the plan folder), ruff, shellcheck, bash -n,
   `./scripts/qa-changed.bash`; the review loop (opus, `261021-n1-hostchecks-review.md`); the report
   `261021-n1-hostchecks.md`; PLAN.md Task 1.4 state.
