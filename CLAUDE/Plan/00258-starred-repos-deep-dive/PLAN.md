# Plan 00258: starred repos deep dive

**Status**: In Progress
**Created**: 2026-11-20
**Owner**: joseph
**Priority**: Medium

## Overview

The owner's GitHub stars are mostly reference repositories: other people building agent fleets,
agent teams, sandboxes, gateways and Claude Code tooling, the same ground as ccy, ccy-teams, the
agent bus and the hooks daemon. The stars change often.

This plan extends the watch list (Plan 00200) into one process that covers them all:

- the list follows the stars, plus repositories added by hand;
- every repository is fully cloned under `untracked/starred-repos/<licence>/<owner>/<repo>`, its
  licence read from its own licence file;
- the repositories are studied in **runs**, each recording the commit it studied per repository,
  so a later run can study only the diff, or run a full baseline again.

One skill (`/watchlist`), one list (`CLAUDE/Watchlist.tsv`), one command (`watchlist`), one
study agent (`watchlist-scout`), one process document ([Watchlist.md](../../Watchlist.md)). The
first run, `baseline-20261120-1050`, is the in-depth baseline.

The licence split decides reuse: `lift` (MIT, Apache-2.0, ISC, BSD) code may be copied with its
notice; `clean-room` (GPL, AGPL, BUSL, PolyForm, proprietary, restricted Apache variants) gives
ideas only, through a clean room where the implementer works from the study report and never
opens the source. Of the GitHub labels, nine were wrong or missing: they were BUSL-1.1, AGPL-3.0,
PolyForm Shield, BSD-2-Clause, MPL-2.0, GPL-3.0, a proprietary licence, Multica's Apache-2.0 with
commercial restrictions, and one with no licence at all.

## Goals

- Every star, and every repository added by hand, is cloned in full under its verified licence,
  or set aside as unrelated.
- A baseline study of every studied repository, recorded against the commit studied.
- Re-syncing and re-studying is one skill: sync, triage, then a diff or baseline run, resumable.
- What we learn becomes issues (clear wins) or plans (needs discussion).

## Non-Goals

- Adopting any finding in this plan. Each adoption is its own issue or plan.
- Studying the excluded repositories.

## Tasks

### Phase 1: The clone set

- [x] ✅ **Task 1.1**: Fetch the stars, verify each licence from its licence file, set aside 14
  unrelated stars, and clone the rest (54, plus coral and lightning, which are on the watch list
  but not starred) in full under `<licence>/<owner>/<repo>`. Both have been renamed upstream since
  the watch list recorded them: `cdknorow/coral` is now `subgentic/coral` (GitHub redirects the
  old name to it) and `Mizerd/lightning` is now `lightning-matrix/lightning`.

### Phase 2: One process

The tooling was held on a branch while this session did research and planning only, and was
merged on the owner's go.

- [x] ✅ **Task 2.1**: Watchlist.md, the `watchlist` skill and the `watchlist-scout` agent
  rewritten for runs, so one skill covers stars, sync, baseline and diff (`57d794f9f`).
- [x] ✅ **Task 2.2**: The `watchlist` command extended per [SPEC-watchlist-runs.md](SPEC-watchlist-runs.md)
  (`sync` from stars, `add`, `set`, `run new|rows|mark|status`), with the list migrated to the new
  format (`e1d24967b`, `b0767d737`).
- [x] ✅ **Task 2.3**: The CCY bump queued for the host (`changed-plays` in `meta-deploy.list`),
  the baseline manifest marked by `run mark` for all nine groups, and the old mirrors in
  `untracked/watchlist/` retired.

### Phase 3: The baseline run

- [x] ✅ **Task 3.1**: One study per group, in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`.
  `watchlist run status` shows all nine groups done (56 repositories).
- [x] ✅ **Task 3.2**: `SYNTHESIS.md` for the run.
- [x] ✅ **Task 3.3**: Act: issues #356, #357, #358; Plans 00259, 00260, 00261; journal entries in
  ten existing plans (SYNTHESIS.md section 8). New-plan candidates 5-9 wait for the owner.

### Phase 4: Recurrence

- [ ] ⬜ **Task 4.1**: The owner chooses how diff runs recur (carried over from Plan 00200, Task 3.1).

## Success Criteria

- [x] `watchlist run status baseline-20261120-1050` shows every group done.
- [ ] Every proposal to copy code names a permissive licence; no clean-room idea carries code.
- [ ] A diff run can start from the baseline with one skill call.
- [ ] The synthesis is turned into issues and plans the owner has seen.

## Delivery & Milestones

- Clone set built (Task 1.1).
