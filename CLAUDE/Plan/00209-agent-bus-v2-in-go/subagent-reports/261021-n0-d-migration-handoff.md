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

- **Review round 3** (`261021-n0-d-migration-review-3.md`): FIX-FIRST, 14 findings (2 blockers).
  All fixed in `63131e3b`. The fence is now a v2-owned nftables table plus a oneshot
  `teamchat-fence@<team>.service`, with a v2 drop-in and restart as the fallback. No v1 verb is
  added: the proposed `agent-bus-install fence` would change shared v1 software and grow a
  ratchet-listed bash file. `rooms-read-only` is dropped too, because v1's play undoes it. The
  archive is stopped at M8 (the new OD-D6 default), opened on demand, and an archive check carries
  what humans post in it.
- **Verification of round 3** (`261021-n0-d-migration-review-3-verify.md`): all 14 FIXED. Its
  verdict was FIX-FIRST on 7 new findings in the fixes (1 blocker: M8 unlocked humans after
  stopping the server). All 7 were addressed in the commit that updates this handoff. No reviewer
  has checked those 7 fixes yet.

## Left

1. If the lead wants it, run one more verifier over the last commit's 7 fixes only.
