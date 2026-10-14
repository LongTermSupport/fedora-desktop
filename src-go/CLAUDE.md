# src-go — the Go module

One module, `github.com/LongTermSupport/fedora-desktop/src-go`, for this repository's large
interactive tools (Plan 00191). `ccy-teams` and `ccy-sessions` live here; their bash originals
are gone.

## Layout

- `cmd/<tool>/main.go` — a thin `main`: read the environment, call one `run` function that
  returns an exit code, `os.Exit` it. No logic beyond that; `run` is tested.
- `internal/<tool>/` — the tool's flows (`teams`, `sessions`).
- `internal/<package>/` — shared adapters, each behind an interface with a fake: `execx`
  (the command runner every external call goes through, and its recording `Fake`), `term`
  (exit codes, error printing), `ui` (gum and fzf prompts), `sudo`, `systemd`, `tmuxccy`,
  `gitx`, `registry`, `agentbus`, `declare`, `marker`, `screen`, `transcript`, `restore`,
  `sessprobe`. Reuse these; a second way to run a command or prompt the user is a defect.
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

- **Exit codes** come from `term` (`ExitOK`, `ExitFailure`, `ExitUsage`, `ExitCancelled`);
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

## Build and deploy

`playbooks/imports/play-claude-yolo.yml` builds both binaries on the host from the checkout
(`go build -trimpath -buildvcs=false`; `ccy-teams` also gets
`-ldflags "-X main.RootDir=<checkout>"` and refuses to run without it) and installs them at
their old paths. Never install a binary by hand: the play is the only route
([InfrastructureAsCode.md](../CLAUDE/InfrastructureAsCode.md)).

## QA

`scripts/qa-go.bash` (fmt, vet, `go test -race` with a coverage floor, golangci-lint) is the
interim gate, run by `qa-all.bash` and by `qa-changed.bash` for a `src-go/` change, until the
go-qa-ci toolchain replaces it. Details: [QA.md](../CLAUDE/QA.md).

`cmd/qa-bash-limits` + `internal/bashlimits` are the QA gate that holds every bash file to the
tooling-is-Go limits (`scripts/qa-bash-limits.bash` runs it; allowlist
`scripts/bash-limits.allowlist`). It is a QA tool, never deployed, so it has no bash oracle suite;
its Go tests (including a real temporary git repository) are the proof.
