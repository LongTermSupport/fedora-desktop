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

- [x] ✅ **Task 3.2**: Create `LongTermSupport/go-qa-ci` (public) and build it by milestones, self-hosted. It is a generic DBF toolchain
  for Go: it names no consumer. Milestones M0-M14 each passed an independent verifier; its
  conformance is declared with recorded gaps, never claimed in full. The verifiers' deferred
  minor items are in `subagent-reports/261014-go-qa-ci-deferred-run*.md`, for a polish pass.

- [x] ✅ **Task 3.3**: This repository's Go QA runs go-qa-ci, pinned. `scripts/qa-go-qa.bash`
  runs `go-qa run --no-fix` over both modules; go-qa is pinned at go-qa-ci `ccf4c1751f05` (Task 3.11) by the
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

- [x] ✅ **Task 3.9**: Re-derive the coverage floor from go-qa's measurement (Fable's review of
  Task 3.3, finding 4; `subagent-reports/261019-go-qa-ci-pin-review-fable.md`). `qa-go.bash`
  measured each package by its own tests (no `-coverpkg`); go-qa measures `-coverpkg=./...`
  across the module, so statements one package's tests execute in another now count. The floor
  stayed at 90 and passes, but it is compared to a different, generally higher number (the old
  gate printed 94.8% and 94.4%). Measure the module total the new way, choose the floor from
  it, and record the choice. go-qa prints the percentage only when the floor is missed, so the
  measurement needs a run with a deliberately high floor or a profile of its own.

  - Measured 2026-10-19, full mode, floor set to 100 for the run only:
    `scripts/qa-go-qa.bash -t coverage` printed `.: statement coverage 92.1%` and
    `qa: statement coverage 94.3%`.
  - Floor kept at 90: one floor serves both modules, and 90 is 2.1 points under the lower
    total. The `go-qa.toml` comment now cites these figures.

- [x] ✅ **Task 3.10**: go-qa-ci diff mode must not fail on a test-only package (found by
  Fable). The build stage named the changed packages and their importers to `go build`, which
  refuses a package with no non-test Go file, although `./...` skips it. Here
  `src-go/internal/freeze/golden` is test-only and imports lxcfreeze, so any change to lxcfreeze
  or podfreeze failed the build stage of `qa-changed.bash`.

  - Fixed test first in go-qa-ci (`240fbf2`, Fable-approved, merged to its main as `8e17b43`):
    the named patterns are resolved with `go list -e` and a package that loads with only test
    files is left out of `go build`; the test stage still compiles its tests, and a named
    package that does not load still fails.
  - Pinned at `v0.0.0-20261019182226-8e17b432bfd3` in `src-go/qa/go.mod`, `go mod tidy`;
    `qa-changed.bash` passes on the bump.
  - Evidence: with the bump committed and a throwaway edit to `src-go/internal/lxcfreeze`
    (reverted), the narrowed go-qa run scoped `./internal/freeze/golden` and its `build` and
    `test` steps passed. The same run failed `coverage`, a separate defect: Task 3.11.

- [x] ✅ **Task 3.11**: A narrowed go-qa run fails the coverage floor that the full run meets.
  With only an lxcfreeze file changed, the diff-mode coverage step measured the 11 affected
  packages at 88.8% statements, which is under the module floor of 90 (`src-go/qa/go-qa.toml`).
  The uncovered functions are in packages the change did not touch (the two `main`s,
  `teams/hot`, `teams/lxcadd`). So until this is fixed, `qa-changed.bash` still fails the go
  stage for a change to lxcfreeze. The fix belongs in go-qa-ci.

  - Fixed test first in go-qa-ci (`6a352d6`, Fable-approved, merged to its main as `ccf4c17`):
    in diff mode `-coverpkg` names only the changed packages (less test-only ones); the
    importers are still tested, since their tests may reach the changed code. Full mode is
    unchanged.
  - Pinned at `v0.0.0-20261019185407-ccf4c1751f05` in `src-go/qa/go.mod`, `go mod tidy`;
    `qa-changed.bash` passes on the bump.
  - Evidence: with the bump committed and a throwaway edit to `src-go/internal/lxcfreeze`
    (reverted), `qa-changed.bash` passed its go stage, and `qa-go-qa.bash --changed --base HEAD`
    scoped 12 packages (now including `teams/winddown`) and passed `build`, `test` and
    `coverage`.
  - Possible follow-up, not this task: judge each changed package against the floor on its own.

