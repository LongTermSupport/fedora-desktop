# Plan 024: Modular CLAUDE.md Restructure

**Status**: ⬜ Not Started
**Created**: 2026-04-02
**Owner**: Claude
**Priority**: High

## Overview

The root `CLAUDE.md` has grown to 40,466 bytes (1,014 lines), exceeding the 40k performance threshold. It mixes three categories of content: LLM-critical rules (enforced every conversation), LLM-reference docs (needed only when working in specific areas), and user-facing documentation (for humans, not Claude). This plan restructures the monolithic file into a modular architecture with strict single source of truth — zero duplication.

## Goals

- Reduce root `CLAUDE.md` to under 15k characters (front page with critical rules + `@` pointers)
- Move LLM-reference documentation into `CLAUDE/` directory as focused topic files
- Move user-facing documentation into `docs/` directory (which already exists with 14 files)
- Create subdirectory stub `CLAUDE.md` files with `@` forced-read pointers to `CLAUDE/` docs
- Eliminate all content duplication across the documentation tree
- Maintain 100% of existing guidance — nothing lost, everything reorganised

## Non-Goals

- Rewriting content (preserve existing wording unless deduplicating)
- Changing the planning workflow or `CLAUDE/PlanWorkflow.md`
- Modifying `.claude/rules/` (path-scoped rules are a future consideration, not this plan)
- Changing any code, playbooks, or scripts

## Context & Background

### Current State Analysis

**Root CLAUDE.md breakdown by section size:**

| Section | Chars | Classification |
|---------|-------|----------------|
| Development Principles (fail-fast, YAGNI, DRY, idempotent, security) | 11,363 | LLM-critical + LLM-reference |
| CCY Container Detection | 7,980 | LLM-critical + LLM-reference (ctrl+z patch) |
| Ansible Style Rules | 4,664 | LLM-reference |
| Special Features (GitHub multi-account, hardware, shell) | 3,887 | User-facing |
| Project Architecture | 1,970 | User-facing |
| Development Workflow | 1,834 | Mixed (plan commit rule is LLM-critical) |
| Debug Commands for Users | 1,726 | LLM-critical |
| Troubleshooting | 1,460 | User-facing |
| Ansible Configuration Patterns | 1,307 | LLM-reference |
| Technology Stack | 1,216 | User-facing |
| Security Considerations (vault management) | 1,176 | LLM-reference |
| Project Maintenance | 826 | User-facing |
| Project Overview | 676 | User-facing |

**Major duplications identified:**

1. **Ansible-Only Deployment ↔ Testing Workflow** — both say "edit → playbook → deploy → test", both forbid manual operations (~4,700 chars combined, could be ~2,000)
2. **Public Repository Warning ↔ Security First principle** — both cover secrets/vault/placeholders (~2,400 chars combined, could be ~1,500)
3. **Vault Management ↔ Public Repository Warning** — vault encryption explained twice
4. **Idempotency** — mentioned in Core Principles, Ansible Style Rules, and Ansible-Only Deployment

**Existing subdirectory CLAUDE.md files:**

| Location | Lines | Purpose |
|----------|-------|---------|
| `playbooks/CLAUDE.md` | 74 | Executable playbook requirements |
| `extensions/CLAUDE.md` | 323 | GNOME Shell extension development |
| `roles/vendor/lts.vault-scripts/shellscripts/CLAUDE.md` | 241 | Vault script conventions |

**Existing docs/ directory** (already has 14 files including README.md, architecture.md, development.md, configuration.md, etc.)

### @ Syntax Mechanics

- `@path/to/file` imports and expands file content into context
- Relative paths resolve from the file containing the import
- Subdirectory `CLAUDE.md` files load on-demand when Claude works in that directory
- Maximum 5 hops of recursive imports
- External imports (outside project) require user approval

## Technical Decisions

### Decision 1: Where LLM-reference docs live

**Context**: Need a home for detailed LLM guidance that doesn't belong in the front page.

**Options**:
1. `CLAUDE/` directory with topic files (e.g., `CLAUDE/AnsibleStyle.md`)
2. `.claude/rules/` with path-scoped frontmatter
3. Subdirectory `CLAUDE.md` files only (no central reference docs)

