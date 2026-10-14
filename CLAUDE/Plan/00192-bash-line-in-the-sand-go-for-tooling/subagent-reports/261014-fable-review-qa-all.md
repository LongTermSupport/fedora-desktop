# Review: the qa-all Go port (branch `go-port/qa-all`)

Adversarial review of commits `1aafe9da` (spec), `069b538f` (port), `239e07e5` (cut-over),
`15891198` (report), diffed against the merge-base `e4f6615c`. Read: `SPEC-qa-all.md`, the port
report, `src-go/qa/{all,gates,cmd/qa-all}`, `src-go/qa/internal/proc`, `scripts/qa-gates.tsv`,
the wrapper and `lib/go-tool.bash`, `.github/workflows/qa.yml`, `CLAUDE/QA.md`, `src-go/CLAUDE.md`,
`helpers/docs/link_check.py`, and the F44 bash (`git show e4f6615c:scripts/qa-all.bash`).
Everything below was run in this worktree; nothing was installed.

## Verdict

**Merge**, with the should-fix items below as a follow-up commit on the branch (none is a
regression against the bash; two are silent passes the bash also had and the port leaves in).

## Findings, ranked

### Blocker

None.

### Should-fix

1. **`passed: 0` is a green tick.** `gates.CaseCount` accepts `passed: 0` (`readers.go` L21-33).
   A suite whose case discovery matched nothing (`passed: 0 failed: 0`, exit 0) prints
   `✓ <name>: passed: 0`. Reproduced with a stub table (below). The bash did the same; B2 fixed
   the no-line case, not the zero case. A gate that ran zero cases proved nothing, the same as
   one that printed no line.
2. **`passed: 3 failed: 2` with exit 0 is a green tick.** The reader takes the last `passed: N`
   and ignores `failed: M` on the same line (`readers.go` L28-32); `✓ mixed: passed: 3` was
   printed for a stub that exited 0 while reporting two failures. A suite that forgets
   `exit 1` after its summary is exactly the class the gate is meant to catch. Cheap defence:
   refuse the pass when the counted line also carries `failed:[[:space:]]+[1-9]`.
3. **A script row's extra words are silently dropped.** `Gate.Command` (`gates.go` L51-56)
   returns `{"bash", <root>/<script>}` for any `scripts/*.bash` command and discards `Argv[1:]`;
   the loader accepts the row. Reproduced: a row `scripts/test-args.bash --flag` ran with an
   empty `$*`. No row in today's table has arguments, so the live table is unaffected, but the
   first contributor who adds one gets a gate running in the wrong mode with no error. Either
   pass `Argv[1:]` through or have `parseRow` refuse words after a script. (`link_check.py`
   `qa_gates` has the same assumption: `command.endswith(".bash")`.)
4. **`realtable_test.go` samples are hand-typed, not real wording.** The test comment says
   "what each detail gate actually prints"; five of eleven differ from the live gates (`go`
   prints `GO-OK 39 package(s)`, the sample adds `, all tested: ...`; `bash-limits`,
   `panel-contract`, `version-pins`, `vmtest-manifest` likewise). The test proves pattern
   matches sample, both written by the same hand, so it cannot catch a gate rewording. That is
   fine *because* B3 makes a non-matching pattern a failed gate on every full run, but the
   comment and `QA.md` ("against a sample of the gate's real wording") overstate it. Reword, or
   record the samples from a real run.

### Nit

05. **The cut-over turns a missing gate script into exit 2 before any gate runs**
    (`gates.CheckScripts`, called from `newSession`). In the bash a deleted suite was one failed
    gate (`bash` 127) and the rest still ran. The spec states it (11.3) but does not number it;
    it is a behaviour change the B-list should carry so a reader of `deviations.json` can find it.
06. **A stage's own stdout is echoed, and can forge the verdict line.** A stub `qa-docs.bash`
    that printed `✓ QA passed: 999 files checked` had that line on qa-all's stdout before the
    real `✗ QA FAILED` (B17, kept by design). Exit status and the last verdict line are right, so
    CI (which uses the exit status) is safe; anything that greps stdout for `^✓ QA passed` is not.
    I found no such grep in the repo (`scripts/git-hooks`, `.claude`, workflows); the daemon's
    `R-STOP-QA-FAILURE` reads the whole output. Record the fact in `QA.md` so nobody adds one.
