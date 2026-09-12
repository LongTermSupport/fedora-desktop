# Claude Code Planning Workflow

> **This file is daemon-owned.** It is deployed by the Claude Code Hooks
> Daemon into `CLAUDE/core/PlanWorkflow.core.md` and is overwritten on every
> daemon deploy/upgrade — do not hand-edit it, any local change is lost on the
> next refresh. Project-specific additions belong in this project's own
> `CLAUDE/PlanWorkflow.md` (the file named by the `plan_workflow.workflow_docs`
> config key), which opens with a markdown link to this document and layers on
> top of it only what is genuinely specific to this project.
>
> A plain markdown link, deliberately, not an `@`-import: `@` auto-resolves
> only inside a `CLAUDE.md` chain, and this document sits outside one, so it
> would not be a mechanism here — while eagerly re-inlining a large file into
> every session is a real cost. The forced read is a convention either way.
>
> Directory names below are written at their **defaults**. The agent-facing
> tree (`documentation.trees.agent`, default `CLAUDE/`) and the plan directory
> (`plan_workflow.directory`, default `CLAUDE/Plan/`) are both per-project
> configuration; read `.claude/hooks-daemon.yaml` for the values this project
> actually uses and substitute as you read.

This document defines the planning workflow supported and enforced by the
Claude Code Hooks Daemon's `plan_workflow` configuration. Developers and AI
agents working in a project with plan tracking enabled should follow it.

---

## Core Principles

1. **Plan Before Execute** - Never start implementation without a documented plan
2. **Break Down Complexity** - Decompose large work into manageable tasks
3. **Track Everything** - Every task has a status and owner
4. **Document Decisions** - Capture rationale for major decisions
5. **Iterate Rapidly** - Plans are living documents, update as you learn
6. **Test First (TDD)** - Write failing tests before implementation, where this project practises TDD
7. **Debug First** - Ground handler design in real hook event data before writing a handler
8. **Orchestrate Intelligently** - Use sub-agents and teams for parallel execution when possible

---

## Execution Strategies

Plans should specify the recommended execution approach based on complexity and model capabilities.

### Strategy Selection Matrix

| Plan Complexity                                        | Recommended Model | Execution Strategy      | Rationale                                                |
| ------------------------------------------------------ | ----------------- | ----------------------- | -------------------------------------------------------- |
| **Simple** (single handler, straightforward logic)     | Sonnet            | Sub-Agent Orchestration | Sonnet delegates independent tasks to specialised agents |
| **Medium** (multiple handlers, some dependencies)      | Sonnet            | Sub-Agent Orchestration | Sonnet coordinates sequential phases with parallel tasks |
| **Complex** (architectural changes, many dependencies) | Opus              | Sub-Agent Teams         | Opus manages a team of agents with task coordination     |
| **Critical** (releases, major refactors)               | Opus              | Sub-Agent Teams         | Opus provides strategic oversight and decision-making    |

**Haiku CANNOT orchestrate plans.** Only Opus and Sonnet are valid executors.

### Execution Strategy Definitions

**1. Single-Threaded**

- Main agent (Sonnet or Opus) executes all work directly
- No delegation to sub-agents
- Use for: Quick fixes, documentation updates, simple changes

**2. Sub-Agent Orchestration** (Sonnet preferred)

- Main agent (Sonnet) spawns specialised sub-agents for independent tasks
- Main agent coordinates but doesn't micromanage
- Sub-agents work in parallel when possible
- Use for: Multi-phase work, parallel test execution, independent modules
- Example: Main Sonnet spawns an Explore agent for codebase research, then spawns a project-specific implementation agent for TDD work

**3. Sub-Agent Teams** (Opus preferred)

- Main agent (Opus) creates a team with a task list
- Team members claim tasks autonomously
- Main agent provides strategic guidance and reviews
- Use for: Large-scale refactors, coordinated multi-file changes, releases
- Example: Opus creates a team with researcher, developer, tester agents; each claims tasks from the shared task list

### Model-Specific Guidance

**When executing as Sonnet:**

- Default to Sub-Agent Orchestration for multi-phase plans
- Spawn agents for independent tasks (an Explore agent for research, a project-specific implementation agent for TDD work)
- Use multiple parallel agents when tasks are independent
- Keep the main thread for coordination and decision-making

