# Final review: `fix/context-headings` at 665ce541 (Fable)

Verdict: **MERGE**. Every FIX-FIRST item of the re-review (`261016-context-headings-rereview-fable.md`)
is closed and verified against the real programs; one non-blocking nit remains.

Reviewed: `git diff origin/F44..665ce541` (merge base 0ce52a7e), of which the heading work is
3e817bb9 and 665ce541 is the F44 merge. Branch `review/context-headings-final` holds this report.

## The re-review's items, verified

**B1 (margins) — closed, and re-measured.** `src-go/internal/ui/heading.go:257-261` has
`framedMargin = 14`, `plainMargin = 4`, `gumMargin = 2`; `fallbackColumns = 80`
(`gumx.go:15`), so the unreadable-width picker header is 66. A fresh pseudo-terminal run (fzf
0.38, gum 0.17, the exact frame from `picker.go:171-176`, the exact `Choose` args from
`choose.go:33`, `gum confirm` with a two-line prompt):

| screen                  | columns | whole | cut | margin used | margin measured |
| ----------------------- | ------- | ----- | --- | ----------- | --------------- |
| framed picker           | 40      | 26    | 27  | 14          | 14              |
| framed picker           | 80      | 66    | 67  | 14          | 14              |
| `Picker.Choose` (plain) | 40      | 37    | 38  | 4           | 3               |
| `gum confirm` header    | 40      | 39    | 40  | 2           | 1               |

The framed margin is exact. The plain and gum margins are each one column on the safe side
(the gum one is documented as "kept off the last column", `heading.go:255-256`); nothing is
cut. The test numbers come from a measured table, not the constant: `heading_test.go:25`
`measuredHeaderWidth = map[int]int{40: 26, 80: 66}`, used by
`TestThePickerWrapsToTheMeasuredWidthOfItsTerminal` and the render tests.

**B1b (cells and graphemes) — closed.** `cells()` is `ansi.StringWidth` (`heading.go:110`);
the hard wrap is `ansi.Truncate` at a cell count (`heading.go:163`), then back to the last
space past the mark; no `[]rune` index cut remains except the one-rune fallback for a cluster
wider than the whole line (`heading.go:168-171`), unreachable at the `max(width, 2)` floor
since no cluster is wider than two cells. `TestAHeadingFitsEveryWidthInCellsAndLosesNothing`
(`heading_test.go:79-111`) drives CJK, an emoji, a ZWJ sequence (`👩‍💻`) and combining
marks (`é`) through every width 2..80, asserting no line wider than the width in cells,
no line starting inside a cluster, nothing lost or reordered, and no `..`/`…`.
`TestAWideRuneHeadingIsWrappedByCellsNotRunes` pins the 18-CJK case at 26 cells.

