# Plan 00209 N0 area C: handoff after review round 3

The review loop is finished: three rounds, the most the N0 rules allow. This file is for whoever
picks area C up next, the integrator included.

## State

- Worktree `/workspace/untracked/worktrees/bus-v2-n0-c-host`, branch `feat/bus-v2-n0-c-host`.
  Never push, never run `qa-all.bash` or Ansible; `./scripts/qa-changed.bash` before each commit.
- The design: `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-C-host.md`, sections 1-8.
- Reviews: round 1 `subagent-reports/261021-n0-c-host-review.md` (24 findings), round 2
  `-review-2.md` (findings 25-42), round 3 `-review-3.md` (findings 43-51). Every finding of all
  three rounds has a fix in the design. Round 3 was FIX-FIRST on 43 and 44 (both in restore,
  3.12.2), and the fixes for 43-51 were made after it. No fourth review was run on them.
- Decisions defaulted: DC1-DC17 (section 4). DC15-DC17 came from round 1, and DC4 was checked
  against the board's needs in 3.11.3.

## If anything is picked up again

- A reviewer could check the round 3 fixes: restore stages before its own backup, plus the
  `restore.json` record, the `restore.block` assertion and `--resume` (3.12.2); LXC online-ness
  in the status record (3.11.2, 3.11.3); background releases on the board (3.13); the placement
  lock and the `.new-` rule (3.7.3); take routing by handle state (3.7.3 take step 1); the
  `units.json` fields (3.2.2); the stamped-only enumeration in `health --all` (3.11.2); the
  splice range edges (3.8).
- Section 7 lists what C assumes of areas A, B and D. The integrator checks those against their
  designs.
