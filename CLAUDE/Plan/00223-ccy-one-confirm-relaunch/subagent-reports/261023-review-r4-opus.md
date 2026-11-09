# Plan 00223 review, round 4 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at d65118ab1 (PLAN.md, DESIGN.md, JOURNAL,
README row), against the r1-r3 reports and the code at the base (unchanged on this branch).
Line numbers are the base's.

**Verdict: CHANGES**

## Earlier items

| Item                | State       | Note                                                                                                                   |
| ------------------- | ----------- | ---------------------------------------------------------------------------------------------------------------------- |
| R3 B1 (P0 at 1196)  | Fixed       | the screen's answer is canonical before insulation; a no-saved-file deferral runs the two calls before tmux            |
| R3 S2 (P0 missing)  | Fixed       | DESIGN 1.1 P0; C13 with default and reason                                                                             |
| R3 S3 (join, R19)   | Fixed       | join is in the answer; the inner run's `--teams LIST` is two words, so its own parse positions are right for the retry |
| R3 S4 (token modes) | Fixed       | screen inside the 1195 guard; Q2 left after the screen as C12, with the reason                                         |
| R3 nits             | Fixed       | `lib/agent-bus-seats.bash` unchanged; `No team` only on the list; no new `ccy_launch_teams` mode                       |
| R1, R2 items        | Still fixed | except that the redesign reopens "restore unchanged" (B1 below)                                                        |

## Blocking

1. **Turning saved settings into explicit flags changes what the inner run and every restore
   of that session do.** DESIGN 2.1 says `--settled` "changes nothing" for a restore or a
   restart because "unattended launches skip Q1". That is not what the code does. An unattended
   launch does not skip the Quick Launch block. The block is where it gets the saved token, keys
   and network (claude-yolo:1263-1295). And an explicit flag is not the same thing as a saved
   value:

   - **Network gone.** A saved network goes through `NETWORK_FROM_CONFIG=true` (1295). When the
     network has gone, that path offers to start compose and asks Q4, which a restore answers by
     itself (2581-2652). An explicit `--network NAME` is `print_error "Network not found"; exit 1`
     (2653-2659). So a person whose compose stack is down now gets a refused launch from the
     screen's yes. And every restore of such a session after a reboot fails, because the record
     replays `--network` (it is in the keep-value group, lib/session-registry.bash:127).
   - **`--ssh-agent` is dropped on replay** (lib/session-registry.bash:124, "the agent socket is
     a different path after a reboot"). A session launched from the screen with the agent key has
     a record of `--settled=launch --token T --network N`. On restore, the `--token` turns Quick
     Launch off (1237), so the key menu at 1357 waits with nobody there. Today that record has no
     choice flags, and the restore takes `LAST_SSH_KEYS`, sentinel included, through Quick Launch.
     The registry was built on exactly that premise: `_ccy_registry_args_with_ssh_key`
     (lib/session-registry.bash:396-426) says "An --ssh-key there would switch Quick Launch off
     and bring the token and network prompts back."
   - **`--settled=token` and `--settled=ask` records carry no `--token`.** The token chosen at
     `select_token` is in no argument. A restore replays `--settled` (unknown flags are kept,
     lib/session-registry.bash:221-224), which skips the whole block (DESIGN 2.1), so the restore
     stops at the token menu. Today the same restore takes the token that 3449-3458 saved.

   This contradicts R14, the PLAN goal "Unattended launches behave exactly as today" and success
   criterion 4. The design must decide how saved values cross the exec while keeping their
   "from saved config" meaning. For example: `--settled=launch` alone makes the inner run apply
   the saved file as today's yes does (1293-1295, `NETWORK_FROM_CONFIG=true`); explicit flags
   only for what the screen changed; and `--settled` classified `drop` in the registry, so a
   restore goes through today's unattended branch. Or something else, but stated, and Task 3.1
   given scenarios for: network gone (attended and restore), `--ssh-agent` restore, and a
   `--settled=token` restore.

## Should-fix

2. **`--settled` must be classified in the session registry, and the plan does not say so or
   choose the class.** `scripts/test-ccy-session-registry.bash:166,181-185` reads every
   `arg" == "--...` flag out of the launcher's loop and fails on any flag that
   `ccy_registry_flag_class` calls `unknown`. A `--settled=*` branch needs a pattern beside
   `--teams=*` (lib/session-registry.bash:137) and a group. The choice of group is the B1
   decision. `ccy_restart_relaunch_args` treats an unknown flag as claude's and keeps it
   (lib/restart-request.bash:287); state that this is wanted. Name lib/session-registry.bash
   (and its test) in Task 3.2. The Non-Goals list implies the libs are untouched.

3. **`--no-bus` and `--opencode` are not handled.** Today Quick Launch with `NO_BUS_MODE` does
   not take the saved team and keeps it in the file (claude-yolo:1272, `quick_kept_team`, written
   back at 3461). Under the design:

   - if the answer adds `--teams LIST`, the re-exec dies at `ccy_no_bus_check` (869;
     lib/agent-bus-membership.bash:33-35, exit 64);
   - if it does not, `--settled` skips the block, `quick_kept_team` is never set, and 3461 writes
     `LAST_TEAMS=""`, so the saved team is lost.

   `--opencode` sets `NO_BUS_MODE=true` (lib/agent-choice.bash:61). R16 must pass no-bus to the
   screen, the answer must omit `--teams`, and the inner run must keep the saved team (for
   example `--settled=launch` re-reads `LAST_TEAMS` into `quick_kept_team`). Add a difftest
   scenario.

4. **The screen's conditions name state that is not set yet at its place.** DESIGN 2.1 tests
   "`CCY_UNATTENDED_LAUNCH` false". That variable is computed at 1220-1223, after 1196, and
   `CCY_SESSION_RESTORE` is read and unset at 1208-1217. R11 leaves out `CCY_SESSION_NAME`,
   which is set at 1201. Before 1195 the test must be what `ccy_launch_teams` uses (875-876:
   `ccy_nobody_to_ask`, `-t 2`, `CCY_SESSION_RESTORE`, `CCY_RESTART_RELAUNCH_SESSION`), and the
   session name must come from `ccy_tmux_current_session` called there. Getting this wrong
   shows the screen in a restore's tmux pane, where stdin and stderr are terminals. Correct 2.1
   and R11.

5. **The no-growth budget is contradictory.**

   - DESIGN 2.4 "Leaves" includes the banner, 1244-1262. Those lines run for unattended
     launches too (restore, headless, end of input), so removing them changes those goldens.
     That breaks R14 and success criterion 4, which require them unchanged.
   - The fallback "more of the Quick Launch block moves to Go (the unattended branch's
     messages)" would mean calling `ccy-teams` on unattended launches. R20 and 2.1 say the screen
     is attended only and never called there.

   Keeping the banner leaves roughly 30 counted lines to remove. What is added is: the 892
   deferral test and a second copy of the 892-899 calls, the screen's argument building and
   call, its `case` and exec, the `--settled` branch with its check, two skips, the R19 retry,
   and the B1 fix. Give an estimate in 2.4 that shows this fits. Name a fallback that does not
   contradict R14 and R20, or make it a decision with a default.

