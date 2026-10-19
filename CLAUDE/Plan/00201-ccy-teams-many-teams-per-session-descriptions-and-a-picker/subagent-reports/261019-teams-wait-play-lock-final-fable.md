# Fable final check: fix/teams-wait-play-lock at 24034b0e (Plan 00201 Task 2.18)

Reviewer: Fable (adversarial, pre-merge). Branch under review: `fix/teams-wait-play-lock` at
`24034b0e`, a merge of F44 `97537444` into the branch my earlier review (`d227099f` on
`origin/review/teams-wait-play-lock`) passed on design and lock correctness, leaving the queue
and the `ui.Heading` adaptation as merge items.

## Verdict: MERGE

Nothing blocks. Two notes for the follow-up are in the judgement call below; one wording nit in
the queue is optional.

## What was verified

| Check                                                                      | Result                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `git diff F44 24034b0e --stat`                                             | 43 files, +2474/-126. Every F44 file touched beyond `internal/playlock` and `internal/teams/ops` was read in full: `chooser/widen.go`, `chooser/chooser.go`, `create/wizard.go`, `login/login.go`, `lxcadd/adder.go`, `picker/offers.go`, `settings/settings.go`, `app.go`, `teamtest/world.go`, `execx`, `qa/rules/table.go`, `.golangci.yml`, `docs/agent-bus.md`, `scripts/test-ccy-teams-command.bash`. Each is the play-lock threading (`Locked` wrapped around declare+apply, `LockFunc` fields, the `tput` answer in rigs); the F44 content inside each hunk is intact. Nothing from F44 is lost. |
| Lock design since my review (`git diff c4560cf4 24034b0e -- playlock ops`) | 4 files, +45/-2, none of it design: `WrapHeader: true` on the wait screen's `PickSpec`, two test rigs answering `tput cols`, and one new wrap test. `internal/playlock/` is byte-identical to what I reviewed.                                                                                                                                                                                                                                                                                                                                                                                           |
| Journal union                                                              | `JOURNAL/` vs parent 1 (`c4560cf4`): +73/-0. Vs parent 2 (`97537444`): +39/-0. Zero removed lines against either parent: a pure union.                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `git merge-tree --write-tree origin/F44 24034b0e`                          | Clean (tree `bf1eb349...`, no conflict output).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| CCY bump                                                                   | `git diff origin/F44 24034b0e -- files/var/local/claude-yolo` is empty. No bump needed; the branch deploys through the existing 3.99.12.12 entry.                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Queue                                                                      | One `playbooks/imports/play-claude-yolo.yml` entry, unchanged position, with the two-line sentence added to its description. Correct kind of entry (the Go binaries are rebuilt by that play). The claims in the sentence are true to the code: `ops.DefaultPlayLockWait = 30 * time.Minute`; the Stop waiting row is `ExitLabel`; every write happens inside `Locked`.                                                                                                                                                                                                                                  |
| Spec section G / Task 2.18 / Task 2.19                                     | PLAN.md lines 217 and 233 carry 2.18 (lock wait) and 2.19 (run.bash wait); the stat shows SPEC-many-teams.md gained 47 lines.                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |

### WrapHeader correctness

`ui.wrapText` (`src-go/internal/ui/heading.go:261`) feeds each header line wider than `width`
through the heading's own `wrap` as one `piece`. `wrapper.fill` (`heading.go:168`) measures with
`Width` = `ansi.StringWidth` (display cells, wide characters two, colour codes none), takes the
cell-bounded prefix with `ansi.Truncate` (grapheme-cluster aware), backs up to the last space in
it, and otherwise cuts between clusters; the remainder is always `text[len(head):]` or
`text[at+1:]`, so the only byte ever dropped is the one break space. A cluster wider than the
whole line gets a line of its own. No elision anywhere, so **no cut**.

`PickSpec.headerArgs` (`picker.go:225-230`) applies it only when `WrapHeader` is set and only on
the no-`RowHeaders` path, to the `width` the heading itself is wrapped to (columns minus
`framedMargin`), so the header and heading agree on the window. Tests: `ui`
`TestAHeaderLineWiderThanTheWindowIsWrappedNotCut` pins the exact break points on 40 columns;
`ops` `TestOnANarrowTerminalTheWaitScreenWrapsTheHolderAndCutsNothing` proves on 40 columns that
every header line is at most `40-RowChrome-2` cells and the holder text survives whole.

### The real clock in the picker and settings wait-screen tests

Not a flake risk. `Holder.Describe(now)` renders `..., held since 11:59:20 UTC, for N minutes`,
and in `picker` and `settings` `now` is the real clock against `teamtest.HoldPlayNote`'s fixed
`since` of 2026-10-16. The assertions (`picker/offers_test.go:326`,
`settings/settings_test.go:262`) match only the prefix up to `(pid 999)` / `held since 11:59:20 UTC`, never the `for N minutes` tail, and both rigs set `Columns = 200` so the holder stays on
one line. `Duration` of a negative span is "less than a minute", so a clock set before the note's
date cannot fail them either. The one way to make this flaky later would be to assert the full
line or shrink `Columns` in those rigs; the `ops` rig with its fake clock is where the full text
is pinned, and the comments in both tests say so.

### QA (targeted, PINGBUS\_\* unset; never qa-all)

