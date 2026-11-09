**n3f spec review, round 2: SPEC-n3f.md at e42ca16e7. Verdict: FIX-FIRST.**

The lead saved this read-only review's reply. Every finding is accepted; the fixes are part
n3f-spec-fix2. For N6 the lead takes the stronger fix: the promotion NOTICE names
`teamchat questions --team T`, and a text test pins the skill step.

Most of round 1 is fixed soundly. M2 and M3 are clean. M5 and M6 hold apart from the stale text noted below. The new design for M1 and M4 (an index persisted with a token) brought in three majors: the questions token, redactions, and the store bound. OW4 has no test. I checked every seam against the code at this base (`questions.go`, `verdict.go`, `session.go`, `filter.go`, `send.go`, `limits.go`, `exits.go`, `records.go`, `store.go`, `cli/run.go`), and each one is cited correctly.

## 1. Round-1 findings

- **M1 is PARTLY fixed** (QX2, QX4, QX5, G1.1, G1.2, D18). The fold, idempotence, the commit-after-SY5 rule and `AttachmentOf` are sound, and `Attach`'s `Deliver`-only gate (`questions.go:114`) is named as a gap. N1, N2 and N4 below break the claim that every seat holds the same state.
- **M2 is FIXED** (QS1, D15). `handle` does call `invites` before `trustTriggered`/`verify` (`session.go:429-435`). The new order is sound, and so is joining by the roster's id.
- **M3 is FIXED** (D1, RP3 step 3).
- **M4 is PARTLY fixed** (D9, QX1, QX5, QX6). The redesign is right, but the 320 KiB share is wrong (N3).
- **M5 is PARTLY fixed** (QS2, QX1, D13, G7). The `left` state is broken (N5).
- **M6 is FIXED**, with one stale line (N7).
- **Minors 1, 2, 4, 6-12 are FIXED.** Minor 3 is PARTLY fixed (N4). Minor 5 is PARTLY fixed (N6).
- **All nits are FIXED.** SPEC-n3.md line 812 now points to the addendum.

## 2. Major

**N1. The questions token can skip events (QX5 L300-303, QS7 L198-206, QS8, QX1).**

