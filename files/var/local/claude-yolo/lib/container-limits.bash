#!/bin/bash
# The limits every container ccy runs is started with, passed explicitly on each run and never
# left to the engine's default. Why, and the choice of defaults: docs/ccy.md, "Container limits".
# The rule that holds every ccy run to them: ccy-container-default-pids-limit (CLAUDE/QA.md,
# .semgrep/bash-conventions.yml); the Go copy of the bounds: src-go/internal/ccylimits.
#
# CCY_PIDS_LIMIT and CCY_MEMORY_MAX on the host override the pids and memory limits. Requires
# print_error (common-pure.bash).

CCY_PIDS_LIMIT_DEFAULT=16384
CCY_PIDS_LIMIT_MIN=256
CCY_PIDS_LIMIT_MAX=32768
CCY_MEMORY_DESKTOP_MIB=16384
CCY_MEMORY_FLOOR_MIB=4096
CCY_MEMORY_SOFT_GAP_MIB=2048
CCY_AGENTS_SLICE=agents.slice

# ccy_container_limits — settle CCY_PIDS_LIMIT (the host's value, else the default) and
# CCY_MEMORY_MAX (ccy_memory_limit), check the slice is installed, and fill CCY_LIMIT_ARGS, the
# limit set every run passes as "${CCY_LIMIT_ARGS[@]}". 1 for a value refused or a missing slice.
# The pids limit stops a fork bomb inside a session before it reaches the user slice's own task
# limit, so there is no way to turn it off: 0 and -1 (no limit to the engine), words such as
# "max", and values past MAX are refused, and anything else would fail only after the session
# was built.
ccy_container_limits() {
    local memory
    CCY_PIDS_LIMIT="${CCY_PIDS_LIMIT:-$CCY_PIDS_LIMIT_DEFAULT}"
    if ! [[ "$CCY_PIDS_LIMIT" =~ ^[1-9][0-9]{0,4}$ ]] || [ "$CCY_PIDS_LIMIT" -lt "$CCY_PIDS_LIMIT_MIN" ] || [ "$CCY_PIDS_LIMIT" -gt "$CCY_PIDS_LIMIT_MAX" ]; then
        print_error "CCY_PIDS_LIMIT=$CCY_PIDS_LIMIT is not a pids limit: a whole number from $CCY_PIDS_LIMIT_MIN to $CCY_PIDS_LIMIT_MAX; it cannot be turned off (the default is $CCY_PIDS_LIMIT_DEFAULT; docs/ccy.md, \"Container limits\")."
        return 1
    fi
    memory=$(ccy_memory_limit /proc/meminfo) || return 1
    ccy_agents_slice_check || return 1
    CCY_MEMORY_MAX="${memory}m"
    CCY_LIMIT_ARGS=()
    CCY_LIMIT_ARGS+=(--cgroup-parent "$CCY_AGENTS_SLICE" --pids-limit "$CCY_PIDS_LIMIT"
        --memory "${memory}m" --memory-reservation "$((memory - CCY_MEMORY_SOFT_GAP_MIB))m")
}

# ccy_memory_limit <meminfo> — a container's memory cap in MiB, printed: CCY_MEMORY_MAX (bytes,
# or a number with k, m or g) from the floor to the machine's RAM, else the RAM less what the
# desktop keeps. 1 for an override refused, or a machine too small for the default: no value is
# picked for it. The cap cannot be turned off (0, -1 and "max" are refused).
ccy_memory_limit() {
    local total kib="" number
    if ! total=$(awk '$1 == "MemTotal:" { print $2; exit }' "$1") || ! [[ "$total" =~ ^[1-9][0-9]*$ ]]; then
        print_error "could not read MemTotal from $1, so no memory cap can be set for a ccy container."
        return 1
    fi
    if [ -z "${CCY_MEMORY_MAX:-}" ]; then
        kib=$((total - CCY_MEMORY_DESKTOP_MIB * 1024))
        if [ "$kib" -lt $((CCY_MEMORY_FLOOR_MIB * 1024)) ]; then
            print_error "this machine has $((total / 1024)) MiB of RAM: less the $CCY_MEMORY_DESKTOP_MIB MiB kept for the desktop, that is under the $CCY_MEMORY_FLOOR_MIB MiB floor of a ccy container's memory cap. Set CCY_MEMORY_MAX on this host to the cap each container should have (bytes, or a number with k, m or g; docs/ccy.md, \"Container limits\")."
            return 1
        fi
    elif [[ "$CCY_MEMORY_MAX" =~ ^([1-9][0-9]{0,11})([bkmgBKMG]?)$ ]]; then
        number="${BASH_REMATCH[1]}"
        case "${BASH_REMATCH[2]}" in
        '' | b | B) kib=$((number / 1024)) ;;
        k | K) kib=$number ;;
        m | M) kib=$((number * 1024)) ;;
        *) kib=$((number * 1048576)) ;;
        esac
    fi
    if [ -z "$kib" ] || [ "$kib" -lt $((CCY_MEMORY_FLOOR_MIB * 1024)) ] || [ "$kib" -gt "$total" ]; then
        print_error "CCY_MEMORY_MAX=$CCY_MEMORY_MAX is not a memory cap: bytes, or a number with k, m or g, from ${CCY_MEMORY_FLOOR_MIB}m to this machine's $((total / 1024)) MiB of RAM; it cannot be turned off (docs/ccy.md, \"Container limits\")."
        return 1
    fi
    printf '%s\n' "$((kib / 1024))"
}

# ccy_agents_slice_check — 0 when the slice's user unit is installed where the play puts it;
# else 1, naming the play. Every container runs in the slice, which caps them all together, and
# without its unit systemd would make an empty slice with no cap at all.
ccy_agents_slice_check() {
    local unit="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$CCY_AGENTS_SLICE"
    [ ! -f "$unit" ] || return 0
    print_error "$CCY_AGENTS_SLICE is not installed ($unit is missing): every ccy container runs in it, so that all of them together cannot take the host's memory. Deploy it with playbooks/imports/play-claude-yolo.yml, then launch again."
    return 1
}
