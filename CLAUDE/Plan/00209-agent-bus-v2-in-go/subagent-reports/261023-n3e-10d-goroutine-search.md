# n3e-10d: every goroutine start in first-party production Go

Searcher's record. Independent of the detector: I did not open qa/rules, qa/driver, go-qa.toml or GoProjectRules.md, and ran no detector. The dispatch did not contain the rule. Scope: src-go (module and nested qa module), first-party non-_test.go files, vendor and testdata excluded. Paths below are relative to src-go/.

Verdict key:
- CARRIES: the stop can be missed on some path, or a send can be stranded, and nothing waits.
- UNWAITED, stop guaranteed: in the class (no owner waits for it) but the stop is reached on every path found. The hazard is latent only.
- UNSURE: depends on a caller, or on a library behaviour I did not verify.
- DOES NOT: an owner stops it and waits for its end signal on every path found.

## 1. Instances that CARRY the hazard

1. internal/teamchat/member/link/link.go:167 `go l.read()` (the originating one). CONFIRMED.
   - Stop: `close(l.quit)`, only in `teardown`. `teardown` is deferred at :182, after awaitLaunch (:171) and build (:175) succeeded.
   - Early returns at :172-173 and :175-176 leave `read` running. `closeControl` (deferred :168) makes Recv return an error. `read` then blocks on `l.msgs <- received{...}`: `msgs` is unbuffered, nobody receives after run returns, and `quit` is never closed.
   - End signal: none. `read` is not in `l.bg`. Nothing waits on any path, including the success path (teardown waits `bg`, not `read`).
   - Found by: text (`go l.read()`) and reading.
2. internal/ui/lines.go:185 `go readLines(bufio.NewReader(l.In), l.want, l.replies)`. CARRIES, confident.
   - Stop: `want` is closed nowhere, and `Lines` has no Close. The goroutine ends only on a read error or EOF. Otherwise it blocks on `for range want` for the life of the process (or on the stdin read). `replies` has capacity 1.
   - End signal: none. Nobody waits.
   - Found by: text, then reading.

## 2. UNSURE

3. internal/teamchat/member/engine/teams.go:177 team goroutine (`tm.Run(runCtx)`).
   - Stop: `runCtx` is built from `context.WithoutCancel(ctx)`. It is cancelled only by Remove or Shutdown (`h.cancel`).
   - End signal: deferred `close(h.stopped)`. The owner waits in `stop` only up to `stopWait`, then logs and moves on (an accepted abandonment).
   - If the engine's `Run` ends without Shutdown being called, nothing cancels the team goroutine. Whether the link always calls Shutdown on every path is not established by this search; I read only the success path through finish.
4. internal/teamchat/member/keeper/osproc.go:58 `go p.wait()` (pidfd watcher).
   - Stop: process exit, or `Close()` of the file. `Close()` does not wait for `wait`.
   - `exited` is closed only when the read succeeds. If the file is closed first, `wait` returns without closing `exited`, so nothing signals the end.
   - The stop depends on the keeper calling Close. I did not verify every keeper path.
5. internal/teamchat/member/keeper/osproc.go:144 `go w.read(w.ch)` (inotify reader).
   - Stop: `Close()` closes `done` and the file. Close does not wait for `read`.
   - The end signal is the deferred `close(ch)`, which nobody awaits.
   - Whether the watcher from `newInotify` (keeper.go:179) is closed on every keeper path is unverified.
6. internal/execx/cleanup.go:80 `SignalContext` goroutine.
   - Stop: only the returned `stop` func (`close(stopped)`). If the caller never calls it, the goroutine blocks on `select` for the life of the process.
   - Nothing waits for its end.
   - It has no non-test callers in src-go (grep of `SignalContext(`), so it is currently unused in production: a latent instance of the class.
7. internal/execx/session.go:64 `go func(){ run.exited <- cmd.Wait() }()`.
   - Awaited on the normal and ctx-cancel paths via `run.await`.
   - On the grace-timeout path (:75), `waiting` may still be true and `exited` is not awaited. The send is buffered (cap 1), so it cannot block.
   - It ends when the child dies. It is stranded only while the child outlives SIGKILL of its group.
   - :65 (`copyAll`) is awaited via `<-run.drained` on the abandon path and `await` otherwise: DOES NOT. :66 (`feed`) is awaited by `stopFeeding` on every return: DOES NOT.

## 3. UNWAITED, stop guaranteed (in the class by definition, hazard latent)

