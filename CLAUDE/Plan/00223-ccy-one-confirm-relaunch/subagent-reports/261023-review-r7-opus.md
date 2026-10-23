# Plan 00223 review, round 7 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at 858ef7c0d, against r1-r6 and the code at
the base (line numbers are the base's; code unchanged on the branch).

**Verdict: CHANGES** (1 blocking, 2 should-fix, 2 nits)

## Earlier items (r6)

| Item                         | State                  | Note                                                                                                                                                                                                                                                                                                                                          |
| ---------------------------- | ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1 no pty in difftest        | Fixed                  | Task 3.0 / C20; the harness facts hold (difftest/sandbox_test.go:505-511, `Stdin` string only at :92; `x/sys/unix` imported at fake_test.go:18)                                                                                                                                                                                               |
| S1 file changes mid-screen   | Fixed                  | C17 stamp. `load_launch_config` (claude-yolo:529-593) never rewrites an accepted file, so a stamp taken after it is stable                                                                                                                                                                                                                    |
| S2 engine                    | Fixed                  | C19 `--engine "$CONTAINER_ENGINE"` (claude-yolo:696-697)                                                                                                                                                                                                                                                                                      |
| S3 `ask` unasked line        | Fixed                  | C18                                                                                                                                                                                                                                                                                                                                           |
| Nits (attach 3, ranges, R12) | Fixed                  | lib/tmux-session.bash:390-394; 1280-1281/1283; Task 1.3 names the Docker read                                                                                                                                                                                                                                                                 |
| 892-899 moved (new)          | Holds, except B1 below | Nothing in 900-1195 reads `CCY_SEATS`, `TEAMS_LIST`, `CCY_ORIG_ARGS` or `"$@"` (checked). At the screen's place the arguments are unrewritten, so the parse's `TEAMS_FLAG_POS`/`TEAMS_VALUE_POS` (757-764, 679-681) are right and dropping the r5 898 change is correct. The only other `ccy_launch_teams` caller (1302) leaves with the join |

## Blocking

1. **`ccy --debug` (layer 1) loses its attended launch: the moved block and the screen now come
   after the debug chooser's stderr redirect.** Layer 1 runs
   `exec > >(tee ...) 2>&1` at claude-yolo:1090, so from there on stdout and stderr are pipes
   (lib/tmux-session.bash:363-364 already names `--debug` for this). Today the team step runs at
   892, before that, and Q1 reads stdin (a terminal) at 1278 with its prompt through the tee.
   Under the plan:
   - the moved block sits "just after the screen's place", before 1195 and so after 1016-1093
     (DESIGN 2.3, PLAN Task 3.2). If the attended test is evaluated there, `[ ! -t 2 ]` (875) is
     true: `ccy --debug --teams` or `--teams TEAM` is refused ("Choosing a team needs a
     terminal", 888); explicit seats go to `ccy_seats_check`'s unattended mode;
   - if it is kept where it is today (DESIGN 2.4 says only "computed once, before
     `ccy_launch_teams`"), the screen's guard passes but the screen refuses itself because stderr
     is not a terminal (R20, DESIGN:331-332), exit 64: the launch fails;
   - and C14's `[ ! -t 2 ]` at 1271 (DESIGN:264-265, 2.4:399) turns today's Q1 under `--debug`
     into `No terminal to ask on: using the previous configuration`, with the saved team dropped
     and no way to reconfigure.
     None of this is in R14's list of changed cases or in Task 3.1's scenarios. Fix: state where
     the attended value is computed (before 1090), and decide `--debug` explicitly: e.g. the
     screen and the moved block placed before the debug chooser with the exiting modes
     (1117-1182) excluded by their own flags, or the screen drawing on the terminal it was given
     and C14's clause written as "stderr not a terminal and not `DEBUG_CCY`"; or, as a recorded
     decision, `--debug` takes the unattended branch. Add a `--debug` scenario to Task 3.1.

## Should-fix

1. **Inner run with `--settled=launch|token` and no loadable file.** The stamp check sits after
   the inner run's own `load_launch_config` in the block (DESIGN:142-143). If another launch's
   load deleted the file meanwhile (546-548, 563, 572 `rm -f`), the `if` at 1243 is false and the
   run falls into the full questions under `--settled=launch`, not the stop C17 promises. Say
   that a missing or refused file under `--settled=launch|token` is a stamp difference and stops
   with the same words.
2. **The team step now runs after flag validation and the debug prompt.** Moving 892-899 below
   945-1093 means a seat or team typo is reported only after `claude --help` may have run a
   container (962-963), and the comment at 857-868 ("before any prompt, SSH or token work")
   becomes false. Update that comment in Task 3.2 and name the order change in the journal
   alongside the exiting modes.

## Nits

- The moved block's "no `run` answer" guard (DESIGN:340-341, 2.4:405, estimate) is not needed:
  `run` execs, `attach` and `quit` exit, so the block can follow the `case` unguarded, saving
  lines for C15's margin.
- `load_launch_config` now runs in the outer run too, so the "DEVELOPER ERROR ... without
  version bump" box (579-591) prints twice (outer terminal and pane). Accept and say so, or
  silence the screen-place call's box.

## Checked and holding

- 1195-1201, 1208-1223, 1237-1343, 3449-3463, 3461, 3776, 3876, 1847, 1859 citations.
- Allowlist row 55 is `2782 12 2713`.
- Registry classes: `--settled=*` beside `--rebuild=*` (lib/session-registry.bash:134-135),
  `--settled` into `CCY_REGISTRY_DROP_FLAGS` (123-125).
- `ccy_teams_canonical_args` (lib/agent-bus-seats.bash:96-112) with unrewritten positions.
- C14 inner run: bare `--teams` with `2>file` is refused in the outer run as today, so the
  inner-run screen never meets an unresolved bare `--teams`.
- C1-C20 taken as choices; only C14 interacts with B1.
