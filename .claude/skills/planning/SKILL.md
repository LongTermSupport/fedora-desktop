---
name: planning
description: Use this skill when the user wants to create, review, or work on planning documents in CLAUDE/Plan/. Ensures planning workflow adherence by reading CLAUDE/PlanWorkflow.md and can optionally invoke opus planning agent for research and proposal of comprehensive plan documents. Use when planning multi-phase work, creating new plans, or reviewing/updating existing plans.
allowed-tools: [Read, Write, Edit, Glob, Grep, Task]
---

# Planning Skill

**Purpose**: Orchestrates the planning workflow for Claude Code projects

**Key Documentation**:
- `CLAUDE/PlanWorkflow.md` - Planning workflow process (MUST READ)

**Workflow State Management**: REQUIRED READING: @CLAUDE/Workflow.md for workflow state management

---

## WHEN TO USE

Use this skill when you need to:

- **Create new plan documents** - "Create a plan for implementing feature X"
- **Review existing plans** - "Review Plan 038 and update status"
- **Research and propose plans** - "Research what's needed for refactoring and create a plan"
- **Update plan progress** - "Update Plan 045 to mark Phase 2 complete"
- **Convert TodoWrite to plan** - "This is getting complex, let's create a proper plan"
- **Validate plan structure** - "Check if Plan 012 follows the correct format"
- **Plan multi-phase work** - "Plan out the full implementation workflow"

### TRIGGER PHRASES

**IMPORTANT**: Automatically invoke this skill when the user says:

- "create a plan for"
- "create plan document"
- "make a plan"
- "we need a plan"
- "plan this out"
- "planning document"
- "update the plan"
- "review plan"
- "check the plan"
- "what's in the plan"
- "convert this to a plan"

If the user mentions "plan" in the context of creating, reviewing, or updating planning documents, this skill should be your **first choice**.

## WHEN NOT TO USE

Do NOT use this skill for:

- **Simple tasks** - Use TodoWrite for work < 2 hours or very low risk
- **Execution of plans** - Use appropriate skills/agents to implement plans
- **General questions about planning** - Answer directly
- **Code planning** - Use EnterPlanMode for implementation planning

## CRITICAL: ALWAYS READ DOCUMENTATION FIRST

**Before ANY planning work, you MUST read:**

```
CLAUDE/PlanWorkflow.md
```

This file contains:
- Plan document structure (required sections, format)
- Task status system (icons, meanings)
- Workflow steps (identify → create → break down → review → execute → complete)
- TodoWrite vs Plan decision criteria
- Best practices and examples

**Why this is critical:**
- Ensures consistent plan format across all plans
- Provides task breakdown guidance
- Defines status tracking system
- Shows required sections and optional sections
- Documents plan lifecycle

---

## PLANNING SKILL WORKFLOWS

### Workflow A: Create New Plan

For creating a new plan document from scratch:

**Step 1: Find Next Plan Number**
```bash
# Find existing plans to determine next number
glob: "CLAUDE/Plan/**/PLAN.md"
# Look at existing numbers, use next sequential
```

**Step 2: Understand Requirements**
1. Read `CLAUDE/PlanWorkflow.md` to understand plan structure
2. Clarify plan scope with user (if unclear)
3. Determine if simple or complex (complex → consider opus agent)

**Step 3: Create Plan Directory**
```
CLAUDE/Plan/XXX-descriptive-name/
├── PLAN.md (required - main plan document)
└── [supporting docs as needed]
```

**Step 4: Populate PLAN.md**

Use the template from PlanWorkflow.md:
- **Status**: 🟡 In Progress | 🟢 Complete | 🔴 Blocked | ⚫ Cancelled
- **Overview**: 2-3 paragraphs explaining what and why
- **Goals**: Clear, measurable objectives
- **Non-Goals**: Explicit scope boundaries
- **Tasks**: Broken down by phase with status icons
- **Dependencies**: What this plan depends on/blocks
- **Technical Decisions**: Key choices made (optional)
- **Success Criteria**: How to measure completion
- **Risks & Mitigations**: What could go wrong (optional)
- **Timeline**: Phase ordering only (NO time estimates)
- **Notes & Updates**: Running log of progress

