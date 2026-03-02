# Plan 020: Semgrep Custom Bash Rules

**Status**: In Progress
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

- [ ] ⬜ **Task 1.1**: Verify semgrep bash support and install method
  - [ ] ⬜ Check `pip install semgrep` vs system package vs binary download
  - [ ] ⬜ Confirm which semgrep version supports bash pattern matching
  - [ ] ⬜ Test a basic bash rule locally in the container
- [ ] ⬜ **Task 1.2**: Add semgrep to CCY Dockerfile
  - [ ] ⬜ Read current `files/var/local/claude-yolo/Dockerfile`
  - [ ] ⬜ Add semgrep install (prefer pip/pipx for version pinning)
  - [ ] ⬜ Verify `semgrep --version` works in built container
  - [ ] ⬜ Bump `CCY_VERSION` as required by CLAUDE.md

### Phase 2: Write Custom Rules

Create `.semgrep/bash-conventions.yml` with rules for:

- [ ] ⬜ **Task 2.1**: Rule — ban `|| echo` error hiding
  - Pattern: `$CMD || echo $MSG` (any command followed by `|| echo`)
  - Message: "Error hiding detected. Use: || { echo 'ERROR: ...'; exit 1; }"
  - Severity: ERROR
  - Test with a positive match (existing bad pattern) and negative match (correct pattern)

- [ ] ⬜ **Task 2.2**: Rule — ban `|| true` on non-cleanup lines
  - Pattern: `$CMD || true`
  - Message: "Swallowing errors with || true. Handle the error explicitly or add # semgrep-disable if this is intentional cleanup"
  - Severity: WARNING (not ERROR — `|| true` is legitimate in some cleanup contexts)
  - Note: May have false positives; needs a disable comment escape hatch

- [ ] ⬜ **Task 2.3**: Rule — require `set -euo pipefail` in scripts
  - Detect bash scripts missing `set -euo pipefail` near the top
  - Severity: ERROR
  - Note: Semgrep may struggle with "absence of pattern" — research approach first

- [ ] ⬜ **Task 2.4**: Validate rules with semgrep's built-in test runner
  - Each rule should have test cases in `.semgrep/tests/`
  - `semgrep --test .semgrep/` must pass

### Phase 3: QA Integration

- [ ] ⬜ **Task 3.1**: Add semgrep to `scripts/qa-all.bash`
  - [ ] ⬜ Read current `scripts/qa-all.bash` to understand structure
  - [ ] ⬜ Add semgrep run after shellcheck section
  - [ ] ⬜ Run only against `files/`, `scripts/`, `fedora-install/` (not node_modules etc.)
  - [ ] ⬜ Fail QA if semgrep finds ERROR-level issues
  - [ ] ⬜ Warn (not fail) for WARNING-level issues
- [ ] ⬜ **Task 3.2**: Fix any existing violations found by new rules
  - [ ] ⬜ Run semgrep against full codebase
  - [ ] ⬜ Fix all ERROR-level violations
  - [ ] ⬜ Review WARNING-level violations, fix or add disable comments

### Phase 4: Documentation & Commit

- [ ] ⬜ **Task 4.1**: Update CLAUDE.md QA section to mention semgrep
- [ ] ⬜ **Task 4.2**: Run `./scripts/qa-all.bash` — must pass fully
- [ ] ⬜ **Task 4.3**: Commit all changes with plan reference

## Technical Decisions

### Decision 1: Semgrep install method
**Context**: Semgrep can be installed via pip, pipx, or as a pre-built binary.
**Options**:
1. `pip install semgrep` — simple, widely available
2. Pre-built binary from GitHub releases — faster, no Python dep
3. System package — not always up to date

**Decision**: Use `pip install semgrep` in the Dockerfile — keeps it consistent with other
Python tooling already in the container. Pin to a specific version for reproducibility.
**Date**: 2026-03-02

### Decision 2: Rule severity for `|| true`
**Context**: `|| true` is sometimes legitimate (cleanup in error handlers, releasing locks).
**Decision**: WARNING not ERROR, with a `# nosemgrep` or `# ok: || true` comment escape hatch
for intentional uses.
**Date**: 2026-03-02

## Success Criteria

- [ ] `semgrep --version` works inside the CCY container
- [ ] `.semgrep/bash-conventions.yml` contains at minimum rules 2.1 and 2.2
- [ ] `semgrep --test .semgrep/` passes (rules have test cases)
- [ ] `./scripts/qa-all.bash` runs semgrep and fails on ERROR-level violations
- [ ] No existing ERROR-level violations remain in the codebase
- [ ] QA runtime increase is acceptable (< 10 seconds added)

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Semgrep bash support limited | High | Medium | Test before committing to approach; fallback to grep-based custom rules in qa-all.bash |
| Too many false positives | Medium | Medium | Start with high-confidence rules only; add disable comment mechanism |
| Semgrep too slow on codebase | Low | Low | Scope to specific directories; use `--include` flags |
| CCY build breaks | High | Low | Test Dockerfile change in isolation first |

## Notes & Updates

### 2026-03-02
- Plan created. Context: user wants ESLint/PHPStan-style custom rule enforcement for bash.
- Key motivation: firstboot script was hiding Ansible failures with `|| echo "WARNING"` — a
  regression that Semgrep rules would have caught.
- Semgrep chosen over custom grep scripts because it has a proper rule/test ecosystem.
