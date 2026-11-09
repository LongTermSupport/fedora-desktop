# n3f-a OW5 (b): a second list for agent thread posts and slots

Branch `feat/bus-v2-n3f-a-ow5b`, from `origin/feat/bus-v2-n3f-a-int` at 42a2fce75. One commit,
code and docs together; not pushed, not merged.

## What was built

OW5: (b), coordinator, 2026-11-05. The question index remembers the events it folded in two
lists, each with its own cap and its own eviction (oldest first, in stream order):

| List                                             | JSON          | Entry            | Cap                       | Holds                                                        |
| ------------------------------------------------ | ------------- | ---------------- | ------------------------- | ------------------------------------------------------------ |
| `IndexState.Posts` (`proto.PostEntry`)           | `posts`       | `{"id","q","t"}` | `MaxAttachments` 1024     | human posts; `t` 1 in its question's thread, else 0          |
| `IndexState.AgentPosts` (`proto.AgentPost`, new) | `agent_posts` | `{"id","q"}`     | `MaxAgentPosts` 256 (new) | agent thread posts (`q` the question) and held slots (`q` 0) |

- `src-go/internal/teamchat/proto/questions_fold.go`: the generic `ring[E]` (cap per ring) in
  place of `postRing`; `AgentPost`; `IndexState.AgentPosts` after `Posts`; `threadPost` records
  into the agent list; `Attach`, `threadPost` and `Slot` look in both lists (`folded`) before
  applying; rule 2 reads a human post only with `t` 1 and an agent post only while its
  question is kept; rule 4 reads human posts only. `postAgent` and `postSlot` are gone.
- `questions.go`: `MaxAgentPosts = 256`, the `agents` ring, `Slot` and `Attachment` on the two
  lists.
- `questions_state.go`: `MaxQuestionsStateBytes` 1 101 056 -> **1 125 888**; the codec refuses a
  null `agent_posts`, a 257th entry, an agent post id not an event id, an id repeated within
  `agent_posts` or across both lists, a negative `q`, and a `posts` kind other than 0 or 1;
  `LoadQuestionIndex` reloads both lists.
- `member/state`: fixtures carry `AgentPosts` (the small key has one, the worst key 256); a list
  cap row for the 257th agent post; `MaxStore`'s comment states the new as-written sum.

### The "t" field: kept, and why

Kept in `posts`, with 0 and 1 only. A 10.4 rule 2 attaches a reply "to a post in a question's
thread"; a human post attached by rule 3 (`Q<n>` prefix) or 4 has a question but is not in its
thread, so `{"id","q"}` alone cannot tell rule 2 whether to follow it. The mutant that drops the
distinction ("rule 2 any human post") is red. `agent_posts` needs no kind: `q` > 0 is a thread
post, 0 a slot. The 83-byte `{"id","q","t"}` entry was already inside the 96 the sum sizes, so
the human list's share is unchanged.

## The sums (recomputed, not copied)

- Agent list: 256 x (96 + 1) = 24 832. Entry at its longest: `{` 1 + `"id":` 5 + 46 + `,` 1 +
  `"q":` 4 + 19 + `}` 1 = 77, sized 96. Human entry with `t`: 83, sized 96.
- `questions` key: 192 x 5121 + 1024 x 97 + 256 x 97 + 64 x 257 + 2048 = 983 232 + 99 328 +
  24 832 + 16 448 + 2048 = **1 125 888** (= 1 101 056 + 24 832); pinned by
  `TestQuestionsStateCapIsTheSum`. The list key `,"agent_posts":[]` (17 bytes) is inside the
  2048 of fixed fields (about 1320 with it).
- n3f adds 1 125 888 + 856 + 1296 + 256 = 1 128 296; with n3d's 3 001 136 the sum is
  **4 129 432**, rounded up to 4 194 304: `MaxStore` stays 4 MiB, 64 872 under.
