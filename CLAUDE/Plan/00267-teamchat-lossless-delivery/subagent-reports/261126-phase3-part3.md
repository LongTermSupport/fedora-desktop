# Plan 00267 Task 3.2, part three: the forge hold

## Built

- `member/teamsync/forge.go`: `forgeRetryEvery` (30 s), `forgeRetryLimit` (10 min),
  `forgeHeldCap` (256); `forgeHeld`, `holds`, `isHeld`, `heldFloor`, `forgeNews.or`. The
  forge-login line now says referenced pings are held, not dropped.
- `member/teamsync/session.go`: `judgeAll` first rechecks the held pings that are due
  (`retryForge`), then judges main (`judgeMain`). A transient failure (`unresolved`,
  `forge-rate`) is held with one log line. A ping that is held already belongs to its held
  entry. `takeMain` is the relay-aware take that both paths share. Main's floor is raised to
  `heldFloor(high, held)`. `commit` stores `high` on the session and the held list on the team.
- `member/teamsync/teamsync.go`: `Team.forgeHeld`.

## Departure from the handoff's design

The hold lives on the Team, not the session. `session.start` resumes from the stored
`next_batch` whenever one is stored. The floor and marker are read only when there is no
position. So a hold kept on the session was lost at every session restart: SD5-r8's 60 s
`Advance` is taken as a freeze and restarts the session, and r8 failed until the hold moved.
For the same reason a link restart still loses a held ping, with no bounce. PLAN.md Task 3.2a
records this and the likely store fix. The coordinator was told, with three options.

## Tests

New: `TestATransientForgeFailureIsHeldUntilItProves`,
`TestATransientForgeFailureIsBouncedAtItsDeadline`, `TestAProvenanceFailureIsBouncedAtOnce`,
`TestPastTheHeldCapAFailureIsDroppedAtOnce`, `TestTheFloorStaysBelowAHeldPing`. Changed to
the hold: SD5-r1, r2 (it now waits on the log, since a held ping alone may persist nothing),
r4, r8, r11 and r12.

Red: with `holds` made to return false, the two transient tests failed, as did r1, r4, r8,
r11 and r12. Green: `go test -race` on the package with and without `-tags busfailpoints`,
`go vet` on both builds, golangci-lint on both builds, and the project rules (the qa driver
as vettool) on both builds.