**When executing as Opus:**

- Default to Sub-Agent Teams for complex plans
- Create a team structure with clear roles
- Use a shared task list for coordination
- Provide strategic oversight and architectural decisions
- Review sub-agent output for quality and consistency

**Haiku:**

- CANNOT orchestrate plans
- Use only for: utility scripts, basic file operations, team support roles

### Choosing Recommended Executor

Plans specify **Recommended Executor** in the header:

```markdown
**Recommended Executor**: Opus | Sonnet
```

**Guidelines**:

- **Opus**: Architectural changes, releases, critical refactors, complex coordination
- **Sonnet**: Feature work, handler implementations, bug fixes, standard complexity

**Minimum**: Sonnet. Haiku cannot orchestrate plans.

---

## Plan Structure

### Directory Layout

Plans live under the directory named by the `plan_workflow.directory` config
key (default `CLAUDE/Plan`); the examples below assume that default:

```
CLAUDE/Plan/
├── 00001-example-plan/
│   ├── PLAN.md                      # Main plan document
│   ├── {supporting-docs*}.md        # Supporting analysis docs
│   └── assets/                      # Diagrams, logs, etc.
├── 00002-another-plan/
│   ├── PLAN.md
│   └── analysis.md
├── 00003-plan-with-journal/
│   ├── PLAN.md
│   ├── JOURNAL/
│   └── subagent-reports/            # Dispatched subagents' file-handoff reports
│       └── {yymmdd}-{agent-name}-{model}.md
└── README.md                        # Index of all plans
```

### Subagent report handoff (`subagent-reports/`)

A subagent dispatched to work on a plan writes any long-form report —
findings, exploration notes, review output — to a file under that plan
folder's `subagent-reports/` subdirectory, never inline as its final message.
The standard filename is `{yymmdd}-{agent-name}-{model}.md` (e.g.
`260901-explore-hook-surfaces-sonnet.md`). The subagent's final message stays
a short completion summary plus the file path.

Rationale: a subagent's return travels over a bounded-size wire channel. An
oversized inline report can have its MIDDLE silently elided by the harness
while both start/end sentinels survive intact — a coordinator can receive
what looks like a complete report while content is missing. The
`dispatch_declaration` handler (PreToolUse on the `Task` tool) injects this
contract at dispatch time when a prompt does not already declare it; the
`subagent_report_size_blocker` handler (SubagentStop) blocks an oversized
final message until it is re-routed through a file. Work that is NOT plan
work should instead declare an explicit destination (falling back to
`untracked/agent-reports/` when none is given).

`subagent-reports/` is a recognised plan-folder member for plan QA purposes —
its presence never triggers a stray-file or unexpected-content finding.

### Plan Numbering

- Plans are numbered sequentially with 5-digit zero-padding: `00001-`, `00002-`, `00003-`, etc. (`NNNNN` in templates)
- Use kebab-case for plan folder names
- Plan numbers never change even if a plan is cancelled
- The next number is tracked authoritatively (see `mkplan.bash` usage below) — never derive it by scanning the plan directory with `ls`/`find`, which misses archived plans and can disagree across branches

---

## Plan Document Structure

Every `PLAN.md` must follow this structure:

```markdown
# Plan NNNNN: [Plan Title]

**Status**: In Progress | Complete | Blocked | Cancelled
**Created**: YYYY-MM-DD
**Owner**: [Name/Agent]
**Priority**: High | Medium | Low
**Recommended Executor**: Opus | Sonnet | Haiku
**Execution Strategy**: Sub-Agent Orchestration | Sub-Agent Teams | Single-Threaded

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

- Depends on: Plan 00001 (Complete)
- Blocks: Plan 00003 (Not Started)
- Related: Plan 00002

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
- [ ] Every release-bound consequence is in the pending-release holding area
      (or: this plan has no release-bound consequences)

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Risk description | High/Med/Low | High/Med/Low | How we'll handle it |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes (git is the SSoT for "when").
     The blow-by-blow activity log lives in JOURNAL/ — see CLAUDE/PlanJournalling.md. -->

- Milestone reached at <commit-hash>
```