**Step 5: Break Down Tasks**

Follow PlanWorkflow.md guidance:
- **Good task**: "Create component with specific functionality"
- **Bad task**: "Work on feature"
- **Task granularity**: 15-60 minutes of focused work
- **Subtask granularity**: 5-15 minutes of specific action
- **Phase**: Group of related tasks (hours/days)

**Step 6: Review and Approve**
1. Check plan completeness (all required sections)
2. Verify tasks are well-defined and actionable
3. Show plan to user for approval
4. Make revisions if needed

**Optional: Invoke Opus Planning Agent**

For complex plans requiring deep research and analysis:
```
Use Task tool with subagent_type: Plan
Model: opus

Prompt:
"Research and propose a comprehensive plan for [TASK].

Read CLAUDE/PlanWorkflow.md for plan structure requirements.
Analyse existing codebase to understand:
- Current architecture and patterns
- Similar work done previously
- Dependencies and constraints
- Technical decisions needed

Output: Complete PLAN.md document ready for review.
This is research and proposal - user will review before approval."
```

---

### Workflow B: Update Existing Plan

For updating progress on an existing plan:

**Step 1: Read Current State**
```bash
# Find the plan
glob: "CLAUDE/Plan/**/PLAN.md"

# Read the plan
read: CLAUDE/Plan/XXX-name/PLAN.md
```

**Step 2: Update Status**

Update task status icons:
- ⬜ Not Started → 🔄 In Progress (when starting task)
- 🔄 In Progress → ✅ Completed (when finished)
- 🔄 In Progress → 🚫 Blocked (if cannot proceed)
- ⬜ Not Started → ❌ Cancelled (if no longer needed)

**Step 3: Add Progress Notes**

Add dated entry to "Notes & Updates" section:
```markdown
### 2025-12-07
- Completed Phase 1: Implementation
- Discovered issue with component (needs update)
- Moving to Phase 2 next
```

**Step 4: Update Plan Status**

If all tasks complete:
- Mark plan status as 🟢 Complete
- Add completion date
- Document lessons learned

**Step 5: Save Changes**
```
Edit the PLAN.md file with updated status
```

---

### Workflow C: Review Plan

For reviewing plan quality and completeness:

**Step 1: Read Plan and Workflow Docs**
```
Read: CLAUDE/Plan/XXX-name/PLAN.md
Read: CLAUDE/PlanWorkflow.md
```

**Step 2: Validate Structure**

Check required sections present:
- [ ] Status and metadata (Created, Owner, Priority)
- [ ] Overview (what and why)
- [ ] Goals (measurable)
- [ ] Non-Goals (explicit boundaries)
- [ ] Tasks (broken down, with status)
- [ ] Success Criteria (how to measure done)

**Step 3: Validate Task Quality**

Check tasks are:
- [ ] Actionable (clear what needs to be done)
- [ ] Specific (not vague like "work on X")
- [ ] Testable (can verify completion)
- [ ] Right granularity (15-60 minutes)

**Step 4: Check Status Consistency**

Verify:
- [ ] Only 1-2 tasks marked 🔄 In Progress
- [ ] Blocked tasks have explanation
- [ ] Completed tasks verified
- [ ] Plan status matches task progress

**Step 5: Report Findings**

Provide review summary:
```markdown
# Plan Review: Plan XXX

## Structure: ✅ PASS
All required sections present.

## Task Quality: ⚠️ NEEDS IMPROVEMENT
- 3 tasks too vague ("Work on...", "Fix...")
- 2 tasks missing subtasks (>60 min work)

## Status Tracking: ✅ GOOD
- Only 1 task in progress
- Completion progress: 45% (9/20 tasks)

## Recommendations:
1. Break down Task 5 into subtasks
2. Make Tasks 12, 14, 15 more specific
3. Add technical decision for approach
```

