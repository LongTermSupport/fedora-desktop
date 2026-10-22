# offline-member-blocks: the four Go instances (fix phase)

Branch `fix/offline-members-go`, from `fix/offline-members-never-block` at 9a91f0b62.

## Instances and fixes

- `src-go/internal/teams/roles/roles.go:367` (B, "Start container"). The entry an LXC handle
  belongs to is now told by the LXC member record first (`Setter.AppliedDir`, wired in `New`
  from `h.AppliedDir`): an entry the record names with a handle is judged by that handle, with
  no checkout read, so a stopped container no longer blocks a role change; an entry held back
  with no handle never joined and is passed over, said as `<user>@<container> is offline (...)`.
  An entry the record does not name is read as before; with a record, a failed read is offline
  and passed over (the last apply did not join it); with no record at all the read failure
  still stops the change (nothing else can say which entry the handle is), now advising
  `ccy-teams apply <team>` and the seat, never starting the container. A checkout that reads but
  does not give an identity (a running member that is wrong) still stops the change.
  `resolve` takes the caller's `said` to name the offline entries.
- `src-go/internal/teams/winddown/winddown.go:58` (B) and `:355` (B). A deferred row, a record
  status `stopped`/`deferred`, and a joined member whose container is not running go to
  `p.idle` as `offline (...)`, not `p.unreached`; the run exits 0 for them. Other record statuses
  (`left`), no record, and no sudo stay not reached. The record loop moved to `plan.byRecord`
  (cyclop). Help text and package doc updated.
- `src-go/internal/teams/shutdown/shutdown.go:332` (C). LXC rows go to `p.lxc` and are resolved
  by the new `resolveLxc` (`lxc.go`): record `stopped`/`deferred` and deferred rows are offline;
  a joined member is judged by the live container list (`lxcfreeze.Lister.Containers`, sudo
  made ready first, as wind-down does): stopped is offline, running stays not reachable and
  fails. A new `p.offline` list is previewed ("Offline, nothing to shut down:"), reported
  (`<member>: offline (...), nothing to shut down`) and counted (`; N member(s) offline`), and
  never makes the exit 1. `Shutdown` gained `Lxc`, `Sudo`, `AppliedDir`.

Specs brought in line: Plan 00203 `SPEC-team-wind-down.md` D4 and Plan 00201
`SPEC-shutdown.md` SD5, each with the departure noted.

## Tests (red first, then green)

- roles: two old tests re-pinned (no "Start container", the apply advice), three new: judged by
  the record's handle with no read; deferred unjoined entry passed over and named; entry the
  record does not name and cannot be read passed over and named.
- winddown `lxc_test.go`: stopped container offline exit 0; record `stopped` offline exit 0;
  record `deferred` row offline exit 0; record `left` still not reached exit 1. The missing
  service session (running, wrong) still fails.
- shutdown `lxc_test.go` (new): record `stopped` offline exit 0 with no lxc-ls; joined and
  stopped offline exit 0; deferred offline exit 0; joined and running still exit 1; no sudo
  still not reached.

Results: `go test -race ./internal/teams/...` all ok; `golangci-lint run` on the three
packages 0 issues; `go vet ./internal/teams/...` clean; the rule
(`tests.helpers.agent_bus_lxc.test_offline_member_rule`) prints no finding under `src-go/`
(the remaining findings are the parent branch's: 00203 acceptance, admin.py, derive.py,
members.py).

## Left for the coordinator

`seats/role.go` and `lxcadd/adder.go` pass a no-op `said`, so the offline entry note is not
shown there (as their live-change lines are not); `hot/role.go` shows it.
