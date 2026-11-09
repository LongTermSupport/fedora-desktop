# Adversarial review: go-port/qa-changed (scripts/qa-changed.bash rewritten in Go)

Read-only. Branch tip 8d9ae92a, level with F44 (merge-base = F44 tip 214002e2). Evidence
reproduced in a scratch worktree and a scratch git repository; the probe script is
`untracked/scratch/fable-qac/probe.bash` (scratch, not for the tree).

Verdict: **do not merge yet — two should-fix items are regressions a per-commit gate must
not ship with (a 3-6x slow-down of `test-qa-changed.bash`, and a test normaliser that
can hide a wrong plan header), plus one latent silent-pass class carried over from the
bash that this port was the moment to close.** No blocker in the "wrong verdict on real
checks" sense: every failure class I could construct produced a non-zero exit.

---

## 1. Silent passes (the attack the gate most needs to survive)

### 1.1 Routing parity against the deleted bash — exhaustive diff of every pattern

Compared `git show F44:scripts/qa-changed.bash` L212-304 (pre-rules + terminal rules +
post rules) against `scripts/qa-routing.tsv`:

| bash                                                                                             | routing row                                                                                                      | parity                                                           |
| ------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| pre: 3 parity refs -> NEED_GO                                                                    | `also <path> go -` x3                                                                                            | same                                                             |
| pre: `*.bash\|*.sh\|*.j2\|src-go/qa/bashlimits/*\|src-go/qa/cmd/qa-bash-limits/*` -> bash-limits | `also *.bash,*.sh,*.j2 bash-limits`, `also src-go/qa/bashlimits/*,…/qa-bash-limits/*,…/internal/proc/*`          | superset (proc added, correct: bashlimits now imports it)        |
| pre: `scripts/bash-limits.allowlist` -> bash-limits, covered                                     | `also … bash-limits covered`                                                                                     | same                                                             |
| pre: path on allowlist -> bash-limits                                                            | `also @listed:bash-limits.allowlist bash-limits -`                                                               | same                                                             |
| `*.md` -> docs, covered, stop                                                                    | `first *.md docs covered`                                                                                        | same                                                             |
| `*.yml\|*.yaml` -> excluded? skipped : classify_yaml                                             | `first *.yml,*.yaml @yaml tree:shell` (after src-go row)                                                         | B7 deviation, intended                                           |
| `*.js\|*.mjs` -> excluded? skipped : node-check (+eslint if extensions/)                         | `also extensions/*.js,*.mjs eslint tree:shell,rel:extensions` + `first *.js,*.mjs node-check covered,tree:shell` | same (node_modules under extensions/ still skipped: checked)     |
| `src-go/*` -> go, closure, covered, stop                                                         | `first src-go/* go covered,closure`                                                                              | same                                                             |
| `*.j2` -> stop, uncovered                                                                        | `first *.j2 - -`                                                                                                 | same                                                             |
| `.py` or python shebang -> py-excluded? skipped : ruff + unit mirror                             | `first @py ruff,unit-tests covered,tree:python`                                                                  | same (mirror logic is code, `addUnitTests`, matches L270-287)    |
| `.sh/.bash` or shell shebang -> excluded? skipped : 3 shell checks                               | `first @sh bash-syntax,shellcheck,semgrep covered,tree:shell`                                                    | same                                                             |
| post: SH non-empty -> bash-limits                                                                | `post any:@sh bash-limits`                                                                                       | same (kinds counted only for non-skipped files, as SH_FILES was) |
| post: SH or PY non-empty -> toolchain                                                            | `post any:@sh,any:@py toolchain`                                                                                 | same                                                             |
| suites: skip `.md` as a pattern; path + basename substring                                       | `selectSuites`: `!HasSuffix(".md")`, `namesFile` Contains path or base                                           | same except B8 (intended) and reason order (1.7 below)           |
| PER_FILE_GATES string                                                                            | `perfile-gate` row                                                                                               | same list                                                        |

No checked file class was lost. The new `scripts/qa-routing.tsv,scripts/qa-trees.tsv -> go covered`
row is an addition (the Go tests read the real tables), correct.

### 1.2 A suite that passes without a `passed: N` line is reported as a pass — **should-fix**