---

## Task Status System

### Status Icons

Use these Unicode icons for task status:

| Status           | Icon | Markdown | Meaning                           |
| ---------------- | ---- | -------- | --------------------------------- |
| **Not Started**  | ⬜   | `⬜`     | Task not yet begun                |
| **In Progress**  | 🔄   | `🔄`     | Currently being worked on         |
| **Completed**    | ✅   | `✅`     | Task finished and verified        |
| **Blocked**      | 🚫   | `🚫`     | Cannot proceed (dependency/issue) |
| **Cancelled**    | ❌   | `❌`     | Task no longer needed             |
| **On Hold**      | ⏸️   | `⏸️`     | Paused temporarily                |
| **Needs Review** | 👁️   | `👁️`     | Work done, awaiting review        |

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
3. **Document Blocks** - If marking 🚫, add a note explaining why
4. **Verify Completion** - Only mark ✅ after testing/verification and QA passing

---

## QA Integration

**Before completing any task, this project's full QA suite must pass.**

### Required QA Verification

Run whatever this project defines as its QA gate — lint, format check, type
check, tests, and anything else it treats as merge-blocking — before marking
any task complete. This project's own QA documentation and scripts (wherever
it keeps them) are the single source of truth for what "QA" means here; treat
any enumeration in a plan or a stale doc as a hint, not gospel.

### QA Task Format

Always include QA verification as a subtask:

```markdown
- [ ] ⬜ **Implement feature X**
  - [ ] ⬜ Write implementation
  - [ ] ⬜ Run this project's QA suite
  - [ ] ⬜ Fix any issues
  - [ ] ⬜ Verify all checks pass
```

---

## Plan QA (automated enforcement)

The daemon enforces plan-tree hygiene automatically. Most plan rot is
cross-file (PLAN.md status ↔ folder location ↔ README row ↔ git state), so
enforcement runs in three stages that share one check catalogue.

**Stage 1 — edit-time lint** (`plan_qa_edit`, PreToolUse): every Write/Edit of
a `PLAN.md` is linted against single-file rules on the content the file *would*
have. New material with a missing/invalid `**Status**:` line, a header that
contradicts an all-ticked body, or ad-hoc task markers is **blocked** with the
exact fix (mode `edit_mode`, default `block`). The plan-index `README.md` is
linted too, against one rule — `index-row-length`: keep every line under 500
characters, because a row is a pointer (link, status, one clause), not a
summary copied from the linked plan.

**Stage 2 — commit gate** (`plan_qa_commit_gate`, PreToolUse on `git commit`):
checks the **staged** tree against cross-file invariants -- index-at-birth (a
new plan folder stages its README row in the same commit), terminal-state
atomicity (a status flip to a terminal state ships the `git mv` into the
archive dir plus the README row and statistics recount in ONE commit), number
collisions, and row/folder bijection. Ships **warn-first** (mode
`commit_gate_mode`, default `warn`); read the advisory findings and amend the
commit before it lands.

**Stage 3 — session sweep** (`plan_qa_sweep`, SessionStart): reports whole-tree
drift once per new session as advisory context; never blocks (mode
`sweep_mode`, default `advise`).

### Allowed status tokens

`Not Started`, `In Progress`, `Complete`, `Blocked`, `Cancelled`,
`Superseded`, `Dormant`. Any **terminal** status (Complete, Cancelled,
Superseded) requires the plan folder to move into the archive directory
(`Completed/`, or `Cancelled/` where configured) in the **same commit** that
sets the status -- alongside the README index row update and statistics
recount.

### CLI

```bash
.claude/hooks-daemon/bin/hooks-daemon plan-qa --sweep          # whole tree; exit 1 on findings (CI-able)
.claude/hooks-daemon/bin/hooks-daemon plan-qa --check-staged   # staged-tree commit-gate checks
.claude/hooks-daemon/bin/hooks-daemon plan-qa --lint <PLAN.md> # single-file edit-stage checks
```

Add `--json` to any of these for machine-readable output.

### Policy & grandfathering

