# N2 qa-fix review (a611205f..HEAD): adversarial review of the n2c-D12 addendum and the plan fixes

**Verdict: FIX-FIRST.** The fix holds for revocation: nothing in it can leave a token live. The
take-side look at the member-writable directory is safe. But the crashed-session path,
`Announce`, has the same defect the addendum closes for `Release`, and it is not covered
(finding 1, medium). Ordering by `since_ms` is also not a strict order: a tie or a wall clock
that steps back brings back the original open low (finding 2). Both were reproduced against
HEAD with throwaway tests, through the production `Store` methods.

Severity count: 0 high, 1 medium, 3 low, 2 nit.

The scratch tests are in `untracked/scratch/qafix-review/zz_review_test.go` (run with
`go test -overlay`; nothing in the worktree was changed).

## Findings

1. **Medium: `Announce` for a holding with no copy announces the other holding's run, so one
   run is posted twice, once under the wrong handle.** `src-go/internal/teamchat/seat/release.go:284-293`
   (`announceHeld`).

   - **Scenario.** Seat A's take for `s1` dies at step 5. A member can cause this by setting
     `sessions/s1/` to 0755, which is what `diedBeforeCopy` does. Seat B's take for `s1` then
     makes `sessions/s1/<team>`. `s1` crashes but is still restorable, and its lock is free and
     names B's run.
   - The sweep's SD3 path runs `Announce` for each holding. `announceHeld(A)` probes
     `SessionSeat(s1, team)`, which is B's lock, and posts B's run with A's `held/` bundle
     (handle `repo.1`). `Announce(B)` posts the same run again as `repo.2`.
   - Reproduced: `TestReviewAnnounceOfAHoldingWithNoCopy` records two notices, `repo.1+site.podman/held`
     and `repo.2+site.podman/held`, for run `0123456789abcdef`.
   - This is the confused state that the n2c open low names: another holding's copy taken for
     this one's. The addendum fixed it for `Release` (SX2 and SX3) but not for SD3.
   - **Fix.** In `announceHeld`, before the probe, call
     `own, err := s.mayOwnCopy(Release{Seat: seat, Session: session}, rec)` and return `err` when
     `err != nil || !own`. It reads only host-owned state, so D14 holds. Add SD3 to the addendum
     row, and add a test like the one above that asserts exactly one notice, from B.

2. **Low: ownership by `since_ms` is not a strict order. A tie or a backward clock step
   reintroduces the open low, and a tie can strand a copy.** `release.go:175` (`>=`), and
   `take.go:244` (`since_ms` is wall-clock `Now()`).

   - **Clock steps back** between A's dead take and B's take, so B gets the smaller `since_ms`.
     A's leave then sees nothing newer, waits the full 60 s on B's live keeper, and moves B's
     live copy into A's quarantine. That is the original open low exactly
     (`TestReviewClockBack`: "waited 1m0s; B's live copy still in the session: false").
   - **Tie, B released first.** B sees A (a tie) and leaves its own copy, so B posts no notice.
     A's later release finds no other holding, moves B's copy, and posts B's run under A's
     handle (`TestReviewTieBFirst`).
   - **Tie, A and B released concurrently** (the sweep and a leave, both outside the lock).
     Each one's check sees the other's `held/`, so both skip the move. The copy is stranded in
     `sessions/s1/<team>`. Because of the new ST1 refusal, it then blocks every later take by
     `s1` of that team for the life of the session, and this is the host's doing, not the
     member's.
   - The addendum's sentence "a tie goes to the other holding, whose own release moves the
     directory" holds only when the other release runs second.
   - No token is left live in any of these cases, because step 4 does not depend on the result.
   - **Fix.** Make `since_ms` strictly increasing per session and team under the allocation
     lock. In `hold`, set `since = max(now, newest SinceMS of the team's other complete holdings naming the session + 1)`, using the same `teamHoldings` scan. Then `>=` and `>` mean the
     same thing, and neither clock steps nor same-millisecond takes can invert ownership.
     Record this in the addendum, and add tests for a backward step and a tie, in both release
     orders.

