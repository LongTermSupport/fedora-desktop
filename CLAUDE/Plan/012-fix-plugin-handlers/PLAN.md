# Plan 012: Fix Plugin Handler Event-Type Registration Failure

**Status**: Cancelled (2026-02-17)
**Created**: 2026-02-17
**Owner**: Agent
**Priority**: High
**Estimated Effort**: N/A

**Cancellation reason**: The bug is in the upstream `claude-code-hooks-daemon` library
(`github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon`), not in this project.
A full bug report has been filed at `untracked/upstream-bug-report-plugin-handler-suffix.md`
for submission to the upstream maintainers. No local fix is appropriate.

## Overview

The hooks daemon restarts with the following warnings, and both custom plugin handlers
are silently skipped — leaving the project without its Infrastructure-as-Code
enforcement guards:

```
Could not determine event type for plugin handler 'prevent-system-file-edits'
  (class: SystemPathsHandler), skipping
Could not determine event type for plugin handler 'enforce-ansible-deployment'
  (class: AnsibleEnforcementHandler), skipping
```

The handlers are defined in:

- `.claude/hooks/handlers/pre_tool_use/system_paths.py` (`SystemPathsHandler`)
- `.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py` (`AnsibleEnforcementHandler`)

They are registered in `.claude/hooks-daemon.yaml` under the `plugins:` key with
`event_type: "pre_tool_use"`. The daemon loads them successfully (the `PluginLoader`
finds and instantiates the classes without error), but immediately discards them during
the event-type assignment step in `DaemonController._load_plugins`.

## Root Cause

### Where the bug lives

The bug is a **daemon bug**, not a handler bug. It lives in:

```
.claude/hooks-daemon/src/claude_code_hooks_daemon/daemon/controller.py
```

in the `_load_plugins` method, lines 247–258.

### What goes wrong, step by step

**Step 1 — Loading succeeds.**
`PluginLoader.load_handler()` (in `plugins/loader.py`, lines 88–100) correctly
discovers the handler class by trying two name forms:

```python
class_name          = snake_to_pascal(stem)          # e.g. "SystemPaths"
class_name_with_suffix = f"{class_name}Handler"       # e.g. "SystemPathsHandler"

if hasattr(module, class_name_with_suffix):           # ✅ found "SystemPathsHandler"
    handler_class_raw = getattr(module, class_name_with_suffix)
elif hasattr(module, class_name):
    handler_class_raw = getattr(module, class_name)
```

The "Handler"-suffixed form is found, the class is instantiated, and a valid
`Handler` object is returned. At this point, `handler.__class__.__name__` is
`"SystemPathsHandler"`.

**Step 2 — Event-type matching fails.**
Back in `controller._load_plugins`, the code attempts to find which plugin config
entry the loaded handler came from, so it can read `plugin.event_type`. It does
this by comparing the handler's class name against a derived expected name:

```python
plugin_module   = Path(plugin.path).stem          # "system_paths"
expected_class  = PluginLoader.snake_to_pascal(plugin_module)  # "SystemPaths"

if handler.__class__.__name__ == expected_class:   # "SystemPathsHandler" == "SystemPaths"
    event_type_str = plugin.event_type             # ❌ never reached
    break
```

`PluginLoader.snake_to_pascal("system_paths")` returns `"SystemPaths"` (no suffix).
The actual class name is `"SystemPathsHandler"`. The comparison is always `False`.
After checking every plugin config entry, `event_type_str` remains `None`, and the
warning is logged:

```python
if event_type_str is None:
    logger.warning(
        "Could not determine event type for plugin handler '%s' "
        "(class: %s), skipping",
        handler.name,
        handler.__class__.__name__,
    )
    continue
```

Both handlers are skipped. Zero plugin handlers are registered.

**The asymmetry**: `PluginLoader.load_handler` tries `class_name_with_suffix` first
(correctly), but `DaemonController._load_plugins` only tries the plain `class_name`
(incorrectly).

### Why the handlers themselves are not at fault

The class naming convention (`SystemPathsHandler`, `AnsibleEnforcementHandler`)
follows the project standard — the "Handler" suffix is consistent with every built-in
handler in the codebase. The `load_handler` method explicitly endorses this as
"common convention". The handlers are correctly structured; the matching logic in the
controller simply does not account for the suffix.

### Why the changes appear in `git diff`

The uncommitted changes to both handler files add `get_acceptance_tests()` methods
(importing `AcceptanceTest` and `TestType`). These are additions made to satisfy the
`PluginLoader`'s acceptance-test validation warning (lines 119–132 in `loader.py`).
They do not affect the registration failure — that failure occurs regardless of whether
`get_acceptance_tests()` exists. The changes are safe and should be committed once the
registration is fixed and verified.

## Goals

- Fix the event-type matching logic in `DaemonController._load_plugins` so that
  handlers whose class names include the "Handler" suffix are correctly matched to
  their plugin config entries.
