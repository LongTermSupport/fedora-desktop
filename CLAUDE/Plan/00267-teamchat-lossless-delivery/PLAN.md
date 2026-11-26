# Plan 00267: teamchat lossless delivery

**Status**: In Progress (questions answered; build test-first, batch by batch)
**Created**: 2026-11-26
**Owner**: dev
**Priority**: High (owner's standing rule, marked MAJOR)

## Overview

The owner's standing rule (26 Nov): "teamchat messages must be guaranteed delivery or bounce
... premium first class signed for delivery. The message transport must be totally lossless."

Today a sender sees only that the homeserver accepted its message. On the receiving side, many
verdicts discard an addressed message, and nobody is told: rate, the delivery budget, stale,
forge, relay, render, re-fetch, team removal, redaction and others. One host seat lost every
PEER message for a day this way (register R69). Delivery to the session is already proven
properly: the link waits until the session's transcript shows the delivery's nonce, not just a
socket write. But only a HUMAN sender ever hears that back.

This plan makes every addressed message end, for its sender, in exactly one visible state:
delivered (proven in the session), held (with the reason, delivered later), bounced (with the
reason), or recorded (an ack taken but not shown). The full verdict table (48 rows), the
delivery path as it is, and the design are in [DESIGN.md](DESIGN.md), every claim with its
file and line.

## Goals

- Each addressed message reaches exactly one terminal state the sender can see: delivered or
  bounced (held is the visible interim). "Accepted by the homeserver" is not delivery.
- Every receive verdict that discards an addressed message bounces it. DESIGN.md section A
  lists each verdict and says whether it bounces or why it is not a loss.
- Rate and budget limits delay a message (held, delivered later) and never drop it.
- A sender can ask for a sent id's state per addressee (`teamchat status <id>`). A member can
  list, and re-deliver, what it dropped (`teamchat dropped [--redeliver <id>]`). Neither needs
  link.log.
- A member on a kit too old to send receipts is shown to senders as "cannot confirm".

## Non-Goals

- Changing who is trusted. Drops of outsiders' events and of forged content stay drops; only
  the report back is added, and an outsider is never bounced to.
- A cryptographic signature per message. The sender is attested by the homeserver, as today.

## Tasks

### Phase 0: decisions

- [x] ✅ **Task 0.1**: The three open questions in DESIGN.md section D, answered by the
  coordinator on 26 Nov as engineering calls under the owner's rule:
  - **Receipts go in the reports room.** A receipt is for its sender, and an old-kit sender
    cannot act on one, so a `schema` drop in main per receipt would buy nothing.
  - **Stale on replay is delivered late, never dropped.** It is marked late, with the original
    send time, and the receiver judges it. The one exception is a verb that means something
    only while its sender waits (a halt or sync ping whose sender has given up): it is bounced
    as `expired`, so the sender is told.
  - **A bounce to a human** is a reply in the room and thread their message came from, which
    they see in their client. A human's message to the coordinator arrives in the questions
    room, so its bounce goes there, never to main.

### Phase 1: protocol (DESIGN.md C1, C7)

- [x] ✅ **Task 1.1**: The `teamchat.receipt` key (v 1), its decoder and closed reason set,
  added inside protocol major 1. Test first, with corpus and agreement tests.
- [x] ✅ **Task 1.2**: A `kit` field on every typed send, ignored by old kits.
- [x] ✅ **Task 1.3**: The receipt-of-receipt guard (one receipt per item and addressee).

### Phase 2: receiver bookkeeping (C2, C6)

- [x] ✅ **Task 2.1**: A bounded drop ledger in the store that refuses loudly when full, never
  drops. Built: the store's `dropped` (512 entries, each event once, kept a week by
  `PruneDropped`) and `owed` (256 receipts, once per item, addressee, state and reason), both
  refusing when full (`TestTheDropLedgerRefusesWhenFullAndNeverDrops`,
  `TestOwedReceiptsAreOncePerItemAndRefuseWhenFull`); a batch's bounces are written there or the
  batch is refused (`TestABouncedDropIsLedgeredAndOwedOnce`). The ledger lists only the drops
  this member bounces, so an outsider's flood cannot fill it.
- [x] ✅ **Task 2.2**: Bounce selection: addressees only, with the coordinator as bouncer for
  undecodable content and for unheard human posts. Built: `proto.Judge.Bounce` and
  `proto.RawAddressees` (`TestBounceSelection`, 20 cases), called by teamsync's `take`
  (`TestADropAddressedHereIsBounced`).
