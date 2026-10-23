# Plan 00223 review, round 6 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at 58ef2b6ab. I checked it against the
r1-r5 reports and against the code at the base. Line numbers are the base's.

**Verdict: CHANGES** (1 blocking, 3 should-fix, 3 nits)

## Earlier items (r5)

| Item                         | State | Note                                                                                                                                                                                                                                                                                                                        |
| ---------------------------- | ----- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1 token modes               | Fixed | C16 `elif` and the stated invariant (DESIGN R14). Checked case by case: the 1195 guard, 875-876, 1237-1239, 1269-1271 and the restart's `--token` (1235-1236) leave no attended path that reaches the block without `--settled` or a reason                                                                                 |
| B2 stale positions           | Fixed | `ccy_teams_canonical_args` always writes two words at flag-pos (lib/agent-bus-seats.bash:104-105), so `TEAMS_VALUE_POS=FLAG+1` at 898 is right for every undeferred form. The deferred form is untouched, and nothing else between 665 and 1195 runs `set --` (only 897)                                                    |
| S1 `ask` with `--no-bus`     | Fixed | the chain runs and 1272 sets `quick_kept_team`; 3461 writes it back                                                                                                                                                                                                                                                         |
| S2 `--session` case          | Fixed | `ccy_tmux_current_session` compares `$$`/`$PPID` with the pane pid (lib/tmux-session.bash:474-480). The `exec` keeps the pid, so the re-exec'd C14 run keeps its name                                                                                                                                                       |
| S3 Attach in tmux            | Fixed | the row is offered only when `TMUX` is empty (R4)                                                                                                                                                                                                                                                                           |
| S4 retry and stale file      | Fixed | the retry is dropped (C9). The saved file is written at 3449-3463, before the take at 3876, as R19 says                                                                                                                                                                                                                     |
| Hand-off on `run`            | Holds | `give` and then `CCY_GH_TOKEN_HANDOFF=... exec` mirror lib/tmux-session.bash:397. The new run takes the token at source time (lib/token-handoff.bash:90)                                                                                                                                                                    |
| Registry and restart classes | Holds | `--settled=*` goes beside `--rebuild=*` (lib/session-registry.bash:135) and `--settled` into DROP_FLAGS (123). `ccy_restart_relaunch_args` (lib/restart-request.bash:264-285) uses `ccy_registry_flag_class`. The tmux command line keeps `--settled`, because insulation passes `"$@"` (1196) and only the record drops it |

## Blocking

1. **Task 3.1's attended scenarios cannot run in the difftest harness as it is.** The harness
   starts the launcher through `execx` with stdin as a `strings.Reader` or nothing, and with
   stdout and stderr captured as pipes (src-go/internal/ccylaunch/difftest/sandbox_test.go:505-511).
   It has no pseudo-terminal: no scenario in `testdata/scenarios.json` sets `stdin`, and there
   is no pty code. Every path this plan changes needs a terminal:

   - the screen's call needs 875-876's `attended` (`! -t 0` via `ccy_nobody_to_ask`, lib/common-pure.bash:114-116, and `[ ! -t 2 ]`);
   - the 892 deferral uses the same test;
   - the C14 case needs stdin to be a terminal.

   Under the harness, "launch accepted", "seat held then next free", "held by a detached
   session then attach", "token used up then chooser", "teammates running (no Q3)", "bare
   `--teams` ... deferred calls before tmux", `Answer every question again`, "a 75 from `seat take`" and "stdin
   a terminal and stderr not" all take today's unattended branch (1271) and never call
   `ccy-teams quick-launch`. Their goldens would pin the unattended path and pass vacuously. Only
   the inner run can be driven without a terminal, by giving `--settled=...` as an argument;
   even then, 1271's `No terminal to ask on` fires first and `--settled` overrides it.

   Fix: add a task that gives the harness a pty mode (a scenario field for which fds are
   terminals; `x/sys/unix` is already imported in fake_test.go), test first, with
   `TestOracleCanFail` extended to a pty scenario. Alternatively, move the outer-run scenarios
   to a `script -q`-driven bash suite, as `scripts/test-ccy-token-mode.bash` and others already
   do. Task 4.4 (on the host) is not a substitute for the goldens that Success Criterion 4 and
   Task 3.2's "each difference explained" rest on.

## Should-fix

1. **What the screen shows can differ from what the inner run applies.** The outer run reads
   `.last-launch.conf` for the screen. The inner run reads the file again in the block (DESIGN
   2.1, "the inner run takes them from `.last-launch.conf`"). The screen is interactive and can
   stay open for any length of time. Meanwhile another launch in the same checkout (the
   owner's multi-session case) rewrites the file at 3449-3463. Enter would then apply the
   other launch's token, keys and network, not the ones shown (R1: "Enter launches with all of
   it"). Today Q1 is shown and applied from one load (1243-1295). Fix: carry the shown file's
   identity in the answer and have the inner run refuse on a mismatch, e.g.
   `--settled=launch:<SAVED stamp>` checked against the file it loads. The `Generated:` line at
   3454 has second resolution. Alternatively, accept the race and state it as a decision.
2. **The screen has no way to know the launch's engine.** R11 and R12 have the screen list
   containers through `internal/engine`. `engine.Select` takes the flag, else the variable, else
   podman (src-go/internal/engine/select.go:125-140). R16's argument list carries no engine.
   `ccy --engine docker` sets and exports `CONTAINER_ENGINE` (claude-yolo:696-697), not
   `CCY_CONTAINER_ENGINE` (lib/common.bash:33). The screen would therefore run `podman ps`, and
   R11's sibling list and its zombie line would come out wrong. Add
   `--engine "$CONTAINER_ENGINE"` to R16 and Task 2.6, with a Task 1.3 case.
3. **`--settled=ask` and the unasked message.** DESIGN 2.1 has `--settled` set `quick_unasked`
   after the chain, and the "New" estimate counts one line for it. If it is set for `ask` too,
   1280-1281 prints "...: using the previous configuration" and then skips the yes body, which
   is false. Say that the line is set only for `launch` and `token`, or that `ask` prints its own
   words. Pin the result in the `ask` golden.

## Nits

- DESIGN 2.4 lists 1282 (`use_config=unasked`) under "Leaves" and the unasked message
  "1280-1283" under "Stays". Write 1280-1281, 1283.
- `attach`: "exits with its status". `ccy_tmux_attach`'s 3 ("another terminal holds it after
  the attach") ends a successful launch with 0 today (lib/tmux-session.bash:390-393). Treat 3
  the same way.
- R12: "its engine call is a read-only `ps`". The screen reads containers before the rootless
  check (1788-1848), so on Docker it needs docker-group access. Today Q3 does the same read
  after that check, so the refusal wording on a Docker failure may differ. Name the case in
  Task 1.3.

## Checked and holding

- DESIGN section 1's line citations: 751-764, 869-899, 1195-1201, 1237-1343, 1470-1483, 1847,
  1859, 3449-3463 and 3876.
- `ccy_tmux_offer`'s seat scoping (lib/tmux-session.bash:209-224).
- Insulation tests stdin and stdout only (360-365).
- Allowlist row 55 is `2782 12 2713`.
- `--engine` is not a choice flag, so it does not suppress the screen. This is consistent with
  today's Q1.
- The decisions C1-C16 are taken as choices. Only C6 interacts with should-fix 1.
