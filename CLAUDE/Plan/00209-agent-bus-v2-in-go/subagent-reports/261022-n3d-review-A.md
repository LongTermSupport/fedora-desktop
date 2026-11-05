# n3d slice review A: the engine's send side and its stores

Reviewer A, read-only, at 825d2ab62 (`git diff 6a0435bde HEAD -- src-go`, my half: `member/engine`
outbox, send, sender, reply, roster, timeout, seam, teams, status, serve, engine.go and tests;
`member/state` store.go and rostercache.go; `member/records`; `proto/send.go`; `member/exits`).

## Verdict: FIX-FIRST

Counts: blocker 0, major 3, minor 4, nit 3.

`go test -race -count=2` is green on `member/engine`, `member/state`, `member/records`,
`member/exits` and `proto`, so there is no global state that breaks `-count=2`. The send path
holds the offline rule in its main lines. A down team queues at once. A transient failure queues at
the deadline and the sender posts it later by the same txn. A 401 removes only the waiting send.
`e.do` is unbuffered and waits for `finished`, so a failed `do` never half-runs a charge. The
`answer`/`unwait` handshake cannot lose a result. The three majors are a queued send that is
acknowledged and then lost, a permanent refusal that is shown as a transient queue, and a
duplicate post that n3d-D4 claims to prevent but does not.

## Major

### M1. The recovery matches a tried send to an earlier identical event: the send is lost, and a ping's ids collide

`member/engine/sender.go:675-702` (`matchTried`), called by `recoverTried` at `:628-649`.

`matchTried` pairs a tried entry with any own `m.room.message` in main's latest 100 events whose
content is equal and whose `origin_server_ts >= since_ms`. Nothing in the content is unique to the
send. `proto.Composer.Text` and `Ping` (`proto/send.go:69-124`) build only `v`, `to`, `text` or
`verb`/`ref`, and the body. The duplicate window refuses an identical second send only within
60 s. `since_ms` is the charge time, not the time of the first PUT.

Failure scenario:

1. The team is down. The agent runs `say --to K` with "ok" at t0, and again with "ok" at t0+61 s.
   Both are queued, and both answer `queued`, exit 0.
2. The team comes up. The sender posts the first ("ok", event E1, origin after t0+61 s) and records
   it. It then marks the second `tried`.
3. The PUT of the second fails transiently (or the link crashes at
   `link-after-charge-before-send`). The link restarts.
4. `RecoverSends` finds E1. E1 has the same content and `origin >= since_ms` of the second, which
   is t0+61 s. So the second is recorded as posted with id E1 and is never PUT. The agent was told
   it was queued, and it is never delivered.

