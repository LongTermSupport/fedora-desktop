# src-go — the Go module

One module, `github.com/LongTermSupport/fedora-desktop/src-go`, for this repository's large
interactive tools (Plan 00191, Plan 00192). `ccy-teams`, `ccy-sessions`, `podfreeze`,
`lxcfreeze`, `vmtest`, `ftp-camera`, `watermark`, `wsi`, `open` and `rclone-cache-status` live here; their bash
originals are gone.
`convert` is new, with no bash original.

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
- `internal/vmtest/` — the VM lab's host CLI (spec `SPEC-vmtest.md`, E1-E60): every verb,
  through `execx` to virsh, virt-install, qemu-img, ssh and curl, and through one seam
  (`python3 -P -m helpers.vmtest.<module>`) to the lab's Python helpers, which stay Python.
- `internal/ftpcamera/` — the camera FTP session tool (spec `SPEC-ftp-camera.md`, E1-E66): the
  launcher, the sort, push, copy and prune modes, and the vsftpd session with its log
  followers and teardown; root through `sudo`, systemctl through `systemd`, and the rclone
  remote control through `rclonerc` (the Go twin of `rclone-rc-auth.bash`, for the rclone
  helpers when they are ported).
- `internal/rclonecache/` — the `rclone-cache-status` command (spec
  `SPEC-rclone-cache-status.md`, E1-E21): the rclone mounts from `findmnt -J`, each mount
  process's `--rc-addr` and `--cache-dir` from `/proc` (`rclonerc.MountProcess`), `vfs/stats`
  through `rclonerc.Client.Call`, and a directory walked against its VFS cache mirror; no
  Python, no `numfmt`.
- `internal/<package>/` — shared adapters, each behind an interface with a fake: `execx`
  (the command runner every external call goes through, and its recording `Fake`), `term`
  (exit codes, error printing), `ui` (gum and fzf prompts), `sudo`, `systemd`, `tmuxccy`,
  `gitx`, `registry`, `agentbus`, `declare`, `marker`, `screen`, `transcript`, `restore`,
  `sessprobe`, `fsx` (atomic file writes), `ccyenv` (is CCY installed, is this a container),
  `shellword` (POSIX shell quoting for a command shown to a person or a value a shell reads).
- `internal/watermark/` — the watermark command (spec `SPEC-wsi-watermark.md`, M1-M14): its
  config files read as `KEY=VALUE` data, the magick and exiftool argv, through `execx`.
- `internal/wsi/` — the batch speech-to-text recorder the panel starts (same spec, W1-W35):
  one select loop over the stop signals, the timers and the recorder (`pw-record` through
  `execx` with `Interrupt: SIGINT`); the sibling helpers (`wsi-stop-grace`, `wsi-setting`,
  `wsi-resolve-model`) stay bash and Python and run from the binary's directory. Its batch
  limit's one home is `limit.go` (`scripts/qa-stt-limits.bash`); `scripts/test-wsi-stop-grace.bash`
  builds and drives the binary end to end.
- `internal/open/` — the `open` command (spec `SPEC-open.md`, E1-E31): a target's type
  (`mimetype`, `xdg-mime`, `file`), the desktop apps `gio mime` names, found in the XDG
  application directories, and the terminal handlers as a table of argv (an optional pager fed
  by an `os.Pipe`, no shell); the chooser is `ui.Picker`, or a numbered menu on `/dev/tty`
  without fzf. Its one run-time program name is the handler the person chose (a
  `dynamicNames` row).
- `internal/convert/` — the `convert` command (Plan 00210 `SPEC-convert.md`, E1-E15): the
  conversion table (input type, output formats, the engine for each pair) is data in
  `table.go`; each engine spells its program in its own `execx.Cmd` (pandoc for Markdown to
  DOCX) and writes a temporary that is renamed into place only on success.
- `internal/wsipost/` — `wsi-postprocess`, the panel menu's Post-Process Last Transcription
  items (Plan 00208, P1-P8 in its package doc): Claude post-processing of the last
  transcription, through the sibling `wsi-claude-process`.
