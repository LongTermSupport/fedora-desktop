# Batch 26 review fixes

The batch 26 review (a410d64ec..e821c84f7) returned FIX-BEFORE-MERGE. This report covers the
fixes for it.

## 1. `go test ./internal/teams` red at HEAD

I reproduced it at bb545782c. `TestDispatchInternalSessionNameIsTheNameCommand` and
`...ReadsTheRegistry` both exited 1 with "the roles of team alpha's seats here: the working
directory: lstat /w". There were two causes:

- `system.Cmd.SeatRoles` resolves the checkout it is given (`filepath.EvalSymlinks` in
  `system.Here`). The tests gave it a made-up `/w/proj`.
- The role lookup read the team's status record through `system.StatusRecord`, which is
  `status.Read(paths.StatusFile(team))`: the machine's own `/var/lib/teamchat-status`. That
  path is outside `teamtest`'s `Host.TeamchatRoot`, the seam every v2 read in the test world
  is meant to go through (config.go: "test suites must not read the machine's own v2 teams").
  Four sibling wirings had the same flaw: the seat adder, `newSystem`, `newLaunch`, `teamLife`
  and `newSweep` (five call sites in all).

**What "not there" should mean, judged:**

- A missing seat-records dir (the pool) already means no seats, so no roles
  (`PoolSeats`: ErrNotExist gives nil).
- A missing status record already means no roles.
- A launch checkout that does not exist stays an error, for fail fast. The launcher runs in its
  checkout, and a pool key comes from the resolved path. A new test pins this case.

**Defence Before Fix.**

- Class: a teamchat machine record read at its fixed path in ccy-teams, so a test run reads the
  machine's own teams.
- No existing rule covered it. `hostnameread`, `transporthome` and `recordfile` judge other
  things.
- New rule `project_teamchat_root_read` (`src-go/qa/rules/teamchatroot.go`), catalogued in
  `CLAUDE/GoProjectRules.md`.
  - Red on its fixture: 6 findings.
  - Red on the real tree: 1 finding, `system/system.go:205`.
  - The sweep found no other instance.
  - Green after the fix: 0 findings.
- It is blocking because it is in `rules.All()`, which go-qa runs.
- The other half of the class cannot be expressed as an analyzer: a unit test resolving a made-up
  path on the real filesystem is a runtime property. That is said under "Not caught".
- Still open, for the coordinator to decide: `internal/sessions` and `internal/conversation`
  also read through `system.StatusRecord`. Neither is given a teamchat root, so they are outside
  the rule's scope as written. This is not a narrowing I chose to hide.

**Fix.**

- `system.StatusUnder(teamchatRoot)` was added. `StatusRecord` is now `StatusUnder("")`.
- All five teams wirings use `StatusUnder(h.TeamchatRoot)`.
- session-name uses the wired `a.System.SeatRoles`, no longer a second `newSystem` built at
  dispatch.
- The two tests now give a real checkout (`h.Root`).
- New test `TestDispatchInternalSessionNameReadsRolesUnderTheTeamchatRoot` checks the role read
  under the world root and the refused missing checkout. It is red with the fixed path
  (`ccy-alpha-proj-4`) and green with the root.
- New test `TestStatusUnderReadsUnderItsRoot`.

The Task 4.2 report's claim that `internal/teams/...` passed has been corrected in place.

CCY: `internal/teams/system` is in the claude-yolo front's `go list -deps`, so CCY_VERSION goes
3.124.98 → 3.124.99, with a changelog entry. The image is unchanged: none of these packages is in
`image-source-paths` or the builder stage.

## 2. meta-deploy.list

- The play-claude-yolo entry now covers:
  - container 2.84.96/98/99: the receipt queue keeping each room's latest, the drop ledger and
    bounces, the owed-receipt sender;
  - 3.124.94: a plain cc joins its seated team;
  - 3.124.97: restore starts team seats, and sessions are named by role;
  - 3.124.99;
  - what to check afterwards.
- The play-teamchat entry gains Plan 00267 Phase 2 for the host kit and the LXC copies, and the
  drill label's off-state behaviour.

## 3. Should-fix

- `drillverb.go`:
  - The port refusal now says "--port is the port the drill's scratch team listens on". A new
    assertion in `TestRestoreDrillUsage` was red first.
  - The help text and the function doc no longer say "declared drill port".
  - The same goes for the comments in `drill.go` and `drillrig_test.go`.
- `play-teamchat.yml`:
  - The header comment now says the toggle only labels the port. teamchat-admin reads neither
    setting, so the toggle neither allows nor refuses a drill.
  - The seport task's state is now present when the drill is on and absent when it is off. When
    off, it runs only if `teamchat_restore_drill_port` is still set and no declared team uses
    that port, so a team's label is never removed.
  - `scripts/qa-ansible.bash` is green.
  - `qa-ansible-syntax` cannot run in the container: it needs a vault password file.

## Checks run

- `go test -race ./internal/teams/...`: ok. Also run: `qa/rules`, `internal/teamchat/admin/...`,
  `restore/drill`, `sessions`, `conversation`.
- `go vet`: ok.
- `golangci-lint`: 0 issues, on the touched packages and `qa/rules`.
- Project rules (driver as vettool) over teams, admin, drill, sessions and conversation: 0
  findings.