- [x] ✅ **Task 3.12**: CI's Go is older than the qa module requires. Since Task 3.3 the qa
  module says `go 1.26.9`, but CI's `actions/setup-go@v5` read `src-go/go.mod`, which says
  `go 1.26.0` (its `toolchain go1.26.9` line is ignored by setup-go). CI installed 1.26.0, and
  `qa-toolchain` correctly failed "Install QA tooling" on every F44 run since.

  - Fix: setup-go reads `src-go/qa/go.mod`, the go.mod with the higher go directive.
    `src-go/go.mod` stays at `go 1.26.0`, so the host build (`GOTOOLCHAIN=local`, Fedora
    golang) does not need a newer Go.
  - Gate: `TestEveryWorkflowSetsUpTheGoFloor` (`src-go/qa/toolchain`) fails when a workflow's
    setup-go names a go.mod whose go directive is not the floor. Red on the old workflow, green
    on the fix.
  - Review follow-ups: the comments say `setup-go@v5` (later majors read the toolchain line
    unless `GOTOOLCHAIN=local`); Plan 00204's note is corrected; `qa-routing.tsv` sends
    `.github/workflows/*.yml` and `*.yaml` to the go check (SPEC-qa-changed B50), and the gate
    reads both extensions.
  - Verified: CI run 39318237801 (commit `8a4def2d`, which holds `ae9bc47e` and `03822051`) is
    green, "Install QA tooling" included. The issue text in the journal was not filed: the
    defect was fixed before an issue was needed.

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

- [x] ✅ **Task 4.2**: The ccy launcher: assess and plan. Assessed in
  [ASSESS-ccy-launcher.md](ASSESS-ccy-launcher.md) (about 15,400 lines with its lib and cc, all
  of it host-side; a strangler port at the same install path, phased). Decisions D1-D10
  defaulted by the coordinator under the owner's standing "do not gate on me"; each can be
  overruled before the phase it shapes merges:

  - D1 (a) strangler at the same path; D2 keep `/var/local/claude-yolo/claude-yolo`, root-owned;
    D5 plain line prompts first; D6 keep both Python pieces; D7 no partial hand-over inside the
    launch path; D8 delete the dead bash now. These are the assessment's recommendations.
  - D3: the version by `-ldflags -X`, the integrity hash over the binary itself; Quick Launch
    keeps its hash check; the pre-commit bump rule extends to the launcher's `src-go/` paths.
  - D4 and D10: phases 0 to 2 only (dead code, harness, standalone modes); phases 3 to 5 wait
    until the owner hits launcher defects or asks. The reversible choice.
  - D9: keep Docker support. Dropping it removes a working capability, which is the
    owner's call, not a default.