All policy lives in `.claude/hooks-daemon.yaml` under `plan_workflow.qa` (one
block shared by all three surfaces and the CLI). Legacy plans predating these
rules can be held to advise-only via `legacy_plan_allowlist` (plan numbers),
and historic duplicate numbers can be tolerated via `collision_allowlist`.

### Truth is enforced on LIVE plans, never on the historical record

Plan QA keeps **active** plans grounded in current truth. It does **not** try to
keep the archive matching today's tree, and no one should.

- **A live plan must state present truth.** It is read in full at the start of
  every session that touches it, so a task citing a path, command or option
  that has since moved actively misleads the next reader. If a live plan's
  current task text has gone stale, **fix it** — that is squarely in scope.
- **An archived plan is a RECORD of what was true when it was written.**
  Completed, Cancelled and Superseded plans are not maintained against the
  present. Editing one so it matches today's tree does not make it more
  accurate; it **falsifies the record** of what the work actually faced.
- **The same applies to a dated historical block inside a live plan** — an
  "assumed defaults (from the audit)" list, an incident write-up, a findings
  table stamped with a date. That block is a record of a moment, not a claim
  about now, and it stays as written.

**Consequence for `path-existence`.** The check cannot tell a live reference
from historical prose, so it will flag backticked `src/...` paths in both. On
an archived plan, or inside a dated block, that finding is **expected noise —
leave the prose alone**. And never un-backtick a path to silence it: that
games the check without changing anything a reader sees. Only a stale path in
a live plan's current text is a real finding.

---

## TDD Integration

Where this project practises Test-Driven Development (optionally enforced by
the daemon's `tdd_enforcement` handler, which blocks creating a production
source file until a corresponding test file exists), implementation work
follows the Red-Green-Refactor cycle.

### TDD Workflow

1. **Red**: Write a failing test that defines the expected behaviour
2. **Green**: Write the minimum code to make the test pass
3. **Refactor**: Clean up the code while keeping tests green
4. **Verify**: Run this project's full QA suite

### TDD Task Format

```markdown
- [ ] ⬜ **Implement parser for X**
  - [ ] ⬜ Write failing test for parse() behaviour
  - [ ] ⬜ Implement parse() to pass test
  - [ ] ⬜ Write failing test for edge cases
  - [ ] ⬜ Implement handling for edge cases
  - [ ] ⬜ Refactor for clarity
  - [ ] ⬜ Verify this project's test coverage requirement is maintained, if it has one
  - [ ] ⬜ Run this project's full QA suite
```

### Coverage Requirement

- If this project sets a minimum test coverage threshold, treat it as a
  completion gate alongside the rest of the QA suite
- New code must have corresponding tests
- Check this project's own QA tooling for where coverage is reported

### Running Tests

Use this project's own test runner and test-file conventions — consult its QA
documentation for the exact commands. There is no single cross-language
answer here; what matters is that the loop is fast enough to support
Red-Green-Refactor above.

---

## Planning Workflow Steps

### Step 1: Identify Work

When new work is identified:

1. Check if it fits in an existing plan
2. If not, determine if a new plan is needed
3. For small, quick tasks, use TodoWrite instead

**New Plan Threshold**: work that is non-trivial, spans multiple phases, or may need to be resumed across sessions

### Step 2: Create Plan

1. Create the plan folder with the scaffolding script, run from inside the
   configured plan directory (`plan_workflow.directory`, default shown):
   ```bash
   CLAUDE/Plan/mkplan.bash "descriptive-kebab-name"
   ```
   This assigns the next plan number atomically from the daemon's tracked
   counter (not a folder scan), scaffolds `PLAN.md` from the template, and
   advances the counter — hand-creating the folder with `mkdir` is blocked
   wherever the scaffolder is deployed.
2. Fill in overview, goals, and initial task breakdown
3. Add the plan entry to the plan index (`README.md` in the plan directory)

### Step 3: Break Down Tasks

1. Decompose work into phases (if needed)
2. Break each phase into concrete tasks
3. Break tasks into subtasks if a task bundles multiple distinct actions
4. Ensure tasks are actionable and testable
5. **Include TDD subtasks for implementation work**
6. **Include QA verification subtasks**

**Good Task**: "Add input validation to the checkout form, with tests"
**Bad Task**: "Fix the checkout page"

