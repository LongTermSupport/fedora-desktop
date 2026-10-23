# Board fixes: unavailable actions offered, keys echoed between screens

Worker report for the coordinator, team desktop. Plans 00177 (Tasks 10.1, 10.2; 10.3 is the host
retest) and 00201 (the seat-chooser scaffold).

## Branch

`agent-aaf94dbcca6e93463-fb2e7585`, two commits on `b1e298e1c` (the tip is given in the reply to
the coordinator). Not pushed, merged or tagged. Base check: `e8f8973ea` is not an ancestor.

## Defect 1: a menu offers what the board calls unavailable

Defence Before Fix (skill 1.1.0). Class: an action line drawn from a different decision than the
board's refusal for the same action. Judged **cannot be a rule** (the two strings come from
separate computations and only their meaning has to match); recorded in
`CLAUDE/GoProjectRules.md`, "Defect classes that cannot be a rule". Independent search:
`subagent-reports/261023-search-unavailable-offered.md`.

Fix: the frame's `newRefusal` is the one decision behind both the header ("Ctrl-N unavailable:
…") and the menus' start line; an unavailable line stays in the menu in the house form
`<label>: unavailable, <why>` (as the ccy-sessions new-session row) and choosing it shows the
refusal. Instances fixed: Start in team/person/member menus outside a project and in an LXC
checkout; Start on a team whose homeserver is not up; Attach on a seat with no checkout, no
running session, not in ccy's tmux, or open in another terminal; Attach in Running sessions.
The seat refusal now reads "homeserver is not up" (SPEC-ccy-teams.md and
`scripts/test-ccy-teams-command.bash` updated with it).

## Defect 2: keys typed between two screens echo as ^\[\[B

Class: an interactive command that leaves the terminal in cooked, echoing mode between screens.
Judged **cannot be a rule** (recorded beside the first, with why the two narrower rules were not
built). Independent search: `subagent-reports/261023-search-echo-between-screens.md`.

Fix: new `src-go/internal/ttyecho`. The hold is taken at the first screen (`execx.Cmd.EchoHeld`,
set by `internal/ui` on every fzf, gum and tput run), so a run with no screen never touches the
terminal; keys typed before the first screen still echo (documented). A child given the terminal
runs in the mode found; a line read in process goes through `Given` with the echo on; the exec
and SIGTERM/SIGHUP (unless ignored) put the mode back; `Held` puts it back at the end, a panic
included, and a mode that cannot be put back is said with "run reset". Wired into ccy-teams,
ccy-sessions, podfreeze, lxcfreeze, open and ftp-camera. A real pseudo-terminal test proves the
echo with and without the hold. Unused `ui.Gum.Spin` deleted. CCY bumped to 3.100.17 (changelog
notes 3.100.16 belongs to a concurrent branch).

## Scaffold (Plan 00201)

`chooser-teams-up.bash` sets up two throwaway teams (`chooser201a`, `chooser201b`) like
`acceptance.bash` minus the headless launch, leaves them up, and prints the
`ccy --teams` command (with the run's own `CCY_TEAMS_HOST_VARS`) for the host agent to drive the
seat chooser (Keep as is / Make coordinator) in tmux. `chooser-teams-down.bash` purges them,
restores the launch config and checks their firewalld rules are gone, however it ends. Both
refuse when the owner's host_vars declare a team of the same name; neither touches the real
teams or host_vars. Shared legs in `_chooser-teams.inc.bash` and `_acceptance-m7.inc.bash`.

## Review

Adversarial Opus review, no worktree isolation: round 1 CHANGES (12 findings, all addressed:
eager hold replaced by a lazy one, panic-safe release, prune read errors surfaced, remaining tools
wired, etc.), `261023-board-fixes-review-r1.md`; round 2 MERGE with six minors, all addressed,
`261023-board-fixes-review-r2.md`. Journalled through `mkplan.bash --journal`.

## QA

`./scripts/qa-changed.bash` passed before each commit. One earlier run failed only on
`test-wsi-stop-grace.bash` under load (load average about 51); it passed when rerun alone. The
DBF conformance-reviewer step was not run; the two classes are "cannot be a rule", so there is
no detector to reproduce red and green.

## meta-deploy entries to queue (not edited, per the brief)

- `playbooks/imports/play-claude-yolo.yml`: ccy-teams, ccy-sessions, CCY 3.100.17
- `playbooks/imports/play-podfreeze.yml` and `play-lxcfreeze.yml`
- `playbooks/imports/play-ftp-camera.yml`
- `playbooks/imports/play-cli-tools.yml` (open; already queued)

Then for the host agent: `chooser-teams-up.bash`, the seat-chooser drive, `chooser-teams-down.bash`,
and the Plan 00177 Task 10.3 retest (no echo after Esc; no unavailable action offered outside a
project).
