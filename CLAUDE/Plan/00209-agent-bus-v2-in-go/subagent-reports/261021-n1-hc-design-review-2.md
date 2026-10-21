# Review round 2: N1 host-check design fixes (Plan 00209), `0ecc8409..HEAD` (fixes in `b7ceb7c2`)

**Verdict: FIX-FIRST** (1 major, 6 minor; each fix is a sentence or two)

## Round 1 findings

| #   | State          | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| --- | -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 01  | VERIFIED-FIXED | B:1332-1354 (the check runs after every answer and before step 06; a retried long-poll is an answer; the read before a long-poll is kept only to save the wait); B:1316 (step 06 persists only a checked answer); B:2178 (replay row: idle connection closed, paused team, restore between the read and the long-poll); B:2411-2416 (C13); C:2241-2254; C:2569-2573; DESIGN-v2.md:176 (seam 58). New defects in this area are findings 1 and 2 below. |
| 02  | VERIFIED-FIXED | C:2286-2295 (fresh random value, differing from the backup's and the status record's copy); A:497; C:772-773. The range edge is finding 3 below.                                                                                                                                                                                                                                                                                                      |
| 03  | VERIFIED-FIXED | C:2569-2573 now says "after every sync answer and before persisting it"; B:1269-1270 says "exactly as step 08's epoch check does". A grep finds no "after an outage", "after every outage" or "before every long-poll" left in the five DESIGN-v2 files or PLAN.md.                                                                                                                                                                                   |
| 04  | VERIFIED-FIXED | C:377-381 names the stopped team, the paused sync (120 s, then 180 s) and a restart longer than about a minute. A 6.6 (A:704-710) was not changed, but it makes no contrary claim and defers to C 3.2.4 for the timers. The arithmetic holds: the 30 s poll plus the 30 s backoff cap leaves 60 s of the 120 s idle timer, so "about a minute" is right.                                                                                              |
| 05  | VERIFIED-FIXED | C:1210-1213 (two logins per take, the doubled limit, 429 by M6); C:365-368 (burst 10 at 0.006/s; v1's values 5 and 0.003 match `helpers/agent_bus/render.py:57-60`; the failed limit keeps 3 and 0.0033). Minor new problems are findings 4 and 5.                                                                                                                                                                                                    |
| 06  | VERIFIED-FIXED | C:2446 (K13 bound stated per case); C:2408 and DESIGN-v2.md:344 agree.                                                                                                                                                                                                                                                                                                                                                                                |
| 07  | VERIFIED-FIXED | A:1993 and A:2012 read "HC-A1 held on 1.9.4 apart from `set_presence=offline`"; A:2074 reads "roster only if OD-A1 (b) is chosen".                                                                                                                                                                                                                                                                                                                    |
| 08  | VERIFIED-FIXED | PLAN.md:200-203: "idempotent within one database, and across a restore because no answer is persisted before B 2.6 step 08's epoch check (H-C14)".                                                                                                                                                                                                                                                                                                    |
| 09  | VERIFIED-FIXED | B:1342-1344. A transient read failure persists nothing and sends no long-poll. A missing, undecodable or refused roster is a trust failure (step 04, exit 22). A lower epoch counts as a change (B:1337-1338).                                                                                                                                                                                                                                        |
| 10  | VERIFIED-FIXED | (a) The integrator note is at `261021-n1-hc-design.md:163`, and Task 1.4a is still only on `feat/bus-v2-n1-hostchecks`, so the integration has to tick it. (b) C:183 now reads "every member's link". (c) The touched lines are rewrapped, apart from six added lines that run 1-6 characters past 100 (finding 7).                                                                                                                                   |

## Findings

1. **major. DESIGN-v2-B-member.md:1313-1315 (step 05), :1316-1319 (step 06), :1334-1337 (step 08).**
   A batch can still be acted on before the check. Step 05 says an answer to one of the member's
   pings "updates the outbox by its `origin_server_ts` ... even when read late". Step 06's checked
   store write lists only `pending`, `next_batch` and the floors, and 2.3.2 does not put the outbox
   in that write either. So nothing stops the outbox's `answered` being written while the answer is
   judged, before step 08's check. Take an answer served by the old database that holds a ping's
   reply. A restore lands before the epoch read. The check discards the batch, but `answered` is
   already set. The reply was posted after backup `N`, so it is not in the restored database, and
   step 01 (b)'s resume cannot find it. The member never gets the reply, and step 07 never makes
   the TIMEOUT because `answered` hides the target. That is a silent loss, the class this fix was
   meant to close. Step 05's `ignore`/`drop` counters and `link.log` lines have the same ordering,
   but they are harmless: at worst they are counted twice.
   **Fix:** in step 05, say that steps 03-05 judge in memory and write nothing. In step 06, say the
   one store write carries the batch's accepted items, `next_batch`, the floors **and the outbox
   updates (`answered`) the batch made**, and that a discarded answer leaves no trace beyond its
   counters and log lines. Add to B:2178's replay row: a ping's reply in a batch discarded by the
   check leaves the ping unanswered, and its TIMEOUT is made.

2. **minor. DESIGN-v2-B-member.md:1265-1299 (step 01 (b), (c)), :1339-1342 (step 08), :622-627 (`fresh`, `restore_epoch`).**
   Step 08 compares the epoch "with the one the store last saw". A fresh store under (b) or (c) has
   none, and neither path says which epoch read it stores with its first position. Safety rests on
   one invariant: an epoch is only ever stored beside a position fetched **after** that epoch was
   read. If (b) or (c) stored an epoch read after it fetched its position, a restore between the
   two would store the new epoch beside a stale position, and nothing later would catch it.
   Separately, step 08 sends every changed epoch to "(b)". For a re-added team that is still
   `fresh` in (c), that delivers history from its marker, against (c)'s rule that nothing posted
   while the team was out is delivered (B33).
   **Fix:** at B:626-627, add: "the epoch stored with a position is always the one read by the
   verification or check that preceded that position, never one read after it". At B:1339-1340,
   write "resumes as step 01 (b), or as (c) for a re-added team whose store is still `fresh`". Add
   a line to B:2178: a restore landing between (b)'s verification and its first persist is caught
   by the next answer's check.

3. **minor. DESIGN-v2-A-protocol.md:497; DESIGN-v2-C-host.md:2293.**
   A 5.1 bounds `restore_epoch` at 0-999999999, and `--roster-from-backup` draws a "fresh random
   value in A 5.1's range". Every later restore adds one. A draw within `k` of the top, followed by
   `k` restores, writes a roster outside the range. C refuses to write an invalid roster (DESIGN-v2.md
   seam 17), so that restore fails at step 5. Even if it were written, every link would read it as
   undecodable, which B:1344 makes a trust failure, so every team would stop with exit 22. The
   chance is tiny, but it is a defined failure the text allows.
   **Fix:** in C:2293, write "a fresh random value in 0-499999999 that differs from ...". In A:497,
   add "(a random value is drawn from the lower half, so later raises stay in range)".
   Alternatively, state that a raise past the top wraps to 0, which B accepts because it compares
   "different". Also say why a random value is enough: it can collide with an older value a member
   still holds with a chance of about 1 in 10^9.

4. **minor. DESIGN-v2-C-host.md:2161-2162 (restore step 5), DESIGN-v2.md:125 (seam 17), DESIGN-v2-A-protocol.md:2085.**
   Several statements still describe only the raise. Step 5 writes the roster event with
   "`restore_epoch` one above the live one", with no exception for `--roster-from-backup`, where
   there is no live roster to read (C:2286-2295 sets a random value). Seam 17 says "`restore_epoch`
   raised only by a restore" and A:2085 says C "raises `restore_epoch` at a restore". A random
   value can be lower, and B treats any change as a restore.
   **Fix:** C:2162: "`restore_epoch` as the merged roster sets it (below: one above the live one,
   or a fresh value under `--roster-from-backup`)". Seam 17: "`restore_epoch` changed only by a
   restore". A:2085: "changes `restore_epoch` at a restore and at nothing else".

