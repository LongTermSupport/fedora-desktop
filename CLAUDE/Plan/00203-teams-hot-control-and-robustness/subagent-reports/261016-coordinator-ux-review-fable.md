# Review: the coordinator's two paths and the three-way offers (fix/coordinator-two-paths)

Branch `fix/coordinator-two-paths` at f3c1151d, based on local F44 83139db8. Read-only review;
the developer's report is `261016-coordinator-ux-opus.md`.

## Verdict: FIX-FIRST

One blocking finding (B1, the version numbers), and the merge work the developer predicted,
which is larger against `dbf/hidden-bindings` than the report says. The behaviour itself
holds: no path demotes a coordinator silently or without naming it, the lxc-add transaction
never leaves two coordinators, the old-no migration is one-time, and every action is a
visible choice.

## Tests run

- `go test ./internal/teams/... ./internal/declare/...`: every package ok.
- `tests/helpers/agent_bus/test_declare.py`: 135 passed (`LxcMemberRoleTest` included).
- `scripts/test-ccy-teams-command.bash`: passed 371, failed 0.
- `scripts/test-ccy-lifecycle.bash` (with `PINGBUS_MEMBERSHIP` unset): passed 215, failed 0.
  The file is mode 100644 in git on F44 and on the branch, so it has to be run as
  `bash scripts/...`; not this branch's doing, but worth a `chmod +x` in its own commit.

## Blocking

### B1. The versions collide with feat/hot-control-m6

- `files/var/local/claude-yolo/claude-yolo:17,81`, `files/var/local/claude-yolo/Dockerfile:36`,
  `docs/ccy-changelog.md:20`, `CLAUDE/Plan/meta-deploy.list` (the new entry), `PLAN.md` Task
  3.5.
- Scenario: `feat/hot-control-m6` already holds CCY 3.99.12, 3.99.12.1, 3.99.12.2 and 3.99.12.3
  with containers 2.77, 2.77.1 and 2.77.2 (commits b959973b, 6f9375e3, 0ccf32d2; its changelog
  says "Includes 3.99.12 to 3.99.12.2, never deployed"). This branch independently claims
  **3.99.12.2 / container 2.77.2** for a different image (the one with `declare.py lxc-role`).
  Two images under one label is the exact class the Dockerfile comment warns of: a host that
  built 2.77.2 from m6 sees `REQUIRED_CONTAINER_VERSION` match after this merges and never
  rebuilds, so the image lacks `lxc-role`; and the two changelog sections carry the same
  number for different changes. Local F44 is itself already at 3.99.10.2 (the branch was cut
  from 3.99.10.1), so the changelog conflicts with F44 too.
- Fix: take numbers strictly above m6's, 3.99.12.4 and container 2.77.3 (m6 is the owner's
  current branch and will land), in all five places; place the changelog section above
  3.99.12.3 when merging and say "after 3.99.12.3"; update the `meta-deploy.list` entry's
  numbers. Alternatively, rebase onto m6 and renumber there; either way one number, one image.

## Judged, not blocking

### Can any path still demote a coordinator silently or without naming who?

No. Every route to a demotion goes through a named question that starts on No:

- `hot/role.go:95-107` `coordinatorPath` decides the path before anything changes; the
  takeover question comes from `coordinator.OfferTakeover` and names the holder and its state.
  `hot/hot.go:541-549` `confirm` always asks with `defaultYes=false`, so the takeover (and the
  plain offer) start on No. Non-interactive or `--yes` without `--demote-current` is exit 64
  with `coordinator.Refusal`, naming the holder and the flag (`hot/role.go:103-104`), and in
  `add` it comes before the seat is made or the route checked (`hot/add.go:55-64`).
- `hot/role.go:75-89` `demoting` is the guard for a coordinator that appears between the
  question and the change: it asks again (state unknown) or refuses non-interactively.
- `seats/role.go:36-38` `ChangeRole` confirms with default No in both paths; `promote` puts the
  holder back if the second set-role fails.
- `choose.go` lists the takeover as its own item after "Keep as is: worker"; `ChangeRole` then
  asks.
- `picker/menu.go` and `picker/sessions.go` only build labels and pass `--demote-current`; the
  command asks.
- `lxcadd/adder.go:428-463` asks the role (worker selected), then the takeover on No; No says
  the holder stays and the member joins as a worker. The summary's Role line names who becomes
  a worker.
