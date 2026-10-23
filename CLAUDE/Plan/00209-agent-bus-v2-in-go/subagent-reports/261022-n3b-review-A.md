# n3b slice review, part A: the orchestration

Scope: `host/lxc` apply.go, absent.go, kit.go, restart.go, probe.go, lxc.go; `admin` (adapters,
child, exit, lxc, serve, verbs); `admin/wire`; `apply/member.go`; `health` (files, online); and
their tests. Slice `0b7542a9b..0f2f54a31`, read at worktree tip `0f2f54a31`.

`go test -race -count=1` passes on `host/lxc`, `admin/...`, `health` and `apply`. Four findings
below were confirmed by reviewer probe tests, added through `go test -overlay` so the tree was
not touched. The probe sources are in this worktree's `untracked/scratch/rva_*_test.go`.

## Verdict: FIX-FIRST

The trust boundary and the n3b-D3 mapping hold on every production path. Five majors break the
offline rule's promise that the system "fixes itself": two can leave a seat failing every absent
phase or never starting, one deletes a live session's kit, one reports a change on every run, and
one is a CLI that does not match the spec.

## Checked and holding

- **n3b-D3, end to end.** `Apply` returns `errors.Join(r.errs...)` and `Restart` returns a bare
  `*OfflineError` (or that joined with a close error). `ExitCodeOf` (`admin/exit.go:24`) gives 69
  only when `decisive` finds no error that is not offline; otherwise the first one that is not
  offline decides. `lxcApply` prints the lines and `LXC-SUMMARY` before `failWith`. Tested by
  `TestLXCApplyARefusalBesideAStoppedTargetIsRefused` and `TestExitCodeOfAnOfflineMember`.
- **The record after step 06.** `recordJoins` (`apply.go:954`) writes every joined handle as
  `deferred` before any bundle is placed. `finish` (`apply.go:1195`) writes each acted team's
  record whether the phase failed or not, and skips only failed seats that set no status. A
  crash between the child's join and `recordJoins` is recovered: the next apply finds the
  handle `active` with no record entry, and `joinOf` reissues it.
- **User moves keep their handle.** `holdMoves` holds the present phase back. `undeclared`
  makes the move a leaver under the old user. `removedStatus`/`settle` rewrite the entry for
  the new user as `deferred`, with the handle kept, and a missing container's move is
  rewritten the same way (`leftOffline`). The next present phase `return`s the parked handle.
- **Probe bound and parallel run.** `Guests.Probe` (`probe.go:45`) has a 5 s
  `context.WithTimeout` and drops a late answer. `probeGuests` (`health/online.go`) runs a
  team's probes concurrently with `wg.Go`. Offline, missing, stopping, no user and no lock all
  answer Stopped or free, never an error.
- **An untargeted stopped member never fails a run.** The code routes it as offline at step 02
  (`containers`), at the pin and reads (`readsFailed`/`gone`), after an attach (`step`), on the
  guest check codes `gh`/`reach`, and on a unit that is down. The absent phase parks it from the
  host (`leftOffline`). An `*OfflineError` is raised only for `m.target`.

## Findings

### A1 (major): the absent phase parks a handle that is not active, and the child refuses it on every later run

`absent.go:126` parks whenever the entry has a handle and is not `left`. The real child's park
takes only an `active` member (`apply/member.go:69`, refused at `:306`, exit 70). Several
reachable states leave an entry that is not `left` with a handle that is not active:

- A crash, or a kill during `waitFree`'s 60 s wait, after `Park` returned but before `finish`
  writes the record. The entry stays `joined`, and the handle is parked.
- `dropSeat` failing after the park (`absent.go:139`). `fail` sets no status, so the entry is
  kept as it was.
- A `return` answered `retired`. The entry is recorded `deferred` with the handle, and the
  roster has it `retired`. Then the operator removes the entry, as the refusal text advises.
- A user move rewritten `deferred` (handle parked), whose container then stops (the entry
  becomes `offline` with the handle still parked), and whose entry is then removed.

Each time, every later absent phase fails that seat with a refusal (70), and the seat never
leaves. The tests cannot see this: `fakeRoster.Park` (`world_test.go:314`) accepts any state.

**Fix:** read `Roster.Members` under the lock in `leaveTeam`, as `joins` does, and park only an
`active` handle. Treat parked, lost and retired as already parked. Also write the record after
the parks (status `left`, as `recordJoins` does for joins), so a crash in the wait cannot lose
the fact. Make `fakeRoster.Park` refuse a handle that is not active, as the child does.