---

### Workflow D: Convert TodoWrite to Plan

When TodoWrite list grows too large or work becomes multi-session:

**Step 1: Read Current TodoWrite**
```
Check current todos in conversation context
```

**Step 2: Assess Criteria**

Does work meet plan threshold?
- [ ] More than 5 todo items
- [ ] Work spans multiple sessions
- [ ] Involves architectural decisions
- [ ] Others need to understand work
- [ ] Work may need resuming later

**Step 3: Create Plan**

Follow Workflow A (Create New Plan) using TodoWrite items as starting point.

**Step 4: Migrate Todos**
1. Convert todo items to plan tasks
2. Add phases if work is multi-step
3. Add context/rationale missing from todos
4. Clear TodoWrite list
5. Reference plan in conversation

**Step 5: Inform User**
```
"This work has grown beyond simple todos. I've created Plan XXX to track it properly.
See: CLAUDE/Plan/XXX-name/PLAN.md

TodoWrite cleared. All tracking now in plan document."
```

---

## PLAN NUMBERING

Plans use sequential numbering:
- `001-first-plan`
- `002-second-plan`
- `003-third-plan`
- etc.

**To find next number:**
```bash
glob: "CLAUDE/Plan/**/PLAN.md"
# Look at existing numbers, use next sequential
```

**Plan numbers never change** even if plan is cancelled.

---

## TASK STATUS ICONS

From PlanWorkflow.md:

| Status | Icon | Markdown | Meaning |
|--------|------|----------|---------|
| Not Started | ⬜ | `⬜` | Task not yet begun |
| In Progress | 🔄 | `🔄` | Currently working on |
| Completed | ✅ | `✅` | Finished and verified |
| Blocked | 🚫 | `🚫` | Cannot proceed |
| Cancelled | ❌ | `❌` | No longer needed |
| On Hold | ⏸️ | `⏸️` | Paused temporarily |
| Needs Review | 👁️ | `👁️` | Awaiting review |

**Rules:**
- Limit 1-2 tasks marked 🔄 at a time
- Update immediately when status changes
- Document blocks with explanation
- Only mark ✅ after verification

---

## COMMIT MESSAGE FORMAT

When committing plan work:

```
Plan XXX: [Brief description of what was done]

[Optional: More details]

Refs: CLAUDE/Plan/XXX-name/PLAN.md
```

Examples:
```
Plan 038: Complete Phase 1 - Implementation

- Implemented core functionality
- Added tests
- Fixed issues

Refs: CLAUDE/Plan/038-feature-name/PLAN.md
```

---

## PLAN TEMPLATES

### Feature Implementation Template

```markdown
# Plan XXX: [Feature Name]

**Status**: ⬜ Not Started
**Type**: Feature Implementation
**Priority**: High | Medium | Low

## Overview
[What feature, why needed]

## Tasks
- [ ] ⬜ Design component structure
- [ ] ⬜ Implement core functionality
- [ ] ⬜ Add styling
- [ ] ⬜ Write tests
- [ ] ⬜ Update documentation

## Success Criteria
- [ ] Feature works as specified
- [ ] Tests passing
- [ ] Documentation updated
```

### Bug Fix Template

```markdown
# Plan XXX: Fix [Bug Description]

**Status**: ⬜ Not Started
**Type**: Bug Fix
**Severity**: Critical | High | Medium | Low

## Bug Description
[What's broken, how to reproduce]

## Tasks
- [ ] ⬜ Reproduce bug locally
- [ ] ⬜ Identify root cause
- [ ] ⬜ Implement fix
- [ ] ⬜ Add regression test
- [ ] ⬜ Verify fix works

## Success Criteria
- [ ] Bug no longer reproducible
- [ ] Test added to prevent regression
```

