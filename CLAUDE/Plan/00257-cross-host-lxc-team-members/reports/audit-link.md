# Audit: LINK (the member's long-running connection and delivery)

## Scope

Read in full: `teamsync/teamsync.go`, `teamsync/session.go`, `teamsync/receipts.go`,
`teamsync/budget.go`, `teamsync/watch.go`, `teamsync/verbs.go`, `teamsync/reports.go`,
`link/link.go`, `link/server.go`, `keeper/loop.go`, `keeper/seats.go` (removal half),
`ends/ends.go`, `deliver/deliver.go`, `inbox/inbox.go`, `lock/lock.go`, `client/client.go`.
Read in part, to follow a path: `teamsync/questions.go` (the long-poll), `link/main.go`,
`wire/control.go` (outline only), `clock/clock.go`, `matrix/client.go`, `matrix/transport.go`,
`matrix/member.go`, `notice/notice.go`, `proto/verbs.go`, `proto/verdict.go`, `proto/limits.go`,
and `engine/timeout.go`, `engine/sender.go`, `engine/send.go`, `engine/outbox.go`,
`engine/storewrite.go` where the link's timers end there. Not read: `records`, `team`,
`teamsync/builder.go`, `lookup.go`, `forge.go`, `facts.go`, `link/hooks.go`, `facts.go`,
`membership.go`, `keeper/prepare.go`. Paths below are under `src-go/internal/teamchat/` and, unless
they start with `matrix/`, `proto/`, `clock/` or `notice/`, under `member/`.

Tests run: `go test` over `member/{teamsync,link,ends,inbox,lock,deliver,records,team,client,wire,keeper}`,
all green. No finding below was reproduced by a new test; each is from reading the code path,
and the two that a test already pins say so.

## What happens when the bus host suspends and resumes (traced)

1. Each member has one `/sync` long-poll in flight (30 s, `teamsync/session.go:27`), bounded by a
   45 s limit armed on a runtime timer (`matrix/sync.go:183`, `matrix/client.go:421`). The frozen
   host answers nothing, so the limit fires and the request fails as `timed out`, transient
   (`matrix/client.go:478-480`).
2. `Run` takes the transient path (`teamsync/teamsync.go:255-258`): backoff 1 s doubling to 30 s
   with up to a quarter of jitter (`:28-32`, `:342-343`). The team is marked `down` at the first
   failure that is 15 s into the streak (`:334-337`), which with the shipped jitter is the sixth,
   about 31 s (`teamsync/run_test.go:254-258`). Counted from the host's freeze that is up to
   45 s + 31 s.
3. Every retry is a whole new `session()` (`teamsync/session.go:52-94`): pair read, main's full
   state verified, sends recovered, store loaded. It fails at the first request each time. The
   dial is bounded at 10 s (`matrix/transport.go:157`). Nothing accumulates: no goroutine, timer or
   connection is left per retry (`backoff` stops its hold, `teamsync/teamsync.go:344-345`).
4. The stored position survives: `start` returns the store's `NextBatch` when its epoch is the
   roster's (`teamsync/session.go:176-185`). An answer is judged and written with its position in
   one store write (`:562-573`, `:641-656`), and an already-seen event is ignored as seen, so the
   reconnect itself neither loses nor repeats an event.
5. TLS: the transport is HTTP/1.1 with no client session cache (`matrix/transport.go:159-169`), so
   a reconnect is a full TLS 1.3 handshake against the pinned CA. There is no session state to lose.
6. On resume the next retry (at most 30 s away) verifies, and the long-poll starts from the stored
   position. The team is shown `up` only when that long-poll answers (LINK-4).
7. Sends made during the outage are queued in the store and posted once the team is up
   (`engine/send.go:643`, `engine/sender.go:145-154`).

So the link does reconnect and does not wedge. What goes wrong is in the timers and the rate
windows around that reconnect: LINK-1 to LINK-5.

## What happens when the member host suspends (traced)

