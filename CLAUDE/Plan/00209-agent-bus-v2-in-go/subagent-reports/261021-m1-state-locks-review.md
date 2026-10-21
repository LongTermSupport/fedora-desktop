Verdict: MERGE

# Review of e7767046: bus/state locks (L1-L12)

Adversarial review, worktree `feat/bus-v2-m1-state-locks` at e7767046. Read `locks.go`, both test
files, `testdata/v1locks.py`, the author's report, SPEC-state.md section 9, the owner direction,
and v1 `inbox.py` 688-704 and 895-1040, `relay.py` `Spool.exclusive`, `membership.telling`,
`inject.Marks`, the `seat.py` constants.

What I ran (sandbox copy under `untracked/scratch/mut/`, worktree untouched):

- `go test -race -count=2 -shuffle=on ./internal/bus/state/`: ok (33 s), coverage 93.7%.
- `go test -race -count=4 -shuffle=on -cpu=1,8` over every lock test: ok (130 s), no flake.
- Mutant: `Release` without `LOCK_UN` (truncate and close only). **It survives**: the whole
  package passes (finding 1).

Checked and sound:

- Each lock is on v1's own path and kind. The v1 peer uses v1's constants and code
  (`acquire_lock`, `acquire_lock_at`, `probe_lock_at`, `read_claim_at`, `TeamState.outbox`,
  `Spool.exclusive`, `telling`, `inject.Marks`), in both directions, for every role. The watch
  key derivation is checked as a side effect: a wrong key would lock a different file and the
  busy assertion would fail.
- The blocking proofs are not vacuous. The Go peer must say `blocked` (after a failed
  `LOCK_NB`) before `held`. The v1 peer must appear as a `->` waiter in `/proc/locks`. The
  v2-waits direction asserts the injected sleep ran.
- Holder-kind and claim parsing match v1 byte for byte: the first word on a space only, ASCII
  with one U+FFFD per byte, the kind whitelist with `unknown` otherwise, and
  `seat (0|[1-9][0-9]{0,15})` as a full match.
