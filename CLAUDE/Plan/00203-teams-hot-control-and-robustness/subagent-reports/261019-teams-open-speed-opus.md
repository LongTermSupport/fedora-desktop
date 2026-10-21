# ccy-teams opens fast (Plan 00203 Task 3.13)

Branch `fix/teams-open-fast`, from F44 at 98cbe962. Not pushed, not merged.

## What made the first screen slow

`ccy-teams --list` and the picker's first screen are one `board.Collector.Collect` pass:
systemctl, then the declare overview, then tmux, the session registry and git, then every
agent-bus call, strictly one after another.

### (a) `seat list` six times

The six runs are **not repeats**. The argv is the same (`agent-bus seat list`) but each run has
a different working directory: `seat list` reads the seats of the git checkout it runs in, and
the board runs it once in each checkout it knows of (tmux sessions of ccy and cc, session records,
desired memberships, the folder ccy-teams runs in) that has a `.claude/ccy/pingbus/seats`
directory. A process poll reads argv, not the working directory, so they looked the same. Six
runs means six such checkouts on the host.

Two things were done:

- **Read once per checkout, whatever it is called.** A checkout reached by two names (a symlink,
  a bind mount: tmux's session path against git's resolved top level) was read twice. The board
  now matches the seat directories with `os.SameFile` and reads each one once. Each name still
  gets its own rows, so the table does not change.
- **Run together** (b, below).

Reading every checkout with **one** call is not possible without a new agent-bus subcommand, and
this branch does not add one (see "Next step").

Tests: `TestCollectReadsTheSeatsOfACheckoutOnceHoweverItIsNamed` (red: 2 runs for one checkout
under two names; green: 1), `TestCollectReadsEachCheckoutsSeatsOnce` (three checkouts, one run
in each).

### (b) The per-team calls run together

`board/fetch.go`: `Collector.read` starts every agent-bus call of the pass in a small bounded
pool (8 at once; `Collector.Parallel` sets another limit; the tests use 1 and 2): each up team's
`sudo -n agent-bus list`, then, as soon as its roster is in, `human devices` for each person
whose count is not kept, and each checkout's `seat list`. The rosters start first because each
one has a second call waiting on it. A person listed twice is read once.

**Errors are reported exactly as before, in the same order.** Each call gets a copy of the
agent-bus client (`agentbus.Client.WithStderr`) whose stderr (the user wrapper's stream, and the
stderr `sudo.AsRoot` copies to the terminal on failure) goes to that call's own tape. The table
is then built in the old order (seats by checkout, then team by team, person by person), and each
call's tape is written to the stream it was meant for at the point where its result is used,
followed by the same `ERROR:` lines and the same per-row reasons as before. Nothing that failed
is skipped, and the exit code rules are unchanged.

Tests: `TestCollectRunsTheAgentBusCallsTogether` (the two seat lists and two rosters are held
until all four run at once; red: one at a time, timed out; green), `TestCollectRunsNoMoreThanItsBoundAtOnce`,
`TestCollectReportsInTheOrderOfTheTeamsHoweverTheCallsFinish` (a seat list, a roster and a
person's sessions fail; the first call of each kind is made to finish last; stderr, the table,
NeedSudo, Incomplete and the reasons are byte for byte what the one-at-a-time pass gives, and
the failures appear in checkout then team order), `TestCollectMembersReadsAPersonListedTwiceOnce`,
and the agentbus tests `TestWithStderrSendsEachStderrThroughWrapAndLeavesTheClientAlone` and
`TestWithStderrKeepsACapturedStderrCaptured`. The board's existing tests (table, rows, golden
layout) pass unchanged.

One small difference: the user wrapper's stderr used to go straight to the terminal and now goes
through a pipe. If agent-bus colours its own stderr when that stream is a terminal, a failing
`seat list`'s message would now be printed without colour. The words are the same, and the
stream only carries anything when a call fails.

### (c) The in-process gaps

There is nothing slow in-process. A CPU profile of the whole pass and table on a zero-latency
fake (4 teams, 6 checkouts) shows **0.9 ms** of Go work, 18 commands, and nothing of the pass in
the profile (only the fixture setup's `mkdir`). The 0.1-0.4 s gaps on the host (1.3 s in all)
are the short children a `/proc` poll misses, run one after another on a host at load 30-39:
`systemctl list-units`, `tmux list-sessions`, two `git rev-parse` (is this folder a checkout,
its top level), plus each child's fork and exec under load. None of them is wasteful (each is
read once per pass, `TestCollectReadsEachSourceOncePerPass`), so they were left as they are.
Running tmux, the registry and git alongside systemctl and the declare overview would hide them
too, but that changes the order of the start-up error messages and starts commands a pass with
no teams never needed. It is not worth that for the size of the gain.

### (d) The declare overview and the pyenv shim