### Step 4: Review & Approve

1. Review plan completeness
2. Verify tasks are well-defined
3. Check for missing dependencies
4. Get stakeholder approval (if needed)

### Step 5: Execute

1. Mark plan status as 🔄 In Progress
2. Work through tasks sequentially
3. **Follow the TDD cycle for implementation, where this project practises it**
4. Update task status in real-time
5. Document any blockers or changes
6. **Run QA before each commit**
7. Commit work with reference to plan: `Plan 00001: <short description>`

### Failsafe Recovery Cron (while executing)

For long, multi-hour plan executions, set up a **non-durable hourly failsafe
recovery cron** at the start of execution (the `recovery_cron_advisor` handler
prompts this on plan creation/progress when enabled). It is a safety net that
resumes work stalled by **external** factors — Claude API overload, rate limits,
usage limits, network failures — by firing only while the REPL is idle.

**This is NOT a heartbeat.** You must **never** pace yourself to the cron or wait
for it between units of work — that is an own goal that turns a recovery net into
an artificial throttle. Work proceeds at full speed until an external factor
actually stops you; the cron only matters once something has already gone wrong.

- Create it non-durable (`CronCreate` `durable:false`, `recurring:true`, an
  off-:00 minute) and record the cron ID in the plan's JOURNAL (the activity
  log; the `Delivery & Milestones` stub is for milestones + commit hashes).
- If you are blocked **only** on human input, the cron is a no-op — keep waiting.
- On plan completion, **do not reflexively delete the cron** — deleting it while
  the session is still live leaves you with no recovery coverage if a rate limit
  or usage limit hits next. Keep it whenever further work may happen this session
  (it is non-durable and dies automatically on session exit, and is a no-op when
  nothing is resumable). Run `CronDelete` only once you are certain the session is
  finished with no further work.

### Step 6: Complete

1. **Verify all QA checks pass**
2. Verify all success criteria met
3. Mark all tasks as ✅
4. Mark plan status as Complete
5. Record the delivery commit hash(es) in the plan's "Delivery & Milestones"
   section (NOT a completion date — git is the source of truth for "when")
6. Document any lessons learned
7. **Follow the Plan Completion Checklist below**

### Definition of done: merged into main, never released

A plan is **done when its work is fully completed, verified, and merged into
main**. That is the whole definition. A release is **never** part of it:

- Do NOT write a task, success criterion, or status line that waits on a
  release — no "run `/release`", no "acceptance-test at release time", no
  "release notes must mention X", no "tag and publish". A plan with such an
  item is not `In Progress`; it is finished work with a mislabelled header.
- A release is a **scope decision made by a human**, bundling whatever is on
  main at that moment. It is gated on the state of main, not on any plan, and
  no plan is gated on it. Tying the two together produces plans that sit
  `In Progress` for weeks with nothing left to do, hide real in-flight work
  among them, and make "is the slate clean?" unanswerable.
- Anything a release must carry (a release-notes callout, an upgrade-guide
  entry, a post-upgrade task, an acceptance probe) is written **into the
  repository's pending-release holding area now**, as one of the plan's own
  tasks — so the release picks it up mechanically. Updating that holding area
  IS the plan's last step where one is needed; waiting for the release to
  consume it is not. (In this project the holding area is
  `CLAUDE/UPGRADES/UNRELEASED/`; a client project names its own.)

If a plan already has a release-shaped item, strike it as out of the
definition of done, record where its substance lives, and close the plan.

### Plan Completion Checklist

When a plan is complete, follow these steps to properly close it out. Skipping steps leads to stale plan indexes and orphaned folders.

0. **Holding area is current (gate — do this BEFORE flipping the status)**:
   every release-bound consequence of this plan is already written into the
   pending-release holding area, in whichever of its shapes applies — a
   post-upgrade task, a truth change, a config change, a release-notes
   callout, an upgrade-guide entry. If the plan has none, the last Success
   Criterion says so explicitly ("no release-bound consequences"). A plan
   whose release-bound content is still in someone's head is not done, and a
   plan waiting for the release to consume that content is already done.
