Verdict: FIX-FIRST

Review of `db3c8350..HEAD` on feat/bus-v2-n1-decisions (Task 0.5 recording, DESIGN-v2.md 7 and 7.1, C 3.7.3, C DC16/DC19 rows).

The recording itself is exact. Every flagged ID is present in DESIGN-v2.md section 7 with the right state. DC8 is accepted with its H-C5/H-C15 return. DC17 carries its allowlist rows and the govulncheck condition. INT3 is left defaulted. The PLAN "others" row holds exactly the 41 unflagged IDs, and none of them was answered. The N5/N6 mismatch is handled correctly in Task 0.5. There are no time estimates and no install-specific names. The findings are in gate placement and in 7.1.

1. **major**: DESIGN-v2.md 7.1, "After a restore" bullet; C 3.6.1 `cred` (lines ~759-763) and C 3.12.2 merge rule (line ~2222).

   - What is wrong: the restore tells pool seats from guest seats by the roster's `cred`. Every `cred: password` handle gets `needs: reissue`; guest (`token`) seats are judged by `cred_seq` and reissued by `lxc apply`. 7.1 sets a pool seat's `cred` to `token`, so:
     - a pool seat whose `cred_seq` is at or below the backup's is no longer marked for reissue;
     - under d2, a token logged out after the backup comes back live, which reopens K13 and fails H-C15's "pool seat's token live at the backup ... refused";
     - the recovery's guest path takes the wrong seats.
   - The claim "DC8 is otherwise unchanged" is false.
   - Fix: 7.1 must change the merge rule so it keys on a pool record or member kind, not on `cred` (or adds a distinct `cred` value for a pool token). State the change in the restore bullet.

2. **major**: DESIGN-v2.md 7.1, "The hot reissue" bullet.

   - What is wrong: C 3.7.3's step 2 is both the login and the write of `held/token`. Step 3 is the session copy and step 4 is B's resume, not a copy. Dropping "step 2's second login" and saying "steps 3 and 4 copy it into `held/`" leaves `held/token` holding the dead old token. The release's disconnected notice then records "not possible" (`M_UNKNOWN_TOKEN`), and d2's logout does nothing.
   - Fix: "Step 2 writes the answer's token as `held/token.new` and renames it over `held/token`, with no login; steps 3 and 4 as now."

3. **major**: PLAN.md Task 0.5 "(Task 2.5)" and Task 2.5's gate B; Task 4.4; Task 5.1.

   - What is wrong:
     - H-C8 is Task 4.4's (it needs N4's ccy kit), not 2.5's. Task 2.5's "H-C1 to H-C12" overlaps 4.4's H-C7/H-C8.
     - 4.4's `teamchat-pilot` with a ccy session, and 5.1's host `cc` members, take pool seats on the host. Gate B is on neither, so if 2.5 goes ahead without a pool seat, the real first deploy is ungated.
     - H-C13 to H-C15 are assigned to no task, and H-C15 needs a pool seat.
   - Fix:
     - Put gate B on 4.4 and 5.1, and on whichever task runs H-C13 to H-C15 on the host.
     - Assign H-C13 to H-C15 to a task, saying whether each runs in the VM lab or on the host.
     - Correct 2.5's H-C range.
     - Make Task 0.5 name the first task that actually takes a pool seat on the host.

4. **major**: PLAN.md Task 4.1 gate A (and the N2 install gate on 2.5).

   - What is wrong: the gates are phrased as "not deployed", but in this repository a merge to F44 reaches the host at the next ccy rebuild or full play run. Once the kit is merged, every unrelated CCY bump deploys `teamchat` paths. Likewise a merged teamchat play's unit files arrive at any full run (H-C11 bounds only what is resident).
   - Fix: bind gate A to the merge into F44 of code that installs an owner-visible name. Alternatively, require that code to stay inert and installed by no play until the owner confirms, and say which.

5. **minor**: DESIGN-v2.md 7.1, final paragraph.

   - What is wrong: "changes the pool's credential file, take step 3, release step 4 and the hot reissue's step 2, and nothing else" contradicts 7.1's own list. Also touched, and not all named:
     - take step 1's complete-record test (both shapes);
     - placement's file and the quarantine's "`password` unlinked";
     - release step 5 (d2);
     - the restore recovery and merge rule;
     - 3.7.1/3.7.2's `password` text;
     - roster `cred`.
   - Fix: list them, or drop "nothing else".

6. **minor**: DESIGN-v2.md 7.1, first bullet.

   - What is wrong: C 3.6.3's `--credential token` also writes `ctl/<team>/guest/<handle>.token` for the health tick. 7.1 says "the path a guest seat already takes" without saying whether a pool seat keeps that copy.
   - Fix: state that the copy is written for LXC seats only, or that it is kept and harmless (root already holds the admin token).

7. **minor**: DESIGN-v2.md 7.1, restore and hot-reissue bullets, and the "H-C13 shrinks" bullet.

   - What is wrong:
     - "as it does for an LXC guest seat" is wrong: the recovery reissues guest seats through `lxc apply`, not `member reissue`.
     - The reissue kills the holder's old token through 3.6.3's password reset with device logout, not through a same-device login. So "(H-C13)" there, and "a login with the same device id replaces the token" in the shrunk H-C13, are not things (d) rests on.
   - Fix: correct the restore wording, and trim H-C13 to "the token cannot change the password, add a device or mint a login token; for d2, `POST /logout` kills it".

8. **minor**: DESIGN-v2.md 7.1, d1 and d2.

   - What is wrong:
     - d1 does not state that any released holder can `POST /logout` the shared token, which forces a password reissue at the next take (a denial of service on the seat).
     - d2 does not say what take step 3 becomes: copying the pool's `token` into `held/`, with a `whoami` check or without one.
   - Fix: add one sentence to each.
