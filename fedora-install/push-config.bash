#!/usr/bin/env bash
# Push localhost.yml to a private GitHub config repo for backup and restore
# on future installs.
#
# Usage: ./scripts/push-config.bash [--account <github-username>]
#
# The config repo is: <primary-github-username>/fedora-desktop-config (private)
# It is created automatically if it does not exist.
#
# Before pushing, automatically encrypts any plain-text sensitive variables
# using ansible-vault, then verifies all vault values decrypt cleanly.
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

## ── Vault validation and auto-encryption ──────────────────────────────────────

echo
info "Checking vault encryption in localhost.yml..."

vault_pass_file="${PROJECT_ROOT}/vault-pass.secret"
if [[ ! -f "$vault_pass_file" ]]; then
    die "Vault password file not found: ${vault_pass_file}\nRun the main Ansible playbook first to initialise your vault."
fi

# Variable names that MUST be Ansible Vault-encrypted (not plain text)
sensitive_pattern="(password|passwd|secret|token|api_key|api_secret|private_key|passphrase)"

# Collect plain-text violations
plain_violations=()
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^[[:space:]]*# ]] && continue
    if echo "$line" | grep -qiE "^\s*[a-z_]*(${sensitive_pattern})[a-z_]*\s*:"; then
        if ! echo "$line" | grep -q "!vault"; then
            plain_violations+=("$line")
        fi
    fi
done < "$LOCALHOST_YML"

if [[ ${#plain_violations[@]} -gt 0 ]]; then
    warning "${#plain_violations[@]} plain-text secret(s) found — encrypting in-place..."
    echo

    for orig_line in "${plain_violations[@]}"; do
        # Extract variable name: strip leading whitespace, take key before first colon
        var_name=$(echo "$orig_line" | sed 's/^[[:space:]]*//' | cut -d: -f1 | sed 's/[[:space:]]*$//')
        # Extract plain value: everything after 'key: ' (standard YAML format)
        plain_value="${orig_line#*: }"

        info "Encrypting: ${BOLD}${var_name}${NC}"

        # Encrypt and write vault block to temp file
        tmp_enc=$(mktemp)
        ansible-vault encrypt_string \
            --vault-id "localhost@${vault_pass_file}" \
            "$plain_value" \
            --name "$var_name" > "$tmp_enc" 2>/dev/null

        # Replace the plain-text line with the vault block in-place
        python3 - "$var_name" "$LOCALHOST_YML" "$tmp_enc" <<'PYEOF'
import sys, re

var_name = sys.argv[1]
yaml_file = sys.argv[2]
enc_file  = sys.argv[3]

with open(enc_file) as f:
    enc_block = f.read().rstrip('\n')

with open(yaml_file) as f:
    content = f.read()

# Match the plain-text YAML line for this variable (captures leading indentation)
pattern = r'^([ \t]*)' + re.escape(var_name) + r'[ \t]*:[ \t]*(?!!vault).*$'

def replace(m):
    indent = m.group(1)
    return '\n'.join(indent + ln for ln in enc_block.split('\n'))

new_content, count = re.subn(pattern, replace, content, count=1, flags=re.MULTILINE)
if count == 0:
    print(f'ERROR: could not locate plain-text line for {var_name}', file=sys.stderr)
    sys.exit(1)

with open(yaml_file, 'w') as f:
    f.write(new_content)
PYEOF

        rm -f "$tmp_enc"
        success "Encrypted: ${var_name}"
    done

    echo
    success "All ${#plain_violations[@]} plain-text secret(s) encrypted in localhost.yml"
else
    success "No plain-text secrets detected"
fi

## ── Verify vault decryption ───────────────────────────────────────────────────

echo
info "Verifying all vault-encrypted values decrypt cleanly..."
if ansible localhost -i "localhost," -c local \
    --vault-id "localhost@${vault_pass_file}" \
    -m debug -a "msg=ok" \
    -e "@${LOCALHOST_YML}" > /dev/null; then
    success "Vault decryption verified — all values decrypt cleanly"
else
    die "Vault decryption check failed — vault password may be wrong or values corrupted\nCheck: ${vault_pass_file}"
fi

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