- QX5 sets the token to "the answer's `next_batch` in SY5's writes", with no condition.
- But while questions is out of the filter (a build, a catch-up, QS7's gap past the bound, QS8 untrusted), SY5 keeps persisting main answers.
- If those writes carry the questions token, the next catch-up starts past events it never folded. The numbering chain then breaks: the next `ask` takes a number that is already used, the question is uncounted by every member, and P1 loses posts.
- **Fix:**
  - The questions token and the index state are written only by an answer whose filter held questions and whose questions timeline was folded, or by a QX1 page.
  - Every other SY5 batch leaves the `questions` key as it was.
  - Say that QX1's page writes go through the engine's one store writer (G3).
  - Test: a QS7 gap with main answers persisted during the catch-up, the link killed mid-catch-up, then a restart. Nothing is skipped.

**N2. A redaction makes the live and rebuilt indexes differ (QS6 L192, QX2 L276, QX5 L304-306).**

- Only the admin can redact (A I4; `verdict.go:235`). A rebuild reads a redacted original as `redacted_because`, which is dropped `schema` (`verdict.go:260`).
- So a redacted human post is attached live (`Attached++`, the question becomes `answered`) but never on a rebuild (`open`). QS6 only removes the post from `unanswered`, so the question stays answered live.
- An admin redaction of a counted question is worse. Live keeps Q5; a rebuild cannot see it, so the next question is Q5 on one seat and Q6 on the other.
- **Fix:**
  - A redaction of a folded human post un-attaches it completely: lower the attach count, work the state out again, and drop its `unattached` record.
  - Decide what a redacted question does to the chain (A 6.5). One option: a redacted event from an agent or the admin in questions keeps its number, by the redaction's `redacts`.
  - Add both cases to QX5's live-equals-rebuilt test.

**N3. The 320 KiB per-team bound does not hold (QX6 L320, D17, OW3, G2).**

- `IndexedQuestion.Question` keeps the collapsed `Question:` section for `OpenDuplicate` (`questions.go:173`). That section can be up to about 8000 bytes.
- 64 questions not closed × 8000 bytes is already about 500 KB, before headlines (192 × up to 480 bytes), 1024 attachments, 64 × 16 unanswered ids, and JSON escaping (up to 6×).
- `MaxStore` is 2 MiB (`store.go:16`), and G2 does not say it is rounded up again.
- **Fix:**
  - Store a SHA-256 of the collapsed section, not the text.
  - Work out the share for the worst case, escaping included.
  - Say that `MaxStore` is rounded up again (n3d-D1's rule).
  - Test: a store holding every cap full at maximum sizes decodes under `MaxStore`.

**N4. A question posted but not yet echoed is lost at a restart (QX3 L285-289, minor 3).**

- Provisional entries are not persisted (QX5) and are placed again only from `sending`.
- Take a PUT that succeeded, so it has left `sending`, then a crash before its echo is synced. After the restart the loaded index is "ready" (QS5), so an `ask` that runs before the first answer arrives takes the same `Next()`. That is n3f-D5's harm: an uncounted question.
- **Fix:** after a restart the index is not ready (20 `not-ready`) until the sync has folded the last own questions send. Track it by n3d's `last_sent` or by persisted provisional ids. Test it with a failpoint between the PUT and the echo.

**N5. A `left` room turns 22 at once (QS8 L216-221, QS2 L161-163).**

- QS8 says that after an own leave with the roster still naming this member, the verbs "wait as for an awaited invite".
- But QS2's 300 s clock runs from "first saw the roster naming it coordinator". For a coordinator of long standing that time is long past, so the next verb exits 22 `untrusted` while `status` says `left`. QS8's own test ("no `untrusted`") contradicts this.
- **Fix:** the clock starts at the later of the roster naming this member and its latest own leave. Test a kick that comes long into a term.

**N6. OW4's skill step has neither text nor test (section 9 OW4 L558-559, section 6 L473, section 10 n3f-e, SPEC-n3 PK2 L793).**

- PK2's text test checks only that the names the skill uses exist. Nothing pins "a promoted coordinator runs `questions`", yet under OW4 (a) that step is the only thing that surfaces posts made during a handover.
- Nothing ties the step to the signal that tells the session it was promoted: DV6's role-change NOTICE, A 13.3's "you are the coordinator".
- **Fix:**
  - n3f-e adds the skill sentence.
  - n3f-e adds a PK2 text test that pins it, as n3e-11 did.
  - Preferably, the promotion NOTICE itself also names `teamchat questions --team T` (A 13.3's template), with a test.

## 3. Minor

- **N7. D6 L517 is stale.** It reads "or whose verb ended 20", but under the M6 ruling a `report` never ends 20: a transient failure queues it with exit 0. Delete it, or name the one case that can end 20.
- **N8. RM1 L418-426 does not wait for a ready index.** During a build, a question that is answered later in the room is open in the partial index, so a wrong reminder is made and the watermark raised. **Fix:** reminders only from a ready index, with a test.
- **N9. The D11 watermark misses late-found questions** (L522, QS8). A question first folded by a catch-up, such as an admin's or a predecessor's post that landed while the room was untrusted, can have a due time below `reminded_ms`, so it is never reminded. **Fix:** a question first folded by a build or catch-up is judged against its own due time, not the watermark.
- **N10. QX2 L273-277 does not fold agent replies to unattached posts.** `threadPost` ignores a reply with no `n` (`questions.go:185`). Without those replies G1.3 and QL1's "until it gets a reply" cannot be rebuilt. **Fix:** list them in QX2.
- **N11. RP4 L122 leaves a refused reports join with no exit.** "RP1's join is tried first" does not say what `report` exits when that join is refused (403). Say 22 `forbidden`, nothing charged.
- **N12. AK1 step 3 / QS8 leave a down server during re-verification with no exit.** A questions verb that re-verifies an untrusted room while the server is down has no stated exit. State 20 `unreachable`; queueing does not apply, since nothing passed the check.
- **N13. QX3 L281-282 tells the agent nothing about an uncounted echo.** When the echo is not counted, the coordinator was told `Q<n>`, exit 0, and only a log line follows. Count it in `status`.

## 4. Nits

- G4 should say that `records.Question.ID` changes type from `string` to nullable (`records.go:193`).
- The catch-up's "at or before the index's last folded event" (QX1) needs a stream position, but event ids have no order. Say that overlap is skipped by G1.1's id idempotence.

## 5. Checks 3-5

- **3. The M6 ruling** is stated consistently in RP4, QL1 and QR2. Only D6 is stale (N7).
- **4. OW4** fails the check (N6).
- **5. The owner's rule:** OF1-n3f L433-434 states it exactly, and no n3f operation targets another member. D4's `--asked-by` agrees with it.

**Verdict: FIX-FIRST.** N1-N6 must be fixed before n3f-a starts; N3 and N4 change G1 and G2.
