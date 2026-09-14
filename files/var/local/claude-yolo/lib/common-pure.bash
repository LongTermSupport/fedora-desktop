#!/bin/bash
# Claude YOLO Common Library — Host-Safe Pure Helpers
#
# Contains ONLY pure helpers with no container-engine dependencies.
# Safe to source from any host shell without triggering a podman-check exit.
#
# Sourced by:
#   - common.bash (in-place, before its podman-check block) so ccy keeps the
#     same surface API.
#   - /var/local/claude-code/cc wrapper, which cannot tolerate sourcing
#     common.bash directly because common.bash:30-34 calls `exit 1` at file
#     scope when podman is not on PATH.
#
# Rule: anything that needs `container_cmd`, `$CONTAINER_ENGINE`, or any
# container binary MUST stay in common.bash. Helpers here must work on a
# minimal host with bash, coreutils, and basename/date/grep only.

# Color codes needed by the helpers in this file. Other color constants
# (GREEN, YELLOW, BLUE, BOLD) live in common.bash because only its functions
# consume them; keeping them out of here avoids SC2034 unused-readonly noise.
readonly COLOR_RESET='\033[0m'
readonly COLOR_RED='\033[0;31m'

# Output formatting helpers
print_error() {
    echo -e "${COLOR_RED}ERROR:${COLOR_RESET} $*" >&2
}

# Decide whether SELinux will refuse this container's reads of a home-directory bind.
#
# On an Enforcing host a ccy container runs as container_t and is denied `read`
# on user_home_t (the project) and ssh_home_t (a key) — measured, audit log in
# hand. The remedy is a relabel on the mounts, and this is the function that
# says whether one is needed. PURE, like engine_rootless_verdict: both answers
# arrive as text, so every combination is testable
# (scripts/test-ccy-selinux-verdict.bash).
#
# Args:   `getenforce` output ("" when the command does not exist),
#         the engine's SELinux report — podman's
#         `info --format '{{.Host.Security.SELinuxEnabled}}'`, exactly `true`
#         or `false` when it worked
# Prints: enforcing | off | unknown
#
# `off` needs BOTH halves to say so: Permissive/Disabled means nothing is
# refused, and an engine that does not label (`label=false` in containers.conf)
# runs its containers unconfined, so an Enforcing host is still `off` for them.
# `unknown` is anything that cannot be read as one of those, and the caller
# treats it as enforcing: relabelling a readable tree costs nothing, while not
# relabelling an unreadable one costs the whole session.
selinux_enforcing_verdict() {
    local enforce report
    enforce=$(printf '%s' "$1" | tr -d '[:space:]')
    report=$(printf '%s' "$2" | tr -d '[:space:]')

    case "$enforce" in
        Permissive|Disabled)
            printf 'off\n'
            return 0
            ;;
        "")
            # No SELinux userland. The engine's own answer is the only one left.
            if [ "$report" = "false" ]; then
                printf 'off\n'
            else
                printf 'unknown\n'
            fi
            return 0
            ;;
        Enforcing)
            ;;
        *)
            printf 'unknown\n'
            return 0
            ;;
    esac

    case "$report" in
        true)  printf 'enforcing\n' ;;
        false) printf 'off\n' ;;
        *)     printf 'unknown\n' ;;
    esac
    return 0
}

# Emit the `--device` flags that hand the container the host's GPU render nodes, or nothing.
#
# The DRM directory (/dev/dri) exists only where a kernel graphics driver is loaded: every
# desktop, and a VM with a virtual GPU. A headless server or a serial-console VM has no such
# directory, and `podman run --device /dev/dri` then aborts the whole session with
# `stat /dev/dri: no such file or directory` — measured on a dev VM, exit 125 before Claude
# ever started. The device only matters for hardware-accelerated browser rendering, which a
# headless box cannot use anyway, so its absence is a fact to adapt to, not an error.
#
# PURE: takes the path to test as an argument (the caller passes /dev/dri; tests pass a
# throwaway directory or a path that does not exist) and prints one flag per line so the
# caller can `mapfile` them into the run argv. Prints nothing when the path is not a
# directory. Never fails: a missing GPU is the answer, not an error.
gpu_device_flags() {
    local dri="${1:?gpu_device_flags: path required}"
    if [ -d "$dri" ]; then
        printf -- '--device\n%s:%s\n' "$dri" "$dri"
    fi
    return 0
}