- The `Release` order is right: truncate, then `LOCK_UN`, then close (the reverse would let a
  new taker's text be truncated).
- No fd leaks on any error return: open, flock, busy, sleep and write failures all close.
  Every open is `O_CLOEXEC`. Retries and waits end on the context. No `//nolint`, no `_ =`.
  Processes are started only through `execx`, and no test reads `time.Now`.
- L10: outbox-then-delivery, delivery twice and outbox twice return `ErrLockOrder` before
  anything is opened. Wrong order on a derived context cannot deadlock silently. LD7's limit
  (a dropped context) is stated honestly.
- LD1-LD8 are each sound. None changes what a v1 process sees on disk or in flock:
  - LD2 and LD3 refuse what would hang or mislead v1.
  - LD5 refuses claims past the year 318000.
  - LD8 matches both v1 callers.
    None is a compatibility break.

## Findings

1. **major**: `locks.go:144` (`Release`'s `LOCK_UN`), against the L12 claim in the report
   (lines 71-74).

   - Evidence: the mutant that removes `unix.Flock(fd, unix.LOCK_UN)` passes every test. L2's
     defining property is that the explicit unlock frees every duplicate of the descriptor
     (Plan 00203 D4; v1 `release` docstring). Nothing tests it: `TestALockReachesAChildOnly...`
     hands the file to a child that exits at once, and every other release has no duplicate,
     so close alone frees the lock. `TestReleaseReportsAFailureToUnlock` fails on `ftruncate`
     EBADF, not on the unlock.

   - Why major: the report and spec L12 say L2 is proven against a second process, but the
     proof does not exercise the code path it vouches for. This is the defect class this
     project names in its QA rules. A later "simplify Release to Close" would pass green and
     break `seat exec`'s handover against v1.

   - Fix: add a proof where a long-lived child holds a duplicate:

     - hand `lock.File()` through `ExtraFiles` to a peer that sleeps on stdin (the v1 fixture
       can hold it with a `hold` command);
     - call `Release` in the parent;
     - assert that a v1 peer and a Go peer each take the lock while the child is still alive.

     Re-run the mutant and see it killed. Record it in the report's mutation list.

2. **minor**: `locks.go:384-385` (`holding`) and `locks.go:405-407` (`WithTold`).

   - Evidence: there is no `defer`. If fn panics and a caller recovers (an errgroup, a
     supervisor loop), the descriptor stays open and the lock stays held until a GC
     finalizer runs. v1 uses `finally`. A v1 `recv`/`wait` blocked in the kernel would wait on
     the garbage collector.
   - Fix: close (and for told, `LOCK_UN`) in a `defer` that joins the error into a named
     return.

3. **minor**: `locks.go:348-359` and `463-476`.

   - Evidence: `WithOutbox`, `WithDelivery` and `TryDelivery` do not check `ctx.Err()` before
     the first try. With a cancelled context and a free lock they run fn anyway. `pathLock`
     (line 420) refuses a cancelled context. The API is inconsistent.
   - Fix: check `ctx.Err()` at the top of `holding` (or of both entry points), and add a test.

4. **minor**: `locks_test.go:624-635`.

   - Evidence: `TestReleaseReportsAFailureToUnlock` closes the raw fd beneath an `*os.File`,
     then calls `Release`. Its name claims the unlock failure is reported, but it passes on
     the truncate error alone (see finding 1). It also leaves the `os.File` pointing at an fd
     number the runtime may reuse.
   - Fix: rename it to what it shows (a release on a dead descriptor reports an error), or
     drop it once finding 1's proof exists.

5. **minor**: `locks.go:70-71`.

   - Evidence: `case KindUnknown: return KindUnknown` is identical to `default`. It is the
     uncovered line in `holderKind` (83.3%).
   - Fix: delete the case.

6. **minor**: `locks.go:156-161` and `348-360`; spec L4 and L7 say "after ensure_dirs".

   - Evidence: `Sync` and `WithOutbox` require the state directory to exist and never make
     it. The report's open question 2 hands this to a sibling `Team` wrapper, but nothing in
     PLAN.md or the code records it apart from that report.
   - Fix: carry it as an explicit item in the sibling's task, or the merge handoff, so that
     L4 and L7 are not marked done on this slice alone.

7. **minor**: `locks_peer_test.go:192-204` (`awaitBlocked`).

   - Evidence: the v1 waiter is found by pid in `/proc/locks`, which assumes that the procfs
     pid namespace is the test's. Under a sandbox with another procfs, it spins about 20 s of
     wall clock and then fails. This is in the report's open question 3. It is acceptable
     here (stable over 8 race runs), but the failure message should name the cause.
   - Fix: when no `->` line exists at all, fail with "no flock waiter visible in /proc/locks
     (pid namespace?)".

No blocker. Finding 1 is the only reason for FIX-FIRST. Findings 2-7 can ride along.

## Round 2

Round 2 reviewed 7a1c5e5b. These runs used a scratch copy under `untracked/scratch/mut2/`, and
the worktree was not touched:

- `go test -race -count=3 -shuffle=on ./internal/bus/state/`: ok (54 s).
- `golangci-lint run ./internal/bus/state/`: 0 issues.
- The round 1 mutant (`Release` without `LOCK_UN`) was run again under
  `TestAReleaseFreesTheLockForEveryCopyOfItsDescriptor`. The test **kills** it in both
  subtests: the go and v1 peers each say `busy unknown`, want `held:`. They fail fast in
  0.26 s, because the taker is `LOCK_NB` and cannot hang.

Round 1 findings:

1. Resolved. See the mutant run above. The parent's `Dup` (`handed`) is kept open over the
   take, so it alone would keep a mutant held, as the live child's copy does. Both are the same
   open file description, so the proof is valid for L2. The dup means the runner goroutine's
   `ExtraFiles` read and `Release`'s close never touch one fd. No race.
2. Resolved. `holding` closes in a `defer` that joins into the named `err`, on every path,
   including the new `ctx.Err()` return. `holdTold` defers close first and unlock second, so
   they run LIFO: unlock, then close. A busy try returns before the unlock defer is
   registered. There is no double close, because every `closeQuietly` in these paths is gone
   and callers pass ownership. `TestAPanicInsideALockReleasesIt` covers outbox and told.
3. Resolved. `holding` checks `ctx.Err()` first. `TestACancelledContextTakesNoBlockingLock`
   covers outbox, delivery and try.
4. Resolved. The test is renamed `TestAReleaseOnADeadDescriptorReportsAnError`.
5. Resolved. The redundant case is gone, and `holderKind` is now a single `if` with the
   whitelist.
6. Resolved as a handoff. The report's open question 2 now says that L4 and L7 must not be
   marked done until the `Team` wrappers run `EnsureDirs`. **The coordinator must carry this
   into PLAN.md at merge.** PLAN.md does not mention it yet.
7. Resolved. The fatal message now names the procfs pid-namespace cause.

New findings (nits only, none blocking):

8. **nit**, `locks_peer_test.go:500-527`.
   - Evidence: when a `t.Fatal` fires between `Sync` and the end of the test, neither
     `handed` nor `lock` is closed, so the test process leaks two fds for the rest of the run.
     The mutant run showed this path. The keeper still exits, because its cleanup closes
     stdin.
   - Fix: register `t.Cleanup` for `handed.Close` and `lock.Release` right after they are
     made.
9. **nit**, `locks.go:466-476`.
   - Evidence: `withDelivery` runs `ensureLockDir` (mkdir `relay/`) and opens the lock file
     before `holding` refuses a cancelled context. The cancelled call has that side effect on
     disk, and it is the same file v1 would create.
   - Fix: none needed. Optionally, check `ctx.Err()` before `ensureLockDir`.

Round 2 verdict: **MERGE**.
