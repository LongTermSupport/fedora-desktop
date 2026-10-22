# n3e brief review, round 2 (Plan 00209 N3, slice n3e)

Reviewed: `subagent-reports/261022-n3e-handoff.md` at 3864cfbc5, section 15a against round 1
(`261022-n3e-brief-review.md`), SPEC-n3.md section 6 and SY4, TB1, TB4, DESIGN-v2-B-member.md
(2.3 store, 2.7.2-2.7.3, 2.8, by grep), DESIGN-v2-A-protocol.md 11.1 and 11.4, the n3c reports
(p2, p4, p5), and the built code at the base (`go doc`; reads of `proto/verdict.go`,
`proto/limits.go`, `teamsync/session.go` `judgeAll`/`take`, `engine.Config`, `execx`).

## Verdict: FIX-FIRST

15a answers most of round 1 correctly. One major does not hold: the fix for `held back:` counts
the wrong limit, and the limit `held back:` reports is built nowhere. Two mediums need a line
each before the builders start.

Findings: 1 major, 3 medium, 4 low.

## Round 1, item by item

| Round 1                    | 15a's answer                                                   | Holds?                    |
| -------------------------- | -------------------------------------------------------------- | ------------------------- |
| M1 `HeldBack` never filled | count `proto.Judge`'s `rate` refusals                          | **No**, finding 1         |
| M2 coordinator             | `Health.Coordinator *string`, filled by n3e-7's teamsync       | Yes                       |
| M3 unusable counts         | `SetUnusable` in n3e-10, status.json plus the `runtime` record | Partly, finding 2         |
| M4 stall reaches the hooks | `Live` false while stalled, `NotPushed` gets all, Stop blocks  | Yes (B 2.7.3, E15)        |
| 05 earlier run's entries   | declared missing at once, `held`; `missed` dropped             | Partly, finding 3         |
| 06 FIFO / transcript open  | TB1's `O_PATH`, `fstat`, `/proc/self/fd` reopen                | Yes (SPEC-n3 TB1, n3-D17) |
| 07 tagged tests in no gate | one `busfailpoints` row, landed by n3e-6                       | Yes in intent, finding 4  |
| 08 execx                   | `Starter.Start` with `NewSession`, own group kill              | Yes (see finding 7)       |
| 09 hooktext imports client | `Recovery(message string)`                                     | Yes                       |
| 10 n3e-6 size              | `member/deliver` moves to n3e-1                                | Yes (see finding 8)       |

Checked and sound after 15a:

- **M2.** `team.Health` today has `Down`, `Stopped`, `Role`, `LastSyncOKMS`. n3e-3 adds the
  field in wave 1, n3e-7 fills it in wave 2, and n3e-10 reads it in wave 3. `nil` maps to the
  frame's "no coordinator" text. No hook op touches `Roster()`.
- **M4.** No contradiction with OF1: a stall is a local socket fault, not an unreachable server.
  Section 12's "never for items the socket will push" stays true because `Live` is false.
- **06.** `O_PATH` and the reopen are the TB1 pattern, word for word. No project rule enforces
  `O_NOFOLLOW` (grep of `src-go/qa/rules`), so the reopen needs no exemption entry. Nothing in
  the tree already does the `/proc/self/fd` reopen, so n3e-2 builds it.
- **08.** `execx.Started` signals only the child (`Signal(sig)`), so a group kill needs its own
  seam. Choosing `Start` over `Run` is justified by holding a signal that arrives before the
  pid is known.
- **09.** `exits.Error.Message` exists. `client.Down.Error(...)` stays in n3e-4.
- **10.** `deliver`'s types (`proto.Answered`, `proto.HeldBack`, `proto.Verdict`,
  `proto.Delivery.Fit`, `proto.MaxDeliveryEntries`, `state.Pending`, `state.Ping`) are all
  built. Wave 1 stays free of cross-imports, and n3e-6 (wave 2) compiles against it.
- **Waves and ownership.** The `SetUnusable` hunk in `status.go` is n3e-10's (wave 3). It comes
  after n3e-7's pacing hunk (wave 2), so the order is sequential and nothing conflicts.
- **B27.** 15a leaves the palette, the hash, C7, C10, C15 and the `status.json` `member` and
  `colour` fields untouched, so Plan 00217's colours are intact.

## Findings

### Major

1. **Major. `held back:` counts the receive flood limit. The delivery budget it reports is
   unbuilt and owned by no part (15a bullet 1, section 9, n3e-D1).**

   What the code does today:

   - `proto.Judge` returns reason `rate` from `admit`, which is
     `Flood.Admit(sender, Limits.RecvPerSenderMinute, ReceiveWindowMs, now)`. That is A 11.1's
     receive flood limit, 60 per sliding minute.
   - It applies to HUMAN events too (`verdict.go:337-338`) and to agents (`:435-436`).
   - `DeliverPerSender`, `DeliveryWindowMs` and `DeliveryKey` appear only in `proto/limits.go`
     and its tests. Nothing applies them.

   What the design asks for:

   - `held back:` is the delivery budget, `deliver_per_sender`: 10 per (team, sender) per
     600 s, on PEER, PING and member-sent NOTICE items only.
   - It is "counted when the item would enter `pending`", and "HUMAN, TIMEOUT and admin items
     are never held back" (B 2.7.3; A 11.1 row; A 11.4).
   - SPEC-n3 SY4 says "the flood budget is applied at admission (T19)". n3c did not build it.

   As written, 15a has three effects:

   - a human over 60 per minute gets a `held back:` line;
   - a flooding agent fills `pending` at 60 per minute, which is the queue growth A 11.4
     rejects;
   - T19's test passes against the wrong limit.

   **Fix:** n3e-7's teamsync hunk goes in `take`'s `Deliver` case.

   - For an agent sender's PEER, PING or member NOTICE (not HUMAN, not TIMEOUT, not admin or
     carried), call
     `j.budget.Admit(proto.DeliveryKey(team, sender), roster.Limits.DeliverPerSender, proto.DeliveryWindowMs, now)`.
   - A refused item goes in no `Items`. Add one to `Batch.HeldBack[sender]`. The floor still
     moves past it.
   - The budget window is copied into `judged` and committed at persist, as `flood` is.
   - The receive limit's `rate` drops stay ordinary `Dropped` counts.
   - Record in D1 that the window is memory-only, so a link restart resets it.
   - Tests:
     - 11 PEER items from one agent in 600 s give 10 pending and `held back: 1`.
     - 11 HUMAN items give none held back.
     - The window slides at 600 s on the fake clock.
     - A discarded answer counts nothing.