- Verify both `SystemPathsHandler` and `AnsibleEnforcementHandler` load and register
  without warnings after the fix.
- Commit the existing uncommitted handler changes (the `get_acceptance_tests` additions)
  alongside or after the fix.
- Add a regression test to the daemon's test suite to prevent the mismatch recurring.

## Non-Goals

- Changing the class naming convention in the handler files.
- Refactoring the broader plugin loading architecture.
- Modifying the `hooks-daemon.yaml` configuration beyond what is strictly necessary.
- Fixing the unrelated `validate_sitemap` / `remind_validator` removals visible in the
  diff (those are separate cleanup items already committed or in progress).

## Context & Background

### How the plugin system works

Plugins are declared in `.claude/hooks-daemon.yaml` under the `plugins:` key:

```yaml
plugins:
  paths: []
  plugins:
    - path: ".claude/hooks/handlers/pre_tool_use/system_paths.py"
      event_type: "pre_tool_use"
      handlers: ["SystemPathsHandler"]
      enabled: true

    - path: ".claude/hooks/handlers/pre_tool_use/ansible_enforcement.py"
      event_type: "pre_tool_use"
      handlers: ["AnsibleEnforcementHandler"]
      enabled: true
```

The `event_type` field is the correct signal for where to register. It is parsed
into a `PluginConfig` model and passed through to `_load_plugins`. The field is
available; it is simply not retrieved because the class-name comparison fails first.

### The working alternative: `ProjectHandlerLoader`

The newer `ProjectHandlerLoader` (in `handlers/project_loader.py`) avoids this
problem entirely by discovering handler classes dynamically — it iterates `dir(module)`
and checks `issubclass(attr, Handler)` rather than comparing class names against a
derived string. This is the more robust approach.

### The `handlers:` list in `PluginConfig`

The YAML config already provides the correct class names in the `handlers:` list
(`["SystemPathsHandler"]`, `["AnsibleEnforcementHandler"]`). These are not currently
used for matching in `_load_plugins` — the matching relies solely on the derived
`expected_class` string. Using the explicit `handlers:` list would be an alternative
(and arguably more correct) fix approach.

## Tasks

### Phase 1: Fix the daemon controller

- [ ] ⬜ **Task 1.1**: Open `controller.py` and locate `_load_plugins` (lines 244–268)
- [ ] ⬜ **Task 1.2**: Fix the class-name comparison to also accept the "Handler"-suffixed form.
  The minimal change is:
  ```python
  expected_class        = PluginLoader.snake_to_pascal(plugin_module)
  expected_class_suffix = f"{expected_class}Handler"

  if handler.__class__.__name__ in (expected_class, expected_class_suffix):
      event_type_str = plugin.event_type
      break
  ```
  An alternative (preferred if the reviewers agree it is cleaner) is to use the
  explicit `plugin.handlers` list when it is populated:
  ```python
  # Use explicit class names from config if provided
  if plugin.handlers:
      if handler.__class__.__name__ in plugin.handlers:
          event_type_str = plugin.event_type
          break
  else:
      # Fall back to derived name matching (with and without "Handler" suffix)
      expected_class = PluginLoader.snake_to_pascal(plugin_module)
      if handler.__class__.__name__ in (expected_class, f"{expected_class}Handler"):
          event_type_str = plugin.event_type
          break
  ```
- [ ] ⬜ **Task 1.3**: Run the daemon's QA suite to confirm no regressions:
  ```bash
  cd /workspace/.claude/hooks-daemon && ./scripts/qa/run_all.sh
  ```

### Phase 2: Add a regression test

- [ ] ⬜ **Task 2.1**: Locate the existing plugin-loading tests in the daemon's test
  suite (search under `.claude/hooks-daemon/tests/` for files referencing
  `PluginLoader` or `_load_plugins`).
- [ ] ⬜ **Task 2.2**: Add a test that:
  - Creates a mock plugin config pointing at a handler file whose class name includes
    the "Handler" suffix (matching the pattern `SomethingHandler`)
  - Calls `_load_plugins` (or equivalent integration path)
  - Asserts that the handler is registered, not skipped
  - Asserts that no "Could not determine event type" warning is logged
- [ ] ⬜ **Task 2.3**: Confirm the test fails before the fix (red) and passes after
  (green).
- [ ] ⬜ **Task 2.4**: Re-run full QA to confirm all tests pass with the regression
  test included.

### Phase 3: Verify live daemon behaviour

- [ ] ⬜ **Task 3.1**: Restart the daemon:
  ```bash
  /workspace/.claude/hooks-daemon/daemon.sh restart
  ```
- [ ] ⬜ **Task 3.2**: Confirm daemon status is RUNNING:
  ```bash
  /workspace/.claude/hooks-daemon/daemon.sh status
  ```
