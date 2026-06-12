# Plan 00050: Fedora 44 Migration Tracking

**Status**: 🔄 In Progress (research phase only — no action plan yet)
**Created**: 2026-06-12
**Owner**: Claude (Fable 5 multi-agent workflow) / joseph
**Priority**: Medium
**Type**: Migration Tracking / Research

## Overview

This repository currently targets **Fedora 43** (`vars/fedora-version.yml` → `fedora_version: 43`). Fedora 44 is the next target. This plan tracks everything in the repo that is version-sensitive and may break or need adjustment when the target moves from Fedora 43 to Fedora 44 — package and repository availability, the Python stack, GNOME Shell / extension compatibility, hardware/kernel modules, the install/bootstrap path, and the many hardcoded version literals scattered across scripts and docs.

**This is the research and planning phase only.** No fixes are made and no execution action plan is written yet. A dynamic multi-agent workflow (Fable 5) swept the codebase across six version-sensitivity dimensions, cross-referencing each against the live Fedora 44 changeset/release notes. Findings live in [research/](research/) (one document per dimension); the ranked summary with links is in [triage.md](triage.md).

The deliverable of this phase is **knowledge, not change**: a clear, evidence-based map of what the Fedora 44 bump will touch, so a future batch can be planned and executed deliberately.

## Goals

- ✅ Establish a tracking plan for the Fedora 43 → 44 migration
- ✅ Evidence-based sweep of every version-sensitive area of the repo (six dimensions)
- ✅ Cross-reference each dimension against the actual Fedora 44 release facts (GNOME version, Python version, kernel, package/repo changes) via live web research
- ✅ One research document per dimension plus a ranked [triage.md](triage.md) with links
- ⬜ (Future, separate decision) Convert triage into an execution action plan and perform the bump

## Non-Goals

- **No fixes in this phase.** Nothing is edited, bumped, or deployed. `fedora_version` stays at 43 until the migration is explicitly planned and executed.
- No execution/action phases — deliberately deferred ("no further planning as yet").
- No history rewrites, no CI changes, no container rebuilds.
- Not auditing general repo health — that is Plan 00049's remit. This plan is scoped strictly to **what changes because the Fedora target changes**.

## Research Dimensions

| #   | Dimension                       | Prefix | Document                                                       |
| --- | ------------------------------- | ------ | -------------------------------------------------------------- |
| 1   | Version literals & gating logic | `VER`  | [research/version-refs.md](research/version-refs.md)           |
| 2   | Packages, repos & DNF           | `PKG`  | [research/packages-repos.md](research/packages-repos.md)       |
| 3   | Python stack                    | `PY`   | [research/python.md](research/python.md)                       |
| 4   | GNOME Shell & extensions        | `EXT`  | [research/gnome-extensions.md](research/gnome-extensions.md)   |
| 5   | Hardware & kernel modules       | `HW`   | [research/hardware-kernel.md](research/hardware-kernel.md)     |
| 6   | Install & bootstrap path        | `BOOT` | [research/install-bootstrap.md](research/install-bootstrap.md) |

## Research Results Summary

55 findings across the six dimensions, ranked in [triage.md](triage.md). No fixes made.

| Severity | Count | Where the list lives                                                                  |
| -------- | ----- | ------------------------------------------------------------------------------------- |
| High     | 7     | [triage.md → High](triage.md#high--will-break-f44-install-or-a-core-flow-7)           |
| Medium   | 14    | [triage.md → Medium](triage.md#medium--likely-breaks-or-needs-a-deliberate-change-14) |
| Low      | 25    | [triage.md → Low](triage.md#low--minor-adjustment--verify-on-f44-25)                  |
| Info     | 9     | [triage.md → Info](triage.md#info--observations--no-migration-action-required-9)      |

**Confirmed Fedora 44 baseline** (live research): released 2026-04-28; **GNOME 50**; kernel 6.19 at GA → **7.0.x** by June 2026; **Python stays 3.14** (no interpreter break); **DNF5 migration complete**; toolchain GCC 16.1 / glibc 2.43.

**The 7 highs**: VER-01/BOOT-01 (the `fedora_version` pin gates everything), VER-02 (undefined `warn` crashes setup.bash on the mismatch path), PKG-01 (dnf5 rejects `config-manager --enable` → core play halts), PKG-02 (`pgdev/ghostty` Copr has no F44 build → core play halts), EXT-01 (all three custom extensions disabled on GNOME 50), HW-01 (fc43 kernel versionlock can block/half-install the fc44 kernel).

**The good news**: the bump's core is one line (`fedora_version: 44`) — all gates read it correctly; CCY containers are not Fedora-coupled (out of scope); no Python interpreter break; and almost every third-party repo already publishes for F44.

## Tasks

### Phase 1: Multi-Agent Research (this phase)

- [x] ✅ **Scope version-sensitive surface** of the repo
- [x] ✅ **Run research workflow**: six Fable agents, one per dimension, each combining codebase evidence with live Fedora 44 facts
- [x] ✅ **Write research docs**: one document per dimension under `research/`
- [x] ✅ **Consolidate** findings into [triage.md](triage.md), ranked by severity with links

### Phase 2: Decision Gate — plan the bump (awaiting user)

- [ ] ⬜ **Review research + triage** and decide scope/timing of the actual Fedora 44 bump
- [ ] ⬜ **Convert triage into an execution action plan** (separate phase — not done here)

## Severity Scale

- **High** — will break the Fedora 44 install or a core deploy/runtime flow if unaddressed.
- **Medium** — likely to break or definitely needs a deliberate change for Fedora 44.
- **Low** — minor adjustment, or "verify on F44" polish.
- **Info** — observation / no action required to migrate.

## Dependencies

- Related: Plan 00049 (full repo audit) — DOC-07 there flags stale Fedora 42 references on the F43 branch; this plan supersedes the version-bump concern for the 43 → 44 move.
- This plan's Phase 2 (execution) depends on the user's go-ahead.

## Success Criteria (research phase)

- [x] Every version-sensitive area has an evidence-backed research document
- [x] triage.md ranks all findings with working links to the evidence
- [x] Fedora 44 specifics (versions, package/repo changes) are sourced from live research, not assumed
- [ ] User has what they need to decide whether/when to plan the bump

## Notes & Updates

### 2026-06-12

- Plan created. Research-only phase: a dynamic Fable workflow swept six version-sensitivity dimensions against the live Fedora 44 changeset. Results in `research/` and `triage.md`. No code changed; `fedora_version` remains 43. Execution deliberately deferred.
