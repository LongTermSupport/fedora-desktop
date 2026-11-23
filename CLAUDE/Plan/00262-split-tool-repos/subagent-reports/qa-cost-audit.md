# QA cost audit: why Go looks slow, and what is busy work

Read-only audit. Nothing was run locally. Evidence is from GitHub run records and logs
(F44 full run 44728543060 on 2026-11-20; diff run 45032569782 and failed full run
45036569973 on 2026-11-22; the last 40 runs of qa.yml) and from the files named below.
**FACT** = read from a log, file or API. **INFERENCE** = reasoned. **UNKNOWN** = not measurable
from what CI prints. Durations in tables are measured from log timestamps unless marked.

## 1. What each leg costs

Full `qa-all` on F44, run 44728543060, 58.6 m of QA in a 60 m run (FACT, stage-line
timestamps):

| Leg                                            | Measured (m) | Share |
| ---------------------------------------------- | ------------ | ----- |
| Runner setup, tool install, ansible-galaxy     | 1.6          | 3%    |
| `go` (go-qa, both modules)                     | 31.7         | 54%   |
| go-deps, go-vuln, bash-limits, names, gitleaks | 0.2          | 0%    |
| `bash` stage (shellcheck over 569 files)       | 1.9          | 3%    |
| python, patterns, ansible, syntax, js, docs    | 0.5          | 1%    |
| `helper-tests` (5773 Python tests)             | 9.9          | 17%   |
| plan-tests                                     | 0.8          | 1%    |
| ~100 `test-*.bash` suites (41 are `ccy-*`)     | 11.9         | 20%   |

Biggest single suite: `ccy-teams-command` 2.9 m. Next: `ccy-image-build-lock` 1.2 m,
`ccy-container-version-hook` 0.9 m. The rest are seconds each.

Diff run 45032569782 (a ccy launcher change on a `ci/` branch), 43 m wall (FACT): setup about
1 m, shell and doc checks about 0.5 m, `go` **32.3 m**, six whole-tree semgrep gates 0.75 m,
45 bash suites 7.4 m. The Go leg in diff mode (32.3) cost the same as the Go leg in full mode
(31.7). Diff mode saved nothing for Go because a launcher change routes to "go whole"
(`scripts/qa-routing.tsv`, `CLAUDE/QA.md` row "The ccy launcher").

The same commit is judged twice: a `ci/` diff run (36 to 48 m with src-go) then an F44 full run
(60 to 82 m). The Go leg runs both times (INFERENCE from the two legs above: roughly double
the Go leg per change).

Go cannot be split finer from CI logs: `qa-go-qa.bash` captures go-qa and CI prints one line,
`✓ go: coverage passed` (FACT). Per-stage and per-package time is UNKNOWN.

## 2. Duplicated checks

All FACT from the files unless marked.

| Check                  | Where it runs more than once                                                                                                                | Cost                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------- |
| govulncheck            | go-qa `vuln` stage and gate `go-vuln` (`qa-go-vuln.bash`)                                                                                   | 3 s in CI               |
| `go mod tidy` / verify | go-qa `mod` stage and gate `go-deps`                                                                                                        | about 1 s (warm)        |
| gitleaks               | its own CI job `gitleaks secret scan` and gate `gitleaks` inside qa-all                                                                     | 3 to 7 s                |
| vet-style analysis     | golangci `govet`, go-qa `analyzers` (go vet -vettool), `go test`'s own vet, `teamchat-failpoints-vet`, `project-rules-tagged` (4 more vets) | UNKNOWN                 |
| golangci-lint          | whole module, then again over `internal/teamchat/...` with `-tags busfailpoints`                                                            | UNKNOWN                 |
| teamchat tests         | whole module under `-race -shuffle -count=1`, then teamchat again under `-race -shuffle -count=2 -tags busfailpoints`                       | UNKNOWN (large)         |
| launcher behaviour     | Go `ccylaunch/difftest` (runs the bash launcher over a scenario matrix) and about 41 `test-ccy-*.bash` suites                               | difftest: see section 4 |
| ccy-teams behaviour    | Go tests of `internal/teams` (57k lines) and `test-ccy-teams-command.bash`, which builds and drives the same Go binary                      | 2.9 m                   |