**Decision**: Option 1 — `CLAUDE/` directory. The `@` syntax in root `CLAUDE.md` provides explicit control over what loads. `.claude/rules/` is a future enhancement that can coexist. Subdirectory stubs will `@`-import from `CLAUDE/` to avoid duplication.

### Decision 2: How to handle the Ansible-Only / Testing Workflow duplication

**Context**: Two sections (~4,700 chars) say essentially the same thing.

**Decision**: Merge into single `CLAUDE/InfrastructureAsCode.md` covering the unified workflow: edit → playbook → deploy → test. Both the "never manual" rules and the "deploy first, test second" rules live there. Root `CLAUDE.md` gets a 2-line summary + `@` pointer.

### Decision 3: User-facing docs destination

**Context**: `docs/` already exists with 14 files. User-facing sections from CLAUDE.md overlap with existing docs.

**Decision**: Merge into existing `docs/` files where content overlaps (e.g., architecture → `docs/architecture.md`, GitHub multi-account → `docs/` new file). Don't create duplicate docs files.

## Tasks

### Phase 1: Create CLAUDE/ topic files (LLM-reference docs)

Extract LLM-reference content from root CLAUDE.md into focused topic files:

- [ ] ⬜ **Task 1.1**: Create `CLAUDE/ContainerRules.md` — CCY container detection rules + ctrl+z patch details
  - [ ] ⬜ Split CCY section: critical rules (container detection, edit-only workflow) stay in root; reference detail (ctrl+z patch internals, version bump examples) goes to topic file
- [ ] ⬜ **Task 1.2**: Create `CLAUDE/InfrastructureAsCode.md` — merged Ansible-Only + Testing Workflow + Idempotency
  - [ ] ⬜ Deduplicate the two overlapping sections into one coherent document
  - [ ] ⬜ Include: edit → playbook → deploy → test workflow, prohibited manual actions, idempotency patterns
- [ ] ⬜ **Task 1.3**: Create `CLAUDE/AnsibleStyle.md` — Ansible style rules + configuration patterns
  - [ ] ⬜ Merge "Ansible Style Rules" section with "Ansible Configuration Patterns" section
  - [ ] ⬜ Include: blockinfile/marker patterns, package management, service management, privilege escalation, variable naming, task organisation, error handling patterns
- [ ] ⬜ **Task 1.4**: Create `CLAUDE/SecurityRules.md` — deduplicated security guidance
  - [ ] ⬜ Merge "Public Repository Warning", "Security First" principle, and "Vault Management" into one file
  - [ ] ⬜ Include: what never to commit, how to use Vault, pre-commit hook enforcement, git diff checks
- [ ] ⬜ **Task 1.5**: Create `CLAUDE/QA.md` — QA workflow and script reference
  - [ ] ⬜ Move QA script details, what each script checks, known limitations
  - [ ] ⬜ Include: qa-all.bash as single entry point, ESLint for extensions, qa-ctrl-z-patch.bash for CCY
- [ ] ⬜ **Task 1.6**: Create `CLAUDE/DebugCommands.md` — non-interactive command rules
  - [ ] ⬜ Move "Providing Debug Commands to Users" section wholesale

### Phase 2: Move user-facing docs to docs/

- [ ] ⬜ **Task 2.1**: Audit existing `docs/` files for overlap with CLAUDE.md user-facing sections
  - [ ] ⬜ Compare: CLAUDE.md "Project Overview/Architecture" vs `docs/architecture.md`
  - [ ] ⬜ Compare: CLAUDE.md "Technology Stack" vs `docs/development.md`
  - [ ] ⬜ Compare: CLAUDE.md "Troubleshooting" vs existing docs
  - [ ] ⬜ Compare: CLAUDE.md "Project Maintenance" vs existing docs
- [ ] ⬜ **Task 2.2**: Merge unique user-facing content into existing docs/ files
  - [ ] ⬜ Merge architecture/overview content (avoid duplication with existing `docs/architecture.md`)
  - [ ] ⬜ Merge tech stack content (avoid duplication with existing `docs/development.md`)
  - [ ] ⬜ Merge troubleshooting content
  - [ ] ⬜ Merge maintenance content
- [ ] ⬜ **Task 2.3**: Create `docs/github-multi-account.md` for GitHub multi-account management
  - [ ] ⬜ Move entire "GitHub Multi-Account Management" section (~2,500 chars of user docs)
