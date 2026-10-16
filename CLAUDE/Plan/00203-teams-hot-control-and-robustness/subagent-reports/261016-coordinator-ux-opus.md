# The coordinator's two paths, and three-way picker offers

Branch `fix/coordinator-two-paths`, from F44. The image carries `helpers/agent_bus/declare.py`
(new `lxc-role`), so the versions are bumped: CCY 3.99.12.4 and container 2.77.3, with a
changelog entry. Queued: `playbooks/imports/play-claude-yolo.yml` in
`CLAUDE/Plan/meta-deploy.list`. Owner's steps: ACCEPTANCE.md, the two sections after M5.

## The wording, from one place

`src-go/internal/teams/coordinator` holds the wording and the decision; every place below
takes its words from it.

- **No coordinator:** "<team> has no coordinator. Make <session> its coordinator?"; in a list
  of roles, "Make it the coordinator (<team> has none)".
- **A coordinator already (takeover):** "<team>'s coordinator is <current> (running). A team
  has one coordinator. Remove <current> as coordinator and make <session> the coordinator
  instead?" It starts on No. The words after the handle are `running` (a live process holds
  its seat), `not running`, `parked`, or `whether it is running is not known here`. In a list
  of roles it is "Replace the coordinator, <current> (running): it becomes a worker...", after
  the worker choice, and never a plain "coordinator".
- **Non-interactive refusal** (no terminal, or `--yes` without the flag): "<team>'s coordinator
  is <current> (running). A team has one coordinator: to remove <current> as coordinator and
  make <session> the coordinator instead, add --demote-current. Nothing was changed." Exit 64.

## Every place, and what changed