1. **Update PLAN.md status**: Change `**Status**:` to `Complete`. Do NOT add a completion date — git history is authoritative for "when". Cite the delivery commit hash(es) in the "Delivery & Milestones" section instead.
2. **Mark all tasks**: Change `- [ ]` to `- [x]` for all completed tasks in the plan
3. **Move to Completed folder**: Relocate the plan directory into the archive
   ```bash
   git mv CLAUDE/Plan/NNNNN-description CLAUDE/Plan/Completed/NNNNN-description
   ```
4. **Update README.md**: Edit the plan index `README.md` with all of the following changes:
   - Remove the plan entry from the "Active Plans" section
   - Add the plan to the "Completed Plans" section with a brief summary
   - Update plan statistics (Total, Active count, Completed count, Success Rate)
5. **Consider aging out old completed rows** (useful once the index grows
   large): after adding your row, if the "Completed Plans" section has grown
   unwieldy, pick a retention window that suits this project (e.g. the most
   recent 30 completed rows) and move every row beyond it, verbatim — no
   rewording, no trimming; multi-line rows with sub-bullets move whole — into
   a `Completed/README.md` archive, in the same commit as the status flip.
6. **Unblock dependent plans**: Check if any other plans had `Blocked by: Plan NNNNN` referencing this plan and remove that blocker so dependent work can proceed
7. **Commit**: Include all plan-related file changes (PLAN.md, README.md, archive README.md, directory move) in a single commit using the message format:
   ```
   Plan NNNNN: Complete - Brief description
   ```

**Why a single commit?** Atomic plan closure ensures the plan index, archive, and status are always consistent. Splitting across commits risks partial updates if work is interrupted.

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

If a TodoWrite list grows beyond 5 items or becomes multi-session:

1. Create a proper plan
2. Migrate tasks to the plan
3. Clear TodoWrite
4. Reference the plan in work

---

## Plan Templates

### Project Handler Implementation Plan Template

