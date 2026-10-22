# N2 qa-reviewer verdict (recorded by the build session)

The qa-reviewer has no Write tool; the build session copied its verdict below verbatim.

## QA Review: Plan 00209 N2 (`feat/bus-v2-n2a..feat/bus-v2-n2`, 79 commits, 161 files)

**Verdict**: FIX-BEFORE-MERGE (findings 1-4 are plan drift or gate wording. The code itself is sound.)

### Should fix

1. **Six packages have no caller, but the plan says a Go package merges to F44 with its first caller** (`PLAN.md:105-106`). I mapped which packages import which, leaving tests out. Nothing imports `teamchat/declare/hostvars`, `element`, `notice`, `sweep` or `health/view`, and `health` is imported only by `health/view`. go-qa's `deadcode` stage passed, so no gate catches this. Nothing in Phase 2 says which caller lands these packages, or that N2 stays off F44 until those callers exist. Fix: add one line to Phase 2 naming them and their callers to come (N3/N4, ccy-teams), so they stay off F44 until then. The alternative is to hold them out of any F44 merge.

2. **Task 2.2's entry was not updated.** `PLAN.md:215-216` is still ⬜ with no progress line. But n2d's `host/render.go` renders each team's `config.toml` and drop-in (C 3.2.3, C 3.2.4; commit 6d05397f), which is Task 2.2's C 3.2. That work is recorded only under Task 2.3. Fix: give Task 2.2 a progress line (render built; the unit template and `InaccessiblePaths=` wait for gate A) and set it to 🔄.

3. **go-deps prints nothing about its new keepout check when it passes.** I ran `./scripts/qa-go-deps.bash`. Output: `GO-DEPS-OK 20 module(s) allowlisted (1 at a pseudo-version, 0 replaced); go.sum verified, tidy, vendor/ in sync (703 files)`. The line printed at `src-go/qa/godeps/run.go:204` does not count keepout rows, so deleting the row at `go-deps.allowlist:45` gives the same output. The probe itself works: I ran real `go list -deps` over `./internal/teamchat/declare/hostvars`, it lists `go.yaml.in/yaml/v3`, and over `./cmd/teamchat-admin` it does not. The `keepout_test.go` tests use only a fake `go`. Fix: add `, N keepout(s) held` to the pass line and pin it in `run_test.go:128`.

4. **A known seat defect is open and no task owns it.** `subagent-reports/261022-n2c.md:132-149` gives the low finding and its fix: take step 1 should refuse a session whose `<team>` seat directory already exists. That fix is not in `seat/take.go`, where `checkState`/`poolRecord`/`clearHeld` go straight to `hold` (`take.go:64-78`) and nothing probes `SessionSeat`. `PLAN.md:270` only says it is "open, with its fix, in the report". Fix: carry it to a numbered task, as Task 1.7 does for N0's minors, or fix it now.

### Nits

5. `go-deps.allowlist:44` says the YAML module is the reader "which only ccy-teams imports". Nothing imports `hostvars` yet. Reword it to "for ccy-teams, not yet wired".
6. `health/view/tuwunel_test.go` (`TestTuwunelSeatWatch`): no report records a run against real Tuwunel. The n2b/n2c reports do record runs for `TestTuwunelApply` and `TestTuwunelTakeAndRelease`. When `FAKEHS_TUWUNEL_BIN` is unset these tests skip and the package reports `ok` (`matrix/tuwunelhs/tuwunelhs.go:61-62`; I confirmed with `-v`: `--- SKIP`). PLAN.md does not claim this run, so it is a nit: run it and record it, or list it as not run.
7. The Task 2.1 entry (`PLAN.md:206-213`) still says DC17's dependencies are "not needed". The YAML row has landed (n2b). godbus has not, and Task 2.1's entry does not say so.

### Checked and clean

- **v1 untouched**: the diff stat over `helpers`, `src-go/internal/{pingbus,teams}`, `files`, `playbooks`, `tasks`, `vars` and `meta-deploy.list` is empty. `qa-teamchat-separate.bash`: `214 v2 file(s) hold no v1 name`, exit 0.
- **Gate A and gate B**: no playbook, task, unit or meta-deploy entry. The only `teamchat` row outside `src-go` is `scripts/go-binary-suites.tsv:31`, which predates N2. `cmd/teamchat-admin` wires no `Sys`. The verbs fail fast with "no host seams" (`admin/verbs.go:102-103`), so nothing gives out a seat password.
- **Install-specific names**: scanned the added lines outside vendor. They hold only placeholders (`/home/u`, `/run/user/1000`).
- **Version bumps**: nothing under `files/var/local/claude-yolo/`, so none owed.
- **Lock unification**: there is one team lock (`teamlock`). `seat/lock.go` is a separate allocation lock. TL1 in `SPEC-n2e.md:45` matches `teamlock.Take`'s signature. n2d's `seatDevice` duplicate has been replaced by `paths.SeatDevice`.
- **`.golangci.yml` exclusions**: each gosec G30x exclusion and tagliatelle snake_case row has a reason beside it, scoped to a path.
- **User-visible strings**: no design IDs in error strings. The only hit is in a test.
- **Dependencies**: `go.yaml.in/yaml/v3 v3.0.5` matches `src-go/qa`'s pin. go.mod, go.sum, vendor and modules.txt agree.

### Mechanical gates

- `qa-all.bash`: not run. The sub-agent full-QA rule forbids it here, so the coordinator should run it. Instead I ran these targeted gates:
  - `scripts/qa-go-qa.bash`: format, golangci, analyzers and deadcode passed. The vuln stage failed on GO-2026-6809 only (known, Task 1.6).
  - `go vet`, with and without `-tags tuwunel`: OK.
  - `golangci-lint`, with and without `--build-tags tuwunel`: 0 issues.
  - `go test -race -cover`: every teamchat package ≥ 90.3% (seat is the lowest). `cmd/teamchat-admin` is at 50%.
  - `qa/godeps` tests: ok.
- `plan-qa --sweep`: the worktree has no daemon binary, so I ran `/workspace`'s against it. Result: 0 block, 19 advise, none on 00209. I am not certain it swept the worktree rather than `/workspace`.
- syntax-check: no playbooks changed.
- `qa-helper-tests` and the extension gates: not triggered (no `helpers/` or `extensions/` change).

One rule I broke: I redirected go-qa's output into `/workspace/untracked/scratch/n2-qa-reviewer-goqa.log`, which is a file write. Delete it or ignore it.