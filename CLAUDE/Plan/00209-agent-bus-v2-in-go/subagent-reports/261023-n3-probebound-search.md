# Search report: real-time bounds raced against asserted steps (teamchat, teams)

Scope was `src-go/internal/teamchat/...` and `src-go/internal/teams/...`. I did not read or run anything under `src-go/qa/rules`. I read no file outside the two trees apart from the checkout's CLAUDE.md files, which the harness attached.

## Searches run

I ran these as greps over both trees, tests and non-tests:
1. `context.WithTimeout`, `context.WithDeadline`, `time.After(`, `time.NewTimer`, `time.AfterFunc`, `time.Sleep`, `time.NewTicker` and `time.Tick`. This gave 48 hits outside `teamchat/clock`.
2. Duration literals in milliseconds or microseconds, and fields named Bound, Timeout, Deadline, Grace, Delay, Wait, Interval, Limit, TTL or Poll set to a duration.
3. Identifier names: `jobWait`, `flushWait`, `ghTimeout`, `reachTimeout`, `ReadTimeout`, `ChildWait`, `GuestWait`, `CallWait`, `StartLimit`, `Bound`, `bound`, `.wait`, `timedOut`, `ProbeBound`, `rec.count`.

## Files read in full

- `host/lxc/guestroot/bound.go`
- `host/lxc/guestroot/bound_test.go`

## Files read in part

- `host/lxc/probe_test.go` lines 330-410
- `matrix/client_test.go` lines 465-535 and 635-675
- `member/link/server_test.go` lines 235-295
- `member/wiring/e2e_world_test.go` lines 350-390 and 580-615
- `member/keeper/osproc_test.go` lines 60-170
- `teams/board/fetch_test.go` lines 35-90
- `health/health_test.go` lines 170-210 and 990-1015
- `member/forge/token_test.go` lines 148-190
- `admin/execchild_test.go` lines 234-260
- `host/lxc/faults_test.go` lines 410-455
- `teams/lxcadd/checkout_test.go` lines 215-285
- `units/units_test.go` lines 360-390
- `host/lxc/guestroot/sys_test.go` lines 40-140

I did not read `matrix/client.go`, `host/lxc/probe.go`, `member/engine/teams.go`, `member/guest/check.go` or `units/units.go` beyond the grep line that matched. Their class (C) status below rests on the grep line plus how the tests use them.

## (A) Assertion depends on work reaching a step before a real-time bound fires

All paths below are under `src-go/internal/teamchat/`.

**A1. `host/lxc/probe_test.go:360-383`, `TestProbesThatHangEndAtTheirOwnBound`.** This is the originating instance. Confident.
- Bound: `g.Bound = 20ms` (line 366), applied by `probe.go:48`.
- Step asserted: `len(entered) == n` (line 379), meaning every probe reached the fake root's blocking step.
- Why: the deadline runs from the probe's start, so under load it fires before a probe reaches the step. The `errors.Is(DeadlineExceeded)` loop at lines 374-378 passes either way.

**A2. `host/lxc/guestroot/bound_test.go:221-244`, `TestAStallIsPaidOncePerMemo`.** Confident it races; the failure needs heavy load.
- Bound: `r.bound = 20ms` (from `stalledRoot`, line 30), applied by `bound.go:80`, `time.NewTimer(r.bound)`.
- Step asserted: `before := rec.count()` (line 229) is taken after the stalled read returns. `n := rec.count() - before; n != 0` (line 242) claims later calls made no syscalls.
- Why: `bounded()` runs `op` on its own goroutine and abandons it when the timer fires. If the timer fires before that goroutine reaches the recorder's `step()` (`sys_test.go` lines 80-90, which counts before it stalls), the goroutine's first syscalls are counted after `before`. That fails the "no system calls" assertion as a flake.

**A3. `host/lxc/guestroot/bound_test.go:52-86` (`TestAStalledReadEndsAtItsBound`) and `:91-135` (`TestAStalledWriteEndsAtItsBound`).** Unsure; weaker form.
- Bound: 20ms, the same field.
- Step asserted: after `release()` / `close(stall)`, `waitClosed(rootFD)` and `rec.leaked()` being empty. The tests also check that the root was still held at Close ("released under a read in flight", lines 75-77 and 127-129).
- Why: the first ErrStalled assertion needs only the timer to fire, so it is safe. But the later "root held by a read in flight" and "closes" assertions assume the abandoned goroutine entered `enter()` and the stalled openat2. With a 20ms bound under load the goroutine may not have reached the stall before Close. The verdict depends on how `bounded` orders `enter` and `go`; `enter` is called before the goroutine starts, so the in-flight count is probably safe. That is why I mark it unsure.

**A4. `host/lxc/guestroot/bound_test.go:137-162`, `TestALateDialIsClosed`.** Unsure; inverted form.
- Bound: 20ms.
- Step asserted: after `close(release)`, the accepted connection reads EOF ("late connection was left open").
- Why: if the 20ms fires before the dial goroutine reaches the stalled step, the dial is dialled after release, the test passes, and the `late` path is never exercised. The accept and read assertion has no way to tell.

**A5. `member/link/server_test.go:277-283`, `TestCloseClosesTheConnectionBeforeTheCancel`.** Confident it is inverted.
- Bound: `time.After(100ms)`.
- Step asserted: the negative assertion "no cancel came before the connection closed". The test holds `srv.mu` and runs `srv.close()` in a goroutine.
- Why: if `close()` has not started or reached its cancel step within 100ms (load), the select takes the timeout and passes without ever exercising the ordering it vouches for. `until(requests()==1)` guarantees the request arrived, not that `close()` was scheduled. The package otherwise uses a fake clock (`w.clk`).

