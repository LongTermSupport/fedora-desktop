# SPEC: N3 slice n3f, the side rooms (Plan 00209 Task 3.1, addendum to SPEC-n3.md)

The addendum SPEC-n3.md section 7 asks for. It specifies the reports room and the questions room
of Plan 00211 for v2's member: the verbs `report`, `ask`, `thread`, `questions`, `history --room questions` and `reply` on a questions-room item, the link's joins of both rooms, the questions
room's sync, the question index and its reminders. Sources: A 4.1-4.3, 5.1, 5.3, 6.3-6.5, 6.10,
7.1, 7.5, 7.6 rules 03 and 05, 10.1-10.6, 11.1-11.2, 13.2; B 2.2.1-2.2.4, 2.3.2, 2.6 step 10;
Plan 00211's SPEC.md and PLAN.md, read for what the rooms are for, not as a model.

Every rule of SPEC-n3.md section 1 holds here (TB4, CK1, OF1 above all), as do its test rules:
each expectation gets a Go test written first and seen to fail, on fakehs with the injected
clock, `-race`, 90% coverage per package. Expectations are numbered by area: RP (reports), QS
(the questions room's sync), QX (the question index), AK (`ask`), TH (`thread`), QL
(`questions`), QR (`reply` and `history` in questions), RM (reminders), OF (the offline rule).
Decisions are n3f-D1 onward (section 8); departures from v1 are n3f-X1 onward (section 7);
points left to the owner are in section 9.

## 0. What n3f builds on, and the seams it uses

| Seam                        | Package                         | What n3f does with it                                                                                                                                     |
| --------------------------- | ------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| the composers and forms     | `proto` (`send.go`, `forms.go`) | `Composer.Report`, `Question`, `Thread`, `Admit`; `ReportForm`, `QuestionForm`, `ThreadBody`: built and tested in N1, used as they are                    |
| the question index          | `proto` (`questions.go`)        | `QuestionIndex` with `Feed`, `Attach`, `OpenDuplicate`, `Next`, `Reminders`; gains what gap G1 names                                                      |
| side-room trust and invites | `proto` (`verify.go`)           | `VerifySide` for each side room; `Invite` for the questions invite                                                                                        |
| the filter                  | `proto` (`filter.go`)           | `SyncFilter` already names questions for the non-parked coordinator; n3f rebuilds the long-poll on a role change                                          |
| the envelope                | `proto` (`envelope.go`)         | `Entry.Questions`, `Entry.Question`, `Entry.Revises`, the reminder entry: built in N1; n3f fills them                                                     |
| the team goroutine          | `member/teamsync`               | `sess.take`'s `proto.Index` case (today a no-op naming n3f), the room of each pending item (today always main), the joins, the index's owner              |
| the state goroutine         | `member/engine`                 | the store's new keys (gap G2), the `questions` and `history` answers, the send path n3d builds                                                            |
| the store                   | `member/state`                  | `store.json` gains `outbox.reports` and `reminded` (n3f-D6, n3f-D11)                                                                                      |
| records                     | `member/records`                | the `question` and `unattached` record kinds, the `human` record's room fields (section 6)                                                                |
| exits                       | `member/exits`                  | 14 `report-rate`, 22 `untrusted` naming a side room, 23 `not-found` for a question, 22 `incomplete` (n3f-D9)                                              |
| the CLI table               | `member/cli` (`verbs.go`)       | the four verbs are already in the table; n3f removes them from `run.go`'s `unbuilt` list (CL2), which is then empty                                       |
| the sends                   | n3d's send gate and `sending`   | every post here goes through SD3: the token charged and the transaction id recorded in one store write before the network call; three tries in 30 s (M22) |

### 0.1 Gaps in the built APIs

- **G1. The question index** (`proto.QuestionIndex`):
  1. `Feed` is not idempotent by event id: an own post fed from the send's answer (QX3) and again
     when the sync brings it would reset `Unanswered` past a human post that arrived between
     them. The index keeps the ids it has fed and ignores a second one.
  2. It does not track a reply to an unattached human post (a `teamchat.thread` without `n`):
     QL1 lists unattached posts with no reply, so the index records, per unattached human post,
     whether an agent post replies to it.
  3. `IndexedQuestion` lacks `asked_by` and `re`, which the `question` record carries (section 6):
     `count` keeps them from the content.
  4. Nothing reads the index's completeness: `Complete()` is false until the history read of
     QX1 reached the room's `m.room.create` (n3f-D9).
- **G2. The store** (`member/state`): `outbox.reports` (n3f-D6) and `reminded` (n3f-D11), each
  with its cap in B 2.3.2's caps list and its strict decoding; `Pending.Room` taking
  `questions` for HUMAN and TIMEOUT items (the decoder already allows the three purposes).
- **G3. The team goroutine** puts every pending item in `proto.PurposeMain`
  (`session.go` `take`): n3f sets the verdict's room.
- **G4. `records`** has no `question` or `unattached` kind and no `headline` on `human`.

## 1. The reports room (RP)

- **RP1. The join.** Once main verifies at a team's start, the team goroutine joins reports
  (`POST /join/<reports id>`, the id the verified roster names) by the restricted rule (A 4.3,
  6.3), once per link run; a reports invite is refused, as A 6.3 says. The join never blocks the
  team: it runs after the first long-poll is sent, on the team's client, under a 30 s deadline.
  A transient failure is retried with SY8's backoff on its own timer, the team going on; a 403
  or any other refusal records the room `refused` with its errcode for `status` (the `room`
  record, section 6) and is tried again at the next start, the next roster change and the next
  `report`. Already joined (the membership in the joined rooms, or `M_FORBIDDEN` naming a
  member already in) is success. Test: a member whose reports join is refused delivers main
  items unchanged and its `status` shows `refused`; a join that fails transiently twice then
  succeeds shows `joined`.
- **RP2. Nobody syncs it.** The reports room is never in a filter (A 6.10); a reports event
  that reaches a judge anyway is A 7.6 rule 05's `fault`, logged once per event with its id and
  never delivered. Test: a filter for every role names no reports room; a forged sync answer
  holding a reports event is a fault line and no item.
- **RP3. `report [--team T]`**: stdin is the text, read to at most 1501 bytes (one past the
  cap, so over the cap is seen without reading on); the whole is the report's `body`, the first
  line its headline (A 10.1). One trailing newline is removed before the form is judged, so
  `echo`, a heredoc and `printf` give the same report (n3f-D1); nothing else is trimmed or
  changed. Then, in this order, the first failure deciding:
  1. CL6's team rule (64 `ambiguous-team`);
  2. the sender's class from the verified roster: a parked or removed member 10 `schema`
     (A's `sender` is refused as `schema`, B 2.2.4's set), the probe 11 `role`;
  3. `ReportForm`: over 1500 bytes, an empty first line or a headline over 120 code points,
     10 `body`, naming which;
  4. the secret check (the twelve shapes and the member's own token): 10 `secret`;
  5. the reports room verified now (RP4);
  6. `Admit`: `report_per_hour` reports already in the last 3600 s, 14 `report-rate`, the
     message naming when the oldest leaves the window; the same text within 60 s, 15
     `duplicate`; the send bucket empty, 14 `rate`;
  7. SD3's send, to the reports room: the token, the transaction id and the report's time
     (n3f-D6) in one store write before the PUT.
     On the answer it prints one `sent` record, `{"kind":"sent","v":1,"team":..,"id":..,"room":"reports"}`,
     and exits 0. Output: nothing but that record on stdout (CL3).
- **RP4. Verified at each send** (B 2.6 step 10, A 6.5's one-shot rule): `report` reads the
  reports room's state with one `GET /rooms/{reports}/state` and judges it by `VerifySide`
  against the main room's verified roster and anchors. Not joined: RP1's join is tried first, in
  the same request, under its deadline. A failed check is 22 `untrusted`, the message naming the
  reports room and the check (create, join rule, power levels, roster, link), and main is left
  as it was: its items are delivered and its sends go on. A 403 on the state read or the PUT
  with the room still verifying is 22 `forbidden`; unreachable, a timeout, a 5xx or a 429 past
  its retries is 20 `unreachable`, the token already spent (SD3). Tests: each failed check;
  main's next item delivered after a reports failure; a homeserver down during `report`.
- **RP5. No one is woken.** The content is A 10.1's exactly: `m.notice`, `m.mentions` `{}`, the
  `teamchat.report` object, no relation. A worker's report does not copy the coordinator (P21 is
  for text to humans). Test: the PUT's content byte for byte; no member's store gains an item
  after a report (a fakehs team of two, the other member's sync answered).
- **RP6. The deadline** is a send's (30 s, LN2), the reports state read and the join inside it.

## 2. The questions room's sync (QS)

- **QS1. The invite** (A 6.3): accepted only when main is verified, the roster names the room
  under `questions`, the inviter and the stripped create sender are the admin, and the roster
  names this member the non-parked coordinator; then the team goroutine joins it. An admin
  invite to a room the verified roster does not name yet, or any admin invite before main
  verifies, is left pending (logged once by room id, never joined, never rejected) and shown in
  `status` as `invited`; every other invite is rejected (`leave`) and logged by room id. Tests:
  each row of `proto.Invite`'s table through the team goroutine; a roster write naming the room
  after the invite arrived joins it at the next roster change.
- **QS2. A coordinator with `human_text: false` cannot happen**: A 5.1 makes such a roster
  invalid, so main fails trust (SY14, 22) and nothing of questions is reached (n3f-D2). Test:
  that roster stops the team with 22 and no questions join is attempted.
- **QS3. The filter.** While this member is the roster's non-parked coordinator and the
  questions room is joined and verified, the long-poll's filter is `proto.SyncFilter`'s
  coordinator form (main and questions); otherwise the worker form. A roster change that
  changes this member's role or parked flag ends the long-poll in flight and starts the next
  with the new filter (B 2.6 step 10). Test: promotion and demotion each change the next
  request's filter, pinned byte-equal to proto's.
- **QS4. The questions room's start.** The room is verified by `VerifySide` before any of its
  events is judged (A 6.5: at start, on join, on a limited timeline, on a trust-type event),
  then the index is built (QX1), and only then is its first batch judged. Its items resume as
  SY1 says per room: from the store's `floor_ms.questions` (a store), or from the earlier of its
  `teamchat.floor` marker and `released_ms`, its own join event when it has neither (no store);
  a first join therefore delivers nothing posted before the join, and `questions` shows it
  (n3f-X4). The marker is SY10's, per room.
- **QS5. The receive path** (A 7.6 rule 05): for the coordinator every human message in
  questions is addressed (no mention needed), kind HUMAN, judged by `stale` never and by `rate`
  as main's are; each is attached by `QuestionIndex.Attach` (A 10.4) before it is persisted, and
  its pending entry has `room: questions`. Another agent's event is dropped `sender`; an agent's
  `teamchat.question` or `teamchat.thread` is rule 03's `index` outcome and feeds the index
  (QX2). A human's edit in questions is an item with `revises` (A 7.5), attached by rule 4.
  Tests: an unmentioned human post delivered with its question; a post in a question's thread, a
  reply to a thread post, a `Q3:` prefix, an edit of an attached post, an unattached post
  (`question: none`); a worker's post dropped `sender`; a human post older than
  `human_max_age_s` still delivered.
- **QS6. A limited timeline** in questions is filled as SY3 fills main's (one sync token
  covers both rooms, so the fill is never contained: a transient failure holds the whole answer
  and persists nothing). Past SY3's page bound, main moves on with its `gap:` line, but the index
  cannot skip events (its numbering is a chain, A 6.5): it is rebuilt by QX1's read before the
  batch's questions events are judged (n3f-D9). Test: a gap of 2100 questions-room events, one
  question in it: the index counts it and the next `ask` takes the next number.
- **QS7. A side-room failure is that room's alone** (A 6.5): a questions room that fails
  `VerifySide`, or whose state or history read is refused (403, 404), is untrusted alone: the
  filter drops it (QS3's worker form), its pending and handed items stay (they passed when they
  were judged), `ask`, `thread`, `questions`, `history --room questions` and `reply` on a
  questions item exit 22 `untrusted` naming the room and the check, `status` shows `untrusted`
  with the check, the SessionStart frame and one hook line say it once, and main goes on. It is
  checked again at the next start, the next roster change, and the next questions verb, which
  re-verifies before it refuses. On recovery its items resume from its floor (QS4), so nothing
  posted while it was untrusted is lost. Transient failures are SY8's (n3f-D8). Tests: each
  failed check, with main's items delivered meanwhile; recovery delivering the posts made while
  untrusted.
- **QS8. Demotion** (B 2.6 step 10): when the roster no longer names this member the non-parked
  coordinator, the team goroutine, before it judges anything else: rebuilds the filter (QS3),
  drops the questions room's `pending` items and returns and drops its `handed` ones in one
  store write (each logged by id, never by body), drops the index, and refuses every questions
  verb with 11 `role`. `awaiting` entries already written to the socket settle as any other
  (DV4); a `sending` questions post is dropped with a log line naming its transaction id (it may
  have landed; the room is the record). Reminders owed are not made. The questions floor and
  marker stay, so a later promotion resumes from them. Tests: a demotion with items pending,
  handed and awaiting; an `ask` racing the demotion's roster read (refused or posted, never
  posted after the store write that dropped the room).
- **QS9. Promotion**: the invite (QS1), the join, QS4's start. A member promoted while its
  questions floor survives in the store resumes from it; otherwise from its join (QS4).

## 3. The question index (QX)

- **QX1. The build.** The coordinator's team goroutine reads the questions room's whole history
  with `/messages` backwards (`dir=b`, 100 per page) from the position its first questions batch
  starts at, until the room's `m.room.create`, and feeds every event to the index in stream
  order (rule 03's `index` outcomes only; nothing read here is delivered). At most 100 pages
  (n3f-D9); past them, the index is `incomplete`: `ask` and `thread` exit 22 `incomplete`,
  `questions` prints what it has and a `fact` record `incomplete` (section 6), delivery of human
  posts goes on with `question: none` for any post the index cannot attach, and `status` says it.
  The read is the team's own traffic under the team's backoff (SY8). Tests: a room of 250 events
  built in three pages; 101 pages: `incomplete`, `ask` refused, a human post still delivered.
- **QX2. Fed by the sync**: each counted question and each thread post of a counted question,
  from any agent account (this member's own included) or a question from the admin, as A 6.5's
  exception says; an event fed twice is fed once (G1.1).
- **QX3. Fed by the send**: the coordinator's own `ask` or `thread`, on its PUT's answer, is fed
  with its event id and `origin_server_ts` as the server will return it (taken from the answer's
  event id and the sync that echoes it; until the echo, the send's time), so the next `ask`
  numbers above it without waiting for the sync.
- **QX4. One questions send at a time per team** (n3f-D5): `ask` and `thread` (and `reply` on a
  questions item) are serialised in the link per team, each composed against the index as it
  stands after the previous one's QX3. Test: two `ask`s sent at once number Q4 and Q5, never two
  Q4.
- **QX5. In memory only.** The index is rebuilt by QX1 at every link start and every promotion;
  the store holds none of it (A: "no session state").

## 4. The coordinator's verbs (AK, TH, QL, QR)

- **AK1. `ask [--team T] [--asked-by N,..] [--re ID,..] [--remind-after S]`**, text on stdin,
  read to at most 8001 bytes (one past A 10.2's cap less its `Q<n> ` prefix is caught by the
  form). One trailing newline is removed (n3f-D1). In this order, the first failure deciding:
  1. CL6 (64 `ambiguous-team`); `--remind-after` outside 600-604800 is 64 `usage` naming the
     range; `--asked-by` and `--re` each at most 16 distinct entries, else 64 `usage`;
  2. the class: not the roster's non-parked coordinator, 11 `role` (the message: ask the
     coordinator with `teamchat say --to-coordinator --team T`);
  3. the questions room joined and verified, else 22 `untrusted` naming it (QS7); the index
     complete, else 22 `incomplete` (QX1);
  4. `--asked-by`: each name resolved as n3d's `--to` resolves it, against the verified roster,
     to a member's user id: 64 `unknown-member` or `ambiguous-member`; a parked member is
     accepted (n3f-D4); a human or the probe is 64 `unknown-member`;
  5. `--re`: each an event id (64 `usage` on its grammar), and an event of this team's main room:
     found in the ledger, the outbox, the own sends or the cache, else by one `GetEvent` on main
     (a 404 or 403 is 10 `re`, naming the id; transient 20);
  6. `Composer.Question`: the form (10 `body`, naming the section), the secret check (10
     `secret`), an open or answered question with the same `Question:` section (15 `duplicate`
     is B 2.2.4's duplicate code, so A's `duplicate` refusal maps to 15, naming that `Q<n>`);
  7. `Admit` (14 `rate`; 15 `duplicate` for the same body within 60 s);
  8. SD3's send to the questions room, QX3, and one `question` record (section 6) with `state`
     `open` and `unanswered` `[]`. Exit 0.
     Tests: each step's refusal; numbering after a `question_floor`, after an admin's carried
     v1 question, after an uncounted bad `n`; the PUT's content (`m.text`, every human
     mentioned, `asked_by`, `re`, `remind_after_s` only when given).
- **TH1. `thread Q<n> [--ask] [--close] [--team T]`**, text on stdin (at most 4001 bytes read,
  one trailing newline removed). In this order:
  1. CL6; `--ask` with `--close` is 64 `usage` (n3f-D3);
  2. the class (11 `role`), the room (22), the index complete (22 `incomplete`);
  3. `Q<n>` not a counted question: 23 `not-found`, naming it; `--close` on a question already
     closed: 10 `target` naming it ("already closed; post without --close") (n3f-D3);
  4. `Composer.Thread` with `N`, no `Answers` (a follow-up names no human post: QR1 does):
     the fallback reply is the thread's latest event (A 10.3); the body 10 `body` (blank and not
     closing, over 4000 bytes); 10 `secret`;
  5. `Admit` (14 `rate`, 15 `duplicate`);
  6. the send, QX3, a `sent` record with `room` `questions`, then the question's `question`
     record as it stands after the post. Exit 0.
     `--ask` sends `m.text` mentioning every human, otherwise `m.notice` with no mentions;
     `--close` with no text sends the body `Q<n> closed.` Tests: each refusal; a follow-up's
     fallback naming the latest human post; `--close` changing the record's state to `closed`.
- **QL1. `questions [--all] [--team T]`** (B 2.2.3's multi-team verb, 60 s deadline): over each
  active team where this member is the non-parked coordinator (n3f-D7), from the index, no
  network call but QS7's re-verification when the room is untrusted:
  - one `question` record per counted question, by `n`: without `--all` those `open` or
    `answered`, with `--all` every one;
  - one `unattached` record per unattached human post that no agent post replies to (G1.2), by
    time (n3f-X3);
  - nothing at all: no records, exit 0.
    With `--team T` and this member not T's coordinator: 11 `role`. Without `--team` and no team
    where it is coordinator: 11 `role`, naming the teams. A team that fails (22, 20) is a
    `team-failed` record, the verb exits 5 (B 2.2.3); every team failing exits with the first's
    error. Tests: each state; P58's unanswered count (human posts after the coordinator's last
    post in the thread, edits included); a handover: a new coordinator lists its predecessor's
    open questions and their unanswered posts; an unattached post listed until it gets a reply.
- **QR1. `reply <id>` on a questions-room item** (extends n3d's SD1, which decides room and
  thread from the ledger): attached to `Q<n>`, `Composer.Thread{N: n, Answers: id}` (the thread
  form with `is_falling_back` false, A 10.3); unattached, `Composer.Thread{Answers: id}` (a plain
  reply, no `n`, closing nothing). `--verb` on a questions item is 64 `usage` (no ping to answer);
  the class, the room and the index as TH1 steps 2; then TH1 steps 4-6, printing `sent` and, when
  attached, the `question` record. An id the ledger does not know is looked up on the
  homeserver in main, then in questions (23 when neither has it). Tests: each form's content; a
  reply to an unattached post leaving `questions` with no `unattached` record for it.
- **QR2. `history --room questions [--team T] [--limit N]`**: the coordinator only (11 `role`
  otherwise, before any request), the room verified (22), then as LN6's `history`: `/messages`
  backwards, newest first, `--limit` as n3c-D8, consuming nothing. Each human post is judged on
  QS5's path and shown as an `in` `history` record whose item carries `room` `questions` and the
  attachment the index gives it; each agent post in the room (this member's and earlier
  coordinators') as an `out` record with its question. Tests: a thread reply and its edit shown
  attached; a worker refused 11 without a request made.

## 5. Reminders (RM) and the offline rule (OF)

- **RM1. The reminder** (A 10.5): for each counted question with `remind_after_s` that is still
  `open` at its `origin_server_ts` plus that bound plus 30 s, one TIMEOUT item for the
  coordinator, id the question's event id, `room` `questions`, kind timeout, made by the team
  goroutine from `QuestionIndex.Reminders` on a timer set to the earliest due (never a tick, as
  SD4), and persisted with the question's id in `reminded` (n3f-D11) in the same store write that
  puts it in `pending`. A question in `reminded` is not reminded again by this seat. Its
  envelope entry is A 13.2's reminder form and its record a `timeout` with `"silent":"humans"`,
  `"question":"Q<n>"` and no `verb` or `ref`. Tests: due exactly at the bound plus 30 s and not a
  millisecond before (fake clock); answered first, no reminder; a link restart after the
  reminder was made, no second one; a replacement coordinator (another seat's store) reminded
  once.
- **RM2. HUMAN and TIMEOUT room**: a reminder takes `pending`'s reserved share (B 2.3.2), so a
  flood of agent items never holds it back.
- **OF1-n3f. Offline members never block** (SPEC-n3 OF1, the owner's rule): no n3f verb, join,
  read or reminder waits for, refuses for, or fails because another member is offline. In
  particular: `ask --asked-by` names a stopped member as any other (its being offline is not
  looked at); `report` and `ask` post to rooms, never to a member, so nothing in them targets a
  member and none of them exits 69 for one; 69 is only the own session's (LN3: `no-session`,
  `no-link`, `link-failed`), which is the member reporting itself. A coordinator that is offline
  blocks no human and no worker: human posts wait in the room and are delivered from its floor
  when it runs (QS4); reminders are computed from history and come once when it runs (RM1).
  Test (each area's row): a team with one worker stopped, its container gone, while the
  coordinator asks naming it in `--asked-by`, threads, lists and is reminded, and the other
  worker reports: every verb exits 0 and its output is the same as with all members running.

## 6. Records (B 2.2.2, settled for n3f)

```json
{"kind":"question","v":1,"team":"<team>","n":4,"state":"open","id":"$<43>","headline":"Which licence for the new docs site?","asked_by":["<repo>.2"],"re":["$<43>"],"asked_ms":1760000000000,"remind_after_s":null,"unanswered":["$<43>"]}
{"kind":"unattached","v":1,"team":"<team>","id":"$<43>","from":"alice","sent_ms":1760000000000}
{"kind":"human","v":1,"team":"<team>","id":"$<43>","from":"alice","from_id":"@alice:<server>","sent_ms":1760000000000,"room":"questions","text":"MIT, please.","question":"Q4","headline":"Which licence for the new docs site?","revises":null,"restored":false}
{"kind":"timeout","v":1,"team":"<team>","id":"$<43>","silent":"humans","question":"Q4","verb":null,"ref":null}
{"kind":"room","v":1,"team":"<team>","purpose":"questions","state":"joined","check":null}
{"kind":"fact","v":1,"team":"<team>","fact":"incomplete","from":null,"count":null,"id":null}
```

- `question`: `state` one of `open`, `answered`, `closed` (A 10.4); `headline` the asking
  coordinator's first line as a JSON string; `asked_by` the askers' `from` forms (P69); `re` the
  main-room ids; `asked_ms` the question's `origin_server_ts`; `remind_after_s` its value or
  `null`; `unanswered` the ids of P58's unanswered posts, oldest first.
- `unattached`: one human post in questions attached to no question and replied to by no agent.
- `human` gains `headline` (the attached question's, else `null`); `question` is `null` in main,
  `"Q<n>"` when attached, `"none"` when unattached; `room` `main` or `questions`.
- `timeout` for a reminder: `silent` `humans`, `question` `Q<n>`; for a ping's TIMEOUT `question`
  is `null`.
- `room` (in `status`, LN5): `purpose` `reports` or `questions`; `state` one of `joined`,
  `invited` (QS1's pending invite), `refused` (RP1), `untrusted` (QS7, RP4, with `check` naming
  the failed check), `not-coordinator` (questions, for a worker), `incomplete` (QX1).
- `fact` gains `incomplete` (QX1).

The skill (PK2) and its text test cover these kinds, fields and codes in the same commit that
adds them.

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
  on join and relied on the coordinator running `questions`; v2 does the same for delivery (QS4)
  but builds the whole index from history first, so numbering, states and unanswered posts are
  right from the first verb, and the reminders of open questions come once.
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

| ID      | Decision                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n3f-D1  | **The headline and body split** (SR1): the text on stdin is used whole as the `body`, its first line the headline (A 10.1, 10.2), after removing exactly one trailing newline. Why: every way a shell passes text (`echo`, a heredoc, `printf '%s\n'`) ends in one newline the writer did not mean as content, and removing more would change text the agent wrote; A's form trims the headline itself.                                                                                                                                        |
| n3f-D2  | **The `human_text` pending invite is not built** (SR2): A 5.1 makes a coordinator with `human_text: false` an invalid roster, so main fails trust and the questions invite is never reached. B 2.6 step 10's bullet and SPEC-n3 SR2's "left pending while `human_text` is false" describe v1's state, which v2's roster rule removed; QS2 tests the roster failure instead. Why: a second path for an impossible state is dead code.                                                                                                           |
| n3f-D3  | **`thread --ask --close` is 64; `--close` on a closed question is 10 `target`.** Why: a post that asks the humans again and closes the question in one message tells them two opposite things; a second close is a mistake to fix, and refusing it says so, where succeeding silently would hide a lost text.                                                                                                                                                                                                                                  |
| n3f-D4  | **`--asked-by` accepts any roster member, parked or offline**, and refuses humans and the probe. Why: `asked_by` records who asked, so the coordinator knows whom to answer; refusing an asker who has since stopped or been parked would block the question for a fact about that member (OF1).                                                                                                                                                                                                                                               |
| n3f-D5  | **Questions-room sends are serialised per team in the link** (QX4), each composed against the index after the previous one's answer was fed to it. Why: the index's `Next` is the only numbering, and two concurrent `ask`s would both take it; the second would then be uncounted by every member (A 6.5).                                                                                                                                                                                                                                    |
| n3f-D6  | **`report_per_hour` is kept in the store**: `outbox.reports`, a list of at most 60 report times (the limit's upper bound), pruned past 3600 s, written in SD3's store write. Why: an in-memory window resets at every link restart, so a crash-looping or restarted link could post past the limit; B 2.3.2 had no home for it.                                                                                                                                                                                                                |
| n3f-D7  | **`questions` and `history --room questions` without `--team` act over the teams where this member is coordinator**, and refuse 11 only when there is none. Why: B 2.2.3 runs a multi-team verb over every active team, which would make every coordinator in a session that is also a worker elsewhere get exit 5 with a `team-failed` it can do nothing about.                                                                                                                                                                               |
| n3f-D8  | **A side room's transient failures are the team's** (SY8's backoff, the answer held): one sync token covers main and questions, so a questions fill or read that fails transiently holds the whole answer, as v1 learnt (its gap fill was never contained). A non-transient failure is the side room's alone (QS7).                                                                                                                                                                                                                            |
| n3f-D9  | **The index's history read is bounded at 100 pages** (10 000 events), and past it the index is `incomplete`: `ask` and `thread` exit 22 with the new code `incomplete`, delivery goes on with `question: none`. Why: the numbering is a chain from `question_floor` and cannot be read from a suffix, so a partial read must refuse to number rather than reuse a number; the questions room is low volume, and the owner's repair is the admin raising `question_floor` (C) over a room that large. B 2.2.4's 22 code set gains `incomplete`. |
| n3f-D10 | **`ask` prints only the `question` record; `thread` and `reply` in questions print `sent`, then the question's record when one is attached.** Why: B 2.2.1 has `ask` print one `question` record, whose `id` is the event id `sent` would give; `thread` changes a question's state, which the agent should see without a second verb.                                                                                                                                                                                                         |
| n3f-D11 | **Reminders made are kept in the store**: `reminded`, the question event ids this seat has reminded, at most 256; an id whose question is no longer `open` is dropped at the next write. A 257th open reminded question gets no reminder and `status` counts it. Why: A 10.5's "once" must survive a link restart; the store is the seat's one durable record.                                                                                                                                                                                 |
| n3f-D12 | **The reports join is eager, at the team's start** (RP1), and retried at a `report`. Why: C's board shows each member's reports membership (C 3.11.3's `rooms`), A 4.1 lists every active member in reports, and a join at the first `report` would make the board show a working member as not joined.                                                                                                                                                                                                                                        |
| n3f-D13 | **`ask` before the questions invite has arrived is 22 `untrusted`** ("not joined yet; run status"). Why: the roster names the room, so its absence is the admin's or the server's state, which only a human can repair if it persists; a "wait" code would make the agent loop on an invite that may never come.                                                                                                                                                                                                                               |

## 9. For the owner

Product behaviours with no clear best answer. The build follows the recommendation unless the
owner says otherwise; the lead asks.

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
- **OW3. The size bound of the questions history.** n3f-D9 refuses to number past 10 000 events
  in the questions room. Options: (a) the bound as stated, the repair being the admin raising
  `question_floor`; (b) a higher bound. **Recommendation: (a)**; the owner may prefer to be told
  the figure is ample before it is built.

## 10. The slicing

| Part  | Covers                                                                                                                                                 | Needs                         |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------- |
| n3f-a | G1 (proto's index), G2 (the store's keys and caps), G4 (records), the exits codes (`report-rate`'s use, `incomplete`), section 6's codecs              | n3d merged                    |
| n3f-b | the reports room: RP1-RP6, the `room` record for reports in `status`, OF1-n3f's `report` row                                                           | n3f-a                         |
| n3f-c | the questions room's sync: QS1-QS9, QX1-QX5, G3, RM1-RM2, the envelope's room lines filled (DV2's renderer, built), PT1's runs gaining questions items | n3f-a; n3e for RM1's envelope |
| n3f-d | the questions verbs: AK1, TH1, QL1, QR1, QR2, the rest of OF1-n3f; CL2's `unbuilt` list emptied                                                        | n3f-c                         |
| n3f-e | the skill's text for the side rooms and its text test (PK2), section 6 in the skill, Task 3.4's acceptance rows                                        | n3f-b, n3f-d                  |

n3f-b and n3f-c may be built at once once n3f-a has merged; n3f-d follows n3f-c; n3f-e closes.
Each part is reviewed to MERGE on its own, as N3's slices are. Gate A holds all of it inert as
SPEC-n3.md says: nothing here runs on the host before Task 3.3.

## 11. B 2.15's tests that fall to n3f

| B 2.15                               | Here                                                                                                                                 |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| P1 no accepted item lost             | QS5, QS6, QS7 (recovery from the floor); PT1's runs with questions items (n3f-c)                                                     |
| P3 position never ahead of its items | QS6 (the held answer), QS4 (the questions floor in the same write)                                                                   |
| P4 a removed team's items            | QS8's drop applies to a removal too: a removed team's questions items never reach the transcript (PT1)                               |
| P11 handed is exclusive              | QS8 (handed questions items returned, then dropped, on a demotion)                                                                   |
| T10 permanent failure                | QS7 (a side room untrusted alone, main unchanged), RP4                                                                               |
| T11 replay and restore               | QS4 (per-room floors and markers), RM1 across a restart; SY7's restore cases with a questions item                                   |
| T19 flood budget                     | QS5 (`rate` applies in questions, `stale` does not)                                                                                  |
| E1 `-tags tuwunel`                   | the restricted join of RP1; a human's post refused in reports; a worker's join and post refused in questions (Task 3.4)              |
| A 14 Element (owner's steps)         | the reports composer hidden, a question's notification, a thread reply and its edit delivered: Task 3.4 with the owner, after gate A |
| OF1 (frame)                          | OF1-n3f's row                                                                                                                        |
