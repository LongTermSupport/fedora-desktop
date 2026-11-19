# B.5 report: host units and the TLS terminator

Plan 00257, Task B.5, worked in place on branch `plan-xhost-lxc-members` as the dispatch asked.
Findings from `reports/audit-host.md`, `audit-admin.md`, `audit-security.md` and
`audit-crosscutting.md`. Each was re-verified against the code before its fix; none was wrong.

## Findings

| Finding           | Result   | Commit      | Tests                                                                                                                                                                                                            |
| ----------------- | -------- | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| HOST-2            | fixed    | `421276071` | `TestAChangedTerminatorBaseUnitOrProgramRestartsTheTerminator`, `TestNoTerminatorProgramIsRefusedBeforeAnyChange`, `TestAUnitsRecordWithoutTheTerminatorRestartsItOnce`, `TestUnitsRecordKeepsTheTerminatorsSet` |
| HOST-3            | fixed    | `421276071` | `TestATerminatorDropInWrittenButNotStartedIsRepaired` (a reload fault, a restart fault)                                                                                                                          |
| HOST-1 (= XCUT-3) | fixed    | `eda224f97` | `TestHOST1TheHealthFilterAllowsEveryAddressTheTickProbes`, `TestHOST1AFilterWrittenButNotReloadedIsReloadedByTheNextApply`, `TestHOST1AnUnchangedFilterIsNotReloaded`                                            |
| SEC-4             | fixed    | `290c17617` | `TestT9OneSourceCannotHoldEverySlot`, `TestT9FullSlotsAreLogged`, `TestT9RefusalsAreLoggedOnceAWindow`                                                                                                           |
| SEC-5             | fixed    | `290c17617` | `TestT10TheAdminAPIIsRefusedAndNeverReachesTheUpstream`, `TestT10TheMatrixAPIIsRelayed`, `TestT10ALaterRequestIsJudgedToo`, `TestT10AmbiguousFramingIsRefused`, `TestT10ASilentClientIsClosedAtRequestLimit`     |
| ADMIN-1           | fixed    | `3d510b900` | six `TestADMIN1*` tests; the red is `TestTheOpenerIsOpenAndTheDefaultsAreTheRealOnes`, which bootstrapped against an in-process listener that is no team's unit                                                  |
| HOST-7            | fixed    | `6bf1bc170` | `TestHOST7ANetworkIsJudgedWhole` (hostnet), `TestHOST7ANetworkSplitAcrossInterfacesIsRefused` (host)                                                                                                             |
| HOST-4            | deferred | none        | none                                                                                                                                                                                                             |

Red before green: each test was seen failing before its fix, except HOST-7, whose tests needed
the new `Route.Split` field to compile. Its red is by reading: at the parent commit,
`hostnet.Route` judged a network by its first address alone.

### HOST-2 and HOST-3: the terminator restarts on what it was started on

`units.json` keeps a third digest set, `terminator`. It holds the digest of the terminator's base
unit, of its drop-in, and of the member program `/usr/local/bin/teamchat` that its `ExecStart=`
runs (links followed). The set is written after the start.

`team apply` reloads and restarts the terminator whenever that set does not match the render.
So each of these now reaches a running terminator at the next apply:

- a changed base unit;
- a new member program;
- a drop-in that was written but whose reload or restart failed.

A `units.json` written before the set existed restarts the terminator once. The render refuses a
host that has no member program.

The play widens a scoped run to every present team when the kit's `current` link moves, as it
already does for a changed base unit.

Docs: DESIGN-v2-C-host.md 3.5.2 step 03, SPEC-cross-network X18c.

### HOST-1: the health unit's drop-in

`team apply` (after its records) and `team remove` (after its files) render
`/etc/systemd/system/teamchat-health.service.d/teams.conf`. It holds `IPAddressDeny=any` and an
`IPAddressAllow=` with the listen address of every team that has a stamp, each as a host
prefix. It holds no networks.

- It is rendered under a host-wide flock in the lock directory.
- systemd is reloaded until the digest of the text on disk is recorded as loaded
  (`health-filter.loaded`). The record is in the lock directory, which a reboot empties, so a
  reboot costs one reload.
- The drop-in joins the unit-text judges (`filteredUnits`, `unitSources`).
- The new step is named `health filter`.

### ADMIN-1: the ctl child checks its peer

Every connection the child's admin and member clients open goes through `checkedDial`, which
runs `Config.Peer` before a byte is sent. The production check, `ctl.ProcPeer`, works like this:

1. It finds, in the child's `/proc/net/tcp`, the socket that accepted the connection and the uid
   that owns it.