Reproduced (probe S1): a suite that exits 0 and prints no `passed:` line gives
`✓ suite test-silent.bash: passed (names tool-a)` and exit 0. `summary.go:23` returns the bare
word `passed` by design, copying `qa_gate_case_count`. Bash did the same, so it is parity, but a
suite whose harness crashed before its summary while a `set -e`-less tail exited 0 is exactly the
silent pass the owner is asking about, and this port had every tool to close it. Fix: in
`run.go:execute`, a suite with status 0 and no `passed: <n>` line is `cannotRun` ("exit 0 but no
case count"); add a golden deviation (new SPEC bug number) and a unit test. The one-word degraded
form then disappears from `gateCaseCount`.

### 1.3 `--files` where every named file is missing is exit 0 "nothing to check" — **should-fix**

Reproduced (probe S12): `qa-changed --files typo/path.bash` prints one stderr line and
`qa-changed: no changed files (explicit list) — nothing to check.`, exit 0. B21 deliberately kept
the drop; a typo in a hook or an agent's command line thereby passes the gate. Fix: drop with a
word when *some* names resolve; when *none* do, exit 2 (`--files named no existing file`).
`collect.go:104-127`.

### 1.4 Deleted playbook / deleted Python file: warning only, exit 0 — nit

Probe S5: a deleted `pb.yml` is `⚠ not covered` (bash dropped it with no word, so this is strictly
better). A deleted playbook could reasonably ask for the docs gate (dangling references); B6's
"whole gates" list (`checks.go:75 wholeGate = docs, go, bash-limits`) is applied only when a
rule's checks include them, and the `@yaml` first row has none. Consider `first *.yml,*.yaml`
also owing `docs` when deleted under `playbooks/`. Low priority.

### 1.5 Exit codes, timeouts, missing tools, empty diff — verified correct

- Untracked bash file with a syntax error (S2): exit 1, all five checks reported.
- Suite exits 2 (S7): `— could not run (exit 2)`, exit 1 because other checks failed, else 2.
- Timeout (retest with a 3 s suite, `QA_CHANGED_TIMEOUT=1ms|500ms`): every long check
  `timed out after …`, exit 1; `0` disables. Note the semgrep timeout line additionally prints
  `cannot read semgrep's report: unexpected end of JSON input` (`checks.go:258-266` runs after the
  kill) — cosmetic, nit: skip `semgrepFindings` when `res.TimedOut`.
- Repo with no commits (S9): exit 2 with git's message (B1 fixed).
- Bad routing row (S11): exit 2 naming the line.
- Space in filename (S3), directory given to `--files` (S4): correct.
- Rename (S10): the old side is not a deletion (`git diff` rename detection), same as the bash;
  an allowlisted file renamed away still runs bash-limits because the new name is bash.
- Missing tool preflight: exercised by golden `run-missing-*` and unit tests; `go`, `golangci-lint`,
  `python3`, `bash` added (B11).
- Wrapper build failure (W4): exit 2 with the compiler's message.

### 1.6 The B16 normaliser in the golden test can hide a regression — **should-fix**

`golden_test.go:201-209 normaliseGolden` rewrites the Go's `plan: N changed file(s) (explicit)` to
`… vs HEAD` and `(explicit list)` to `(vs HEAD, plus untracked)` on **every** scenario, not only
`--files` ones. If a regression made the git-collection path print the explicit header (e.g. a
future refactor of `describeSource`/`printPlan`), the normaliser would map it onto the bash's text
and the replay would pass. The report says it was done to avoid 60 near-identical deviation
files; the honest alternative is to apply the replacement only when `sc.Args` contains `--files`,
and to make the test fail if the explicit wording appears in a scenario without it. Two lines.

### 1.7 "Order of reasons" difference is acknowledged but not a numbered bug — should-fix (small)

The port report (lines 94-95, 141) says a suite's reasons are now in collection order where the
bash sorted by matched text; SPEC section 10 has no entry and `deviations.json` none. The golden
corpus did not hit it (every `group-*` scenario matched), so it is unpinned. Add B27 ("reasons
in file order, not matched-text order; cosmetic"), or sort `why` by the matched token to keep
parity. `suites.go:185-190`.

---

## 2. Parity: the 19 deviations

All 19 entries in `testdata/deviations.json` cite B1, B2, B3, B4, B5, B6, B7, B8, B13, B15, B21 —
each is a **Fix** row of SPEC section 10, each reason matches the row. `TestGoldenReplay…` fails
on a stale or unmatched deviation and on a scenario that is pinned but equal (good hygiene). The
rewritten `test-qa-changed.bash` B6 case (lines 150-153) now asserts a deleted file is absent
from per-file checks and later (lines 304-310) that a deleted allowlisted tool runs the
bash-limits gate; both directions proven. The corpus is 118 scenarios, captured from `f0284848`;
the bash last changed in `f958f051`, an ancestor, so the capture is of the current bash. The
unpinned B16 normalisation is 1.6 above.

---

## 3. The wrapper

- Builds into `mktemp -d -p ~/.cache/fedora-desktop-qa` per run, removes on exit: no stale
  binary is ever run (keyed by source by construction), two concurrent runs in different
  worktrees do not collide (W2: both exit 0, no leftovers W3), not `/tmp` (noexec concern
  handled). Build failure is exit 2 (W4). `GOPROXY=off` with a warm module cache works (W5).
- **`HOME` unset: `line 28: HOME: unbound variable`, exit 1** (W1), the same defect in
  `qa-bash-limits.bash`. nit: `${XDG_CACHE_HOME:-${HOME:?}/.cache}` gives exit 2 with a message,
  or fall back to `mktemp -d` under `$TMPDIR` only when it is not noexec.
- **Duplicated wrapper** — `qa-changed.bash` L25-35 and `qa-bash-limits.bash` L26-33 are the
  same ten lines (go check, cache dir, mktemp, trap, build, run). should-fix (DRY): one
  `scripts/lib/go-tool.bash` (`run_go_tool <cmd> -- args…`), each wrapper three lines.
- Cold cache: 3.2 s (T5); warm `--plan`: 0.18-0.33 s vs bash 0.15 s (T1/T3); binary alone
  0.006 s (T2). Fine for a single invocation.

---

## 4. Performance — **should-fix: the per-commit path got slower**

`scripts/test-qa-changed.bash` runs the wrapper for every case (~60 invocations, each a
`go build` link): **12.1 s via the wrapper vs 7.6 s for the F44 bash suite vs 2.0 s with
`QA_CHANGED_BIN`** (measured in the same container). That suite is selected for most commits —
its text names `main.go`, `go.mod`, `ui.go`, `app.go`, `QA.md`, `extension.js`,
`scripts/qa-bash.bash` and more by basename — so the real-run comparison on one shell file (T4)
is **16.4 s Go vs 10.3 s bash**, the whole gap being this suite. Fix: the suite builds the binary
once at start (reuse the wrapper for exactly one case to keep the wrapper covered) and sets
`TARGET` to it; the hook it already has (`QA_CHANGED_BIN`) is the mechanism. Expected result:
~2.5 s, 3x faster than the bash it replaced, which is the port's promised payoff.

---

## 5. Code quality

- Architecture is sound: `collect -> classify (data) -> selectSuites -> preflight -> execute`,
  a `checkDef` registry, one `proc.Runner` seam shared with `bashlimits` (its `OSGit` now
  delegates to `proc.OS`). Typed `ExitCode`, no discarded errors, outputs via `outf/errf` with a
  write-error turning a pass into exit 2 (good). 93.8 % / 96.4 % coverage, 88 tests, a real-tree
  test that the bash and Go readers agree on `qa-trees.tsv`.
- DRY nits: `wholeGate` (`checks.go:75`) is a second classification of checks beside `gate`
  on `checkDef`; make it a field. `kinds[r.m.kind]++` counts `"glob"` as a kind
  (`classify.go:173`) — harmless but meaningless; count only `contentBased()` rules. `plan.add`
  keeps the first `rel` silently if two rules disagree (`classify.go:49-57`).
- `.golangci.yml`: the new `_test\.go` G302|G306 exclusion is module-wide, so it also loosens the
  main module's tests on a branch about qa-changed; scope it to `qa/changed/.*_test\.go`. The
  `qa/changed/` G304|G703 exclusion covers production code wholesale; acceptable given the tool's
  job, but the reason belongs in the file (it is).
- `cmd/qa-changed/main_test.go` writes `--help` to the real stdout during `go test`; nit.
- `readListed` (`classify.go:95-105`) registers any `text<TAB>` prefix, so a commented header
  with a tab would be a "listed path"; harmless today.
- `loadPopulation` silently drops a suite named in `qa-all.bash` whose file is gone
  (`suites.go:45-49`), as the bash did; `qa-all` would fail on it anyway. nit.
- TDD: test files exist for every source file; the realtree and golden tests are not
  anchored on the implementation. `TestRoutingAndShellcheckArgumentsMatchTheGatesTheyDuplicate` is
  a text assertion on `qa-bash.bash`/`qa-patterns.bash` (B25 interim), acceptable.

---

## 6. Project rules do not run over the nested module — risk and proposal

Running the driver myself (`go build ./driver; go vet -vettool=… ./...` in `src-go/qa`) reports
**8 findings today**:

```
internal/proc/proc.go:57 exec.CommandContext outside internal/execx
internal/proc/proc.go:72 (*exec.Cmd).Run outside internal/execx
internal/proc/proc_test.go:58,66,67 time.Now/Since in a test
changed/run_test.go:201,205 time.Now/Since in a test
rules/homeabbrev.go:106 strings.HasPrefix with "~" (the rule judging its own source)
```

The two `proc.go` lines are the seam itself (false positive: `execOwner` is a single constant,
`rules/table.go:21`). The four test-clock reads are real rule violations of the kind the
rule exists for; `run_test.go:201-205` is redundant (the test would hang on a failed kill and
`go test`'s own timeout covers it) and should go; `proc_test.go` is the legitimate "prove the
kill lands in time" case, the same shape as the `tmuxccy` allowance.

Proposal (small, in `qa-go.bash` and `rules/table.go`):

1. `execOwner` becomes `execOwners = []string{"internal/execx", "qa/internal/proc"}` and
   `ownedBy` takes the slice (one loop). `seamcommands`' owner table is unaffected: the qa
   module runs `git`, `bash`, the gates and the tools, none of which are owned programs except
   `git` — add `"qa/changed"`, `"qa/bashlimits"` to the `git` owners, or (cleaner) exempt the qa
   module from `project_seam_commands` since it has no `gitx` to share.
2. `qa-go.bash` runs the driver a second time: `go vet -C "$QA_DIR" -vettool="$VETTOOL" $(go list ./... | grep -v /rules)` — every package of the nested module except `rules`
   (so the rules still do not judge themselves; `homeabbrev.go:106` is that case).
3. Add `clockAllowances` rows for `TestOSKillsTheWholeProcessGroupOnTimeout` with the reason;
   delete the clock read in `TestACheckThatOutlivesItsTimeoutIsKilledAndReported`.
4. Fix `src-go/CLAUDE.md` L92-94 ("keeps their discipline by hand") to say the rules now run
   over it.

Until then the risk is concrete: this very branch landed two test-clock violations unnoticed.

---

## 7. Docs

- `CLAUDE/QA.md` section "qa-changed.bash — diff-scoped QA" (L1026-1076) is accurate to the
  code: flags, exit codes, timeout, deleted-file semantics, data files, proofs, the golden
  revision. One claim to tighten: "as `qa-bash-limits.bash` does" is true but should become a
  pointer to the shared lib once 3 is done.
- `src-go/CLAUDE.md` L86-94 accurate; L92-94 to change with section 6.
- `CLAUDE.md` root table says `.claude/rules/go-code.md` loads for `src-go/`; that rule still
  says "every external command through `execx`" with no mention of `qa/internal/proc` as the
  nested module's seam — a one-line addition.
- `scripts/qa-routing.tsv` header is a good self-contained grammar; `scripts/qa-trees.tsv`
  preserved every reason from the deleted bash arrays (checked line by line).
- PLAN.md Task 3.3 and the journal updated in the same commits; no drift found.
- The port report's "99 identical, 19 differ" matches `deviations.json` (19 keys, 118
  scenarios).

---

## Ranked list

| #   | severity   | finding                                                                                                              | where                                             |
| --- | ---------- | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| 1   | should-fix | `test-qa-changed.bash` rebuilds the tool per case: 12.1 s vs 7.6 s bash (2.0 s possible); real runs 16.4 s vs 10.3 s | `scripts/test-qa-changed.bash` L20-35             |
| 2   | should-fix | golden B16 normaliser applies to every scenario; would mask the explicit header on a git-mode run                    | `changed/golden_test.go:201-209`                  |
| 3   | should-fix | a suite exiting 0 with no `passed: N` line is a pass (inherited silent-pass class)                                   | `changed/summary.go`, `run.go:317-330`            |
| 4   | should-fix | `--files` with no existing file is exit 0 "nothing to check"                                                         | `changed/collect.go:104-127`                      |
| 5   | should-fix | project rules not run over `src-go/qa`; running them finds 4 real test-clock violations on this branch               | `scripts/qa-go.bash:113-117`, `rules/table.go:21` |
| 6   | should-fix | the wrapper is duplicated verbatim with `qa-bash-limits.bash`                                                        | `scripts/qa-changed.bash:25-35`                   |
| 7   | should-fix | reason-order change is known but has no SPEC bug number / deviation                                                  | `suites.go:185-190`, SPEC section 10              |
| 8   | nit        | `HOME` unset -> bash unbound-variable exit 1 in both wrappers                                                        | `scripts/qa-changed.bash:28`                      |
| 9   | nit        | `_test\.go` gosec exclusion is module-wide                                                                           | `src-go/.golangci.yml`                            |
| 10  | nit        | semgrep timeout also prints "cannot read semgrep's report"                                                           | `checks.go:252-266`                               |
| 11  | nit        | `wholeGate` beside `gate`; `kinds` counts `glob`; `plan.add` first-`rel`-wins                                        | `checks.go:75`, `classify.go:49,173`              |
| 12  | nit        | deleted playbook owes no docs gate                                                                                   | `classify.go:apply`, routing `@yaml` row          |
