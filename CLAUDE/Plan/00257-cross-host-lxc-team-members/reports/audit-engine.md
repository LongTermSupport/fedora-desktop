# Audit: ENGINE (the member program's command layer)

Scope. Read from the working tree: `src-go/internal/teamchat/member/engine` (send, sender,
outbox, timeout, reply, firstcontact, storewrite, serve, hookview, teams, the hand-out half of
recv, the head of deliverloop), `member/hook`, `member/hooktext`, `member/statusline`
(statusline.go, head of inner.go), `member/agent`, `member/exits`, `member/cli` (run, launch),
`member/wiring`, `member/state` (writer; store caps), `cmd/teamchat`, and the kit under
`files/opt/claude-yolo/optional/teamchat/`. Where a finding needed it I followed the call into
`member/client`, `member/link/hooks.go`, `member/teamsync/verbs.go`, `member/homefs` and `proto`.
Not read in depth: engine `proof.go`, `deliver.go`, `status.go`, `report.go`, `statuspace.go`,
`state/store.go` beyond its caps, `cli/parse.go`, `cli/verbs.go`, `cli/guest.go`, `cli/place.go`.
Tests run, all green: `go test` over `member/engine`, `cli`, `hook`, `hooktext`, `statusline`,
`state`, `exits`, `agent` and `cmd/teamchat`. Every finding below was confirmed by reading the
cited code; none was reproduced by a new test (audit only). `tc/` = `src-go/internal/teamchat/`.

What holds up: store writes are one atomic replace each (temp, fsync, rename, directory fsync:
`tc/member/homefs/homefs.go:207-221`), memory never runs ahead of disk
(`tc/member/engine/storewrite.go:60-84`), a send is marked tried before its PUT and retried by
its transaction id (`tc/member/engine/sender.go:108-140`), a send made while the team is down is
answered queued at once (`tc/member/engine/send.go:643`), and the hook always exits 0
(`tc/member/hook/hook.go:129-134`).

## Findings

### ENGINE-1 (HIGH, CRITICAL if the bus VM's clock is not stepped at resume): deadlines mix the member's wall clock with the server's, with no skew handling

- Where: `tc/member/engine/outbox.go:52` (an echo moves a ping's `sent_ms`/`due_ms` to the
  server's `origin_server_ts`); `tc/member/engine/timeout.go:27-37`, `:57-77`, `:91-127` (the
  TIMEOUT timer and `owedTimeouts` compare those server times with `Clock.WallMS()` of the
  member); `tc/proto/verbs.go:121-135` and `tc/proto/verdict.go:335`, `:433-434` (receive drops a
  ping or text whose `now - origin_ts` is past its age limit, `now` being the member's wall clock,
  `tc/member/teamsync/session.go:744`). Nothing estimates the offset between the two clocks: the
  only "skew" code is the member's own freeze detector (`tc/member/teamsync/teamsync.go:413-447`).
- Failure: the bus VM is suspended for two hours and resumed; its wall clock is two hours behind
  until something steps it. Every event it stamps in that window has an `origin_server_ts` two
  hours old. (a) Sender: the first echo sets `due_ms` two hours in the past, so the TIMEOUT timer
  fires at once and each addressee is reported silent seconds after the ping was posted. (b)
  Receiver: every ping is `now - origin > timeout` (default `ack_timeout_s` 900,
  `tc/proto/limits.go:34`), so it is dropped `stale` and never delivered; texts and human
  messages are dropped too once the lag passes `human_max_age_s` (default 86400). The messages are
  counted as dropped and logged, nothing reaches the session.
- Fix: derive a per-team clock offset from each sync (the server's `Date` header or the newest
  `origin_server_ts` against the request's send and receive times) and judge every server time
  through it; refuse to judge staleness or make TIMEOUTs while the offset is unknown or has just
  jumped. On the host side, step the guest clock on resume (owner of that area). Add engine tests
  with the server clock behind and ahead of the member's.
- Known weakness: not in W1-W9 (W6 covers the suspend but not clocks).

### ENGINE-2 (HIGH): TIMEOUTs are made while the homeserver is known to be down, and answers held in an outbox through the outage count late

- Where: `tc/member/engine/timeout.go:91-127` and `tc/member/engine/recv.go:70` (`eligible` does
  not look at `health.Down`); `tc/member/engine/outbox.go:69` (`late := a.OriginMS > p.DueMS`);
  `tc/member/engine/sender.go:215`, `:222` (a queued answer is stamped by the server when it is
  finally posted).
- Failure: A pings B (`sync`, awaiting an answer) and the bus host is suspended a second later.
  A's link marks the team down, and when `due_ms` plus the grace passes it still writes a TIMEOUT
  item for B, which is delivered to A's session as a fact that B was silent. B meanwhile answered
  in time; its answer was queued (exit 0). After resume the answer is posted with an origin past
  A's deadline, so it is recorded as a late answer. The owner's requirement "timers judged sanely
  across the jump" is not met: an outage of the bus is charged to the addressee.
- Fix: stop a ping's deadline clock while its team is down (hold the due time by the length of
  the outage, measured on the boot clock), or carry the compose time in the answer's content and
  judge on-time by it when the event was queued. Do not make TIMEOUTs while `health.Down`.
