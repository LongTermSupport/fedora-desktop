# Code Quality Audit — CCY Codebase

**Date**: 2026-04-03
**Method**: Manual review of all 9 CCY files (~6,000 lines)
**Total findings**: 42 (6 High, 10 Medium, 16 Low, 10 cosmetic/excluded)

## High Severity (6)

### H1: Double-sourcing of library files
- **File**: common.bash + claude-yolo
- **Lines**: common.bash:49-61, claude-yolo:25-32
- **Description**: `common.bash` sources all other libraries, then `claude-yolo` also sources them all after sourcing `common.bash`. Every library is loaded twice per invocation. Functions and variables are defined twice.
- **Fix**: Remove one of the two sourcing points.

### H2: Library functions call `exit` instead of `return` — token-management.bash
- **File**: token-management.bash
- **Lines**: 133, 200, 276, 389, 427, 431, 433
- **Description**: `create_token()` and `select_token()` use `exit 0`/`exit 1` which terminates the entire shell. As library functions, they should `return` to allow callers to handle cleanup. Any error during token creation kills the process without running cleanup traps.
- **Fix**: Change `exit` to `return`, update callers to check return values.

### H3: Library functions call `exit` instead of `return` — ssh-handling.bash
- **File**: ssh-handling.bash
- **Lines**: 124, 136, 170, 182, 198
- **Description**: `build_ssh_mounts_and_validate()` uses `exit 1` at multiple points.
- **Fix**: Change to `return 1`.

### H4: Library functions call `exit` instead of `return` — network-management.bash
- **File**: network-management.bash
- **Lines**: 125, 246, 311, 411, 415
- **Description**: `connect_to_network()` uses `exit` at multiple points.
- **Fix**: Change to `return`.

### H5: Monolithic network section in claude-yolo
- **File**: claude-yolo
- **Lines**: ~1459-2158 (~700 lines)
- **Description**: The network detection, cross-engine mismatch handling, and compose service management spans 700 lines of top-level script with 4+ levels of nesting. Indentation is inconsistent, making it very hard to follow control flow.
- **Fix**: Not in scope for this plan (too large). Flagged for future decomposition into network-management.bash.

### H6: Duplicate `load_launch_config`/`save_launch_config` definitions
- **File**: claude-yolo + common.bash
- **Description**: Both files define these functions with different signatures. The library versions are dead but confusing for maintenance.
- **Fix**: Remove library versions (covered in dead code removal Phase 1).

## Medium Severity (10)

### M1: Unquoted word-splitting variables in `container_cmd run`
- **File**: claude-yolo
- **Lines**: 2394, 2396
- **Description**: `$DOCKER_FLAGS` and `$NETWORK_FLAG` rely on word splitting. Fragile if values contain spaces.
- **Fix**: Convert to arrays.

### M2: Duplicate compose-file scanning logic
- **File**: claude-yolo
- **Lines**: ~1726-1730, ~2041-2046 + network-management.bash `has_compose_files()`
- **Description**: Same 6-filename-pattern scan repeated 3 times in main script. Library version exists but isn't used.
- **Fix**: Consolidate to use `has_compose_files()` from network-management.bash. Defer to future network refactor.

### M3: Duplicate network re-scanning logic
- **File**: claude-yolo
- **Lines**: ~1762-1770, ~2097-2104, ~1597-1605
- **Description**: Network scanning (skip bridge/host/none/podman, filter by project) copy-pasted 3 times.
- **Fix**: Extract to function. Defer to future network refactor.

### M4: `|| true` suppressing real errors
- **File**: claude-yolo
- **Lines**: 1077, 1701, 1710, 2431
- **Description**: Container operations with `2>/dev/null || true` hide legitimate failures.
- **Fix**: Use `if/else` pattern to capture and log errors.

