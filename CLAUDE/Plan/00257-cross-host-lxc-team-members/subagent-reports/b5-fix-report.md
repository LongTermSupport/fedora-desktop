# B.5 review fixes

Plan 00257, the fixes the independent review of Task B.5 asked for (`b5-review.md`), with the
coordinator's rulings under the owner's "all weaknesses resolved". Worked in place on branch
`plan-xhost-lxc-members`. Commits are named by their subject; `git log` gives the SHAs.

| Item | Finding                                            | Result                         | Commit                                                                                  |
| ---- | -------------------------------------------------- | ------------------------------ | --------------------------------------------------------------------------------------- |
| 1    | ADMIN-1 not closed (blocking)                      | fixed                          | `the ctl peer check takes the unit's exact system cgroup` (394a17bb3)                   |
| 2    | the per-source cap and resume                      | fixed (coordinator ruling)     | `the terminator's per-source cap is the team's, dead connections reclaimed` (0e2d38a60) |
| 3    | HOST-4 deferred with no owner                      | Task B.2d (coordinator ruling) | `B.2d, the B.5 review rulings, HOST-7's warning, the fix report`                        |
| 4    | user docs say the terminator relays all bytes      | fixed                          | with item 2                                                                             |
| 5    | HOST-7 can refuse what applied before              | said so                        | with item 3                                                                             |
| 6    | gate nits                                          | fixed                          | the same commit as item 2 (0e2d38a60)                                                   |
| 7    | flaky `TestT10ASilentClientIsClosedAtRequestLimit` | fixed (DBF)                    | `Defence Before Fix ... project_shared_due across a package's limits` (b0d043d26)       |

## 1. ADMIN-1: the exact system cgroup

`ctl/peer.go` now requires the process's cgroup v2 line (`0::<path>`) to equal
`/system.slice/system-teamchat\x2dserver.slice/teamchat-server@<team>.service`. That is the path
systemd gives an instance of the system template: its default slice is
`system-<template prefix, unit-name escaped>.slice` (`unit_set_default_slice`, `src/core/unit.c`;
the escaping is systemd-escape's, systemd.unit(5) "String Escaping for Inclusion in Unit Names",
so `-` is `\x2d`). The unit sets no `Slice=`. Only systemd, as root, makes cgroups under
`/system.slice`. A cgroup v1 line naming the path and a cgroup below the unit's are refused too.

- RED: `TestADMIN1ASquatterIsRefused/a_user_unit_of_the_same_name`
  (`/user.slice/user-1000.slice/user@1000.service/app.slice/teamchat-server@alpha.service`, socket
  and process both uid 1000) and `/the_unit's_path_in_a_cgroup_v1_line` failed with the suffix
  match. `TestADMIN1TheUnitsCgroupIsSystemdsPath` pins the escaping.
- Docs: SPEC-n2f OP1, the comment block of `peer.go`.
- DBF: the class is a trust decision on a process's identity from a cgroup path a non-root user
  can also create. No detector can tell a trust judgement of a cgroup path from a display one by
  syntax (`helpers/containerwatch` and `lib/tmux-session.bash` read cgroups for display). The
  sweep by search found `peer.go` the only trust use; `hostnet`'s `cgroupUnit` was already strict
  (n2f-D11).
- The review's "better still" (also require the unit's dynamic uid) is not needed: only a process
  in the exact system cgroup counts, and no user can put one there.

## 2. The per-source cap: a team setting, dead connections reclaimed

Coordinator ruling: the cap is per team, default 256, and a source at its cap has its dead
connections reclaimed before a refusal.

- `connections_per_source` in a `teamchat_teams` entry: an integer 1 to 256
  (`paths.MaxConnectionsPerSource`), default 256 for a present team (`declare`). It is not in
  `team.json` and not an approved field.
- `team apply` renders it into the terminator's `ExecStart` as `--per-source N`; a changed cap is
  a changed drop-in, so the next apply restarts the terminator onto it.
- `teamchat tls --per-source N` (optional, 1 to 256) sets `tlsterm.Config.MaxPerSource`.
  `tlsterm.DefaultMaxPerSource` is now 256, every slot.
- Reclaim (`tlsterm` T9a): when a new connection finds its source at its cap, the source's held
  connections get a short TCP keepalive (1 s idle, two probes 1 s apart). A peer that is gone (a
  suspended host, a NAT that dropped the mapping) answers none; the kernel fails the connection,
  its relay ends and frees its share. The new connection waits for a share at most
  `ProbeLimit` (4 s, on the injected clock) and is refused only if none is freed. Connections that
  answer get Go's usual keepalive back.
- Tests: `TestT9aSeatsBehindOneAddressReconnectAfterASuspend` (three seats behind one address, the
  old connections dead but open, three new ones all admitted; RED at the parent: the fourth
  connection was closed, `EOF`), `TestT9OneSourceCannotHoldEverySlot` (live connections are not
  reclaimed: refused once `ProbeLimit` is up), `TestT9aTheLivenessCheckIsAShortKeepalive`,
  `TestTheTerminatorIsGivenTheTeamsPerSourceCap`, `TestV6TheTerminatorIsGivenThePerSourceCap`
  (also pins 256 = `tlsterm.DefaultMaxConns`), the declaration's refusals and defaults, the CLI's
  refusals.
- Docs: `tlsterm` package doc (T9, T9a), SPEC-cross-network X18 and X18b, DESIGN-v2-C-host.md
  3.8's YAML, `docs/agent-bus-cross-network.md`, the ccy changelog.
