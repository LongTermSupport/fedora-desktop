Verdict: FIX-FIRST

Round 2 review of `db3c8350..HEAD` (fix commit 8d19d829) on feat/bus-v2-n1-decisions.

## Round-1 findings

1. **major, restore keyed on `cred`.** Fixed in intent. 7.1 now gives a pool seat `cred` `pool_token`, extends C 3.12.2's merge rule to it, and runs the recovery for handles with a pool record. LXC guests keep `lxc apply`. The fix has a new defect: nothing ever writes `pool_token` (new finding 1).

2. **major, hot reissue step 2.** Fixed. Step 2 now writes `held/token.new` and renames it over `held/token`, with no login. Steps 3 and 4 are unchanged. The old token died at the reset with device logout. This matches C 3.7.3.

3. **major, gate B placement and the H-C ranges.** Mostly fixed:

   - gate B is on Tasks 4.4 and 5.1;
   - Task 0.5 names the first task that takes a pool seat;
   - 2.5's range no longer claims H-C7 or H-C8;
   - H-C13 to H-C15 are assigned.

   The assignment of H-C15 to Task 2.5 is new finding 2.

4. **major, gate A binds at the merge.** Fixed. Task 0.5 and Tasks 2.5 and 4.1 bind gate A at the merge to F44. This does not conflict with "N1-N4 build on them": Go packages and binaries may merge, and only play tasks, units and the image change wait. The repository installs Go binaries one play task at a time, so a merged binary that no play installs is inert. One residue is in Task 2.5's wording (minor 4).

5. **minor, the "nothing else" list.** Mostly fixed. The list now covers the pool record, take steps 1 and 3, release steps 4 and 5, placement and quarantine, the hot reissue, roster `cred`, and the restore. It still leaves out the C 3.6.3 verb and guest-copy change (part of new finding 1).

6. **minor, the guest token copy.** Fixed in wording ("for guest seats only"). The child has no input that tells it which kind of seat it is serving (new finding 1).

7. **minor, the restore wording and H-C13.** Fixed. "as it does for an LXC guest seat" is gone. H-C13 is trimmed to the token's limits plus logout.

8. **minor, d1 and d2.** Fixed. d1 states the `POST /logout` denial of service. d2 states take step 3 (no login, no `whoami`). The step numbering is slightly off (minor 3).

## New findings

1. **major: `pool_token` has no source.** Location: DESIGN-v2.md 7.1, first bullet and the "After a restore" bullet; DESIGN-v2-C-host.md 3.7.3 DC16 (d) bullet (~1269); C 3.6.1 (~759-763); C 3.6.3 verb table.

   Problem:

   - 7.1 has ccy-teams call `member add|return|reissue --credential token`, and the restore recovery runs `member reissue --credential token`. The C-host (d) bullet repeats "a token from `--credential token`".
   - C 3.6.1 defines `cred` as "the credential last issued, `password` ... or `token`". The verb table has only `--credential password|token`.
   - As written, a pool seat's issue therefore records `cred: token`, not `pool_token`. The restore merge rule then misses it: round-1 finding 1 comes back for every pool seat, including one that the recovery itself reissued.
   - For the same reason, the child cannot tell a pool seat from a guest seat when it decides whether to write `ctl/<team>/guest/<handle>.token`.

   Fix: state where `pool_token` comes from, and list that change among 7.1's changes. Either:

   - (a) add a third verb value, `--credential pool_token`, which issues a token, records `cred: pool_token` and writes no guest copy; ccy-teams and the restore recovery pass it; or
   - (b) the child derives it from the roster's `kind` (pool kinds against `lxc`), and 7.1 says so.

   Also amend the C-host (d) bullet to match.

2. **major: H-C15 cannot run in Task 2.5.** Location: PLAN.md Task 2.5 ("H-C13 to H-C15 (DC16, DC8) in the VM lab, or on the host once gate B is cleared") and Task 0.5's gate B sentence.

   Problem:

   - H-C15 requires that "a held ccy session works again after ccy-teams' hot reissue (and then PUTs its own floor ...)". That needs the member binary and link (N3, Task 3.2) and the ccy kit and launcher (N4, Tasks 4.1 and 4.2).
   - None of these exists at N2, in the VM lab or on the host.
   - Task 2.5 therefore cannot be closed as written, and DC8 (restore refuses unless H-C15 passes) has nothing to check against until N4.
   - H-C13 and H-C14 need only N2's pieces, so they are fine in 2.5.

   Fix:

   - Split H-C15. Its staging-namespace, revocation, floor-read and `restore_epoch` clauses stay in 2.5.
   - Its held-ccy-session-after-hot-reissue clause moves to Task 4.4, which carries gate B already. Alternatively, move H-C15 whole to 4.4.
   - Say that restore stays refused (DC8) until the clause passes. Adjust Task 0.5's "H-C13 to H-C15 in Task 2.5" to match.

3. **minor: take step numbers in 7.1 d1 and d2.** In C 3.7.3, step 3 is the login and step 4 writes `held/token`. Both 7.1 shapes say that "take step 3 ... copies it into `held/`". Fix: "take step 3 makes no login (d1: `whoami` on the pool's `token`); step 4 writes the pool's `token` as `held/token`."

4. **minor: Task 2.5's gate A names only Tasks 2.2 and 2.3.** Task 2.4 also brings units with owner-visible names (`teamchat-health.service`, the backup units, and per C 3.11 and 3.12 the owner's `teamchat-seats.timer`, DC21). Task 3.3 brings LXC apply play tasks and `teamchat-session-<user>.service`. Task 0.5's general rule covers them, but the task-level gate lists fewer. Fix: "the play tasks and units of Tasks 2.2 to 2.4", and add a gate A line on Task 3.3, or say "of any task".

5. **minor: hot reissue step 1 under (d).** 7.1 says that "step 1 writes the answer's token over the pool's `token`". Under (d2), a held seat's pool record still has `token`, since release step 5 runs only at release, so this is consistent. It would read more clearly with "as `token.new-<claim>`, renamed over `token`", mirroring C 3.7.3 step 1's `O_EXCL|O_NOFOLLOW` discipline.

Findings: 2 major, 3 minor (new). All 8 round-1 findings are addressed; findings 1 and 3 each left a defect, filed as new majors 1 and 2.