- Known weakness: touches W6; not stated there.

### ENGINE-3 (HIGH): a pending item is dropped as stale when it is re-fetched after a link restart

- Where: content of a pending item lives only in memory (`held.content`); after a restart it is
  re-fetched and judged again (`tc/member/engine/recv.go:94-134`, `:201-228`, `:319-321`). The
  re-judge uses the ordinary judge with `NowMs` = now (`tc/member/teamsync/verbs.go:287-290`), so
  the age rule of `tc/proto/verdict.go:433-434` applies a second time, and a verdict that is not
  `Deliver` deletes the item (`recv.go:224-228`, logged "restored item ... dropped").
- Failure: a ping is synced into pending and the session does not read it yet (the agent is in a
  long turn, or delivery is by `wait`). The link restarts (session relaunch, keeper restart, a
  crash, or a session started again after the bus resumed) more than the ping's timeout after it
  was sent (default 900 s). On the next delivery the item is re-fetched, judged stale and removed.
  The store had already accepted it; the session never sees it. No engine test covers a restored
  item past its age (no `stale` case in `tc/member/engine/*_test.go`).
- Fix: an item already admitted to pending is exempt from the age rule on re-judge (judge with
  the time it was first admitted, which the store can hold), or keep the admitted content on disk
  so a restart needs no re-judge for age.
- Known weakness: no.

### ENGINE-4 (HIGH): exit 20 `unreachable` tells the agent "the send is queued", and the code returns it only when nothing was queued

- Where: `tc/member/exits/exits.go:157` ("unreachable: the send is queued and the link retries
  it"), repeated in `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-B-member.md:519`. In the code
  a queued send is exit 0 with `"queued":true` (`tc/member/engine/send.go:402-403`, `:438`,
  `:450-457`), and a send answers 20 only before anything is charged: no verified roster
  (`send.go:219-221`, `:249-251`), no store yet (`send.go:488-490`), or a reply's fetch that failed
  (`tc/member/engine/reply.go:184-216`).
- Failure: a session starts while the bus host is suspended and has no store yet (a new seat) or
  names a member its cached roster cannot place. `teamchat say` exits 20. `teamchat help exit-codes` says the send is queued, so the agent moves on. Nothing was queued; the message is
  lost. The skill's own line for the class ("wait and try once more", SKILL.md:78) contradicts the
  table.
- Fix: decide one behaviour. Either queue in these cases too (hold the uncomposed request until
  first contact), or change the table and B 2.2.4 to "nothing was sent; send it again later", as
  `forge-down` says.
- Known weakness: W6 notes the exit 20 itself, not the false text.

### ENGINE-5 (HIGH): the skill, the help and the coordinator's SessionStart frame teach three verbs the binary does not build

- Where: `teamchat ask`, `thread` and `questions` are listed as agent verbs
  (`tc/member/cli/verbs.go:223`, `:228`, `:232`), taught by the skill
  (`files/opt/claude-yolo/optional/teamchat/plugin/teamchat/skills/teamchat/SKILL.md:63-65`,
  `:89-92`) and named in the frame every coordinator is given at SessionStart
  (`tc/member/hooktext/hooktext.go:36`, `:178`). They are in `unbuilt`
  (`tc/member/cli/run.go:58-60`) and answer exit 1 `internal`, "not built in this binary"
  (`run.go:101-102`). No link op serves them (`tc/member/engine/serve.go:31-67`).
- Failure: a coordinator follows its frame, runs `teamchat ask --team <team>`, gets exit 1, whose
  class is "stop using teamchat for this and tell a human". The only channel the frame gives it
  for reaching the humans does not exist.
- Fix: until the verbs are built, drop them from the frame, the skill and `help`'s agent list (the
  frame text and the verb table can both be driven from `unbuilt`), or build them.