- [ ] ⬜ **Task 3.3**: Confirm the two handlers appear in the loaded handler list and
  that the warnings are absent from the daemon logs.
- [ ] ⬜ **Task 3.4**: Perform a quick live sanity check — attempt a Write to `/etc/`
  and confirm it is blocked by `SystemPathsHandler`; attempt a `dnf install` Bash
  command and confirm it is blocked by `AnsibleEnforcementHandler`.

### Phase 4: Commit the handler changes and the fix together

- [ ] ⬜ **Task 4.1**: Stage all related changes:
  - `.claude/hooks-daemon/src/claude_code_hooks_daemon/daemon/controller.py` (fix)
  - `.claude/hooks-daemon/tests/...` (regression test)
  - `.claude/hooks/handlers/pre_tool_use/system_paths.py` (acceptance tests addition)
  - `.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py` (acceptance tests addition)
- [ ] ⬜ **Task 4.2**: Run `./scripts/qa-all.bash` from the workspace root.
- [ ] ⬜ **Task 4.3**: Commit with a clear message referencing this plan.

## Dependencies

- None. The fix is self-contained within the daemon codebase and the two handler files.

## Technical Decisions

### Decision 1: Minimal fix vs. use-explicit-handlers-list fix

**Context**: There are two viable fixes for the class-name comparison.

**Options Considered**:

1. **Minimal fix** — check both `expected_class` and `expected_class_suffix` in the
   existing comparison. Pros: tiny diff, easy to review, no behaviour change for
   plugins that use the plain class name. Cons: still relies on name derivation as
   primary mechanism; doesn't use the explicit `handlers:` list already in the config.

2. **Explicit-list fix** — when `plugin.handlers` is non-empty, match against those
   class names directly; fall back to derivation only when the list is absent. Pros:
   uses the config's own authoritative data; more robust for unusual naming. Cons:
   slightly larger change; requires understanding `PluginsConfig.handlers` semantics.

**Decision**: Either option is correct. The minimal fix (Option 1) is recommended as
the first PR since it is the smallest change with the least risk. The explicit-list
approach (Option 2) can be adopted in a follow-up if the team prefers it. The plan
implementer should confirm with the project owner before choosing Option 2.

**Date**: 2026-02-17

### Decision 2: This is a daemon bug, not a handler bug

**Context**: The two handler files could be "fixed" by removing the "Handler" suffix
from their class names (`SystemPaths` instead of `SystemPathsHandler`). This would
make the plain-form comparison succeed.

**Decision**: Do not do this. The "Handler" suffix is the established convention for
every built-in handler in the daemon codebase (e.g. `DestructiveGitHandler`,
`SedBlockerHandler`, `AbsolutePathHandler`). The handlers are correct; the controller
matching logic is wrong. Fixing the handlers would paper over the bug and leave the
daemon broken for any future plugin handler that follows the standard naming
convention.

**Date**: 2026-02-17

## Success Criteria

- [ ] Daemon restarts without any "Could not determine event type" warnings for the
  two plugin handlers.
- [ ] Both `prevent-system-file-edits` and `enforce-ansible-deployment` appear in the
  registered handler list for `pre_tool_use`.
- [ ] A regression test exists in the daemon's test suite that would catch this class
  of mismatch if reintroduced.
- [ ] Full QA suite passes (`./scripts/qa/run_all.sh` inside the hooks-daemon project,
  and `./scripts/qa-all.bash` at the workspace root).
- [ ] Live blocking behaviour confirmed for both handlers in a real Claude Code
  session.

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Fix breaks other plugin handlers that use plain class names (no "Handler" suffix) | Medium | Low | The `in (expected_class, expected_class_suffix)` check retains backward compatibility with plain names |
| Daemon QA suite tests are hard to run in CCY container | Low | Medium | Run `bash -n` syntax check as minimum; note in plan if full QA unavailable in container |
| The `handlers:` list in `PluginConfig` has different semantics than assumed | Low | Low | Read `config/models.py` `PluginConfig` definition before implementing Option 2 |

## Notes & Updates

### 2026-02-17

- Investigation completed. Root cause confirmed by tracing the code path through
  `controller._load_plugins` and verifying the `snake_to_pascal` output for both
  file stems.
- `snake_to_pascal("system_paths")` → `"SystemPaths"` (not `"SystemPathsHandler"`)
- `snake_to_pascal("ansible_enforcement")` → `"AnsibleEnforcement"` (not `"AnsibleEnforcementHandler"`)
- The daemon crashed with `ConnectionResetError` when reading logs during investigation,
  consistent with the plugin loading failure causing daemon instability on restart.
- Both handler files have uncommitted changes that add `get_acceptance_tests()` methods.
  These are correct and should be committed as part of this plan's resolution.
- The `hooks-daemon.yaml` diff also removes `validate_sitemap` and `remind_validator`
  entries — these are unrelated to this plan and should be committed separately or
  confirmed as intentional cleanup.
