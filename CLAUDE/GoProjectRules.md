# src-go project rules (Defence Before Fix)

Defect classes the `ccy-teams` and `ccy-sessions` ports actually hit, each held by a
`go/analysis` analyzer in `src-go/qa/rules`. They run from `scripts/qa-go.bash` as a hard
step: the driver (`src-go/qa/driver`) is built once and used as `go vet -vettool` over
`src-go`. Every finding fails the gate. There is no inline suppression: the only excuse is a
row in a table in `src-go/qa/rules/table.go`, with its reason.

**A new defect class found in `src-go` gets a rule here before its fix.** Write the rule, prove
it red on a fixture reproducing the defect (`src-go/qa/rules/testdata/src`, run by
`analysistest`), then green on the real tree, fixing every real instance it finds.

The rules live in their own module (`src-go/qa`, its own `go.mod`, depending on
`golang.org/x/tools`) because the shipped binaries are built on the host with plain `go build`
and `src-go/go.mod` must stay dependency-free. `qa-go.bash` checks that module too (format,
vet, test, coverage, lint). This file is under `CLAUDE/` because the markdown guard allows no
`.md` beside the Go code.

## Fit with go-qa-ci

go-qa-ci documents project analyzers as a project driver package, analyzers proven with
`analysistest`, and one page per analyzer under its `docs_dir`. The analyzers and their
`analysistest` proofs match, and the names are identifier-only (`project_*`), so adopting it is
a change of driver (`driver.Main(rules.All()...)` in place of `unitchecker.Main`). Gaps, because
of the dependency-free constraint on `src-go/go.mod`: go-qa-ci expects the driver in the
project's own module (`[analyzers] driver`) and here it sits in the nested `src-go/qa` module,
which its per-module run (`modules` in `qa/go-qa.toml`) must be told about; and the per-rule
pages are not written, this catalogue stands in until adoption.

## The rules

| Rule                    | Defect class                                                                                                                    | Found in                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `project_seam_exec`     | a process started outside the runner, so no fake sees it (including `sh -c` evasion of gosec G204)                              | `teams`: `syscall.Exec` in the launcher hand-over                              |
| `project_seam_commands` | an owned program named as a command outside its package (sudo without `-n`; tmux target `=name:` vs `=name`, fedora-desktop#69) | `sessions`: `systemctl reboot`; `teams`: the take-down heading via `gum style` |
| `project_testclock`     | wall-clock reads in tests (date-dependent tests failed in the hour after midnight)                                              | `transcript` and `sessprobe` tests: 23 `time.Now()` fixtures                   |
| `project_homeabbrev`    | a hand-rolled `~` home abbreviation (a bare prefix replace turned `/home/<user>X` into `~X`)                                    | `teams` tests: `strings.Replace(..., "~", 1)` twice                            |

### project_seam_exec

`exec.Command`, `exec.CommandContext`, `syscall.Exec` and `os.StartProcess` only in
`internal/execx`. `exec.LookPath` is allowed anywhere.

- Red, fixture: `testdata/src/fix/internal/badexec` (each call flagged, `sh -c` included,
  `LookPath` clean; the owner `execx` clean).
- Red, real tree: `internal/teams/app.go:274: syscall.Exec outside internal/execx`.
- Green: `execLauncher` now hands over through `execx.Execer` (`launcherVia`, tested with the
  fake).

### project_seam_commands

An `execx.Cmd` whose `Name` is a constant (literal, named constant, keyed or unkeyed literal,
or `.Name =` assignment) equal to an owned program, outside its owner. The owners are the
`commandOwners` table: `sudo` to `internal/sudo`, `tmux` to `internal/tmuxccy`, `gum` and
`fzf` to `internal/ui`, `systemctl` to `internal/systemd`, `git` to `internal/gitx`. A
`LookPath("gum")` presence check and text that merely contains the name are not commands and
are not flagged; test files are exempt (a test spells out the argv it expects).

- Red, fixture: `testdata/src/fix/internal/badcmd` (every form flagged; `wl-copy`, `LookPath`,
  prose and the `_test.go` file clean; the `sudo` owner clean).
- Red, real tree: `internal/sessions/reboot.go:199` (`systemctl`),
  `internal/teams/takedown.go:105` (`gum`).
- Green: `systemd.Reboot` (new, tested) and the existing `ui.Gum.Box`.

### project_testclock

`time.Now`, `time.Since`, `time.Until` in `_test.go` files. Excuse: a `clockAllowances` row
(package, test function, reason). One row exists, `TestTakeOverDefaultsToTheRealClockAndSleep`,
which proves the default `Sleep` really sleeps and can only do that against the wall clock.

- Red, fixture: `testdata/src/fix/internal/badclock` (the three calls flagged; `time.Date`
  and production code clean) and `allowedclock` (the allowed function clean, its neighbour
  flagged).
- Red, real tree: 23 findings in `transcript_test.go` and `sessprobe/join_test.go`.
- Green: fixed instants (`fixedMtime`, `sampledAt`).

### project_homeabbrev

`strings.Replace`, `ReplaceAll`, `TrimPrefix`, `HasPrefix`, `CutPrefix` with a string constant
beginning `~`, anywhere (test files included) except `internal/term`, which owns
`term.AbbrevHome`.

- Red, fixture: `testdata/src/fix/internal/badhome` (each function flagged, a `_test.go`
  flagged, an unrelated `Replace` clean; the owner `term` clean).
- Red, real tree: `internal/teams/collect_test.go:270`, `element_test.go:47`.
- Green: both use `term.AbbrevHome`.

## Control

`scripts/qa-go.bash` was run with `strings.HasPrefix(p, "~")` planted in `internal/gitx`: every
earlier step passed and the gate failed at the rules step with a `project_homeabbrev` finding.
The plant was removed.
