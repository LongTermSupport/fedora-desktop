**n3f spec review, round 1: SPEC-n3f.md at de3704b69. Verdict: FIX-FIRST.**

The lead saved this read-only review's reply. Every finding is accepted; the fixes are part
n3f-spec-fix1. M4's repair is the fixer's to design and justify; the handover point is OW4, for the
coordinator.

Six major findings, all fixable inside the spec without changing its structure. I checked the seams against the worktree code. Note that this base is before n3d and n3e: the send gate, `sending` and the own-sends list are not in this tree yet.

## Major

**M1. The rebuilt index knows no human posts (QX1 L173-181, QS5 L134, G1).**

- QX1 feeds only rule 03's `index` outcomes. `IndexedQuestion.State()`, `Unanswered`, `Latest` and `humans` are filled only by `Attach` (`proto/questions.go`). After any restart or promotion, every answered question therefore reads `open`.
- Consequences:
  - RM1 reminds questions that were already answered.
  - QL1 lists no unanswered and no unattached posts, so QL1's handover test and n3f-X4 contradict QX1.
  - QR1's reply to an older post fails `threadRelation`'s `known` check with 10 `target`.
- `Attach` is not idempotent (`Attached++`), and QS5 calls it "before it is persisted". An answer held and re-read (SY8, SY6 epoch, SY7 resend) therefore counts posts twice. G1.1 covers `Feed` only.
- QR2's history "on QS5's path" would mutate the index and spend `rate`.
- A human post dropped by `rate` is not attached live but would be by a rebuild, so the two disagree.
- **Fix:**
  - QX1 also runs every human post through attachment, without delivering it.
  - `Attach` is idempotent by event id, and applied only after SY5's write.
  - Attachment is independent of `rate`.
  - QR2 uses the read-only `Attachment`.
  - An epoch change (SY6) also rebuilds the index (QX5).

**M2. Room order within one sync answer is not specified (QS1 L107, QS8 L158).**

- `proto.Invite` returns `InviteReject` when the roster names the room but not this member as coordinator.
- A 5.2 writes the roster first, then invites, so both can land in one answer. If the invite is judged before main's roster, the new coordinator leaves the room for good.
- On demotion, the kick from questions (an own `m.room.member` leave, an A 6.5 trigger) judged before the roster gives a false QS7 `untrusted`, with its frame and hook line.
- **Fix:** in every answer, main's state and timeline are judged first, then invites, then questions. A leave from questions that follows a demotion is the demotion, not a trust failure. Add a test with both in one answer.

**M3. The stdin caps can silently cut text (RP3 L71, AK1 L199, TH1 L222).**

- The rule reads cap+1 bytes, then strips one trailing newline. Take 1500 bytes, then `\n`, then `x`: it reads 1501 bytes, strips to 1500, and accepts, losing `x`.
- That breaks A 10.1's "never cut", and a test written exactly as stated passes on the wrong code.
- **Fix:** read cap+2 bytes, or require EOF after the strip. Test that case.

**M4. n3f-D9's repair does not repair (D9 L366, OW3, G1.4).**

- `Complete()` is defined as "the read reached `m.room.create`". Raising `question_floor` does not shorten that read.
- A 5.1 makes `question_floor` a migration-only admin write (area D). No C operation exists to raise it later.
- So past 10 000 events, `ask` and `thread` are refused for ever.
- **Fix:** define completeness so a floor raise satisfies it (an anchor the reader can see), or name a different repair, such as C replacing the questions room. Make that the actual content of OW3.

**M5. A transient build is reported as "tell a human" (AK1 step 3 L205, TH1 step 2, QX1, QS6).**

- `Complete()` is false while QX1 is still reading: at link start, at promotion, and on every QS6 rebuild.
- An `ask` in that window exits 22 `incomplete`, a code whose class is "tell a human".
- D13 does the same for the normal gap between the roster naming the new coordinator and the invite arriving.
- **Fix:** while a build or the awaited invite is in progress, the verb waits within its 30 s deadline, then exits 20 (or another wait-class code). 22 is kept for the bound actually passed, or an invite that never came.

**M6. Wrong exit code for a 429 in RP4 (L96).**

- RP4 maps "a 429 past its retries" to 20 `unreachable`.
- B 2.2.4 and the built `teamsync/verbs.go` `verbErr` map `ErrRateLimited` to 14 `server-rate`.
- **Fix:** 14 `server-rate`, and add a test row for it.

## Minor

