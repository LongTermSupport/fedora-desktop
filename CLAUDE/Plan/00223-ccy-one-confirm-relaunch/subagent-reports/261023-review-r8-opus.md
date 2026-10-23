# Plan 00223 review, round 8 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at cd7c5b7b8, against r1-r7 and the code
at the base (code unchanged on the branch; line numbers are the base's).

**Verdict: CHANGES** (1 blocking, 2 should-fix, 2 nits)

## Earlier items (r7)

| Item                                    | State                        | Note                                                                                                                                                                                                                                                  |
| --------------------------------------- | ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1 `--debug` layer 1                    | Fixed for the outer run only | C21 holds where the screen runs: `CCY_RESTART_RELAUNCH_SESSION` is set at claude-yolo:463 and `CCY_SESSION_RESTORE` unset only at 1217, so a test at 870 sees both; 1090 is after it. It does not hold for the run that applies the answer (B1 below) |
| S1 missing file under `--settled`       | Fixed                        | DESIGN:160-163. The deletions are claude-yolo:547, 563, 572                                                                                                                                                                                           |
| S2 order and the 857-868 comment        | Fixed                        | Task 3.2 and DESIGN 2.3:366-368                                                                                                                                                                                                                       |
| Nits (guard dropped, box printed twice) | Fixed                        | DESIGN 2.3:363-365, 2.1:101-103                                                                                                                                                                                                                       |

## Blocking

1. **Under `--debug` the `run` exec parses `--debug` again, so the launch is asked the debug
   chooser a second time and the run that applies the answer is not attended.** The answer is
   "the launcher's arguments ... Nothing else is added" except removing the given `--teams` words
   (DESIGN 2.1:125-129). `--debug` therefore stays in the list, and the exec'd run sets
   `SHOW_DEBUG_CHOOSER=true` again (claude-yolo:802-803). It asks `Enter layer numbers` and
   `Continue with this configuration? [Y/n]` again (1036, 1074), because stdin is still a
   terminal (1016, `ccy_nobody_to_ask`, lib/common-pure.bash:114-116).
   - Layer 1: today the chooser is asked once, because 1090 makes stdout a pipe and there is no
     insulation (lib/tmux-session.bash:363-365). Under the plan it is asked twice. The second
     run's 1090 starts a second `tee`, writing into the first, and a second debug log (1084).
     The exec'd run also inherits fd 2 as the first `tee` pipe and works out `CCY_ATTENDED`
     again at 870 (`[ ! -t 2 ]` is true), so it is **false** there. That contradicts DESIGN
     R14:286-287 ("Under `--debug` the variable was computed before 1090, so a launch on a
     terminal stays attended") and C21 (PLAN:232). In that run 1271 (now `CCY_ATTENDED != true`)
     sets `quick_unasked="No terminal to ask on"`. Under `--settled=ask` this prints `No terminal to ask on: using the previous configuration...` (1281) before the `n` words, which C18
     (DESIGN:175-178) says is never printed. The moved block also runs in unattended mode
     (`--resolved`, lib/agent-bus-seats.bash:49-51). With canonical seats that does no harm, but
     it is not the attended launch C21 promises.
   - Layers 2 and 3 only (insulated): today the chooser is asked twice, once outside and once in
     the tmux run, because insulation re-runs `"$@"` (claude-yolo:1196). Under the plan it is
     asked three times.
     Task 3.1's `--debug` scenario ("the launch attended, C21", PLAN:157-158) cannot pass as
     designed, and the change adds questions, against the owner's aim. It is also not in R14's
     list of changes. The fix needs a decision. Options: the answer drops `--debug` and the
     exec'd run gets the chosen layers and the attended value some other way (this costs lines,
     C15); or the exec'd run skips the chooser and trusts an inherited `CCY_ATTENDED` when
     `--settled` is present; or `--debug` launches show no screen. The last option breaks R14's
     invariant (DESIGN:289-298). The attended read is gone, so such a launch would need its own
     unasked reason, or it would quick-launch with no question. Add the second chooser to Task
     3.1's `--debug` scenario as a check that must not happen.

## Should-fix

1. **Redirection order for `{CCY_TTY_FD}>&2`.** DESIGN 2.1:109-110 says only "in the same
   `exec`". Bash applies redirections left to right. If `{CCY_TTY_FD}>&2` comes after `2>&1`
   at claude-yolo:1090, it copies the `tee` pipe, not the terminal, and the screen and the 878
   chooser fail R20's terminal test (exit 64). State that it goes before `> >(tee ...)`.
2. **"the `else` of the 1243 `if`" does not exist.** DESIGN:162 names it, but 1243's `if` is
   followed by `elif [ "$HEADLESS_MODE" = true ]` (1336) and has no `else`. The missing-file
   stop under `--settled=launch|token` must be a new `elif` before 1336, because a headless
   launch never carries `--settled`. Say so, so that Task 3.2 does not add an `else` that also
   catches the headless refusal's path.

## Nits

- The migration prompt (claude-yolo:471-491) is asked again on the `run` exec when it was
  answered `n`, as the tmux re-exec already does today. The plan should say this is accepted,
  or else name it.
- Moving 875-876 to top level needs `CCY_ATTENDED=false` set first, and `ccy_launch_teams`
  needs `attended` dropped from its `local` and replaced by `$CCY_ATTENDED` at 877 and 882.
  Folding the initial value into 870 (`CCY_SEATS="" CCY_ATTENDED=false`) keeps "no line added"
  true (DESIGN 2.4:427).

## Checked and holding

- 857-900, 1016-1093, 1184-1223, 1237-1343 against DESIGN 2.1-2.4, including the 1268-1273
  chain, the 1293-1295 yes body and the 1300-1305 join.
- `ccy_seats_check`'s `--resolved` mode (lib/agent-bus-seats.bash:45-63) accepts the canonical
  list in the exec'd run.
- Insulation tests stdin and stdout only (lib/tmux-session.bash:360-366). The attach exit codes
  0 and 3 are at 391-397. `TMUX` on the ccy server is a no-op (336-345).
- C1-C21 are taken as choices. Only C21's claim about the exec'd run is challenged (B1).
