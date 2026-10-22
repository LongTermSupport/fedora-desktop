Verdict: MERGE

Narrow check of the round-2 fixes on feat/bus-v2-n1-decisions: `8d19d829..c3b01452`. The
round-2 review (`261021-n1-decisions-review-2.md`) was committed in c3b01452 together with its
fixes, so the fix diff is that commit's changes to PLAN.md, DESIGN-v2.md and DESIGN-v2-C-host.md.

## The 13 findings

| #   | Finding (review 2)           | State          | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| --- | ---------------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R1  | restore keyed on `cred`      | VERIFIED-FIXED | The residue was new finding 1, fixed below. 7.1's restore bullet now runs `member reissue --credential pool_token`, so the reissued seat records `cred` `pool_token` again and the merge rule keeps catching it.                                                                                                                                                                                                                                                                                     |
| R2  | hot reissue step 2           | VERIFIED-FIXED | Unchanged from round 1. It writes `held/token.new` and renames it over `held/token`, with no login. This matches C 3.7.3 step 2's write discipline.                                                                                                                                                                                                                                                                                                                                                  |
| R3  | gate B placement, H-C ranges | VERIFIED-FIXED | The residue was new finding 2, fixed below. Gate B is on Tasks 2.5 (host case), 4.4 and 5.1.                                                                                                                                                                                                                                                                                                                                                                                                         |
| R4  | gate A binds at the merge    | VERIFIED-FIXED | Task 0.5 and Tasks 2.5 and 4.1 bind it at the merge. Task 2.5's residue was new minor 4, fixed below.                                                                                                                                                                                                                                                                                                                                                                                                |
| R5  | the "nothing else" list      | VERIFIED-FIXED | 7.1's closing list now starts with "C 3.6.3's `--credential pool_token` and the roster's `cred` value". It names take steps 3 and 4 and the hot reissue's steps 1 and 2.                                                                                                                                                                                                                                                                                                                             |
| R6  | guest token copy             | VERIFIED-FIXED | The child now knows the seat kind from the verb: under `pool_token` it "writes no `ctl/<team>/guest/<handle>.token` copy".                                                                                                                                                                                                                                                                                                                                                                           |
| R7  | restore wording, H-C13       | VERIFIED-FIXED | Unchanged from round 1. It was correct then.                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| R8  | d1 and d2                    | VERIFIED-FIXED | The step numbering residue was new minor 3, fixed below.                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| N1  | `pool_token` has no source   | VERIFIED-FIXED | Option (a) was taken. DESIGN-v2.md 7.1's first bullet adds `--credential pool_token` to C 3.6.3's verbs, which ccy-teams passes in place of `password`. It records `cred` `pool_token` (C 3.6.1) and writes no guest copy. The restore recovery passes `pool_token`. The C-host DC16 (d) bullet at line 1270 now says "a new `--credential pool_token` (3.6.3)". The C 3.6.1 and 3.6.3 text itself is untouched, which is correct because (d) is not built unless chosen. 7.1 lists both as changes. |
| N2  | H-C15 cannot run in Task 2.5 | VERIFIED-FIXED | Task 2.5 excepts "H-C15's clause that a held ccy session works again after ccy-teams' hot reissue, which needs N3 and N4 and is Task 4.4's; restore stays refused (DC8) until that clause passes too". Task 4.4 now opens with "H-C15's held-ccy-session clause (Task 2.5)". Task 0.5's gate B sentence reads "H-C15 without its held-ccy-session clause" for 2.5 and gives "that clause" to 4.4.                                                                                                    |
| N3  | take step numbers in d1, d2  | VERIFIED-FIXED | d1 now reads "Take step 3 makes no login: it checks the pool's `token` with `whoami`, and step 4 writes it as `held/token`". d2 now reads "take step 3 then makes no login and no `whoami` ..., and step 4 writes it as `held/token`". Both match C 3.7.3 steps 3 and 4, where step 3 is the login and step 4 writes `held/token`.                                                                                                                                                                   |
| N4  | Task 2.5's gate A too narrow | VERIFIED-FIXED | Task 2.5 now covers "the play tasks and units of Tasks 2.2 to 2.4 (and of any later task, such as 3.3)". Task 3.3 has its own gate A line. Task 0.5's list now reads "Tasks 1.4, 2.5, 3.3, 4.1".                                                                                                                                                                                                                                                                                                     |
| N5  | hot reissue step 1 under (d) | VERIFIED-FIXED | Step 1 now writes "the answer's token as `token.new-<claim>` (\`O_EXCL                                                                                                                                                                                                                                                                                                                                                                                                                               |

## The coordinator's answers, the gates, and 7.1 against C 3.7

- **Recording.** The fix diff does not touch the Decisions table or Task 0.5's Accepted and
  Irreversible lists.
  - The VISIBLE row is unchanged and names all 15: OD-A10, BD9, BD15, BD16, DC4, DC16, DC17,
    DC18, DC19, DC21, OD-D6, INT4, INT5, INT7 and DC8.
  - The IRREVERSIBLE row and TID's row carry "accepted with gate A". They name OD-A7, DC1, DC5,
    DC15, OD-D1, OD-D5 and TID.
  - INT6 is "owner only, with gate B".
  - DC8's condition ("comes back to the coordinator if H-C5 or H-C15 fails") still holds. H-C15
    now completes only at Task 4.4, and Task 2.5 says that restore stays refused until then.
- **Gate placement.**
  - Gate A is on Tasks 1.4, 2.5 (covering 2.2 to 2.4 and later tasks), 3.3, 4.1 and 6.2.
  - Gate B is on Tasks 2.5 (host case), 4.4 and 5.1.
  - No pool-seat deploy and no `teamchat`-named play task or unit is left ungated.
- **7.1 against C 3.7.3.**
  - (d1) and (d2) now follow C 3.7.3's step order: step 1 checks the record, step 3 is the login
    or what replaces it, and step 4 writes `held/token`.
  - The hot reissue's steps 1 and 2 match the rename discipline of C 3.7.3 steps 1 and 2.
  - Under (d2), a held seat's pool record keeps `token` until release step 5, so writing step 1
    over it is consistent.
  - In the restore recovery, `pool_token` sets a fresh password with device logout, which leaves
    the new token live. A later (d2) take of that seat then finds a pool `token`. That token is
    as fresh as one minted inside the take, because nothing has logged it out. Skipping
    `whoami` is therefore safe.

## New findings

No major findings.

1. **minor: Task 2.5's gate B parenthetical.** It still says "H-C13 to H-C15 where they run on
   the host" without the exception the task text and Task 0.5 now make. This causes no wrong
   action: 4.4 carries gate B for that clause anyway. It would read more exactly as "H-C13 to
   H-C15, less H-C15's held-ccy-session clause, where they run on the host".
2. **minor: DESIGN-v2-C-host.md line 1270 is now longer than the bullet's other lines.** It
   reads "...from a new `--credential pool_token` (3.6.3) and no password anywhere, with either
   the". This is cosmetic and could be rewrapped.

Findings: 0 major, 2 minor. All 13 round-2 findings are VERIFIED-FIXED.
