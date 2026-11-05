Verdict: MERGE

# Plan 00209 N1: design-sync review, round 2 (fix commit `4197d156`)

Scope: `git show 4197d156` and `git diff ad0405ca HEAD` over the plan folder, checked against
`src-go/internal/teamchat/proto`, `paths` and `matrix`. Every round-1 finding is fixed. The fix
commit adds no major. It leaves 2 minor items and 2 nits, listed below. None blocks the merge.

## Round-1 findings

| #   | Sev   | State          | Evidence                                                                                                                                                                                                                                   |
| --- | ----- | -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 01  | major | VERIFIED-FIXED | A:1924-1927 names `released`, `removed`, `parked` (B 2.10), proto renders `removed` only and refuses others; `proto/membership.go:11-13,72-77` agree; B:1753-1757 and C:1392,1405,1742 agree; owed item in the design-sync report          |
| 02  | major | VERIFIED-FIXED | A 10.1 (A:1369-1372) no longer claims the section-name refusal; A 10.2 (A:1405) keeps it; `forms.go:54` (ReportForm) vs `:77` (QuestionForm)                                                                                               |
| 03  | minor | VERIFIED-FIXED | C:1486 `team` comment names the reserved ids (ParseTeamID); C:1530-1531 and DESIGN-v2.md:88 say "free as a v2 id"; `paths/ids.go:82-91`                                                                                                    |
| 04  | minor | VERIFIED-FIXED | D:785 M0 check "is not reserved (3.5)" and refusal "is reserved (3.5)"                                                                                                                                                                     |
| 05  | minor | VERIFIED-FIXED | PLAN.md:75, DESIGN-v2.md:208-209 and :360, D:1374-1375 widened; word-diff of DESIGN-v2.md changes only those words (the rest is table realignment), so the INT3 state words ("defaulted ... not among the Task 0.5 answers") are unchanged |
| 06  | minor | VERIFIED-FIXED | SPEC-matrix:221-223 (M45) and :261-264 (M52) say the take uses `Logout`; C:1222-1225 agrees; `matrix/seat.go` comments recorded as owed to N2                                                                                              |
| 07  | minor | VERIFIED-FIXED | A:863-864 and A:1029-1030; `content.go:511-512` sends `m.replace` to `decodeEdit` (`:542`)                                                                                                                                                 |
| 08  | minor | VERIFIED-FIXED | A:1419-1421; `send.go:218-220` checks the roster, `content.go:429` checks count and distinctness only                                                                                                                                      |
| 09  | minor | VERIFIED-FIXED | A:209 table row and A:218-219; D:514; `registry.py` `normalise_repo` uses `raw.lower()`, `proto/ids.go:183-189` folds ASCII only. See new finding 02 for one claim the fix adds                                                            |
| 10  | minor | VERIFIED-FIXED | A:249-251; `proto/ids.go:11,21`; PLAN.md:206-207 names both. See nit 04                                                                                                                                                                    |
| 11  | minor | VERIFIED-FIXED | A:455-458, A:531-534, D:292-293, D:1110-1111 rewrapped. A scan of every added prose line (no table, no fence) finds only new finding 03                                                                                                    |
| 12  | nit   | VERIFIED-FIXED | A:1874-1875; `envelope.go:221-223`                                                                                                                                                                                                         |
| 13  | nit   | VERIFIED-FIXED | A:986; `verdict.go:612`                                                                                                                                                                                                                    |
| 14  | nit   | VERIFIED-FIXED | A:218; `ids.go:184-187`                                                                                                                                                                                                                    |
| 15  | nit   | VERIFIED-FIXED | C:2380 and C:2654 say "changed"                                                                                                                                                                                                            |

Check 2: A 13.3's farewell sentence agrees with B 2.10 (B:1753-1757) and C 3.7.5 (C:1392, 1405,
1742). In all three, `desired.json` `farewell` carries one of three reasons. A 13.3 is about the
local MEMBERSHIP notice. B's room-level `left` (reason `released` only) is a separate notice, and
the two do not conflict. No Task 0.5 state word changed.

## New findings

1. **minor**: DESIGN-v2.md:360 (INT3 row) and D:1107-1110 (5.10). For a reserved name the
   proposal is refused by ccy-teams' own rule. INT3 now covers a v1 name that is a reserved id,
   yet both places still say ccy-teams "proposes a short id (the name cut to 22 characters)".
   `status` cut to 22 characters is `status`, which is reserved, so ccy-teams would then refuse
   the id it proposed (D:1110 "refuses an id that ... is reserved"). The flow still works, since
   the owner may change the id, but the stated default is wrong for the new case.
   **Fix**: in D 5.10, after "with trailing hyphens removed", add "(for a name that is reserved,
   the owner must choose the id, since the cut name is the name)". In DESIGN-v2.md:360, after
   "(the name cut to 22 characters", add "; a reserved name has no proposal and the owner chooses".

2. **minor**: D:514 (5.1, Agent accounts) says the fold difference is caught: "M0 names it".
   M0's row (D:785) checks nothing of the kind. It lists members and checks software, space, port,
   health and the team id, but no handle or repository fold. Three unqualified statements also
   remain: D:1338-1339 ("The v1-to-v2 mapping is the identity"), A:2234-2235 ("a seat's identity
   survives migration") and DESIGN-v2.md:160 (row 41, "Met"). They now contradict A 2.3's own
   note (A:218-219).
   **Fix**: add an M0 check to D:785: "that every ccy seat's checkout derives the same `<repo>`
   under v2's `NormaliseRepo` as its v1 handle holds". Add a refusal cell or a named gap for it.
   Then add "(except a repository name with a non-ASCII letter `str.lower()` folds to ASCII,
   A 2.3)" to D:1339, A:2235 and row 41's evidence cell. Otherwise, drop "M0 names it" from
   D:514 and record the check as owed to the migration build.

3. **nit**: SPEC-matrix.md:268 is 183 characters. The fix joined M52's auth JSON into one code
   span, which is allowed, but it then put the prose ", and must be answered 200 with an object. A
   wrong password (401" on the same line. **Fix**: end line 268 after the closing backtick and
   move the comma and the prose to the start of line 269, then rewrap 269-270 to 100.

4. **nit**: PLAN.md:206-207, "the service-account localparts `conduit` and `tuwunel` reserved by
   HC-A6". HC-A6 recorded only `conduit`. A:249-251 now says `tuwunel` is held against a later
   pin. **Fix**: "the service-account localparts `conduit` (HC-A6) and `tuwunel` reserved (A 2.3)".

Counts: 15 round-1 findings VERIFIED-FIXED, 0 PARTIAL, 0 NOT-FIXED. New: 0 major, 2 minor, 2 nit.
