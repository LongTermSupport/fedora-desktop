# src-go — the Go module

One module, `github.com/LongTermSupport/fedora-desktop/src-go`, for this repository's large
interactive tools (Plan 00191, Plan 00192). `ccy-teams`, `ccy-sessions`, `podfreeze` and
`lxcfreeze` live here; their bash originals are gone.

## Layout

- `cmd/<tool>/main.go` — a thin `main`: read the environment, call one `run` function that
  returns an exit code, `os.Exit` it. No logic beyond that; `run` is tested.
- `internal/<tool>/` — the tool's flows (`teams`, `sessions`; `podfreeze` and `lxcfreeze` are
  the two engines of the freeze tools). `teams` is the dispatcher and
  the wiring only; each flow is a package under it that says what it owns in its package doc:
  `host` (the adapters over one runner), `board` (the table and `--list`), `picker`, `ops`
  (apply, remove, backup, restore), `create` (the wizard), `login` (Element and passwords),
  `seats`, `flow` (what the interactive flows share), `config`, and `teamtest` (the fake
  machine their tests run on). A package takes the few adapters it uses as fields; none
  imports a sibling flow it does not call.
- `internal/freeze/` — what `podfreeze` and `lxcfreeze` share: the group menu, drill-down,
  derived verb, dry run and act loop (`freeze`), the command-line grammar (`freeze/cli`), the
  flow in spec order (`freeze/app`), and the golden replay (`freeze/golden`, test only). An
  engine implements `freeze.Engine`; never an `if` on the engine in the shared half.
- `internal/<package>/` — shared adapters, each behind an interface with a fake: `execx`
  (the command runner every external call goes through, and its recording `Fake`), `term`
  (exit codes, error printing), `ui` (gum and fzf prompts), `sudo`, `systemd`, `tmuxccy`,
  `gitx`, `registry`, `agentbus`, `declare`, `marker`, `screen`, `transcript`, `restore`,
  `sessprobe`, `fsx` (atomic file writes), `ccyenv` (is CCY installed, is this a container).
  Reuse these; a second way to run a command or prompt the user is a defect.
- The installer `agent-bus-install` (`cmd/agent-bus-install`, flows in `internal/businstall`)
  runs host programs only through their owners: `hostnet` (the network tools and curl),
  `hostsys` (packages, accounts, architecture, zstd), `firewalld`, `systemd` (units) and
  `busadmin` (the Python admin tool and bundler). `fsx` does every root write through
  `os.Root`; `tarstate` reads and writes the state tar; `busaddr`, `pin` and `netprobe`
  parse. Its test prefix (`AGENT_BUS_INSTALL_TEST_PREFIX`) exists only in a binary built with
  `-tags agentbustest`.

## Rules

- **Test first.** The test file exists before the source file (the TDD hook enforces it).
  Every flow is unit tested through the fakes, without a terminal.

- **No discarded errors.** Never `_ =` or `_ :=` an error; return it or handle it.

- **No suppression.** No `//nolint`; fix what the linter reports. `.golangci.yml` is strict on
  purpose.

- **Exit codes** are `term.ExitCode` (`ExitOK`, `ExitFailure`, `ExitUsage`, `ExitCancelled`);
  every run function returns one and only a `main` turns it into the `int` for `os.Exit`;
  diagnostics go to stderr, the payload to stdout ([StderrHygiene.md](../CLAUDE/StderrHygiene.md)).

- **Secrets never in argv.** A password goes to its consumer on stdin.

- **Project rules.** `qa/` is a nested module (so this `go.mod` stays dependency-free) of
  `go/analysis` rules for the defect classes the ports hit: one process seam, owned programs,
  no wall clock in tests, one home abbreviation. [GoProjectRules.md](../CLAUDE/GoProjectRules.md) is the catalogue.
  A new defect class found here gets a rule there before its fix.

## Parity oracles

The bash suites that tested the bash tools now drive the Go binaries and remain the
behaviour oracle: `scripts/test-ccy-teams-command.bash`, the four
`scripts/test-ccy-sessions-*.bash` / `test-ccy-tmux-targets.bash` suites, and
`scripts/test-agent-bus-install.bash` (`AGENT_BUS_INSTALL_SUITE=go|bash` picks the Go binary or
the bash original; the cases under "Go port" run in Go mode only). Some Go tests run
the real bash libraries and Python helpers and compare results, so a change to those files
reruns the Go gate. A behaviour change that a suite pins is changed in the suite in the same
commit, with the reason.

The freeze tools' bash originals and their white-box suites are deleted; their oracle is the
golden replay, `internal/freeze/golden`. `testdata/golden/<scenario>/` is what the shipped bash
tools printed (stdout, stderr, exit status, the commands run) over each scenario in
`testdata/scenarios.json` under fakes and a pseudo-terminal, recorded by `CLAUDE/Plan/00192-.../golden-capture/capture.py` from a git
revision; the test drives the Go entry points over the same world and compares byte for byte. A
difference is allowed only as a numbered bug of that plan's SPEC-freeze.md section 10, pinned in
`testdata/deviations.json` with the bugs and the reason and in `testdata/deviation/`;
`go test ./internal/freeze/golden -update-deviations` rewrites the pinned files for the entries
already listed. Behaviour the golden pins is changed in the same commit as its deviation entry.

## Build and deploy

`tasks/build-go-command.yml` builds a command on the host from the checkout
(`go build -trimpath -buildvcs=false`, as the login user, into `~/.cache/ccy-go-build`) and
installs it with a checksum-compared `copy`; it takes the command name, install path, mode and
optional ldflags. `playbooks/imports/play-claude-yolo.yml` imports it for both ccy binaries
(`ccy-teams` also gets `-ldflags "-X main.RootDir=<checkout>"` and refuses to run without it),
and `tasks/deploy-freeze.yml` for the freeze tools, which `play-podfreeze.yml` and
`play-lxcfreeze.yml` import (both import `play-golang.yml` first for the toolchain). Each binary
lands at its old path. A new deployed command takes a row in `scripts/go-binary-suites.tsv`. Never
install a binary by hand: the play is the only route
([InfrastructureAsCode.md](../CLAUDE/InfrastructureAsCode.md)).

## QA

`scripts/qa-go.bash` (fmt, vet, `go test -race` with a coverage floor, golangci-lint) is the
interim gate, run by `qa-all.bash` and by `qa-changed.bash` for a `src-go/` change, until the
go-qa-ci toolchain replaces it. Details: [QA.md](../CLAUDE/QA.md).

`qa/bashlimits` + `qa/cmd/qa-bash-limits` (in the nested `qa` module, which holds the
`mvdan.cc/sh` parser dependency) are the QA gate that holds every bash file to the tooling-is-Go
limits (`scripts/qa-bash-limits.bash` runs it; allowlist `scripts/bash-limits.allowlist`; the
measure is defined in the package doc). It is a QA tool, never deployed, so it has no bash oracle
suite; its Go tests (including real temporary git repositories) are the proof.