- [x] ✅ **Task 2.3**: A durable receipt queue and its sender loop. The existing `m.read` queue
  stops losing when full. Built: the `m.read` queue, full, keeps each room's latest receipt
  (`TestAFullReceiptQueueKeepsEachRoomsLatest`), and the rule `project_store_refusal_lost`
  holds every other store add to returning its refusal. The owed receipts' sender posts each
  in the reports room (D1) under `proto.ReceiptTxn`, and keeps it through any failure
  (`TestAnOwedReceiptIsPostedInTheReportsRoom`, `TestARefusedOwedPostIsKeptAndPostedOnTheBackoff`,
  `TestAnOwedReceiptPostedTwiceIsOneEvent`); a failed record moves nothing in memory
  (`TestAFailedOwedSentKeepsTheReceipt`).

### Phase 3: never lose under limits (C3, C4)

- [x] ✅ **Task 3.1**: Rate and budget hold the item (`NotBefore`) instead of dropping it. A full
  agent share of pending bounces `rate`. Built: `proto.Window.Reserve` gives the window's next
  free slot (`TestReserveGivesTheNextFreeSlot`); rule 10 delivers an agent event past the
  receive window with that slot as `Verdict.NotBefore` (`TestVerdictRate`, routing corpus
  updated), and teamsync's budget reserves its own slot instead of a `HeldBack`
  (`TestAFloodingPeerIsHeldToTheNextFreeSlot`); `Pending.not_before` holds the later of the two,
  only on an agent item (`TestAHeldItemsSlotIsKeptAndChecked`). The delivery loop waits for the
  slot (`TestAHeldItemIsDeliveredAtItsSlot`); `recv` hands a held item at once, since the session
  asked for it. A held item that finds pending's agent share full is bounced `rate` through the
  drop ledger and owed receipts of Phase 2 (`TestAHeldItemPastTheAgentShareBouncesRate`). No
  item is held back now, so the `held back:` line (A 11.4) is never written; Phase 5 decides its
  text with the `held(rate)`/`held(budget)` receipts of Phase 4.
- [x] ✅ **Task 3.2**: A transient forge failure retries to a deadline. A stale item on replay is
  delivered marked late with its send time; a waiting-only verb whose sender gave up bounces
  `expired`. Built, the replay half: `proto.Judge.Replay`, set for a resume's answer and for
  `Refetch`, delivers a stale item with `Verdict.Late` and `Pending.late`, rendered as a `late:`
  line (`TestVerdictStaleOnReplay`, `TestAStaleMentionReadAtTheStartIsDeliveredLate`,
  `TestARefetchedItemPastItsAgeIsDeliveredLate`, golden `late.txt`); a stale `halt` or `sync`
  (`proto.WaitsOnSender`) there is dropped and bounced `expired`. Off replay, stale is still a
  bounced drop. The forge half is Task 3.2a's durable hold.
- [x] ✅ **Task 3.2a**: The forge hold is durable (the coordinator's ruling of 26 Nov: no path to
  a silent loss; a hold in the link's memory was refused, because the link resumes from the
  stored `next_batch`, past a held ping, so a restart would lose it unbounced). A check that
  gives `unresolved` or `forge-rate` (`proto.ForgeTransient`) takes the ping into pending
  unproven: `Pending.forge_until`, the deadline `proto.ForgeRetryLimitMs` (10 min) after it was
  first judged, and `not_before` its next check, `proto.ForgeRetryEveryMs` (30 s) on. Its
  verdict is kept out of memory, so the delivery loop and `recv` re-fetch it (`Team.Refetch`,
  which runs the check) before it is shown. The engine's `forgeHeld` keeps a check still failing
  before the deadline to the next slot, and at the deadline, or on `provenance`, drops it into
  the drop ledger and bounces it to its sender; a refused bounce keeps the item. A restored item
  whose re-fetch fails transiently is given the same mark. Pending's agent share bounds the
  unproven pings (past it, bounced `rate`). Tests: `TestATransientForgeFailureIsTakenUnproven`,
  `TestAnUnprovenPingStillFailingIsHeldToItsNextCheck`, `TestAnUnprovenPingIsBouncedAtItsDeadline`,
  `TestAnUnprovenPingThatProvesIsHanded`, `TestAPersistedUnprovenPingIsCheckedAgainBeforeItIsHanded`,
  `TestAnUnprovenPingSurvivesALinkRestart` (an engine restart: a fresh engine over the persisted
  store, with a fake team, delivers it or bounces it at its deadline; teamsync's resume from the
  stored `next_batch` past the ping is not exercised, as the store, not the re-read, holds it),
  `TestAnUnprovenPingsDeadlineIsKeptAndChecked`,
  `TestForgeTransient`; SD5-r1/r2/r4/r8/r11/r12 changed to take the ping unproven.

