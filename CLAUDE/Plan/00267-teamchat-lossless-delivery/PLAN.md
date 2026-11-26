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

- [ ] ⬜ **Task 2.1**: A bounded drop ledger in the store that refuses loudly when full, never
  drops.
- [ ] ⬜ **Task 2.2**: Bounce selection: addressees only, with the coordinator as bouncer for
  undecodable content and for unheard human posts.
- [ ] ⬜ **Task 2.3**: A durable receipt queue and its sender loop. The existing `m.read` queue
  stops losing when full.

### Phase 3: never lose under limits (C3, C4)

- [ ] ⬜ **Task 3.1**: Rate and budget hold the item (`NotBefore`) instead of dropping it. A full
  agent share of pending bounces `rate`.
- [ ] ⬜ **Task 3.2**: A transient forge failure retries to a deadline. A stale item on replay is
  delivered marked late with its send time; a waiting-only verb whose sender gave up bounces
  `expired`.

### Phase 4: receipts at each point (C2)

- [ ] ⬜ **Task 4.1**: delivered at the transcript proof and at `recv` + ack; held on each held
  transition; bounced at each verdict and at the post-delivery loss points (render, rejudge,
  removal, redaction); recorded for a recorded ack.

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
responses should also be encouraged from agents."

Today (checked): one delivery already carries up to 8 entries in 12 KiB
(`proto/delivery.go:16-17`, `Fit` at :129-157), one team's pending at a time
(`member/deliver/deliver.go:112-150`). The deliver loop sends as soon as something is pending
and then waits `paceWait` 2 s (`member/engine/deliverloop.go:21-26`); that is a pace, not a
window, so a burst is split across the first delivery and the next. Pings and notices are an
entry each, never folded. Delivery debouncing (Plan 00209 Task 3.5, issue #129: a quiet window
with a ceiling, a halt ping and a human's direct message passing through) is planned, not
built; this phase builds it here and closes that task.

- [ ] ⬜ **Task 7.1**: A coalesce window before a delivery: items for one session are held for a
  short quiet bound with a ceiling, then delivered as one. A HUMAN item and a halt ping skip the
  hold. Test first, on the fake clock. Plan 00209 Task 3.5 held this until Plan 00246 Task 8.8
  (cross-network, still 🔄) is done; whether the owner's Q19 lifts that gate is asked of the
  coordinator. Until then Tasks 7.2-7.4, which do not touch the delivery path's timing, go
  first.
- [ ] ⬜ **Task 7.2**: Receipts never cost a turn of their own. delivered, held and recorded
  receipts are not delivered to a session at all; the sender reads them by `status`. A bounce
  rides in the next delivery as one trailing line per bounced message, unless it is the only
  thing pending, when it waits out the window like any item.
- [ ] ⬜ **Task 7.3**: Notices of one kind about one member in one window fold into one line
  (e.g. left then back).
- [ ] ⬜ **Task 7.4**: The skill text (`member/kit/skill.txt` and the plugin's copy, which must stay
  identical) asks a seat to answer several messages in one reply where it can.

## Success Criteria

- [ ] Every row of DESIGN.md's verdict table marked "bounce" has a passing test that forces it
  and sees the bounce at the sender.
- [ ] No addressed message can be discarded without a ledger entry and a receipt (a test sweeps
  every drop and ignore call site).
- [ ] Live: a message to a seat, held while its session is down, shows "held" to the sender and
  "delivered" once the session is back.

## Delivery & Milestones

- Design: [DESIGN.md](DESIGN.md), read in the code on 26 Nov.
