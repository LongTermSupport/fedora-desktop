#!/usr/bin/env bash
# Reclone all git repos from the projects manifest in the private config repo.
# Safe to re-run — already-cloned repos are skipped.
#
# Usage: ./fedora-install/pull-projects.bash [--account <github-username>]
#
# Reads: <username>/fedora-desktop-config/projects.manifest
# SSH aliases in remote URLs (e.g. git@github.com-work:org/repo) are preserved,
# so multi-account git works automatically once SSH keys are configured.

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

echo -e "${BOLD}Fedora Desktop — Pull/Reclone Projects${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

if ! command -v gh > /dev/null; then
    die "GitHub CLI (gh) is not installed. Run: sudo dnf install gh"
fi

if ! command -v git > /dev/null; then
    die "git is not installed. Run: sudo dnf install git"
fi

if ! gh auth status; then
    die "Not authenticated with GitHub. Run: gh auth login"
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

## ── Fetch manifest ────────────────────────────────────────────────────────────

echo
info "Fetching ${MANIFEST_PATH} from github.com/${config_repo}..."

if ! manifest_content=$(gh api \
    "repos/${config_repo}/contents/${MANIFEST_PATH}" \
    --jq '.content' 2>/dev/null | base64 -d); then
    die "Manifest not found in github.com/${config_repo}\nRun push-projects.bash first to generate it."
fi

# Count non-comment, non-blank lines
total=0
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    [[ "$line" =~ ^# ]] && continue
    total=$((total + 1))
done <<< "$manifest_content"

info "Manifest contains ${total} repositories"

## ── Reclone ───────────────────────────────────────────────────────────────────

echo
info "Recloning into ${PROJECTS_DIR}..."
mkdir -p "$PROJECTS_DIR"

cloned=0
skipped=0
failed=0
failed_repos=()

while IFS=$'\t' read -r rel_path origin_url; do
    # Skip blank lines and comments
    [[ -z "$rel_path" ]] && continue
    [[ "$rel_path" =~ ^# ]] && continue

    target="${PROJECTS_DIR}/${rel_path}"

    if [[ -d "${target}/.git" ]]; then
        info "Already exists: ${BOLD}${rel_path}${NC} — skipping"
        skipped=$((skipped + 1))
        continue
    fi

    info "Cloning: ${BOLD}${rel_path}${NC}"
    info "  ${ARROW} ${origin_url}"
    mkdir -p "$(dirname "$target")"
    if git clone "$origin_url" "$target" 2>&1; then
        success "Cloned: ${rel_path}"
        cloned=$((cloned + 1))
    else
        warning "Failed: ${rel_path} (${origin_url})"
        failed_repos+=("${rel_path}: ${origin_url}")
        failed=$((failed + 1))
    fi
done <<< "$manifest_content"

## ── Done ──────────────────────────────────────────────────────────────────────

echo
echo -e "${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║              Projects reclone complete                      ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
echo
echo -e "  ${ARROW} Cloned:  ${BOLD}${cloned}${NC}"
echo -e "  ${ARROW} Skipped: ${BOLD}${skipped}${NC} (already existed)"
echo -e "  ${ARROW} Failed:  ${BOLD}${failed}${NC}"
echo
if [[ "$cloned" -gt 0 ]]; then
    echo -e "${CYAN}Note: SSH aliases in remote URLs (e.g. github.com-work) require${NC}"
    echo -e "${CYAN}multi-account SSH keys to be configured. Run:${NC}"
    echo -e "  ${BOLD}./playbooks/imports/optional/common/play-github-cli-multi.yml${NC}"
    echo
fi
if [[ ${#failed_repos[@]} -gt 0 ]]; then
    echo -e "${YELLOW}${WARN} Failed repos (check SSH keys / access rights):${NC}"
    for _repo in "${failed_repos[@]}"; do
        echo -e "  ${RED}${CROSS}${NC} ${_repo}"
    done
    echo
    exit 1
fi