- `go test ./...`: 73 packages ok, both at the terminal's width and with `COLUMNS=30`.
- `go test -race ./internal/playlock/... ./internal/teams/ops/...`: ok.
- `scripts/qa-hidden-bindings.bash`: `HIDDEN-BINDINGS-OK 446 shell file(s), 342 Python file(s)`.
- `scripts/test-ccy-teams-command.bash`: passed 383, failed 0, including the two new checks that
  the fake play sees the lock handed down (`lock.py held`) and never runs outside it.

### Optional nit (not blocking)

`CLAUDE/Plan/meta-deploy.list`'s description opens "Plan 00204 D4, Plan 00201 Task 2.17 and
Plan 00203 Task 3.8"; the lock sentence was appended without adding "Task 2.18" to that list. The
owner reads the sentence either way; the plan reference is cosmetic.

## Judgement call: screens that cut a wide header today, and a default-on WrapHeader

fzf does not wrap `--header` lines: one wider than its window is cut at the right edge. gum's
renderer (Bubble Tea's standard renderer) truncates a line wider than the terminal the same way.
Only the heading is wrapped today (`Heading.render`); the text under it is not, except on the wait
screen.

### Screens whose header can exceed a narrow window

Framed fzf pickers through `PickSpec.Header` (the path a default-on `WrapHeader` would cover):

| Screen               | Site                                                      | What is wide                                                                                              |
| -------------------- | --------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| Restore a team       | `src-go/internal/teams/ops/takedown.go:398`               | the 95-cell sentence on what a restore does                                                               |
| Row menu (every row) | `src-go/internal/teams/picker/menu.go:187`                | "What to do with team X:" and the member's full handle (F44 made handles whole, so this grew)             |
| Running sessions     | `src-go/internal/teams/picker/sessions.go:79`             | `sessionsKeyLine` (80 cells) and `hot.OnlyTmuxLine` (95 cells)                                            |
| Session question     | `src-go/internal/teams/picker/sessions.go:184`            | `questionKeyLine` plus a question sentence                                                                |
| New ccy session      | `src-go/internal/teams/seats/seats.go:185`                | "Runs ccy --teams <seat>@team in <checkout>; ..." — grows with the checkout path                          |
| Coordinator          | `src-go/internal/teams/settings/settings.go:543`          | "Changed at once, not by Save and apply." plus the about line                                             |
| Every Confirm        | `src-go/internal/ui/picker.go:298`                        | the question sentence of every yes/no (remove, purge, backup, shutdown questions all pass sentences here) |
| Every Menu           | `src-go/internal/ui/picker.go:364`                        | `m.Header`, the row menu above among them                                                                 |
| ccy-sessions list    | `src-go/internal/sessions/picker.go:138` (used at `:156`) | the 120-cell key legend                                                                                   |

Paths a default-on `PickSpec.WrapHeader` would **not** reach:

| Screen                         | Site                                                                                           | Why not                                                                                        |
| ------------------------------ | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| Choose teams (`ccy --teams`)   | `src-go/internal/teams/chooser/list.go:37` through `src-go/internal/ui/choose.go:33`           | `ChooseSpec` is the unframed path and has no wrap                                              |
| ftp-camera and freeze choosers | `src-go/internal/ftpcamera/args.go:214`, `src-go/internal/freeze/chooser.go:90` and `:203`     | same `ChooseSpec` path (freeze's lines are 50-75 cells, so only very narrow windows cut)       |
| gum confirm / input / choose   | `src-go/internal/ui/ui.go:66`, `:85`, `:97`                                                    | gum gets `h.above(text)` as is; the heading is wrapped, the question or header under it is not |
| Teams board, team Settings     | `src-go/internal/teams/picker/picker.go:266`, `src-go/internal/teams/settings/settings.go:139` | `RowHeaders` path; `headerArgs` wraps only when `RowHeaders` is empty                          |

### Would default-on be safe?

**On the `PickSpec.Header` path, yes, with one guard**: the wrapping is the heading's own
algorithm (spaces first, clusters only when a word is wider than the line, nothing dropped), and
every header on that path is prose or a key legend separated by spaces, which wraps well. The
`ops` rig already shows that a wrapped header leaves the rows and the Exit label untouched. The
guard: a header line carrying colour (`term.Bold ... term.Reset`) that is split across lines
leaves the open code on one fzf header line and the reset on the next; fzf parses ANSI per line,
so the second line may lose the attribute or the first may bleed it into the rule. No header on
the `PickSpec.Header`-only path carries colour today, but the follow-up should pin that with a
test (wrap a bold line and assert each fragment is closed and reopened) before flipping the
default.

**Not safe as a blanket rule for `RowHeaders`**: the Teams board's row headers
(`picker/picker.go:156-168`) end with the table's heading row, column-aligned to the data rows
and bold when colour is on. F44's full-handles work deliberately sizes those columns and moves
overflow above the headings rather than wrapping; wrapping the heading row while fzf cuts the data
rows would misalign the two. `headerArgs` already leaves the `RowHeaders` path alone, so a
default-on flag would not touch it, but the follow-up should keep it that way on purpose, and
Settings' row headers (sentence-shaped, safe to wrap) would want a separate opt-in.

**The gum and `Choose` paths need their own change**: `WrapHeader` is a `PickSpec` field, so it
cannot reach `ui.go:66/85/97` or `choose.go:33`. The same `wrapText(text, width)` call before
`h.above` on those four lines would close the gap; gum's width is `columns - gumMargin`, Choose's
`columns - plainMargin`, both already computed there.

Recommendation for the follow-up: flip `WrapHeader` on by default for the no-`RowHeaders` path
(or drop the flag and always wrap there), add the colour-split test, and apply `wrapText` on the
gum and `Choose` paths. None of it blocks this merge.