01. **The seam list is not accurate (section 0, G1, G4 L52).**
    - `records` already has `KindQuestion` and `Question{n,state,id,asked_by,unanswered}`.
    - `Human.Question` is `*int64`. Section 6 makes it the string `"Q4"` / `"none"`: a type change to a built record, and G4 does not name it.
    - `IndexedQuestion.Unanswered` is a count, but section 6 needs ids, oldest first.
    - `SyncFilter` chooses the coordinator form from the roster alone, so QS3/QS7's worker form while questions is untrusted needs a new API.
    - `Admit` refuses the report window as `ReasonRate` / `"report_per_hour"`. `proto.Window` has to be seeded from `outbox.reports` and has to expose the oldest time for RP3's message.
    - Add these as gaps.
02. **QX3 L185 (stream order).** The send-fed entry goes in before its echo arrives. If an admin's carried question with the same `n` lands earlier in the stream, this member keeps its own question while a replacement coordinator keeps the admin's; P58's order can also differ. **Fix:** a send-fed entry is provisional and is placed in stream order when its echo arrives.
03. **SD3 restart and QX4.** A questions `sending` entry is re-sent by its transaction id after a restart, with a `Q<n>` composed against the old index. **Fix:** re-send it before QX1 finishes and before any new questions send, then feed its outcome.
04. **QS9 L168, re-promotion.** A member promoted again resumes from a surviving `floor_ms.questions`. That delivers, with no `stale` rule in questions, posts the interim coordinator already handled. **Fix:** drop the questions floor at demotion, or resume from the later of the floor and the join.
05. **QS8 L160 with QS4 (handover loss, P1).** Human posts accepted by the old coordinator are dropped. The new one starts from its join, so posts in between reach nobody's inbox and show only in `questions` (and only once M1 is fixed). This is a product choice: add it as **OW4**, recommending that `questions` lists them and the skill says so.
06. **QR1 L251.**
    - A non-coordinator must never look up an id in questions: its 403 must not become 22, only 23.
    - QR1 omits A 10.3's sender index check, which the composer refuses as 10 `target`. List it, with a test.
07. **RP3 step 2 L77, AK1 step 2.** A parked member fails main's roster check (A 6.4: "listed and not parked"), so its team is already 22. A removed member's team is gone (LN8). "Parked → 10 `schema`" cannot be reached: delete it or say why.
08. **n3f-D11 L368.**
    - Skipping a 257th reminder conflicts with B 2.3.2's "never a silent drop".
    - Due times are fixed by history, so a single `reminded_through_ms` watermark is O(1) and needs no cap.
    - RM1 must use A 10.5's key `<id>~humans` (`proto.ReminderKey`), not the bare id.
    - Name the timer's clock: wall time, re-armed after an SY12 freeze.
09. **Unbounded output and memory (QL1 L237, section 6).**
    - Unattached posts are listed for ever, `--all` and `unanswered` have no cap, and the in-memory index grows past 10 000 during a link's life, then becomes `incomplete` at the next restart.
    - **Fix:** cap the records with a trailing count `fact`, bound the age of unattached posts, and apply the 10 000 bound the same way live and at build.
10. **QX1 runs inline (D8 L365).**
    - Up to 100 pages are read before the answer is persisted, so main's delivery and the long-poll wait on questions.
    - A 5xx that persists on questions' `/messages` alone stops main for ever.
    - **Fix:** persist main's items first, or bound the build. Treat repeated failures as the room's alone (QS7).
11. **OF1-n3f L286.** "69 is only the own session's" reads as a general rule. It contradicts the owner's rule (an operation that targets an offline member reports 69: n3b's `lxc restart`). **Fix:** limit the sentence to n3f's verbs.
12. **QX4 test L191.** As stated it can pass by timing. **Fix:** fakehs holds the first PUT while the second `ask` is composed.

## Nits

- **RP2 test:** `verdict.go` drops a non-roster sender as `sender` before the reports `Fault` check, so the forged event must come from a roster agent.
- **RP1:** do not match on `M_FORBIDDEN` error text; read the joined rooms. The join's second connection belongs in 2.14's count.
- **RP4:** "with the room still verifying" is unclear.
- **n3f-D6:** a report whose PUT ends 20 still uses up a slot in the hourly window. Say so in the message.
- **TH1:** say what a plain `thread Q<n>` on a closed question does.
- **CL2:** the `unbuilt` list is empty only after n3d and n3e remove their entries.
- **SPEC-n3.md section 7** still says "scope only"; it needs a pointer to the addendum.

**Verdict: FIX-FIRST.** M1-M6 and minors 1-5 need fixing before n3f-a starts. The slicing order holds, but n3f-a also needs the gaps in minor 1.
