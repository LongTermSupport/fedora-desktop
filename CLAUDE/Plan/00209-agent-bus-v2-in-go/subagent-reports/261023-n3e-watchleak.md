# n3e-watchleak: the proof watcher "leak" in TestLiveNeedsEveryFact

Plan 00209 N3, slice n3e. Found by QA over the staged merge of `feat/bus-v2-n3e-10` into
`feat/bus-v2-n3e` (the busfailpoints test stage, shuffle seed 1792730410753262461). The work is in
the working tree of that merge, staged, not committed (the lead commits the merge).

## Finding: the production code was right; the test's second leak check judged too early

The reported goroutine is `proof.(*watcher).run` blocked in its read of the inotify fd. Every
path of the engine closes its watch and waits for it, and did before this work:

- Stop: `proofLoop` defers `closeWatch(w)`; `Run` waits for the proof loop (`loop.Wait`) before
  it returns; the world's cleanup waits for `Run`.
- Replacement: `path != watched` closes the old watch before opening the new one.
- Failure: `Watch`'s own error paths close the fd; a watch that fails later closes its
  `Changes`, and the loop then closes it (`!open`).
- `watcher.Close` closes the `*os.File` and then receives from `done`, which `run` closes by
  defer, so Close returns only once `run` has. The fd is made with `IN_NONBLOCK`, so
  `os.NewFile` registers it with the runtime poller and `Close` unblocks a pending `Read` with
  `os.ErrClosed`, which `run` returns on. Proven by the new
  `TestCloseUnblocksTheReadAndWaitsForTheWatcher` (it waits until the goroutine is parked
  `[IO wait]` in `run`, closes, and finds `done` already closed), under `leakcheck`.

The cause is in the test: `TestLiveNeedsEveryFact` built two worlds, `dw` and `wait`, and each
world constructor (`newWorldWith`) calls `leakcheck.Check(t)`. Cleanups run last in, first out:
`wait`'s engine stop, then `wait`'s Check, then `dw`'s engine stop, then `dw`'s Check. `wait`'s
Check judged while `dw`'s engine was still running; `dw`'s proof loop opens its transcript watch
asynchronously after `SetSession(Transcript)`, so by the shuffle order and the scheduler that
watch could be opened after `wait`'s snapshot, and was then reported as a goroutine "started by
the test still running". Reproduced: `-race -tags busfailpoints -shuffle=1792730410753262461 -count=3`, 2 of 3 runs failed (`untracked/scratch/watchleak-repro1.log`).

## Why project_goroutine_end did not flag it

It judges whether a goroutine signals its end: `watcher.run` defers `close(w.done)`, so it does,
and its owner waits for it. There was no production instance to flag; the rule has no gap here.
The defect class is in how the tests use `leakcheck`, which no rule covered.

## Defence Before Fix report

### Specification followed

Defence Before Fix method specification 1.1.0, cached copy (refresh script: SPEC.md cached
1.1.0, DETECTOR-SPEC.md cached 1.0.0, TOOLING-SPEC.md cached 0.2.0, project-prompt.md cached).

### Toolchain

No manifest declaration of the method (src-go has none; not added). The detector is the
project's `go/analysis` rules in `src-go/qa/rules`, run by go-qa (go-qa-ci) as go vet's tool
through `scripts/qa-go-qa.bash` (`qa-all`, `qa-changed`). Single-rule harness: `analysistest`
(`go test ./rules -run TestLeakcheckOnce`) and `go vet -vettool=<driver> -project_leakcheck_once ./...`. Identifier resolution: `<driver> help project_leakcheck_once` prints the doc; the
catalogue `CLAUDE/GoProjectRules.md` stands in for go-qa-ci's per-rule pages (an existing gap,
recorded there). No listing command enumerates the defences beyond `rules.All()` and that
catalogue. Calibration assumed: blocking, no exceptions (none recorded).

### Defect and class

Instance: `src-go/internal/teamchat/member/engine/deliver_test.go:289` (as it stood),
`TestLiveNeedsEveryFact`'s second `newDeliveryWorld(t, ...)`.
Class: a test on which `leakcheck.Check` is registered more than once, directly or through
helpers.
Hazard: the later Check judges before the cleanups registered between the two have run, so the
goroutines of a world those cleanups stop and wait for are reported as a leak: a seed- and
timing-dependent red that blames correct production code and invites a wrong fix.

### Rule