07. **`qa-bash.bash`'s `shellcheck` line hardcodes `/tmp/qa-results.json`** ("see
    /tmp/qa-results.json .checks.bash.shellcheck_diagnostics"), which B7 (`QA_RESULTS_JSON`) makes
    wrong when the path is overridden. The stage could print `${QA_RESULTS_JSON:-/tmp/qa-results.json}`.
08. **`proc.OS.Run` discards the capture on `ErrWaitDelay`.** A passing gate that leaves a
    background child holding its pipe is reported, after the 2s grace, as "cannot run bash:
    ... wait delay" with an empty capture (`proc.go` L94-99 returns `Result{}`). Strictly better
    than the bash (`$(...)` hung for ever) but the gate's output would say which child.
09. `QA.md` calls the wrapper "two-line"; it is 30 lines, 5 of them code. Say "a wrapper".
10. New `ERROR:` lines the Go can print (`cannot clear the previous report`, `cannot write the report`, `unknown argument`) are not matched by `verdicts.py` `TOOL_ABORT`, so a capture
    ending that way is read as truncated rather than aborted. Minor; the exit code is 2 either way.

## What held up

### 1. Silent passes

Constructed cases, run against the built binary (`go build ./cmd/qa-all`) over a scratch root
with stub gates:

| case                                                      | result                                                       |
| --------------------------------------------------------- | ------------------------------------------------------------ |
| hard gate exit 0, no `passed:` line                       | `✗ ... exited 0 but printed no 'passed: <n>' line` (B2)      |
| detail pattern that matches nothing                       | `✗ ... ran, but its summary line was not found` (B3)         |
| hard gate exit 1 / 2 / could not start                    | `✗` with the row's reason, capture on stderr                 |
| gate hangs with a background child, `QA_ALL_TIMEOUT=2s`   | `✗ slow: ... (timed out after 2s)` in 2.01s; child killed    |
| SIGINT after 1s (`timeout -s INT`)                        | `✗ qa-all interrupted`, exit 130, no report, no orphan       |
| stage exit 2                                              | `ERROR: <row text>`, exit 2, previous report removed (B6)    |
| stage exit 0 with empty / malformed / summary-less report | `✗ <stage>: its report is unusable (...)`, slot `null` (B4)  |
| stage exit 0 with `summary.failed > 0`                    | `✗ <stage>: exited 0 but its report counts N failed` (B5)    |
| drift gate exit 0 with no stage line                      | `✗ deployed-drift: ran, but printed no ... stage line` (B12) |
| helper suite stdout noise / bad counts / wrong token      | one `✗ helper-tests` line each, reason on stderr             |
| stdout write failure on a pass                            | `settle` turns exit 0 into 2 (`run.go` L75-80)               |
| **`passed: 0 failed: 0`, exit 0**                         | **`✓ zero-cases: passed: 0`** (should-fix 1)                 |
| **`passed: 3 failed: 2`, exit 0**                         | **`✓ mixed: passed: 3`** (should-fix 2)                      |
| stage prints `✓ QA passed: 999 files checked`             | echoed to stdout; real verdict and exit still fail (nit 6)   |
| stage exits 0, prints nothing, report `total: 0`          | passes, `✓ QA passed: 0 files checked` (bash did the same)   |

The verdict line and exit status come from `failedGates`/`failedStages` only (`run.go`
L172-201); a gate missing from the table cannot pass because nothing else lists it, and the
table itself is validated (short row, unknown kind/reader, duplicate, bad pattern, missing script
= exit 2 with the line, 14 refusals pinned in `gates_test.go`).

### 2. Coverage against the bash (mechanical)