8. cmd/pingbus-status-line/main.go:38 `go relaySignals(...)`. Stop: `defer close(done)` at :37, immediately before the go. Nothing waits.
9. internal/teamchat/member/statusline/statusline.go:64 `go relay(ctx, ...)`. Stop: `defer cancel(nil)` at :63. Nothing waits.
10. cmd/pingbus-status-line/main.go:61 and internal/teamchat/member/statusline/statusline.go:119 (stdin readers).
    - They send to a buffered channel (cap 1), so the send never blocks.
    - On cancel, the read is abandoned by design and blocks on stdin until EOF or process exit. Nothing waits.
    - The process exits soon after, so the practical risk is low. The pattern is "abandon a blocked read".
11. internal/freeze/chooser.go:284 and internal/open/choose.go:161 (`readLine` tty readers).
    - Same pattern: buffered done, read abandoned on ctx or interrupt, held `tty` reader, no wait.
    - chooser.go:281 documents the abandonment explicitly.
12. internal/teamchat/units/bus.go:30 `dialBus` dial goroutine.
    - Buffered `done`. When ctx wins, `cancel()` of `life` aborts the dial: the stop is guaranteed.
    - The goroutine is not awaited. A dial that succeeds after the abandon is closed by godbus via the cancelled `life`.
13. internal/teamchat/units/units.go:175 `go s.dispatch(ch)`.
    - It ends when godbus closes the signal channel on `conn.Close()`.
    - Close is called on the setup-failure path (:177), on the lost path (:164) and in `Manager.Close`.
    - Nothing waits for `dispatch`. The stop relies on godbus closing registered channels, which I did not verify in the vendored source.
14. internal/teamchat/member/link/link.go:191 `go l.srv.run(ctx)` (accept loop).
    - Stop: `srv.close()` in `teardown`, which is deferred before this start. Guaranteed on every path after :182.
    - `srv.close` waits `s.wg` (the handlers) but not the accept loop. Nothing waits for `run` itself.
15. internal/teamchat/member/link/server.go:212 per-request `go func(){ select{<-s.stopping; <-ctx.Done()} }()`.
    - `cancel()` is called on every return path of `handle` after the start (hook branch :222, normal :230). Guaranteed.
    - `handle` is in `s.wg`; this goroutine is not.
16. internal/teamchat/member/keeper/loop.go:42, :166, :170, :458.
    - :42 `go k.send(...)` from a timer callback: bounded by `k.done`, closed unconditionally when `loop()` ends.
    - :166 and :458 are `proc.Wait()` goroutines that then call `k.send`. They end when the child exits, and `k.send` is bounded by `k.done`.
    - :170 `go k.read(l)` ends when `l.conn` is closed (`closeQuietly(l.conn)` in `linkExited`, :352), so it relies on the link process exiting.
    - Nothing waits for any of them; the keeper process ends afterwards.
