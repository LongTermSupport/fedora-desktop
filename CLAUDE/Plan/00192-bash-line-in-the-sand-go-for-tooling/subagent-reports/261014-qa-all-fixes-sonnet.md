# qa-all port: the review findings fixed (branch `go-port/qa-all`)

Merged `review/qa-all` and `F44` into `go-port/qa-all` (no-ff), then fixed every finding of
`261014-fable-review-qa-all.md`, tests first where a test could be written before the code.

## Merge

- Conflicts: `src-go/qa/changed/classify.go` (this branch already had `applyAlso` split out;
  F44's copy duplicated it, kept this branch's) and `scripts/test-ccy-info-flags.bash` (kept
  F44's two summary lines, `Summary: ...` and the canonical `passed: N   failed: M`).
- `TestEverySelectableSuitePrintsTheCanonicalCountLine` already reads the suite list through
  `loadPopulation`, which reads `scripts/qa-gates.tsv` on this branch; nothing on F44 read
  `qa-all.bash` text. The guard still fails for a selectable suite without the count line.
- The merge left `scripts/bash-limits.allowlist` one entry loose (`meta-deploy.bash` 187 -> 184
  code lines); tightened with `qa-bash-limits.bash --update` (the ratchet only shrinks).

## Findings

| #   | Finding                                         | Fixed how                                                                                                                                                                                                                                                                                            |
| --- | ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `passed: 0 failed: 0`, exit 0, was a pass       | `gates.CaseCount` now returns an error: `ErrZeroCases`. qa-all fails the gate (B21); qa-changed reports could not run (SPEC-qa-changed B31). Golden scenarios `zero-count` and `run-suite-zero-count`.                                                                                               |
| 2   | `failed: M > 0` with exit 0 was a pass          | Same reader, `ErrFailedCases`. qa-all fails the gate (B22); qa-changed reports a failed suite, exit 1 (B32). One shared reader in `qa/gates`, no second copy; `changed.judgeCount` is a mapping only. Golden scenarios `count-line-reports-failures`, `run-suite-count-reports-failures`.            |
| 3   | `Gate.Command` dropped `Argv[1:]`               | Script rows pass their words through as arguments; the returned slice is a copy. Test `TestCommandPassesAScriptsArgumentsThrough`. `helpers/docs/link_check.py` named such a row an error; it now names the script (test added).                                                                     |
| 4   | Hand-typed "real wording" samples               | Replaced by `src-go/qa/all/testdata/wording/<gate>.txt`, the real output of each detail gate. Tests match every table pattern against them and fail on an orphan file. `-capture-wording` recaptures by running the gates for real, refusing a failing gate or an unmatched pattern. QA.md reworded. |
| 5   | Missing-gate-script exit 2 unnumbered           | B24 in SPEC-qa-all section 10 and 11.3 (existing test in `run_test.go`).                                                                                                                                                                                                                             |
| 6   | Stage output could forge the verdict            | `guard.go`: a line from a gate (stage stream, drift line, failed-gate capture) that opens `[✓✗⚠] QA ` is indented two spaces, everything else untouched so `verdicts.py` still reads stage lines. B23, golden scenario `stage-forges-verdict`, unit tests incl. split chunks.                        |
| 7   | `qa-bash.bash` hardcoded `/tmp/qa-results.json` | It read `QA_JSON_OUT_MERGED`, which nothing sets; now `QA_RESULTS_JSON`, the variable the runner honours. Pinned by a real-tree test. B7 text extended.                                                                                                                                              |
| 8   | `proc.OS.Run` dropped capture on `ErrWaitDelay` | Capture kept, new `Result.Leftover`, the leftover process group is killed. qa-all fails the gate or stage (B25); qa-changed turns it into a failing status with a note. Tests in `proc_test.go`, `gate_test.go`, `run_test.go`.                                                                      |
| 9   | QA.md "two-line wrapper"                        | Reworded to what the file is.                                                                                                                                                                                                                                                                        |
| 10  | `verdicts.py` `TOOL_ABORT` vs Go `ERROR:`       | Go side: every abort of the runner is `ERROR: qa-all: ...` (B26; `unknown-flag` deviation re-pinned). Python: `TOOL_ABORT` accepts the prefix; test `test_every_abort_the_go_runner_prints_is_a_terminal_state`.                                                                                     |

## Checks run

`scripts/qa-go.bash` (GO-OK, golangci-lint clean, project rules clean), `go test ./...` in
`src-go/qa`, shellcheck on the changed bash, `scripts/qa-bash-limits.bash`,
`scripts/test-qa-changed.bash` (109 passed), the `verdicts` and `link_check` Python tests, ruff on
the changed Python. The full `qa-all` was not run, as instructed.

## Notes

- The two golden captures were re-run (`capture.py` of both goldens); only additions came out
  except `src-go/qa/changed/testdata/world.json`, whose stale `test-gone` line was left as committed.
- The `go` and `bash-limits` captured wording files had to be seeded once for the first capture
  (the Go gate runs these tests, which need every file); later captures need no seeding.
