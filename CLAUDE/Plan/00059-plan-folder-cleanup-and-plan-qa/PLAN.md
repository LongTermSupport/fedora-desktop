# Plan 00059: Plan folder cleanup and enable plan_workflow.qa

**Status**: In Progress
**Created**: 2026-07-07
**Owner**: joseph
**Priority**: Medium

## Overview

The hooks-daemon v3.32.0 upgrade added the `plan_workflow.qa` subsystem (Plan
00144 upstream), which lints the plan tree for cross-file drift. Its first sweep
surfaced pre-existing drift that had accumulated in `CLAUDE/Plan/` — folders
never indexed in `README.md`, completed plans still sitting in the active root,
plans missing a parseable `**Status**:` header, and a lowercase `plan.md`.

This plan does two things: (1) makes the `plan_workflow.qa` policy **explicit** in
`.claude/hooks-daemon.yaml` (it previously ran on invisible legacy-migration
defaults), and (2) resolves every drift finding so `plan-qa --sweep` exits 0 and
is CI-able going forward.

## Goals

- Add an explicit top-level `plan_workflow:` block (with `qa:`) to
  `.claude/hooks-daemon.yaml` and restart the daemon.
- Bring every plan folder into row↔folder↔status coherence so
  `plan-qa --sweep` exits 0 with no blocking findings.
- Rewrite `CLAUDE/Plan/README.md` so every folder has exactly one index row in
  the section matching its physical location, with accurate statistics.

## Non-Goals

- Reviving or re-executing any dormant plan's actual work — this is
  metadata/index hygiene only.
- Editing anything under `.claude/hooks-daemon/` (upstream, overwritten on
  upgrade).
- Adding grandfather allowlists — legacy plans are being fixed properly, not
  suppressed.

## Context & Background

Each flagged folder's **verified** disposition (from its `PLAN.md` status header,
git recency, and whether its deliverables actually shipped into the repo):

| Folder                                | Verified state                                 | Action                                          |
| ------------------------------------- | ---------------------------------------------- | ----------------------------------------------- |
| `002-nordvpn-openvpn-manager`         | Complete (`nord` + playbook + docs shipped)    | add `**Status**` header; mv → `Completed/`      |
| `006-documentation-audit-and-update`  | Complete (already under `Completed/`)          | add missing Completed README row only           |
| `012-fix-plugin-handlers`             | Cancelled (2026-02-17)                         | `git mv` → `Cancelled/`; fix row link           |
| `00036-cc-ccy-parity`                 | Cancelled (2026-06-10)                         | `git mv` → `Cancelled/`; fix row link           |
| `015-article-mode`                    | Complete (`wsi-article` shipped); lc `plan.md` | rename→`PLAN.md`, add header, mv → `Completed/` |
| `017-merge-ccy-ccb`                   | Complete                                       | `git mv` → `Completed/` + row                   |
| `021-firstboot-wizard-redesign`       | Complete                                       | `git mv` → `Completed/` + row                   |
| `018-fedora-kickstart-install`        | In Progress (blockquote `> Status:`; ~25d)     | convert to `**Status**` header; active row      |
| `022-install-security-and-resilience` | In Progress                                    | add active README row                           |
| `030-phpantom-lsp`                    | "In Progress" but ~5mo stale                   | → `Dormant` (name the blocker)                  |
| `031-reliable-screen-sharing`         | "In Progress" but ~5mo stale                   | → `Dormant` (name the blocker)                  |

