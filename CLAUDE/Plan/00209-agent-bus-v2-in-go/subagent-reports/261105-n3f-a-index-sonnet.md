# n3f-a, part G1 (the question index): the fold built, the codec and the build record not

Branch `feat/bus-v2-n3f-a-index` (from `feat/bus-v2-n3f-a`), code commit `cb9c0e9e7`. The builder
passed its context budget after the fold, so it stopped at a clean point: items 1-6, 8 and 9 of
SPEC-n3f 0.1 G1 are built and tested; item 7 is half (`State()` and `Copy()`, no codec) and item
10 (the build record) is not started. Read "Not done" first if you continue.

## Built (all in `src-go/internal/teamchat/proto/`)

- `questions.go`: `IndexedQuestion` (the 13 stored keys QX6's byte sums count: `n`, `id`, `latest`,
  `headline`, `hash`, `asked_by`, `re`, `asked_ms`, `remind_after_s`, `closed`, `attached`,
  `unanswered`, `unanswered_more`; `Provisional` is `json:"-"`), `UnansweredCount()`,
  `Redacted()`, `Reminder.DueMs` (deadline plus the 30 s grace), `UnattachedPost`, the caps as
  exported constants, `Next()`, `Open()`, `Evicted()`, `Question`, `Attachment`, `AttachmentOf`,
  `Unattached`, `OpenDuplicate` (hashes what it is given), `Reminders`, `Slot`.
- `questions_fold.go`: `Feed`, `Attach(event, human)` (replaces `Attach(verdict)`), the posts
  ring (`PostEntry`), the caps applied in the fold, `IndexState` (the stored shape of the index),
  `State()`, `Copy()`.
- `questions_prov.go`: `PlaceQuestion`, `PlaceThread`, `TakeDropped`, `ErrNotPlaced`; the echo of
  an own post replaces its provisional entry in stream order, an echo not counted drops it and is
  reported by transaction id.
- Tests: `questions_test.go` (adapted), `questions_fold_test.go`, `questions_slots_test.go`,
  `questions_prov_test.go`. `send_questions_test.go`'s `TestComposeUnattachedReply` changes with
  the spec (a reply to an unattached post now changes the index).
