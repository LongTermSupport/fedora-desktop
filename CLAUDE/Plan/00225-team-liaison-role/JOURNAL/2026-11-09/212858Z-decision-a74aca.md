## 21:28 · decision · —   — Retargeted to bus v2; round 5 and 6 fixes

Plan retargeted to agent bus v2 (`teamchat`) and reviewed again.

- Why: the owner feature-froze v1 on 2026-11-02 (Plan 00209 direction 7), and v2 landed on F44
  (Plan 00246, pull request 124). Nothing of this plan is built in v1 now (DESIGN section 0,
  LD10). A real team can have a liaison only after Plan 00209 Phase 6 migrates it.
- Round 5's three blockers fixed: an edited or redacted `Q<n>` post fails the relay check as
  `edited` (LZ6); no SSH keys or write-capable token at a liaison launch, refused in Go where
  role and selection meet (LZ17a); in-container `ro` binds are optional and trusted only with
  `CAP_SYS_ADMIN` dropped and probed (LZ17, LD12).
- The owner's Element input: one conversation across the terminal and Element, a terminal
  answer confirmed in the room (LZ7b, LD13); the reports room is already read-only to humans in
  v2 (LZ24), checked in acceptance.
- Round 6 (CHANGES, 2 blockers): the daemon's `hooks-relay` and virtualenvs had been classed as
  writable state, and the per-seat directory did not match what the launcher and entrypoint
  read. Fixed: anything executed is configuration; the liaison's writable paths are per-seat;
  tracked launch files bound `ro` from the checkout. Should-fix: liveness candidates named from
  v2 (A 7.4 notices, presence); the framing listing widened to everything the liaison said to a
  human since the hand-off, stated as transparency; `liaison_note` split rule; `unshare` probes.
- Next: round 7 review of the round-6 fixes; then the plan is ready to merge as Not Started.
