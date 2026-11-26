# Handoff: Plan 00267 Phase 3, Task 3.2

Issue #443 and Task 3.1 are built and committed on `v2-smoke` (see PLAN.md Task 3.1 and the
journal of 26 Nov). Task 3.2 is not started. This file is what the next worker needs.

## What Task 3.2 asks (PLAN.md, DESIGN.md A rows 20, 37, 38, 42 and the Decisions)

1. A transient forge failure (`unresolved`, `forge-rate`) is queued and retried, and bounced only
   at a deadline. `provenance` stays a bounce.
2. Staleness never drops an addressed item judged on replay (rows 20, 38): it is delivered
   marked late, with its send time.
3. A verb meaningful only while its sender waits, whose sender has given up, bounces `expired`.

## Where the code is

- Forge: `member/teamsync/forge.go`. `forgeCheck` turns a check that does not prove into a Drop
  with proto's reason (`collect` marks every check still running at `forgeBudget` (30 s)
  `unresolved`). `take` (`member/teamsync/session.go`) then counts the drop and, through
  `Judge.Bounce`, queues a bounce. A retry needs the event judged again later. Nothing keeps a
  dropped event: the floor passes it. Two ways to look at: hold the event as pending with a
  forge-pending mark and `NotBefore` (Task 3.1's field, `state.Pending.NotBefore`, wall ms),
  re-checked before delivery; or keep the room's floor below it so the next sync fetches it
  again. The deadline needs a field too. Take the store's size budget into account
  (`sizePending` 464, `state/store.go`) and the store rule `checkPending`.
- Stale: `proto/verdict.go` `stale` (rule 10 for agent pings and text), `TextStale` and
  `PingStale` (`proto/verbs.go`), and the human branch (`main && TextStale` gives a
  `ReasonStale` drop). Replay is known only in teamsync: `rj.replayed(originTs, id)` in
  `judgeRoom` (`session.go`), which gives replayed history its own flood window. The judge needs
  to be told that an event is replayed (a `Judge` field set per event, as `ej.Flood` is) so that
  it delivers instead of dropping. "Marked late with its send time" is delivery text
  (`member/deliver`, `proto/delivery.go`). That text is pinned by the routing and delivery
  corpora, so it is a protocol text change.
- Expired: a ping verb whose answer timeout has passed (`PingStale` by verb). "Its sender has
  given up" needs the sender's ping timeout, which the ping carries. `ReasonExpired` must be in
  the bounce reasons of `proto/receipt.go` (check `BounceReasons`) and in `Judge.Bounce`.

## What Phase 2 and Task 3.1 give you (reuse, never a second ledger)

- A bounce at the engine: `bounce(s, team.Bounce, now)` in `member/engine/storewrite.go` (drop
  ledger plus owed receipt, once per event).
- A bounce at the verdict: `Judge.Bounce` returns the reason a Drop owes, and `take` appends it
  to `batch.Bounces`.
- A held item: `Pending.NotBefore`. The delivery loop (`lookAt`, `deliverloop.go`) waits for it;
  `recv` does not.

## Tooling faults met (reported to the coordinator, not yet filed)

- The `lint_on_edit` hook lints analysistest fixtures under `qa/rules/testdata/src/`, which no
  gate lints: every fixture function is reported `unused`. The existing `storeadd` fixture would
  be reported the same way.
