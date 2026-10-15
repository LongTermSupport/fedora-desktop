# Plan 00201 M7 acceptance: adversarial review (Fable)

Scope: `git diff 14058c62...agent-a275e561ca3b18324-4f62db25` (acceptance.bash,
`_acceptance-m7.inc.bash`, `m7_check.py` + tests, ACCEPTANCE-owner.md, meta-deploy entry).
Read-only review; nothing was run on a host.

## Collateral-damage audit (what was attacked, what held)

| Path                               | Result                                                                                                                                                                                                                                                                                                             |
| ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Purge of real teams                | Every purge, park, `rm -rf`, `podman rm` is keyed on the two literal names `accept201a`/`accept201b` (`M7_TEAMS`, readonly). No variable can be empty: `rm -rf -- "${M7_SEATS_DIR:?}/${team}"` guards the root; the installer's `remove --purge` deletes only `$STATE_ROOT/$team` and `$INSTALL_STATE_ROOT/$team`. |
| `remove --purge` on an absent team | `agent-bus-install cmd_remove` (lines 856-889): units not enabled, `remove_firewall` returns 0 with no record, no drop-in, no stamp, no state. Exit 0, so `m7_leftovers` on a fresh host is a no-op, not a crash.                                                                                                  |
| Firewall rules of real teams       | Only rules in `/var/lib/agent-bus-install/<accept-team>/firewalld.rules` are queried or removed; a real team cannot share a rule (different port).                                                                                                                                                                 |
| Owner's `localhost.yml`            | Read once (`declare declared`) for the collision check; never written. `ccy-teams`/the launcher read `CCY_TEAMS_HOST_VARS` (Go `config.go:35`); no `apply`, `create` or `describe` runs; the headless launch with explicit `SEAT@TEAM` is unattended, so `choose-teams` (the scope-widening path) never runs.      |
| Element                            | The installer has no Element step; Element accounts come from `play-agent-bus.yml` on `agent_bus_element_teams`, which the run never invokes and the throwaway host_vars does not declare.                                                                                                                         |
| `.last-launch.conf` on exit paths  | Saved before the first launch, restored in `m7_cleanup` and in `m7_teardown_after_stop`, which `plan_on_cleanup` runs on EXIT, INT, TERM and HUP (planlib 569-572, 466-485). `check_leg`/`plan_deploy_leg` fail via `exit 1`, so the EXIT trap runs. Holds for every path except SIGKILL/power loss (see F1).      |
| Crash inside the trap              | Each teardown step is \`                                                                                                                                                                                                                                                                                           |
| PlanScriptStandards                | R1 bounded walk present; `plan_prime_sudo` before `plan_start_log`; `plan_mode deploy`; all legs bare top-level; diagnostics to stderr; `--check` refused with 64. Compliant.                                                                                                                                      |
| Goodbye (Plan 00194)               | The script counts no room messages. `expect_sent` judges `say`'s own `SENT` line; the 4 s goodbye wait (`hooks.py GOODBYE_TIMEOUT_S`) sits inside `M7_EXIT_S=60`. Not affected.                                                                                                                                    |
| meta-deploy order                  | Entry is last, after `play-agent-bus.yml`, `play-claude-yolo.yml` and `00161/acceptance.bash`. Correct. meta-deploy does not stop on an earlier FAIL, which is acceptable here (the run checks its own prerequisites).                                                                                             |

## Findings

**F1 — MEDIUM — a rerun after a SIGKILL replaces the owner's Quick Launch record for good.**
`_acceptance-m7.inc.bash:209-213` (`m7_leftovers`) and `:383-386` (`m7_save_launch_conf`).
Scenario: a run is killed (not signalled) after `m7_start_multi`; `.last-launch.conf` now
carries `LAST_TEAMS="1@accept201a,1@accept201b"`. The next run's `m7_leftovers` does not look
at it, `m7_save_launch_conf` saves the polluted file, and C1 "restores" it: PASS, while the
owner's real record is gone. Meanwhile every interactive `ccy` runs `ccy-teams describe-seats`
on a purged, undeclared team (`claude-yolo:1241`). ACCEPTANCE-owner.md:29-31 pushes this onto
the owner by hand. Fix: in `m7_leftovers`, if the live conf's `LAST_TEAMS` names either
acceptance team, restore from the newest `last-launch.conf.before` under
`untracked/plan-runs/<plan>/acceptance.bash/*/` (or FAIL naming that file); make
`m7_save_launch_conf` refuse to save a conf that already names an acceptance team.

**F2 — LOW — the collision guard covers declared teams only.** `:165-173` checks the owner's
host_vars; `:200-213` then purges by name. A team installed under either name but not
declared (another checkout's host_vars, a half-finished `ccy-teams create`) is purged. Fix:
before `remove --purge`, `sudo -n cat` the installer's recorded team file for that name and
require its `description` to start with "Plan 00201 acceptance", else FAIL.

**F3 — LOW/MEDIUM — the bus-teams check bypasses the daemon.** `m7_check.py:345-356` loads
the handler by file and calls `.handle()` with a hand-made `{"session_id", "hook_event_name"}`.
That proves the handler's logic under the venv python, not that the daemon running in the
container has it loaded and enabled (`.claude/hooks-daemon.yaml:383-386`) or that the
status line renders it. Fix: drive the real daemon (send a `status_line` event for the
session ID through `bin/hooks-daemon`/its socket and read the segment), or at least assert in
the container that the yaml enables `BusTeamsHandler` and that `hooks-daemon health` lists it.

**F4 — LOW — ports from the ephemeral range.** `m7_check.py:132-143` binds `127.0.0.1:0`,
so the teams listen on 32768+ on the bus address; a connection grabbing the port between
`free-ports` and the homeserver start fails the run (not collateral). The bus's own range is
6167-6266 (`declare.py:127`), and `declare` already has a free-port finder that also avoids the
owner's declared ports. Fix: reuse it against the owner's file.

**F5 — LOW — a missing owner prerequisite is reported as FAIL.** `acceptance.bash:27` and
`:160-164`: no `.last-launch.conf` exits 1, which meta-deploy records as FAIL
(`meta-deploy.bash:221-236`) although it has the exit-2 "could not establish" verdict for
exactly this. Fix: exit 2 for `OWNER:` prerequisites.

**F6 — LOW — misleading teardown warning.** `:758-760`: with `TEAMS_PRESENT -ne 1` the branch
prints "the acceptance seats could not be parked" even when there were none. Fix: split the
condition; warn only when `m7_park_seats` itself failed.

**F7 — LOW — the `say` inference check depends on an unverified-record race it does not name.**
`:596-609`: `_sending_member` (`cli.py:835-847`) refuses inference while any team's record is
not yet cached. D3's `status-ready` poll (trust=trusted + ABOUT) makes it very likely cached,
but R1 fails with the generic "exited 64" message if not. Fix: on 64, print the stderr and say
the record was not yet verified, or poll R1 with status 3 the way D3 is polled.

**F8 — INFO — `expect_block` fixes the role regex to `[a-z ]+`.** `m7_check.py:286-290`: a
role with a digit or hyphen fails M1 for a formatting reason. Fine today; note it.

## Verdict

**MERGE AFTER FIXES.** The purge/apply/cleanup paths are tightly scoped and the restore holds
on every signalled or errored exit; the real collateral risk is F1 (a rerun after a hard kill
silently commits the pollution of the owner's Quick Launch record). F1 is required before the
host run; F3 is recommended so M3 vouches for the daemon rather than the module.
