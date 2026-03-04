#!/usr/bin/env bash
# Scan ~/Projects for git repos and push a reclone manifest to the private
# config repo so all projects can be restored on a fresh machine.
#
# Usage: ./fedora-install/push-projects.bash [--account <github-username>]
#
# The manifest is stored in: <username>/fedora-desktop-config/projects.manifest
# Run push-config.bash first to create the config repo.

set -euo pipefail
IFS=$'\n\t'

## ── Colors ────────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

CHECK="✓"
CROSS="✗"
ARROW="➜"
INFO="ℹ"
WARN="⚠"

## ── Helpers ───────────────────────────────────────────────────────────────────

die() {
    echo -e "${RED}${CROSS} ERROR: $*${NC}" >&2
    exit 1
}

info() {
    echo -e "${CYAN}${INFO} $*${NC}"
}

success() {
    echo -e "${GREEN}${CHECK} $*${NC}"
}

warning() {
    echo -e "${YELLOW}${WARN} $*${NC}"
}

## ── Paths ─────────────────────────────────────────────────────────────────────

PROJECTS_DIR="$HOME/Projects"
CONFIG_REPO_NAME="fedora-desktop-config"
MANIFEST_PATH="projects.manifest"

## ── Preflight ─────────────────────────────────────────────────────────────────

echo -e "${BOLD}Fedora Desktop — Push Projects Manifest${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

if ! command -v gh > /dev/null; then
    die "GitHub CLI (gh) is not installed. Run: sudo dnf install gh"
fi

if ! gh auth status; then
    die "Not authenticated with GitHub. Run: gh auth login"
fi

if [[ ! -d "$PROJECTS_DIR" ]]; then
    die "Projects directory not found: $PROJECTS_DIR"
fi

## ── Account selection ─────────────────────────────────────────────────────────

selected_account=""
while [[ $# -gt 0 ]]; do
    case "${1:-}" in
        --account|-a)
            selected_account="${2:-}"
            shift 2
            ;;
        *)
            die "Unknown argument: $1 (usage: $0 [--account <github-username>])"
            ;;
    esac
done

mapfile -t auth_accounts < <(
    gh auth status 2>&1 \
        | grep "Logged in to github.com account" \
        | grep -oE "account [^ ]+" \
        | awk '{print $2}'
)

