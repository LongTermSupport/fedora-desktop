# n3f-a, G4 (records) and G5 (the sync filter): built

Branch `feat/bus-v2-n3f-a-records`, from `feat/bus-v2-n3f-a` (6a24d46ad). Builder: Sonnet 5.5.
Two code commits (G5 `f03f09c74`, G4 `30aa6f7d6`) and this report. G1 (the question index) and
G2 (the store) were not in this slice; another builder has the index.

## Built

| Part | What                                                                                                                                                                                                                                                                                                                                                                          | Where                                                                                                          |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| G5   | `SyncFilter(r, a, questionsReady)` and `SyncFilterReAdd(r, a, questionsReady)`: the questions room is in the filter only when the roster names this member the coordinator and the argument is true. A coordinator with `false` equals the worker's filter byte for byte, in both forms. Callers pass `true` (teamsync, `reach_test`, `filterpin_test`, the pinned JSON test) | `proto/filter.go`, `filter_test.go`, `reach_test.go`, `matrix/filterpin_test.go`, `member/teamsync/session.go` |
| G4   | `Question` (`headline`, `re`, `asked_ms`, `remind_after_s`, `unanswered_more`, `redacted` last; `id` and `asked_ms` nullable), kind `unattached`, `Human.Question` a nullable string and `Human.Headline` after it, `Room.Check` and `Room.Uncounted`, `Timeout.Question` after `silent` and `Timeout.Verb` nullable; `QuestionNone`                                          | `member/records/records.go`                                                                                    |
| G4   | Encoders that refuse: a redacted question holding more than its number, a question label that is not null, `none` (human only) or A 2.2's `Q<n>` (`proto.ValidQuestion`), a human headline with no `Q<n>` holding the post                                                                                                                                                    | `member/records/records.go` (`Human`, `Timeout`, `Question` `MarshalJSON`)                                     |
| G4   | The engine's timeout sets a verb only when the outbox holds the ping, else null                                                                                                                                                                                                                                                                                               | `member/engine/render.go`, and the two engine timeout tests                                                    |
| G4   | The skill names `unattached` (a minimal line under `questions`; n3f-e writes the section)                                                                                                                                                                                                                                                                                     | `member/kit/skill.txt`                                                                                         |

`engine/status.go` (`Room`) and `teamsync/verbs.go` (`Human`) needed no change: they build the
records with keyed literals, so the new fields are null. Plan state: `PLAN.md` Task 3.2 and
`BUILT-n3.md`.

## What each test was shown to catch

Each expectation's test was written first and seen red for the stated reason.

- **G5** `TestACoordinatorWhoseQuestionsRoomIsNotReadyHasTheWorkersFilter`: with the argument added
  to the signatures and ignored, red in both forms: "a coordinator, room not ready" printed a
  `rooms` list of main and the questions room where the worker's has main alone. Green after
  `&& questionsReady`. Planted `ok && coordinator == a.Self || questionsReady`: red on the
  worker's `true`/`false` equality, on the ready coordinator, on "the worker's filter names the
  questions room", and on the pinned `filter-worker.json` and `filter-probe.json`; reverted.
- **G4 byte pins** (`TestEveryKindEncodesAsBRendersIt` updated, `TestSection6RecordsAreBytePinned`
  new, 11 cases: a question, with a reminder and more unanswered than listed, queued, redacted;
  unattached; a human attached and one no question holds; a reminder's timeout and a ping's;
  a questions room with `uncounted` 0 and an untrusted reports room with `check`): written with
  the fields missing, so red as compile errors; green once the structs had the fields. Planted
  `AskedBy` before `Headline` in `Question`: red in four byte-pin cases and in
  `TestEveryKindEncodesAsBRendersIt`; reverted. This is the only evidence the order is judged.
- **G4 encoders** (`TestARedactedQuestionCarriesNothingButItsNumber`,
  `TestAQuestionLabelIsRefusedUnlessItIsOneOfTheThree`, `TestAHistoryItemKeepsItsQuestionChecks`):
  with the fields and no encoder, every refusal case encoded and the tests were red (9 leaks of a
  redacted question, 9 bad labels, the history item). Planted a redaction check that ignores
  `re`: red on exactly "a redacted question with a main id", reverted. Each of the nine leak
  cases is its own field.
- **Engine** `TestATimeoutRendersFromItsKeyAndTheOutbox` (extended: a timeout whose ping the
  outbox lacks has a null verb and ref, and no timeout of a ping has a question) and
  `TestSD4_9bAPendingTimeoutKeepsItsPing`: planted `r.Verb = nil` in `timeoutRecord`: both red;
  reverted.
- **Skill** `TestTheSkillNamesWhatTheBinaryHas`: red "SKILL.md misses the record kind
  unattached" with the kind added; my first skill line wrote `questions` as a code span and was
  red again ("the code span "questions" is no command and no record kind"); green with
  `teamchat questions` and `unattached` in spans.
