# M4 review (Fable): FIX-FIRST

Branch `feat/hot-control-m4` at 1cff0234, against SPEC-hot-control.md. Read-only; no qa-all, no
commit. The developer's notes: [261016-m4-opus.md](261016-m4-opus.md).

Verdict: **FIX-FIRST**, for two findings (B1, B2). Both are small. The rest of M4 holds: the
lock order cannot deadlock, no lock survives a reboot, a crash at any step of move, role or
remove is finished by the same command again, H26 is sound and cheap, the merge kept every F44
change, the versions are bumped, the queue is exact, and no install-specific name entered a
tracked file.

## Tests run

- `go test ./internal/teams/... ./internal/agentbus/... ./internal/sessprobe/...`: every package ok.
- pytest over `tests/helpers/pingbus` and `tests/helpers/agent_bus`, each tree on its own (the two
  trees both have a `test_cli.py`, so one pytest over both fails to collect; the project's runner
  names modules and does not hit this): pingbus 1524 passed, agent_bus 585 passed.

## Blocking

### B1. The change lock is inside the directory the container has mounted, so the session can hold it for good

`src-go/internal/teams/hot/hot.go:390` takes `.change.lock` in `membership.Dir(runtime, session)`,
the directory `lib/agent-bus-membership.bash:54` bind-mounts read-only at `/run/ccy/membership`.
`flock(2)` needs no write permission: a lock is taken on a descriptor however the file was
opened, and a bind mount shares the inode. So a process in the session's container can
`open("/run/ccy/membership/.change.lock", O_RDONLY)` and `flock(LOCK_EX)` and hold it until the
container stops.

Scenario: an agent in the session (hostile, or a stray tool call) holds the lock. Every owner
command on that session is refused with exit 75, "another change to it is in progress: another
ccy-teams that has not said what it is yet": `session remove`, `move`, `role`, `repair`. H29's
`ccy-teams remove <team>` then refuses to take the team down ("still in <team>"). The only way
out is to stop the session. This is the hole question 4 asks about: nothing in the container
changes membership, but the container can freeze the owner out of controlling it.

The branch already made this argument for the other lock (settled gap 01: "a container could hold
it for good") and moved the allocation lock to `$XDG_RUNTIME_DIR/ccy/alloc/`. The same reasoning
applies to `.change.lock`; the spec's H20 wording put it in the membership directory before the
mount's reach was weighed.

Fix: take the change lock at `$XDG_RUNTIME_DIR/ccy/change/<session>.lock` (beside `alloc/`, same
`takeLock`), never under the mounted directory; a `lock_test` case that the path is not under
`membership.Dir`; H20's bullet and the "As built in M4" note updated. The relaunch request M3
writes into the same directory is host-written and only read by the host, so it is unaffected;
it is worth one sentence in the spec saying the mounted directory carries only files the
container may see.

### B2. H28's restore skips the desired-file removal when the record write fails

`src-go/internal/teams/hot/add.go:335`:

```go
if x.writeRecord(t, seats, "restored") != nil || x.dropDesired(t) != nil {
```

`||` short-circuits: when `SetTeams` fails, `dropDesired` never runs, and the desired file stays,
naming a seat for a session that is not running. H28: "Neither leaves a desired file for a
session that is not running." Scenario: the session ends during the add (container killed, or
`/exit`), and its record cannot be written at that moment (removed by the ending session's own
registry cleanup, or any registry write error). The next launch of that name writes generation 1
from the record, but until then `session add` on another session reads that file as a claim
(`claimsElsewhere` counts only live sessions, so this one is harmless there, but `agent-bus seat choose` offers the seat as held while the container is still listed, and `session list` shows a
stale membership).

The same shape is in `restore()` (`add.go:327` via `commit`, `hot.go:459`): a failed record write
skips the desired restore, so the keeper takes the seat the record no longer names, and a
restart drops it again.

Fix: run both steps and join the errors, in both places; a test where the record write fails
and the desired file is still removed (restoreEnded) or still restored (restore). Acceptance (f)
cannot catch it: `grep -qF "${M4_TEAM_B}" "${M4_RECORDS}/${M4_A}"` also passes when the record
file is absent (see A3).

## Not blocking, should be fixed in the same round

### N1. A no-op `session role` prints `✓`, not `-`, in production (H19)

