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