2. It requires a process in the cgroup of `teamchat-server@<team>.service` whose effective uid
   is that uid (the unit's `DynamicUser=`).

An accepted socket's uid is the uid that created the listener. So the check holds for each
connection, and there is no window between the check and the send.

A refusal names the unit. The restore's staging client stays direct, because it runs in the
staging unit's own network (RU2).

Not run on a host: the check reads the homeserver's `/proc/<pid>/cgroup` and `status` as
`teamchat-ctl`. A host that mounts `/proc` with `hidepid` would refuse every open, with a
message naming the unit.

Docs: SPEC-n2f OP1, the `ctl` package doc.

### SEC-4 and SEC-5: the terminator

The terminator now gates requests (`tlsterm/gate.go`):

- It speaks HTTP/1.1 only.
- It relays only targets under `/_matrix/` or `/.well-known/matrix/`, with no dot segment,
  whether spelt or written as `%2e`.
- It finds each request's body by that request's own framing.
- It refuses ambiguous framing, upgrades, CONNECT and a bare LF.

The bytes it relays are the client's own. A refused first request gets a 403 or 400 answer and
the connection is closed. A refused later request is never sent, and the connection ends once
the answers to the earlier requests have gone through. The upstream is dialled only after a
first request has passed, which must arrive within `RequestLimit` (10 s) of the handshake.

One source address holds at most `MaxPerSource` connections (default 64, a quarter of 256).
Every refusal is logged: the first of each minute at once, and the count of the rest at the end
of that minute.

Docs: `tlsterm` package doc (T5, T9, T10), SPEC-cross-network X18.

### HOST-7: a network is judged whole

`hostnet.Route` now uses only a route that covers the whole network. `host.Route.Split` names any
other interface that a longer route inside the network goes through, and `team apply` refuses
such a network as drift (exit 70). Loopback host routes, which list the host's own IPv6
addresses, do not count as a split.

Docs: SPEC-cross-network X3.

### HOST-4: deferred

The fix needs three new pieces:

- a unit that runs at resume (`After=suspend.target`) and a boot equivalent;
- a health-tick comparison of the live interface addresses with each `units.json` listen set,
  reported as its own problem ("the host's address moved: run team apply");
- a declaration rule that requires a DNS name or a static address on a host that suspends.

That is more than a small change. It belongs with the suspend work (B.2) or a task of its own.
HOST-1 already removes the part that made every resume look like an outage.

## Defence Before Fix

No detector can express any of these classes, so each was fixed conventionally with a
regression test:

- **HOST-2 and HOST-3:** a restart decided by comparing file bytes rather than a record of what
  the running unit was started on. This is control flow across steps; an analyzer cannot tell
  which comparison stands for "what systemd loaded".
- **HOST-1:** a unit's IP filter narrower than what its program dials. The nearest mechanical
  hold is the unit-text judge, which now covers the drop-in.
- **ADMIN-1:** a secret sent to an unauthenticated listener. An analyzer cannot see which values
  are secrets or which dials are authenticated.
- **SEC-4 and SEC-5:** resource bounds and a path policy in a relay. These are design, not a
  code pattern.
- **HOST-7:** a prefix judged by its first address. This cannot be told apart from a legitimate
  use of `Prefix.Addr()`.

## QA

- `go test ./internal/teamchat/...` passed after each change. `tlsterm` passed five times under
  `-race`.
- `qa-changed.bash` results per commit:

| Commit      | Result                                                                                                                             |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `421276071` | passed                                                                                                                             |
| `eda224f97` | cut short by my own wrapper's timeout; every check that ran had passed, the Go gate included                                       |
| `290c17617` | failed once on the project rule `gqaci_map_range_order` (a map-ranged test table); after the fix, `qa-go-qa.bash --changed` passed |
| `3d510b900` | passed (one run covering this commit and the next)                                                                                 |
| `6bf1bc170` | passed (same run)                                                                                                                  |

- The ccy image's `teamchat` is built from `paths` and `tlsterm`, so the series needed a version
  bump. It ends at CCY 3.122.25, container 2.84.32, with one changelog entry for B.5.
- `meta-deploy.list` queues `play-teamchat.yml`, then `play-claude-yolo.yml`.

## Noted, not fixed (outside B.5)

- The pre-commit image-input gate compares each commit with HEAD. So a series of commits on one
  branch has to bump the container version again for every commit that touches an image file:
  here it went from 2.84.30 to 2.84.32 for one release. Judging against the base on the
  protected line, as the CCY_VERSION merge rule does, would need a single bump. This should be
  logged against the gate (dogfood rule), and I could not file it from here.
- The SI3 check in `carry/import.go` confirms that the base units and the Tuwunel binary are
  installed, but not the member program the terminator now needs. An import onto a host without
  that program fails at the render of the apply that follows, with a clear message.
- The per-source default of 64 is a judgement call. A host whose members all reach the bus
  through one NAT address (rootless containers through pasta, a VPN that masquerades) is held to
  64 connections, about 30 seats. `MaxPerSource` is a field of `tlsterm.Config`, not yet a flag
  of `teamchat tls`.
- `docs/agent-bus-cross-network.md` (B.11's file) does not describe the request gate or the
  per-source bound yet.
