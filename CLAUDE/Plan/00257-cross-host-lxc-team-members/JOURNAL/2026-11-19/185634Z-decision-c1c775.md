## 18:56 · decision · — — The terminator's dead-connection reclaim is removed

The terminator's dead-connection reclaim (the liveness check at the per-source cap, commit
0e2d38a60) is removed. The coordinator withdrew its earlier "reclaim by liveness check" ruling
after the second review (`subagent-reports/b5-fix-review.md`), for these reasons:

- With 1024 slots and 256 per source by default, the per-source cap binds only when one address
  holds 256 connections. A suspend ride-out at most doubles the connections of the seats behind
  one address, briefly.
- The probe could not catch the commonest dead connection: Linux sends no keepalive probe while
  a socket has unacknowledged data, which a `/sync` long-poll answered after the member host
  suspended usually has (review item 3).
- Dead connections are freed by the 2-minute idle limit (T8), while the member's link backs off
  and reconnects.
- Two review rounds found races in it (a waiter holding a global slot, review item 1; one
  reclaim restoring the keepalive another was probing, item 2). Removing it removes both by
  construction.

Now: over its cap a connection is refused at once and logged; the share is checked before the
global slot is taken, so a refused connection never holds a slot. Tests
`TestT9aSeatsBehindOneAddressRideOutASuspend` (old connections dead but held, every seat
reconnects under the cap) and `TestT9aASourceAtItsCapIsAdmittedAgainAtIdleLimit` (refused at once
with no clock moved, admitted again once IdleLimit ends the dead ones) are the evidence that the
ride-out works without the reclaim.
