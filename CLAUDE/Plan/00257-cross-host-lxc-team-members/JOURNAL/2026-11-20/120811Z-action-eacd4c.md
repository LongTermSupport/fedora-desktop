## 12:08 · action · —

Q52 review fixes (subagent-reports/q52-review.md, should-fix 1-3 and the nits), in commits 03bedb9da, 0696f515d and 01597b4fe.

- **Two bounds.** sessclose now has two bounds. `Waits.Prompt` is 30 s again, for a pane never seen in a turn: starting, at a dialog, or hung. `Waits.Turn` is 300 s, and the first Busy look extends the deadline to it, counted from the start of the wait. The deadline is never cut back, so a pane that is Busy and then NotDrawn keeps 300 s, because a dialog in a turn is part of that turn.
- **The timeout notes** name the case:
  - host: "a turn was still running after 300 s" or "its prompt was not ready within 30 s";
  - LXC: "busy past the bound" or "no prompt".
- **Where the 300 s wait is shown.** `sessclose.TurnWait` says it in three places:
  - `ccy --stop`;
  - ccy-sessions' Close question and progress line, `--no-compact` included;
  - the shutdown preview.
- **Specs.** SPEC-team-close TC1, TC8, TC9, TC12 and D2, and Plan 00201 SPEC-shutdown SD10 and SD12, state the two bounds.
- **Versions.** CCY 3.122.46 carries the changelog entry giving the full reach. 3.122.47 is the follow-up: `project_screen_state_compared` caught a `== screen.Busy` in the new note code, now a switch over every state.
- **Nits.** The help names the compact floor. The v2_test comment, the agent-bus.md bold lead and the TC1 wrap are fixed.

Report: subagent-reports/team-winddown-fix-report.md, section "Q52 review fixes".
