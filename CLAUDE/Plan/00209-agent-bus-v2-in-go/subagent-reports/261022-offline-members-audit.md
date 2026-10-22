# Plan 00209: audit of v2 for offline members blocking anything

Branch `feat/bus-v2-n3`, from tip 727087da9. The owner's rule is that team config supports
members that are not running. An offline member is shown as offline and never blocks anything.
This audit covers:

- the design (FRAME, A, B, C, D and DESIGN-v2.md);
- the specs (SPEC-n2a to n2f, SPEC-n3.md; n2g's SPEC on `feat/bus-v2-n2g` was read for restore);
- the built code (`src-go/internal/teamchat`, N2 and n3a).

Two sonnet digests did the first pass over A+B and C+D. Every BLOCKS row below was checked
against the text or the code.

Classes:

- **BLOCKS** violates the rule;
- **SHOWN** the member is shown offline and nothing blocks;
- **INTENDED** the operation targets that one member (or the member's own link), so refusing or
  waiting for it alone is right.

Slices are PLAN.md's. The brief's "N5 migration" is PLAN.md's Phase 6, N6.

## Counts

| Class    | Count |
| -------- | ----- |
| BLOCKS   | 13    |
| SHOWN    | 34    |
| INTENDED | 17    |

## BLOCKS

| #   | Location                                                                        | What blocks                                                                                                                                                                                                                               | Slice                                  | Fix                                                                                                                                                                                                                      | Status                                                                                                                                  |
| --- | ------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------- |
| B1  | `health/view/view.go:375` (VW5; C 3.11.2 step 3)                                | The open coordinator invited to questions but not joined was a team problem with the fix "restart it". A coordinator promoted while offline accepts the invite only when its link next runs, so one offline member turned the record red. | n2e                                    | Only a coordinator neither joined nor invited is a problem. An invited one is shown by its `invite` membership.                                                                                                          | **Fixed** 181390f52 (test `TestAnOfflineCoordinatorInvitedIsNoProblem`, SPEC-n2e VW5)                                                   |
| B2  | `health/online.go:180,189,206` (HT6; C 3.11.3)                                  | One LXC member's `launch-failed`, unprobeable seat, or unposted `disconnected` notice was a team-level problem. C's `status` exits 1 on any problem, and the board colours the team by problems.                                          | n2e                                    | `status.Problem` gains `member` (a handle of the record's own member entries; decode refuses any other). The tick sets it on those three problems. A reader judging a team counts only the problems that name no member. | **Fixed** f9dfa4c3b (tests in `status` and `health`, including `TestAStoppedMemberIsShownOfflineAndIsNoTeamProblem`; SPEC-n2e SR1, HT6) |
| B3  | C 3.11.3, line 2092: "`teamchat-admin status` ... exits 1 if any has a problem" | A member that is merely offline (launch failed, coordinator wanted) makes `status` exit 1, and anything using it as a gate goes red.                                                                                                      | `status` verb (unbuilt; N2 wiring, N4) | Exit 1 only on problems with no `member`. Member problems are printed on the member's row.                                                                                                                               | Mechanism built (B2). Rule in SPEC-n2e SR1 and the FRAME. **Left:** the C 3.11.3 text and the verb itself.                              |
| B4  | C 3.6.4, lines 970-974                                                          | `coordinator_wanted` (declared, not created yet, e.g. in a stopped LXC container) is "listed as a problem", so the team is red until that member first runs.                                                                              | n2e view and `status` (not built yet)  | Show it as team state (`coordinator_wanted: <handle>` and its fix) on the board, not as a team problem; or make it a member problem once the handle exists.                                                              | The FRAME says so. **Left:** C's text. The code does not emit it yet.                                                                   |
| B5  | C 3.9.2 step 02 (line 1669), C32; SPEC-n3 LX2                                   | A declared container that no longer exists fails `lxc apply --phase present` for every other guest and team.                                                                                                                              | n3b                                    | Missing is `DEFERRED ... container missing`, like stopped. Only `--only C` for that container exits 1. `declare check` names it.                                                                                         | **Spec amended** 6cf0dc7fa (LX2). **Left:** C's text in 3.9.1 and 3.9.2.                                                                |
| B6  | SPEC-n3 LX3 (n3-D7)                                                             | "A different or dead pid refuses the container for this phase": a container that stops during the apply could fail the phase for all entries.                                                                                             | n3b                                    | That entry is `DEFERRED ... container stopped during the apply`, retried next apply.                                                                                                                                     | **Spec amended** 6cf0dc7fa                                                                                                              |
| B7  | SPEC-n3 LX6; C 3.9.2 steps 05, 07, 08                                           | A guest op's failure fails the phase. An `lxc-attach` into a container that stopped meanwhile would fail every entry.                                                                                                                     | n3b                                    | After a failed or timed-out attach, ask `lxc-info -sH` again. If the container is not running, that entry is `DEFERRED`. Only a running container's failure fails the phase.                                             | **Spec amended** 6cf0dc7fa                                                                                                              |
| B8  | SPEC-n3 LP1 (L3 probe); B 2.11 risk, line 2291                                  | A missing container, one that stops between `lxc-info` and the open, or a user with no seat directory yet, could answer a probe error, which is a problem on every tick.                                                                  | n3b                                    | Answer these as `Stopped`. An error is only a fault in the host's own reads. The problem it raises already names the member (B2).                                                                                        | **Spec amended** 6cf0dc7fa                                                                                                              |
| B9  | A 3.2, lines 302-307 (raise; also A 6.6, line 748)                              | C's raise refuses while any non-removed member's recorded kit lacks the new major. An offline LXC member whose kit could not be upgraded while stopped holds the whole team's raise.                                                      | future major (no slice)                | The raise refuses only for live members on old kits and names the offline ones as lagging. A returning member's link refuses its own team (a NOTICE: "kit too old: apply its container") until the kit is upgraded.      | The FRAME says so. **Left:** A's text.                                                                                                  |
| B10 | D 5.4 M0, line 785                                                              | Migration preflight refuses the whole team when one LXC member's rootfs cannot be opened (stopped, unmounted, missing). This contradicts D 5.2 row 5 and P1's `state_read_from: server-only`.                                             | N6 (`migrate/v1`)                      | Drop the clause. Such a member is `server-only`, a named gap in the plan of record and in the report.                                                                                                                    | **Left** (design text; the FRAME names it)                                                                                              |
| B11 | D 6.1 A7, line 1143 (M7, line 792)                                              | The acceptance's live probe pings the coordinator, or the first worker. If that member is offline, `timed_out` fails acceptance, so M7 fails.                                                                                             | N6 (acceptance)                        | Target a member whose seat lock is held. If none is live, A7 is `not run: no live member`, not a failure, as A6 already treats stopped members.                                                                          | **Left** (the FRAME names it)                                                                                                           |
| B12 | D 5.7 step 4, line 1039                                                         | Rollback waits up to 600 s for fresh v1 presence from every member running at the fence, including one that has since gone offline.                                                                                                       | N6 (rollback)                          | Read each member's session record or unit state once. An offline one is listed at once with its restart command. Wait only for members whose process is up.                                                              | **Left** (the FRAME names it)                                                                                                           |
| B13 | D 5.4 M6, line 791; D 5.3, line 717                                             | M6 fails, and refuses later restarts, if a session running at M2 is down at M6 and does not come up within its bound. This contradicts D 6.2 ("reported and is not killed").                                                              | N6, N4 (ccy-teams restart)             | M6 restarts only the sessions running at M6. A stopped one is recorded `moves when next started`. A missed bound is a report line, not a step failure.                                                                   | **Left** (the FRAME names it)                                                                                                           |

## SHOWN

| Location                                                                                   | How the offline member is shown, with nothing blocked                                                                   | Slice    |
| ------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- | -------- |
| A 6.6, lines 719-742                                                                       | Liveness is the seat lock; presence goes `unavailable`, then `offline`, as display only                                 | design   |
| A 6.5, lines 683-692, 710                                                                  | An offline member's catch-up is bounded; a lagging `rev` is shown in `status`, blocks nothing                           | n3c      |
| A 7.4, lines 957-1005                                                                      | One `disconnected` per run, one `back` after it; `moved` is one room event, read on return                              | n2e, n3c |
| A 8.3-8.5, lines 1247-1274                                                                 | A ping to an offline member is sent; one TIMEOUT at the deadline, never retried; `stale` on late return                 | n3d      |
| A 10.5, lines 1498-1506                                                                    | One reminder to the coordinator, computed from history                                                                  | n3f      |
| B 2.6 step 08, lines 1351-1359; B 2.4.2, lines 1174-1180                                   | Own homeserver loss: `(down)`, bounded backoff, other teams unaffected, CLI exits 69 at once                            | n3c      |
| B 2.9, lines 1706-1727                                                                     | Status-line red marks are the member's own link, never a peer's                                                         | n3e      |
| B 2.11, lines 1902-1964, 2018-2020, 2291                                                   | Every end gives one `left` or `disconnected` from host-held state; an unreachable guest counts as not running           | n3c, n2e |
| B 2.5, lines 1210-1262                                                                     | A probe of a held seat is a bounded `Busy`, not a wait                                                                  | n2c      |
| B 2.7.3, lines 1543-1547; B 2.3.3, lines 1300-1306                                         | A returning member's backlog is bounded with a `gap:` line; nothing waits for a seat to be held                         | n3c, n3e |
| B 2.2.1, lines 370, 399-405                                                                | `status` shows who is live; no liveness-check verb exists                                                               | n3c      |
| C 3.9.3, lines 1809-1831 (C32)                                                             | A stopped container is never entered and never fails the apply; `SKIPPED` or `DEFERRED`; record `stopped` or `deferred` | n3b      |
| C 3.9.2 absent phase, lines 1788-1796                                                      | Removing a stopped guest's member parks it from the host at once                                                        | n3b      |
| C 3.9.2 step 03 and `--only-teams`, lines 1688, 1751-1755                                  | No placement into a stopped container; a scoped apply never touches other members                                       | n3b      |
| C 3.11.2 step 6, lines 2013-2032 (C43)                                                     | Offline LXC member: one notice per run, `lxc-online.json` flag, never repeated                                          | n2e      |
| C 3.11.3, lines 2074-2088                                                                  | `online: false` is data; the record's own expiry is the only staleness, with no per-member sync-age check               | n2e      |
| C 3.13, lines 2106-2112, 2392                                                              | The board probes the seat lock; opening the board never waits on a seat or the homeserver                               | N4       |
| C 3.7.5, lines 1417-1466; C 3.7.3, lines 1180-1182                                         | A crashed ccy session is announced once; a hot reissue never waits for a stopped session                                | n2c, N4  |
| C 3.12, lines 2142-2152, 2287-2295; SPEC-n2g RV7, RM12                                     | Backup and restore never enter or wait for a member; an offline seat is fixed at its next take                          | n2g      |
| D 5.2, 5.2.1, 5.5, 5.6, 5.8, 6.1 A5-A6, 6.2, 6.3 (lines 567-609, 887-990, 1064, 1141-1213) | Stopped and remote members are deferred, server-only, or pending on the server; the pilot seeds a stopped member        | N6       |
| DESIGN-v2.md row 29, line 143                                                              | A farewell for a seat whose session is not running is covered by the admin's `removed`                                  | frame    |
| `health/health.go:154` RunAll (HT1)                                                        | One team's failure is its own outcome                                                                                   | n2e      |
| `health/online.go:126` (step 6)                                                            | A stopped member is `online: false` with one notice (new test)                                                          | n2e      |
| `sweep/sweep.go:53` (SW2)                                                                  | Each holding on its own; one failure is in its Result                                                                   | n2e      |
| `seat/release.go:276` Announce                                                             | A held lock is "not yet", not an error                                                                                  | n2c      |
| `apply/apply.go:321` roles                                                                 | Promotion needs only the roster state (`active` or `parked`), never liveness                                            | n2b, n2d |
| `host/apply.go:749` waitReady                                                              | `team apply` waits only for its own homeserver                                                                          | n2d      |
| `rooms/rooms.go:412` (RE7)                                                                 | Invites are made and never waited on                                                                                    | n2a      |
| `notice/notice.go:65` (NT1-NT3)                                                            | Bounded read, one post per run id, a transient failure bounded to 10 min                                                | n2e      |
| SPEC-n3 SD2, SD4                                                                           | A send to an offline member is sent; one TIMEOUT                                                                        | n3d      |
| SPEC-n3 SY8, SY9, SY14                                                                     | Own failures stop only that team; presence is display                                                                   | n3c      |
| SPEC-n3 KP2, LN3                                                                           | A link that cannot stay up is `failed` and said; a CLI call answers 69 in under 300 ms                                  | n3c      |
| SPEC-n3 EN3, EN5                                                                           | The ended run's notice is posted once, by the next take or the host                                                     | n3c      |
| SPEC-n3 DV6, HK2                                                                           | The member's own notices; Stop blocks only its own session, once per pending set                                        | n3e      |

## INTENDED

| Location                                                  | What waits or refuses, for that member alone                                                                 | Slice    |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | -------- |
| A 4.3 and 7.2, line 920                                   | `--to` names a parked or removed member: refused by roster state, an owner act, not liveness                 | n3d      |
| A 6.5, lines 713-717                                      | This member's own untrusted main room refuses its own sends                                                  | n3c      |
| B 2.2.1, line 359 (`wait`)                                | Waits on its own inbox, bounded                                                                              | n3c      |
| B 2.2.4, lines 1160-1166                                  | Own sends retried three times within 30 s against the homeserver                                             | n3d      |
| B 2.6 step 08, lines 1355-1358                            | Own 401, 403 or lost trust stops its team until the bundle changes                                           | n3c      |
| B 2.7, line 1540                                          | Own transcript stall                                                                                         | n3e      |
| B 2.10, lines 1768-1799; SPEC-n3 LN8                      | A removal waits up to 10 s for the link and 30 s for the leave notice, for that seat                         | n3c      |
| C 3.9.2 absent phase step 4, lines 1775-1786; SPEC-n3 LX8 | The removed member's own seat, 60 s, then `DEFERRED ... token revoked`                                       | n3b      |
| C 3.7.3 release, lines 1252-1263; `seat/release.go:247`   | Its own seat lock, 60 s, only while the session is restorable                                                | n2c      |
| C 3.7.3, line 1164                                        | `member return` refused while that seat is held                                                              | n2c, N4  |
| C 3.9.4, lines 1916-1919; BD2                             | `ccy-teams restart` or `lxc restart` of that member; a verb with no link exits 69 at once                    | n3b, n3c |
| C 3.9.2 step 07, line 1741; SPEC-n3 LX6                   | `place-bundle` 75 under a held seat is that seat's `DEFERRED`                                                | n3b      |
| D 5.8, lines 1066-1069                                    | A remote member needs the owner's mark: management, not liveness                                             | N6       |
| C 3.12.2, lines 2287-2295                                 | `needs: reissue` bound to that handle                                                                        | n2g, N4  |
| SPEC-n3 RN2                                               | The member's own launch: a seat held by another live session is 75, and its own link not ready in 30 s is 78 | n3c      |
| SPEC-n3 RC1                                               | A hot add of a seat held elsewhere is refused for that seat; the others carry on                             | n3c      |
| SPEC-n3 SY12, SY13                                        | Own freeze or full disk, bounded, own team                                                                   | n3c      |

## Design text added

[DESIGN-v2-FRAME.md, "Offline members never block (binding)"](../DESIGN-v2-FRAME.md#offline-members-never-block-binding),
commit d545eabcf. It holds:

- the rule;
- what offline means in v2: a free seat lock, a container stopped or missing, the link down, no
  sync, a seat nobody holds, a member declared but not yet created, a coordinator still only
  invited;
- what may never fail, refuse, exit non-zero, wait or repeat because of an offline member;
- the one exception: an operation that targets that member alone, or a member's own verbs
  reporting its own link;
- how an offline member is shown: the status record, member-scoped problems, the LXC record and
  apply lines, the rooms' notices;
- the chapter sections that must honour it, B3 to B13 above.

DESIGN-v2.md's index points at it. Spec amendments:

- SPEC-n2e VW5, SR1 and HT6, in the two fix commits;
- SPEC-n3.md OF1 (a rule for every N3 slice) and n3b's LX2, LX3, LX6 and LP1 (6cf0dc7fa).

## Fixed and committed, and what is left

Fixed in built code (n2e): B1 (181390f52) and B2 (f9dfa4c3b). Both were done test first, run
with `-race`, and passed `qa-changed`. Coverage: `status` 94.8%, `health` 93.7%,
`health/view` 93.1%. The proof of B2 is the three `membersProblem` assertions.
`TestAStoppedMemberIsShownOfflineAndIsNoTeamProblem` passed before the change too, so it is a
regression guard only.

Review round 1 (`261022-offline-members-review.md`, FIX-FIRST, 6 medium and 6 low) led to these
changes:

- commit c88b3eacd: `member` crosses the child's view wire (SPEC-n2f CO3, finding 05). VW5's
  invited coordinator is no longer dropped; it is a problem naming the coordinator, so it is
  shown on that member's row and is never the team's (finding 07, which supersedes B1's first
  form). `status.View.TeamProblems()` is the one split that team readers use (finding 09).
- The FRAME:
  - it separates "shown offline", liveness as A R27 defines it, from "never blocked on" (a failed
    link, a quiet sync, an only-invited coordinator; finding 04);
  - the exception no longer names a container-scoped apply that does not exist (finding 01);
  - the raise remedy rests on A 3.1's `features` rule (finding 11);
  - a probe of one member is bounded (finding 06).
- SPEC-n3:
  - LX2: a missing container follows stopped's `SKIPPED`/`DEFERRED` split under every scope, and
    gets the record status `missing`. The `--only C` and `declare check` clauses are gone
    (findings 01, 02, 10).
  - LX6: the `lxc-info` re-check also covers LX5 and SU2 failures. An entry deferred mid-phase
    takes no further step, keeps its handle and is left out of `seats` (finding 03).
  - LP1: each probe is bounded to 5 s; a running guest with no seat directory is free (finding 06).
  - LX9: names `missing`.
- B5's "left" now includes C 3.9.1 (finding 12).

Left, per slice:

- **n3b** (unbuilt): build LX2, LX3, LX6 and LP1 as amended (B5 to B8), and LX9's `missing` status.
  C 3.9.1 and 3.9.2 still say that a missing container fails the phase, and C 3.9.3 lacks
  `missing`.
- **`status` verb and the N4 board** (unbuilt): exit 1 and the team row count only
  `View.TeamProblems()` (B3). C 3.11.3's text still says "any problem".
- **n2e view and C 3.6.4**: `coordinator_wanted` is to be shown as team state, not a problem (B4).
  The code does not emit it today.
- **N6**: B10 to B13. These are D text fixes, owned by the migration slice when it is specified.
- **Future major**: B9, A 3.2's raise.
- **n3c to n3f**: nothing blocks. OF1's stopped-beside-running test is owed by each slice. The
  n3c handoff was not changed, since none of its items blocks.

Out of the rule's scope, noted only: C 3.9.2 steps 03 and 05 fail the whole phase when one
running guest fails a check (forge login, `allow_from`). That couples a broken member to the
others, but the member is not offline. The owner may want each entry to be its own `FAILED`.