- `TestAHostileBodyIsOneLineReadBackEqual` now reads the record back with `reflect.DeepEqual`
  (the field-by-field comparison it replaced had reached the cyclomatic limit with the new
  fields), so a hostile `question` and `headline` are also read back equal.

## Gate results

- `./scripts/qa-changed.bash`, run once over the staged tree of both code commits together
  (14 changed files; it queued behind another worktree's run first): docs, go (go-qa, both
  modules, diff mode on a git change) and teamchat-keeps-off-v1 passed; "qa-changed passed: 14
  changed file(s)", exit 0. The report's own commit ran `qa-changed` separately; its result is in
  that commit's message.
- `go test -race -count=1 -cover`: records 100.0, proto 98.3, kit 93.3, cli 98.9, teamsync 95.3,
  engine 92.7, matrix 98.9, all ok. `-shuffle=on` over records, kit and proto: ok.
  `golangci-lint run` over records, engine, proto, teamsync, matrix: 0 issues.
- `go test ./internal/teamchat/member/...` and `matrix/...` once: every package ok except
  `member/wiring`, which failed `TestClaudeLinkSilent` ("the link did not come up ... the keeper
  went before the launch") in a long run on a loaded host. It passed run alone, and the whole
  package passed on a second `-race` run. I did not run the package on the base commit, so this
  is a flake I judge environmental from those two runs, not a proven pre-existing one.
- Not run: the `busfailpoints` and `tuwunel` tagged stages, `go-qa`'s stages by name (they ran
  inside qa-changed's go-qa; I have no per-stage line), `qa-all.bash` (forbidden here).

## Not done, and what the next builder needs

- **Nothing sets the new fields.** `Human.Question`, `Human.Headline`, `Timeout.Question`,
  `Room.Check` and `Room.Uncounted` are null in every record the engine and teamsync build today.
  Their setters belong to the index (G1), the reminders (RM) and the status rooms (LN5), which are
  n3f-b to n3f-d. In particular a questions-room human is still rendered with a null `question`,
  which section 6 says is main's; and `status` emits the questions room with a null `uncounted`,
  which section 6 says is a number (the store key `outbox.questions_uncounted` does not exist
  until G2).
- **The encoder does not tie `question` to `room`** (null if and only if main). I left it out on
  purpose: the engine renders a `questions` pending item today with no question, and refusing it
  would turn those renders into failures before the slice that builds the questions-room
  delivery can set the field. The slice that does should add the check and its test.
- **`teamsync` passes `true`** to the filter. The caller that knows whether the questions room is
  ready (QS3) is n3f-b's; until then a coordinator's filter is what it was.
- **`records` now imports `proto` directly** (`proto.ValidQuestion`, `proto.QuestionClosed`);
  `records` already depended on it through `paths`, so there is no new cycle. The other builder
  is editing `proto/questions*.go`: if that work renames either name, `records` stops compiling
  at the merge.
- **No project rule was written.** The classes met here are a spec example that drifts from the
  built record (two keys omitted) and a string-typed field that needs a grammar. For the first,
  the existing detector is the byte-pin tests plus `TestEveryKindEncodesAsBRendersIt`, which
  fails for a kind in `records.Kinds` with no pinned case, and the skill test; a document that
  omits a key is not something an analyzer over Go can see. For the second, the encoder check is
  the fix and a rule would only restate "a string field has a grammar", which no analyzer can
  express in general. The `defence-before-fix:dbf` skill was not available; I followed the
  method in `CLAUDE.md`.

## Findings against the spec

- Section 6's `question` example omits `redacted`, which the bullet under it and G4 name. Built
  last, after `unanswered_more`.
- Section 6's `timeout` example omits `restored`, as B 2.2.2's example does; the built record has
  had `restored` last since n3b, and the new `question` and nullable `verb` sit before it.
- G4 lists `Room.uncounted` only; section 6's example also has `check`, and G4 lists neither
  `Timeout.question` nor the `Timeout.verb` change. All are built.
- `headline` of a redacted question is the empty string, not null ("every other field null or
  empty" allows either): it is a string field everywhere else, so a reader has one type.
- The previous report named `engine/status.go:331` and `teamsync/verbs.go:216` as callers that
  change; neither does, since both use keyed literals. Only `engine/render.go` changed.

## What the reviewer most needs to check

1. The bytes and key order in `section6_test.go` against section 6's examples, and the two
   decisions where the examples are short (`redacted` last; `restored` kept on the timeout).
2. Whether the three encoder refusals are right as contract: a redacted question with any
   content, a label outside null, `none`, `Q<n>`, and a headline with no `Q<n>`. The headline
   rule is the strictest; n3f-b must not need a headline on a `none` or main post.
3. That `SyncFilter`'s only change for every existing caller is the added `true`, and the
   `member/wiring` flake above.