`project_leakcheck_once`, `src-go/qa/rules/leakcheckonce.go`, registered in `rules.All()`.
Message: `leakcheck.Check registered again on <t>, through <callee>: the later check judges before the cleanups registered between them run; register it once per test, first (build a second world from the first, or use t.Run)`. Documentation: the analyzer's Doc and
`CLAUDE/GoProjectRules.md` § project_leakcheck_once.
Width: a call registers the check when it is a `leakcheck` package's `Check` or a same-package
function or method that passes one of its parameters on to one (fixpoint over the package);
within one body (each function literal its own) the second such call on the same identifier is
flagged, and any such call in a `for` or `range` loop. Test and non-test files alike, both
modules. Excluded as hazard-free: a `t.Run` subtest's own `t` (a different test), and a second
world built without Check (`another`). Not caught: a test carried in a struct field or a
function value.
Next wider rule considered, not built: every cleanup that stops goroutines registered on a test
before its first Check (same hazard, other shape). Whether a cleanup owns goroutines is not a
shape one package shows; the independent search found no instance. Referred to the owner below.

### Proof

Fixture `src-go/qa/rules/testdata/src/fix/internal/twicecheck` (kept as the rule's test,
`TestLeakcheckOnce`): the originating two-worlds-through-a-wrapper shape, two different
wrappers, the test on a second parameter, `Check` twice, a world in a loop, two in a function
literal, a helper that checks its `testing.TB` twice, all flagged; one world plus `another`,
`t.Run` subtests in a loop, two different tests, clean. `go test ./rules` ok
(`untracked/scratch/watchleak-rule1.log`).
Red on the real tree before any fix: 6 findings, identical untagged, `busfailpoints` and
`tuwunel`, none in the nested `qa` module (`untracked/scratch/watchleak-red-tree.log`).
No red commit: the lead's brief forbids commits in this worktree (the merge is staged), so the
red run survives only as that log. The conformance reviewer reproduced it (6 findings, untagged
and `busfailpoints`) by reverting the instance hunks in a scratch copy, and holds that this does
not meet clause 3.3 Part A. Referred to the lead below: the red commit has to come after the
merge commit (the HEAD test files do not compile against the merged engine).

### Independent search

Dispatched to `defence-before-fix:independent-searcher` before the rule existed, kept out of
`src-go/qa/rules/`. It found `Check` called directly only in `engine.newWorldWith` and
`link.newKeeperWorld` and five tests reaching it twice on one `t`: `TestLiveNeedsEveryFact`,
`TestAnItemAwaitingProofIsNotPending`, `TestMissingSurvivesARestart`,
`TestBeginRunReturnsAnEarlierRunsEntries` (twice), and `link` `TestRemoveAnswersThenLeaves` (a
world in a plain loop); none of the other shape. Text search found the first four; reading was
needed for the loop (a per-function count of one call hides it) and to tell `t.Run` loops from
plain ones. The rule's 6 findings are exactly these; nothing to widen. Report:
`untracked/scratch/watchleak-search.md` and the agent's saved reply.

### Sweep and count

Scope: all first-party Go in `src-go` and the nested `src-go/qa` module, test files included,
untagged, `busfailpoints` and `tuwunel`; vendored code excluded (go vet does not analyse
`vendor/`). Count: 6 instances in 5 tests. No narrowing.

### Fixes

