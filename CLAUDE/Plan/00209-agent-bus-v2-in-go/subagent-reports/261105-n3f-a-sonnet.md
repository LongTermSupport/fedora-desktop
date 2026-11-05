# n3f-a (Plan 00209 Task 3.2): a partial slice, G6 and G7 built, the rest handed on

Branch `feat/bus-v2-n3f-a`, from `origin/feat/bus-v2-n3e-wire` (fab5444ad). Builder: Sonnet 5.5.
This slice is **not complete**. The builder's context passed its budget after the two small
parts, so it stopped at a clean point instead of leaving a half-built index. Read "Not done".

## Built

| Part | What                                                                                                                                                                                                                                                                                                                          | Where                                                          |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- |
| G7   | `CodeNotReady` (`not-ready`) in exit 20's set, `CodeOpenLimit` (`open-limit`) in exit 10's; row 20's meaning and next step as SPEC-n3f 12.5 words them                                                                                                                                                                        | `member/exits/exits.go`, `exits_test.go`                       |
| G6   | `proto.SeedWindow(key, times)` (sorts a copy, oldest first), `Window.Oldest(key, span, now)`, `Window.Within(key, span, now)` (a copy of the times to store), `proto.DetailReportPerHour`, the detail by which a caller maps the report window's `rate` refusal to 14 `report-rate` and the bucket's (no detail) to 14 `rate` | `proto/limits.go`, `send.go`, `limits_test.go`, `send_test.go` |

## What each test was shown to catch

Each test was written first and seen red for the stated reason, then green; the planted defects
were run and reverted.

- `TestNotReadyAndOpenLimitAreInTheirExits` and `TestTableIsBsTable` (new expectations): with the
  two code constants added and the table not yet changed, 6 failures: `ExitOf("open-limit")` and
  `ExitOf("not-ready")` not found, exit 10's and 20's sets short, row 20's meaning and next step
  lacking the new words, and `exits.New(CodeNotReady ...)` panicking "unknown code". Green after
  the table row edits. Fixes: the skill's class lines (`kit/skill.txt`) are unchanged because the
  four classes and their exits are unchanged (exit 20 was already `wait`, exit 10 `fix`); the
  `cli` and `kit` packages pass with the new table.
- `TestTheReportWindowIsSeededFromTheStore`: planted `SeedWindow` storing the times unsorted: red,
  "Oldest = 3000 true, want 1000: the seed is sorted, oldest first". Planted `Oldest` reading
  the window's list without the span: red, "after the oldest left, Oldest = 1000 true, want
  2000" (the planted build also drew a lint finding, `spanMs` unused, which is the same defect
  seen by the linter). Both reverted, green.
- `TestAdmitSaysWhichRateRefused`: asserts the window's refusal has the detail
  `DetailReportPerHour` and the bucket's has none, and that a report the bucket refused never
  entered the window. Not planted separately; the detail was an untested string literal before
  this slice, and the test now pins it by the exported constant.

## Gate results

`./scripts/qa-changed.bash` was started over the staged G6 and G7 tree and **did not run**: it
queued on the shared QA lock behind another worktree's `qa-changed` that had been running for 45
minutes when the builder stopped, and printed only "still waiting for the QA run" lines (6
minutes of them in its log at the last look, 20 minutes after the start). Its process was left
waiting; if it finishes, its log is `untracked/scratch/qa-changed-1.txt` in the builder's
worktree and its result is not part of this report. So the commit's gate is **not** a qa-changed
pass. What was run instead, over the touched packages only:

- `go test -race -count=1 -cover` over `proto`, `member/exits`, `member/cli` and `member/kit`: all
  four ok. Coverage: proto 98.3, exits 98.9, cli 98.9, kit 93.3 (all above 90).
- `golangci-lint run ./internal/teamchat/proto/ ./internal/teamchat/member/exits/`: 0 issues.
- Not run: `-shuffle=on`, the `busfailpoints`/`tuwunel` tagged stages, the project rules driver
  over the tree, `go-qa`'s dead-code and vulnerability stages, the docs gate over the changed
  markdown. The wiring package (a caller of `exits`) was not run.

## Not done, and what the next builder needs

G1, G2, G4, G5, and section 6's codecs are not started. What was learned reading the spec and
the tree, so the next builder does not repeat it:

1. **G5** (`proto/filter.go`): `SyncFilter(r, a)` and `SyncFilterReAdd(r, a)` become
   `SyncFilter(r, a, questionsReady bool)` and the same for the re-add form; `syncFilter` adds the
   questions room only when the roster names this member the coordinator **and** the argument is
   true. Callers: `member/teamsync/session.go` lines 181 and 185 (pass `true`, behaviour
   unchanged), `proto/filter_test.go` (pinned JSON in `proto/testdata`; add a coordinator with
   `false` equal to the worker form byte for byte), `proto/reach_test.go:149` and
   `matrix/filterpin_test.go:44`.
