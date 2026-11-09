# leakcheck moves to the shared `internal/leakcheck` (Plan 00209 Phase 8)

Owner's decision on pull request 124: "Move `leakcheck` into a shared package
(`internal/leakcheck`)." Done: `src-go/internal/teamchat/leakcheck` is now
`src-go/internal/leakcheck`, same package name, same API, no behaviour change (the two files
moved with `git mv`, 0 lines changed).

## Commits

- `19488a324` the move, the 14 import updates, the fixture-stub doc and `CLAUDE/GoProjectRules.md`.
- The commit that adds this report (the report names the first commit's hash, so it follows it).

## File classes touched (18 paths in the first commit)

- Moved: `leakcheck.go`, `leakcheck_test.go`.
- Importers, import path only (13 under `src-go/internal/teamchat/`, 1 in `src-go/internal/ccc/guard_test.go`).
  In `engine/engine_test.go`, `engine/deliver_test.go` and `link/process_test.go` the line also moved
  up one place, because `internal/leakcheck` sorts before `internal/teamchat/...` (gofmt clean).
- Project-rules fixture: the doc comment of the stub `src-go/qa/rules/testdata/src/fix/internal/leakcheck/leakcheck.go`
  (it named the old path; code unchanged).
- Docs: `CLAUDE/GoProjectRules.md` (one line, the run-time half's path).

Looked at and needing no change: the rules `leakcheckonce.go` and `goroutineend.go` (they match the
package by name, `fn.Pkg().Name() == "leakcheck"`, never by path); the fixtures `twicecheck` and
`checkedgo` (they import the stub, whose path is unchanged); `src-go/qa/teamchatseparate` data and
code; `scripts/qa-gates.tsv`, `CLAUDE/QA.md`, `src-go/CLAUDE.md`, `src-go/.golangci.yml`,
`src-go/qa/go-qa.toml` (none names the package); live plan text (the only remaining mentions of
the old path are in `JOURNAL/` and `subagent-reports/`, append-only, left as they are).

## How the rules stay exactly as strong

The rules never named the path, so there was nothing to narrow. Proven, not assumed:

- `go test ./rules/... ./teamchatseparate/... ./cmd/qa-teamchat-separate/...` in `src-go/qa`: all `ok`
  (the rules' fixtures still show red where they showed red, `badgo`, `twicecheck`, and green on `checkedgo`).
- Red/green on the real new path (throwaway test-only packages, deleted again, not committed): a test
  calling `leakcheck.Check(t)` twice and a bare goroutine in a package with no leak TestMain gave
  `project_leakcheck_once` and `project_goroutine_end` findings from the driver; a package whose
  TestMain is `os.Exit(leakcheck.Tests(m).Run())`, one `Check`, and the same bare goroutine gave none.
  Both imports were `internal/leakcheck`.
- `teamchat-keeps-off-v1` (`scripts/qa-teamchat-separate.bash`, run on the real tree): exit 0,
  "587 v2 file(s) hold no v1 name". The two moved files leave its scope (`src-go/internal/teamchat/`);
  they are shared test support now, used by `ccc`, and hold none of the 14 v1 names (rg over the
  list found nothing). The gate's meaning (files v2 owns) is unchanged, so no data edit.

## Commands run (from `src-go` unless noted) and results

- `go build ./...`: exit 0.
- `go vet ./internal/...`: exit 0; `go vet -tags busfailpoints ./internal/teamchat/...`: exit 0;
  `go vet -tags tuwunel ./internal/teamchat/...`: exit 0.
- `go test -race -shuffle=on ./internal/leakcheck/... ./internal/ccc/... ./internal/teamchat/...`:
  exit 0, 64 packages `ok` (the slowest `wiring`, 166 s).
- `gofmt -l` on the 17 changed Go files: nothing listed.
- In `src-go/qa`: `go test ./rules/... ./teamchatseparate/... ./cmd/qa-teamchat-separate/...`: all `ok`.
- Project-rules driver (`go -C qa build -o <scratch>/driver ./driver`, then `go vet -vettool=<driver>`):
  `./internal/...` untagged, and `./internal/teamchat/...` with `busfailpoints` and with `tuwunel`: each
  exit 1 with the same 5 findings, none about leakcheck:
  `internal/teamchat/clock/clock.go:60,70,73` (`project_realtime_bound`, three) and
  `internal/teamchat/matrix/sync.go:74` and `matrix/fakehs/session.go:10` (`project_liveness_word`,
  "online"). They are in files this change did not touch and in packages that do not import
  leakcheck (clock is imported by it, never the reverse), so they predate it. The driver on
  `./internal/leakcheck/` alone: no finding.

## Not run / for the owner

- Not run: `scripts/qa-all.bash` and `scripts/qa-changed.bash` (full gates are blocked here), golangci-lint
  (the edit hook ran it per file; one parallel-run refusal was a hook artefact, not a finding), a baseline
  driver run on the commit before the move (the five findings are in untouched files, argued above).
- Remaining coupling: `internal/leakcheck` still imports `internal/teamchat/clock` (its settle waits on a
  `clock.Clock`; `leakcheck_test.go` imports it too). So `ccc`'s test now depends on a shared package, but
  that package still depends on a teamchat one. Removing it means moving or duplicating `clock` too, which
  the decision did not ask for and I did not do: the owner's call.
- Stages `teamchat-failpoints-*` and `project-rules-tagged` in `src-go/qa/go-qa.toml` select `./internal/teamchat/...`;
  leakcheck leaves that selection. It has no tagged files, and the untagged stages (`./...`) judge it.
