#!/bin/bash
# Claude YOLO Common Library
# Shared helpers for claude-yolo (ccy)
#
# Version: 1.4.0

# Color codes for consistent output
readonly COLOR_RESET='\033[0m'
readonly COLOR_RED='\033[0;31m'
readonly COLOR_GREEN='\033[0;32m'
readonly COLOR_YELLOW='\033[0;33m'
readonly COLOR_BLUE='\033[0;34m'
readonly COLOR_BOLD='\033[1m'

# ═══════════════════════════════════════════════════════════════════════════════
# Container Engine Abstraction
# ═══════════════════════════════════════════════════════════════════════════════
# Supports both Docker and Podman via CCY_CONTAINER_ENGINE environment variable.
# Default: docker (set in bashrc include or environment)
#
# To switch to Podman:
#   export CCY_CONTAINER_ENGINE=podman
#
# All container commands should use container_cmd() instead of calling docker directly.
# ═══════════════════════════════════════════════════════════════════════════════

CONTAINER_ENGINE="${CCY_CONTAINER_ENGINE:-podman}"

# Validate container engine is available
if ! command -v "$CONTAINER_ENGINE" &>/dev/null; then
    echo "ERROR: Container engine '$CONTAINER_ENGINE' not found" >&2
    echo "Install $CONTAINER_ENGINE or set CCY_CONTAINER_ENGINE to an available engine" >&2
    exit 1
fi

# Container command wrapper - use this instead of calling docker/podman directly
# Usage: container_cmd run --rm -it image:tag
#        container_cmd build -t name .
#        container_cmd ps -a
container_cmd() {
    "$CONTAINER_ENGINE" "$@"
}

# Export for use in subshells
export CONTAINER_ENGINE
export -f container_cmd

