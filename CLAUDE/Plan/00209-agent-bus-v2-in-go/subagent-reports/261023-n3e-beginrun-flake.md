# n3e: the BeginRun flake (Defence Before Fix report)

Branch `feat/bus-v2-n3e-beginrun`, from `feat/bus-v2-n3e` at `9c21548ec`. Test files and the
Go rules catalogue only; no product code changed.

## Specification followed

Defence Before Fix method specification 1.1.0, detector specification 1.0.0, toolchain
specification 0.2.0, each the cached copy the plugin's refresh script printed (no fetch failed).

## Toolchain

The project's detector is `go/analysis` in `src-go/qa/rules`, run by go-qa-ci's driver in the Go
gate (`scripts/qa-go-qa.bash`, and `qa-changed.bash` in diff mode); its catalogue, the listing of
the defences and the one place exceptions are written, is `CLAUDE/GoProjectRules.md` with the
excuse tables in `src-go/qa/rules/table.go`. The single-rule harness is `analysistest` in
`src-go/qa/rules/rules_test.go`. No manifest declaration of the method was found in `src-go`
(a Go module has no manifest key for it); none was added. The class was not already defended:
no rule in the catalogue judges test synchronisation.

## Defect and class

Originating instance: `src-go/internal/teamchat/member/engine/proof_test.go`,
`TestBeginRunReturnsAnEarlierRunsEntries`, the store read right after `pw.e.BeginRun` (line 633
at `9c21548ec`).

The failure, reproduced under load with the busfailpoints test binary built `-race`
(`-test.count=200 -test.shuffle=on`, `-test.cpu` 1, 2, 4 and 8, six runs each, beside a loop of
`go test -race ./internal/teamchat/...` on a host already at a load of 50 to 60): 19 failures
in the 13 runs that finished inside the time cap (about 2,600 iterations; 1 at `-cpu 1`, 17 at
`-cpu 2`, 1 at `-cpu 4`), every one the same:

```text
proof_test.go:633: after BeginRun: {... Pending:[{ID:$...001 ... Repeat:true Held:false ... Handed:0xc00035e4d0} ...
```

