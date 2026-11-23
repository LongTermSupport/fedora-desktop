# Interim CI speed-ups (PROPOSAL.md section 7, items 1 to 3)

Branch `ci-interim-speedups`, built on F44 `2098bcc49`. Proven on `ci/ci-interim-speedups`
(two-push from the F44 tip). Not pushed to F44.

Local checks were light only, because the host was under load: `go test` of the touched packages'
targeted tests (seconds each), the YAML parse of `qa.yml`, the workflow partition test. No
`qa-changed`, `qa-all`, semgrep or golangci by hand. The edit hook did run golangci-lint and
`go vet` on each edited Go package; the commit hooks ran only the private-names and secret scans,
and the pre-push hook ran private-names, gitleaks and the push gate (seconds each). `actionlint`
is not installed in the container, so GitHub itself checked the workflow on the proving run.

## 1. The Go cache

**What was wrong.** `actions/setup-go` (default `cache: true`) looked for `go.sum` at the
repository root, found none (it is in `src-go/` and `src-go/qa/`), and restored and saved nothing.

**The change.** In the QA parts, `cache-dependency-path` names `src-go/go.sum` and
`src-go/qa/go.sum` (both exist; they are the two modules' sums). `cache` is on for the `go` part
only, and off in the `base` job. Reason: setup-go keys the cache on the go.sum hashes alone and
saves it once per key, from the first job that ends; the `base` job or the helpers part would
otherwise save a near-empty cache under the key, and the Go part could never save its own.

**What the cache does and does not buy.**

- go-qa's built-in test runner always runs `go test -race -shuffle=on -count=1`
  (go-qa-ci `internal/runners/testrun.go`, `testFlags`), and the coverage pass shares that run.
  `-count=1` bypasses the test result cache, so **the main test run is re-run every time**. Only
  compiling (packages and race-instrumented test binaries), `go vet` results and the module
  cache are saved. Changing `-count` is the owner's call; nothing here changes it.
- The `teamchat-failpoints-test` stage (`go-qa.toml`, `-count=2`, no `-count=1`) can hit the test
  cache when teamchat did not change.
- golangci-lint's own cache (`~/.cache/golangci-lint`) and go-qa's tool store
  (`~/.cache/go-qa/tools`) are not in setup-go's cache. Caching them (actions/cache) is a possible
  next step.
- **Limitation of setup-go's cache:** the key changes only when a go.sum changes, and a key is
  saved once. The first F44 run after this lands saves GOCACHE; later runs restore that snapshot
  and recompile whatever changed since, so it goes stale until a go.sum changes. A rolling cache
  (actions/cache with `github.sha` in the key and a `restore-keys` prefix) would stay fresh, at
  the cost of a save each run against the repository's 10 GB cache limit.
- A `ci/` branch restores caches of the default branch (F44) and its own. The proving run was
  cold, because F44 has never saved one.

## 2. QA split into parallel parts

- `qa-all` and `qa-changed` take `--only LIST` or `--except LIST` (comma-separated gate names, or
  qa-changed check ids). One list given to both runs each gate exactly once. An unknown name, an
  empty name, both flags, or a subset that leaves nothing is exit 2 before anything runs. A
  subset `qa-all` neither clears nor writes the local verdict record (`qaverdict`). qa-changed's
  `--only` plans no suites, `--except` keeps them, and a `note:` line says what was left out.
- `qa.yml`: job `qa-part` is a matrix of `go` (`--only go`), `helpers` (`--only helper-tests`,
  diff mode `--only unit-tests`) and `rest` (`--except` both); `fail-fast: false`. Job `qa`
  (still named `QA (<mode>)`) needs `base` and `qa-part`, runs unless the run was cancelled, and
  fails unless both results are `success`, so a failed, skipped or cancelled part fails it.
- **The "CI passed" contract.** `src-go/qa/pushgate` (the pre-push hook) and the release
  (`release.yml`, `scripts/release.bash`) read the *workflow run's* conclusion for the commit
  (`completed` and `success`), not a job. A run concludes `success` only if no job failed. No
  ruleset has required status checks (checked with `gh api .../rulesets`). So the contract holds
  unchanged, and the `qa` job also gives one named check if one is ever required.
- `TestTheQAPartsRunEveryGateOnce` (`src-go/qa/toolchain/qaparts_test.go`) reads `qa.yml`: for both
  modes the `--except` list must equal the union of the `--only` lists, with no duplicate and
  exactly one `--except` part; every full-mode name must be a gate of `qa-gates.tsv`; and the `qa`
  job must need `qa-part` and run after a failed one. `TestEachBrokenPartitionIsFound` proves
  each broken shape is caught.
- The merge-commit ccy-version judgement runs in the `rest` part only. Every part installs the
  full toolchain, so `qa-toolchain.bash` asserts the same versions everywhere.
- Unit tests: `src-go/qa/all/subset_test.go`, `src-go/qa/changed/subset_test.go`. The golden
  deviations `help.json` and `unknown-flag.json` of both tools were updated for the new usage text.
- Bash: untouched (no new bash). `CLAUDE/QA.md` (Primary Rule) describes the parts.

**Not proven on GitHub before F44:** a `ci/` push runs diff mode only, so the full-mode
`qa-all --only/--except` path runs for the first time on the F44 push. It is covered by the unit
tests and the partition test. If it fails there, it is fixed forward as usual.

## 3. Sharding the Go gate: not done

It is not simple. go-qa runs over whole modules and has no package-subset option for a full run
(`-p` limits paths, `--changed` picks its own scope). The 90% coverage floor is computed per
module over all its packages in the same `go test` run, so splitting teamchat out would either
lose the floor or need coverage profiles merged across jobs. The project rules, deadcode and
govulncheck also judge the whole module. Doing it properly needs a go-qa-ci change upstream (a
package-set option with a coverage merge), or moving teamchat to its own module (the split this
plan weighs). The parts already take the helper tests and the other gates off the Go gate's path.

## Per-stage and per-package timing in the Go gate (QA cost audit request)

go-qa (go-qa-ci, an external module pinned by `src-go/qa/go.mod`) prints no time per stage and
has no option for it (`go-qa run` takes only `-t`, `-p`, `--ci`, `--no-fix`, `--fail-fast`,
`--changed`, `--base`). Its test runner captures `go test` output without `-json`, so per-package
times are not available either, and qa-all hides a passing gate's output. Either needs a change
**upstream in go-qa-ci** (a stage-duration line, and `go test -json` package times). Nothing was
hacked around it here. The whole Go gate's time is now visible as the `go` part's job time.

## Wall times

Measured from GitHub's run records (`gh run view --json`).

**Before** (one `qa` job, no Go cache), the last runs:

| Run kind                                  | Wall time of the run               |
| ----------------------------------------- | ---------------------------------- |
| Full `qa-all` on F44 (8 runs, Nov 18-22)  | 60, 61, 67, 69, 75, 76, 76, 78 min |
| Diff gate, `ci/` branch touching `src-go` | 42, 43, 46, 47, 48 min             |

**After**, on `ci/ci-interim-speedups` (diff mode, this branch changes the `qa` module):

| Run                                         | Run wall | go part | helpers part | rest part | go check itself |
| ------------------------------------------- | -------- | ------- | ------------ | --------- | --------------- |
| 45153935845 on `64c62e5b9` (cache cold)     | 41.6 min | 41.1    | 2.0          | 3.0       | 39.3            |
| 45160388875 on `f2a002378` (cache restored) | 30.9 min | 30.4    | 1.6          | 3.4       | 28.8            |

Both runs: conclusion `success`, every part `success`, the `QA (diff)` verdict job `success`.
The restored cache was 635 MB and took 7 s to restore. The warm Go check ran the same diff
scope 10.5 min faster than the cold one; that is the build and vet cache, since the main test
pass is `-count=1`.

**What this means for the full run (INFERENCE, to be measured on the first F44 runs):** the full
run becomes the Go part: its gate (38.7 min cold on 2026-11-22) plus the toolchain install,
instead of the sum of every gate. Expected near 40 min cold and lower once F44's own cache is
saved, against 60 to 78 min before. A `ci/` run with no `src-go` change stays a few minutes. A
`ci/` run restores F44's cache once F44 has saved one; until then each `ci/` branch's first
run is cold.

## How the commits must land

The pre-push gate accepts a protected-branch move only to a commit whose own CI run passed. The
CI-proven commit is `f2a002378`. The report commit on top changes only this file (docs), so it
gets no CI run of its own: push `f2a002378` to F44 first, then the report commit, which the
gate then judges locally as a docs-only range.
