# SPEC: N3 slice n3f, the side rooms (Plan 00209 Task 3.1, addendum to SPEC-n3.md)

The addendum SPEC-n3.md section 7 asks for. It specifies the reports room and the questions room
of Plan 00211 for v2's member: the verbs `report`, `ask`, `thread`, `questions`, `history --room questions` and `reply` on a questions-room item, the link's joins of both rooms, the questions
room's sync, the question index and its reminders. Sources: A 4.1-4.3, 5.1-5.3, 6.3-6.5, 6.10,
7.1, 7.5, 7.6 rules 03 and 05, 10.1-10.6, 11.1-11.3, 13.2; B 2.2.1-2.2.4, 2.3.2, 2.6 step 10;
Plan 00211's SPEC.md and PLAN.md, read for what the rooms are for, not as a model.

Every rule of SPEC-n3.md section 1 holds here (TB4, CK1, OF1 above all), as do its test rules:
each expectation gets a Go test written first and seen to fail, on fakehs with the injected
clock, `-race`, 90% coverage per package. Expectations are numbered by area: RP (reports), QS
(the questions room's sync), QX (the question index), AK (`ask`), TH (`thread`), QL
(`questions`), QR (`reply` and `history` in questions), RM (reminders), OF (the offline rule).
Decisions are n3f-D1 onward (section 8); departures from v1 are n3f-X1 onward (section 7);
points left to the owner are in section 9.

n3f is built on n3d and n3e merged. Every send here is n3d's (SD3, its one sender per team, and
n3d-D2's queue: a send whose server is down, or whose tries all fail transiently, answers `sent`
with `"id":null,"queued":true` and exit 0, and is posted by the same transaction id later).

## 0. What n3f builds on, and the seams it uses

The tree this spec was checked against is the base before n3d and n3e. The column "From" says
where each seam is: `base` is in that tree; `n3d` and `n3e` are added by `feat/bus-v2-n3d` and
`feat/bus-v2-n3e`, which n3f-a waits for.

| Seam                        | Package                                                                         | From      | What n3f does with it                                                                                                                                                                                                     |
| --------------------------- | ------------------------------------------------------------------------------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| the composers and forms     | `proto` (`send.go`, `forms.go`)                                                 | base      | `Composer.Report`, `Question`, `Thread`, `Admit`; `ReportForm`, `QuestionForm`, `ThreadBody`, used as they are; `Outgoing.DuplicateKey` is n3d's                                                                          |
| the question index          | `proto` (`questions.go`)                                                        | base      | `QuestionIndex` with `Feed`, `Attach`, `Attachment`, `OpenDuplicate`, `Next`, `Reminders`, `ReminderKey`; `IndexedQuestion.Unanswered` is a count. Gains G1                                                               |
| side-room trust and invites | `proto` (`verify.go`)                                                           | base      | `VerifySide` for each side room; `Invite` (accept, reject, pending), judged against the roster passed in                                                                                                                  |
| the filter                  | `proto` (`filter.go`)                                                           | base      | `SyncFilter` picks the coordinator form (main and questions) from the roster alone. Gains G5                                                                                                                              |
| the flood windows           | `proto` (`limits.go`, `send.go`)                                                | base      | `Window{Times}` with `Count`, `Admit`, `Prune`; `Admit` refuses the report window as `ReasonRate` with the detail `report_per_hour`. Gains G6                                                                             |
| the envelope                | `proto` (`envelope.go`)                                                         | base      | `Entry.Questions`, `Entry.Question`, `Entry.Revises`, the reminder entry, filled by n3f; DV2's renderer is n3e's                                                                                                          |
| the team goroutine          | `member/teamsync` (`session.go`)                                                | base      | `handle` judges the answer's invites before main's trust, against the roster held before the answer (G3); `take`'s `proto.Index` case is a no-op; every pending item is `proto.PurposeMain` (G3)                          |
| the read verbs' errors      | `member/teamsync` (`verbs.go`)                                                  | base      | `verbErr` maps `matrix.ErrRateLimited` to 14 `server-rate`                                                                                                                                                                |
| the sends                   | `member/engine` (`send.go`, `sender.go`, `outbox.go`, `reply.go`, `timeout.go`) | n3d       | SD3, the team's sender, n3d-D2's queue and n3d-D27's refusal at post; SD1's `reply` and its room from the ledger; SD4's timer                                                                                             |
| the store                   | `member/state` (`store.go`)                                                     | base, n3d | `outbox.sending` and `outbox.gate` exist in the base (caps 64 and 64); n3d sets them to 32 and 96, adds the sending entry's `type`, `content`, `tried`, `ping`, and `last_sent`, and rounds `MaxStore` (n3d-D1). Gains G2 |
| the roster cache            | `member/state` (`rostercache.go`)                                               | n3d       | sends composed before the first verification (n3d-D3)                                                                                                                                                                     |
| records                     | `member/records`                                                                | base, n3d | `KindQuestion` and `Question{team,n,state,id,asked_by,unanswered}`, `Human.Question *int64`, `KindRoom`, `KindMore` are in the base; `KindQueued` and `Sent{ID *string, Queued}` are n3d's. Gains G4                      |
| exits                       | `member/exits`                                                                  | base      | 14 {`rate`, `server-rate`, `report-rate`}, 20 {`unreachable`}, 22 {`untrusted`, `forbidden`}, 23 `not-found`. Gains G7                                                                                                    |
| the CLI table               | `member/cli` (`run.go`)                                                         | base      | `unbuilt` lists n3d's `reply`, `say`, `ping`, n3e's `hook`, `status-line` and n3f's `report`, `ask`, `thread`, `questions`; n3f removes its four (CL2), and the list is empty once n3d and n3e have removed theirs        |

### 0.1 Gaps in the built APIs

