#!/bin/bash
# Claude YOLO Common Library
# Shared helpers for claude-yolo and claude-yolo-browser
#
# Version: 1.0.0

# Color codes for consistent output
readonly COLOR_RESET='\033[0m'
readonly COLOR_RED='\033[0;31m'
readonly COLOR_GREEN='\033[0;32m'
readonly COLOR_YELLOW='\033[0;33m'
readonly COLOR_BLUE='\033[0;34m'
readonly COLOR_BOLD='\033[1m'

# Output formatting helpers
print_error() {
    echo -e "${COLOR_RED}ERROR:${COLOR_RESET} $*" >&2
}

print_warning() {
    echo -e "${COLOR_YELLOW}WARNING:${COLOR_RESET} $*" >&2
}

print_success() {
    echo -e "${COLOR_GREEN}✓${COLOR_RESET} $*"
}

print_header() {
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "$*"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Git repository detection
is_git_repo() {
    [ -d .git ]
}

check_git_repo() {
    if ! is_git_repo; then
        print_error "Not in a git repository root directory"
        echo "" >&2
        echo "Claude YOLO must be run from the root of a git repository." >&2
        echo "This ensures Claude Code operates on the correct codebase." >&2
        echo "" >&2
        echo "Current directory: $PWD" >&2
        echo "" >&2
        echo "To fix:" >&2
        echo "  1. Navigate to your git repository root: cd /path/to/your/repo" >&2
        echo "  2. Verify .git exists: ls -la .git" >&2
        echo "  3. Run command from there" >&2
        return 1
    fi
    return 0
}

# Project name extraction
get_project_name() {
    basename "$(pwd)" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/_/g'
}

# YOLO mode warning
show_yolo_warning() {
    echo "" >&2
    echo "⚠️  ${COLOR_YELLOW}YOLO MODE WARNING${COLOR_RESET}" >&2
    echo "" >&2
    echo "Running with ${COLOR_BOLD}--dangerously-skip-permissions${COLOR_RESET}" >&2
    echo "" >&2
    echo "This mode bypasses ALL permission checks for rapid development." >&2
    echo "Claude Code can modify any file without confirmation." >&2
    echo "" >&2
    echo "Benefits:" >&2
    echo "  ✓ Fastest development workflow" >&2
    echo "  ✓ No interruptions for file changes" >&2
    echo "  ✓ Ideal for exploratory work" >&2
    echo "" >&2
    echo "Risks:" >&2
    echo "  ⚠  No safety net" >&2
    echo "  ⚠  Mistakes happen faster" >&2
    echo "  ⚠  Always commit work first" >&2
    echo "" >&2
}

# Claude Code token detection
has_claude_token() {
    local token_dir="${HOME}/.claude"

    # Check for OAuth tokens (sk-ant-oat01-)
    if [ -f "${token_dir}/config" ]; then
        if grep -q "sk-ant-oat01" "${token_dir}/config" 2>/dev/null; then
            return 0
        fi
    fi

    # Check environment variable
    if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
        return 0
    fi

    return 1
}

check_claude_token() {
    if ! has_claude_token; then
        print_warning "No Claude Code token detected"
        echo "" >&2
        echo "Claude Code requires authentication." >&2
        echo "" >&2
        echo "To set up a token:" >&2
        echo "  1. Run 'claude setup-token' from desktop" >&2
        echo "  2. Or use 'ccy --create-token' for container-specific tokens" >&2
        echo "" >&2
        echo "Tokens are stored in ~/.claude/ and automatically available" >&2
        echo "to all environments (desktop, ccy, ccy-browser)." >&2
        echo "" >&2
        return 1
    fi
    return 0
}

# Version comparison
version_greater_than() {
    local version1="$1"
    local version2="$2"

    [ "$(printf '%s\n' "$version1" "$version2" | sort -V | head -n1)" != "$version1" ]
}

# Help text section formatter
print_help_section() {
    local title="$1"
    echo ""
    echo "${COLOR_BOLD}${title}:${COLOR_RESET}"
}

# Command option formatter
print_option() {
    local option="$1"
    local description="$2"
    printf "  ${COLOR_GREEN}%-20s${COLOR_RESET} %s\n" "$option" "$description"
}

# Example formatter
print_example() {
    local description="$1"
    local command="$2"
    echo "  ${description}"
    echo "  ${COLOR_BLUE}${command}${COLOR_RESET}"
    echo ""
}

# Spinner for long-running operations
show_spinner() {
    local pid=$1
    local message="$2"
    local spin='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    local i=0

    while kill -0 $pid 2>/dev/null; do
        i=$(( (i+1) %10 ))
        printf "\r${message} ${spin:$i:1} "
        sleep 0.1
    done
    printf "\r${message} ✓ \n"
}

# Confirmation prompt
confirm() {
    local prompt="$1"
    local default="${2:-n}"  # Default to 'n' if not specified

    if [ "$default" = "y" ] || [ "$default" = "Y" ]; then
        prompt="${prompt} [Y/n]: "
    else
        prompt="${prompt} [y/N]: "
    fi

    while true; do
        read -p "$prompt" response
        response=${response:-$default}

        case "$response" in
            [Yy]|[Yy][Ee][Ss])
                return 0
                ;;
            [Nn]|[Nn][Oo])
                return 1
                ;;
            *)
                echo "Please answer yes or no."
                ;;
        esac
    done
}