Extracted from `e4f6615c:scripts/qa-all.bash`: 93 invocations in order, 89 `qa_hard_gate_failed`
sites (3 are `helper-tests`), 13 `qa_gate_detail` patterns, 7 `ERROR:` abort messages. Against
`scripts/qa-gates.tsv` (92 rows):

- Names and order: identical once `helper-counts-reader` (the deleted suite) is removed.
- Reason texts: `diff` of the 86 non-stage reasons is empty; the two extra `helper-tests`
  reasons are in `gate.go` L149-156 verbatim.
- The 7 stage exit-2 messages: identical.
- The 13 detail patterns: identical text, compiled `regexp.CompilePOSIX` (leftmost-longest as
  awk/`=~`). Count readers: the 72 `count` rows are the bash's `qa_gate_case_count` sites.
- Env/cwd: `QA_JSON_OUT` per stage as before; every gate now runs in the root (B11; the bash ran
  two python gates there and the rest in the caller's cwd); helpers gate gets
  `--counts-file`/`--counts-token` with split streams as before; everything else inherited.
- Hard/soft: hard gates status 1 and 2 alike fail (K2); stages keep the exit-2 abort.

### 3. The deleted suites

`test-qa-helper-summary.bash` (718 lines, 8 sections) against the Go tests:

| TH section                                        | Go                                                                                       |
| ------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| well-formed counts read exactly (9)               | `readers_test.go` `TestHelperCountsAreReadExactly` (9 cases)                             |
| unreadable counts refused, never zero (18)        | `TestHelperCountsRefusesAnUnreadableFile...` (18) + missing file + empty-vs-foreign      |
| refusal goes to stderr                            | `gate_test.go` `TestTheHelperGateFailsOnceForEachWayItCanFail`                           |
| end to end with the real `unittest_counts` runner | `TestHelperCountsReadsWhatTheRealRunnerWrites`                                           |
| `qa_gate_case_count` (14)                         | `TestCaseCountReadsTheLastCountLine...`, `...NeverAsANumber`                             |
| `qa_gate_detail` (5)                              | `TestDetailReadsTheRealWordingOfAGate`, `TestDetailAnswersNoMatchAsNotFound`             |
| every pattern run against its real gate (13)      | run time (B3) + `realtable_test.go` samples (see should-fix 4)                           |
| registered both directions                        | by construction (one row)                                                                |
| exactly one `exit 1` / call sites > 0             | `realtable_test.go` `TestEveryGateOfTheRealTableRunsWhenAnyOneOfThemFails` (92 subtests) |

**The "1 failed case in the container" is explained.** I ran the deleted suite from an
`e4f6615c` tree: the failing case is `the pattern qa-all.bash applies to $go_out no longer matches that gate's output` because the **`go` gate itself was red at `e4f6615c`**: gofumpt
rejected a one-member `var (...)` block in `src-go/qa/changed/golden_test.go`. The port fixed
that file in `069b538f` (the report's "gofumpt diff in changed/golden_test.go"). It is not a
wording drift; the `go` gate's `GO-OK` line matched in the full run here. (A second failure in my
extracted tree, `bash-limits`, is an artefact of `git archive` having no `.git`.)

### 4. Timeouts, kill, streams, wrapper

- Per-gate `context.WithTimeout` (`gate.go` L18-29); `proc.OS` sets `Setpgid` and
  `Cancel = kill(-pid, SIGKILL)`, `WaitDelay` 2s. Proven live above (hanging gate with a
  background child: both gone). `proc_test.go` pins the group kill without a wall clock.
- Sequential (D2); stages stream live through `StdoutTo/StderrTo` on the one seam, hard gates
  capture merged (true order). No interleaving between gates: one at a time.
- Wrapper: no `go` → `✗ qa-all: go not installed (...)` exit 2; no `HOME`/`XDG_CACHE_HOME` →
  exit 2; build failure → exit 2 with the compiler output. The binary is rebuilt every run into a
  `mktemp -d` under the cache, so there is no stale binary (cost: ~1s). `--typo` → exit 2 with
  usage.
- Build/vet/test (`go test -count=1 ./...` over `src-go/qa`): all 10 packages ok in 8.9s wall.

### 5. The golden

138 scenarios, 109 identical, 29 deviations, each pinned with a bug number from SPEC section 10:
B1 ×2, B2 ×2, B3 ×3, B4 ×3, B5 ×1, B6 ×1, B12 ×1, B13 ×11, B20 ×5 (= 29). I diffed six
golden/deviation pairs (`fail-bash`, `stage-report-empty-shifts`, `helpers-counts-wrong-token`,
`no-count`, `stage-exit0-with-failures`, `drift-empty`): each differs only in the stated fix
(an added `stage(s) failed` line; a `null` slot instead of a shifted merge; a reworded
diagnostic; a `✗` where the bash ticked). Normalisers replace only the scratch root, the report
path, the token and `/qa-all-<n>` temp names; nothing that could hide a verdict change. The
golden proves orchestration (the runner is faked), so the process layer is covered by `proc_test`
and the live run, not by the golden. The capture cut `helper-counts-reader` out of the legacy
text and parameterised its report path; both are stated in `capture.py`.

### 6. Rules, docs

- golangci-lint clean per the Go gate in the full run (`GO-OK 39 package(s)`); the `.golangci.yml`
  change is a scoped gosec exclusion for `qa/(all|gates)/` only.
- `ExitCode`, `Kind` typed; no discarded errors found; `settle` records write failures.
- `QA.md`, `src-go/CLAUDE.md`, `qa.yml` comment, `qa-routing.tsv`, `unittest_counts.py`
  docstring all point at the table; the `test-qa-helper-summary.bash` row is removed. Overstatements:
  should-fix 4 and nit 9.

### End-to-end runs (timings)

**Go port, this worktree, `./scripts/qa-all.bash`** (`QA_RESULTS_JSON` set to a scratch path):
exit 0, `✓ QA passed: 1436 files checked`, 94 stage lines (92 rows + the two extra lines
`shellcheck` and `patterns` print), stderr empty, report `{"total":1436,"passed":1436,"failed":0}`.
Wall time **774s** (12m54s). Longest single gate is well inside the 30m default. No vault
placeholder was needed: `qa-ansible-syntax` passed in the worktree as it stands. `deployed-drift`
reported `⚠ skipped (linked git worktree)`, as it does for every worktree.

**F44 bash, same commit of the gates?** It cannot run in the cut-over tree: it `source`s
`scripts/lib/qa-helper-summary.bash` and runs `test-qa-helper-summary.bash` and
`gate_call_sites.py`, all deleted in `239e07e5`. The nearest comparison is the F44 bash in a clone
checked out at the merge-base `e4f6615c` (`untracked/scratch/qa-all-review/f44clone`): see the
section below.

**F44 bash in the `e4f6615c` clone:** exit **2** after **186s**, aborted at the `js` stage with
`ERROR: Missing required tools (node / extensions node_modules)`: the fresh clone has no
`extensions/node_modules` (gitignored; the worktree has it). Before the abort: `ansible-syntax`
failed 83/83 because the clone has no `vault-pass.secret` (also untracked; the worktree's stage
passed), and **`go` failed** on the gofumpt diff in `changed/golden_test.go` (the cause of the
deleted suite's one red case, above). So the comparison is partial and environmental, not a
difference in the orchestrators: the ten stage lines the bash printed before aborting are the
same names in the same order as the Go run's first ten (`toolchain go bash-limits shellcheck bash python patterns patterns ansible ansible-syntax`), and the gate population is proven equal
mechanically in section 2. The abort itself is a live showing of B18 (the `✗ go` line was the only
record of the earlier failure; no verdict, no report) and of why the Go port's exit-2 abort now
also removes the stale report (B6). A like-for-like F44 run would need the clone to be given the
two untracked files; I did not copy them (the vault file is a secret).

### Scratch artefacts (untracked, not committed)

`untracked/scratch/qa-all-review/`: the F44 bash and the `e4f6615c` archive and clone, the
deleted suite's run log (`th-run.log`), both full-run captures (`go-run.*`, `f44-run.*`), and the
stub-table experiment (`exp/`).