- [ ] ⬜ **Task 2.4**: Update `docs/README.md` index with any new/moved content

### Phase 3: Create/update subdirectory CLAUDE.md stubs

Stub files use `@` pointers to CLAUDE/ topic files — no duplicated content.

- [ ] ⬜ **Task 3.1**: Create `scripts/CLAUDE.md` stub
  - [ ] ⬜ Brief intro (2-3 lines) about the scripts directory
  - [ ] ⬜ `@../CLAUDE/QA.md` forced read for QA workflow
- [ ] ⬜ **Task 3.2**: Create `files/CLAUDE.md` stub
  - [ ] ⬜ Brief intro about filesystem mirror structure
  - [ ] ⬜ `@../CLAUDE/InfrastructureAsCode.md` forced read for deployment workflow
  - [ ] ⬜ `@../CLAUDE/SecurityRules.md` forced read for sensitive data handling
- [ ] ⬜ **Task 3.3**: Create `environment/CLAUDE.md` stub
  - [ ] ⬜ Brief intro about inventory and host vars
  - [ ] ⬜ `@../CLAUDE/SecurityRules.md` forced read for vault encryption workflow
- [ ] ⬜ **Task 3.4**: Update `playbooks/CLAUDE.md` to add `@` pointers
  - [ ] ⬜ Add `@../CLAUDE/AnsibleStyle.md` for style rules
  - [ ] ⬜ Add `@../CLAUDE/InfrastructureAsCode.md` for deployment workflow
  - [ ] ⬜ Preserve existing executable playbook requirements (unique content stays)
- [ ] ⬜ **Task 3.5**: Review `extensions/CLAUDE.md` for `@` pointer opportunities
  - [ ] ⬜ Check if any content duplicates CLAUDE/ topic files
  - [ ] ⬜ Add `@` pointers where appropriate (likely `@../CLAUDE/QA.md` for ESLint reference)

### Phase 4: Rewrite root CLAUDE.md as front page

- [ ] ⬜ **Task 4.1**: Draft lean root `CLAUDE.md` (~10-15k chars target)
  - [ ] ⬜ Keep inline: CCY container detection (critical 3-line rule + `@` pointer to details)
  - [ ] ⬜ Keep inline: Fail-fast hard rule (core principle, 1 paragraph + 1 example)
  - [ ] ⬜ Keep inline: Public repo warning (2-3 line summary + `@CLAUDE/SecurityRules.md`)
  - [ ] ⬜ Keep inline: Ansible-only deployment (2-3 line summary + `@CLAUDE/InfrastructureAsCode.md`)
  - [ ] ⬜ Keep inline: QA mandatory (2-3 line summary + `@CLAUDE/QA.md`)
  - [ ] ⬜ Keep inline: Plan commit rule
  - [ ] ⬜ Keep inline: CCY version bump requirement (2-line rule)
  - [ ] ⬜ Keep inline: Debug commands rule (2-line summary + `@CLAUDE/DebugCommands.md`)
  - [ ] ⬜ Keep inline: YAGNI, DRY (compact — 1-2 lines each)
  - [ ] ⬜ Add `@` pointers section listing all CLAUDE/ topic files with one-line descriptions
  - [ ] ⬜ Remove ALL user-facing documentation (now in docs/)
  - [ ] ⬜ Remove ALL detailed examples that moved to topic files
- [ ] ⬜ **Task 4.2**: Verify no content was lost
  - [ ] ⬜ Diff old CLAUDE.md against new structure — every line accounted for
  - [ ] ⬜ Check each `@` pointer resolves correctly
  - [ ] ⬜ Verify subdirectory stubs load their `@` imports

### Phase 5: Validation and cleanup

- [ ] ⬜ **Task 5.1**: Run `./scripts/qa-all.bash` (in case any bash/python files touched)
- [ ] ⬜ **Task 5.2**: Verify character count of new root CLAUDE.md is under 15k
- [ ] ⬜ **Task 5.3**: Verify zero duplication — grep for key phrases across all CLAUDE.md and CLAUDE/ files
- [ ] ⬜ **Task 5.4**: Test subdirectory `@` imports work (read each stub, confirm pointers are valid paths)
- [ ] ⬜ **Task 5.5**: Update `CLAUDE/Plan/README.md` with this plan entry
- [ ] ⬜ **Task 5.6**: Commit all changes together (plan + restructured docs)

