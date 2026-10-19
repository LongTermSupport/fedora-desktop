# Plan 00191: Go CLI tooling (ccy-teams, then ccy-sessions, then ccy)

**Status**: In Progress
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: High

## Overview

The owner's decision (2026-10-13): this repository's large interactive tools leave bash.
`ccy-teams` is about 1,940 lines of bash with gum and fzf. A bug the owner hit on the host
(`back_to_list: command not found`, a function called before its definition on a branch no
test took) is the class of failure bash cannot rule out: names are bound when a line runs,
nothing is typed, and the only tests drive the whole script through a pseudo-terminal.
"We are burning way too much time using bash for stuff that is way beyond bash."

The tools are rebuilt as one Go module under `src-go/`: well architected, DRY, TDD,
strictly typed and linted, fast. `ccy-teams` comes first, then `ccy-sessions`, then
possibly the `ccy` launcher itself. In the same effort a new repository,
`LongTermSupport/go-qa-ci`, gives Go the QA/CI pipeline its siblings `rust-qa-ci`,
`ts-qa-ci` and `php-qa-ci` give theirs, and this repository's QA runs it.

The Python helpers the tools drive (`helpers/agent_bus/declare.py`, `element.py`,
`pingbus`, `agent-bus`) stay Python. Go calls them through the same seams the bash calls
today; rewriting them is out of scope.

## Goals

