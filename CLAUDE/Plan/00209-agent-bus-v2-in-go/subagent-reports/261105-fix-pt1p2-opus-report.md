# PT1, P2's two readings (issue 156): the record that tells them apart, and what a failure prints

Branch `fix/pt1-p2-readings-156`, from `origin/feat/bus-v2-n3e-wire`. Test files only; no
production file changed (two plants were put in and reverted, below). Seed 57 stays in `pt1Fixed`,
and that list was not touched.

## 1. What each reading predicts, from the code

What the code guarantees (`engine/recv.go`, `engine/deliverloop.go`, `link/server.go`):

- A delivery's awaiting entry, with the items handed to its nonce, is written to the store
  before its user line goes (`await`; "not persisted" means "not sent"). The entry leaves the
  store only by its proof (its line found in the transcript), by `unawait` (the link judged
  nothing written: the items go back to pending, no repeat), by `declareMissing` (moved to
  `missed`), or by a restart's `returnEarlierRun` (dropped).
- A recv hands out only items with `Handed == nil`. An item handed to a delivery's nonce cannot be
  handed to a recv, and the reverse.
- A failing recv's items go back by `Return` (`settleStore`: `Handed = nil, Repeat = true`), called
  by the link once the client's connection closes, so **asynchronously to `cli.Run`'s return**.
  The planted run below shows it: at the mark the item was still `handed to <the recv's ack>`.
  A delivery after the return copies `Repeat` into the entry's `repeat:` line.

What each reading predicts, for the copy P2 judges (index at or after `marked`, no repeat line):

| record                                                                                               | (b), the harness                      | (a), production                                                                                                                                                                                 |
| ---------------------------------------------------------------------------------------------------- | ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| copy first seen (harness sequence) vs the mark's sequence                                            | after                                 | after                                                                                                                                                                                           |
| copy carries a repeat line                                                                           | no                                    | no                                                                                                                                                                                              |
| a second copy                                                                                        | not required                          | not required                                                                                                                                                                                    |
| the copy's delivery nonce among the store's in-flight deliveries of the item **before the recv ran** | **yes**: it began before the hand-out | **no**: it began after the recv started (in flight at the mark: a redelivery after the return that lacks the line; in neither: begun after the mark, or taken back by `unawait` though written) |

So the sequence record the brief names (each copy's landing against the moment `marked` is
sampled) **does not** separate the readings by itself: under both, the unmarked copy is first
seen after the mark. The smallest extra record that does: each copy's delivery nonce (its marker
line, `<<teamchat <nonce> i/n>>`) and the store's in-flight nonces for the item (awaiting and
missed entries holding it) read just before the failing recv runs. A second snapshot at the mark
(store read just before the transcript is counted) splits (a) into its two sub-cases and shows
the item's state.

