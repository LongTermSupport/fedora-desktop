# n3f-a, G2: the seat store's seven keys, `MaxStore` at 4 MiB

Branch `feat/bus-v2-n3f-a-store`, from `feat/bus-v2-n3f-a-int` (head 1d3ed2a47). G2 is built, and
with it every row of n3f-a (G1, G2, G4, G5, G6, G7) is built; the slice awaits its review. All
code is in `src-go/internal/teamchat/member/state/` (`store.go`, `store_test.go`,
`store_questions_test.go`).

## Built

- The seven keys of SPEC-n3f 12.6, each in the `Store` struct so the strict decoder refuses a
  store that lacks one, has one in another case, has null where a value is needed, or has a key
  the struct does not: `outbox.reports` (`[]int64`), `outbox.questions_unechoed` (`*string`),
  `outbox.questions_uncounted` (`Uncounted{Count, Said}`), `outbox.questions_handover` (`bool`),
  and at the top level, between `outbox` and `run`, `questions` (`*proto.QuestionsState`, null
  for a seat with no index), `reminded_ms` (`int64`) and `remind_late` (`[]int64`). The new
  outbox keys come after `last_sent`, which n3d put after `gate` (12.6 says "after `gate`").
- The store learns the team's server as it always did: `paths.ServerName(s.Team)` in
  `Store.check`. It gives that to `proto.QuestionsState.Check(server)`, and the refusal carries
  the codec's own rule text: `questions: <rule>` (never a value from the file).
- Rules (each its own text, so a test can tell which one refused): `reports` at most 60 (n3f-D6)
  and each a time (`validMS`); `questions_unechoed` an event id or null; `questions_uncounted`
  with `0 <= said <= count`; `remind_late` a list of at most 64 (`proto.MaxOpenQuestions`, n3f-D11)
  distinct numbers from 1; `reminded_ms` not negative; the three non-null lists non-null (the
  existing "an outbox list is not a list" rule for `reports`, a new one for `remind_late`).
- `MaxStore = 16 * 256 << 10` (4 MiB). `TestStoreFullFitsTheFileCap` builds a store with every cap
  full at its widest (all of n3d's, 60 report times, the longest ids and counts, a 64-number
  `remind_late`, the worst `questions` key: 192 questions with 16 `asked_by` of 128 bytes, 16 `re`,
  16 unanswered and a headline of 120 `<`, 1024 posts, 64 unattached, a 1024-byte token and a build
  under way) and holds that it encodes and decodes: 3 428 163 bytes, the `questions` key
  1 001 559 of at most 1 101 056 (`proto.MaxQuestionsStateBytes`). It pins n3d's sum at
  3 001 136, the new sum at 4 104 600 (QX6's arithmetic: 60 x 14 + 16, 64 x 20 + 16, the key,
  and 256), and `MaxStore` as that sum rounded up to the next 256 KiB, 4 MiB.
- `NewStore` writes all seven (`"reports":[]`, `"questions_unechoed":null`,
  `"questions_uncounted":{"count":0,"said":0}`, `"questions_handover":false`, `"questions":null`,
  `"reminded_ms":0`, `"remind_late":[]`).
- **The item id grammar takes `~humans`** (finding 2): a reminder's key
  (`proto.ReminderKey(question event id)`, A 10.5) is a valid pending, ledger and awaiting id,
  and only a TIMEOUT in the questions room may carry it (pending and ledger).

## Decisions and spec readings (the smallest that keeps the spec)

1. **A store written before this change is refused, not read with the keys defaulted.** The
   strict decoder's rule is that every key is present ("a key missing"); `Version` stays 1 (n3d:
   no store exists outside tests), so no migration exists. The engine already treats an
   undecodable store as refused (`msgStoreRefused`: "run status and tell a human", and the
   `set-aside` verb renames it for a fresh one). A store the engine creates is `NewStore`'s, with
   all seven. `TestEveryKeyOfTheQuestionsRoomIsRequired` removes each key in turn, and all seven
   together, and each is refused.
2. **`said <= count`**: QX3 says the line is owed "while `count` is above `said`" and `said` is
   raised by one when a hook takes a line, so `said` past `count` cannot be written by the spec's
   own writes. The spec does not state it as a rule; I made it one.
3. **`remind_late`: distinct numbers from 1, no order, and not cross-checked with the index.**
   RM1 says each number is an open question of the index written with it, but a build owed
   writes no index while it keeps the replaced index's numbers, so the store cannot check it.
   That check is n3f-c's, at the write.
4. **`reports`: no order rule.** `proto.SeedWindow` sorts whatever order it is given, and two
   reports may share a millisecond, so equal times are allowed. A time is `validMS`.
