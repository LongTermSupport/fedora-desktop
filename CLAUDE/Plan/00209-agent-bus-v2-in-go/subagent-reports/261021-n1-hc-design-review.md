# Review: N1 host-check design fixes (Plan 00209), `0ecc8409..097416dc`

**Verdict: FIX-FIRST**

Scope: the diff on `feat/bus-v2-n1-hc-design`, checked against the "Findings on Tuwunel 1.9.4" in
`261021-n1-hostchecks-handoff.md`, the probe (`hca_probe.py`) and its run record
(`261021-n1-hostchecks-run.md`) on `feat/bus-v2-n1-hostchecks`.

What holds: HC-A1's fact is recorded right, and every "other syncs pass `set_presence=offline`"
clause is gone from A, B and DESIGN-v2.md. The 120/60 timers match the probe's measured shape
(unavailable after idle, offline after idle plus offline). H-C13's chosen order (login, logout with
the new token, login) rests on what the probe actually showed: the logout was made with the
*second* token and the *first* then answered 401. DC16 (both tables), K13, the C 3.14 seats row
with `fakehs`, release step 4 and 7.1's (d) check all agree with each other. H-C15 does cover the
pool-token revocation that 7.1 cites it for. Staging the restore (DC8, C 3.12.2 step 5) writes the
raised `restore_epoch` before members can reach the restored database, so the epoch read cannot
see the backup's old roster. HC-A6, the A 14 Element row, the PLAN.md Task 2.5 gate B parenthetical
and the DC16 (d) rewrap are done.

## Findings

01. **major. DESIGN-v2-B-member.md:1331-1346 (step 08), with :1316 (step 06), DESIGN-v2-C-host.md:2240-2244, DESIGN-v2.md seam 58.**
    Reading the epoch *before* each long-poll leaves a gap between the read and the send. If a
    process is frozen after its epoch GET returns and before its `/sync` goes out (`podman pause`, a
    cgroup freeze, SIGSTOP; this is the "process that was stopped" case the step itself names), and a
    restore runs during the freeze, the thawed link sends the old `since` to the restored server.
    Per H-C14 that waits, then answers the lower `next_batch`. Step 06 persists it, and every post
    made after the restore is lost without a word. The freeze row in 2.4.5 (B:1168) does not catch
    this: no request is in flight to be "long overdue", and a cgroup freeze opens no
    BOOTTIME/MONOTONIC gap. The same race applies to a retry that re-sends the long-poll.
    **Fix:** make the check that guards the store come *after* the answer. Once a `/sync` answers,
    read the roster's `restore_epoch` by the direct state request **before step 06 writes
    `next_batch`**. If it changed, throw the batch away and resume as step 01 (b). Keep the read
    before the long-poll only to avoid the 30 s wait, or drop it, since the cost is the same one GET
    per batch. Say that a retried long-poll goes through the same check. Update step 06, step 08,
    C 3.12.2's sync paragraph, seam 58 and B 6 C13 to match. Add a case to B 2.15's replay row: the
    restore lands between the epoch read and the long-poll, and the stale batch is never persisted.

02. **major. DESIGN-v2-C-host.md:2274-2280 (merged roster, `--roster-from-backup`).**
    `restore_epoch` is "the live one plus one". But `--roster-from-backup` is the case where the
    live roster cannot be read, so the base becomes the backup's epoch. Take backup `N` at epoch 0
    and a later restore raising it to 1, which every member stores. A second restore from `N` with
    `--roster-from-backup` writes 0+1 = 1. That equals what every member stored, so no link drops
    its position, which is the H-C14 silent loss. The diff now makes the epoch the only signal, so
    this has to be closed. **Fix:** in the `--roster-from-backup` bullet, raise `restore_epoch`
    above every value the host still holds: the status record's copy of the roster's epoch, and
    failing that, a value no member can hold. B compares "different", not "greater", so a fresh
    random 30-bit value within A 5.1's range also works. State it in A 5.1's `restore_epoch` row
    and in C 3.6.1's bullet at :765.

03. **minor. DESIGN-v2-C-host.md:2554-2557 (section 7, B assumption 08); DESIGN-v2-B-member.md:1269-1270 (step 01 (a)).**
    Both still state the old trigger: "on every reconnect after a homeserver outage it reads the
    roster", and "exactly as step 08 does after an outage". These contradict the new step 08.
    **Fix:** C:2555 should read "before every long-poll (and, per finding 1, before persisting each
    answer) it reads the roster by a direct state request, not by sync, since a `since` ahead of the
    database waits and then skips (H-C14)". B:1269-1270 should read "exactly as step 08 does when the
    epoch changes".

04. **minor. DESIGN-v2-C-host.md:370-376 (presence timers); DESIGN-v2-A-protocol.md 6.6.**
    The claim that "a healthy member never drops to `unavailable`" ignores B's own pause. A full
    `pending` stops that team's sync (B:686-688) while the session lives. With 120/60 such a seat
    shows `unavailable` at 120 s and `offline` at 180 s, faster than under the defaults. The restart
    reasoning is also only true for short restarts. The gap since the last sync is 30 s (poll) plus
    the downtime plus up to 30 s of backoff (more with jitter), so any homeserver restart longer than
    about 60 s still flickers members to `unavailable`. **Fix:** say both in C 3.2.4 and A 6.6. A
    team paused on a full queue shows `unavailable`/`offline` after 120/180 s (that is display only,
    and `status` and the board show the pause), and a restart longer than about 60 s shows members
    `unavailable` until their next sync. Alternatively, have the link `PUT /presence online` once a
    minute while paused. That is not proven to reset Tuwunel's idle timer, so it would need a line
    in HC-A1's Tuwunel-tagged row.