# Output formatting helpers
print_error() {
    echo -e "${COLOR_RED}ERROR:${COLOR_RESET} $*" >&2
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

# ═══════════════════════════════════════════════════════════════════════════════
# SECURITY: Ensure .claude/ccy/ is protected and check for tracked sensitive files
# ═══════════════════════════════════════════════════════════════════════════════
# The .claude/ccy/ directory contains sensitive session data that should NEVER
# be committed to git. Only .gitignore and Dockerfile are safe to track.
#
# This function:
# 1. Forces .claude/ccy/.gitignore to exist with correct content (warns if updating)
# 2. FAILS LOUDLY if any dangerous files are already tracked in git
# 3. WARNS if Dockerfile exists but is gitignored (won't be shared with team)
# ═══════════════════════════════════════════════════════════════════════════════

check_ccy_gitignore_safety() {
    local ccy_dir=".claude/ccy"
    local ccy_gitignore="$ccy_dir/.gitignore"
    local ccy_dockerfile="$ccy_dir/Dockerfile"

    # Expected .gitignore content
    local expected_gitignore="# CCY session data - NEVER commit sensitive files
# Only .gitignore, Dockerfile, and allowed-hostnames are safe to track
*
!.gitignore
!Dockerfile
!allowed-hostnames"

    # ═══════════════════════════════════════════════════════════════════════════
    # STEP 1: Force .claude/ccy/.gitignore to exist with correct content
    # ═══════════════════════════════════════════════════════════════════════════

    # Create directory if needed
    if [ ! -d "$ccy_dir" ]; then
        mkdir -p "$ccy_dir"
    fi

    # Check if .gitignore needs to be created or updated
    local current_content=""
    if [ -f "$ccy_gitignore" ]; then
        current_content=$(cat "$ccy_gitignore")
    fi

    # Check if file is missing the critical "ignore all" rule
    if ! echo "$current_content" | grep -q "^\*$"; then
        if [ -f "$ccy_gitignore" ]; then
            echo -e "${COLOR_YELLOW}⚠  Updating .claude/ccy/.gitignore (was missing protection rules)${COLOR_RESET}"
        else
            echo "✓ Creating .claude/ccy/.gitignore for security"
        fi
        echo "$expected_gitignore" > "$ccy_gitignore"
    fi

    # ═══════════════════════════════════════════════════════════════════════════
    # STEP 2: CRITICAL - Check for dangerous tracked files
    # ═══════════════════════════════════════════════════════════════════════════

    # Get list of tracked files in .claude/ccy/
    local tracked_files
    tracked_files=$(git ls-files "$ccy_dir" 2>/dev/null || echo "")

    if [ -z "$tracked_files" ]; then
        # Nothing tracked - check if Dockerfile exists but is being ignored
        check_dockerfile_gitignored
        return $?
    fi

    # Filter out safe files: .gitignore and Dockerfile
    local dangerous_files=""
    while IFS= read -r file; do
        local basename
        basename=$(basename "$file")
        case "$basename" in
            .gitignore|Dockerfile|allowed-hostnames)
                # Safe to track
                ;;
            *)
                # Dangerous!
                dangerous_files="$dangerous_files$file"$'\n'
                ;;
        esac
    done <<< "$tracked_files"

    # Remove trailing newline
    dangerous_files=$(echo "$dangerous_files" | sed '/^$/d')

    if [ -z "$dangerous_files" ]; then
        # Only safe files tracked - still check if Dockerfile should be tracked
        check_dockerfile_gitignored
        return $?
    fi

    # ═══════════════════════════════════════════════════════════════════════════
    # SCREAM AT THE USER - DANGEROUS FILES ARE TRACKED
    # ═══════════════════════════════════════════════════════════════════════════
    echo "" >&2
    echo -e "${COLOR_RED}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}██████████████████████████████████████████████████████████████████████████████████${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}██                                                                              ██${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}██  ⚠️  SECURITY ALERT: SENSITIVE FILES TRACKED IN GIT  ⚠️                       ██${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}██                                                                              ██${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}██████████████████████████████████████████████████████████████████████████████████${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
    echo "" >&2
    echo -e "${COLOR_BOLD}These files in .claude/ccy/ are being TRACKED by git:${COLOR_RESET}" >&2
    echo "" >&2

    while IFS= read -r file; do
        echo -e "  ${COLOR_RED}✗${COLOR_RESET} $file" >&2
    done <<< "$dangerous_files"

    echo "" >&2
    echo -e "${COLOR_YELLOW}Why this is dangerous:${COLOR_RESET}" >&2
    echo "  • .last-launch.conf contains token names and SSH key paths" >&2
    echo "  • Session files contain your conversation history" >&2
    echo "  • These files should NEVER be pushed to a repository" >&2
    echo "" >&2
    echo -e "${COLOR_GREEN}To fix (purge from entire git history):${COLOR_RESET}" >&2
    echo "" >&2

    # Build filter-repo command with specific dangerous files
    local filter_cmd="git filter-repo --invert-paths"
    while IFS= read -r file; do
        filter_cmd="$filter_cmd --path '$file'"
    done <<< "$dangerous_files"

    echo "  $filter_cmd" >&2
    echo "" >&2
    echo "  # Then force push (required after history rewrite)" >&2
    echo "  git push --force-with-lease" >&2
    echo "" >&2
    echo -e "${COLOR_RED}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
    echo -e "${COLOR_BOLD}CCY will NOT start until this is fixed.${COLOR_RESET}" >&2
    echo -e "${COLOR_RED}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
    echo "" >&2

    return 1
}

# ═══════════════════════════════════════════════════════════════════════════════
# check_allowed_hostname: Enforce hostname restrictions for CCY projects
#
# If .claude/ccy/allowed-hostnames exists, the current hostname must match
# at least one entry. This prevents accidentally running Docker-based CCY on
# projects that should only run inside an LXC or other specific environment.
#
# File format (.claude/ccy/allowed-hostnames):
#   - One entry per line
#   - Lines starting with # are comments; inline # also stripped
#   - Blank lines are ignored
#   - never  alone on a line = disable CCY for this project entirely (any host)
#   - *      alone on a line = allow any hostname (useful to document without blocking)
#   - Glob patterns supported: myhost-*, *.local, prod-??
#
# If the file does not exist, there is no restriction (opt-in enforcement).
# ═══════════════════════════════════════════════════════════════════════════════