- [x] ✅ **Task 4.3**: Launcher phase 0.1: delete the dead bash of the assessment's section
  5.3, after re-proving each name dead on current F44 (the assessment predates later work;
  the goodbye acceptance now uses `ccy_known_prompts`, so that one is live). CCY bump.
  Re-proved on the branch: only the take-over chain was dead. Deleted from
  `lib/tmux-session.bash`: `ccy_tmux_take_over`, `ccy_tmux_client_words`,
  `ccy_tmux_idle_words` and the bash default of `CCY_TMUX_TAKE_OVER_WAIT_TENTHS`, with
  their cases in `test-ccy-sessions-take-over.bash` (the suite stays: it tests the Go
  picker's Ctrl-T). CCY 3.99.13.12. Kept, each with a live caller: the whole session-registry
  restore chain (the Go suites for `registry`, `restore` and `transcript` source the library
  and call it as their parity oracle; `ccy_registry_restore` and `_restore_args` were already
  gone), `ccy_known_prompts` (Plan 00194's goodbye harness, `screen_test.go`) and
  `_ccy_tmux_clients` (`ccy_tmux_attach`, `ccy_tmux_is_detached`). The restore chain can go
  only with the Go parity cases that call it: the owner's call. Named follow-up, deferred to
  that decision: remove the restore chain and its Go test cases together.

- [x] ✅ **Task 4.4**: Launcher phase 0.2: the differential harness, proven able to fail.
  Named follow-up, deferred: a pseudo-terminal driver for the interactive scenarios (see "Not
  covered yet" below).
  Built as the Go test package `src-go/internal/ccylaunch/difftest` (test only; a recorded
  oracle on purpose, noted in `src-go/CLAUDE.md`). Each scenario runs the launcher once in a
  sandbox: scratch HOME, a one-commit repository, TMPDIR and XDG_RUNTIME_DIR, TZ=UTC, a PATH of
  only the sandbox's bin. The test binary is every fake (podman, docker, tmux, gh, curl,
  ssh-add, ccy-teams, ...): it records argv and the names, never values, of the environment
  variables that program reads (the names an engine call passes by name, the whole environment
  for tmux and systemd-run, a token for gh and claude, the agent for ssh; `readsEnv` in
  `fake_test.go`), and answers from the scenario's rules; hostname, id and uname answer
  unrecorded; date and stat run on a pinned clock. Goldens (`testdata/golden/<scenario>.txt`:
  exit, stdout, stderr, calls, every file left with mode and content) are recorded from the
  bash launcher with `-update`, which refuses with `LAUNCHER` set; `LAUNCHER=<path>` runs the
  matrix against any program. A token never appears in a tracked file (`{token:NAME}`), and a
  check fails any call whose argv carries one. Launcher seams made it hermetic, CCY 3.99.13.13
  and 3.99.13.14 (the commit hook requires a bump per launcher commit),
  in `lib/host-paths.bash`: `CCY_DOCKERFILE_DIR` (default `/opt/claude-yolo`, also the startup
  text's and `--custom` templates' directory), `CCY_GPU_DEVICE_DIR` (default `/dev/dri`) and
  `CCY_ZONEINFO_DIR` (default `/usr/share/zoneinfo`; the sandbox holds only the zone its world
  names), each failing at start when set to no directory or to `/` or `/dev`
  (`scripts/test-ccy-gpu-device.bash` and four `override-*` scenarios); and every temporary
  file follows `TMPDIR`. The machine still answers `/etc/localtime`: the launcher always reads
  the link (`readlink`, when it is one) but uses it only when timedatectl names no zone, which
  no world does, so the goldens do not depend on it. The launcher's pid is marked only where the launcher
  writes it. qa-changed routes the launcher, `lib/*.bash` and the Dockerfile to the Go gate.

  - Not redirected, on purpose: `CCY_LIFECYCLE_PLUGIN_PATH` (`lib/session-lifecycle.bash`)
    names the plugin's path inside the image, not on the host, and nothing reads the export.
  - Matrix: 82 scenarios. Standalone modes 43 (`--version`/`-v`, `--help`/`-h` with and without
    the image, `--list-tokens`, `--token-usage` table, JSON, API down and its refusals,
    `--export-token`, `--prevent`, `--top`, allowed-hostnames, outside a repository, usage
    errors, the refused and accepted overrides); engine selection 10; token choice 9 (refusals
    with no terminal); launch path 20
    (plain, prompt argument and flag, headless, `--no-ssh`, `--github-443`, `--network`,
    `--no-network`, GPU, SELinux enforcing, gh logged out, `GH_TOKEN` exported or handed on to
    the tmux re-run by file, image missing or old,
    `--rebuild=claude`, Quick Launch replay, container exit status).
  - Not covered yet: everything behind a terminal prompt beyond its refusal (token picker,
    Quick Launch Y/n, SSH key menu and passphrase, tmux insulation), `--custom`,
    `--custom-docker`, `--teams`, a real `--connect`/`--disconnect`, compose, the restart
    relaunch (exit 75) and restore. A pseudo-terminal driver is the next step for them.
  - Red and green: `TestOracleCanFail` edits a scratch install of the launcher and must see its
    scenario differ on the line it changed: dropping `--dangerously-skip-permissions` from the
    run line fails `launch-plain`, and `-v` answering as `-V` fails `version-short`; it does
    not run under `-update`. By hand with
    `LAUNCHER=`: an unmodified scratch copy is green (82 of 82 after the review fixes); the same copy with
    `--label ccy=true` changed to `--label ccy=yes` is red, 22 scenarios failing on that line.
  - Bash behaviour the goldens now pin, for the port to decide deliberately: a trailing
    `--engine` is ignored; under docker one `podman network ls` still runs; an unknown
    `CCY_CONTAINER_ENGINE` fails at token validation, not at the engine check;
    `--update-token` with no terminal exits 1 after its banner, saying nothing.
  - Pinned bash behaviour the port must NOT keep: tokens are exported to every child. Bash
    exports `GH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN` (`claude-yolo` and
    `lib/token-management.bash`), with its `CCY_*` settings, so getenforce, curl, timedatectl
    and every other program it starts get them. The goldens record a variable only where the
    program reads it, so they do not pin the leak; the port hands each token only to the call
    that needs it (the engine run that passes it by name, gh). The bash launcher stopped
    exporting them in Task 4.4b; the dead `CCY_LIFECYCLE_PLUGIN_PATH` export is still to go.
  - Review (Opus, FIX-FIRST): `subagent-reports/261021-launcher-harness-review-opus.md`; every
    finding fixed as above.

- [x] ✅ **Task 4.4b**: Launcher review follow-ups (round 1 finding 1, round 2 findings 1, 2,
  4, 5, 7; the hand-off's round 2 findings 1, 2 and 6, round 3 findings 1 to 4 and 7), CCY
  3.99.13.15 to 3.99.13.18,
  containers 2.78.12 and 2.78.13 (the image carries the guide, corrected twice; the commit
  hook requires a bump each time).

  - Secrets: the launcher held `GH_TOKEN` and `CLAUDE_CODE_OAUTH_TOKEN` exported from
    `claude-yolo`'s pre-run `export` (every later program: uname, timedatectl, sudo, curl in
    the restart update, compose) and `create_token`'s `export GH_TOKEN` (global from then on),
    and a caller's exported token reached everything, tmux's server included, whose global
    environment gave the first launch's token to every later session. Now
    `export -n GH_TOKEN CLAUDE_CODE_OAUTH_TOKEN CLAUDE_OAUTH_TOKEN` first thing, and a prefix
    assignment on the three engine runs that pass a token by name (the session,
    `validate_token`, `claude setup-token`).
  - The tmux re-run (review finding 1, BLOCKING): with the export gone, the launcher that
    `ccy_tmux_insulate` re-runs inside tmux lost a caller's exported `GH_TOKEN`, so a box with
    no gh login was refused. `lib/token-handoff.bash` (`ccy_token_handoff_insulate` wraps
    the insulation): the outer launcher writes the token to a fresh 0600 file in a `mktemp -d`
    (0700) directory under `XDG_RUNTIME_DIR`, else `TMPDIR`, failing the launch if it cannot;
    only the path travels, as `env CCY_GH_TOKEN_HANDOFF=<path>` on the pane's command (never
    tmux's environment) or on the `exec` of an attach refused; the library, sourced before
    every other one, reads it and deletes it; when insulation does not apply the outer
    launcher takes the file back itself. The custom-Dockerfile re-runs
    (`lib/dockerfile-custom.bash`) give the token to the `exec` in its own environment.
    Red first: `scripts/test-ccy-token-handoff.bash` (new gate, 27 checks, the launcher's
    own entry point under `script` with a fake tmux that runs the pane) failed 10 on the
    merged branch, the inner launcher holding no token; scenario `launch-gh-token-handoff`
    (`TestLaunchHandsTokensOn`, gh logged out, the file named) started no session against a
    scratch copy of that launcher. Green after; its golden is byte for byte
    `launch-gh-token-env`'s, and the file is gone.
  - Harness: every fake names the variables whose value holds a secret (`carriers`); a
    recorded call given one it does not read, or tmux and systemd-run given any, fails the
    scenario (`secretAllowed` in `checkRun`); a quiet or clock fake given one fails itself.
    Red: the unchanged bash failed 51 checks (uname, id, date, getenforce, podman given
    `GH_TOKEN` or both). `TestSecretCheckCanFail` removes the `export -n` line and must see
    getenforce reported; `TestLaunchHandsTokensOn` holds that the session run of
    launch-plain, launch-gh-token-env, launch-gh-token-handoff, launch-headless and
    launch-quick-replay still carries both tokens by name. Re-recorded with `-update`: no
    golden changed by the narrowing (the goldens already recorded only names a program
    reads). Not checked: the pure tools that run for real (review finding 7, low risk: a
    future export would still reach getenforce or podman, which are checked).
  - `lib/host-paths.bash`: an override must be absolute (refused naming the variable), is
    resolved, and the resolved path is stored; beyond its default, a path in `/`, `/proc`,
    `/sys` or `/dev` is refused, for all three. `scripts/test-ccy-gpu-device.bash` 60 of 60;
    `override-relative`, `override-gpu-dir-system-tree` (`/dev/shm`),
    `override-gpu-dir-default`; the missing-path scenarios use absolute paths.
  - `CCY-GUIDE.txt`: the failed-build advice names the kept build log and the prompt ccy
    already gives Claude, not the deleted `/tmp/ccy-fix-dockerfile-*.txt`, and says the log is
    on the host, which the container cannot read: copy it into the project for more than the
    prompt's last 50 lines (the prompt says so too). The image carries the guide, so the
    container version is bumped.
  - The restart relaunch still unsets both tokens (defence; they are no longer exported), and
    the relaunch re-derives them: the Claude token from its `--token` file, `GH_TOKEN` from
    gh's login, so an exported one is not carried into a restart. Three suites found the run
    line by `^container_cmd run`; they now match its prefixed form.
  - Review (Opus, FIX-FIRST): `subagent-reports/261021-launcher-token-hardening-review-opus.md`;
    findings 1, 2, 4, 5 and 6 fixed as above, 7 left as noted, 3 and 8 to 12 confirmations.
  - Hand-off review round 2 (Opus, FIX-FIRST):
    `subagent-reports/261021-launcher-token-handoff-review-2-opus.md`. CCY 3.99.13.17.
    - Finding 1 (BLOCKING): a launcher read and deleted any file of the user's that
      `CCY_GH_TOKEN_HANDOFF` named. Now only `<base>/ccy-gh-token.XXXXXX/token` is taken,
      `<base>` resolved (`cd -P`) as the writer resolves it; the directory must be the
      user's, 0700 and not a link, the file the user's, 0600, one link and not a link
      (`find -perm`/`-links`, not `stat`, which the harness runs as a clock fake). Anything
      else ends the launch, naming the variable, and the file is not read or deleted.
    - Finding 2: a new session's outer launcher ends in `exec tmux attach-session`, which
      drops its EXIT trap, so the trap covers only Esc and no insulation (an attach refused
      also ends in an `exec`, of the launcher itself, which takes the file first).
      The inner launcher now sources `lib/token-handoff.bash` first, before `common.bash`
      and the library check, so a launch that fails after that still removes the file. What
      remains: if the inner launcher never starts (tmux cannot run it, or that one library
      is missing), the file stays in the user's 0700 `XDG_RUNTIME_DIR` until logout, or
      under `TMPDIR` when there is no runtime directory.
    - Finding 6: `--debug`'s `set -x` traced the token. The hand-off's lines and the three
      GitHub token tests in `lib/ssh-handling.bash` expand no token (`${GH_TOKEN:+set}`,
      `read` into the variable, the write in a subshell with `set +x`). Every other traced
      token line (the session run, `create_token`, `validate_token`) still expands it, so
      the log's copy of the output has GitHub (`gh?_`, `github_pat_`) and Claude (`sk-ant-`)
      tokens masked by an `awk` stage; the terminal shows the raw trace. The log is now made
      by `mktemp` (0600, a fresh name), not a plain redirect into `/tmp`. Not masked: a
      token of no known prefix (a legacy 40-hex GitHub token) in those other lines.
    - Red first: `scripts/test-ccy-token-handoff.bash` failed 27 of 64 on the merged branch
      (each refusal case, the source order, the real launcher leaving the file, the debug
      log holding all three tokens at mode 0644); green 72 of 72. The harness fixture
      `launch-gh-token-handoff` was refused too (an 8-character suffix, a 0755 directory):
      it now has a 6-character suffix and a 0700 directory (`dirMode` in a scenario's file),
      and keeps a token file with no newline, as a 3.99.13.16 outer launcher writes it.
  - Hand-off review round 3 (Opus, FIX-FIRST):
    `subagent-reports/261021-launcher-token-handoff-review-3-opus.md`. CCY 3.99.13.18.
    - Finding 1 (BLOCKING): a relative `abcdef/token` passed the check, the prefix strip
      removing nothing, and was read and deleted from the launch's directory. Now the path
      must be absolute and match `/…/ccy-gh-token.XXXXXX/token`, its base one of the
      reader's resolved `XDG_RUNTIME_DIR`, `/run/user/<uid>`, `TMPDIR` and `/tmp`.
    - Finding 3: the pane's environment is the tmux server's, so the writer's and reader's
      bases can differ; the reader now accepts any of those four bases rather than the one
      it would itself write to. Still refused (fail closed, naming the variable): a writer
      base the reader's environment does not name, such as a `TMPDIR` only the outer
      launcher had, with no runtime directory.
    - Finding 2: an empty hand-off file is refused with an error and removed with its
      directory. Finding 4: the "Debug logging to:" notice says the terminal shows the raw
      trace; the changelog says only prefixed tokens are masked. Finding 7: the trap sentence
      above is corrected.
    - Red first: `scripts/test-ccy-token-handoff.bash` failed 8 of 91 (the relative path
      taken, the empty file silent and kept, a TMPDIR hand-off refused by a reader with a
      runtime directory); green 91 of 91.
  - Hand-off review round 4 (Opus, MERGE):
    `subagent-reports/261021-launcher-token-handoff-review-4-opus.md`. CCY 3.99.13.19.
    - [x] ✅ Finding 5: the reader computes the uid once with `id -u` (the launch ends if it
      fails) and uses it for `/run/user/<uid>` and every `find -user`, not `$EUID`.
    - [x] ✅ Finding 6: a newline-only hand-off file gets the "is empty" error and is removed.
    - [x] ✅ Finding 7: the changelog says the refusal lists the accepted bases.
    - Red first: `scripts/test-ccy-token-handoff.bash` failed 6 of 100 (the newline-only file
      silent, a genuine hand-off refused under an exported `EUID`, another user's hand-off
      taken under a forged `EUID`); green 100 of 100.

- [x] ✅ **Task 4.5**: Launcher phases 1 and 2: the foundation packages, then `--version`,
  `--help` and the other standalone modes, each a one-flag cut-over.

  - Phase 1 built (nothing cut over or installed, no play change, no CCY bump), each package
    to the intended behaviour its package doc numbers, test first, bash suites' cases ported
    as Go tables: `internal/engine` (E1-E9: selection, a runner for image inspect, ps, rm -f,
    network ls, info and the uid map, the rootless and SELinux verdicts with every case of
    `rootless-guard` and `selinux-verdict`, the relabel preflight's pure parts with the pure
    cases of `relabel-preflight`; an Engine is built only by `engine.New`, podman or docker);
    `internal/tokens` (T1-T8: file names and expiry, validity, expiry colours, the shadow
    warning, the listing, the pool verdict per mode and the import switch, with the cases of
    `token-mode` and `token-import-cleanup` that need no container); `execx` (`Secret`: a
    value passed to one child by name, refused in argv, never printed, the child seeing that
    one value of its name; `TakeSecret` moves it out of the environment; `Foreground`: the
    child in the terminal's group, TERM and HUP forwarded, INT and QUIT outlived, its status
    returned, proven with real `sh` children and real signals; `Cleanups` and
    `SignalContext`); `ui.Lines` (plain line questions: y/N, text, a keyed menu, three tries,
    end of input or the end of the context cancels).
  - Departures from bash, decided: an unknown engine is refused at selection and a dangling
    `--engine` is a usage error; a failed image inspect is not a missing image; docker's
    unreadable SELinux report relabels; a token stamp must be `<name>.<calendar date>.token`;
    `--token NAME` never picks an undated file; expiry colour only on a terminal; tokens are
    never exported to unrelated children.
  - Review (`subagent-reports/261021-launcher-phase1-review-opus.md`, FIX-FIRST) fixed: the
    relabel fix argv lacked find's closing `+` (now the printed fix quotes the argv, and a
    test runs it under the real find); a Secret or Env name the parent also exported reached
    an exec'd child twice (`execx.ChildEnv` now gives each name once); `Engine.cmd`'s
    exemption rested on an unchecked kind (now unexported, set by `engine.New`); `ui.Lines`
    takes a context; the Foreground and SignalContext interplay, the Secret residues and the
    `Secret` rule in `src-go/CLAUDE.md` are documented; the flaky
    `TestSessionDeadlineKillsTheGroupAfterTheShellExited` read a process reaped mid-read
    (ESRCH) as an error, and now counts it as ended.
  - Follow-ups, not phase 1: `internal/wsitoken` parses the same token pool with opposite
    rules (a file dated today usable, the newest file wins); port it onto `tokens`. Phase 2
    states how the launcher combines Foreground with SignalContext (the child's status from
    `Exit`), and tests that tokens reach only their consumer (`TakeSecret`). `ui.Lines`'
    `Colour` flag is the opposite sense of `ui.Gum`'s `Plain`. From the round-2 review
    (`subagent-reports/261021-launcher-phase1-review-2-opus.md`), before phase 2 uses
    `ui.Lines`: each `Lines` leaks its stdin reader goroutine, which after a cancelled question
    would take the first line typed to a later `Foreground` child (give `Lines` a `Close`, or
    exit the flow after a cancel); the send at `lines.go:187` sits outside the `select`, so a
    copied `Lines` can hang (move it in, or use `Lines` by pointer only).
  - Phase 2 built, CCY 3.99.14.0 (host run pending: `meta-deploy.list`, play-claude-yolo.yml).
    `src-go/cmd/claude-yolo` over `internal/ccylaunch` (F1-F9 in its package doc), installed by
    the play at `/var/local/claude-yolo/claude-yolo` (root, 0755, checksum-compared copy, built
    with `-X main.Version=<CCY_VERSION>` read from the bash launcher and asserted); the bash
    launcher is copied first, beside it, as `claude-yolo.bash` (same owner and mode), so a
    failed build leaves the old launcher working. The image does not copy the launcher.
    - First cut (superseded by the later bullets, which cut over `--list-tokens`,
      `--token-usage`, `--prevent` and `--export-token NAME`; only `--top` and the interactive
      export stay in bash): `--version`/`-v` and `--help`/`-h` (help text embedded as
      `help.txt`, held byte for byte to the bash heredoc by a test; the engine's two help
      questions are `engine` E10). Everything else is `execx.Exec` of `claude-yolo.bash`, no
      shell, argv and environment unchanged. The `ui.Lines` follow-ups above stay open for the
      first flag that prompts.
    - Routing and fail-safe: the front answers a handled flag only where the bash would reach
      its own answer as modelled, and otherwise hands over (F4: `CCY_GH_TOKEN_HANDOFF` set,
      `BASH_ENV`-like variables or an exported function, an engine name or program not found,
      a tool the bash runs first missing, a library or the launcher missing or unreadable, an
      override `ccy_host_dir` refuses, by a Go `realpath -m` checked against the real one).
      `CCY_LEGACY` set to anything but empty or 0 sends every run to the bash (risk 10). A
      hand-over that cannot exec exits 1 naming the path. Documented in `docs/ccy.md`.
    - D3, the version and hash rule: `--version` prints the front's `-X main.Version` and, as
      `Hash:`, the bash launcher's own `CCY_HASH` rule (its text less `CCY_HASH=` lines, then
      `lib/*.bash` and `lib/*.py`, md5, 16 hex), computed by `ccylaunch.IntegrityHash` and held
      to the bash block itself by a test. So Quick Launch's check is unchanged: the bash writes
      and compares the same value the front prints. The front refuses `--version` and `--help`
      when its version is not the bash's `CCY_VERSION` (two checkouts' halves), which is what
      pins the binary. The pre-commit gate demands the bump for `src-go/cmd/claude-yolo/` and
      `src-go/internal/ccylaunch/` (tests and `difftest/` aside;
      `test-ccy-container-version-hook.bash` red 6 on the old hook, green 105). Narrowed from
      D3's "hash over the binary itself", for the owner to overrule: a binary's bytes change
      with every edit to a shared package (`execx`, `engine`, `term`) and every Go toolchain
      upgrade, none of which the gate can see, so Quick Launch would report a "developer
      error" after each. Not covered, by either rule: a change to a shared package alters the
      front's behaviour without a bump.
    - Acceptance in the container: the harness green against bash unchanged (82), against the
      front in-process (`TestFrontParity`, 82; `TestFrontAnswersItself` with the bash's two
      answers broken; `TestFrontHandsOver` with a launch flag mutated), and by hand with
      `LAUNCHER=` the built binary beside a copy of the bash (82 of 82);
      `test-ccy-info-flags.bash` now runs every case against the bash and the built front, and
      the legacy and missing-launcher cases (90 of 90). `--help` byte for byte: the help
      scenarios with and without an image.
    - Review (`subagent-reports/261022-launcher-phase2-review-opus.md`, FIX-FIRST) fixed,
      CCY 3.99.14.2, container 2.78.14: the tmux insulation recorded `$0`, which behind the
      front is `claude-yolo.bash`, as the session's `launcher=`, so restores and team restarts
      would skip the front; it and both custom-Dockerfile modes now name
      `$SCRIPT_DIR/claude-yolo`, as the restart relaunch did (`test-ccy-token-handoff.bash`
      runs the launcher's own lines from a file named `claude-yolo.bash`: red 2, green 103).
      Plan scripts of 00092, 00161, 00169 and 00203 that read the deployed launcher as text
      read `claude-yolo` while it is a script, else `claude-yolo.bash`. `docs/ccy.md` gives the
      way past a front that will not start; `ContainerRules.md` states the gate's blind spot;
      the in-image guide names both files; the meta-deploy rollback note says what the
      leftover `claude-yolo.bash` does.
    - Review round 2 (`subagent-reports/261022-launcher-phase2-review-2-opus.md`, FIX-FIRST)
      fixed, CCY 3.99.14.3 (container unchanged): a direct run of `claude-yolo.bash`, the
      documented way past a front that will not start, still opened a pane on that front. The
      front now execs the bash with `CCY_LAUNCHER_FRONT=<its own path>` (F2); the new
      `lib/launcher-path.bash`, sourced at the end of `lib/token-handoff.bash` (so the launcher
      gains no line; the front's F4 checks it as it checks `common-pure`, and the play
      installs it), reads it once into `CCY_LAUNCHER_PATH`, unsets it, and falls back to the
      bash's own file, warning, on a value that is not an absolute executable file; the pane,
      the record, the restart relaunch and both custom-Dockerfile modes use it.
      `token-handoff.bash`'s drop lost two statements to stay within the bash limits. Red
      then green: `ccylaunch` 33 failing (no variable on the exec), then ok;
      `TestFrontParity` 45 failing on the new front-leak check (`getenforce`, `podman`, `gh`
      inherited the variable from the old bash), then 82 of 82, with
      `TestFrontLeakCheckCanFail` proving the check; `test-ccy-token-handoff.bash` 22 of 116
      failing (front start, direct start, three invalid values, no tmux call given the
      variable), then 116 of 116. `docs/ccy.md` says what each start records. The plan
      scripts fail on a missing or unreadable `claude-yolo` instead of falling through. The
      project-owned `system_paths` handler maps `claude-yolo.bash` to the bash source and
      `claude-yolo` to `src-go` (its two new tests red, then green).
    - Review round 3 (`subagent-reports/261022-launcher-phase2-review-3-opus.md`, FIX-FIRST)
      fixed, CCY 3.99.14.4: `lib/launcher-path.bash` took any executable file as the front, so
      a stray `CCY_LAUNCHER_FRONT` became the program every later restore ran; it now takes
      only the file that resolves to `$SCRIPT_DIR/claude-yolo`, else warns naming the variable
      and records the bash itself (`test-ccy-token-handoff.bash`, `/usr/bin/env` case: 3
      failing, then 0). The "unset before anything runs" comments and `docs/ccy.md` now say
      before tmux, the engine and claude start (a few system tools run earlier and never read
      it), and the meta-deploy rollback note says a session started by running
      `claude-yolo.bash` directly restores onto the stale copy.
    - Phase 2 standalone flags (spec: `subagent-reports/261022-launcher-flags-handoff.md`).
      CCY 3.99.14.5, `--list-tokens` alone (F10): the front tells the time by a `Clock`, the
      harness's pinned one only in `-tags difftest` builds; expiry colours match the bash.
      CCY 3.99.14.6, `--token-usage [--json]` (F11, `tokenusage.go`): table, JSON, exit
      codes, cache and one curl call per account as the bash's, the token fed to curl's stdin
      through the new `execx.Feed` (a Secret, checked against argv; `Foreground` and `Exec`
      refuse it); hands over wherever the pool, a setting, a cache entry or the API's headers
      could read otherwise (F11 lists them). Difftest: `TestLauncherParity` 82/82 against
      bash and against `LAUNCHER=` a `-tags difftest` build; `TestFrontParity` 82/82;
      `TestFrontAnswersItself` covers the seven `token-usage-*` scenarios the front answers
      (`token-usage-not-first` stays the bash's).
      CCY 3.99.14.7, the token flags simplified (review
      `subagent-reports/261022-launcher-token-flags-review-opus.md`, SIMPLIFY): kept the JSON
      document, the cache format and TTL, the fed fetch and the table; dropped the byte-for-byte
      copies the goldens do not exercise, each declared in the package doc (D1 to D9: byte
      order, colour only on a terminal, calendar-day colour, Go's zone, cleaned paths,
      `os.CreateTemp` names, no awk round trip, decimal resets and TTL, a non-ASCII header not
      read). Go now answers a non-C locale with ordinary names (tests in `listtokens_test.go`
      and `tokenusage_test.go`). Review findings: 3, a junk token a curl argument holds hands
      over before anything is written, a random temp name holding it is made again; 4, no
      hand-over remains after the fetch, so the comment is true; 5, `Front.Dir` removed; 7,
      `GLOBSORT` added to `shellEnv` (the host's bash is 5.3, per Plan 00138's research). The
      harness names its one allowance, colour, for `list-tokens-one` and `list-tokens-states`
      (`colourAllowance`), and its temp-name mark takes Go's digits.
      CCY 3.99.14.8, review 2 (`subagent-reports/261022-launcher-token-flags-review-2-opus.md`,
      MERGE) findings 2 to 5: the temporary-name source is injected (`Front.CreateTemp`), so the
      retry and the 64-try failure are pinned deterministically under `t.TempDir()`; the names'
      prefix is built as `os.CreateTemp` builds it (an unclean `TMPDIR` with a junk token hands
      over instead of exit 1); `colourAllowance` requires Go output with no colour at all and is
      never given to a bash launcher, however `LAUNCHER` names it (`isBash` reads the shebang);
      D5 corrected.
      CCY 3.99.14.9, `--prevent` alone (F12) and `--export-token NAME` (F13) in the same style:
      the in-repo prelude (`inrepo.go`: `.gitignore`, dist and `ccy.env.local` checks, token
      directories; its texts and the export body held to the libraries by a test), new
      `gitx.TrackedUnder`; hand-over wherever the bash's prelude asks, refuses or reads otherwise
      (F12 lists them), decided before anything is written; declared package divergence D10 (warning and error
      colour only on a terminal; `export-token-missing` joins the allowance) and D11 (a failed
      write is exit 1). Difftest: `TestLauncherParity` green against bash and `LAUNCHER=` a
      `-tags difftest` build, `TestFrontParity` green, `TestFrontAnswersItself` now covers
      `prevent`, `export-token-named` and `export-token-missing`; `ccylaunch` coverage 94.6%.
      Stay in bash: `--top` and the interactive `--export-token` (read loops that need a
      terminal).
    - Phase 2 done: every standalone flag that needs no terminal is the front's. Phases 3 to 5
      (the launch path itself: argument parsing, the prelude for a real launch, then the rest)
      wait for the owner, per the plan decisions D4 and D10 (Task 4.2's gate, unrelated to the
      package divergences of `front.go`).
    - Review fix (CCY 3.99.14.10, `subagent-reports/261022-launcher-last-flags-review-opus.md`):
      the front hands over, before any write, when git would quote a tracked path under
      `.claude/ccy` (non-ASCII, `"`, `\`, control byte) or `git ls-files` wrote to stderr, as the
      bash judges the quoted listing; a leading-nought version number is handed over (matches the
      bash's printing, no new divergence). Red: five new cases answered instead of handing over;
      green after the fix, real temporary git repositories including `café/README.md`.
    - The failing project handler test is Task 4.6.

- [ ] 🔄 **Task 4.6**: The project hook handler tests were run by no gate. Now
  `scripts/qa-hook-handlers.bash` (gate `hook-handlers` in `qa-gates.tsv`; `qa-changed` routes
  `.claude/hooks/handlers/*` and the gate's own Go to it) runs them. The tool is Go
  (`src-go/qa/hookhandlers`, expectations K1-K8 in its package doc): the daemon's interpreter
  (`<daemon>/scripts/lib/resolve_venv.sh python`, the daemon found in the main checkout from a
  linked worktree) runs pytest isolated (`-I -B`, no plugin autoload) with the project's pytest
  appended to its path: the one `python<X.Y>` of the venv's own version imports (the CCY
  image's `python3-pytest`; the host's from `play-python.yml`, added here), so the venv's own
  modules always win and two Python versions never mix. A missing daemon, venv or pytest is
  exit 2 naming the fix. CI's checkout has no daemon: `.github/workflows/qa.yml` sets
  `QA_HOOK_HANDLERS_EXEMPT` with its reason (honoured on GitHub Actions only, exit 2 elsewhere)
  and the gate prints `HOOK-HANDLERS-EXEMPT`. Three tests failed on F44.
  `test_matches_write_to_root_claude_projects_returns_false`: the handler was wrong,
  `Path.resolve()` followed the container's `/root/.claude` symlink (to `.claude/ccy`) so the
  runtime-directory exemption never matched; it now judges the path lexically and again after
  symlinks against the projects directory's own target (a link leaving it is not exempt).
  Review of that fix: it resolved the normalised path, so `projects/link/../x` (link to
  `/etc/ssh`) was exempt though the kernel writes `/etc/x`; it now resolves the original path.
  The block side compared the raw path, so `//etc/x`, `/./etc/x` and `<project>/../etc/x`
  passed; every prefix is now judged on the folded path. Real-symlink tests cover both.
  The two `ansible_enforcement` "dnf in a quoted string" tests were wrong: the handler
  deliberately matches quoted text (`bash -c 'dnf install x'` is quoted), so they now assert
  that. Open: the host run of `play-python.yml` (queued in `meta-deploy.list`), then the
  gate passing on the host.

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