Runtime timers stop with the machine (`clock/clock.go:4-5`). On wake the in-flight long-poll is
still waiting on what is left of its 45 s. When it ends (an answer, an error, or the limit), the
freeze detector sees boot minus mono grown by more than 2 s (`teamsync/teamsync.go:429-453`),
drops the idle connections and restarts the session at once with no backoff
(`teamsync/session.go:471-473`, `:489-493`). A good answer that arrives with the freeze is
discarded, and since the position was not moved it is asked for again: no loss. See LINK-7 for the
delay, and LINK-1 for what the catch-up then does to the backlog.

## Findings

### LINK-1 (HIGH) A backlog is rate-limited by when it arrives, so catch-up after an outage throws messages away

- `teamsync/session.go:744` reads the wall clock once per answer; `:828` gives it to the judge as
  `NowMs`; `proto/verdict.go:369-371` and `teamsync/budget.go:20-33` admit against windows keyed
  on that one instant (`proto/limits.go:225-235`).
- Two limits apply to every event of one answer as if all were sent in the same millisecond:
  the receive flood (`recv_per_sender_minute`, default 60, `proto/limits.go:33`), whose excess is
  dropped as `rate` (`proto/verdict.go:337`), and the delivery budget (`deliver_per_sender`,
  default 10 per 10 minutes, `proto/limits.go:38`, `:95`), whose excess "never enters pending"
  (`teamsync/budget.go:16-19`) and is only counted in `HeldBack`.
- Scenario A (bus suspend): the bus sleeps for two hours. A coordinator keeps working and queues
  25 PEER and PING items to one worker. The sender charges its own gate when it queues
  (`engine/send.go:630-655`) and `engine/sender.go` has no pacing between queued posts (its only
  wait is the 30 s hold after a failure, `:23`, `:160`). On resume all 25 are posted within
  seconds, the worker receives them in one or two answers, delivers 10 and reports "15 held back".
  Those 15 are never delivered; the floor moves past them (`teamsync/session.go:836`).
- Scenario B (member suspend or partition): a worker's host sleeps overnight. On wake the first
  answer is filled back up to 20 pages of 100 (`teamsync/session.go:28-29`, `:373-394`). Every
  agent sender's 11th and later item is held back, and a sender's 61st event is dropped.
- Fix: judge the windows on each event's `origin_server_ts` (clamped to now) instead of the
  arrival instant, or exempt a catch-up answer (first answer, limited timeline, or the first
  answer after `down`) from the delivery budget. Add a test that drives a queue of more than
  `deliver_per_sender` through an outage.
- Known weakness: no (W7 says "members queue and nobody blocks"; it does not say the queue is cut).

### LINK-2 (HIGH) Ping deadlines and staleness keep running while the bus is asleep

- A ping is stale when `now - origin_server_ts` exceeds its verb's timeout, default 900 s, 300 s
  for a halt (`proto/verbs.go:121-130`, `proto/limits.go:34-35`); a stale ping is dropped
  (`proto/verdict.go:444-452`). Text is stale after 24 h (`proto/verbs.go:133-135`).
- The sender's TIMEOUT timer runs on its own wall clock and does not look at the team's health:
  `eligible` is only removed, refused or no store (`engine/recv.go:70-72`), and `makeTimeouts`
  writes every TIMEOUT due (`engine/timeout.go:91-127`).
- Scenario: a ping is posted one minute before the owner suspends the bus for an hour. The
  addressee had not synced it yet, or had and was working on it. Fifteen and a half minutes into
  the outage the sender's link writes "TIMEOUT: silent" for an addressee that could not have
  answered. On resume, an addressee that had not received the ping judges it an hour old and
  drops it as stale. The ping is lost and the sender has been told the peer went silent.
- This is the code doing what A 8.3 and 8.5 say, so it is a design gap against the owner's
  requirement, not a coding slip.
- Fix: do not count time the team was `down` toward a ping's deadline or its staleness. The link
  already knows the outage interval (`failStart` to `recovered`, `teamsync/teamsync.go:328-367`):
  hold TIMEOUTs while `Down`, extend `due_ms` by the outage on recovery, and judge staleness
  against "now minus the outage". A deliberate suspend could also be announced by the host
  (an admin notice carrying the pause) so both ends agree on the extension.
- Known weakness: W7, but only its availability half; the false TIMEOUT and the stale drop are new.

### LINK-3 (HIGH, conditional on the host's clock) The member trusts the server's clock against its own, and nothing checks the two agree

