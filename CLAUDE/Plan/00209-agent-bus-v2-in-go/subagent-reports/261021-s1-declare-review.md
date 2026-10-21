# Stage 1 review, round 1: in-process declare reads, seats, V40-V42

Scope: `git diff f460abb7...HEAD -- src-go scripts` on `feat/bus-v2-s1-declare`, against
SPEC-view.md V1-V42 and B-S1-B-S6. Adversarial review; no code changed.

## Runs

- `cd src-go && go test -race ./internal/bus/... ./internal/declare/... ./internal/agentbus/... ./internal/teams/...`:
  all 30 packages `ok`, exit 0.
- `bash scripts/test-ccy-teams-command.bash`: `passed: 383  failed: 0`, exit 0.
- Extra host_vars fixtures (in a scratch copy, removed afterwards) run through
  `TestParityWithDeclarePy`: JSON strings with `\u` escapes and surrogate pairs, comments after
  quoted values, `a#b`, a tab after a colon, an empty `description:`, flow and block `repos`,
  `exclude` as a string, a list and a mapping, `path_prefixes` as a string, `org: 0`, a scope ID
  bus address, IPv6 bus addresses, an indentation mismatch, `>`/`|` scalars, element
  `base_url` as a list or a number. 2200 cases: every answer matched declare.py except
  `::ffff:192.0.2.1`, which is B-S5 (this container's python3 is 3.11).
- Seat reader against v1's `cli.main(["seat","list"])` on member.json in the shape
  `admin._bundle` writes (IPv4, IPv6, a scope ID, https with `limits: {}`, pretty-printed with a
  trailing `/`): identical but the IPv4-mapped host, which is B-S5 again.
- Mutation runs (each in the scratch copy, restored after):

| Mutation                                                         | Result                                       |
| ---------------------------------------------------------------- | -------------------------------------------- |
| drop `p.Board.HoldTeams()` (picker.go:129)                       | killed (the V42 open test)                   |
| drop `o.changed()` after the play (ops.go:280)                   | killed (three ops tests)                     |
| `ForgetTeams` a no-op (collect.go)                               | killed (two board tests)                     |
| drop each of the host_vars, installed, stamps, bridge, pin keys  | killed (verdict_test, `TestRealOverview...`) |
| verdict never reused                                             | killed                                       |
| drop `h.Declare.Edited = b.ForgetTeams` (app.go:109)             | **survived**: no test fails                  |
| drop the same line in the picker rig (picker/helpers_test.go:67) | **survived**: no picker test fails           |

The V40 key was checked against declare.py `status` and everything it reads: `_read(--file)`,
`read_stamps` (every `*.json` in the applied directory), `bridge.live_subnet`,
`declared_software` (the pin file, the installer, `bundle.build` over the `*.py` of
`helpers/pingbus` and `helpers/agent_bus`), `--installed`, and `teamfile.parse_team_file` /
`config_digest`, which read nothing from disk. Every file input is in the key. Invalidation
was checked over every `declare.Client` edit (`edit()` calls `Edited` for create, describe,
add-repo, element, drop-limit, set, remove, lxc-add, lxc-role) and every ops flow
(`defer o.changed()`, and `o.changed()` right after the play).

## Findings

1. **should-fix: the wiring V41 depends on for an edit made during the start-up is not tested.**
   `src-go/internal/teams/app.go:109` (`h.Declare.Edited = b.ForgetTeams`) is the only thing that
   drops the start-up's shared read after the start-up offers edit the host_vars
   (`offerPathLimits` calls `DropLimit`, offers.go:150; `offerDescriptions` calls `Describe`,
   offers.go:263). Then `offerUpdate` calls `Board.Refresh` (offers.go:318), and the first
   `Collect` reads too. Delete the line and every test still passes. The picker rig copies the
   wiring by hand (picker/helpers_test.go:67), and deleting that copy also fails no picker test.
   Without the wiring, a description set at the start-up would leave the update offer and the
   first draw using the read made before the edit: the old description, and an APPLIED
   `current` when declare status would now say `outdated` (the description is part of the
   config digest). The code is right today. Nothing stops it regressing.
   Fix: move the wiring into one function that `Wire` and the picker rig both call (or build the
   picker rig with `teams.Wire`), and add a picker test where the start-up describes alpha
   (`OnDeclareEdit` writes the new description, and the status script changes to `outdated`
   after it). The test asserts that the update offer is made, or that the first draw shows the
   new description and verdict, and that `declare.py status` ran twice.

2. **note: the V42 open test counts a filtered set of processes.** `teamReads`
   (picker/opencost_test.go:13-21) keeps only `systemctl`, `declare.py *` and `agent-bus (user) *`.
   A Python read started any other way (`python3 -m ...` directly, keyed `python3` by
   `World.Spawned`) would not be counted. The spec words ("for team reads") allow this, and an
   unscripted command fails the fake anyway. The board redraw test pins the full map. Fix wanted:
   pin the open's full `Spawned()` map too (systemctl, declare.py status, git, tmux, the sudo
   reads), so any new process fails it.

3. **note: the V40 key leaves out what declare.py runs on but does not read as data.** declare.py
   imports `helpers.agent_bus_lxc.{derive,members}` at module level (declare.py:140), and these
   are outside `verdictTrees`. The python3 interpreter is also not in the key: teamfile
   canonicalises `allow_from` with `ipaddress`, whose output changed in 3.13 (B-S5). Neither can
   turn a CURRENT into an OUTDATED within one ccy-teams session in practice: one can only make
   status crash, the other is a python upgrade while ccy-teams is open. It is worth one line in
   B-S1, or add `helpers/agent_bus_lxc` to `verdictTrees`, which costs nothing.

