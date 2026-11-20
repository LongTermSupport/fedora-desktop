## 17:18 · action · —

Fixed the two wind-down triage items that also serve v2 and ccy-sessions (commit 68e3b603f, CCY
3.122.49, container unchanged); report in subagent-reports/winddown-v1-fix-report.md.

- tmuxccy.Submit takes a pane out of copy or view mode before typing. Any other mode, or a mode
  entered while the line is typed, is ErrPaneInMode, with no Enter pressed. Red first: in copy
  mode the old Submit returned success for a line that never reached the box.
- The picker no longer prints a container-checkout refusal when ccy-teams opens in a container's
  checkout. The refusal stays in the list header and on a row's Start line. Red first.
- Items 1 and 2 (an LXC member without service: true counted as failed; N@team labels) are
  v1-only: left for the v1 retirement at the owner's direction. The unfinished work was backed
  out and kept untracked under untracked/scratch/.
