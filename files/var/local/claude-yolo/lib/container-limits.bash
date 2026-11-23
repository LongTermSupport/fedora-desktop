#!/bin/bash
# The limits every container ccy runs is started with, passed explicitly on each run and never
# left to the engine's default. Why, and the choice of defaults: docs/ccy.md, "Container limits".
# The rule that holds every ccy run to them: ccy-container-default-pids-limit (CLAUDE/QA.md,
# .semgrep/bash-conventions.yml); the Go copy of the bounds: src-go/internal/ccylimits.
#
# CCY_PIDS_LIMIT and CCY_MEMORY_MAX on the host override the pids and memory limits; the memory
# default is the slice's MemoryMax, which the play writes from host_vars (CCY_AGENTS_SLICE_SETTING)
# or the RAM. Requires print_error (common-pure.bash).

CCY_PIDS_LIMIT_DEFAULT=16384
CCY_PIDS_LIMIT_MIN=256
CCY_PIDS_LIMIT_MAX=32768
CCY_MEMORY_FLOOR_MIB=2048
CCY_MEMORY_SOFT_GAP_MIB=2048
CCY_AGENTS_SLICE=agents.slice
CCY_AGENTS_SLICE_SETTING=ccy_agents_slice_memory_max_mb

# ccy_container_limits — settle CCY_PIDS_LIMIT (the host's value, else the default), check the
# slice is installed, settle CCY_MEMORY_MAX (ccy_memory_limit), and fill CCY_LIMIT_ARGS, the
# limit set every run passes as "${CCY_LIMIT_ARGS[@]}". 1 for a value refused or a missing slice.
# The pids limit stops a fork bomb inside a session before it reaches the user slice's own task
# limit, so there is no way to turn it off: 0 and -1 (no limit to the engine), words such as
# "max", and values past MAX are refused, and anything else would fail only after the session
# was built.
ccy_container_limits() {
    local memory soft
    CCY_PIDS_LIMIT="${CCY_PIDS_LIMIT:-$CCY_PIDS_LIMIT_DEFAULT}"
    if ! [[ "$CCY_PIDS_LIMIT" =~ ^[1-9][0-9]{0,4}$ ]] || [ "$CCY_PIDS_LIMIT" -lt "$CCY_PIDS_LIMIT_MIN" ] || [ "$CCY_PIDS_LIMIT" -gt "$CCY_PIDS_LIMIT_MAX" ]; then
        print_error "CCY_PIDS_LIMIT=$CCY_PIDS_LIMIT is not a pids limit: a whole number from $CCY_PIDS_LIMIT_MIN to $CCY_PIDS_LIMIT_MAX; it cannot be turned off (the default is $CCY_PIDS_LIMIT_DEFAULT; docs/ccy.md, \"Container limits\")."
        return 1
    fi
    ccy_agents_slice_check || return 1
    memory=$(ccy_memory_limit /proc/meminfo "$CCY_AGENTS_SLICE_UNIT") || return 1
    CCY_MEMORY_MAX="${memory}m"
    soft=$((memory / 2 < CCY_MEMORY_SOFT_GAP_MIB ? memory / 2 : CCY_MEMORY_SOFT_GAP_MIB))
    CCY_LIMIT_ARGS=()
    CCY_LIMIT_ARGS+=(--cgroup-parent "$CCY_AGENTS_SLICE" --pids-limit "$CCY_PIDS_LIMIT"
        --memory "${memory}m" --memory-reservation "$((memory - soft))m")
}

# ccy_memory_limit <meminfo> <slice unit> — a container's memory cap in MiB, printed:
# CCY_MEMORY_MAX (bytes, or a number with k, m or g) from the floor to the machine's RAM, else
# the slice unit's last MemoryMax=<n>M, held to the same bounds. 1 for an override refused, or a
# slice with no MemoryMax or one out of bounds: no value is picked for it. The cap cannot be
# turned off (0, -1 and "max" are refused).
ccy_memory_limit() {
    local total kib="" number line
    if ! total=$(awk '$1 == "MemTotal:" { print $2; exit }' "$1") || ! [[ "$total" =~ ^[1-9][0-9]*$ ]]; then
        print_error "could not read MemTotal from $1, so no memory cap can be set for a ccy container."
        return 1
    fi
    if [ -z "${CCY_MEMORY_MAX:-}" ]; then
        while IFS= read -r line || [ -n "$line" ]; do
            if [[ "$line" =~ ^MemoryMax=([1-9][0-9]{0,8})M$ ]]; then kib=$((BASH_REMATCH[1] * 1024)); fi
        done <"$2"
        if [ -z "$kib" ]; then
            print_error "$CCY_AGENTS_SLICE has no MemoryMax=<MiB>M line, which a container's default memory cap is read from. Deploy it with playbooks/imports/play-claude-yolo.yml, then launch again (or set CCY_MEMORY_MAX on this host)."
            return 1
        elif [ "$kib" -lt $((CCY_MEMORY_FLOOR_MIB * 1024)) ] || [ "$kib" -gt "$total" ]; then
            print_error "$CCY_AGENTS_SLICE's MemoryMax is $((kib / 1024)) MiB, a container's default memory cap, which must be from the $CCY_MEMORY_FLOOR_MIB MiB floor to this machine's $((total / 1024)) MiB of RAM. Set $CCY_AGENTS_SLICE_SETTING in this host's host_vars to the MiB all ccy containers together may use and run playbooks/imports/play-claude-yolo.yml (docs/ccy.md, \"Container limits\")."
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

# ccy_agents_slice_check — 0 when the slice's user unit is installed where the play puts it,
# with CCY_AGENTS_SLICE_UNIT set to its path; else 1, naming the play. Every container runs in
# the slice, which caps them all together, and without its unit systemd would make an empty
# slice with no cap at all.
ccy_agents_slice_check() {
    CCY_AGENTS_SLICE_UNIT="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user/$CCY_AGENTS_SLICE"
    [ ! -f "$CCY_AGENTS_SLICE_UNIT" ] || return 0
    print_error "$CCY_AGENTS_SLICE is not installed ($CCY_AGENTS_SLICE_UNIT is missing): every ccy container runs in it, so that all of them together cannot take the host's memory. Deploy it with playbooks/imports/play-claude-yolo.yml, then launch again."
    return 1
}
