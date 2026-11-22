# Lone Enter and tmux modes: Defence Before Fix report

The review of 68e3b603f found that the tmux-mode class was only partly closed. `Tmux.Submit`
leaves copy or view mode before it types, but four bare `SendEnter` calls never went through it.
In copy mode, tmux took the Enter while the screen beneath still showed the dialog or the line.

Branch `plan-xhost-lxc-members`, worktree `untracked/worktrees/sealed-carry`.

## Commits

| Commit      | What                                                                                                                                  |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `eb234303b` | The red defence on its own: `project_submit_unchecked` flags any `Tmux.SendEnter` outside `internal/tmuxccy`. Comes with its fixture. |
| `8029fea80` | The fix: `Tmux.PressEnter`, the four sites, the tests, the corrected Submit comment, the docs and CCY 3.122.50.                       |
| (this one)  | This report and the plan journal entry.                                                                                               |

## Detector (red)

- **Rule.** Outside the owner package, a `Tmux.SendEnter` is reported as a lone Enter, whether
  it is called or taken as a method value. A function that also types with `SendKeys` keeps the
  existing "typed then entered" message.
- **Fixture.** `src-go/qa/rules/testdata/src/fix/sub/loneenter/lone.go` reproduces the shapes of
  the four sites: the `/exit` re-Enter, the `/compact` re-Enter, the background-work answer and
  a method value. `PressEnter` is the clean case. In `badsubmit`, the lone `reenter` was not
  judged before and is now a `want`. A `reenterChecked` case through `PressEnter` is clean.
  `go test ./rules -run TestSubmitUnchecked` passes, which means every `want` was reported.
- **Sweep at `eb234303b`.** The driver ran as `go vet -vettool` over the module, the `qa`
  module and the four tagged builds (`busfailpoints`, `tuwunel`, `ccyteamstest`, `difftest`).
  **4 findings, all of them the known sites:**
  - `internal/sessclose/sessclose.go:637`
  - `internal/sessclose/sessclose.go:746`
  - `internal/sessclose/sessclose.go:847`
  - `internal/teams/winddown/windstep.go:317` (the `ccyteamstest` build finds the same site)

## Fix

- **`tmuxccy.Tmux.PressEnter`** (`submit.go`) reuses the `leaveMode` step:

  - copy or view mode is cancelled and the mode is checked again;
  - any other mode, or a mode that stays after the cancel, is `ErrPaneInMode` with
    "no Enter was pressed";
  - a mode read that fails returns an error before any key.

  `leaveMode` now takes the words for what the caller did not do. Submit keeps "nothing was
  typed".

- **`answerBackgroundWork`** sets `answered`, and restarts the wait, only after `PressEnter`
  returns nil. Otherwise the session fails with: `claude's "Background work is running" question was not answered: <why>; it is still running: attach it (ccy-sessions)`. A container session
  names where it runs instead. The package doc gains C8a.

- **The `/exit` and `/compact` re-Enters** fail with "... stayed in its input box and was not
  entered again: <why>", followed by where the session is left.

- **The wind-down `reenter`** builds its note from the reason the line stayed: "after a second
  Enter", or "and was not entered again: <why>". This covers `/goal clear`, the wind-down and
  `/compact`.

- **The Submit comment** (the reviewer's nit) now says what a mode entered mid-line does. Keys go
  to the mode until a key of the text ends it (`q` or Escape in vi copy mode), so the box may
  hold none of the line or only its tail.

## Tests (red, then green)

The fakes in `sessclose_test.go` and `winddown/helpers_test.go` now model a pane's tmux mode:
`display-message` answers with the mode, every key except a cancel goes to the mode, and a
cancel leaves the mode unless it is stuck.

- **`tmuxccy.TestPressEnter*`** cover five cases: no mode, copy and view mode, a mode it cannot
  leave, a failed mode read with no key sent, and a failed Enter. They were red first because
  the function did not exist yet.
- **`sessclose.TestTheBackgroundWorkQuestionIsAnsweredAfterLeavingCopyMode`.** Red: `answered true, enters 0`, which is the defect. Green: answered by one delivered Enter, and the mode is
  left.
- **`sessclose.TestTheBackgroundWorkQuestionUnderAModeItCannotLeaveStaysUnanswered`** covers
  tree-mode and a stuck copy-mode. `answered` stays false, the deadline does not move, no Enter
  is pressed, and the note gives the reason. Red at first: the error was nil and `answered` was
  true.
- **`sessclose.TestAReEnterLeavesCopyModeAndFailsUnderAModeItCannotLeave`** covers `/exit` and
  `/compact`. Red at first: copy mode took the Enter.
- **`winddown.TestAReEnterLeavesCopyModeAndFailsUnderAModeItCannotLeave`** covers the goal, text
  and compact phases. Red at first: copy mode, `enters 0`, and no failure.

## Green evidence

- `go test -race`: tmuxccy, sessclose, teams/winddown, teams/shutdown and sessions all pass.
- `golangci-lint`: 0 issues on the touched packages and on `qa/rules`.
- Project-rules sweep: clean over the module and the `ccyteamstest` build.
- `bash scripts/test-ccy-teams-command.bash`: 397 passed, 0 failed.
- `qa-changed.bash --base origin/F44`: **exit 0** (27 changed files). Log:
  `untracked/scratch/lone-enter-qa.log`.

## Docs and version

- **`CLAUDE/GoProjectRules.md`**, section `project_submit_unchecked`:
  - "what it flags" and "not flagged" now cover the lone `SendEnter` and `PressEnter`;
  - the false claim that routing typed lines through Submit held the mode class is replaced:
    Submit holds it for typed lines and `PressEnter` for lone Enters;
  - the lone-Enter red and green results are added;
  - the catalogue row is widened.
- **The rule's own doc string** is updated to match.
- **CCY 3.122.50**, with an entry in `docs/ccy-changelog.md`. The container stays at 2.84.43,
  because none of the touched packages is in `image-source-paths`. The pre-commit version gate
  passed on commit.

## Dogfooding notes

Nothing in this repository's own tooling failed. Two places needed a second try:

- The post-edit Go lint hook reports GOPATH-style analysistest fixtures
  (`qa/rules/testdata/src/fix/...`) as broken: it says "package fix/sub/... is not in std",
  and `unused` fires on the deliberately unused fixture methods. The fixtures are correct and
  `analysistest` runs them. This lint noise was already there for the existing `badsubmit` and
  `tmuxccy` fixtures; the coordinator may want to log it.
- A Bash heredoc that appended to a test file was denied as a write outside the root, because
  its path had a relative `../untracked` part. That was my mistake, and the hook's message was
  clear.