- **G1. The question index** (`proto.QuestionIndex`), whose rules are section 3:
  01. `Feed` and `Attach` are idempotent by event id: an event already folded changes nothing
      (QX3's echo, a page overlapping the sync, a re-read answer).
  02. Attachment takes any human post A 10.4 counts, whatever the post's delivery verdict (n3f-D18):
      a new entry point takes the parsed post, not a `Deliver` verdict, so the build and the sync
      attach the same posts. A read-only `AttachmentOf(post)` gives the attachment the rules would
      give, changing nothing (QR2).
  03. Per unattached human post, whether an agent post replies to it (QL1, A 10.3's index check):
      the fold takes an agent's or the admin's plain reply, one with no `n`, which `threadPost`
      ignores today (`questions.go:185`) (QX2).
  04. `IndexedQuestion` gains `asked_by` (each a user id of at most A4's 128 bytes that passes
      A's user-id grammar, so it needs no JSON escaping; another entry is not kept), `re` and `remind_after_s` from the content, and `Unanswered` becomes the ids
      of P58's unanswered posts, oldest first, with a count past the 16 kept (QX6).
      `IndexedQuestion.Question`, the collapsed `Question:` section kept for `OpenDuplicate`
      (`questions.go:173`, up to about 8000 bytes), is replaced by its SHA-256, and
      `OpenDuplicate` hashes the section it is given (QX6's store bound).
  05. Provisional entries: an own question or thread post placed at its number when its send is
      recorded, replaced in stream order when its echo is folded (QX3).
  06. The caps of QX6, applied in the fold itself, so the live index and a rebuilt one agree.
  07. A strict JSON codec of the index's state, for the store (QX5), and a copy for an answer's
      judging (QX4's commit rule).
  08. `Reminders` gives each due reminder's due time, for RM1's watermark.
  09. Redacted slots (QX7): the fold counts the admin-redacted agent and admin events since the
      highest counted was last raised; `Next()` is 1 plus the highest counted plus that count, so
      a number is never given out twice, and an agent's question is counted anywhere from 1 plus
      the highest to `Next()`.
  10. The build record (QX5): the codec holds an owed or running build (`build`, with its
      `start`, `after` and `late_from`), so a load resumes a build as a build.
- **G2. The store** (`member/state`), each key with its cap in B 2.3.2's caps list and its strict
  decoding: `outbox.reports` (n3f-D6); `outbox.questions_unechoed` and
  `outbox.questions_uncounted` (`{"count": N, "said": N}`, QX3); `outbox.questions_handover`
  (QX1's ready line); `questions`, the index's state, its `/messages` token (at most n3d's
  `maxNextBatch`, 1024 bytes), its last folded event id and its `build` record (QX5);
  `reminded_ms` and `remind_late` (n3f-D11), at the document's top level beside `questions`
  (section 12 says where each key sits); `Pending.Room` taking
  `questions` for HUMAN and TIMEOUT items (the decoder already allows the three purposes).
  **`MaxStore` is rounded up again** by n3d-D1's rule (the caps' sum rounded up to the next
  256 KiB, pinned by `TestStoreFullFitsTheFileCap`): at n3d's caps it rises from 12 x 256 KiB
  (3 MiB) to **16 x 256 KiB (4 MiB)**, QX6 giving the sum. The test of a store with every cap
  full at its maximum sizes, the new keys included, decodes under the new `MaxStore`.
- **G3. The team goroutine**: the answer's order (QS1: main's roster, then invites, then each
  room's timeline); the room of each pending item from its verdict; the index's owner, its
  builder goroutine (QX1) and its writes. Every write of the `questions` key, from an answer's
  SY5 write or a QX1 page, is made by the engine's one store writer, and only by the writes QX5
  names.
- **G4. `records`**: `Question` gains `headline`, `re`, `asked_ms`, `remind_after_s`,
  `unanswered_more` and `redacted`; its `id` changes type from `string` to a nullable string
  (`records.go:221`), and `id` and `asked_ms` are null for a queued question; `Human.Question`
  changes type from `*int64` to a nullable string (`"Q4"`, `"none"`, null) and `Human` gains
  `headline`; `Room` gains `uncounted` (QX3); a new `unattached` kind (section 6).
- **G5. The filter**: the coordinator form needs the questions room ready (QS3), not only the
  roster naming this member; `SyncFilter` takes that as an argument.
- **G6. The report window**: `proto.Window` seeded from `outbox.reports` at each `report`, and an
  accessor for the oldest time in the span, for RP3's message; the caller maps `Admit`'s
  `report_per_hour` refusal to 14 `report-rate`, the bucket's to 14 `rate`.
- **G7. Exits**: 20's set gains `not-ready` (M5: the room or the index is not ready yet), 10's set
  gains `open-limit` (QX6). B 2.2.4's table gains both, and row 20's meaning widens (section 12.5).

## 1. The reports room (RP)

- **RP1. The join.** Once main verifies at a team's start, the team goroutine joins reports
  (`POST /join/<reports id>`, the id the verified roster names) by the restricted rule (A 4.3,
  6.3), once per link run; a reports invite is refused, as A 6.3 says. The join never blocks the
  team: it runs after the first long-poll is sent, on the team's client (its connection counts in
  B 2.14's count), under a 30 s deadline. A transient failure is retried with SY8's backoff on its
  own timer, the team going on; a 403 or any other refusal records the room `refused` with its
  errcode for `status` (the `room` record, section 6) and is tried again at the next start, the
  next roster change and the next `report`. Whether this member is already in the room is read
  from the joined rooms (`GET /joined_rooms`) after a refusal, never from an error's text; already
  joined is success. Test: a member whose reports join is refused delivers main items unchanged
  and its `status` shows `refused`; a join that fails transiently twice then succeeds shows
  `joined`; a 403 for a member already in shows `joined`.
- **RP2. Nobody syncs it.** The reports room is never in a filter (A 6.10); a reports event that
  reaches a judge anyway is A 7.6 rule 05's `fault`, logged once per event with its id and never
  delivered. Test: a filter for every role names no reports room; a forged sync answer holding a
  reports event from a roster agent (a non-roster sender is dropped `sender` before rule 05) is a
  fault line and no item.
- **RP3. `report [--team T]`**: stdin is the text, read by n3f-D1's rule with the cap 1500 (at
  most 1502 bytes read); the whole is the report's `body`, the first line its headline (A 10.1).
  An over-long text is refused, never cut. Then, in this order, the first failure deciding:
  1. CL6's team rule (64 `ambiguous-team`);
  2. the sender's class from the verified roster: the probe is 11 `role`. (A parked member's
     main fails the roster check, A 6.4's "listed and not parked", so its team is already 22; a
     removed member's team is gone, LN8. Neither reaches this step.)
  3. the size: more than 1501 bytes read, or more than 1500 after the newline is removed, 10
     `body` ("a report is at most 1500 bytes; it is never cut"); then `ReportForm`: an empty
     first line or a headline over 120 code points, 10 `body`, naming which (three messages:
     the size, the empty headline, the long headline). The client does only the reading and the
     one newline of n3f-D1 (and 64 for a terminal, 10 `schema` for non-UTF-8, which cannot
     travel in the request); it refuses no size and no empty text, since only the link knows
     the role and step 2 comes first;
  4. the secret check (the twelve shapes and the member's own token): 10 `secret`;
  5. the reports room verified now (RP4);
  6. `Admit`, its report window seeded from `outbox.reports`: `report_per_hour` reports already
     in the last 3600 s, 14 `report-rate`, the message naming when the oldest leaves the window
     as seconds from now, rounded up, so it reads the same with any clock ("team T allows N
     reports an hour and they are made; the oldest leaves the window in S seconds; a report that
     was not posted still counts");
     the same text within 60 s, 15 `duplicate`; the send bucket empty, 14 `rate`;
  7. SD3's write: the token, the transaction id, the content and the report's time in
     `outbox.reports` (n3f-D6) in one store write before the PUT, which the team's sender makes.
     The verb answers n3d's `sent` record with `"room":"reports"` (with the event id, or queued per
     n3d-D2) and exits 0. Output: nothing but that record on stdout (CL3).
- **RP4. Verified at each send** (B 2.6 step 10, A 6.5's one-shot rule, n3f-D16): `report` reads
  the reports room's state with one `GET /rooms/{reports}/state` and judges it by `VerifySide`
  against the main room's verified roster and anchors. Not joined: RP1's join is tried first,
  within the verb's deadline; a join refused (a 403 or any other refusal, membership read from
  `GET /joined_rooms` as RP1 says) is 22 `forbidden` naming the reports room, nothing charged,
  and the `room` record `refused`; a join failing transiently is the transient case below. A
  failed check is 22 `untrusted`, the message naming the reports
  room and the check (create, join rule, power levels, roster, link), and nothing is charged; main
  is left as it was: its items are delivered and its sends go on. A 403 on the state read is 22
  `forbidden`. A transient failure of the state read (unreachable, a timeout, a 5xx, a 429 past
  its retries) does not refuse: the owner's rule says a send to a down server is queued (n3d-D2),
  so the report is charged and queued, and the team's sender verifies the room before its PUT. A
  report the sender cannot verify, or whose PUT is answered 403, is removed and answered 22 when
  its verb still waits, and otherwise is n3d-D27's refusal at post (logged by transaction id,
  counted `unsent` in `status`); its token and its report-window slot stay spent (n3f-D6). Tests:
  each failed check, nothing charged; a `report` whose join is refused 403: 22 `forbidden`,
  `outbox.reports` and the gate unchanged, `status` `refused`; main's next item delivered after a reports failure; a
  homeserver down during `report`: queued, exit 0, posted after verification when it returns; a
  queued report whose room fails verification at the sender: removed, logged, `unsent` 1.
- **RP5. No one is woken.** The content is A 10.1's exactly: `m.notice`, `m.mentions` `{}`, the
  `teamchat.report` object, no relation. A worker's report does not copy the coordinator (P21 is
  for text to humans). Test: the PUT's content byte for byte; no member's store gains an item
  after a report (a fakehs team of two, the other member's sync answered).
- **RP6. The deadline** is a send's (30 s, LN2), the reports state read and the join inside it.

## 2. The questions room's sync (QS)

- **QS1. The order within one sync answer** (n3f-D15). `handle` today judges invites first,
  against the roster held before the answer; n3f orders every answer:
  1. an invite to main (SY11, a re-add), as today, since main's trust can need it;
  2. main's trust: when the answer triggers it (SY14), main is verified from this answer's state
     and the roster replaced;
  3. the side-room invites, judged by `proto.Invite` against that roster;
  4. the questions room's membership and trust (QS4, QS7, QS8), against that roster;
  5. main's timeline, then the questions timeline.
     So a roster that names this member coordinator joins the room at once, and an invite to the
     room in the same answer (seen only under a filter that names the room, QS2) is accepted, not
     left; a demotion's roster and its kick in one answer are a demotion (QS8).
- **QS2. The invite and the join** (A 6.3). The questions room is joined by its id from the verified
  roster (`POST /join/<questions id>`), never by an invite's id: an invite is only the signal to
  try. The join is tried while the roster names this member the non-parked coordinator and the
  room is not joined: at a trigger (a team's start, a roster change, a promotion, this member's
  own leave of the room (QS8), an invite seen, a questions verb), and at every sync answer 30 s
  or more after the last try; between triggers, never twice within 30 s. An invite is seen only
  under a filter that names the room, and the worker form (QS4) names main alone while the
  server filters invites by room, so a member waiting for its invite is not told by it: its
  next try is the first answer 30 s on (OW6). The room's state is read first, and the join made
  only when it shows this member not joined (a 403 on the read counts as not joined): a restart
  or a retry never makes a second join, which would move QS5's floor. A 403 on the join is "not
  invited yet" (`awaiting-invite` in `status`); any other failure of it (a 5xx, a timeout) is one
  log line and the next try 30 s on, holds nothing and is not SY8's backoff, so main goes on
  (QS8, n3f-D8); a success starts QS5. An invite `proto.Invite` rejects is rejected (`leave`) and
  logged by room id; one left pending is logged once by room id and not answered. An awaited
  invite is a passing state (n3f-D13): the questions verbs wait for it within their deadline,
  then exit 20 `not-ready`; once 300 s have passed since the clock's start, with the join still
  refused, they exit 22 `untrusted` ("no questions invite since the roster named you
  coordinator; run status"), and `status` says `untrusted` with the check `invite`. The clock
  starts at the later of (a) when this link first saw the roster naming this member
  coordinator and (b) when it saw this member's latest own leave of questions (a leave or a
  kick, QS8); both are this link's times, so a link restart starts the clock again at its first
  roster read. Tests: each row of `proto.Invite`'s table through the team goroutine; one answer
  holding the roster naming this member and the invite: joined, not left; a roster naming the
  room after the invite arrived: joined at that roster change; a join refused 403 is not tried
  again by an answer before 30 s and is by the first after (fake clock), and a join failed 502 is
  one log line, tried again 30 s on, with main's items delivered meanwhile; a restart of a
  member already in the room makes no second join; no invite for 299 s then 301 s
  (fake clock): 20, then 22; a kick 10 h into a term with the roster still naming this member:
  20 `not-ready` at 299 s after the kick, 22 at 301 s.
- **QS3. A coordinator with `human_text: false` cannot happen**: A 5.1 makes such a roster
  invalid, so main fails trust (SY14, 22) and nothing of questions is reached (n3f-D2). Test:
  that roster stops the team with 22 and no questions join is attempted.
- **QS4. The filter** (G5). While this member is the roster's non-parked coordinator and the
  questions room is joined, verified and its index at the head (QX1), the long-poll's filter is
  `proto.SyncFilter`'s coordinator form (main and questions); otherwise the worker form. The
  filter waits for the head only, never for QX3's unechoed own post: that echo comes only
  through the coordinator filter, so waiting for it would hold `outbox.questions_unechoed` set,
  and every questions verb at 20, for ever. The unechoed post holds back only the composes, the
  verbs and the reminders (QX3, RM1). A change of any of the three ends the long-poll in
  flight and starts the next with the new filter (B 2.6 step 10). The worker form names main
  alone, so an invite to the questions room is not seen under it (QS2, OW6). Tests: promotion, demotion,
  the index reaching the head and the room becoming untrusted each change the next request's
  filter, pinned byte-equal to proto's; a restart with `outbox.questions_unechoed` set: the
  first long-poll's filter is the coordinator form, the echo is folded, and the key goes back
  to null in that answer's write.
- **QS5. The questions room's start.** The room is verified by `VerifySide` before any of its
  events is judged (A 6.5: at start, on join, on a limited timeline, on a trust-type event). Its
  index is then loaded from the store or read (QX1), and only then is its first sync batch judged.
  Its items resume per room as SY1 says, from the later of (a) the store's `floor_ms.questions`,
  or with no store the earlier of its `teamchat.floor` marker and `released_ms`, and (b) this
  member's latest own join event in the room. A first join therefore delivers nothing posted
  before it, and `questions` shows those posts (n3f-X4, OW4); a re-promotion delivers nothing the
  coordinator in between handled. The comparison with the floor is strict, as main's: a post in
  the same millisecond as the own join is attached (QS6) but not delivered. The marker is SY10's,
  per room. Tests: a first promotion over a
  room with three human posts: none delivered, all three in `questions`; a restart within a term
  resumes from the floor; a re-promotion with a surviving floor resumes from the new join.
- **QS6. The receive path** (A 7.6 rule 05): for the coordinator every human message in
  questions is addressed (no mention needed), kind HUMAN, judged by `stale` never and by `rate`
  as main's are. Each is attached by the index (A 10.4) whatever that verdict (n3f-D18); its
  pending entry has `room: questions`. Another agent's event is dropped `sender`; an agent's
  `teamchat.question` or `teamchat.thread` is rule 03's `index` outcome and is folded (QX2). A
  human's edit in questions is an item with `revises` (A 7.5), attached by rule 4. An admin
  redaction in questions removes its target from the index completely, by QX7's rebuild. Tests: an unmentioned human
  post delivered with its question; a post in a question's thread, a reply to a thread post, a
  `Q3:` prefix, an edit of an attached post, an unattached post (`question: none`); a worker's post
  dropped `sender`; a human post older than `human_max_age_s` still delivered; a post dropped
  `rate` still attached.
- **QS7. A limited timeline** in questions is filled as SY3 fills main's when the fill reaches
  the index's last folded event within SY3's page bound (one sync token covers both rooms: a
  transient failure holds the whole answer and persists nothing). Past that bound the index
  cannot skip events (its numbering is a chain, A 6.5), so the questions room is caught up by
  QX1's catch-up instead, off the delivery path: this answer's questions events are not taken,
  main's are persisted as usual, and the questions filter is dropped until the catch-up has
  reached the head. That answer's SY5 write, and every main answer's while the catch-up runs,
  leaves the `questions` key as it was (QX5), so the key's token never passes an event the index
  has not folded. Tests: a gap of 2100 questions-room events, one question in it: main's items in
  the same answer are delivered at once, the index counts the question after the catch-up, and the
  next `ask` takes the next number; the same gap with main answers persisted during the
  catch-up and the link killed mid-catch-up, then a restart: the catch-up resumes from the last
  page's token, nothing in the gap is skipped, and the next `ask` takes the next number.
- **QS8. A side-room failure is that room's alone** (A 6.5): a questions room that fails
  `VerifySide`, or whose state or history read is refused (403, 404; the check is named
  `state`), is untrusted alone: the
  filter drops it (QS4), its pending and handed items stay (they passed when they were judged),
  `ask`, `thread`, `questions`, `history --room questions` and `reply` on a questions item exit 22
  `untrusted` naming the room and the check, `status` shows `untrusted` with the check, the
  SessionStart frame and one hook line say it once, and main goes on. It is checked again at the
  next start, the next roster change, and the next questions verb, which re-verifies before it
  refuses. A re-verification that cannot reach the server exits as `verbErr` maps it: 20
  `unreachable`, or 14 `server-rate` for a 429 past its retries; nothing is charged and nothing
  is queued, since nothing passed the check (n3d-D2 queues sends that passed it). On recovery its index is caught up from its token (QX1) and its items resume from its
  floor (QS5), so nothing posted while it was untrusted is lost. Transient failures are SY8's
  (n3f-D8) and a build's own (QX1). This member's own leave of questions is never a trust failure:
  with the roster no longer naming it coordinator it is QS9's demotion; with the roster still
  naming it, the room is `left` in `status`, the join is tried again (QS2), and the verbs wait as
  for an awaited invite, QS2's 300 s clock starting at the leave. Tests: each failed check, with
  main's items delivered meanwhile; recovery delivering the posts made while untrusted; a kick
  seen while the roster still names this member: `left`, no `untrusted` and no frame line
  within 300 s of the kick; an untrusted room re-verified by an `ask` while the homeserver is
  down: 20 `unreachable`, the gate and `sending` unchanged.
- **QS9. Demotion** (B 2.6 step 10): when the roster no longer names this member the non-parked
  coordinator, the team goroutine, before it judges anything else in the answer: rebuilds the
  filter (QS4), stops a running build or catch-up, drops the questions room's `pending` items and
  returns and drops its `handed` ones, drops the index (the store's `questions` key),
  `reminded_ms`, `remind_late`, `outbox.questions_unechoed`,
  `outbox.questions_uncounted` and `outbox.questions_handover`, in one store write (each item
  logged by id, never by body). That write is the demoting answer's own SY5 write, which carries
  the drop: a write of its own before the answer would need a position, and the answer's would
  skip main's events not yet persisted; the room is ignored for the rest of the answer. A
  start by a member the roster does not name coordinator, over a store that still holds any of
  these, drops them in its first write. The member refuses every
  questions verb with 11 `role`. `awaiting` entries already written to the socket settle as any
  other (DV4); a `sending` questions post is dropped with a log line naming its transaction id
  (it may have landed; the room is the record). Reminders owed are not made. The questions floor
  stays (QS5 takes the later of it and the next join). Tests: a demotion with items pending, handed
  and awaiting; one answer holding the demotion's roster and the kick: QS9, no `untrusted`, no
  frame line; an `ask` racing the demotion's roster read (refused or posted, never posted after
  the store write that dropped the room).
- **QS10. Promotion**: QS2's join, QS5's start, QX1's build (a promoted member's store holds no
  index, QS9).

## 3. The question index (QX)

The index is the questions room folded in stream order by A 6.5 and 10.4's rules: every counted
question, every agent thread post, every human post. Its state is persisted in the store with a
`/messages` token after its last folded event (QX5), so a link restart never reads the room's
history again; the room is read from its start only when the seat has no index (n3f-D9) or an
admin redaction makes it rebuilt (QX7).

- **QX1. The build and the catch-up**, run by a goroutine of the team's own, never on the
  delivery path (the long-poll goes on with the worker filter, QS4), under the team's backoff
  (SY8), stopped by a demotion or the team's end:

  - **The build** (no `questions` key in the store: a first promotion, after QS9, an epoch
    change (SY6); a key holding a `build` record, owed or under way (QX5): an admin redaction,
    QX7, or a build a crash interrupted; and a token the server refuses): `VerifySide`; then `/messages` backwards (`dir=b`, 100 per page)
    from the team's sync position, keeping only the last page, until a page with no `end` (the
    room's start, holding `m.room.create`); then that page folded oldest first, and the room read
    forwards (`dir=f`, 100 per page) from that page's `start` to the head (a page with no events),
    folding each event.
  - **The catch-up** (an index with a token: QS7's gap, QS8's recovery): the room read forwards
    from the index's token to the head, folding each event. Event ids have no order, so the
    overlap with what the index already folded is not found by position: each such event is
    skipped by G1.1's idempotence by event id.
  - Each event is folded by QX2's rules; each human post after the questions floor (QS5) is also
    taken as an item, judged as the sync judges it (QS6); a build that replaces an index (a
    refused token, QX7) takes as items only the posts after its forward read has passed the
    replaced index's last folded event (the build record's `after`, QX5), since the sync took
    those before. An epoch change drops the key, `reminded_ms` and `remind_late` as a new term
    does, and its build takes items as a first build does: a restore redelivers marked
    `restored` (SY6, T11), and a reminder made again after it is of that class. Each page's
    items, the index's state, its new token and its build record are persisted in one store
    write, made by the engine's one store writer (G3) (P3: the index never ahead of its items).
  - **At the head and ready.** At the head (a forward page with no events) the build or
    catch-up ends, its last page write sets the build record to null, the filter takes
    questions (QS4), and the first questions batch is taken only if it connects to the index's
    last folded event (its events or SY3's fill reach it; earlier events are skipped); otherwise
    the catch-up runs again. The index is **ready** when it is at the head and no own post is
    unechoed (QX3). Only the questions composes, the questions verbs and the reminders wait for
    ready; the filter waits for the head alone (QS4).
  - **The ready line** (OW4, section 6.1). A promoted coordinator's NOTICE comes while its build
    runs, so a `questions` run then may exit 20 `not-ready`, and the wait class says to try once
    more, not to loop. So the first page write of a build begun with no `questions` key (a
    promotion, or after an epoch change) sets `outbox.questions_handover` true, and once the
    index is ready one hook line says, at the next UserPromptSubmit or SessionStart, word for
    word: `teamchat: in team <t> the questions room is ready for you as coordinator. To see its open questions and the human posts made while the coordinator changed, which were not delivered to you, run teamchat questions --team <t>.`
    The store write the link makes once a hook has taken the line sets the key back to false,
    so a crash between the two says the line again rather than not at all; a demotion drops the
    key (QS9). The
    line ends at clause punctuation after `<t>`, so PK2's scan reads no following word as an
    argument, and it is pinned as section 6.1 pins the NOTICE.
  - A transient failure of a page is retried under the backoff, the pages already folded kept;
    a 403 or 404 is QS8's. The bound: memory is the index's caps (QX6) plus one page; requests are
    at most 2 x ceil(E/100) + 1 for a build over E events and ceil(E'/100) + 1 for a catch-up over
    the E' events since the token; nothing else waits on it (n3f-D9).
  - While a build or catch-up runs, `ask`, `thread`, `reply` on a questions item and `questions`
    wait for it within their deadline, then exit 20 `not-ready` naming the state and the events
    read so far; `status` says `building` (M5).
  - Tests: a room of 250 events built in three pages each way; a build over 10 500 events whose
    main items are delivered during it (fakehs holding each questions page until main's item is
    in the store); a page failing transiently twice; an `ask` during a build held by fakehs: 20
    `not-ready` at the deadline (fake clock), and exit 0 when the page is released before it; a
    promotion whose build is held: `questions` exits 20, the ready line is said once when the
    build reaches the head, and a link killed mid-build says it once after the restart's build
    ends; a catch-up and a redaction's rebuild set no ready line.

- **QX2. The fold**: each counted question and each thread post of a counted question, from any
  agent account (this member's own included) or a question from the admin, as A 6.5's exception
  says; each agent's or the admin's plain reply to an unattached human post (a reply with no
  `n`, A 10.3; this member's own included), which marks that post replied to (G1.3) so that
  `questions` stops listing it, rebuilt as live; each human post A 10.4 counts, attached by its
  rules whatever its delivery verdict (n3f-D18); each admin redaction (QX7) and each redacted
  slot (QX7). An event folded twice is folded once (G1.1). The sync, the build and the catch-up
  fold through the same function.

- **QX3. Own sends**: the coordinator's own question or thread post is placed in the index as a
  provisional entry when SD3's write records it, numbered `Next()` at that moment, so the next
  `ask` numbers above it without waiting for the echo. When the echo is folded, the provisional
  entry is replaced by the folded one in stream order; if the echo is not counted (an admin's
  question took that number first), the entry is dropped and the link logs it by transaction id.
  The team's sender posts a queued questions send only once the index is ready and only if its
  number is still free (not counted by a folded event); otherwise it is n3d-D27's refusal at post.

  - **What the agent is told.** The answer to `ask` names the number the question got at its
    compose: the `question` record's `n`, and the body's `Q<n> ` prefix. When that question's
    echo is not counted, because another's question was numbered `Q<n>` first, the agent is told
    in two places: the questions `room` record in `status` counts it (`uncounted`, section 6),
    and one hook line says it once, at the next UserPromptSubmit or SessionStart, word for
    word: `teamchat: in team <t> your question sent as Q<n> was not counted: Q<n> is another question, so no answer attaches to yours; run teamchat questions --team <t>, then ask it again.` A `thread` post whose number is not counted is told the same way, the line naming
    the thread post. "Once" is persisted: `outbox.questions_uncounted` is `{"count": N, "said": N}`; the write that drops an uncounted entry raises `count`, the line is owed
    while `count` is above `said` (one line per uncounted post, in order), and the store write
    the link makes once a hook has taken a line raises `said` by one, so a restart neither
    repeats a line already taken nor loses one owed. The skill (n3f-e) says what an
    `uncounted` count means.
  - **Posted but not yet echoed.** n3d records a send posted (its event id in `last_sent`) when
    its PUT succeeds; for a questions-room send that same store write sets
    `outbox.questions_unechoed` to the event id, and the store write that folds that event (an
    answer's SY5 write or a QX1 page, counted or not) sets it back to null. Sends are one at a
    time per team and posted in order (QX4), so the last own post folded means every earlier one
    is. After a link restart the index loaded from the store is at the head but not ready
    (QX1) while `outbox.questions_unechoed` is set: the filter takes questions (QS4), so the
    echo arrives, and meanwhile `ask`, `thread`, `reply` on a questions item and
    `questions` wait within their deadline for the fold of that event, then exit 20
    `not-ready`, and no questions send is composed, so no compose takes a number the unechoed
    post already holds (n3f-D5).
  - After a link restart the provisional entries are placed again from `sending` before any new
    questions send is composed, and the sender posts them first, in `sending` order (n3d-D4).
  - Tests: an admin's carried question with the coordinator's number folded before the echo: the
    admin's holds the number on this seat as on a replacement's build, the `room` record counts
    `uncounted` 1 and the hook line is said once; a restart after the line was taken: not said
    again; a restart before a hook took it: said once after it; a restart with a questions send in `sending`:
    re-sent by its transaction id before the next `ask`, which numbers above it; a failpoint
    between the PUT's success being recorded and the echo's fold, then a restart and an `ask`
    before the first sync answer: the `ask` exits 20 `not-ready` at its deadline when the echo is
    held, and numbers above the echoed question when the echo is released before it, never at
    the same number.

- **QX4. One questions compose at a time per team, and the commit rule** (n3f-D5): `ask`, `thread`
  and `reply` on a questions item are composed one at a time per team by the team goroutine,
  which owns the index: each is numbered and placed (QX3) only after the previous one's SD3 write
  answered. An answer's folds are made on a copy of the index, written with the answer's items in
  SY5's one store write, and become the live index only after that write succeeds, as `commit`
  takes the flood window; a held or failed answer leaves the live index as it was, so a re-read
  answer never attaches a post twice. Tests: fakehs holds the first `ask`'s PUT while a second
  `ask` is composed; on release the two bodies are `Q4` and `Q5` and the second PUT follows the
  first (no timing in the test: the hold is explicit and the assertion is on the contents); an
  answer whose store write fails, then re-read: each post attached once.

- **QX5. Persisted with the position.** The store's `questions` key holds the index's state (QX6's
  bounded parts, never the provisional entries or anything else of the seat's), its token and
  its last folded event id, and its `build` record, written in the same store write as the
  items and the position they belong to. **Only two writes write the key** (n3f-D19), both
  through the engine's one store writer (G3), apart from the two that drop it (QS9's demotion,
  SY6's epoch change, QX1):

  1. an answer's SY5 write, when that answer's filter held the questions room (QS4's
     coordinator form) and its questions timeline was folded (taken, not set aside by QS7): the
     token is the answer's `next_batch`;
  2. a QX1 page's write: the token is the page's `end`.

  Every other SY5 write (the worker filter during a build or a catch-up, QS7's answer past the
  bound, QS8's untrusted room) leaves the key byte for byte as it was, so its token never passes
  an event the index has not folded and the numbering chain (A 6.5) is never broken. A link
  restart loads it and syncs on from the store's position, so the questions room is never read
  again; a key whose token lags the position is caught up from its token (QX1).

  The token is held as the key's size bound counts it (QX6): non-empty, at most 1024 bytes, and
  printable ASCII without a space, `"`, `\`, `<`, `>` or `&` (the characters `encoding/json`
  escapes, which would make 1024 bytes more in the file). This is narrower than the sync
  token's own check in n3d's store (any printable ASCII), and real tokens (letters, digits, `_`,
  `-`, `~`, `.`) pass. A `next_batch` or page `end` outside it is a malformed answer: the
  engine refuses to write it, treats it as SY8's transient path with one log line naming the
  token's first offending byte, and never writes the key with it (smallest reading; the spec
  was silent, and a store that cannot write the key must not be reached by a token it cannot
  hold).

  **A build owed or under way is persisted** (n3f-D21). The `build` record is null for an
  index at the head or behind it, or `{"start": <id or null>, "after": <id or null>, "late_from": <n or null>}`:

  - **Owed.** An SY5 write or a catch-up's page write whose fold holds an admin redaction in
    questions (QX7; a build's own page follows the next bullet) writes no index: it writes the key as a build owed, with no state, token or last event, and
    `build` `{"start": null, "after": A, "late_from": N}`, where A is the newest event whose
    items have now been taken (the last event of that answer's questions timeline, or of that
    page) and N the replaced index's `Next()`. A refused token needs no owed write: the old key
    stays until the build's first page write, and a restart's catch-up is refused again.
  - **Under way.** Each page write of a build records its `start` (the newest event of its first
    backward page, QX7), `after` and `late_from`. `after` is the replaced index's last folded
    event for a refused token, or the owed record's A, and null for a build begun with no key;
    the first forward page write that folds that event sets it to null. `late_from` is the
    replaced index's `Next()` for a refused token, or the owed record's N, and null for a build
    begun with no key. A redaction met after
    `start` begins the build again, recorded as owed: `after` is kept while the forward read
    has not passed it, else it becomes the last event of the page that met the redaction (the
    newest whose items were taken), and `late_from` is kept.
  - **The load.** A key with a `build` record is never caught up: with `start` null the build
    begins again from the backward read; with `start` set the forward read resumes from the
    key's token with the state, `start`, `after` and `late_from` recorded, under the build's
    rules (items only past `after`, a redaction after `start` restarts it). So a crash after the
    redaction's write rebuilds, and a crash mid-build resumes the build; neither treats a
    partial build as an index with a token, whose catch-up would take every post after the
    floor as an item, posts the sync already delivered.
  - The ready line's key (QX1) and `remind_late` (RM1) are seat state outside this key.

  A replacement coordinator's build folds the same events by the same rules into the same state,
  so the persisted index is the room's state, not the seat's (A: "no session state"). Test (M1):
  a coordinator with Q1 answered by human post H1 and Q2 open with `remind_after_s`; (a) a link
  restart: `questions` shows Q1 `answered` with `unanswered` [H1], Q2 `open`, and Q1 is never
  reminded; (b) a promotion on another seat (a full build): `questions` prints the same records
  byte for byte, Q2 is reminded once and Q1 never; (c) H2 attached to Q2 in an answer whose
  store write fails, then re-read: Q2 has `unanswered` [H2] once; (d) H3 dropped by `rate` live:
  attached on both seats; (e) an answer whose filter did not hold the questions room (a build
  running, the worker filter): the `questions` key byte-equal before and after its SY5 write;
  (f) an admin redaction of human post H1 (QX7): live, after the redaction's rebuild, Q1 is
  `open` with `unanswered` [], and a fresh build on another seat prints the same `questions`
  records byte for byte and holds a byte-equal `questions` key; (g) an admin redaction of a
  counted Q2 with a later Q3 counted, and again with no later question (QX7): live and rebuilt
  byte-equal, the next `ask` numbering Q4 in the first case and Q3 in the second on both seats,
  Q2 a `redacted` record in the first case only (the later Q3 shows the number was skipped; with
  no later question the count alone holds it, `Next()` is 3, and `questions` lists no Q2), so no
  number is given out twice; (h) the link killed by a
  failpoint after the SY5 write that folds an admin redaction, with human posts H4 before the
  redaction and H5 after it in the same answer and H6 posted later: the restart finds the build
  owed and builds, never a catch-up; its result equals a fresh build on another seat byte for
  byte; H4 and H5 are delivered once (by the sync, before the kill) and H6 once (by the build),
  none twice; (i) the link killed mid-build, after two of five forward pages of a redaction's
  rebuild and again of a first build, with human posts on both sides of `after`: the restart
  resumes the forward read from the last page's token as a build, live equals rebuilt byte for
  byte, every post past `after` is delivered once and none before it again, and a reminder owed
  by `late_from` is made once.

- **QX6. The caps** (n3f-D17), applied by the fold in stream order, so every seat folding the same
  room holds the same state: at most 64 questions not closed (the coordinator's 65th `ask` is 10
  `open-limit`, "close or answer one first"; an admin's carried question past 64 evicts the oldest
  not closed, which `status` counts); the 128 most recent closed questions (an older one is known
  only as below the highest: `thread` or `reply` naming it is 23 `not-found`, "Q<n> is closed and
  older than the index keeps"); per question the 16 oldest unanswered ids kept and the rest
  counted; the 1024 most recent human posts' attachments (A 10.3's index check on an older post is
  10 `target`); the 64 most recent unattached posts no agent post replies to.

  - **The store's share, worst case.** Each entry is sized as compact JSON at its longest, as
    n3d-D1 sizes the lists: an event id is 46 bytes with its quotes, a user id at most 130 (A4's
    128), an integer at most 19 digits, and a headline of 120 code points at most 722, since the
    encoder writes no code point in more than 6 bytes (`<` for `<`; a 4-byte code point is
    not escaped). The `Question:` section is kept as its SHA-256, 66 bytes in hex with quotes,
    never as text (G1.4): kept as text it would be up to 8000 bytes, escaped up to 6 times that,
    and the 192 the caps keep (64 open, 128 closed) alone would pass 3 MiB.
    - A question: 15 bytes of braces and commas, 131 of keys, and the values `n` 19, `id` 46,
      the thread's latest event 46, `headline` 722, the hash 66, `asked_by` 2 + 16 x 130 + 15 =
      2097, `re` 2 + 16 x 46 + 15 = 753, `asked_ms` 19, `remind_after_s` 19, `closed` 5, the
      attach count 19, `unanswered` 753, its count past 16 19: 4729 bytes, sized 5120.
    - An attachment `{"id":…,"q":…}`: 77 bytes, sized 96. An unattached post (id, `from_id`,
      `sent_ms`): 224 bytes, sized 256. The fixed fields (the token at n3d's `maxNextBatch`,
      1024, the last folded event, the next number, the slot and eviction counts, and the
      `build` record's two event ids and `late_from`; about 1300 bytes with their keys): 2048.
    - The `questions` key: 192 x 5121 + 1024 x 97 + 64 x 257 + 2048 = 983 232 + 99 328 + 16 448 +
      2048 = **1 101 056 bytes (about 1.05 MiB) per team**.
    - The seat keys beside it, each with its key name: `outbox.reports` 60 x 14 + 16 = 856;
      `remind_late` 64 x 20 + 16 = 1296; and four scalars, whose values alone are 19
      (`reminded_ms`) + 46 (`outbox.questions_unechoed`) + 38 (`outbox.questions_uncounted`'s
      two counts) + 5 (`outbox.questions_handover`) = 108 bytes, and with their keys, braces
      and commas about 210, sized **256 together**. n3f adds 1 101 056 + 856 + 1296 + 256 =
      1 103 464 bytes.
    - n3d's bound (`TestStoreFullFitsTheFileCap`: 4096 + 1024 + each list's count x (size + 1))
      is 3 001 136 bytes. The new sum is 4 104 600 bytes, which n3d-D1's rule rounds up to 16 x
      256 KiB = 4 194 304: **`MaxStore` rises from 3 MiB to 4 MiB** (G2), so a cap always fires
      before the file cap. Counted as written, escaping included, as the store's sizing now is
      (the coordinator's decision (c) on n3d's store sizing), the sum is 4 110 165 bytes: the
      same 4 MiB, 84 139 bytes under it.
  - Tests: the 65th `ask` refused; 1100 human posts folded live and rebuilt: the same state; a
    store with every cap full at its maximum sizes (192 questions with 16 `asked_by` of 128
    bytes, 16 `re`, 16 unanswered, a headline of 120 escaped code points; 1024 attachments; 64
    unattached; the longest token), the new keys included, encodes and decodes under
    `MaxStore`, and `TestStoreFullFitsTheFileCap` holds the new sum rounded up.

- **QX7. Redactions** (n3f-D20). Only the admin redacts, one event at a time, with the reason
  `secret` (A 4.2, 12.4; `verdict.go:235`), and a read of a redacted event carries
  `unsigned.redacted_because`, which rule 02 drops `schema` (`verdict.go:260`): a build cannot
  see the post, the question or the thread post it was. So a redaction removes its target from
  the index completely, the way a build would:

  - **By a rebuild.** An admin redaction in questions that the sync or a catch-up folds makes
    the index rebuilt (QX1's build, which replaces it), and the write that folds it records the
    build owed, so a crash before the rebuild ends still rebuilds (QX5, n3f-D21). A build marks where it began (the newest
    event of its first backward page); a redaction its forward read meets after that event is
    new since the build began, and starts the build again; one met before it is history, whose
    target the server already returned stripped. The live index is then the rebuilt one, so the
    two are equal by construction. Why not an un-attach in place: the caps drop what a removal
    would need back (the 17th unanswered id is only counted, the 1025th attachment is gone),
    and a redacted thread post may have closed its question or reset P58's unanswered posts,
    neither of which can be undone from the bounded state. A redaction is rare (the owner's
    `teamchat-admin redact`, for a leaked secret), and a build costs at most 2 x ceil(E/100) +
    1 requests off the delivery path.
  - **A redacted human post** is in no question, no unanswered list, no attachment and no
    `unattached` record; a question it alone answered is `open` again.
  - **A number is never given out twice** (n3f-D20), by redacted slots (G1.9): an event in
    questions from an agent account or the admin whose `redacted_because` is the admin's is a
    slot, and the fold counts the slots since the highest counted was last raised (a question
    that does not raise it, an admin's carried one below the highest, leaves the count as it
    is). `Next()` is 1 plus the highest counted plus that count. Which questions are counted is
    unchanged by it: an agent's question is counted when its `n` is from 1 plus the highest to
    `Next()`, the window of round 2; only the coordinator's own numbering moves, so it always
    numbers past any question that may have been redacted. Each number skipped by a counted
    question is **a number held by a redacted event**: an index entry, `closed`, with no
    headline, no hash and the record's `redacted` true, whether the redacted event was a
    question, a thread post or a plain reply; the count then starts again at 0. A redacted
    question with no later question counted leaves `Next()` past it: Q1 then a redacted Q2 gives
    `Next()` 3, and the next `ask` is Q3, never a second Q2. Why: `Q<n>` is the one name humans,
    main-room messages, agent transcripts and the `uncounted` hook line use, so a human's late
    `Q2:` for the redacted question would attach to a new Q2; and the chain (A 6.5) stays
    unbroken, since a question the build cannot see would otherwise leave every later question
    uncounted by every seat for ever.
  - **Live equals rebuilt.** Every input is what a build sees (the sender and
    `redacted_because`), so the live index, which is a rebuild, and any later build agree. A
    slot that appears later, before a question already counted, never uncounts it: it only
    widens the window that question was judged by. A redacted thread post leaves a harmless gap
    in the numbers. Under this rule the case round 2 accepted as a risk, a question that skips a
    number and is still counted, is the normal path after a redaction, and every seat agrees on
    it. Rejected: a reason naming `Q<n>` in the admin's redaction (a change to A 12.4's fixed
    reason and to C's `teamchat-admin redact` for no gain the slots do not give); giving a
    redacted question's number again (round 2's rule, refused by the lead for the reason above).
  - Tests: QX5 (f) to (i); a redaction arriving while a build runs, after the build's start:
    the build starts again and its result equals a build begun after the redaction; a redacted
    thread post of Q1 then an `ask`: Q3, with Q2 held by a redacted event on every seat; a slot
    before an admin's carried question below the highest: the count kept, the next `ask` past
    it.

## 4. The coordinator's verbs (AK, TH, QL, QR)

- **AK1. `ask [--team T] [--asked-by N,..] [--re ID,..] [--remind-after S]`**, text on stdin read by
  n3f-D1's rule with the cap 8000 (A 10.2's cap is the composed body's, its `Q<n> ` prefix
  included, which the form judges). In this order, the first failure deciding:
  1. CL6 (64 `ambiguous-team`); `--remind-after` outside 600-604800 is 64 `usage` naming the
     range; `--asked-by` and `--re` each at most 16 distinct entries, else 64 `usage`; each `--re`
     an event id by A 2.2's grammar, else 64 `usage` (A 10.2 checks `re` for grammar only,
     n3f-D14);
  2. the class: not the roster's non-parked coordinator, 11 `role` (the message: ask the
     coordinator with `teamchat say --to-coordinator --team T`);
  3. the questions room: untrusted, it is re-verified first (QS8), and a failed check is 22
     `untrusted` naming it and the check; a re-verification that cannot reach the server is 20
     `unreachable` (14 `server-rate` for a 429 past its retries), nothing charged and nothing
     queued; joining, awaiting its invite (QS2), its index not ready (QX1) or an own post not
     yet echoed (QX3), the verb waits within its deadline, then exits 20 `not-ready`;
  4. `--asked-by`: each name resolved as n3d's `--to` resolves it, against the verified roster, to
     a member's user id: 64 `unknown-member` or `ambiguous-member`; a parked or offline member is
     accepted (n3f-D4); a human or the probe is 64 `unknown-member`;
  5. more than n3f-D1's rule allows, or `Composer.Question`'s form: 10 `body`, naming the section;
     the secret check: 10 `secret`; an open or answered question whose `Question:` section has
     the same SHA-256 once collapsed (G1.4): 15 `duplicate` naming that `Q<n>` (B 2.2.4's duplicate code; A's `duplicate` refusal); 64
     questions not closed: 10 `open-limit` (QX6);
  6. `Admit` (14 `rate`; 15 `duplicate` for the same body within 60 s);
  7. SD3's write, QX3's provisional entry, then n3d's `sent` record with `"room":"questions"` and
     the `question` record (section 6) with `state` `open`, `unanswered` `[]` and `n` the number
     the question got (both with a null id when queued, n3f-D10). Exit 0. A number later found
     to be another's is told as QX3 says.
     Tests: each step's refusal; numbering after a `question_floor`, after an admin's carried v1
     question, after an uncounted bad `n`; the PUT's content (`m.text`, every human mentioned,
     `asked_by`, `re`, `remind_after_s` only when given); a queued `ask` while the homeserver is
     down, posted when it returns, numbered as composed.
- **TH1. `thread Q<n> [--ask] [--close] [--team T]`**, text on stdin read by n3f-D1's rule with the
  cap 4000. In this order:
  1. CL6; `--ask` with `--close` is 64 `usage` (n3f-D3);
  2. the class (11 `role`), the room (22, or 20 `not-ready` after waiting) and the index (20
     `not-ready` after waiting), as AK1 steps 2-3;
  3. `Q<n>` not a counted question the index keeps: 23 `not-found`, naming it; `--close` on a
     question already closed: 10 `target` naming it ("already closed; post without --close")
     (n3f-D3); a post without `--close` on a closed question is posted, and the question stays
     closed (A 10.4 has no reopening);
  4. `Composer.Thread` with `N`, no `Answers` (a follow-up names no human post: QR1 does): the
     fallback reply is the thread's latest event (A 10.3); more than n3f-D1's rule allows, a body
     blank and not closing, 10 `body`; 10 `secret`;
  5. `Admit` (14 `rate`, 15 `duplicate`);
  6. SD3's write, QX3, a `sent` record with `room` `questions`, then the question's `question`
     record as it stands after the post. Exit 0.
     `--ask` sends `m.text` mentioning every human, otherwise `m.notice` with no mentions;
     `--close` with no text sends the body `Q<n> closed.` Tests: each refusal; a follow-up's
     fallback naming the latest human post; `--close` changing the record's state to `closed`; a
     plain post on a closed question.
- **QL1. `questions [--all] [--team T]`** (B 2.2.3's multi-team verb, 60 s deadline): over each
  active team where this member is the non-parked coordinator (n3f-D7), from the index, with no
  network call but QS8's re-verification when the room is untrusted (a 429 past its retries 14
  `server-rate`, unreachable 20 `unreachable`, as `verbErr` maps them):
  - one `question` record per counted question the index keeps, by `n`: without `--all` those
    `open` or `answered`, with `--all` every one kept;
  - one `unattached` record per unattached human post that no agent post replies to (G1.3), by
    time, those sent within the last 14 days;
  - at most 256 records per team, then one `more` record with the count left out;
  - nothing at all: no records, exit 0.
    With `--team T` and this member not T's coordinator: 11 `role`. Without `--team` and no team
    where it is coordinator: 11 `role`, naming the teams. A team that fails (22, 20, 14) is a
    `team-failed` record, the verb exits 5 (B 2.2.3); every team failing exits with the first's
    error. A team whose room or index is not ready waits within the deadline, then is a
    `team-failed` record with 20 `not-ready`. Tests: each state; P58's unanswered ids (human posts
    after the coordinator's last post in the thread, edits included); a handover: a new
    coordinator lists its predecessor's open questions and their unanswered posts, and the
    unattached posts made before its join (OW4); an unattached post listed until it gets a reply;
    300 open and closed questions with `--all`: 256 records and `more`.
- **QR1. `reply <id>` on a questions-room item** (extends n3d's SD1, which decides room and thread
  from the ledger): attached to `Q<n>`, `Composer.Thread{N: n, Answers: id}` (the thread form
  with `is_falling_back` false, A 10.3); unattached, `Composer.Thread{Answers: id}` (a plain reply,
  no `n`, closing nothing). `--verb` on a questions item is 64 `usage` (no ping to answer). The
  class, the room and the index as TH1 step 2; then A 10.3's sender index check: an unattached
  post the index did not see unattached, or an attached post not attached to that question (or
  older than QX6 keeps), is 10 `target`; then TH1 steps 4-6, printing `sent` and, when attached,
  the `question` record. An id the ledger does not know is looked up on the homeserver in main
  (n3d's lookup) and, only for the coordinator with the questions room ready, in questions; a
  member that is not the coordinator never asks questions (its 403 would read as 22), so for it
  an id main does not have is 23 `not-found`. Tests: each form's content; a reply to an unattached
  post leaving `questions` with no `unattached` record for it; the index check refusing a post
  attached to another question; a worker's `reply` to an id only in questions: 23, no request to
  questions.
- **QR2. `history --room questions [--team T] [--limit N]`**: the coordinator only (11 `role`
  otherwise, before any request), the room verified (22), then as LN6's `history`: `/messages`
  backwards, newest first, `--limit` as n3c-D8, consuming nothing, changing nothing: the index
  is read, never fed, and no flood budget is spent. Each human post is shown as an `in`
  `history` record whose item carries `room` `questions` and the attachment `Attachment` gives it
  (or `AttachmentOf` for a post the index does not keep); each agent post in the room (this
  member's and earlier coordinators') as an `out` record with its question. A 429 past its
  retries is 14 `server-rate`, unreachable 20 `unreachable` (`verbErr`). Tests: a thread reply
  and its edit shown attached; the index and the flood window byte-equal before and after; a
  worker refused 11 without a request made; a 429 past its retries: 14.

## 5. Reminders (RM) and the offline rule (OF)

- **RM1. The reminder** (A 10.5): for each counted question with `remind_after_s` that is still
  `open` at its `origin_server_ts` plus that bound plus 30 s, one TIMEOUT item for the
  coordinator, keyed `<question event id>~humans` (`proto.ReminderKey`), `room` `questions`, kind
  timeout, made by the team goroutine from `QuestionIndex.Reminders` on a timer set to the
  earliest due (never a tick, as SD4). The timer is on wall time (`origin_server_ts` is the
  server's wall clock) and is set again after an SY12 freeze and after every fold. Reminders are
  made only from a ready index (QX1, QX3): while a build or a catch-up runs, or an own post is
  not yet echoed, none is made and the watermark is not raised, since a question open in a
  partial index may be answered later in the room. Every reminder due is put in `pending` in
  one store write that also sets `reminded_ms` to the latest due time made and removes the
  questions it reminded from `remind_late` (n3f-D11). A question whose due time is at or below
  `reminded_ms` is not reminded again by this seat, unless its number is in `remind_late`.
  **`remind_late` holds only questions that are `open` and have `remind_after_s`** (n3f-D11): such
  a question first counted by a build or a catch-up (its number at or above the index's `Next()`
  when that build or catch-up began, the build record's `late_from` for a rebuild, QX5; or any
  number when there was no index) is put in by the page write that counts it, and is judged by
  its own due time, not by the watermark. A number is removed in the write that reminds it, and
  in the write that folds its question out of `open` (answered or closed, a redacted slot
  included) or evicts it (QX6); a build's last page write removes every number whose question
  is not open in the finished index. **The cap of 64 holds:** each number is an open question
  of the index written with it, and QX6 keeps at most 64 not closed; during a rebuild the
  entries kept from the replaced index are its open questions, numbered below `late_from`, and
  a number at or above `late_from` is counted only by an event after the replaced index's last
  folded event, by which point the forward read has folded each kept entry again and removed
  those no longer open. A question that is `answered` or has no `remind_after_s` is never put
  in, so a first build over a room of 300 questions puts in at most its open ones. Its envelope entry is A 13.2's reminder form and its record a `timeout` with
  `"silent":"humans"`, `"question":"Q<n>"` and no `verb` or `ref`. Tests: due exactly at the bound
  plus 30 s and not a millisecond before (fake clock); answered first, no reminder; a link restart
  after the reminder was made, no second one; a replacement coordinator (another seat's store)
  reminded once; a freeze over the due time: reminded once on thaw; a build held by fakehs
  after the page holding Q3 (open, overdue) and before the page whose human post answers it:
  no reminder and `reminded_ms` unchanged during the build, none after it; an admin's question
  posted while the room was untrusted, overdue and with a due time below `reminded_ms`, counted
  by the recovery's catch-up: reminded once when the index is ready, and not again after a
  restart; a first build over 300 questions, 70 of them open with `remind_after_s` (the
  admin's carried questions, 6 of them evicted by the open cap), 30 answered and 200 closed: `remind_late` never above 64 at any page write, and
  the store write is accepted; a question in `remind_late` answered before its due time:
  removed in that fold, never reminded.
- **RM2. HUMAN and TIMEOUT room**: a reminder takes `pending`'s reserved share (B 2.3.2), so a
  flood of agent items never holds it back.
- **OF1-n3f. Offline members never block** (SPEC-n3 OF1, n3b-D1, the owner's rule): an offline
  member never blocks anything, and only an operation that targets that member reports it, as 69.
  No n3f verb, join, read or reminder targets another member: `report` posts to the reports room
  and `ask`, `thread` and `reply` to the questions room, and `--asked-by` records who asked
  without addressing them, so no n3f verb waits for, refuses for, fails because of or exits 69
  for another member being offline; a parked or stopped member is named in `--asked-by` as any
  other (its being offline is not looked at). A coordinator that is offline blocks no human and no
  worker: human posts wait in the room and are delivered from its floor when it runs (QS5);
  reminders are computed from history and come once when it runs (RM1). Test (each area's row): a
  team with one worker stopped, its container gone, while the coordinator asks naming it in
  `--asked-by`, threads, lists and is reminded, and the other worker reports: every verb exits 0
  and its output is the same as with all members running.

## 6. Records (B 2.2.2, settled for n3f)

```json
{"kind":"question","v":1,"team":"<team>","n":4,"state":"open","id":"$<43>","headline":"Which licence for the new docs site?","asked_by":["<repo>.2"],"re":["$<43>"],"asked_ms":1760000000000,"remind_after_s":null,"unanswered":["$<43>"],"unanswered_more":0,"redacted":false}
{"kind":"unattached","v":1,"team":"<team>","id":"$<43>","from":"alice","sent_ms":1760000000000}
{"kind":"human","v":1,"team":"<team>","id":"$<43>","from":"alice","from_id":"@alice:<server>","sent_ms":1760000000000,"room":"questions","text":"MIT, please.","question":"Q4","headline":"Which licence for the new docs site?","revises":null,"restored":false}
{"kind":"timeout","v":1,"team":"<team>","id":"$<43>","silent":"humans","question":"Q4","verb":null,"ref":null,"restored":false}
{"kind":"room","v":1,"team":"<team>","purpose":"questions","state":"joined","check":null,"uncounted":0}
```

- `question`: `state` one of `open`, `answered`, `closed` (A 10.4); `id` the question's event id,
  null while its send is queued (n3d-D2) and for a redacted question; `redacted` true for a
  number kept by a redacted question (QX7), which is `closed` with every other field null or
  empty, and false otherwise; `headline` the asking coordinator's first line as a JSON
  string; `asked_by` the askers' `from` forms (P69); `re` the main-room ids; `asked_ms` the
  question's `origin_server_ts`, null while queued; `remind_after_s` its value or `null`;
  `unanswered` the ids of P58's unanswered posts, oldest first, at most 16, and `unanswered_more`
  the count past them (QX6).
- `unattached`: one human post in questions attached to no question and replied to by no agent.
- `human` gains `headline` (the attached question's, else `null`); `question` changes from a
  number to a string: `null` in main, `"Q<n>"` when attached, `"none"` when unattached; `room`
  `main` or `questions`.
- `timeout` for a reminder: `silent` `humans`, `question` `Q<n>`; for a ping's TIMEOUT `question`
  is `null`. It gains `restored`: true for a reminder made again after a restore (QX1, SY6),
  false otherwise, as on `human`.
- `room` (in `status`, LN5): `purpose` `reports` or `questions`; `state` one of `joined`,
  `awaiting-invite` (QS2), `left` (QS8), `building` (QX1), `refused` (RP1), `untrusted` (QS8, QS2,
  RP4, with `check` naming the failed check), `not-coordinator` (questions, for a worker);
  `uncounted` for questions the number of this member's own questions and thread posts in the
  current term whose echo was not counted (QX3), and `null` for reports. It is seat state, not
  the room's, so it is kept in `outbox.questions_uncounted` (its `count`; `said` records the
  hook lines taken, QX3), never in the `questions` key, and dropped at a demotion (QS9).
- `more` (B 2.2.2's) closes a `questions` listing cut at 256 records (QL1).

The skill (PK2) and its text test cover these kinds, fields and codes in the same commit that
adds them.

### 6.1 The promotion NOTICE and the skill step (OW4)

Under OW4 (a) a promoted coordinator's run of `questions` is the only thing that surfaces the
human posts made during a handover, so the step is said where the session learns of its
promotion, said again when the index can answer, and pinned in the skill. The NOTICE comes
while the promotion's build runs, when `questions` may exit 20 `not-ready`; so QX1's ready
line follows once the index is ready. All three texts are fixed word for word:

- **The NOTICE.** A 13.3's coordinator role line, which DV6's membership NOTICE carries at a
  promotion and at a join as coordinator, gains a second sentence. The line becomes, `<t>` the
  team:
  `in team <t> you are the coordinator. Human posts made in the questions room while the coordinator changed are not delivered to you; to see them, run teamchat questions --team <t>.`
  proto's `membership.go` renders it and the corpus pins it (A 13.3 is amended to match); the
  line is within A 13.3's 600-character rule for any team name A allows.
- **The skill step**, in the skill's questions-room section:
  `When a notice says you are now the coordinator of team T, first run teamchat questions --team T. Human posts made in the questions room while the coordinator changed are listed there and are not delivered to you; answer or reply to each one. If it exits 20 not-ready, run it again when a teamchat line says the questions room is ready for you as coordinator.`
- **The ready line** (QX1), a hook line at the next UserPromptSubmit or SessionStart once the
  promotion's build has reached the head and the index is ready, said once (its key
  `outbox.questions_handover`):
  `teamchat: in team <t> the questions room is ready for you as coordinator. To see its open questions and the human posts made while the coordinator changed, which were not delivered to you, run teamchat questions --team <t>.`
  It is within the 600-character line rule for any team id A allows (at most 22 characters).
- **The test** (n3f-e), as n3e-11's PK2 test pins the exit classes word for word: the rendered
  role line for a promotion equals the NOTICE text above with `<t>` filled, byte for byte; the
  ready line as the link renders it equals the text above with `<t>` filled, byte for byte; the
  skill holds the step text byte for byte; the `teamchat questions --team` command in each of
  the three parses with CL2's parser to the `questions` verb, with no word after the team
  read as an argument (PK2); a promotion through the team goroutine delivers one membership
  NOTICE holding the role line, and once its build reaches the head one ready line; and the
  same with the link killed mid-build: the ready line is said once, after the restart's build
  ends.

## 7. Departures from v1 (Plan 00211's Python)

v1 is the prototype. What it was for is kept: a quiet read-only reports room nobody is woken by;
a questions room only the coordinator and the humans are in, every human message reaching the
coordinator; numbered self-contained questions; threads; answers dispatched by the coordinator.
Where it was weak:

- **n3f-X1. No `no-room` and no deploy-time pre-flight on members.** v1's `ask-human` and `report`
  had a `no-room` refusal for rooms not yet made, and its deploy 2 refused while any LXC container
  was down. In v2 every room exists from the team's creation (A 4.1), so a missing room is a
  fault (22), and nothing in n3f, nor its acceptance, refuses because a member is stopped (OF1).
- **n3f-X2. The index is a chain, not "one more than the highest".** v1 numbered from the highest
  `Q<n>` among the coordinators' posts, so one bad post moved the numbering for ever; v2 counts an
  agent's question only at the next number (A 6.5), with `question_floor` for a migration.
- **n3f-X3. Unattached posts are listed.** v1's `questions` listed replies per question only, so
  a topic the owner started unprompted (00211 D15) reached the coordinator once and was lost to a
  handover. `questions` lists each unattached human post until an agent replies to it.
- **n3f-X4. A new coordinator's history is read, not delivered.** v1 skipped the room's history
  on join and relied on the coordinator running `questions`; v2 does the same for delivery (QS5)
  but builds the whole index from history first, human posts included, so numbering, states and
  unanswered posts are right from the first verb, and the reminders of open questions come once.
- **n3f-X5. Reports' hourly window survives a restart.** v1 kept its send times in a file of its
  own beside the outbox for an upgrade-compatibility reason v2 does not have; v2 keeps them in the
  store, in the same write that charges the gate (n3f-D6).
- **n3f-X6. `thread --re` is gone; `reply` answers a human post in questions** (A 10.3), and a
  reply to an unattached post is a plain reply that closes nothing.
- **n3f-X7. A side-room failure is said in `status`, the frame and one hook line, and never in
  the exit of a main-room verb**; v1 exited 10 for a side room (a protocol refusal), which told
  the agent to change its message. v2 uses 22, "tell a human" (B 2.2.4).
- **n3f-X8. Reminders.** v1's questions waited with no expiry; `--remind-after` (A 10.5,
  OD-A6) makes one TIMEOUT for the coordinator.

## 8. Decisions

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n3f-D1  | **The stdin rule and the headline and body split** (SR1): a verb with text cap C reads stdin to at most C + 2 bytes; more than C + 1 bytes read is refused 10 `body` without stripping; otherwise exactly one trailing newline is removed, and a text still over C is refused 10 `body`. The text is then used whole as the `body`, its first line the headline (A 10.1, 10.2). Never cut: C bytes, a newline and one more byte are C + 2 bytes read, refused, where reading C + 1 and stripping would have accepted C bytes and lost the last. Why one newline: every way a shell passes text (`echo`, a heredoc, `printf '%s\n'`) ends in one newline the writer did not mean as content, and removing more would change text the agent wrote; A's form trims the headline itself. Tests, for each of `report`, `ask` and `thread`: C bytes, a newline and `x` refused; C bytes and a newline accepted with a body of C bytes; C + 1 bytes with no newline refused.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| n3f-D2  | **The `human_text` pending invite is not built** (SR2): A 5.1 makes a coordinator with `human_text: false` an invalid roster, so main fails trust and the questions invite is never reached. B 2.6 step 10's bullet and SPEC-n3 SR2's "left pending while `human_text` is false" describe v1's state, which v2's roster rule removed; QS3 tests the roster failure instead. Why: a second path for an impossible state is dead code.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| n3f-D3  | **`thread --ask --close` is 64; `--close` on a closed question is 10 `target`.** Why: a post that asks the humans again and closes the question in one message tells them two opposite things; a second close is a mistake to fix, and refusing it says so, where succeeding silently would hide a lost text.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| n3f-D4  | **`--asked-by` accepts any roster member, parked or offline**, and refuses humans and the probe. Why: `asked_by` records who asked, so the coordinator knows whom to answer; refusing an asker who has since stopped or been parked would block the question for a fact about that member (OF1).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| n3f-D5  | **Questions-room composes are serialised per team by the team goroutine** (QX4), each numbered against the index after the previous one's SD3 write placed its provisional entry. Why: the index's `Next` is the only numbering, and two concurrent `ask`s would both take it; the second would then be uncounted by every member (A 6.5).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| n3f-D6  | **`report_per_hour` is kept in the store**: `outbox.reports`, a list of at most 60 report times (the limit's upper bound), pruned past 3600 s, written in SD3's store write. A report charged and then refused at its post (RP4) still holds its slot, and the 14 `report-rate` message says so ("a report that was not posted still counts"). Why: an in-memory window resets at every link restart, so a crash-looping or restarted link could post past the limit; B 2.3.2 had no home for it.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| n3f-D7  | **`questions` and `history --room questions` without `--team` act over the teams where this member is coordinator**, and refuse 11 only when there is none. Why: B 2.2.3 runs a multi-team verb over every active team, which would make every coordinator in a session that is also a worker elsewhere get exit 5 with a `team-failed` it can do nothing about.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| n3f-D8  | **A side room's transient failures in the sync are the team's** (SY8's backoff, the answer held): one sync token covers main and questions, so a questions fill that fails transiently holds the whole answer, as v1 learnt (its gap fill was never contained). A fill past SY3's bound, and every history read of the index, is QX1's on its own goroutine, so a questions room whose history fails for ever never holds main. A non-transient failure is the side room's alone (QS8). The questions room's join (QS2) is not a sync request: its failure other than a 403 is one log line and the next try 30 s on, never the team's backoff.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| n3f-D9  | **The index is persisted in the store with its position, and the room is read from its start only when the seat has none** (QX1, QX5). This replaces the 100-page bound and its repair: raising `question_floor` is an admin migration-only write (A 5.1) that would not have shortened a read from `m.room.create` anyway, so past the bound `ask` would have been refused for ever. Now a link restart reads nothing; a build happens on a first promotion, after an epoch change, a refused token or an admin redaction (QX7); a gap or a recovery is a catch-up from the token. The build has no page bound, because nothing waits on it: it runs off the delivery path, its memory is the caps of QX6 plus one page, and its requests are at most 2 x ceil(E/100) + 1 for E events in the room (a room of 100 000 events is 2001 requests, under the team's backoff). The verbs wait and exit 20 `not-ready` meanwhile, which says "retry", never "tell a human". Rejected: the high-water and the numbers alone in the store (a restart would forget which questions were answered, review M1); a checkpoint posted in the room (a new A event and trust rule for what the store does for restarts, promotions being rare); a page bound with any repair (every repair needs an admin write the protocol does not have). The persisted state is the room's fold, the same on every seat, so A's "no session state" holds.                                                                       |
| n3f-D10 | **`ask`, `thread` and `reply` in questions print n3d's `sent` record, then the question's record**; a queued question's record has a null `id` and `asked_ms`. Why: B 2.2.1 has `ask` print one `question` record, whose `id` is the event id; with n3d-D2 a send may be queued with no id yet, and `sent` is the one record that says so. `thread` changes a question's state, which the agent should see without a second verb. B 2.2.1's `ask` line is to read "a `sent` record, then the `question` record" (section 12.4).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| n3f-D11 | **Reminders made are kept as a watermark**: `reminded_ms`, the latest due time this seat has reminded; a reminder whose due time is at or below it is not made again, and the TIMEOUT is keyed `<id>~humans` (A 10.5, `proto.ReminderKey`). Every reminder due is made in one write that raises the watermark, in due order, and only from a ready index (RM1). A question `open` with `remind_after_s` and first counted by a build or a catch-up is put in `remind_late` by the page write that counts it, and is removed when reminded, when it leaves `open` and when it is evicted, so the list holds at most 64 numbers, the cap on questions not closed (RM1 shows the bound across a rebuild); and is judged by its own due time, not the watermark. Why it is exact: a question's due time is fixed by history; a question the sync counts is known from its own post (QX3) or the sync before its due time passes, so it is never found below the watermark; the questions found late are those a catch-up or a build counts (an admin's or a predecessor's post that landed while the room was untrusted, or before a rebuild), and `remind_late`, written in the same write as the fold that counts them, makes each one reminded once, across a crash too; a new term starts with neither (QS9 drops both), so a promotion reminds each overdue open question once. One number and a list bounded by the open cap replace a list of every reminder made, so no reminder is ever dropped. |
| n3f-D12 | **The reports join is eager, at the team's start** (RP1), and retried at a `report`. Why: C's board shows each member's reports membership (C 3.11.3's `rooms`), A 4.1 lists every active member in reports, and a join at the first `report` would make the board show a working member as not joined.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
| n3f-D13 | **A questions room on its way is a passing state** (QS2, QX1): a questions verb before the invite has arrived, while the join runs, or while the index is built or caught up waits within its deadline, then exits 20 `not-ready`. 22 `untrusted` is kept for a room that fails its check, and for an invite still absent 300 s after the roster named this member coordinator. Why: A 5.2 sends the invite right after the roster write, and a build ends by itself, so both pass without a human; 22 says "tell a human" and would send the agent to a human for a state that fixes itself, while an invite that never comes is the admin's to repair.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| n3f-D14 | **`--re` is checked for grammar only** (A 10.2), with no lookup on main. Why: A says so, receivers do not check it, and a lookup would make `ask` fail 20 while the homeserver is down, where n3d-D2 queues the send.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| n3f-D15 | **Each sync answer is judged in a fixed order** (QS1): main's invite, main's trust and roster, the side-room invites, the questions room's membership and trust, then main's timeline and the questions timeline. Why: `proto.Invite` judges against the roster it is given; judged first against the roster from before the answer, the invite that follows a promotion in the same answer (A 5.2) would be rejected and the room left for good, and a demotion's kick would read as a trust failure.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| n3f-D16 | **`report` verifies the reports room at the verb when the server answers, and the sender verifies it again before posting a queued report** (RP4). Why: B 2.6 step 10 asks for verification at each send, and the owner's rule (n3d-D2) asks that a send to a down server be queued, not failed; verifying at the verb keeps a failed check from charging anything, and verifying at the sender keeps a report queued past a change from being posted into a room that no longer verifies.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| n3f-D17 | **The index is bounded by caps applied in its fold** (QX6): 64 questions not closed (the 65th `ask` is 10 `open-limit`), 128 closed kept, 16 unanswered ids per question, 1024 human posts' attachments, 64 unattached posts; the `Question:` section kept as its SHA-256, not its text; at most 1 101 056 bytes (about 1.05 MiB) in the store per team at the worst case, escaping included, which raises `MaxStore` from 3 MiB to 4 MiB by n3d-D1's rule (QX6 gives the sum); `questions` prints at most 256 records per team, then `more`. Why: the index lives for a link's life and in the store, and the room grows for ever; caps applied in stream order keep the live and the rebuilt index identical, which a trim by age or at load would not.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| n3f-D18 | **Attachment does not depend on delivery** (QS6, QX2): a human post is attached whether it is delivered, dropped `rate` or past `human_max_age_s`, and a held answer's attachments are committed only after its store write (QX4). Why: the build reads history with no flood window, so attaching only delivered posts would make a rebuilt index disagree with the live one; and attaching before the write would count a re-read post twice.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| n3f-D19 | **Only a fold of the questions room writes the `questions` key** (QX5): an answer's SY5 write whose filter held questions and whose questions timeline was folded, or a QX1 page's write, both through the engine's one store writer; every other SY5 write leaves the key as it was. Why: while questions is out of the filter (a build, a catch-up, QS7's gap past the bound, QS8's untrusted room) SY5 goes on persisting main answers, and a token taken from those would start the next catch-up past events never folded; the next `ask` would then take a number already used, uncounted by every member (A 6.5), and P1 would lose the posts in between.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| n3f-D20 | **A redaction is applied by rebuilding the index, and a number is never given out twice** (QX7). `Next()` is 1 plus the highest counted plus the redacted slots since the highest was last raised; which questions are counted is unchanged (an agent's `n` from 1 plus the highest to `Next()`), so live still equals rebuilt. Each number a counted question skips is held by a redacted event. Why: a build sees a redacted event only as its sender and `redacted_because`, so the one live state a build can reproduce is the one a build makes; an in-place un-attach cannot restore what the caps dropped or undo a redacted thread post's close. The slots keep the chain (A 6.5) unbroken past a redacted question, from what every build sees, and numbering past them keeps `Q<n>`, the one name humans, main-room messages, transcripts and the `uncounted` line use, from ever naming two questions: a human's late `Q2:` for a redacted Q2 can never attach to a new one. Rejected: dropping the number (every later question uncounted for ever); giving the number again with no later question (a late answer attaches to the wrong question); a reason naming `Q<n>` in the admin's redaction (a change to A 12.4 and C for nothing the slots lack).                                                                                                                                                                                                                                |
| n3f-D21 | **A rebuild owed or under way is persisted in the `questions` key** (QX5): the write that folds an admin redaction writes the key as a build owed, with the replaced index's newest event whose items were taken (`after`) and its `Next()` (`late_from`); each page write of a build records its `start`, `after` and `late_from`; a load that finds a `build` record resumes the build, never a catch-up. Why: without it the redaction's write would persist the old index with its token past the redaction, and a crash before the rebuild ends would leave live and rebuilt different for good; and a partial build reloaded as an index with a token would be caught up, taking every human post after the floor as an item, posts the sync had already delivered. Tests: QX5 (h) and (i).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |

## 9. For the owner

Product behaviours with no clear best answer. The team coordinator has set each recommendation
below as the **coordinator default**: n3f is built on OW1 (a), OW2 (a), OW3 (a), OW4 (a), OW5 (a)
and OW6 (a). Each stays open to the owner changing it later; the coordinator lists them for the
owner (OW5 and OW6 are for the coordinator to answer, engineering readings and not product
behaviours).

- **OW1. A human posts in questions while the coordinator is offline.** Nothing tells the human
  that no coordinator session is running; the post waits and is delivered when one runs (OF1).
  Options: (a) nothing in the room, the board shows the coordinator offline; (b) the admin posts a
  quiet notice in questions when its coordinator's seat has been free for a while.
  **Recommendation: (a).** (b) adds an admin post driven by liveness, which is display, and a
  notice per outage in a room meant to be quiet.

- **OW2. Telling the askers that their question went up.** When the coordinator runs `ask --asked-by`, the askers learn nothing unless the coordinator says so in main. Options: (a) as
  v1, the coordinator decides; (b) `ask` also sends each asker a quiet text in main naming
  `Q<n>`. **Recommendation: (a).** The skill tells the coordinator to tell the asker; (b) costs
  each asker a model turn for a fact it may not need.

- **OW3. The index's caps** (n3f-D17). The index no longer has a size bound on the room (n3f-D9);
  what bounds it are its caps: 64 questions not closed, 128 closed kept, 1024 human posts'
  attachments, 64 unattached posts, which at their worst take about 1.05 MiB of store per team
  and raise `MaxStore` to 4 MiB (QX6). Options: (a) the caps as stated; (b) higher caps, at more
  store per team. **Recommendation: (a)**: 64 questions open at once is far past what a team asks,
  and a refused 65th `ask` tells the coordinator to close some; the owner may prefer to be told the
  figures are ample before they are built.

- **OW4. Human posts during a coordinator handover reach nobody's inbox.** At a demotion the old
  coordinator's questions items not yet seen are dropped (QS9), and the new coordinator delivers
  only what is posted after its own join (QS5). A human post made in between, or accepted by the
  old coordinator and never seen, is in no session's inbox; it is in the room and in the index.
  Options: (a) as specified: nothing is delivered across the gap, every such post is listed by
  `questions` (as an unanswered post of its question, or an `unattached` record), the promotion
  NOTICE names `teamchat questions --team T`, QX1's ready line names it again once the index
  can answer, and the skill tells a coordinator to run it when it is promoted, all three texts
  pinned word for word (section 6.1); (b) the new coordinator delivers
  from the old coordinator's leave of questions instead of its own join, so posts in the gap are
  delivered (the dropped items of the old one are still not); (c) as (b), and a demotion hands
  the old coordinator's unseen questions items to the new one through the room by an admin post
  listing their ids. **Recommendation: (a).** After review M1's fix nothing is lost: the index
  holds every post with its question, and `questions` lists them; (b) reads the room's membership
  history to find the leave, and delivers posts the humans may already have seen answered in
  Element; (c) adds an admin post and a trust rule for it. If the owner prefers (b), it is QS5's
  floor taking the predecessor's leave event in place of the own join.

- **OW5. What the index's 1024-entry post list holds** (QX6, n3f-D17; for the coordinator to
  answer, found by the n3f-a index builder). QX6 says the list is "the 1024 most recent human
  posts' attachments". The fold also has to remember, by event id, each agent thread post (it
  resets a question's unanswered count and may close it, A 10.4) and each held slot, or an
  event fed again (a page re-read after a failed write, QX4's held answer, QX1's catch-up) would
  apply twice: a thread post fed again after later human posts would reset their unanswered
  count to 0 and wipe it. QX6's byte sum has no list for them. Options:

  - (a) **As built in n3f-a:** the same list, each entry `{"id","q","t"}` with `t` an integer
    kind (0 a human post attached by another rule or to none, 1 a human post in a question's
    thread, 2 an agent's thread post, 3 a slot; a bool cannot tell an agent's thread post from a
    human's, and `Composer.threadRelation` must go on seeing human posts only). At most 83 bytes
    inside the 96 the sum sizes, so the 1 101 056-byte sum and `MaxStore` stand. QX6's wording
    becomes "the 1024 most recent human posts and agent thread posts". **What it breaks:** the list
    is shared, so 1024 agent thread posts in a row push every earlier human post out of it; an
    older human post then fails A 10.3's index check (`reply` to it is 10 `target`) and a late
    edit of it loses its attachment (rule 4), though the question itself is still kept. If thread
    posts and human posts alternate one for one, the human window is 512 posts; it is 1024 only
    while the coordinator posts no thread post.
  - (b) A second list for agent thread posts and slots, with its own cap (256 entries of
    `{"id","q"}`, sized 96 as an attachment is, 97 with its comma: 24 832 bytes more; the store at
    its widest, 3 428 163 bytes, becomes 3 452 995, under 4 MiB), so the humans keep all 1024. **What it breaks:** n3f-a's finished
    codec, `QuestionsState` shape, fold and size test are reworked and the 71 codec mutants
    re-run; a second eviction rule must keep live equal to rebuilt; the format gains a field
    before any seat has written one (cheap now, a migration later).
  - (c) Remember no ids: apply a thread post only when it is past the persisted token. **What it
    breaks:** idempotence by event id (n3f-D18's re-read of a held answer, the property test that
    live equals rebuilt after any re-feed), and the unanswered count of a question whose old
    thread post is fed again; it needs an exact-once feed position in place of the id, which the
    spec does not give.

  **Recommendation: (a).** It is built, tested and inside the sum. The case it harms (1024
  coordinator thread posts with no human post between) costs the coordinator a model turn each,
  and what it loses is the attachment of a human post that old; the question and its other posts
  are kept. If the coordinator would rather the humans' window never shrink, (b) is the choice,
  and it is cheaper before n3f-c writes the key than after. On the answer, QX6's sentence and
  n3f-D17's row change to say which list holds what.

- **OW6. How a member waiting for its questions invite learns it has come** (QS2, QS4; for the
  coordinator to answer, found by the n3f-c1b builder). QS2 makes an invite the signal to try
  the join, and QS4 makes the filter the worker form, which names main alone, while the room is
  not ready. The server filters invites by room (fakehs does, as Tuwunel's room filter), so the
  invite never reaches a member that is waiting for it. Options:

  - (a) **As built in n3f-c1b:** an invite is judged when one arrives (QS1's test calls `handle`
    with one), but none arrives under the worker form; the join is tried at each trigger and at
    each answer 30 s or more after the last try. **What it breaks:** a promoted coordinator whose
    first try is refused 403 because A 5.2's invite has not been sent yet is joined at the first
    answer 30 s on, and a long-poll ends within 30 s, so up to two long-polls after the refusal,
    with its questions verbs at 20 `not-ready` meanwhile; the invite trigger is code nothing in a
    running team can reach.
  - (b) QS4 lets the coordinator form through while the room is awaited (this member the
    non-parked coordinator, the room not joined): a room not joined has no timeline, so the
    filter then delivers the room's invite and nothing else of it, the invite is the signal, and
    the join is at once. **What it breaks:** QS4's rule (the coordinator form only when joined,
    verified and at the head) and its filter tests change, and the filter changes once more at
    the join; the build's and the catch-up's filter (worker form while they run) is unchanged.
  - (c) QS2 drops the invite trigger and says the join is polled. **What it breaks:** nothing
    that runs today; the QS1 test of an invite in an answer goes, and a first promotion costs
    the 30 s above, as in (a).

  **Recommendation: (b).** It makes the design's own signal (A 5.2) work for the cost of one
  condition in `filterFor`, and removes the 30 to 60 s in a promotion's first minute. Until the
  coordinator answers, n3f is built on (a): the cost is only the first promotion's latency, and
  (c) is the same behaviour with the dead code removed.

## 10. The slicing

| Part  | Covers                                                                                                                                                                                                                                                                                                                                   | Needs              |
| ----- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ |
| n3f-a | G1 (proto's index: idempotence, attachment, caps, provisional entries, the hash, redacted slots and `Next()` past them, the codec with the build record), G2 (the store's keys and caps, `MaxStore` at 4 MiB), G4 (records), G5 (the filter), G6 (the report window), G7 (exits), section 6's codecs                                     | n3d and n3e merged |
| n3f-b | the reports room: RP1-RP6, the `room` record for reports in `status`, OF1-n3f's `report` row                                                                                                                                                                                                                                             | n3f-a              |
| n3f-c | the questions room's sync: QS1-QS10, QX1-QX7 (the build record's writes and its load, QX5 (h) and (i)), G3, RM1-RM2 (`remind_late`'s rule), QX1's ready line and its key, the envelope's room lines filled (DV2's renderer), PT1's runs gaining questions items                                                                          | n3f-a              |
| n3f-d | the questions verbs: AK1, TH1, QL1, QR1, QR2, the rest of OF1-n3f; CL2's `unbuilt` list without n3f's verbs                                                                                                                                                                                                                              | n3f-c              |
| n3f-e | the skill's text for the side rooms and its text test (PK2), section 6 in the skill; section 6.1: the promotion NOTICE's role line (proto's `membership.go` and its corpus), the skill step, QX1's ready line, and the test pinning all three word for word; QX3's `uncounted` hook line pinned the same way; Task 3.4's acceptance rows | n3f-b, n3f-d       |

n3f-b and n3f-c may be built at once once n3f-a has merged; n3f-d follows n3f-c; n3f-e closes.
Each part is reviewed to MERGE on its own, as N3's slices are. Gate A holds all of it inert as
SPEC-n3.md says: nothing here runs on the host before Task 3.3.

## 11. B 2.15's tests that fall to n3f

| B 2.15                               | Here                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| P1 no accepted item lost             | QS6, QS7, QS8 (recovery from the floor and the token); QX1 (a catch-up's items); QX5 (the key written only by folds, n3f-D19; a rebuild owed or under way persisted and resumed as a build, n3f-D21, tests (h) and (i)); QX3 (an own post not yet echoed); PT1's runs with questions items (n3f-c)                                                    |
| P3 position never ahead of its items | QS7 (the held answer), QS5 (the questions floor in the same write), QX1 and QX5 (the index and its token in the items' write)                                                                                                                                                                                                                         |
| P4 a removed team's items            | QS9's drop applies to a removal too: a removed team's questions items never reach the transcript (PT1)                                                                                                                                                                                                                                                |
| P11 handed is exclusive              | QS9 (handed questions items returned, then dropped, on a demotion)                                                                                                                                                                                                                                                                                    |
| T10 permanent failure                | QS8 (a side room untrusted alone, main unchanged), RP4                                                                                                                                                                                                                                                                                                |
| T11 replay and restore               | QS5 (per-room floors and markers), QX5 and RM1 across a restart; SY6's epoch change rebuilding the index; SY7's restore cases with a questions item                                                                                                                                                                                                   |
| T19 flood budget                     | QS6 (`rate` applies in questions, `stale` does not; attachment regardless)                                                                                                                                                                                                                                                                            |
| E1 `-tags tuwunel`                   | the restricted join of RP1; a human's post refused in reports; a worker's join and post refused in questions; QX1's backward-then-forward read; `/messages` on Tuwunel returning an admin-redacted event with `unsigned.redacted_because` (A 12.4), on which QX7's slots and every rebuild depend, read both backwards and forwards (Task 3.4, n3f-c) |
| A 14 Element (owner's steps)         | the reports composer hidden, a question's notification, a thread reply and its edit delivered: Task 3.4 with the owner, after gate A                                                                                                                                                                                                                  |
| OF1 (frame)                          | OF1-n3f's row                                                                                                                                                                                                                                                                                                                                         |

## 12. Amendments to the design

Each change n3f needs in the design documents, for the lead to apply exactly. "Old" is the
current text joined onto one line (in the file it wraps; only the words matter); "New" replaces
it whole. No design file is edited by this spec. All seven are applied to DESIGN-v2-A-protocol.md,
DESIGN-v2-B-member.md and SPEC-n3.md (SR2's sentence). One is adapted: 12.5's row 20 had already
gained n3d's `forge-down` and its two meanings, so `not-ready` and the widened meaning are added
to that row as it stands, and the row's action text keeps `unreachable` and `forge-down` as they
are.

### 12.1 A 13.3, the coordinator's role line (section 6.1, OW4)

Old:

```text
The role lines are the first clause of H12's `role_*` templates (`in team <t> you are the coordinator.`, `... you are a worker; the coordinator is <h>.`, `... you are a worker; the team has no coordinator now.`): how a role works is told at SessionStart (B), not in the notice, since v1's longer text names `pingbus` flags B's CLI may not have.
```

New:

```text
The role lines are the first clause of H12's `role_*` templates (`in team <t> you are the coordinator.`, `... you are a worker; the coordinator is <h>.`, `... you are a worker; the team has no coordinator now.`), and the coordinator's line has one more sentence: `in team <t> you are the coordinator. Human posts made in the questions room while the coordinator changed are not delivered to you; to see them, run teamchat questions --team <t>.` That sentence is in the notice because a promotion's notice is where a new coordinator learns it must look, and nothing else delivers those posts (B's questions room, SPEC-n3f OW4); it names only the `teamchat questions` verb, which B's CLI has. Otherwise how a role works is told at SessionStart (B), not in the notice, since v1's longer text names `pingbus` flags B's CLI may not have.
```

### 12.2 A 6.5, the numbering (QX7, n3f-D20)

Old:

```text
For numbering, counting starts from the roster's `question_floor` (5.1) rather than 0: an agent's question counts only when its `n` is exactly 1 plus the highest counted so far (at first, `question_floor`), in stream order; any other `n` from an agent is ignored for numbering, so one bad post (a client bypassed, 6.9) cannot push the numbering past the `Q` grammar for ever.
```

New:

```text
For numbering, counting starts from the roster's `question_floor` (5.1) rather than 0. A redacted slot is an event in questions from an agent account or the admin that the server returns redacted by the admin (`unsigned.redacted_because`, 12.4); the index counts the slots since the highest counted was last raised. An agent's question counts only when its `n` is from 1 plus the highest counted so far (at first, `question_floor`) to that number plus the slots, in stream order; each number a counted question skips is held by a redacted event, a closed question with no text, and the slot count starts again at 0. Any other `n` from an agent is ignored for numbering, so one bad post (a client bypassed, 6.9) cannot push the numbering past the `Q` grammar for ever, and a question the server returns redacted never breaks the chain. The index's next number is 1 plus the highest counted plus the slots since it, so no number is given out twice, a redacted question's included. An admin redaction in questions removes its target from the index entirely: the index is always the one a fresh read of the room gives.
```

### 12.3 A 10.2, the `n` bullet (QX7, n3f-D20)

Old:

```text
- `n` is 1 plus the larger of the roster's `question_floor` (5.1) and the highest counted `n` of the question index (6.5), so a replacement coordinator numbers the same way with no session state (00211).
```

New:

```text
- `n` is the question index's next number (6.5): 1 plus the larger of the roster's `question_floor` (5.1) and the highest counted `n`, plus the redacted slots since that highest was raised, so a replacement coordinator numbers the same way with no session state (00211), and a number held by a redacted question is never given out again.
```

### 12.4 B 2.2.1, the `ask` row (n3f-D14, n3f-D10)

Old (the row's second cell):

```text
the coordinator asks the team's humans a numbered question in the questions room; text on stdin. `--asked-by` takes members' short names or handles, resolved as `--to` is (64 `unknown-member` or `ambiguous-member`); each `--re` must be an event of this team's main room (else 10 `re`). Prints one `question` record
```

New:

```text
the coordinator asks the team's humans a numbered question in the questions room; text on stdin. `--asked-by` takes members' short names or handles, resolved as `--to` is (64 `unknown-member` or `ambiguous-member`); each `--re` is a main-room event id checked for grammar only, as A 10.2 says, with no lookup (else 64 `usage`). Prints a `sent` record, then the `question` record
```

### 12.5 B 2.2.4, rows 10 and 20 (G7, n3f-D13)

Row 10's codes. Old: `` `size`, `schema`, `secret`, `target`, `verb`, `ref`, `re`, `body`, `empty` ``. New:
`` `size`, `schema`, `secret`, `target`, `verb`, `ref`, `re`, `body`, `empty`, `open-limit` ``.

Row 20. Old:

```text
| 20   | the homeserver did not answer | `unreachable` | wait | tell a human if it persists; the link keeps retrying on its own |
```

New:

```text
| 20   | the homeserver did not answer, or a room or index the verb needs is not ready yet | `unreachable`, `not-ready` | wait | `not-ready` comes ready by itself: try again after your next item, or when a teamchat line says it is ready; tell a human if it persists; the link keeps retrying on its own |
```

`member/exits`'s table and n3e-11's pinned exit-class text follow the row in n3f-a (G7).

### 12.6 B 2.3.2, the seat store (G2, QX6)

Seven keys, where each sits. In the schema's `"outbox"` object, after `"gate"`:

```text
"reports": [1760000000000],
"questions_unechoed": null,
"questions_uncounted": {"count": 0, "said": 0},
"questions_handover": false
```

At the document's top level, after `"outbox"` and before `"run"`:

```text
"questions": {"index": <the index's state, QX5>, "token": "<token>", "last": "$<43>", "build": null},
"reminded_ms": 0,
"remind_late": []
```

Then one bullet after the `gate` bullet:

```text
- `outbox.reports` is the report times of the last 3600 s, for the hourly report limit (SPEC-n3f n3f-D6). `outbox.questions_unechoed` is the event id of the coordinator's last questions post that is posted and not yet folded, or null; `outbox.questions_uncounted` counts the own questions posts this term whose echo was not counted and the hook lines said about them; `outbox.questions_handover` is true while the line that a promotion's questions index is ready is owed (SPEC-n3f QX3, QX1). `questions` is the room's question index: its bounded state, its `/messages` token, its last folded event and an owed or running rebuild (SPEC-n3f QX5); it is the room's fold, the same on every seat. `reminded_ms` and `remind_late` are this seat's reminder watermark and the open questions found below it (SPEC-n3f RM1). All seven are seat state of the coordinator's term, dropped at a demotion.
```

Caps. Old: the cap sentence below, or n3d's form of it if n3d's amendment (n3d-D1, 3 MiB) has
been applied first; either is replaced whole:

```text
The file cap is their sum, about 1.9 MiB, rounded up to **2 MiB**, so a list cap is always the one that fires.
```

New, complete on its own (it keeps n3d's base caps, about 3.0 MiB, and adds this room's):

```text
The lists above, with n3d's caps (about 3 001 136 bytes), are the base. The questions room adds its own: 60 report times (14 bytes), 64 `remind_late` numbers (20 bytes), the `questions` key at most 1 101 056 bytes (64 questions not closed, 128 closed, 1024 attachments, 64 unattached posts; SPEC-n3f QX6), and the three `outbox.questions_*` keys with `reminded_ms`, 256 bytes together. The file cap is the sum of the base and these, about 4 104 600 bytes, rounded up to the next 256 KiB: **4 MiB** at these caps (`MaxStore`, pinned by `TestStoreFullFitsTheFileCap`), so a list cap is always the one that fires.
```

And in 2.14's row "store writes per event": old `2 MiB at worst`, new `4 MiB at worst`.

### 12.7 B 2.6 step 10, the `human_text` sentence (n3f-D2)

Old:

```text
A coordinator whose roster entry has `human_text` false (A 5.1, read from the verified roster, never from the bundle) leaves the questions invite pending, and `status` and the SessionStart frame say why (C1).
```

New:

```text
A coordinator's roster entry never has `human_text` false: A 5.1 makes such a roster invalid, so main fails trust (22) and the questions invite is never reached (SPEC-n3f n3f-D2, QS3).
```

SPEC-n3 SR2's "its invite left pending while `human_text` is false (said in `status` and the
frame);" is v1's state for the same reason, and is deleted.