---

## COMMON MISTAKES TO AVOID

### Mistake 1: Vague Tasks
```markdown
❌ BAD: "Work on feature"
✅ GOOD: "Create component with specific functionality"
```

### Mistake 2: Too Many In Progress
```markdown
❌ BAD: 5 tasks marked 🔄 In Progress
✅ GOOD: 1 task marked 🔄 In Progress, rest are ⬜ or ✅
```

### Mistake 3: No Progress Updates
```markdown
❌ BAD: Plan created 2 weeks ago, no Notes & Updates
✅ GOOD: Regular dated entries showing progress
```

### Mistake 4: Missing Success Criteria
```markdown
❌ BAD: No "Success Criteria" section
✅ GOOD: Clear, measurable completion criteria
```

### Mistake 5: Using TodoWrite When Plan Needed
```markdown
❌ BAD: TodoWrite with 10+ items spanning days
✅ GOOD: Proper plan document when work > 2 hours
```

---

## CRITICAL PLANNING RULES

### NO TIME ESTIMATES

**NEVER include time estimates in plans:**
- ❌ "This will take 2-3 weeks"
- ❌ "Estimated effort: 5 days"
- ❌ "Phase 1: 3 hours"
- ✅ "Phase 1: Implementation" (order only)
- ✅ "Phase 2: Testing" (sequence only)

**What to include instead:**
- Phase ordering (what comes first, second, third)
- Task breakdown (concrete steps)
- Dependencies (what blocks what)
- Priority (High, Medium, Low)

### RESEARCH-BASED, NOT ASSUMPTION-BASED

**Plans MUST be based on researched facts:**
- ❌ "We should probably use X"
- ❌ "This might need a migration"
- ✅ "Research shows current system uses Y"
- ✅ "Analysis confirms 3 components need updating: [specific list]"

**Before planning, RESEARCH:**
- Current architecture (read existing code)
- Similar work done previously (check git history, other plans)
- Technical constraints (verify assumptions)
- Available components/tools (check documentation)

### PLANS EVOLVE

**Plans are living documents:**
- ✅ Plans can change over time
- ✅ Tasks can be added/removed
- ✅ Approaches can be adjusted
- ⚠️ **BUT**: Only humans can request changes
- ⚠️ **Always**: Document why changes were made in Notes & Updates

**Agents cannot:**
- Unilaterally change plan scope
- Remove tasks without human approval
- Change goals/success criteria independently

**Agents can:**
- Update task status (⬜ → 🔄 → ✅)
- Add progress notes
- Report blockers
- Suggest changes (for human approval)

## REMEMBER

- **Read PlanWorkflow.md first** - Always understand current standards
- **Plans for complexity** - TodoWrite for simple, plans for complex
- **NO time estimates** - Order and steps only, never durations
- **Research before planning** - Facts, not assumptions
- **Plans evolve** - Living documents, human-approved changes only
- **Break down tasks** - Concrete actionable steps
- **Update in real-time** - Status changes immediately
- **Opus for research** - Complex plans benefit from deep exploration
- **Verify completion** - Check success criteria before marking done
- **British English** - Throughout all documentation

---

## QUICK REFERENCE

| User Request | Action |
|--------------|--------|
| "Create a plan for X" | Read PlanWorkflow.md → Create plan → Get approval |
| "Update Plan XXX" | Read plan → Update status → Save |
| "Review Plan XXX" | Read plan and workflow → Validate → Report |
| "This todo list is too big" | Convert to plan using Workflow D |
| "Research and plan X" | Invoke opus planning agent |
| "What's the plan status?" | Read plan → Summarise progress |

---

**See Also**:
- `CLAUDE/PlanWorkflow.md` - Complete planning workflow documentation
- `CLAUDE/Plan/README.md` - Index of all plans

---

**Purpose**: Orchestrate planning workflow and ensure adherence to planning standards
