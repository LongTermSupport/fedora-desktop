# go-qa-ci M13: diff mode (`go-qa run --changed [--base <ref>]`)

Built: new package `internal/changed` (git changed set, `go list` reverse-dependency closure across the
workspace modules, widening rules, summary text), `tools.Context.Diff`, `internal/tools/diff.go`
(narrowing per step), variadic package patterns on `golangci.Lint`, `vet.Tool.Vet`, `runners.Test`,
`runners.MeasureCoverage` (coverage's `-coverpkg` narrows with them), the `--changed` / `--base` flags,
summary mode and scope lines, README "Diff mode" section and `--help` text.

Spec clauses served: TOOLING-SPEC 4.5 (stage order and stop rules unchanged in diff mode), the
Blocking contract (a narrowed run still fails on any unlicensed finding in what it runs), and the
Project record obligations (record, docs and summary always run). Diff mode makes no conformance
claim; the README says the full run is the gate and a green narrowed run says nothing about what it
did not run (TOOLING-SPEC 9).

Behaviour: changed set = `git diff HEAD` + untracked non-ignored; with `--base` the merge base of
ref and HEAD plus working tree. Per-package engines run on changed packages plus transitive importers
(Imports, TestImports, XTestImports); file-scoped detectors on existing changed files; deadcode and
mutants run when any package is affected; vuln and mod are skipped (a go.mod/go.sum change widens).
Widening: go.mod, go.sum, go.work(.sum), tools.lock, .golangci.\*, qa/, the record path, the config file,
or a changed .go file whose package no longer exists. Stale-exception checks do not speak in a
narrowed run.

Tests: `internal/changed` (leaf/importer closure, unrelated package excluded, every widening file,
`--base` committed change and merge-base semantics, untracked included and ignored excluded, deleted
file, removed package, testdata, nested module, errors, Describe); `internal/tools/diff_test.go`
(real go test, golangci, coverage, suppression through the executor; widened; nothing changed);
`internal/cli/diff_test.go` (summary, --base, widening, explicit config file, no change exits 0 with
record run, usage errors, help).

Deviations: none from the brief. Choices: `-p` with `--changed`, and `--base` without `--changed`, are
usage errors; mutants and deadcode are not narrowed beyond "any package affected" (whole-program).
