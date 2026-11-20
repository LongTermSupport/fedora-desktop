## 09:27 · decision · —

Q52 (SPEC-team-close "Decisions taken here", D1-D3) answered by the owner: "all as recommended".

- D1: the team row's two lines are named by what they do. "Shut down every session of <team>"
  is "Close every session of <team> (compact, then exit)"; "Wind down the team's sessions" is
  "Compact every session of <team> (stays open)". Their confirmations' crumbs are
  `<team> › Close` and `<team> › Compact`. The CLI verbs (`ccy-teams shutdown`,
  `ccy-teams wind-down`) keep their names: the owner asked for the row names, and scripts call
  the verbs.
- D2: a busy box is never given Escape, host and LXC alike (this changes the host close too:
  sessclose C4, Plan 00201 SPEC-shutdown SD10). The close waits for the box to be ready and
  empty up to the prompt bound, now 300 s (was 30 s after the Escape). 300 s is the bound the
  team wind-down (Compact every session) already gives a busy session before it types (its
  Idle wait), so both team actions wait the same; it is long enough for most turns to end on
  their own, and short enough that a stuck box is still reported within minutes. Busy past
  the bound keeps its outcome: a host seat is stopped (C8), an LXC seat fails (TC9). The
  wind-down path already never sent Escape (W2). tmuxccy.SendEscape had no caller left and is
  removed.
- D3: an LXC seat that will not end is left running and failed, never killed: confirmed, no
  code.
