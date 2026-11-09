## 09:39 · action · —   — 3.20 green: wind-down compacts each session

Task 3.20 green. `go test -race ./internal/teams/... ./internal/compactfloor/... ./internal/sessions/...` passes and `golangci-lint run` over the same reports 0 issues. Coverage is 93.7% for winddown and 100% for compactfloor.

The code:

- `src-go/internal/compactfloor` is the floor's one parser, moved from sessions, which now uses it. `FromUnit` reads the floor from `systemctl --user show --property=Environment --value ccy-sessions-set-going.service`.
- `winddown/compact.go` holds the rest: the transcripts are found before anything is typed (`conversation.For`, `transcript.Dir`/`For`). Once a session is idle again after the wind-down (seen busy, or idle for the settle bound), its context is read. At or above the floor it is queued, and `/compact` is typed at an idle moment through 3.14's typeLine and reenter. A host session ends on `CompactedSince`; an LXC member on its pane going idle again.
- `windstep.go` gains the phases and the scheduler `nextCompaction`: one at a time, the coordinator by priority.
- A 15-minute `Waits.Window` bounds everything.

Three branch tests were added after green: a floor that is not tokens, a turn that never came, and a `/compact` stuck in the box. usage.txt and docs/agent-bus.md now say what wind-down does with compaction.
