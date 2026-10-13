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
- [x] ✅ **Task 1.2**: Host and CI. `playbooks/imports/optional/common/play-golang.yml`
  installs `golang`, `gcc` (what `go test -race` links against) and golangci-lint at the
  `.qa-versions` pin (checksum-verified from the release's checksums file, probed so it is
  idempotent and a pin change is applied). `play-python.yml` imports it ahead of its
  QA-toolchain check, so the one play that installs the QA tools installs these too, with a
  single source of truth. `scripts/qa-toolchain.bash` asserts golangci-lint at the pin and
  `go` at no less than `src-go/go.mod` requires; `.github/workflows/qa.yml` installs both.
  `play-claude-yolo.yml` does not depend on the Go play yet: that arrives with Task 2.5,
  which is where it first builds a binary.
- [ ] 🔄 **Task 1.3**: `src-go/` skeleton: `go.mod`, `.golangci.yml` (strict), `cmd/`,
  `internal/` (execx runner + recording fake, prompter interface + gum/fzf implementation +
  fake, paths, version), one trivial tool proving build, test and lint.
- [ ] 🔄 **Task 1.4**: `scripts/qa-go.bash` exists and is wired into `qa-all.bash` (a
  hard gate) and `qa-changed.bash` (any `src-go/` change): `golangci-lint fmt --diff` (any
  diff fails), `go vet`, `go test -race`, `golangci-lint run`, whole module, a missing tool
  a hard failure. Still to do: the coverage floor and govulncheck. Interim, until Task 3.3
  puts go-qa-ci in its place.

### Phase 2: ccy-teams in Go

- [x] ✅ **Task 2.1**: The functional spec of the bash ccy-teams:
  [SPEC-ccy-teams.md](SPEC-ccy-teams.md).
- [x] ✅ **Task 2.2**: Domain and adapters: teams/members/seats/humans/statuses; the
  declare.py, agent-bus, systemd, sessions-registry, Element, gh adapters; each unit tested
  (`internal/term`, `sudo`, `systemd`, `tmuxccy`, `gitx`, `registry`, `agentbus`, `declare`,
  `marker`).
- [x] ✅ **Task 2.3**: Flows: list, picker and keys, create wizard, apply, remove, password,
  Element, new session, attach, restore, park; start-up offers (`internal/teams`).
- [ ] 🔄 **Task 2.4**: Parity: `scripts/test-ccy-teams-command.bash` runs against the Go
  binary (`CCY_TEAMS_IMPL=go`) and passes; where a check tests bash internals or a bug the
  port fixes (the spec's section 8), it is made mode-conditional, said in the journal.
- [ ] ⬜ **Task 2.5**: Cut-over: `play-claude-yolo.yml` builds and installs the binary; the
  bash `ccy-teams.j2` is deleted in the same commit; docs updated; CCY version bump.

### Phase 3: go-qa-ci

- [x] ✅ **Task 3.1**: Brief from rust-qa-ci, ts-qa-ci and php-qa-ci, and the Defence
  Before Fix toolchain spec read from its first-party repository:
  [RESEARCH-go-qa-ci.md](RESEARCH-go-qa-ci.md).
- [ ] 🔄 **Task 3.2**: Create `LongTermSupport/go-qa-ci` (created private; the owner makes it
  public after review) and build it by milestones, self-hosted. It is a generic DBF toolchain
  for Go: it names no consumer.
- [ ] ⬜ **Task 3.3**: This repository's Go QA runs go-qa-ci, pinned.
- [ ] ⬜ **Task 3.4**: go-qa-ci diff mode, `go-qa run --changed [--base <ref>]`: per-package
  engines on the affected packages only, repo-wide defences always, and a change to
  `go.mod`, `.golangci.yml` or `qa/` widens it to everything. This repository's
  `qa-changed.bash` calls it.

### Phase 4: ccy-sessions, then ccy

- [ ] 🔄 **Task 4.1**: ccy-sessions in Go on the shared packages, by the same method:
  [SPEC-ccy-sessions.md](SPEC-ccy-sessions.md); the shared adapters (`tmuxccy`, `registry`,
  `execx` additions; `screen`, `transcript`, `restore`, `sessprobe`) are built; the flows
  and parity against its suites are in progress.
- [ ] ⬜ **Task 4.2**: The ccy launcher: assess and plan (about 4,000 lines and the container
  boundary); decide with the owner.

### Phase 5: Review

- [ ] ⬜ **Task 5.1**: qa-reviewer over the whole plan; the owner's host test.

## Success Criteria

- [ ] `ccy-teams` on the host is the Go binary, built by the play, and the owner finds it
  works as before or better.
- [ ] The parity suite and the Go unit tests pass in `qa-all.bash`, with golangci-lint strict
  and race-detector tests.
- [ ] `go-qa-ci` exists, tests itself, and this repository's Go QA runs it.

## Delivery & Milestones

- <!-- delivery commit hashes -->
