# Plan 00209 Task 2.2 stage 1: src-go/internal/bus/seat and agentbus.SeatList, builder notes

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Uncommitted in the worktree (branch feat/bus-v2-s1-declare); the coordinator commits.

## Files

- `src-go/internal/bus/seat/seat.go`: package doc, `Seat`, `Listing`, `RefusalError`, `TopLeveler`,
  `Checkout`, `List`, the owner checks (`_real_dir`) and the tree walk (`list_seats`).
- `member.go`: member.json's decoder (config.read_member), with a comment saying
  internal/bus/state will own it and the file moves there. `baseurl.go`: check_base_url and the
  urlsplit subset it needs (same move).
- `lock.go`: the seat.lock probe. `pytext.go`: python's path rendering, sort order, strerror.
- Tests: `seat_test.go`, `member_test.go`, `baseurl_test.go`, `lock_test.go`, `pytext_test.go`,
  `parity_test.go`, `fixture_test.go` (shared builders).
- `src-go/internal/agentbus/agentbus.go`: `SeatList` switched; `agentbus_test.go` updated.

## Exported API

```go
package seat
type Seat struct{ Team, Seat string; Held bool; Handle string }
type Listing struct{ Seats []Seat; Failures []string } // "seat <seat>@<team>: <reason>"
type RefusalError struct{ Msg string }                 // v1's exit-78 class, v1's words
type TopLeveler interface{ TopLevel(ctx context.Context, dir string) (string, error) } // gitx.Git
func Checkout(ctx context.Context, git TopLeveler, dir string) (string, error)
func List(top string, uid int) (Listing, error)
```

Outcomes: a `Listing` (Failures non-empty is v1's partial 78); a `*RefusalError` (whole list
refused, 78); any other error is the crash class (B-S3: v1's uncaught exceptions).

`agentbus.Client.SeatList(ctx, dir) ([]Seat, bool, error)`: same signature and meaning. A
named dir runs no process (EvalSymlinks of its absolute path, B-S2); `""` runs ONE
`git rev-parse --show-toplevel` through `gitx.New(c.Runner)` (gitx owns git under
project_seam_commands). Each failure line and a whole-list refusal are written to `c.Err` as
`agent-bus: <text>\n`, unreadable true, err nil (refusal returns `[]Seat{}`). A crash is an
error, `agent-bus seat list: ...` (board's readSeats then reports it through reportErr, as it
reports any non-CommandError). Self is never set (V35). A write error on `c.Err` is returned;
`c.Err == nil` drops the lines. New unexported field `Client.uid func() int` (nil = os.Getuid),
set by the package's tests. `ParseSeats` is no longer called by SeatList; it is left in place
(exported, still tested) for the integration step to keep or delete.

## Red / green evidence

1. RED (seat): all `_test.go` first; `go test ./internal/bus/seat/` failed to build:
   `undefined: Listing`, `undefined: memberFile`, `undefined: lockFile`, `undefined: probeLock`,
   ... `[build failed]`.
2. GREEN, first run with sources: everything passed except what the v1 oracle caught:
   - member.json that is a DIRECTORY: v1 says `member.json cannot be read (Is a directory)`, not
     `is not a regular file`: python's `os.fdopen` refuses a directory with EISDIR before the
     S_ISREG check. My expectation (read from the code) was wrong; the Go now follows v1 and the
     unit case was corrected. A FIFO still gets `is not a regular file`.
   - strerror: Go's errno table lacks 41, 58 (glibc `Unknown error N`) and 133 (EHWPOISON,
     `Memory page has hardware error`); added. `TestStrerrorIsPythons` compares all 1..133.
3. Lint restructure (thelper on table closures: builders now take an `fx` fixture value; funlen
   split of the case tables; no named returns; cyclop/nestif splits; `filepath.Clean` for G304).
   Still green.
4. RED (agentbus): new tests first; vet failed `r.c.uid undefined (type *Client has no field or method uid)`. GREEN after the switch. `TestStartFailureIsNotACommandError` used SeatList only
   as a user command that could fail to start; it now uses SeatRemove (still a process).
5. Final: `go test -race -cover ./internal/bus/seat/ ./internal/agentbus/`: seat 95.7%,
   agentbus 95.7%. `golangci-lint run ./internal/agentbus/ ./internal/bus/seat/`: 0 issues.
   No //nolint, no discarded errors, no wall clock; every process through execx (flock, git,
   python3 in tests).