### M5: Hardcoded `docker` in cross-engine mismatch handler
- **File**: claude-yolo
- **Lines**: 1698-1713
- **Description**: Uses `docker` directly instead of `container_cmd`. Line 1710 `docker ps -q | xargs -r docker stop` stops ALL docker containers, not just project ones — overly aggressive.
- **Fix**: Scope cleanup to project containers only.

### M6: Temp file without `mktemp` — token-management.bash
- **File**: token-management.bash
- **Lines**: 211
- **Description**: `"/tmp/${tool_name}-token-setup-$$"` uses predictable PID-based path. No trap for cleanup on signals.
- **Fix**: Use `mktemp`, add signal trap.

### M7: Temp file without `mktemp` — claude-yolo
- **File**: claude-yolo
- **Lines**: 1354
- **Description**: `CONFIG_TEMP="/tmp/claude-yolo-$$"` — same issue.
- **Fix**: Use `mktemp`.

### M8: Leaked variables in ssh-handling.bash
- **File**: ssh-handling.bash
- **Lines**: 143-146
- **Description**: `alias`, `token_func`, `key_basename` not declared `local` — leak into global scope.
- **Fix**: Add `local` declarations.

### M9: `systemctl` error suppression in overlay2 migration
- **File**: docker-health.bash
- **Lines**: 372, 441
- **Description**: `systemctl --user stop ... 2>/dev/null || true` hides errors before destructive operations.
- **Fix**: Will be removed with dead code (overlay2 migration is dead).

### M10: Silent pipeline failure in entrypoint.sh
- **File**: entrypoint.sh
- **Lines**: 94-96
- **Description**: `curl | jq | sed >> known_hosts` — each stage has `2>/dev/null`, hiding where failures occur. If GitHub API returns invalid JSON, silently produces incorrect entries.
- **Fix**: Add error checking or `set -o pipefail` for the pipeline.

## Low Severity (16)

| ID | File | Description |
|----|------|-------------|
| L1 | claude-yolo | `CUSTOM_DOCKER_MODE` and `TOP_MODE` not initialised to `false` (inconsistent with other flags) |
| L2 | claude-yolo | `DOCKER_BUILD_FAILED` uses `${:-false}` default instead of initialisation |
| L3 | claude-yolo | `COMPOSE_ALREADY_HANDLED` used before being set |
| L4 | common.bash | `is_git_repo()` uses `[ -d .git ]` instead of `git rev-parse` (fails for worktrees) |
| L5 | common.bash | `check_ccy_gitignore_safety` uses `sed` for newline removal |
| L6 | common.bash | `show_spinner` hardcodes spinner width to 10 |
| L7 | docker-health.bash | `show_container_top` uses recursion for refresh (stack growth) |
| L8 | docker-health.bash | `export -f` for all functions (unnecessary for sourced libraries) |
| L9 | docker-health.bash | `grep | wc -l` pattern (fragile — should use `grep -c`) |
| L10 | ssh-handling.bash | `grep -oP` is a GNU extension (Fedora-only so acceptable) |
| L11 | network-management.bash | Repeated `network ls | grep` instead of `network inspect` |
| L12 | network-management.bash | `$compose_cmd` unquoted (intentional word splitting, should be array) |
| L13 | dockerfile-custom.bash | `get_dockerfile_creation_prompt` is 356 lines (just a heredoc) |
| L14 | dockerfile-custom.bash | `head | grep | sed` pipeline (could be single `sed -n`) |
| L15 | entrypoint.sh | `eval "$(ssh-agent -s)"` failure not checked |
| L16 | All files | Inconsistent error output — mix of `print_error` and raw `echo >&2` |

## Cross-File Issues

### All library files use `export -f` unnecessarily
All libraries are sourced into the main script's shell. `export -f` is only needed for child shell processes, which doesn't apply here. Adds overhead and bash version dependency. Low severity but affects all files.

### No `set -o pipefail` anywhere
Neither the main script nor any library uses `pipefail`. Pipelines can fail in early stages while reporting success from the last stage. The main script has `set -e` but without `pipefail` this doesn't catch pipeline failures.