Mechanism: the world is live (session facts told by `pw.facts(true)`, the roster verified, the
verdicts set), and `BeginRun` makes the store this run's and pokes the delivery loop
(`returnEarlierRun`'s `e.poke()`; `look` then passes `begun`). The loop dials the fake socket,
whose event channel is buffered so the loop never waits on the test, persists a new awaiting
entry with the returned items handed, and writes the user line. When the test goroutine is
descheduled between `BeginRun`'s return and `pw.store(a)`, it reads that later store: the items
handed, an awaiting entry, the ledger refilled. Proven deterministically by waiting for the
fake's first write between the two lines: the assertion fails the same way every run. The code
under test is right: delivering the returned items at once is what 15b asks for.

Class: a test asserts on state that a goroutine or process of the code under test, still
running and able to act, is free to change between the test's action and its read, so the
assertion holds only if the test goroutine wins the race. Hazard: a test that fails rarely and
only under CPU load, whose message is seldom kept.

## Rule

None could be written; the class is recorded under "Defect classes that cannot be a rule" in
`CLAUDE/GoProjectRules.md`, which says what holds it instead. Whether a read races depends on
what is live at that moment: session facts told, a roster verified, items deliverable and not
held, a timer that hands its work to another goroutine (`keeper.after` runs its callback through
`go k.send`) or a client that exits before the server has applied its ack. None of that is a
shape in the syntax tree. The rules considered:

- A store read (`w.store(`) after an engine call that reaches `poke`, in a test of a world with
  an inbox: it matches the reads after `quietFor` (the fake clock runs the timer inside
  `Advance`, and the items are held after a miss), after `settled` and `until`, and in worlds
  never told the session facts, all of them ordered. Most of its matches would be reads that do
  not carry the hazard, which the method forbids.
- The next wider, a read after any wait on a log line (`logged`, `log.String()`): the keeper's
  log is written after `keeper.json` and the reports, and the searchers found those reads
  ordered; only the lock probe after the log races. Same objection.

The method's Appendix A then allows the conventional fix, which follows, with the sweep done by
search and reading instead.

## Proof

No rule, so no red commit of a rule. Each instance was proven red and green by a scratch delay
in the code under test (never committed) widening the window the test races across:

- engine: a wait for the fake's first write between `BeginRun` and the read: the old test fails
  at `proof_test.go:634`; the fixed test parks the loop first, so no write can come.
- keeper: a 300 ms sleep between the log line and the lock close in `fail()` (`loop.go`) and
  between the log line and `drop` in the owed timer (`seats.go`): the old tests fail
  (`kp_test.go:181: a failed link's seat is still held`, `rc_test.go:191: the seat is held after the notice's 30 s`); the fixed tests pass.
- wiring: a 500 ms delay before `eng.Ack` in the link's `settle` (`server.go`): the old
  `TestE2EItems` fails (`e2e_test.go:44: inbox after the acks: [...]`); the fixed test passes.

## Independent search

Two `defence-before-fix:independent-searcher` runs, dispatched before any fix and kept out of
`src-go/qa/rules`: the first over `member/engine`, `member/link`, `member/teamsync`,
`member/wiring` and part of `member/keeper` (its report kept under `untracked/scratch/`), the
second over every other test package under `src-go/internal/teamchat`. Text search found the
candidate reads (`.store(`, `log.String()`, `hitCount`, `dialCount`, `writeCount`, `Frames()`,
`keeperJSON()`, `held(`) and the buffered fake channels, but cannot tell which are live. Reading
found what makes a read race or not: the liveness of the delivery loop, that the fake clock runs
timers inside `Advance` (so a read after `quietFor` is ordered), that the store file is written
before the memory (so `until` then a disk read is ordered), that the keeper's timers hand their
work to the loop through `go k.send` (so a log line is not a barrier for what follows it), and
that the CLI writes its ack and exits without waiting for the link to apply it.

Judged and left as ordered: the other store reads of the same test (worlds with no session
facts, or a store of another run), every read after `settled`, `until`, `quietFor` or `poll`,
the keeper's `keeper.json` and report reads (written before the log), the fakeinbox frames, and
the tuwunel and failpoints e2e re-`recv`s (a handed item is not handed out again, so a late ack
cannot change them). `matrix/client_test.go` `TestCloseLeavesNoGoroutine` counts goroutines with
a bounded poll: an adjacent wall-time bound, not this class. Packages the second searcher read
only by search: statusline, hook, kit, ends, guest, the matrix fakes, health, seat, ctl, admin,
apply, rooms, declare, host, notice, element, paths, proto; none has a test that reads state a
goroutine of its code can change after the test's signal, by that search.

## Sweep and count

Scope: every `_test.go` under `src-go/internal/teamchat` (where the engine, the link, the keeper
and their harnesses live; the class needs a background goroutine or process of the code under
test), first-party only, `vendor/` excluded. Four instances:

1. `member/engine/proof_test.go` `TestBeginRunReturnsAnEarlierRunsEntries`: the store read after
   `BeginRun` woke a live delivery loop (the originating one).
2. `member/wiring/e2e_test.go` `TestE2EItems`: `inbox` run right after the `recv` client exited;
   the link applies the ack after the client has gone, so `inbox` may still list the items
   owed. The spec (LN4) asks that items be delivered on the ack, not that the ack be applied
   before the CLI exits.
3. `member/keeper/kp_test.go` `TestLinkRestartsWithBackoff`: the seat lock probed after the log
   line `the link failed: crash-loop`, which `fail()` writes before it closes the locks.
4. `member/keeper/rc_test.go` `TestOwedNotice`: the seat lock probed after the owed timer's log
   line, which it writes before `drop`.

## Fixes

Each examined individually; each test now waits on the effect itself, not on a signal that
comes before it:

1. The test parks the delivery loop at its after-auth failpoint hook (dialled, its entry not yet
   written), reads the store, then releases it; the rest of the test (the delivery of the
   returned items, marked repeat) is unchanged. The hook is a test field, so this runs untagged
   and with busfailpoints.
2. `inbox` is polled with the wiring harness's `eventually` until it lists nothing.
3. and 4. The lock is polled with the keeper harness's `eventually` until it is let go.

No timeout was lengthened, no sleep added, nothing skipped or suppressed.

## Decisions referred to the owner

None. Every instance found is fixed.

## Permanence

No rule, so nothing joins the blocking checks; the catalogue entry in `CLAUDE/GoProjectRules.md`
is the standing record, as for the earlier class there. The fixed tests run in the Go gate as
before.

## Toolchain and detector gaps

The detector has its single-rule harness, identifiers and catalogue; no gap bears on this
remediation. Already recorded by the catalogue: go-qa-ci expects one documentation page per
analyzer, and the catalogue stands in for them.

## The original defect

Fixed in `TestBeginRunReturnsAnEarlierRunsEntries` as above. Proof under load, the fixed
binaries beside the same `go test -race ./internal/teamchat/...` loop:

- the test alone, busfailpoints, `-race`, `-test.count=200 -test.shuffle=on`: 14 runs (six
  each at `-cpu 1` and `-cpu 2`, two at `-cpu 4`), 2,800 iterations, 0 failures, against 19
  failures in about 2,600 iterations of the same form before the fix (17 of them at `-cpu 2`);
- the whole engine package, `-race -test.count=2 -test.cpu=1,2,4 -test.shuffle=on`, four
  seeds with busfailpoints and four untagged: all passed;
- the keeper's two tests `-race -shuffle=on -cpu 1,2,4 -count=20`, and `TestE2EItems`
  `-count=5`: passed.

Coverage: engine 93.3% untagged, 93.4% with busfailpoints; keeper 90.7% and 90.6%; wiring
100% with busfailpoints. golangci-lint over the three packages: 0 issues untagged, with
`tuwunel` and with `busfailpoints`. `./scripts/qa-changed.bash`: passed (docs, go-qa in diff
mode over both modules, teamchat-keeps-off-v1).

## Review

The `defence-before-fix:conformance-reviewer` could not be dispatched: its worktree creation
timed out twice (`git worktree add` past 30 s on the loaded host). The lead's review over the
merged slice covers this branch.
