# n3e: merge of feat/bus-v2-n3 (n3-tagvet, n3-probebound) into feat/bus-v2-n3e

This continues `261023-n3e-merge-n3b.md`. Worktree `/workspace/untracked/worktrees/bus-v2-n3e`,
branch `feat/bus-v2-n3e`, from `c738680f1`. Logs are under `/workspace/untracked/scratch/`
(`n3e-merge-c-*`).

## Commits

| Commit      | What                                                                                       |
| ----------- | ------------------------------------------------------------------------------------------ |
| `530af7be1` | the merge of `e3b6c30b4`; four conflicts, listed in its message                            |
| `4874fb6f2` | n3-probebound's bound tests name their timer by its due (`project_fakeclock_count`)        |
| `614586471` | leakcheck's settle pauses on a clock, and no test shortens it (`project_realtime_bound`)   |
| `8dec4dfab` | `project_realtime_bound`'s doc names `AwaitDue`; its leakcheck instance recorded           |
| `667fddefd` | leakcheck tests read a leak once its goroutine is running in `parked`                      |
| `6b1f8d532` | fakeinbox announce order: the defence, red                                                 |
| `b1fd2a94c` | fakeinbox records a delivery's last frame after the close, and hands on before it records  |
| `8b4586107` | the keeper harness's fake child and pidfd exit before they announce a SIGKILL              |
| `1f129684d` | GoProjectRules: "a fake that announces before it acts" listed as a class no rule can judge |
| `1121a2d1c` | `clock.Fake`'s `AwaitDue` and `Armed` refuse a due two waiters share, naming both          |
| `8177efece` | `project_fakeclock_count`'s catalogue records that the fake now holds an ambiguous due     |

## Part 1: the merge

The four textual conflicts:

- **`CLAUDE/GoProjectRules.md`:** the rule table. All of n3e's four rows are kept, and so is
  n3's `project_realtime_bound` row.
- **`qa/rules/table.go`:** `All()` holds n3e's 14 rules and `RealTimeBound`, 15 in all.
  `rules_test.go`'s count is 15 to match.
- **`guestroot/bound.go`:** n3e's ending is kept (the call and its caller meet under `mu`, with
  no second goroutine). n3's bound on the root's clock (`r.hold(callBound)`) is kept too.

One conflict was not textual. n3-probebound's tests waited with `clock.Fake.BlockUntil(ctx, n)`,
which n3e had removed, so four test files did not compile at the merge commit. The merge commit
says so. `go build` passed untagged and with `busfailpoints`, `tuwunel` and `teamchatrelease`.

### qa-changed findings, each with its own commit after the merge

1. **`project_fakeclock_count`, existing rule (`4874fb6f2`).** These are n3's `BlockUntil` and
   `Waiters()` uses in four places: `guestbus`, `units`, `guestroot`'s `stall.past`, and `lxc`'s
   two probe tests. Each now names its timer with `AwaitDue`/`Armed`. In
   `TestProbesThatHangEndAtTheirOwnBound`, n probes armed their bounds at the same instant. Each
   probe now starts 1 ms after the last one, so every bound has its own due. Each bound is then
   checked and passed in turn.
2. **`project_realtime_bound`, existing rule from n3 (`614586471`).** The finding was
   `leakcheck.go:97`, a `time.Sleep(s.pause)` that the tests shortened to 3 ms.
   - The settle now pauses on a `clock.Clock`. `standard` is 5 s of `clock.System()`, as before.
   - A test that needs a goroutine to end uses `standard`.
   - A test that keeps its goroutine parked takes one reading with no pause.
   - The docs no longer name `BlockUntil` (`8dec4dfab`).
   - The rule finds nothing over the tree, untagged and with `busfailpoints`, `tuwunel` and
     `teamchatrelease`. That covers it and the n3e rules (`fakeclock_count`, `goroutine_end`,
     `rawconn_probe`, `leakcheck_once`). The tagged stage then passed inside qa-changed.

### The first qa-changed run (`n3e-merge-c-qa1.log`) failed two tests

- **leakcheck `TestCheckReportsAGoroutineLeftRunning`.** This was my regression. The single
  reading could come before the goroutine was scheduled: the stack showed `gowrap1` in state
  `[runnable]`, not `leakcheck.parked`. The 3 ms settle used to hide this.
  - Red: 568 of 9000 runs of the three tests that read stacks.
  - Fix (`667fddefd`): `park` starts `parked` and returns only once it is running.
  - Green: 0 of 9000.
  - The only other stack reader, proof's `readParked`, polls until the goroutine is parked, so
    it does not have this shape.
- **inbox `TestAWrongTokenIsRetryAndNothingIsWritten`.** A flake: the branch does not touch inbox
  or fakeinbox. DBF is below.

## DBF: a fake that announces an event before it has done what the event stands for

- **Specification:** method 1.1.0, cached (`DETECTOR-SPEC` 1.0.0, `TOOLING-SPEC` 0.2.0).
- **Toolchain:** `go/analysis` rules in `src-go/qa/rules`, run by go-qa's `analyzers` and
  `project-rules-tagged` stages through `scripts/qa-go-qa.bash`. No manifest declaration was
  found; a Go module has no field for one.
