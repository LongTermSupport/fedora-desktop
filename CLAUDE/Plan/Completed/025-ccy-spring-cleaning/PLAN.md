# Plan 025: CCY Codebase Spring Cleaning

**Status**: ✅ Complete
**Created**: 2026-04-03
**Type**: Refactoring
**Priority**: Medium

## Overview

The CCY (Claude YOLO) codebase has accumulated significant technical debt across its 9 bash files (~6,000 lines). A comprehensive audit identified 63 shellcheck warnings, 20 dead functions, 2 duplicate function definitions, 12 unnecessary exports, and 42 code quality issues including 6 high-severity problems.

The worst offenders are `docker-health.bash` (27 shellcheck warnings, 7 dead functions), `common.bash` (16 dead functions, double-sourcing bug), and the main `claude-yolo` script (monolithic 700-line network section, duplicate code patterns).

This plan addresses all findings systematically, ordered by impact and risk.

## Goals

- Eliminate all 63 shellcheck warnings across 2 affected files
- Remove all 20 dead functions and 1 dead file (`ui-helpers.bash`)
- Fix 6 high-severity code quality issues
- Address medium-severity issues where practical
- Maintain identical runtime behaviour throughout

## Non-Goals

- Adding new features or changing UX
- Refactoring the network section into a separate library (too large for this plan)
- Fixing low-severity cosmetic issues unless they're in files already being edited
- Changing entrypoint.sh patterns (different execution context)

## Context & Background

The CCY system consists of:

| File | Lines | Role |
|------|-------|------|
| `claude-yolo` | ~2,468 | Main script — arg parsing, container lifecycle |
| `lib/common.bash` | ~1,028 | Core utilities, sourcing hub |
| `lib/docker-health.bash` | ~810 | Container health, zombie/stale detection, overlay2 migration |
| `lib/token-management.bash` | ~590 | OAuth/API token management |
| `lib/ssh-handling.bash` | ~206 | SSH key discovery and mounting |
| `lib/network-management.bash` | ~754 | Docker/Podman network management |
| `lib/dockerfile-custom.bash` | ~777 | Custom Dockerfile creation wizard |
| `lib/ui-helpers.bash` | ~45 | Single dead function — entire file is dead code |
| `entrypoint.sh` | ~162 | Container init script |

Key finding: `common.bash` sources all other libraries at lines 49-61, and `claude-yolo` ALSO sources them at lines 25-32 after sourcing `common.bash`. Every library is loaded twice per invocation.

## Tasks

### Phase 1: Dead Code Removal

Remove dead functions, dead file, and unnecessary exports. Lowest risk — removing unused code cannot break anything.

- [ ] ⬜ **1.1: Remove dead file `ui-helpers.bash`**
  - [ ] ⬜ Delete `lib/ui-helpers.bash` (only contains dead `show_quick_launch_command()`)
  - [ ] ⬜ Remove `ui-helpers` from the source loop in `claude-yolo` line 25
  - [ ] ⬜ Remove sourcing in `common.bash` if present
  - [ ] ⬜ Remove `SAVED_NETWORK` variable from `claude-yolo` (only consumer was dead function)

- [ ] ⬜ **1.2: Remove dead functions from `common.bash`** (16 functions)
  - [ ] ⬜ Remove `show_yolo_warning()` (line 407) + export
  - [ ] ⬜ Remove `has_claude_token()` (line 429) + export
  - [ ] ⬜ Remove `check_claude_token()` (line 447) + export
  - [ ] ⬜ Remove `print_help_section()` (line 474) + export
  - [ ] ⬜ Remove `print_option()` (line 481) + export
  - [ ] ⬜ Remove `print_example()` (line 488) + export
  - [ ] ⬜ Remove `show_spinner()` (line 497) + export
  - [ ] ⬜ Remove `confirm()` (line 512) + export
  - [ ] ⬜ Remove `is_in_container()` (line 546) + export
  - [ ] ⬜ Remove `is_in_distrobox()` (line 551) + export
  - [ ] ⬜ Remove `get_claude_version()` (line 556) + export
  - [ ] ⬜ Remove `list_ccy_tokens()` (line 594) + export (duplicates `list_tokens` in token-management.bash)
  - [ ] ⬜ Remove `get_project_state_dir()` (line 655) + export
  - [ ] ⬜ Remove `discover_github_ssh_keys()` (line 830) + export (duplicates ssh-handling.bash)
  - [ ] ⬜ Remove `print_success()` (line 72) + export
  - [ ] ⬜ Remove `print_header()` (line 76) + export
  - [ ] ⬜ Remove `print_warning()` (line 68) + export (only caller was dead `check_claude_token`)

- [ ] ⬜ **1.3: Remove duplicate function definitions**
  - [ ] ⬜ Remove dead `load_launch_config()` from `common.bash` (line 715, 4-arg version) + export
  - [ ] ⬜ Remove dead `save_launch_config()` from `common.bash` (line 799, 7-arg version) + export

