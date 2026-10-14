# Plan 00192: Line in the sand: tooling is Go, bash only for small helpers

**Status**: In Progress
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: High

## Overview

The owner's decision (2026-10-14): no more tooling written in bash. Plan 00191 ported
`ccy-teams` and `ccy-sessions` to Go and showed why: bash binds names when a line runs, types
nothing, and its interactive tools can only be tested through a pseudo-terminal. From now on
bash is for very small helpers only; anything over a measured threshold is written in Go in
`src-go/`. The rule is written into the root `CLAUDE.md` and `README.md` and enforced by a QA
gate, with the existing large scripts on a ratchet allowlist that may only shrink.

The existing bash (and then the standalone Python the Go tools call) is inventoried and the
large tools are rewritten in Go, one feature branch per programme, by the Plan 00191 method:
the existing suite is the parity oracle, run against the Go binary, before the cut-over.

go-qa-ci (Plan 00191 Phase 3) is reviewed for completeness, and this repository starts
recording its own Defence Before Fix rules for the defect classes the ports found.

## Goals

- The rule, with pragmatic thresholds, in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md`
  linked from the root `CLAUDE.md`.
- A QA gate in `qa-all.bash` and `qa-changed.bash` that fails a new bash file over the
  thresholds and an allowlisted file that grows; the allowlist only shrinks.
- An inventory of bash and Python with a ranked list of rewrite programmes.
- Each major rewrite on its own feature branch, merged after full QA and a review.
- Project DBF rules for the defect classes found in the ports.
- A Fable adversarial review of the whole and its findings fixed.

## Non-Goals

- Python that Ansible loads in-process (plugins, filters, modules, callbacks) or that imports
  Ansible: it stays Python.
- Bash that must run before Go exists (the bootstrap) or is sourced into a user's shell.
- The `ccy` launcher's port: assessed in Plan 00191 (`ASSESS-ccy-launcher.md`), waiting on
  the owner's decisions D1-D10 there.

## Tasks

### Phase 1: The rule

- [ ] 🔄 **Task 1.1**: Inventory of tracked bash and Python with sizes, invokers, tests and a
  port/keep class: [INVENTORY.md](INVENTORY.md).
- [ ] ⬜ **Task 1.2**: Thresholds chosen from the inventory's counts; the rule in `CLAUDE.md`
  and `README.md`; `src-go/CLAUDE.md` linked from the root `CLAUDE.md`.
- [ ] ⬜ **Task 1.3**: The gate, in Go, with its ratchet allowlist, wired into `qa-all.bash`
  and `qa-changed.bash`; control fixtures prove it fails.

### Phase 2: DBF rules and go-qa-ci

- [ ] 🔄 **Task 2.1**: Audit go-qa-ci's state and rule mechanisms; list the defect classes
  the ports found and whether a rule catches each.
- [ ] ⬜ **Task 2.2**: Project DBF rules for those classes, each proven red then green.

### Phase 3: Rewrites, one feature branch each

- [ ] ⬜ **Task 3.1**: Programmes from the inventory, ranked; each its own branch.
  Branch `go-port/freeze` (P7): functional spec written, [SPEC-freeze.md](SPEC-freeze.md); Go
  port in waves: library (`internal/freeze`) and podfreeze (`internal/podfreeze`,
  `cmd/podfreeze`, shared `internal/freeze/cli`) done; lxcfreeze and the cut-over to come.

### Phase 4: Review

- [ ] ⬜ **Task 4.1**: Fable adversarial review of the rule, gate, rules and rewrites; findings
  fixed.
- [ ] ⬜ **Task 4.2**: qa-reviewer over the plan.

## Success Criteria

- [ ] A new bash file over the thresholds fails QA; an allowlisted one that grows fails QA.
- [ ] Every port in Phase 3 passes its existing suite against the Go binary before cut-over.
- [ ] Each defect class found in the ports has a rule, or a recorded reason it cannot.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