check_allowed_hostname() {
    local allowed_file=".claude/ccy/allowed-hostnames"

    # No restriction file = no restriction
    if [[ ! -f "$allowed_file" ]]; then
        return 0
    fi

    local current_hostname
    current_hostname=$(hostname)

    # Parse allowed entries: strip comments and blank lines
    local allowed_hostnames=()
    while IFS= read -r line; do
        line="${line%%#*}"            # strip inline comments
        line="${line//[[:space:]]/}"  # strip all whitespace
        [[ -n "$line" ]] && allowed_hostnames+=("$line")
    done < "$allowed_file"

    # Empty file after parsing = no restriction
    if [[ ${#allowed_hostnames[@]} -eq 0 ]]; then
        return 0
    fi

    # Match hostname against each entry (glob patterns via case statement)
    local pattern
    for pattern in "${allowed_hostnames[@]}"; do
        # Special keyword: never = unconditionally disabled for this project
        if [[ "$pattern" == "never" ]]; then
            print_error "CCY is disabled for this project"
            echo "" >&2
            echo "This project has CCY unconditionally disabled via:" >&2
            echo "  .claude/ccy/allowed-hostnames  (contains 'never')" >&2
            echo "" >&2
            echo "To re-enable CCY for this project, remove or edit that file." >&2
            return 1
        fi
        # shellcheck disable=SC2254  # unquoted intentional: glob pattern matching
        case "$current_hostname" in
            $pattern) return 0 ;;
        esac
    done

    print_error "CCY is not allowed to run on this host"
    echo "" >&2
    echo "  Current hostname:  $current_hostname" >&2
    echo "  Allowed patterns:  ${allowed_hostnames[*]}" >&2
    echo "" >&2
    echo "This project restricts which hosts can run the Docker-based CCY." >&2
    echo "  Restriction file: .claude/ccy/allowed-hostnames" >&2
    echo "" >&2
    echo "This project is intended to run via CCY inside its own environment" >&2
    echo "(e.g. an LXC container), not from the desktop CCY Docker container." >&2
    echo "" >&2
    echo "To allow this host, add it to the restriction file:" >&2
    echo "  echo \"$current_hostname\" >> .claude/ccy/allowed-hostnames" >&2
    return 1
}

# ═══════════════════════════════════════════════════════════════════════════════
# Check if Dockerfile exists but is gitignored (won't be shared with team)
# ═══════════════════════════════════════════════════════════════════════════════
# This is a WARNING, not an error. The project will still work, but team members
# won't get the custom container configuration.
# ═══════════════════════════════════════════════════════════════════════════════

check_dockerfile_gitignored() {
    local ccy_dockerfile=".claude/ccy/Dockerfile"

    # If Dockerfile doesn't exist, nothing to check
    if [ ! -f "$ccy_dockerfile" ]; then
        return 0
    fi

    # Check if Dockerfile is tracked
    if git ls-files --error-unmatch "$ccy_dockerfile" >/dev/null 2>&1; then
        # Dockerfile is tracked - all good!
        return 0
    fi

    # Check if Dockerfile is gitignored
    if git check-ignore -q "$ccy_dockerfile" 2>/dev/null; then
        # Dockerfile exists but is gitignored - WARN but don't fail
        echo "" >&2
        echo -e "${COLOR_YELLOW}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
        echo -e "${COLOR_YELLOW}⚠️  WARNING: Custom Dockerfile is gitignored${COLOR_RESET}" >&2
        echo -e "${COLOR_YELLOW}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
        echo "" >&2
        echo "Your custom Dockerfile won't be shared with your team:" >&2
        echo "  ${COLOR_RED}✗${COLOR_RESET} $ccy_dockerfile (exists but gitignored)" >&2
        echo "" >&2
        echo -e "${COLOR_YELLOW}Why this matters:${COLOR_RESET}" >&2
        echo "  • Team members won't get your container configuration" >&2
        echo "  • CI/CD won't have the right environment" >&2
        echo "  • Container setup is lost when cloning repository" >&2
        echo "" >&2
        echo -e "${COLOR_GREEN}To fix (recommended):${COLOR_RESET}" >&2
        echo "" >&2
        echo "  # Check what's ignoring it" >&2
        echo "  git check-ignore -v $ccy_dockerfile" >&2
        echo "" >&2
        echo "  # Option 1: Add exception to project .gitignore" >&2
        echo "  echo '!.claude/ccy/Dockerfile' >> .gitignore" >&2
        echo "" >&2
        echo "  # Option 2: Track it explicitly (overrides gitignore)" >&2
        echo "  git add -f $ccy_dockerfile" >&2
        echo "" >&2
        echo "  # Verify it will be tracked" >&2
        echo "  git ls-files $ccy_dockerfile" >&2
        echo "" >&2
        echo -e "${COLOR_BLUE}Safe to track:${COLOR_RESET}" >&2
        echo "  ✓ .claude/ccy/Dockerfile     (container configuration)" >&2
        echo "  ✓ .claude/ccy/.gitignore     (protection rules)" >&2
        echo "" >&2
        echo -e "${COLOR_RED}NEVER track:${COLOR_RESET}" >&2
        echo "  ✗ .claude/ccy/.last-launch.conf  (contains token names)" >&2
        echo "  ✗ .claude/ccy/*                  (session data)" >&2
        echo "" >&2
        echo -e "${COLOR_YELLOW}════════════════════════════════════════════════════════════════════════════════${COLOR_RESET}" >&2
        echo "" >&2

        # This is a warning, not a fatal error
        return 0
    fi

    # Dockerfile exists but is neither tracked nor ignored (unusual)
    # This can happen in a fresh repo before first commit
    return 0
}

# Project name extraction
# Uses parent-project format to avoid collisions (e.g., "ec-site" instead of just "site")
# Excludes generic parent folder names
get_project_name() {
    local project_dir parent_dir
    project_dir=$(basename "$(pwd)" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/_/g')
    parent_dir=$(basename "$(dirname "$(pwd)")" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9_-]/_/g')
    local generic_folders="projects|repos|work|src|code|dev|home"

    # Check if parent folder is NOT a generic name (case insensitive)
    if ! echo "$parent_dir" | grep -qiE "^($generic_folders)$"; then
        echo "${parent_dir}-${project_dir}"
    else
        echo "$project_dir"
    fi
}

# Check if command exists
command_exists() {
    command -v "$1" &> /dev/null
}

# CCY Token Management
# These functions work with the ccy token directory structure

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

# Container version validation and build helpers
# These functions work with Docker/Podman container labels for version tracking

# Validate container version and rebuild if needed
# Args: $1 = image name, $2 = Dockerfile path, $3 = REQUIRED_CONTAINER_VERSION
# Validates both version and hash (like CCY_VERSION/CCY_HASH pattern)
# Returns: 0 if container is up to date, 1 if rebuild needed
validate_container_version() {
    local image_name="$1"
    local dockerfile_path="$2"
    local required_version="$3"

    # Get version and hash from built image
    local image_version image_hash current_hash
    image_version=$(container_cmd image inspect "$image_name" \
        --format '{{index .Config.Labels "claude-yolo-version"}}' 2>/dev/null || echo "0")
    image_hash=$(container_cmd image inspect "$image_name" \
        --format '{{index .Config.Labels "claude-yolo-dockerfile-hash"}}' 2>/dev/null || echo "unknown")

    # Calculate current Dockerfile hash (16 char md5, like CCY_HASH)
    current_hash=$(md5sum "$dockerfile_path" | cut -d' ' -f1 | cut -c1-16)

    # Validate version and hash together (like CCY version validation)
    local version_match=false
    local hash_match=false

    [[ "$image_version" == "$required_version" ]] && version_match=true
    [[ "$image_hash" == "$current_hash" ]] && hash_match=true

    # Special case: "unknown" hash means image was built before hash tracking
    # This happens during migration from old system or Ansible builds without hash arg
    local is_migration=false
    [[ "$image_hash" == "unknown" ]] && is_migration=true

    if $version_match && $hash_match; then
        # Perfect match - container is up to date
        return 0

    elif $is_migration && $version_match; then
        # Migration from old system - rebuild without scary warning
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "ℹ️  Migrating to hash-based version tracking"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "Version: v$required_version"
        echo ""
        echo "Rebuilding container with hash tracking enabled..."
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        return 1

    elif $version_match && ! $hash_match; then
        # VERSION same but HASH different = Developer forgot to update version!
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "⚠️  DEVELOPER ERROR: Dockerfile modified without version bump" >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "Version:       $required_version (unchanged)" >&2
        echo "Image hash:    $image_hash" >&2
        echo "Current hash:  $current_hash" >&2
        echo "" >&2
        echo "The Dockerfile has been modified but claude-yolo-version was not updated." >&2
        echo "This is a developer error - version numbers must be incremented" >&2
        echo "when the Dockerfile changes to ensure proper deployment." >&2
        echo "" >&2
        echo "Rebuilding container automatically..." >&2
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" >&2
        echo "" >&2
        return 1

    elif ! $version_match; then
        # VERSION different = Normal upgrade
        echo ""
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "ℹ️  Container version update required"
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo "Image version: v$image_version"
        echo "Required:      v$required_version"
        echo ""
        echo "Rebuilding container with latest changes..."
        echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
        echo ""
        return 1
    fi
}

# Build container with hash
# Args: $1 = image name, $2 = dockerfile directory, $3 = additional flags (optional)
build_container_with_hash() {
    local image_name="$1"
    local dockerfile_dir="$2"
    local dockerfile_path="$dockerfile_dir/Dockerfile"
    # Optional 4th arg: if set, also build this image name using --target base.
    # Used to produce claude-yolo:base alongside claude-yolo:latest so project
    # Dockerfiles can extend the lean base and add only the LSPs they need.
    local base_image_name="${4:-}"

    # Collect optional extra flags as array (avoids unquoted expansion)
    local -a extra_flags=()
    [[ -n "${3:-}" ]] && extra_flags=("$3")

    # Calculate Dockerfile hash
    local dockerfile_hash
    dockerfile_hash=$(md5sum "$dockerfile_path" | cut -d' ' -f1 | cut -c1-16)

    # Build base stage first if requested (shares layers with full build below)
    if [[ -n "$base_image_name" ]]; then
        container_cmd build \
            "${extra_flags[@]}" \
            --build-arg DOCKERFILE_HASH="$dockerfile_hash" \
            --target base \
            -t "$base_image_name" \
            "$dockerfile_dir"
    fi

    # Build with hash as build arg (builds final stage: full image with LSPs)
    container_cmd build \
        "${extra_flags[@]}" \
        --build-arg DOCKERFILE_HASH="$dockerfile_hash" \
        -t "$image_name" \
        "$dockerfile_dir"
}

# Export functions for use in other scripts
export -f print_error
export -f is_git_repo
export -f check_git_repo
export -f get_project_name
export -f command_exists
export -f is_token_valid
export -f check_ccy_gitignore_safety

# Function to get next available container name for a project
# Args: $1 = project_name, $2 = suffix (e.g., "yolo" or "browser")
# Returns: Available container name (e.g., "myproject_yolo" or "myproject_yolo_2")
get_next_container_name() {
    local project_name="$1"
    local suffix="$2"
    local base_name="${project_name}_${suffix}"

    # Get all containers matching this project (including stopped/dead)
    # Using -a to prevent name collisions with stale containers from unclean shutdowns
    local all_names
    all_names=$(container_cmd ps -a --format '{{.Names}}')
    local existing_containers=""
    if echo "$all_names" | grep -q "^${base_name}"; then
        existing_containers=$(echo "$all_names" | grep "^${base_name}")
    fi

    # If no container exists, use base name (no suffix)
    if [ -z "$existing_containers" ]; then
        echo "$base_name"
        return
    fi

    # If base name (no suffix) is not in use, use it
    if ! echo "$existing_containers" | grep -q "^${base_name}$"; then
        echo "$base_name"
        return
    fi

    # Find next available number
    local max_num=1
    while echo "$existing_containers" | grep -q "^${base_name}_${max_num}$"; do
        max_num=$((max_num + 1))
    done

    echo "${base_name}_${max_num}"
}

export -f validate_container_version
export -f build_container_with_hash
export -f get_next_container_name