- As written (the store's sizing since #164): 4 110 165 + 24 832 = **4 134 997**, 59 307 under
  4 MiB; `TestStoreFullFitsTheFileCap` logs exactly this.
- Measured: the full store is 3 447 379 bytes (OW5 (b) had estimated 3 452 995 from the sized
  96; written entries are 77), its `questions` key 1 020 775 of at most 1 125 888 (both the
  store's and proto's worst-case tests).

## Spec gaps decided (folded into SPEC-n3f QX6 "The two lists of folded events")

1. `posts` keeps `t` (0/1), for rule 2 (above).
2. An event id is in at most one list: the fold checks both before applying any event (a slot
   fed as a human post, a human post fed as a slot or thread post, change nothing), and the
   codec refuses an id in both.
3. A human post fed again whose id is an agent thread post returns that post's question, as
   before (unchanged answer of `Attach` for an event already folded).
4. Rule 4 (an edit) takes the original's attachment from human posts only; an "edit" naming an
   agent post id attaches by its new body's `Q<n>` or to none.
5. Past a cap an evicted event fed again applies again, live and rebuilt alike; a re-feed is at
   most a page or an answer behind, inside 256 agent events. Stated in QX6.
6. `agent_posts` is written after `posts`.

## Tests: red, then green

New `proto/questions_agentposts_test.go`: the newest 256 agent posts kept and the 257th oldest
fed again counts; slots share the list (257 slots: 256 kept, the evicted one counts again);
1024 human posts survive 512 thread posts and 512 slots; a thread post fed again after 1124
later human posts resets nothing, and a reply to it attaches by rule 2; an id folded as one kind
only; `Copy` holds both lists, evicts on its own and shares nothing; rules 2 and 4 read each list
for what it holds; the sum; the two JSON forms. Reworked: the codec fixture and rule rows
(`questions_state_test.go`), the worst case with 256 agent posts (`questions_worst_test.go`).

The property test (`questions_property_test.go`) now fires an "agent posts" cap too (every seed,
2600 events) and feeds the restarted index again: each page's last 0-6 events (as before), after
one event in four one event of the last 40 (so thread posts come again after later human posts),
and after every page the oldest entry each of the two lists holds. Live equals rebuilt after
every page, for seeds 1-3.

- Red 1: the new tests did not compile (`AgentPosts`, `MaxAgentPosts`, `AgentPost` undefined).
- Red 2, with the type and field stubbed and the old fold behind them: 6 new tests failed
  (KeepsTheNewestAgentPosts, SlotsShareTheAgentPostsList, AgentPostsLeaveTheHumanPostsAlone,
  AThreadPostFedAgainResetsNothing, CapIsTheSum, WritesTheTwoListsInTheirForms), the property
  test failed on every seed ("the agent posts cap never fired"), FixtureHoldsEachPostKind and
  the new rule rows failed.
- Green: `go test ./internal/teamchat/proto/ ./internal/teamchat/member/state/` ok.

## Mutation pass

`untracked/scratch/ow5b/mutate.py` (not committed): 67 mutants of the changed codec and fold, run
through `go test -overlay` (proto, then member/state for a proto survivor), covering the cap
bytes, the second list's cap, null, id, repeat (within and across lists), `q` checks, the human
kind rules, the load, both rings' caps and eviction order, entries and clone, the JSON tags, the
State/Copy wiring, and every re-feed path (thread post, slot, Attach, `folded`, rules 2 and 4).
First pass 58 of 67 red; 9 survivors, 8 killed by the tests added after
(`TestQuestionIndexCopyKeepsBothLists`, `TestQuestionIndexRulesTwoAndFourReadEachList`, a
stronger `Attachment` check): **66 of 67 red**. The one survivor, "ring compacts every add"
(`r.head >= r.limit` -> `r.head >= 1`), is equivalent: it only changes when the backing slice is
re-cut, never what the ring holds.

## Commands and results

From `src-go`:

- `go test -race -count=1 ./internal/teamchat/...`: 66 packages ok, exit 0.
- `golangci-lint run ./internal/teamchat/...`: 0 issues (and again over `proto` after the last
  tests).
- `go -C src-go/qa build -o untracked/scratch/ow5b/driver ./driver`, then
  `go vet -vettool=<driver> ./internal/teamchat/...` and the same with `-tags busfailpoints`:
  both clean.
- `./scripts/qa-changed.bash` (background, behind the QA lock): passed, exit 0: `docs` whole gate, `go` go-qa over both modules (diff mode), `teamchat-keeps-off-v1` whole gate; 13 changed files. Neither environmental failure (qa-js TestReal\*, ansible-syntax) was routed for this diff.

## Uses in the stacked refs that this change affects

`git grep` over `origin/feat/bus-v2-n3f-{b,c1b,c1c,c1d,c2a}-int`. No ref changes `proto/`
relative to n3f-a-int (`git diff --stat` empty for c2a, which contains the others), so the proto
files merge clean. Outside `proto`, the same sites in every one of the five refs, all tests in
`member/state` and all edited on this branch:

- `member/state/store_questions_test.go:21` and `:31` (`questionsKey`: needs `AgentPosts`, else
  "agent_posts is null"), `:401` and `:414-415` (`worstQuestionsKey`: 256 agent posts added),
  `:436-438` (`TestWorstQuestionsKeyIsFull`: now through `worstListsFull`, cyclop).
- `member/state/store_test.go:328-331` (`MaxQuestionsStateBytes`, value only) and `:467` in
  every stacked ref (`:445` here; the "questions posts" row; a new "questions agent posts" row
  follows it). The refs' only other change to this file is `TestN3fAReportInFlightRoundTrips`
  (n3f-b), in another hunk.
- `member/state/store_written_test.go:245` (`MaxQuestionsStateBytes` as the questions row; no
  edit needed).
- `member/state/store.go`: the stacked refs change other lines (`SendReport`); this branch
  changes only the `MaxStore` comment.

Unchanged and still valid everywhere: `PostEntry{ID, Q, T: 1}` literals, `proto.MaxAttachments`,
`Attachment`, `Attach`, `Feed`, `Slot`, `State`, `Copy`, `LoadQuestionIndex` signatures. The
teamsync tests in c2a that call `idx.Attachment` (`builder_test.go:186,235,296`,
`questions_gap_test.go:183,213,216`, `questions_receive_test.go:178,238`) ask about human posts;
none relies on agent posts evicting them, but they were not run on the merged result here. A
seat store written by a stacked branch before this change has no `agent_posts` and is refused
by the strict decoder (no seat has written one yet, per OW5).

## Not in scope, noted

- Pre-existing, not changed: `count` re-counts an admin's carried question fed again after the
  open or closed cap evicted it (no id memory for evicted questions); the property test's
  re-feed window does not reach it.
