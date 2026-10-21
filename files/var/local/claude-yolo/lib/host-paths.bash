#!/bin/bash
# The host directories the launcher reads that a test sandbox must be able to replace: the
# image's build context, the GPU render-node directory and the zone database. Each has its
# default and an override for the differential harness (src-go/internal/ccylaunch/difftest),
# and an override is checked as soon as this library is sourced: one that names nothing fails
# at start, not later as a missing Dockerfile or, for the GPU, as silently no GPU.

# ccy_host_dir <variable> <override-variable> <default> [absent-ok] — set <variable> to the
# directory to use.
#   The override unset or empty: the default, unchecked (a host may have no /dev/dri).
#   Set: it must be an absolute path (a relative one depends on where ccy was started), and
#   is resolved (symlinks and ..), and the resolved path is what is checked and stored. It
#   must be the default, or a directory outside the system trees: /, /proc, /sys and /dev
#   would hand a build or a device mount the whole tree or what is not a GPU. It must be a
#   directory; with absent-ok its parent must be one instead: the GPU directory's absence is
#   an answer (no GPU), a missing parent is a wrong path.
ccy_host_dir() {
    local target="${1:?ccy_host_dir requires the variable to set}"
    local name="${2:?ccy_host_dir requires the override variable}"
    local default="${3:?ccy_host_dir requires a default}" absent_ok="${4-}"
    local dir="${!name-}" checked resolved
    if [ -z "$dir" ]; then
        printf -v "$target" '%s' "$default"
        return 0
    fi
    if [[ "$dir" != /* ]]; then
        print_error "$name=$dir is a relative path; give an absolute one, or unset $name to use $default."
        return 1
    fi
    resolved="$(realpath -m -- "$dir")" || return 1
    case "$resolved" in
    "$default") ;;
    / | /proc | /proc/* | /sys | /sys/* | /dev | /dev/*)
        print_error "$name=$dir resolves to $resolved, in a system tree (/, /proc, /sys, /dev) ccy will not build from or mount. Unset $name to use $default."
        return 1
        ;;
    esac
    checked="$resolved"
    if [ -n "$absent_ok" ]; then
        checked="$(dirname -- "$resolved")"
    fi
    if [ ! -d "$checked" ]; then
        print_error "$name=$dir: $checked is not a directory. Unset $name to use $default."
        return 1
    fi
    printf -v "$target" '%s' "$resolved"
}

ccy_host_dir DOCKERFILE_DIR CCY_DOCKERFILE_DIR /opt/claude-yolo || exit 1
ccy_host_dir GPU_DEVICE_DIR CCY_GPU_DEVICE_DIR /dev/dri absent-ok || exit 1
ccy_host_dir ZONEINFO_DIR CCY_ZONEINFO_DIR /usr/share/zoneinfo || exit 1