- `src-go/.golangci.yml`: `internal/teamchat/proto` joins the snake_case JSON overrides (the
  index's stored shape carries snake_case tags, like `member/state`), and `proto/corpus_test.go`,
  whose fixture keys are camelCase, is excused from tagliatelle (the file's pattern for fixtures).

## What each test was shown to catch

Red first: the new tests were run against a compile-only skeleton (types and no-op methods): nine
of them failed on their assertions (idempotence: `Feed said it changed the index`, `Attached:4`;
the caps: `open 0 evicted 0`, `kept 0`; and so on); the rest failed once a panic in one test's
failure message was fixed. Then the code, then green.

Then defects planted one at a time by `untracked/scratch/mutate.py` (scratch, not committed), each
run against the named test and the file restored: all 20 end red, and three survived at first.
Red: thread-post idempotence guard removed, attach idempotence guard removed, unanswered cap off
by one, agent thread post not resetting unanswered, posts ring one too long, unattached list
uncapped, open cap not applied, closed cap not applied at a close, slots not widening the
window, held numbers not made, a slot counted twice, a held entry taking a post, a plain reply
not clearing the post, hash of the text not the collapsed text, `asked_by` not filtered,
uncounted own echo not dropped, provisional entries not counted by `Next`, `Copy` sharing the
questions, a taken number treated as the same event, an echo folded again removing another
entry. The three survivors were a test list naming the wrong test, a guard the taken-number
check made redundant (restructured: a taken number is `repeated` only for the same event id),
and a missing case (`TestQuestionIndexAnEchoFoldedAgainRemovesNoOtherEntry`, added).

Not shown by any test: the "live equals rebuilt" property (see Not done). The caps tests fold 1100
posts and compare counts; they do not compare a paged fold with a live one.

## Gates, exactly

- `go test -race -cover ./internal/teamchat/proto`: ok, 98.2% of statements (the per-function
  holes in the question files: `questionNumber` 85.7%, `postRing.add` 85.7%, `attached` 66.7%,
  `PlaceQuestion` 85.7%, `PlaceThread` 85.7%).
- `golangci-lint run ./internal/teamchat/proto/`: 0 issues.
- `./scripts/qa-changed.bash` for the code commit: exit 1. docs, teamchat-keeps-off-v1 and
  test-qa-changed (179) passed. The go gate failed only in the nested `qa` module, in the four
  qa-js tests (`TestRealToolsFailASyntaxErrorAndALintFinding`,
  `TestRealMissingEslintToolingIsExitTwo`, `TestRealNestedConfigAndInlineDisableCannotWeakenTheRules`,
  `TestRealToolsPassACleanTreeAndWriteNothingIntoIt`), the known environmental failures of issue
  135\. Its output shows no result for the `src-go` module's own tests, so the proto figures
  above are from my own runs, not the gate's.

## Findings (the lead's three decisions, then mine)

1. **Thread posts (decision 1).** Agent thread posts and human posts rooted at a question are held
   in the same 1024-entry list as the human attachments. The entry is `{"id","q","t"}`, but `t`
   is an integer kind, not the bool the decision named: 0 a human post attached by another rule
   or to none, 1 a human post in a question's thread, 2 an agent's thread post, 3 a slot. Why: a
   bool cannot tell an agent's thread post from a human's, and `Composer.threadRelation`
   (`send.go`) calls `Attachment` for A 10.3's check, which must keep knowing human posts only. At
   most 83 bytes, inside the 87 and the 96 the sum sizes. QX6's "1024 most recent human posts'
   attachments" therefore also counts agent thread posts and slots.
2. **Slots need the raw event (decision 2).** `Slot(id)` is built: it takes the event id and is
   idempotent (an id in the posts list or among the roots is none). `RedactedSlot(raw, server)`
   over the raw event is **not built**. A redaction needs no index entry point: the judge's
   `Redact` outcome tells the engine to build, and the index has nothing to fold, so there is no
   `Redact` method.
3. **Smallest readings where the spec is open.**
   - A redacted slot **with no later question** leaves only the count, no entry (Q1 then a
     redacted Q2: `Next()` 3, `Question(2)` absent). QX5 (g) says "Q2 a `redacted` record in
     both"; that holds only with a later counted question (Q3), the case the fold can see.
   - An admin's carried question that jumps past the window (highest 1, two slots, admin Q9)
     holds the numbers the slots account for (Q2, Q3) and no others (Q4 to Q8 are no entries).
   - A held (redacted) number takes no attachment: a late `Q2:` post naming it is unattached, so
     it can never attach to a new Q2 and never to the redacted entry.
   - The closed cap evicts the lowest-numbered closed question (held entries count as closed);
     the open cap evicts the lowest-numbered open one and counts it.
   - Idempotence holds within what the index still keeps (the 1024 posts, the kept questions). An
     admin's question re-fed after its question was evicted is counted again, since the index has
     no memory of used numbers beyond the highest; a catch-up overlap is a page or so, far inside
     the window.
   - `from_id` and `asked_by` entries are kept only if they pass `ClassifyUserID` and are at most
     128 bytes; a human post whose sender fails that is attached normally but not listed unattached.
   - A plain reply counts (changes the index) only when its target is listed unattached; a reply
     to an attached, unknown or already replied-to post counts for nothing. An own plain reply
     placed provisionally takes its post out of `Unattached()` at once.
   - `AttachmentOf(h Human)` takes the `Human`, not the event, since the rules read nothing else.
   - `PlaceQuestion` refuses (`ErrNotPlaced`) a number that is not `Next()` and a transaction
     already placed; `PlaceThread` refuses a question that is not indexed. An own echo is matched to
     its entry by sender and shape (a question by number; a thread post by number, close flag and,
     for a reply, its target), first entry first, because sends are posted in order.
4. `TestComposeUnattachedReply` asserted the opposite of G1.3 and was changed with the spec.
5. The lint hook reports `tagliatelle` for snake_case tags in `proto`; the override and the corpus
   excusal in `.golangci.yml` are the fix. The suppression record is off until the owner signs
   (QA.md), so no record entry was needed; the reviewer should confirm the excusal is acceptable.

## Not done, and what the next builder needs

1. **G1.7, the strict codec and the load.** `IndexState`, `IndexedQuestion`, `PostEntry` and
   `UnattachedPost` already carry the snake_case JSON tags (QX6's byte sums count these exact keys).
   Needed: an exported `QuestionsState` for the store's `questions` key,
   `{"index": IndexState|null, "token": string|null, "last": string|null, "build": BuildRecord|null}`
   (index, token and last null together for a build owed; build null when the index is at the
   head), `Encode()`, `DecodeQuestionsState(data, server)`, `Check(server)` so `member/state` can
   embed it, and `LoadQuestionIndex(server, floor, IndexState) (*QuestionIndex, error)`.
   Decode like `member/state/strict.go`: `proto.Parse` (refuses repeated keys, integers over 2^53),
   `json.Decoder.DisallowUnknownFields`, a round trip that must equal the parsed value (so a
   missing key, null for a list, or another case is refused), then `Check`. Every list must be
   non-nil (`[]` not `null`). `Check` should hold: `highest`, `slots`, `evicted` >= 0 and their sum
   below 2^53; questions ascending, distinct, `n` in 1..highest, at most 64 not closed and 128
   closed; a question with an event has valid `id` and `latest`, a 64-hex `hash`, a headline of 1
   to 120 code points, at most 16 distinct valid `asked_by` (grammar and 128 bytes) and `re`,
   `remind_after_s` 0 or 600 to 604800, `attached` >= 0, at most 16 distinct valid `unanswered`
   ids with `unanswered_more` > 0 only at 16 and `attached` >= their sum; an entry with no `id` is
   closed with every other field empty or zero; posts at most 1024, distinct valid ids, `t` in 0..3
   with `q` > 0 for kinds 1 and 2 and `q` 0 for a slot; unattached at most 64, distinct, valid
   `from_id`, `sent_ms` >= 0. The token is non-empty, at most 1024 bytes and made of characters
   that need no JSON escape. Load sets `highest = max(state.Highest, floor)`, rebuilds `roots`
   from the non-redacted questions, the posts ring and the unattached list.
2. **G1.10, the build record.** `{"start": id|null, "after": id|null, "late_from": n|null}`;
   the rules for which combinations are valid are SPEC-n3f QX5 (null `build` with an index;
   an owed record has index, token and last null and `start` null; a build under way with an index
   has `start` set). The codec holds it; the engine's use of it is n3f-c's.
3. **`RedactedSlot(raw Value, server string) (id string, ok bool)`** in `proto` (verdict.go's
   sibling, with its own test): `type` `m.room.message`, a valid `event_id`, no `state_key`,
   `unsigned.redacted_because` an object whose `sender` is `AdminID(server)`, and the event's
   `sender` classified `IDAgent` or `IDAdmin` on this server. Test it with the judge: the same raw
   event gets `Drop`/`schema` from `Judge.Judge` and `ok` from the function.
4. **The property tests.** (a) A randomised "live equals rebuilt": over a fixed seed set, build a
   stream of questions, thread posts, plain replies, human posts and slots (more than 192 questions
   and 1100 posts, so every cap fires), fold it live, in pages with `State()` encoded, decoded and
   loaded between pages, and with the last few events of each page folded again; compare the
   `State()` JSON. The fold test helpers (`askVerdict`, `threadVerdict`, `humanEvent`, `eid`,
   `stateJSON`) are in `questions_fold_test.go`. (b) The QX6 worst case: 192 questions with 16
   `asked_by` of 128 bytes, 16 `re`, 16 unanswered, a headline of 120 `<` code points; 1024 posts;
   64 unattached; a 1024-byte token; encoded with `encoding/json` it must be at most 1 101 056
   bytes and round-trip through the strict decoder.
5. Plan documents: `BUILT-n3.md` and `PLAN.md` Task 3.2 say the codec, the build record and
   `RedactedSlot` are not built; update them in the commit that builds them.
6. Check before dropping a field: `IndexedQuestion.Sender` and `OriginTs` are gone (no caller
   outside `proto`; `AskedMs` is the old `OriginTs`).
