## 19:39 · action · —   — ccy-teams --graph: red then green

Task 15.1 (issue #111 step 1): `ccy-teams --graph`.

Spelling: `--graph`, beside `--list` (both print the board; the other modes are verbs).

Source: the same `board.Collector.Collect` pass as `--list`; the snapshot now keeps each
team's LXC member record (`Snapshot.LXC`) so LXC members can be grouped by container.
`board.Mermaid` is a pure formatter over the snapshot.

Live state: ccy-teams has no pingbus listening status on the host (`pingbus status`
reads a member's own state, inside the member), so the graph uses what `--list` knows:
a seat is online while its session runs or a live process holds it, else offline; a parked
member is offline; an LXC member is judged by the last apply's record (stopped, deferred:
offline; joined: unknown, its container is read as root, never asked for); a member of
another machine is unknown. No "stale" is invented. Rooms (reports, questions) are not
known to the team code, so not drawn.

Failure: as `--list` (shared `sayUnshown` and `verdict`): need sudo or an unreadable part
is drawn with why and exits 1.

Red: with a stub `Mermaid` returning "" and `Graph` returning 0, the 9 tests in
`board/graph_test.go` failed; the 3 new or changed app tests (`TestUsageErrors` for
`--graph x`, `TestDispatchGraphIsTheBoardAsAFlowchart`, `TestHelpNamesTheGraph`) failed
before the wiring. Green: all pass, `go test ./internal/teams/...`, golangci-lint clean.
