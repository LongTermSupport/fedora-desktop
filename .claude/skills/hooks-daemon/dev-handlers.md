# Develop Project-Level Handlers

Scaffold new project-level handlers with automatic file generation and TDD structure.

## Quick Start

```bash
/hooks-daemon dev-handlers
```

This interactive command will:

1. Prompt for handler details (name, event type, priority)
2. Create handler file in `.claude/project-handlers/{event_type}/`
3. Create co-located test file
4. Generate TDD-ready boilerplate
5. Register handler in `.claude/hooks-daemon.yaml`
6. Display next steps

## What Gets Created

### Handler File

Location: `.claude/project-handlers/{event_type}/{handler_name}.py`

```python
from claude_code_hooks_daemon.core import Handler, HookResult, Decision
from claude_code_hooks_daemon.constants import HandlerID, Priority

class MyCustomHandler(Handler):
    def __init__(self) -> None:
        super().__init__(
            handler_id=HandlerID.PROJECT_MY_CUSTOM,
            priority=Priority.CUSTOM_50,
            terminal=False
        )

    def matches(self, hook_input: dict) -> bool:
        # TODO: Implement matching logic
        return False

    def handle(self, hook_input: dict) -> HookResult:
        # TODO: Implement handler logic
        return HookResult(decision=Decision.ALLOW)

    def get_acceptance_tests(self) -> list:
        # TODO: Add acceptance tests
        return []
```

### Test File

Location: `.claude/project-handlers/{event_type}/test_{handler_name}.py`

```python
import pytest
from .my_custom_handler import MyCustomHandler

class TestMyCustomHandler:
    def test_initialization(self):
        handler = MyCustomHandler()
        assert handler.name == "project_my_custom"
        assert handler.priority == 50

    def test_matches_positive_case(self):
        # TODO: Test when handler should match
        handler = MyCustomHandler()
        hook_input = {}  # Add test input
        assert handler.matches(hook_input) is True

    def test_matches_negative_case(self):
        # TODO: Test when handler should NOT match
        handler = MyCustomHandler()
        hook_input = {}  # Add test input
        assert handler.matches(hook_input) is False

    def test_handle_returns_expected_result(self):
        # TODO: Test handler behavior
        handler = MyCustomHandler()
        hook_input = {}  # Add test input
        result = handler.handle(hook_input)
        assert result.decision == Decision.DENY
```

## TDD Workflow

After scaffolding, follow Test-Driven Development:

### 1. RED Phase - Write Failing Tests

```bash
# Run tests - they should FAIL
pytest .claude/project-handlers/{event_type}/test_{handler_name}.py -v
```

Write tests that define expected behavior before implementing.

### 2. GREEN Phase - Implement Handler

Update handler implementation to make tests pass:

- Implement `matches()` logic
- Implement `handle()` logic
- Add acceptance tests to `get_acceptance_tests()`

```bash
# Run tests - they should PASS
pytest .claude/project-handlers/{event_type}/test_{handler_name}.py -v
```

### 3. REFACTOR Phase - Clean Up

Improve code quality while keeping tests green:

- Remove duplication
- Improve clarity
- Add comments only where needed

### 4. Integration Test

```bash
# Restart daemon to load handler
$PYTHON -m claude_code_hooks_daemon.daemon.cli restart

# Verify handler loaded
$PYTHON -m claude_code_hooks_daemon.daemon.cli handlers | grep my_custom

# Test with real hook events
# (trigger the scenario your handler intercepts)
```

## Handler Configuration

Handlers are automatically registered in `.claude/hooks-daemon.yaml`:

```yaml
project_handlers:
  enabled: true
  path: .claude/project-handlers
  handlers:
    {event_type}:
      my_custom:
        enabled: true
        priority: 50
```

## Event Types

Choose the appropriate event type for your handler:

- **pre_tool_use** - Intercept before tool execution (blocking/advisory)
- **post_tool_use** - React after tool execution (context injection)
- **session_start** - Run once at session startup
- **user_prompt_submit** - Process user messages
- **status_line** - Contribute to status line display
- **stop** - Run before session ends
- **notification** - Handle Claude Code notifications

## Priority Guidelines

Choose priority based on handler type:

- **10-20**: Safety handlers (destructive operations)
- **25-35**: Code quality (linting, TDD enforcement)
- **36-55**: Workflow (planning, project-specific rules)
- **56-60**: Advisory (non-blocking guidance)
- **100+**: Logging/cleanup

## Testing Project Handlers

```bash
# Run project handler tests
$PYTHON -m claude_code_hooks_daemon.daemon.cli test-project-handlers --verbose

# Validate handlers load correctly
$PYTHON -m claude_code_hooks_daemon.daemon.cli validate-project-handlers

# Generate acceptance test playbook (includes project handlers)
$PYTHON -m claude_code_hooks_daemon.daemon.cli generate-playbook > /tmp/playbook.md
```

## Examples

See example project handlers:

- `examples/project-handlers/pre_tool_use/example_blocker.py` - Blocking handler
- `examples/project-handlers/post_tool_use/example_advisory.py` - Advisory handler
- `examples/project-handlers/session_start/example_startup.py` - Startup handler

## Documentation

For comprehensive handler development guide:

- See: `CLAUDE/HANDLER_DEVELOPMENT.md`
- See: `CLAUDE/PROJECT_HANDLERS.md`
- See: `CLAUDE/DEBUGGING_HOOKS.md` (event flow debugging)

## Next Steps After Scaffolding

1. **Debug event flow** (recommended):

   ```bash
   ./scripts/debug_hooks.sh start "Testing my handler scenario"
   # ... perform actions that should trigger handler ...
   ./scripts/debug_hooks.sh stop
   # Analyze logs to see what hook_input data is available
   ```

2. **Write failing tests** for matches() and handle()

3. **Implement handler** to make tests pass

4. **Add acceptance tests** to get_acceptance_tests()

5. **Restart daemon** and verify handler loads

6. **Test in real session** with actual hook events
