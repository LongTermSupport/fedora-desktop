# Claude Code Planning Workflow

**Version 2.0** | Effective: January 2026

This document defines the standard planning workflow for all work on the Claude Code Hooks Daemon project. All developers and AI agents must follow this workflow to ensure efficient, trackable, and high-quality work.

---

## Core Principles

1. **Plan Before Execute** - Never start implementation without a documented plan
2. **Break Down Complexity** - Decompose large work into manageable tasks
3. **Track Everything** - Every task has a status and owner
4. **Document Decisions** - Capture rationale for major decisions
5. **Iterate Rapidly** - Plans are living documents, update as you learn
6. **Test First (TDD)** - Write failing tests before implementation
7. **Debug First** - Introspect hook events before writing handlers

---

## Plan Structure

### Directory Layout

```
CLAUDE/
└── Plan/
    ├── 001-handler-implementation/
    │   ├── PLAN.md                      # Main plan document
    │   ├── {supporting-docs*}.md        # Supporting analysis docs
    │   └── assets/                      # Diagrams, logs, etc.
    ├── 002-config-refactoring/
    │   ├── PLAN.md
    │   └── config-analysis.md
    ├── 003-qa-improvements/
    │   ├── PLAN.md
    │   └── coverage-report.md
    └── README.md                        # Index of all plans
```

### Plan Numbering

- Plans are numbered sequentially: `001-`, `002-`, `003-`, etc.
- Use kebab-case for plan folder names
- Plan numbers never change even if plan is cancelled

---

## Plan Document Structure

Every `PLAN.md` must follow this structure:

```markdown
# Plan XXX: [Plan Title]

**Status**: In Progress | Complete | Blocked | Cancelled
**Created**: YYYY-MM-DD
**Owner**: [Name/Agent]
**Priority**: High | Medium | Low
**Estimated Effort**: [Hours/Days]

## Overview

[2-3 paragraphs describing what this plan aims to achieve and why]

## Goals

- Clear, measurable goal 1
- Clear, measurable goal 2
- Clear, measurable goal 3

## Non-Goals

- Explicitly what this plan will NOT do
- Helps scope creep management

## Context & Background

[Summary relevant background, previous decisions, or context needed]
Refer to detailed info in supporting docs as required

## Tasks

### Phase 1: [Phase Name]

- [ ] **Task 1.1**: Description of task
  - [ ] Subtask 1.1.1: More specific work
  - [ ] Subtask 1.1.2: More specific work
- [ ] **Task 1.2**: Description of task

### Phase 2: [Phase Name]

- [ ] **Task 2.1**: Description of task
- [ ] **Task 2.2**: Description of task

## Dependencies

- Depends on: Plan 001 (Complete)
- Blocks: Plan 003 (Not Started)
- Related: Plan 002

## Technical Decisions

### Decision 1: [Title]
**Context**: Why this decision is needed
**Options Considered**:
1. Option A - pros/cons
2. Option B - pros/cons

**Decision**: We chose Option A because [rationale]
**Date**: YYYY-MM-DD

## Success Criteria

- [ ] Criterion 1 that must be met
- [ ] Criterion 2 that must be met
- [ ] All QA checks passing

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Risk description | High/Med/Low | High/Med/Low | How we'll handle it |

## Timeline

- Phase 1: [Date range]
- Phase 2: [Date range]
- Target Completion: YYYY-MM-DD

## Notes & Updates

### YYYY-MM-DD
- Update or note about progress/changes

### YYYY-MM-DD
- Another update
```

---

## Task Status System

### Status Icons

Use these Unicode icons for task status:

| Status | Icon | Markdown | Meaning |
|--------|------|----------|---------|
| **Not Started** | ⬜ | `⬜` | Task not yet begun |
| **In Progress** | 🔄 | `🔄` | Currently being worked on |
| **Completed** | ✅ | `✅` | Task finished and verified |
| **Blocked** | 🚫 | `🚫` | Cannot proceed (dependency/issue) |
| **Cancelled** | ❌ | `❌` | Task no longer needed |
| **On Hold** | ⏸️ | `⏸️` | Paused temporarily |
| **Needs Review** | 👁️ | `👁️` | Work done, awaiting review |

### Task Formatting

```markdown
- [ ] ⬜ **Task Title**: Clear description of what needs to be done
  - [ ] ⬜ Subtask 1: Specific action
  - [ ] 🔄 Subtask 2: Another action (currently working)
  - [ ] ✅ Subtask 3: Already completed
```