Confirmed: `declare.Client` ran `env PYTHONPATH=<checkout> python3 -P -m helpers.agent_bus.declare`,
so on the host `python3` resolves to the pyenv shim (a bash script, about 0.1 s before Python
starts). There is precedent for naming the interpreter by path for this reason:
`fedora-desktop-health` and `host-health-report.bash` run their helpers as
`PYTHONPATH=... /usr/bin/python3 -P -m helpers...`, with a comment giving the pyenv shim as the
reason, and agent-bus itself (`/usr/local/bin/agent-bus`, `agent-bus-hs-health@.service`) runs on
`/usr/bin/python3`. `helpers.agent_bus.declare` is stdlib only and is part of the same
code family. So the declare client now runs `/usr/bin/python3` (the `systemPython` constant in
`internal/declare/declare.go`). The other Go callers (vmtest) run a bare `python3` and were left
alone. The declare integration tests ran green on the real interpreter. The gain on every
declare call (overview on each redraw, and every edit) is about 0.1 s, and in the fakes it
cannot be measured.

## Before and after, on fakes

The rig is a temporary test, not committed (it reads the wall clock, which the project rules
forbid in tests). It has 4 teams up, one person each, and 6 checkouts with seats. Each agent-bus
call and the declare overview take 0.4 s, and systemctl, tmux and git take 20 ms each. The
`--list` pass was timed end to end:

|                                             | wall clock                             | most calls at once |
| ------------------------------------------- | -------------------------------------- | ------------------ |
| before (F44 98cbe962)                       | 6.12 s                                 | 1                  |
| after, bound 6 (first try)                  | 1.66 s                                 | 6                  |
| after, bound 8, rosters first (this branch) | 1.26-1.67 s (3 runs: 1.27, 1.67, 1.26) | 8                  |

The best this pass can do with this many calls is 0.4 (declare) + 0.06 + 0.8 (roster, then
sessions) = about 1.26 s, and the branch reaches it in most runs. The 1.67 s runs are the
scheduler putting a person's sessions call into a third round.

On the host the gain depends on what the 0.45 s per call is spent on. If it is mostly waiting
(disk and imports from a cold cache), the whole agent-bus phase of 2.6 + 4.4 s comes down to
about 1-1.5 s. If it is CPU (Python compiling and importing) on a host already loaded past its 22
cores, calls run together compete for the same cores and the gain is smaller. The host
measurement after deploy will show which. Either way, nothing waits on a call it does not need.

## QA run

- `go test -race ./internal/teams/... ./internal/agentbus/ ./internal/declare/`: pass (board
  also `-count=3`)
- `golangci-lint run` over the same packages: 0 issues. `gofmt -l`: clean. `go build ./...`: ok
- project rules (`qa/driver` as `go vet -vettool`, over `./...`): clean
- `scripts/test-ccy-teams-command.bash`: 383 passed, 0 failed
- `scripts/test-ccy-teams.bash`: 214 passed, 0 failed
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK
- `scripts/qa-changed.bash` before the first commit: docs and the seven Go command suites
  passed. The go gate failed in the nested `src-go/qa` module, which this branch does not
  touch: `qa/cmd/qa-patterns` `TestRealSemgrepScansTestsDirectoriesAndLargeScripts` hit a
  semgrep per-file `Timeout` on its `scripts/big.bash` fixture at load 39.9. It failed again on
  its own at that load and passed (10.8 s) at load 35.7. That test is sensitive to host load,
  and the problem should be followed up in the qa module. The gate's later steps over the main
  module, which stop after that failure, were run by hand: fmt diff empty, build, vet,
  `go test -race` with coverage 91.3% (floor 90), golangci-lint 0 issues. `qa-changed.bash --base 98cbe962` was then run over the whole branch.
- `scripts/qa-changed.bash --base 98cbe962` over the committed branch, at load 16-28: passed, 18
  changed files. The docs gate, the whole go gate (`qa-go.bash`, the qa module included) and all
  seven Go command suites passed.

## Deploy

`CLAUDE/Plan/meta-deploy.list` queues `playbooks/imports/play-claude-yolo.yml`, which rebuilds and
installs ccy-teams. No CCY version or image change: nothing under `files/var/local/claude-yolo/`
was touched.

## Next step (not in this branch): one agent-bus call for the whole board

Most of the time left is Python starting 14 times: 6 `seat list` runs as the user, and 8 root
chains of sudo, bash, runuser and Python. Two calls could replace them:

- a root subcommand that prints every named team's roster and each person's devices in one run
  (`agent-bus board <team>...`, with its own sudoers rule matching the exact argv), and
- `seat list` taking several checkouts, as the user.

Estimated gain on the host: the agent-bus phase goes from 14 interpreter starts to 2, about
0.5-1 s on the wall clock rather than this branch's 1-1.5 s. CPU spent per open falls by about
12 x 0.45 s, roughly 5 s, which is what matters on a host at load 30-39. This needs a new
subcommand, a sudoers rule and a play change, so it belongs in its own branch, as asked.
