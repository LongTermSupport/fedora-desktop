# Design: v2 questions verbs (backlog R0, with R1-R4, R10, R11)

The design, test first, of [BACKLOG-coordinator-retro.md](BACKLOG-coordinator-retro.md) R0: the
verbs a coordinator needs to ask the owner on v2. Sources: [SPEC-n3f.md](SPEC-n3f.md) sections
3-6 (which already specify `ask`, `thread`, `questions`, `reply` and `history` in questions),
A 10.1-10.6, B 2.2.1, Plan 00211's SPEC section 8 and `helpers/pingbus` as the v1 requirements
source, and issues #364, #365, #367 and #369. Claims are marked FACT (read in a file or issue),
INFERENCE or UNKNOWN.

R0 is mostly **finishing n3f** (SPEC-n3f section 10: n3f-c2b, n3f-c2c, n3f-d, n3f-e), plus the
deltas below that the retrospective and the four issues ask for. This file does not restate
SPEC-n3f; it names what changes in it.

## 1. What v2 has today, and what is missing

Has (FACT):

- The index, in proto: fold, attachment, caps, provisional entries, redacted slots, the codec
  (`src-go/internal/teamchat/proto/questions.go`, `questions_fold.go`, `questions_prov.go`,
  `questions_state.go`); caps at `questions.go:21-26` (64 open, 128 closed, 64 unattached).
- The composers: `Composer.Question` (`proto/send.go:211`) and `Composer.Thread`
  (`proto/send.go:247`), with the attached-reply form (`send.go:383-399`).
- The questions room's sync (n3f-c1): QS1-QS10, `Refetch`, `status`'s questions record; QX1's
  builder (n3f-c2a) in `member/teamsync/builder.go` and `teamsync/questions.go` (PLAN.md Task
  3.2; BUILT-n3.md, n3f-c1c to c2a).
- `verify`, main room only: `member/teamsync/verbs.go:142-169` fetches from `a.MainRoom` (:152)
  and sets `Room: proto.PurposeMain`.

Missing (FACT):

- The verbs: `member/cli/run.go:58-60` lists `ask`, `thread`, `questions` as unbuilt (slice n3f);
  no `close`, `retitle`, `bump` exist anywhere, nor `verify` in questions.
- QX3 (own sends placed and echoed), QX4 (one compose at a time) and QX7 (redaction rebuild):
  n3f-c2b and c2c (BUILT-n3.md, "Open, from the reports").
- The ready line, the skill text and the NOTICE text (n3f-e).
- #367's inheritance: `attachment` (`proto/questions_fold.go:317-339`) inherits a reply only
  from a human post **in a thread** (`e.T == postHumanThread`, :330) or an agent post with a
  number; a thread rooted at anything but a question root is not followed (:324).
- #364's delivery: A 10.6 says the coordinator relays each answer by hand, on purpose (A
  10.6 last paragraph). Workers are not in the questions room (A 4.1 table, :342), so a worker
  cannot read or verify an owner's answer.
- Agents never edit (A, the `edits` row, :857), so v1's retitle has no v2 form.

## 2. The verbs

All are the coordinator's (11 `role` otherwise), except `verify`. Common to every verb:
SPEC-n3f's steps for class, room and index (22 `untrusted`, 20 `unreachable` or `not-ready`,
14 `server-rate`), n3f-D1's stdin rule, the secret check (10 `secret`), `Admit` (14 `rate`,
15 `duplicate`), n3d's queue (`sent` with `queued`). Every size refusal names the body's size
and the limit: `the text is 4628 bytes, over the 4000-byte limit` (#365, R9; one helper in
`exits`, used by every verb here and by `say`).