### Rules for Status Updates

1. **One Task In Progress** - Limit to 1-2 tasks marked 🔄 at a time
2. **Update Immediately** - Change status as soon as state changes
3. **Document Blocks** - If marking 🚫, add note explaining why
4. **Verify Completion** - Only mark ✅ after testing/verification and QA passing

---

## QA Integration (Python Project)

**CRITICAL: Before completing ANY task, all QA checks must pass.**

### Required QA Verification

Run the complete QA suite before marking any task complete:

```bash
# Run ALL QA checks (REQUIRED before commits)
./scripts/qa/run_all.sh
```

### What Gets Checked

| Check | Tool | Requirement |
|-------|------|-------------|
| Code Formatting | Black | Auto-formats (line length 100) |
| Linting | Ruff | Auto-fixes violations |
| Type Checking | MyPy | Strict mode, all functions typed |
| Tests | Pytest | 95% minimum coverage |
| Security | Bandit | No HIGH severity issues |

### QA Task Format

Always include QA verification as a subtask:

```markdown
- [ ] ⬜ **Implement feature X**
  - [ ] ⬜ Write implementation
  - [ ] ⬜ Run QA: `./scripts/qa/run_all.sh`
  - [ ] ⬜ Fix any issues
  - [ ] ⬜ Verify all checks pass
```

### Individual QA Commands

```bash
# Individual checks (auto-fix enabled by default)
./scripts/qa/run_lint.sh          # Ruff linter
./scripts/qa/run_format_check.sh  # Black formatter
./scripts/qa/run_type_check.sh    # MyPy type checker
./scripts/qa/run_tests.sh         # Pytest with coverage

# Manual auto-fix
./scripts/qa/run_autofix.sh       # Runs Black + Ruff --fix
```

---

## TDD Integration

This project enforces Test-Driven Development. All implementation work must follow the Red-Green-Refactor cycle.

### TDD Workflow

1. **Red**: Write a failing test that defines the expected behavior
2. **Green**: Write the minimum code to make the test pass
3. **Refactor**: Clean up the code while keeping tests green
4. **Verify**: Run full QA suite

### TDD Task Format

```markdown
- [ ] ⬜ **Implement handler for X**
  - [ ] ⬜ Write failing test for matches() behavior
  - [ ] ⬜ Implement matches() to pass test
  - [ ] ⬜ Write failing test for handle() behavior
  - [ ] ⬜ Implement handle() to pass test
  - [ ] ⬜ Refactor for clarity
  - [ ] ⬜ Verify 95%+ coverage maintained
  - [ ] ⬜ Run full QA suite
```

### Coverage Requirement

- Minimum 95% test coverage is required
- New code must have corresponding tests
- Coverage reports in `untracked/qa/coverage.json`

### Running Tests

```bash
# Run all tests with coverage
./scripts/qa/run_tests.sh

# Run specific test file
pytest tests/handlers/pre_tool_use/test_my_handler.py -v

# Run with coverage report
pytest --cov=src --cov-report=html
```

---

## Planning Workflow Steps

### Step 1: Identify Work

When new work is identified:

1. Check if it fits in an existing plan
2. If not, determine if a new plan is needed
3. For small tasks (< 1 hour), use TodoWrite instead

**New Plan Threshold**: If work will take > 2 hours or involves multiple phases

### Step 2: Create Plan

1. Create new folder: `CLAUDE/Plan/XXX-descriptive-name/`
2. Copy plan template to `PLAN.md`
3. Fill in overview, goals, and initial task breakdown
4. Update `CLAUDE/Plan/README.md` with plan entry

### Step 3: Break Down Tasks

1. Decompose work into phases (if needed)
2. Break each phase into concrete tasks
3. Break tasks into subtasks if task > 30 minutes
4. Ensure tasks are actionable and testable
5. **Include TDD subtasks for implementation work**
6. **Include QA verification subtasks**

**Good Task**: "Create PreToolUse handler to block destructive sed commands with 95% coverage"
**Bad Task**: "Work on handlers"

### Step 4: Review & Approve

1. Review plan completeness
2. Verify tasks are well-defined
3. Check for missing dependencies
4. Get stakeholder approval (if needed)

### Step 5: Execute

