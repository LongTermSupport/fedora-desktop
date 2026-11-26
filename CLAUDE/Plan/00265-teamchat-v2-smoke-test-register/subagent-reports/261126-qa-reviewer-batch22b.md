**FIX BEFORE MERGE**

Three of the four findings are closed. The fix for finding (2) adds a new violation of a project rule, and CI will fail on it.

### Blocking

1. **The new lock wait breaks the `project_realtime_bound` rule.** In `src-go/internal/conversation/seatrecord.go:183`, `func (realLockClock) Sleep(d time.Duration) { time.Sleep(d) }` sleeps for a length set at run time. `src-go/qa/rules/table.go` has no `realClocks` row for `internal/conversation`. I built the project-rules driver and ran `go vet -vettool=<driver>` on the changed packages. It printed:
   `internal/conversation/seatrecord.go:183:47: time.Sleep on real time with a bound set at run time (project_realtime_bound): bound it on the injected clock`
   `internal/teams` and the `ccyteamstest` build of `internal/teams/ops` are clean.
   - The fix is a `realClocks` row for `internal/conversation`, `realLockClock.Sleep`, like `qa/internal/qalock`'s `RealClock.Sleep` row. Its reason: the real side of `lockClock`, whose tests replace it whole.
   - Apart from this, the seam is what the rules ask for. The test uses `time.Unix`, not `time.Now`, so `project_testclock` is clean. The bounds are constants, the fake clock's `Sleep` moves its time on, and no test in the package uses `t.Parallel`, so swapping the global is safe.
   - The CI run for `842294f2` was still `in_progress` when I looked.

### Findings from the first review
- **(1) The missing test: closed.** `TestATeamMadeAgainResumesNothingOfAStoppedSessionTakenOff` (`internal/teams/forgetteam_test.go:331`) runs `conversation.ForSeats` before and after `takeStoppedOff`. The REGISTER says plainly that this test was never seen red.
- **(2) The unbounded lock: closed, apart from the blocking item.** It tries the lock without blocking every 100 ms for 30 s, then fails naming the lock. The test covers both writers and checks that the records were left unchanged.
- **(3) `O_NOFOLLOW`: closed, and its claim is correct.** In Go 1.26.11, `os/root_unix.go:85-92` always adds `O_NOFOLLOW` itself and, on ELOOP, follows a link that stays inside the root (`checkSymlink`). So a caller's `O_NOFOLLOW` changes nothing, and comparing the opened file (`f.Stat`) with `root.Lstat` of the name is the right guard. A name swapped between the open and the Lstat fails the same-file check either way. A hard link passes, which is harmless because only flock is taken on it. One small nit: a dangling relative link is followed and its target is created inside the checkout before the open is refused.
- **(4) `stillStopped`: closed.** It runs after the play, inside `confirmDown`, and re-reads `SessionsIn`. That reads the running sessions' desired membership and does not depend on the team's status, so it still works once the team is down (`hot/list.go:351-390`). A session that is running again is left alone and named, the exit is 1, and `ForgetTeam` still runs. A failure to read keeps every session as it is. There is an unavoidable small window between this check and `TakeStoppedOff`, which is acceptable.

### Versions, changelog and REGISTER: clean
- `CCY_VERSION` is 3.124.78, and its comment starts with this batch's changes.
- The Dockerfile LABEL and `REQUIRED_CONTAINER_VERSION` both stay 2.84.89. The diff changes no image source under `files/var/local/claude-yolo/` other than the launcher, so no container bump is needed.
- The 3.124.78 changelog entry and the REGISTER lines for (1) to (4) match the code.

### Gates
- `go test -race -count=1` on `internal/conversation`, `internal/teams` and `internal/teams/hot`: all ok. `internal/teams/ops` with `-tags ccyteamstest`: ok.
- `plan-qa --sweep`: 3 blocking findings, none in 00265.
- The working tree's tree equals `842294f23`'s (`ecc39ae96`), so all of this was judged on the commit itself.
- `qa-all` was not run, as instructed.

Building the driver left one binary at `/workspace/untracked/scratch/qar/driver`. It is untracked scratch and can be deleted.