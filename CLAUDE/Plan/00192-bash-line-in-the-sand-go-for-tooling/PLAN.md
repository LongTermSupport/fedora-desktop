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
large tools are rewritten in Go, one feature branch per programme. The owner's method
(2026-10-14), from qa-ansible on: a spec of intended behaviour as clear expectations, each a
test written first; the bash is read for requirements and never treated as correct, and no
replay of it is recorded (src-go/CLAUDE.md, "How a port is built"). The earlier ports' replays
stay as regression tests.

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
- [x] ✅ **Task 1.2**: Thresholds chosen from the inventory's counts (60 code lines, 90
  statements, 4 functions, no associative arrays, no interactive UI, no data parsing past one `jq -r`); the
  rule in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md` linked from the root `CLAUDE.md`.
- [x] ✅ **Task 1.3**: The gate, in Go (`src-go/qa/cmd/qa-bash-limits`, `src-go/qa/bashlimits`), with its
  ratchet allowlist `scripts/bash-limits.allowlist` (records only move down; `--base` refuses
  an added entry or a raised record), wired into `qa-all.bash`, `qa-changed.bash` and CI;
  controls (over-long, too many functions, `declare -A`, `fzf`, growth, an added entry) fail.

### Phase 2: DBF rules and go-qa-ci

- [x] ✅ **Task 2.1**: go-qa-ci audited: [AUDIT-go-qa-ci.md](AUDIT-go-qa-ci.md). Complete with
  recorded gaps; its bundled rule set is one analyzer; this repo had no project rules.
- [x] ✅ **Task 2.2**: Project DBF rules as `go/analysis` analyzers in the nested module
  `src-go/qa` (so the shipped binaries stay dependency-free), run by `qa-go.bash`: one process
  seam, owned programs (sudo, tmux, gum, fzf, systemctl, git), tmux target forms, no wall clock in
  tests, one home abbreviation, a `WaitDelay` that names its error (six rules); catalogue [GoProjectRules.md](../../GoProjectRules.md). Each red on a fixture of
  the historical defect, green on the tree after fixing the real instances; on landing they
  caught the bash-limits gate running git outside `gitx`.

### Phase 3: Rewrites, one feature branch each

- [ ] 🔄 **Task 3.1**: Programmes from the inventory, ranked; each its own branch.
  `go-port/freeze` (P7), podfreeze and lxcfreeze: merged into F44 and pushed (`f0284848`). Spec
  [SPEC-freeze.md](SPEC-freeze.md); the library (`internal/freeze`), the two engines and the
  shared flow in Go; golden replay of the bash tools' recorded output
  (`internal/freeze/golden`); one task file builds any Go command
  (`tasks/build-go-command.yml`); the bash tools, library, suites and gates deleted. The Fable
  review's fixes are in (sudo asks on `/dev/tty`, Ctrl-C and a lapsed sudo timestamp end an act
  batch cleanly, the sudo `-n true` probe, the inventory generic over the engine's record).
  Host acceptance: deployed by `meta-deploy.bash`. Output parity with the bash tools is
  established: the first run matched byte for byte on three of the four commands (the fourth
  stopped on a sudo prompt the Go sudo probe raised wrongly, now fixed), and the second run,
  with no prompt, differed only by the containers started between the runs, every common row
  identical. A live comparison against a saved baseline cannot repeat on a machine whose
  containers change, so `acceptance.bash` is retired from the queue. Left for the owner: the
  freeze, thaw and menu look in [ACCEPTANCE-freeze.md](ACCEPTANCE-freeze.md) steps 3 and 4.
- [ ] 🔄 **Task 3.2**: `agent-bus-install` on `go-port/agent-bus-install`: spec
  `SPEC-agent-bus-install.md` on the branch; built, reviewed and its review fixed, not merged.
  It runs as root and changes behaviour (its D7), so it merges only after the owner's host test
  as root (the branch's `ACCEPTANCE-agent-bus-install.md`).
- [ ] 🔄 **Task 3.3**: The rest of the inventory's programmes, ranked there; the `ccy` launcher
  waits on Plan 00191's D1-D10. `qa-changed.bash` (P3, first of the QA orchestrators): done,
  merged into F44 and pushed (`de85afbc`); spec [SPEC-qa-changed.md](SPEC-qa-changed.md), nested
  `src-go/qa/changed`, routing and excluded trees as data, one process seam shared with the
  bash-limits gate, golden replay of the bash over 118 scenarios with 19 numbered deviations,
  the script a build-and-run wrapper. `qa-all.bash` (P3, the second orchestrator) on
  `go-port/qa-all`: ported, reviewed, its review fixed, and merging into F44 together with the
  other pending branches through that one integration branch; spec
  [SPEC-qa-all.md](SPEC-qa-all.md); nested `src-go/qa/all`, `qa/gates`, `qa/cmd/qa-all`; the
  gate list is data (`scripts/qa-gates.tsv`), read by the runner, by `qa-changed` and by the
  QA.md inventory gate; a reader that finds nothing fails its gate; golden replay of the bash
  over 142 scenarios with 32 pinned deviations (B1-B6, B12, B13, B20-B23, B26); the script a
  build-and-run wrapper, its allowlist entry, the `qa-helper-summary` library, its suite and the
  call-site parser gone (their cases are Go tests). The full `qa-all` run comes before the merge.
  `qa-ansible.bash` (P3, third orchestrator) on `go-port/qa-ansible`: ported, not merged; spec
  [SPEC-qa-ansible.md](SPEC-qa-ansible.md) states the intended behaviour as expectations E1-E19
  (the bash is not an oracle; 13 departures B1-B13 listed there); nested `src-go/qa/ansible`,
  `qa/cmd/qa-ansible`; the script a build-and-run wrapper, its allowlist row gone;
  `test-qa-ansible-failfast.bash` now drives the built tool. Same output and report as the bash on
  the live tree. Fable review (subagent-reports/261014-fable-review-qa-ansible.md) dispositions:
  F1 quoted values evaded the fail-fast rules, fixed (E11, B14; the live tree had no quoted
  violation); F2 symlink to a directory or dangling non-`.yml` link, fixed as exit 2 (E7, B16);
  F3 owner decision, the gate judges what git would publish and never reads gitignored files
  (E6a, B15); F4 `CLAUDE/QA.md` updated; F5 stdout write failure now an `ERROR:` line (E3, B17);
  F6 (`-x` is root's view, any execute bit) and F7 (four passes over each file) are info only,
  accepted, no change.

### Phase 4: Review

- [ ] 🔄 **Task 4.1**: Fable adversarial review of the rule, gate, rules and rewrites; findings
  fixed. Three reviews (subagent-reports/261014-fable-review-{gate,go,gqaci}.md) found
  blockers in each: the gate is evadable and its CI ratchet bypassable; the DBF rules caught
  1 of 13 planted evasions and Ctrl-C can wedge the terminal; go-qa-ci cannot be adopted (no
  nested-module driver, disabled linters counted as suppressions) and ships one rule. Fixing in
  parallel: the gate on a real bash parser with a versioned ratchet; type-based rules; runtime
  correctness; go-qa-ci M15 (adoption, gate) and M16 (a bundled rule pack). All five are
  merged (go-qa-ci's on its own remote), and so are typed state, the ccy-sessions and
  ccy-teams redraw cost, and the `teams` package split. The ports get their own review before
  merge: freeze's found two blockers, fixed and merged; qa-changed's and qa-all's findings are
  fixed; agent-bus-install's are fixed on its branch.
- [ ] 🔄 **Task 4.2**: qa-reviewer over the plan: done once
  ([subagent-reports/261014-qa-reviewer-plan.md](subagent-reports/261014-qa-reviewer-plan.md));
  its fixes are on `go-port/qa-all` ([the fix report](subagent-reports/261014-qa-reviewer-fixes-sonnet.md)).
  Runs again over the finished plan.

## Success Criteria

- [ ] A new bash file over the thresholds fails QA; an allowlisted one that grows fails QA.
- [ ] Every port in Phase 3 passes its existing suite against the Go binary before cut-over.
  Where the bash tool and its suites were deleted, the oracle was replaced, not dropped: the
  freeze tools' white-box bash suites are replaced by a golden replay of the shipped bash tools'
  recorded output (stdout, stderr, status and commands over a scenario corpus) plus Go tests, and
  qa-all's by a golden replay of the legacy script plus the Go tests that took over the cases of
  the deleted `qa-helper-summary` suite. That meets the criterion because the replay compares the
  Go with the bash byte for byte, and a difference is allowed only as a numbered bug pinned with
  its reason, which a suite of a deleted tool could no longer do.
- [ ] Each defect class found in the ports has a rule, or a recorded reason it cannot
  ([GoProjectRules.md](../../GoProjectRules.md): `project_waitdelay` for the dropped capture
  (B25); the `sudo -n -v` probe is recorded as not a rule, with what holds it).

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
