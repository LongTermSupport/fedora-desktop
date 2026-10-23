# n3e-9 handoff: proof transitions (DV4, DV5's start; P8-P10)

Branch `feat/bus-v2-n3e-9`, worktree `untracked/worktrees/bus-v2-n3e-9`, base 08e62f9e0. The
first builder stopped at its context budget. The WIP commit holds all the code and tests below;
**the engine package is not green yet** (see "Left").

## Design as built (engine only; no link hunk)

- `member/engine/proof.go` (new) and `proof_test.go` (new, 20 tests).
- The engine owns the watcher. `SetSession` already gives it the transcript, so a proof loop
  beside the delivery loop (`startProof`, socket mode only) holds one `proof.Watch` on
  `e.sess.transcript`, replaced when the path changes, and runs `proofRound` on the state
  goroutine at each change, each kick and once after each new watch. Decision: no link hunk, so
  nothing to conflict with n3e-10 in `member/link`. The seam is the unexported `e.watch` field
  (nil means `proof.Watch`), set by tests in `newWorldWith`'s `ready`.
- The scan runs **on the state goroutine**, bounded: `scanChunk` 1 MiB a call, `scanRound`
  8 MiB a path a round. When a round does not catch up, the loop is poked to continue, and
  nothing is declared missing in that round.
- Memory: `proofState{marks map[path]*pathMark, stop/due/gen (one AfterFunc timer), reread, kick, rounds}`. `pathMark{mark, high, grewAt, grew, failed}`. Growth is judged against `high`,
  the furthest end seen in the inode. A refusal is logged once per distinct error.
- Paths sought: each awaiting entry's own path, from its offset. The session transcript too:
  from the smallest missed offset, or from 0 when it holds no entry (the /clear case). Every
  awaiting and missed nonce of the provable stores is sought in every path.
- `dueAt(a)` is the earlier of `at_ms + 1h` and `max(stop_ms, last growth) + 180s`.
- The timer is armed from `publish()` through `proofChanged()` (arm, then poke), so it is
  synchronous with every store write. `world.armed` counts it (an `engine_test.go` hunk).
- **Re-read at a deadline** (`startReread`): when any entry is due, every mark goes back to
  offset 0 and the transcripts are read whole before anything is declared missing. Without
  this, a file truncated and regrown past its mark between two scans would be missed.
  `TestACutUnseenIsFoundAtTheDeadline` covers it.
- `settleProof`: one write per team per round.
  - proven: the entry leaves awaiting; its items leave pending; the ids go into the ledger when
    the entry was `unknown`; HUMAN items get receipts; floors are raised.
  - missing: the entry goes to missed `{nonce, ids, offset, at_ms}`; the items become
    `repeat`, `held`, handed nil, out of the ledger; then `e.hit(pointAfterMissing)`, a new
    point in `points.go`.
  - late find: still-pending items leave as proven. Items in the ledger, or handed to a recv
    right now, are counted `seen twice` and logged; a handed one also leaves pending, so a
    Return cannot show it a third time.
  - Missed entries older than 24 h are dropped.
- Missing count, n3e-D11: `held.shownMissing`, the missed nonces present at the last proof,
  are excluded from `held.missing()`. The proof re-publishes after setting it.
- `held.seenTwice` feeds status's `counts.seen_twice` through `teamView.seenTwice`.
- Idle (`setIdle`, called from `SetSession`): on the false-to-true transition, every awaiting
  entry without `stop_ms` gets one, in one write per store.
- An earlier run (15b, 15c): `returnEarlierRun` runs inside `BeginRun`'s one write, keyed by
  `s.Run.ID != e.cfg.Run`. Awaiting items go back to pending `repeat` and **not** `held`, out
  of the ledger; awaiting and missed are emptied. The missed entries are dropped because their
  boot times belong to another run's clock. It also pokes the delivery loop. A store not yet
  begun is neither delivered from (the `begun(h)` gate in `deliverloop.go` `look`) nor proven
  (`provable`). That gate keeps entries delivered before BeginRun from being returned as an
  earlier run's.

## Shared hunks (one line each where possible)

- `engine.go`: Engine fields `proof`, `watch`; in `Run`, `e.startProof(ctx, &loop)` inside the
  delivers block, and `defer e.disarmProof()`.
- `teams.go`: held fields `shownMissing`, `seenTwice`.
- `status.go`:
  - `publish` gains `e.proofChanged()`;
  - `statusRow`'s `Missing` becomes `h.missing()`;
  - `teamView.seenTwice`, filled in `snapshot` and given to `TeamCounts.SeenTwice`.
- `seam.go`: `BeginRun` gains `e.returnEarlierRun(s)`.
- `deliver.go`: `SetSession` gains `e.setIdle(f.Idle)`.
- `deliverloop.go`: `look` gains `|| !e.begun(h)`.
- `points.go`: `pointAfterMissing`.
- `engine_test.go`: `armed` counts `proof.stop`.

## Left

1. **A hang under `-shuffle=on -count=4`** (seed 1792716039412185248): the test timed out at
   4m40s in `recv_test.go` `TestARestoredItemIsJudgedAgain` (`newWorld`, cleanup). It may be a
   deadlock reached through `publish -> proofChanged`, or a timer that `Shutdown` does not
   release. Check `untracked/scratch/t1.txt` in the worktree for the goroutine dump. Suspects:

   - `armProof` arms even when `delivers()` is false (no proof loop). The timer then fires
     `onGoroutine`, which is harmless, but check it.
   - `proofRound` via `onGoroutine` inside `Advance`.

   A single `-count=1` run passed every test but one, which is now fixed.

2. Make `points_test.go` cover `pointAfterMissing`.

3. The P10 real-SIGKILL link test (`busfailpoints`, `link/failpoints_test.go` style). Plant a
   store whose awaiting entry has `at_ms = stop_ms = boot-200s` (real `CLOCK_BOOTTIME`) and a
   transcript sized at its offset; start the link with `LinkAfterMissing` armed; after the
   kill, every item is pending `repeat`+`held` and none is in the ledger. Restart; recv shows
   each once.

4. Coverage of 90% or more, golangci with no tags, with `tuwunel` and with `busfailpoints`,
   `qa-changed`, then the report `261022-n3e-9.md`.