### Phase 4: receipts at each point (C2)

- [x] ✅ **Task 4.1**: delivered at the transcript proof and at `recv` + ack; held on each held
  transition; bounced at each verdict and at the post-delivery loss points (render, rejudge,
  removal, redaction); recorded for a recorded ack. Built in `member/engine/owe.go`: each point
  queues the receipt in the store's `owed` list (Task 2.3's sender posts it); a PEER or PING
  sender is owed every state, a HUMAN only a bounce (its `m.read` stays its delivered), a
  TIMEOUT or NOTICE none. Delivered at `prove`, `findLate` and an ack; held at an item added held
  (`team.Item.Held`: rate, budget, the forge reason, from teamsync's `heldWhy`), added while a
  socket session is not live (`absent`), declared missing, refused by the full pending list
  (`full`, a write of its own), and a forge re-check still failing; bounced at render (both drop
  sites), a re-fetch judged out (its bounce reason) or `gone`, a redaction, and a removal
  (`team.Team.FlushOwed` posts its bounces and the receipts owed beside the floor marker, before
  the store goes); recorded for an ack or late answer not shown as an item. A terminal receipt
  replaces a held one still queued; a refused receipt refuses its write whole (#443: an ack or
  proof stays unsettled until the queue has room). Tests: `owe_test.go` (14), teamsync
  `TestHeldWhy`, `TestFlushOwedPostsEachReceipt`. Not built: held(absent) for items already
  pending when a live session goes down (only items added while it is down owe it).

### Phase 5: verbs and text (C5, C6, C7)

- [ ] ⬜ **Task 5.1**: `teamchat status <id>` and `--all-open`.
- [ ] ⬜ **Task 5.2**: `teamchat dropped [--since] [--redeliver <id>]`. This also closes register
  R71's lost-items verb.
- [ ] ⬜ **Task 5.3**: Kit display in the roster view; the skill text; the help defines every
  status count (R71).

### Phase 6: proof (DESIGN.md D)

- [ ] ⬜ **Task 6.1**: A table-driven test per bounce row, each one: send, force the drop, assert
  exactly one bounce, the sender's `status` shows it, and the receiver's `dropped` lists it.
- [ ] ⬜ **Task 6.2**: Delivered only on proof (a socket write alone is never delivered);
  absent, compacting and restarting sessions are held, then delivered.
- [ ] ⬜ **Task 6.3**: Old-kit interop, and a failpoint between the drop-ledger write and the
  receipt send that yields exactly one receipt after a restart.

### Phase 7: turn efficiency (the owner, Q19, 26 Nov)

The owner: "teamchat should try to buffer events messages etc and batch deliver info, batch
responses should also be encouraged from agents." Then, 26 Nov 09:10Z (relayed by the
coordinator): "I'd suggest any unfetched status receipts are sent along with the next batch. Ask
fedora dekstop to generally look for turn efficiency opportuntities and make a plan to get this
implemented - a single turn carrying multiple types of info, status, mesages etc at once. Its
not an API its an agentic interface - design it this way. I see no reason to limit to 8
messages but we do need to limit turn/message size by volume"

So a delivery is designed for an agent's turn, not as an API call: one delivery carries every
kind of information pending for the session at once, each kind in its own section, and is
limited by volume only.