## Nits (not blocking)

- 2.3: the deferred calls must also run when the screen is skipped for reasons other than "no
  saved file or refused". Those are `--create-token` and `--update-token` with `--teams TEAM`,
  and `stdout` not a terminal. Better: add "not a token mode" to the 892 deferral test.
- 2.1 `attach`: `ccy_tmux_insulate` re-execs the launcher when the attach is refused with
  anything but 0 or 3 (lib/tmux-session.bash:391-397). Say what the screen's `attach` does then.
- R16 passes `LAST_SSH_KEYS` raw. Say how the Go recognises `SSH_AGENT_SENTINEL`
  (`ssh-agent`, lib/ssh-handling.bash:295): pass it, or share a constant.
- A saved key that is now missing: `--ssh-key PATH` exits 1 at parse (claude-yolo:700-703). The
  screen should show it as missing rather than offer a launch that fails at once.

Checked and holding:

- The Go front decides only on `Args[0]` (front.go:283-313), so a leading `--settled=...`
  is handed over. `handOver` adds its env to the parent's (execx.go:28), so
  `CCY_GH_TOKEN_HANDOFF` reaches the bash.
- The token hand-off survives the extra exec: `take` runs at source, `give` runs again at
  insulate (lib/token-handoff.bash:96).
- `CCY_LAUNCHER_PATH` is re-derived after the exec (lib/launcher-path.bash).
- `load_launch_config` needs only `CONFIG_VERSION`, `CCY_VERSION` and `CCY_HASH` (17, 131,
  142), all set before 600.
- Allowlist row 55 is `2782 12 2713`.
- Plan 00146 Task 3.3 rewrites the record as well as the file, so a pinned `--token` does not
  conflict with it.
