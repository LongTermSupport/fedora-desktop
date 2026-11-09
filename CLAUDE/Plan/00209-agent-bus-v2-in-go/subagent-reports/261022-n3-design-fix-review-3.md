# N3 design-text fixes and the 2.0e decision: adversarial review, round 3

Diff reviewed: `46fd12d8..HEAD` (e4e42a5c, ab127186, 76e523e1, 18e9c68b) on `feat/bus-v2-n3`.

Verdict: FIX-FIRST

Counts: major 0, minor 1, nit 4.

## Round 2's findings

All eight are resolved as their fixes said, and no new inconsistency was found:

- The board's read-cost table now describes the L3 probe as 3.7.3 release step 1 opens it.
- B 2.5's two lists name `guest place-bundle`'s identity change.
- B 2.6 step 05 has the own-sends tier and caches a 404 or 403.
- C 3.9.2 step 07 cites B 2.3.2 and releases the lock last.
- B 2.15's "the end" row runs `restart` with the switch on and off.
- RS2 names B 2.4.2's order.
- The run record gives `state` `skipped` and `txn` null.

B 2.4.2's order (`launch-failed`, `stop`, `restart`, then `exit` or `killed`) agrees with RS2 and
with B 2.11's first row.

## Final pass

There is no stale #127, `RestartRecord`, adapter, "swapped the same way", or
"fails the apply" wording left for a missing container in the chapters, SPEC-n3 or either PLAN.md.
00194's line 118 sits in its "Was:" history, which is correct. 00194's 2.0g now points at
SPEC-n3 UH1, and 00209's carried-requirements row matches RS2. Task 4.2 carries RS3.

The diff invents no mechanism beyond the spec. The filter-inline change and the 10 s / 2 s freeze
numbers are the spec's (n3-D18 and the filter fix), and both were settled in round 1.

Neither owner gate is crossed:

- Gate A: no owner-visible name or path changed.
- Gate B: no process is given a seat password.

No install-specific names were added.

## Findings

1. **minor.** `DESIGN-v2-B-member.md:536` (B 2.3.1's file table, the seat store's "Writer"
   column). This is the third list of the store's writers. Round 2's finding 2 fixed the other
   two in B 2.5, but this one still reads "the link only; `seat set-aside` under the seat lock;
   the keeper only deletes it, after a removal that times out (2.5)". It leaves out
   `guest place-bundle`'s identity change, which LK3 ("GU3's identity change") and B 2.5 now list.
   So the table still disagrees with LK3 on the single-writer rule's exceptions. **Fix:** "the
   link only; `seat set-aside` and `guest place-bundle`'s identity change (2.13), each under the
   seat lock; the keeper only deletes it, after a removal that times out (2.5)".

2. **nit.** `DESIGN-v2-B-member.md:1526-1529` (B 2.7.3 Content). It says the related-event fetch
   is made "from a bounded in-memory cache first (2.6 step 05)". Step 05's order is ledger, then
   outbox, then own sends, then cache, then `GET`, so "cache first" misstates it. **Fix:** "made
   before the batch is judged, only after the ledger, outbox, own sends and bounded cache miss
   (2.6 step 05)".

3. **nit.** `DESIGN-v2-B-member.md:1338`. SY4a caches a 404 or 403 "for the link's life", but the
   design caches it in the 1024-entry evicting cache. An evicted entry costs one more fetch, so
   the count is not wrong, only the lifetime differs. **Fix:** either add "for the link's life"
   to B (held outside the evicting cache), or change SY4a to "cached as an unknown sender (in
   SY4a's cache)". The SPEC wording is the simpler one to change.

4. **nit.** The rewrap is still incomplete. These added prose lines run past the files' width:

   - `DESIGN-v2-B-member.md:1230` (172 characters: "so they run only when ... 2.11.)")
   - `DESIGN-v2-B-member.md:1339` (125: "transient failure holds the batch. Steps 03 to 05 ...")
   - `DESIGN-v2-C-host.md:1190` (137: "The held-seat refusal above is for pool seats; ...")
   - `SPEC-n3.md:665` (113: RS2's first line)

   **Fix:** reflow each paragraph to the file's width.

5. **nit.** `DESIGN-v2-B-member.md:1268-1270` and `:2202` (2.5's crash-point tests, and B 2.15's
   "single writer" row). LK3's test lists only `guest remove` and `seat set-aside` against a held
   seat, so these rows match the spec. The place-bundle identity case is tested in B 2.15's
   "guest operations" row. No change is needed for consistency. Optionally add "and
   `place-bundle`'s identity change (75, inode unchanged)" to the single-writer row, so that each
   exception the rule lists has its own test.

Finding 1 leaves B 2.3.1 disagreeing with LK3 (and with B 2.5). It is one cell, but by this
review's rule it keeps the verdict at FIX-FIRST. With it fixed, the diff is MERGE.
