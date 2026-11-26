# Lossless teamchat delivery: verdicts, delivery path, smallest design

Plan 00267's design research, read in the code on 26 Nov 2026 (v2-smoke). Nothing is built.
Paths are under
`src-go/internal/teamchat/`. Each claim carries **[C file:line]** (checked: I read that code) or
**[U]** (unchecked: inferred, or read only in part).

---

## A. The verdict table

### How a verdict is reached and applied (checked)

- `Judge.Judge` runs rules 01-10 in order and the first rule that decides wins [C proto/verdict.go:209-242].
  Routing: a non-message goes to `notMessage`; a schema failure, `seen`, the questions index,
  `self` or a sender with no class end it there; the reports room gives `Fault`; the questions
  room goes to `questions`; everything else goes to `main` [C verdict.go:210-241].
- Teamsync applies each verdict in `take` \[C member/teamsync/session.go:903-941\]:
  - Deliver: the delivery budget `admit` (an item over budget is only counted in `HeldBack`),
    then a `state.Pending` item [C session.go:905-928, teamsync/budget.go:20-32].
  - Ignore: counted (except `seen`), plus the UH1 "unheard" line [C session.go:929-930, 944-957].
  - Drop: `j.drop` counts it under its reason (status `dropped_by`) and writes one link.log line
    [C session.go:931-932, 959-968].
  - Redact: the batch's `Redacts` [C session.go:933-935].
  - Record: `answered` [C session.go:936-937].
  - Index and Fault: nothing here [C session.go:938-939].
- Today a drop is visible only to the receiver, as a counter and a link.log line. Nothing goes
  back to the sender [C session.go:959-968; no send call in take/drop].
- **The order of the checks matters for "addressed".** In the agent path every content check
  (decode, then `checkAgent`) runs BEFORE the addressing test [C verdict.go:450-460]. So a
  malformed agent event is dropped and logged by **every** member, addressed or not. A bounce must
  therefore be emitted only by the addressees named in the event's `to`, or there are N bounces
  per event. When content does not decode, `to` has to be read best-effort from the raw object
  [U: there is no helper today].
- Replay: on resume, events that the floor marker covers, or that are restored, are judged against
  a fresh, empty flood `Window`. So history never fills the receive window (R64)
  [C session.go:261, 874-880]. `seen` still stops re-delivery of anything the store knows
  [C verdict.go:220-221].
- Unparseable JSON is dropped `schema` with no id [C session.go:837-840]. State events are skipped
  [C session.go:842-844].

### Rows (one per reason or outcome)

Legend for "Addr?": **yes** means the event named this member; **n/a** means not a message to
anyone; **unknown** means the content failed before `to` was read.
"Loss today" means an addressed message never reaches the session and the sender is never told.

