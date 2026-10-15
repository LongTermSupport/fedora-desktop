# Plan 00193: coral adoption audit

**Status**: Complete (2026-10-15: not adopted; coral moved to the repo watch list, Plan 00200)
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: Medium

## Overview

`cdknorow/coral` on GitHub is an external project for running and supervising AI coding
agents. This plan audits it against what this repository already builds for the same job:
`ccy` (the Podman container launcher for Claude Code), `ccy-sessions`, `ccy-teams`, the
agent bus, and the hooks daemon.

The audit answers four questions for the owner: what coral is and does; whether it fits, or
could be made to fit, the ccy/cc model (rootless Podman, one container per project, strict
IaC); how opinionated it is (what it assumes and what it forces on its user); and whether to
adopt it as is, fork it, borrow ideas from it, or leave it.

## Goals

- A written audit, `AUDIT-coral.md` in this folder, with a recommendation the owner can
  decide on, backed by evidence from coral's code, not only its README.

## Non-Goals

- Installing, running or adopting coral; that waits on the owner's decision.
- Copying coral code into this repository.

## Tasks

### Phase 1: Audit

- [x] ✅ **Task 1.1**: Audit coral from a reference clone under `untracked/repos/`: its
  architecture, language and dependencies, licence, maintenance health, security model, how
  opinionated it is, and an overlap/gap map against ccy, ccy-sessions, ccy-teams, the agent
  bus and the hooks daemon. Write `AUDIT-coral.md`.
- [x] ✅ **Task 1.2**: The owner decides: adopt, fork, borrow ideas, or leave. Owner's decision
  (2026-10-15): coral is not useful here; leave it, and keep it on the repo watch list (Plan
  00200\) so later changes are scanned for ideas. The audit counts as its first full review.

## Success Criteria

- [x] `AUDIT-coral.md` answers the four questions with evidence, and the owner has decided.

## Delivery & Milestones

- Audit: delivered in [`AUDIT-coral.md`](AUDIT-coral.md). Recommendation: leave coral, and
  borrow its task-dependency, wait-don't-poll, cost-rollup and idle-escalation ideas into
  `ccy-teams` and the agent bus. Decided: not adopted; on the watch list (Plan 00200).
