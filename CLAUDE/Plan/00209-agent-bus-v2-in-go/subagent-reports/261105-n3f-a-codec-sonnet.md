# n3f-a, the rest of G1: the question index's strict codec, build record, `RedactedSlot` and the property tests

Branch `feat/bus-v2-n3f-a-codec`, from `feat/bus-v2-n3f-a-int` (head 2e261a410). All four items of
the slice are built; G1 is whole. The store (`member/state`, G2) is untouched.

## Built (all in `src-go/internal/teamchat/proto/`)

- `questions_state.go`: `QuestionsState{Index, Token, Last, Build}` (pointers, so null is
  representable), `BuildRecord{Start, After, LateFrom}`, `Encode(server)`,
  `DecodeQuestionsState(data, server)`, `QuestionsState.Check(server)`, `IndexState.Check(server)`,
  `LoadQuestionIndex(server, floor, IndexState)`, `QuestionsStateError{Code, Rule}` (codes `json`,
  `schema`; never quotes a value from the document; `errors.Is(err, ErrQuestionsState)`),
  `MaxQuestionsStateBytes` (1 101 056). Decoding is `member/state/strict.go`'s: the cap,
  `proto.Parse` (repeated keys, integers past 2^53, bad UTF-8, trailing content), an object,
  `DisallowUnknownFields`, a round trip that must equal the parsed value, then `Check`.
- `redacted.go`: `RedactedSlot(raw Value, server string) (id string, ok bool)`.
- Fold fixes found by the codec tests (see Findings 1): `IndexedQuestion.clone()` and `State()`
  now give `[]` for an empty list (`cloneList`, `questions.go`), and `count` copies `re` with it.
- Tests: `questions_state_test.go` (codec, load, build record), `redacted_test.go`,
  `questions_property_test.go` (live equals restarted), `questions_worst_test.go` (QX6's worst
  case).

## Valid combinations of the build record (QX5), as built

| key `build`        | `index`, `token`, `last` | `start`           | `after`             | `late_from`         |
| ------------------ | ------------------------ | ----------------- | ------------------- | ------------------- |
| null (at the head) | all set                  |                   |                     |                     |
| owed               | all null                 | null              | set (an event id)   | null or 1 to 2^53-1 |
| under way          | all set                  | set (an event id) | null or an event id | null or 1 to 2^53-1 |

## What each test was shown to catch

Method: the tests were run first against a compile-only skeleton (types and no-op methods): 9
tests and 109 subtests failed on their assertions (a decoder returning an empty state, an
encoder returning no bytes, a load returning an empty index). Then the code. Then defects planted
one at a time by `untracked/scratch/mutate.py` (scratch, not committed; each mutant run against
the named tests, the file restored): **71 mutants of the codec, all red** at the end (4 of the first 70 were survivors or build failures and
were rerun after the fixes named below).

- Every rule of `Check` (every `c.need(` line, 52 of them): dropped in turn, each turns the
  codec tests red (the table `ruleRows`/`TestQuestionsStateBuildRecord` names the rule by its text,
  so a different rule refusing first does not pass the row).
- The decoder's stages: syntax errors not refused; the size cap; `Check` not run by Decode, by
  Encode's read-back, by Load; the round trip dropped; each red. Two stages overlap another by
  design and were first **not** red alone: the "not an object" rule (an array or null is also
  refused by the decode into the struct and the round trip, with the same code) and
  `DisallowUnknownFields` (an unknown key is also refused by the round trip). The tests now tell
  them apart by the rule's text, and both turn red alone; dropping both stages together was red too.
- Load: the floor ignored, the slots and the evicted count dropped, no roots, a held entry given a
  root (a post with no thread root then attached to a redacted number), no posts, no unattached
  list, the questions' lists and the unattached list shared with the caller: all red.
