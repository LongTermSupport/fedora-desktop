# Issue 140: the engine send test hang

Branch `fix/engine-send-hang-140`, from `origin/F44`. Addresses #140 (the hang of
`TestSD3_8bAQueuedSendOutlivesA401`; the other two failures of the issue are another agent's).

## Mechanism: proven, and it is not the one hypothesised

The hypothesis (a retry hold armed after the test's `SetHealth`) is refuted by the code: the 401
path of `sendFailed` (sender.go) sets no hold. It calls `refuseWaiting`, sets `h.sendStop = code`
on the state goroutine and returns nil.

The real mechanism: the fake records a PUT (`txns` appended) as `Send` is entered, before the
sender has read the 401 and set `h.sendStop`. The test waited for `len(sent()) == 1` (line 560),
then played the team's life: `SetHealth(Stopped)` (561), `SetHealth(up(startWall+5))` (570). The
second health clears `sendStop` only on a transition out of stop or down (`SetHealth`,
storewrite.go), and it ran BEFORE the sender's late `h.sendStop = token`. The stop then stands
over a team whose health is up, `mayPost` is false for ever (`hl.Stopped = h.sendStop` in
`shown`), no later health transition exists in the test, and the sender sits in its `select`:
exactly CI's trace (test goroutine `[runnable]` in `until`, `sendLoop` idle in `select`).

Deterministic proof, test only: the fake's `Send` waits on a gate after recording its txn
(`fakeNotices.holdAnswers`), the test runs lines 561 to 570 verbatim, then releases the gate.

- Red: `go test -race -count=1 -timeout 60s -run TestProof140TheSenderIsSlow ./internal/teamchat/member/engine/`
  failed with `panic: test timed out after 1m0s`, goroutine `[runnable]` in
  `(*world).until` called from the test, `sendLoop` in `[select]`: CI's trace.
- State after the late 401 (same schedule, no spin): `sendStop` before the answer is read `""`,
  after `"token"`; shown to status `"token"` with the team's health up; 1 send left; 1 PUT, never
  retried.

The proof test is not committed (it hangs by design, and its second form would pin a hazard as
behaviour). Its source is kept in the worktree's untracked scratch and is the test body of
`TestSD3_8bAQueuedSendOutlivesA401` with `release := f.notices.holdAnswers()` before the first
`SetHealth(up)` and `release()` after the second.

## Production: a latent hazard, not changed (the coordinator's call)

In production the sender's 401 is handled within microseconds, so the CI hang is a test race.
The same ordering exists with a wider window: `RecoverSends` clears `sendStop`, then waits for
`postMu`, which the sender holds across its PUT. A PUT made under the old token that returns 401
after the recovery cleared the stop sets `sendStop` after it: a stale stop over a team on a new
token, cleared only by the next health transition out of stop or down. The window is one PUT
round trip across a token reissue. Not changed (off limits); a fix would make the sender set
the stop only when the token it posted under is still the team's.

## Class and detector

Class (a), a wait that spins and cannot fail: `for !cond() { runtime.Gosched() }`. Its only exit is
the condition; the guards found in the tree (`t.Context().Err() != nil`, three of them) never
fire, because `t.Context()` ends only when the test does. Detector written first and proven:
`project_test_spin` (`src-go/qa/rules/testspin.go`, fixture `testdata/src/fix/internal/spin`,
catalogue entry in `CLAUDE/GoProjectRules.md`). `analysistest` green over the fixture (12 `want`
lines, 7 clean neighbours); red on the real tree before any fix: 9 findings in
`./internal/teamchat/...`.

Sweep (every `runtime.Gosched()` in `src-go`: all nine are in teamchat's tests):
engine `world.until`, `dworld.settle`, `recv_test.go` (two), `health` `TestAtMostFourTeamsAtOnce`,
`proof/watch_test.go`, `link` `until`, `wiring_test.go`, `teamsync` `waitLog`; plus `waitFor`
(a spin through round trips, not caught by the rule).

Failure path chosen, no wall clock and no count: `leakcheck.Idle` (every other goroutine is
blocked on a channel, select, lock or wait group, read from a stop-the-world stack dump, so one
instant) and `leakcheck.Poll(tb, cond)`, which fails the test when Idle holds and `cond` still
does not (cond is read again after the Idle reading, so no goroutine ran between). It cannot
spin to the package timeout when the world is closed: a pending timer, a running or runnable
goroutine, a system call or the network poller keeps Idle false. Limit, stated: a world with a
goroutine in the network poller (`link`, `teamsync`, `wiring` listeners) is never idle, so
there `Poll` waits as before; the failure path is real for the engine, `health` and `proof`.
Seven loops became `Poll`; the `recv_test.go` advance-until-answered loop became
`awaitDue(waitRetry)`, `Advance`, then a receive.

## Class (b) and the fix of the test

Class (b), a test acting on the engine after a fake recorded a call without waiting for the
engine's follow-up: no rule can see which call has a follow-up (the engine's meaning), so it is
fixed conventionally. The four `waitFor(sent() == 1)` sites: only line 560 acted on health
afterwards; 816, 932 wait for a hold or a waiting op, 855 waits for the log line written after
the stop is set. `TestSD3_8bAQueuedSendOutlivesA401` now waits for `sendStop == token` on the
state goroutine before `SetHealth`. `TestAPutIsRecordedBeforeItsAnswerIsHandled` holds the
answer at the gate and asserts the stop is not yet set, then releases and sees it: the
committed deterministic form of the mechanism.

## Results

Run in this container (a host at load average near 90, so wall times are long):

- Engine package, CI's flags: `go test -race -shuffle=on -count=2 -tags busfailpoints ./internal/teamchat/member/engine/`:
  first run FAILED in `TestWaitRetriesARestoredItemThatCouldNotBeFetched` (my first conversion
  used `AwaitDue(waitRetry)`, which the fake refuses as ambiguous: the retry hold and
  status.json's pacing timer are both due in 1 s); fixed (advance inside `leakcheck.Poll`); rerun
  `ok 47.222s`.
- `-cpu 1 -count 500 -run TestSD3_8bAQueuedSendOutlivesA401`: default tags `ok 4.751s`,
  `-tags busfailpoints` `ok 6.927s`. `-race -count 100` of
  `TestAPutIsRecordedBeforeItsAnswerIsHandled`: `ok`.
- The other touched packages, `-race -count=1`: `leakcheck`, `health`, `link`, `proof`,
  `teamsync`, `wiring` (211 s) all `ok`.
- Rule: `go test ./rules/` in `src-go/qa` `ok` (the analysistest of the fixture and
  `TestAllIsValid` now 24 rules); the driver over `./internal/teamchat/...`: red 9 findings
  before the sweep, 0 after.
- `golangci-lint run` default over the seven touched `src-go` packages: 0 issues; `--build-tags busfailpoints` over `engine`, `link`, `wiring`: 0 issues; `golangci-lint run ./...` in
  `src-go/qa`: 0 issues (the first `qa-changed` went red on three `goconst` findings there, now
  fixed); `go vet` over the touched packages: clean.
- `scripts/qa-changed.bash`, first run: docs passed, `teamchat-keeps-off-v1` passed, go FAILED
  on three `goconst` findings in the nested `qa` module (fixed above). The rerun was queued
  behind other agents' QA runs on the shared QA lock (waiting 8 minutes and more at the time of
  writing) and had not finished when this report was committed; the coordinator should run
  `./scripts/qa-changed.bash` once more on the branch before the merge. Not run: the js suites
  (no Go-change path reaches them).

## Not done

- The `link`, `teamsync` and `wiring` `Poll` sites keep the spin where a listener sits in the
  network poller (above): the rule is satisfied, the failure path is nominal there.
- No production change; the late-stop window is the coordinator's call.
- The 22 `until` sites of `link` were not converted to events.
- Not run: `scripts/qa-all.bash` (forbidden here), the tagged `tuwunel` build.