05. **minor. DESIGN-v2-C-host.md:1192-1204 (take step 3), :355-359 (`[rate_limiting.login.*]`).**
    A take now makes two password logins. A failed take that is retried makes four. The probe
    records a burst of five logins (`hca_probe.py:877`). The design does not check the doubled count
    against the login limits v1's tables set, or against a launcher starting several sessions at
    once (if the limit is per source address, every ccy container counts against it). It also does
    not say what a 429 on the second login does. **Fix:** in take step 3, state the logins per take,
    and that `team apply`'s login limits admit them with a retry (cite the values, or add the check
    to H-C13's row). State that a 429 is M6's transient path (`held/` left naming a session that
    holds nothing, as for a failure between logout and login).

06. **minor. DESIGN-v2-C-host.md:2431 (K13); DESIGN-v2-C-host.md:1258-1259 (release step 4).**
    "Lives at most until the seat's next take **or release**" is false for the case K13 names in
    brackets. A take that died before `held/token` leaves a release with no token to log out, so
    only the next take ends it. The token was never written anywhere, so no hostile copy exists.
    **Fix:** state that precisely: "a token whose release never ran lives at most until that
    release or the seat's next take; one minted by a take that died before `held/token` was never
    written and dies at the next take". Alternatively, make release step 4 with no `token` do a
    login and logout from the pool's password, so the bound holds as written.

07. **minor. DESIGN-v2-A-protocol.md:1993 (OD-A1), :2012 (risk row), :2073 (C assumption).**
    "HC-A1 having passed" and "HC-A1 passed on 1.9.4" contradict the run record. The run gives
    HC-A1 FAIL on one fact, and the 16 row records it. :2073 still says "or the roster only if HC-A1
    fails". **Fix:** write "HC-A1 held on 1.9.4 apart from `set_presence=offline`, which 6.6's rule
    now avoids" in both rows, and replace ":2073 … only if HC-A1 fails" with "or the roster only if
    OD-A1 (b) is chosen".

08. **minor. PLAN.md:200-202 (Task 3.1).**
    "net/http may resend a dropped `/sync` … safe because a sync with a since token is idempotent."
    That is only true within one database. H-C14 shows that a resend reaching a restored server
    answers a lower position and skips posts. B 2.6 step 08 now uses this very retry as a reason for
    the epoch read. **Fix:** "… idempotent within one database; across a restore the epoch check
    (B 2.6 steps 06/08, H-C14) is what makes it safe".

09. **minor. DESIGN-v2-B-member.md:1331-1338 (step 08).**
    The diff does not say what the link does when the epoch read itself fails. It should cover a
    404 (no roster), a roster that does not decode strictly, an error at the room's power level, and
    a transient error. **Fix:** a transient failure takes the step's backoff and no long-poll is sent
    (no sync without a fresh read). A 404, an undecodable roster or a lower epoch is trust-bearing:
    re-verify the room (step 04) and stop the team with exit 22 if verification fails. Treat a
    *lower* epoch as a change (drop the position), which fails safe as redelivery.

10. **minor. Housekeeping.**
    (a) This branch's PLAN.md has no Task 1.4a. That task lives on `feat/bus-v2-n1-hostchecks`
    (`PLAN.md:139` there) and names these design-text fixes, so the integration must tick it
    together with this commit (Plan Commit Rule).
    (b) DESIGN-v2-C-host.md:182 says "every member's keeper hold a long-poll sync", against B14
    (the keeper makes no network call). This predates the diff but is in the same presence and sync
    statements. Fix: "every member's link".
    (c) Several new lines run past the files' wrap: A:231-232, B:1352, C:2223 (pre-existing). Rewrap.

## Checked, no finding

- **HC-A1 timers.** 10/10 in the probe showed unavailable at 10 s and offline at 20 s, so 120/60
  gives 2 min and 3 min as stated. The keys were accepted by a real 1.9.4 config, and the A 14
  Tuwunel row now asserts the live-link case.
- **H-C13 option (i).** The logout with the newest token ended the older one (run:99). A failure
  between the logout and the last login is covered. Release step 4's `M_UNKNOWN_TOKEN`-as-done
  still holds. The hot reissue needs no extra logout, since its reset logs the devices out.
- **H-C14 citations.** These are recorded as "restore simulated by a cold copy". C 3.12.2's claim
  that the roster read "answers at once" matches run:112. The epoch read cannot see the backup's
  roster, because the staged copy is secured first (DC8, step 5).
- **7.1 (d).** No take login under (d1) or (d2). H-C15 does prove that a pool token live at the
  backup is refused after the reset.
- **Seams 11, 18, 22, 58 and DC16 in DESIGN-v2.md 3 and 7** match the chapters, apart from
  finding 1's addition.