4. **note: the start-up's shared read does not consult the V40 key.** `readTeams` returns
   `startup.read` without calling `Overview` (collect.go). A change made outside this process
   while a start-up prompt waits (a hand edit of host_vars, another ccy-teams applying) is not
   seen until the first `Collect`. V41 names only in-process operations as invalidators, so this
   is within spec. Say it in V41 or B-S1 so it reads as a decision.

5. **note: two test pins changed, and both are justified.**

   - wizard_test `TestYesWithEveryValue...` no longer pins that the next steps come after the
     install. It still pins their text, and `TestAFailedInstallSaysHowToRetry` pins their absence
     after a failed install.
   - `TestAFlagValueMayLookLikeAFlag` now expects exit 64 where it expected 0. The old fake
     answered `next-steps` for a team no file declares. Real declare.py refuses it with 64, so
     the new pin is v1's behaviour.

   The 15 declare_test and 3 lxc_test tests removed parsed Python marker lines that no longer
   exist. Their behaviours are re-proven over real files in reads_test.go, and
   `TestACommandThatCannotRunIsNotAnError` moved there. The teamtest world now refuses to script
   an in-process read (`OnDeclare` panics on it), and the tests read real host_vars and seat
   folders. This is stronger than before.

No drift from v1 was found outside B-S1-B-S6 in the declare reader, the seat reader or
`SeatList` (stderr still goes to `Err` with the `agent-bus: ` prefix; a whole-list refusal and
a seat refusal are unreadable, not errors; a v1 crash is still an error). The process-count
claims of V42 are proven for systemctl, declare.py and the user's agent-bus, and the hold is
mutation-checked.

VERDICT: FIX-FIRST

## Round 2

Scope: commit 267069c3 (the round 1 fixes), then `git diff f460abb7...HEAD -- src-go scripts`
hunted again. No code changed: each mutation was made in the worktree, run, and restored,
and `git status` was clean afterwards.

### Runs

- `cd src-go && go test -race ./internal/bus/... ./internal/declare/... ./internal/agentbus/... ./internal/teams/...`:
  all 30 packages `ok`, exit 0.
- `bash scripts/test-ccy-teams-command.bash`: `passed: 383  failed: 0`, exit 0.

### Mutations repeated against the fixes

| Mutation                                                                    | Result                                                                                                    |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- |
| drop `h.Declare.Edited = c.ForgetTeams` from `board.Wired` (collect.go:91)  | killed: `TestAnEditOfTheHostVarsDuringTheStartUpIsReadAgain`                                              |
| a stray `p.Board.Tmux.List(ctx)` before `offerPathLimits` (picker.go)       | killed: `TestAnOpenWithNothingToOfferAndARedrawStartFewTeamReads` (`tmux:2`, want `tmux:1`)               |
| drop `helpers/agent_bus_lxc` from `verdictTrees` (verdict.go:26)            | killed: `TestEachInputOfTheVerdictChangedRunsItAgain/an_agent_bus_lxc_file's_size` (status ran 1, want 2) |
| `teams.Wire` builds `board.New(h)` instead of `board.Wired(h)` (app.go:106) | survived (finding 1)                                                                                      |

Round 1's findings 1-3 are closed: the wiring now has one home and a test that is red without
it, the open's whole `Spawned()` map is pinned (so a stray process of a scripted kind fails it;
an unscripted kind already failed the fake), and a change under `helpers/agent_bus_lxc` makes
the verdict run again. `treeKey` skips `__pycache__`, so byte-compiling that tree does not
invalidate. Finding 4 is closed by the V41 sentence.

### Re-hunt

- The hold (`HoldTeams` .. first `Collect`) is reached only from `pickerSession`. Every edit
  the start-up can make is followed by `ApplyTeams` (`offerPathLimits`, `offerDescriptions`),
  whose deferred `changed()` drops the shared read, or by leaving (`offerCreate` returns
  `flow.Leave(p.Create.Run(...))`; a failed `DropLimit`/`Describe` leaves). `offerUpdate`'s
  `Apply` pre-check uses the shared read, as v1's pre-check used the read made just before;
  its post-check is fresh, since `runPlay` now calls `o.changed()` (round 1 mutation-checked).
- The shared `teamsRead` is only read by `collectTeams`: `addDeclared` and the status loop copy
  values into the snapshot and never write the shared slices, so one pass cannot corrupt the
  next.
- Non-picker commands (`apply`, `--list`, `remove`) never hold, so they read every time, as v1.
- `scripts/test-ccy-teams-command.bash`: the removed lines are the fake `seat list` and its
  `$STATE/seats` files, replaced by real seat folders the Go reader reads; no assertion was
  removed.

### Findings

1. **note: `teams.Wire`'s use of `board.Wired` is not pinned.** Reverting app.go:106 to
   `board.New(h)` fails no test: the board test calls `Wired` itself and the picker rig calls
   `Wired` rather than `teams.Wire`. It is not observable today, because every start-up edit is
   followed by `ApplyTeams` (its deferred `changed()`) or by leaving, which the builder's
   commit message also says. It becomes observable only if a start-up offer ever edits without
   applying. Wanted fix (optional): one `app_test` assertion that, after `Wire(w.Host)`, a
   declare edit drops the board's held read (or build the picker rig with `teams.Wire`).

No blocker or should-fix remains. No behaviour drift from v1 was found outside B-S1-B-S6, no
test was weakened, and the V42 process counts are now proven over every process the open and
the redraw start.

VERDICT: MERGE