3. **Low (acceptable as fail-fast, but undocumented and untested): a strange or unreadable
   sibling in the pool now stops the revocation of an unrelated holding.** `release.go:170`,
   `holdings.go:62-68`.

   - Before this diff, releasing seat A read only A's own `held/`. Now every release, every
     `clearHeld` and every take that clears an ended holding scans all seats of the team.
   - Reproduced: `TestReviewStrayPoolName` puts a file `NOT A SEAT` in the team's pool directory.
     A remove of `s1` then fails with `ErrInvalid` before step 4, and the token is still alive.
     A sibling `held/held.json` that is not a regular file has the same effect, through
     `readRegular`.
   - The pool is host-owned, so a member cannot reach this, and `Holdings` (the sweep) already
     stops on it. I judge it acceptable fail-fast, not a trust-boundary break.
   - **Fix.** State in the addendum row that a pool entry the store never makes, or a sibling
     `held/` it cannot read, stops the release before step 4, naming the entry, and that the
     owner fixes it before revocation can complete. Add a test that pins this error.

4. **Low: PLAN.md cites a report that is not in the tree.** `PLAN.md:287` and `PLAN.md:298`
   cite `subagent-reports/261022-n2-qafix.md` as the record of the fix and of the
   real-Tuwunel run. The file does not exist at HEAD; `ls subagent-reports/` has only the
   `n2-lock*` reports and `n2-qa-reviewer`.

   - The claim itself is supported: `untracked/scratch/n2-qafix-tuwunel-run2.log` has a PASS
     for every tagged test (`TestTuwunel`, `TestTuwunelConverge`, `TestTuwunelApply`,
     `TestTuwunelTakeAndRelease`, `TestTuwunelSeatWatch`, tuwunelhs ok) and no SKIP.
   - Run 1 has a `fakehs/... [setup failed]` FAIL.
   - The log predates commits 5e83ba0e and 39ce1171 (07:36 against 07:41 and 07:42).
     39ce1171 is test-only. 5e83ba0e's code was presumably in the working tree, but nothing
     records that.
   - **Fix.** Commit the qafix report with the run's commit hash and the exact command, or cite
     the log's contents in the plan.

5. **Nit: the new refusal reuses `ErrHeld`, whose doc says the seat's `held/` names a live
   session.** `take.go:19-20` and `take.go:225-237`.

   - Here the refusal is about the session, not the seat. A future seat-choice caller that
     answers `ErrHeld` by trying the next pool seat would walk every seat for nothing. Each
     attempt runs `clearHeld`, which can release ended holdings and wait up to 60 s under the
     allocation lock.
   - There is no caller of `seat.Take` yet, so this is a nit.
   - **Fix.** Use a distinct sentinel, for example `ErrSessionSeat`, or widen `ErrHeld`'s doc and
     tell seat choice not to retry other seats on this refusal.

6. **Nit: test coverage gaps.**

   - Nothing covers the clearHeld/`alreadyLocked` path with a second holding of the ended
     session.
   - Nothing covers finding 3's error.
   - Nothing covers ties in the B-first order.
   - `TestATakeRefusesASessionItCannotLookInto` shows the FIFO is never opened only by not
     hanging; a regression would be a test timeout, not a named failure. An `IN_OPEN` watch, as
     `TestAFIFOIsNeverOpened` uses, would make it deterministic.

## Checked and clean

- **Trust boundary of the take-side look.**
  - `refuseSessionSeat` uses `exists`, which opens `sessions/<s>` with `openat2`
    `O_DIRECTORY|O_NOFOLLOW` and `RESOLVE_BENEATH|NO_SYMLINKS`, then calls
    `fstatat(AT_SYMLINK_NOFOLLOW)` on `<team>`.
  - A FIFO, device, socket or file at `sessions/<s>` gets `ENOTDIR` from the `O_DIRECTORY`
    check before `vfs_open`, so it is never opened. A link gets `ELOOP`. A mode gets `EACCES`.
    Each of these is `ErrHeld` before `held/` is made or a token minted.
  - Anything at `<team>` is only `fstatat`ed.
  - A member can stop only its own session's take. It cannot affect another session, because
    the path is keyed by its own session. It cannot affect a holding's revocation, because the
    check is take-only.
- **mayOwnCopy reads only the pool**, which is host-owned. Its result gates only step 2's move
  and step 3's run source, never step 4. Concurrent takes, finishes and placements seen outside
  the lock degrade to `Complete:false` or to `held/` absent, never to an error.
- **Re-checked under the lock.** A copy another seat makes during step 1's wait is left alone
  when ties are equal or newer (but see finding 2).