| #   | Verdict / reason                                                 | Condition                                                                                                     | file:line                                                                                           | Addr?                               | Loss today                                                                                                                       | Proposed                                                                                                          |
| --- | ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| 1   | Ignore `type` (non-message)                                      | event type is not m.room.message and is not an admin redaction                                                | [C verdict.go:246-252]                                                                              | n/a                                 | no                                                                                                                               | not a loss: not a message                                                                                         |
| 2   | Ignore `type` (human msgtype)                                    | a human's non-text msgtype (an image, etc.)                                                                   | [C verdict.go:360-363, content.go:585]                                                              | unknown (decided before addressing) | **yes** if it mentions this member; UH1 counts it [C session.go:997]                                                             | bounce `type`, only by a member it mentions; a human gets the bounce as a reply                                   |
| 3   | Redact                                                           | admin redaction of a valid id                                                                                 | [C verdict.go:248-249]; pending item removed unless awaiting proof [C engine/storewrite.go:315-319] | yes (the item redacted)             | **silent removal**: the sender of the redacted item is not told                                                                  | bounce `redacted` (admin's act, but the sender must see the end state)                                            |
| 4   | Drop `schema` (unparseable)                                      | JSON does not parse                                                                                           | [C session.go:838-840]                                                                              | unknown                             | yes, if it was addressed                                                                                                         | cannot bounce (no id, no sender); count only. Not reachable by a current kit's sender [U]                         |
| 5   | Drop `schema` (rule 02)                                          | bad event id / state_key / redacted / no sender / negative ts                                                 | [C verdict.go:218-219, 267-281]                                                                     | unknown                             | yes, if addressed                                                                                                                | bounce if sender and id are readable, else count only                                                             |
| 6   | Ignore `seen`                                                    | id already judged or stored                                                                                   | [C verdict.go:220-221]                                                                              | —                                   | no                                                                                                                               | not a loss: duplicate                                                                                             |
| 7   | Index                                                            | questions-room question/thread/retitle post                                                                   | [C verdict.go:223-227, 286-309]                                                                     | n/a (index feed)                    | no                                                                                                                               | not a loss                                                                                                        |
| 8   | Drop (questions index decode: `edit`/`schema`/`version`/`size`…) | a malformed question or retitle                                                                               | [C verdict.go:293-296, 303-306]                                                                     | not member-addressed                | no member delivery lost; the question index misses it                                                                            | bounce to the poster (the coordinator) with reason                                                                |
| 9   | Ignore `self`                                                    | own event (an echo)                                                                                           | [C verdict.go:228-229, session.go:948-950]                                                          | —                                   | no                                                                                                                               | not a loss                                                                                                        |
| 10  | Drop `sender` (no class)                                         | sender not on the roster                                                                                      | [C verdict.go:233-234]                                                                              | unknown                             | not a team message                                                                                                               | not a loss: an outsider (never bounce outsiders, to avoid an oracle)                                              |
| 11  | Fault                                                            | any rostered sender in the reports room                                                                       | [C verdict.go:235-237]                                                                              | n/a                                 | no                                                                                                                               | not a loss (no member reads reports)                                                                              |
| 12  | Ignore `not-coordinator`                                         | a questions-room post seen by a non-coordinator                                                               | [C verdict.go:333-334]                                                                              | not addressed to this member        | no                                                                                                                               | not a loss                                                                                                        |
| 13  | Drop `sender` (agent in questions)                               | an agent's non-index post in questions                                                                        | [C verdict.go:338]                                                                                  | to the coordinator                  | yes                                                                                                                              | bounce `sender`                                                                                                   |
| 14  | Drop `sender` (human, `human_text` off)                          | a human in main, and this member does not take human text                                                     | [C verdict.go:348-349]                                                                              | unknown (before addressing)         | **yes** if it mentions this member                                                                                               | bounce `sender` to the human, only by a mentioned member, with the wording "this member does not take human text" |
| 15  | Drop `sender` (human sends carried)                              | a human posts a `teamchat.carried`                                                                            | [C verdict.go:357-358]                                                                              | unknown                             | yes, if addressed                                                                                                                | bounce `sender`                                                                                                   |
| 16  | Drop `schema`/`size` (DecodeHuman)                               | bad body, or too big                                                                                          | [C verdict.go:360-363, content.go:591,593,618]                                                      | unknown                             | yes, if addressed                                                                                                                | bounce, only by a mentioned member                                                                                |
| 17  | Ignore `empty`                                                   | empty human body                                                                                              | [C content.go:620, verdict.go:197-201]                                                              | unknown                             | UH1 counts it [C session.go:997]                                                                                                 | bounce `empty` if it mentions this member, else not a loss                                                        |
| 18  | Drop `edit` (human)                                              | edit whose original is unknown, in another room, or from another sender                                       | [C verdict.go:365-368, 382-388]                                                                     | unknown                             | yes                                                                                                                              | bounce `edit`                                                                                                     |
| 19  | Ignore `not-addressed` (human)                                   | no mention, `@room`, reply, thread or edit of an addressed message                                            | [C verdict.go:369-370, 392-395]                                                                     | no                                  | no for this member; **yes for the human** if no agent at all was reached (UH1 `ReachedNoAgent`) [C session.go:999-1000]          | not a loss per member; **the coordinator** bounces `unheard` to the human (one bouncer, not N)                    |
| 20  | Drop `stale` (human, main)                                       | older than `HumanMaxAgeS` at judgement                                                                        | [C verdict.go:371-372, verbs.go:145-147]                                                            | yes                                 | **yes**: hit on resume after a long absence                                                                                      | bounce `stale`. Better: no staleness for an addressed item judged on replay; deliver it marked late               |
| 21  | Deliver HUMAN + OverLimit                                        | a human over the receive rate                                                                                 | [C verdict.go:374-377, session.go:925-927, 974-977]                                                 | yes                                 | no (delivered, only counted)                                                                                                     | not a loss                                                                                                        |
| 22  | Ignore `admin`                                                   | admin event with no admin key, or a member-event notice                                                       | [C verdict.go:414-415, 424-425]                                                                     | n/a                                 | no                                                                                                                               | not a loss                                                                                                        |
| 23  | Drop (admin decode: `schema`/`version`/`size`/`edit`)            | malformed admin content                                                                                       | [C verdict.go:417-419]                                                                              | unknown                             | yes, if addressed                                                                                                                | the admin is software: log it, and raise a fault to the coordinator, not a bounce                                 |
| 24  | Drop `target` (admin notice)                                     | subject not a member, or bad `to`                                                                             | [C verdict.go:427, 715-722]                                                                         | maybe                               | yes                                                                                                                              | as row 23                                                                                                         |
| 25  | Drop `schema`/`verb`/`ref` (carried)                             | bad carried `from`/verb/ref                                                                                   | [C verdict.go:429-430, 726-738]                                                                     | yes (`to` is one id)                | **yes**: a carried item is a relayed message                                                                                     | bounce to the carried item's `from`, through the admin [U: needs the admin path]                                  |
| 26  | Ignore `not-addressed` (admin)                                   | `to` lacks self                                                                                               | [C verdict.go:435-436]                                                                              | no                                  | no                                                                                                                               | not a loss                                                                                                        |
| 27  | Drop `sender` (agent sends carried)                              | an agent posts `teamchat.carried`                                                                             | [C verdict.go:444-445]                                                                              | unknown                             | yes, if addressed                                                                                                                | bounce `sender`                                                                                                   |
| 28  | Drop `role` (probe non-ping)                                     | the probe sends anything but a ping                                                                           | [C verdict.go:447-448]                                                                              | unknown                             | yes, if addressed                                                                                                                | bounce `role`                                                                                                     |
| 29  | Drop `schema`/`version`/`size`/`edit` (agent decode)             | DecodeContent fails: no or unknown typed key, `v`≠1 or `features`, >16 KiB, an edit, a bad relation           | [C verdict.go:450-452, content.go:300-329, 369-378]                                                 | **unknown** (`to` not decoded)      | **yes**: the version-skew case                                                                                                   | bounce, read `to` best-effort from the raw typed object; else the coordinator bounces it                          |
| 30  | Drop `verb`                                                      | unknown verb                                                                                                  | [C verdict.go:455-458, 571-573]                                                                     | decoded `to` known                  | yes, if addressed                                                                                                                | bounce `verb`, only by addressees                                                                                 |
| 31  | Drop `role`                                                      | verb not sendable by this class                                                                               | [C verdict.go:574-575]                                                                              | known                               | yes                                                                                                                              | bounce                                                                                                            |
| 32  | Drop `schema` (relation)                                         | relation does not fit the verb                                                                                | [C verdict.go:576-577]                                                                              | known                               | yes                                                                                                                              | bounce                                                                                                            |
| 33  | Drop `target`                                                    | bad `to` (count, order, duplicate, not addressable); `check` not to exactly 1; a notice about another subject | [C verdict.go:578-582, 598-609, 560-564, 705-713]                                                   | known (it may not include self)     | yes, for listed members that are valid                                                                                           | bounce `target`, by each valid addressee that is listed                                                           |
| 34  | Drop `ref`/`allowlist`/`excluded`                                | reference grammar or trust class                                                                              | [C verdict.go:584, 643-691]                                                                         | known                               | yes                                                                                                                              | bounce with `RefCause` [C verdict.go:611-630]                                                                     |
| 35  | Ignore `not-addressed` (agent)                                   | `to` lacks self                                                                                               | [C verdict.go:459-460]                                                                              | no                                  | no                                                                                                                               | not a loss                                                                                                        |
| 36  | Drop `sender` (relay to member without human_text)               | a relay to a member that does not take human text (issue #422)                                                | [C verdict.go:461-464]                                                                              | yes                                 | **yes**                                                                                                                          | bounce `sender` to the relaying coordinator                                                                       |
| 37  | Record (ack, or stale answer)                                    | the answer to an own ping is an `ack`, or is stale                                                            | [C verdict.go:466-470, session.go:936-937]                                                          | yes                                 | an ack is consumed by the link (recorded): not a loss. **A stale answer is never shown**                                         | ack: receipt `recorded`. Stale answer: deliver it marked late, or bounce `stale`                                  |
| 38  | Drop `stale` (agent ping/text)                                   | past the verb's answer timeout, or `HumanMaxAgeS` for text                                                    | [C verdict.go:477-478, 488-497, verbs.go:133-147]                                                   | yes                                 | **yes**                                                                                                                          | bounce `stale` (and as row 20, consider not stale on replay)                                                      |
| 39  | Drop `rate`                                                      | sender over `recv_per_sender_minute`                                                                          | [C verdict.go:479-480, 405-409]                                                                     | yes                                 | **yes**: the comment says so [C verdict.go:474-476 on relays]                                                                    | **queue**: hold as pending with `NotBefore`, deliver late, and receipt `held(rate)`                               |
| 40  | Relay exempt from stale and rate                                 | a relay is not window- or age-checked                                                                         | [C verdict.go:473-476]                                                                              | yes                                 | —                                                                                                                                | (the comment confirms a dropped relay is lost: "nothing sends it again")                                          |
| 41  | Drop `provenance` (relay)                                        | no questions room, post not found or forbidden, unparseable, or judged not a human                            | [C teamsync/relay.go:60-78, proto/relay.go:51-60]                                                   | yes                                 | **yes**                                                                                                                          | bounce `provenance` to the relaying coordinator                                                                   |
| 42  | Drop `provenance`/`unresolved`/`forge-rate` (forge check)        | the referenced ping's forge check failed, timed out, or hit the rate limit; no login                          | [C teamsync/forge.go:120-121, 146, 173-177, 186-200]                                                | yes                                 | **yes**                                                                                                                          | `provenance`: bounce. `unresolved`/`forge-rate`: **queue and retry** (transient), and bounce only at a deadline   |
| 43  | Deliver, but over the delivery budget                            | `deliver_per_sender` per window exceeded                                                                      | [C teamsync/budget.go:16-32]                                                                        | yes                                 | **YES, silent**: it "never enters pending", only a `held back: N` count reaches the session [C engine/deliver.go:60-70, 104-106] | **queue** (do not drop): pending with `NotBefore`; receipt `held(budget)`                                         |
| 44  | Deliver, render failure                                          | a slot of the entry does not render                                                                           | [C engine/deliverloop.go:331-333, 341-362, deliver.go:356-370]                                      | yes                                 | **yes** (`unusable.render`, link.log only)                                                                                       | bounce `render`                                                                                                   |
| 45  | Deliver, restored item re-judged out / gone                      | on re-fetch the item no longer passes, or the server will not serve it                                        | [C engine/recv.go:200-231]                                                                          | yes                                 | **yes** (link.log only)                                                                                                          | bounce with the re-judge reason, or `gone`                                                                        |
| 46  | Deliver, team removed                                            | team removal drops pending, handed and awaiting items                                                         | [C engine/teams.go:337-347]                                                                         | yes                                 | **yes** (counted in one log line)                                                                                                | bounce `removed` for each item before the store goes                                                              |
| 47  | Deliver, pending full                                            | a full pending list refuses the batch, which is fetched again                                                 | [C engine/storewrite.go:212-213, 309-311; store.go:614-622]                                         | yes                                 | no (re-fetched), but it stalls                                                                                                   | receipt `held(full)`                                                                                              |
| 48  | Questions items on a coordinator change                          | QS9: a non-coordinator drops questions-sync items                                                             | [C state/store.go:189-195; drop site U]                                                             | yes                                 | possibly                                                                                                                         | bounce `role-changed`, or hand the items to the new coordinator [U]                                               |

**Totals:** 16 `Reason*` and 7 `Ignore*` constants [C verdict.go:6-34], and every one appears above.
`secret`/`body`/`duplicate` are send-side refusals \[C verdict.go:36-41\]: the sender already sees
those at send time, so they are not receive losses.

**Distinct receive-side losses today:** rows 2, 3, 13-18, 20, 25, 27-34, 36, 38, 39, 41-46, plus
the "reached no agent" human case (19).

---

## B. The delivery path today

1. **The store.** A judged Deliver becomes `state.Pending{ID, Room, Item, SentMS, From, Restored, Verb, RelayedBy, Q}` in the batch [C session.go:909-924]. `Persist` adds it, unless it is
   already pending or in the ledger [C engine/storewrite.go:286-314]. Pending holds 2048 items, with
   256 reserved for HUMAN/TIMEOUT [C state/store.go:24-29, 614-622]. The floor stays just below the
   oldest owed item, so a restart re-reads it [C storewrite.go:324-330].
2. **The deliver loop** [C engine/deliverloop.go:65-103] makes one delivery per 2 s, waits 10 s
   after a failure, and calls 120 s of failures a stall [C deliverloop.go:21-26]. It runs only
   while `liveLocked`: socket mode, plus socket, token and transcript all told, and not `gone`
   [C engine/deliver.go:287-291].
   - `pick` picks the team; `deliver` re-fetches restored content, builds the envelope with a
     fresh 8-hex nonce, and dials [C deliverloop.go:257-298, deliver.go:249-262].
   - `await` persists an `Awaiting{Nonce, IDs, TranscriptMark{path, size}, Written: unknown}`
     **before** the write and marks the items `Handed=nonce` [C deliverloop.go:402-452].
   - `conn.WriteUser` [C deliverloop.go:284].
3. **The inbox socket** \[C member/inbox/inbox.go:1-11, 31-40, 204-221\]: one connection per delivery,
   an auth line then a user line, a half-close, and a read to EOF. The outcomes are: nil (a full
   write), `ErrPartial` (cut), and `ErrRetry` (nothing written). `ErrGone` at connect stops delivery
   until a hook tells a new socket [C deliverloop.go:367-390].
4. **After the write** \[C deliverloop.go:457-501\]: a full write sets `Written=yes` and **adds the
   items to the ledger at once** (status counts them as delivered here). `ErrRetry` undoes the
   await (the items are pending again, with no repeat flag). A cut line stays `unknown` for the
   proof to settle.
5. **The proof, which is the real acknowledgement** \[C member/engine/proof.go:19-30, 359-540; member/proof/scan.go:1-10\]:
   the link scans the session's **transcript JSONL** for the needle `teamchat delivery <nonce>:`.
   Claude Code writes that text into the transcript when the envelope is taken into the
   conversation, both as an idle `user` record and as a busy `queued_command` attachment
   [C scan.go:6-9].
   - **Found:** the entry is proven. Its items leave pending, go into the ledger if they were not
     there yet, and each HUMAN item queues an `m.read` receipt [C proof.go:466-499].
   - **Not found:** after 180 s of transcript quiet past the Stop, or 1 h after the write
     [C proof.go:24-27], the entry is **missing**. Its items go back to pending with
     `Repeat=true, Held=true`, are taken **out of the ledger**, and are never pushed to the socket
     again [C proof.go:501-512]. Missing entries are watched 24 h for a late find, with at most 16
     kept [C proof.go:27, store.go:37].
   - So **a socket write alone is provisionally counted** (the ledger at step 4), but it is
     corrected to missing when the transcript never shows it. The transcript record is the ack
     from Claude Code. There is no other ack channel from Claude Code [C: inbox reads to EOF only].
6. **The hook path** \[C engine/hookview.go:94-119\]: on each hook, items that are `Held` (missing),
   or anything not pushed while delivery is not live, are listed as `NotPushed` with counts. The
   hook text tells the session to run `recv` [U: wording in member/hooktext].
7. **`recv`/`wait`** [C engine/recv.go:21-23, 466-559] hands out at most 8 items or 12 KiB, marked
   `Handed=ack`. `Ack` moves them into the ledger. `Return` (no ack in 10 s, a closed connection,
   a failed write) puts them back to pending with `Repeat`. An item awaiting a transcript proof is
   never settled by recv.
8. **Absent, restarting or compacting session:**
   - With no socket or token told, nothing is pushed: items wait in pending, survive restarts
     through the store and the floor, and show in the hook's NotPushed [C deliver.go:287-291, hookview.go:108-117].
   - A new run returns an earlier run's awaiting items to pending with repeat
     [C proof.go:572-594]; at team add, every handed item is returned [C teams.go:219-227].
   - **Compaction** \[U\]: a delivery written during compaction is proven only if its needle reaches
     the transcript. If the transcript rotates or shrinks, the scan resets [C scan.go Result.Reset].
     Otherwise the missing clock makes it Held, and the hook then tells the session to recv.
9. **What the sender sees today:**
   - HUMAN items get Matrix `m.read` when proven [C proof.go:490-499, teamsync/receipts.go:14-40].
   - A ping with no answer gets a TIMEOUT item back to the pinger [C store Outbox, deliver.go:233-247].
   - **Agent text and every drop are invisible to the sender**, apart from link.log and
     `teamchat verify <event>` (which re-fetches one event) [C member/cli/verbs.go:278].

---

## C. The smallest design that meets 1-8

### C1. One new typed key: `teamchat.receipt` (v 1)

```
"teamchat.receipt": {"v":1, "of":"$orig", "to":["@sender"], "state":"delivered|held|bounced|recorded",
                     "reason":"<closed set>", "at":<ms>, "kit":<n>}
```

- Posted by the **addressee's** link as an ordinary Matrix event from its own account. That is the
  "signing" this system has: the sender is homeserver-attested. There is no per-content
  cryptographic signature today \[C: no signature code in proto/ or member/ beyond
  `unsigned.*` reads, engine/sender.go:684-732\].
- The reasons are the `DropReasons` plus `render`, `gone`, `removed`, `redacted`, `unheard`,
  `type`, `empty`, `role-changed`.
- **One receipt per (item, addressee), never a receipt of a receipt.** A receipt that fails its
  own judgement is counted only. This is a loop guard.
- **Which room.** The coordinator asked for main. Cost of main: an older kit judges an unknown
  typed key as `typedKey == ""`, then `ReasonSchema`, then **drop `schema`**
  [C content.go:300-302, 351-366, verdict.go:450-452]. Every receipt would add a schema drop in
  every old member's status and link.log, and would be visible to humans in their client.
  - **Recommendation: the reports room.** Any rostered sender there is judged `Fault` before any
    decode, which old kits ignore silently [C verdict.go:235-237, session.go:938-939]. Members
    already join and post there [C member/engine/sender.go:251, teamsync/reports.go:61-70].
  - The sender verb reads receipts there by `/messages` and needs no judge change.
  - If main is mandatory, accept the old-kit schema noise until every kit is upgraded.
- **Adding a kind:** add it to `typedKeys`, a `Key*` constant, the room's key list, and an
  `objectDecoders` entry [C content.go:12-31, 322]. `v` stays 1: `features` and `v != 1` are
  refused [C content.go:368-378]. So the change is additive inside major 1, not a new major.
  Unknown extra fields inside a typed object are ignored, because `readFields` does not enforce
  exact keys [C strictjson.go:499-505; decodeNoticeObject reads named fields only, content.go:464-492].
  That makes an additive `kit` field safe for old kits.

### C2. When each state is sent

- **delivered:** at the **proof** (the transcript shows the nonce), never at the socket write
  \[C hook point: member/engine/proof.go:466-484 `prove`, and 514-540 `findLate`\].

  - `recv` + `Ack` also counts as delivered, because the session read it through the verb
    [C recv.go:468-473].
  - The existing HUMAN `m.read` stays as it is.

- **held:** sent once when an item enters any of these and has not been proven within N minutes:

  - pending while delivery is not live,
  - `Held` (missing),
  - pending past the rate or budget (C4),
  - pending full.

  It is sent again only when the reason changes. This covers acceptance 4: absent, restarting or
  compacting sessions.

- **bounced:** at the verdict, for each row marked bounce in A, sent by addressees only (see the
  order note in A). For rows 4, 5 and 29 with no readable `to`, the **coordinator** is the
  designated bouncer. Also at post-Deliver loss points: render (deliverloop.go:341), rejudge
  (recv.go:218-228), removal (teams.go:337), redaction (storewrite.go:316-319).

- **recorded:** an ack or late answer that was recorded and not shown (row 37).

- **Durability:** receipts queue in the store, like `Receipts` [C store.go:252-256, 630], and a
  sender goroutine sends them like `startReceipts` [C teamsync/receipts.go:14-40]. A full queue
  must refuse new work loudly, never drop. The current receipt queue loses on full
  [C proof.go:490-491]; that has to change.

### C3. A held state the receiver keeps

`Pending` gains `NotBefore int64` (for a rate or budget queue) and `HeldSince/HeldWhy`. The deliver
loop's `lookAt` already skips items by time [C deliverloop.go:211-233], so it gets one more
condition.

### C4. Rate and budget never lose (acceptance 3)

- Rule 10 `rate` and the budget `admit` are changed to **Deliver with `NotBefore`**: the window's
  next free slot.
- Replay already uses its own window [C session.go:874-880].
- Bound: when pending's agent share is full, **bounce `rate`** instead of holding [C store.go:614-622].

### C5. Sender verb: `teamchat status <event-id>` (or `sent`)

- For one id: each addressee's latest receipt (delivered, held with its reason, bounced with its
  reason, or recorded), or "no receipt" with that addressee's kit (C7).
- For "no receipt + old kit", it reads "accepted, cannot confirm".
- It reads the receipts room by `/messages` filtered on `of`. The sender's own link can also cache
  the receipts it has seen.
- A `--all-open` form lists own sends that are not yet terminal.

### C6. Member verb: `teamchat dropped [--since] [--redeliver <id>]`

- It lists what this member dropped, ignored-but-addressed, or lost after Deliver, each with
  sender, time, reason and whether a bounce went.
- It needs a bounded **drop ledger** in the store (id, room, from, ts, reason, bounced?).
  Today `j.drop` writes only a counter and a log line [C session.go:959-968].
- `--redeliver` re-fetches the event and adds it to pending as a member-forced item, bypassing
  stale, rate and budget but **not** trust rules (sender, schema, provenance). It then sends a new
  `delivered` receipt that supersedes the bounce.
- This replaces grepping link.log and running `teamchat verify` per id.

### C7. Old-kit visibility (acceptance 7)

- Every typed object a new kit sends carries `"kit": <n>`, which old kits ignore (C1).
- The sender verb and `teamchat roster` show each member's last-seen kit. A member never seen
  with `kit` is shown as **"old kit: cannot bounce; delivery unconfirmable"**.
- Roster members carry only role, human_text and parked [C proto/roster.go:60-64, 127-138]. Putting
  the kit in the admin-written roster would need the admin's help, which is why the field rides on
  the member's own events.

### C8. What is not changed

The trust drops stay drops: `sender` from outsiders, schema/provenance on forged content. Only
the **report back** is added. Outsiders are never bounced (row 10).

---

## D. Tests and size

### Tests per verdict (acceptance 8)

There is one table-driven test per bounce row of A. Each one sends from fake member X to member Y
over the in-process homeserver fake used by `member/wiring` \[U: the e2e harness exists,
member/wiring/e2e\_\*\_test.go; its fault injection is unchecked\]. It forces the condition, then
asserts all of these:

- (a) Y posts exactly one `teamchat.receipt{of, state: bounced, reason}`;
- (b) X's `status <id>` shows it;
- (c) Y's `dropped` lists it;
- (d) no receipt-of-receipt.

How each condition is forced:

| Rows                                                 | How the condition is forced                                                                                        |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| schema, version, size, edit (29)                     | raw content: `v:2`, `features`, 17 KiB, an `m.new_content`                                                         |
| verb, role, target, ref, allowlist, excluded (30-34) | raw content past the composer                                                                                      |
| stale (20, 38)                                       | fake clock past `HumanMaxAgeS` and the ack timeout                                                                 |
| rate (39) and budget (43)                            | burst N+1, then assert **held**, then delivered later (no bounce)                                                  |
| forge-rate, unresolved (42)                          | fake forge, then assert held, then retried                                                                         |
| provenance (41, 42)                                  | forge says provenance; relay of a missing post                                                                     |
| render (44)                                          | an item whose slot does not render                                                                                 |
| gone (45)                                            | the fake server returns ErrItemGone on re-fetch                                                                    |
| removed (46), redacted (3)                           | —                                                                                                                  |
| human rows 2, 14-18                                  | —                                                                                                                  |
| unheard (19)                                         | the coordinator is the only bouncer                                                                                |
| delivered                                            | proof found means delivered; a socket write with no transcript line means held, then missing, and **no** delivered |
| recv path                                            | recv+Ack means delivered                                                                                           |
| absent session                                       | no socket, then held, then a socket is told, then delivered                                                        |
| old kit                                              | no `kit` seen, so the sender verb says unconfirmable; an old kit's schema-drop of a receipt in main does not loop  |

### Batches (task counts)

1. **Protocol** (5 tasks): the `teamchat.receipt` key, its decoder and its reason set; the `kit`
   field on every typed send; the reports-room or main decision with a spec text; the
   receipt-of-receipt guard; corpus and agreement tests.
2. **Receiver bookkeeping** (5): the drop ledger in the store (cap, refuse-loudly); the
   best-effort raw `to` reader; addressees-only bounce selection, with the coordinator as bouncer
   for undecodable content and unheard; the durable receipt queue and its sender loop; making the
   existing m.read queue not lose on full.
3. **Never-lose changes** (5): rate becomes NotBefore-queue; budget becomes NotBefore-queue;
   forge transient becomes retry with a deadline; stale-on-replay policy; pending-full held
   receipt plus bounce at the agent cap.
4. **Receipts at the loss and proof points** (6): proof and findLate give delivered; Ack gives
   delivered; held transitions; render, rejudge, removal and redaction give bounces; Record gives
   recorded.
5. **Verbs** (4): the sender `status <id>` and `--all-open`; the member `dropped` and
   `--redeliver`; the roster/kit display; skill and kit text updates (`member/kit/skill.txt`) plus
   a ccy version bump if the image changes.
6. **Tests** (5): the per-verdict bounce table (A rows); proof-versus-socket delivered tests;
   absent, compacting and restart held tests; old-kit interop; a failpoint test (a crash between
   the drop-ledger write and the receipt send yields exactly one receipt after restart).

**About 30 tasks in 6 batches.** Batches 1-2 come first; 3 and 4 can run in parallel after them;
5 and 6 come last.

### Decisions (the coordinator, 26 Nov)

- Receipts go in the reports room (C1).
- Staleness never drops an addressed item judged on replay (rows 20, 38): it is delivered
  marked late, with its send time. A verb meaningful only while its sender waits, whose sender
  has given up, bounces `expired`.
- A bounce to a human is a reply in the room and thread their message came from; a message
  from the questions room bounces there, never to main.
