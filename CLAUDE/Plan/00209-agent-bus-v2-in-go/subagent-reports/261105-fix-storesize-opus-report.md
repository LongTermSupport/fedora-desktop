# Fix: the seat store's caps measured as written (option c)

Branch `fix/store-size-as-written`, from `origin/merge/pr157-onto-f44`. Production code in
`member/state` and one step in `member/engine`'s send. Nothing pushed or merged.

## What is built

1. **One encoder, every size through it** (`member/state/strict.go`): `marshal` is the package's
   only encoder (`encode` and `sameJSON` call it), and `writtenSize(v)` is `len(marshal(v))`.
2. **The store's entry rule counts the bytes written** (`store.go`): `listCap` gains `entries`,
   each list's entries in a `Store`; `checkLists` holds each list's count and each entry's
   `writtenSize` against its cap. It replaces DecodeStore's measure of `proto.Value.Size()` over
   the parsed file (compact form) and the hand-ordered `counts` slice in `Store.check`. Because
   the check is on the typed value's re-encoding, a store written by any other writer is judged
   as this writer would write it. `decodeStrict` no longer returns the parsed value (its only
   user was the old size loop; `unparam` required the signature change in nine codecs).
3. **The verb refuses before the charge** (`engine/send.go`): after `sendingOf`, the engine asks
   `entry.Fits()` (`state.Sending.Fits`, the store's own rule through `writtenSize`); on
   `state.ErrTooLarge` the answer is `refusal(st.id, &proto.RefusalError{Reason: ReasonSize})`,
   the same exit an over-size content gets from the composer: **exit 10, code `size`, message
   "the send was refused (size); fix the message"**. Nothing is charged, nothing sent.
4. **The sweep's two other defects fixed**: `MaxRosterCache` 65 KiB -> `6*65536 + 1<<10`
   (385 KiB) and `MaxSession` 8 KiB -> 16 KiB (below).
5. **Docs**: the class in `CLAUDE/GoProjectRules.md` under "Defect classes that cannot be a
   rule"; PLAN.md Task 3.2 and BUILT-n3.md one line each; a JOURNAL action entry (mkplan).

## User-visible effect

A send that was accepted before and is refused now: a `say` or plain `reply` whose text is
heavy in `<`, `>`, `&` (or U+2028/U+2029). The text appears twice in the composed content
(`body` and `teamchat.text.text`), so each such character costs 10 more bytes in the entry; by
arithmetic (not measured) a text at the 4096-byte cap is refused once roughly 1 150 or more of
its characters are `<>&`, about 28%; a text of 4096 `<` is refused (measured). Short texts,
code with ordinary density of `<>&`, and pings (references and verbs are grammar-safe) are
unaffected. Before the fix such a send was accepted and written; 32 in flight at the content
cap broke the file cap and every later store write failed with "the store was not written".

## Red/green evidence

Raw captures: `untracked/scratch/red-state.txt`, `red-files.txt`, `plant-*.txt` (not tracked).

- `TestEveryListIsCappedAsWritten` (new, `state/store_written_test.go`), red on the base:
  `sending: an entry of 16373 × "<" is accepted and written as 101263 bytes, over its cap of 20480`; the same for `>` and `&`; `5457 × " "` written as 35 767; `awaiting: an entry of 1017 × "<" is accepted and written as 6345 bytes, over its cap of 2252`; `339 × " "` as
  2 277; and for `<`: `a full store with 32 entries ... is refused: ... larger than its cap of 3145728 bytes` and one over the largest accepted refused by the file cap, not the entry rule.
  `TestEveryListAtItsWorstFitsTheFileCap` red with the file-cap refusal. Green after the fix.
- Planted: `checkLists` measuring `proto.Parse(marshal(entry)).Size()` (the old compact
  measure): 23 failure lines, red. Reverted, green.
- `TestSendingFitsIsTheStoreRule`: planted `Fits` bound `6*sizeSending`: 5 failures, red; the
  engine test then answered 78 "the store of team ops was not written" instead of 10. Reverted.
- `TestASendTooLargeAsWrittenIsRefusedAsSize` (new, `engine/send_written_test.go`): with the
  base state and engine, `{Exit:0 Error:<nil>}` (accepted); with the state fix alone, exit 78
  `store` (the write refused); with both, green: 10 `size`, the outbox and gate unchanged,
  nothing sent, and a 4096-byte text of `a` still sent.
- `TestEveryListOfTheRegistryHasARow`: planted a ninth `listCaps` row: red ("has no row in
  freeText"). `TestEveryFixedFieldHasARow`: planted the `sync.next_batch` row removed: red.
- `TestTheLargestRosterIsCachedAsWritten`, `TestTheLargestSessionIsWritten`
  (`state/files_written_test.go`), red on the base: the roster cache of `<` and of U+2028
  `larger than its cap of 66560 bytes`; session.json of `<` `larger than its cap of 8192 bytes`
  (written 9 605). Green after the cap changes.
- Non-ASCII multibyte (`é`, `€`, an emoji) is **not** red: encoding/json writes UTF-8 as it is,
  as A's compact form does. Only U+2028 and U+2029 among non-ASCII are written longer.

## The class and the detector

Class: a size or cap computed from another encoding than the one written (compact against
HTML-escaped JSON, a Go `len` against a JSON length, characters against bytes, runes against
bytes). **No detector**: written up in GoProjectRules.md: which call measures the value a later
write writes, and in which encoding, is a data flow between two places the syntax tree does not
join, and flagging every `Size()`/`len` near a cap would flag A's wire caps, which are correct
(both ends measure the compact form). Held instead by one encoder per writer and by table tests
enumerated from the code (`listCaps` and a reflective walk of the `Store` type).

## The sweep, key by key

The store (`member/state/store.go`):

| Key                                                                             | Verdict                                         | Why                                                                                                                                                                                                                               |
| ------------------------------------------------------------------------------- | ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `outbox.sending[]`                                                              | defective, fixed                                | content free text; the defect reported                                                                                                                                                                                            |
| `awaiting[]`                                                                    | defective, fixed                                | the transcript path is any UTF-8; 1017 `<` written 6 345 against 2 252. Now refused at the store write; a path with that many `<>&` is not realistic, and a delivery that cannot be recorded fails loudly as any store write does |
| `pending`, `ledger`, `receipts`, `missed`, `outbox.pings`, `outbox.gate.recent` | sound                                           | every string is a grammar with no character the writer escapes: event ids `[A-Za-z0-9_-]`, user ids and handles, human names `[a-z0-9_-]`, verbs, refs (segments `[A-Za-z0-9._-]`), nonces, hex digests                           |
| `sync.next_batch`                                                               | sound in the file, test arithmetic wrong, fixed | `syncTokenRe` admits `<>&"\`, so 1024 bytes can be written as 6 146; `TestStoreFullFitsTheFileCap` counted 1024. The bound now sums `fixedWritten()` (every fixed field as written); `MaxStore` still rounds to 3 MiB             |
| `team`, `seat`, `user_id`, `floor_ms`, `last_sent`, `run.*`                     | sound                                           | grammars and closed sets with no escaped character                                                                                                                                                                                |
| `MaxStore`                                                                      | sound                                           | it is compared with `len(data)`, the bytes written; its derivation is now true because every entry is capped as written                                                                                                           |

Other files and packages:

| Cap                                                                                                      | Verdict          | Why                                                                                                                                                                                                                            |
| -------------------------------------------------------------------------------------------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `MaxRosterCache` (roster.json)                                                                           | defective, fixed | Matrix's 65 536 is its canonical JSON (no `<` escape); the cache writes up to 6x. A valid roster heavy in `<` was refused at the cache write, and `saveRoster` keeps the roster only after the write, so sends waited for good |
| `MaxSession` (session.json)                                                                              | defective, fixed | the longest transcript and socket paths in `<` are 9 605 bytes written; the cap was 8 192 (and "fit in half of it" was false even for letters: 4 100)                                                                          |
| `MaxStatus`, `MaxDesired`, `MaxReport`, `MaxKeeper`, `MaxConversation`, `MaxLockRecord`, `MaxSessionPID` | sound            | every string is a grammar (team ids, seats, short names, session names `[A-Za-z0-9._-]`, colours) or a closed set                                                                                                              |
| `wire.MaxRequest` (64 KiB)                                                                               | sound            | measured on the line as written; the largest free text is a say/reply stdin of 4096 bytes (24.6 KiB escaped); hook args at most about 9 KiB escaped                                                                            |
| `wire.MaxMessage` (64 KiB)                                                                               | sound            | measured on `json.Marshal`'s output, the bytes sent; a launch's two paths of PATH_MAX are at most 49 152 escaped                                                                                                               |
| delivery caps (`MaxDeliveryBytes`, `proto.WireBytes`)                                                    | sound            | measured by `EscapeASCII`, the escaper `inbox.UserLine` writes with: the model of the fix                                                                                                                                      |
| A's content caps (`MaxContent`, `MaxPingObject`, text/body caps)                                         | sound            | A 2.1 defines them in the compact form and both ends measure it (`Size()` on the parsed value)                                                                                                                                 |
| `MaxHeadlineEscaped`                                                                                     | sound            | measured on `EscapeASCII`, what the envelope writes                                                                                                                                                                            |
| `matrix` request builders (`jsonBody`, raw content)                                                      | sound            | no client-side cap; Matrix's event cap is the server's, on its canonical form. A content re-sent from the store goes out as stored (escaped `<`), larger on the wire, the same event                                           |
| `engine` (`clone`, store writes)                                                                         | sound            | no cap computed; `clone` re-encodes, so a content in memory is escaped after the first store change                                                                                                                            |
| `keeper`                                                                                                 | sound            | reads with the codecs' caps above                                                                                                                                                                                              |

PR #158's seven keys (read on `origin/feat/bus-v2-n3f-a-int`, not edited):

| Key                          | Verdict | Why                                                                                                                                                                                                                               |
| ---------------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `outbox.reports`             | sound   | integers                                                                                                                                                                                                                          |
| `outbox.questions_unechoed`  | sound   | an event id or null                                                                                                                                                                                                               |
| `outbox.questions_uncounted` | sound   | two integers                                                                                                                                                                                                                      |
| `outbox.questions_handover`  | sound   | a boolean                                                                                                                                                                                                                         |
| `questions`                  | sound   | sized as written by design: the token's grammar excludes every character the writer escapes, the headline (the one free text) is counted at 6 bytes a code point (QX6), and the worst-case test writes through the store's writer |
| `reminded_ms`                | sound   | an integer                                                                                                                                                                                                                        |
| `remind_late`                | sound   | integers                                                                                                                                                                                                                          |

When #158 merges, `TestEveryFixedFieldHasARow` fails on purpose until `fixedWritten()` gains
rows for the new leaves: `outbox.reports` (60 x 14 + 2), `outbox.questions_unechoed` (2 + 44),
`questions` (`proto.MaxQuestionsStateBytes`, one row for the whole key: the walk stops at a
path that has a row) and `remind_late` (64 x 20 + 2). `questions_uncounted`, `questions_handover`
and `reminded_ms` hold only numbers and booleans and need none. Neither new list is in
`listCaps`, so `freeText()` needs no row.

## Files touched (for the #158 merge)

- `src-go/internal/teamchat/member/state/store.go`: `listCap.entries` and the `listCaps` table
  (a fifth field per row), `anys`, `checkLists` (end of file), `ErrTooLarge` and `Sending.Fits`
  (before `Sending.check`), `DecodeStore` shortened, `at` removed, `Store.check`'s count loop
  replaced by `s.checkLists(c)`, the caps comment. Expect conflicts in `DecodeStore`,
  `Store.check` and around `MaxStore`.
- `.../state/strict.go`: `marshal`, `writtenSize`; `decodeStrict` returns `error` only.
- `.../state/{conversation,desired,keeper,lockrecord,report,rostercache,session,sessionpid,status}.go`:
  `if err := decodeStrict(` (one line each); `rostercache.go` and `session.go` also their caps.
- `.../state/strict_test.go` (two call sites), `.../state/store_test.go` (one line: the bound
  in `TestStoreFullFitsTheFileCap`, which #158 also edits), new `store_written_test.go`,
  `files_written_test.go`.
- `src-go/internal/teamchat/member/engine/send.go` (the `Fits` step), new `send_written_test.go`.
- `CLAUDE/GoProjectRules.md`, PLAN.md, BUILT-n3.md, this report, one JOURNAL entry.

## Spec sentence for SPEC-n3d's cap rule (the coordinator folds it)

> Each entry's size is the bytes the store writes for it, with `<`, `>`, `&`, U+2028 and U+2029
> escaped as its encoder escapes them, measured through that encoder; a send whose entry is over
> 20 KiB as written is refused at the verb with 10 `size`, before anything is charged, even when
> its content is within A 11.2's 16384 bytes.

## Gates

- `./scripts/qa-changed.bash --base origin/merge/pr157-onto-f44`, run after the last code
  commit (f500967c3): exit 0. Its lines: `plan: 22 changed file(s)`, checks `docs`, `go`,
  `teamchat-keeps-off-v1`; `✓ docs: whole gate`, `✓ go: go-qa, both modules (diff mode on a git change)`, `✓ teamchat-keeps-off-v1: whole gate`, `✓ qa-changed passed: 22 changed file(s)`. No "tests failed in ." line: the root module passed. (`ansible-syntax` and `qa-js`
  were not selected for this diff.) It queued on the shared QA lock behind another run first.
- `go test -race -count=1 -cover`: `state` ok, 100.0%; `engine` ok, 92.6%; `keeper` ok, 91.1%;
  `wire` ok, 94.2%; `cli` ok, 98.9%; `link` ok, 92.4%; `teamsync` ok, 95.2%; `wiring` ok,
  100.0%.
- `golangci-lint run` over `member/state` and `member/engine`: 0 issues.
- `qa-all.bash` not run (not this builder's to run).

## Not done

- No `go/analysis` rule (none can be written; stated in GoProjectRules.md).
- The "about 28%" threshold for a 4096-byte text is arithmetic, not measured.
- `member/client` (record lines of 1 MiB, written without HTML escaping) and `member/hook` were
  outside the named sweep and not judged.

## Findings

1. **`SPEC-n3d.md` does not exist in this base.** n3d's rules are in
   `subagent-reports/261022-n3d-handoff.md` (the n3d-D1 row: "32 entries of at most 20 KiB; the
   file cap is the new sum rounded up to the next 256 KiB") and B 2.3.2; SPEC-n3f's 12.6 quotes
   n3d's cap text. The spec sentence above is written for wherever n3d's cap rule is folded.
2. **The spec assigns no code to "fits compact, not written"**; the smallest reading is A 11.2's
   over-size send, 10 `size`, through the same `refusal` path as the composer's.
3. **Non-ASCII multibyte content is not affected** (encoding/json writes UTF-8 as is); the brief
   expected it red. Only U+2028 and U+2029 are written longer (6 bytes for 3).
4. **Turning HTML escaping off** (`SetEscapeHTML(false)`) is not done. With option (c) it would
   change nothing about correctness, only make `<>&` sends cost 1 byte instead of 6 (fewer
   refusals) and keep U+2028/9 at 6; `member/records` already writes that way. A separate
   decision, not taken.
5. **`awaiting` now refuses a transcript path heavy in `<>&`** at the store write (loud, as any
   failed store write). If the owner wants such paths to work, `sizeAwaiting` would rise by about
   5 KiB (32 entries: 160 KiB, within `MaxStore`'s headroom) rather than refusing.
6. **Cost**: every store write marshals each entry once more (one more pass over the file).
   `member/state`'s tests take about 150 s under `-race` on the loaded host; the new
   `TestEveryListIsCappedAsWritten` writes a full store only for `<` and U+2028 to bound that.
7. The engine's `case err != nil` after `Fits` (an entry that does not encode) cannot happen for
   a composed content; kept so no error is discarded.