- [ ] ⬜ **1.4: Remove dead overlay2 migration suite from `docker-health.bash`** (7 functions)
  - [ ] ⬜ Remove `show_overlay2_migration_tui()` (line 280) + export
  - [ ] ⬜ Remove `is_using_native_overlay2()` (line 246) + export
  - [ ] ⬜ Remove `check_overlay2_kernel_support()` (only caller was dead)
  - [ ] ⬜ Remove `check_overlay_module()` (only caller was dead)
  - [ ] ⬜ Remove `get_docker_storage_driver()` (only callers were dead) + export
  - [ ] ⬜ Remove `has_docker_data()` (only caller was dead) + export
  - [ ] ⬜ Remove `get_docker_data_size()` (only caller was dead) + export
  - [ ] ⬜ Remove `MIN_KERNEL_OVERLAY2` constant (only consumer was dead)
  - [ ] ⬜ Remove `version_greater_than` export from common.bash (only caller was dead overlay2 code)

- [ ] ⬜ **1.5: Remove unnecessary exports** (12 functions)
  - [ ] ⬜ Remove `export -f` for `find_zombie_containers` (internal to docker-health.bash)
  - [ ] ⬜ Remove `export -f` for `get_container_stats` (internal)
  - [ ] ⬜ Remove `export -f` for `get_container_uptime` (internal)
  - [ ] ⬜ Remove `export -f` for `find_stale_containers` (internal)
  - [ ] ⬜ Remove `export -f` for `show_zombie_container_tui` (internal)
  - [ ] ⬜ Remove `export -f` for network-management.bash internals: `get_expected_network_name`, `get_network_persistence_file`, `save_network_preference`, `network_has_running_containers`, `has_compose_files`, `_do_compose_start`, `_compose_already_running`

- [ ] ⬜ **1.6: Verify and commit Phase 1**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit: "refactor(ccy): remove dead code — 20 functions, 1 file, 12 exports"

### Phase 2: Fix Double-Sourcing

High-severity bug — every library file is loaded twice per invocation.

- [ ] ⬜ **2.1: Eliminate double-sourcing**
  - [ ] ⬜ Remove library sourcing from `common.bash` lines 49-61 (let `claude-yolo` be the single source point)
  - [ ] ⬜ OR remove the source loop from `claude-yolo` lines 25-32 and rely on `common.bash` to source everything
  - [ ] ⬜ Decide which approach based on whether any library depends on `common.bash` functions being available during sourcing
  - [ ] ⬜ Test that all functions remain available after the change

- [ ] ⬜ **2.2: Verify and commit Phase 2**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit

### Phase 3: Shellcheck Fixes — SC2155 (29 warnings)

All in `docker-health.bash` (27) and `claude-yolo` (2). Mechanical fix: split `local var=$(cmd)` into `local var` then `var=$(cmd)`.

- [ ] ⬜ **3.1: Fix SC2155 in `docker-health.bash`** (27 instances)
  - [ ] ⬜ Fix all `local var=$(cmd)` patterns — split declaration and assignment
  - [ ] ⬜ Verify no behaviour change

- [ ] ⬜ **3.2: Fix SC2155 in `claude-yolo`** (2 instances, lines 1027, 1030)
  - [ ] ⬜ Split declaration and assignment

- [ ] ⬜ **3.3: Verify and commit Phase 3**
  - [ ] ⬜ Run shellcheck on both files — confirm 0 SC2155 warnings
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit

### Phase 4: Shellcheck Fixes — SC2162 (26 warnings)

All `read -p` calls missing `-r` flag. Mechanical fix: change `read -p` to `read -rp`.

- [ ] ⬜ **4.1: Fix SC2162 in `claude-yolo`** (18 instances)
  - [ ] ⬜ Add `-r` flag to all `read -p` calls

- [ ] ⬜ **4.2: Fix SC2162 in `docker-health.bash`** (8 instances)
  - [ ] ⬜ Add `-r` flag to all `read -p` calls

- [ ] ⬜ **4.3: Verify and commit Phase 4**
  - [ ] ⬜ Run shellcheck — confirm 0 SC2162 warnings
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit

### Phase 5: High-Severity Code Quality Fixes

- [ ] ⬜ **5.1: Fix `exit` vs `return` in library functions**
  - [ ] ⬜ `token-management.bash`: Change `exit 0`/`exit 1` to `return 0`/`return 1` in `create_token()` and `select_token()`
  - [ ] ⬜ `ssh-handling.bash`: Change `exit 1` to `return 1` in `build_ssh_mounts_and_validate()`
  - [ ] ⬜ `network-management.bash`: Change `exit` to `return` in `connect_to_network()`
  - [ ] ⬜ Update callers to check return values where needed
  - [ ] ⬜ Verify cleanup traps still run correctly

- [ ] ⬜ **5.2: Fix unquoted variables in `container_cmd run`**
  - [ ] ⬜ Convert `DOCKER_FLAGS` from string to array
  - [ ] ⬜ Convert `NETWORK_FLAG` from string to array
  - [ ] ⬜ Update `container_cmd run` to use `"${DOCKER_FLAGS[@]}"` and `"${NETWORK_FLAG[@]}"`

