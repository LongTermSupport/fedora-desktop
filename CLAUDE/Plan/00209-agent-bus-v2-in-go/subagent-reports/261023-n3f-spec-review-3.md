The lead saved this read-only review's reply: the last Opus round. B1-B3 are accepted (B3's rule
as proposed: a question number is never given out twice); F1-F5 and the design amendments are
made now in fix round 3 rather than during the build. A narrow check follows.

**n3f spec review, round 3: SPEC-n3f.md at db7c34d40. Verdict: FIX-FIRST (narrow).** Three short text fixes, all small enough for the lead's narrow check. Everything else is FOLLOW-UP.

## 1. Round 2's findings

- N1: FIXED. D19 and QX5's two writers are sound, and an empty questions timeline under the coordinator filter still advances the token.
- N2: PARTLY. The rebuild design is right; how a rebuild is persisted is missing (B1).
- N3: FIXED. The sums check out (one line is off, F1).
- N4: FIXED, apart from F2.
- N5 to N8: FIXED.
- N9: PARTLY (B2).
- N10 to N13 and both nits: FIXED.

## 2. BLOCKERs

**B1. A rebuild that is owed or under way is not persisted (QX5, QX7, G2). Part: n3f-a codec, n3f-c.**

- The SY5 write that folds an admin redaction is a "questions timeline folded" write under QX5 rule 1. As written, it persists the index from before the redaction, with its token past the redaction.
- A crash before the rebuild ends means the restart loads that index, and nothing ever rebuilds it. Live then differs from rebuilt for good, which is N2's harm again, this time through a crash.
- Second problem: a replacement build's partial forward pages, after a restart, look like an index with a token. They are then resumed as a catch-up, which takes every human post after the floor as an item, not only the posts after the replaced index's last event. The sync already delivered those, so they are delivered twice.
- Fix:
  - The write that folds a redaction drops the index state and records "build owed", plus the replaced index's last folded event.
  - Each page write of a replacement build records the build's start marker and that event.
  - A load that finds either resumes the build, not a catch-up.
  - Test: kill the link after the redaction's write, and again mid-rebuild.

**B2. `remind_late` overflows its cap (RM1, D11). Part: n3f-c; n3f-a's cap only holds under this rule.**

- As written, every question first counted by a build goes in. A first build over a room with more than 64 questions breaks the cap of 64, and the strict codec then refuses the store write.
- Fix: put in only questions that are `open` and have `remind_after_s`. Remove a question from the list when it leaves `open` or is evicted. The cap then follows from the cap on questions not closed.

**B3. A number given out twice (item 3). Part: n3f-a, G1.9.**

- My judgement: this is not sound. `Q<n>` is the one name humans, main-room messages, agent transcripts and the `uncounted` hook line use. A human's late `Q2:` for the redacted question would attach to the new Q2.
- Simplest rule: `Next()` = 1 + the highest counted + the slots since it. The counting window stays exactly as specified: n from highest + 1 to highest + 1 + slots. Only the coordinator's numbering changes, so it always numbers past any possibly redacted question.
- What it keeps:
  - Every input is still what a build sees, so live still equals rebuilt.
  - A slot that appears later, before a question already counted, never uncounts it: the window only widens, which I checked case by case.
  - A redacted thread post leaves a harmless gap. Record it as "a number held by a redacted event".
- QX5 (g)'s second case becomes Q3, not Q2.

## 3. Item 4, the accepted risk

- Acceptable. It needs an agent that numbered from a wrong index, every seat agrees on the result, and nothing is lost.
- Under B3 it becomes the normal path, not a risk.

## 4. FOLLOW-UPs

- **F1 (n3f-a).** QX6 gives "64 together" for `reminded_ms`, `questions_unechoed` and `questions_uncounted`, but their values alone are 19 + 46 + 19 = 84 bytes, before the key names. There is about 88 KB of margin under 4 MiB, so the result stands. Fix the line, and say `asked_by` entries are kept only if they pass A's user-id grammar: the sum assumes they need no escaping. The rest checks out: 4729 bytes per question, 1 101 056 for the key, 4 104 376 in all, rounded up to 4 194 304. The store is per seat (`NewStore(userID, team)`), so the per-team figure is right.
- **F2 (n3f-c).** "Ready" has two meanings: QX1's (at the head) and QX3's (no own post unechoed). If QS4's filter waits for QX3's meaning, the echo is never synced and `questions_unechoed` never clears, so every questions verb answers 20 for ever. Say that the filter takes questions once the index is at the head, and that unechoed blocks only composes, the verbs and reminders.
- **F3 (n3f-e or n3f-c).** The promotion NOTICE comes exactly while the build runs. `questions` may then answer 20 `not-ready`, and the wait class says "try once more; do not loop", so the handover posts can go unseen. Under OW4 (a) that NOTICE is the only signal. Fix: one hook line when a promotion's build reaches the head, naming `teamchat questions --team T`, pinned like section 6.1. Or tell the agent to retry after its next item.
- **F4 (n3f-c, Task 3.4).** Slots and builds depend on `/messages` returning `unsigned.redacted_because` (A 12.4 asserts it). Add a `-tags tuwunel` row to section 11's E1 line.
- **F5 (n3f-c).** QX3's hook line is said "once", but nothing records that it was said. Persist it beside `questions_uncounted`.

## 5. Item 5: the amendments

- **A 13.3:** the line is given word for word and fits the 600-character rule. But A 13.3 also says how a role works "is told at SessionStart, not in the notice". That sentence must change too; list it.

- **A 6.5:** no A wording is given. Give the replacement sentence for 6.5's "exactly 1 plus the highest counted" (it becomes the window) and for A 10.2's "`n` is 1 plus…" (B3).

- **B 2.3.2:** all six keys are named, and their caps are spread over D6, QX3, QX6 and D11. Say which object `reminded_ms` and `remind_late` sit in.

- **Missing from the amendment list:**

  - B 2.2.1's `ask` row (B line 373) still says `--re` "must be an event of this team's main room (else 10 `re`)", against D14.
  - B 2.2.4 row 20's meaning ("the homeserver did not answer") and its next step must widen for `not-ready`.
  - B 2.6 step 10's `human_text` bullet (D2).

  One amendment list in section 9 or 10 would serve the lead.

## 6. Item 6: consistency and the owner's rule

- Every exit maps to B 2.2.4: 10 `open-limit`/`target`/`body`, 15 `duplicate`, 20 `not-ready`/`unreachable`, 22, 23, 64, 5. Only the meaning of 20 lags (section 5).
- No n3f verb, join, read or reminder targets another member, and no n3f path exits 69. OF1-n3f holds.
- No other cross-section conflicts.

**Verdict: FIX-FIRST (narrow).** Fix B1 to B3, then n3f-a can start; F1 to F5 are fixed while building, in the parts named.
