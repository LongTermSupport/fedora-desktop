# Plan 00223 review, round 5 (Opus)

Reviewed: `git diff c15620d06..HEAD -- CLAUDE/Plan/` at 122702e32 (PLAN.md, DESIGN.md, JOURNAL,
README row), against the r1-r4 reports and the code at the base (unchanged on this branch).
Line numbers are the base's.

**Verdict: CHANGES** (2 blocking, 4 should-fix)

## Earlier items

| Item                    | State  | Note                                                                                                                                                                                                              |
| ----------------------- | ------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R4 B1 (flags vs saved)  | Fixed  | only `--settled` and `--teams` cross the exec; the yes body 1293-1295 still applies the saved token, keys and network (`NETWORK_FROM_CONFIG`)                                                                     |
| R4 S2 (registry class)  | Fixed  | `--settled=*` beside `--rebuild=*` (session-registry.bash:135), `--settled` in DROP_FLAGS (123); the test's derivation (test-ccy-session-registry.bash:166) finds `arg" == "--settled` and the stale check passes |
| R4 S3 (`--no-bus`)      | Partly | fixed for `launch` and `token`; open for `ask` (S1 below)                                                                                                                                                         |
| R4 S4 (state set later) | Fixed  | 875-876's test; 1201 reads only TMUX/TMUX_PANE/pids (tmux-session.bash:474-480), safe to move above 1195                                                                                                          |
| R4 S5 (budget)          | Fixed  | banner kept; the fallback is C15 (shrink, else stop), consistent with R14/R20                                                                                                                                     |
| R4 nits, R1-R3 items    | Hold   | token modes in the 892 test; deferred calls keyed on "no `run`"; refused attach ends; sentinel constant; missing key shown                                                                                        |

Verified and holding: the "Leaves" count of 25 (1277-1279, 1282, 1286-1287, 1318-1332, 1300-1305) is right; the restart filter (restart-request.bash:264) uses the same classes; insulation tests stdin and stdout only (tmux-session.bash:360-365), so C14's inner-run screen is reached; nothing between 892 and 1195 reads `CCY_SEATS` or `TEAMS_LIST` (C15's shrink move is sound); allowlist row 55 is `2782 12 2713`.

## Blocking

1. **Token modes reach the Quick Launch block attended, and the design leaves them a silent path.**
   `--create-token` and `--update-token` exit at claude-yolo:1470-1483, *after* the block
   (1237-1343), and set none of the 1237-1239 flags (811-821). Today an attended
   `ccy --create-token` with a saved file asks Q1 (Y: saved keys and network for the GH setup;
   n: the key menus). Under the design the screen never runs for them (the 1195 guard, DESIGN
   2.1), they carry no `--settled`, and the 1268-1273 chain (plus C14's `[ ! -t 2 ]`) has no
   branch for them, so `quick_unasked` is empty. With the read and the `case` removed (DESIGN
   2.4 "Leaves"), the yes body runs with no question and, because 1280 tests `quick_unasked`, no
   "using the previous configuration" line either. That contradicts success criterion 4
   ("token-mode launches are unchanged") and the Task 3.1 scenario "`--create-token` and
   `--update-token` (no screen, no deferral)". The design needs to state the invariant: every
   attended path that reaches the block now has `--settled` or an unasked reason. Then it needs to
   choose what token modes get. One option is an unasked reason in the chain (`Token mode`, an
   in-place clause on 1270). Another is joining the 1237 test, so the key menu is asked. Either way
   it is a stated change with its golden explained.

2. **The answer's `--teams` removal uses parse positions that are stale after 892 rewrote the
   arguments.** DESIGN 2.1 / Task 2.5 build the new list from "the `--teams` flag and value
   positions the bash recorded while parsing" applied to the launcher's arguments. For an
   undeferred `--teams=SEAT@TEAM` (one word, so `TEAMS_FLAG_POS` = `TEAMS_VALUE_POS` = k,
   claude-yolo:757-764), 892-898 has already replaced that word with two words, `--teams LIST`
   (agent-bus-seats.bash:104-105, `set --` at 897). The screen runs on this path (2.3: "taken as
   given at 892 and shown on the screen") and removes only word k. The canonical list is left
   behind as a stray word, which the inner run treats as claude's first message. Fix: after 892
   the `--teams` is canonical at k and k+1. So pass `k` and `k+1` (or have the bash set
   `TEAMS_VALUE_POS` after the rewrite), and add a `--teams=SEAT@TEAM` difftest scenario to Task 3.1.

## Should-fix

1. **`--settled=ask` with `--no-bus`/`--opencode` loses the saved team, and R18 offers a team
   chooser there.** `ask` skips the block (DESIGN 2.1), so 1272 never sets `quick_kept_team`,
   and 3461 writes `LAST_TEAMS=""`. Today `n` keeps it, because the chain runs before the `case`.
   R18 also runs the team chooser "when the saved settings had teams". Under `--no-bus` the answer
   may never carry `--teams` (R16, 869). State that under `--no-bus` R18 asks no team question, and
   that `ask` keeps the saved team. For example, `--settled=ask` joins the 1237 test only after the
   chain has run, or the 3461 fallback covers it. Add the case to Task 3.1.
2. **The case given for `--session NAME` is wrong.** DESIGN 2.1 and R11 say a `ccy` started by
   hand in a ccy pane has a session name. `ccy_tmux_current_session` (tmux-session.bash:468-480,
   and its comment) returns nothing for exactly that case: only the pane's own first process or
   its parent gets a name. Before 1195 the name is non-empty only in an inner run that shows the
   screen, which is C14's. The mechanism still works for C14. Correct the text, and make Task 3.1's
   C14 scenario assert the `record-seats` call.
3. **The `Attach` row is offered where attach cannot work.** Inside tmux (the C14 inner run, or
   a `ccy` run by hand in any tmux pane), `ccy_tmux_attach`'s `attach-session`
   (tmux-session.bash:124) is a nested attach, and tmux refuses it. Today P0 never runs there,
   because insulation returns at `TMUX` set (336-340). Offer `Attach` only when `TMUX` is empty, and
   say so in R4.
4. **R19's retry leaves the saved file naming the lost seat.** `.last-launch.conf` is written at
   3449-3463, before the take at 3876. A seat moved by the retry is therefore saved as the old
   (held) seat, and the next launch's screen offers a move again. Say whether that is accepted, or
   rewrite `LAST_TEAMS` in the retry. Separately, the retry's "3 lines" (DESIGN 2.4) cannot hold
   the screen call, the canonical-args rewrite (mapfile, wait, assignment: 892-899's shape),
   `record-seats` and the second take. Together with the deferral test that repeats 875-876 and
   1237-1239, "New about 24" is low. C15 covers growth, but computing the attended and
   choice-flag tests once, in a variable that 892, the screen's guard and 1237 all read, would
   make the budget plausible.

## Nits

- DESIGN 2.1: say that the screen-place `load_launch_config` call and its warning go to stderr
  (today they are inside the `{ } >&2` at 1242-1342).
- Q3 skip under C14 when not insulated: there is no `--settled`, so Q3 is still asked; consistent
  with today, but worth one word in R11.
