# n3e: the eight goroutines whose end was signalled but not awaited

Branch `feat/bus-v2-n3e-goroutines`, from `406d97c0a`. The list is the "Wider form" of
`261023-n3e-merge-n3b.md`, section "DBF: project_goroutine_end ×4". The lead ruled to review each
owner's paths one at a time, and not to build the call-graph rule.

Two agents did the work. The first hit a usage limit with nothing committed. The second
(`n3e-goroutines2`) checked that work, ran a mutation for each bound, and committed four pieces,
then stopped when the session ended, before it decided `reexec` or wrote this report. The lead
decided `reexec` and wrote this report from the commits and the logs in the worktree's
`untracked/scratch/` (`goroutines-*.log`).

## Decisions

| Goroutine                                                                                           | Decision            | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| --------------------------------------------------------------------------------------------------- | ------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `wiring/e2e_world_test.go`, `link/server_test.go`, `client/client_test.go` (the three test readers) | (a), fixed          | `28abbca01`. Each reader now runs under a `sync.WaitGroup` that the test's cleanup waits on after it closes the listener, so the goroutine has ended on every cleanup path.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `host/lxc/guestroot/bound.go` (the stalled guest call)                                              | (b), by design      | `db0e53f5d`. No host call can interrupt an operation on a mount that does not answer, so the goroutine lives until the guest answers, and a memo caps it at one per member. The comment at the `go` statement says so. The pin test proves the goroutine ends once the guest answers, and was killed by a mutation (`goroutines-mut-guestroot-*.log`).                                                                                                                                                                                                                                                                                                                                                                                        |
| `host/lxc/probe.go` (the probe past `ProbeBound`)                                                   | (b), by design      | `53eb1c0d7`. Its lxc calls end at the cancel. It lives for at most the guest reads it has left, each within guestroot's `callBound`, then closes the root. The comment and the pin test hold this.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `member/engine/teams.go` (a team's stop)                                                            | (b), by design      | `23d5b950f`. `stop` waits for the team at most `stopWait`. A team in a call that ignores its cancel runs until that call returns or the link exits. The comment names the existing pin, `TestRemoveWaitsAtMostTwoSecondsForATeamThatIgnoresItsCancel`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `member/forge/token.go` (the gh read)                                                               | (b), by design      | `3c0e4afbf`. When every caller has gone, the read stays running for the next caller to take. It lives at most `ghLimit` (10 s) on the login's clock, whose cancel ends gh. The pin test is `TestAnAbandonedGhRunEndsAtItsOwnBound`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| `reexec/reexec.go` (the helper's runner, `start`)                                                   | (c), always awaited | The goroutine closes `p.done` when the child ends. The two failure paths in `start` wait for it (`reexec.go:231`, `:242`). Every `*Process` that is returned goes through `held` (`reexec.go:158`, called from `StartFiles` and `StartInUserNamespace`). Its test cleanup kills a helper that is still running and waits on `p.done` (`kill`, `reexec.go:397`). `Release` also waits (`reexec.go:381`). The new commit adds a package-wide `leakcheck.Tests` to `TestMain`, which proves this for every test in the package. Mutant: with `held`'s cleanup kill switched off, `TestCleanupKillsAHelperLeftHeld` fails (`goroutines-reexec-mutant.log`). Restored, the package is green under `-race -count=3` (`goroutines-reexec-leak.log`). |

## Commits

- `28abbca01`: the test readers wait for their goroutine on every cleanup path.
- `db0e53f5d`: guestroot's stalled call, left running by design, with its bound pinned.
- `53eb1c0d7`: the probe past `ProbeBound`, left running by design, with its bound pinned.
- `23d5b950f`: a team that ignores its cancel, left running by design past `stopWait`.
- `3c0e4afbf`: an abandoned gh read, left running by design, with its 10 s bound pinned.
- `819ae76e2`: the wind-down WIP commit, which holds the `reexec` package-wide leak check, unfinished at
  the time and not QA'd.
- The commit that adds this report: it records `reexec` as decision (c), with the QA of the whole
  branch.

## Not done, and referred

- I did not repeat the second agent's per-goroutine `-count=20` runs. Its logs are in the
  worktree's `untracked/scratch/`. This branch's `qa-changed --base 406d97c0a` run covers the gate.
- Coverage per package is the gate's figure from that `qa-changed` run. No package was reported
  below 90%.
- No new project rule. Each (b) is a judgement about one owner's paths, held by its comment and
  its pin test. The call-graph rule that would catch the wider form stays unbuilt, by the lead's
  ruling.