- `internal/wsitoken/` — `wsi-claude-token`, the one chooser of the Claude account
  post-processing runs as (Plan 00208, K1-K8 in its package doc): the named token, or the one
  with the most allowance left by `ccy --token-usage --json`; `wsi-claude-process` runs it.
- `internal/wsimedia/` — `wsi-lower-media-volume`, the playing media lowered to a third of its
  volume while a recording runs (Plan 00212, V1-V10 in its package doc): the MPRIS players on
  the session bus through `busctl`, their streams' volumes through `pactl --format=json`, held
  until the panel's TERM or the parent's end; a stream still at the volume it set is restored
  exactly, and a player with no stream found is paused and played again instead.
- `internal/ccproject/` — `cc-project-dir`, the directory host `cc` starts its session in
  (Plan 00167, P1-P7 in its package doc): the top of the git work tree, or outside one the
  fedora-desktop checkout baked in with `-X main.RootDir`, offered `[Y/n]` at a terminal.
- `internal/fetchage/` — `git-fetch-age`, the shell prompt's stale-fetch mark (Plan 00183,
  A1-A15 in its package doc): the age of the newest `FETCH_HEAD` of the checkout's repository
  with a record in it (an empty one is a fetch that failed or is running), read from files
  only; it starts no process.
- `internal/promptfetch/` — `git-prompt-fetch`, bash-git-prompt's background fetch made unable
  to prompt (Plan 00183, F1-F6 in its package doc): `git fetch` with ssh in BatchMode (added to
  the ssh command git would use), no terminal prompt, no askpass, stdin from `/dev/null`.
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

- **Pickers and menus: visible choices, not magic keys.** Every action in a `ui` picker is
  a named row or menu line; a key is only an accelerator, printed beside its action
  ([InteractiveScripts.md](../CLAUDE/InteractiveScripts.md) rule 14).