### Medium

2. **Medium. The render count's route can deadlock, and the `runtime` record's change has no
   owner (15a bullet 3).**

   - The deadlock: `engine.Config`'s callbacks run on the state goroutine and "must not block"
     (`OnState`'s doc). 15a has `OnRender` call `CountUnusable("render")`, which writes
     `session.json`, and that "leads to the same call", `SetUnusable(ctx, …)`. `SetUnusable` is
     an engine request served by that same state goroutine.
   - The missing owner: `records.Runtime` has only `Keeper` and `Link`. An `unusable` object is a
     record schema change: its golden, the `status` output, and PK2's record-kind check. No part
     lists it.
   - A simpler route: the `runtime` record is produced by `Config.Runtime`, which the link
     supplies, so the link can fill it with no engine call.

   **Fix:** `OnRender` only records and returns. The link persists the count off the state
   goroutine and never calls back into the engine from the callback. `SetUnusable` carries
   `hook_stdin` for `status.json`. n3e-10's file list gains the `status.go` publish hunk and
   `records.Runtime.Unusable`, with its golden. Add a test: a failed slot under `-race` on the
   fake clock completes, and its count reaches `status`.

3. **Medium. An earlier run's awaiting entries have no owning part, and `held` contradicts what
   `held` means (15a bullet 5).**

   - B 2.3 says `held` marks an item "held from the socket because its delivery was declared
     missing: only `recv` and `wait` take a held item".
   - The new session's socket has failed nothing. Marking the items `held` keeps them off its
     socket for good.
   - With `held` set, the new session's prompt answers the `missing` template ("a delivery did
     not reach the conversation") for a delivery made to another session.

   **Fix:**

   - Name n3e-9 as the owner.
   - Put the rule in the engine's `BeginRun` store write, the same write, so a crash cannot
     split it.
   - Items go back to pending with `repeat` only, not `held`, and count in no `Missing`.
   - Test across `BeginRun`, with a `SIGKILL` before and after the write.
   - If `held` is kept, record the reason in a decision for the lead.

4. **Medium. Round 1 low 14 is a state the brief's own API produces, and it is left
   unanswered.** `Conn.WriteUser` returns `ErrRetry` "when nothing was written". Step 07 of
   section 8 handles only a full write and `ErrPartial`. After step 06 has persisted the awaiting
   entry, an `ErrRetry` leaves the builder to choose. The likely choice, leaving it `unknown`,
   delays the items by up to 1 h and marks them `repeat` although nothing reached Claude Code.

   **Fix (one line in 15a):** on `ErrRetry` at `WriteUser`, one store write drops the awaiting
   entry and puts its ids back in pending with no `repeat`, then retry in 10 s. Add a DV3 test
   row for it.

### Low

5. **Low. The "Go gate row" has no mechanism named (15a bullet 7).** `qa/go-qa.toml` has no
   build-tag setting. A new row is:

   - a `scripts/qa-gates.tsv` row and its wrapper script;
   - the matching `CLAUDE/QA.md` row (the docs gate checks the two agree);
   - a `scripts/qa-routing.tsv` route for `qa-changed`.

   `./internal/teamchat/...` also re-runs every untagged test under `-race` a second time.
   **Fix:** name those files. Limit the row to the packages holding `busfailpoints` files, or
   `-run` their tests.

6. **Low, but the brief prescribes the defect (round 1 low 16, unanswered).** Section 7's OF1
   test still allows "no segment" for an undecodable or oversized `status.json` under a held
   `link.lock`. That is a silent fault, against B 2.8's "seen, not absorbed" and C16. A builder
   following the text builds it. **Fix (in 15a):** with `link.lock` held, a red `teamchat status unreadable`; with it free, the red stopped form.

7. **Low. Half of round 1 medium 08 is unanswered.** S9's working directory (the project
   directory when it exists) and S7's "HOME only if absolute" are still missing from section 7's
   inner-line rules. **Fix:** add both to section 7, item 1.

8. **Low. The `deliver` move leaves the test list and section 1 undivided.**

   - Section 1 still gives DV2 to n3e-6.
   - Section 8's tests still list the mapping goldens, the 8-entry and 12 KiB caps, a single item
     over the cap, HUMAN behind agent in one team, and `Choose` across three teams. These are now
     n3e-1's.

   **Fix:** one sentence in 15a:

   - n3e-1 gets DV2's goldens and `not shown:`, `Fit`'s cap rows and `Choose`;
   - n3e-6 keeps DV1's use, pacing, persistence, failpoints, the stall, and the
     `unusable.render` count.

The other round 1 lows (11-13, 15, 17-19) are wording points or a one-line fix a builder finds
for itself. Round 1 low 12 is harmless: `StateTypes` feeds only the filter and
`trustTriggered`, so a colour event causing a re-verification is how n3e-7 re-reads the colour.
Low 19's n3e-13 driver design can wait for that part's own brief (wave 5).