- **Class and hazard:** a test fake wakes waiting tests (it records a frame, so `Await` returns)
  before it has done the action the event stands for. A test that waits on the event and then
  asserts the action's effect races it. Under load it fails on correct code, or it passes before
  the effect has happened.
- **Originating instance:** `fakeinbox.serve` recorded the refused auth frame, and its deferred
  `drop` closed the connection only afterwards. `inbox`'s `WriteUser` therefore sometimes wrote
  into a connection that was still open (got nil, want ErrRetry).
- **Rule:** none could be written, and `GoProjectRules.md` ("Defect classes that cannot be a
  rule", `1f129684d`) says why. Which action an announcement stands for is part of the fake's
  contract. The action can be a defer, a callback or the next statement, and the same shapes put
  an announcement after its action correctly everywhere else.
- **Defence, two deterministic tests (`6b1f8d532`):**
  - `TestTheLastFrameIsRecordedOnceTheConnectionIsClosed` (internal): the connection's `Close`
    reads the frames. It covers six cases.
  - `TestAFrameIsHandedOnBeforeItIsRecorded`.
  - Before the fix both were red in 200 of 200 runs. After it, both were green in 200 of 200.
- **Independent search:** `defence-before-fix:independent-searcher`, dispatched before any fix.
  Its notes are in `untracked/scratch/n3e-merge-c-search-announce.md` (not tracked). It found
  three instances:
  - A1, the originating instance: every record-then-return path in `serve`.
  - A2: `record` woke `Await` before `OnAccepted` handed the frame on (latent).
  - A3: the keeper harness's `child.Signal` and `fakePidfd.Signal` sent SIGKILL before the exit
    (latent).
  - Text search found every announcement and every waiter. Only reading could find the order.
  - It judged everything else it read safe, with a reason for each.
- **Fixes (each examined):**
  - A1 and A2 (`b1fd2a94c`): `serve` now records the last frame after `drop`, and `record`
    hands the frame on before it wakes `Await`.
  - A3 (`8b4586107`): the fakes exit first, then announce the signal.
    `TestAFakeSIGKILLExitsBeforeItIsSeen` was red in 398 of 500 runs before the change. That is
    not deterministic, because the old exit ran just after the read. It was green in 500 of 500
    after.
- **The original test:** passes alone with `-count=50`, and with the failing shuffle seed with
  `-count=20`. Every package that imports fakeinbox passes `-race -shuffle=on -count=2`, and so
  does keeper, untagged and with `busfailpoints`.
- **Referred to you:**
  1. The searcher was unsure about `engine/deliver_test.go:558-559` (`dw.next(); dw.idleAfter(0)`):
     the idle count could already be above 0 from before the write. It is a count of idles, not
     an announcement, so it is outside this class and is not fixed here.
  2. The class has no rule. That is my judgement under the method's "cannot write a rule"
     clause, and it is recorded in the catalogue.
- **Conformance review:** not run, and this report is not reviewed. The class has no rule to
  reproduce red and green through the entry point. The tests' red and green are in the logs
  `n3e-merge-c-announce-red2.log` and `n3e-merge-c-sigkill-red.log`.

## Part 2: an ambiguous due (`1121a2d1c`)

- **The test came first.** `TestAnAmbiguousDueIsRefusedNamingBothWaiters` covers four cases: an
  `AfterFunc` and a `Hold` at one due; a sleeper and a timer at one due; and an unshared due that
  is still named. It did not build before the change (`Armed` returned a bool, and there was no
  `ErrAmbiguousDue`).
- **The change.** `AwaitDue` and `Armed` now answer `clock.ErrAmbiguousDue` for a due that two
  waiters share. The error names where each waiter was armed: the file and line of the first
  caller outside package clock, so a `Hold` or a `Sleep` names its caller's line. `Armed` now
  returns `(bool, error)`. Its four callers were adapted mechanically in the same commit, because
  it would not build otherwise.
- **Every teamchat test that uses `AwaitDue` or `Armed` was run.** That is 14 packages, untagged
  and with `busfailpoints`, under `-race -shuffle=on -count=2`. All passed (logs
  `n3e-merge-c-due-untagged.log` and `-due-busfp.log`).
- **No test waited on a shared due,** so there was no separate fix commit. The one test that arms
  several timers at once (the n-probe test) already uses distinct dues since `4874fb6f2`.
- **The catalogue** (`8177efece`) records that the fake holds this form.

## Part 3: coverage and QA

- **Coverage,** `go test -count=1 -cover` over every Go package touched since `c738680f1` (log
  `n3e-merge-c-cover.log`):
  - `member/keeper` 90.8%
  - `admin/wire` 91.5%
  - `host/lxc` 91.9%
  - `member/link` 92.3%
  - `member/engine` 92.7%
  - `qa/rules` 93.9%
  - `fakeinbox` 94.3%
  - `guestroot` 95.8%
  - `leakcheck` 96.3%
  - `guestbus` 97.9%
  - `clock` 98.4%
  - `units` 98.6%
  - `matrix` 98.9%
- **qa-changed `--base c738680f1`:**
  - The second run, on `1f129684d` (`n3e-merge-c-qa2.log`), passed with exit 0.
  - The final run, on `8177efece` (`n3e-merge-c-qa3.log`), passed with exit 0 over 44 changed
    files.
  - The known `test-wsi-stop-grace.bash` flake did not appear.

## Out of scope

The 8 goroutines of the wider form from the last report.