if [[ ${#auth_accounts[@]} -eq 0 ]]; then
    die "No GitHub accounts found. Run: gh auth login"
fi

if [[ -n "$selected_account" ]]; then
    found=false
    for acct in "${auth_accounts[@]}"; do
        if [[ "$acct" == "$selected_account" ]]; then
            found=true
            break
        fi
    done
    if [[ "$found" == "false" ]]; then
        die "Account '$selected_account' not authenticated. Authenticated: ${auth_accounts[*]}"
    fi
elif [[ ${#auth_accounts[@]} -eq 1 ]]; then
    selected_account="${auth_accounts[0]}"
    echo -e "\n${CYAN}One GitHub account detected: ${BOLD}${selected_account}${NC}"
    read -rp "Use this account? [Y/n]: " _confirm
    if [[ "${_confirm,,}" == "n" ]]; then
        die "Aborted. Authenticate the correct account with: gh auth login"
    fi
else
    echo -e "\n${CYAN}Multiple GitHub accounts authenticated:${NC}"
    i=1
    for acct in "${auth_accounts[@]}"; do
        echo -e "  ${BOLD}${i})${NC} ${acct}"
        i=$((i + 1))
    done
    echo
    while true; do
        read -rp "Select account number (1-${#auth_accounts[@]}): " choice
        if [[ "$choice" =~ ^[0-9]+$ ]] && [[ "$choice" -ge 1 ]] && [[ "$choice" -le ${#auth_accounts[@]} ]]; then
            selected_account="${auth_accounts[$((choice-1))]}"
            break
        fi
        echo -e "${RED}Invalid choice. Enter a number between 1 and ${#auth_accounts[@]}${NC}"
    done
fi

gh auth switch --hostname github.com --user "$selected_account"
success "Active account: $selected_account"

config_repo="${selected_account}/${CONFIG_REPO_NAME}"

# Config repo must already exist (push-config.bash creates it)
if ! gh repo view "$config_repo" --json name --jq '.name' > /dev/null; then
    die "Config repo not found: github.com/${config_repo}\nRun push-config.bash first to create it."
fi

## ── Scan ~/Projects ───────────────────────────────────────────────────────────

echo
info "Scanning ${PROJECTS_DIR} for git repositories..."

tmp_manifest=$(mktemp)

# Write header
{
    printf '# fedora-desktop projects manifest\n'
    printf '# Generated: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf '# Format: <path-relative-to-%s><TAB><git-origin-url>\n' "$PROJECTS_DIR"
    printf '#\n'
} > "$tmp_manifest"

repo_count=0
skip_count=0

# Map GitHub org/user login → SSH key alias using ownership data.
# Strategy:
#   1. SSH-probe each ~/.ssh/github_* key to identify its GitHub username.
#      GitHub exits 1 even on auth success ("Hi USER!") — use if to capture output
#      regardless of exit code without hiding errors.
#   2. For each identified account, ask the GitHub API which orgs it OWNS
#      (role=owner on the membership, not merely member/admin-on-repos).
# This fixes the "first-key-wins" problem: git ls-remote succeeds with any key
# on public repos, so alphabetically-first keys were always selected.

declare -A _user_key_map  # github_username → key_alias
declare -A _org_key_map   # org_or_user_login → key_alias

info "Mapping SSH keys to GitHub accounts..."
for _kf in ~/.ssh/github_*; do
    [[ "$_kf" == *.pub ]] && continue
    [[ ! -f "$_kf" ]] && continue
    _kalias="${_kf#"$HOME/.ssh/github_"}"
    # GitHub always exits 1 even on success — use if to capture output safely
    _ssh_id=""
    if _ssh_id=$(ssh -i "$_kf" \
        -o IdentitiesOnly=yes -o BatchMode=yes \
        -o StrictHostKeyChecking=no -o ConnectTimeout=5 \
        git@github.com 2>&1); then :; fi
    # GitHub responds: "Hi USERNAME! You've successfully authenticated..."
    if [[ "$_ssh_id" =~ Hi\ ([^!]+)! ]]; then
        _gh_username="${BASH_REMATCH[1]}"
        _user_key_map["$_gh_username"]="$_kalias"
        _org_key_map["$_gh_username"]="$_kalias"
        info "  key:${_kalias} → @${_gh_username}"
    fi
done

info "Resolving org ownership via GitHub API..."
for _gh_user in "${!_user_key_map[@]}"; do
    _kalias="${_user_key_map[$_gh_user]}"
    gh auth switch --hostname github.com --user "$_gh_user" 2>/dev/null || continue
    while IFS= read -r _org_login; do
        [[ -z "$_org_login" ]] && continue
        # First org-owner mapped wins — org ownership (role=owner) is the actual creator/admin
        # of the org, not just someone with repo access granted by the owner
        if [[ -z "${_org_key_map[$_org_login]:-}" ]]; then
            _org_key_map["$_org_login"]="$_kalias"
            info "  org:${_org_login} → key:${_kalias}"
        fi
    done < <(gh api /user/memberships/orgs \
        --paginate \
        --jq '.[] | select(.role == "owner") | .organization.login' 2>/dev/null)
done

# Restore the account selected for this session
gh auth switch --hostname github.com --user "$selected_account"

# Look up the SSH key alias for a repo URL — pure map lookup, no SSH probing.
_find_key_alias() {
    local url="$1"
    local _url_path owner
    _url_path="${url##*:}"    # strip up to last colon → OWNER/repo.git
    owner="${_url_path%%/*}"  # strip from first slash → OWNER

    # Non-SSH URLs (local paths, https) — no key hint needed
    if [[ -z "$owner" ]] || [[ "$owner" == "$url" ]]; then
        echo ""
        return
    fi

    echo "${_org_key_map[$owner]:-}"
}

# Find all .git directories up to 6 levels deep (supports org/category/repo nesting)
while IFS= read -r git_dir; do
    repo_dir="${git_dir%/.git}"
    rel_path="${repo_dir#"${PROJECTS_DIR}/"}"

    if ! origin_url=$(git -C "$repo_dir" remote get-url origin 2>/dev/null); then
        warning "No origin remote: ${rel_path} — skipping"
        skip_count=$((skip_count + 1))
        continue
    fi

    # Convert GitHub HTTPS URLs to SSH format — avoids credential prompts on pull
    if [[ "$origin_url" == https://github.com/* ]]; then
        origin_url="git@github.com:${origin_url#https://github.com/}"
    fi

    key_alias=$(_find_key_alias "$origin_url")
    printf '%s\t%s\t%s\n' "$rel_path" "$origin_url" "$key_alias" >> "$tmp_manifest"
    info "  ${BOLD}${rel_path}${NC} ${ARROW} ${origin_url}${key_alias:+ [key: ${key_alias}]}"
    repo_count=$((repo_count + 1))
done < <(find "$PROJECTS_DIR" -maxdepth 6 -name ".git" -type d | sort)

echo
success "Found ${repo_count} repositories"
if [[ "$skip_count" -gt 0 ]]; then
    warning "${skip_count} skipped (no origin remote)"
fi

## ── Push manifest ─────────────────────────────────────────────────────────────

echo
info "Pushing ${MANIFEST_PATH} to github.com/${config_repo}..."

manifest_content=$(base64 -w 0 "$tmp_manifest")
commit_message="Update projects.manifest from $(hostname) on $(date -u +%Y-%m-%dT%H:%M:%SZ)"

existing_sha=""
if existing_sha=$(gh api \
    "repos/${config_repo}/contents/${MANIFEST_PATH}" \
    --jq '.sha' 2>/dev/null); then
    info "Updating existing manifest (sha: ${existing_sha:0:8}...)"
else
    info "Creating manifest for the first time"
fi

if [[ -n "$existing_sha" ]]; then
    gh api "repos/${config_repo}/contents/${MANIFEST_PATH}" \
        -X PUT \
        -f message="$commit_message" \
        -f content="$manifest_content" \
        -f sha="$existing_sha" \
        > /dev/null
else
    gh api "repos/${config_repo}/contents/${MANIFEST_PATH}" \
        -X PUT \
        -f message="$commit_message" \
        -f content="$manifest_content" \
        > /dev/null
fi

## ── Verify ────────────────────────────────────────────────────────────────────

echo
info "Verifying push..."

remote_content=$(gh api \
    "repos/${config_repo}/contents/${MANIFEST_PATH}" \
    --jq '.content' \
    | base64 -d)

local_content=$(cat "$tmp_manifest")
rm -f "$tmp_manifest"

if [[ "$remote_content" != "$local_content" ]]; then
    die "Verification FAILED — remote content does not match. Check github.com/${config_repo}"
fi

success "Verification passed"

## ── Done ──────────────────────────────────────────────────────────────────────

echo
echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║         Projects manifest pushed successfully!              ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
echo
echo -e "  ${ARROW} Repo:        ${BOLD}https://github.com/${config_repo}${NC}"
echo -e "  ${ARROW} Manifest:    ${BOLD}${MANIFEST_PATH}${NC}"
echo -e "  ${ARROW} Repos saved: ${BOLD}${repo_count}${NC}"
echo
echo -e "${CYAN}To reclone on a fresh machine:${NC}"
echo -e "  ${BOLD}./fedora-install/pull-projects.bash${NC}"
echo
