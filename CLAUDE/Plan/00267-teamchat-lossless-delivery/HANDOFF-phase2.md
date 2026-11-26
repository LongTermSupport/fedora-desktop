# Plan 00267 Phase 2: handoff

Tasks 2.1 and 2.2 are built. Task 2.3 is half built: the `m.read` queue no longer loses a
receipt (532246730). The full account is
[subagent-reports/261126-phase2.md](subagent-reports/261126-phase2.md).

## What is left of Task 2.3: the sender loop for the owed receipts

The queue exists: the store's `owed` list (`state.Owed`, `AddOwed`, `OwedSent`), filled by
`engine.applyBounces` at each Persist. Nothing sends it yet. Build it test first, on the model
of the `m.read` sender:

1. **Engine side.** The model is `engine/receipts.go`. Add `PendingOwed(t)`, which returns the
   list and a channel that is closed when it changes. Add `OwedSent(ctx, t, o)`, one store
   write that calls `s.OwedSent(o)`. The change channel has to fire when `owed` changes:
   `changeLocked` only compares `Receipts` today, at `before := h.store.Receipts` and
   `tellReceipts`. Widen that to `Owed`, or give `Owed` a channel of its own. Add the two
   methods to `team.Receipts`, or to a new interface beside it, and wire it in
   `wiring.go:72`.
2. **teamsync side.** The model is `teamsync/receipts.go`'s loop. For each owed receipt, post
   `proto.ReceiptContent(proto.Receipt{Of, To: []string{To}, State, Reason, At: AtMS})` to the
   **reports room** (decision D1), using `s.c.Send(ctx, reportsRoomID, proto.MessageType, txn, content)`.
   - Derive the txn from the receipt: `tc` plus 32 hex digits of the sha256 of
     `of|to|state|reason`. A post that may have reached the server then repeats the same txn
     after a restart, and Matrix dedupes it per access token, so the failpoint of Task 6.3
     yields one receipt.
   - It must never drop. `ErrNotFound`, `ErrForbidden`, a reports room not joined yet and no
     reports room in the roster are all retries on the backoff, each with one log line. That
     is unlike the `m.read` loop, which drops what it cannot send.
   - Start it in the same place as `startReceipts`.
3. **Tests.** A fake homeserver post, then `owed` empty. A refused post kept, then sent on the
   backoff. The same receipt twice gives one post (the same txn).

Then tick 2.3 in PLAN.md, bump the container and CCY versions, add a changelog entry, and
commit through a temp `GIT_INDEX_FILE` (the worktree is shared).

## Task 7.2

It is not done in code. Nothing reads receipts on the sender's side yet, so there is nothing
unfetched to carry. It needs the Task 5.1 cache of seen receipts first, and then a `proto`
`sections` row. It is not small, and I did not build it.