One consequence worth the reviewer's eye: under a correct link, (b) cannot happen in a seed run at
all, because a delivery in flight holds the item and the recv then cannot hand it out. A (b)
reading therefore also means the recv handed out an item a delivery held (handed exclusivity,
P11's ground) unless the item's state in the snapshots says otherwise; the printed reason says so.

## 2. What is built

All in `src-go/internal/teamchat/member/wiring/`, tag `busfailpoints`:

- `pt1_model_test.go`: `pt1Copy.nonce` (read from the entry's marker line); `pt1Snap`, `pt1Mark`,
  `pt1Seen.marks` and `pt1Seen.firstSeen`; `pt1Classify` (the reading of one judged copy, and why),
  `pt1Place`, `pt1Record` (the printed record). `pt1Repeats`' returned-item finding keeps its first
  line byte for byte and adds, indented under it, the reading with its reason and the record.
- `pt1_run_test.go`: the runner's `seq` counter; `copies()` gives each copy the next sequence number
  the first time a read holds it (the transcript only grows, so a copy keeps its place); `seen()`
  passes `marks` and `firstSeen` (the transcript read is taken before the literal, so `firstSeen`
  is complete).
- `pt1_redeliver_test.go`: `recvFail` reads the store (`storeAt`, at its own sequence number)
  before `cli.Run`; `mark` reads the store again and then counts the transcript, recording both;
  `pt1SnapOf` is one item's snapshot. A store that cannot be read is recorded as such, never a
  failure of the run. New real-run row `TestPT1MarkBeforeAnInFlightDelivery`.
- `pt1_readings_unit_test.go` (new): the forced timelines and the print and verdict tests.

The classifier, in order: no mark record, store unread before the recv, no nonce, or a copy first
seen at or before the mark's sequence (a record that contradicts itself) is **neither**; the nonce
in the before snapshot is **(b)**; otherwise a link crash in the run is **neither** (a restart drops
an earlier run's in-flight entries, so absence proves nothing); otherwise **(a)**, saying whether
the delivery was in flight at the mark.

What a P2 failure prints now (from the real (b) row's log):

```
P2: $<id> was returned to pending marked repeat and its copy 1 in the transcript carries no repeat line
    reading (b), the harness: delivery 8a753eef was in flight before the recv ran, so it began before the hand-out, its copy rightly carries no repeat line, and the mark (seq 2) was sampled before it landed; a delivery holding the item while a recv handed it out is itself P11's question, read the item's state in both snapshots
    record of $<id>:
      before the recv (seq 1): store read, $<id> pending, handed to 8a753eef, in the ledger, in flight [8a753eef]
      at the mark (seq 2): 0 copies in the transcript, store read, $<id> pending, handed to 8a753eef, in the ledger, in flight [8a753eef]
      copy 1: delivery 8a753eef, no repeat line, first seen at seq 3
```

## 3. What each test was shown to catch

- `TestPT1P2ClassifierNamesTheReading` (eight forced timelines: (b); (a) redelivery in flight at
  the mark; (a) begun after the mark; neither for no record, store unread, no nonce, a crash, a
  copy first seen before the mark). RED on the stub classifier (all eight unnamed). A plant in the
  classifier, the before snapshot swapped for the at snapshot in the (b) case, made the
  "redelivery begun after the return" timeline fail (named (b)); reverted, green.
- `TestPT1P2FailurePrintsTheReadingAndTheRecord`: asserts the printed text holds the old first
  line, the reading, both snapshot lines and the copy line, for (b) and for (a). RED on the stub
  (the finding held only its first line).
- `TestPT1P2VerdictIsTheRecordsNot`: every timeline's first lines equal the old finding exactly and
  equal those of the same observation with the record removed; with the copy marked repeat, no
  finding. This one was green on the stub too: it is a guard that the record never changes the
  verdict, not something the stub broke. The existing `TestPT1RemovalAndReturnedItemChecks` and
  `TestPT1ChecksFireOnTheirOwnProperty` cases are unchanged and green.
- `TestPT1ReadsTheDeliveryNonceOfEachCopy`: RED on the stub (nonce empty).
  `TestPT1ReadsTheTranscriptBackIntoCopies` now expects the nonces.
- `TestPT1MarkBeforeAnInFlightDelivery` (real keeper, link and fake Claude Code): the inbox held, an
  item's delivery waited for until its awaiting entry is in the store, the item marked then (a mark
  sampled before a delivery in flight lands, which is reading (b) by construction), the inbox
  released. P2 fails with its unchanged first line and reads (b) (the output above); the same item
  marked after its copy landed gives no finding. It forces the harness's timeline; it does not
  run a recv, because a correct link cannot reach (b) through one (section 1).
- Reading (a) over a real run, planted: `settleStore` changed to `it.Handed = nil` (no repeat), then
  `TestPT1Redelivery` failed with
  `reading (a), production: delivery dc6bd48b was in flight neither before the recv ran nor at the mark ...`,
  before snapshot `pending, in flight []`, at the mark `0 copies ..., pending, handed to d72561ae, in flight []`,
  copy 1 `delivery dc6bd48b, no repeat line, first seen at seq 4`. Reverted (`git status` clean
  on `engine/`); the row passes again.

Not claimed: no test exercises the "taken back by `unawait` though written" branch of (a) over a
real run, nor the missed-entry snapshot (180 s of quiet); both are named in the printed reason and
the unit timelines only.

## 4. Gates

- `go vet -tags busfailpoints ./internal/teamchat/member/wiring/`: clean. `gofmt -l`: clean.
- `go test -race -tags busfailpoints -shuffle=on -count=2 -run TestPT1 ./internal/teamchat/member/wiring/`:
  `ok` in 261.0 s (every PT1 test twice, seed 57 among the fixed seeds).
- `./scripts/qa-changed.bash --base origin/feat/bus-v2-n3e-wire`: see section 7.

## 5. Suggested comment for issue 156 (for the coordinator to post)

> The PT1 harness now records enough to tell the two readings apart (branch
> `fix/pt1-p2-readings-156`). Seed 57 stays in the gate. When P2 fires on an item a failed recv
> returned, the finding keeps its first line and adds, under it, the reading and its record:
>
> - `before the recv (seq N)`: the store just before the failing recv ran: the item's state and
>   the nonces of its deliveries then in flight (awaiting or missed entries).
> - `at the mark (seq M)`: the store just before the transcript was counted for `marked`, and the
>   count.
> - `copy K: delivery <nonce>, repeat line | no repeat line, first seen at seq S`: each transcript
>   copy of the item, the delivery that carried it, and the harness's sequence number when a read
>   first held it.
>
> How to read it: `reading (b), the harness` means the copy's delivery was in flight before the
> recv ran, so it began before the hand-out and its copy rightly has no repeat line; the harness
> sampled before it landed. Since a correct link never hands out an item a delivery holds, check
> the two snapshots' item state too: an item `handed to <nonce>` in the before snapshot that the
> recv still printed is a hand-out exclusivity defect. `reading (a), production` means no delivery
> of the item was in flight before the recv: the copy's delivery began after the hand-out and
> lacks the line; `in flight at the mark` points at a redelivery after `Return` (`settleStore` or
> the render), `in neither` at a delivery begun after the mark or one the link took back as
> unwritten (grep `link.log` for the nonce: "nothing of the user line was written"). `neither reading` says which part of the record is missing (no record, store unread, no nonce, a link
> crash in the run, a contradictory record). Order of the sequence alone cannot decide: under both
> readings the copy is first seen after the mark; the nonce against the before snapshot does.

## 6. Not done, and findings

- Not done: a real-run proof of the `unawait`-though-written and missed branches; P2's generic
  check ("copy after the first is not marked repeat") gets no reading, by scope.
- Finding: (b) as issue 156 states it is unreachable through a recv on a correct link (section
  1); if it is ever printed from a seed run, the defect is the hand-out's exclusivity, not the
  harness's sampling alone.
- Finding: `Return` runs after `cli.Run` returns (the link sees the connection close), so at the
  mark a returned item can still be handed to the recv's ack; the record shows it, the verdict is
  unaffected (a later delivery waits for the return).
- The mark now reads the store before the transcript, which adds one file read before the count;
  the count is the same quantity as before.

## 7. qa-changed

(filled in below after the run)
