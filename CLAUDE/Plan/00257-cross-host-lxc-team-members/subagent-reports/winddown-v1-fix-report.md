# Wind-down v1 triage: the fixes

Source: `winddown-v1-triage.md` in this folder. Raw evidence stays under `untracked/`; no
install-specific name is in this report or in the commits.

## Scope change during the work

Partway through, the owner retired v1 teams. The coordinator's direction was to fix items 1
and 2 only where the same code also serves v2 (teamchat) teams. Items 3 and 4 were to be done as
briefed. The owner's host seats compacting and staying open is the intended "Compact every
session (stays open)" behaviour, not a defect.

## Item 1: an LXC member without `service: true` counted as a failure. v1-only, left for the v1 retirement

- `winddown.forTeam` refuses a v2 team (`board.V2Refusal`), so all of `teams/winddown` is v1.
- `shutdown.resolveLxc` and `addGuest` run only for a v1 team. A v2 team goes through
  `resolveV2Host` and `resolveV2Lxc`.
- **Nothing was committed for this item.** A full fix had been built and seen red before the
  scope change:
  - a shared `lxcsession.Judge` for wind-down and shutdown;
  - a member with no unit and not listening is "not running";
  - a member with no unit that is listening on the bus is "not reachable (started by hand)",
    and its tmux server is not asked;
  - a member with a unit but no session is "not running";
  - a project rule, `project_lxc_unit_server`.
- That work was unfinished, so it was backed out rather than committed. It is kept, untracked,
  as `untracked/scratch/winddown-v1-items-1-2-abandoned.patch` and `...-new-files.tar`.

## Item 2: N@team labels. v1-only, left for the v1 retirement

- The sites are `winddown.addSeat` and `nameOf`, `shutdown.addSeat` and `restart.addSeat`.
  `restart` also refuses v2 teams.
- v2 shutdown names a seat by its handle (`v2host.go`, `v2Label`), so it does not have this
  defect.
- The planned fix and its rule, `project_seat_number_label`, were backed out with item 1 and are
  in the same patch.
- The rule's sweep also found `seats.ToggleRestore` and `seats.removeSeat`. Both show
  agent-bus's own `<seat>@<team>` reference beside the handle and the checkout, so they are not
  this defect.

## Item 3: the false container-checkout warning. Fixed

- `picker.offerJoinHere` printed the refusal on the way in. Opening ccy-teams starts no session.
- It now returns without printing anything.
- The refusal is still shown where a session would start from that path: the list header ("A
  new ccy session is unavailable: …") and a row's Start line.
- Swept the other places that print `Refusal`:
  - `chooser`, `find` and `seats` print it when a launch was asked for;
  - the create wizard prints it in place of a join offer it would otherwise make;
  - `sessions/picker` shows it on its New line.
  - None of them prints it with nothing asked.
- Defence Before Fix: no static detector can tell "a refusal printed when nothing was asked"
  from a correct one, because that depends on the flow. So it was fixed conventionally, with a
  test.
- Red: `TestPickerHeaderNeverOffersANewSessionInAContainersCheckout` now asserts that nothing is
  printed on the way in. It failed with the old line on stderr
  (`untracked/scratch/winddown-fix-item3-red.log`). It is green after the fix.

## Item 4: a line lost to tmux copy mode reported as typed. Fixed

- **The defect.** In a mode, `send-keys` keys go to the mode. `capture-pane` reads the screen
  beneath, so the box looked Ready, and `Submit` called a lost line taken.
- **The fix, in `tmuxccy.Submit`:**
  - Before typing, it reads `#{pane_mode}`.
  - copy-mode or view-mode is left with `send-keys -X cancel` and then checked again.
  - Any other mode, or a mode still there after the cancel, is `ErrPaneInMode` with nothing
    typed.
  - Before each Enter, a pane found in a mode (it entered one while the line was typed) is
    `ErrPaneInMode`. No Enter is pressed, because in copy mode Enter copies and leaves the mode,
    and the error says the line may be lost.
- **The class.** It is held by the existing `project_submit_unchecked`, which routes every
  typed line through `Submit`. Its catalogue entry in `GoProjectRules.md` now says so.
- **What it covers.** This is shared code: v2 shutdown, `ccy --stop`, ccy-sessions' Close,
  wind-down and set-going all get the check.
- **A remaining gap, noted but not changed.** A lone `SendEnter` that answers a dialog (in
  sessclose and windstep) is not mode-checked. A lost Enter there is seen on the next screen
  read and retried; it is never reported as taken.
- **Red.** `TestSubmitLeavesCopyModeBeforeTyping`, `TestSubmitRefusesAPaneItCannotTakeOutOfItsMode`
  and `TestSubmitDoesNotEnterALineTypedIntoCopyMode` failed
  (`untracked/scratch/winddown-fix-item4-red.log`). In the copy-mode case the old code returned
  success for a line that never reached the box, which is the defect itself.
- **Green.** All three pass after the fix.
- **Fakes updated to answer the mode question:**
  - sessclose, sessions and winddown test fakes;
  - `scripts/test-ccy-teams-command.bash`;
  - `scripts/test-ccy-sessions-reboot.bash`.

## Version, docs, checks

- CCY 3.122.49. The container is unchanged, because tmuxccy and the picker are not image
  sources.
- The changelog entry is in `docs/ccy-changelog.md`. `docs/agent-bus.md` gains one sentence on
  the wind-down.
- `go test -race` and golangci-lint pass on tmuxccy, picker, sessclose, sessions and winddown
  (0 issues).
- `scripts/test-ccy-teams-command.bash` and `scripts/test-ccy-sessions-reboot.bash` exit 0.
- `qa-changed.bash --base origin/F44` exits 0 on commit 68e3b603f (64 checks and suites).

## A tool fault met while working

The `lint_on_edit` hook runs `go vet` in module mode on GOPATH-style analyzer fixtures
(`src-go/qa/rules/testdata/src/...`) and reports every fixture import as "not in std". This is a
false failure on every fixture edit. It is to be logged as an issue by the coordinator, because
this seat did not file it.