1. Mark plan status as 🔄 In Progress
2. Work through tasks sequentially
3. **Follow TDD cycle for implementation**
4. Update task status in real-time
5. Document any blockers or changes
6. **Run QA before each commit**
7. Commit work with reference to plan: `Plan 001: Implement destructive git handler`

### Step 6: Complete

1. **Verify all QA checks pass**
2. Verify all success criteria met
3. Mark all tasks as ✅
4. Mark plan status as Complete
5. Add completion date to plan
6. Document any lessons learned

---

## Using TodoWrite vs Plans

### Use TodoWrite For:
- Very small tasks and LOW RISK tasks
- Single-session work
- No major architectural decisions
- Temporary tracking during active work

### Use Plans For:
- Medium sized+ work
- Any risk
- Work with multiple phases
- Architectural or design decisions
- Work that may need to be resumed later
- Work that others need to understand

### Converting TodoWrite to Plan

If TodoWrite list grows beyond 5 items or becomes multi-session:
1. Create proper plan
2. Migrate tasks to plan
3. Clear TodoWrite
4. Reference plan in work

---

## Plan Templates

### Handler Implementation Plan Template

Use this template when creating new handlers for hook events.

```markdown
# Plan XXX: [Handler Name] Handler

**Status**: Not Started
**Type**: Handler Implementation
**Event Type**: PreToolUse | PostToolUse | SessionStart | etc.
**Priority Range**: [10-20 for safety, 25-35 for quality, 36-55 for workflow]
**Estimated Effort**: [X hours]

## Overview

[What handler, why needed, what behavior it enforces]

## Goals

- Intercept [specific event/pattern]
- Enforce [specific behavior]
- Maintain 95%+ test coverage

## Non-Goals

- [What this handler does NOT do]

## Debug Analysis

**Before implementation**, capture event flow:
```bash
./scripts/debug_hooks.sh start "Testing [scenario]"
# ... perform actions in Claude Code ...
./scripts/debug_hooks.sh stop
```

**Event Analysis**:
- Event Type: [PreToolUse, etc.]
- Tool Name: [Write, Bash, etc.]
- Key hook_input fields: [list relevant fields]
- Trigger Pattern: [what triggers this handler]

## Tasks

### Phase 1: Debug & Design
- [ ] ⬜ Run debug script for target scenario
- [ ] ⬜ Analyze captured events and data
- [ ] ⬜ Document event type and patterns
- [ ] ⬜ Design handler matching logic
- [ ] ⬜ Determine priority and terminal behavior

### Phase 2: TDD Implementation
- [ ] ⬜ Create test file: `tests/handlers/{event_type}/test_{handler_name}.py`
- [ ] ⬜ Write failing test for matches() - positive case
- [ ] ⬜ Write failing test for matches() - negative cases
- [ ] ⬜ Implement matches() to pass tests
- [ ] ⬜ Write failing test for handle() - expected result
- [ ] ⬜ Write failing test for handle() - edge cases
- [ ] ⬜ Implement handle() to pass tests
- [ ] ⬜ Refactor and clean up

### Phase 3: Integration
- [ ] ⬜ Register handler in config
- [ ] ⬜ Update handler count in CLAUDE.md
- [ ] ⬜ Run full QA suite: `./scripts/qa/run_all.sh`
- [ ] ⬜ Test with live Claude Code session
- [ ] ⬜ Update documentation

## Handler Specification

```python
class [HandlerName]Handler(Handler):
    def __init__(self) -> None:
        super().__init__(
            name="[handler-name]",
            priority=[XX],
            terminal=[True/False]
        )

    def matches(self, hook_input: dict) -> bool:
        # Pattern matching logic
        pass

    def handle(self, hook_input: dict) -> HookResult:
        # Handler behavior
        pass
```

## Success Criteria

- [ ] Handler correctly intercepts target events
- [ ] All tests passing
- [ ] 95%+ coverage maintained
- [ ] Live testing successful in Claude Code
- [ ] Documentation updated
- [ ] All QA checks pass
```

### Feature Implementation Plan Template

