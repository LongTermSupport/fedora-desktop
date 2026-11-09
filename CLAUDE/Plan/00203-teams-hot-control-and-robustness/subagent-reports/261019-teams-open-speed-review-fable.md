# Review: ccy-teams opens fast (Plan 00203 Task 3.13, branch fix/teams-open-fast)

Reviewed 9c2213cb (code in 902bc615, from F44 98cbe962), read-only, in a detached checkout of the
commit. Builder's report: `261019-teams-open-speed-opus.md`. Nothing was changed.

## Verdict

**MERGE.** No defect found. Three advisory notes (findings 1-3) for a follow-up, none of which
blocks the branch; the builder's own "one agent-bus call for the whole board" next step is the
real answer to all three.

## QA run here

- `go test -race -count=2 ./internal/teams/... ./internal/agentbus/... ./internal/declare/...`: pass
- `golangci-lint run` over the same packages: 0 issues
- project rules (`qa/driver` built, `go vet -vettool` over `./...`): clean
- `scripts/test-ccy-teams-command.bash`: 383 passed, 0 failed
- `scripts/test-ccy-teams.bash`: 214 passed, 0 failed
- qa-all not run (as asked)

## Concurrency: correct

- **Data race**: none under `-race`. Every result lives in a per-call struct (`seatRead`,
  `rosterRead`, `devicesRead`) written by exactly one goroutine; the main goroutine reads them
  only after `pool.wait()` (WaitGroup gives the happens-before). `c.devices` is read concurrently
  in `readRoster` and written only in `addMembers`, after the wait. `tape` has its own mutex. The
  `readRoster` map `at.devices` is written by the roster goroutine alone and read after the wait.
- **Pool with jobs that start jobs**: `readRoster` holds a slot, calls `p.run` for each person,
  then returns and frees its slot; it never waits on its children, so a bound of 1 (the tests use
  it) cannot deadlock. `wg.Go` from inside a running job is legal (counter > 0 during the Add).
- **Deterministic order**: rows and stderr are built in the main goroutine in the pre-existing
  order (seats by checkout, then team by team, person by person); each call's tape is replayed at
  the point its result is used. `TestCollectReportsInTheOrderOfTheTeamsHoweverTheCallsFinish`
  pins stderr, table, NeedSudo, Incomplete and the reasons byte for byte against a `Parallel=1`
  run with the first call of each kind forced to finish last. Verified the test does what it
  claims (the `held` runner gates on the finish of a specific later call).
- **Fail-fast semantics unchanged**: the old pass never aborted on an agent-bus failure; each
  failure became a row reason and an `ERROR:` line, and that is still so. No call is cancelled by
  another's failure (none was before either); all run, all are reported, the first error reaches
  stderr in exactly its old position. The only visible timing difference: nothing prints until
  every call has finished (about one call's time) rather than trickling.
- **ctx cancel** (Ctrl-C) reaches every in-flight child through execx as before.
- **`WithStderr`**: copies `Client`, `Sudo` and `term.Term` by value. `Term` carries `step` and
  `writeErr`, so the copy's `writeErr` would diverge from the original's; harmless here because
  `tapeWriter.Write` never fails, and the replay goes through `term.Printer.Print`, which records
  a failed write in `Out`. `Sudo.asked` is only read on the Ready path, which List/HumanDevices do
  not use, so no concurrent `/dev/tty` prompting.

## Dedup by path: correct

`os.Stat` of `<dir>/.claude/ccy/pingbus/seats` (follows symlinks) + `os.SameFile` (st_dev,
st_ino) matches a symlinked name and a same-filesystem bind mount. Each name still gets its own
rows (`into[dir] = shared`), tested. A checkout that disappears between the Stat and the call:
`SeatList` fails, reported as before (`could not read every seat in <dir> (above)`,
Incomplete set). The existence check itself was already in the old `readSeats`, only moved.

## sudo: safe to run eight at once

- The rule (`files/etc/sudoers.d/agent-bus-read-only.j2`) is `NOPASSWD` for exactly `list <team>`
  and `human devices <team> <name>`; `sudo -n` with NOPASSWD neither needs nor refreshes a
  timestamp, so there is no timestamp lock to contend for. A concurrent `sudo -n` that is refused
  returns exit 1 with sudo's words, which `AsRoot` maps to `ErrNeedsPassword` as before.
- `agent-bus list` (`admin.list_members`) and `human devices` (`admin.human_devices`) take **no
  lock**: `registry.load_registry` is a plain read (the `flock` is only in `update_registry`),
  and the rest is homeserver HTTP. `_session` uses the **stored admin token** (`whoami`, no
  sign-in), so eight calls within a second do not touch Tuwunel's sign-in rate buckets.
- `seat list` (`pingbus.seat.seat_lines`) probes each seat lock with `LOCK_SH|LOCK_NB` and
  releases it at once; shared probes do not conflict with each other, and the same checkout is
  never probed twice in a pass thanks to the dedup.

## /usr/bin/python3 for declare: justified

`helpers/agent_bus/declare.py` imports stdlib plus `helpers.agent_bus.{bridge,registry,teamfile}`,
`helpers.agent_bus_lxc.{derive,members}`, `helpers.pingbus.{bundle,protocol}`; every module in
those packages imports stdlib only (checked all three trees). The `agent-bus` wrapper already runs
the pyz on `/usr/bin/python3 -I`, and `python3` is a package of both `play-claude-yolo.yml` (the
play that installs ccy-teams) and `play-agent-bus.yml`, so the interpreter is guaranteed by the
same play. `fedora-desktop-health` already names the path for the same pyenv-shim reason.

## Output unchanged

The board's table, rows and golden layout tests pass unmodified; the 383-case bash oracle suite
and the 214-case ccy-teams suite pass. The only test edits are the `python3` -> `/usr/bin/python3`
argv prefix in the declare fakes.

## Findings (advisory, none blocking)

1. **The bound of 8 is a constant, not a function of the machine.** On the 22-core host at load
   30-39, 8 extra runnable processes is a modest and short burst, so it is a sensible figure
   there. On a small laptop (4 cores) eight Python starts at once would be a worse experience
   than four. `min(8, runtime.NumCPU())` would cost one line; `Collector.Parallel` already
   exists as the seam. Not worth blocking the merge: the real fix is the builder's proposed
   single `agent-bus board` call, which removes the interpreter starts rather than scheduling them.
2. **"The rosters go first" is best effort.** `p.run` starts goroutines that race for the slots;
   Go gives no ordering guarantee between them, so a seat list may take a slot before a roster.
   It is an optimisation of the critical path (roster then devices), not a correctness
   property, and the doc comment in `fetch.go` could say "are started first" rather than "go
   first". Cosmetic.
3. **User-wrapper stderr is now a pipe, not the terminal.** The builder noted it: if agent-bus
   coloured its own stderr on a TTY, a failing `seat list` would now print uncoloured. I found no
   colour code in the user-side helpers, and the stream only carries anything on failure. No
   action.

## Plan hygiene

PLAN.md Task 3.13 entry, the journal entry (through mkplan, timestamped) and
`meta-deploy.list` (play-claude-yolo.yml, with a description) are all in the same commit as the
code. No CCY version bump needed: nothing under `files/var/local/claude-yolo/` changed.
