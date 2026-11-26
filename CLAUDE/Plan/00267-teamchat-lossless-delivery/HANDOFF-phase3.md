# Handoff: Plan 00267 Phase 3, Task 3.2 (the forge half)

Task 3.2's replay half is built and committed on `v2-smoke` (PLAN.md Task 3.2): a stale item
judged on replay is delivered marked `late:`, and a stale `halt` or `sync` there is bounced
`expired`. What is left is the forge half, DESIGN.md A row 42:

- A transient forge failure (`unresolved`, `forge-rate`) is held and checked again, and is bounced
  only at a deadline. `provenance` stays a drop, bounced at once.

Then tick Task 3.2 in PLAN.md and delete this file.

## Where the code is

- `member/teamsync/forge.go`: `forgeCheck(ctx, f, vs)` sets each verdict that does not prove to
  `Drop` with the reason (and `Answer` nil). `collect` makes every check still running at
  `forgeBudget` (30 s) `unresolved`.
- `member/teamsync/session.go` `judgeAll`: `judgeRoom`, then `forgeCheck` over `jr.vs`, then
  `take` for each event. `take` counts a drop (`j.drop`) and adds the bounce that
  `jr.judge.Bounce(v)` owes. `judgeAll` raises `batch.Floors[main]` to `jr.floor`. `commit` (from
  `settle`, the only caller, after the persist succeeds) applies the judging's effects to the
  session. A failed persist commits nothing, and the next answer judges the same events again.

## The design worked out (not built)

Hold in the session. The floor keeps it durable: no store field, no change to the engine.

- Constants: `forgeRetryEvery` 30 s, `forgeRetryLimit` 10 min (from the first judging),
  `forgeHeldCap` 256 (past it, a failure is dropped and bounced at once, never lost).
- `forgeHeld{id, v (the verdict BEFORE its check: clone jr.vs before forgeCheck), jr roomJudged (for take's judge), next, deadline int64}`. `sess.forgeHeld` holds them. `judged.held` starts as
  `slices.Clone(s.forgeHeld)`, and `commit` sets `s.forgeHeld = j.held`, so a failed persist
  changes nothing.
- In `judgeAll`, a verdict with Drop and `unresolved`/`forge-rate` is held instead of taken: one
  log line, `held <id>: its forge check gave <reason>; it is checked again until <time>`. A
  repeated id is not held twice.
- `retryForge`, in `judgeAll` before its early return on no events: re-run `forgeCheck` on the
  held entries with `next <= now`. A proven one, a `provenance` drop and a failure past its
  deadline go through `take` (with `h.jr`), so they are delivered or dropped and bounced. The rest
  get `next = now + forgeRetryEvery`. Merge its `forgeNews` into `j.forge` (OR both fields), so
  `noteForge` still says forge-login once.
- `heldFloor(jr.floor, j.held)`: for each held entry the floor covers
  (`Covers(v.Event.OriginTs, id)`), lower it to `state.Before(ts)`. Raise `batch.Floors[main]` only
  to that. A link that restarts then re-reads the held ping at resume and judges it again. The
  events after it come back as `seen`.
- Tests to write first, in `forge_test.go`: a transient failure is held (no item, no drop, no
  bounce, the floor not covering it), and after `forgeRetryEvery` and a new answer (post another
  ping) the proven check delivers it; a failure still there at `forgeRetryLimit` is dropped and
  bounced with its reason. Use `w.clk.StepWall` for wall time.
- Existing tests that assert the old drop and must change: SD5-r1 (only provenance dropped; the
  other four held), r2 (no drop), r4 (ten held), r8 (two held; after the window they are
  delivered with the new one: 3 items), r11 (two held), r12 (count the held line's
  `gave forge-rate`).
- Not in this design: `Team.Refetch` (an item restored from pending) still turns a transient
  failure into a drop. Say in the plan whether that path needs the same hold.

## Tooling faults met (reported to the coordinator, not yet filed)

- The `lint_on_edit` hook lints analysistest fixtures under `qa/rules/testdata/src/`, which no
  gate lints: every fixture function is reported `unused`. The existing `storeadd` fixture would
  be reported the same way.
