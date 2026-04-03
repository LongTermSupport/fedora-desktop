# Dead Code Audit — CCY Codebase

**Date**: 2026-04-03
**Method**: Cross-referenced every function definition against all call sites across all 9 CCY files
**Total dead functions**: 20
**Total duplicate definitions**: 2
**Total unnecessary exports**: 12
**Dead files**: 1 (`lib/ui-helpers.bash`)

## Dead Functions

### lib/common.bash (16 dead functions)

| Function | Line | Why Dead | Notes |
|----------|------|----------|-------|
| `print_warning()` | 68 | Only caller is dead `check_claude_token()` | |
| `print_success()` | 72 | Never called | |
| `print_header()` | 76 | Never called | |
| `show_yolo_warning()` | 407 | Never called | |
| `has_claude_token()` | 429 | Only caller is dead `check_claude_token()` | |
| `check_claude_token()` | 447 | Never called | |
| `print_help_section()` | 474 | Never called | |
| `print_option()` | 481 | Never called | |
| `print_example()` | 488 | Never called | |
| `show_spinner()` | 497 | Never called | |
| `confirm()` | 512 | Never called (line 541 uses a local variable, not this function) | |
| `is_in_container()` | 546 | Never called | |
| `is_in_distrobox()` | 551 | Never called | |
| `get_claude_version()` | 556 | Never called | |
| `list_ccy_tokens()` | 594 | Superseded by `list_tokens()` in token-management.bash | Duplicate functionality |
| `get_project_state_dir()` | 655 | Never called — project state now uses `.claude/ccy/` directly | |
| `discover_github_ssh_keys()` | 830 | Superseded by `discover_and_select_ssh_keys()` in ssh-handling.bash | Duplicate functionality |

### lib/docker-health.bash (7 dead functions)

The entire overlay2 migration feature and its helpers are dead code — never wired into startup.

| Function | Line | Why Dead |
|----------|------|----------|
| `check_overlay2_kernel_support()` | 220 | Only called from dead `show_overlay2_migration_tui()` |
| `check_overlay_module()` | 231 | Only called from dead `show_overlay2_migration_tui()` |
| `get_docker_storage_driver()` | 238 | Only called from dead functions |
| `is_using_native_overlay2()` | 246 | Only called from dead functions |
| `has_docker_data()` | 253 | Only called from dead `show_overlay2_migration_tui()` |
| `get_docker_data_size()` | 271 | Only called from dead `show_overlay2_migration_tui()` |
| `show_overlay2_migration_tui()` | 280 | Never called from any file |

Also dead: `MIN_KERNEL_OVERLAY2` constant (line 8) and `version_greater_than` export in common.bash (only consumer was overlay2 code).

### lib/ui-helpers.bash (1 dead function — entire file is dead)

| Function | Line | Why Dead |
|----------|------|----------|
| `show_quick_launch_command()` | 9 | Never called from any file |

This is the only function in the file. The entire file can be deleted.

## Duplicate Function Definitions

| Function | Location 1 (ACTIVE) | Location 2 (DEAD) | Difference |
|----------|---------------------|-------------------|------------|
| `load_launch_config()` | claude-yolo:263 (1-arg) | common.bash:715 (4-arg) | Different signatures — main script version is simpler |
| `save_launch_config()` | claude-yolo:342 (4-arg) | common.bash:799 (7-arg) | Different signatures — main script version is simpler |

The main script definitions shadow the library versions. The library versions are exported but never called.

## Unnecessary Exports

Functions that are only called within their own file — `export -f` adds overhead with no benefit.

### docker-health.bash (5 unnecessary exports)

| Function | Internal Callers |
|----------|-----------------|
| `find_zombie_containers` | `show_zombie_container_tui()`, `check_zombie_containers_startup()` |
| `get_container_stats` | `show_zombie_container_tui()`, `show_container_top()`, `check_project_containers_startup()` |
| `get_container_uptime` | Same as above |
| `find_stale_containers` | `clean_stale_containers_startup()` |
| `show_zombie_container_tui` | `check_zombie_containers_startup()` |

### network-management.bash (7 unnecessary exports)

| Function | Internal Callers |
|----------|-----------------|
| `get_expected_network_name` | `connect_to_network()` |
| `get_network_persistence_file` | `save_network_preference()`, `load_network_preference()` |
| `save_network_preference` | `connect_to_network()` |
| `network_has_running_containers` | `check_and_start_compose_services()`, `_do_compose_start()` |
| `has_compose_files` | `check_and_start_compose_services()`, `offer_compose_start()` |
| `_do_compose_start` | `check_and_start_compose_services()`, `offer_compose_start()` |
| `_compose_already_running` | `offer_compose_start()` |

## Unused Variables

| File | Variable | Line | Assessment |
|------|----------|------|------------|
| claude-yolo | `SAVED_NETWORK` | 632 | Dead — only consumer was dead `show_quick_launch_command()` |
| claude-yolo | `CONFIG_VERSION` | 53 | Partially dead — used by local functions but not library versions |
