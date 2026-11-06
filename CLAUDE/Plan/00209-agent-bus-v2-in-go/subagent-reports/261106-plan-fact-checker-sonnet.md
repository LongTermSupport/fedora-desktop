# Fact check: BUILT-n3.md decision on awaiting / sizeAwaiting (diff lines 225-228)

| # | Claim | Verdict | Evidence |
|---|-------|---------|----------|
| 1 | `sizeAwaiting` stays 2252 | VERIFIED | src-go/internal/teamchat/member/state/store.go:33 `sizeAwaiting = 2252` |
| 2 | `awaiting` refuses a transcript path heavy in `<>&` | VERIFIED by mechanism, NOT pinned by any test | store.go:599-610 `checkLists` measures each entry via `writtenSize` (the writer's encoder, escaping) and rejects n > l.size; row at store.go:76 uses sizeAwaiting. store_test.go:274 fills awaiting to the cap with a 1024-byte "p" path and passes; store_test.go:392 only tests an over-length path. No test in member/state uses `<`, `>`, `&` in an awaiting path (grep: none). |
| 3 | `validTranscript` does not itself exclude `<>&` | VERIFIED | values.go:72-74: absolute, clean, `.jsonl`, len <= 1024 only. The refusal therefore comes solely from the size rule, only for paths long enough to exceed 2252 escaped (each char becomes 6 bytes). A short path with a few `<>&` is accepted, not refused. |
| 4 | A Claude Code transcript path never holds those characters | UNVERIFIABLE-HERE | External product behaviour; no file in the repo settles it (grep for slug in teamchat: none). Settle by a real ~/.claude/projects slug rule or a doc. |

## REFUTED
None strictly. Imprecision: "the path is refused" is true only when heavy enough to exceed the entry size; no test pins it (Defence Before Fix would want a red/green test, e.g. a 1024-byte path of `<` in store_test.go).