- Known weakness: related to W10 (docs drift), not listed.

### ENGINE-6 (MEDIUM): a send answered "queued" (exit 0) can be discarded later with no word to the session

- Where: `tc/member/engine/sender.go:285-318` (a queued send the re-check refuses is removed,
  logged, `h.unsent++`), `:373-382` (a non-transient homeserver refusal removes it the same way);
  the count surfaces only in `teamchat status` (`tc/member/engine/status.go:288`). On a team's
  removal every queued send is dropped with one log line (`tc/member/engine/teams.go:339`).
- Failure: during a suspend an agent sends five messages, each exit 0 queued. On resume the
  roster has changed (an addressee parked) or the server refuses one; those sends vanish. The
  agent was told success and is told nothing else: no item, no notice, no hook line.
- Fix: make a dropped queued send a pending item of its own (as a TIMEOUT is), naming the
  addressees and the reason, so it is delivered to the session.

### ENGINE-7 (MEDIUM): the outbox has no policy for a long outage: 32 sends, then "tell a human", and no age bound on what is posted at resume

- Where: `tc/member/state/store.go:63` (`MaxSending = 32`); `tc/member/engine/send.go:575-577`,
  `:633-634` (the 33rd send is `store`, exit 78, class "tell a human",
  `tc/member/exits/exits.go:164`); `since_ms` is recorded (`send.go:644`) and read only by status
  (`send.go:798`).
- Failure: (a) an agent that reports progress through a long suspend hits exit 78 and stops
  using teamchat, for a condition that is only "wait". (b) At resume every queued send is posted
  with a fresh `origin_server_ts`, so a request written hours earlier ("sync now", "halt") is
  delivered as new and passes every receiver's age rule.
- Fix: answer a full queue with a wait-class code; carry the charge time in the content and let
  receivers judge age by it; drop or re-confirm queued pings older than their own timeout, telling
  the session (ENGINE-6).

### ENGINE-8 (MEDIUM): one send that keeps failing transiently blocks the team's queue for ever

- Where: `tc/member/engine/sender.go:76-104` (oldest first, one at a time), `:383-387` (any
  failure that is not auth, forbidden or non-transient is held and retried), `:23` (`retryHold`);
  there is no attempt count and no age limit.
- Failure: the homeserver answers one event with a repeating 5xx (or rate-limits it). The head
  entry is retried on every sync for the life of the seat; every later send queues behind it
  (`send.go:642-643`, `older`), and after 32 the team's sends are refused (ENGINE-7).
- Fix: bound attempts or age per entry, then drop it through the ENGINE-6 path and move on.

### ENGINE-9 (MEDIUM): recovery can pair a tried send with an older, identical event and record it as posted