Use this template when implementing a project-level handler — a custom hook
handler scoped to this project, living in this project's own repository
alongside its code (see this project's project-handler guide, if it has one).

````markdown
# Plan NNNNN: [Handler Name] Handler

**Status**: Not Started
**Type**: Project Handler Implementation
**Event Type**: PreToolUse | PostToolUse | SessionStart | etc.
**Priority Range**: [pick a priority number for this handler; lower runs earlier]

## Overview

[What handler, why needed, what behaviour it enforces]

## Goals

- Intercept [specific event/pattern]
- Enforce [specific behaviour]
- Maintain this project's required test coverage, if it has one

## Non-Goals

- [What this handler does NOT do]

## Design

**Before implementation**, ground the design in real data rather than
assumption:
```bash
.claude/hooks-daemon/bin/hooks-daemon logs
# ... perform the target actions in a live Claude Code session ...
```
````

**Event Analysis**:

- Event Type: [PreToolUse, etc.]
- Tool Name: [Write, Bash, etc.]
- Key hook_input fields: [list relevant fields]
- Trigger Pattern: [what triggers this handler]

## Tasks

### Phase 1: Design

- [ ] ⬜ Inspect recent event flow for the target scenario
- [ ] ⬜ Analyse captured events and data
- [ ] ⬜ Document event type and patterns
- [ ] ⬜ Design handler matching logic
- [ ] ⬜ Determine priority and terminal behaviour

### Phase 2: TDD Implementation

- [ ] ⬜ Create a co-located test file (`test_{handler_name}.py`)
- [ ] ⬜ Write failing test for matches() - positive case
- [ ] ⬜ Write failing test for matches() - negative cases
- [ ] ⬜ Implement matches() to pass tests
- [ ] ⬜ Write failing test for handle() - expected result
- [ ] ⬜ Write failing test for handle() - edge cases
- [ ] ⬜ Implement handle() to pass tests
- [ ] ⬜ Refactor and clean up

### Phase 3: Integration

- [ ] ⬜ Validate: `.claude/hooks-daemon/bin/hooks-daemon validate-project-handlers`
- [ ] ⬜ Run this project's full QA suite
- [ ] ⬜ Restart the daemon and confirm it is running:
  `.claude/hooks-daemon/bin/hooks-daemon restart`
- [ ] ⬜ Test with a live session
- [ ] ⬜ Update documentation

## Handler Specification

```python
class [HandlerName]Handler([Event]HandlerBase):
    def __init__(self) -> None:
        super().__init__(
            handler_id="[handler-name]",
            priority=[XX],
            terminal=[True/False],
        )

    def matches(self, hook_input: dict) -> bool:
        # Pattern matching logic
        pass

    def handle(self, hook_input: dict) -> [Event]Result:
        # Handler behaviour
        pass
```

The base and result type are chosen by the event: `PreToolUseHandlerBase`/`GatingResult`, `PostToolUseHandlerBase`/`BlockingResult`, or `<Event>HandlerBase`/`AdvisoryResult` for everything else — the base class only allows constructing the result its event can actually deliver.

## Success Criteria

- [ ] Handler correctly intercepts target events
- [ ] All tests passing
- [ ] This project's coverage requirement maintained, if it has one
- [ ] Live testing successful
- [ ] Documentation updated
- [ ] All QA checks pass

### Feature Implementation Plan Template

```markdown
# Plan NNNNN: [Feature Name]

**Status**: Not Started
**Type**: Feature Implementation

## Overview

[What feature, why needed]

## Tasks

### Phase 1: Design
- [ ] ⬜ Analyse requirements
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
- [ ] ⬜ Run this project's full QA suite
- [ ] ⬜ Fix any QA issues
- [ ] ⬜ Update documentation

## Success Criteria

- [ ] Feature works as specified
- [ ] All tests passing, meeting this project's coverage requirement if it has one
- [ ] All QA checks pass
- [ ] Documentation updated
```

### Bug Fix Plan Template

```markdown
# Plan NNNNN: Fix [Bug Description]

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
- [ ] ⬜ Run this project's full QA suite
- [ ] ⬜ Verify fix works in live testing

## Success Criteria

- [ ] Bug no longer reproducible
- [ ] Failing test now passes
- [ ] No regression in other tests
- [ ] All QA checks pass
```

### Refactoring Plan Template

Use this template when improving existing code without changing behaviour.

```markdown
# Plan NNNNN: Refactor [Component/Area]

**Status**: Not Started
**Type**: Refactoring

## Overview

[What needs refactoring, why it improves the codebase]

## Goals

- Improve [readability/maintainability/performance]
- Maintain existing behaviour
- Maintain or improve test coverage

## Non-Goals

- No new features
- No behaviour changes

## Tasks

### Phase 1: Preparation
- [ ] ⬜ Identify all affected code
- [ ] ⬜ Ensure adequate test coverage exists
- [ ] ⬜ Document current behaviour

### Phase 2: Refactoring
- [ ] ⬜ Apply refactoring incrementally
- [ ] ⬜ Run tests after each change
- [ ] ⬜ Verify no behaviour changes

### Phase 3: Verification
- [ ] ⬜ Run this project's full QA suite
- [ ] ⬜ Compare before/after behaviour
- [ ] ⬜ Update documentation if needed

## Success Criteria

- [ ] All existing tests pass
- [ ] Test coverage maintained (or improved) against this project's own requirement, if it has one
- [ ] No behaviour changes
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
- "Increase test coverage for the payments module to this project's required threshold"

❌ **Bad Tasks**:

- "Fix the daemon"
- "Make it work better"
- "Work on handlers"

### Task Granularity

- **Task**: a single focused unit of work with a clear definition of done
- **Subtask**: a specific, narrow action within a task
- **Phase**: a group of related tasks

### Status Update Discipline

1. **Before starting work**: Review plan, mark task 🔄
2. **During work**: Update status if blocked
3. **After completing**: Mark ✅, run QA, commit with reference
4. **Regularly**: Review the plan and edit it IN PLACE so it states current
   truth. Append the narrative of what happened to the plan's `JOURNAL/`
   day-file — never to `PLAN.md`. See [CLAUDE/PlanJournalling.md](../PlanJournalling.md).

### Handling Changes

When requirements change mid-plan:

1. **Document Change**: Edit `PLAN.md` in place to state the new truth
   (revise Goals/Tasks, record the reasoning under Technical Decisions), and
   append a dated entry to the plan's `JOURNAL/` recording what changed and
   why. Do NOT append a change-log section to `PLAN.md`.
2. **Update Tasks**: Revise task list as needed
3. **Assess Impact**: Update estimates, dependencies
4. **Communicate**: Ensure stakeholders are aware

---

## Plan Reviews

### Daily Review (If actively working on plan)

- Are tasks up to date?
- Any blockers need attention?
- Is plan still on track?
- Are QA checks still passing?

### Weekly Review (For active plans)

- Progress vs plan?
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

### Commit Messages

Reference plans in commits:

```
Plan 00001: Add rate-limit check to the checkout API

- Add RateLimiter to guard the checkout endpoint
- Include tests for burst and sustained load
- Register the limiter with a configurable threshold

Refs: CLAUDE/Plan/00001-example-plan
```

### Branch Naming

For larger plans, use feature branches:

```
plan/00001-checkout-rate-limit
plan/00002-config-refactor
plan/00003-search-indexing
```

### Pre-Commit Verification

Before committing, always verify:

```bash
# Run this project's full QA suite
<this project's QA command>

# Check git status
git status

# Stage specific files (avoid staging secrets)
git add src/payments/rate_limiter.py
git add tests/payments/test_rate_limiter.py

# Commit with plan reference
git commit -m "Plan 00001: Add rate-limit check to the checkout API"
```

---

## Plan Index

Maintain the plan index `README.md`:

```markdown
# Plans Index

## Active Plans
- [00001: Checkout Rate Limit](00001-example-plan/PLAN.md) - In Progress

## Completed Plans
- None yet

## Blocked Plans
- None

## Cancelled Plans
- None
```

---

## AI Agent Guidelines

When Claude Code (or other AI agents) work on a project with plan tracking enabled:

01. **Always check for existing plans** before starting work
02. **Create a plan if none exists** for non-trivial work
03. **Choose execution strategy** based on current model and plan complexity:
    - **Sonnet**: Default to Sub-Agent Orchestration
    - **Opus**: Default to Sub-Agent Teams
    - **Haiku**: Default to Single-Threaded
04. **Ground handler design in real event data first** - Before writing a
    project-level handler, inspect recent hook event flow (e.g. via
    `.claude/hooks-daemon/bin/hooks-daemon logs`) rather than guessing at the
    shape of `hook_input`
05. **Follow TDD workflow**, where this project practises it - Write failing tests before implementation
06. **Update task status in real-time** as you work
07. **Run QA before commits** - this project's full QA suite must pass
08. **Document blockers immediately** if you get stuck
09. **Ask user for approval** before marking plan complete
10. **Reference plans in all commits** for traceability

### Agent Workflow Example

```
User: "Add a project handler that reminds to update the changelog when release files change"

Agent:
1. Checks the plan directory for an existing plan
2. If none, creates Plan 00001 (via `mkplan.bash`)
3. Inspects recent event flow to see what hook_input looks like for the scenario
4. Analyses events to determine handler design
5. Breaks down into TDD tasks
6. Shows plan to user for approval
7. Begins execution:
   - Write failing test
   - Implement handler
   - Run this project's QA suite
8. Commits with "Plan 00001: Add changelog-reminder project handler"
9. Ticks the task in PLAN.md and appends the narrative to the plan's JOURNAL/
10. Marks complete when all QA passes
```

### Project Handler Development Workflow

**Ground design in real data first, develop second:**

1. Identify the scenario ("remind about X after Y", "block Z", etc.)
2. **Inspect recent hook event flow** for the scenario, e.g.
   `.claude/hooks-daemon/bin/hooks-daemon logs`
3. Determine which event type fires and what data is available in `hook_input`
4. Write tests first (TDD)
5. Implement the handler
6. Run this project's full QA suite
7. Validate: `.claude/hooks-daemon/bin/hooks-daemon validate-project-handlers`
8. Test in a live Claude Code session

---

## Summary

**Remember**:

- Plan before you code
- Ground handler design in real event data before writing one
- Write tests first (TDD), where this project practises it
- Run QA before commits
- Update status religiously
- Keep tasks concrete and testable
- Document decisions and changes
- Plans are living documents

**Questions?** See examples in your plan directory, or ask in conversation.