**Who is notified** (the coordinator's requirement (1)). FACT, v1: a question and `thread --ask`
are `m.text` with `m.mentions.user_ids` = every declared human; a plain `thread` post is
`m.notice` with empty mentions (`helpers/pingbus/protocol.py:1582-1592`, :1508-1512). That is
why three of four v1 bumps were silent. INFERENCE, from the Matrix push rules: the server's
`.m.rule.is_user_mention` notifies and highlights a mentioned user, and `.m.rule.suppress_notices`
(an override rule earlier in the list) silences any `m.notice`, mention or not; edits are
silenced by `.m.rule.suppress_edits`. Lightning is a Matrix client and reads the server's
notification and highlight counts (UNKNOWN: whether it raises a desktop notification per thread;
an acceptance row records it, step 8). So in v2:

- **Notify** = `m.text` and `m.mentions.user_ids` naming the humans meant. Used by default by
  every post that waits on a human: `ask-human`, `bump`, `thread` (default), and the link-back.
- **Quiet** = `m.notice`, empty mentions. Used by `close`, `thread --note`, the retitle edit, and
  the digest (a report, A 10.1). Quiet must be asked for; notify is the default (v1's default is
  inverted).

### 2.1 `ask-human [--for SEAT,..] [--re ID,..] [--remind-after S] [--check] [--skeleton]`

SPEC-n3f AK1, with these changes. `--for` replaces AK1's `--asked-by` (same resolution, same
wire field `asked_by`, n3f-D4 holds); it also drives delivery (section 4).

- **Body** (A 10.2's form, P54) gains a required section `Why a human:` whose first word is one of
  `decision` (owner-only), `physical` (a physical act), `credential`, `preference`, then free
  text (R4). Missing or another word: 10 `body`, naming the section and the four words.
- `--skeleton` prints the body template (headline, `Context:`, `Question:`, `Why a human:`,
  `Options:`) and exits 0, no team needed.
- `--check` runs every step up to `Admit` and sends nothing: it prints the body as it would be
  posted, the `question` record with `n` null, and, once the seat duty work
  ([PROPOSAL-seat-kinds.md](PROPOSAL-seat-kinds.md)) has landed, one `hint` record per roster seat
  whose duty could do a `physical` request (a host seat). Exit 0, or the refusal it would give.
- Output, exit codes and events: AK1 step 7 (`sent`, then `question`). The `question` record
  gains `why`, `for` (the `asked_by` short names) and `waiting_since_ms`.
- **Protocol change (A 10.2):** `teamchat.question` gains `why` (one of the four words), required
  for a coordinator's question and absent from the admin's carried ones (A 7.7). FACT: no seat
  has written a question yet, so `v` stays 1.

### 2.2 `thread Q<n> [--note] [--team T]`

TH1, with `--ask` gone (notify is the default) and `--close` moved to `close`. `--note` sends a
quiet post. The thread form is A 10.3's. Refusals as TH1: 23 `not-found`, 10 `body`, 10 `secret`.
Prints `sent`, then the `question` record. A post is always in Q<n>'s thread (`rel_type`
`m.thread`, root the question), never top level.

### 2.3 `reply <id>` on a questions item (QR1, kept), and the link-back

For a human post attached to Q<n>, QR1 posts in Q<n>'s thread with `m.in_reply_to` the post and
`is_falling_back` false (FACT: `send.go:399`). It notifies the post's sender only (they asked).