- **The tests are real.** I ran each mutation through `-overlay`:
  - `mayOwnCopy` always true: 3 onecopy tests fail.
  - No re-check under the lock: `TestACopyAnotherSeatMadeDuringTheWaitIsNotMoved` fails.
  - `refuseSessionSeat` a no-op: 3 take tests fail.
  - `>=` changed to `>`: the during-the-wait test fails.
  - At HEAD: `go test -race -shuffle=on -count=3` on seat passes.
- **go-deps.** The pass line counts `len(v.keepouts)`, which is reached only when every keepout
  held. The `qa-gates.tsv:32` regex `GO-DEPS-OK [0-9]+ module[(]s[)] allowlisted .+` still
  matches. The wording fixture, G11 and `run_test`/`keepout_test` agree. `qa/godeps` and
  `qa/all` tests pass.
- **PLAN.md accuracy.**
  - `go list` confirms no importer of `declare/hostvars`, `health/view`, `notice`, `sweep` or
    `element`, and that `health` is imported only by `health/view`.
  - godbus is absent from `go.mod`.
  - Task 4.3 exists.
  - The Task 2.2 progress line matches n2d.
  - `go-deps.allowlist:44` and the hostvars doc wording are fixed.
  - The n2c open item is marked fixed, but see findings 1 and 2.

## Round 2 (39ce1171..HEAD: 97e75b80, 156dca87)

**Verdict: MERGE.** All six round-1 findings are resolved. One new nit, which does not block.

- **Finding 1 (Announce).** `announceHeld` now checks `mayOwnCopy` before probing. The check
  reads the pool only, and its error is returned (fail fast).
  - My scratch reproduction (`TestReviewAnnounceOfAHoldingWithNoCopy`) now records one
    notice, B's.
  - Mutation: removing the gate turns `TestAnnounceOfAHoldingWithNoCopyIsTheOthers` red.
- **Finding 2 (strict order).** `nextSince` is `max(now, newest other complete holding of the team naming the session + 1)`. It runs under the allocation lock and reads host-owned
  `held/` only.
  - The update is monotone, so the order the holdings are scanned in does not matter.
  - Only live holdings take part in the ordering, and every live holding is in the scan, so a
    later take always sorts above an older live holding, whatever the clock does. A holding
    that is already released cannot invert the order: it has left the scan, and it no longer
    takes part in `mayOwnCopy`.
  - Incomplete holdings are skipped. Under the lock, an incomplete holding can only be a take
    that died at step 2, which has no copy.
  - Scratch reproductions:
    - Clock stepped back: the wait is now 0 s, and B's live copy stays where it is.
    - Tie with B released first: B moves its own copy and posts its own notice once, and A's
      release then posts nothing more.
  - Concurrent tied releases can no longer strand a copy, because a tie can no longer happen.
  - Mutation: `nextSince` set to plain now turns `TestSinceOrdersTheHoldingsOfASessionStrictly`
    and `TestTheNewerHoldingReleasedFirstMovesItsCopy` red.
- **Finding 3 (pool scan).** The fail-fast is kept, recorded in the addendum and pinned by
  `TestAStrangePoolNameStopsTheRelease`. This is acceptable: the pool is the owner's alone.
- **Finding 4 (report).** `261022-n2-qafix.md` is now committed.
- **Finding 5 (`ErrHeld` doc).** The doc is widened, and it says seat choice must not answer the
  error by trying the next seat. That is sufficient while `seat.Take` has no caller.
- **Finding 6 (test gaps).** The clearHeld path, the FIFO `IN_OPEN` case and the B-first order
  each now have a test.
- **At HEAD,** `go test -race -shuffle=on -count=2` on the seat package passes.

### New finding

1. **Nit: `nextSince` can overflow.** `take.go` (`nextSince`) and `records.go:224`.
   - **Scenario.** A sibling `held.json` holds `since_ms` equal to `MaxInt64`. The decoder
     accepts it, because it checks only `< 0`. `+1` then wraps to a negative value, and the new
     `held.json` that is written no longer decodes, so a later release sends the holding to
     quarantine.
   - Only the host writes these records, so no member can cause this.
   - **Fix (optional).** Bound `since_ms` in `decodeHeldRecord`, for example to a few centuries
     past the epoch, or refuse it in `nextSince` with `ErrInvalid` naming the sibling.
