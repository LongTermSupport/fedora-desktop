## 10:08 · action · —   — Task 3.20 review fixes: green

Task 3.20 review fixes, green. `go test ./internal/teams/winddown/` passes, the five red tests
included.

- F1 (`compact.go` `compacting`): a host session idle again with no `compact_boundary` sets
  `boundaryBy` to the settle bound (`Waits.Settle`, 30 s) and its transcript is read on every
  look until then; a busy look clears it; the fault comes only when it passes, and now says
  "within 30 s of its end".
- F2 (`compact.go` `takeTurn`, `windstep.go` `nextCompaction`): a session given the turn has
  `turnBy` (the settle bound). A look that is not Ready past it sends the session back to the
  queue with one more `passes`, never interrupted; `nextCompaction` gives the turn to the
  fewest passes, then the coordinator, then the first. One never compacted is named at the
  window's end: "its box was not idle and empty when its turn to compact came".
- U3: `aboveFloor` (shared with `answered`) reads the context again when the turn comes; a
  session compacted while it waited is finished below the floor.
- U2: `windowEnded` reads a compacting host session's transcript once more first (`recorded`).
- U5: covered by the floor, `TestASecondWindDownDoesNotCompactAgain` the guard.

SPEC section 7 WC3, WC4 and WC4a say so; PLAN.md Task 3.20 records the fixes and leaves the LXC
evidence and the slow-hook judgement as unticked follow-ups.

Gate: `golangci-lint fmt`, `golangci-lint run ./internal/teams/... ./internal/compactfloor/...`
0 issues; `go test -race` over both passes; coverage winddown 93.8%, compactfloor 100%.