Before this phase: one delivery carried up to 8 entries in 12 KiB, one team's pending at a
time (`member/deliver/deliver.go:112-150`). The deliver loop sent as soon as something was
pending and then waited `paceWait` 2 s (`member/engine/deliverloop.go:21-26`); that is a pace,
not a window, so a burst was split across the first delivery and the next. Now the entry cap is
gone (Task 7.6): a delivery holds as many entries as fit `MaxDeliveryBytes`, 12 KiB
(`proto/delivery.go:17`, `Fit` at :160). Delivery debouncing (Plan 00209 Task 3.5, issue #129)
is built here, in `member/engine/coalesce.go` (Tasks 7.1 and 7.5), which closes that task.

- [x] ✅ **Task 7.1**: A coalesce window before a delivery: items for one session are held for a
  short quiet bound with a ceiling, then delivered as one. A HUMAN item and a halt ping skip the
  hold. Test first, on the fake clock. This supersedes Plan 00209 Task 3.5, whose gate (after
  Plan 00246 Task 8.8) the owner's Q19 lifts for this work (the coordinator, 26 Nov).
- [ ] 🔄 **Task 7.2** (revised by the owner, 26 Nov 09:10Z): A receipt never gets a delivery of
  its own. An unfetched receipt (one the sender has not read by `status`) rides in the next
  delivery, folded into a compact status section; a bounce likewise. Done: a receipt is never
  an item, so it can never make a delivery (`TestAReceiptIsNeverAnItem`, 5ad4357b8). To do: the
  status section carrying the unfetched receipts and bounces, once batches 2-4 send them; it is
  one more row of `proto`'s `sections` (Task 7.7), with no receipt data to carry yet.
- [x] ✅ **Task 7.3**: Notices from one sender about one member pending together fold into one
  entry, the latest, with an `earlier:` line for each before it (e.g. left then back).
- [x] ✅ **Task 7.4**: The skill text (`member/kit/skill.txt` and the plugin's copy, which must
  stay identical) asks a seat to answer several messages in one reply where it can. The line,
  agreed with the coordinator: "When a delivery holds several messages, or more arrive while you
  work, answer them together: one reply per sender covering each of their messages, not one
  reply per message. A closed verb (ack, done) still goes to its own ping."
- [x] ✅ **Task 7.5**: The human message debounce (the owner asked for it in Q19): a settle
  window on HUMAN items only. Each new HUMAN item from the same human restarts a 3 s wait,
  capped at 10 s from the first, so several quick messages arrive as one delivery and cost one
  turn. 3 s covers a person sending a thought in two or three short posts. The 10 s cap bounds
  how late the first message can be. A halt ping passes straight through. Test first, on the
  fake clock.
- [x] ✅ **Task 7.6**: The 8-entry cap goes (the owner: "I see no reason to limit to 8
  messages"). A delivery is limited by volume only (its bytes on the wire); what does not fit
  carries to the next delivery, and the delivery says how much is still pending. Built:
  `proto.MaxDeliveryEntries` is gone, `Fit` takes the most entries that fit 12 KiB and counts
  the rest on its `<k> more pending` line (`TestADeliveryIsLimitedByVolumeOnly`,
  `TestNoCountOfEntriesLimitsADelivery`, `TestADeliveryIsLimitedByItsBytesOnly`). The fold
  bound `MaxFold` keeps 8 as its own constant, and one turn's re-fetch keeps its bound
  (`maxRefetch`). `recv`'s own 8-item cap (LN4) is a verb's, not a delivery's: TURN-SURVEY.md.
- [x] ✅ **Task 7.7**: One delivery carries every kind pending at once (HUMAN items, PEER
  messages, pings, notices, receipts, bounces, the trailing facts), each in its own section, so
  a turn reads it all. Built: each kind group of A 13.7's order is a section headed by
  `== <nonce> <KIND>, <k> entries: <what they are> ==`, and the trailing facts follow
  `== <nonce> facts: ... ==` (`TestADeliveryIsInSections`,
  `TestEveryKindPendingGoesInOneDeliveryInSections`); the corpus templates, the frame text
  (item 6), A 13.1 and 13.5, and the skill (both copies) say so. Receipts and bounces get their
  section with Task 7.2. One team per delivery stays: every team in one delivery is
  TURN-SURVEY.md item 1, its own design.
- [x] ✅ **Task 7.8**: A survey of the whole agent-side surface for further turn savings
  (deliveries, verb outputs, `status`, `recv` and `verify` outputs, the skill's procedures):
  each opportunity with its saving and its cost, in [TURN-SURVEY.md](TURN-SURVEY.md).

## Success Criteria

- [ ] Every row of DESIGN.md's verdict table marked "bounce" has a passing test that forces it
  and sees the bounce at the sender.
- [ ] No addressed message can be discarded without a ledger entry and a receipt (a test sweeps
  every drop and ignore call site).
- [ ] Live: a message to a seat, held while its session is down, shows "held" to the sender and
  "delivered" once the session is back.

## Delivery & Milestones

- Design: [DESIGN.md](DESIGN.md), read in the code on 26 Nov.
