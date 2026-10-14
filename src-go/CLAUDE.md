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
  tmux target forms, no wall clock in tests, one home abbreviation, a `WaitDelay` that names
  its error. [GoProjectRules.md](../CLAUDE/GoProjectRules.md) is the catalogue.
  A new defect class found here gets a rule there before its fix.

## How a port is built: spec first, tests first

A new port is built to a spec of intended behaviour, not to a replay of the bash. The plan's
`SPEC-<tool>.md` states each behaviour as a numbered, testable expectation (inputs, output
lines, exit codes, failure modes, all fail-fast); the old bash is read only to find the
requirements, and is not assumed correct. Where it was wrong, the spec states the right
behaviour and notes the departure. Each expectation gets a Go test written first and seen to
fail, then the code (red and green recorded in the plan journal). Before cut-over, the Go tool
runs on the live tree and any difference from the old tool's result is explained.
`SPEC-qa-ansible.md` and `qa/ansible` are the model. Do not record the bash's output as an
oracle for a new port.

## Parity oracles (earlier ports)

The replays below came from earlier ports and stay as regression tests; they are not
extended. Where one gets in the way, replace it with expectation tests rather than recording again.

The bash suites that tested the bash tools now drive the Go binaries and remain the
behaviour oracle: `scripts/test-ccy-teams-command.bash` and the four
`scripts/test-ccy-sessions-*.bash` / `test-ccy-tmux-targets.bash` suites. Some Go tests run
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

`qa/changed` + `qa/cmd/qa-changed` (same nested module) are `scripts/qa-changed.bash`, the
diff-scoped QA run before each commit: the script only builds and runs the command. The routing
of a changed file to checks is data (`scripts/qa-routing.tsv`, `scripts/qa-trees.tsv`), the
checks are a registry in `changed/checks.go`, and the proofs are its Go tests, a golden replay of
the bash it replaced (`changed/golden_test.go`, difference only as a numbered bug pinned in
`testdata/deviations.json`) and the black-box suite `scripts/test-qa-changed.bash`. The project
rules run over this nested module too (`qa-go.bash` runs the driver a second time over it):
every process it starts goes through the one seam `qa/internal/proc` (shared with
`qa/bashlimits`' git runner), faked in tests, and its tests read no wall clock.

`qa/all` + `qa/cmd/qa-all` + `qa/gates` (same nested module) are `scripts/qa-all.bash`, the full
QA run before a push: the script only builds and runs the command. **What it runs is data:
`scripts/qa-gates.tsv`**, one row per gate in run order, loaded strictly by `qa/gates`, which
also holds the three stage-line readers (case count, detail pattern, the helper-tests counts
file) and is read by `qa/changed` for its suite population; the docs gate checks the same file
against `CLAUDE/QA.md`. A gate whose reader finds nothing fails. The proofs are its Go tests (a
fake process seam over the real table, every gate failing in turn), a golden replay of the bash
it replaced (`qa/all/golden_test.go`, recorded by `CLAUDE/Plan/00192-.../qa-all-golden/capture.py`;
difference only as a numbered bug of `SPEC-qa-all.md` section 10 pinned in
`testdata/deviations.json`), and the real tree (`scripts/test-qa-changed.bash`, the docs gate).
`qa/ansible` + `qa/cmd/qa-ansible` (same nested module) are `scripts/qa-ansible.bash`, the static
Ansible gate (fail-fast directives, playbook hygiene, self-default vars, scope and guard,
deprecated fact vars, guessed runtime-dir uids): the script only builds and runs the command. It
starts no process itself: the one `git ls-files` it needs (it judges only what git would
publish) is injected and run by the command through the seam. Its spec is the intended behaviour
(`SPEC-qa-ansible.md`, expectations E1-E19), proven by unit tests and end-to-end scenario trees,
not by a recording of the old bash; `scripts/test-qa-ansible-failfast.bash` drives the built tool.

`qa/nokill` + `qa/cmd/qa-nokill-containerwatch` (same nested module) are
`scripts/qa-nokill-containerwatch.bash`, the no-kill gate for the container-watch watchdog: the
script only builds and runs the command. Its spec is `SPEC-qa-nokill-containerwatch.md`
(expectations E1-E15), proven by unit tests over a fake tree and a real work tree; the one
`git ls-files` it needs is run by the command through the seam.

`qa/patterns` + `qa/cmd/qa-patterns` (same nested module) are `scripts/qa-patterns.bash`, the
bash-convention gate: semgrep stays the engine and `.semgrep/bash-conventions.yml` the data; the Go
owns scope (what git would publish), the rule self-test against its fixture, the scan mirror and
the refusal to pass over a script semgrep did not read or parse. git and semgrep go through
`qa/internal/proc`, faked in the unit tests; `SPEC-qa-patterns.md` (P1-P18) is the specification.

`qa/python` + `qa/cmd/qa-python` (same nested module) are `scripts/qa-python.bash`, the Python gate:
ruff and python3 stay the engines; the Go owns scope (what git would publish, by shebang or `.py`
name, never by mode; both lists come from `qa/discovery`), the one-process compile, the substitution and compile of Python templates, the
`ruff --show-files` proof that ruff read every file, and the judgement. `SPEC-qa-python.md` (Y1-Y21) is the specification.
`qa/discovery` + `qa/cmd/qa-discovery` (same nested module) are the shared source-file discovery
the bash gates source through `scripts/qa-discovery.bash` (`qa_discover <list> <root>`; the exclusion
test stays in the wrapper over `scripts/qa-trees.tsv`): what git would publish, one predicate per
language, and the tracked yardsticks each gate measures its coverage against.
`SPEC-qa-discovery.md` (D1-D14) is the specification.

`qa/internal/proc` can stream a command's output to a writer (a report stage's output reaches the
terminal live and in order).
