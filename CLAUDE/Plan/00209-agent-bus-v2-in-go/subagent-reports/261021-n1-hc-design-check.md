# Narrow check: N1 host-check design, round-2 fixes (Plan 00209), `b7ceb7c2..a4c655f7`

**Verdict: MERGE.** 6 of 7 round-2 findings are fixed, and 1 is partial: two long lines remain, a
wrap issue only. The four mechanisms have no new major. Two minors follow.

## Round-2 findings

| #   | Finding                                           | State          | Evidence                                                                                                                                                                                                                                                                                                                                                                                                 |
| --- | ------------------------------------------------- | -------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | major: outbox `answered` written before the check | VERIFIED-FIXED | B:1319 (steps 03-05 judge in memory and write only counters and log lines); B:1320-1325 (step 06's one checked store write carries `pending`, `next_batch`, floors and the outbox updates; a discarded answer leaves the ping unanswered and still gets its TIMEOUT); B 2.15 replay row (B:2185) gains that case. The resume-marker PUT (B:650-651) follows a store write, so it is after the check too. |
| 2   | the epoch stored with a position; (c) on a change | VERIFIED-FIXED | B:627-630 states the invariant: the stored epoch is the one read before the position was fetched, never after it. B:1347-1348 sends a still-`fresh` re-added team to step 01 (c) (B33). The replay row has "a restore landing between step 01 (b)'s verification and its first persist is caught by the next answer's check".                                                                            |
| 3   | random epoch could leave the range                | VERIFIED-FIXED | C:2299-2303 ("0-499999999, the lower half ... so that later raises stay in range", with a collision chance of about 1 in 500 million); A:497 and C:775-776 agree.                                                                                                                                                                                                                                        |
| 4   | "raised" where a random value is possible         | VERIFIED-FIXED | C:2167-2168 (restore step 5: "as the merged roster sets them ... or a fresh value under `--roster-from-backup`"); DESIGN-v2.md:125 (seam 17: "changed only by a restore"); A:2085 ("changes `restore_epoch` at a restore and at nothing else"). For leftover "raised" wording elsewhere, see minor N1.                                                                                                   |
| 5   | take step 3's 429 named the wrong state           | VERIFIED-FIXED | C:1215-1218: a refused first login leaves the earlier tokens untouched, and the next take's logout ends them. A refused last login is the logout-to-login failure.                                                                                                                                                                                                                                       |
| 6   | C 3.2.4 key list                                  | VERIFIED-FIXED | C:363-365: the presence timers and `cache_capacity_modifier` are listed as added, and `database_backup_path` as moved. C:368-370 explains the doubled login limit against v1's admin-API minting.                                                                                                                                                                                                        |
| 7   | six lines past 100 characters                     | PARTIAL        | Fixed: PLAN.md:179 and :203. PLAN.md:196 and C:1309 are unbreakable Markdown links, which is accepted. Still long: B:1276 (107, was B:1271 at 104) and C:1199 (107, was C:1194 at 103).                                                                                                                                                                                                                  |

## The four mechanisms

- **Login, logout, login (H-C13).** This is correct against the fact. C:1203-1210: the first
  login mints T1 and leaves earlier tokens alive. The logout with T1 ends T1 and every earlier
  token, because they share the device. The second login's token is the only one left alive. A
  take can crash between any two steps:

  - Before the logout: T1 exists only in the dead process, and `held/token` was never written.
    Release step 4 (C:1272-1273) has nothing to log out, and the next take's step 3 ends T1.
  - Between the logout and the second login: the device has no token, and `held/` names a session
    that holds nothing (C:1213-1214, C:1231-1233).
  - After the second login, before `held/token`: the same as the first case.
  - A refused first login: no logout ran, and earlier tokens die at the next take (C:1216-1217).

  No path leaves a token alive past the next take. K13 (C:2454), DC16 (C 4, DESIGN-v2.md:344),
  B:2413-2416 and DESIGN-v2.md 7.1 (:447-457) all agree.

- **Presence 120 s + 60 s.** This is consistent with BD9 and DC21. Those rules post the
  `disconnected` notice within about a minute, from the seat lock, while presence is display only
  (R27, OD-A1). The timers claim no liveness bound, and the notice can arrive before the dot
  changes. A 6.6 (A:699-710), B step 09 (B:1362-1371), B 2.11 (B:1988-1991), A:2099, C:372-386 and
  seams 11 and 22 all agree.

- **The `restore_epoch` placement (H-C14).** No stale `since` is persisted. Step 06 writes only
  after step 08's check (B:1320, B:1340-1343). A position fetched before a restore and persisted
  after it carries the old epoch (B:627-630). The next long-poll's answer is then checked, found
  different and discarded before any write. Step 01 (a) compares before the first sync (B:1268-1273).

- **Cross-chapter grep** of A, B, C, D, FRAME, DESIGN-v2.md and PLAN.md:

  - No "before every long-poll", "after an outage", "after every outage" or "replaces" wording is
    left, apart from C:1340's history of H-C13.
  - The only syncs are the link's. `@conduit` is in A:231-232, A:1982, seam 18 and PLAN.md:179.
  - D names none of these mechanisms.

## New findings

- **N1 minor.** "Raised" survives where a random value is also possible: DESIGN-v2.md:176 (seam
  58, "raised by C 3.12.2") and C:2240 and C:2248 ("the roster's raised `restore_epoch`"). B
  treats any change as a restore, so behaviour is unaffected. Fix: "changed".
- **N2 minor.** Rewrap B:1276 and C:1199 (finding 7).