- **Every screen has a heading.** Each `ui` question and pick takes a `ui.Heading` (crumbs,
  a wizard's step, a context line with the answers so far) as a required argument, and refuses
  to draw without crumbs (`ui.ErrNoHeading`). It is wrapped to the width `tput cols` gives
  through the runner, never truncated. ccy-teams screens start from `flow.Here`, the
  `ccy --teams` questions from `flow.LaunchTeam` (rule 15 of the same file). `teamtest`'s
  `Script.HeadingAt` asserts a screen's heading; `Script.Columns` sets the fake width.

- **Project rules.** `qa/` is a nested module (so the QA tooling's dependencies stay out of
  this `go.mod` and out of the binaries) of `go/analysis` rules for the defect classes the ports hit: one process seam, owned programs,
  tmux target forms, no wall clock in tests, one home abbreviation, a `WaitDelay` that names
  its error, no action reachable only by a key (a key reaches fzf only as a `ui.Action`, and a Bubble Tea key only `internal/ui`, through a
  `key.Binding` with help; Tab completing a text input is navigation).
  [GoProjectRules.md](../CLAUDE/GoProjectRules.md) is the catalogue.
  A new defect class found here gets a rule there before its fix.

## Dependencies

This module may depend on third-party modules, vendored, pinned and allowlisted (Plan 00204
decision D2). `go.mod` names each module and its version, `go.sum` locks the content of each,
and `vendor/` holds their source, committed, so every build compiles only what is in the
checkout, offline (`-mod=vendor`, Go's default when `vendor/` exists, and set explicitly by
`tasks/build-go-command.yml`). Nothing is installed system-wide.

- **Every module is on `go-deps.allowlist` with its reason**: the direct ones and every module
  they pull in. A pseudo-version (an untagged commit) or a `replace` directive needs a row of
  its own saying why. A row nothing needs is removed. Adding a module is a decision: high
  quality, maintained, and worth its weight in the binaries.
- **Add or bump** with `go get <module>@<tag>`, then `go mod tidy` and `go mod vendor`, and
  commit `go.mod`, `go.sum`, `vendor/` and the allowlist together; the diff of `vendor/` is the
  review of the new code. Never edit a file under `vendor/`.
- **Gates**: `scripts/qa-go-deps.bash` (the allowlist, pinning, `go mod verify` (the module
  cache unchanged since download), `go mod tidy` and `vendor/` byte for byte what
  `go mod vendor` writes, the last two checking every module against `go.sum`) and `scripts/qa-go-vuln.bash`
  (`govulncheck` over this module, reading `vendor/`), both in `qa-all` and `qa-changed`.
  govulncheck itself is pinned by the `tool` directive of `qa/go.mod` and runs as
  `go tool govulncheck`. The pre-commit secret scan skips `vendor/` only when `qa-go-deps`
  passes over the staged tree.
- **Only `internal/ui` imports the TUI libraries** (Charm: Bubble Tea, Bubbles, Lip Gloss); the
  flows build semantic screens. `internal/ui/frame` is their first user, and
  `internal/ui/pathinput` (a path line with Tab completion, `ccy-teams lxc-add`'s Checkout
  step) the first a command runs. A project rule will
  hold this (`project_seam_imports`, Plan 00204 Task 2.2); until it lands, review holds it.

## How a port is built: spec first, tests first

A new port is built to a spec of intended behaviour, not to a replay of the old tool (bash or
Python). The old tool is treated as a prototype: the Go version fixes and improves it. The plan's
`SPEC-<tool>.md` states each behaviour as a numbered, testable expectation (inputs, output
lines, exit codes, failure modes, all fail-fast); the old tool is read only to find the
requirements, and is not assumed correct. Where it was wrong, the spec states the right
behaviour and notes the departure. Each expectation gets a Go test written first and seen to
fail, then the code (red and green recorded in the plan journal). A port that replaces the old
tool in place runs on the live tree before cut-over, and any difference from the old tool's
result is explained. `SPEC-qa-ansible.md` and `qa/ansible` are the model. Do not record the old
tool's output as an oracle of its behaviour. An oracle against the old tool is optional, and
where there is one it checks only what the two must share while both run side by side: on-disk
formats and locks, wire formats, and the output other programs parse. It never pins the old
tool's internals, wording or process structure; where that shared contract is itself bad, the
spec says so as a departure. A rebuild that runs beside the old tool as a separate system and
shares none of these has no contract to check, so it has no oracle: its expectation tests stand
alone.

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

`pingbus status-line` (Plan 00213) has a recorded oracle on purpose, which makes it an
exception to the rule above. The owner requires exact parity with the Python. The Python stays
the implementation of the rest of pingbus, and the Go must read its on-disk formats the same
way. The spec, `SPEC-status-line.md`, comes first. The oracle is
`internal/pingbus/statusline/golden/testdata/`: `scenarios.json` is the hand-edited source, and
`golden/` is recorded from the Python zipapp by `go test ./internal/pingbus/statusline/golden -update`. The default run judges the Go `statusline.Run`; `-impl=python` judges the zipapp and
`-impl=<binary>` any other command.

## Build and deploy

`tasks/build-go-command.yml` builds a command on the host from the checkout
(`go build -trimpath -buildvcs=false` with `GOFLAGS=-mod=vendor`, offline from `vendor/`, as
the login user, into `~/.cache/ccy-go-build`) and
installs it with a checksum-compared `copy`; it takes the command name, install path, mode and
optional ldflags. `playbooks/imports/play-claude-yolo.yml` imports it for both ccy binaries
and `cc-project-dir` (`ccy-teams` and `cc-project-dir` also get
`-ldflags "-X main.RootDir=<checkout>"` and refuse to run without it), and `tasks/deploy-freeze.yml` for the freeze tools, which `play-podfreeze.yml` and
`play-lxcfreeze.yml` import (both import `play-golang.yml` first for the toolchain), and
`play-vm-test-lab.yml` for `vmtest`, `play-ftp-camera.yml` for `ftp-camera`,
`play-image-watermarking.yml` for `watermark`, `play-speech-to-text.yml` for `wsi` and
`play-cli-tools.yml` for `open` and `convert`, `play-rclone.yml` for `rclone-cache-status`,
`play-basic-configs.yml` for `git-fetch-age` and `play-git-configure-and-tools.yml` for
`git-prompt-fetch`, and `play-claude-yolo.yml` (into the ccy image's build context) and
`play-agent-bus.yml` (into the member kit, which LXC members copy) for `pingbus-status-line`,
built with `go_command_static: true` (`CGO_ENABLED=0`) because it runs in other distributions
(each imports `play-golang.yml` too); `watermark`, `git-fetch-age` and
`git-prompt-fetch` are installed root-owned
in `/usr/local/bin` (`go_command_owner: root`; only that copy runs as root). Each binary lands
at its old path. A new deployed command takes a row in
`scripts/go-binary-suites.tsv`. Never
install a binary by hand: the play is the only route
([InfrastructureAsCode.md](../CLAUDE/InfrastructureAsCode.md)).

## QA

`scripts/qa-go-qa.bash` is the Go gate: go-qa-ci's `go-qa`, pinned by the `tool` directive of
`qa/go.mod` and configured by `qa/go-qa.toml`, over both modules (format, golangci-lint, vet with
its bundled analyzers and the project rules, dead code, govulncheck, module files, build,
`go test -race -shuffle=on`, a 90% coverage floor per module). `qa-all.bash` runs it whole and
`qa-changed.bash` in go-qa's diff mode for a `src-go/` change. A test that re-runs its own binary
(the closed-stdout checks) does it through `qa/internal/cmdmaintest`, whose `Main` is the
package's `TestMain`: the process ends only in a `main` or a `TestMain`. Details:
[QA.md](../CLAUDE/QA.md).

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
rules run over this nested module too (go-qa runs the driver over both modules):
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

`qa/ansiblesyntax` + `qa/cmd/qa-ansible-syntax` (same nested module) are
`scripts/qa-ansible-syntax.bash`, `ansible-playbook --syntax-check` over every playbook: the script
only builds and runs the command. Ansible stays the engine; the tool owns the playbook set (the
`yaml` list of `qa/discovery`, the `tracked-playbooks` yardstick), the prerequisites (a password
file the config names is asked of `ansible-config`, never opened) and the judgement. Spec
`SPEC-qa-ansible-syntax.md` (S1-S15); its unit tests and the real-engine tests in
`qa/cmd/qa-ansible-syntax` are the proof.

`qa/nokill` + `qa/cmd/qa-nokill-containerwatch` (same nested module) are
`scripts/qa-nokill-containerwatch.bash`, the no-kill gate for the container-watch watchdog: the
script only builds and runs the command. Its spec is `SPEC-qa-nokill-containerwatch.md`
(expectations E1-E15), proven by unit tests over a fake tree and a real work tree; the one
`git ls-files` it needs is run by the command through the seam.

`qa/ansiblepause` + `qa/cmd/qa-ansible-pause` (same nested module) are
`scripts/qa-ansible-pause.bash`, the rule pause-without-register: the script only builds and runs
the command. A line scan (the annotation is a comment, which a YAML load drops) over the `yaml`
list of `qa/discovery` under `playbooks/` and `tasks/`; `SPEC-qa-ansible-pause.md` (E1-E17) is the
specification, and the rule is proven against `tests/fixtures/ansible-pause` by its Go tests.

`qa/vmtestmanifest` + `qa/cmd/qa-vmtest-manifest` (same nested module) are
`scripts/qa-vmtest-manifest.bash`, the VM-test scenario manifest gate: the script only builds and
runs the command. `vars/vm-test-scenarios.yml` is decoded strictly as YAML (`go.yaml.in/yaml/v3`),
handed as JSON to the host's own Python validator (`helpers/vmtest/validate_manifest.py`, run
through `qa/internal/proc`, which gained `Cmd.Stdin` for it; one definition of a valid manifest, so
the gate needs python3), then cross-checked against the guest scripts that `qa/discovery` lists as
published: each runnable scenario's `planned` against the `PLANNED` assignment read from its
checker's `mvdan.cc/sh` syntax tree, each `guest-prepare-*` fixture against a declared reboot, and
orphaned guest scripts. `SPEC-qa-vmtest-manifest.md` (V1-V16) is the specification, proven by unit
tests over real temporary git repositories with a fake validator and by the real validator in
`qa/cmd/qa-vmtest-manifest`.

`qa/toolchain` + `qa/cmd/qa-toolchain` (same nested module) are `scripts/qa-toolchain.bash`, the
gate that the QA tools are the versions `.qa-versions` pins (go: not older than the higher go directive of `src-go/go.mod` and `qa/go.mod`):
the script only builds and runs the command. The pin file is parsed with the `mvdan.cc/sh` parser
and `go.mod` with `x/mod/modfile`, never sourced or awk-ed; tool drift is exit 1 (the machine is
wrong), an unreadable pin file or go.mod exit 2 (the gate cannot judge). Version runs go through `qa/internal/proc`, faked in the
unit tests; `SPEC-qa-toolchain.md` (T1-T16) is the specification.

`qa/docs` + `qa/cmd/qa-docs` (same nested module) are `scripts/qa-docs.bash`, the documentation
integrity gate: the script only builds and runs the command. Markdown is read with a CommonMark
parser (`goldmark`, with its table extension: links, headings and the gate rows of `CLAUDE/QA.md`
come from the syntax tree), `playbooks/playbook-main.yml` as YAML, and `scripts/qa-gates.tsv` by
`qa/gates`, so the inventory check and `qa-all` cannot disagree on what a gate is. The scan is the
`markdown` list of `qa/discovery` (what git would publish) narrowed to the core documents, a link
target is asked its trackedness through the `tracked-files` list, and an empty scope, an unread
file or an unparsed file is exit 2. git goes through `qa/internal/proc`, faked in the unit tests;
`SPEC-qa-docs.md` (D1-D25) is the specification, proven by unit tests over on-disk trees and
real-git tests in `qa/cmd/qa-docs`, and `scripts/test-qa-docs-exit-codes.bash` drives the built
tool over fixture trees.

`qa/readywait` + `qa/cmd/qa-ready-wait-rules` (same nested module) are
`scripts/qa-ready-wait-rules.bash`, the gate for `ready-wait-ignores-child-exit`: the script only
builds and runs the command. Python stays semgrep's (`.semgrep/ready-wait.yml` is the data); the
shell half is a rule in Go over the `mvdan.cc/sh` syntax tree (it replaced `helpers/ready_wait`), and each half is proven
against its fixture in `.semgrep/` on every run. Spec `SPEC-qa-ready-wait-rules.md` (R1-R26), proven
by unit tests over a fake semgrep and real-semgrep tests in `qa/cmd/qa-ready-wait-rules`.

`qa/patterns` + `qa/cmd/qa-patterns` (same nested module) are `scripts/qa-patterns.bash`, the
bash-convention gate: semgrep stays the engine and `.semgrep/bash-conventions.yml` the data; the Go
owns scope (what git would publish), the rule self-test against its fixture, the scan mirror and
the refusal to pass over a script semgrep did not read or parse. git and semgrep go through
`qa/internal/proc`, faked in the unit tests; `SPEC-qa-patterns.md` (P1-P18) is the specification.

`qa/ghscopes` + `qa/cmd/qa-gh-scopes` (same nested module) are `scripts/qa-gh-scopes.bash`, the
`gh-scope-outside-ssot` gate: semgrep stays the engine and `.semgrep/gh-scopes.yml` the data; the
Go owns scope (what git would publish, handed by name), the rule self-test against its fixture and
the refusal to pass over a file semgrep did not read (the rule's own `paths.exclude` globs say
which are not expected). `SPEC-qa-gh-scopes.md` (G1-G16) is the specification.

`qa/planscriptlogging` + `qa/cmd/qa-plan-script-logging` (same nested module) are
`scripts/qa-plan-script-logging.bash`, the Plan 00130 gate: the script only builds and runs the
command. It walks the active plan folders on disk (an ignored `logs/` directory must be seen, so
not a git listing), finds plan scripts by name or shell shebang (`discovery.ShellShebang`), parses
each with `mvdan.cc/sh` and reports a bare `exec` redirected into an output process substitution;
an unparseable or unreadable file is exit 2. It starts no process.
`SPEC-qa-plan-script-logging.md` (L1-L14) is the specification, proven by unit tests over in-memory
trees and a real-file-system test in the command package.

`qa/versionpins` + `qa/cmd/qa-version-pins` (same nested module) are `scripts/qa-version-pins.bash`,
the Plan 00109 gate on `vars/version-pins.yml`: the script only builds and runs the command. The
manifest and each `.yml` target are read with the YAML library's node API (repeated keys, wrong
types and unknown keys are findings); the `var: value` line form is the one
`helpers/version_pins/check_pins.py` reads. A missing, unreadable or unparsed file, or no pins, is
exit 2; a wrong manifest or pin is exit 1, and every finding is reported. It starts no process.
`SPEC-qa-version-pins.md` (V1-V25) is the specification, proven by unit tests over in-memory trees
and real-file-system tests (symlinks, the live manifest) in the command package.