- Every comparison above is member wall time against `origin_server_ts`. An own ping's deadline
  is re-based on the echo's server time (`engine/outbox.go:51-52`). `notice.LatestOwn` stops at
  the first lifecycle event older than the run's start, which is member wall time
  (`notice/notice.go:154`, `ends/ends.go:351`, `engine/storewrite.go:415`).
- If the bus VM comes back with its wall clock behind by the length of the pause (a hypervisor
  pause with no clock step afterwards), every event it stamps is that much "old". Behind by more
  than 15 minutes: every new ping is stale on arrival at every member, and every own ping is past
  its deadline as soon as its echo arrives, so the sender gets a TIMEOUT at once. Behind by more
  than 24 h: every text is dropped too. This lasts until the clock is corrected.
- Confirmed in code: there is no skew check anywhere in the member (no use of the HTTP `Date`
  header, no comparison of an own echo's time with the send time), and `play-teamchat.yml`
  configures no time sync and no sleep handling (searched for chrony, makestep, timesync,
  guest-agent, sleep.target, suspend: no match).
- Not confirmed: whether the owner's way of suspending leaves the guest clock behind, and for how
  long. That needs one measurement on the real bus VM (see Unconfirmed).
- Fix: measure skew from each own send (local send time against the echo's `origin_server_ts`,
  both already in `engine/outbox.go`), log and show it in status past a threshold, and judge
  deadlines with the skew removed. Host side: make the play own time sync with a step on resume.
- Known weakness: no.

### LINK-4 (MEDIUM) After an outage a team stays `down`, and its queued sends stay held, until its first long-poll answers

- `recovered()` runs only in `settle` and `readd` (`teamsync/session.go:217`, `:588`). A team with
  a stored position makes no sync of its own at a restart (`:184-185`), so in an idle room the
  first handled answer is the long-poll's timeout, 30 s later. `TestABackTeamIsShownUpAtItsFirstLongPoll`
  pins this (`teamsync/run_test.go:216-252`).
- While `Down` the sender does not post (`engine/sender.go:150-151`) and a new send is queued at
  once (`engine/send.go:643`).
- Scenario: the bus resumes, every member reconnects within 30 s, the room is quiet because
  everybody's sends are held, and everybody waits another 30 s for their own long-poll to time
  out. The first minute after every resume is silent for no reason.
- Fix: end the failure streak when the session's own requests have succeeded (after `verify` and
  `epochCheck`, `teamsync/session.go:60-89`), or make the first poll after a failure streak a
  zero-timeout sync.

### LINK-5 (MEDIUM) `back` is tried once per run and never again

- `teamsync/session.go:408-417` sets `backStarted` before the goroutine runs; `:420-431` logs
  "back not posted" on any error and returns.
- In a non-plain session `back` waits for Claude Code's first SessionStart (`:421-427`), which can
  be long after the link connected. If the bus is asleep at that moment (or the request just
  fails), the member's latest lifecycle notice stays `disconnected` for the whole run although it
  is working and answering.
- Fix: retry `Back` on the team's backoff until it posts or the run ends; it is idempotent by its
  per-run transaction id (`ends/ends.go:365`).

### LINK-6 (MEDIUM) A stopped team is woken only by a change in its seat directory

- `await` waits on `t.wake` or the context (`teamsync/teamsync.go:274-281`). The only caller of
  `Wake` in the member is the seat watcher's forwarder (`:263-272`; `grep` for `.Wake()` finds no
  other). No timer, no verb and no keeper message wakes it.
- A stop is any 403, a 404 on main's state or roster, a second 401, a trust failure, or any
  `exits.Error` out of the engine (`:284-297`, `teamsync/session.go:127-129`, `:437-439`).
- Scenario: during a homeserver restart, restore or membership repair the server answers one
  403 or 404 for main. The team stops with `forbidden` or `untrusted` and stays stopped after the
  server is healthy again, until someone rewrites the seat files or restarts the session.
- Fix: re-probe a stopped team on a slow timer (the 5 minute `causeEvery` cadence would do), and
  let `status` or any verb for the team wake it.

### LINK-7 (MEDIUM) A member-host suspend is noticed only when the in-flight long-poll ends