- [ ] ⬜ **5.3: Initialise all mode flags consistently**
  - [ ] ⬜ Add `CUSTOM_DOCKER_MODE=false` and `TOP_MODE=false` to the variable declaration block
  - [ ] ⬜ Add `DOCKER_BUILD_FAILED=false` initialisation
  - [ ] ⬜ Add `COMPOSE_ALREADY_HANDLED=false` initialisation

- [ ] ⬜ **5.4: Verify and commit Phase 5**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit

### Phase 6: Medium-Severity Code Quality Fixes

- [ ] ⬜ **6.1: Fix temp file security in `token-management.bash`**
  - [ ] ⬜ Replace `"/tmp/${tool_name}-token-setup-$$"` with `mktemp`
  - [ ] ⬜ Add trap for temp file cleanup on SIGINT/SIGTERM
  - [ ] ⬜ Do the same for `CONFIG_TEMP` in `claude-yolo`

- [ ] ⬜ **6.2: Fix leaked variables in `ssh-handling.bash`**
  - [ ] ⬜ Add `local` to `alias`, `token_func`, `key_basename` in `build_ssh_mounts_and_validate()`

- [ ] ⬜ **6.3: Fix recursive `show_container_top` refresh**
  - [ ] ⬜ Replace recursive call with a `while true` loop wrapping the function body

- [ ] ⬜ **6.4: Verify and commit Phase 6**
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit

### Phase 7: Remaining Shellcheck (SC2034, SC2086)

- [ ] ⬜ **7.1: Address SC2034 (unused variables)**
  - [ ] ⬜ Investigate `CONFIG_LOADED` and `PODMAN_DEFAULT_DETECTED` — add shellcheck directive if false positive, remove if genuinely unused

- [ ] ⬜ **7.2: Fix SC2086 (unquoted variables)**
  - [ ] ⬜ Quote `$containers` on line 1701 (or already fixed if using arrays from Phase 5)
  - [ ] ⬜ Verify `$NETWORK_FLAG` quoting (handled by Phase 5 array conversion)

- [ ] ⬜ **7.3: Verify and commit Phase 7**
  - [ ] ⬜ Run shellcheck on all files — confirm zero actionable warnings
  - [ ] ⬜ Run `./scripts/qa-all.bash`
  - [ ] ⬜ Bump CCY_VERSION
  - [ ] ⬜ Commit

## Dependencies

- None — this is self-contained refactoring work

## Technical Decisions

### Decision 1: Phase ordering — dead code first
**Context**: Dead code removal is the lowest-risk change and makes all subsequent phases easier (fewer lines to fix shellcheck in, cleaner grep results).
**Decision**: Phase 1 removes dead code before any other changes.

### Decision 2: Double-sourcing fix approach
**Context**: Libraries are sourced from both `common.bash` and `claude-yolo`. Need to pick one source point.
**Options**:
1. Remove from `common.bash` — `claude-yolo` controls sourcing explicitly
2. Remove from `claude-yolo` — `common.bash` is the single entry point

**Decision**: Deferred to implementation — need to check if any library references `common.bash` functions during its own sourcing (which would require common.bash to be loaded first).

### Decision 3: `exit` vs `return` — caller impact
**Context**: Changing `exit` to `return` in library functions means callers must now check return values. Currently, `exit` terminates everything so callers never see the error.
**Decision**: Each caller site needs review to ensure proper error handling after the change. This is the right fix but requires careful testing.

## Success Criteria

- [ ] Zero shellcheck warnings on `claude-yolo` and `docker-health.bash` (excluding SC1090/SC1091 source-following)
- [ ] Zero dead functions across all CCY files
- [ ] No duplicate function definitions
- [ ] All library functions use `return` instead of `exit`
- [ ] No double-sourcing of library files
- [ ] `./scripts/qa-all.bash` passes
- [ ] All existing CCY functionality works identically

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Removing "dead" function that's actually used via eval/indirect call | High | Low | Grep for function name as string literal before removing |
| `exit` to `return` change breaks error handling flow | Medium | Medium | Test each function's error paths manually |
| Double-sourcing fix breaks library load order | Medium | Low | Check inter-library dependencies before removing |
| Shellcheck fixes introduce subtle behaviour changes | Low | Low | SC2155 and SC2162 fixes are mechanical with no logic change |

## Notes & Updates

### 2026-04-03
- Plan created based on comprehensive audit by three research agents
- Shellcheck: 63 findings (29 SC2155, 26 SC2162, 3 SC2034, 2 SC2086, 3 SC1090/1091)
- Dead code: 20 functions, 1 dead file, 2 duplicate definitions, 12 unnecessary exports
- Code quality: 42 issues (6 High, 10 Medium, 16 Low + 10 cosmetic)
- Supporting analysis: [shellcheck-audit.md](shellcheck-audit.md), [dead-code-audit.md](dead-code-audit.md), [code-quality-audit.md](code-quality-audit.md)
