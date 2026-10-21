# Plan 00209 N0 area D: handoff

The area D designer stopped here because it passed its context budget. A fresh agent can pick up
from this file.

- Branch: `feat/bus-v2-n0-d-migration`.
- Worktree: `/workspace/untracked/worktrees/bus-v2-n0-d-migration`.
- File: `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-D-migration.md`.
- Evidence digests in the same folder: `RESEARCH-v1-names.md` (v1's names and paths) and
  `RESEARCH-v1-team-state.md` (what a running v1 team holds).

## State

- **Review round 1** (`261021-n0-d-migration-review.md`): FIX-FIRST, 21 findings. All of them were
  addressed in `caabbbf6`.
- **Review round 2** (`261021-n0-d-migration-review-2.md`): FIX-FIRST. Of round 1's findings, 17
  were fixed and 3 partly fixed. It raised 13 new findings: 2 blockers, 4 majors and 7 minors. All
  13 were addressed in `79b63277` and in the commit that adds this handoff.
- **The main change in round 2.** The fence is now a network fence, not a park. A new v1 verb,
  `agent-bus-install fence|unfence`, leaves the v1 server reachable only over loopback, and every
  human is locked.
  - Why: v1's return routes cannot keep member state. `add-member --out` refuses a directory that
    exists, and v1's LXC apply deletes the guest's team directory. A park also ends every watcher
    for good.
  - With a network fence, watchers retry and survive (`helpers/pingbus/cli.py:1757-1805`), so
    rollback re-mints nothing.
  - Humans are also logged out only when the team's Element address is loopback. They then get a
    new password at rollback or at M8.
  - Members are parked only at M8, after commit and before the fence is lifted.

## Left

1. **Review round 3**, the last of the 3 allowed. Spawn one opus adversarial reviewer over the file
   and the frame. Give it round 2's report, and ask it to verify the round-2 fixes and the new v1
   facts the design now relies on:

   - the watcher's retry on a network fault;
   - that a `_deliver` local `TIMEOUT` is the only state change possible during the fence;
   - that systemd applies `IPAddressAllow=` to a running unit. That one is left for P1 to prove
     and is stated as such in 5.5.

   It writes `261021-n0-d-migration-review-3.md`. Fix its findings.

2. **The final message to the lead**, under 1200 characters: branch, tip, file, verdict, the
   defaulted decisions (OD-D1 to OD-D7, section 9), and the assumptions on other areas (section 12).