- `hot/role.go:275-276` a coordinator's remove asks demote-or-not with yes by default, as the
  spec says (H23), and the remove's own confirm (default No) then names where the role goes.
- The registry refuses a second coordinator by name (`registry.py:_free_to_hold`) as the
  backstop.

### Is the lxc-add demote-then-write transactional?

Order is demote, write entry, apply (`adder.go:631-660`). A crash between any two steps never
yields two coordinators:

- Declared holder: `lxc-role` sets its entry to worker, then `lxc-add` writes the new entry as
  coordinator. Crash between: host_vars has no coordinator declared, the live team still has
  the old one; the next apply demotes it (the play runs every demotion before any promotion,
  `helpers/agent_bus_lxc/members.py:1005-1011`, and corrects a joined member's role when it
  differs, `present_plan`). So "none", recoverable, never two.
- Live holder: `sudo agent-bus set-role ... worker`, then the write. Crash between: the live
  team has no coordinator, host_vars unchanged; `lxc-add` again or `set-role` puts it back.
  Write failure rolls back and names the manual command if the rollback fails.

Gap N2 below: an apply failure after the write leaves the live holder a worker and says
nothing about it.

### Is `set-role` authorised as the owner only?

`agent-bus set-role` is unchanged and runs as root through sudo (`cli.py:279`, the registry is
root-owned); a container session has no host sudo and a peer only has pingbus messages. The
new `declare lxc-role` edits the host_vars file with the login user's rights, the same
authority as the existing `lxc-add`, `lxc-remove` and `describe` edits; it is in the image
because the image carries `declare.py`, but a session's ability to edit the checkout's
host_vars is pre-existing surface, not widened here.

### Is the old-no migration correct and one-time?

`picker/offers.go:65-82` `askedBefore`: no file is "ask"; body exactly `never v2\n` is "never";
anything else (the old prompt wrote an empty file) is removed, said, and asked once. Only a
never answer writes a marker again (`keepAnswer`), so an old marker cannot recur. The team-row
undo (`askAgain`) removes the marker. Tests: `TestAnOldNoIsTakenAsNotNow`, the two
`...NotNowAndEscAreAskedAgain`, the two `...NeverIsRememberedAndNeverAskedAgain`, and the
row-menu undo. Correct.

### Does every action stay a visible choice (InteractiveScripts rule 14)?

Yes. "Not now" is a row as well as what Esc means; the two undo lines are menu rows with no
shortcut, shown only on a team marked never; the takeover is a listed item, never a key.

### Do the tests cover both paths in each place?

| Place                        | None path                                                            | Takeover path                                                    |
| ---------------------------- | -------------------------------------------------------------------- | ---------------------------------------------------------------- |
| `session role`               | `TestRoleWithNoCoordinatorIsOfferedPlainly`                          | `...AsksTheTakeoverOnceDefaultingToNo`, refusal, not-running     |
| `session add --role`         | `...OnATerminalAsksOneQuestionNamingThePath`                         | `...IsRefusedBeforeAnythingChanges`, `add_test.go` with the flag |
| remove/move                  | `TestRemoveOfTheCoordinatorSaysWhereTheRoleGoes`                     | same (heir wording)                                              |
| `seats.ChangeRole`           | existing role tests                                                  | `TestTheTakeoverSaysWhetherTheCoordinatorRuns`                   |
| launch role (`choose`)       | `...KeepWorkerFirstAndNeverOfferAPlainCoordinator`                   | same test                                                        |
| picker seat menu             | `menu_test.go:231` label                                             | `TestASeatsMenuOnATeamWithACoordinatorOffersTheTakeoverLast`     |
| picker sessions: add         | `TestAddAsCoordinatorOfATeamWithNoneIsOfferedPlainly`                | `sessions_test.go:150-163`                                       |
| picker sessions: Ctrl-O role | not seen (only the takeover at `sessions_test.go:263`)               | covered                                                          |
| lxc-add                      | `TestTheCoordinatorIsOfferedPlainlyWhenTheTeamHasNone`               | declared, live, rollback, and no silent worker                   |
| `declare lxc-role`           | `LxcMemberRoleTest` (replace, add line, unchanged, refused, dry-run) |                                                                  |

One small gap: the picker's Ctrl-O none path (`roleOptions` with no holder in `roleInTeam`).

