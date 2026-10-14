# qa-changed Go port: implementation wave and cut-over (report)

Branch `go-port/qa-changed`. Not pushed, not merged into F44. F44 (including its later
`214002e2`) is merged into the branch with `--no-ff`. Commits on the branch: the Go port with
parity (`90e09066`), the cut-over with the golden replay and docs (`9f3777a7`), the F44 merge, and
this report.

## What was built

| Piece                                                                            | Where                                                                                                 |
| -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| The tool: flags, collection, classification, suites, checks, preflight, executor | `src-go/qa/changed/` (one package, 12 files plus tests)                                               |
| The command (signal handling, real runner, `LookPath`)                           | `src-go/qa/cmd/qa-changed/`                                                                           |
| The one process seam, shared with the bash-limits gate's git runner              | `src-go/qa/internal/proc/` (`bashlimits.OSGit` now delegates to it)                                   |
| Classification as data                                                           | `scripts/qa-routing.tsv`                                                                              |
| Excluded trees, one source for the bash gates and the Go tool                    | `scripts/qa-trees.tsv`, read by `scripts/lib/qa-trees.bash` (new, 29 lines) and by Go                 |
| The wrapper (build into the cache, run, same exit code)                          | `scripts/qa-changed.bash` (660 lines of bash replaced by 36, 0.17s warm)                              |
| Golden capture of the bash from `f0284848`, and its replay                       | `CLAUDE/Plan/00192-.../qa-changed-golden/capture.py`, `src-go/qa/changed/golden_test.go`, `testdata/` |
| Black-box suite, now 98 cases                                                    | `scripts/test-qa-changed.bash`                                                                        |

The `qa-bash-limits.bash --update` run removed the `qa-changed.bash` entry (110 to 109 entries); the
`qa-discovery.bash` record went down (185 to 158 code lines; statements stay at 221, so it did not need raising).

## Decisions applied (the coordinator's defaults)

- D1: 30 minute per-check timeout, `QA_CHANGED_TIMEOUT` (a Go duration, `0` disables); kills the
  process group, reports `timed out after <d>`, counts as a failure.
