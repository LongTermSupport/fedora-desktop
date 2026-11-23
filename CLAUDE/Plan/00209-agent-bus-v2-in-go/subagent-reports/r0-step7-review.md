**Verdict: BLOCK.** One defect loses a human's answer; there are two should-fix items and one nit. All file:line references are at `520a0c367`. As you asked, I did reading and targeted greps only: I did not run `qa-all.bash`, `plan-qa` or `go test`.

### Blocker

**1. A worker throws away an undelivered relayed answer every time its sync session restarts, not only after a process restart.** This makes open point (a) worse than the builder described.
- `teamsync/session.go:840`-ish `take` stores the relayed item as `Pending{ID: <human post>, Room: questions}`. The `judgeAll` change at `session.go:753-760` sets `room = PurposeQuestions`.
- At the start of every sync session, `questions.go:212-213` (`qstart`) runs this for any member that is not the coordinator: `s.dropOwed = s.dropOwed || holdsQuestions(store)`.
- `holdsQuestions` at `questions.go:294-298` counts any `Pending` with `Room == questions`.
- The next write then deletes it: `questionswrite.go` `applyQuestions` drops every questions-room `Pending` that is not awaiting. All it leaves is the log line "dropped with the questions room".
- `session()` (`session.go:52`) is "one pass of the loop … the long-poll until an error". So a network blip or a homeserver restart is enough to trigger it.
- The main room's floor has already moved past the relay event, so the answer is never delivered again.
- The `Refetch`/`Fetched` path the design describes for restarts (`DESIGN-questions-v2.md:246-248`) is never reached for these items.

The test misses this. `TestAWorkersRefetchOfARelayedPost` (`teamsync/relay_test.go:112`) calls `tm.Refetch` directly on an empty store. It never puts a relayed item in a store and then starts a session.

**Fix:** make `holdsQuestions` and the `DropQuestions` delete skip relayed items. Either mark them on `Pending` (for example `Relayed`/`RelayedBy`/`Q`, which would also fix (a)'s lost lines), or give them their own room tag. Then add a test that stores a relayed pending item, ends the session, starts it again, and checks the item is delivered.

### Should fix

**2. A relay can be lost to the receiver's rate limit, with no retry.**
- A relay is an ordinary `teamchat.text`, so it goes through `j.admit` (`recv_per_sender_minute`, default 60, `limits.go:33`) and `j.stale` (`proto/verdict.go:~460-467`).
- It is counted against the coordinator's window together with its normal chat.
- Because of (c), the coordinator queues relays straight into `sending` (`engine/relay.go:110`) and skips its own send limiter. So the side that is not limited is exactly the side that can push the receiver over its limit.
- A relay dropped for `rate` or `stale` is gone for good: the worker never re-fetches it and the coordinator never re-sends it.

**Fix:** either exempt a coordinator `relay` from `admit` on the receiving side (the post is already proven human by the fetch), or send relays through the coordinator's send limiter so they cannot burst. Also add a test for a burst of relays.

**3. A relayed entry tells the worker to use a reply command that fails.**
- The golden output `deliver/testdata/human-relayed.txt` prints `reply: teamchat reply '$h…'`.
- The skill says "Answer an entry with the command on its reply: line" (`kit/skill.txt:28`).
- For a worker, that command goes to `replyQuestions` (`engine/reply.go:61`) and is refused with `msgQNotCoordinator` (`questionsverbs.go:56`).
- The skill paragraph at `skill.txt:47-51` contradicts the entry printed directly above it.

**Fix:** for an entry with `RelayedBy`, print `reply: teamchat say --to-coordinator` or print no reply line (`proto/envelope.go:~309`), and update the golden.

### Nit

**4. A seat that is parked when the answer arrives never gets the relay.** `relays()` (`teamsync/relay.go:231-235`) logs "not relayed" and moves on, so the seat does not receive the answer after it is unparked. The parked seat cannot read anything, so nothing leaks, but the answer is silently not delivered. Either note this in the design's known gaps or include it in `questions --for`.

### Checked and clean
- **Provenance chain:**
  - A relay from anyone but the coordinator is dropped with `role` (`checkRelay`).
  - A relay must have exactly one addressable `to`, and must be addressed to this member (`verdict.go:456`).
  - The post is fetched from the questions room the roster names, by this member's own link.
  - `Fetched` requires an `m.room.message` from a roster human, with no state key, not redacted, not carried, and the original sender for an edit.
  - The event ID must equal `relay.id`.
  - A forged relay that names an agent's post is dropped (tested).
  - A worker cannot post in questions: `events_default`/`state_default` are 100 and `m.room.message` is 50 when open.
- **Lockout:** room version 12, so the admin is the creator. Humans and the coordinator are at 50. The rooms engine runs kick, then `writeState`, then invite (`rooms/rooms.go:106`), so power levels are in place before workers are invited and there is no window where a worker could post. `history_visibility` is `shared`, so a member who joins late can read earlier answers.
- **Duplicates:** the transaction ID is derived from the post and the seat. Duplicates are dropped while still in `sending`, and `RecoverSends` keeps the same transaction ID. On the worker, `known[post ID]` stops a second item.
- **Kick race:** a parked seat is skipped at compose time and its token is revoked. A join-on-refusal attempt after a kick fails and is dropped as `provenance`. Nothing leaks.
- **Version bumps:** the Dockerfile label and `REQUIRED_CONTAINER_VERSION` are both 2.84.59, and `CCY_VERSION` is 3.124.8 with a comment. The deployed `SKILL.md` body matches `kit/skill.txt`.
- **Plan state:** `PLAN.md` was updated in the same merge. Its restart claim at design lines 246-248 is wrong, per finding 1.

### Mechanical gates
- `qa-all.bash`, `plan-qa --sweep`, `go test`: not run, as you asked.

Relevant files are under `/workspace/src-go/internal/teamchat/`: `member/teamsync/{questions.go,session.go,relay.go,relay_test.go}`, `member/engine/{questionswrite.go,relay.go,reply.go}`, `member/kit/skill.txt`, `member/deliver/testdata/human-relayed.txt`, `proto/{relay.go,verdict.go,envelope.go,rooms.go}`, plus `/workspace/CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-questions-v2.md`.