All 6 examined individually (each test read, its second world's use checked); none received
the change by pattern only.

- `engine`: `newWorldWith` now registers Check and calls `buildWorld`; `newDeliveryWorld`
  registers Check and calls `buildDeliveryWorld`; `dworld.another(delivery)` and
  `pworld.another()` build a second world of the same test without Check (the method exists only
  on a world, so the test's check is already registered). `TestLiveNeedsEveryFact`,
  `TestAnItemAwaitingProofIsNotPending`, `TestMissingSurvivesARestart` and
  `TestBeginRunReturnsAnEarlierRunsEntries` (2) use them.
- `link`: `TestRemoveAnswersThenLeaves` runs each farewell in a `t.Run` subtest (short names:
  the link socket path holds the test name and must stay under 100 bytes).
  Green: 0 findings in all three builds and the `qa` module
  (`untracked/scratch/watchleak-green-tree.log`). No suppression; the hazard is gone (one Check a
  test, registered before every world).

### Decisions referred to the owner

- Open, awaiting the owner: the next wider rule (a goroutine-owning cleanup registered before
  the test's first Check) is not built, because it is hard to build without false positives
  (which cleanups own goroutines is a cross-package question), not because the hazard cannot
  arise there. Fixed 0, remaining 0 known (the independent search found none). Building it needs
  a notion of goroutine-owning cleanups across packages; meanwhile the run-time half
  (`leakcheck`) reports such a case as a red.
- Open, awaiting the owner: the rule does not follow a test carried in a struct field (a Check
  on `dw.t` inside a method such as `another` would not be flagged, since the call site names
  `dw`, not `t`). Following it needs an alias of the world to its test. No instance exists (the
  search found `Check` called directly only in `newWorldWith`, `newDeliveryWorld` and
  `newKeeperWorld`, each on a parameter).
- For the lead: the red proof is not a commit (see Proof). To meet clause 3.3, commit the rule,
  fixture, `table.go` and `rules_test.go` after the merge commit with the six instances still in
  place (the analyzers stage red), then the instance fixes, and keep both reachable. The
  watch-path tests (`TestEveryTranscriptWatchIsClosed`, the proof `TestMain` and Close test)
  are separate from the defence and can go in a commit of their own.
- Found, not fixed (outside this brief): `tests/speech_to_text/test_server_file_job.py`
  `test_a_running_job_is_marked_for_the_panel_and_unmarked_when_it_ends` failed once in
  `qa-changed` (suite `test-wsi-stop-grace.bash`) and passed six times alone. Cause:
  `wsi-stream-server` `FileJob._run` publishes the job's end state under the lock before it
  unlinks `FILE_JOB_MARKER`, so a client that sees "failed" can still find the marker (the panel
  can show a finished job for a moment). Class: a completion published before the cleanup it
  implies. Fix: unlink the marker before setting the state.
- go-qa's analyzers stage runs the project rules untagged only (existing gap, recorded under
  project_rawconn_probe); this rule finds the same in all builds today.

### Permanence

The rule is in `rules.All()` (TestAllIsValid now expects 12), so go-qa's analyzers stage runs it
blocking in `qa-all.bash`, `qa-changed.bash` and CI. Entry-point run: see QA below.

### Toolchain and detector gaps

No per-rule documentation page under go-qa-ci's `docs_dir` (the catalogue stands in, as for every
rule here); no listing command beyond the catalogue. Not worked around.

### The original defect

Fixed by the instance fix: `TestLiveNeedsEveryFact` builds its wait-mode world with
`dw.another`. Reproducing test: the test itself under the seed, red before
(`watchleak-repro1.log`, 2 of 3 failed), green after (below). The red is timing-dependent: the
conformance reviewer's reverted copy passed the same seeded run, so the deterministic red is the
rule's. Also added:

- `engine` `TestEveryTranscriptWatchIsClosed`: a recording watch factory; a moved transcript
  closes the old watch, a failed watch (Changes closed) is closed, and nothing is open after the
  engine stops; a double Close is an error. Mutation proof: without the close on replacement it
  never settles (timed out, `watchleak-mutation-replace.log`); without the deferred close on stop
  it fails "watches still open after the engine stopped" (`watchleak-mutation-stop.log`).
- `proof`: `TestMain` runs `leakcheck.Tests`; `TestCloseUnblocksTheReadAndWaitsForTheWatcher`.
- `leakcheck.Check`'s doc says once a test, and names the rule.

## QA

- `go test -race -shuffle=on -count=5 -cpu 1,4 -cover` over `engine`, `proof`, `link`, untagged
  and `busfailpoints`: all ok; coverage engine 93.8% (93.9% busfailpoints), proof 90.7%, link
  92.5%. The seed 1792730410753262461, `-count=5 -cpu 1,4`, both builds: ok
  (`untracked/scratch/watchleak-prove.log`).
- golangci-lint over `internal/teamchat/...` untagged, `tuwunel`, `busfailpoints`: 0 issues; the
  `qa` module's rules and driver: 0 issues; `go test ./rules ./driver` ok
  (`untracked/scratch/watchleak-lint.log`).
- `./scripts/qa-changed.bash --base feae02a6c`, first run: red on the go check, one finding of
  mine (`gqaci_map_range_order` in `leakcheckonce.go`, a map ranged into a slice; fixed by
  ranging the sorted keys). Second run: the go check (go-qa, both modules) green; red only on
  the Python flake above (`watchleak-qa-changed2.log`).

## Review

`defence-before-fix:conformance-reviewer` (saved reply under `untracked/agent-reports/auto/`,
`...conformance-reviewer-a332492e9109c2f20.md`): red (6) and green (0, all builds) reproduced,
identifier resolves, no suppression. Its findings and what was done: no red commit (referred to
the lead, above); the next wider rule and the struct-field gap recorded as open owner decisions
(above); the per-instance examination recorded; the watch-path tests to be committed apart.

## Files

- `src-go/qa/rules/leakcheckonce.go` (new rule), `table.go` (registered), `rules_test.go`
  (TestLeakcheckOnce, 12 rules)
- `src-go/qa/rules/testdata/src/fix/internal/twicecheck/` (fixture),
  `.../fix/internal/leakcheck/leakcheck.go` (stub gains `Check`)
- `src-go/internal/teamchat/member/engine/engine_test.go`, `deliver_test.go`, `proof_test.go`
- `src-go/internal/teamchat/member/link/link_test.go`
- `src-go/internal/teamchat/member/proof/watch_test.go`
- `src-go/internal/teamchat/leakcheck/leakcheck.go` (doc only)
- `CLAUDE/GoProjectRules.md` (table row and section)