## Non-blocking findings

- **N1. A false "has no coordinator" when the chosen seat's handle already holds the role.**
  `hot/add.go:86` and `hot/role.go:99-101`: `coordinator.Holding(roster, handle, ...)`
  excludes the handle itself, so `session add S T/N --role=coordinator` on a free existing seat
  whose handle is the coordinator (the spec's Decision 5 case) asks "Add S to T as seat N (h).
  T has no coordinator. Make S its coordinator?", which is untrue; `setRoleTo` then skips as
  "already". The picker's add (`sessions.go:247-248`, `roleOptions(team, "", holder)`) shows
  "Make it the coordinator (T has none)" in the same case. Fix: when
  `roster.Member(handle).Role == coordinator`, offer nothing to change and say "seat N's handle
  <h> is already T's coordinator" (as `choose.roleHeader` already does).
- **N2. lxc-add's apply-failure message after a live takeover.** `adder.go:648-652`: by then the
  live coordinator has been made a worker and the new entry is declared but not joined, so the
  team has no coordinator, and the message says only "declared ... but not joined". Fix: when
  `ans.takeover.holder != ""`, add that `<holder>` is now a worker and the team has no
  coordinator until `ccy-teams apply <team>` succeeds.
- **N3. Esc on lxc-add's takeover question cancels the whole run** (`adder.go:449-452` through
  `gumErr`), after three answered questions, rather than meaning No. Acceptable ("Esc or No
  changes nothing"), but No would be kinder; the spec text for the picker says Esc is No.
- **N4. `set_lxc_role` ignores `state`** (`declare.py:1140-1142`); unambiguous because declare
  refuses a duplicate (team, container, user) and `coordinatorOf` passes only a present
  member. Note only.
- **N5. The `# Container ...` comment on `CCY_VERSION`** must move with B1.

## Merge risk

Dry-run merges (`git merge-tree`), none applied:

- **Against fix/picker-visible-actions (238529e7):** `menu.go` and `keys.go` auto-merge cleanly.
  Conflicts: `picker/sessions.go` (`roleInTeam`: the picker branch takes the team as a
  `hot.Cell` from a per-team menu line "Change role in T (now R)"; this branch asks the team
  with `askTeamOf`), `picker/sessions_test.go`, `picker/help.txt`, `ACCEPTANCE.md`, the
  journal, `meta-deploy.list`. Resolution that keeps both intents: keep the picker branch's
  `roleInTeam(ctx, s, cell)` signature and callers, and this branch's body
  (`coordinator.Holding(..., seats.SnapshotRunning(r.snap))` and
  `roleOptions(team, current string, holder coordinator.Holder)`); delete the picker branch's
  `roleOptions(team, current, holder string, held bool)`.
- **Against dbf/hidden-bindings** (contains the picker branch, then moves `action` to
  `{id, key, label, short}` over `ui.Action` and adds the `project_hidden_bindings` rule):
  `menu.go` conflicts in the imports, the key constants, `shown()`/`line()` (gone in dbf),
  `kindActions` and `seatActions`; `sessions.go`, `sessions_test.go` and `help.txt` as above.
  The new rows must be rewritten as `action{actAskDescription, "", label, short}` and
  `action{actAskLimit, "", ...}`, `coordinatorAction`'s `(action, bool)` return and the
  takeover-last ordering re-applied in dbf's `seatActions`, and the `keys.go` `do` cases moved
  to however dbf dispatches an action ID. Both intents survive: the new lines are menu-only
  actions with no key, which is what `project_hidden_bindings` requires. Run that analyzer
  after the merge; it did not exist on this branch.
- **Against feat/hot-control-m6:** the version files (B1), the plan docs, and
  `scripts/test-ccy-lifecycle.bash`, where the same `unset PINGBUS_MEMBERSHIP` fix exists
  three times (this branch, dbf c384e235, m6 15e1e02f) and conflicts on the comment only;
  keep one.

## Versions and queue

Dockerfile label and `REQUIRED_CONTAINER_VERSION` agree (2.77.2), the changelog entry exists,
the `meta-deploy.list` entry names the play and what it does, and the md-viewer entry above it
is F44's, untouched. All coherent except B1's numbers.

## Install-specific names

None in the diff: the plan docs, journal, report and tests use `<team>`, `<handle>`,
`container-a`, `proj`, `example-org`.