**S1 (tests do not read the shell's width) — closed.** No `COLUMNS` or ioctl reader exists
(`grep -rn 'Getenv("COLUMNS\|ioctl\|GetWinsize' src-go/internal` is empty). `go test ./...`
and `COLUMNS=30 go test -count=1 ./...`: 69 packages ok each, exit 0.

**S2 (one width reader) — closed.** `columns()` in `gumx.go:62-72` is the only reader:
`tput cols` through the Runner, non-zero exit, unparsable or `<= 0` output → 80. `Gum` and
`Picker` both call it (`heading.go:264-269`); `Explain` uses it too. The `Columns` fields are
gone (teamtest builds `ui.Gum{}`/`ui.Picker{}` without them, `console.go:283-284`).
`go.mod`, `go.sum`, `go-deps.allowlist` and `vendor/` are byte-identical to F44
(`git diff --stat origin/F44 665ce541 -- …` is empty); `golang.org/x/sys` is `// indirect`
again.

**S3 (end-to-end narrow tests) — closed.** `teamtest.Script.Columns` answers `tput cols`,
0 → 80 (`console.go:19-20,44-45,129-134`).
`lxcadd/checkout_test.go:79` runs the real lxc-add to step 4 (a `Gum.Choose`, `adder.go:442`)
at 40 columns and asserts the exact four-line wrapped heading, every line ≤ 38 and no `..`.
`picker/sessions_test.go:452` runs the real Move → coordinator path at 40 columns and asserts
the exact seven-line heading with every line ≤ 26. Both tests' own line checks count runes,
which is fine for their ASCII text.

**Nits.** N1: `widen.go` no longer repeats the trail (`c.say(head.Trail())` removed), and
`SPEC-many-teams.md` S-C1 and the 00203 ACCEPTANCE row are reworded to match. N2:
`InteractiveScripts.md:120-123` now says fzf cuts to `..` and gum never wraps, which is what I
measured. N3: a mark that would push a fitting piece onto two lines ends the line before
(`heading.go:147-153`, `TestAMarkThatWouldPushAPieceOntoTwoLinesEndsTheLineBefore`); an
empty context part draws no mark (`contextPieces`, `heading.go:210-224`). N4:
`src-go/CLAUDE.md:73-78` rewrapped and names `tput cols` and `Script.Columns`.

## The owner's extra questions

- **tput failing.** Real `tput cols` with `TERM` unset exits 2 → `columns()` returns 80;
  with no tty on stdout (pipe) it prints terminfo's 80 and exits 0. The Fake with
  `ExitCode: 1` is tested (`fakeColumns("")`, `heading_test.go:34`;
  `TestExplainWrapsToTheTerminalWidthLessFour` cases `0`, `wide`, exit 1 → 76). No panic
  path: `strconv.Atoi` errors are handled, `cols <= 0` is handled.
- **Very narrow widths.** A scratch test (not committed) drove `render` at widths −20, −4,
  −1, 0, 1 and `Pick`, `Choose`, `Confirm`, `Explain` at `tput cols` of 10, 3, 1, −5, `abc`
  and failing: no panic, every heading line ≤ 2 cells (`render` floors at 2,
  `heading.go:233`). At 10 columns the picker's heading width is −4 → 2, so the heading is
  one grapheme per line, which is the honest answer for a terminal fzf cannot draw in anyway.
- **Explain's 44 floor removed.** Judged acceptable: a 43-column terminal now gets a 39-wide
  explanation instead of one wrapped at 76 and cut by the terminal, which is the behaviour the
  heading work is for. One rough edge (nit below): at `tput cols` of 1..3 the width is 0 or
  negative; `gum style --width 0` is "no wrap" (fine), but `--width -1` is a kong parse error
  (gum exits 80), so `Explain` would return an error on a terminal narrower than four
  columns. No real terminal is that narrow, and `render` already clamps, so this does not
  block.
- **meta-deploy.** `CLAUDE/Plan/meta-deploy.list` holds `playbooks/imports/play-claude-yolo.yml`
  (00204 D4 description: accurate — rebuilds `ccy-teams` and `ccy-sessions`, headings wrap at
  `›` and `·`, running sessions untouched) then
  `playbooks/imports/optional/common/play-speech-to-text.yml` (Plan 00179 Task 0.7, inherited
  from F44). `git diff origin/F44 665ce541 -- files/ playbooks/ tasks/` is empty, so no CCY
  version bump is owed.
- **Merge.** `git merge-tree --write-tree origin/F44 665ce541` writes a tree, exit 0, no
  conflicts (F44 is the merge base, so it is a fast-forward).

## Nit (non-blocking)

- **N1** `gumx.go:55`: `g.columns(ctx)-explainMargin` can go negative when `tput cols`
  answers 1..3; gum refuses a negative `--width`. `max(0, …)` (0 = unwrapped) closes it, with
  one more row in `TestExplainWrapsToTheTerminalWidthLessFour`. Same shape as the
  `render` floor; a terminal under four columns is not a real case.

## What I ran

- `go test ./...` and `COLUMNS=30 go test -count=1 ./...` in `src-go`: both exit 0, 69 ok.
- `scripts/qa-hidden-bindings.bash`: exit 0 (`HIDDEN-BINDINGS-OK 446 shell file(s), 342 Python file(s)`).
- `test-ccy-teams`, `test-ccy-teams-command`, `test-ccy-session-registry`,
  `test-ccy-sessions-take-over`, every `PINGBUS_*` unset: all exit 0.
- `scripts/qa-go.bash`: exit 1, the one failure being
  `qa/cmd/qa-patterns TestRealSemgrepScansTestsDirectoriesAndLargeScripts` with
  `scripts/big.bash (Timeout)` from nested semgrep — the known host-load noise, in the `qa`
  module this branch does not touch; every `internal/` package passed.
- The pseudo-terminal width probe (`untracked/scratch/final-review/fzf-width.py`, not
  committed) over fzf framed, fzf plain and gum confirm; the scratch narrow-width Go test.
- `qa-all` was not run.

**Verdict: MERGE.**