5. **No upper bound on a count or a time beyond the decoder's.** A first version bounded
   `count`, `reminded_ms` and `remind_late` by 2^53 - 1 itself; the mutants of those rules
   survived because `proto.Parse` already refuses an integer past it, so the rules were dead code
   and were removed. The tests show the decoder refuses 2^53 in each.
6. **No byte rule for the `questions` key.** Every part of it is capped by `Check`, so its size
   is at most 1 001 559 bytes by construction, and no state that passes `Check` can exceed the
   cap, so a byte rule could not be shown to fire. The worst-case test pins the number against
   `MaxQuestionsStateBytes`, so a raised index cap without a raised `MaxStore` turns it red.
7. **No `Add...` mutators for the two new lists.** `proto.Window.Within` and `SeedWindow` give and
   take the report list whole, and `remind_late` is rewritten at the engine's writes (n3f-b,
   n3f-c); nothing could call them yet. The refusal at a full list is the store's rule at encode
   (a write that would exceed a cap is refused loudly, and never rolls an in-window report away).
8. **The cap arithmetic follows QX6, not `listCaps`' `count x (size + 1)`.** The two lists are
   not in `listCaps` (it would add an entry-size rule: a report time is 13 digits to the year
   2286 but `validMS` allows 15). Their share is the spec's, in the test.

## What each test was shown to catch

Method. The test files were written first and run against a type-only skeleton (the struct fields,
no check, no `NewStore` default, no `MaxStore` change, the old id grammar): red for the right
reason in `TestFreshStoreWritesTheSevenKeys` (no `"reports":[]`), `TestEveryKeyOfTheQuestionsRoomIsRequired`
(null `reports` and `remind_late` accepted), all 21 rows of `TestStoreRulesOfTheQuestionsRoom`,
`TestPendingRoomQuestionsForHumanAndTimeout` ("a pending entry is not well formed": the reminder
key `<id>~humans`), and `TestStoreFullFitsTheFileCap` ("larger than its cap of 3145728 bytes").
Then the code. Then 37 defects planted one at a time in `store.go` by a scratch runner (not
committed, under `untracked/`), each run against the store tests and the file restored: **all 37
red** at the end. Four first-round mutants were wrong and fixed: two did not compile (an unused
variable) and were replaced by compiling ones, and the survivors in the list below led to dead
rules removed (decision 5) and to a ledger row for the reminder's item kind.

- `MaxStore` at 3 MiB, and at 4.25 MiB: red (`TestStoreFullFitsTheFileCap`).
- Each of the seven keys' JSON names renamed (and `count`, `said`): red (`TestFreshStoreWritesTheSevenKeys`,
  which pins the order and the contiguous top-level run `... "questions":null,"reminded_ms":0, "remind_late":[],"run":{`).
- `NewStore` with a null `reports` or `remind_late`: red.
- The decoder check for a null `reports`, and for a null `remind_late`, dropped: red.
- Each rule dropped in turn: `reports` count, a report time, `questions_unechoed`, `said` past
  `count`, a negative `said`, `reminded_ms` negative, `remind_late` count, zero, negative and a
  duplicate: red (`TestStoreRulesOfTheQuestionsRoom`, by the rule's own text).
- `Questions.Check` not called, called with another team's server, and its rule text left out
  of the refusal: red. `checkQuestionsRoom` not called from the outbox or from the store: red.
- The caps constants `capReports` 61 and `capRemindLate` 65: red in
  `TestStoreRulesOfTheQuestionsRoom` and `TestStoreHoldsTheQuestionsRoomListsAtTheirCaps` (the rows
  of `TestStoreListCapsFireFirst`, which grow a full store by one, are built from the constants and
  so do not see the constant change; they do see the rule dropped: `reports`, `remind_late`
  and `Questions.Check` dropped, each red there too).
- Reminder keys: the pending placement, the ledger placement dropped; `~humans` accepted in any
  room, for any item; any word taken as a reminder key; the word not accepted at all: red
  (`TestOnlyAReminderIsKeyedHumans`, `TestPendingRoomQuestionsForHumanAndTimeout`).
- `TestStoreListCapsFireFirst` gained seven rows for the new caps in a full store: `reports`,
  `remind_late`, and the index's caps through the store (a 65th question not closed, a 1025th
  post, a 65th unattached post, a 17th unanswered id, a 1025-byte token), each refused by its own
  rule before the file cap.
- Not shown by a planted defect: `TestWorstQuestionsKeyIsFull` is the test of the test (it
  asserts that every cap is full and the headline is written at 6 bytes a code point); it judges
  the fixture, not the store.

