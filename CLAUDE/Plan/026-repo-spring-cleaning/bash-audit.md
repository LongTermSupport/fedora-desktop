# Bash Script Audit (Non-CCY) — Full Repository

**Date**: 2026-04-03
**Method**: shellcheck -x + manual review of all non-CCY bash scripts
**Scope**: All bash scripts excluding `files/var/local/claude-yolo/` (covered by Plan 025)
**Total findings**: 18 (2 High, 10 Medium, 6 Low)
**Clean files**: All 6 `qa-*.bash` scripts, `run.bash`

## High Severity (2)

### H1: Missing `set -e` in `scripts/nvidia-status.bash`
- **Lines**: Missing from top of file
- **Description**: No fail-fast. Script continues executing even if commands fail, masking hardware status check failures.
- **Fix**: Add `set -e` after shebang.

### H2: Missing `set -e` in `scripts/check-displaylink-status.sh`
- **Lines**: Missing from top of file
- **Description**: Same issue — no fail-fast enforcement.
- **Fix**: Add `set -e` after shebang.

## Medium Severity (10)

### M1: Unused variable `PACKAGES_OK` — nvidia-status.bash
- **Lines**: 112
- **Description**: Set but never read. Dead code.
- **Fix**: Remove or use in final status summary.

### M2: Unused variable `VAAPI_OK` — nvidia-status.bash
- **Lines**: 317
- **Description**: Set but never read. Dead code.
- **Fix**: Remove or use in final status summary.

### M3: Unused variable `DKMS_OK` — check-displaylink-status.sh
- **Lines**: 99, 102, 106
- **Description**: Set at 3 locations but never checked in conditional logic.
- **Fix**: Remove or use in final status summary.

### M4: Unquoted variables in nvidia-smi output — nvidia-status.bash
- **Lines**: 192-194 (SC2086)
- **Description**: `$(echo $gpu_name | xargs)` — unquoted `$gpu_name`, `$driver_ver`, `$memory`.
- **Fix**: Quote variables: `"$gpu_name"`, `"$driver_ver"`, `"$memory"`.

### M5: Glob pattern with `-d` test — check-displaylink-status.sh
- **Lines**: 83 (SC2144)
- **Description**: `[ -d /usr/src/evdi-* ]` — `-d` doesn't work with glob patterns.
- **Fix**: Use loop: `for dir in /usr/src/evdi-*; do [ -d "$dir" ] && ...; done`

### M6: Problematic quoting in docker args — zz_lts-fedora-desktop.bash
- **Lines**: 98 (SC2027)
- **Description**: `dp="$dp -v "$PWD":/usr/src/app"` — nested quotes cause parsing issues.
- **Fix**: Use proper quote escaping.

### M7: Unquoted command substitution — zz_lts-fedora-desktop.bash
- **Lines**: 100 (SC2086, SC2046)
- **Description**: `docker run $dp $(docker-node-image) "$@"` — word splitting risk.
- **Fix**: Quote: `docker run "$dp" "$(docker-node-image)" "$@"`

### M8: Function references `$1` never passed — zz_lts-fedora-desktop.bash
- **Lines**: 78-89, 91 (SC2120)
- **Description**: `docker-node-version()` references `$1` but called without arguments from `docker-node-image()`.
- **Fix**: Pass argument explicitly or redesign.

### M9: `ls` instead of `find` — check-displaylink-status.sh
- **Lines**: 84, 154, 161 (SC2012)
- **Description**: `ls /dev/dri/card* 2>/dev/null | wc -l` — fragile with non-alphanumeric filenames.
- **Fix**: Use `find` or glob patterns.

### M10: `ls` instead of `find` — gnome-shell-extract-js.bash
- **Lines**: 95 (SC2012)
- **Description**: Same pattern as M9.
- **Fix**: Use `find` or `printf '%s\n' /path/*`.

## Low Severity (6)

| ID | File | Description |
|----|------|-------------|
| L1 | nvidia-status.bash:189 | Indirect `$?` check instead of direct command test (SC2181) |
| L2 | zz_lts-fedora-desktop.bash:44-55 | 12-line commented-out dead code block (old prompt config) |
| L3 | zz_lts-fedora-desktop.bash | Inconsistent conditional syntax (`[[ ]]` vs `[ ]`) |
| L4 | zz_lts-fedora-desktop.bash:56 | SC1091 source of runtime-deployed file (not a real issue) |
| L5 | check-displaylink-status.sh | Inconsistent quoting style |
| L6 | All status scripts | No `--help` or usage information |

## Summary by File

| File | Issues | Severity |
|------|--------|----------|
| `scripts/nvidia-status.bash` | 4 | HIGH, MEDIUM |
| `scripts/check-displaylink-status.sh` | 5 | HIGH, MEDIUM |
| `files/etc/profile.d/zz_lts-fedora-desktop.bash` | 5 | MEDIUM, LOW |
| `extensions/scripts/gnome-shell-extract-js.bash` | 1 | MEDIUM |
| `scripts/qa-*.bash` (6 files) | 0 | Clean |
| `run.bash` | 0 | Clean |

## Security Check

- No hardcoded API keys, passwords, tokens, or credentials detected
- No hardcoded user-specific paths
- **PASS**