# Decide, from a container engine's OWN report, whether it is rootless.
#
# ccy runs `claude --dangerously-skip-permissions` and bind-mounts the project at
# /workspace. That is only safe while the engine is ROOTLESS, so container uid 0
# maps to an unprivileged host user through a user namespace.
#
# The uid-0 guard in claude-yolo constrains the CLIENT; it cannot say what the
# ENGINE is. A non-root user can still drive a rootful engine — CONTAINER_HOST or
# DOCKER_HOST pointing at a system socket, or a docker context. So ccy asks the
# engine instead of inferring from its own uid.
#
# PURE by design: takes the raw report as an argument and runs no command, which is
# what makes the rootful and unreadable cases testable at all — you cannot ask a
# real engine to be rootful just to prove the guard notices
# (scripts/test-ccy-rootless-guard.bash).
#
# Args:   engine (podman|docker), raw report text (stdout+stderr of `info`)
# Prints: rootless | rootful | unknown
#
# `unknown` is NOT a pass, and that is the load-bearing decision. A guard that
# treats an unreadable answer as safety passes hardest exactly when it can see
# least. Measured precedent — Plan 00068 group F: asking podman for an ABSENT label
# returns exit 0 and ZERO BYTES, so comparing two unknowns returns "equal" and
# reports the safe-sounding answer having measured nothing at all.
engine_rootless_verdict() {
    local engine="$1"
    local report="$2"

    # Strip whitespace so a trailing newline cannot become a third state.
    report=$(printf '%s' "$report" | tr -d '[:space:]')

    if [ -z "$report" ]; then
        printf 'unknown\n'
        return 0
    fi

    case "$engine" in
        podman)
            # `podman info --format '{{.Host.Security.Rootless}}'` prints exactly
            # `true` or `false`. Anything else means the field moved, or the command
            # failed and its error text landed in the capture — either way we cannot
            # claim to know.
            case "$report" in
                true) printf 'rootless\n' ;;
                false) printf 'rootful\n' ;;
                *) printf 'unknown\n' ;;
            esac
            ;;
        docker)
            # `docker info --format '{{.SecurityOptions}}'` prints a Go slice such as
            # `[name=seccomp,profile=builtin name=cgroupns]`. `name=rootless` appears
            # only on a rootless daemon. A report carrying `name=` at all is a
            # SecurityOptions list we understood; without `name=` it is not.
            case "$report" in
                *name=rootless*) printf 'rootless\n' ;;
                *name=*) printf 'rootful\n' ;;
                *) printf 'unknown\n' ;;
            esac
            ;;
        *)
            # An engine ccy has never been taught to interrogate. Refusing beats
            # guessing: this is the branch that keeps a future engine from silently
            # inheriting podman's answer.
            printf 'unknown\n'
            ;;
    esac
    return 0
}

# Check if a token is valid (not expired or expiring today)
# Args: token_file_path
# Returns: 0 (true) if valid, 1 (false) if expired/expiring
is_token_valid() {
    local token_file="$1"
    local filename
    filename=$(basename "$token_file")

    # Extract expiry date from filename: NAME.YYYY-MM-DD.token
    if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
        local expiry_date="${BASH_REMATCH[1]}"
        local today
        today=$(date +%Y-%m-%d)

        # Compare dates
        if [[ "$expiry_date" < "$today" ]]; then
            return 1  # Expired
        elif [[ "$expiry_date" == "$today" ]]; then
            return 1  # Expiring today
        else
            return 0  # Valid
        fi
    else
        # Old format token without expiry date - treat as expired
        return 1
    fi
}

