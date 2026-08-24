# Shellcheck Audit — CCY Codebase

**Date**: 2026-04-03
**Tool**: shellcheck -x
**Total findings**: 63 (0 errors, 34 warnings, 29 info)
**Files with issues**: 2 of 9
**Clean files**: common.bash, token-management.bash, ssh-handling.bash, network-management.bash, dockerfile-custom.bash, ui-helpers.bash, entrypoint.sh

## Summary by SC Code

| SC Code | Count | Severity | Description | Files |
|---------|-------|----------|-------------|-------|
| SC2155 | 29 | warning | `local var=$(cmd)` masks return value | docker-health.bash (27), claude-yolo (2) |
| SC2162 | 26 | info | `read` without `-r` mangles backslashes | claude-yolo (18), docker-health.bash (8) |
| SC2034 | 3 | warning | Variable appears unused | claude-yolo |
| SC2086 | 2 | info | Unquoted variable — word splitting risk | claude-yolo |
| SC1090 | 2 | warning | Non-constant source path | claude-yolo |
| SC1091 | 1 | info | Source file not found for static analysis | claude-yolo |

## File: claude-yolo (28 findings)

### SC2162 — `read` without `-r` (18 instances)

Lines: 78, 503, 541, 653, 753, 776, 797, 879, 896, 1422, 1513, 1670, 1689, 1745, 1881, 1920, 1955, 1988, 2082

Fix: Change `read -p` to `read -rp` at all locations.

### SC2034 — Unused variables (3 instances)

| Line | Variable | Assessment |
|------|----------|------------|
| 632 | `SAVED_NETWORK` | Genuinely dead — only consumer is dead `show_quick_launch_command()` |
| 678 | `CONFIG_LOADED` | Needs investigation — may be consumed by sourced library |
| 2033 | `PODMAN_DEFAULT_DETECTED` | Needs investigation — may be checked later in the script |

### SC2155 — Declare and assign separately (2 instances)

| Line | Code |
|------|------|
| 1027 | `local container_version=$(container_cmd run ...)` |
| 1030 | `local latest_version=$(timeout 5 curl ...)` |

### SC2086 — Unquoted variables (2 instances)

| Line | Variable | Notes |
|------|----------|-------|
| 1701 | `$containers` | Unquoted in `docker stop $containers` |
| 2396 | `$NETWORK_FLAG` | Intentional word splitting — should convert to array |

### SC1090/SC1091 — Source following (3 instances)

Lines 17, 27, 273. Not actionable — dynamic source paths. Can add `# shellcheck source=` directives if desired.

## File: docker-health.bash (35 findings)

### SC2155 — Declare and assign separately (27 instances)

Lines: 22, 32, 84, 138, 139, 140, 141, 221, 247, 259, 260, 281, 291, 340, 341, 342, 362, 393, 433, 458, 596, 597, 598, 599, 603, 674, 721, 722, 723

All follow the same pattern: `local var=$(some_command)`. Fix: split into `local var` then `var=$(some_command)`.

**Note**: Many of these (lines 247, 259, 260, 281, 291, 340, 341, 342, 362, 393, 433, 458) are in the overlay2 migration code which is dead and will be removed in Phase 1. That eliminates ~12 of these 27 warnings automatically.

### SC2162 — `read` without `-r` (8 instances)

Lines: 158, 178, 362, 433, 625, 739, 762

**Note**: Lines 362 and 433 are in dead overlay2 code — will be removed in Phase 1.