# Check if command exists
command_exists() {
    command -v "$1" &> /dev/null
}

# Check if running in container
is_in_container() {
    [ -f /.dockerenv ] || grep -q "/docker/" /proc/1/cgroup 2>/dev/null
}

# Check if running in distrobox
is_in_distrobox() {
    [ -n "${CONTAINER_ID:-}" ] && command_exists distrobox-export
}

# Get Claude Code version
get_claude_version() {
    claude --version 2>/dev/null | head -1 || echo "unknown"
}

# CCY Token Management
# These functions work with the ccy token directory structure

# Check if a token is valid (not expired or expiring today)
# Args: token_file_path
# Returns: 0 (true) if valid, 1 (false) if expired/expiring
is_token_valid() {
    local token_file="$1"
    local filename=$(basename "$token_file")

    # Extract expiry date from filename: NAME.YYYY-MM-DD.token
    if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
        local expiry_date="${BASH_REMATCH[1]}"
        local today=$(date +%Y-%m-%d)

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

# List available ccy tokens
# Args: token_dir
# Returns: 0 if tokens exist, 1 if no tokens
list_ccy_tokens() {
    local token_dir="$1"

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "Available Claude Code Tokens for YOLO Mode"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""

    if [ ! -d "$token_dir" ] || [ -z "$(ls -A "$token_dir"/*.token 2>/dev/null)" ]; then
        echo "No tokens found in: $token_dir"
        echo ""
        echo "Create a token with: ccy --create-token"
        echo ""
        return 1
    fi

    echo "Token storage: $token_dir"
    echo ""

    local today=$(date +%Y-%m-%d)

    for token_file in "$token_dir"/*.token; do
        if [ -f "$token_file" ]; then
            local filename=$(basename "$token_file")
            local token_name="${filename%.*.token}"

            # Extract expiry date from filename
            if [[ "$filename" =~ ([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$ ]]; then
                local expiry_date="${BASH_REMATCH[1]}"
                local status="✓ Valid"

                if [[ "$expiry_date" < "$today" ]]; then
                    status="✗ EXPIRED"
                elif [[ "$expiry_date" == "$today" ]]; then
                    status="⚠ Expires TODAY"
                fi

                echo "  • $token_name"
                echo "    File: $token_file"
                echo "    Expires: $expiry_date ($status)"
            else
                echo "  • $filename"
                echo "    File: $token_file"
                echo "    Status: ✗ INVALID FORMAT (missing expiry date)"
            fi
            echo ""
        fi
    done

    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
}

# Export functions for use in other scripts
export -f print_error
export -f print_warning
export -f print_success
export -f print_header
export -f is_git_repo
export -f check_git_repo
export -f get_project_name
export -f show_yolo_warning
export -f has_claude_token
export -f check_claude_token
export -f version_greater_than
export -f print_help_section
export -f print_option
export -f print_example
export -f show_spinner
export -f confirm
export -f command_exists
export -f is_in_container
export -f is_in_distrobox
export -f get_claude_version
export -f is_token_valid
export -f list_ccy_tokens