**Link-back** (the coordinator's requirement (2)): when that human post is **not in Q<n>'s
thread** (top level, attached by a `Q<n>` prefix, by a reply, or by inheritance), `reply` also
posts one short top-level reply to the human post: `Q<n>: answered in its thread: <permalink to the thread post>`, `m.text`, mentioning that human. Why both: a human who asked at the top level
watches the top level (it happened today), and an in-thread `m.in_reply_to` to a top-level post
is shown inconsistently by clients (UNKNOWN for Lightning; INFERENCE for Element). The link-back
carries `teamchat.thread {"v":1,"n":<n>,"link":"<thread post id>"}` and no thread relation, so
the fold records it as an agent post of Q<n> (it does not reset the unanswered posts, does not
close), and a human reply to it attaches to Q<n> by section 3. **Protocol change (A 10.3):** the
`link` form. One send each, both under one `Admit`; the second is sent only after the first has
an event id (the permalink needs it; a queued first send queues both, in order).

An unattached post: QR1's plain reply, kept (it asks the human which question they meant).

### 2.4 `close Q<n> [Q<m>..] [--team T]`

Each named question: a quiet thread post (`Q<n> closed.`, or stdin's note when given) with
`teamchat.thread` `close: true` (A 10.3), then the retitle edit of 2.5 when this seat posted the
root. All names are checked before any send: one not kept is 23 `not-found`; one already closed
10 `target` (n3f-D3). Prints `sent` and `question` per question. A root another seat posted is
not edited (Matrix lets only the sender edit: FACT, 00211 SPEC 8.4); the record says
`retitled: false` and stderr says why.

### 2.5 `retitle Q<n> [--team T]`

The new headline on stdin (1-120 code points). Sends an `m.replace` edit of the question root,
quiet, whose `m.new_content` is the same `teamchat.question` and sections with the new headline.
A closed question's title is `Q<n> ✅ CLOSED — <headline>` (the owner's v1 form, 2026-11-17).
Refusals: 23 `not-found`; 10 `target` "Q<n> was asked by another seat; only its sender can edit
it"; 10 `body`. **Protocol change (A, the `edits` row and 10.4):** a coordinator may edit its own
counted question root, only with the same `n`, `asked_by`, `re`, `why` and `Question:` hash; the
fold takes the new headline from such an edit and drops any other agent edit `edit`, as today.

### 2.6 `questions [--all] [--waiting] [--for SEAT] [--digest] [--team T]` (R2)

QL1, plus:

- the index gains per question `waiting_since_ms`: set by a notifying agent post (the question,
  a default `thread`, a `bump`), cleared by an attached human post and by a close. **Waiting on a
  human** = not closed and `waiting_since_ms` set. `--waiting` lists only those, oldest first.
- `--for SEAT` lists the questions whose `asked_by` names that seat; records carry `for`.
- `--digest` posts one report to the reports room (A 10.1, quiet, `report_per_hour` applies):
  headline `Waiting on you: N questions`, then one line per waiting question `Q<n> (<age>, for <seats>): <headline>`, cut at 1500 bytes with `and K more`. The owner sees in one place what
  waits on them, without a notification. No protocol change.
- Store: two small fields per kept question (about 30 bytes x 192); `TestStoreFullFitsTheFileCap`
  re-pinned (INFERENCE: inside QX6's 59 307-byte headroom).

### 2.7 `bump Q<n> [Q<m>..] | --waiting [--note TEXT] [--min-age S] [--team T]` (R11)

For each question, a notifying thread post: `Q<n> still waiting: <headline>` and the note.
The headline is the index's (FACT: the `Question:` section is kept only as its SHA-256, G1.4,
so the full question text is not available without a fetch). `--waiting` picks every waiting
question of the team (the coordinator owns its predecessors' too). A named question not kept is
23; one not waiting is skipped with a `bump` record `{"q":"Q<n>","bumped":false,"why":"answered"}`;
one whose `waiting_since_ms` is younger than `--min-age` (default 3600 s) is skipped the same
way, so the Stop handler cannot spam the owner. Exit 0 when every name was judged; each post is
one `Admit`. Events: A 10.3 thread posts, no protocol change.

### 2.8 `verify <id> [--room main|questions]` (R10, #369)

`Team.verify` fetches from the named room; without `--room`, main, then questions on 404 when
this member is in questions. The `verified` record gains `human` (sender a declared team human),
`question` (`"Q<n>"`, `"none"`, or null when this member holds no index) and `headline`. A
worker holds no index: it reports the question only when the event's own relation names a
question root it can fetch (rules 1-2), else null. Exits as today: 23 `not-found`, 22
`untrusted`, 20, 14. It reads no secret. Needs worker read access (section 4) for a worker.

## 3. Attachment rules that fix #367

A 10.4, rewritten. The first that holds:

1. Its thread root is **any event attached to Q<n>**: the question, an agent post of Q<n> (a
   thread post or a link-back), or a human post attached to Q<n>.
2. Its `m.in_reply_to` names any event attached to Q<n> (same set; human posts at the top level
   included, which `:330` excludes today).
3. Not an edit, body starts `Q<n>` then a space, `:` or `.`.
4. An edit: the original's attachment, else its new body's `Q<n>`.
5. Otherwise unattached.

So attachment is inherited along `m.in_reply_to` and thread roots through the index's two lists
(`posts`, `agent_posts`), never cut by a top-level hop. The kind bit `t` stays, for the link-back
rule. Bounded as today: an event older than the lists keep is not inherited (QX6). An agent's
answer to an attached post is always in Q<n>'s thread (2.3); to an unattached one, a plain reply.
Live equals rebuilt holds: every input is in the room.

## 4. Delivery that fixes #364

The answer must reach the asking seat and each `--for` seat, and a worker must be able to check
it itself (#364: a worker must not act on a peer's relay of an owner ruling).

- **Read access (protocol change, A 4.1-4.2 and C).** Every active agent is a member of the
  questions room; `events_default` and the humans' and coordinator's levels are raised so that a
  worker cannot post. Workers never sync it (QS4's worker filter is unchanged); they only fetch
  events by id. A demotion becomes a power change, not a kick (QS9 changes). FACT: v2's reports
  room already holds every member with posting limited by power (A 4.1).
- **The relay.** The coordinator's link, when it commits a human post attached to Q<n> whose
  `asked_by` is not empty (the sync, never the build, so a new coordinator relays only posts
  after its floor), sends each `asked_by` seat a quiet main-room `teamchat.text` with
  `relay {"room":"questions","id":<human post>,"q":<n>}`, transaction id derived from the post
  id and the seat, so it is sent once across restarts. Edits are relayed with `revises`. No model
  turn is needed. **Protocol change (A 7.x):** the `relay` key on `teamchat.text`.
- **The worker's side.** On a relay item, the worker's link fetches the human post from
  questions and delivers a HUMAN item (`room: questions`, `question: Q<n>`, `relayed_by`) only if
  the post is a declared human's, in questions, and its text hashes as fetched; otherwise the
  relay is dropped `provenance`. The question number is the coordinator's claim and the record
  says so. An offline worker gets it when it runs (OF1).

## 5. Build order, test first

Each step: write the test, see it fail, then the code. Fakes only (fakehs, the fake clock).

**Status:** steps 1-3 built on `v2-questions`; step 4's QX3 and QX4 built (own posts placed and
sent to the questions room, `questions_unechoed` and the `uncounted` count written, one compose
at a time under the team's judging hold); QX7's redaction rebuild and the `uncounted` hook line
not yet. Step 5 in part: the verbs' command line (`ask-human`, `thread --note`, `close`,
`retitle`, `bump`, `questions --waiting/--for/--digest`; `run.go`'s `unbuilt` empty), the client
(`--skeleton` printed with no session, close's not-retitled stderr line), the wire args, and the
`question` record's `why`, `for`, `waiting_since_ms` and `retitled` with the `bump` record; the
link's serving of them (the engine) is next.

Step 3 settled three points the sections above left open. The index keeps each question's
sender (`from`, 128 bytes at most), so the fold itself checks that a retitle edit comes from the
question's sender, and a rebuild gives the same answer as the live fold. A `reply` to an
attached post mentions only that post's sender (`ThreadPost.To`) and does not wait. The new
fields (`from`, `why`, `waiting_ms`) keep the worst-case state within QX6's 5 120 bytes per
question, so `MaxQuestionsStateBytes` is unchanged.

1. **#365 sizes** (exits helper). Red: `say` and `report` refusals over the cap say both sizes.
2. **proto: section 3.** Red: #367's comment scenario: a question; a human top-level `Q3:` post;
   an agent's plain reply; a human reply to that; a human thread on it: all attach to Q3, live
   and rebuilt byte-equal (the existing property test widened).
3. **proto: forms.** Red: `Composer.Question` refuses no `Why a human:` (10 `body`, size named);
   thread default is `m.text` with the humans mentioned, `--note` is `m.notice`; the link-back and
   its fold; the retitle edit and its fold; `waiting_since_ms`'s fold; codec round trip.
4. **n3f-c2b, c2c** (QX3, QX4, QX7), as SPEC-n3f specifies.
5. **The verbs** (n3f-d, changed): `ask-human`, `thread`, `reply` with the link-back, `close`,
   `retitle`, `questions` with `--waiting`, `--for`, `--digest`, `bump`. Red per verb: each
   refusal; the PUT contents (notify or quiet as section 2 says); `bump` skips answered and young
   questions; the link-back sent second with the thread post's permalink; `close` on another
   seat's root says `retitled: false`. `run.go`'s `unbuilt` emptied.
6. **verify in questions.** Red: a coordinator verifies a questions-room human id; a worker
   without membership gets 23 today, 0 after step 7.
7. **Delivery** (section 4). Red: Q4 asked `--for` w1; the owner answers; w1 gets a HUMAN item
   with `question: Q4`; a forged relay naming a non-human post is dropped; a restart sends no
   second relay; w1 offline: delivered when it runs.
8. **n3f-e and acceptance.** The skill text; a Lightning row (the owner, by hand): a notifying
   post notifies, a quiet one does not, a thread reply and a top-level reply attach, recorded as
   fixtures (as A's HC-A2/HC-A3 rows do for Element).

## 6. Fit with Task 5.1 and the cut-over

- R0 needs no host member: it is member code in containers and LXC seats, so it does not wait
  for Task 5.1 (INFERENCE from PLAN.md Task 5.1 and section 2: nothing here is host-only). The
  `--check` duty hint waits only for the `duty` field, which PROPOSAL-seat-kinds section 5 says
  lands before 5.1.
- Task 6.4 step 5 ("v2 has no questions verbs yet") is unblocked by steps 1-6. A team whose
  coordinator asks for workers also needs step 7 before it moves, or the owner types rulings
  twice, as on v1.
- **Can land first**, with no protocol change: step 1, step 4, step 6 for the coordinator, and
  `questions --waiting/--digest` once step 5 has the field. Steps 2-3 and 7 change A (10.2,
  10.3, 10.4, the edits row, 4.1-4.2, 7.x) and C (questions power levels, demotion), and need
  A's amendment reviewed first, as SPEC-n3f section 12 did for its amendments.

## 7. Owner decisions

- **OD-Q1. Workers read the questions room** (section 4). (a) members without posting power,
  verify and relay as above; (b) the coordinator's relay only, no read access. **Recommend (a):**
  (b) is v1's #364 again, a ruling a worker must not trust.
- **OD-Q2. Notify by default** in questions (`thread`, `bump`, link-back), quiet by `--note`.
  **Recommend yes:** a silent post that waits on the owner is the failure seen today.
- **OD-Q3. The link-back** top-level reply beside the thread answer. **Recommend yes**; the
  alternative, the answer only in the thread, is what the owner missed today.
- **OD-Q4. Names.** `ask-human` (B 2.2.1 says `ask`) and `--for` (AK1 says `--asked-by`).
  **Recommend `ask-human` and `--for`:** both are unbuilt, so renaming costs one table row, and
  agents moving from v1 already know `ask-human`. The wire field stays `asked_by`.

**Decided (2026-11-23):** the coordinator took all four recommendations as the defaults: (a) for OD-Q1, and yes to OD-Q2, OD-Q3 and OD-Q4. These are coordinator decisions, not owner rulings. The owner was told and may override them. The build waits for the owner's go-ahead in Q61.

## 8. Amendments to the design (areas A and C)

The protocol changes sections 2-4 need, in SPEC-n3f section 12's style: "Old" is the current
text joined onto one line, "New" replaces it whole. Unlike SPEC-n3f's, these are applied to
[DESIGN-v2-A-protocol.md](DESIGN-v2-A-protocol.md) and [DESIGN-v2-C-host.md](DESIGN-v2-C-host.md)
in the same commit as this section (the owner's GO, Q61: one branch, one review at its end), so
the build reads A and C as they will be. Five things are settled here that sections 2-4 left
open:

- **`wait`** on `teamchat.thread`. The fold needs to know which agent posts wait on a human
  (`waiting_since_ms`, 2.6), and no receiver reads `msgtype` (A 7.1). So a thread post that
  waits carries `"wait": true`, and its sender makes it `m.text` with the humans mentioned
  exactly then. A question waits by being a question. `waiting_since_ms` is the
  `origin_server_ts` of the newest waiting post (the question, a waiting thread post, a bump)
  not yet followed by an attached human post or a close; so `bump --min-age` measures the time
  since the owner was last notified, which is what stops a Stop handler bumping at every stop.
- **`why`** is checked when present (one of the four words, else `schema`) and absent is
  accepted: the admin's carried questions have none, and the sender's form is what makes it
  required for a coordinator (2.1). A receiver that dropped a coordinator's question for a
  missing `why` would break the numbering chain (A 6.5) on every seat, for a fault the sender
  already refuses.
- **The retitle edit** is the one agent edit a receiver reads: in questions, from the sender of
  a counted question, `m.relates_to` exactly `{"rel_type":"m.replace","event_id":<question>}`
  and `m.new_content` a `teamchat.question` content whose `n`, `asked_by`, `re` and `why` equal
  the question's and whose `Question:` section hashes the same (G1.4). The index takes its
  headline and nothing else. Any other agent edit is still dropped `edit`.
- **The relay** is a `teamchat.text` from the coordinator, so it is addressed, rated and
  delivered by the rules a `teamchat.text` already has; the worker's link turns it into a HUMAN
  item only after fetching the post itself (section 4), and otherwise drops it `provenance`. It
  is never delivered as a PEER item: a relay that fails its check is not shown at all.
- **Questions membership.** Every non-parked member is invited to questions and accepts the
  invite; posting stays the coordinator's and the humans' by power level (the coordinator listed
  at 50, `m.room.message` at 50). A parked member is not in questions' desired membership (its
  token is revoked; it can read nothing). A demotion is a power-levels write and no kick.

### 8.1 A 4.1, the questions row

Old:

```text
| questions | `<name> · questions` | `invite` | humans, the coordinator | humans, the coordinator; nobody when closed | the coordinator, for every human message |
```

New:

```text
| questions | `<name> · questions` | `invite` | humans, every non-parked member | humans, the coordinator; nobody when closed | the coordinator, for every human message |
```

### 8.2 A 4.2, the questions room's power levels

Old:

```text
Questions room while closed (no coordinator in the roster): the main-room object with `"events": {"m.room.message": 100}`.
```

New:

```text
Questions room while open: the main-room object with the coordinator in `users` at 50 beside the humans and `"events": {"m.room.message": 50}`, so a worker, a member there to read answers (10.6), cannot post. Questions room while closed (no coordinator in the roster): the main-room object with `"events": {"m.room.message": 100}`.
```

In the table "What each level allows", the questions (open) column is split from main: the
coordinator (50) posts messages and read receipts, an agent at 0 reads only.

### 8.3 A 4.3, the questions row and the demotion sentence

Old (the row):

```text
| questions | admin invite | admin invites the coordinator; the member accepts it per 6.3 | admin kick on removal or demotion |
```

New:

```text
| questions | admin invite | admin invites every non-parked member; the member accepts it per 6.3 | admin kick on removal or park |
```

Old:

```text
A demoted or parked coordinator is kicked from questions before any new coordinator is invited (00211 section 3; R10 demote before promote), and while the roster names no non-parked coordinator, questions is closed.
```

New:

```text
A demotion is a power-levels write of questions that drops the old coordinator from `users`, made before any new coordinator is raised (00211 section 3; R10 demote before promote); the demoted member stays joined, as a reader. A parked member, coordinator or not, is kicked from questions. While the roster names no non-parked coordinator, questions is closed.
```

### 8.4 A 6.3, the questions invite

Old:

```text
| questions | main is verified; the roster names this room under `questions`; inviter and stripped create sender are the admin; the roster names this member coordinator |
```

New:

```text
| questions | main is verified; the roster names this room under `questions`; inviter and stripped create sender are the admin; the roster names this member, not parked |
```

### 8.5 A 7.1, the edits row, the msgtype table and the relation table

Edits row. Old: `` `m.new_content` anywhere, or `rel_type` `m.replace`: drop `edit`. Agents never edit ``.
New: `` `m.new_content` anywhere, or `rel_type` `m.replace`: drop `edit`. Agents never edit, but one: the coordinator's retitle of its own question (10.4) ``.

msgtype table. Old rows:

```text
| `teamchat.question`; `teamchat.thread` that asks again | `m.text` | a question is what humans must be told about (00211 D14) |
| `teamchat.thread` otherwise | `m.notice` | follow-ups are quiet |
```

New rows:

```text
| `teamchat.question`; `teamchat.thread` with `wait` or `link` | `m.text` | a post that waits on a human, or answers one who asked outside the thread, must reach them |
| `teamchat.thread` otherwise; the retitle edit | `m.notice` | a note, a close and a retitle are quiet |
```

Relation table. Old row: `` | `teamchat.thread` with `n` | the thread form (10.3) | ``. New rows:

```text
| `teamchat.thread` with `n` and no `link` | the thread form (10.3) |
| `teamchat.thread` with `n` and `link` | exactly a reply, to a human post (10.3's link-back) |
```

### 8.6 A 7.3, the relay

After the secret check's bullet, a new bullet:

```text
- **The relay** (10.6). The coordinator's `teamchat.text` may carry `"relay": {"room": "questions", "id": <event ID>, "q": <n>}`: a human post in questions attached to Q<n> whose question names the addressee in `asked_by`. Only the coordinator sends one (a relay from anyone else: drop `role`), `to` is exactly one member, and it is `m.notice` with that member mentioned. The receiver delivers it only through the relay check of 10.6, never as PEER.
```

### 8.7 A 7.6, rules 03 and 08

Rule 03. Old: `` or a `teamchat.question` from the admin (a carried open question, 7.7), feeds the question index ``.
New: `` or a `teamchat.question` from the admin (a carried open question, 7.7), or a coordinator's retitle edit (10.4), feeds the question index ``.

Rule 08. Old: `` `teamchat.text` kind PEER (the same `human_text` rule applies, as v1); ``.
New: `` `teamchat.text` kind PEER (the same `human_text` rule applies, as v1), except one with `relay`, which takes the relay path (10.6) and, if it passes, kind HUMAN with the room lines of questions; ``.

### 8.8 A 10.2, the form and `why`

Old (the form bullet):

```text
- Form (P54): headline 1-120 code points (trimmed of Unicode white space; one that starts with a section name is `body`, as v1), then `Context:` and `Question:` sections (required) and `Options:` (optional, lines `1. ` to at most `9. ` in order); the whole at most 8000 bytes; `body` starts `Q<n> `. A form error is refused `body`, naming the section.
```

New:

```text
- Form (P54): headline 1-120 code points (trimmed of Unicode white space; one that starts with a section name is `body`, as v1), then `Context:`, `Question:` and `Why a human:` sections (required) and `Options:` (optional, lines `1. ` to at most `9. ` in order); the whole at most 8000 bytes; `body` starts `Q<n> `. `Why a human:`'s first word is one of `decision` (owner-only), `physical` (a physical act), `credential`, `preference`, and the object's `why` holds that word. A form error is refused `body`, naming the section, and the size and the limit when the size is the error. A receiver requires neither `Why a human:` nor `why` (the admin's carried questions have neither); a `why` that is not one of the four words is `schema`.
```

### 8.9 A 10.3, waiting posts and the link-back

Old:

```text
- A post that asks the humans something again is `m.text` and mentions them; otherwise `m.notice` and no mentions (00211 `--ask`).
```

New:

```text
- A post that waits on a human (B's `thread` by default, `bump`) carries `"wait": true` and is `m.text` mentioning every human; otherwise (`thread --note`, a close) no `wait`, `m.notice` and no mentions.
- **The link-back.** When the coordinator answers a human post attached to Q<n> that is not in Q<n>'s thread, it posts the answer in the thread (with `m.in_reply_to` the post, as above) and then one top-level reply to the post: a `teamchat.thread` `{"v": 1, "n": <n>, "link": <the thread answer's event ID>}`, `m.relates_to` exactly `{"m.in_reply_to": {"event_id": <the post>}}`, `m.text` mentioning that post's sender, body `Q<n>: answered in its thread: <permalink to the thread answer>`. It neither closes nor waits nor resets the unanswered posts; a reply to it attaches to Q<n> (10.4 rule 2).
```

### 8.10 A 10.4, attachment, the retitle edit and waiting

Old (rules 1 and 2):

```text
1. it is in the thread of a question (`rel_type` `m.thread`, root a question);
2. it replies to a question or to a post in a question's thread;
```

New:

```text
1. its thread root (`rel_type` `m.thread`) is any event attached to a question: the question, an agent's post of it (a thread post or a link-back), or a human post attached to it;
2. its `m.in_reply_to` names any event attached to a question, as rule 1 counts them (a human post attached at the top level included);
```

After the state paragraph, new paragraphs:

```text
So a reply chain never loses its question at a top-level hop. The index remembers attachments for a bounded number of recent events (B's caps); an older event is not inherited from.

**The retitle edit.** The coordinator that posted a counted question may edit it with `m.replace`, quiet (`m.notice`), whose `m.new_content` is a `teamchat.question` content with the same `n`, `asked_by`, `re` and `why` and a `Question:` section of the same hash; its new headline is the index's from then on. A closed question's retitle is `Q<n> ✅ CLOSED — <headline>`. Any other edit of a question is dropped `edit`.

**Waiting on a human.** A question is waiting while it is not closed and its newest waiting agent post (the question, or a thread post with `wait`) has no attached human post or close after it; `waiting_since_ms` is that post's `origin_server_ts`.
```

### 8.11 A 10.6, the whole flow

Old (steps 2 and 5 and the paragraph after them):

```text
2. The coordinator answers itself if it can; otherwise it runs `ask` (10.2) with `--asked-by` and `--re` naming the worker and the worker's message.
5. The coordinator passes the answer to each asker in main (`reply '<their event>'`, B 2.2.1) and closes the question (`thread Q3 --close`). A later edit arrives with `revises:`; the coordinator corrects what it passed on.

The asker gets the answer from the coordinator, not from the room, on purpose: the coordinator owns the work's direction and may combine answers, and workers stay out of the humans' notification stream (00211 D7).
```

New:

```text
2. The coordinator answers itself if it can; otherwise it runs `ask-human` (10.2) with `--for` and `--re` naming the worker and the worker's message (`asked_by` on the wire).
5. The asker gets the human's post itself: the coordinator's link relays each human post attached to Q3 to each member `asked_by` names, as a `teamchat.text` with `relay` (7.3), once per post and member (its transaction ID derived from both), with no model turn. The worker's link fetches the post from questions (every non-parked member is in questions, 4.1, and reads by event ID only, never syncing it) and delivers it as HUMAN with the room lines of questions and `relayed by:` only if the post is in questions, from a team human, and its text is what was fetched; otherwise the relay is dropped `provenance`. The question number is the coordinator's claim and the entry says so. The coordinator closes the question (`teamchat close Q3`). A later edit is relayed the same way, with `revises:`.

The asker reads the human's own words, never a peer's account of them, on purpose: a worker must not act on a ruling a peer relayed (#364). Workers still never post in questions and are not notified there; the coordinator still owns the work's direction and may add to the ruling in main.
```

### 8.12 A 13.2 and 13.3, the relayed line and the reminder sentence

13.2, a new row in the first table after `revises:`: `` | `relayed by:` | relayed only | - | - | - | - | the coordinator's handle (10.6) | ``,
and in the line grammars: `` | `relayed by:` | `relayed by: <handle> (the question number is its claim; the text was fetched from questions)` | ``.

13.3, the reminder's authority sentence. Old: `remind the humans with teamchat thread Q<n> --ask, or go on.` New: `remind the humans with teamchat bump Q<n>, or go on.`

### 8.13 C 3.6.2 and 3.6.4, park and demotion

C 3.6.2. Old: `a parked coordinator is kicked from the questions room and the questions room takes its closed form (A §4.3)`.
New: `a parked member is kicked from the questions room, and a parked coordinator's questions room takes its closed form (A §4.3)`.

C 3.6.4 step 1. Old: `demote the old (A's team record and its room specs say what changes, for example removal from the questions room) and apply it fully.`
New: `demote the old (A's team record and its room specs say what changes, for example the questions room's power levels, which drop it as a poster; it stays joined as a reader) and apply it fully.`

C 3.6.4, "apply it fully". Old: `` the questions room's membership and its open or closed form (open while an `enrolled` coordinator is joined, closed otherwise, A §4.3) ``.
New: `` the questions room's membership (every non-parked member) and its open or closed form (open, with the coordinator's power, while an `enrolled` coordinator is joined, closed otherwise, A §4.3) ``.