For an answer-expecting ping (two `ping review --to K --ref X` sent more than 60 s apart while
down), the same match gives `recordPosted` a second `AddPing` with ID E1. `Ping.check`
(`state/store.go:373`) does not refuse duplicate ping ids, so the write succeeds and the outbox
holds two pings with one id. `findPing` only ever answers the first one. When both fall due,
`owedTimeouts` adds `TimeoutKey(E1, k)` twice. `checkPending` refuses that ("pending holds an id
twice"), so `makeTimeouts` fails the whole team's TIMEOUT write. It retries every 60 s for as long
as the link runs, and no other TIMEOUT for that team is ever made. That also breaks the rule that
an offline member's silence is a TIMEOUT.

Fix: anchor the search on the last send this link recorded, not on a time. `recordPosted` already
removes the entry in a store write. Have it also record the posted event id (for example
`outbox.last_sent`). `matchTried` then reads the backward page only down to that id and considers
only the events newer than it. The `since_ms` comparison then goes (see m1). An alternative is to
prefer `unsigned.transaction_id` when the server returns it for the same device. Also make
`Outbox.check` refuse duplicate ping ids, so a defect like this is refused at its write and does
not get as far as the TIMEOUT. Test: two identical queued sends more than 60 s apart, the second
tried and not PUT, then a restart: both reach the room once.

### M2. A 403 on a queued send neither stops the team nor reaches the agent: the queue is retried after every sync, and later sends answer `queued`, exit 0

`member/engine/sender.go:477-498` (`failed`).

On `ErrForbidden`, `failed` answers and removes only the sends an op waits on. It then keeps the
head behind a non-transient hold (`afterSync`). Nothing sets `Health.Stopped`. teamsync stops the
team only on its own sync's 403 (`teamsync.go:234-237`). A PUT that is forbidden while `/sync`
still works is common: for example, the member has been kicked from main, or muted by power
levels. In that case:

- after every successful sync, `mayPost` is true again and the head is PUT again. The 403
  repeats, once per long-poll, for as long as the link runs;
- every later `say`/`ping` finds older entries, so `charge` sets `queuedNow` (`send.go:579-580`)
  and answers `{"queued":true}`, exit 0. The agent is told its sends are waiting, when the server
  will never take them;
- `status` shows the team up, with `queued` records `tried: true`. The only trace of the refusal
  is in `link.log`.

The brief (section 9: "Permanent (401, 403, lost trust): the sender stops for that team ... shown
in `status` with the team's stop") says the opposite. Under the offline rule the agent should get
22 `forbidden` for new sends (step 3, `stoppedErr`), and the queued ones should wait for a stop to
clear.

Fix: on `ErrForbidden` (and on `ErrAuthRefused`), have the sender record a stop on the held team,
for example `h.health.Stopped = CodeForbidden` through the same path `SetHealth` takes, or a
send-side stop flag that `stoppedErr` and `mayPost` read. It is cleared when teamsync next reports
a healthy run. Then later sends exit 22, `status` shows the stop, and the queue waits without a
PUT on every sync. Test: a 403 on a queued head while syncs succeed. The next `say` exits 22,
`status` shows the team stopped, and no second PUT happens before the stop clears.

### M3. A token reissued within one link run re-PUTs a tried send under a new device: a duplicate that n3d-D4 claims to cover

`member/engine/sender.go:604-626` and `member/teamsync/session.go:56-61` (B's file; the contract is
mine).

`RecoverSends` runs once per team goroutine (`t.sendsRecovered`), and `h.recovered` is never reset.
n3d-D4 claims that the recovery covers "a token reissued between tries (a new device),
unconditionally". That holds only across a link restart. Within one run:

1. A PUT reaches the server and its answer is lost (transient). The entry stays `tried`, behind a
   30 s hold.
2. Before the retry, the pair changes. `retryAuth` (`teamsync.go:252`) picks up a reissued token,
   or a stop clears after a restore's token reissue (SD3-8b's path). `session` runs again, but
   `sendsRecovered` is already true.
3. The sender re-PUTs the same txn under the new access token. Matrix keeps a txn per device or
   token, so the room gets the message twice.

Fix: run the recovery whenever the session starts with a pair (token) other than the one the last
recovery ran under. On the engine side, set `h.recovered = false` when a new recovery begins (an
entry point such as `BeginRecovery`, or `RecoverSends` itself clearing the flag first on the state
goroutine). That keeps the sender from posting while the search runs, which the brief requires
("each retried team starts a new recovery"). Test: a lost answer, then a pair change, then the next
session. One event in main.

## Minor

### m1. The recovery's time bound compares the member's wall clock to the server's, and stops at 100 events

`member/engine/sender.go:694` (`ts >= s.SinceMS`) and `:225` (`recoveryPage = 100`).

`since_ms` is local wall time, and `origin_server_ts` is the server's (CK1). If the server's clock
is behind the member's by more than the gap from charge to PUT, which can be a few hundred ms for
an unqueued send, the event is not matched. A busy main (more than 100 events since the send,
which is likely after a long outage of the member) misses it too. Either miss re-PUTs by txn. That
is harmless under the same token and a duplicate under a new one (M3's case, and B section 4's
"homeserver that forgets transaction ids", which n3d-D4 sets out to cover).

Fix: M1's anchor removes the time comparison. Read pages back until the anchor, or until a bound
of a few pages, instead of one fixed page of 100. If the time bound is kept, subtract a skew
margin (for example 5 min). The spec reads one page (n3d-D4), so this also needs a text change.

### m2. A send before the run's recovery holds the agent's call for the whole deadline

`member/engine/send.go:579-582`.

`queuedNow` is `Down || older`. At a link start with the homeserver down, `recovered` stays false
(the recovery runs only after a successful verify), and `Down` is false until teamsync's streak
reaches `downAfter`. A `say` in that span registers a waiter that no sender can answer. It holds
the call for the request deadline and then answers `queued`. n3d-D19's intent is to answer
`queued` at once when nothing can post. Fix: `queuedNow = h.health.Down || older || !h.recovered`.

### m3. A team with no roster turns an unresolvable name into 64 `unknown-member`, not 20

`member/engine/send.go:220-225`.

With no `--team`, a team skipped for having no roster (verified or cached) sets `noRoster`. But
when any other team resolved the names partly (`first != nil`), the answer is 64
`unknown-member, run status`, even though the name may belong to the unverified team. That team is
down or unverified, so the right answer is 20 `unreachable`, which the agent waits on, rather than
an error it should fix. Fix: when `noRoster` and nothing was found, answer 20 for the roster-less
team ahead of `unknown-member`.

### m4. `reply` to an unknown id on a team with no goroutine is a nil interface call

`member/engine/reply.go:52-56` and `:173`.

`replyTeam` accepts a team whose `h.team` is nil (Stopped `bundle`, NewTeam failed) when a roster
cache exists, because `loadRoster` runs from seat.json whatever `NewTeam` does. `repliedTo` then
reaches `fetched`, which calls `st.tm.Refetch`, and that panics. `say`/`ping` are safe because
`sendable` runs `stoppedErr` first. This is reachable only when `NewTeam` fails, which the wiring
rarely does, but a panic on a connection goroutine takes the link down. Fix: run `stoppedErr`
(through `sendable`) in `replyTeam`, before any fetch.

## Nit

- **n1.** `teams.go:291-294`: `drop` answers every waiting send with 64 "not held", including a
  head whose PUT may already have reached the room. The agent may then send it again. Answer
  `queued`-like or 4 `timed-out` instead, so the agent is not told it failed.
- **n2.** `send_test.go:740-762` (SD3-7) cancels the request right after the first 502, so it
  tests a cancelled op, not "every try fails transiently for 30 s: queued at the deadline". The
  path is the same `await` branch, but the test's name claims more than it runs. Use a context
  with a deadline on the fake clock, or rename the test.
- **n3.** `state/store.go`: `Pending.verb`, `Ledger.verb` and `Sending.type/content/tried/ping`
  are new required keys under the strict decoder (a missing key is `schema`), and `Version` stays
  1. A store written by an n3c build is refused whole (`h.refused`, Stopped `store`). This is
     harmless before release. If any n3c-era store exists on a test seat, either bump `Version` or
     note in the plan that n3c stores are not carried over.

## Checked and found sound

- The charge: caps, gate, Admit and the entry are in one write, and nothing is written on a
  refusal (SD3-1 shows that memory does not run ahead of the disk). The forge check comes before
  the Admit. The digest-only gate window is correct.
- The sender has one goroutine per team, ended by `h.cancel`. The hold timers are released on
  every path, and no path loops tightly: every nil-hold return either moves the queue or ends on
  the next `do`.
- The crash windows: before the PUT, the entry is `tried` in the charge or in its own write. After
  the PUT and before the record, the store keeps it `tried`, so a restart recovers it (but see M1
  and m1).
- TIMEOUTs: one `AfterFunc`, re-read against `WallMS`, the `reported` mark in the same write, a
  60 s retry on refusal, never a tick. Down and stopped teams still get theirs. `fromServer`
  keeps TIMEOUTs out of the floors.
- Pruning, `timeoutKeyed`, the late and on-time answer rules (by `due_ms` alone), and the echo
  move all match n3d-D6, n3d-D8 and n3d-D9.
- The roster cache is written once per rev, decoded against seat.json's anchors at `Add`, and
  unlinked at removal.
- Tokens: the composer's `Token` is used only by `SecretShape`, whose refusal detail is the shape
  name. No log line formats a token.
- Exits: `forge-rate` is 14 and `forge-down` is 20, as the brief says.
