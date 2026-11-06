# Builder report: #160 status internal, restore.OS.ReadDir sorted, #149 restart flake

Three branches, each from `origin/merge/pr157-onto-f44`; nothing pushed or merged.

## (A) `fix/status-internal-160`: Addresses #160

Cause, by test: every op `Serve` answers waits for the state goroutine (`Engine.do`) under the
request's own deadline (2 s for status, inbox, recv). When the state goroutine does not take the
step in time (CPU starvation), `do` returns the context's error, and status, inbox, recv,
verify, history, say, ping and reply handed it to `internal`/`doneOf`: 1 `internal`. Other paths
checked: `ErrStopped` after `Shutdown` (not a wait, not reachable by load, still 1); a failed
`out.Record`/`Flush` (the client gone, nobody reads it); `Runtime()` failing (logged only).
Existing tests pinned the defect ("status on an ended context = exit 1") and are changed.

Fix: the engine's one failure sink, `internal` (which `doneOf` falls back to), answers a
context's end as 4 `timed-out` ("the link did not answer within the request's deadline"). No
new exit code.

Evidence: `TestARequestWhoseStepTheStateGoroutineDoesNotTakeTimesOut` holds the state goroutine
and ends each op's deadline only once the op is blocked (`leakcheck.Drive`'s step). Planted (the
sink's check off): red on 8 of 9 ops (wait was sound), each `{Exit:1 ... the link failed inside}`; reverted: green. `TestAContextsEndIsNeverInternal` pins the sink.

DbF: class "a request's wait ending reported as an engine failure"; no rule can judge it (a
run-time fact through `do` and wrappers), stated in `GoProjectRules.md` with the sink as guard.
Sweep: every op `serve.go` serves (fixed by the sink); hooks' empty answer and the client's own
`internal`s checked sound.

Gates: engine 92.8%, link 92.4%, client 93.2%, wiring 100% (`-race`); lint 0. `qa-changed --base origin/merge/pr157-onto-f44` at a1e8761d8: PASSED, exit 0. (A first run without `--base`
checked nothing.) Head f9589b1ed.

## (B) `fix/restore-readdir-sorted`

Finding: `project_listing_order` gave 0 findings with `OS.ReadDir` unsorted (it judged only a
test's comparison; the #157 test sorts). Extended: outside tests it reports a function that reads
a directory itself and returns the listing unsorted; a same-package helper whose body sorts
counts as a sort (the first run flagged `bus/seat`'s `names`, sorted through `sortNames`: false
positive, fixed; a cross-package fact was tried and wrongly counted `t.Fatal` as a sort).
Fixture: 4 roots reported (red with the clause off), sorted-at-read, sorted-through-helper (red
with that guard off) and read-without-listing clean. Real tree: exactly one finding,
`restore/fs.go` `OS.ReadDir`, over every gated build; 0 after.

Fix: `OS.ReadDir` sorts by name. `TestOSReadDirListsByName`: 22 names made unsorted, the raw
directory shown unsorted (fatal precondition); red 3 of 3 before, green after. The #157 test's
sort is removed (the order is now promised, so its comparison checks it); the test is kept for
kinds, missing directory and file cases.

Gates: restore 94.8%, rules 94.2%, lint 0. `qa-changed` at deb9eb933: FAILED, exit 1, go only:
the one failing package is `qa/cmd/qa-js` (four real-tool tests need `extensions/node_modules`,
issue 135); "tests failed in qa" only, no "tests failed in .": root module passed. Head e9ba1d659.

## (C) `fix/teamsync-restart-149`: Addresses #149

Cause (refines the issue): the second store write is the one the test's own post causes
(fakehs wakes the long-poll when it stores the event, before writing the post's answer), so the
hook's restart can drop the post and the client's three unpaused tries meet the stopped server.

Forced red: with the post's answer held (new `fakehs.Fault.Hold`) and the hook's restart split
so `Start` waits for the test's end: `resend_test.go:92: matrix: PUT .../send/...: no answer`,
every run. A plain `Restart` in the hook with the hold did not go red (retries reached the
restarted server). Fix: the hook answers the post, signals and waits; the test restarts after its
post is answered. Green 50 runs and 60 at `-cpu 1,2,4`. `TestAHeldRequestIsHandledAndAnsweredOnRelease`
proves Hold.

DbF: new rule `project_fake_restart` (a fake package's `Server.Stop`/`Restart` in a test's
function literal or go statement; literals given to `testing` methods or deferred exempt).
Fixture red/green; real tree exactly one finding (`resend_test.go:73`) with the test as it was,
0 after over every gated build. Others swept: on the test's goroutine, a subtest or a cleanup.

Gates: teamsync 95.2%, fakehs 93.4%, rules ok, lint 0. qa-changed at dda813db9: FAILED, exit 1, go only: the one failing package is qa/cmd/qa-js (issue 135, environmental); "tests failed in qa" only, no "tests failed in .": root module passed; docs and teamchat-keeps-off-v1 passed.

## Not done and findings

- `ErrStopped` is still answered 1 `internal`.
- B 2.2.4's exit-4 row says "wait timed out with nothing"; n3d-D32, verify, history and now every
  served op use 4 for a request deadline. Smallest reading taken; the text is the spec owner's.
- The held-goroutine test lists served ops by hand; a new op is covered by the sink only.
- `fakehs` `TestRestartEndsAHeldSync` waits on real time (untouched). `sender.go` untouched.
