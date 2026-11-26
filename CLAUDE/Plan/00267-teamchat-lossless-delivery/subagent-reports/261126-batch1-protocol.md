# Plan 00267 batch 1 (protocol): report

Tasks 1.1-1.3 built in `src-go/internal/teamchat/proto/`, test first, with the spec text in
`CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-A-protocol.md` (7.1 rows, 7.6 rule 05, new 7.8).

## What changed

- `receipt.go` (new): `KitLevel = 1`, `MaxKit`, `MaxReceiptTo = 8`; the states
  (`ReceiptDelivered/Held/Bounced/Recorded`, `ReceiptStates`); `BounceReasons` (the 16
  `DropReasons`, then `render gone removed redacted unheard type empty role-changed expired`);
  `HeldReasons` (`absent missing rate budget full forge-rate unresolved`); `ReceiptReasons` (their
  union, each once); `Receipt`; `decodeReceiptObject`; `ReceiptContent` (the wire form, checked by
  decoding it, so batch 2's sender has one composer); `ReceiptOwed` (the loop guard).
- `content.go`: `KeyReceipt` in `typedKeys`, `ReportKeys` and `objectDecoders`; `Content.Kit` and
  `Content.Receipt`; `readKit` reads `kit` once for every typed object inside `DecodeContent`.
- `send.go`: `kitted(members)` appends `"kit": KitLevel` last; used by Ping, Text, Notice, Report,
  Question, Thread and LinkBack (and `ReceiptContent`). Carried (the admin's v1 import) is not
  kitted. Retitle copies the original question's object as it was, so it has `kit` only if the
  original had.
- `verdict.go`: the reports-room branch of `Judge` is now `j.reports(v)`.

## The reports-room verdict: what it did, what it does

Before: `Judge` returned `Fault` for every rostered sender in the reports room, before any decode
(verdict.go, the `PurposeReports` case). Its one consumer is `teamsync.(*sess).faults`
(member/teamsync/reports.go), which logs each Fault or Drop once ("fault <id>" / "dropped <id>:
<reason>") and never makes an item; members' filters do not name the reports room, so it sees
events only by accident. `session.take` treats Fault as nothing.

Now: the outcome is still `Fault` for everything, except that when the sender is a coordinator or
worker **and** the content carries `teamchat.receipt`, the content is decoded with `ReportKeys`:
a receipt that decodes stays `Fault` with `v.Content` holding it (Key, Receipt, Kit); one that does
not is `Drop` with its reason. Humans, the probe and the admin posting a receipt key are `Fault`
undecoded, as before. Why: the later sender verb and receipt reader get the decode from the judge
rather than a second path, and the faults log is unchanged for every valid receipt; a malformed
one logs "dropped <id>: <reason>" instead of "fault <id>". Old kits: unchanged by construction,
since their judge returns `Fault` before decode, and their `typedKeys` lacks the key.

## Decisions taken (conservative, for the lead to confirm)

1. **Held reasons were added.** The brief's closed set has no reason for C2/C4's held states
   (budget, pending full, session absent, missing). A closed set that later grows is a feature
   inside major 1 that older new-kits would drop, so they are in now: `absent missing budget full`
   beside `rate forge-rate unresolved`. Reasons are checked per state: `held` takes only
   `HeldReasons`, `bounced` only `BounceReasons`.
2. **`recorded` takes no reason**, like `delivered`; either with a reason is `schema`.
3. **`to`**: 1 to 8 distinct `@local:server` IDs (any server; the roster check is the judge's later
   job), sorted on read. 8 is an arbitrary small bound: the sender plus what it came through.
4. **`kit`**: optional; present must be an integer 1..999999999, else `schema` (strict on a new
   kit; old kits never look). Decoded for every key, carried included.
5. **The guard takes the raw content** (`ReceiptOwed(content Value)`), not `Content`, so a
   receipt that does not decode is also never receipted. One receipt per (item, addressee) is
   batch 2's bookkeeping; this guard is only "never a receipt of a receipt".
6. **`ReportKeys` now holds the receipt key**, and `engine.recheckReport` decodes a queued report
   with `ReportKeys`. A queued report is always `Composer.Report` output and is re-composed from
   its body, so nothing changes in practice; batch 2 may want it to decode `[]string{KeyReport}`.
7. A content holding two typed keys, one of them the receipt, was a single-key event to an old kit
   and is `schema` now (stricter, never looser).

## Red and green

- Red 1: `receipt_test.go` written first; the package did not build (`undefined: BounceReasons`,
  `Content.Receipt`, ...).
- Red 2 (behaviour, after the types and decoder, before the verdict and the sends):
  `TestEveryTypedSendCarriesTheKit` (no `kit` in the link-back's object), `TestVerdictReports`
  (a bad receipt judged Fault, want Drop schema / version), `TestComposedReceiptIsReadInTheReports`
  (Content empty on the Fault). `TestReceiptReasonSets` also failed on my own wrong count, fixed in
  the test.
- Green: `go test ./internal/teamchat/proto/` ok; `-update` wrote only the new
  `testdata/routing/TestVerdictReports.json`, so no existing corpus file moved with `kit`.

Tests added: `TestReceiptReasonSets`, `TestDecodeReceipt` (every bounce reason accepted),
`TestDecodeReceiptRefuses` (each rejection: of, to, state, reason/state pairing, at, kit, version,
relation, main room, two keys), `TestKitIsReadOptionally`, `TestEveryTypedSendCarriesTheKit`,
`TestReceiptOwed`, `TestVerdictReports` (pinned to the routing corpus), and
`TestComposedReceiptIsReadInTheReports` (agreement: composed receipts decode back identically in
the reports room; refused ones are `schema`).

## Checks run by hand

The full Go gate does not run on this arm64 machine (issue #426), so these were run directly:

- `go test -race ./internal/teamchat/...`: still running at commit time (the tree also holds
  the lead's uncommitted member/client and statusline work); its result is in the reply to the lead.
- `golangci-lint run ./internal/teamchat/proto/...`: 0 issues.
- Project rules: the `qa/driver` built and run as `go vet -vettool=<driver>` over
  `./internal/teamchat/proto/`: no findings.