5. **minor. DESIGN-v2-C-host.md:1210-1213 (take step 3).**
   "a 429 ... past its retries it is such a failure" points back to "a failure between the logout
   and the last login". That is wrong for the first login: a 429 there comes before the logout, so
   the device's earlier tokens are still alive and no token was minted. The outcome is fine (the
   take fails, and the next take's logout ends those tokens), but the text names the wrong state.
   **Fix:** "a 429 is retried as M6 says; past its retries, a refused first login fails the take
   with the device's earlier tokens untouched (the next take's logout ends them), and a refused
   second login is a failure between the logout and the last login (above)".

6. **minor. DESIGN-v2-C-host.md:355-368 (C 3.2.4, "The key set is v1's").**
   The values are right: v1's `LOGIN_LIMITS` are `account` 5 at 0.003 and `failed` 3 at 0.0033
   (`helpers/agent_bus/render.py:57-60`), and C doubles only `account`. But the sentence "The key
   set is v1's ... Added in v2: ..." does not list `presence_idle_timeout_s` or
   `presence_offline_timeout_s`. v1 does not set them (`render.py:74-108`), and the next paragraph
   (C:373) adds them. `database_backup_path` is listed as "Added in v2", yet v1 sets it
   (`render.py:82`): only its location is new.
   **Fix:** C:363-364 should read "Added in v2: `presence_idle_timeout_s` and
   `presence_offline_timeout_s` (below), `cache_capacity_modifier` from H-C1; moved in v2:
   `database_backup_path`, now inside the state directory".

7. **minor. Wrap.** Six added lines run past 100 characters: B:1271 (104), C:1194 (103),
   C:1304 (102), PLAN.md:179 (105), PLAN.md:194 (101) and PLAN.md:203 (106). **Fix:** rewrap them.

## Checked, no finding

- **Step 08's check against step 01 (a).** The store is compared before the first sync, and every
  answer is checked afterwards, so a restore during the first long-poll is caught too.
- **Step 03's limited-timeline fill.** Whether the fill runs before or after the check, nothing
  stale is stored with a new epoch. A fill against a restored server, persisted beside the old epoch,
  is caught by the next answer's check. The rule holds as long as finding 2's invariant does.
- **The delivery wake.** It comes after step 06's checked write (B:1318-1319), so no item from an
  unchecked answer is delivered.
- **Step 04 on a stale batch.** It verifies against the restored, merged roster, which is valid, so
  a stale answer does not wrongly stop the team.
- **Presence timer text.** C:370-384 matches HC-A1's measured shape (120 + 60), B 2.6 step 02 (30 s
  poll), step 08 (30 s cap, 45 s deadline), and 2.3.2's full-queue pause. The added epoch GET
  between long-polls does not change the bound.
- **Stale-phrase grep** of DESIGN-v2.md, A, B, C, D, FRAME and PLAN.md:
  - "after every outage", "before every long-poll" and "if Tuwunel can" are gone.
  - Every `set_presence=offline` that remains states the HC-A1 fact or the rule that avoids it.
  - "raised by one" remains only for `rev`, `seq` and `restore_epoch`. The `restore_epoch` lines
    carry the random-value exception, apart from finding 4's lines.
  - "fallback" and "replace" remain only in an unrelated sense (DESIGN-v2.md:300 is OD-A1's option
    (b); C:1335 tells the history of H-C13).