- `src-go/`: one Go module (go 1.26, Fedora 44's toolchain), `cmd/<tool>` thin mains over
  `internal/` packages shared by every tool, behind interfaces (command runner, prompter,
  clock, filesystem) so that every flow is unit tested without a terminal.
- `ccy-teams` in Go, behaviour-complete: the existing `scripts/test-ccy-teams-command.bash`
  suite passes against the Go binary unchanged (the parity oracle), plus Go unit tests for
  every flow. Then the bash version is deleted and the play deploys the binary.
- The toolchain by IaC: Go and golangci-lint pinned in the ccy image (`.claude/ccy/Dockerfile`,
  `.qa-versions`), on the host (`play-claude-yolo.yml` installs `golang` and builds the
  tools), and in CI.
- Go QA in every gate: gofmt, go vet, golangci-lint (strict config), `go test -race` with a
  coverage floor, govulncheck; in `qa-all.bash` and `qa-changed.bash`.
- `go-qa-ci`: a new public repository, built like `rust-qa-ci`, that this module's QA then
  runs.

## Non-Goals

- Rewriting the Python helpers, pingbus or agent-bus.
- A full-screen TUI redesign of ccy-teams in this phase: the port keeps the same prompts and
  the gum/fzf widgets so the existing suite is the oracle. A Bubble Tea UI is a later phase,
  with the owner looking at it.

## Tasks

### Phase 1: Toolchain and skeleton

- [x] ✅ **Task 1.1**: Go 1.26.9 and golangci-lint 2.14.0 in the ccy image
  (`.claude/ccy/Dockerfile`, `.qa-versions` GOLANGCI_LINT), checksum-verified; installed in
  the running container now (golangci-lint under `untracked/tools/bin` until the image is
  rebuilt).
- [x] ✅ **Task 1.2**: Host and CI. `playbooks/imports/play-golang.yml` (moved out of
  `optional/` once two core plays imported it) installs `golang`, all a build needs. `play-python.yml`, the one play that installs the QA
  tools, imports it and installs the Go QA toolchain itself: `gcc` (what `go test -race` links
  against) and golangci-lint at the `.qa-versions` pin (checksum-verified from the release's
  checksums file, probed so it is idempotent and a pin change is applied). `scripts/qa-toolchain.bash` asserts golangci-lint at the pin and
  `go` at no less than `src-go/go.mod` requires; `.github/workflows/qa.yml` installs both.
  `play-claude-yolo.yml` imports it too since Task 2.5, where it first builds a binary.
- [x] ✅ **Task 1.3**: `src-go/` skeleton: `go.mod`, `.golangci.yml` (strict), `cmd/`,
  `internal/` (execx runner + recording fake, prompter interface + gum/fzf implementation +
  fake). The two ported tools are what prove build, test and lint.
- [x] ✅ **Task 1.4**: `scripts/qa-go.bash` exists and is wired into `qa-all.bash` (a
  hard gate) and `qa-changed.bash` (any `src-go/` change): `golangci-lint fmt --diff` (any
  diff fails), `go vet`, `go test -race` with a whole-module statement coverage floor of 90%
  (measured 94.2%) and every package tested, `golangci-lint run`, whole module, a missing
  tool a hard failure.
  - Added `go build ./...` per module, before vet and test: a production file calling a
    helper defined only in a `_test.go` file passes vet, test and lint (all load test
    files) and breaks only the build. Red/green proven with a throwaway fixture package.
    go-qa-ci has the same stage, merged to its main at `7fc4547e92f2` after a Fable review
    (no binary left behind, go.mod and go.sum never rewritten, go.work and vendor/ respected).
  - Retired by Task 3.3: `scripts/qa-go-qa.bash` (go-qa-ci) is the Go gate now, and
    govulncheck, deadcode and the bundled analyzers came with it.

### Phase 2: ccy-teams in Go

- [x] ✅ **Task 2.1**: The functional spec of the bash ccy-teams:
  [SPEC-ccy-teams.md](SPEC-ccy-teams.md).
- [x] ✅ **Task 2.2**: Domain and adapters: teams/members/seats/humans/statuses; the
  declare.py, agent-bus, systemd, sessions-registry, Element, gh adapters; each unit tested
  (`internal/term`, `sudo`, `systemd`, `tmuxccy`, `gitx`, `registry`, `agentbus`, `declare`,
  `marker`).
- [x] ✅ **Task 2.3**: Flows: list, picker and keys, create wizard, apply, remove, password,
  Element, new session, attach, restore, park; start-up offers (`internal/teams`).
- [x] ✅ **Task 2.4**: Parity: `scripts/test-ccy-teams-command.bash` passes against the Go
  binary, 323/323. One assertion was mode-conditional (the spec's section 8 bug 11: the
  installer is called with `sudo -n`); the Go behaviour is now the only one.
- [x] ✅ **Task 2.5**: Cut-over: `play-claude-yolo.yml` imports `play-golang.yml`, builds
  the binary from the checkout (`-trimpath`, `main.RootDir` injected, installed only when the
  checksum differs) and installs it at the same path and mode; `ccy-teams.j2` is deleted;
  the suite builds the Go binary only; `qa-changed.bash` runs it for any `src-go/` file in
  the binary's build closure; docs updated; CCY 3.95.6. Done pending the owner's host run
  (`meta-deploy.bash` lists `play-claude-yolo.yml`).

### Phase 3: go-qa-ci

- [x] ✅ **Task 3.1**: Brief from rust-qa-ci, ts-qa-ci and php-qa-ci, and the Defence
  Before Fix toolchain spec read from its first-party repository:
  [RESEARCH-go-qa-ci.md](RESEARCH-go-qa-ci.md).

- [x] ✅ **Task 3.2**: Create `LongTermSupport/go-qa-ci` (created private; the owner makes it
  public after review) and build it by milestones, self-hosted. It is a generic DBF toolchain
  for Go: it names no consumer. Milestones M0-M14 each passed an independent verifier; its
  conformance is declared with recorded gaps, never claimed in full. The verifiers' deferred
  minor items are in `subagent-reports/261014-go-qa-ci-deferred-run*.md`, for a polish pass.

- [x] ✅ **Task 3.3**: This repository's Go QA runs go-qa-ci, pinned. `scripts/qa-go-qa.bash`
  runs `go-qa run --no-fix` over both modules; go-qa is pinned at go-qa-ci `7fc4547e92f2` by the
  `tool` directive of `src-go/qa/go.mod` (one pin for the driver import and the binary) and
  configured by `src-go/qa/go-qa.toml`; it installs its own pinned tools (Task 3.7).
  `scripts/qa-go.bash` is deleted. qa-all runs the gate whole, qa-changed in diff mode for a
  `src-go/` change; the toolchain gate takes the higher go directive of the two modules and holds
  the `.qa-versions` golangci-lint pin to go-qa-ci's `tools.lock`. The bundled analyzers'
  findings (map order, a wall-clock test, `os.Exit` outside `main`) are fixed with no
  suppression; the `TestMain` re-exec is `qa/internal/cmdmaintest`. semgrep timed out clean
  files on a loaded host (its 5 s default per rule per file is wall time): every semgrep scan now
  passes `--timeout=120` (`semgrepreport.TimeoutFlag`), and the large-script test pads with
  comments. Full gate green, coverage
  included. Report: `subagent-reports/261019-go-qa-ci-pin-opus.md`.

- [ ] ⬜ **Task 3.8**: The owner signs a go-qa project record for this repository (go-qa-ci adopt
  step B2), then `[tools] record` and `suppression` are switched on in `src-go/qa/go-qa.toml`.
  Both are off until then: without a record every `.golangci.yml` exclusion would fail the
  suppression detector. `qa-go.bash` had neither check, so nothing was lost by Task 3.3.

- [x] ✅ **Task 3.4**: go-qa-ci diff mode, `go-qa run --changed [--base <ref>]` (M13):
  per-package engines on the affected packages and their importers, repo-wide defences always,
  and a change to `go.mod`, `go.sum`, the golangci config, `qa/` or the tool lock widens it to
  everything. This repository's `qa-changed.bash` calls it with Task 3.3.

- [x] ✅ **Task 3.7**: Installing go-qa-ci provides every tool it runs, with nothing further
  required (owner, 2026-10-15: "simple and reliable"). go-qa embeds `tools.lock` and installs
  each pinned tool into a cache it manages: release archives are checked against the pinned
  sha256 before extracting, `[go]` tools are built with `go install`, and each tool is moved into
  place atomically. `go-qa run` installs any that are missing, so `go install` plus `go-qa run`
  is the whole setup. Built test first. A Fable review found no blockers. Its two medium
  findings are fixed:

  - `GOFLAGS=-mod=mod` and `GOWORK=off`, so a project that vendors can use it;
  - Go-built tools are keyed by the Go version that built them, so a toolchain upgrade
    rebuilds them.

  Merged to go-qa-ci main at `f125f711e6f6`. When Task 3.3 adopts it, the go-qa-ci/adopt
  branch replaces its per-tool IaC installs (deadcode, govulncheck) with one
  `go-qa install-tools`.

  - The owner also said not to lock into go-qa-ci. The Go workflow, the hooks daemon's Go
    handlers included, assumes only the plain Go toolchain, and go-qa is an add-on.

### Phase 4: ccy-sessions, then ccy

- [x] ✅ **Task 4.1**: ccy-sessions in Go on the shared packages, by the same method. Ported
  as `cmd/ccy-sessions` plus `internal/sessions`. Cut over: `play-claude-yolo.yml` builds it
  into the shared `ccy-go-build` scratch directory and installs it at the same path and mode
  (`-trimpath`, installed only when the checksum differs); `files/home/.local/bin/ccy-sessions`
  is deleted; the four oracle suites are Go-only (the bash library cases stay); `qa-changed.bash`
  runs them for any `src-go/` file in the binary's build closure; docs updated; CCY 3.95.7.
  Done pending the owner's host run (`meta-deploy.bash` lists `play-claude-yolo.yml`).
- [ ] 🔄 **Task 4.2**: The ccy launcher: assess and plan; decide with the owner. Assessed in
  [ASSESS-ccy-launcher.md](ASSESS-ccy-launcher.md) (about 15,400 lines with its lib and cc, all
  of it host-side; a strangler port at the same install path, phased); waiting on the owner's
  decisions D1-D10 there.

### Phase 5: Review

- [ ] ⬜ **Task 5.1**: qa-reviewer over the whole plan; the owner's host test.

## Success Criteria

- [x] `ccy-teams` on the host is the Go binary, built by the play, and the owner finds it
  works as before or better (owner's host test after meta-deploy: "seems good").
- [ ] The parity suite and the Go unit tests pass in `qa-all.bash`, with golangci-lint strict
  and race-detector tests.
- [x] `go-qa-ci` exists, tests itself, and this repository's Go QA runs it (Task 3.3; its
  record and suppression checks follow the owner's record, Task 3.8).

## Delivery & Milestones

- <!-- delivery commit hashes -->
