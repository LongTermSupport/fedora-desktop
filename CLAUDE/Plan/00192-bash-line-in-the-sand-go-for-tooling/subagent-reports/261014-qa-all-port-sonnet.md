# qa-all Go port: spec, implementation and cut-over (report)

Branch `go-port/qa-all`, from the F44 tip `e4f6615c`. Not pushed, not merged into F44. The full
`qa-all.bash` was not run (the coordinator's gate); a trimmed run of the new command over 18 real
cheap gates is below.

## What was built

| Piece                                                                                   | Where                                                                                         |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Functional spec, 20 numbered bugs (B1-B20), the oracles' disposition                    | `CLAUDE/Plan/00192-.../SPEC-qa-all.md`                                                        |
| The gate list as data: 92 rows, strict loader                                           | `scripts/qa-gates.tsv`, `src-go/qa/gates/gates.go`                                            |
| The three stage-line readers (case count, detail pattern, helper-tests counts file)     | `src-go/qa/gates/readers.go`                                                                  |
| The runner: four gate kinds, timeouts, interrupt, the report merge by name, the verdict | `src-go/qa/all/` (`config.go`, `run.go`, `gate.go`, `report.go`)                              |
| The command                                                                             | `src-go/qa/cmd/qa-all/main.go`                                                                |
| Live streaming of a stage's output through the one process seam                         | `src-go/qa/internal/proc` (`Cmd.StdoutTo`, `StderrTo`)                                        |
| Golden capture of the bash from `e4f6615c`, and its replay                              | `CLAUDE/Plan/00192-.../qa-all-golden/capture.py`, `src-go/qa/all/golden_test.go`, `testdata/` |
| The wrapper                                                                             | `scripts/qa-all.bash` (1446 lines of bash replaced by 29, 24 of them comments)                |

## Cut-over

- `scripts/qa-all.bash` is a wrapper via `lib/go-tool.bash`. `bash-limits.allowlist` lost its entry
  and the `lib/qa-helper-summary.bash` entry (109 to 107), by `qa-bash-limits.bash --update`; nothing
  was folded to fit.
- Deleted: `scripts/lib/qa-helper-summary.bash`, `scripts/test-qa-helper-summary.bash`,
  `helpers/qa_environment/gate_call_sites.py` and its test. Every case of the suite moved to Go
  (`gates/readers_test.go`: the counts reader's 28 refusals and reads, the case-count and detail
  corpora, and the end-to-end case against the real `unittest_counts` runner). The two source-text
  guards (exactly one `exit 1`, `qa_hard_gate_failed` call sites) became a run: the real table with a
  fake seam, every one of the 92 gates failing in turn, every other gate still run in order and the
  failure named in the verdict (`all/realtable_test.go`). The pattern-against-real-gate coupling (it
  re-ran 11 gates in a suite) is now run time: a pattern that finds nothing fails that gate in every
  full run (B3).
- `qa-changed` reads its suite population from `qa-gates.tsv` (B9 of SPEC-qa-changed closed);
  `qa/changed/summary.go` removed (it is `gates.CaseCount`); `qa-routing.tsv` routes
  `qa-gates.tsv` to the Go gate.
- `helpers/docs/link_check.py` reads the table for the QA.md inventory (both directions), tests
  rewritten; `test-qa-docs-exit-codes.bash` fixture writes the table; `test-qa-changed.bash` names
  another pair for "picked by name" and gains two cases for the table.
- Docs: `CLAUDE/QA.md` (intro, the reader section rewritten, the guard paragraph, the row for the
  removed suite), `src-go/CLAUDE.md`, `.github/workflows/qa.yml` comment, plan `PLAN.md` Task 3.3.
- Lint fixes the earlier port left (golangci-lint is on this host's PATH and flagged them): a
  gofumpt diff in `changed/golden_test.go`, a cognitive-complexity split in `changed/classify.go`,
  and one `mirror` finding in `proc_test.go`.

## Bugs fixed, each with a test (SPEC-qa-all.md section 10)

| Bug | Fix                                                                                                                    | Pinned by                                                                        |
| --- | ---------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| B1  | arguments were ignored (a typo ran the suite); `--help`, unknown is exit 2                                             | golden `help`, `unknown-flag`; `config_test.go`                                  |
| B2  | a suite with no `passed: <n>` line was a green tick over the word `passed`                                             | golden `no-count*`; found live: `test-ccy-info-flags.bash` (now prints the line) |
| B3  | a detail pattern that matched nothing printed `summary unreadable` under a green tick                                  | golden `detail-miss-*`                                                           |
| B4  | positional jq merge: an empty report shifted every later stage down a slot; a malformed one aborted jq with no verdict | golden `stage-report-*`; `report_test.go`                                        |
| B5  | a stage exiting 0 while counting failures passed                                                                       | golden `stage-exit0-with-failures`                                               |
| B6  | an aborted run left the previous run's report, whose `.status` could say `pass`                                        | golden `stale-report-then-abort`; `gate_test.go`                                 |
| B7  | one fixed report path for every run (found while recording: another QA run on this host corrupted the capture)         | `QA_RESULTS_JSON`; `run_test.go`                                                 |
| B8  | no gate time limit                                                                                                     | `gate_test.go` (timeout), `proc` test                                            |
| B9  | Ctrl-C left no word and possibly orphans                                                                               | `run_test.go` (interrupt, exit 130)                                              |
| B10 | the gate list was scanned out of source text by three tools                                                            | the table; `gates_test.go`, `suites_test.go`, `link_check` tests                 |
| B11 | gates ran in the caller's directory                                                                                    | `gate_test.go`                                                                   |
| B12 | `deployed-drift` passing with no output printed a blank line                                                           | golden `drift-empty`                                                             |
| B13 | a failed stage with no failures list was not named in the verdict                                                      | golden `fail-<stage>`, `stage-failures-merge`                                    |
| B20 | cosmetic: the counts reader's diagnostics                                                                              | golden `helpers-counts-*`                                                        |

Kept: B14 (stale header counts, removed with the text), B15 (needs `go`: the wrapper exits 2 with the
hint), B16-B18 as in the spec.

## Parity evidence

- Golden replay: 138 scenarios recorded from `e4f6615c` (all pass, each of the 92 gates failing
  alone, each stage exiting 2, two- and many-failure runs, every reader shape, the counts file wrong
  seven ways, drift pass/skip/fail/empty/exit 2, stage reports empty/malformed/no summary, stage
  output passthrough, stale report, arguments). 109 identical, 29 differ, each a numbered bug pinned
  in `testdata/deviations.json` and `testdata/deviation/`. The capture cuts the one removed gate
  (`helper-counts-reader`) out of the legacy text and changes its report-path assignment to read
  `QA_RESULTS_JSON`, both stated in `capture.py`.
- Live check: the new command over 18 real cheap gates (toolchain, bash-limits, the docs stage,
  nokill, deployed-drift, the whole helper unit suite through the counts file, secret-scan, planlib,
  ssh-suspend-guard, extension-compat, panel-contract, vmtest-manifest, plan-script-logging,
  version-pins, docs-exit-codes, ccy-info-flags, ccy-rootless-guard, failfast-pattern-tests): every
  detail pattern matched the real gate's wording, `QA passed: 81 files checked`, exit 0.
- `scripts/test-qa-changed.bash` (now on the table-driven population): passed 109, failed 0.

## QA results

All targeted, all green on the cut-over tree:

- `scripts/qa-go.bash`: fmt, vet, `test -race`, coverage 94.9% (floor 90%), golangci-lint clean; qa
  module 10 packages at 94.4%; project rules clean.
- shellcheck on the touched bash; `qa-bash-limits.bash` OK (445 files, 107 allowlisted).
- Python: 132 tests OK. `qa-docs`: 81 files OK.
- `test-qa-changed` 109/0, `test-qa-docs-exit-codes` 4/0, `test-ccy-info-flags` 43/0.
- `bash scripts/qa-all.bash --typo`: usage, exit 2.

## Things to know and open questions

1. `golangci-lint` is on this host's PATH (the hook ran it on every edit); the brief said it was not.
   `qa/` is clean under it, with `src-go/.golangci.yml` extended for `qa/(all|gates)` G304/G302/G306
   (computed repo paths, the report file).
2. `scripts/test-qa-helper-summary.bash` failed 1 case in this container before it was deleted
   (77 passed, 1 failed, seen in a run of every suite): a pattern or gate that only differs here.
   It is gone, but the cause is now a live question for the first full run: if a detail gate's
   wording differs on this image, `qa-all` will say so by name (B3) instead of ticking.
3. A host with no `go` now gets exit 2 from the wrapper before any gate runs (B15); before, every
   other gate ran. CI has `go` (the `go` gate needs it already).
4. Per-gate timeout default is 30 minutes; the longest real gate (`qa-go.bash` with `-race`) should
   be measured on the coordinator's full run.
5. `meta-deploy.bash`: no entry; nothing is installed on a host.
6. Rollback is `git revert` of the cut-over commit.
