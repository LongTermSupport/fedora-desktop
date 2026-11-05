# n3e-10 fix search: tests that sync a fake clock by waiter COUNT, then advance

Root: src-go/internal/teamchat (paths relative to it). I did not open qa/rules, go-qa.toml or qa/driver, and did not run the detector. The dispatch contained no rule text.

Only count-API fake clock: clock/fake.go (Waiters() :95, BlockUntil(ctx,n) :103, waits for exact equality). Other fakeClocks (host, matrix, guest, lock, teamlock, seat, health, sweep, ends hsClock, ccylaunch, pinclock) have only Sleep/now, no count wait. matrix/timers_test.go fakeTimers records armed timers; the test waits on `<-timers.notify` (timers_test.go:76), an "any arm" signal, not a count, and fires by hand: not this class. Nothing in src-go/qa uses a count.

Legend: HIGH = another timer is demonstrably armed/disarmed concurrently; MED = plausible, world needs reading to settle; LOW = only timer by construction.

## Instances (file:line, chain, judgement)

1. member/link/hooks_test.go:229-235 TestAnOpBeforeThePIDIsNotAnswered. Snapshot armed := Waiters(), BlockUntil(armed+1), Advance(hookDeadline). ORIGINATING. Concurrent: server.go:186 readLimit AfterFunc armed at accept on the handle goroutine, stopped at :188 after the line is read. Count reaches armed+1 on readLimit before the request is read; hookDeadline (server.go:211) is armed afterwards. HIGH.
2. member/link/server_test.go:220 TestTheDeadlineEndsTheRequest. until(requests()==1), BlockUntil(1), Advance(5s). readLimit is stopped before Serve (server.go:188 vs :228) and the deadline AfterFunc (:211) is armed before Serve, so 1 is the deadline. LOW-MED (relies on that ordering).
3. member/link/server_test.go:277 TestASilentClientIsClosed. dial, BlockUntil(1), Advance(readLimit). Intended timer is readLimit, only waiter. LOW.
4. member/link/server_test.go:361 TestALateAckReturns. handOut, BlockUntil(1), Advance(ackWait). Concurrent: readLimit (server.go:186) armed at accept then stopped; ackWait armed at server.go:276 after the hand-out is sent. If handOut returns before readLimit is stopped, 1 can be readLimit. MED (read w.handOut to confirm).
5. member/link/link_test.go:512 TestReadyWaitsTenSecondsForTheAdds. BlockUntil(2), Advance(addsWait). Comment names the pair: adds Hold (link.go:169) and the engine's status.json timer (engine/statuspace.go:88). Exact 2 could be met by another pair (status timer re-armed or stopped by the engine goroutine, a readLimit on an add connection). MED.
6. member/link/link_test.go:577 TestNoLaunchInTenSeconds. BlockUntil(1) after start; adds Hold (link.go:169) is the only waiter. LOW.
7. member/link/link_test.go:620 TestAHeldLinkLockIs78. Goroutine loop x2: BlockUntil(1), Advance(1s); waiters are lock.TakeLink Sleep retries; the adds Hold at link.go:169 comes after the lock so is absent. LOW-MED.
8. member/client/nolink_test.go:140, 146, 180, 230. Loops of BlockUntil(1) then Advance(d): client.go:206 Sleep backoff, client.go:228 AfterFunc grace. :230 waits for the client's deadline timer after AcceptUnix. Backoff Sleep and grace timer are sequential in one goroutine. LOW.
9. member/hook/hook_test.go:475 hungHook. Fresh Fake, BlockUntil(1), Advance(300ms); hook.go:206 deadline AfterFunc is the only waiter. LOW.
10. member/inbox/phase_test.go:47-53 helper expire() = BlockUntil(1)+Advance(PhaseTimeout); callers :66, :118, :140. inbox.go:126 phase bound, inbox.go:247 finish bound. LOW for :66.
11. member/inbox/phase_test.go:90-109 helper expireUntil (goroutine BlockUntil(1) racing done, then Advance(PhaseTimeout)); caller :82. Its comment (:87-89) already records the hazard: the connect phase's bound (already stopped) is armed just before the auth line's, so count 1 may be the connect's and the auth bound is armed after the clock moved. It loops until done answers. MED (passes by looping, not by synchronisation).
12. member/inbox/inbox_test.go:211 TestAFinishThatTimesOutLeavesTheWriteCounted. BlockUntil(1) after the Finish goroutine, Advance(PhaseTimeout-1ms); finish bound inbox.go:247; earlier dial phase bounds should be stopped. LOW-MED.
13. member/keeper/harness_test.go:381-385 helper (w *world).advance(n,d) = BlockUntil(n)+Advance(d). Callers: keeper/kp_test.go:53 (n=2), 56, 115, 117, 127, 129, 206, 227, 245, 248, 291; rc_test.go:122, 178 (n=2), 188, 270, 370; loop_test.go:18. Concurrent timers: keeper.after (loop.go:38) arms readyTimer (:175), endTimer (:173, :629), afterTerm (:519, :586), rewatchTimer (seats.go:174), removing timer (seats.go:295), owed timer (seats.go:363), all on the keeper loop goroutine; cancel() (loop.go:54) disarms them there; a fired timer's follow-up runs via k.send after Advance. A count of 1 can be a timer about to be cancelled or replaced. Suspects: kp_test.go:53 and rc_test.go:178 (exact 2), kp_test.go:56 (afterTerm just fired, next armed later), kp_test.go:245-248 (readyWait). MED, not individually traced.
14. member/keeper/kp_test.go:165, 211, 232, 250, 303 and rc_test.go:291: mustDo(BlockUntil(t.Context(),1)) after l.code(1) (link exit), then reads keeper.json or advances. Intended: restart backoff timer. Concurrent: the dead link's readyTimer cancelled on the loop goroutine (loop.go cancel) in the same handler; if still armed when counted it satisfies 1. MED (exit handling needs reading to settle).
15. member/keeper/prepare_test.go:156 "version hangs". BlockUntil(1), Advance(versionWait); prepare.go:210 only timer before the loop. LOW.
16. member/engine/engine_test.go:505-513 helper (w *world).armed(n): reads on the state goroutine whether statusPace.stop is set, adds 1, then BlockUntil(n'). Its comment admits the race: "The caller makes no change meanwhile, which would re-arm it." Callers: deliver_test.go:210 (inside dworld.advance, item 17), teams_test.go:203 (Remove with stopWait Hold, teams.go:228), recv_test.go:291 (refetchWait AfterFunc, recv.go:111). Concurrent: the status pace timer (statuspace.go:88) can be armed after the snapshot as the engine reacts; delivery Holds (deliverloop.go:114, 130) and recv waitRetry (recv.go:396) are armed on engine goroutines. MED.
17. member/engine/deliver_test.go:208-212 helper dworld.advance(d) = armed(1)+Advance(d). Callers: deliver_test.go:366, 488, 497, 522, 572, 576, 598, 633, 669, 684, 709, 721, 791, 792, 796, 814, 821, 836, 858, 867, 873, 888; deliverloop_test.go:49. Competing: delivery pacing/retry Hold re-armed on the loop goroutine, status pace timer, refetch AfterFunc. Back-to-back advances (791-796, 872-873) have the shape where a just-fired timer's replacement is armed after the next count. MED-HIGH for consecutive advances.
18. member/teamsync/resend_test.go:117 BlockUntil(2), Advance(longPoll). Comment: fakehs's hold plus the request's deadline (teamsync.go:283, receipts.go:74 Holds). Exact 2 could be met by another pair (receipt Hold plus poll deadline, or a stale hold from an earlier poll before RestoreTo). MED.

## Waiters() uses that do not advance (assertions, not syncs)
clock/clock_test.go:39 (helper armed = BlockUntil, fake's own tests, single goroutine, LOW), 82, 108, 120, 150; client/nolink_test.go:26; inbox/inbox_test.go:88; faketranscript/faketranscript_test.go:175. None waits for a positive count and then advances.

## Searches run
- grep -rnE "BlockUntil|Waiters\(\)|\.Armed\(|pending\(\)|NumTimers|numWaiters|Sleepers\(" --include=*.go over src-go, excluding vendor, qa/rules, qa/driver
- grep -rnE "type [a-zA-Z]*(Clock|Timer|Ticker|Sleeper)[a-zA-Z]* (struct|interface)|func \(.*\) (After|NewTimer|AfterFunc|Sleep|NewTicker)\(" (fake clock inventory)
- grep -rnE "clk\.(Armed|Until|Pending)|waitArmed|awaitTimer|func \(w \*[a-z]*\) (arm|settle|await|wait|armed)..." --include=*_test.go
- grep -rnE "len\((c|f|w)\.(timers|waiters|sleepers|armed|pending)" --include=*_test.go (only matrix fakeTimers)
- grep -rnE "\.advance\(|\bexpire\(|expireUntil\(|\.armed\(" --include=*_test.go under member (helper callers)
- grep -rnE "\.(AfterFunc|Hold|Sleep)\(" over non-test files of link keeper engine inbox client hook teamsync (to name concurrent timers)
- Forms looked for: direct BlockUntil, helper wrappers, Waiters() snapshots and comparisons, polling loops over timer lists, custom fakes with counts, until(func(){ Waiters()==n }) (last form not found).

## Attribution by technique
Text search found: every BlockUntil/Waiters call site, helper definitions by name, and helper callers (items 1-12, 15, 18, direct sites in 13-14, helper definitions in 16-17).
Reading found: the concurrent production timers (server.go readLimit/ackWait/deadline, keeper.after set, statuspace, delivery Holds), the existing phase_test.go comment recording the same hazard, the engine armed() snapshot adjustment, BlockUntil's exact-equality semantics (fake.go:101-115), and every per-site judgement. Text search could not say whether a count is the intended timer.
Not done: no tests run; MED items not traced to a witness interleaving.
