# Q52 fix 2: a close says what happens at its bounds

Answers the review `q52-fix-review.md` (findings 1-3) of commits 03bedb9da..dd4f72964. The wait
logic is unchanged: no behaviour change, only wording, docs and tests.

## 1. `sessclose.TurnWait`

`TurnWait(w Waits, where Where)` now says, in one place for every caller:

- a turn is given up to 300 s from the start of the wait and is never interrupted while it runs;
- a session never seen busy whose prompt is not ready within 30 s is not waited on longer;
- a session still not ready at its bound is stopped (its tmux session killed) on the host
  (`OnHost`), left running in its LXC container and reported failed (`InContainer`), or either,
  by where it runs (`OnHostOrInContainer`).

Callers:

| Surface                                                     | Sessions it closes              | Where passed                                   |
| ----------------------------------------------------------- | ------------------------------- | ---------------------------------------------- |
| ccy-sessions Close question and progress line (`picker.go`) | this host's, `--no-compact` too | `OnHost`                                       |
| `ccy --stop` (`stop.go`)                                    | ccy's tmux server only          | `OnHost`                                       |
| `ccy-teams shutdown` preview (`shutdown.go`)                | host and LXC seats              | chosen from the jobs: host, container, or both |

Test first: `TestTurnWait` (all three cases), the picker, `ccy --stop`, the shutdown previews
(host only, and host plus an LXC seat in `lxc_test.go`) were changed first; they failed to
compile against the old one-argument `TurnWait`, then passed.

## 2. Docs

- `docs/ccy.md` (Close and `ccy --stop`): the 300 s turn wait from the start of the wait, the
  30 s prompt wait, the stop at the bound and 60 s after `/exit`.
- `docs/agent-bus.md` (shutdown): the turn bound from the start of the wait, "never seen busy",
  and the host stop at the bound.
- F23 in `src-go/internal/ccylaunch/front.go`: the same waits and outcomes.
- `--stop` help line: one line, "a turn 300 s, a prompt 30 s, then stopped", in the bash
  launcher's help, `help.txt` and the four `difftest` help goldens (the front prints the bash's
  help text byte for byte). It stays one line because the launcher is on the bash-limits
  ratchet: a first try with two continuation lines failed `bash-limits` (2705 > 2703 code
  lines).
- `docs/ccy-changelog.md` 3.122.48.

## 3. Tests and mutations

New in `sessclose_test.go`, on the existing fake clock and panes (a `script` hook on the fake
session changes its screen over time):

| Test                                                 | Pins                                                                                                                     |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| `TestATurnFirstSeenLateIsBoundFromTheStartOfTheWait` | busy first seen at 29 s, stuck: stopped at 300 s, not 329 s                                                              |
| `TestTheTurnBoundIsCountedFromTheStartOfTheWait`     | busy from 29 s: ending at 295 s is waited for; ending at 310 s is stopped at 300 s                                       |
| `TestANewTurnAfterTheCompactionGetsItsOwnTurnBound`  | a turn before the compaction and a new one after it: /exit once it ends; stuck, stopped 300 s after the compaction's end |

Each was shown to fail on a mutant of a copy of `sessclose.go`, then the file was restored:

| Mutant                                                          | Fails                                          |
| --------------------------------------------------------------- | ---------------------------------------------- |
| `j.waitFrom.Add(c.Waits.Turn)` to `c.Now().Add(...)` (line 656) | the first two tests (5m29s; exited at 315.8 s) |
| `j.inTurn = false` dropped after the compaction (line 789)      | the third (stopped, /exit never typed)         |
| `j.waitFrom = c.Now()` dropped after the compaction             | the third (stopped 4m47s after the compaction) |

## 4. CCY version

`sessclose` is in the launcher front's `go list -deps`, and the bash launcher's help changed, so
`CCY_VERSION` 3.122.47 to 3.122.48 with a changelog entry. The container version is unchanged:
`image-source-paths` covers none of the changed packages.

## 5. QA

- `go test -race -count=1` on `sessclose`, `sessions`, `ccylaunch/...`, `teams/shutdown`,
  `teams/picker` and `cmd/claude-yolo`: all ok.
- `golangci-lint run` on `sessclose`, `sessions`, `ccylaunch`, `teams/shutdown`: 0 issues.
- `bash scripts/test-ccy-teams-command.bash`: passed 397, failed 0.
- `qa-changed.bash --base dd4f72964`: the first run exited 1 on `bash-limits` only (the
  two-line help, see section 2); after the help went back to one line it **exited 0**.
- Commit: 85c7f113d (code, tests, docs, version), pushed.

## Tooling fault seen (dogfooding)

The `bash-limits` failure ends with "a masked substitution:
CLAUDE/QA.md#assignment-masks-substitution-failure", a pointer to an unrelated rule; the finding
was only a code-line count. Misleading output, to be logged by the coordinator: this seat has
no remit to file it.
