# Plan 020: Semgrep Custom Bash Rules

**Status**: 🟢 Complete
**Created**: 2026-03-02
**Owner**: Agent
**Priority**: Medium

## Overview

Add [Semgrep](https://semgrep.dev) to the project as a custom rule enforcement tool for bash
scripts — the equivalent of PHPStan/ESLint for this codebase. Shellcheck covers many static
analysis cases but cannot enforce project-specific conventions. Semgrep supports user-written
YAML rules that can catch patterns shellcheck cannot, such as error hiding, missing fail-fast
headers, and other project conventions.

The project has a strong "fail fast, no error hiding" principle. This has already caught real
bugs: the firstboot script was silently swallowing Ansible failures with `|| echo "WARNING"`.
Semgrep rules will prevent regressions of these patterns.

## Goals

- Install Semgrep in the CCY Dockerfile so it is available in the dev container
- Write a starter set of custom rules enforcing project bash conventions
- Integrate Semgrep into `scripts/qa-all.bash` so it runs on every QA check
- Rules must run fast enough not to annoy developers (< 5 seconds on this codebase)

## Non-Goals

- Replacing shellcheck — both tools complement each other
- Writing rules for Python or YAML files (scope creep)
- Achieving 100% false-positive-free rules immediately — iterative improvement is fine
- Enforcing code style (indentation etc.) — that is `shfmt` territory

## Context & Background

The project's bash conventions (from CLAUDE.md):
- **Fail Fast**: `set -euo pipefail` at the top of every script
- **No error hiding**: `|| echo "WARNING"` is banned; use `|| { echo "ERROR: ..."; exit 1; }`
- **Explicit error handling**: Every critical command must fail loudly

Semgrep supports bash via its generic/pattern matching engine. Rules are written in YAML and
can match patterns like `$CMD || echo $MSG` to catch error hiding.

The CCY Dockerfile is at `files/var/local/claude-yolo/Dockerfile`. The QA script is at
`scripts/qa-all.bash`. Custom Semgrep rules live in `.semgrep/` by convention.

## Tasks

### Phase 1: Research & Setup

- [x] ✅ **Task 1.1**: Verify semgrep bash support and install method
  - [x] ✅ Confirmed `pipx install semgrep` works (version 1.153.1)
  - [x] ✅ Bash pattern matching works — tested `$CMD || echo $MSG`
- [x] ✅ **Task 1.2**: Add semgrep to CCY Dockerfile
  - [x] ✅ Added `RUN pipx install semgrep` to Dockerfile
  - [x] ✅ Bumped container version 2.9 → 2.10
  - [x] ✅ Bumped CCY_VERSION 3.7.1 → 3.7.2

### Phase 2: Write Custom Rules

- [x] ✅ **Task 2.1**: Rule — ban `|| echo` error hiding
  - Pattern: `$CMD || echo $MSG` matches both statement-level AND `$(cmd || echo "default")`
  - Both are error hiding: the failure is swallowed either way
  - Rule in `.semgrep/bash-conventions.yml`
  - Severity: ERROR

- [x] ✅ **Task 2.4**: Validate rules with test cases
  - Test cases in `.semgrep/tests/bash-conventions.bash`
  - Manual verification: 4 expected matches, 0 false positives on `if ! cmd` pattern

### Phase 3: QA Integration

- [x] ✅ **Task 3.1**: Add semgrep to `scripts/qa-all.bash` via `qa-patterns.bash`
  - `qa-patterns.bash` rewritten to use semgrep (replaced grep-based approach)
  - JSON output format compatible with `qa-all.bash` merger
  - Exit code 2 on missing semgrep (same pattern as qa-bash.bash)
  - `qa-all.bash` updated to handle exit 2 from qa-patterns.bash
- [x] ✅ **Task 3.2**: Fix existing violations
  - `files/usr/local/bin/debug-pipewire.bash` — fixed `|| echo` at line 39
  - `files/var/local/claude-yolo/lib/network-management.bash` — fixed `$(load_network_preference 2>/dev/null || echo "")` at line 177
  - Full project scan: 0 violations remain

### Phase 4: Documentation & Commit

- [x] ✅ **Task 4.1**: Updated CLAUDE.md QA section to describe all three checks
- [x] ✅ **Task 4.2**: `./scripts/qa-all.bash` passes (306 files, 44 patterns checked)
- [x] ✅ **Task 4.3**: All changes committed with plan reference

## Technical Decisions

### Decision 1: Semgrep install method
**Context**: Semgrep can be installed via pip, pipx, or as a pre-built binary.
**Decision**: `pipx install semgrep` — keeps it isolated and avoids breaking system packages.
**Date**: 2026-03-03

### Decision 2: No $() exclusion
**Context**: The original plan considered excluding `$(cmd || echo "default")` from detection
as a "legitimate fallback idiom". After discussion, this pattern IS error hiding — the
failure of `cmd` is silently swallowed regardless of whether it's inside `$()` or not.
**Decision**: Flag ALL `|| echo` patterns, no exclusions. Use `if ! cmd` for explicit handling.
**Date**: 2026-03-03

### Decision 3: grep-based fallback dropped
**Context**: The grep-based `qa-patterns.bash` was partially implemented as a fallback.
**Decision**: Replaced entirely with semgrep once confirmed it works for bash.
**Date**: 2026-03-03

## Success Criteria

- [x] `semgrep --version` works inside the CCY container (installed via Dockerfile)
- [x] `.semgrep/bash-conventions.yml` contains the `|| echo` error-hiding rule
- [x] `./scripts/qa-all.bash` runs semgrep and fails on ERROR-level violations
- [x] No existing violations remain in the codebase
- [x] QA runtime increase is acceptable (< 10 seconds added)

## Notes & Updates

### 2026-03-02
- Plan created. Context: user wants ESLint/PHPStan-style custom rule enforcement for bash.
- Key motivation: firstboot script was hiding Ansible failures with `|| echo "WARNING"` — a
  regression that Semgrep rules would have caught.
- Semgrep chosen over custom grep scripts because it has a proper rule/test ecosystem.

### 2026-03-03
- Semgrep installed via `pipx install semgrep` (1.153.1).
- Tested bash support — `$CMD || echo $MSG` pattern works correctly.
- Decided to flag `$(cmd || echo "default")` too — it IS error hiding, not a "fallback idiom".
- Replaced grep-based qa-patterns.bash with semgrep integration.
- Fixed 2 violations: debug-pipewire.bash and network-management.bash.
- Full scan clean: 0 violations in 44 bash files.
- CCY Dockerfile updated (version 2.10), CCY_VERSION bumped to 3.7.2.