- `cloneList` keeping a nil list nil: red.
- `RedactedSlot`: 10 mutants (type, event id, state key, agent refused, admin refused, any sender
  accepted, sender not classified, any redactor accepted, redactor not compared with this server's
  admin, the redaction path not followed): all red. The judge test shows the same raw event gets
  `Drop`/`schema` from `Judge.Judge` and `ok` from `RedactedSlot`.
- The live-equals-restarted property (3 seeds, 2600 events each, pages of 60 to 459, the state
  encoded, decoded and loaded after every page and the last 0 to 6 events folded again): planted
  into the fold and the codec, red for: the thread-post and the attach idempotence guards removed,
  the unanswered cap off by one, the unattached list not capped, a question's root kept when it is
  evicted (first a survivor: the generator only posted into recent threads; fixed by adding posts
  into old threads), the slot idempotence guard removed, and Load building no roots, no posts, no
  slots, no evicted count or a held entry's root. The test asserts that each cap fired in the live
  fold (open evicted, lowest closed evicted, the 17th unanswered post counted, the oldest of 1024
  posts dropped, the oldest of 64 unattached dropped) from the state's own change at the step, so a
  generator that stops overflowing a cap fails the test.
- QX6's worst case: 14 mutants (the key's cap under the worst size, the token cap, a tilde in the
  token refused, headline, `asked_by`, `re`, unanswered, open, closed, posts and unattached caps one
  short, the reminder bound, `late_from`'s bound): 13 red under the worst-case tests; the 14th
  (an owed build record refused) is the build-record test's, which is red for it (the `need` run).
  The full state is 1 001 559 bytes with `encoding/json` (at most 1 101 056) and round-trips.

Not shown by the property test, and covered elsewhere: the plain reply clearing a listed post
and the slot guard for a counted question's id (both live and restarted hold the same wrong
state; the fold's own tests plant them, `questions_fold_test.go` and `questions_slots_test.go`).

## Gates, exactly