# Validate ONE line of a project's tracked .claude/ccy/mounts file.
#
# The file is tracked in git, so a clone declares binds of the cloner's host
# paths at launch. That is the feature (a Drive mount follows the project) and
# the risk (a repo could ask for ~/.ssh read-write), so every line is checked
# here and any failure aborts the launch before a container exists. PURE: no
# filesystem access, so the deny rules can be proven with fixtures. Existence
# of the source is checked by the caller, which can see the disk.
#
# Line format: <host-src>:<container-dst>[:ro|rw]
#   - `~/` or `$HOME/` at the start of the host path expands to $home; nothing
#     else is expanded, so a line cannot run code or read other variables.
#   - both paths must be absolute and free of `..` segments.
#   - the host path may not be a credential or state directory, the project
#     itself or an ancestor of it (that would expose sibling projects), or a
#     system root.
#   - the container path may not shadow the workspace, its state, or a system
#     directory the container needs to function.
#   - the only options are ro and rw (default rw). No SELinux relabels: a `:z`
#     on a home directory relabels it for every container on the machine.
#
# Args:   line, home, project_dir
# Prints: "<src>|<dst>|<opt>" on success
# Errors: one message per rule broken, on stderr; return 1
ccy_validate_mount_line() {
    local line="$1" home="$2" project_dir="$3"
    local src dst opt rest colons
    local errors=0

    colons="${line//[^:]/}"
    if [ "${#colons}" -lt 1 ] || [ "${#colons}" -gt 2 ]; then
        echo "  expected <host-src>:<container-dst>[:ro|rw], got: $line" >&2
        return 1
    fi
    src="${line%%:*}"
    rest="${line#*:}"
    dst="${rest%%:*}"
    opt=""
    [ "$rest" != "$dst" ] && opt="${rest#*:}"
    opt="${opt:-rw}"

    # Literal prefixes, assembled so shellcheck does not read them as an
    # intended expansion: these are the two spellings a mounts file may use.
    local tilde_prefix home_prefix
    tilde_prefix=$(printf '\x7e/')
    home_prefix=$(printf '\x24HOME/')
    if [ "${src#"$tilde_prefix"}" != "$src" ]; then
        src="$home/${src#"$tilde_prefix"}"
    elif [ "${src#"$home_prefix"}" != "$src" ]; then
        src="$home/${src#"$home_prefix"}"
    fi
    [ "$src" != "/" ] && src="${src%/}"
    [ "$dst" != "/" ] && dst="${dst%/}"

    local p
    for p in "$src" "$dst"; do
        case "$p" in
            /*) ;;
            *) echo "  not an absolute path: $p" >&2; errors=$((errors + 1)) ;;
        esac
        case "/$p/" in
            */../*) echo "  '..' is not allowed: $p" >&2; errors=$((errors + 1)) ;;
        esac
    done

    case "$opt" in
        ro|rw) ;;
        *) echo "  option must be ro or rw, got: $opt" >&2; errors=$((errors + 1)) ;;
    esac

    # Host side: credentials, launcher state, system roots, and the project or
    # any directory above it. The root and the home directory are refused
    # exactly, not as prefixes: everything lives under them.
    local denied
    if [ "$src" = "/" ] || [ "$src" = "$home" ]; then
        echo "  host path is off limits: $src" >&2
        errors=$((errors + 1))
    fi
    local -a src_deny=(
        "$home/.ssh" "$home/.gnupg" "$home/.aws" "$home/.kube"
        "$home/.claude" "$home/.claude-tokens" "$home/.config/gh"
        "$home/.local/share/keyrings" "$home/.password-store"
        "/etc" "/root" "/run" "/var/run" "/proc" "/sys" "/dev" "/boot"
    )
    for denied in "${src_deny[@]}"; do
        if [ "$src" = "$denied" ] || [[ "$src" == "$denied/"* ]]; then
            echo "  host path is off limits: $src (under $denied)" >&2
            errors=$((errors + 1))
            break
        fi
    done
    p="$project_dir"
    while [ -n "$p" ] && [ "$p" != "/" ]; do
        if [ "$src" = "$p" ]; then
            echo "  host path is the project or a directory above it: $src" >&2
            errors=$((errors + 1))
            break
        fi
        p="${p%/*}"
    done

    # Container side: the workspace and its state, and what the image needs.
    local -a dst_deny=(
        "/" "/workspace" "/workspace/.claude" "/root" "/home"
        "/etc" "/usr" "/bin" "/sbin" "/lib" "/lib64" "/opt"
        "/proc" "/sys" "/dev" "/run" "/var" "/tmp/claude-config-import"
    )
    # `/` and `/workspace` are refused exactly; anything else is a prefix.
    for denied in "${dst_deny[@]}"; do
        if [ "$dst" = "$denied" ] || { [ "$denied" != "/" ] && [ "$denied" != "/workspace" ] && [[ "$dst" == "$denied/"* ]]; }; then
            echo "  container path is off limits: $dst (under $denied)" >&2
            errors=$((errors + 1))
            break
        fi
    done

    [ "$errors" -gt 0 ] && return 1
    printf '%s|%s|%s\n' "$src" "$dst" "$opt"
}

# Project name from the working directory: "<parent>-<dir>" unless the parent is a
# generic folder, lowercased, odd characters replaced by "_". Names the CCY container
# and, since 3.53.0, the tmux session of both ccy and the host cc wrapper — which is
# why it lives here rather than in common.bash: cc loads only this engine-free library.
get_project_name() {
    local project_dir parent_dir
    project_dir=$(basename "$(pwd)" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/_/g')
    parent_dir=$(basename "$(dirname "$(pwd)")" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/_/g')
    local generic_folders="projects|repos|work|src|code|dev|home"

    if ! echo "$parent_dir" | grep -qiE "^($generic_folders)$"; then
        echo "${parent_dir}-${project_dir}"
    else
        echo "$project_dir"
    fi
}

export -f print_error
export -f is_token_valid
export -f ccy_validate_mount_line
export -f get_project_name
