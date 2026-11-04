# Adversarial review: go-qa-ci `feat/build-stage` (6eb4ab5)

Read-only review of `git diff main...feat/build-stage` in `<repo>/untracked/projects/go-qa-ci`.
Nothing changed, nothing committed. Every claim below that is marked **verified** was reproduced
with the installed `go1.26.9 linux/amd64` on a throwaway module.

## What the change does

Adds a `build` runner (`internal/runners/build.go`, wired in `internal/tools/runners.go:150`,
`executor.go:63`, `tools.go:42`, `config.go:142`) that runs `go build <patterns>` in each module
with `GOWORK=off` and `GOFLAGS=$GOFLAGS -mod=mod`. In `--changed` mode it uses the same
changed-packages-plus-importers set as `test`. Docs, catalogue tests, cli listing and the self-gate
tool list are updated. The purpose (catching a production file that calls a `_test.go`-only helper)
is real and the two fixtures prove it: `go test` passes the fixture and `go build` fails it.

## Findings

### F1. Single `main` package: the gate writes a binary into the module directory — HIGH

- `internal/runners/build.go:27` — `go build` with no `-o`.
- **Verified**: a module whose only package is `main` (`go build ./...`) leaves `./<module>` binary
  in the module dir. A narrowed run with one pattern that is a `main` package
  (`go build ./cmd/x`) does the same. Multi-package invocations discard output, which is why the
  fixtures in `runners_test.go` with `main.go` + nested module did not reveal it (TempDir hides the
  file; the root module there *is* a single-main module and does write `fixture`).
- Scenario: a consumer with `cmd/foo/main.go` and nothing else, or `run --changed` after editing
  only `cmd/foo`, gets an untracked binary in its tree. The next `--changed` plan then sees a new
  file (and if it is under a package dir, a Go-ignored but still "changed" path). Tools that gate
  on a clean tree (the self-gate's suppression step already trips on a stray file per the builder's
  report) will fail for a reason the user did not cause.
- Fix: pass `-o os.DevNull`. **Verified** that `go build -o /dev/null ./...` and
  `go build -o /dev/null ./cmd/x ./lib` both succeed with multiple packages (the go command treats
  `/dev/null` specially). Add a test: single-main module, assert the module dir has no new entries
  after `Build`.

### F2. `-mod=mod` lets a QA gate rewrite `go.mod` and `go.sum` — HIGH

- `internal/runners/build.go:14`.
- **Verified**: with a production import that `go.mod` does not require, `GOFLAGS=-mod=mod go build`
  downloaded the module, added `require golang.org/x/text v0.43.0` to `go.mod`, created `go.sum`,
  and rewrote the `go` directive from `1.26` to `1.26.0`. `-mod=readonly` fails with
  `import lookup disabled by -mod=readonly` and touches nothing.
- The `mod` runner (`go mod tidy -diff`) does catch the missing requirement and, because
  `pipeline.Execute` (`internal/pipeline/pipeline.go:128`) stops before the next runner once any
  step failed, `build` does not run after a `mod` failure in a full run. The guard is absent when
  `mod = false` in the consumer's config, when the user runs `-t build`, or in any future reordering.
  A gate that can mutate the tree it judges is wrong regardless of what usually runs before it.
- The stated reason for `-mod=mod` ("so a vendor directory cannot hide a missing requirement") is
  backwards; see F3.
- Fix: use `-mod=readonly` explicitly (not the default, so a caller's `GOFLAGS=-mod=mod` cannot
  reintroduce the write; **verified** that the last `-mod=` in GOFLAGS wins and duplicates are
  accepted). Add a test asserting `go.mod`/`go.sum` bytes are unchanged after a failing build.

### F3. `-mod=mod` masks a non-vendored import in a vendored project and needs the network — MEDIUM

- `internal/runners/build.go:11-14`, `docs/runners.md:27`.
- **Verified**: module with `vendor/` present; production file imports a package of an already
  required module that is not in `vendor/modules.txt`. Default (`-mod=vendor`) build fails;
  `-mod=mod` build **passes** (after downloading); `go mod tidy -diff` exits 0, so `mod` does not
  catch it either. The `test` runner, which runs with default flags, is the only step that would.
- So the build gate judges a different program than the consumer ships, and a vendored project
  that vendors precisely to build offline now needs `GOPROXY` reachable for `build` alone.
- Fix: `-mod=readonly` honours vendor mode when `vendor/` exists (go >= 1.14 semantics) and still
  forbids edits; that is the right flag for a gate. If the author wants "vendor cannot hide a
  requirement", that is `go mod vendor` consistency, a separate check, not this one.

### F4. `GOWORK=off` makes `build` the one step that fails a legitimate `go.work` project — HIGH

- `internal/runners/build.go:15`; `internal<home><home>.go:15-28` makes the go.work directory the
  project root, so workspaces are a supported layout.
- **Verified**: workspace `use (./ma ./mb)`, `ma` imports `example.com/mb`, no `require` in
  `ma/go.mod` (the normal workspace shape). `go test ./...` in `ma` passes under the workspace;
  `GOWORK=off GOFLAGS=-mod=mod go build ./...` fails with `cannot find module providing package example.com/mb`. With a reachable proxy and `-mod=mod` it would instead try to *fetch*
  `example.com/mb` and, if a module of that path is published, write that version into `go.mod`
  (F2) and compile against the wrong code.
