## 10:34 · decision · —   — Plan made: team starts through the one launch summary

Plan made from the owner's request that team starts get the launch summary's "change one
thing" flow. Mapped from the code at F44 2d4aa6842 (RESEARCH-team-start-today.md): ten routes,
every prompt with file and line.

Findings that shaped the design:

- The summary (`ccy-teams launch-summary`, Plan 00201 Task 2.33) already holds team rows,
  held-seat and role-changed marks, and opens `settle` and `PickSeat` per line. It is called
  with `--skip-teams` whenever `--teams` is given (lib/launch-summary.bash:64), because
  `ccy_launch_teams` settles the teams before tmux (claude-yolo:886). That is why `ccy --teams`
  costs team list, seat, role, summary and the existing-containers menu: five answers.
- Plan 00223 (Not Started) designed `ccy-teams quick-launch`, a screen before tmux, before
  Task 2.33 existed. Building it now would be a second summary. Dedupe result: a new plan that
  extends the existing summary, with D1 asking the owner to supersede 00223's screen; 00223's
  valid requirements (held seat to the next free one, attach a detached holder, token usage
  line, Q3 skip, `--teams` filtering) are carried as tasks. 00177 and 00201 are at or near the
  PLAN.md size block, and neither owns team-start defaults; 00201 gets a pointer for Task 2.33's
  open items.
- The token-usage gate 00223 waited for is merged (`ccy-token-usable`, ccylaunch/usable.go:120).
- No model choice exists at launch today; Plan 00207 owns minimum models (D9).

Design chosen: keep the summary inside tmux where it runs (D2), not 00223's before-tmux
placement, which needed three hand-off decisions (C17, C21, C22); the cost is that the
detached-session picker is not seat-scoped for bare `--teams` (D3). Step counts: R1 `ccy --teams`
5 to 1, R2 4 to 1, R6 picker start 6 to 2, R9 add a team to a running session 4 to 2.