2. **G4** (`member/records`): `Question` gains `headline`, `re`, `asked_ms`, `remind_after_s`,
   `unanswered_more`, `redacted`, with `id` and `asked_ms` nullable; the section 6 example shows
   the key order `team, n, state, id, headline, asked_by, re, asked_ms, remind_after_s, unanswered, unanswered_more`; the example **omits `redacted`**, which the bullets and G4 name
   (put it last; a finding against the spec). `Human.Question` becomes `*string`
   (`"Q4"`, `"none"`, null) and `Human` gains `headline` (`*string`) after `question`. A new kind
   `unattached` (`team, id, from, sent_ms`) joins `Kinds`. `Room` gains `check` (`*string`) and
   `uncounted` (`*int64`), though G4 names only `uncounted` (the section 6 example shows both).
   `Timeout` needs `question` (`*string`, after `silent`) and `verb` as `*string` (a reminder has
   none), which G4 does not list but section 6 shows; this changes `engine/render.go`
   (`timeoutRecord`, `Human`), `engine/status.go:331` (`Room`), `teamsync/verbs.go:216` (`Human`)
   and their tests. **The skill test** `TestTheSkillNamesWhatTheBinaryHas` fails for any kind in
   `records.Kinds` the skill's code spans do not name, so the commit that adds `unattached` must
   also name it in `member/kit/skill.txt` (a minimal mention; n3f-e writes the section).
3. **G1** (`proto/questions.go`, the large part). Reading QX6's byte sums, the stored entry shapes
   are fixed by them, and the key names reproduce its sums exactly:
   - a question: 15 bytes of braces and commas plus 131 of keys are exactly the 13 keys `n`, `id`,
     `latest`, `headline`, `hash`, `asked_by`, `re`, `asked_ms`, `remind_after_s`, `closed`,
     `attached`, `unanswered`, `unanswered_more` (4+5+9+11+7+11+5+11+17+9+11+13+18 = 131); the
     values sum to 4729 bytes. A redacted entry (QX7) can be the same struct with `hash` and
     `headline` empty and `id` empty (so `Redacted()` is derived, no 14th key).
   - an attachment is `{"id":...,"q":...}` (77 bytes), an unattached post `{"id":...,"from_id":..., "sent_ms":...}` (224 bytes, exactly the sum QX6 gives).
   - the fixed fields (token at `maxNextBatch`, last folded event, the floor, the highest counted,
     the slot count, the eviction count, the `build` record's `start`, `after`, `late_from`) fit
     the 2048 bytes QX6 allows.
   - **Finding against the spec (QX6 and A 10.4 rule 2):** rule 2 attaches a reply to "a post in a
     question's thread", which the current index keeps in `threads` (agent thread posts, and
     human posts whose thread root is a question). QX6's byte sum has no list for it. The smallest
     reading that keeps the existing attachment tests (`TestQuestionIndexAttachment` cases `r`
     and `k`) green and the sum within `MaxStore`: hold those ids in the same 1024-entry list as
     the human attachments, each entry carrying a thread flag (`{"id","q","t"}`: at most 87
     bytes, inside the 96 the sum sizes). The report to the coordinator should say that QX6's
     "the 1024 most recent human posts' attachments" then also counts agent thread posts.
   - `IndexedQuestion.Sender` has no use in the spec's stored fields; check its callers
     (`grep -rn '\.Sender'` over `proto/questions*` and the callers of `Question(n)`) before
     dropping it.
   - `Unanswered` becomes the ids (oldest first, 16 kept, the count past them), reset by an agent
     thread post; `Attach` and `Feed` must be idempotent by event id; attachment must not require
     a `Deliver` verdict (n3f-D18); the judge drops a redacted event `schema` before the index
     sees it (`proto/verdict.go` `decodeEvent`), so QX7's slots need either a new judge outcome or
     a function over the raw event (`unsigned.redacted_because` present, sender an agent account
     or the admin) that the build and the sync both call; that is an API the spec leaves open.
4. **G2** (`member/state/store.go`): the seven keys of SPEC-n3f 12.6 (`outbox.reports`,
   `outbox.questions_unechoed`, `outbox.questions_uncounted` as `{count, said}`,
   `outbox.questions_handover`, top-level `questions` as `{index, token, last, build}` (null when
   the seat has no index; `index`, `token` and `last` null for a build owed), `reminded_ms`,
   `remind_late`), each strictly decoded (the store's decoder refuses a key the struct lacks and
   one it does not round-trip, so every key is always present); `Pending.Room` already allows
   `questions`. `MaxStore` goes from `12 * 256 << 10` to `16 * 256 << 10` and
   `TestStoreFullFitsTheFileCap` must hold the new sum (4 104 600 bytes, QX6) rounded up. The
   store's `Version` is not raised, as n3d decided (no store exists outside tests).
5. Environment notes: `untracked/scratch` does not exist in a fresh worktree (`mkdir -p` it before
   redirecting qa output there); the pipe-to-tail/head hook blocks `go test ... | head`, so run
   `go test` unpiped or through `echd-capture`; the host's load average was 25 on 8 cores.

## Findings

- The spec's section 6 example of the `question` record omits `redacted`, which the bullet under
  it and G4 give; and its `room` example has `check`, which G4 does not list; `timeout`'s `verb`
  becomes nullable, which G4 does not list.
- QX6's byte accounting has no list for A 10.4 rule 2's thread posts (see 3 above).
- SPEC-n3f section 0's table says exit 20's base set is `unreachable` alone in 12.5's "Old"; the
  built table also holds `forge-down` (n3d), so the new set is `unreachable`, `forge-down`,
  `not-ready` and the next-step text keeps n3d's forge-down clause.