Not duplicated: shellcheck runs once (`qa-bash`). `hidden-bindings`, `lxc-attach`,
`bash-limits` and `patterns` each parse the shell files with a different parser for a
different rule, so they are distinct checks.

The go-qa test run and the coverage run are one run (FACT, go-qa-ci
`internal/runners/testrun.go`: "The same run supplies the test verdict ... counting
statements"). So coverage is not run twice, but it makes that run heavier (atomic mode under
`-race`).

## 3. Low value per unit of time

History (FACT, last 40 runs): 3 red F44 full runs were opened in the logs (45036569973,
44772504436, 44764003972). All three failed only on `helper-tests`. The Go gate was not the red
leg in any. The other non-green runs were 4 cancels and 4 failures at 0 m wall (INFERENCE:
start-up or config failures; not opened). So in this sample the Go gate is 54% of the full run
and never caught a failure, while helper-tests (17%) caught all three. Caveat: 40 runs is a
short window, and a gate that never fails may be a gate that is working.

Lowest value per unit of time (INFERENCE):

- `-race` on `ccylaunch/difftest`. The comment in `scripts/qa-go-qa.bash` says this one package
  takes 11 m under `-race` on a 4-vCPU runner (FACT). Its job is to run the bash launcher and
  fake tools as child processes. The race detector only instruments the Go test binary, which
  mostly waits for children. Little for `-race` to see.
- The `ccy-*` bash suites that rebuild Go binaries and drive them end to end, where Go unit
  tests of the same packages already run.
- The tagged-build lint, vet and rule passes over teamchat (section 4).

## 4. -race, -shuffle, the failpoint pass

- `-race` (FACT: built into go-qa, `testFlags = test -race -shuffle=on -count=1`). It finds
  data races; teamchat is concurrent and 213k lines. Cost: a multiple of the plain run in CPU
  and memory (INFERENCE). No catch is visible in CI history in the window above. Git log shows
  many "wait on the terminator, not on luck" style test repairs (for example 202e04679,
  235ee6175, 7f4eb06af); those are flaky-test fixes, not proof that `-race` caught anything.
  UNKNOWN how often `-race` ran red.
- `-shuffle=on`: no extra cost, it rides the same run. KEEP.
- The failpoint pass, `teamchat-failpoints-test` (`go-qa.toml:69-78`):
  `-race -shuffle=on -count=2 -tags busfailpoints ./internal/teamchat/...`. It catches (a) crash
  scenarios that exist only in that build (keeper, link, wiring rows), and (b) a test that
  leaves package state behind, because every test runs twice in one process. Cost: the biggest
  package group run a second time, twice over. Not measured (UNKNOWN); INFERENCE it is the
  second biggest block of the Go leg after the main test run. The stage has no `-count=1`, so it
  can hit Go's test cache when teamchat is unchanged, but CI has no cache (next section).
  Nothing in the sampled history shows it red.

## 5. Test cache

- **FACT** CI restores no Go cache. The log says
  `##[warning]Restore cache failed: Dependencies file is not found ...` (seen on the `base`
  job; the `qa` job has the same setup step). `actions/setup-go` looks for `go.sum` at the repo
  root; it is in `src-go/` and `src-go/qa/`. Fix: `cache-dependency-path` on both setup-go
  steps (`qa.yml:83-86`, `155-158`).
- **FACT** go-qa's built-in test run uses `-count=1`, which turns Go's test cache off for the
  main test run, locally and in CI. A cache restore alone will not skip tests there. It would
  save only compiling (module download, and compile of dependencies and unchanged packages
  under `-race`). INFERENCE: a modest saving, not the largest win the proposal hoped for.
- **FACT** the failpoint stage has `-count=2` and no `-count=1`. INFERENCE: a cached result is
  possible locally when teamchat is untouched; in CI never (empty cache each run). Whether
  `-shuffle=on` also blocks caching: UNKNOWN.
