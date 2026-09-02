# Plan 00068: Make ccy fully non-interactive so CI can invoke it

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

> This is the lean plan. The full pre-slimming prose, kept verbatim, is in
> [PLAN_archive.md](PLAN_archive.md). Durable decisions, requirements, evidence and the risk
> table are in [DECISIONS.md](DECISIONS.md). Dated narrative is in [JOURNAL/](JOURNAL/).

## Overview

lts-infra Plan 00030 has a self-hosted runner fetch a repo's checkout, check out the job's SHA,
and launch `ccy` in the repo root with the instruction for that GitHub event. `ccy` behaves
normally there, including rebuilding the project image when `.claude/ccy/Dockerfile` changes.
That is the product, not a hazard to design around
([DECISIONS.md §1](DECISIONS.md#1-the-architecture-this-serves)).

This plan specifies what `ccy` must gain to be launchable unattended. The governing framing
([DECISIONS.md §2](DECISIONS.md#2-the-framing-correction-that-governs-everything)): a CI mode is
a different flow through the launcher, sharing its libraries and the image and token seams. It is
not the desktop flow with guards on it, and it is not a parallel CI product beside `ccy`.

This plan specifies; it does not build. Implementation is handed to a separate plan (Task 4.1).

## Goals

- **Specify a CI mode**: resolve the image, resolve credentials from what it was handed, run with
  a fixed flag set, exit with the container's status. Reuses `lib/*.bash` and the image and token
  seams; never walks the desktop's interactive discovery path.
- Token-first credentials, MCP injection, a restricted tool surface, and no desktop-only
  device, GUI or preflight assumptions
  ([DECISIONS.md §3](DECISIONS.md#3-what-ci-needs-from-ccy)).
- **Do not regress the desktop.** A safety property to be tested, not a constraint that forces CI
  through the desktop's code path.

## Non-Goals

- **No implementation.** No file outside this plan folder is modified.
- **No parallel CI product.** No second launcher, and no reimplementation of image staleness or
  the project `Dockerfile` (Decision 1). A CI credential path (token-first) and a CI-specific flow
  are in scope; only the parallel product is out.
- No `LABEL` identity convention (Decision 2).
- **No `--egress`** (Decision 8), and no assumption either way about the runner's own egress
  posture. That is lts-infra's to decide.
- No permission surface on the desktop. Decision 4 stands there and is reversed only for CI by
  Decision 9.

## Context & Background

- Requirements, the "already solved, do not rebuild" table, and the working rule:
  [DECISIONS.md §2–3](DECISIONS.md#2-the-framing-correction-that-governs-everything).
- Accepted risk, the Claude OAuth token being readable inside every container:
  [DECISIONS.md §4](DECISIONS.md#4-accepted-stated-risk--the-claude-oauth-token-is-readable-inside-every-container).
- Decisions 1–9: [DECISIONS.md §5](DECISIONS.md#5-technical-decisions).
- Open owner decisions, proof obligations, risks:
  [DECISIONS.md §8–10](DECISIONS.md#8-open-decisions--owner).
- Reports: [host-run-verdicts.md](reports/host-run-verdicts.md),
  [ci-required-config.md](reports/ci-required-config.md), [ci-flow.md](reports/ci-flow.md),
  [mcp-and-egress.md](reports/mcp-and-egress.md).
- Probes: `triage.bash` (HOST only) drives `probe-engine.bash`, `probe-launcher.bash` and
  `probe-network.bash`.
- The folder was truncated at commit `0dde4f0`: twenty-two reports and one probe that specified
  retracted mechanisms were deleted, not archived. Git has them; nothing here should be
  reconstructed from them.

## Tasks

### Phase 1 — Ground the unverified claims

- [x] ✅ **Task 1.1**: Host facts collected and verdicts recorded in
  [reports/host-run-verdicts.md](reports/host-run-verdicts.md). Run `20260731-225344` is
  authoritative. E6 confirmed a blocker; C3 confirmed by first direct measurement.
- [x] ✅ **Task 1.2**: Enumerate all prompt sites: 46, carried into
  [reports/ci-required-config.md](reports/ci-required-config.md).
- [x] ✅ **Task 1.3**: Confirm what `play-claude-yolo.yml` deploys and how the image is built.

### Phase 2 — `--non-interactive`

- [x] ✅ **Task 2.1**: The fail-fast contract, site by site, in
  [reports/ci-required-config.md](reports/ci-required-config.md). 46 sites classified; no new
  message format. Of 15 preconditions originally specified, 4 survive.

### Phase 3 — Re-specify the remaining scope against the current architecture

The scope below is the owner's, settled 2026-08-01 (Decisions 7, 8, 9).

- [x] ✅ **Task 3.1**: MCP injection is required (Decision 7). Design in
  [reports/mcp-and-egress.md](reports/mcp-and-egress.md) with its dead premises marked;
  DECISIONS.md is authoritative where they differ. Outstanding sub-question: the missing env
  passthrough for the MCP server's token.
- [x] ✅ **Task 3.2**: `--egress` dropped by the owner (Decision 8). ccy gets unfettered egress at
  launch. C3 retained for any future revisit.
- [x] ✅ **Task 3.5**: The CI flow, in [reports/ci-flow.md](reports/ci-flow.md). Eight steps, each
  naming what it reuses and what it never enters. Requirement 1 re-derived: about 6 reachable
  prompt sites, not 46, all credential resolution. Compose and networking are CI requirements;
  what CI drops is the negotiation
  ([DECISIONS.md §6](DECISIONS.md#6-task-35--the-ci-flow-and-composenetworking)). The site
  count is a derivation, not a measurement: confirm by instrumenting the CI path before
  implementing.
- [ ] ⬜ **Task 3.4**: The CI tool surface (Decision 9). No longer gated: `--disallowedTools`
  composes with `--dangerously-skip-permissions`, measured 2026-08-10. Remaining work:
  1. Specify the per-event surfaces: `push`/`pull_request` (may run `ci.bash` and read; no write,
     commit or push) and `issues`/`issue_comment` (narrower, no `ci.bash`).
  2. Derive every layer from one per-class list, and assert the tool names absent from the
     session, never a tool count.
  3. Prefer an allowlist wherever the vocabulary is not ours; a denylist fails open on a typo.
- [x] ✅ **Task 3.3**: Unattended-launch hygiene: four cited defects, each with a guard-shaped fix
  ([DECISIONS.md §7](DECISIONS.md#7-task-33--unattended-launch-defects)). The container-naming
  race (3.3.1) is primarily fixed by serialising jobs on the runner, lts-infra Plan 00030
  Task 2.8.

### Phase 4 — Hand off to implementation

Ungated since 2026-08-10: the scope is the owner's and the restriction mechanism is settled by
measurement (E9).

- [ ] ⬜ **Task 4.1**: Create the implementation plan.

## Dependencies

- **Blocks**: the ccy CI implementation plan, not yet created.
- **Consumed by**: lts-infra Plan 00030, which owns the runner-side dispatch.

## Success Criteria

- [x] ✅ Every claim is cited to a `file:line` or explicitly marked unverified.
- [x] ✅ A reader can say what happens to an existing `.claude/ccy/Dockerfile`: nothing.
- [x] ✅ Desktop ccy is provably unaffected when the new flags are absent.
- [x] ✅ The fully non-interactive contract is specified site by site.
- [x] ✅ Task 1.1's host facts are answered by a run, not by inference.
- [x] ✅ Every surviving report describes a live mechanism.
- [x] ✅ MCP, `--egress` and the unattended-launch capabilities are resolved against the current
  architecture, and by the owner rather than by this plan's internal reasoning.
- [x] ✅ The restriction mechanism is settled by measurement (E9), and E8 dissolved with it.
- [ ] ⬜ The CI tool surface is specified per event.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00068-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phases 1 and 2 delivered: host verdicts and the 46-site fail-fast contract.
- Folder truncated to live mechanisms only: `0dde4f0`.
- Phase 3 scope settled by the owner (Decisions 7, 8, 9); Tasks 3.1, 3.2, 3.3, 3.5 delivered.
- E9 measured; Phase 4 ungated.
- Plan slimmed: history to `PLAN_archive.md`, decisions to `DECISIONS.md`.