- DBF: a resource bound and its recovery are design, not a code pattern; no detector can express
  the class. Fixed with the tests above.
- **For the coordinator:** with the ruled default of 256, equal to the terminator's 256 slots, one
  source may hold every slot unless its team declares a lower cap, so SEC-4's bound does nothing
  by default. The docs say to set it lower to keep one address from holding every slot. If SEC-4
  should hold by default, the default needs to be lower than 256 or the slots higher.
- Not caught by the keepalive: a dead connection with unacknowledged data in flight is governed by
  the retransmission timer, not by keepalive, and is reclaimed only when that gives up or at
  `IdleLimit`.

## 3. HOST-4: Task B.2d

Coordinator ruling: HOST-4 is not deferred. PLAN.md has Task B.2d beside B.2a-c ("a team's listen
address moves while the bus host is suspended") with HOST-4's finding and B.5's design: a unit
that runs at resume (`After=suspend.target`) and a boot equivalent, a health problem for a moved
address, and a declaration rule. B.5's line, `b5-report.md` and `reports/AUDIT.md` now point at
B.2d. Both rulings are recorded in PLAN.md as coordinator rulings. Not implemented here.

## 4. The user docs

`docs/agent-bus-cross-network.md` now says the terminator relays the Matrix API only
(`/_matrix/`, `/.well-known/matrix/`), refuses others with 403 or 400, closes a client that sends
no request within 10 s, holds 256 connections, and caps a source at `connections_per_source` with
the liveness check.

## 5. HOST-7's new refusal

The `play-teamchat.yml` entry of `meta-deploy.list` warns that a team whose network has a longer
route inside it through another interface is now refused (exit 70) though it applied before, and
says what to do: declare each part one interface routes as its own network, or remove the
overlapping route, then run again. `docs/agent-bus-cross-network.md` says the same.

## 6. Gate nits

- A chunk size is hexadecimal digits alone, at most 15 of them, before any extension: `+5`, ` 5`,
  `5 `, a tab, `0x5` and an empty size are refused, and the size line is judged before it is
  relayed (it was written to the homeserver first). RED: `TestT10AChunkSizeIsHexDigitsAlone` (the
  `+5` body was relayed whole).
- An HTTP/1.0 request with a `Transfer-Encoding` is refused (RFC 9112 6.1). RED:
  `TestT10AmbiguousFramingIsRefused/HTTP/1.0 chunked` (it was relayed).
- `TestT10AnAdminRequestAfterABodyIsRefused`: an admin request after a request with a body, framed
  by its length or chunked, never reaches the homeserver (it passed before the change: coverage,
  not a defect).
- DBF: a framing parse looser than the RFC grammar has no detector a Go analyzer could express
  (which grammar a parser should hold is not in the code). Fixed with the tests.

## 7. The flaky test

Cause: `HandshakeLimit` and `RequestLimit` are both 10 s and are armed one after the other.
`AwaitDue(RequestLimit)` could be answered by the handshake's timer. The test's `Advance` then
landed between that timer's stop and the request timer's arming; the request timer was armed 10 s
from the new time and never fired. The fake clock refuses only a due two waiters share at once,
so a sequence is not seen.

- DBF: `project_shared_due` widened from teamsync's `reportsJoinLimit` to any package: it flags
  `AwaitDue`/`Armed` whose constant due equals two or more package-level `time.Duration`
  constants of the package under test. Red fixture `fix/internal/sharedlimits`
  (`TestSharedDueAcrossLimits`, 6 wants unmet before the widening). Red real tree: 12 findings
  (tlsterm 2, matrix 1, member/client 1, member/keeper 1, member/link 4, member/engine 3), none
  tagged or in the qa module. Fix: `clock.(*Fake).AwaitDueFrom(ctx, d, fn)` names a timer by its
  due and the function that armed it (`TestAwaitDueFromNamesTheTimerByWhereItIsArmed`); every
  finding now uses it. The sweep is clean untagged and with `busfailpoints`, `tuwunel`,
  `teamchatrelease`, `ccyteamstest` and `difftest`. CLAUDE/GoProjectRules.md records it.
- Proof: at the parent, `-race -count=200 -cpu 1,2,8` of the test, run beside `-race -count=20`
  of the package, failed 3 of 600 runs ("a client that sent no request was left open"). With the
  fix, the same run passed all 600, and the package's 20.

## Version

ccy 3.122.27, container 2.84.33 with item 7; ccy 3.122.28, container 2.84.34 with items 2 and 6.
The pre-commit image gate wants a bump on every commit that touches an image source, so one
release took two; logged as issue #352. Changelog entries for both.

## QA

- `go test -race ./internal/teamchat/...` passed before each commit.
- `qa-changed.bash` (dummy vault word) passed before the commits of items 1 and 7. Before the
  commit of items 2 and 6 it failed twice: once on `project_test_conn_read` in a new test (fixed:
  the listener is bounded), once on `TestClaudeStopBlocks` in `member/wiring`, a known load flake
  (issue #211, open; this occurrence and its seed added as a comment; it passed alone with that
  seed). The third run passed. The version bump that commit then needed (2.84.34) was a version
  line only, judged by the pre-commit hook, not by a further `qa-changed` run.
- Item 7's stress proof: see section 7.

## Other faults met

- Issue #352 (filed): the image-input gate's bump per commit.
- Issue #211 (open, commented): `TestClaudeStopBlocks` flakes under load.