`src-go/internal/teams/hot/role.go:166`: `if !changed && x.Now().Equal(start)`. `start` is one
`Now()` call and the condition is another; under the test clock (`hot_test.go:105`, advanced only
by `Sleep`) they are equal, on the wall clock they are nanoseconds apart. So the real command
prints `✓ session: told it is alpha's coordinator (0.0 s)` and never "Nothing to do" for a role
that already holds. `role_test.go:28` pins the behaviour only on the frozen clock, and
acceptance (a) `role-again` asserts no `✓ (role|record|membership)`, which does not cover the
`session:` line. Fix: a `first` boolean for the first poll; a test whose clock advances on
every `Now()`.

### N2. A coordinator removed while its homeserver is down keeps the role (hidden decision)

`role.go:190` skips the role when the homeserver is down ("role: not checked"), `remove.go`
releases the seat, and the owed-farewell re-run (`removeFrom` with `!in`) never returns to the
role. The team record then carries a coordinator whose seat is free. H26 says the goodbye and
status stay owed; it says nothing about the role, so this was settled by omission. Either the
role is owed too (a `role` field on the farewell entry, demoted on the re-run), or the remove of
a coordinator while down is refused without `--coordinator=demote`, which then also stays owed.
Put it to the owner as part of the H26 wording; the first is what H19 would expect.

### N3. `move --coordinator=hand-to=<seat>` that does not resolve leaves the session in both teams for a usage error

`move.go:34` `coordinatorSaid` refuses before the add only when the option is absent. An option
that names no member, or two (`handTo`, `role.go:232`), is exit 64 from `leaveRole`, after the
add has joined the new team. H24 allows a failed remove to leave both, but a knowable input
error should not get there. Fix: resolve `handTo` in `coordinatorSaid` as well.

### N4. "Live" differs between the two allocators (settled gaps 02 and 03), and the restart window is uncovered

