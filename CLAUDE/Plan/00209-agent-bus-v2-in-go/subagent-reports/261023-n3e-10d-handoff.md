# n3e-10d handoff: the link leak and the pidfd hang (Plan 00209 N3 n3e-10 fix)

Worktree /workspace/untracked/worktrees/bus-v2-n3e-10, branch feat/bus-v2-n3e-10. Earlier state:
[261023-n3e-10c-handoff.md](261023-n3e-10c-handoff.md); report so far:
[261022-n3e-10-fix.md](261022-n3e-10-fix.md) (extend it with the two remediations below, using
the DBF report template; spec 1.1.0, cached).

## Done

Class A, `project_goroutine_end` (a go statement whose goroutine signals no end, so no owner
can wait for it).

- Independent search, dispatched before the rule:
  [261023-n3e-10d-goroutine-search.md](261023-n3e-10d-goroutine-search.md).
- Red commit `1ea729a07`: rule `src-go/qa/rules/goroutineend.go` (in `rules.All()`, count 10,
  driver test lists it), fixture `qa/rules/testdata/src/fix/internal/badgo`, catalogue row and
  section in `CLAUDE/GoProjectRules.md`. Real tree: 15 findings, the same untagged, with
  `busfailpoints` and with `tuwunel`; none in `qa`. Sweep command:
  `go vet -vettool=untracked/scratch/dbf-driver -project_goroutine_end [-tags=X] ./...` from
  `src-go` (rebuild the driver with `go build -o ../untracked/scratch/dbf-driver ./driver` in
  `src-go/qa`).
- Commit `09eb8ba57` (instances, part):
  - link: reader on `l.reader` (WaitGroup); `stopReading` (deferred right after the start)
    closes quit once (`closeQuit`, a `sync.OnceFunc`), closes the control socket, waits.
    `srv.run` on `l.bg`; the per-request watcher in `server.go` is
    `defer context.AfterFunc(s.stopping, cancel)()` (`stopping` is now a context).
  - keeper: link and CMD waits and the link reader on `k.bg`, waited after `close(k.done)`;
    timer work goes through `post`/`runFired` (a queue plus a `fired` wake the loop selects
    on), no goroutine; pidfd `Close` waits `waited`; inotify `Close` waits `readers`.
  - units: `session.close()` closes the conn and waits `dispatched`.
  - pingbus-status-line and teamchat statusline: the signal relays on a WaitGroup waited after
    the stop.
  - `internal/teamchat/leakcheck` (new, 98% coverage): `Tests(m)` for a TestMain, `Check(tb)`
    for one test. `reexec.Main` now takes `reexec.Runner`. Link's `TestMain` is
    `reexec.Main(leakcheck.Tests(m), helpers)`; `newKeeperWorld` calls `leakcheck.Check(t)`
    first.
  - The original's reproduction: before the fix the four link tests that end before the
    launch (`TestTheFirstMessageIsTheLaunch`, `TestNoLaunchInTenSeconds`,
    `TestPidfdFiresBeforeLaunch`, `TestAnEngineThatCannotBeMadeIs78`) each failed leakcheck
    with the reader left running (log `untracked/scratch/leak-red.log`); after it the link
    package passed once under race (`leak-green.log`).
  - Green so far (race, count 1): link, keeper (90.8%), statusline, pingbus-status-line (83%:
    check this was its coverage before), reexec, units (count 3).

Class B, found while tracing the pidfd hang: a RawConn Read callback that does not test its
descriptor. Cause of `TestTheWatchFiresWhenTheProcessEnds`'s hang: `RawConn.Read` calls
`prepareRead`, which resets readiness the poller already saw (`runtime_pollReset`); `watch` in
`link/parent.go` returns false on its first call without testing the pidfd, so a keeper that
died (here: killed and reaped by `p.Kill()`) before the watch goroutine first ran is never
seen. A production bug too: the link would miss its keeper's death. The keeper's own pidfd
wait (`keeper/osproc.go` `wait`) tests with `unix.Poll(fd, 0)` on every call and is right.
6000 isolated runs at low load passed (`untracked/scratch/watch-alone.log`): it needs load.
Independent search record:
[261023-n3e-10d-readiness-search.md](261023-n3e-10d-readiness-search.md) (one instance, the
originating one).

## Left

1. Class A, the rest:
   - `src-go/internal/execx/cleanup.go`: SignalContext's listener now closes `ended` and stop
     waits; the change is UNCOMMITTED in the working tree because the pre-commit hook demands
     a CCY version bump (execx is in the ccy launcher's build). SignalContext has no
     production caller. Decide: bump `CCY_VERSION` in `files/var/local/claude-yolo/claude-yolo`
     with the change, or ask the lead. The WIP commit message says it was fixed; it was not
     committed.
   - `internal/teams/flow/kit.go:264` is owned (every return sends `got`, and readLine waits
     on every path; the searcher says DOES NOT). Narrow the rule, as excluding hazard-free
     code: a body signals its end also when every return of its own is directly preceded by
     an end statement and it cannot fall off its end without one (its last statement is an
     end statement, or a `for` with no condition and no break). Add fixture lines (every
     return sends: clean; one return without: flagged; a loop with a break: flagged).
   - `internal/ui/lines.go:185` `readLines`: `want` is never closed and `Lines` has no Close.
     Give `Lines` an owner path (a Close that closes `want` and waits, called by its users),
     or restructure; read its callers first.
   - `internal/vmtest/app.go:110` `WithSignals`: as SignalContext, `defer close(ended)` in the
     goroutine and `<-ended` in release.
   - Rerun the sweep to 0 in all three builds; run each touched package's tests
     (race, shuffle, cover >= 90%).
   - Add `leakcheck.Tests` to the engine's test main (`member/engine` has no TestMain yet:
     `func TestMain(m *testing.M) { os.Exit(leakcheck.Tests(m).Run()) }`) and fix what it
     finds. Then update the GoProjectRules section's Green and Not caught lines (mention
     leakcheck there).
   - The searcher's unwaited-but-ended items not caught by the rule (stdin and tty readers
     abandoned by design with a buffered result, `units/bus.go:30`, `execx/session.go:64` on
     the grace path, the engine team goroutine bounded by `stopWait`): judge each in the
     report; the rule's Not caught line already names "an end signalled and never awaited".
2. Class B DBF: rule (for example `project_rawconn_probe`: a `syscall.RawConn` Read or Write
   callback whose fd parameter is unnamed, blank or unused, or that is not a function literal
   or same-package function), fixture, red commit, sweep (1 finding expected), fix
   `link/parent.go` `watch` to test with `unix.Poll` like the keeper (or share one helper),
   and a test reproducing the original deterministically: open the pidfd on a reexec helper,
   kill it, wrap the fd and give the poller time to see it (a short `time.Sleep` is allowed:
   the clock rule bans only Now, Since and Until) before the watch starts reading; the old
   callback hangs (bound the wait with `time.After`), the new one fires. That needs `watch`
   split into open plus a watch of an open fd.
3. `untracked/scratch/stress.bash` (the worktree's) as the c handoff describes, then the
   package proof (seeds 1792717137451862071, 1, 42, 20261023; -count 20 -cpu 1,4; untagged and
   busfailpoints), coverage, golangci clean in three builds, `./scripts/qa-changed.bash`.
4. `defence-before-fix:conformance-reviewer` over both remediations (red commits `1ea729a07`
   and class B's), then the report.