17. internal/vmtest/app.go:110 `WithSignals` goroutine. Stop: the returned `release` (`close(done)`). The only caller, cmd/vmtest/main.go:24, defers it. Not awaited.
18. Timer and context.AfterFunc callbacks (helper-started goroutines, no `go` keyword).
    - Examples: engine/statuspace.go:88 (`onGoroutine`, bounded by `e.done`, timer stopped in `Run`'s deferred `disarmStatus`), keeper/loop.go:41, link/server.go:186/211/247/276, and the `cancel` and `closeSock` callbacks.
    - `context.AfterFunc` at engine/deliverloop.go:383 (stopped by the returned func).
    - faketranscript.go:96 (test fake in a non-test file) fires `t.record` on fake-clock advance.
    - Trivial, bounded bodies; none blocks on a channel. Listed for completeness, not as hazards.
19. signal.NotifyContext call sites (stdlib goroutine). All have `stop()` on every path: cmd/convert/main.go:18 (`stop()` before os.Exit), cmd/ftp-camera, cmd/stt, freeze/app/app.go:42, teams/picker/picker.go:107, qa/cmd/qa-all, qa/cmd/qa-changed. teams/app.go:134 hands the stop func to the caller; I did not follow the callers.

## 4. DOES NOT carry the hazard (stop and wait found on every path)

- ccylaunch/tokenusage.go:352: `wg.Go` + `wg.Wait` (:354).
- execx/foreground.go:65: buffered `done`; the loop returns only on `<-done`, so Wait always ends before return.
- execx/session.go:254 (`copyAll`): `wg.Go` + `wg.Wait`.
- ftpcamera/server.go:527, :545, :563 and :604.
  - `s.bg` has `stopBg` + `s.bg.Wait()` in `teardown`, which `serve` runs on every path after `follow`.
  - The worker WaitGroup is waited inside the tail goroutine.
  - The `tail` goroutine at :604 is awaited by `<-done` unconditionally.
- open/launch.go:108: `<-produced` on every path after the start.
- teamchat/matrix/tuwunelhs/tuwunelhs.go:76 (test helper in a non-test file): `t.Cleanup` registered immediately after the go; it cancels and waits on `ended`.
- teamchat/member/ends/ends.go:83, teamchat/health/health.go:163, engine/recv.go:119, engine/teams.go:299, link/link.go:458, teams/board/fetch.go:138 (pool; `p.wait()` at :70 with no early return between), qa/ansiblesyntax/check.go:31: all `wg.Go` with `wg.Wait` in the same function.
- engine/engine.go:185: `defer loop.Wait()` registered before the start; it stops on ctx.
- link/link.go:179 (`engDone`): buffered; awaited by the deferred `teardown` (:182) on every path after the start, with no early return between :179 and :182.
- link/link.go:180, :181, :192, :408, :413: `l.bg`; `quit` or ctx stops them; `teardown` closes `quit` and calls `bg.Wait()`.
- link/server.go:115: `s.wg.Go`, waited in `close`.
- link/parent.go:57: `Close` closes the file, then `<-p.done`. The only user, `run`, defers `parent.Close()` immediately.
- proof/watch.go:73: `Close` closes the file, then `<-w.done`. The caller is responsible for calling Close.
- teamsync/watch.go:44 (`SeatWatcher`): `Close` closes the file, then `<-w.done`. The caller is responsible for calling Close.
- teamsync/receipts.go:23: the returned stop cancels and waits; its only caller does `defer s.startReceipts(ctx)()`.
- teamsync/session.go:345 and teamsync.go:179: `Run` has `defer wg.Wait()` and `defer cancel()`.
- reexec/reexec.go:197 (test helper): `<-p.done` on its error paths; `Release` and `Kill` wait `<-p.done`.
- matrix/fakehs/http.go:62 (test fake): `Stop` closes the server and waits `<-done`.
- fakeinbox/fakeinbox.go:102 and :241 (test fake): `Close` closes the listener and connections, then `wg.Wait()`.
- teams/flow/kit.go:264: on cancel, it closes `r` and then `<-got`.
- teams/ops/playlock.go:171: `stopWait` is deferred; `done` is awaited on every return. :185: `picked` is awaited on every path.
- vmtest/guest.go:213 (follower): `stop()` cancels and waits `<-f.done`. It is called on every exit path (`building.teardown`, `running.teardown`, an explicit `f.stop()` right after the install run).
- wsi/record.go:58: the loop waits for `Done` before return, and `stopRecorder` waits `<-Done`.

## 5. What text search found that reading could not have checked

- The full inventory of go statements and `.Go(` calls in src-go (about 55 first-party sites). I enumerated them by regex and eliminated the vendor/ tree by path.
- The set of timer and context.AfterFunc starts, and the NotifyContext sites, which are helper-started goroutines with no `go` keyword.
- That `SignalContext` has no production caller, that `close(l.want)` appears nowhere, and which sites call `Wait()` on the WaitGroups.
- That no `errgroup` is used by first-party code; errgroup appears only in vendor/.

## 6. What only reading could judge

- Whether the stop is reached on every path: the defer ordering relative to early returns (link.go, where teardown is deferred only after awaitLaunch and build; playlock.go; ftpcamera `serve`), and whether a send is buffered or guarded by a quit/done channel (the `lines.go` and `link.go` blocks).
- Which `wg.Go` and `Wait` pairs have a path between them that skips the Wait.
- Whether a done signal is awaited on the grace-timeout and abandon paths (execx/session.go).
- That an abandoned read (stdin, tty) is by design, with a buffered result channel.
- Library-dependent behaviours I could not confirm from first-party code: godbus closing the signal channel on Close (item 13) and the keeper's closing of its inotify watcher on every path (item 5).

## 7. Searches run

- `^\s*go\s+[A-Za-z_(]`, `\bgo func`, `\bgo [a-zA-Z_.]+\(`
- `\.Go\(`, `errgroup`, `AfterFunc`, `NotifyContext`, `WaitDelay`, `http.Serve`, `.Serve(`, `time.NewTicker`
- Callers of `startReceipts`, `WithSignals`, `SignalContext`, `follow`, `unlock`
- Presence of `close(l.want)`, `bg.Wait`, `stopFeeding`
- Forms I looked for: bare `go`, `wg.Go`, errgroup, goroutine-helper functions, timer-callback goroutines, signal.Notify* goroutines, `exec.Cmd` WaitDelay goroutines (stdlib-internal; not first-party).
- Out of scope, not read: qa/rules, qa/driver, go-qa.toml, GoProjectRules.md.
