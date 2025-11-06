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

# CCY Project Configuration System
# Shared between ccy (Docker) and ccy-browser (Distrobox)

# Get project state directory based on git remote
# Args: ccy_root (e.g., ~/.claude-tokens/ccy)
# Returns: path to project's .claude directory
get_project_state_dir() {
    local ccy_root="$1"
    local project_dir="$ccy_root/projects"
    local repo_path="$PWD"

    # Get git remote URL - fail fast if not configured
    local git_remote=$(git remote get-url origin 2>/dev/null)
    if [ -z "$git_remote" ]; then
        print_error "No git remote 'origin' configured"
        echo "" >&2
        echo "Claude YOLO requires a git remote to identify the project." >&2
        echo "This ensures conversations are properly tied to the correct repository." >&2
        echo "" >&2
        echo "To fix:" >&2
        echo "  git remote add origin <url>" >&2
        echo "" >&2
        echo "Current directory: $repo_path" >&2
        return 1
    fi

    # Extract repo name from git remote URL
    # Examples:
    #   git@github.com:user/repo.git -> user_repo
    #   https://github.com/user/repo.git -> user_repo
    local project_name=$(echo "$git_remote" | sed -E 's|.*[:/]([^/]+)/([^/]+)(\.git)?$|\1_\2|')

    local project_root="$project_dir/$project_name"
    local project_claude_dir="$project_root/.claude"

    # Create project structure if it doesn't exist
    if [ ! -d "$project_root" ]; then
        mkdir -p "$project_root"
        chmod 700 "$project_root"

        # Create metadata file for reference
        cat > "$project_root/.project-info" <<EOFINFO
# Claude Code YOLO Project State
Repository Path: $repo_path
Git Remote: $git_remote
Project Name: $project_name
Created: $(date -Iseconds)
EOFINFO
        chmod 600 "$project_root/.project-info"
    fi

    # Create .claude directory if it doesn't exist
    if [ ! -d "$project_claude_dir" ]; then
        mkdir -p "$project_claude_dir"
        chmod 700 "$project_claude_dir"
    fi

    echo "$project_claude_dir"
}

# Load last launch configuration for current project
# Args: project_claude_dir, config_version, tool_version, tool_hash
# Returns: 0 if valid config loaded, 1 if no config or invalid
# Sets: SAVED_* variables if successful
load_launch_config() {
    local project_claude_dir="$1"
    local config_version="$2"
    local tool_version="$3"
    local tool_hash="$4"
    local config_file="$project_claude_dir/../.last-launch.conf"

    # Check if config exists
    if [[ ! -f "$config_file" ]]; then
        return 1  # No config, use interactive
    fi

    # Source config
    source "$config_file" 2>/dev/null || {
        echo "Warning: Config file corrupted, reconfiguring..." >&2
        rm -f "$config_file"
        return 1
    }

    # Validate CONFIG_VERSION (schema version)
    if [[ "${SAVED_CONFIG_VERSION:-0}" != "$config_version" ]]; then
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "⚠️  Config schema outdated" >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "Saved config:  v${SAVED_CONFIG_VERSION:-0}" >&2
        echo "Expected:      v${config_version}" >&2
        echo "" >&2
        echo "Your saved launch configuration uses an old format." >&2
        echo "Reconfiguring with interactive prompts..." >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "" >&2
        rm -f "$config_file"
        return 1
    fi

    # Validate tool version and hash together
    local version_match=false
    local hash_match=false

    [[ "$tool_version" == "${SAVED_TOOL_VERSION:-}" ]] && version_match=true
    [[ "$tool_hash" == "${SAVED_TOOL_HASH:-}" ]] && hash_match=true

    if $version_match && $hash_match; then
        # Perfect match - config is valid
        return 0

    elif $version_match && ! $hash_match; then
        # VERSION same but HASH different = Developer forgot to update version!
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "⚠️  DEVELOPER ERROR: Script modified without version bump" >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "Version:       ${tool_version} (unchanged)" >&2
        echo "Saved hash:    ${SAVED_TOOL_HASH:-unknown}" >&2
        echo "Current hash:  ${tool_hash}" >&2
        echo "" >&2
        echo "The script has been modified but version was not updated." >&2
        echo "This is a developer error - version numbers must be incremented" >&2
        echo "when the script changes to ensure config compatibility." >&2
        echo "" >&2
        echo "Forcing reconfiguration for safety..." >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "" >&2
        rm -f "$config_file"
        return 1

    elif ! $version_match; then
        # VERSION different = Normal upgrade/downgrade
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "ℹ️  Tool version changed" >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "Saved config:  v${SAVED_TOOL_VERSION:-unknown}" >&2
        echo "Current tool:  v${tool_version}" >&2
        echo "" >&2
        echo "Reconfiguring for compatibility with new version..." >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "" >&2
        rm -f "$config_file"
        return 1
    fi
}

# Save launch configuration for current project
# Args: project_claude_dir, config_version, tool_version, tool_hash, token_name, ssh_keys_str, network
save_launch_config() {
    local project_claude_dir="$1"
    local config_version="$2"
    local tool_version="$3"
    local tool_hash="$4"
    local token_name="$5"
    local ssh_keys_str="$6"
    local network="$7"

    local config_file="$project_claude_dir/../.last-launch.conf"

    cat > "$config_file" <<EOFCONFIG
# CCY Launch Configuration  
# Config Version: ${config_version}
# Tool Version: ${tool_version}
# Tool Hash: ${tool_hash}
# Generated: $(date '+%Y-%m-%d %H:%M:%S')
SAVED_CONFIG_VERSION=${config_version}
SAVED_TOOL_VERSION="${tool_version}"
SAVED_TOOL_HASH="${tool_hash}"
LAST_TOKEN="${token_name}"
LAST_SSH_KEYS="${ssh_keys_str}"
LAST_NETWORK="${network}"
LAST_LAUNCH_DATE="$(date '+%Y-%m-%d')"
EOFCONFIG

    chmod 600 "$config_file"
}

# Discover github_ SSH keys in ~/.ssh/
# Returns: array of SSH key paths in GITHUB_KEYS global variable
discover_github_ssh_keys() {
    GITHUB_KEYS=($(find "$HOME/.ssh" -type f -name "github_*" ! -name "*.pub" 2>/dev/null | sort))
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
export -f get_project_state_dir
export -f load_launch_config
export -f save_launch_config
export -f discover_github_ssh_keys

