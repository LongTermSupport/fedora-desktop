# Project-Level Handlers

This directory contains **project-specific** handlers for Claude Code hooks.

## When to Use Project-Level Handlers

Use project-level handlers when you need custom hook behaviour that is:
- Specific to this project's workflow
- Not generally useful for other projects
- Related to project-specific conventions or policies

## Handler Development Workflow

1. **TDD Required** - Write tests FIRST, then implementation
2. **Use scaffolding tool** - Run handler creation command
3. **Implement matches() and handle()** methods
4. **Test thoroughly** - Minimum 95% coverage required
5. **Register in config** - Add to .claude/hooks-daemon.yaml

## Directory Structure

Each hook event has its own subdirectory:
- `pre_tool_use/` - Before tool execution
- `post_tool_use/` - After tool execution
- `session_start/` - When session begins
- (etc for all 10 hook events)

Each subdirectory contains:
- `README.md` - Event-specific guide
- Handler Python files
- `tests/` - Test files (required)

## Creating a New Handler

See individual event README files for examples and templates.

For general handler development, see:
.claude/hooks-daemon/README.md
