# N3 design-text fixes and the 2.0e decision: adversarial review, round 2

Diff reviewed: `46fd12d8..HEAD` (e4e42a5c, ab127186, 76e523e1) on `feat/bus-v2-n3`.

Verdict: FIX-FIRST

Counts: major 0, minor 3, nit 5.

## Round 1's findings

All 16 are resolved as their fixes said:

- RS2 now sends any other end to B 2.11's table. It says the switch is one constant in the link,
  shipped off and turned on by RS3's commit, and tested in both positions with RS1's test pinning
  the shipped value.
- B 2.4.2 and B 2.11's `restart` row agree with RS2. The row is now first. The ccy `/exit` kinds
  row and both B 2.15 rows match it.
- 00194 Task 2.0e names Tasks 3.1, 3.2 and 4.2, and 00209 Task 4.2 carries RS3.
- C 3.7.x reissue follows n3-D1, C32 and step 02 follow n3-D12, step 05 names `check`'s stdin, and
  step 08 leaves a `DEFERRED` seat out (LX7).
- B 2.10 names `missing`. B 2.15 has GU3's place-bundle cases. Step 07 covers an undecodable
  `seat.json`, and B 2.5 cites C 3.7.3.

Two fixes left loose ends, findings 3 and 4 (the 1024-entry cache in B 2.6 step 05) and finding 5
(the rewrap). Nothing applied needs Gate A or Gate B. No install-specific names were added. No
stale #127, `RestartRecord` or adapter wording is left in the chapters, SPEC-n3 or either PLAN.md;
00194's line 118 is inside its "Was:" history and is correct there.

## Findings

1. **minor.** `DESIGN-v2-C-host.md:2375` (the board's read-cost table, row "the seat store: pool
   and session directories"). It still describes the L3 probe as "`openat2` with
   `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS` from that directory's fd, `O_NONBLOCK` and `fstat`
   (3.7.3)". That is the read-only, non-blocking open that fix 04 (n3-D17, TB1) removed from 3.7.3
   release step 1, because on a FIFO it makes the owner a reader. The grep for the old flags missed
   it. **Fix:** "...the L3 probe of the seat lock in the holding session's directory, as 3.7.3
   release step 1 opens it (`O_PATH` with
   `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS`, `fstat`, then a reopen through
   `/proc/self/fd/<n>`)".

2. **minor.** `DESIGN-v2-B-member.md:1227-1228` and `:1250-1251` (B 2.5). Both lists of who
   touches seat state are still the pre-n3-D1 lists. "The only other writer is `seat set-aside`"
   and "every deletion of seat state (the link's removal step, the keeper's deletion after a
   timed-out removal, `guest remove`, `seat set-aside`)" leave out `guest place-bundle`'s identity
   change. Fix 01 made that a store set-aside under the seat lock (B 2.13, C 3.9.2 step 07), and
   SPEC-n3 LK3 lists it ("GU3's identity change"). So B 2.5 and LK3 disagree on the single-writer
   rule's exceptions. **Fix:** at :1227, "The only other writers are `seat set-aside` and
   `guest place-bundle`'s identity change (2.13), which set the store aside under the seat lock,
   so they run only when no session holds the seat". At :1251, add "`guest place-bundle`'s
   identity change" to the parenthesis.

3. **minor.** `DESIGN-v2-B-member.md:1332-1335` (B 2.6 step 05, round 1's fix 14). The design
   resolves a related event "from the ledger and outbox, then from an in-memory cache ..., then by
   one bounded `GET`". SY4a (and gap 11) has a tier between them, "the member's own sent events
   (each own send, `say`, `reply`, `ping` and notices, recorded at its send, n3d)". SY4a's first
   test, "a 'Reply in thread' under this member's own `say`" with a store holding no record,
   relies on that tier. B nowhere names an own-sends record, so the design and the spec list
   different lookup orders. SY4a also caches a 404 or 403 as an unknown sender, and step 05 does
   not say so. **Fix:** "...resolved before the batch is judged: from the ledger and outbox, then
   from the member's own sends (each `say`, `reply`, `ping` and notice, recorded in memory at its
   send), then from an in-memory cache (...), then by one bounded `GET ...`. A 404 or 403 is cached
   as an unknown sender; a transient failure holds the batch."

4. **nit.** `DESIGN-v2-C-host.md:1738` (step 07). "(B 2.13 `seat set-aside`)" cites the wrong
   section: `seat set-aside` is defined in B's verb table and B 2.3.2, and B 2.13 only points at it.
   The step also does not say that the lock is released only after the files are placed (GU3:
   "releasing the lock last"). **Fix:** "...takes the seat lock, sets the store aside as `seat set-aside` does (B 2.3.2) and places the files, releasing the lock last; it exits 75 if the seat
   is held, ...".

5. **nit.** Rewrap is still incomplete. Round 1's nit 16 moved some long lines without wrapping
   them:

   - `DESIGN-v2-B-member.md:761` is 154 characters.
   - 00209 `PLAN.md:366` is 106.
   - `DESIGN-v2-A-protocol.md:1033` ("step 05). A related event") and `DESIGN-v2-C-host.md:1190`
     ("it). The held-seat refusal above is for pool") are ragged short lines.
   - Fix 04 added prose lines at `DESIGN-v2-C-host.md:1255`, `:1258`, `:1261` and `:1668` that
     run past the files' width.

   **Fix:** reflow each paragraph to the files' width.

6. **nit.** `DESIGN-v2-B-member.md` B 2.15 row "the end (2.4.2, 2.11)". It lists `restart` among
   the ends but not RS2's "run in both positions through a test seam". **Fix:** after "against each
   SessionEnd reason" add "(the `restart` row with its switch on and off)".

7. **nit.** `SPEC-n3.md:664-666` (RS2). The spec says that a CMD exit of 75 makes the end
   `restart`, but not that B 2.4.2's order puts `launch-failed` and `stop` first, so a stop whose
   CMD then exits 75 is still `stop`. The design is right and the spec is only less specific.
   **Fix:** "when CMD exited 75 and no stop was forwarded (B 2.4.2's order), the end is
   `restart`".

8. **nit.** `DESIGN-v2-B-member.md:1849-1850` (the run record). "`notice` `none` with `reason`
   `restart`" leaves out `state` `skipped` and `txn` null, which B 2.3.2 and n3-D2 give. **Fix:**
   "or `{"notice": "none", "reason": "restart", "state": "skipped", "txn": null, "at_ms": N}` for a
   ccy relaunch with the switch on (2.3.2)".