`qa/godeps` + `qa/cmd/qa-go-deps` and `qa/cmd/qa-go-vuln` (same nested module) are
`scripts/qa-go-deps.bash` and `scripts/qa-go-vuln.bash`, the gates on this module's dependencies
([Dependencies](#dependencies)): the scripts only build and run the commands. `go.mod` is parsed
with `x/mod/modfile`; the go commands (`go mod verify`, which checks the module cache against the
hashes recorded at download, `go mod tidy -diff` and `go mod vendor -o` into a scratch tree
compared byte for byte, which check `go.sum`, and `go tool govulncheck`) run through
`qa/internal/proc` with `GOFLAGS` emptied, `GOWORK=off`, `GOTOOLCHAIN=local`, the public proxy and
checksum database pinned and no module exempted from them. A go command that could not reach the
proxy (a cold cache offline) could not judge: exit 2, unless a definite finding makes it exit 1. A reachable
vulnerability is exit 1; a govulncheck that could not judge (no vulnerability database) is exit
2, never a pass. Plan 00204's `SPEC-qa-go-deps.md` is the specification, proven by unit tests
over a fake go and by the real go over a module with no dependencies.

`qa/sttrules` + `qa/cmd/qa-speech-to-text-rules` (same nested module) are
`scripts/qa-speech-to-text-rules.bash`, the gate for the rules in `.semgrep/speech-to-text.yml`:
semgrep stays the engine and the rule file the data (decoded as YAML and written out again, one
rule file per language, so an extensionless Python script never meets a JavaScript rule); the Go
owns scope (the `js` and `python` lists of `qa/discovery`, or the named files), the proof that
every rule has a `ruleid:` line in its language's fixture and matches it, and the refusal to pass
over a file semgrep did not read. `SPEC-qa-speech-to-text-rules.md` (S1-S17) is the specification.

`qa/helpertests` + `qa/cmd/qa-helper-tests` (same nested module) are `scripts/qa-helper-tests.bash`,
the helper unit-test gate: the script only builds and runs the command. The stdlib `unittest` runner
(`helpers/qa_environment/unittest_counts.py`) stays Python and the engine; the Go owns scope (a walk
of `tests/helpers` checked against the `tracked-helper-tests` list of `qa/discovery`; none found or
one lost is exit 2), the module names (each segment an identifier), the environment (the machine's
git configuration kept out), and the judgement: the counts file is read back with `gates.HelperRun`
(the integer sibling of `gates.HelperCounts`, which `qa-all` reads) and must agree with the request,
so a run that did not happen cannot pass. `SPEC-qa-helper-tests.md` (H1-H13, G1) is the specification,
proven by unit tests over a fake tree and runner and a real git tree with the real runner in the
command package.

`qa/python` + `qa/cmd/qa-python` (same nested module) are `scripts/qa-python.bash`, the Python gate:
ruff and python3 stay the engines; the Go owns scope (what git would publish, by shebang or `.py`
name, never by mode; both lists come from `qa/discovery`), the one-process compile, the substitution and compile of Python templates, the
`ruff --show-files` proof that ruff read every file, and the judgement. `SPEC-qa-python.md` (Y1-Y23) is the specification.
`qa/js` + `qa/cmd/qa-js` (same nested module) are `scripts/qa-js.bash`, the JavaScript gate:
`node --check` and the project's ESLint 8 stay the engines (ESLint run as `--no-eslintrc --config extensions/.eslintrc.json --no-inline-config`, so nothing beside the repository's config changes the
rules; `qa-changed`'s per-file eslint is given the same pins); the Go owns scope (the `js` list of
`qa/discovery`), the proof that ESLint read every file it was given, the exit 2 for missing tooling
(`extensions/node_modules`) and the judgement. `SPEC-qa-js.md` (J1-J16) is the specification.
`qa/bash` + `qa/cmd/qa-bash` (same nested module) are `scripts/qa-bash.bash`, the shell gate:
`bash -n` and shellcheck stay the engines (shellcheck with `--norc --external-sources` and an
empty `SHELLCHECK_OPTS`, so nothing beside a script changes the rules); the Go owns scope (the
`shell` list of `qa/discovery`), the `tracked-shell` coverage guard, the suppression ratchet
(`# shellcheck disable=` against `scripts/shellcheck-disable.allowlist`, `shell=` against the
shebang; `--directives FILE...` runs only this, for `qa-changed`) and the judgement.
`SPEC-qa-bash.md` (S1-S20) is the specification.
`qa/discovery` + `qa/cmd/qa-discovery` (same nested module) are the shared source-file discovery
every Go gate imports (`qa/discovery`, `discovery.Main` with a list name; no bash wrapper; the exclusion
trees are `scripts/qa-trees.tsv`, read by the Go tools only): what git would publish, one predicate per
language, and the tracked yardsticks each gate measures its coverage against.
`SPEC-qa-discovery.md` (D1-D19) is the specification; the `markdown` and `tracked-files` lists are
the docs gate's.

`qa/deployeddrift` + `qa/cmd/qa-deployed-drift` (same nested module) are
`scripts/qa-deployed-drift.bash`, the repo-against-host gate: the script only builds and runs the
command. It reads the repo and the host through `fs.FS`; the one git question (is this a linked
worktree) is asked by the command through the seam. `SPEC-qa-deployed-drift.md` (E1-E18) is the
specification, proven by unit tests over in-memory trees.

`qa/internal/rootpath` is the one place a gate turns `--root`/`--scripts` into its root: absolute,
clean, symlink-free, an unresolvable root being exit 2. No gate resolves a root on its own; each
carries a symlinked-root test, because an engine that prints realpaths leaks the checkout path
when the root is stripped unresolved. A gate whose tests run on a fake tree takes the resolver as
a field (`rootpath.RootVia`).

`qa/internal/proc` can stream a command's output to a writer (a report stage's output reaches the
terminal live and in order).

A new gate's command and package are built on four shared packages under `qa/internal`, never on
a copy of their code. `cmdmain` is for the `main`: every command's `run` starts with
`cmdmain.RefuseClosedStdout()` (a closed stdout cannot carry the verdict: exit 2), and a command
that writes a machine report wires `cmdmain.WriteReport` (mode 0644, read by other accounts) or
`cmdmain.WriteReportPrivate` (0600) in a `newEnv` function, so a test can pin the choice with
`cmdmaintest`. `emit` is the two-stream writer that remembers its first write error (`Line`,
`Text`, `Block` for a usage text) and `emit.Finish`, the one exit-code rule for an unwritable
stdout or stderr. `rootargs` is the `-h`/`--root DIR`/`--scripts DIR` grammar (a missing, empty or
`-`-leading value is refused, an unknown argument is a typed error answered with the usage text);
a gate with more flags keeps its own parser. `rootpath` (above) resolves what `rootargs` read.
`cmdmaintest` is the test side: `RefusesClosedStdout` (through `Child`, which re-runs the test
binary; the package declares `func TestMain(m *testing.M) { os.Exit(cmdmaintest.Main(m)) }`) and
`WritesReportWithMode`.
