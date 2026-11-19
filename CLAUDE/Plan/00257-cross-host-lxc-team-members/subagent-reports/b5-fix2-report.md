# B.5 second fix round: the terminator's slots, and the review of b5-fix-review.md

In progress. Each section is filled as its commit lands.

## Step 0: 1024 slots, 256 per source (review item 5)

The coordinator's ruling (c): `tlsterm.DefaultMaxConns` is 1024 and `DefaultMaxPerSource` stays
256 (`paths.MaxConnectionsPerSource`), so by default one source holds a quarter of the slots and
SEC-4 holds without any team setting. The terminator unit's `LimitNOFILE` is `2*1024+64`.

The homeserver's room for 1024 upstream connections, established from the source of the pinned
release (Tuwunel v1.9.4, `files/usr/local/share/teamchat/tuwunel.pin`):

- `src/main/server.rs:61` calls `sys::maximize_fd_limit()` at start, and fails the start if it
  cannot (`.expect`).
- `src/core/utils/sys/limits.rs:33-45` raises the soft `RLIMIT_NOFILE` to the hard limit.
- `teamchat-server@.service` sets no `LimitNOFILE`, so it gets systemd's default for system
  services, `DefaultLimitNOFILE=1024:524288` (systemd 240 and later, `systemd-system.conf(5)`):
  the homeserver runs with a soft limit of 524288, far above 1024 connections plus its database
  files. A host whose `system.conf` lowers the hard limit would change this; nothing in this
  repository does.

## The dead-connection reclaim is removed (review items 1 to 4)

The coordinator changed the scope mid-task: remove the reclaim rather than fix it (reasons in
PLAN.md B.5 and the journal entry `185634Z-decision-c1c775`). Gone: the reclaim, the waiter loop
in `admit`, `Config.Probe`, `ProbeLimit`, the `released` channel, `bySource`, and the keepalive
functions and values (`probeKeepAlive`, `heldKeepAlive`, `keepaliveProbe`, `restoreKeepalive`,
`setKeepAlive`: all were added by 0e2d38a60, so nothing that predates it was removed; accepted
connections keep Go's default listener keepalive, as before). Kept from 0e2d38a60: the
per-team `connections_per_source`, `--per-source`, and the request-gate fixes.

`admit` now checks the source's share first, under the lock, then takes a slot without
waiting; either full is a refusal at once, logged as T9 logs.

| Item | Outcome | Test (RED seen against the reclaim code at b3eabaec0) |
| ---- | ------- | ----------------------------------------------------- |
| 1. a waiter holds a global slot | gone by construction: nothing waits, and the share is checked before the slot | `TestT9AFloodAtTheCapHoldsNoSlot`: RED, the other source was refused (`EOF`) while the flood's connections held the slots |
| 2. two reclaims restore each other's keepalive | gone by construction: no probe, no restore | none needed: the code path no longer exists |
| 3. the probe cannot see a connection with unacknowledged data | no probe; docs now say exactly what happens: over the cap a connection is refused at once, dead connections are freed by the 2-minute idle limit, the member's link backs off and reconnects (docs/agent-bus-cross-network.md, SPEC X18, PLAN.md, changelog, package doc T9/T9a) | n/a |
| 4. the production probe is never tested on a dead peer | no probe left. A loopback peer always acknowledges, so no unit test could have made a dead one. PLAN.md B.2c now expects the lab test to suspend a member's host behind the terminator and see every seat reconnect while its old connections are still held | n/a |

Evidence that the ride-out works without the reclaim (the coordinator's condition for removing
it; neither shows a failure):

- `TestT9aSeatsBehindOneAddressRideOutASuspend`: eight seats behind one address leave dead but
  held connections; eight new ones are admitted at once and served, the source holding 16.
  Green against both the reclaim code and the new code (at the default cap the reclaim never
  ran).
- `TestT9aASourceAtItsCapIsAdmittedAgainAtIdleLimit`: at a cap of 1, a new connection is refused
  with no clock moved; after the fake clock passes IdleLimit the dead one is closed and the
  same source is served again. RED against the reclaim code (the refusal waited on ProbeLimit,
  which never came: timeout).

Also changed: `TestT9OneSourceCannotHoldEverySlot` and `TestT9AtTheDefaultsOneSourceLeavesSlotsForOthers`
refuse at once with no probe; `TestT9FullSlotsAreLogged` dials its second connection from
another source, because with one slot a same-source connection is now refused by its share
first (the log says "1 connections are held from it", not "all 1 connections are held").

DBF: a wait that holds a shared resource, and a probe whose restore races another, are design
defects of one mechanism, now removed; no analyzer expresses either class.