- The freeze detector is read only at a poll's start and answer (`teamsync/session.go:465-473`).
  The request's limit is a runtime timer that resumes with whatever it had left
  (`matrix/client.go:421`), so after a wake the member can sit up to 45 s on a connection the
  server gave up on long ago. Go's default TCP keep-alive usually ends it sooner, but a custom
  `Config.Dial` (`matrix/transport.go:155-158`) carries no such promise.
- `frozen` calls `Client.Close`, which is `CloseIdleConnections` (`matrix/client.go:183`): the
  receipts, reports and builder goroutines keep any connection they have in flight.
- Side effect: a sync that waited out two 429s (`matrix/client.go:389-408`) can take longer than
  55 s and is then logged as "a freeze or suspend" (`teamsync/teamsync.go:450-452`), which
  misleads triage.
- Fix: one watchdog per link that reads boot minus mono every second or two and cancels the
  in-flight requests of every team on a jump; cancel by context, not by closing idle connections.

### LINK-8 (MEDIUM) A seat released while the bus is unreachable loses its `left` notice for good

- `link.leave` logs a failed presence and a failed leave post and sends `left` regardless
  (`link/link.go:444-456`). The keeper then drops the seat, or drops it anyway after 30 s
  (`keeper/seats.go:356-385`). The owed transaction id lives only in the keeper's memory
  (`:364-365`) and the store is already deleted (`:351`).
- The floor marker's last put has 2 s (`engine/teams.go:316-323`), so it is not put either.
- Scenario: a seat is returned or parked while the bus VM sleeps. Peers never see `left: released`.
  The next holder of the seat starts from an old floor marker and is shown again, as restored,
  what the previous holder had already handled.
- Fix: keep an owed leave (team, user, txn) in a small file in the member's state directory and
  post it at the next link start for that team, as `KindOwed` already does within one run.

### LINK-9 (MEDIUM) No test drives an outage end to end

- `Suspend` is used in `teamsync` only for a few seconds, to trip the detector
  (`teamsync/floor_test.go:181-231`, `teamsync/answers_test.go:163`, `:237`). `StepWall` moves the
  member's clock, never the server's.
- Missing: (a) server unreachable for longer than `ack_timeout_s` with a ping in flight;
  (b) a queue of more than `deliver_per_sender` flushed after an outage (LINK-1);
  (c) server time behind member time (LINK-3); (d) `back` failing once (LINK-5);
  (e) a real two-process run where the fake homeserver is SIGSTOPped and continued.
- Fix: add these to `teamsync` and to the wiring end-to-end tests before any of LINK-1 to LINK-5
  is changed, so each fix is proved red first.

### LINK-10 (LOW) The 401 rule compares with a pair another goroutine may already have replaced

- `retryAuth` treats "the pair read now equals `t.pair`" as "unchanged, so stop"
  (`teamsync/teamsync.go:307-319`). `current()` replaces `t.pair` whenever the files changed
  (`:376-398`) and is called from other goroutines through `Notices` (`teamsync/verbs.go:329-359`).
  `teamsync/reports.go:156-158` names this hazard and avoids it for the reports loop only.
- Scenario: a reissue places a new token; a send reads the new pair first; the session's long-poll
  gets a 401 for the old token; `retryAuth` sees no difference and stops the team as `token`.
  The seat watcher's buffered wake normally restarts it at once, so the visible effect is a false
  "stopped (token)" line, unless that wake was consumed by an earlier backoff.
- Fix: compare with the pair the failing session started with (`sess.pair`).

### LINK-11 (LOW) A link exit of 78 is final, and the link returns 78 for failures that can pass

- `keeper/loop.go:415-416` fails the link for good on exit 78 and closes every seat lock
  (`:455-465`). The link returns 78 when `link.lock` is busy after three tries 50 ms apart
  (`link/link.go:144-147`), when `link.sock` cannot be bound (`:198-201`) and when `session.json`
  cannot be read (`:295-297`).
- Scenario: a full or briefly failing runtime directory at one restart leaves the session with no
  link until a person restarts it.
- Fix: count these as a crash (the six-exit rule already bounds a loop) and keep 78 for a real
  configuration fault.