## Target File Structure

```
CLAUDE.md                              # Front page (~10-15k chars) with @ pointers
CLAUDE/
├── AnsibleStyle.md                    # Ansible style rules + config patterns
├── ContainerRules.md                  # CCY container details + ctrl+z patch
├── DebugCommands.md                   # Non-interactive command rules for users
├── GnomeShell.md                      # (existing) GNOME Shell integration
├── InfrastructureAsCode.md            # Merged ansible-only + testing workflow
├── PlanWorkflow.md                    # (existing) Planning workflow
├── QA.md                              # QA scripts, what to run when
├── SecurityRules.md                   # Merged public repo + vault + security
└── Plan/                              # (existing) Plans directory
docs/
├── (existing 14 files)
├── github-multi-account.md            # (new) Moved from CLAUDE.md
└── README.md                          # (updated) Index
playbooks/CLAUDE.md                    # (updated) + @ pointers to AnsibleStyle, IaC
extensions/CLAUDE.md                   # (reviewed) + @ pointer to QA
scripts/CLAUDE.md                      # (new) stub + @ pointer to QA
files/CLAUDE.md                        # (new) stub + @ pointers to IaC, Security
environment/CLAUDE.md                  # (new) stub + @ pointer to Security
```

## Content Flow Diagram

```
Root CLAUDE.md (front page)
├── [inline] CCY container: edit-only rule (3 lines)
│   └── @CLAUDE/ContainerRules.md (full details, ctrl+z patch)
├── [inline] Fail-fast hard rule (compact)
├── [inline] Public repo: never commit secrets (summary)
│   └── @CLAUDE/SecurityRules.md (full rules, vault, git hooks)
├── [inline] Ansible-only: no manual ops (summary)
│   └── @CLAUDE/InfrastructureAsCode.md (full workflow)
├── [inline] QA mandatory (summary)
│   └── @CLAUDE/QA.md (full script reference)
├── [inline] Plan commit rule
├── [inline] CCY version bump rule
├── [inline] Debug commands: non-interactive (summary)
│   └── @CLAUDE/DebugCommands.md (full examples)
└── [inline] YAGNI, DRY (1-2 lines each)

Subdirectory stubs (load on-demand):
├── scripts/CLAUDE.md → @CLAUDE/QA.md
├── files/CLAUDE.md → @CLAUDE/InfrastructureAsCode.md, @CLAUDE/SecurityRules.md
├── environment/CLAUDE.md → @CLAUDE/SecurityRules.md
├── playbooks/CLAUDE.md → @CLAUDE/AnsibleStyle.md, @CLAUDE/InfrastructureAsCode.md
└── extensions/CLAUDE.md → @CLAUDE/QA.md (ESLint section)
```

## Success Criteria

- [ ] Root `CLAUDE.md` is under 15,000 characters
- [ ] Zero content duplication across the entire documentation tree
- [ ] Every line from the old CLAUDE.md is accounted for (moved, merged, or deliberately removed with justification)
- [ ] All `@` pointers in all files resolve to existing files
- [ ] All subdirectory CLAUDE.md stubs contain only brief intros + `@` pointers (no substantive content)
- [ ] `docs/` contains all user-facing documentation previously in CLAUDE.md
- [ ] `CLAUDE/` contains all LLM-reference documentation previously in CLAUDE.md
- [ ] QA passes (`./scripts/qa-all.bash`)
- [ ] No existing subdirectory CLAUDE.md files (playbooks/, extensions/) lost content

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| @ import depth exceeds 5 hops | Medium | Low | Architecture is max 2 hops (root → CLAUDE/ topic file) |
| Context bloat from too many @ imports | Medium | Medium | Subdirectory stubs load on-demand only; root keeps inline content minimal |
| Lost content during migration | High | Medium | Phase 4 Task 4.2 explicitly diffs old vs new |
| Existing subdirectory CLAUDE.md files break | Medium | Low | Phase 3 preserves existing content, only adds @ pointers |

## Notes & Updates

### 2026-04-02
- Plan created based on research from three parallel agents
- Current CLAUDE.md: 40,466 bytes, 1,014 lines
- Target: root under 15k chars with modular CLAUDE/ topic files
- docs/ already exists with 14 files — merge user-facing content there