- `changed.listPackages` (`internal/changed/packages.go:52`), `test`, `mod`, `vuln`, `golangci`
  all run under the caller's workspace, so `build` alone sees a different module graph.
- The analogy to `goInstallEnv` (`internal/toolstore/store.go:321`) does not hold: installing a
  pinned tool must ignore the consumer's module; compiling the consumer's module must not.
- Fix: drop `GOWORK=off` (and the extra env entirely once F2 is `-mod=readonly`). Replace
  `TestBuildIgnoresTheCallersWorkspace` with a test that a two-module workspace builds.

### F5. `GOFLAGS` env overrides and discards `go env -w GOFLAGS` — LOW

- `internal/runners/build.go:14` reads only the process environment.
- **Verified**: with `GOFLAGS=-tags=special` set through the go env file and no env var, `go env GOFLAGS` is `-tags=special`; setting the env var to `" -mod=mod"` yields ` -mod=mod` (file value
  dropped). A consumer that configured tags via `go env -w` gets them in every other step and not in
  `build`, so a tag-gated file compiles differently here than in `test`.
- Fix: falls away with F2/F4 (no env override). If an override is kept, read `go env GOFLAGS`
  first, not `os.Getenv`.

### F6. Narrowed-mode test never exercises an importer failing to build — MEDIUM

- `internal/tools/diff_test.go:218` `TestDiffBuildCoversTheChangedPackageAndItsImportersNotAnUnrelatedOne`.
- Both assertions break the *changed* package (`leaf`) or prove `other` is not built. No case
  changes `leaf` so that it still compiles but `mid` (the importer, `diff_test.go:21`) does not,
  e.g. renaming `Leaf`. That is exactly the scenario the doc comment (`runners.go:148-149`,
  `docs/runners.md:30-31`) uses to justify building importers, and it is untested. The expansion
  itself (`packages.go:107-146`, BFS over `Imports+TestImports+XTestImports`) is correct and
  over-approximates harmlessly, so this is a test gap, not a logic bug.
- Fix: add a case with `leaf/leaf.go` renaming `Leaf` to `Renamed`; assert `build` fails and
  stderr names `<diffMod>/mid`.

### F7. "Every package compiled" is oversold: constraints, cgo and linking — LOW

- `docs/runners.md:21-25`, `README.md:40`.
- `go build ./...` compiles only files selected by the current `GOOS`/`GOARCH`/tags; a
  `//go:build windows` or `//go:build integration` file that calls a test-only helper is not
  compiled, same as `vet`/`test`. Non-main packages are compiled but not linked, so linker-visible
  defects are not seen (neither are they by `vet`). `-race` is irrelevant (it is a `test` flag) and
  cgo follows `CGO_ENABLED` like every other step.
- Fix: one sentence in `docs/runners.md`: "for the host GOOS/GOARCH and the caller's build tags;
  files excluded by constraints are not compiled". No code change.

### F8. Residual stale lists and nested-module fixture — LOW

- `internal/runners/command.go:1-2` package doc lists `(vuln, mod, test, coverage, mutants)`.
- `internal/cli/run_test.go:75` asserts runners not started after a detector failure for
  `vuln, mod, test, coverage, mutants` only; `build` should be in that list (it would pass, it
  is the same phase, but the proof is the point).
- `internal/tools/runners_test.go:286` `TestBuildCoversANestedModule`: the root module is single-
  `main`, so the test currently writes `fixture` into the TempDir (F1) without noticing. After F1
  this test is the natural place to assert no binary appears.
- `docs/CONFORMANCE.md`, `docs/defences.md`, `docs/enumeration.md`: no tool list to update; the
  conformance test does not enumerate runners. `AGENTS.md:60`, `README.md`, `docs/runners.md`,
  `cli_test.go`, `tools_test.go`, `selfgate/runs_test.go` are consistent with each other.

## Answers to the six attack points

1. Build invocation: correct for the multi-package case; writes a binary for a single `main`
   (F1); constraints/cgo/linking as F7; `-race` not relevant. `-o /dev/null` is needed and works.
2. `-mod=mod` is the wrong flag for a gate (F2, F3). `mod` guards the full run only by phase
   ordering. Use `-mod=readonly`.
3. Importer expansion is correct; a build-only importer failure is caught in principle but has no
   test (F6).
4. Nested `modules` work (each built in its own dir; `./...` stops at nested `go.mod`). Vendored
   projects build a different program than they ship (F3). `go.work` projects break (F4).
5. Yes: both test packages run the real `go` on real temp modules, and the runners test proves
   the fixture passes `go test` before `go build` fails it. Gaps: F1, F2, F4, F6 scenarios.
6. Docs are consistent except the two low-value stale lists (F8); `docs/runners.md` states the
   `-mod=mod`/`GOWORK=off` behaviour accurately, which means the doc has to change with F2/F4.

## Verdict

**MERGE AFTER FIXES.** The gate is well-motivated, correctly wired through the catalogue, config,
pipeline phase, diff narrowing and self-gate, and its core fixture is genuine. It cannot merge as
is: it writes a binary into the consumer's tree (F1), can rewrite `go.mod`/`go.sum` (F2), and
fails every multi-module `go.work` project that the tool otherwise supports (F4). All three are
fixed by replacing `buildEnv()` with no env override and the arguments
`build -mod=readonly -o <os.DevNull> <patterns>`, plus the three tests named above and the doc line.
