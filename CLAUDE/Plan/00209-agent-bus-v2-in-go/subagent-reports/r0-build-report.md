# R0, the v2 questions verbs: build report (steps 1-8)

The design is [DESIGN-questions-v2.md](../DESIGN-questions-v2.md); its section 5 is the build
order, and this report follows it. Steps 1-6 were built on branch `v2-questions` (last at
e486543f2, CCY 3.124.5 / container 2.84.56). Step 7 is being built on `v2-questions` now. Step 8
was built on `v2-questions-docs`, cut from e486543f2 (CCY 3.124.6 / container 2.84.57). The lead
merges both branches into `v2`. Every step was built test first, with fakes only (fakehs and the
fake clock). The partial reports from earlier in the run are
[r0-steps-4-5-report.md](r0-steps-4-5-report.md) and [r0-step-5-report.md](r0-step-5-report.md).

## Built

1. **Sizes (#365, R9).** A size refusal names the body's size and the limit, through one helper
   in `exits` that every verb uses, `say` and `report` included.

2. **Attachment (#367, R3), section 3.** A reply chain keeps its question across a top-level
   hop: a post attaches to Q<n> when its thread root or `m.in_reply_to` is anything already
   attached to Q<n>. The live fold and the rebuilt index give the same bytes; the property test
   was widened to check this.

3. **The forms (A 10.2-10.4, section 8).**

   - A question needs `Why a human:`, and its `why` is one of four words.
   - A thread post that waits on a human carries `wait`; it is `m.text` and mentions the humans.
   - A note is `m.notice`.
   - The link-back is a `teamchat.thread` with a `link` field.
   - The retitle edit is folded only when it comes from the question's sender.
   - The index records `waiting_since_ms`.
   - The index keeps each question's sender, `from`, so a rebuild judges a retitle the same way
     as the live fold.
   - The worst-case state still fits QX6's 5 120 bytes per question.

4. **n3f-c2b and c2c (QX3, QX4, QX7).**

   - QX3: a question or thread post this seat sends is placed in the index straight away, and
     its echo from the server takes the place. An echo whose number another post already took
     counts as uncounted, and the hook says so once, with the number.
   - QX4: one compose runs at a time, under the team's judging hold.
   - QX7: when an admin redacts a post, the index is built again. A build that started before
     the redaction ends instead of looping.

5. **The verbs (n3f-d, changed).**

   - `ask-human` takes `--for`, `--re`, `--remind-after`, `--check` and `--skeleton`.
   - `thread` notifies by default and is quiet with `--note`.
   - Also built: `close`, `retitle`, `bump` (with `--waiting` and `--min-age`) and
     `questions --waiting/--for/--digest`.
   - `reply` to a human post attached to Q<n> answers in Q<n>'s thread. When that post is not
     in the thread, `reply` also posts a top-level link-back.
   - The link serves every verb, in `engine/questionsverbs.go`. `run.go`'s `unbuilt` list is
     empty.
   - Known gaps are in section 5 of the design: no duty `hint` records, a `--check` that prints
     the record rather than the body, no link-back for a queued answer, and a provisional post
     that sets no waiting time.

6. **`verify --room main|questions` (R10, #369).** The `verified` record gains `human`,
   `question` and `headline`.

Step 7 is the section below. **Step 8, the skill and the reminder text (n3f-e).** The skill
now teaches:

- `ask-human`, with its form and the `--for`, `--check` and `--skeleton` flags;
- `thread` and `thread --note`;
- `close`, `retitle`, `bump --waiting`, and `questions --waiting/--for/--digest`;
- `verify --room questions`;
- the hook line for a post that was not counted.

A question's TIMEOUT reminder now says `teamchat bump Q<n>`, as A 13.3 says. The `kit` tests
were left unchanged. They pass again, and the image's `SKILL.md` matches the assembled kit.
HC-Q1, the owner's check in Lightning, is in section 9 of the design.

## Being built on `v2-questions`: step 7, delivery (#364, R1)

This is a description from the design (section 4, amendments 8.1-8.7 and 8.11-8.13), not of
built code.

- **Membership.** Every non-parked member joins the questions room and reads it by event ID
  only. Posting stays limited to the humans and the coordinator, by power level. A demotion
  becomes a power-levels write instead of a kick.
- **The relay.** When the coordinator's link commits a human post attached to a question with
  a non-empty `asked_by`, it sends each seat in `asked_by` a quiet main-room `teamchat.text`
  carrying `relay {room, id, q}`. The relay's transaction ID is derived from the post and the
  seat, so a restart does not send it twice.
- **The worker's side.** The worker's link fetches the post from the questions room itself. It
  delivers the post as HUMAN, with `question: Q<n>` and a `relayed by:` line, only if the post
  is a declared human's. Otherwise it drops the relay as `provenance`. A worker that was offline
  gets the post when it next runs.

## Tests run (no -race)

- Steps 1-6: proto, hooktext, engine, link, teamsync, state, team, client, wire, records and cli
  passed, and golangci-lint was clean on each package.
- Step 8: proto, `member/deliver`, `member/kit`, `member/cli` and `member/hooktext` passed.

## Open

- **HC-Q1 has not been run.** It is for the owner, by hand, in Lightning. Until it is run, it
  is unknown whether Lightning notifies per thread post and how it shows an in-thread reply to
  a top-level post.
- **The skill does not describe a relayed HUMAN entry** or its `relayed by:` line. That text
  belongs with step 7.
- **The version bump may collide.** Step 7's own version bump may also take CCY 3.124.6 and
  container 2.84.57, so the merge into `v2` takes the higher version on each side.
