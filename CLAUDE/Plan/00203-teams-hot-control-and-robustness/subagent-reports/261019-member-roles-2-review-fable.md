# Review: fix/teams-member-roles-2 at 04163717 (Task 3.8, member roles re-merged onto F44)

Adversarial review of the `--no-ff` merge of `fix/teams-member-roles` 60584607 with F44 5ce37f5e.
Nothing in the branch was changed. Checked out detached at 04163717 in this worktree.

## Verdict

**FIX-FIRST** — one two-line drift fix (finding 1); everything else is MERGE-grade.

## Summary

The merge keeps both sides: F44's screen headings (set before the member-role dispatch, so a
handle's question is headed "member <handle>"), the typed Checkout path (`pathinput.Asker`
beside the branch's `roles.New`), the add-running-session rows (inside `lastDestructive`, so the
add lines stay together and Park/Remove/Take down stay last) and the play-lock wait (the entry,
the live role and the apply in one `ops.Locked` hold, pinned by
`TestADeclaredChangeWritesTheEntryTheLiveRoleAndAppliesInOneHoldOfThePlayLock` and
`TestAStoppedWaitForThePlayLockChangesNothing`). The LXC role persists end to end: `roles.Change`
finds the entry by probing candidates, writes it with `declare lxc-role`, sets the live role,
applies the team, and `members.py`'s `present_plan` then sees `registered[0] == member.role`, so
the next play does not revert it; a takeover demotes a declared holder's entry first, which
`parse_members`'s one-coordinator check requires. Versions 3.99.12.15 / 2.77.11 agree in the
launcher (:17, :81), the Dockerfile (:36) and the changelog ("Follows 3.99.12.14 (container
2.77.10)"). `meta-deploy.list` lines 1-21 are F44's byte for byte (`cmp`), agent-bus first then
claude-yolo, and the text is accurate (the present phase does `_sync_kit` into each container).
The `order_test.go` change hides nothing real: both normalisations match deliberate F44
behaviour (A10: only a live ccy seat gets "Add this session to another team"; Task 2.17: the
Remove seat line carries the role), the role line is still matched exactly, and the
destructive-last check still sees the Remove line. The one defect is drift the reversal of the
"coordinator leaves" default left behind: the spec's H23 clause and the function comment still
say demote is the default.

## Findings

1. **FIX-FIRST — H23 and `leaveRole`'s comment still say the old default.**
   `SPEC-hot-control.md:452-453` ("Interactively it is asked, with demote as the default") and
   `src-go/internal/teams/hot/role.go:279` ("asked on a terminal, demote by default") contradict
   the code (`x.ask(..., false)`), the branch's own addendum at SPEC line 543 ("starts on No") and
   `TestRemoveOfTheCoordinatorAsksAboutItsRoleDefaultingToNo`. Fix both lines.

   Which default is right: **No**, as the branch has it. The yes answer is a role change (the
   coordinator becomes a worker, the role goes to no one), and the owner's rule that nothing
   destructive sits where Enter lands applies to a confirm's default as much as a menu's first
   row; every other role question (`seats.ChangeRole`, the takeover offer, hot's `demoting`)
   already defaults to No, so yes here was the lone exception. F44 had no separate reason for
   yes: `git log -S` shows the `true` was introduced with the question itself in 7b5ce2e2
   (Plan 00203 M4, 2026-10-16), following the H23 clause written before the owner's rule, and
   never revisited on F44. The re-merge report's "F44 had changed that default to yes" is
   therefore inaccurate: it was always yes. The cost of No is that Enter now cancels the remove
   (exit 0, nothing changed) and the owner re-runs with `--coordinator=…`; the question's text
   says so, and that is the right trade under the rule.

2. **Advisory — the probe-failure message can lie about the entry.** `roles.go:292` lets an
   entry with `seat` and `host` set match, and `entries.of` still probes every candidate's
   checkout (the repo must be confirmed), but the error at `roles.go:367` says "it gives no seat,
   so its checkout is read". When the entry does give its seat the sentence is false and the
   advice ("give each of team X's entries its seat") cannot help. Drop the "gives no seat"
   clause, or skip the probe when host and seat are set and exactly one entry matches. Not
   blocking: the change still stops before anything is written.

3. **Note, no action** — handle-less member rows (a seat whose member ccy has not made, a
   deferred LXC member) get `memberActions` with Park and a role line built on `Handle == ""`.
   F44's `seatActions` had the same gap (no guard on either parent), so this is not a regression
   of the merge; it is the collision the re-merge report assigns to `fix/lxc-stopped-member-2`,
   and that branch's guard must land in `memberActions` and drop the role action too.

## What was checked

- Combined diff (`git show --cc`) of every conflict-resolved file: `hot/hot.go`, `hot/role.go`,
  `lxcadd/adder.go`, `picker/menu.go`, `picker/menu_test.go`, `picker/picker_test.go`,
  `ops/takedown.go`, `settings/settings.go`, `seats/seats.go`, `lxcadd/adder_test.go`,
  `pingbus/hooks.py`, the 26-10-19 journal (all five headings from both sides, in time order),
  PLAN.md (F44's two 3.8 entries, the branch's, 3.9-3.11), ACCEPTANCE files, docs.
- `order_test.go` against both parents; `addrunning.go`'s `addActions`; `board.Row.Label`.
- `roles.go`, `seats/role.go`, `hot/role.go` in full; `declare.py`'s `lxc-role` and
  `set_lxc_role` (adds a `role:` line when the entry has none; `UNCHANGED` is a pass in
  `declare.change`); `members.py` `present_plan`, the one-coordinator check and the
  demote-before-promote join order; `play-agent-bus.yml` present phase.
- `meta-deploy.list` header `cmp` against F44; the versions at each of the four sites.

## Targeted QA (all green; qa-all not run, as instructed)

- `go test -race ./internal/teams/... ./internal/declare/... ./internal/agentbus/... ./internal/ui/... ./cmd/...`: 40 packages ok.
- `golangci-lint run ./...`: 0 issues; `gofmt -l`: empty.
- `scripts/test-ccy-teams.bash`: passed 214, failed 0.
- `scripts/test-ccy-teams-command.bash`: passed 383, failed 0.
- `tests/helpers/{agent_bus,agent_bus_lxc,pingbus}/test_*.py` by module name (46 modules):
  Ran 2403 tests, OK. Plan 00201's `test_m7_check.py`: 45 tests, OK.
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK (174 checked, 75 allowlisted, 99 within limits).
