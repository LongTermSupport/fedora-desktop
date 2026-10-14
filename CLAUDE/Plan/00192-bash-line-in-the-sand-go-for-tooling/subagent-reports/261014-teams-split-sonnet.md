# internal/teams split, test-seam removal, DRY, redraw cost

Branch from F44, five commits, not pushed, not merged.

## Result

- Parity suite `scripts/test-ccy-teams-command.bash`: 323/323 before and after, suite file unchanged.
- `bash scripts/qa-go.bash`: green (31 packages, coverage 94.5% against a 90% floor, golangci-lint clean, qa module 95.0%, project rules clean).
- `./scripts/qa-changed.bash`: green, 60 changed files, all five parity suites and `test-qa-changed.bash` pass.
- `qa-all.bash` was not run, as instructed.

## Commits

1. `92a2e983` one FileIsTerminal, YesNo, atomic write and ccy-installed check, shared by the tools.
2. `729ed4d9` `declare.py overview` answers declared and status in one run.
3. `0f4c3a64` a ccy-teams redraw starts 7 processes where it started 21.
4. `9703a758` `teams/config` and `teams/create` become packages of their own.
5. `f747e84e` the rest of the split: board, ops, picker, seats, login, flow, host, teamtest.

## New package map (`src-go/internal/teams/`)

| Package        | Owns                                                                                                                                                   |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `teams` (root) | `App`: dispatch, usage, wiring. `usage.txt`. Nothing else.                                                                                             |
| `config`       | `Config`, `Load`                                                                                                                                       |
| `host`         | `Host`: the adapters over one runner (sudo, systemd, tmux, git, declare, bus, registry, gum, picker, streams, env, LookPath, OpenTTY, Exec) and `Wire` |
| `flow`         | `Kit`: what interactive flows share (Tell, ReportErr, Confirm, Choose, Refuse, SudoReady, WaitForEnter...), `Result`, `Leave`, `ExitCodeOf`            |
| `board`        | `Collector` (collect, refresh, device cache), `Snapshot`, table painting, `--list`                                                                     |
| `ops`          | `Ops`: apply, remove, backup, restore, element play, trust build-tag files                                                                             |
| `create`       | the wizard                                                                                                                                             |
| `login`        | Element and passwords                                                                                                                                  |
| `seats`        | attach, new session, restore toggle, remove or park, start ccy                                                                                         |
| `picker`       | the interactive picker: loop, keys, offers, help                                                                                                       |
| `teamtest`     | fake machine for tests: `World`, `Script`, `Console`                                                                                                   |

A package takes the few adapters it uses as fields and imports no sibling flow it does not call.
Docs updated: `src-go/CLAUDE.md` layout, `CLAUDE/GoProjectRules.md` evidence paths, and the
`src-go/qa/rules/table.go` rows (`hostPkg`, `opsPkg`).

## Test-only indirection removed

- `liveOps`, the `teamOperator` interface and the `fakeOps` double are gone.
- Picker, seats, login and wizard tests now run the real `ops.Ops` over `execx.Fake` and the
  `teamtest` scripts, so they exercise the production path.
- `Status.Word()` moved to `declare/words.go`, replacing a teams-local `status.go`.

## DRY results

- One `term.FileIsTerminal` (now excluding /dev/null); the sessions-only copy is gone.
- One `term.YesNo`, replacing copies in registry, restore, sessions/setgoing and teams.
- New `fsx.WriteAtomic` and `fsx.RemoveIfPresent`, used by registry and restore.
- New `ccyenv` (`Missing`, `InContainer`, constants), used by teams and sessions.
- `agentbus.IsCommandError` replaces a duplicated `isCommandErr`.
- `term.Printer`, `term.Bold`, `term.Reset` exported instead of re-declared.

### Left alone, for the other agent

The `ps` runner is already single (`sessprobe.ProcessTable`). What is still duplicated is the
`ParseProcs` pair in `sessions/routes.go` and `sessprobe/collect.go`. Both are in the area I was
told to stay out of, so it is not touched.

## Redraw cost (pinned in `board/redraw_cost_test.go`)

Scenario: 3 teams, 3 humans each.

|                   | Before       | After |
| ----------------- | ------------ | ----- |
| First draw        | 21 processes | 17    |
| Each later redraw | 21           | 7     |

Before: declare.py 2, git 1, systemctl 4, tmux 1, seat list 1, sudo list 3, devices 9.
After, first draw 17: overview 1, systemctl 1, tmux 1, git 1, seat list 1, agent-bus list 3,
devices 9. After, steady redraw 7: overview 1, systemctl 1, tmux 1, seat list 1, list 3.

How: the ACTIVE column of one `list-units` replaces one `is-active` per team
(`systemd.StateWord`); one `declare.Client.Overview` call replaces separate declared and status
runs; the git work tree is asked once; a per-team-and-person device cache keeps "signed in"
results until `ForgetDevices`, which `Ops.Changed` (after apply, remove, restore) and
`IssuePassword` call. A failed read is not cached (`TestAReadThatFailedIsAskedForAgain`).

## Residual notes

- The wizard keeps function fields `ApplyTeams`, `NewPassword`, `LaunchElement`, `ExecLauncher`.
  `create.New` wires each to the real flow, so they are cross-package composition, not a
  test-only interface.
- `declare.Client.Declared` and `Status` are now used only by tests; `Overview` is the
  production path. They can be deleted when the tests move to `Overview`.
- Two `askYes` wrappers remain (`flow.Kit.AskYes` and the wizard's); both wrap `Gum.Confirm` and
  could merge in a follow-up.