| Place                                                                                                     | Change                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| --------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `hot/role.go` `session role`                                                                              | One question naming the path (it asked two before). `who` = "<session> (seat N, <handle>)". The takeover needs `--demote-current` with no terminal or `--yes`. Running state comes from the seat lists of this checkout and every recorded checkout.                                                                                                                                                                                                                                                                                                                                                                        |
| `hot/add.go` `session add --role=coordinator`                                                             | The question is "Add S to T as seat N (h). <offer>". The takeover refusal now comes before the seat is made or the route checked; before, it came after both (M4 `hot/add.go:55-62`).                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `hot/role.go` `demoting`                                                                                  | The guard for a coordinator that appears between the read and the change uses the same words (state unknown).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `hot/remove.go`, `leaveRole` (remove and move `--coordinator`)                                            | The remove question names where the role goes: "Seat N is T's coordinator: it becomes a worker, and T has no coordinator." or "...: <heir> becomes T's coordinator instead." (Fable M5 N2: the confirmation names the demoted holder).                                                                                                                                                                                                                                                                                                                                                                                      |
| `seats/role.go` `ChangeRole` (picker seat menu, launch chooser)                                           | Takes a `coordinator.Lookup`. The title is "Make H the coordinator of T?" or "Replace X as coordinator of T?". The header carries the path text. The action is "remove X as coordinator and make H the coordinator". It defaults to No. New: `SnapshotRunning` (from the board's rows).                                                                                                                                                                                                                                                                                                                                     |
| `choose/choose.go` launch role question (`ccy --teams`, `cc --teams`, Quick Launch, ccy-sessions restart) | Choices are "Keep as is: worker" (selected), then the none choice or the takeover choice. The header is "...; T has no coordinator." or "...; T's coordinator is X (running), and a team has one." The running state comes from the chooser's own seat rows.                                                                                                                                                                                                                                                                                                                                                                |
| `picker/menu.go` seat menu                                                                                | The label is the none choice or the takeover choice. The takeover is listed after "Remove seat N from T". (Label and choice data only.)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| `picker/sessions.go` add and role questions                                                               | Worker first, then the none choice or the takeover (value `takeover`, which passes `--demote-current`; the command asks the takeover, on No).                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| `lxcadd/adder.go` `ccy-teams lxc-add` (the picker's Add an LXC container's agent)                         | Before, it silently made the member a worker when the team had a coordinator. Now the role question is worker (selected), then the none choice or the takeover of `<user>@<container>` (declared) or `<handle>` (live). The takeover asks the takeover question, on No; No keeps the member a worker and says so. On yes, after the summary (its Role line names who becomes a worker), the declared holder's entry is made a worker through the new `declare lxc-role`, or the live holder through `sudo agent-bus set-role ... worker`. That happens before the new entry is written, and is put back if the write fails. |
| `helpers/agent_bus/declare.py` `lxc-role` (new), `declare/lxc.go` `LxcSetRole`                            | Sets one declared LXC member's role line, keeping every other line, with a backup. UNCHANGED when it already has that role; 64 for an unknown member or role. Tests: `tests/helpers/agent_bus/test_declare.py` `LxcMemberRoleTest`.                                                                                                                                                                                                                                                                                                                                                                                         |
| `agent-bus set-role` / `add-member` (`helpers/agent_bus/registry.py`)                                     | Unchanged: both already refuse a second coordinator by name ("already has a coordinator: X; it must stop being coordinator first").                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| Help: `hot.go` session help, `usage.txt`, `picker/help.txt`, `docs/ccy.md`                                | Describe the two paths.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

## The picker offers (two coordinator follow-ups)

- `picker/offers.go`:
  - The describe offer is "Describe <team> now" (the cursor starts there), "Not now (ask again next time)" or "Never ask about <team>". The path-limit offer is "Remove <team>'s limit now", "Not now..." or "Keep <team>'s limit and never ask again".
  - Esc means not now. Only never is kept: the marker holds `never v2`.
  - An empty marker, which the old yes/no prompt wrote, is taken as not now once. It is removed and the person is told why, so the owner is asked again with no hand edit.
  - The text box asks "What is team <name> for?".
- Team row menu: "Ask me to describe <team> again" and "Ask me about <team>'s limit again". Each shows only for a team marked never, and removes the marker.
- The sweep found one more prompt that did not name its subject. "Remove this seat" is now "Remove seat N from <team>". The other "this" wordings are in launcher text about the session being launched, so they were left as they are.
- Plan 00201 SPEC S-A15 and ACCEPTANCE-owner step 2 are updated.

## Which team a launch question is about (coordinator's last follow-up)

- `chooser.heading` builds the context line once: "Team 2 of 3: <team>", or "Team <team>" for one team.
  - In a given list, the place counts every item.
  - The chooser passes the heading to the seat chooser through `SeatFunc`, which gained a `heading` argument.
- Every question of a team's sequence carries it:
  - scope: "Team 2 of 3: beta · scope: beta does not trust ..." (`chooser/widen.go`)
  - seat: "... · seat: which seat does this session take?" (`choose.PickSeat`)
  - role: "... · role of seat N: ..." (`choose.roleHeader`)
- Each team's heading is also said on its own line before its questions. So a team's notes ("references external", "Taking seat ...") sit under its own heading and never under the next team's.
- There is no per-team description question in this flow.
- **Where a shared heading plugs in:** the text is built only in `chooser.heading` and handed down as one string. A ui-layer screen heading can take that string as its context, in place of the " · <step>" prefix that `choose` and `widen` now put into the gum `--header` and the line they say first.
- The seat list itself and the board's "stale" are unchanged: the owner withdrew that request.
- Docs: Plan 00201 SPEC S-C1, ACCEPTANCE (i), `docs/ccy.md`.

## Also

`scripts/test-ccy-lifecycle.bash` failed 3 cases when run inside a bus-kit session. It inherited `PINGBUS_MEMBERSHIP`, which makes the entrypoint add the relaunch plugin. The wrapper cases now unset it. This was a defect in the test's isolation; the entrypoint is not changed.

## Tests

The tests were written first, and each was seen to fail before the code: `coordinator/coordinator_test.go`, `hot/twopaths_test.go`, `seats/role_test.go`, `choose/choose_test.go`, `picker/{sessions,menu,offers}_test.go`, `lxcadd/adder_test.go`, `declare/lxc_test.go`, and `test_declare.py`. `go test` and `golangci-lint run` over `./internal/teams/...` and `./internal/declare/` now pass, as do the declare helper tests and `scripts/test-ccy-teams-command.bash` (371). That suite pins the path-limit offer, so it was changed in this commit to the three-way choice.

## Coordination

The branch is based on F44, not on fix/picker-visible-actions: the picker changes are only the label and choice data, and two new team-row actions. Merge conflicts are likely in `picker/menu.go` (`seatActions`, `kindActions`), `keys.go` (`do`) and `sessions.go` (`roleOptions`) against the `ui.Action` and `sessionChoices` work.