Also fix in the same pass (README rot not itself a sweep finding): the
`00050-fedora-44-tracking` / `00051-ansible-lint-improvement` rows are run
together (00050 has no description; 00051 carries 00050's Fedora-tracking text) —
split them.

Config schema (verified in the daemon's `PlanWorkflowConfig`): the QA subsystem
had been running via the legacy migration
(`markdown_organization.options.track_plans_in_project` → synthesised
`plan_workflow{enabled:true, qa:defaults}`). Defaults: `edit_mode: block`,
`commit_gate_mode: warn`, `sweep_mode: advise`, `completed_dir: Completed`,
`cancelled_dir: Cancelled`. An **explicit** top-level `plan_workflow:` block
disables that auto-migration, so the block MUST restate `enabled: true` +
`directory: CLAUDE/Plan` or plan tracking silently turns off.

## Tasks

### Phase 1: Make plan_workflow.qa explicit

- [x] ✅ **Task 1.1**: Add a top-level `plan_workflow:` block to
  `.claude/hooks-daemon.yaml` (`enabled: true`, `directory: CLAUDE/Plan`,
  `workflow_docs`, and a `qa:` sub-block pinning the effective modes).
- [x] ✅ **Task 1.2**: Restart the daemon and confirm status RUNNING with plan
  tracking still active (sweep still enumerates).

### Phase 2: Resolve folder/index drift

- [x] ✅ **Task 2.1**: Create `CLAUDE/Plan/Cancelled/` and `git mv` the two
  cancelled plans (`012-fix-plugin-handlers`, `00036-cc-ccy-parity`) into it.
- [x] ✅ **Task 2.2**: `git mv` the completed plans still in the active root
  (`002-nordvpn-openvpn-manager`, `017-merge-ccy-ccb`,
  `021-firstboot-wizard-redesign`) into `Completed/`; add a `**Status**` header
  to 002 first.
- [x] ✅ **Task 2.3**: Normalise `015-article-mode` — `git mv plan.md PLAN.md`,
  add a `# Plan 015:` title + `**Status**: Complete` header, then
  `git mv` the folder into `Completed/`.
- [x] ✅ **Task 2.4**: Convert `018-fedora-kickstart-install`'s blockquote
  `> Status:` to a `**Status**: In Progress` header (stays in active root).
- [x] ✅ **Task 2.5**: Flip `030-phpantom-lsp` and `031-reliable-screen-sharing`
  to `**Status**: Dormant` with a parenthetical naming what each is blocked on.

### Phase 3: Rewrite the README index + verify

- [x] ✅ **Task 3.1**: Rewrite `CLAUDE/Plan/README.md` so every folder has one
  row in the section matching its location: add Completed rows for
  002/006/015/017/021; add active rows for 018/022 and this plan (00059); move
  012/00036 rows into a Cancelled section pointing at `Cancelled/`; split the
  00050/00051 rows; refresh the stale "Creating New Plans" snippet to use
  `mkplan.bash`.
- [x] ✅ **Task 3.2**: Run `plan-qa --sweep` and confirm exit 0 (no blocking
  findings; residual advisories understood).
- [ ] 🔄 **Task 3.3**: Run `./scripts/qa-all.bash` (green) and commit the
  cleanup as one coherent Plan 00059 change (config + moves + README + this plan
  file staged together, satisfying index-at-birth and terminal-state atomicity).

## Success Criteria

- [ ] `plan-qa --sweep` exits 0 with zero blocking findings.
- [ ] Every `CLAUDE/Plan/` folder has exactly one README row in the section
  matching its physical location; every row link resolves.
- [ ] `.claude/hooks-daemon.yaml` carries an explicit `plan_workflow.qa` block
  and the daemon restarts RUNNING.
- [ ] `./scripts/qa-all.bash` passes.

## Notes & Updates

### 2026-07-07

- Scaffolded during the v3.31.1 → v3.32.0 hooks-daemon upgrade follow-up.
- Failsafe recovery cron `20cd0898` created (hourly at :37, non-durable).
- Investigation corrected first-draft dispositions: 002, 006, 015, 017, 021 are
  all genuinely **Complete** (their deliverables — `nord`, the doc audit,
  `wsi-article`, the CCB retirement, the firstboot redesign — shipped into the
  repo), not orphans/active. 006 already sits in `Completed/` and only lacks a
  README row.