- `golangci-lint run ./internal/teamchat/proto/`: 0 issues (after `golangci-lint fmt`).
- `go vet ./internal/teamchat/...`: clean.
- `go test -race -shuffle=on -cover ./internal/teamchat/proto`: ok, 98.3% of statements. The
  uncovered statements are the two `json.Marshal` error branches (the types cannot fail to
  marshal), `questionNumber`, `PlaceQuestion` and `PlaceThread` (the earlier builder's).
- `./scripts/qa-changed.bash` over the 11 staged files (this report's last edit, this line, came
  after the run): exit 0. docs, go (go-qa, both modules, diff mode) and teamchat-keeps-off-v1
  passed, no "tests failed in ." line. The four qa-js tests and `ansible-syntax` (known
  environmental failures) did not run for this diff.
- The property test costs about 9 s under `-race` (3 seeds); the whole `proto` package took 11 to
  104 s under `-race -shuffle=on` on a loaded host. The `teamchat-failpoints-test` stage runs every
  test twice.

## Findings

01. **A defect class found: a list that must be `[]` came out `null`.** `State()` gave a nil
    `unattached` for an index with none, and `slices.Concat([]string{}, nil)` (the fold's attempt
    to make `re` non-nil) and `slices.Clone(nil)` give nil, so an index folded from a question with
    no `re` encoded `"re":null`, which the codec rightly refuses. Fixed as a class by construction:
    `cloneList` is the one copy that is never nil, used by `clone()`, `State()` and `count`. **No
    project rule**: "a slice meant to be non-nil" is not expressible by a detector (nil and empty
    are both valid Go, and the intent is only in the stored shape). The defences are
    mechanical instead: `Check` refuses every null list and `Encode` reads its output back, so a
    writer fails fast, and the property test encodes `State()` after every page of a 2600-event room;
    `TestQuestionsStateOfAFreshIndexAndOfBareQuestionsEncodesNoNull` pins the three constructors
    that had it. The owner may still want a rule; I could not state one.
02. **`Encode` takes the server**: `Encode(server)`, not `Encode()`, because `Check` does (user
    ids are validated against the server). `member/state` embeds the struct and runs
    `Questions.Check(server)` in its own check.
03. **The token's character set is stricter than `member/state`'s `syncTokenRe`** (`[\x21-\x7e]+`):
    the spec says "characters that need no JSON escape" so that 1024 bytes is 1024 bytes in the
    file (QX6's worst case). I took printable ASCII without `"`, `\`, `<`, `>`, `&` (encoding/json
    escapes the last three) and without space. A homeserver token holding one of these would make
    the key unwritable; the engine (n3f-c) must fail fast on it. Real tokens (alphanumerics, `_`,
    `-`, `~`, `.`) pass.
04. **`last` is required with an index.** Smallest reading: an index at the head, and a build
    under way, hold token and last event together; a room always has `m.room.create`. `build` null
    with any of the three missing is refused.
05. **An owed build may have `late_from` null**, because QX5 keeps `late_from` null when a
    redaction restarts a build begun with no key. An owed build always has `after`: it is "the last
    event of the page that met the redaction". Under way, every combination of `after` and
    `late_from` null or set is valid.
06. **`highest + slots + evicted + 1` must be at most 2^53-1** (the report said "their sum below
    2^53"): the `+1` so that `Next()` is itself a safe integer; the difference is unreachable.
07. **`Check` keeps to the earlier report's list; one extra it did not name:** `IndexState.Check`
    is exported (Load runs it, and a store may). A post's `q` is not required to be at most
    `highest` (a corrupted `q` is harmless to the fold); `n` is not bounded by 999 999 999
    (`maxQuestionN`), which content decoding already holds for any real question.
08. **`RedactedSlot` is the simplest form of the contract.** `unsigned.redacted_because` must be an
    object whose `sender` is the admin: "an object" is implied by having a `sender`, so it is not a
    separate condition (a test row, `redacted_because: true`, covers it). It does not read
    `origin_server_ts` or `content`: the judge's `schema` drop of a redacted event is what the
    engine checks first, and the function judges only the slot's own facts.
09. **Idempotence stays "within what the index still keeps".** The property test's overlap is the
    last 0 to 6 events of a page, far inside the 1024 posts; an admin's question re-fed after it
    was evicted would be counted again (the earlier report's finding), so the generator's admin
    questions always carry a number above the highest.
10. **`SPEC-n3f` 12.6's example** shows `"index": {}`; the codec refuses `{}` (every key is
    required), as it does every other missing key. It is an illustration of the key's place, not a
    value; the example is not changed (design documents are not mine).
11. **Lead's decisions.** The posts entry's `t` stays the integer kind 0 to 3, now held by the
    codec (`t` outside 0 to 3 refused; kinds 1 and 2 need `q` > 0, a slot has `q` 0). Where the
    spec is open the smallest reading that keeps QX5 (a) to (i) satisfiable was taken: findings
    3 to 6 and 8 are those places.
12. `.golangci.yml` is unchanged. The property test uses its own LCG (`splitmix`) because `gosec`
    flags `math/rand/v2` in tests (G404) and `proto/*_test.go` is not excused; a seed names a room for
    ever whatever the Go release.

## Not done, and what the next builder needs

1. **G2, the store** (`member/state`): embed `proto.QuestionsState` as the `questions` key; the
   store's own check calls `Questions.Check(server)`; the store's `MaxStore` rises to 4 MiB with
   `TestStoreFullFitsTheFileCap` holding the new sum; the `questions` key at most
   `proto.MaxQuestionsStateBytes`. `member/state`'s private `sameJSON` and `proto`'s are the same
   function; one may be dropped for the other later.
2. **The engine's use of the build record** (n3f-c): when a record is written, the `after` and
   `late_from` rules of QX5, the load that resumes a build. The codec only holds the valid shapes.
3. A fresh seat's **`questions` key absent** is not a `QuestionsState` (all-null with no build is
   refused): the store holds the key as absent, not as an all-null value.