- So PROPOSAL section 7 item 1 ("Go cache is likely the largest single win") is too
  optimistic. The main test run is uncached by design; to skip tests, a unit must be skipped,
  not cached.
- Locally the QA lock runs go-qa at nice 19 and ionice idle (FACT, `qa-go-qa.bash`). The gate
  yields to everything else, so it stretches as far as the owner's machine is busy.

## 6. What `qa-changed` runs locally for a typical Go change

For a change under `src-go/internal` (FACT from `qa-routing.tsv`, `go-qa.toml`, the 22 Nov diff
log):

1. go-qa diff mode: golangci-lint, vet and analyzers, deadcode, govulncheck and module checks
   (whole-module by go-qa's own rules), build, then `go test -race -shuffle=on -count=1` of the
   changed packages **and their importers**, with a coverage profile. A change to the launcher,
   any `src-go/qa/` file or `go.mod` makes it whole.
2. Project stages, which take no `{files}` and so always run whole: failpoints lint, vet and
   `-count=2` race test of all teamchat; four tagged project-rule vets (builds the driver
   first); four static cross-builds (`amd64` and `arm64` for `teamchat` and `teamchat-admin`).
3. Six whole-tree semgrep gates, because the row `src-go/internal/*` triggers them: arch-pin,
   argv-leak, boot-hold, gh-poll, place-issued-token, secret-screen. 0.75 m in CI (FACT).
4. Every bash suite whose Go build closure holds a changed file (`go-binary-suites.tsv`):
   45 suites, 7.4 m in the 22 Nov run.

Why it is heavy on a laptop: `-race` multiplies CPU and memory; the importers fan-out of a
shared package (`execx`, `ui`, `term`) pulls in most of the module; the teamchat stages are whole
every time; the test cache is off by `-count=1`; everything runs serially at nice 19.

## 7. Short list for the owner

Savings are FACT only where the cost was measured in a log; otherwise INFERENCE.

01. **Stop running `-race` on `ccylaunch/difftest`.** Cost FACT 11 m per Go run; saving
    INFERENCE (nearly all of it). Needs a go-qa package exclusion or a split stage. **CUT**
02. **Teamchat failpoint pass (`-count=2`, tagged lint and vet).** Cost UNKNOWN, INFERENCE large;
    no red seen. **MOVE TO NIGHTLY**
03. **Go gate full run on F44 after a `ci/` pass of the same commit.** FACT 31.7 m per push.
    Keep diff mode on `ci/`. **MOVE TO NIGHTLY**
04. **Local `qa-changed` on Go: drop `-race`, coverage, the four cross-builds, the tagged rule
    vets and the six semgrep gates (0.75 m, FACT).** Keep build, vet, changed-package tests.
    Saving INFERENCE, the larger part of a local Go run. **MOVE TO CI ONLY (not local)**
05. **`ccy-teams-command` (2.9 m, FACT) and other `ccy-*` suites that drive Go binaries already
    unit tested.** The ccy and run-bash families total 11.9 m (FACT); how much overlaps is
    INFERENCE. **MOVE TO NIGHTLY**
06. **Fix the Go cache key (`cache-dependency-path`).** Saves compile only, not tests
    (`-count=1`); size INFERENCE modest. **KEEP and fix**
07. **`go-vuln`, `go-deps` and the gitleaks gate duplicate go-qa stages and a separate CI job.**
    FACT seconds each, so no real time saved; saves a second place to maintain. **CUT**
08. **`helper-tests` (9.9 m, FACT).** It is the gate that went red 3 of 3 times. Retire v1
    (INFERENCE halves it) and run it in parallel; do not cut. **KEEP**
09. **`-shuffle=on`.** No extra cost. **KEEP**
10. **Print per-stage and per-package time in the go gate.** Today the CI log shows one line, so
    items 1 to 3 rest on INFERENCE. Do this first. **KEEP (add)**
