# Port report: reclaim (Task 3.3, P6)

Branch `go-port/reclaim`, cut from F44 `842d88ddd`; not merged (a Fable review comes first).
Work commit `77a948631`.

## Why reclaim

Of the four P6 tools left, `reclaim` (456 code lines, 29 functions, interactive, churn 5) and
`nord` (505 lines, 18 functions, churn 8) rank highest. `reclaim` is the one a fake proves
without the owner: its danger is which commands run after which answer, which a recorded
command list pins. `nord` needs a live NordVPN connection and changes routing, `nvidia-status`
needs the GPU, and `ssh-with-key` is small and is credentials. Detail in
[SPEC-reclaim.md](../SPEC-reclaim.md) section 1.

## Built

- Spec [SPEC-reclaim.md](../SPEC-reclaim.md): D1-D4, E1-E29, B1-B11, H1-H5.
- `src-go/cmd/reclaim` and `src-go/internal/reclaim`; shared packages reused: `execx`
  (`Fake`, `Foregrounder`, `Combined`), `sudo`, `term`, `ui.Heading`, `rclonecache.IEC`.
- `src-go/qa/rules/table.go`: `internal/reclaim` added as an owner of `podman` and `docker`.
- Tests written first and seen red (39 failing against a stub), then green under `-race`;
  golangci-lint 0 issues; the project rules driver clean.

## Oracle

`src-go/internal/reclaim/testdata/scenarios.json`: 27 scenarios (programs installed, their
answers, the home's files, the lines typed; expected exit status, changing commands with root
marked, files left). Judged against the Go by `TestScenarios` (and `TestScenarioTableCanFail`),
and by `reclaim-parity/parity.py` against the built Go (27/27) and the bash at `842d88ddd`
(20 the same, 7 differing exactly as each `bashDiffers` departure says). Wording is not
compared. Bash defects found: B3 (a failing `dnf repoquery` preview read as "nothing", then
autoremove and clean ran on a yes) and B7 (`du` with an unreadable entry ended `reclaim report`
halfway with status 1).

## Cut over and deleted

- `files/home/.local/bin/reclaim` deleted (it had no suite); its row left
  `scripts/bash-limits.allowlist`.
- `play-cli-tools.yml` builds the command with `tasks/build-go-command.yml` (tag
  `disk-reclaim`, which now also imports `play-golang.yml`).
- `scripts/go-binary-suites.tsv` gains `reclaim	qa-go`; `scripts/test-qa-changed.bash` uses
  `nord` as its allowlisted-tool fixture.
- Docs: `docs/playbooks.md`, `src-go/CLAUDE.md`; PORTS.md and Task 3.3's status ("built,
  awaiting review"); journal entry; `meta-deploy.list`'s `changed-plays` description names
  `play-cli-tools.yml` (to run once merged).
- `qa-changed.bash` passed before the commit.

## Host checks

All the host agent's, after the merge and the play run: H1 (ELF, `--version`, `reclaim report`), H2 (menu in tmux: ncdu, files, refresh, invalid exits, Ctrl-D), H3 (previews behind
sudo answered No; sudo given at the run), H4 (cache and trash cleared on a scratch HOME,
podman shown and not pruned), H5 (baobab by screenshot). The real cleanups answered yes are
the owner's to run when wanted.

## Left

The Fable review and its fixes, the merge, the play run through meta-deploy, H1-H5. Two tests
skip when run as root (the container): the unreadable-entry count and the removal failure;
they run on the host's QA.
