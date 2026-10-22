# Plan 00209 N3: the 2.0e decision and the design-text fixes

Branch `feat/bus-v2-n3`, from 46fd12d8. Plan-document edits only. No code was written, and nothing
was run on the host.

## Commit 1 (e4e42a5c): 2.0e trusts CMD exit 75 alone

- **SPEC-n3.md section 8.** RS1-RS3 no longer depend on hooks-daemon #127.
  - The switch is ccy's CMD exiting 75 with no stop forwarded. The end is recorded as
    `ended.notice: none`, reason `restart`.
  - The accepted risk is stated: a Claude Code that itself exits 75 is also taken as a restart.
  - n3c ships the switch off, as one constant in the link. RS3's commit (Task 4.2) turns it on.
    The table test runs both positions, and RS1's test pins the shipped value as off.
  - One line says #127's record can be added as a second signal once a released hooks-daemon
    carries it.
  - n3c's slice row says "the restart switch, off" instead of "the `RestartRecord` seam".
- **00209 PLAN.md.** The 2.0e row in "Requirements carried" and Task 3.1's 2.0e line no longer
  wait on #127. Round 1 added RS3 to Task 4.2.
- **00194 PLAN.md Task 2.0e.** Its first line now says the task is specified in 00209 Task 3.1,
  built in Task 3.2 and turned on with Task 4.2, and that #127 no longer gates it. The "Was:"
  history and the JOURNAL are unchanged.

## Commit 2 (ab127186): the 15 fixes

| Fix | Applied where                                                                                                                                                                                                                                         |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01  | C 3.9.2 step 07, B 2.13 `place-bundle` row, B 2.13's exit list (75 for `place-bundle`); round 1 also covered C 3.7.x reissue, round 2 covered B 2.5's writer lists, round 3 covered B 2.3.1's table                                                   |
| 02  | B 2.6 step 02: the filter is sent inline (n3-D5)                                                                                                                                                                                                      |
| 03  | C 3.11.1: `RESOLVE_IN_ROOT` (n3-D14)                                                                                                                                                                                                                  |
| 04  | C 3.9.2 step 03 (reads, and systemd as init by device and inode), the L3 probe in C 3.9.2, C 3.7.3 release step 1 (the ccy probe); round 2 covered C's read-cost row                                                                                  |
| 05  | C 3.9.4: the unit's path, its absolute wants link, the private socket opened `O_PATH` through the guest root and connected through `/proc/self/fd`; H-C9 updated to match                                                                             |
| 06  | C 3.9.2 step 03 and C 3.7.4: the handle is derived at the first join only, and a changed origin is refused (n3-D10)                                                                                                                                   |
| 07  | K10: the init is pinned by a pidfd (n3-D7)                                                                                                                                                                                                            |
| 08  | C 3.9.2 step 04: both runtime parents are read (n3-D8)                                                                                                                                                                                                |
| 09  | C 3.9.1, C 3.9.2 absent phase, step 08, step 10 (n3-D12, n3-D13); round 1 covered C32 and step 02                                                                                                                                                     |
| 10  | B 2.13 `check`: stdin and `gh auth status` (n3-D6); round 1 covered C 3.9.2 step 05                                                                                                                                                                   |
| 11  | B 2.3.2 `run.ended` `none`, B 2.3.3 `missing`, B 2.4.1 the missing `desired.json`; also the `restart` end in B 2.4.2, B 2.11's table, its ccy kinds row and B 2.15's rows; round 1 covered B 2.10                                                     |
| 12  | A 7.5: `empty` and `type` count as unheard at every member, judged over the roster's agents (UH1); the cache bound moved to B 2.6 step 05 in round 1. n3-D20 records the departure from B 2.7.3's "only re-fetch", and B 2.7.3 names the second fetch |
| 13  | B 2.5: the seat lock is tried three times too                                                                                                                                                                                                         |
| 14  | B 2.4.5's freeze row: the thresholds (n3-D18)                                                                                                                                                                                                         |
| 15  | 00194 PLAN.md Task 2.0g points at 00209's N3 and SPEC-n3 UH1                                                                                                                                                                                          |

## Held for the owner

None of the fixes was held:

- None changes an owner-visible name or path (Gate A). Fix 05 names the unit's file path inside
  the guest, which is the standard location for the unit name the design already had.
- None gives a process a seat password (Gate B).

Code named by fixes 01 and 04 (`seat.PlaceGuest` and `seat/lockprobe.go`) was not edited. That
work belongs to the build, as GU3 and TB1 say.

## Review rounds

| Round | Report                             | Verdict   | Findings                 | Fixed in                        |
| ----- | ---------------------------------- | --------- | ------------------------ | ------------------------------- |
| 1     | `261022-n3-design-fix-review.md`   | FIX-FIRST | 0 major, 11 minor, 5 nit | 76e523e1                        |
| 2     | `261022-n3-design-fix-review-2.md` | FIX-FIRST | 0 major, 3 minor, 5 nit  | 18e9c68b                        |
| 3     | `261022-n3-design-fix-review-3.md` | FIX-FIRST | 0 major, 1 minor, 4 nit  | the commit carrying this report |

- **Round 3.** The reviewer said the diff is MERGE once its one minor is fixed. That minor was
  B 2.3.1's store-writer cell, which omitted `place-bundle`'s identity change.
- **Fixed after round 3:** the minor and nits 2-4. Nit 2 was B 2.7.3's lookup order. Nit 3 made
  B's caching of a 404 or 403 last for the link's life, as SY4a says. Nit 4 was the rewrap.
- **Not fixed:** nit 5 was optional and left out. Adding a place-bundle case to the single-writer
  test row is not needed for consistency, because the guest-operations row tests it.
- **Not re-reviewed:** the round-3 fixes, because of the three-round limit.
