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

export -f print_error
export -f is_token_valid
