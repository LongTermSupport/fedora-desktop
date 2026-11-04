# n3c slice review, part A: the data path

Reviewer A (Opus). Branch `feat/bus-v2-n3c` at a3b52b845, diff `727087da9..a3b52b845`, scoped to
`member/engine`, `member/state`, `member/teamsync`, `member/team`, `member/records`, `proto`
(filter, reach, stateevents, verdict), `matrix` (client, sync, fakehs) and `notice`, with their tests.

`go test -race -count=1` passes over all nine packages (output in the n3c worktree at
`untracked/scratch/review-A-race.txt`).

## Verdict: FIX-FIRST

There are three majors. The first (A1) leaves restored items stuck with no way out. The second
(A2) leaves a stale marker on a seat taken with no store. The third (A3) settles the open question
the wrong way round, against the spec.

## The open question: n3-D20 against `proto.ReachedNoAgent`

**n3-D20 is the intended rule.** SY4a repeats it ("A 404 or 403 is cached as an unknown sender
... not counted unheard (it undercounts rather than miscounts)") and asks for the test "a 404
counts nothing".

**The code does not follow it.** `proto/reach.go:23-33` `relatedReaches` answers `false` for an id
the lookup does not hold. So a relation the server refused counts as *reaching no agent*, and a
human message whose only addressing is that relation is counted unheard. That is the overcount
n3-D20 forbids. See A3 for the details and the fix. The handoff's interface comment ("A relation
the lookup misses counts as reaching no agent (n3-D20's undercount)") misreads n3-D20. The code and
`reach_test.go` were built from that comment.

## The two fixes from the last commits

**`ownedFloors` (engine/seam.go:257-266) is correct within a persist.** It works over all of
`Pending`, handed items included, so each owed item is newer than its room's floor after every
`Persist`. A restart with the store is safe. The position resumes. Owed items stay in `pending`.
`rejudge`'s floor check (recv.go:302) no longer drops them, which was the B44 defect W fixed.

**The marker follows the store floor within one link's life.** `commit` (session.go:485-498)
takes Persist's answer. `judgeAll` (session.go:639-651) computes the high-water from `s.floor`,
which is now the store floor. Every later batch still carries `Floors`, and `ownedFloors` lowers
it again. The marker is put only after the store write, so it is never ahead of the disk.
`TestTheMarkerStaysBelowAnOwedItem` exercises this. Its fake engine re-implements the rule (see
A6), and the real rule is covered by the engine test and the Tuwunel run.

Two cases can still go wrong:

- **A seat taken with no store can replay, and in a rare case lose.** The marker stops following
  the store floor at a clean end, and after any kill. See A2.
- **The floor is not raised when the last owed item is acknowledged.** It stays at owed-1 until
  the next batch with an event. See A4.

## Findings

### A1 (major): a restored item whose re-fetch answers 404 or 403 is never delivered or dropped, and it blocks its team's later restored items

- **Where:** `engine/recv.go:120-128` (refetch stops a team at its first error), `recv.go:196-197`
  (`rejudgeAll` skips any `f.err`), `recv.go:259-262` (`render` refuses an item with no content),
  and `teamsync/ln6.go:250-256` (`Refetch` returns the raw `GetEvent` error, and a usage error for
  any room but main).
- **Scenario:**
  1. The link restarts with items P1 (ts 100) and P2 (ts 200) pending. No content is held in memory.
  2. P1's event now answers 404. A history purge, a store-planted id or a lost server event can
     cause this. A 403 does the same.
  3. Each recv sorts by `sent`, fetches P1 first, fails and stops that team's loop, so P2 is never
     fetched.
  4. Neither item is handed out or dropped.
- **Effects:**
  - `ownedFloors` holds the floor and the marker at 99 for good.
  - `hasPending` stays true, so every `wait` cycles recv, GET and log line once a second
    (`waitRetry`) until its deadline, and exits 4.
  - `more` and `inbox` report the items for good.
- **Why it matters:** B 2.7.3 says "one that no longer passes is dropped with its reason". B 2.6.5
  (DESIGN-v2-B line 566) relies on that re-fetch to defeat a planted id, which here sticks instead.
- **Fix:**
  - On `matrix.ErrNotFound` or `ErrForbidden` from `Refetch`, or a non-main room, drop the item
    from the copy in `rejudgeAll` with one log line, like a verdict that no longer passes.
  - Keep transient errors as they are.
  - Do not stop a team's loop at a permanent failure.
  - Test: one restored item that answers 404 ahead of one that answers, so the second is delivered
    and the first dropped.

### A2 (major): SY10's "at a clean end" put is missing, and a restart forgets a marker it still owed

- **Where:** `engine/teams.go:259-273` (`Shutdown` cancels and closes, with no `FlushFloor`). The
  only caller of `FlushFloor` in the tree is `Remove` (teams.go:207). Also
  `teamsync/session.go:732-738`: `knowFloor` sets the floor without `dirty`.
- **Scenario A (replay):**
  1. P is owed and the marker is at P-1.
  2. P is acknowledged. The next answer raises the store floor to 700, and the put is held by the
     10 s coalescing.
  3. The session ends cleanly within that window. Nothing puts the marker.
  4. The next link starts with `knowFloor(700)`, not dirty. In a quiet room the floor never moves,
     so the marker stays at P-1 for good.
  5. A seat taken with no store replays P and everything after it. That is the replay the lead
     asked about.
- **Scenario B (loss):** the store floor drops below the marker when an owed item's
  `origin_server_ts` is at or below the previous high-water, for example after a clock step. The
  lowered marker is coalesced and then lost at a kill or a clean end. A seat taken with no store
  then cuts that item (`cut`: `ts > bd.floor`).
- **Fix:**
  - Put the marker at a clean end, bounded like the removal's 2 s, for example from `EndTeams` or
    `Shutdown` through `FlushFloor`.
  - Have `knowFloor` mark the floor dirty, which costs one PUT per session start, or compare it
    with the marker read at the start.
  - Put a downward move at once, never coalesced.

### A3 (major): a relation the server refuses (404 or 403, or poisoned after 5 transients) counts the message unheard, against n3-D20

- **Where:** `proto/reach.go:19-33`, used through `teamsync/session.go:703-704`. The resolver
  marks such an id unknown (`lookup.go:195-208`) and leaves it out of `resolved`, so
  `memLookup.Event` misses.
- **Scenario:** a human replies in a thread under agent X's post, with no mention, and the root
  answers 404. Every member counts it unheard and logs a line, though it probably reached X. That
  miscounts, where n3-D20 allows only an undercount.
- **Tests:** `proto/reach_test.go:68,71,74` assert the forbidden behaviour ("reply to an unknown
  event": true). SY4a's required test "a 404 counts nothing" is absent:
  `TestAMissingRelationIsFetchedOnce` (judge_test.go:225) uses messages that mention `me`, so they
  are delivered and the unheard path is never reached.
- **Fix:**
  - In `ReachedNoAgent`, treat a set relation id that the lookup misses as *may have reached*, and
    return false (not unheard).
  - Flip the three test rows.
  - Add the SY4a test: an unaddressed reply to a 404 event, with a count of 0.
  - Correct the handoff's interface comment.

### A4 (minor): the store floor is not raised when the last owed item leaves `pending`

- **Where:** `engine/recv.go:476-493` (`settleStore`) and the rejudge and redaction removals.
  Floors are only recomputed in `apply`.
- **Scenario:**
  1. Item P (ts 100) is acknowledged, and in the same window events up to ts 500 were judged and
     ignored.
  2. The store floor stays at 99, though the store owes nothing. B 2.3.2 says the floor is then
     "the room's high-water".
  3. The room goes quiet, so no batch raises it.
  4. A seat taken with no store, or an epoch reset, replays 100 to 500. P is delivered again, and
     carries `restored` only when it is at or before the release time.
- **Fix:** in `settleStore`, when the room owes nothing more, raise the floor to at least the
  settled items' `SentMS`. A high-water kept in the store would be exact. Re-run `ownedFloors`
  afterwards.

### A5 (minor): the resolver holds its mutex across the network fetch

- **Where:** `teamsync/lookup.go:161-188`. `r.mu` is held through `r.fetch`, which calls `GetEvent`.
- **Scenario:** the team goroutine resolves a related event on a slow server, up to the client's
  try limit. During that time, `notices.RecordOwnSend` blocks: a `say` or `reply` that has already
  sent, and the ends' `back`. So does a recv's `Refetch`, which then times out at `refetchWait` and
  delivers none of that team's restored items. Only that team is held, so the offline rule is not
  breached, but the stall is needless.
- **Fix:** collect the misses under the lock, fetch without it, and take the lock again to record
  the results.

### A6 (nit): the floor rule has two homes

`teamsync/world_test.go:456-462` re-implements `ownedFloors` in the fake engine, so a change to
the real rule would not fail the teamsync floor tests. The fix report says "the floor rule has one
home". Either export a tiny `state` helper both call, or say in the fake's comment that the engine
test covers the rule.

### A7 (nit): `records.TeamCounts.SeenTwice` is always 0

Nothing sets `SeenTwice` (records.go:268), though LN5 lists `seen twice`. If it belongs to n3e's
proof, say so in the field's comment or leave it out until then.

### A8 (nit, forward): `ownedFloors` ignores `awaiting`

B 2.3.2 defines the floor as below the oldest item owed "in `pending` or `awaiting`". `Awaiting`
entries carry ids only, with no timestamp. Nothing in n3c writes `awaiting`, but the slice that does
must give the entries a `sent_ms`, or keep the floor before moving items there.

## Checked and found sound

- **Offline rule in the engine:**
  - The state goroutine makes no network call.
  - `refetch` is bounded by `refetchWait` for each team (`TestAnOfflineTeamHoldsNoOtherTeamsItems`).
  - `status` reads memory only.
  - verify and history run on the team's own client.
- **Persist ordering:** memory follows the disk (`changeLocked`), so the marker is never ahead of
  the store. A full pending list refuses the whole batch and holds the position.
- **`restoreHanded` at load:** handed items return to pending marked repeat and restored before
  the team starts.
- **Removal:** stop is bounded at 2 s and flush at 2 s. The store is unlinked while the lock is
  held, and `seat.lock` keeps its inode.
- **matrix:**
  - The `Timers` seam keeps the time-limit classification ("timed out" against cancelled).
  - `StatusError` is the one classifier.
  - `FilterJSON` refuses two filters, or one that is not an object.
  - The relaxed limited check accepts only a timeline with no events.
- **notice:** the `LatestOwn` and `Roster` refactors keep the previous behaviour.
- **SeatWatcher:** uses `IN_DONT_FOLLOW` and `IN_ONLYDIR`, and the inotify parser is bounds-checked.