### A2 (major): `--only-teams` or `--member` lets the kit sweep delete a live hand-run session's kit

`runParents` (`kit.go:237`) lists the login run roots only for `g.members`. Those are the seats
this phase acts on and whose facts were read; `holdMoves` seats and offline seats have uid 0. LX5
asks for "every agent uid of the guest". A `service: false` seat of another team in the same
container, run by hand under `/run/user/<uid>/teamchat/run/`, is not scanned. The sweep then
removes the old digest that its keeper runs from.

Confirmed by probe (`rva_kit_test.go`): t1/agent1 and t2/agent2 share c1, and agent2 has a live
login session naming the old digest. With no scope the old kit is kept. With `--only-teams t1`
it is deleted.

**Fix:** take the uids from every declaration entry and every record entry that names the
container, whatever the scope, resolved against the passwd already read. Better still, list
`/run/user/` by the uid grammar, as `/run` is listed for the unit roots, and keep every digest
when it cannot be listed.

### A3 (major): a unit start that fails once is never retried, and the record says `joined`

`placeUnit` (`apply.go:1138-1141`) calls systemd only when the file or the link changed, or when
the prior `why` is `whyUnitDown`. Suppose `Reload` or `Start` fails with an error that is not
`ErrSystemd`, `ErrGone` or refused (a 30 s bound reached, say). `unit` then fails the seat
(`deferred`, `whyFailed`). The next apply finds the file and link unchanged, makes no call, sets
`complete`, and records the seat `joined`. The unit stays inactive until the container next
boots.

Confirmed by probe (`rva_unit_test.go`): the first run's calls were `[Reload]` with a deadline
error. The second run made no systemd calls and recorded `Status:joined`.

**Fix:** retry the start when the prior entry is not `joined`, or more simply whenever
`m.prior.Status != StatusJoined`, not only for `whyUnitDown`.

### A4 (major): a declared `state: absent` entry with no record entry prints CHANGED on every run

`leave` (`absent.go:134`) prints `CHANGED ... entry removed; it never joined` for any leaver with
no handle, including an absent entry the record never held. That is the normal steady state once
a seat has gone. The play never converges, and the summary counts 1 changed on every run.

Confirmed by probe (`rva_absent_test.go`): two runs in a row each give `1 changed`.

**Fix:** in `absent()`, skip a declared absent entry that has no record entry and print nothing,
the way `only` drops other states. Keep the CHANGED line for a recorded entry with no handle.

### A5 (major): `lxc restart` takes `--container C --key K`, but SPEC-n3 SU3, n3b-D1 and DESIGN-C say `--member C/K`

`admin/lxc.go:148-165` and `TestLXCRestart` pin `--container`/`--key`. SU3, n3b-D1 ("`lxc restart --member C/K`"), DESIGN-v2-C-host.md's member-target row and its `ccy-teams restart`
line all name `--member <container>/<key>`, the same target form as `lxc apply`. No caller exists
yet, so changing it is still cheap.

**Fix:** parse `--member` with `parseTarget`, exactly one given. Or record the departure as a
SPEC row and amend SU3, n3b-D1 and C. Do not ship both forms.

### A6 (minor): the seat-key collision check reads only the declared teams' records

`keys()` (`apply.go:354`) reads the records of `r.req.Decl.Teams`. A team dropped from the
declaration altogether (not `state: absent`) keeps its record and its live seats, and those keys
are not checked. `Restart` (`restartSeat`/`recordTeams`) already reads every record in
`LXCRecordDir`. A collision with such a seat would share the host copy
`_lxc/<c>/<key>.json`, the runtime directory and the unit's tmux socket. It needs a 64-bit
collision, so it is rare, but it is the rule as written: "every team's records read".

**Fix:** build `keys()` on `recordTeams(d.Root)` together with the declared teams.

### A7 (minor): step 03's equal-handle rule skips seats held by `holdMoves`

`equalHandles` (`apply.go:748`) skips `m.done` seats, and a `holdMoves` seat is done before its
facts are read, so `m.want` is empty. Example: a user-moved seat (c1, co, 1) whose old entry is
`left` (container stopped), and a second entry of the team at (c2, co, 1). Both derive one
handle. c2 is not refused, and it returns the parked handle. When the move completes, `settle`
writes `statusMove` as `deferred` with the same handle beside c2's `joined` entry. `Encode`
refuses the record ("two entries that are not left hold one handle"), so `finish` fails to write
the whole team's record on every later run.

