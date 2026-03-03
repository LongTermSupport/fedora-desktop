#!/usr/bin/env bash
# Push localhost.yml to a private GitHub config repo for backup and restore
# on future installs.
#
# Usage: ./scripts/push-config.bash [--account <github-username>]
#
# The config repo is: <primary-github-username>/fedora-desktop-config (private)
# It is created automatically if it does not exist.
#
# Before pushing, validates that all sensitive variables in localhost.yml
# are Ansible Vault-encrypted. Refuses to push plain-text secrets.
#
# After pushing, verifies the file was written correctly by reading it back.

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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
LOCALHOST_YML="${PROJECT_ROOT}/environment/localhost/host_vars/localhost.yml"
CONFIG_REPO_NAME="fedora-desktop-config"
CONFIG_FILE_PATH="localhost.yml"

## ── Preflight ─────────────────────────────────────────────────────────────────

echo -e "${BOLD}Fedora Desktop — Push Personal Config${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

if ! command -v gh > /dev/null; then
    die "GitHub CLI (gh) is not installed. Run: sudo dnf install gh"
fi

if ! gh auth status; then
    die "Not authenticated with GitHub. Run: gh auth login"
fi

if [[ ! -f "$LOCALHOST_YML" ]]; then
    die "localhost.yml not found: $LOCALHOST_YML"
fi

## ── Account selection ─────────────────────────────────────────────────────────

# Parse --account flag
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

# Discover authenticated accounts — gh auth status writes to stderr
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
    # Validate the requested account is actually authenticated
    found=false
    for acct in "${auth_accounts[@]}"; do
        if [[ "$acct" == "$selected_account" ]]; then
            found=true
            break
        fi
    done
    if [[ "$found" == "false" ]]; then
        die "Account '$selected_account' is not authenticated. Authenticated: ${auth_accounts[*]}"
    fi
elif [[ ${#auth_accounts[@]} -eq 1 ]]; then
    selected_account="${auth_accounts[0]}"
    info "Using GitHub account: ${BOLD}${selected_account}${NC}"
else
    echo -e "\n${CYAN}Multiple GitHub accounts authenticated:${NC}"
    i=1
    for acct in "${auth_accounts[@]}"; do
        echo -e "  ${BOLD}${i})${NC} ${acct}"
        ((i++))
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

# Switch to the selected account
info "Switching to account: ${BOLD}${selected_account}${NC}"
gh auth switch --hostname github.com --user "$selected_account"
success "Active account: $selected_account"

config_repo="${selected_account}/${CONFIG_REPO_NAME}"

## ── Vault validation ──────────────────────────────────────────────────────────

echo
info "Validating vault encryption in localhost.yml..."

# Variable names that MUST be Ansible Vault-encrypted (not plain text).
# Match lines like:  some_secret_key: plainvalue
# A vaulted value looks like:  some_secret_key: !vault |
sensitive_pattern="(password|passwd|secret|token|api_key|api_secret|private_key|passphrase)"

plain_violations=()
while IFS= read -r line; do
    # Skip blank lines and comments
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    # Check if the line defines a sensitive var with a non-vault value
    if echo "$line" | grep -qiE "^\s*[a-z_]*(${sensitive_pattern})[a-z_]*\s*:"; then
        if ! echo "$line" | grep -q "!vault"; then
            plain_violations+=("  $line")
        fi
    fi
done < "$LOCALHOST_YML"

if [[ ${#plain_violations[@]} -gt 0 ]]; then
    echo -e "${RED}${CROSS} Sensitive variables must be Ansible Vault-encrypted before pushing:${NC}" >&2
    for v in "${plain_violations[@]}"; do
        echo -e "${RED}${v}${NC}" >&2
    done
    echo >&2
    echo -e "${YELLOW}${ARROW} Encrypt with:${NC}" >&2
    echo -e "   ansible-vault encrypt_string 'your-secret' --name 'var_name'" >&2
    die "Refusing to push plain-text secrets to GitHub"
fi

success "Vault validation passed — no plain-text secrets detected"

## ── Create repo if needed ─────────────────────────────────────────────────────

echo
info "Checking config repo: github.com/${config_repo}"

if gh repo view "$config_repo" --json name --jq '.name' > /dev/null; then
    success "Config repo exists: github.com/${config_repo}"
else
    info "Repo not found — creating private repo: ${config_repo}"
    gh repo create "$CONFIG_REPO_NAME" \
        --private \
        --description "fedora-desktop personal configuration for ${selected_account}"
    success "Created private repo: github.com/${config_repo}"
fi

## ── Push file ─────────────────────────────────────────────────────────────────

echo
info "Pushing localhost.yml to github.com/${config_repo}..."

# base64-encode the file content for the GitHub Contents API
file_content=$(base64 -w 0 "$LOCALHOST_YML")
commit_message="Update localhost.yml from $(hostname) on $(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Get current SHA if file already exists (required by the API for updates)
existing_sha=""
if existing_sha=$(gh api \
    "repos/${config_repo}/contents/${CONFIG_FILE_PATH}" \
    --jq '.sha' 2>/dev/null); then
    info "Updating existing file (sha: ${existing_sha:0:8}...)"
else
    info "Creating file for the first time"
fi

# Push via GitHub Contents API; redirect response JSON to /dev/null (it's large)
if [[ -n "$existing_sha" ]]; then
    gh api "repos/${config_repo}/contents/${CONFIG_FILE_PATH}" \
        -X PUT \
        -f message="$commit_message" \
        -f content="$file_content" \
        -f sha="$existing_sha" \
        > /dev/null
else
    gh api "repos/${config_repo}/contents/${CONFIG_FILE_PATH}" \
        -X PUT \
        -f message="$commit_message" \
        -f content="$file_content" \
        > /dev/null
fi

## ── Verify push ───────────────────────────────────────────────────────────────

echo
info "Verifying push was successful..."

# Read back from GitHub and compare with local file content
remote_content=$(gh api \
    "repos/${config_repo}/contents/${CONFIG_FILE_PATH}" \
    --jq '.content' \
    | base64 -d)

local_content=$(cat "$LOCALHOST_YML")

if [[ "$remote_content" != "$local_content" ]]; then
    die "Verification FAILED — remote content does not match local file. Check github.com/${config_repo}"
fi

success "Verification passed — remote content matches local file"

## ── Done ──────────────────────────────────────────────────────────────────────

echo
echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║              Config pushed successfully!                    ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
echo
echo -e "  ${ARROW} Repo:   ${BOLD}https://github.com/${config_repo}${NC}"
echo -e "  ${ARROW} File:   ${BOLD}${CONFIG_FILE_PATH}${NC}"
echo -e "  ${ARROW} Commit: ${BOLD}${commit_message}${NC}"
echo
echo -e "${CYAN}On a fresh install, run.bash will automatically pull this config${NC}"
echo -e "${CYAN}during setup, skipping the manual configuration prompts.${NC}"
echo
