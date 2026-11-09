# n3b r2a handoff (Plan 00209 N3, slice n3b, rework r2a: host/lxc keyed per seat)

Branch `feat/bus-v2-n3b-r2`, worktree `untracked/worktrees/bus-v2-n3b-r2`. I stopped at the
context budget. The production code is reworked and builds. The package's tests are partly
rewritten and **do not compile yet**. Paths below are under `src-go/internal/teamchat/`.

## Done

- Step 1, the merge: `git merge --no-ff feat/bus-v2-n3b` had no conflicts and is committed
  (through the hook). `qa-changed --base 8bfe03610` has **not** been run yet.
- Step 2, production code, all built (`go build ./internal/teamchat/...` is clean):
  - `host/lxc/lxc.go`: `Target{Container, Key paths.SeatKey}`. `Line` gains `Checkout`,
    `Seat` and `Key`, and its `String` is `<kind>\t<team>\t<user>@<container>/<repo>-<seat>-<key>\t<why>`.
    `Join.Seat` is the entry's own seat. The `Roster.Join` doc says the child's class `retired`
    is an error that `errors.Is(apply.ErrRetired)`.
  - `host/lxc/apply.go`, rewritten:
    - Step 01: the duplicate key is (team, container, checkout, seat), using `Target`.
      `seatOf` checks the seat and the checkout before making the key, because `LXCSeatKey`
      panics. `keys()`/`keyOwners.claim` hold the per-container key rule across every declared
      team's entries and record entries not `left`, whatever `--only-teams` says.
      `r.record()` reads each record once.
    - `holdMoves`: a recorded key whose user changed prints DEFERRED `whyOldUser`, and the
      entry is kept as it is (`statusKeep`).
    - Step 03: `/etc/passwd` is read once per container. Each checkout is read once per
      container (`guestBox.checkout`, `checkoutFiles`). The `ccy.env.local` owner may be any
      user whose declared entries in the container name the checkout (`owners`). The handle is
      derived as `prefix.Numbered(seat)` into `m.want`, and a recorded handle must equal it
      (`guestfacts.SameMember(derived, recorded)`, with the new message). `equalHandles`
      refuses equal handles within a team (derived, or recorded and not left).
    - Step 05: `guest check` runs once per (container, user, team) (`r.checked`).
    - Step 06: `joinOf` chooses by roster state: none or `claimed` is add (with
      `--seat <decl.Seat>`), `parked`/`lost` is return, `active` is reissue when there is no
      record entry or a reissue is pending (otherwise nothing), and `retired` is refused by
      name. `joinedAs` checks the exact handle. A Join error that `errors.Is(apply.ErrRetired)`
      becomes DEFERRED `seat N's account is deactivated; give the entry another seat`, status
      deferred, and the phase is not failed. `recordJoins` writes the record under the lock
      before any `place`, with each joined handle deferred (`whyBundleFailed`).
    - Steps 08 and 09 run per seat. `copyOf` reads and writes the seat's own copy by `SeatID`,
      holding one seat at most, or none for a held seat. `unit`, `placeUnit` and `removeUnit`
      use the seat's unit name, file and wants link.
    - Fixes: `fail` keeps a held seat's error, where the old code dropped it silently.
      `settle` writes `joined` only for a seat that took every step (`m.complete`), so a seat
      the phase never reached keeps its entry. `statusMove` rewrites the entry for
      `m.moveTo`: deferred, the same handle, `whyMoved`.
  - `host/lxc/absent.go`, rewritten. Leavers are entries declared absent, record-only
    entries, and user moves (a declared present entry whose recorded user differs: it leaves
    under the record's user, with `moveTo` set). `dropSeat` gives the seat's copy no seat and
    the farewell. `removeUnit` runs per seat, unconditionally. `waitFree` uses
    `LXCLayout(home, GuestRuntimeDir(key), key)`. After `guest remove`, `removed()` runs
    `DeleteCopy` and then drops the entry, or rewrites it for a user move. A leaver with
    `guestfacts.ErrNoAccount` skips steps 2, 4 and 5: its unit is removed, its copy deleted and
    its entry dropped or rewritten. A missing container deletes the copy too, and a user move
    is rewritten there as well.
  - `host/lxc/kit.go`: `runParents` collects the login run root per uid, plus
    `GuestUnitRunRoot(k)` for each name in the guest's `/run` that
    `ParseGuestRuntimeDirName` accepts. The listing is `ReadDir("/run", anyName, 4096)`, and a
    refusal keeps every digest.
  - `host/lxc/restart.go`: `Restart(ctx, d, container, key)`. `restartSeat` lists
    `root+paths.LXCRecordDir`, reads each team's record, and keeps the one entry not `left`
    whose key in that container is `key`. No entry, or two, is `host.Error{Step:"record", Drift:true}` (70), raised before any container is touched. Then the existing offline and
    unit checks run, with `ResetFailed` and then `Restart` of
    `paths.SessionUnit(team, checkout, seat)`.
  - Small API additions outside host/lxc, each with its test:
    - `paths.LXCRecordDir`, which `LXCRecord` now uses (the separation test's sample list has
      it).
    - `guestfacts.ErrNoAccount`, wrapped together with `ErrRefused` by `Passwd`.
    - `guestfacts.SameMember(derived, recorded proto.Handle)`, which compares the whole handle;
      its message is "the checkout's origin changed; give the entry another seat to take a new
      handle".
    - Both packages pass their tests and golangci.

## Left

1. **Finish the host/lxc tests.** `world_test.go` is updated: the fakes take unit names,
   `guestCall.Key` is recorded, Join adds by `j.Seat`, `member()` declares `Seat: 1`, and
   there are new helpers `seatID`, `keyOf`, `unitOf`, `unitFileOf`, `wantsOf` and `lineOf`. A
   regex pass rewrote the line strings, the bus calls, `Find`, `ReadCopy` and `Target` in the
   other test files. These still need fixing by hand (`go vet ./internal/teamchat/host/lxc/`
   lists them):
   - `absent_test.go` `seatLock`: use `paths.LXCLayout("/srv/agent1", paths.GuestRuntimeDir(k), k)`.
     The `w.log` order is now `systemd Stop <unitOf>`.
   - `faults_test.go` `breakCopy`: use `paths.LXCDesiredCopy(box, keyOf(t,"t1"))`.
   - `kit_test.go`: the planted run directory is `paths.GuestUnitRunRoot(keyOf(t,"t1"))[1:]`.
   - `restart_test.go`: `Restart(..., keyOf(w.t,"t1"))`, and the unit file is `unitFileOf`.
     The no-unit test now meets step `record` when there is no record. Add a test for a
     recorded seat with no unit file (step `unit`).
   - `apply_test.go` `placedUnit` and the unit test: use `unitFileOf`/`wantsOf`. The
     membership stdin session is `lxc-<keyOf>`.
   - `lxc_test.go` `TestLineString`: add the new fields.
   - Semantics changed:
     - `TestRosterStatesChooseTheJoin`: lost is now `return`, and a handle missing from the
       roster is now an add, not a refusal.
     - `TestOtherTeamsSeatsKeptOutsideOnlyTeams`: copies are per seat now, so assert that
       t1's copy is untouched.
     - `TestAbsentKeepsTheUnitForAnotherTeam`: assert that only t1's unit is stopped and t2's
       is not.
2. **New tests the brief names.** None is written yet:
   - two seats of one user in one container;
   - two users' seats on one checkout, which the env owner check accepts;
   - the user move in both phases, including a deleted old user and a missing container;
   - the retired join as DEFERRED;
   - a phase failing in another container after a join, then the entry removed and parked;
   - equal handles refused;
   - `keyOwners.claim` with a planted collision;
   - restart's record lookup;
   - the `/run` listing in the kit sweep.
3. **Step 3, the guestroot flake (test only):** make the sweep's recorder in guestroot's fault
   test retry a real EAGAIN/EINTR before counting. Not started.
4. **Gates:**
   - `go test -race` at 90% or more per package;
   - golangci clean with and without `--build-tags tuwunel`;
   - `qa-changed` green, and also `qa-changed --base 8bfe03610`.
5. The final report `261022-n3b-r2a.md`. Out of scope: the probe (c), the admin side (d) and
   PLAN.md.