## Parity (TestParityWithV1)

One `python3 -P -c` with `PYTHONPATH=<repo root>` runs v1's own `cli.main(["seat", "list"])`
(exit mapping, `agent-bus: ` prefix, stderr encoded backslashreplace as python's stderr does)
for every fixture, each a `git init`ed temporary checkout (v1 asks git), with the uid passed
through `dataclasses.replace(checkout.real_system(top), uid=...)`. Required: same exit code,
stdout and stderr byte for byte; where v1 raised (crash), the Go must return a crash-class error.

- 36 fixture checkouts, 16 of them v1 crashes (`.claude` a file, pingbus a file, seat.lock a
  socket (ENXIO), member.json nested 30000 deep (RecursionError), 11 base_url values urlsplit
  raises ValueError on, plus a team dir name undecodable is a refusal, not a crash).
- 172 seat cases in one checkout: every member.json case of `member_test.go` (file kinds,
  symlinks, 65536/65537 bytes, bad UTF-8, encoded surrogate, BOM, duplicate keys top and
  nested, 5000-digit int, unknown keys with name / non-name / 32 / 33 letters / six / code-point
  order, every required key missing, every field rule, user_id forms, plain_http_hosts forms,
  limits bounds under/over for all seven keys, bools, floats, strings, huge ints, 56 base_url
  forms incl. uppercase scheme, ports 0/00443/65535/65536/+1/1:2, brackets, IPvFuture, zones,
  http to listed / unlisted IPs) and 8 lock cases (missing, unheld, held -x by a second process,
  held -s, symlink, directory, FIFO, member refused before lock).
- Tree fixtures: no ccy, ccy symlink / file, no seats dir, empty tree, order, someone else's
  checkout (uid+4242), every whole-list tree refusal, undecodable names (`\udcff`), python's
  code-point sort (U+DCFF before U+E000).

## v1 behaviour not matched, and why

- Two member cases are set aside from parity (`notOn311`): IPv4-mapped plain_http_hosts. The
  container's python 3.11 writes `::ffff:c000:201`, the host's 3.13+ writes `::ffff:192.0.2.1`;
  the Go follows the host (B-S5, via pycompat). Their expected results are asserted by
  `TestReadMember` directly.
- Recursion depth: pycompat refuses nesting > 990 (RecursionError); python's limit depends on
  its stack, so a depth between roughly 900 and 1000 may differ. Only "very deep" is proven.
- Crash detail: v1 printed a traceback (exit 1); the Go returns an error with its own text. The
  class is matched, not the words (B-S3).
- git's stderr in the "not in a git checkout" refusal: v1 ran git with `LC_ALL=C`; gitx does
  not set it, so a non-C locale could word git's message differently. gitx is not this task's
  file; flagged for the integration step.
- A named dir that does not exist: v1's wrapper process could not start there (a Go error, not
  a CommandError); the Go returns a crash-class error the same way.
- Unreadable directories / files as non-root (EACCES) are tested only when not root (skipped
  in the container, which runs as root); the seats-tree EACCES case has no test (gosec refuses
  the chmod back to 0700 in a test, and root cannot be refused).

## Tests outside these packages that fake `agent-bus seat list` (now broken; NOT migrated)

`go test ./internal/teams/...`: 141 failing tests in 9 packages: `internal/teams` (app_test.go),
`teams/board` (collect_test.go, list_test.go, redraw_cost_test.go), `teams/choose`
(choose_test.go), `teams/hot` (hot_test.go), `teams/login`, `teams/picker`, `teams/restart`
(helpers_test.go fakeSeatList, restart_test.go), `teams/seats` (seats_test.go),
`teams/shutdown` (helpers_test.go fakeSeatList, shutdown_test.go). Also the bash suite
`scripts/test-ccy-teams-command.bash` (its fake agent-bus answers `seat list` from
`$STATE/seats/<checkout name>`; 15+ FAIL lines). redraw_cost_test.go counts
`"agent-bus (user) seat list": 1`, which is now 0 processes.

## QA

`./scripts/qa-changed.bash` from the worktree (log `untracked/scratch/261021-s1-seat-qa-changed.log`):
fails on (a) the teams packages and the teams-command suite above (expected, integration step),
and (b) `internal/bus/declare` `TestParseRefusals`, the other agent's package, in progress.
The two packages of this task pass; every other suite it ran passed.