- Where: `tc/member/engine/sender.go:665-691`: with no `transaction_id` on the event (the token
  was reissued, so the server knows it as another device's) a tried send matches any own message
  with equal content. Reports are searched with no anchor over the newest 500 events
  (`sender.go:583-595`, `:616-638`); main is searched past the anchor when the anchor is not
  found in 500 events. Nothing compares the event's `origin_server_ts` with the entry's
  `since_ms`.
- Failure: a member reports the same headline twice ("tests green"). The second report is in
  flight when the bus host suspends; the seat is reissued before resume. Recovery finds the first
  report, pairs it with the second entry, records it posted and removes it. The second report is
  never sent.
- Fix: accept a content match only for an event whose origin is not older than the entry's
  charge time (minus the skew allowance of ENGINE-1).

### ENGINE-10 (MEDIUM): the hook's 300 ms deadline covers fsync'd writes and the engine's queue; on a miss the session is told a false state

- Where: `tc/member/hook/hook.go:112-125`, `tc/member/wire/request.go:60-61`. The link's answer
  path takes its mutex, replaces `session.json` (fsync), asks the engine twice through the state
  goroutine, which may be inside a store write (fsync), and for SessionStart replaces
  `session.json` again (`tc/member/link/hooks.go:62-68`, `:97`, `:102-103`, `:160`). On a miss the
  hook answers from `client.ReadDown`, which for a live link says "teamchat is starting or
  restarting" (`tc/member/client/nolink.go:55-69`, `:89-90`).
- Failure: on a loaded host (the code already allows for CPU starvation,
  `tc/member/engine/serve.go:110-114`) SessionStart's answer arrives late. The session gets
  "starting or restarting" instead of its frame: no handle, no role, no delivery mode. The link
  has already recorded the frame as told (`hooks.go:160`). A missed Stop answer is `{}`, so
  pending items are not announced.
- Fix: answer the hook before the bookkeeping writes (or write them without fsync on this
  path); record `frame_told` only after the answer line was written to the socket; give
  SessionStart a longer deadline than Stop and UserPromptSubmit.

### ENGINE-11 (MEDIUM): important paths with no test

- No engine or teamsync test puts the server clock behind or ahead of the member's (ENGINE-1),
  makes a TIMEOUT fall due while the team is down (ENGINE-2), or restores an item past its age
  (ENGINE-3). `tc/member/engine/timeout_test.go` and `outbox_test.go` use one clock for both
  sides.
- No test drives a full outage through the sender: charge while down, health up, recovery,
  post, with a roster change in between (ENGINE-6) or a 33rd send (ENGINE-7).

### ENGINE-12 (LOW): the hook reads its payload with no bound, and the kit sets no hook timeout

- `tc/member/hook/hook.go:148` passes no `GiveUpAfter`; the 300 ms deadline starts only after the
  read (`:105`, `:112`). `hooks.json` names no `timeout`, so a payload that never ends holds the
  hook for Claude Code's default hook timeout. Fix: bound the read (a few seconds) and answer `{}`.

### ENGINE-13 (LOW): small defects

- `tc/member/agent/agent.go:109-111`: when the kit settings already hold the inbound key, or are
  JSON `null`, the log line prints `read <file>: <nil>`; the cause is not said.
- `tc/member/hook/hook.go:129-134` and `tc/member/statusline/statusline.go:92-95`: an `if err != nil` whose two branches are the same statement. The intent (a failed write keeps exit 0) is
  fine; the conditional is dead.
- `tc/member/engine/storewrite.go:25-35`, `:68`: every store change deep-copies the store through
  `json.Marshal`/`Unmarshal`, then rewrites and fsyncs the whole file; one send is three such
  writes (charge, tried mark, posted). Correct, and a cost that grows with the ledger.
- The skill never says what a `sent` record with `"id":null,"queued":true` means for the agent
  (SKILL.md:61-62, `:68-69` mention `queued` only under `status`).

## Unconfirmed

- U1. Whether the bus VM's wall clock is stepped at resume (decides whether ENGINE-1 is HIGH or
  CRITICAL). Not in this area's code; no evidence either way was read.
- U2. A retry of a tried send by its transaction id relies on the homeserver remembering the
  transaction across a suspend or restart for the same device. If Tuwunel forgets it, the retry at
  `tc/member/engine/sender.go:215` posts a duplicate. Not verified against Tuwunel.
- U3. Whether the link withdraws `frame_told` when its answer could not be written to a hook
  client that already gave up (ENGINE-10); `tc/member/link/server.go` was not read.
- U4. The client waits 5 s past a send's deadline (`tc/member/client/client.go:50`) for the link's
  "queued" answer; whether a state goroutine stalled longer than that can leave the client at
  exit 69 "send cut" for a send that was charged was not traced.
- U5. `--resume <id>` is built from the conversation record's session id with only a base-name
  and leading-dot check (`tc/member/agent/agent.go:141`); whether the record's decoder refuses an
  id that begins with `-` was not read.

## Bloat and debloat candidates

- Half-built questions feature in the command layer: `tc/member/engine/questionswrite.go`,
  `wire.OpQuestions` with a deadline (`tc/member/wire/request.go:50`, `:65`), three verb rows and
  help text, with no served op (ENGINE-5). Either finish it or take it out of the shipped surface.
- Two template-fill mechanisms with separate grammars: `exits.Format`/`Fill`
  (`tc/member/exits/exits.go`) and `hooktext.fill` (`tc/member/hooktext/hooktext.go:66-79`). One
  would do.
- A send's text rules are written twice, with two message sets: `tc/member/client/send.go:90-106`
  and `tc/member/engine/send.go:102-114`. The link must check; the client copy only saves a round
  trip.
- `clone` through JSON (`storewrite.go:25-35`) could be a typed copy; it is on every state change.
- `tc/member/engine` is about 9,000 production lines in one package with one goroutine owning
  sends, delivery, proof, status, hooks and membership. Splitting the send side (send, sender,
  outbox, timeout, reply, report: about 2,800 lines) behind the store-change seam would make
  ENGINE-1 to ENGINE-9 testable in isolation.