## Gates, exactly

- `golangci-lint run ./internal/teamchat/member/state/`: 0 issues (after `golangci-lint fmt`).
- `go build ./...` and `go vet ./internal/teamchat/...`: clean. `go test -count=1 ./internal/teamchat/member/...`: 28 packages ok (before the final rows).
- `go test -race -shuffle=on -count=1 -cover` over `member/state`, `engine`, `teamsync`,
  `keeper`, `link`: ok; coverage `state` 99.8%, `engine` 92.7, `teamsync` 95.2, `keeper` 91.2,
  `link` 92.3, and `wiring` ok at 100.0% (175 s under `-race`). The state package was run again
  with `-race -shuffle=on -cover` after the last test rows: ok, 99.8%.
- `./scripts/qa-changed.bash` over the 6 staged files (code, tests, PLAN.md, BUILT-n3.md and this
  report; this line was written after the run): exit 0. docs, go (go-qa, both modules, diff mode)
  and teamchat-keeps-off-v1 passed, and the output has no "tests failed in ." line, so the root
  module's tests passed. `ansible-syntax` and the four `qa-js` tests did not run for this diff.

## Findings

1. **A defect found, not fixed, that is the owner's to decide (n3d's sizing): `outbox.sending[].content`
   is sized in A's compact form and written HTML-escaped.** `proto.Value.Size()` (what the store's
   entry-size rule measures) writes only `"`, `\` and control characters escaped, but the store's
   bytes come from `encoding/json`, which writes `<`, `>`, `&` as 6 bytes and U+2028/2029 as 6. A send in
   flight whose content is 16 384 `<` (A 11.2's cap) passes the 20 KiB entry rule and the 16 384 content
   rule, and is written as **98 395 bytes**; 32 of them with the other lists full are refused only at
   the file cap ("larger than its cap"), so for that input "a list cap always fires first" (n3d-D1) does
   not hold. A realistic send near the cap with a fifth of its characters `<>&` (code, HTML, templates) is
   about 32 KiB written. I measured it with a throwaway probe test. The class: a cap measured in one form
   of a value and written in another. No project rule can be written (which `Size()` call guards free
   text is not decidable from syntax), so it is fixed conventionally, and the fix is a sizing decision:
   (a) size `sizeSending` and `MaxStore` for the escaped form (about 2.4 MiB more), (b) write the store with
   `SetEscapeHTML(false)` (still doubles U+2028/2029), or (c) measure the entry as written and refuse
   a send whose written form does not fit (refuses legal sends full of `<>&`). The `questions` key
   does not have this defect: QX6 sizes a headline at 6 bytes a code point, and the worst-case test
   writes it so.
2. **The store refused the reminder's key (fixed).** `validItemID` took a TIMEOUT key `<id>~<handle>`
   or `<id>~<human name>`, and `humans` is a reserved localpart, so `ValidHumanName("humans")` is
   false: every reminder TIMEOUT (`proto.ReminderKey`, RM1, RM2) would have been refused at its first
   store write. Found by the G2 item "check that Pending.Room takes questions for HUMAN and TIMEOUT
   items" (the room was always allowed; the id was not). The class is an id one package builds that
   another package's grammar refuses; no rule can state it, and the defence is the grammar test
   (`TestItemKeyGrammarTakesTheReminderWord`) over the forms `proto.TimeoutKey` and `ReminderKey` build.
3. `engine.clone` is a JSON round trip of the whole store on every `Load` and every change, and each
   write encodes and decodes it again; with the `questions` key full that is a store of about 3.4 MB
   copied twice and read back once per write. I did not measure it. 12.6's "4 MiB at worst" is the
   spec's own figure for 2.14's row; n3f-c, which makes the writes, should look at it.
4. **SPEC 12.6's example `"index": {}`** (also finding 10 of the codec report) is not a value the
   decoder takes: every key is required, as for the other keys.
5. `proto`'s and `member/state`'s `sameJSON` are the same function (the codec report's note); not
   merged here.

## Not done, and what the next builder needs

- What writes the seven keys: the report path (n3f-b: `reports` from `proto.Window.Within`), the
  questions sync, the build record's writes, `remind_late`'s rule at each page write, the ready line's
  `questions_handover` (n3f-c), and the engine's drop of the keys at a demotion or an epoch change
  (n3f-c). The store holds the shapes; it never reads the keys' meaning.
- `Pending.Room` for a PEER, PING or NOTICE item in the questions room was always allowed and has
  no test of its own beyond the existing ones.
- Finding 1 is open until the owner picks a sizing.
