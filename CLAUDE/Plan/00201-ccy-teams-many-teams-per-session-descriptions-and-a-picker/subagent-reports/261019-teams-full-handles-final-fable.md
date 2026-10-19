# Fable final review: fix/teams-full-handles at a5bc2b1b

Reviewed: `a5bc2b1b` (merge of `origin/fix/context-headings` 665ce541 into `fix/teams-full-handles`),
on top of `92b38fde` (the FIX-FIRST item from the re-review a76793a5). Reviewed against local F44 at
`22d7fbb9`, which already holds context-headings (15abef60) and ccy 3.99.12.11.

## The fix (92b38fde)

`src-go/internal/teams/picker/sessions.go`: the four places the re-review named (the already-in
refusal in `teamOptions`, the demote row and the question in `askCoordinator`, the role question in
`roleInTeam`) now print `agentbus.Label(handle, "")`. `seatLabel` resolves the handle through
`seatHandle` and falls back to `"seat N"` only when the board has no row for the seat. The
`askCoordinator` path uses the handle directly: it has already returned early when no handle
resolves, so the fallback is unreachable there and is correctly not written.

Tests: the three existing tests were tightened to the handle (`proj.1+local.podman`), the demote
row is asserted, and `TestTheRoleOfASeatWithNoHandleYetNamesTheSeat` covers the fallback. Good.

## The merge (a5bc2b1b) against context-headings' final (665ce541)

- `git diff 665ce541 a5bc2b1b` touches only Plan 00201 files plus three `ui/` files; nothing from
  665ce541 is reverted. The whole `src-go/internal/ui/` tree is **byte-identical** to
  `origin/fix/lxc-checkout-typed` ebc35cb5 (`git diff ebc35cb5 a5bc2b1b -- src-go/internal/ui/` is
  empty), so the three branches share one `ui.Width`.
- Guidance followed: `cells()` in `heading.go` is deleted and every call is `Width`; `cut` and
  `memberWidth` are gone from `board/table.go`; `Row.Crumb` uses `agentbus.Label(r.Handle, "")`
  and keeps `"seat N"` for a ccy row with a checkout and no handle, with a table test.
- Margins: 665ce541 had `framedMargin = 14, plainMargin = 4, gumMargin = 2`. Now
  `framedMargin = RowChrome + 2`, `RowChrome = 2*(frameMargin+frameBorder+framePadding) + pointerWidth = 2*(2+1+2)+2 = 12`, so 14 still. `plainMargin`/`gumMargin` unchanged. The equivalence is pinned
  by `TestRowChromeIsWhatTheFrameTakesFromTheTerminal` (asserts the fzf `--margin=1,2`,
  `--padding=1,2`, `--border=rounded`, `--pointer=▶` args AND `RowChrome == 12`), and the heading
  tests measure through `unwrapped = fallbackColumns - framedMargin`. The fzf args now derive from
  the same constants, so the two cannot drift apart.
- Property/measured tests survive: `TestAHeadingFitsEveryWidthInCellsAndLosesNothing`,
  `TestACrumbLongerThanTheWidthIsHardWrappedNotCut`, `TestThePickerWrapsToTheMeasuredWidthOfItsTerminal`,
  `TestGumAndTheUnframedChooseWrapToTheirOwnMargins`, `TestEveryScreenRefusesToDrawWithoutAHeading`,
  `TestTheHeadingIsOnTopOfEveryGumQuestion`, `TestTheHeadingIsOnTopOfEveryFzfHeader` all present.
- No truncation returned: the only `…` in non-test Go are gum spinner text, the `Type a path…`
  label and a usage hint; no `cut(` or width cap remains. `board.Layout` sizes each column to its
  longest value and goes narrow (TEAM, MEMBER, KIND, STATE on the row, the rest in `Rest`) rather
  than cutting.

## Beyond the brief

- Three heading context lines (`removeFromTeam`, `moveTeams`, `roleInTeam`) use `seatLabel`.
  Consistent with the fix; the handle in the heading and in the question agree.
- `sessions/picker.go`: `projectHere` was removed by context-headings, so the merge reworked
  `pickState` to `unavailable`/`refusal`/`noProject` driven by `hostcheckout.Finder.Find`:
  - no git repo: `noProject`, the old wording kept ("is not a project folder");
  - LXC container checkout: `refusal = checkout.Refusal(...)`, row says "unavailable, <top> is LXC
    container box's checkout", heading says "no new session can start in <here>", Ctrl-N says the
    refusal and counts a try, and `noSessions` says the refusal under the usual line;
  - a Find error is also a refusal (shown as the error text), which is the fail-fast reading.
    Tests at `sessions/picker_test.go:544-578` exercise the row, the heading `argv`, the stderr
    refusal with "ccy-teams lxc-add" and the three-tries exhaustion, and the no-sessions path.
    The teams picker's `pickerFrame` uses the same Finder for its Ctrl-N line. The guard still
    refuses.

## Merge against local F44 (22d7fbb9)

`git merge-tree --write-tree F44 a5bc2b1b` conflicts in exactly one file, `CLAUDE/Plan/meta-deploy.list`;
every source file auto-merges. The branch's queue predates ccy 3.99.12.11, so the three-way is:
F44 = (00204 play-claude-yolo, 00179 speech-to-text, 3.99.12.11 play-claude-yolo); branch =
(00179 speech-to-text, one combined 00204 + 00201 play-claude-yolo). Resolve to the Plan 00179
`play-speech-to-text.yml` entry plus ONE `playbooks/imports/play-claude-yolo.yml` whose description
folds in the 00204 D4, 00201 Task 2.17 and 3.99.12.11 launcher notes. Queue-only, as the brief
expected.

## Targeted QA (no qa-all)

- `src-go`: `go build ./...`, `go vet ./...`, `go test ./...` pass; `COLUMNS=30 go test -count=1 ./...` passes.
- `scripts/qa-hidden-bindings.bash`: `HIDDEN-BINDINGS-OK 446 shell file(s), 342 Python file(s)`.
- `scripts/test-ccy-teams-command.bash` and `scripts/test-ccy-teams.bash` with every `PINGBUS_*`
  unset (verified zero left in the environment): both exit 0, 381 and 191 PASS lines, no FAIL line.

## Verdict

MERGE. Resolve the one queue conflict as above; nothing else is needed.
