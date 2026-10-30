# Plan 00177 Task 14.1: ccy-teams has no shortcut keys

The owner: "i dont want magic key comboes in ccy-teams - i want context menu items when i hit
return".

## What changed

- `src-go/internal/teams/picker`: the `action` type lost its `key` field and the key
  constants (Ctrl-N, -R, -X, -E, -P, -U, `?`) are gone; the Running sessions row lost Tab and
  the sessions list's help lost `?`. No ccy-teams list passes `--expect` any more; a row's
  menu hands back only Esc then an arrow (`alt-up,alt-down`, navigation). The footer is now
  `This row, Enter to: <what the menu offers>, ...`; the list's key line is "Enter lists what
  can be done to the row, help among it   Running sessions is the last row   Esc or q quit";
  the new-session header line reads "A new ccy session (Enter on a row) starts in ..." or "A
  new ccy session is unavailable: ...". Help is the menu line "What the columns and the menu
  lines mean".
- Dead plumbing removed: the two `ui.NotOfferedError` branches (no key can reach them),
  `nothingOnThisRow`, `footerPart`. An unknown action ID in `doTeam` is now an error (fail
  fast) instead of a "that key does nothing" note: every ID comes from a menu line.
- Messages that named keys now name menu lines: `seats` (attach refusal, restore notes),
  `login` (no Element profile), `hot/add.go` (team full).
- `help.txt` and `usage.txt` describe the menu lines; "There are no shortcut keys."
- The shared `ui` package keeps `Action.Key` (ccy-sessions still uses Ctrl-T/Ctrl-X/Ctrl-N).

## Tests

- Red first: `TestNoFormerShortcutKeyChoosesAnything` (sessions_test.go) failed on
  `--expect=tab,ctrl-e,ctrl-n,ctrl-u,?,ctrl-x,ctrl-r,ctrl-p`; green after the change. It walks
  list, menu, sessions list and checks no `--expect` but `alt-up,alt-down`, no `--bind` of a
  former key, no line with a key beside it.
- The fake fzf of `teamtest` (console.go) and of `scripts/test-ccy-teams-command.bash` now
  fail a test whose queued answer presses a key the screen's `--expect` does not name, so no
  test can drive a key the real fzf would not hand back.
- Every key route in keys_test, menu_test, picker_test, sessions_test, lxc_test, help_test and
  the bash suite is now Enter on the row then the menu line. Cases that tested "a key on a row
  whose menu lacks it" are gone (nothing to test).
- `go test ./internal/teams/... ./internal/sessions/... ./internal/ui/...` green;
  `scripts/test-ccy-teams-command.bash` all passed; `qa-changed.bash` green.

## Docs

`docs/agent-bus.md`, `docs/ccy.md` (the ccy-teams line only; the ccy-sessions keys are
another tool's), Plan 00201 `ACCEPTANCE-owner.md` step 11 ("Enter on your row under A, then
New password").

## Left as they are

- Journals, completed plans, subagent reports and `docs/ccy-changelog.md` (history).
- Specs and research of other plans that describe the old keys (00191 `SPEC-ccy-teams.md`,
  00203 `SPEC-hot-control.md`, 00201 PLAN/DONE files): they record what was built then.
- 00204 `ACCEPTANCE.md` H5/H6 are `ccy --teams` (Tab marks several) and ccy-sessions, not
  ccy-teams' picker.
- `files/var/local/claude-yolo/lib/session-registry.bash:482`, a comment "(ccy-teams'
  Ctrl-R)": changing it needs a CCY version bump for a comment, so it was not touched here.
  No file under `files/var/local/claude-yolo/` changed, so no CCY bump.

## Deploy

ccy-teams is built by `playbooks/imports/play-claude-yolo.yml`, which is already first in
`CLAUDE/Plan/meta-deploy.list`; its description now names this change too.