### LINK-12 (LOW) `SeatLock.WriteRecord` is not crash-atomic

- `lock/lock.go:208-223` writes at offset 0, then truncates, then syncs. A crash between the write
  and the truncate leaves the new record followed by the tail of a longer old one, which does not
  decode. A reader holding only a shared probe can also read the mixed bytes.
- Fix: pad records to a fixed length, or write a length-prefixed record so a tail is ignored.

### LINK-13 (LOW) `link.sock` answers are written with no write bound

- `link/server.go:245-248`, `:272-277`, `:346-353`: records and `done` are written through a
  `bufio.Writer` with no deadline, and the request's context does not interrupt a blocked write.
- Scenario: a client that is stopped (SIGSTOP) or whose stdout pipe is full stops reading. The
  handler blocks until the link closes, and the items of a hand-out stay handed, so they are not
  delivered to anyone else (`deliver/deliver.go:91-93`).
- Fix: arm the same `AfterFunc` close used for the read (`link/server.go:208`) around each answer.

### LINK-14 (LOW) Docs against code

- `research/weaknesses.md` section 6 says a team is marked down "after 15 s". In code it is the
  first failure at least 15 s into the streak, about 31 s with jitter, and the streak begins only
  when the 45 s long-poll limit fires (see the trace, step 2).
- The same section's "sends do not block" is true, but queued sends are then held up to 30 s
  after the server is back (LINK-4), and more than ten to one addressee are cut (LINK-1).

## Unconfirmed

- U1. Which clocks jump on the owner's bus VM at suspend and resume. A hypervisor pause freezes
  boot and mono alike and leaves the wall clock behind until something steps it; a guest S3
  suspend moves boot and wall. LINK-3's severity depends on this. One measurement settles it:
  read `CLOCK_REALTIME`, `CLOCK_BOOTTIME` and `CLOCK_MONOTONIC` in the guest before and after a
  real suspend, and again some minutes later.
- U2. Whether Tuwunel answers 401, 403 or 404 for a short time while it starts or resumes. If it
  does, LINK-6 turns from an edge case into a wedge at every resume.
- U3. LXC members sit on the bus host (W3), so they freeze and thaw with the server and their
  connections survive. With a hypervisor pause the freeze detector sees nothing, which is right,
  but a later wall-clock step re-arms nothing until the next sync answer
  (`engine/storewrite.go:420-423`). I did not trace whether a TIMEOUT can fire late there.
- U4. A bus VM reverted to a snapshot (not resumed) leaves members holding a position from the
  server's future. Only a bumped restore epoch resets it (`teamsync/session.go:435-459`); I did
  not check that the host's snapshot procedure bumps it.
- U5. `engine/sender.go` was read for pacing only. If queued posts are paced somewhere I did not
  read, scenario A of LINK-1 needs a longer queue to trigger; the receive-side cut is unaffected.

## Bloat and debloat candidates

- `teamsync.Team` carries nine test-seam function fields (`teamsync/teamsync.go:141-155`) beside
  its state; a single `hooks` struct, nil in production, would shrink `New` and the struct.
- `teamsync/session.go` is 1,083 lines holding the session loop, the resume rules, the judging,
  the floor markers and the invite rules. The floor marker (`:946-1083`) and the resume bounds
  (`:221-394`) are self-contained and would each be a small file with its own tests.
- `verifyMain` (`teamsync/session.go:118-121`) is a two-line wrapper over `mainState` with one caller.
- `deliver` redeclares the item kinds (`deliver/deliver.go:25-31`) that `records` already exports
  (`records/records.go:25`).
- `howKeeperDied` exists twice (`link/link.go:50`, `ends/ends.go:38`).
- The `tc` plus 32 hex transaction id is built twice (`ends/ends.go:356-362`,
  `keeper/seats.go:281-286`).
- The run-directory-to-layout lookup is written twice (`link/main.go:100-115`,
  `client/client.go:167-186`).
- `link.refuse` has two branches that return the same value (`link/link.go:162-167`).
- Three copies of the same mutex-guarded `logf` that discards a failed write
  (`link/link.go:630-637`, `link/server.go:190-197`, `ends/ends.go:293-300`).
