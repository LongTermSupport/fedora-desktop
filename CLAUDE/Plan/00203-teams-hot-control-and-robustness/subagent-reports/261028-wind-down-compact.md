# Task 3.20: wind-down compacts each session (build report)

Branch `plan/00203-wind-down-compact`, built in its worktree. Committed, not pushed or merged.

## What changed

- **Spec first.** SPEC-team-wind-down.md section 7 is revised for the owner's direction of
  2026-10-28, quoted there: "maybe for now we do need to allow typing into sessions because
  blocking on hooks daemon will slow us down a lot".
  - WC1: every kind 3.14 reaches is typed `/compact`.
  - WC2: the answer, the floor's one home, the faults.
  - WC3: the LXC choice, below.
  - WC4: one at a time, the coordinator by priority.
  - WC4a: the window, new.
  - WC8 and WC9: the supervisor route is a later follow-up, and nothing is filed upstream.
  - PLAN.md Task 3.20 matches, and is 🔄 with three unticked items: host acceptance, the
    follow-up WC5 `shutdown --compact`, and the follow-up WC8.
  - Journal entries: one `decision`, then the red and the green as `action`.
- **`src-go/internal/compactfloor`** (new) is the floor's single home in code. It holds
  `Parse`/`Floor`, moved out of `sessions/app.go` (which `set-going` now uses), the names
  `Env` and `Unit`, and `FromUnit`. `FromUnit` parses the play-written value out of the
  set-going unit's environment. It asks for that environment through the new
  `systemd.Systemd.UserUnitEnvironment` (`systemctl --user show --property=Environment --value`),
  because the project rule `project_seam_commands` gives `systemctl` to `internal/systemd`. So
  `set-going` and `wind-down` cannot disagree. No playbook change.
- **`winddown`**:
  - The new `compact.go` holds the prepare step, the answer and compaction steps, the window
    and `compactText`.
  - `windstep.go` gains the phases answer, queued, compact-wait and compact, and the scheduler
    `nextCompaction`.
  - `winddown.go` gains the fields (Floor, Registry, Getenv, Transcripts, `Waits.Window`), and
    the help, preview and report lines. The report adds a count line: `<team>: N compacted, M not compacted.`
- usage.txt and docs/agent-bus.md now describe the compaction.

## How it behaves

1. The wind-down instruction goes to every session together, as before.
2. A session has answered when it is idle and empty again: either after a busy look, or after
   the settle bound (30 s) with no busy look caught.
3. A host session's context is then read from its transcript: the one its SessionStart hook
   recorded, through `conversation.For`, never the newest.
   - At or above the floor, it is queued.
   - Below the floor, the report says "not compacted: its context, N tokens, is below the
     floor, F tokens", with exit 0.
4. A queued session gets the single turn when it is free: the coordinator first if it has
   answered, else the first in order. A coordinator still answering holds up no one. With the
   turn, the session is typed `/compact <fixed instruction>` at an idle moment. The typing is
   3.14's: the submit pause, one poll of grace, one more Enter.
5. A host session's compaction is done at its transcript's `compact_boundary` after the
   moment of typing. If it is idle again with no boundary, it fails (exit 1).
6. A 15-minute window (TW1's default) bounds everything. A session still answering, waiting
   for its turn, or compacting at the end of the window is named, exit 0, never interrupted.
   When 3.16 lands, its window replaces this one.
7. Faults are said and exit 1, and the wind-down text still goes in. The faults are:
   - a floor that cannot be read or is not tokens,
   - no recorded conversation or transcript,
   - a transcript that cannot be read,
   - a `/compact` stuck in the box.

**LXC choice.** An LXC member's transcript is in its container, under the member user's
home, which the host reads only as root through its rootfs. ccy-teams does not read it. So an
LXC member is compacted whatever its size, and its compaction's end is read from its pane:
busy after `/compact`, then idle again (or idle for the settle bound), bounded by the window.
The report says so: "compacted (its transcript is in its container, so its context is not
read; the end was read from its pane)". The floor is read only when there is a host session.

**WC5** (`ccy-teams shutdown --compact`) did not come cheaply: shutdown is a different flow
with no answer phase. It is left as an unticked follow-up.

## Gate

- `golangci-lint fmt` and `golangci-lint run ./internal/teams/... ./internal/compactfloor/... ./internal/sessions/...`: 0 issues.
- `go test -race` over the same packages: pass.
- Coverage: winddown 93.7%, compactfloor 100%.
- `./scripts/qa-changed.bash` passed over 22 changed files. It ran the docs gate, go-qa (both
  modules), and the ccy-teams-command, ccy-sessions-reboot, take-over, tmux-targets,
  restore-askpass, container-name, run-bash-consumer and qa-changed suites. Its first run
  failed on `project_seam_commands`: `compactfloor` was running `systemctl` itself, which the
  rule gives to `internal/systemd`. The call moved there, tests first.
- qa-all was not run (the coordinator runs it).

## For the coordinator

- The host acceptance needs the host agent: a team wound down on the host, each session
  compacted (or said to be below the floor), then its resume typed `continue` (WC6). Nothing
  needs deploying beyond the usual ccy-teams and ccy-sessions build in play-claude-yolo.yml.
- `wind-down` now waits for the answers, up to 15 minutes, before it returns. Before this, it
  returned once the text was typed. That is the feature, but the owner will notice it in the
  menu.
