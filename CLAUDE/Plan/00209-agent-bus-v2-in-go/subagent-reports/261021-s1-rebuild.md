# Plan 00209 stage 1, rebuilt on F44 (b18e91f5)

Branch feat/bus-v2-s1-rebuilt, not pushed. Base e918261b (the clean plan base), F44 b18e91f5
merged in (fe4b7c00), then the fourteen stage 1 commits cherry-picked in order. Commit 35acceab
is not an ancestor (`git merge-base --is-ancestor 35acceab HEAD` exits 1). Review:
[261021-s1-rebuild-review.md](261021-s1-rebuild-review.md).

## The merge

One conflict, `CLAUDE/Plan/README.md`: F44's index kept whole, the 00209 entry after 00210.

## The cherry-picks and what needed judgement

| New      | From     | Conflict, and how it was integrated                                                                                                                                                                                                                                                                                                                                                                                                          |
| -------- | -------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 813df12e | ce87e241 | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| f4404c81 | d342732b | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| ae50124b | b6404cef | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| da713fdb | 86933d71 | agentbus.go: F44's `WithStderr` kept beside the in-process `SeatList`. world.go: F44's `OnNotARepository` kept, the seat list fake dropped. Not conflicts but broken by it, fixed in the same commit: the board's concurrent fetch tests, F44's two `WithStderr` tests, winddown's seat list fakes, and bus/seat's limits (F44's v1 added `report_per_hour`, which the parity test found). Coverage tests added for the go-qa floor (below). |
| ca54e5c5 | 462d0b88 | declare.go: F44's `systemPython` kept for the runs that stay Python. Tests: every stage 1 line names `/usr/bin/python3`; chooser and find keep `OnNotARepository`; ops drops the `opsHostVars` stage 1 had already removed; the board's `busy` world writes host_vars; the picker header has both F44's wind-down action and stage 1's describe-again undo.                                                                                  |
| bb4fdf3b | 23e0a477 | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| b7f61ba8 | f8e2dfd3 | none textual. `collect.go` merged cleanly: the start-up's shared teams read (V41) sits in `collectTeams`, before F44's concurrent `read`, which is untouched.                                                                                                                                                                                                                                                                                |
| 657edd59 | 26ea1aa8 | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| a3a03265 | a29de4bb | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 817294e0 | b1e6ca98 | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 4c815384 | 267069c3 | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 9dcc1e0c | 5ad32ff3 | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| f9130752 | e88ed44d | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 5d0e1dc9 | d5ff5e4e | none                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| 0d2afc77 | (new)    | `TestAFailedStatusRunIsNotKept` (from f8e2dfd3) scripted a bare `python3` status run; F44 runs `/usr/bin/python3`.                                                                                                                                                                                                                                                                                                                           |

### The concurrent board fetch (F44 Plan 00203 Task 3.13)

`board/fetch.go` is F44's, apart from three comments. Each checkout's seats are still read once,
in the pool, through `Client.WithStderr` and the tape; only the read itself is in-process
(`SeatList` writes its words to the wrapped `Err`, so the tape keeps them in team order). The
fetch tests (`fetch_test.go`) keep every claim F44 made, now for the in-process read:

- read once per checkout under two names, once per checkout: counted through `Client.ListSeats`,
  and no `agent-bus seat list` process ran;
- the seat reads and the rosters run together: the gate (`held`) holds a seat read as it holds a
  command, so four calls must be in flight at once, and peak stays within the bound;
- reports in team order however the calls finish: the first checkout's seat read is made to
  finish after the second's, as the first's seat list was.

`failing`'s seat failure is now a seat its reader refuses (`BreakSeat`), said through the tape.

### v1 drift found

F44's v1 (Plan 00211) added `limits.report_per_hour` (1 to 60) to member.json. bus/seat would
have refused a live member.json that sets it. Added to `limitBounds`, pinned in `member_test.go`;
the parity test against v1's own `cli.main` now runs 175 seat cases, all equal. `declare.py` is
unchanged between the stage 1 base and F44, so bus/declare's parity (2,056 cases) still holds.

### Coverage

qa-changed's go-qa floor (90%) is over the change's reverse-dependency scope, and F44 alone
measures 89.9% on that scope. Added: `RefusalError.Error` (in `wantRefusal`), a
`teamtest.RemoveSeat` test, the root reads' failure and malformed-line paths (agentbus), and
`session list`'s terminal table (hot).

## Tests

- `go test -race ./...` in src-go: green.
- `scripts/test-ccy-teams-command.bash`: passed 383, failed 0.
- `./scripts/qa-changed.bash`: green before each commit made here.
- No test proves less: the only test functions gone relative to F44 are stage 1's own deletions
  (the SEAT and declare marker parsers and their process fakes, reviewed in stage 1); no test F44
  added is removed. F44 checks that a read process ran became checks that none ran and that the
  answer is the file's.

## Timing, this container's fake world (10 runs each, two passes in opposite order)

`untracked/scratch/261021-s1-rebuild-timing.bash`, each revision's own suite fakes and binary:

| Run                        | F44 b18e91f5 | rebuilt 0d2afc77 |
| -------------------------- | ------------ | ---------------- |
| picker open, then Esc (ms) | 675-759      | 141-173          |
| `--list` (ms)              | 113-134      | 114-146          |

`--list` is one pass with one Python run either way (F44 `overview`, now `status`), so it does
not change. In the fake world `agent-bus` is a bash stub, so F44's open does not include v1's
real Python seat list or the host's slower Python; the host saving is larger.

## V42, against F44

The same test (`picker/opencost_test.go`'s open and redraw) run on F44 with the counts logged:

| Processes                     | F44 open | rebuilt open | F44 redraw | rebuilt redraw |
| ----------------------------- | -------- | ------------ | ---------- | -------------- |
| declare.py (Python)           | 5        | 1 (status)   | 1          | 0              |
| agent-bus seat list (Python)  | 1        | 0            | 1          | 0              |
| systemctl                     | 3        | 1            | 1          | 1              |
| git, tmux                     | 3, 1     | 3, 1         | 0, 1       | 0, 1           |
| sudo agent-bus list / devices | 1 / 3    | 1 / 3        | 1 / 0      | 1 / 0          |
| total                         | 17       | 10           | 5          | 3              |

F44's open runs declare.py `overview` 3 times, `limited` and `undescribed` once each. Team reads
on an open: 6 Python runs and 3 systemctl on F44, 1 Python and 1 systemctl now.

## Host check to queue

Unchanged from stage 1, against the deployed F44 binary: after the play that builds ccy-teams
runs, time an open and `--list` on the live teams; compare old and new `--list` byte for byte on
the live host_vars and checkouts (departures B-S1 to B-S6 only); check an open starts one
`declare status` and no `agent-bus seat list`; and a live member.json with `report_per_hour` set
is listed, not refused.
