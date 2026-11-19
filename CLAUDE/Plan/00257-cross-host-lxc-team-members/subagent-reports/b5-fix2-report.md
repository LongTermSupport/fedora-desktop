# B.5 second fix round: the terminator's slots, and the review of b5-fix-review.md

Branch `plan-xhost-lxc-members`. Commits: b3eabaec0 (step 0), e67a8301d (the reclaim removed),
and the minors commit that carries this report's last sections.

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

| Item                                                          | Outcome                                                                                                                                                                                                                                                                          | Test (RED seen against the reclaim code at b3eabaec0)                                                                     |
| ------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| 1. a waiter holds a global slot                               | gone by construction: nothing waits, and the share is checked before the slot                                                                                                                                                                                                    | `TestT9AFloodAtTheCapHoldsNoSlot`: RED, the other source was refused (`EOF`) while the flood's connections held the slots |
| 2. two reclaims restore each other's keepalive                | gone by construction: no probe, no restore                                                                                                                                                                                                                                       | none needed: the code path no longer exists                                                                               |
| 3. the probe cannot see a connection with unacknowledged data | no probe; docs now say exactly what happens: over the cap a connection is refused at once, dead connections are freed by the 2-minute idle limit, the member's link backs off and reconnects (docs/agent-bus-cross-network.md, SPEC X18, PLAN.md, changelog, package doc T9/T9a) | n/a                                                                                                                       |
| 4. the production probe is never tested on a dead peer        | no probe left. A loopback peer always acknowledges, so no unit test could have made a dead one. PLAN.md B.2c now expects the lab test to suspend a member's host behind the terminator and see every seat reconnect while its old connections are still held                     | n/a                                                                                                                       |

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

## Minors

- **AUDIT.md B.2d row** (review nit 6): `reports/AUDIT.md` has a B.2d row for HOST-4, and the
  parenthetical on the B.5 row is gone.
- **Whitespace before a chunk extension** (nit 7): RFC 9112 section 7.1.1 is
  `chunk-ext = *( BWS ";" BWS chunk-ext-name [ BWS "=" BWS chunk-ext-val ] )`, so spaces and
  tabs between the size and `;` are allowed. `chunkSize` now trims them before the `;` only; a
  space after a size with no extension (`5 `), a space before the size, a sign or a prefix stay
  refused. RED: `TestT10AChunkSizeMayHaveWhitespaceBeforeItsExtension` (`5 ;x=1`, `5\t;x=1`,
  `5 \t ;x=1` were refused: `unexpected EOF`); `TestT10AChunkSizeIsHexDigitsAlone` now also
  refuses `5\t`, ` ;x=1` and `5 x;y=1`. By my reading of hyper's chunked decoder (the HTTP
  server under Tuwunel; read, not run), it also takes whitespace after the size before a `;`, so
  the two read one size one way. DBF: a parse looser or stricter than the RFC's grammar is not a
  code shape an analyzer can name; held by the tests.
- **ADMIN-1 with a dashed team id** (nit 8): `TestADMIN1ATeamIDWithADash`. The unit is started by
  its plain name (`paths.ServerUnit`, `teamchat-server@ops-a.service`); systemd names a unit's
  cgroup by the unit's own name (only the template prefix in the default slice's name is
  escaped, `system-teamchat\x2dserver.slice`), so the expected path keeps the instance's `-`:
  `/system.slice/system-teamchat\x2dserver.slice/teamchat-server@ops-a.service`. The test pins
  that path, and that a process in it is accepted while one in a cgroup with an escaped instance
  (`ops\x2da`) is not. It was green on its first run: the code was already right, as the
  reviewer read it; this is a pin, not a fix.

## Notes for the coordinator

- No owner decision was needed beyond the coordinator's two rulings.
- Dogfood: `mkplan.bash --journal` takes the plan number (`00257`), not the folder name the brief
  gave; its error says so plainly, so no fault is logged.
- `leakcheck.Poll` waits unbounded in a world with a network goroutine (its doc says so), so a
  wrong expectation in a terminator test hangs to the package timeout instead of failing at
  once. Documented behaviour, not logged as a fault.
