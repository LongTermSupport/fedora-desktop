# Claude Code Hooks - Front Controller Architecture

**CANONICAL DOCUMENTATION** - Single source of truth

## Critical Rules

⚠️ ALL new hooks MUST use front controller pattern
⚠️ Standalone hook files are FORBIDDEN
⚠️ TDD is MANDATORY (tests before code)
⚠️ 95%+ coverage required

## Architecture

Current: Front controller with Handler base class pattern (10x faster, easy to maintain)

Performance improvement: 200ms to 21ms per PreToolUse operation

## Directory Structure

All hooks live in controller/ directory:
- front_controller.py - Core engine with Handler, HookResult, FrontController classes
- pre_tool_use.py - PreToolUse entry point with all handlers registered
- handlers/ - Handler implementations organised by event type
- tests/ - Comprehensive test suite (364 tests, 100% coverage)

## Handler Pattern

All handlers inherit Handler base class and implement:
- matches() method - Pattern matching logic
- handle() method - Execution logic returning HookResult

Priority-based dispatch: lower number runs first (5 to 60 range)

First match wins: once matches() returns True, handle() executes and dispatch stops

## Hook Guidance Pattern

Hooks can provide guidance to agents without blocking:

HookResult supports `guidance` parameter for "allow with feedback" mode:
- decision="allow" - Operation proceeds
- guidance="..." - Agent receives actionable feedback

Agent Acknowledgment Protocol:
When hook provides guidance, agent MUST explicitly acknowledge:
1. ✅ Hook detected: [handler name]
2. 📋 Guidance: [summary of guidance]
3. 🎯 My action: [what agent will do based on guidance]

Example: PlanWorkflowHandler provides guidance when creating plans

## Agent Identification

**LIMITATION**: Hooks cannot currently identify which agent is making tool calls.

`hook_input` only contains:
- `tool_name`: Name of tool (Bash, Write, Edit, etc.)
- `tool_input`: Tool-specific parameters (command, file_path, etc.)

**No agent metadata available**:
- No agent name
- No session ID
- No execution context

**Workaround**: Use command/file patterns to infer context indirectly.

**Implication**: Cannot create agent-specific hook behavior or log which agent triggered a hook.

## Core Concepts

Handler: Base class with matches() and handle() methods

HookResult: Return object with decision (allow/deny/ask) and optional reason

FrontController: Dispatch engine that routes events to handlers

Priority ranges: 5=architecture, 10-20=safety, 25-45=workflow, 50-60=tool usage

## Workflow State Handlers

Two handlers work together to preserve workflow state across compaction cycles:

### WorkflowStatePreCompactHandler

**Event**: PreCompact (before compaction)
**Location**: `handlers/pre_compact/workflow_state_pre_compact_handler.py`
**Tests**: 12 tests passing

**Purpose**: Detects active workflows and saves/updates persistent state file

**Behavior**:
1. Detects if formal workflow is active (checks CLAUDE.local.md or active plans)
2. If NO workflow: Returns allow, no file created
3. If YES workflow:
   - Sanitizes workflow name for safe directory/filename
   - Looks for existing state file in `./untracked/workflow-state/{workflow-name}/`
   - If found: UPDATES existing file (preserves `created_at` timestamp)
   - If not found: CREATES new file with start_time timestamp
4. Always returns allow (never blocks compaction)

**State File**: `./untracked/workflow-state/{workflow-name}/state-{workflow-name}-{start-time}.json`

### WorkflowStateRestorationHandler

**Event**: SessionStart (when session resumes)
**Matcher**: `source: "compact"`
**Location**: `handlers/session_start/workflow_state_restoration_handler.py`
**Tests**: 14 tests passing

**Purpose**: Restores workflow context after compaction with forced file reading

**Behavior**:
1. Finds all state files in `./untracked/workflow-state/*/`
2. If none: Returns allow, no guidance
3. If found: Sorts by modification time (most recent first)
4. Reads most recently modified state file
5. Builds guidance message with:
   - Workflow name and current phase
   - **REQUIRED READING** with @ syntax (forces file reading)
   - Key reminders
   - Context variables
   - ACTION REQUIRED section
6. **DOES NOT DELETE** state file (persists across compaction cycles)
7. Returns allow with guidance context

**Complete Documentation**: REQUIRED READING: @CLAUDE/Workflow.md for complete workflow state management documentation

## TDD Workflow

1. Write tests FIRST
2. Implement handler to make tests pass
3. Register in __init__.py and entry point
4. Verify 95%+ coverage
5. Test live with real tool calls

## Learning by Example

Read actual implementation files as canonical examples:
- controller/front_controller.py - Core engine and utility functions
- controller/handlers/pre_tool_use/*.py - 17 working handler examples
- controller/tests/test_*.py - Complete test examples

## Testing

Run tests: python3 controller/run_tests.py
Check coverage: python3 controller/analyze_coverage.py

Current status: 397 tests (364 PreToolUse + 33 Workflow State), 100% coverage

## Enforcement

Handler exists to prevent standalone hook creation - all hooks must use controller pattern

## Reference

- This file: Architecture overview
- README.md: Legacy hook inventory
- controller/: All implementation files (read these for complete details)