## (B) Real-time safety net only, no race with an asserted step

| File | Instances | Notes |
|---|---|---|
| `health/health_test.go` | 1 | `time.After(10s)` in the `guests.Probe` fake; errors only if the probes run serially |
| `matrix/fakehs/http_test.go`, `matrix/fakehs/sync_test.go` | 3 | `time.After(10s)` |
| `member/link/server_test.go` | 1 | `:255` `time.After(5s)` |
| `member/wiring/e2e_world_test.go` | 2 | `eventually`/`within` and `session.wait`, bound `settle` |
| `member/keeper/osproc_test.go` | 3 | `bound` constant; `fd.Exited()` waits |
| `member/keeper/harness_test.go` | 1 | polling barrier, `pollGap` 5ms capped by `bound` |
| `teams/board/fetch_test.go` | 1 | `stuck` is 20s; the test fails if `timedOut` |
| Real-Tuwunel tests, `tuwunelTestLimit` / `StartLimit` | 7 files | build tag `tuwunel` |
| `matrix/client_test.go` | 3 | see below |
| `matrix/sync_test.go` | 1 | `:281` 1ms sleep poll |
| `matrix/fakehs/http_test.go` | 1 | `:196` 1ms sleep poll |

- `matrix/client_test.go:474-492`, `TestRequestTimeLimit`, sets `m.limit = 20ms`. It already removes the arrival assertion (comment at lines 483-488) and asserts only the timeout outcome. It is the fixed shape of A1.
- `matrix/client_test.go:519` is a 1ms context followed by `<-ctx.Done()`. It is deterministic.
- `matrix/client_test.go:650-660`, `TestCloseLeavesNoGoroutine`, polls with 5ms sleeps, capped at 400 iterations (about 2s).
- `teams/lxcadd/checkout_test.go:230,260` set `ReadTimeout = 1` (1ns). The deadline has already passed, and the test asserts only the "took longer" outcome, not that the hang was reached.
- `units/units_test.go:370` sets `jobWait = 0`. It is already expired and no step is asserted.
- `admin/execchild_test.go:252` and `host/lxc/lxccmd/guest_test.go:87` use `WithDeadline(time.Time{})`, an already-past deadline injected through a seam.
- `member/forge/token_test.go:152` (the 10s gh bound) uses `clock.Fake`. The `host/lxc/faults_test.go` stall tests use a stall log, not a timer.

## (C) Production code with a real-time bound that forces its tests into (A)

- **`host/lxc/probe.go:48`, `Guests.Probe`.** `context.WithTimeout(ctx, g.bound())` runs on real time. A bound field `Bound` exists (`ProbeBound` is 5s), but no clock is injected. It forces A1. The `health` package fakes the whole probe, so only A1 is affected.
- **`host/lxc/guestroot/bound.go:80`, `bounded[T]`.** `time.NewTimer(r.bound)` on real time (`callBound` is 10s). Tests can set only the `r.bound` field, to 20ms. It forces A2, A3 and A4.
- **`matrix/client.go:78,100`.** Timer and AfterFunc at lines 78 and 100 sit inside what looks like a clock adapter. `m.limit` is a real-time field, which forces the 20ms use in `TestRequestTimeLimit`. That test is already safe. Unsure whether the adapter takes `Config.Clock` or only the rate limiter does.
- **Real-time bounds the tests keep out of the race (no A forced).**
  - Fully injectable: `admin/execchild.go:50` (`c.bound` seam), `units/units.go:467` (`jobWait` field).
  - Real-time const, only the const value asserted: `host/lxc/lxccmd/guest.go:104` (`GuestWait`), `host/lxc/guestbus/guestbus.go:179` (`CallWait`), `member/guest/check.go:142,161` (`ghTimeout`, `reachTimeout`).
  - Real-time const, no test sets it: `member/engine/teams.go:291` (`flushWait` 2s).
  - Real-time field, tests set 1ns: `teams/lxcadd` and `teams/roles` `ReadTimeout`.

## Technique attribution

**Found only by reading (a pattern search would miss these).**
- A2: the race is in the count taken after the stall, not in any timer call in the test.
- A3 and A4: the consequence is in the later `leaked` / accept assertions. The `20 * time.Millisecond` literal is in the shared helper `stalledRoot`, so a per-test grep misses the connection.
- A5: the bound is `time.After(100ms)` inside a negative `select`, so the assertion is "nothing happened". Grep sees a `time.After`; only reading shows it is an assertion bound and not a safety net.
- The classification of every (B) entry between safety net and race needed reading, for example `stuck`, `eventually`, `jobWait = 0`.

**Found by text search, and reading could not have checked them all.** All the listed grep hits were found by text search. Reading covered only the ones I opened. The `tuwunel`-tagged tests and a few `Sleep` and `After` hits in files I did not open I classified (B) from the match line and its constant name alone.

## Count

- (A): 5 instances (A1 and A5 confident, A2 confident of the race, A3 and A4 unsure).
- (B): about 20 instances in the files above.
- (C): 3 production sites that force (A) tests (`probe.go:48`, `guestroot/bound.go:80`, `matrix/client.go` timer).