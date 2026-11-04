# Bug Report: Plugin Handlers with `Handler` Class Name Suffix Are Silently Skipped

**Project**: claude-code-hooks-daemon
**Repository**: https://github.com/Edmonds-Commerce-Limited/claude-code-hooks-daemon
**Version**: v2.13.0
**Severity**: High — plugin handlers are silently discarded on every daemon start, protection is inactive with no indication to the user

---

## Summary

Any project-level plugin handler whose class name follows the established `Handler` suffix convention (e.g. `SystemPathsHandler`, `AnsibleEnforcementHandler`) is silently skipped when the daemon loads plugins. The daemon starts successfully, reports no error, but the handlers are never registered and never fire.

The root cause is an asymmetry between `PluginLoader.load_handler()` (which correctly handles the `Handler` suffix when loading a class) and `DaemonController._load_plugins()` (which does not account for the `Handler` suffix when matching the loaded class back to its config entry to determine the event type).

---

## Reproduction Steps

1. Create a project-level plugin handler following the documented convention, with class name `MyFeatureHandler` in a file named `my_feature.py`.

2. Register it in `.claude/hooks-daemon.yaml` under the `plugins:` section:
   ```yaml
   plugins:
     - path: .claude/hooks/handlers/pre_tool_use/my_feature.py
       event_type: pre_tool_use
       enabled: true
   ```

3. Restart the daemon:
   ```
   Could not determine event type for plugin handler 'my-feature'
   (class: MyFeatureHandler), skipping
   ```

4. The handler is loaded by `PluginLoader` (no error), instantiated successfully, but then silently discarded. It is not registered and never fires.

---

## Expected Behaviour

Plugin handlers named with the `Handler` suffix (the documented convention in `PluginLoader` itself) should be registered and fire correctly. The daemon should not silently discard loaded handlers.

---

## Actual Behaviour

The daemon logs a warning and skips the handler:
```
Could not determine event type for plugin handler 'prevent-system-file-edits'
(class: SystemPathsHandler), skipping
```

The daemon continues to start and reports `RUNNING`, giving no indication that configured handlers are inactive.

---

## Root Cause

The bug is an asymmetry between `PluginLoader.load_handler()` and `DaemonController._load_plugins()`.

### Step 1: Loading succeeds (correct)

`PluginLoader.load_handler()` in `src/claude_code_hooks_daemon/plugins/loader.py` lines 88–99 correctly handles the `Handler` suffix:

```python
# Find the Handler class (try both with and without "Handler" suffix)
class_name = PluginLoader.snake_to_pascal(handler_name)      # "SystemPaths"
class_name_with_suffix = f"{class_name}Handler"               # "SystemPathsHandler"

# Try with "Handler" suffix first (common convention)
if hasattr(module, class_name_with_suffix):
    handler_class_raw = getattr(module, class_name_with_suffix)  # ✅ found
```

`handler.__class__.__name__` is therefore `"SystemPathsHandler"`.

### Step 2: Event-type matching fails (bug)

`DaemonController._load_plugins()` in `src/claude_code_hooks_daemon/daemon/controller.py` lines 254–260 tries to match the loaded handler back to its config entry:

```python
plugin_module = Path(plugin.path).stem                          # "system_paths"
expected_class = PluginLoader.snake_to_pascal(plugin_module)    # "SystemPaths"

if handler.__class__.__name__ == expected_class:                # "SystemPathsHandler" == "SystemPaths"
    event_type_str = plugin.event_type                          # ❌ never reached
    break
```

`snake_to_pascal("system_paths")` returns `"SystemPaths"` without any suffix. The actual class name is `"SystemPathsHandler"`. The comparison always fails.

After exhausting all plugin config entries, `event_type_str` remains `None` and the warning is emitted (lines 262–269). The handler is discarded.

This is a direct contradiction within the same codebase: `load_handler()` calls the `Handler` suffix the "common convention" and tries it first, while `_load_plugins()` does not account for it at all.

---

## Proposed Fix

**File**: `src/claude_code_hooks_daemon/daemon/controller.py`
**Line**: 258

Change:
```python
if handler.__class__.__name__ == expected_class:
```

To:
```python
if handler.__class__.__name__ in (expected_class, f"{expected_class}Handler"):
```

This mirrors the logic already present in `load_handler()` and makes the matching consistent with the loading.

### Alternative approach

An arguably cleaner fix avoids the class-name matching entirely by using the already-built `path_to_event_type` mapping (constructed on lines 233–237) directly. Since `PluginLoader.load_from_plugins_config()` returns handlers in the same order as the config entries, or since the plugin path is available, the event type could be resolved by path rather than by class name reverse-lookup:

```python
# Instead of reverse-matching by class name, use the path_to_event_type map
# by having PluginLoader return (handler, source_path) tuples, or by
# building the path→event_type map and using handler.name to resolve.
```

This would be a more robust solution but requires a small interface change to `PluginLoader`.

---

## Impact

- All plugin handlers following the `Handler` suffix naming convention fail silently.
- The `Handler` suffix is described as "common convention" in `loader.py` and is used by every built-in handler in the project (`DestructiveGitHandler`, `SedBlockerHandler`, `AbsolutePathHandler`, etc.).
- There is no error visible to the user beyond the startup warning — the daemon reports `RUNNING` and appears healthy.
- The affected handlers provide no protection. In our case, `SystemPathsHandler` (blocks direct editing of deployed system config files) and `AnsibleEnforcementHandler` (blocks direct package/service management commands) were both silently inactive.

---

## Workaround

Rename the handler class to omit the `Handler` suffix:

```python
# Workaround: name class without Handler suffix
class SystemPaths(Handler):   # instead of SystemPathsHandler
    ...
```

This is inconsistent with all other handlers in the project and the stated convention in `loader.py`, but it does work around the bug.

---

## Environment

- **Daemon version**: v2.13.0
- **Python**: 3.11 (CCY container), 3.12 (host)
- **OS**: Fedora 42 (host), container (podman)
- **Config**: `plugins:` section in `.claude/hooks-daemon.yaml` (not `project_handlers:`)

---

## Relevant Code References

| File | Lines | Description |
|---|---|---|
| `src/claude_code_hooks_daemon/plugins/loader.py` | 88–99 | `load_handler()` — correctly tries both `ClassName` and `ClassNameHandler` |
| `src/claude_code_hooks_daemon/daemon/controller.py` | 254–260 | `_load_plugins()` — only checks `ClassName`, misses `ClassNameHandler` |
| `src/claude_code_hooks_daemon/daemon/controller.py` | 262–269 | Warning logged, handler skipped |

---

*Reported: 2026-02-17*