ccy-teams counts a session as live when its tmux session exists and honours a stopped session's
`restore=yes` record (N4); `agent-bus seat take` and `seat choose` (`helpers/agent_bus/checkout.py`
`named_elsewhere`) count only a running `ccy-session` container and read no records. H21 says
both apply the one rule. Consequences: the launcher's chooser offers a seat that a stopped
restorable session will replay; and a session in its age-restart or first-join relaunch window
(tmux alive, container down) has a desired file that ccy-teams honours and agent-bus does not,
so a `ccy --teams` launched in those seconds can take its seat and the returning keeper is
refused `held-elsewhere`. The reason given (the launcher's own file must not block it) is right
for its own session only. Fix: agent-bus excludes the session being launched by name (the
launcher knows it), and otherwise applies the same two tests; or the spec records the asymmetry
and its window as accepted. Owner-visible either way.

### N5. `allocation()` runs with no lock when there is no runtime directory

`checkout.py` `allocation`: `if not system.runtime_dir ...: yield; return`. ccy-teams refuses the
same condition (`hot.go:361`). A silent unlocked allocation is the "skip and continue" shape; at
least say so on stderr, or refuse as ccy-teams does.

## The acceptance

What it proves is right for (a), (b), (d), (e): each check is judged from exit codes, step lines,
the membership files, `agent-bus seat list` and the roster, never prose. Teardown is sound:
`m4_cleanup` is registered before `m4_prepare`, `M4_TEAMS_PRESENT` is set before the installer
loop so a half-built pair is purged, `m2_cleanup` removes a dead container (`podman ps -a`,
`rm -f`) and a dead tmux session, and the seat directories and reports go last. Three points:

- **A1 (c), a race.** The second add starts once the first has said "running, can be
  hot-controlled". The first then needs only a free existing seat, the reach probe and the
  keeper's apply (about 1-3 s). If it finishes before the second reaches `takeLock`, the second
  exits 0 "Nothing to do" and the leg fails for the wrong reason. Make the first hold for a
  known time: kill A's keeper first (as (f) does) so the first add waits its 10 s at the apply
  step and is rolled back, then assert the second's 75 inside that window; or accept and note
  the flake risk in ACCEPTANCE.md.
- **A2 (d).** "A's status line, rendered in its container" is `pingbus status-line` under
  `podman exec` with `PINGBUS_HOME` set by hand, the shape the 00201 fix moved away from. It
  proves the code path with a real mark, which is what (d) needs; say so in ACCEPTANCE.md rather
  than "as its claude renders it".
- **A3 (f).** `grep -qF team record` passes when the record file is absent; check the file
  exists first, or the check is vacuous exactly in the case B2 describes.

The M3 launch fix (no `exec`, the trailing status write) and `m2_adopt`'s third argument are
right; `m2_ended_early` now names the right files. The `assertGone` fix is correct (ESRCH between
the open and the read of `/proc/<pid>/stat`) and its forcing test is a fair proof.

## The settled gaps

| #   | Decision                                      | Sound?                                                                    |
| --- | --------------------------------------------- | ------------------------------------------------------------------------- |
| 01  | allocation lock under the runtime directory   | Yes; apply the same to the change lock (B1)                               |
| 02  | "live": tmux for ccy-teams, container for bus | Two definitions; N4                                                       |
| 03  | only `restore=yes` records count              | Right for ccy-teams; agent-bus does not apply it (N4)                     |
| 04  | `hand-to` resolution                          | Yes                                                                       |
| 05  | `add --role=coordinator` needs demote         | Yes, consistent with `role`                                               |
| 06  | interactive coordinator remove: one question  | Yes for M4; M5 owns the menu                                              |
| 07  | move's coordinator check before the add       | Incomplete (N3)                                                           |
| 08  | `session list` states and sources             | Yes; the report is never authority, the lock is cross-checked             |
| 09  | role not told in 10 s: exit 1, role stays     | Yes, and said                                                             |
| 10  | `ccy-teams remove` terminal-only, demote      | Yes                                                                       |
| --  | H26 300 s staleness                           | Yes: the backoff caps at 30 s, so a live watcher refreshes well inside it |
| --  | coordinator removed while down keeps the role | Hidden; N2 for the owner                                                  |

## Correctness of move and role (question 1)

- Lock order is change, then allocation, both `LOCK_NB` in Go; `agent-bus seat take` waits up to
  15 s on the allocation lock holding nothing else; `--alloc-held` skips it when ccy-teams holds
  it. No cycle. Both locks are flocks under `$XDG_RUNTIME_DIR` (tmpfs): a crash releases them,
  a reboot removes them. The holder's line makes a refusal name the pid.
- Move is add then remove under one change lock. A crash between the record and the desired
  write, after the add, or between the farewell write and the goodbye, is finished by the same
  move again (`alreadyIn`, the owed-farewell path), as H19 and H24 require. N3 is the one half
  state reached by a knowable input.
- `repair` is sound: `cmd_seat_keep` defaults `--root` and `--home` to ccy's paths and reads
  `PINGBUS_MEMBERSHIP` from the container's environment, so the bare `podman exec -d` works, and
  `keeperAlive` and the acceptance's `m4_keeper_pid` match the same `pingbus ... seat keep`.

## H26 (question 3)

The watcher marks on `MatrixError` only (a full disk is not "down"), refreshes on each retry,
clears on the next successful sync and in the thread's `finally`, so a hot remove clears it. The
status line does one `lstat` per team per render and ignores anything that is not a regular
file younger than 300 s; the mark and its directory are 0600/0700, opened `O_NOFOLLOW`. A
SIGKILLed watcher leaves a mark for at most 300 s, which is the stated design. No race matters:
the worst is one render showing `(down)` a second late.

## Owner-only (question 4)

`agent-bus seat take` is host-only; `TakeOut` runs only inside `ops.Remove`, which is
terminal-only; the keeper's report is used for `told` roles and for choosing the relaunch path,
never to decide a hold (`waitHeld` requires the lock as well). The one gap is B1: the session
cannot change its membership, but it can freeze it.

## Merge, versions and queue (question 6)

- `git diff d7e6344e e4d1adc6` over every file F44 touched (`helpers/pingbus/seat.py`,
  `tests/helpers/pingbus/test_seat.py`, the 00201 and 00161 plan files, `m2-check.bash`,
  `m2_inbound.py`) is empty, and none of them is touched again by 1cff0234; F44's `add.go` hunk
  (`rep.Inbound`) is present. The launcher identity fix predates the branch point (291d78e9).
- `CCY_VERSION="3.99.10"`, `REQUIRED_CONTAINER_VERSION="2.75"`, the Dockerfile matches, the
  changelog entry covers 3.99.9 and 3.99.9.1.
- `meta-deploy.list` holds exactly: `play-claude-yolo.yml`, `play-agent-bus.yml`,
  `00203-teams-hot-control-and-robustness`, the 00201 `acceptance.bash`, the 00161
  `acceptance.bash`, each with its description. Its 00203 description should mention B1's new
  lock location once fixed (it describes the locks).

## Names (question 7)

A regex scan of every added line in `d7e6344e..1cff0234` (home paths, non-example e-mail
domains, session and container name shapes, private subnets, owner and company names) found
nothing. The pre-commit's denylist scan needs the gitignored `localhost.yml`; I did not open it.