```markdown
# Plan XXX: [Feature Name]

**Status**: Not Started
**Type**: Feature Implementation
**Estimated Effort**: [X hours/days]

## Overview

[What feature, why needed]

## Tasks

### Phase 1: Design
- [ ] ⬜ Analyze requirements
- [ ] ⬜ Design solution architecture
- [ ] ⬜ Document technical decisions

### Phase 2: TDD Implementation
- [ ] ⬜ Write failing tests for core functionality
- [ ] ⬜ Implement core functionality
- [ ] ⬜ Write tests for edge cases
- [ ] ⬜ Implement edge case handling
- [ ] ⬜ Refactor for clarity

### Phase 3: Integration & QA
- [ ] ⬜ Integrate with existing code
- [ ] ⬜ Run full QA: `./scripts/qa/run_all.sh`
- [ ] ⬜ Fix any QA issues
- [ ] ⬜ Update documentation

## Success Criteria

- [ ] Feature works as specified
- [ ] All tests passing with 95%+ coverage
- [ ] All QA checks pass
- [ ] Documentation updated
```

### Bug Fix Plan Template

```markdown
# Plan XXX: Fix [Bug Description]

**Status**: Not Started
**Type**: Bug Fix
**Severity**: Critical | High | Medium | Low

## Bug Description

[What's broken, how to reproduce]

## Tasks

- [ ] ⬜ Reproduce bug locally
- [ ] ⬜ **Write failing test that demonstrates the bug**
- [ ] ⬜ Identify root cause
- [ ] ⬜ Implement fix (make test pass)
- [ ] ⬜ Add additional regression tests
- [ ] ⬜ Run full QA: `./scripts/qa/run_all.sh`
- [ ] ⬜ Verify fix works in live testing

## Success Criteria

- [ ] Bug no longer reproducible
- [ ] Failing test now passes
- [ ] No regression in other tests
- [ ] All QA checks pass
```

### Refactoring Plan Template

Use this template when improving existing code without changing behavior.

```markdown
# Plan XXX: Refactor [Component/Area]

**Status**: Not Started
**Type**: Refactoring
**Estimated Effort**: [X hours]

## Overview

[What needs refactoring, why it improves the codebase]

## Goals

- Improve [readability/maintainability/performance]
- Maintain existing behavior
- Maintain or improve test coverage

## Non-Goals

- No new features
- No behavior changes

## Tasks

### Phase 1: Preparation
- [ ] ⬜ Identify all affected code
- [ ] ⬜ Ensure adequate test coverage exists
- [ ] ⬜ Document current behavior

### Phase 2: Refactoring
- [ ] ⬜ Apply refactoring incrementally
- [ ] ⬜ Run tests after each change
- [ ] ⬜ Verify no behavior changes

### Phase 3: Verification
- [ ] ⬜ Run full QA: `./scripts/qa/run_all.sh`
- [ ] ⬜ Compare before/after behavior
- [ ] ⬜ Update documentation if needed

## Success Criteria

- [ ] All existing tests pass
- [ ] Coverage maintained at 95%+
- [ ] No behavior changes
- [ ] All QA checks pass
- [ ] Code is cleaner/more maintainable
```

---

## Best Practices

### Task Writing

✅ **Good Tasks**:
- "Create PreToolUse handler to block force push to main/master"
- "Add terminal flag to npm audit handler with priority 45"
- "Fix MyPy type error in FrontController.dispatch method"
- "Increase test coverage for daemon/server.py to 95%"

❌ **Bad Tasks**:
- "Fix the daemon"
- "Make it work better"
- "Work on handlers"

### Task Granularity

- **Task**: 15-60 minutes of focused work
- **Subtask**: 5-15 minutes of specific action
- **Phase**: Group of related tasks (hours/days)

### Status Update Discipline

1. **Before starting work**: Review plan, mark task 🔄
2. **During work**: Update status if blocked
3. **After completing**: Mark ✅, run QA, commit with reference
4. **Daily**: Review plan, update progress notes

### Handling Changes

When requirements change mid-plan:

1. **Document Change**: Add note to plan with date
2. **Update Tasks**: Revise task list as needed
3. **Assess Impact**: Update estimates, dependencies
4. **Communicate**: Ensure stakeholders aware

---

## Plan Reviews

### Daily Review (If actively working on plan)

- Are tasks up to date?
- Any blockers need attention?
- Is plan still on track?
- Are QA checks still passing?

### Weekly Review (For active plans)

- Progress vs timeline?
- Any scope changes needed?
- Dependencies still valid?
- Test coverage maintained?

### Completion Review

- All success criteria met?
- All QA checks pass?
- Lessons learned documented?
- Follow-up work identified?

---

## Plan Metrics

Track these for each plan:

- **Planned vs Actual Effort**: Improve estimation
- **Blocker Count**: Identify process issues
- **Scope Changes**: Track requirements stability
- **Completion Rate**: % of tasks completed
- **QA Pass Rate**: How often QA passes on first run

---

## Integration with Git

### When to Commit Plans

**The hard rule**: never let plan state lag behind code state. Both of these patterns are valid and encouraged:

**Pattern A — Plan + code in one commit** (preferred when both change in one session):

```bash
git add CLAUDE/Plan/001-handler/PLAN.md src/handlers/destructive_git.py
git commit -m "Plan 001: Implement destructive git handler"
```

**Pattern B — Plan-only commit** (encouraged for plan creation, research updates, decision-gate notes, or status changes that are not tied to a code change):

```bash
git add CLAUDE/Plan/001-handler/
git commit -m "Plan 001: Add research notes for handler design"
```

**What is prohibited**: committing code that completes plan tasks while leaving the plan file unchanged on disk. This creates drift between what the plan says and what the codebase actually contains, which is the bug the rule exists to prevent.

The canonical version of this rule lives in [CLAUDE.md → Plan Commit Rule](../CLAUDE.md#plan-commit-rule).

### Commit Messages

Reference plans in commits:

```
Plan 001: Implement destructive git handler

- Add DestructiveGitHandler to block force push and reset --hard
- Include tests for all blocked patterns
- Register handler with priority 10

Refs: CLAUDE/Plan/001-handler-implementation
```

### Branch Naming

For larger plans, use feature branches:

```
plan/001-destructive-git-handler
plan/002-config-refactoring
plan/003-tdd-enforcement
```

### Pre-Commit Verification

Before committing, always verify:

```bash
# Run full QA suite
./scripts/qa/run_all.sh

# Check git status
git status

# Stage specific files (avoid staging secrets)
git add src/handlers/pre_tool_use/my_handler.py
git add tests/handlers/pre_tool_use/test_my_handler.py

# Commit with plan reference
git commit -m "Plan 001: Implement destructive git handler"
```

---

## Plan Index

Maintain `CLAUDE/Plan/README.md`:

```markdown
# Plans Index

## Active Plans
- [001: Destructive Git Handler](001-handler-implementation/PLAN.md) - In Progress

## Completed Plans
- None yet

## Blocked Plans
- None

## Cancelled Plans
- None
```

---

## AI Agent Guidelines

When Claude Code (or other AI agents) work on this project:

1. **Always check for existing plans** before starting work
2. **Create plan if none exists** for work > 2 hours
3. **Debug hook events first** - Before writing handlers:
   - Use `scripts/debug_hooks.sh` to capture event flow
   - Analyze logs to understand what events fire
   - See CLAUDE/DEBUGGING_HOOKS.md for complete guide
4. **Follow TDD workflow** - Write failing tests before implementation
5. **Update task status in real-time** as you work
6. **Run QA before commits** - `./scripts/qa/run_all.sh` must pass
7. **Document blockers immediately** if you get stuck
8. **Ask user for approval** before marking plan complete
9. **Reference plans in all commits** for traceability

### Agent Workflow Example

```
User: "Implement a handler to block destructive sed commands"

Agent:
1. Checks CLAUDE/Plan/ for existing plan
2. If none, creates Plan 001
3. Runs debug script to capture sed usage events
4. Analyzes events to determine handler design
5. Breaks down into TDD tasks
6. Shows plan to user for approval
7. Begins execution:
   - Write failing test
   - Implement handler
   - Run QA suite
8. Commits with "Plan 001: Implement sed blocker handler"
9. Updates plan progress notes
10. Marks complete when all QA passes
```

### Handler Development Workflow

**CRITICAL**: Always debug first, develop second:

1. Identify scenario ("enforce TDD", "block destructive git", etc.)
2. **Use `scripts/debug_hooks.sh` to capture event flow**
3. Analyze logs to determine which event type and what data is available
4. Write tests first (TDD)
5. Implement handler
6. Run QA suite
7. Debug again to verify handler intercepts correctly
8. Test in live Claude Code session

---

## Summary

**Remember**:
- Plan before you code
- Debug hook events before writing handlers
- Write tests first (TDD)
- Run QA before commits
- Update status religiously
- Keep tasks concrete and testable
- Document decisions and changes
- Plans are living documents

**Questions?** See examples in `CLAUDE/Plan/` or ask in conversation.

---

**Maintained by**: Claude Code Hooks Daemon Contributors
**Last Updated**: January 2026
