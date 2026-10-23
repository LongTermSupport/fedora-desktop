# Plan 00223 review, round 3 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at ea012722a (PLAN.md, DESIGN.md,
JOURNAL, README row), against the round-1 and round-2 reports and the code at the base. Line
numbers are the base's.

**Verdict: CHANGES**

## Earlier items

| Item          | State                 | Note                                                                                                                                                                                                                       |
| ------------- | --------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R1 B1         | Fixed, but new defect | DESIGN 2.3 is now buildable; the deferral breaks the tmux offer at 1196 (B1 below)                                                                                                                                         |
| R1 B2, B3     | Fixed                 | 2.4 lists moves/stays (one gap, S3); `SeatView` + Task 0.3                                                                                                                                                                 |
| R1 S4-S12     | Fixed                 | as round 2 recorded                                                                                                                                                                                                        |
| R2 S1         | Fixed                 | no saved file read at 892; the refused/missing-file fallback is named (2.3)                                                                                                                                                |
| R2 S2         | Fixed, but new defect | the whole block 1788-1848 moves; it reads only `CONTAINER_ENGINE`, `HEADLESS_MODE`, `CCY_UNATTENDED_LAUNCH` (checked in docker-health), and `ccy_container_menu_exit` (1837) is still defined before 1860. Token modes: S4 |
| R2 S3         | Fixed                 | stdout `-`; `TEAM_NAME_PATTERN = r"[a-z]..."` (helpers/pingbus/protocol.py:87) holds; 880 and `ccy_teams_canonical_args`'s `${3:?}` (lib/agent-bus-seats.bash:99) are named                                                |
| R2 nits (all) | Fixed                 | run-for stated in R19; function count 12; own session left out; Go filters; C11                                                                                                                                            |

## Blocking

1. **The deferral breaks the seat-scoped tmux offer at 1196.** `ccy_tmux_insulate` reads the
   `--teams` value from the argv (lib/tmux-session.bash:376-380, comment: "By here the launcher
   has replaced the list with its canonical form, every seat named") and `ccy_tmux_offer`
   offers only detached sessions that hold one of those seats (209-212, 219-224; Plan 00177
   Task 4.6: "Enter never drops --teams into a session that is in other seats or none"). Under
   DESIGN 2.3 the argv still holds what was given at 1196:
   - bare `--teams` as last word: `want` is empty, so **every** detached session here is
     offered, including ones in other seats or none, and the person gets an fzf prompt before
     the screen (one more confirm, against R1);
   - `--teams alpha`: `ccy_tmux_seats_shared "alpha" "2@alpha"` (181-189) never matches, so no
     detached session of that team is ever offered (today a seat chosen first can reattach
     the detached session holding it);
   - bare `--teams` followed by a flag (the launcher allows it, claude-yolo:670):
     `ccy_tmux_teams_arg` (161-177) returns that flag as the list, and the registry's flat
     parser keeps it as `--teams`'s value (lib/session-registry.bash:104-105, 127) in the
     record until `record-seats` rewrites it.
     The design does not mention the tmux offer at all. It must say what the offer gets on a
     deferred launch (for example the named teams, matched by team, or nothing with a stated
     reason), and where that change goes: `lib/tmux-session.bash` is on the ratchet (allowlist
     row 66, `ui`), so it may not grow.

## Should-fix

2. **DESIGN section 1 leaves out a question asked before Q1.** For plain `ccy`, `want` is empty, so
   whenever this project has a detached tmux session, a teammate's included,
   `ccy_tmux_offer` shows the "a session is detached here" picker (lib/tmux-session.bash:245-248,
   from claude-yolo:1196) before Quick Launch. Success criterion 1 ("one Enter, with a teammate
   running in the same repo too") is not met when the teammate's session is detached. Add it to
   the inventory and decide what happens to it (fold it into the screen's Running line with an
   attach row, scope it to the saved seats, or keep it with the reason), as a C decision with a
   default.

3. **The saved-team join for `TEAMS_GIVEN=false` is in neither list.** Lines 1296-1305
   (`TEAMS_LIST` from the saved seats, `ccy_launch_teams`, `CCY_ORIG_ARGS+=(--teams ...)`,
   `record-seats`) fall inside the "moves to Go" range 1277-1332 (DESIGN 2.4) and are not in
   "what stays". 2.3's deferred step covers only `TEAMS_GIVEN=true`. Name this path as staying
   in bash, and include R18 with saved teams and no `--teams`: there `TEAMS_FLAG_POS` is 0, so
   `ccy_teams_canonical_args` (lib/agent-bus-seats.bash:104) replaces nothing and the list
   must be appended. Also say how the R19 retry finds the `--teams` words to rewrite a second
   time. After the first rewrite they are at `TEAMS_FLAG_POS` and `+1`, or at the end, not at
   the parse positions: bare `--teams` has flag == value position, and `--teams=x` becomes two
   words.

4. **The token modes now get the preflight and the screen.** `--create-token` and
   `--update-token` pass through 1229-1483 and exit at 1470-1483, before today's preflight
   (1788). R12's "before the Quick Launch block, for every launch" runs the root and rootless
   checks, the stale cleanup and **Q2** (a menu that stops containers) in `ccy --create-token`.
   And the 1237 flag test has no mode test, so the Go screen (seats, sessions,
   `Choose a seat...`) would replace Q1 in those modes as well. State what the token modes get
   (a guard, or accept it with the reason and a difftest scenario), within the no-growth rule.

## Nits (not blocking)

- lib/agent-bus-seats.bash is not on the allowlist and is at 57 of 60 code lines and 4 of 4
  functions. The empty-list change to `ccy_teams_canonical_args` must add no function and at most 3
  lines (it can be done in 0: `[ -z "$canonical" ] || printf ...`). Say so in Task 3.2.
- `--offer-none` with `--teams TEAM` goes through `resolve` (teams/chooser/chooser.go:179-201),
  which shows no team list, so there is no place for a `No team` row. Say which question carries it,
  or that it applies only to the list.
- `ccy_launch_teams` must know when it is the reconfigure call, to pass `--offer-none` at 878.
  On `-` it must return before 882 (seat check), not exit. Name the mechanism (an argument or a
  global) in Task 3.2.

Claims re-checked and holding: claude-yolo 670, 754-763, 869-900, 1195-1227, 1237-1343,
1788-1860, 3461, 3876; lib/agent-bus-seats.bash 96-112; lib/session-registry.bash 87-96, 127;
chooser.go 172-201; protocol.py 87; allowlist row 55 (`2782 12 2713`); the sibling's
`func (f Front) TokenUsability(ctx, file string, fetch bool)` (usable.go:116).