**Fix:** for a held move, check the recorded handle against the other seats' derived handles.
Or refuse, at step 01, two entries of one team with equal (checkout, seat) in different
containers while one of them is a pending move.

### A8 (minor): the LX6 deferral line is SKIPPED, not DEFERRED

`offline()` (`apply.go:400`) picks SKIPPED whenever `m.handle != ""`, and that includes a handle
joined in this phase whose bundle was never placed. LX6 says "its output line stays the
`DEFERRED`". `TestStopAfterJoinThenReissueOnce` (`apply_test.go:465`) pins SKIPPED.

**Fix:** use DEFERRED when `m.joined && !m.settled` (or for `whyBundle`). Otherwise, amend LX6.

### A9 (minor): a failing `lxc-info` is read as "container stopped"

The following all treat an error from `State` or `Containers` as offline: `gone()`
(`apply.go:460`), `containers()` (`:555`), `restartRoot` (`restart.go:142`), `leaveBox`
(`absent.go:187`, where even a failed `lxc-ls` makes every leaver `left`), and `waitFree`
(`absent.go:352`, where any probe error that is not refused or `ErrNotExist` is "stopped" with
no `State` asked). A host fault in the lxc tools then shows as offline and exits 0. The present
phase treats an `lxc-ls` failure as a host fault, so the absent phase disagrees with it.

**Fix:** pass `State`/`Containers` errors up as host faults (`host.Error`). Call a box offline
only on an answered not-running state. In `waitFree`, ask `State` before answering "stopped".

### A10 (minor): `waitFree` hides a layout error as "free"

`absent.go:344` returns `(false, "", false)` when `paths.LXCLayout` fails, so the leave goes on to
`guest remove` as if the seat were free. The home comes from the guest's passwd, already
grammar-checked, so this is unlikely. It still hides an error.

**Fix:** return it, and let `leaveFailed` handle it.

### A11 (minor): the v1 holdback seam fails every seat of the container, not only the migration team's

`member()` returns the `v1NotBuilt` error (`wire/lxc.go`) as a host fault. `reads` then calls
`readsFailed` for the whole box and stops reading. Until N6, a migration-origin team in a shared
container fails the other teams' seats in that container too.

**Fix:** fail only that seat (`r.fail(m, ...)`), and continue the loop.

### A12 (minor): the probe hang test ends by cancelling, not at the bound

`TestProbesThatHangEndAtTheBound` (`probe_test.go:351`) cancels the parent context, and
`TestProbeIsBounded` only checks that a deadline exists. No test shows a hung probe returning
the deadline error, and no health-tick test runs over hanging probes; the tick's parallel test
uses probes that return. LP1 asks for "tests with one and with many hanging fakes".

**Fix:** make `ProbeBound` a `Guests` field (defaulting to 5 s), and test a small bound with 1 and
with N hanging fakes. Assert `context.DeadlineExceeded` and that the elapsed time stays under the
bound plus a small margin.

### A13 (nit): `decisive` stops at a single-wrapped join

`decisive` (`admin/exit.go:72`) unwraps only `Unwrap() []error`.
`fmt.Errorf("lxc: %w", errors.Join(off, drift))` gives 69, confirmed by probe
(`rva_exit_test.go`). No production path wraps a join today, so this is a trap for later.

**Fix:** follow `Unwrap() error` too, while the node is offline. Add the case to
`TestExitCodeOfAnOfflineMember`.

### A14 (nit): `leaveTeam` does not defer the lock release

`absent.go:116-119` releases the lock only when every `leave` returns normally. A `Release`
error then ends the loop over the remaining teams.

**Fix:** use `defer`, joining the error into `r.errs` as `joins` does.

### A15 (nit): restart's message for a missing unit misleads on a `service: false` seat

`restart.go:47` says "lxc apply places it". The apply removes the unit of a `service: false`
seat.

**Fix:** name the cause: "the seat has `service: false`", or "no unit is placed yet".

## Simplification

`member` carries ten state flags (`done`, `failed`, `joined`, `settled`, `complete`,
`heldSeat`, `target`, `recorded`, `status`, `why`). Their combinations decide `fail`, `offline`,
`membership`, `unit` and `settle` (for example `apply.go:1027`'s
`g.gone || !m.heldSeat && (m.done || !m.settled)`). A3 and A8 both come from that implicit state
machine. One explicit stage enum (`read`, `joined`, `placed`, `member`, `unit`, `done`), plus an
outcome, would make each transition's record entry and line one table.