- D2: sequential, bash order.
- D3: a failed check outranks a gate that could not run: exit 1 if any failed, else 2 if a gate
  exited 2; the FAILED line names both. Only the gate scripts, `qa-toolchain.bash` and the suites
  can "not run" (their exit 2 is the repo's convention); a plain tool's exit 2 (`bash -n` on a
  syntax error is exit 2) is a failure.
- D4: yes, but not inline: an inline reader grew `qa-discovery.bash` to 235 statements against its
  record of 221, so the reader is `scripts/lib/qa-trees.bash` and `qa-discovery.bash` shrank. A Go
  test runs the bash reader and the Go reader over the real table and compares the lists, and
  another that the reader refuses a missing table and an unknown list.
- D5: not done. D6: yes, with one extension noted below.
- `scripts/go-binary-suites.tsv` stays a separate file in its existing format (it is about
  deployed Go commands); the loader reads any `command<TAB>suites` row, so the agent-bus-install
  cut-over's added row loads unchanged. It now also refuses a suite `qa-all.bash` does not run
  (B10), so a row for a suite that suite file lists but `qa-all.bash` does not invoke would fail
  loudly.

## Spec bugs fixed, each with a test

| Bug | Fix                                                                                                                                             | Pinned by                                                                  |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| B1  | a git failure is exit 2 with git's message                                                                                                      | `TestCollectGitFailureIsAnErrorNotAnEmptyList`, golden `bad-base`, suite   |
| B2  | `--files` with no file is exit 2                                                                                                                | `TestParseArgsUsageErrors`, golden `explicit-empty`                        |
| B3  | flags after `--files` are flags; `--` ends flags                                                                                                | `TestParseArgs`, golden `explicit-then-flag`                               |
| B4  | `--base` refuses a flag as its value                                                                                                            | `TestParseArgsUsageErrors`, golden `base-swallows-flag`                    |
| B5  | paths cleaned (`./x`, absolute inside the repo), outside is exit 2, duplicates collapse                                                         | `TestCollectExplicit*`, golden `explicit-*`                                |
| B6  | deletions collected; they owe the whole gates and select suites naming them                                                                     | `TestDeletedFilesOweOnlyTheWholeGates`, golden `git-deleted-*`             |
| B7  | `src-go/*` before the YAML rule                                                                                                                 | `TestSrcGoRouting...`, golden `file-src-go-golangci-yml`                   |
| B8  | each name tested independently; every owner credited and listed                                                                                 | `TestEveryChangedFileWithTheSameBasename...`, golden `group-same-basename` |
| B10 | tsv suite must be one `qa-all.bash` runs                                                                                                        | `TestGoRowsAreValidatedAgainstThePopulation`, real-tree test               |
| B11 | each check declares its tools; one preflight (adds `python3`, `go`, `golangci-lint`, `bash`)                                                    | `TestPreflightKnowsEveryToolAChecksNeeds`                                  |
| B12 | the semgrep JSON is decoded in Go; a bad or missing report fails the check                                                                      | `TestSemgrepFailuresAreFailuresNotPasses`                                  |
| B13 | `could not run (exit 2)` for gates; failure outranks                                                                                            | `TestAGateThatCannotRunIsNotAnOrdinaryFailure`, golden `run-gate-exit-2*`  |
| B14 | per-check timeout                                                                                                                               | `TestACheckThatOutlivesItsTimeoutIsKilledAndReported`, `proc` test         |
| B15 | `--help` to stdout; the text is the tool's own                                                                                                  | golden `help`, suite                                                       |
| B16 | `plan: N changed file(s) (explicit)`, and the no-op line says `(explicit list)`; `no changed files` kept                                        | normalised in the replay (below)                                           |
| B17 | an unreadable root is exit 2                                                                                                                    | `TestMainRefusesBadInputWithExitTwo`                                       |
| B21 | an explicit path that is a directory is refused; one that does not exist is dropped with a word on stderr; a git-listed non-file gets a `note:` | `TestCollect*`, golden `explicit-missing`                                  |

Kept as the spec says: B9 (the population is still a text scan of `qa-all.bash`, with real-tree
tests that it contains the expected suites and no per-file gate), B18, B19, B20, B22, B23, B24, B25
(a test asserts `qa-bash.bash` still gates on error and warning and `qa-patterns.bash` still uses
the same semgrep config), B26 (moved to the routing table).

## Parity evidence

- Leg 1: `scripts/test-qa-changed.bash` with `QA_CHANGED_BIN` set, run before the cut-over: the 73
  original cases passed against the bash and the Go binary alike (one case rewritten for B6: a
  deleted file is no longer "dropped", it just gets no per-file check; the suite then had 74).
  After the cut-over the default target is the wrapper: 98 cases pass, and `QA_CHANGED_BIN` still
  works for the binary.
- Leg 2: golden replay, 118 scenarios recorded from git revision `f0284848` (the bash exactly as the
  spec read it): every world file alone with `--plan --files`, nine groups, option and error cases,
  git collection cases in the world repository (modified, untracked, ignored, deleted, renamed,
  staged, `--base` over two commits), and 38 run-mode scenarios under stub tools that log their argv
  (all pass, each failure alone, two failures, a semgrep finding, a gate exiting 2, a bash syntax
  error, each missing tool, the eslint set-up, uncovered-only, markdown-only, a suite picked by name,
  the Go closure). The replay compares stdout, stderr, status and the argv log. 99 identical, 19
  differ, every one a numbered bug pinned in `testdata/deviations.json` (`bad-base`,
  `base-swallows-flag`, `explicit-{absolute,dot-slash,duplicates,empty,missing,then-flag}`,
  `file-src-go-golangci-yml`, `git-deleted-{allowlisted,go,named-by-suite}`,
  `git-modified-untracked-deleted`, `group-everything`, `group-same-basename`, `help`,
  `unknown-flag`, `run-gate-exit-2`, `run-gate-exit-2-and-fail`).
- B16 is not pinned scenario by scenario: it would be on every `--files` run, so the replay maps
  the Go's `(explicit)` wording back to `vs HEAD` before comparing, with the reason in a comment.
- Not in the golden: `acceptance.bash` (leg 3, real-tree comparison over recent F44 commits) was not
  written; instead I compared the old and new `--plan` output on the real diff against F44 (before
  and after the F44 merge): identical except the order of the reasons when one suite names several
  files (it was alphabetical by matched text in the bash, it is collection order now).

## QA results

All run in the worktree with the PATH from the brief; `qa-all.bash` not run.

- `scripts/qa-go.bash`: `GO-OK 39 package(s) ... coverage 94.9% (floor 90%) ... qa module 7 package(s) at 94.4%; project rules clean`.
- `scripts/qa-bash-limits.bash`: `BASH-LIMITS-OK 446 bash files ... 109 allowlisted`.
- `./scripts/qa-changed.bash --base F44` (now the Go one): passed, 179 changed files, 98 cases in
  `test-qa-changed.bash`, `qa-ready-wait-rules.bash` 730.
- The F44 bash `qa-changed.bash` on the same diff: see the last section.
- `scripts/test-qa-changed.bash`: `passed: 98   failed: 0`.

## Things to know and open questions

1. The first full run of the Go tool found a real bug the unit tests did not: `--scripts scripts`
   gave a relative root, and the semgrep check (which runs in a scratch directory) was handed a
   relative config path. Fixed by making root and scripts absolute in `newSession`, with a test
   that fails without it. Worth the reviewer's attention: any other command that runs in another
   directory (eslint in `extensions/`, the semgrep mirror) gets paths built from those two.
2. A deleted file for D6 runs `docs` for a deleted `.md` too (the spec's list named the bash-limits
   gate, the Go gate and suites). It is cheap and a deleted doc can leave dangling links; say so if
   the spec's narrower list was meant.
3. Repo files are read with plain `os.ReadFile`/`os.Open` on computed paths (a symlink is followed,
   as the bash's `[[ -f ]]` did), with a `.golangci.yml` exclusion for G304 and G703 in
   `qa/changed` (the tool reads computed repo paths by design). `bashlimits` uses `os.Root`
   instead; the two could converge in the `qa-all` port.
4. The project rules (`project_seam_exec`, `project_seam_commands`) run over `src-go` and stop at the
   nested module, so they do not judge `qa/`; the module keeps the rule by hand (all processes
   through `qa/internal/proc`, tools named in the check registry). Making the driver judge `qa/`
   too is a follow-up for the rules module, not done here.
5. Ctrl-C ends the current check's process group and the run exits 130 with `✗ qa-changed interrupted`; there is a test, but no test with a real terminal.
6. `qa-all.bash`'s `qa-changed-mapping` gate is unchanged and still runs the suite, which now builds
   the tool on each of about 60 invocations (0.17s each when cached).
7. Cut-over rollback is `git revert` of the cut-over commit; no bash fallback is kept, by design.
8. `scripts/qa-routing.tsv` and `qa-trees.tsv` are named by `test-qa-changed.bash` (a comment) and
   routed to the Go gate, so a change to either runs both; the agent-bus-install branch needs no
   change to merge with this one beyond `scripts/go-binary-suites.tsv`'s new row.

## Old bash on the same diff

The F44 `qa-changed.bash`, run as an untracked copy beside the new files (`scripts/qa-changed-old.bash`,
since deleted) over the same diff against F44, planned the same checks and gave the same verdicts
as the Go one (toolchain, `bash -n`, shellcheck, semgrep, ruff, docs, Go gate and both suites
pass). The two differences are the copy itself: its file count is one higher, and the bash-limits
gate rightly failed on the 543-line copy (`✗ qa-changed FAILED: bash-limits`, exit 1). Plan output
for the real branch diff, before and after the F44 merge, differed only in the order of reasons
on a suite that names several files.
