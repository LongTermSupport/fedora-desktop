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

- [x] ✅ **Task 1.1**: Inventory of tracked bash and Python with sizes, invokers, tests and a
  port/keep class: [INVENTORY.md](INVENTORY.md).
- [x] ✅ **Task 1.2**: Thresholds chosen from the inventory's counts (60 code lines, 4
  functions, no associative arrays, no interactive UI, no data parsing past one `jq -r`); the
  rule in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md` linked from the root `CLAUDE.md`.
- [x] ✅ **Task 1.3**: The gate, in Go (`cmd/qa-bash-limits`, `internal/bashlimits`), with its
  ratchet allowlist `scripts/bash-limits.allowlist` (records only move down; `--base` refuses
  an added entry or a raised record), wired into `qa-all.bash`, `qa-changed.bash` and CI;
  controls (over-long, too many functions, `declare -A`, `fzf`, growth, an added entry) fail.

### Phase 2: DBF rules and go-qa-ci

- [x] ✅ **Task 2.1**: go-qa-ci audited: [AUDIT-go-qa-ci.md](AUDIT-go-qa-ci.md). Complete with
  recorded gaps; its bundled rule set is one analyzer; this repo had no project rules.
- [x] ✅ **Task 2.2**: Project DBF rules as `go/analysis` analyzers in the nested module
  `src-go/qa` (so the shipped binaries stay dependency-free), run by `qa-go.bash`: one process
  seam, owned programs (sudo, tmux, gum, fzf, systemctl, git), no wall clock in tests, one home
  abbreviation; catalogue [GoProjectRules.md](../../GoProjectRules.md). Each red on a fixture of
  the historical defect, green on the tree after fixing the real instances; on landing they
  caught the bash-limits gate running git outside `gitx`.

### Phase 3: Rewrites, one feature branch each

- [ ] ⬜ **Task 3.1**: Programmes from the inventory, ranked; each its own branch.
  Branch `go-port/freeze` (P7): functional spec written, [SPEC-freeze.md](SPEC-freeze.md); Go
  port in waves: library (`internal/freeze`) and podfreeze (`internal/podfreeze`,
  `cmd/podfreeze`, shared `internal/freeze/cli`) and lxcfreeze (`internal/lxcfreeze`,
  `cmd/lxcfreeze`, shared flow in `internal/freeze/app`) done; cut-over (wave 4) done on the
  branch: golden replay of the bash tools' recorded output (`internal/freeze/golden`), one task
  file builds any Go command (`tasks/build-go-command.yml`), the bash tools, library, suites and
  gates deleted, the host check in [ACCEPTANCE-freeze.md](ACCEPTANCE-freeze.md) with the owner
  (via `deploy.bash` in `meta-deploy.bash`: it saves the bash tools' output first and ends with
  `acceptance.bash`, which compares the Go tools with it). The Fable review's fixes are in:
  sudo asks on `/dev/tty`, Ctrl-C and a lapsed sudo timestamp end an act batch cleanly, the
  sudo seam methods are judged by `project_seam_commands`, the inventory is generic over the
  engine's record.
- [ ] 🔄 **Task 3.2**: `agent-bus-install` on `go-port/agent-bus-install`:
  its spec `SPEC-agent-bus-install.md` is on the branch; wave 1 (root-guarding
  validation) and wave 2 (every subcommand, the host adapters, the Go binary and the suite in
  both modes) done; the cut-over wave (play build and install, declare digest, deleting the
  bash installer) is next. Runs as root and changes behaviour (its D7), so it merges only after
  review and the owner's host test.
- [ ] ⬜ **Task 3.3**: The rest of the inventory's programmes, ranked there; the `ccy` launcher
  waits on Plan 00191's D1-D10.

### Phase 4: Review

- [ ] 🔄 **Task 4.1**: Fable adversarial review of the rule, gate, rules and rewrites; findings
  fixed. Three reviews (subagent-reports/261014-fable-review-{gate,go,gqaci}.md) found
  blockers in each: the gate is evadable and its CI ratchet bypassable; the DBF rules caught
  1 of 13 planted evasions and Ctrl-C can wedge the terminal; go-qa-ci cannot be adopted (no
  nested-module driver, disabled linters counted as suppressions) and ships one rule. Fixing in
  parallel: the gate on a real bash parser with a versioned ratchet; type-based rules; runtime
  correctness; go-qa-ci M15 (adoption, gate) and M16 (a bundled rule pack). All five are
  merged (go-qa-ci's on its own remote), and so are typed state, the ccy-sessions and
  ccy-teams redraw cost, and the `teams` package split. The two ports get their own review
  before merge: freeze's found two blockers, being fixed on its branch.
- [ ] ⬜ **Task 4.2**: qa-reviewer over the plan.

## Success Criteria

- [ ] A new bash file over the thresholds fails QA; an allowlisted one that grows fails QA.
- [ ] Every port in Phase 3 passes its existing suite against the Go binary before cut-over.
- [ ] Each defect class found in the ports has a rule, or a recorded reason it cannot.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
