#!/usr/bin/env bash

## Setup
## !! BUMP THIS VERSION ON EVERY CHANGE TO THIS FILE — NO EXCEPTIONS !!
## !! If you forget, there is NO WAY to tell which version is running !!
RUN_BASH_VERSION="1.5.1"  # Cross-host config selection when no config for current host
set -e
set -u
set -o pipefail
IFS=$'\n\t'

# Safety net: always clean up sensitive temp files on exit
trap 'rm -f /tmp/.github_ssh_pp' EXIT

# Flags
OPTIONAL_ONLY=false
for _arg in "$@"; do
  case "$_arg" in
    --help|-h)
      cat <<'USAGE'
Usage: ./run.bash [OPTIONS]

Fedora Desktop Configuration Installer

Options:
  --optional-only   Skip core setup, jump straight to optional playbook menu
  -h, --help        Show this help message

First run:
  ./run.bash                Full install (system deps, SSH, GitHub, Ansible,
                            main playbook, then optional playbooks menu)

Subsequent runs:
  ./run.bash --optional-only  Re-run only the optional playbooks menu
                              (useful for adding components after initial setup)

Requirements:
  - Fedora Linux (version must match the branch)
  - Network connectivity (GitHub, DNF repos)
  - Must NOT be run as root (uses sudo internally)
USAGE
      exit 0
      ;;
    --optional-only)
      OPTIONAL_ONLY=true
      ;;
    *)
      echo "Unknown option: $_arg" >&2
      echo "Run './run.bash --help' for usage" >&2
      exit 1
      ;;
  esac
done

## Colors and formatting
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

## Unicode symbols
CHECK="✓"
CROSS="✗"
ARROW="➜"
INFO="ℹ"
WARN="⚠"
BUG="🐛"

## Step counter
STEP_CURRENT=0
STEP_TOTAL=13

## Assertions
if [[ "$(whoami)" == "root" ]];
then
  echo -e "\n${RED}${BOLD}${CROSS} ERROR${NC}"
  echo -e "${RED}Please do not run this as root${NC}\n"
  echo -e "Simply run as your normal user\n"
  exit 1
fi

# Header
clear
echo -e "${BLUE}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}${BOLD}║          FEDORA DESKTOP CONFIGURATION INSTALLER             ║${NC}"
echo -e "${BLUE}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
echo -e "  ${CYAN}run.bash v${RUN_BASH_VERSION}${NC}\n"

# Detect actual Fedora version (version check happens after repo clone)
fedora_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
echo -e "${CYAN}${INFO} Running on Fedora ${fedora_version}${NC}"

## Functions

title(){
  ((STEP_CURRENT++)) || true
  echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${CYAN}${BOLD}[$STEP_CURRENT/$STEP_TOTAL]${NC} ${BOLD}$1${NC}"
  echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

completed(){
  echo -e "${GREEN}${CHECK} Completed successfully${NC}"
}

info(){
  echo -e "${CYAN}${INFO} $1${NC}"
}

success(){
  echo -e "${GREEN}${CHECK} $1${NC}"
}

warning(){
  echo -e "${YELLOW}${WARN} $1${NC}"
}

error(){
  echo -e "${RED}${CROSS} $1${NC}"
}

wait_for_network(){
  info "Checking network connectivity..."
  local attempts=0
  local max_attempts=30
  while ! curl --silent --show-error --max-time 5 --output /dev/null https://github.com; do
    attempts=$((attempts + 1))
    if [[ $attempts -ge $max_attempts ]]; then
      echo -e "${RED}${CROSS} ERROR: No network connectivity after $max_attempts attempts${NC}" >&2
      echo -e "${YELLOW}${INFO} Please check your network connection and re-run this script${NC}" >&2
      exit 1
    fi
    echo -e "${YELLOW}${WARN} Network not ready (attempt $attempts/$max_attempts) — retrying in 2s...${NC}"
    sleep 2
  done
  success "Network connectivity confirmed"
}

# Abort the script if the given repo has uncommitted changes. run.bash
# does `git pull` at two points and a dirty working tree causes the pull
# to fail with an unhelpful error. Fail fast with a clear remediation.
assert_clean_worktree(){
  local dir="$1"
  local dirty
  dirty="$(git -C "$dir" status --porcelain)"
  if [[ -n "$dirty" ]]; then
    error "Working tree at $dir has uncommitted changes"
    echo -e "${YELLOW}${ARROW} run.bash needs a clean working tree before pulling updates.${NC}"
    echo -e "${YELLOW}${ARROW} Inspect:${NC}   ${BOLD}cd $dir && git status${NC}"
    echo -e "${YELLOW}${ARROW} Resolve by committing:${NC}"
    echo -e "     ${BOLD}git add -p && git commit${NC}"
    echo -e "${YELLOW}${ARROW} Or by temporarily stashing (remember to restore afterwards):${NC}"
    echo -e "     ${BOLD}git stash push -m 'pre-run.bash' && ./run.bash && git stash pop${NC}"
    exit 1
  fi
}

confirm(){
  local msg="$1"
  local yn=""
  echo
  echo -e "${YELLOW}${ARROW}${NC} $msg"
  while true; do
    read -rsp "   Press 'y' to confirm, 'n' to skip: " -n 1 yn
    echo
    if [[ "$yn" == "y" ]]; then
      echo -e "${GREEN}${CHECK} Confirmed${NC}\n"
      return 0
    elif [[ "$yn" == "n" ]]; then
      echo -e "${YELLOW}${INFO} Skipped${NC}\n"
      return 1
    else
      echo -e "${RED}${CROSS} Invalid input. Please press 'y' or 'n'${NC}"
    fi
  done
}

# Back up a config file with timestamp. No-op if file doesn't exist.
backup_config(){
  local config_file="$1"
  if [[ ! -f "$config_file" ]]; then
    return 0
  fi
  local backup_file
  backup_file="${config_file}.backup.$(date +%Y%m%d-%H%M%S)"
  cp "$config_file" "$backup_file"
  success "Config backed up to $(basename "$backup_file")"
}

# Selective config import: decode saved config, show top-level YAML keys,
# let user exclude some, write filtered result to output file.
# Sets _excluded_keys (comma-separated) for the caller to check.
selective_config_import(){
  local raw_b64="$1"
  local output_file="$2"
  local temp_config
  local temp_excluded
  temp_config=$(mktemp)
  temp_excluded=$(mktemp)
  printf '%s' "$raw_b64" | base64 -d > "$temp_config"

  python3 scripts/config_merge.py selective "$temp_config" "$output_file" "$temp_excluded"
  _excluded_keys=""
  if [[ -s "$temp_excluded" ]]; then
    _excluded_keys=$(cat "$temp_excluded")
  fi
  rm -f "$temp_config" "$temp_excluded"
}

# Merge remote config into local config interactively.
# Shows per-key diff: unchanged keys auto-keep, changed keys prompt L/R,
# new remote keys prompt A/S. Local-only keys always kept.
merge_config_import(){
  local raw_b64="$1"
  local local_file="$2"
  local temp_remote
  temp_remote=$(mktemp)
  printf '%s' "$raw_b64" | base64 -d > "$temp_remote"

  python3 scripts/config_merge.py merge "$local_file" "$temp_remote" "$local_file"
  rm -f "$temp_remote"
}

# Push local config to the per-host path in the config repo.
# Uses GitHub Contents API (create or update).
push_config_to_repo(){
  local config_file="$1"
  local repo="$2"
  local path="$3"
  local host_label="$4"

  local content_b64
  content_b64=$(base64 -w0 "$config_file")

  # Get existing file SHA if updating (not needed for first create)
  local existing_sha=""
  if existing_sha=$(gh api "repos/${repo}/contents/${path}" --jq '.sha' 2>/dev/null); then
    :  # SHA retrieved for update
  else
    existing_sha=""  # File doesn't exist yet — will create
  fi

  local -a api_args=(
    --method PUT
    --field "message=Update config from ${host_label}"
    --field "content=${content_b64}"
  )
  if [[ -n "$existing_sha" ]]; then
    api_args+=(--field "sha=${existing_sha}")
  fi

  gh api "repos/${repo}/contents/${path}" "${api_args[@]}" --silent
}

# Prompt for GitHub username(s) and write github_accounts YAML block to stdout.
# All user-facing prompts go to stderr so stdout is clean YAML for redirection.
prompt_github_accounts_yaml(){
  echo -e "\n${CYAN}${ARROW}${NC} Enter your GitHub username(s)" 1>&2
  echo -e "   These are the usernames you log into github.com with." 1>&2
  echo -e "   For multiple accounts, prefix each with a short alias and colon." 1>&2
  echo -e "" 1>&2
  echo -e "   ${BOLD}One account:${NC}      johndoe" 1>&2
  echo -e "   ${BOLD}Multiple accounts:${NC} personal:johndoe,work:johndoe-corp" 1>&2
  local github_accounts_raw _account_count _has_unaliased _alias
  github_accounts_raw="$(promptForValue 'GitHub username(s), comma separated')"

  _account_count=$(printf '%s' "$github_accounts_raw" | tr ',' '\n' | grep -c '[^[:space:]]')
  _has_unaliased=false
  while IFS= read -r pair; do
    pair="${pair// /}"
    [[ -z "$pair" ]] && continue
    if [[ "$pair" != *":"* ]]; then
      _has_unaliased=true
    fi
  done < <(printf '%s\n' "$github_accounts_raw" | tr ',' '\n')

  if [[ "$_has_unaliased" == "true" ]] && [[ "$_account_count" -gt 1 ]]; then
    error "Multiple accounts require aliases. Use format: alias:username,alias:username" 1>&2
    echo -e "   You entered: ${BOLD}${github_accounts_raw}${NC}" 1>&2
    echo -e "   Example:     ${BOLD}personal:user1,work:user2${NC}" 1>&2
    exit 1
  fi

  declare -A _seen_aliases=()
  while IFS= read -r pair; do
    pair="${pair// /}"
    if [[ "$pair" == *":"* ]]; then
      _alias="${pair%%:*}"
    elif [[ -n "$pair" ]]; then
      _alias="personal"
    else
      continue
    fi
    if [[ -n "${_seen_aliases[$_alias]:-}" ]]; then
      error "Duplicate alias '${_alias}' — each account needs a unique alias" 1>&2
      exit 1
    fi
    _seen_aliases[$_alias]=1
  done < <(printf '%s\n' "$github_accounts_raw" | tr ',' '\n')

  # Output clean YAML to stdout
  printf '# GitHub CLI accounts — to add more later: scripts/gh-account-setup.bash --add=alias:username\n'
  printf 'github_accounts:\n'
  while IFS= read -r pair; do
    pair="${pair// /}"
    if [[ "$pair" == *":"* ]]; then
      printf '  %s: "%s"\n' "${pair%%:*}" "${pair##*:}"
    elif [[ -n "$pair" ]]; then
      printf '  personal: "%s"\n' "$pair"
    fi
  done < <(printf '%s\n' "$github_accounts_raw" | tr ',' '\n')
}

# Function to sanitize sensitive data from error logs
sanitize_error_log(){
  local log_content="$1"
  local sanitized="$log_content"
  
  # Remove potential API keys, tokens, and secrets
  sanitized=$(echo "$sanitized" | sed -E 's/(api[_-]?key|token|secret|password|passwd|pwd)[[:space:]]*[:=][[:space:]]*[^[:space:]]+/\1=***REDACTED***/gi')
  
  # Remove email addresses
  sanitized=$(echo "$sanitized" | sed -E 's/[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}/***EMAIL***/g')
  
  # Remove IP addresses
  sanitized=$(echo "$sanitized" | sed -E 's/[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}/***IP***/g')
  
  # Remove SSH key fingerprints
  sanitized=$(echo "$sanitized" | sed -E 's/SHA256:[a-zA-Z0-9+/]+/SHA256:***FINGERPRINT***/g')
  
  # Remove home directory paths with actual username
  local current_user
  current_user="$(whoami)"
  # shellcheck disable=SC2001
  sanitized=$(echo "$sanitized" | sed "s|/home/$current_user|/home/***USER***|g")

  # Remove vault passwords and encrypted content
  # shellcheck disable=SC2016
  sanitized=$(echo "$sanitized" | sed -E 's/\$ANSIBLE_VAULT;[^[:space:]]+/\$ANSIBLE_VAULT;***ENCRYPTED***/g')
  
  echo "$sanitized"
}

# Function to check if Claude Code is available for enhanced sanitization
check_claude_code(){
  if command -v claude &> /dev/null; then
    return 0
  else
    return 1
  fi
}

# Function to create GitHub issue for failed playbook
create_github_issue(){
  local playbook_name="$1"
  local exit_code="$2"
  local error_log=""
  
  echo -e "\n${YELLOW}${BOLD}${BUG} Playbook Failure Detected${NC}"
  echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  
  # Check if we're in the git repo and have gh CLI
  if ! git rev-parse --git-dir > /dev/null 2>&1; then
    error "Not in a git repository. Cannot create issue."
    return 1
  fi
  
  if ! command -v gh &> /dev/null; then
    error "GitHub CLI not found. Cannot create issue."
    return 1
  fi
  
  # Get system information
  local fedora_version branch commit hostname kernel
  fedora_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
  branch=$(git branch --show-current)
  commit=$(git rev-parse --short HEAD)
  hostname=$(hostname)
  kernel=$(uname -r)
  
  # Ask user to paste error output
  echo -e "\n${CYAN}${ARROW} Please copy and paste the relevant error output from above${NC}"
  echo -e "${YELLOW}${INFO} Paste the error, then press Ctrl+D when done:${NC}\n"
  error_log=$(cat)
  
  # Sanitize the error log
  info "Sanitizing error log for sensitive data..."
  local sanitized_log
  sanitized_log=$(sanitize_error_log "$error_log")
  
  # If Claude Code is available, use it for enhanced sanitization
  if check_claude_code; then
    info "Using Claude Code for enhanced sensitive data removal..."
    local temp_file
    temp_file=$(mktemp)
    echo "$sanitized_log" > "$temp_file"

    # Ask Claude to sanitize the log further
    local claude_sanitized
    claude_sanitized=$(claude "Please remove any potentially sensitive information from this error log including passwords, API keys, tokens, personal data, private URLs, or system-specific paths that shouldn't be shared publicly. Return ONLY the sanitized version of the log, preserving the error messages and structure but with sensitive data replaced with placeholders like ***REDACTED***:\n\n$(cat "$temp_file")" 2>/dev/null || echo "")
    
    if [[ -n "$claude_sanitized" ]]; then
      # Use Claude's sanitized version
      sanitized_log="$claude_sanitized"
      success "Claude Code: Additional sensitive data removed"
      
      # Optional: Show what Claude changed
      if [[ "$VERBOSE" == "true" ]]; then
        info "Claude Code sanitization applied"
      fi
    else
      warning "Claude Code sanitization failed, using basic sanitization only"
    fi
    rm -f "$temp_file"
  fi
  
  # Prepare issue title and body
  local issue_title
  issue_title="[Automated] Playbook failure: $(basename "$playbook_name") on Fedora $fedora_version"
  
  local issue_body
  issue_body="## Playbook Failure Report

### Environment
- **Fedora Version**: $fedora_version
- **Branch**: $branch
- **Commit**: $commit
- **Hostname**: $hostname
- **Kernel**: $kernel
- **Date**: $(date -u +"%Y-%m-%d %H:%M:%S UTC")

### Failed Playbook
\`\`\`
$playbook_name
\`\`\`

### Exit Code
$exit_code

### Error Output
<details>
<summary>Click to expand error log</summary>

\`\`\`
$sanitized_log
\`\`\`

</details>

### Steps to Reproduce
1. Fresh Fedora $fedora_version installation
2. Run \`./run.bash\`
3. Playbook fails at: $playbook_name

### Additional Context
_This issue was automatically generated. The error log has been sanitized to remove potentially sensitive information._

---
_Generated by fedora-desktop automated error reporting_"
  
  # Show preview to user
  echo -e "\n${CYAN}${BOLD}Issue Preview${NC}"
  echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${BOLD}Title:${NC} $issue_title\n"
  echo -e "${BOLD}Body:${NC}"
  echo "$issue_body" | head -n 50
  echo -e "\n... [truncated for preview] ...\n"
  
  # Ask for confirmation
  if confirm "Do you want to create this GitHub issue?"; then
    info "Creating GitHub issue..."
    
    # Create the issue
    local issue_url
    if issue_url=$(${GH_REPO:-gh} issue create \
      --title "$issue_title" \
      --body "$issue_body" \
      --label "bug" \
      --label "automated" \
      2>&1); then
      success "Issue created successfully!"
      echo -e "${GREEN}${ARROW} View issue: $issue_url${NC}"
      return 0
    else
      error "Failed to create issue: $issue_url"
      return 1
    fi
  else
    info "Issue creation cancelled"
    return 1
  fi
}

# Function to run playbook with option to create issue on failure
run_playbook_with_issue_option(){
  local playbook="$1"
  local name="$2"
  local exit_code=0
  
  echo -e "\n${CYAN}${ARROW} Running: $name${NC}"
  
  # Run playbook normally with full colors
  if sudo -n true 2>/dev/null; then
    "$playbook"
    exit_code=$?
  else
    "$playbook" --ask-become-pass
    exit_code=$?
  fi
  
  if [[ $exit_code -eq 0 ]]; then
    success "Completed: $name"
    return 0
  else
    error "Failed: $name (exit code: $exit_code)"
    
    # Offer to create GitHub issue
    if confirm "Would you like to create a GitHub issue for this failure?"; then
      create_github_issue "$playbook" "$exit_code"
    fi
    
    return $exit_code
  fi
}

promptForValue(){
  local item v yn validate
  item="$1"
  validate="${2:-}"
  while true; do
    echo -e "\n${CYAN}${ARROW}${NC} Please enter your ${BOLD}$item${NC}:" 1>&2
    read -rp "   " v
    # Basic validation: must not be empty
    if [[ -z "${v// /}" ]]; then
      echo -e "   ${RED}${CROSS} Cannot be empty${NC}" 1>&2
      continue
    fi
    # Custom validation
    if [[ "$validate" == "email" ]] && [[ "$v" != *@*.* ]]; then
      echo -e "   ${RED}${CROSS} Must be a valid email address${NC}" 1>&2
      continue
    fi
    if [[ "$validate" == "min3" ]] && [[ "${#v}" -lt 3 ]]; then
      echo -e "   ${RED}${CROSS} Must be at least 3 characters${NC}" 1>&2
      continue
    fi
    echo -e "\n   You entered: ${BOLD}$v${NC}" 1>&2
    read -rsp "   Is this correct? (y/n): " -n 1 yn 1>&2
    echo 1>&2
    [[ "$yn" == "y" ]] && break
  done
  echo "$v"
}

## Process

if [[ "$OPTIONAL_ONLY" != "true" ]]; then

echo -e "\n${MAGENTA}${BOLD}Installation Process${NC}"
echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
wait_for_network

echo -e "\n${YELLOW}${INFO} You will be asked for your sudo password${NC}\n"
title "Installing System Dependencies"
info "Installing: git, python3, python3-libdnf5, grubby, jq, openssl, pipx"
sudo dnf -y install \
  git \
  python3 \
  python3-pip \
  python3-libdnf5 \
  grubby \
  jq \
  openssl \
  pipx > /dev/null 2>&1
completed

title "Checking for Legacy Grub Configurations"
info "Checking for old cgroup settings"
if sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy"; then
  warning "Found legacy cgroup configuration, removing..."
  sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=0"
  sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=1"
  
  # Verify the removal worked
  if sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy"; then
    error "Failed to remove cgroup configuration - may need manual intervention"
    echo -e "${YELLOW}${INFO} To manually remove, run:${NC}"
    echo -e "   sudo grubby --update-kernel=ALL --remove-args='systemd.unified_cgroup_hierarchy=0'"
  else
    success "Legacy cgroup configuration removed successfully"
  fi
else
  success "No legacy cgroup configuration found"
fi

title "Setting up Ansible Environment"
# sudo dnf install pipx (above) or the kickstart %post can create ~/.local owned
# by root, which causes pipx to fail with PermissionError on its log directory.
# Fix ownership before running pipx.
if [[ -d ~/.local ]] && [[ "$(stat -c%U ~/.local)" != "$(whoami)" ]]; then
    info "Fixing ~/.local ownership (was created by root)"
    sudo chown -R "$(id -u):$(id -g)" ~/.local
fi
mkdir -p ~/.local/bin ~/.local/share ~/.local/state
info "Installing Ansible and dependencies via pipx"
if pipx list --short | grep -q "ansible"; then
  success "Ansible already installed"
else
  pipx install --include-deps ansible
  pipx inject ansible jmespath
  pipx inject ansible passlib
  pipx inject ansible ansible-lint
fi
# Ensure ~/.local/bin exists, then force-create symlink
mkdir -p ~/.local/bin
ln -sf ~/.local/share/pipx/venvs/ansible/bin/ansible-lint ~/.local/bin/ansible-lint
completed

_ssh_key_password=""  # saved here, offered as default for github_ SSH keys later
title "Creating SSH Key Pair\n\nNOTE - you must set a password\n\nSuggest you use your login password"
if [[ ! -f ~/.ssh/id ]]; then
  while true; do
    read -rsp "Password: " password
    echo
    read -rsp "Password (confirm): " password2
    echo
    [ "$password" = "$password2" ] && break
    echo "Passwords not matched, please try again"
  done
  ssh-keygen -t ed25519 -f ~/.ssh/id -P "$password"
  _ssh_key_password="$password"
else
  echo " - found existing key"
fi
completed

title "Set Custom Hostname"
if [[ "$(hostname)" == "fedora" ]]; then
  echo "found default hostname, please choose a new one"
  echo "(your machine hostname, eg joseph-laptop, joseph-fedora etc)"
  read -rp "Hostname: " hostname
  sudo hostnamectl set-hostname "$hostname"
fi

title "Installing Github CLI"
sudo dnf -y install 'dnf-command(config-manager)'
# Check if gh-cli repo already exists before adding
if ! sudo dnf repolist | grep -q "gh-cli"; then
  sudo dnf config-manager addrepo --from-repofile=https://cli.github.com/packages/rpm/gh-cli.repo
else
  echo "GitHub CLI repository already configured"
fi
sudo dnf -y install gh
completed

title "GitHub Authentication Setup"
info "You will need to authenticate with your browser"
# Only add GH_HOST if not already present
if ! grep -q 'export GH_HOST="github.com"' ~/.bashrc; then
  echo 'export GH_HOST="github.com"' >> ~/.bashrc
fi

# Check the active gh token carries a required OAuth scope.
# Honours GitHub's scope hierarchy: admin:* implies write:* implies read:*,
# and `user` implies user:email/read:user/user:follow. A token granted
# admin:org therefore satisfies a read:org requirement.
# Anchored grep on '^X-Oauth-Scopes:' avoids matching the unrelated
# Access-Control-Expose-Headers line whose value lists the header name.
function ghCheckTokenPermission(){
  local permission="$1"
  local failSilent="${2:-false}"
  local gh_cmd="${GH_REPO:-gh}"
  local scopes_csv
  scopes_csv=",$($gh_cmd api -i user 2>/dev/null \
    | grep -i '^X-Oauth-Scopes:' \
    | sed 's/^[^:]*: //' \
    | tr -d ' \r' \
    | tr '\n' ','),"
  # Array, not space-separated string: this file sets IFS=$'\n\t' at the top
  # so unquoted $string would not word-split on spaces.
  local satisfiers=("$permission")
  case "$permission" in
    read:org)         satisfiers=("$permission" write:org admin:org) ;;
    write:org)        satisfiers=("$permission" admin:org) ;;
    read:public_key)  satisfiers=("$permission" write:public_key admin:public_key) ;;
    write:public_key) satisfiers=("$permission" admin:public_key) ;;
    read:repo_hook)   satisfiers=("$permission" write:repo_hook admin:repo_hook) ;;
    write:repo_hook)  satisfiers=("$permission" admin:repo_hook) ;;
    read:gpg_key)     satisfiers=("$permission" write:gpg_key admin:gpg_key) ;;
    write:gpg_key)    satisfiers=("$permission" admin:gpg_key) ;;
    read:user|user:email|user:follow) satisfiers=("$permission" user) ;;
  esac
  local s
  for s in "${satisfiers[@]}"; do
    if [[ "$scopes_csv" == *",${s},"* ]]; then
      echo " - found $permission permission"
      return 0
    fi
  done
  if [[ "$failSilent" == "true" ]]; then
    return 1
  fi
  echo " - missing $permission permission"
  echo "Please run this command ON THE MACHINE ITSELF, NOT REMOTELY"
  echo "    $gh_cmd auth refresh -h github.com -s '$permission'"
  return 1
}

if ! gh auth status > /dev/null 2>&1; then
  echo -e "\n${YELLOW}${BOLD}┌─────────────────────────────────────────────────┐${NC}"
  echo -e "${YELLOW}${BOLD}│                    IMPORTANT                    │${NC}"
  echo -e "${YELLOW}${BOLD}│   YOU MUST CHOOSE SSH WHEN ASKED FOR THE       │${NC}"
  echo -e "${YELLOW}${BOLD}│   PREFERRED PROTOCOL FOR GIT OPERATIONS        │${NC}"
  echo -e "${YELLOW}${BOLD}└─────────────────────────────────────────────────┘${NC}\n"

  read -rp "Confirm you will choose SSH for GitHub authentication (Y/n): " confirm_ssh
  if [[ "${confirm_ssh,,}" == "n" ]]; then
    error "SSH is required - please re-run and choose SSH"
    exit 1
  fi

  if ! gh auth login; then
    error "Failed to login to GitHub"
    echo -e "${YELLOW}${ARROW} Please try running 'gh auth login' manually${NC}"
    exit 1
  fi
  success "GitHub authentication successful"
else
  success "Already authenticated with GitHub"
fi
primary_gh_username="$(gh api user --jq '.login')"
success "Primary GitHub account: $primary_gh_username"
completed

# This repo lives in the LongTermSupport org. If a previous install set up
# multi-account wrappers (play-github-cli-multi.yml generates gh-<alias> bash
# functions), prefer gh-lts for operations that act on this repo — so we talk
# to GitHub as the LTS account even when a different account is the active
# default. Falls back to plain gh on fresh installs where the wrappers don't
# yet exist.
GH_REPO="gh"
_gh_aliases_file="$HOME/.bashrc-includes/gh-aliases.inc.bash"
if [[ -f "$_gh_aliases_file" ]]; then
  # shellcheck source=/dev/null
  source "$_gh_aliases_file"
  if declare -F gh-lts >/dev/null; then
    GH_REPO="gh-lts"
    info "Using gh-lts wrapper for LTS-org operations"
  fi
fi
export GH_REPO

title "Configuring GitHub SSH Access"
# Check if we have the required permission
if ! ghCheckTokenPermission "admin:public_key" > /dev/null 2>&1; then
  warning "Missing admin:public_key permission - requesting it now"
  $GH_REPO auth refresh -h github.com -s admin:public_key
fi

ssh_key_fingerprint=$(ssh-keygen -lf ~/.ssh/id.pub | awk '{print $2}')
# Use gh api to check for SSH keys without triggering signing key scope warning
if ! $GH_REPO api user/keys 2>/dev/null | grep -q "$ssh_key_fingerprint"; then
  # Add SSH key for authentication only (not signing)
  if $GH_REPO ssh-key add ~/.ssh/id.pub --title="$(hostname) Added by fedora-desktop setup script on $(date +%Y-%m-%d)" --type=authentication 2>&1; then
    success "SSH authentication key added to GitHub"
  else
    error "Failed to add SSH key to GitHub"
    echo -e "${YELLOW}${ARROW} Try manually adding your SSH key:${NC}"
    echo -e "   cat ~/.ssh/id.pub | $GH_REPO ssh-key add --title='$(hostname)' --type=authentication"
    exit 1
  fi
else
  success "SSH key already configured on GitHub"
fi
completed

title "Updating SSH Known Hosts"
info "Configuring GitHub host keys"
# Remove existing GitHub entries silently
ssh-keygen -R github.com &>/dev/null || true
# Add fresh GitHub host keys
curl -sL https://api.github.com/meta | jq -r '.ssh_keys | .[]' | sed -e 's/^/github.com /' >> ~/.ssh/known_hosts
success "GitHub host keys updated"
completed

title "Setting up Project Directory and Repository"
mkdir -p ~/Projects
if [[ ! -d ~/Projects/fedora-desktop ]]; then
  info "Cloning fedora-desktop repository"
  git clone https://github.com/LongTermSupport/fedora-desktop.git ~/Projects/fedora-desktop
  success "Repository cloned"
else
  info "Pulling latest changes"
  assert_clean_worktree ~/Projects/fedora-desktop
  # Use `command git` to bypass any git() bash wrapper function (e.g. from
  # gh-aliases.inc.bash). Wrappers that run subcommands and assign to vars
  # can propagate non-zero exits under `set -e` even when they're benign.
  command git -C ~/Projects/fedora-desktop pull
  success "Repository updated"
fi
cd ~/Projects/fedora-desktop

# Fail fast: verify Fedora version matches this branch
version_file=~/Projects/fedora-desktop/vars/fedora-version.yml
if [[ ! -f "$version_file" ]]; then
  error "Cannot find $version_file — repository may be corrupt"
  exit 1
fi
expected_version=$(grep "fedora_version:" "$version_file" | cut -d: -f2 | tr -d ' ')
if [[ "$fedora_version" != "$expected_version" ]]; then
  error "Fedora version mismatch"
  echo -e "   Expected: Fedora ${BOLD}$expected_version${NC} (from branch)"
  echo -e "   Actual:   Fedora ${BOLD}$fedora_version${NC}"
  echo -e "\n${YELLOW}${ARROW} Check out the correct branch for your Fedora version${NC}"
  exit 1
fi
success "Fedora version verified: $fedora_version matches branch"
completed


title "Loading Personal Configuration"
localhost_yml=~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml
config_repo="${primary_gh_username}/fedora-desktop-config"
config_hostname=$(hostname)
config_host_path="hosts/${config_hostname}.yml"

# Discover config repo and find best available config for this host.
# gh api returns non-zero when a resource doesn't exist — that's expected
# for probe-then-act checks, not an error to propagate.
has_config_repo=false
has_remote_config=false
raw_content=""
config_source_label=""

if gh api "repos/${config_repo}" --jq '.name' > /dev/null 2>/dev/null; then
  has_config_repo=true

  # Try host-specific config first
  if raw_content=$(gh api "repos/${config_repo}/contents/${config_host_path}" --jq '.content' 2>/dev/null); then
    has_remote_config=true
    config_source_label="${config_hostname}"
    info "Config found for this host (${config_hostname})"
  else
    # No config for this host — list available hosts and legacy file
    info "No saved config for ${config_hostname}"
    declare -a _available_sources=()
    declare -a _available_labels=()

    # Collect host-specific configs
    _host_list=$(gh api "repos/${config_repo}/contents/hosts" --jq '.[].name' 2>/dev/null) || _host_list=""
    if [[ -n "$_host_list" ]]; then
      while IFS= read -r _hfile; do
        _hname="${_hfile%.yml}"
        _available_sources+=("hosts/${_hfile}")
        _available_labels+=("${_hname}")
      done <<< "$_host_list"
    fi

    # Check for legacy localhost.yml
    if gh api "repos/${config_repo}/contents/localhost.yml" --jq '.sha' > /dev/null 2>/dev/null; then
      _available_sources+=("localhost.yml")
      _available_labels+=("localhost.yml (legacy)")
    fi

    if [[ ${#_available_sources[@]} -gt 0 ]]; then
      echo -e "\n   Available configs in repo:"
      for _i in "${!_available_labels[@]}"; do
        echo -e "     $(( _i + 1 ))) ${_available_labels[$_i]}"
      done
      read -rp "   Choose a config to use (number, or Enter to skip): " _src_choice
      if [[ -n "$_src_choice" ]] && [[ "$_src_choice" =~ ^[0-9]+$ ]]; then
        _src_idx=$(( _src_choice - 1 ))
        if [[ $_src_idx -ge 0 ]] && [[ $_src_idx -lt ${#_available_sources[@]} ]]; then
          _chosen_path="${_available_sources[$_src_idx]}"
          if raw_content=$(gh api "repos/${config_repo}/contents/${_chosen_path}" --jq '.content' 2>/dev/null); then
            has_remote_config=true
            config_source_label="${_available_labels[$_src_idx]}"
            info "Using config from ${config_source_label}"
          fi
        fi
      fi
    fi
  fi
else
  info "No config repo found at github.com/${config_repo}"
fi

# Present configuration source choice
echo -e "\n${CYAN}${ARROW}${NC} How would you like to configure this system?"
_option=1
if [[ "$has_remote_config" == "true" ]]; then
  echo -e "   ${_option}) Pull full saved configuration (${config_source_label})"
  _opt_pull=$_option
  (( _option++ ))
  echo -e "   ${_option}) Selective import (choose what to keep) (${config_source_label})"
  _opt_selective=$_option
  (( _option++ ))
fi
if [[ "$has_remote_config" == "true" ]] && [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
  echo -e "   ${_option}) Merge remote config into local (diff/merge per key)"
  _opt_merge=$_option
  (( _option++ ))
fi
if [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
  echo -e "   ${_option}) Keep existing local configuration"
  _opt_keep=$_option
  (( _option++ ))
fi
if [[ "$has_config_repo" == "true" ]] && [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
  echo -e "   ${_option}) Save local config to repo (as ${config_hostname})"
  _opt_push=$_option
  (( _option++ ))
fi
echo -e "   ${_option}) Configure fresh (enter details manually)"
_opt_fresh=$_option

read -rp "   Choice [1-${_option}]: " _config_choice

if [[ "$has_remote_config" == "true" ]] && [[ "${_config_choice}" == "${_opt_pull}" ]]; then
  backup_config "$localhost_yml"
  printf '%s' "$raw_content" | base64 -d > "$localhost_yml"
  success "Configuration pulled (${config_source_label})"

elif [[ "$has_remote_config" == "true" ]] && [[ "${_config_choice}" == "${_opt_selective:-}" ]]; then
  backup_config "$localhost_yml"
  selective_config_import "$raw_content" "$localhost_yml"
  success "Selective import (${config_source_label})"

  # Prompt for essential keys that were excluded
  if ! grep -q '^user_login:' "$localhost_yml"; then
    info "user_login was excluded — entering identity details"
    echo ""
    read -rp "   User login [$(whoami)]: " user_login
    user_login="${user_login:-$(whoami)}"
    if [[ ${#user_login} -lt 3 ]]; then
      error "User login must be at least 3 characters"
      exit 1
    fi
    read -rp "   Full name [${user_login}]: " user_name
    user_name="${user_name:-$user_login}"
    user_email="$(promptForValue 'email address' email)"
    {
      printf 'user_login: "%s"\n' "$user_login"
      printf 'user_name: "%s"\n' "$user_name"
      printf 'user_email: "%s"\n' "$user_email"
    } >> "$localhost_yml"
    success "Identity added to configuration"
  fi
  if ! grep -q '^github_accounts:' "$localhost_yml"; then
    info "github_accounts was excluded — entering GitHub accounts"
    prompt_github_accounts_yaml >> "$localhost_yml"
    success "GitHub accounts added to configuration"
  fi

elif [[ -n "${_opt_merge:-}" ]] && [[ "${_config_choice}" == "${_opt_merge}" ]]; then
  backup_config "$localhost_yml"
  merge_config_import "$raw_content" "$localhost_yml"
  success "Merge complete"

elif [[ -n "${_opt_keep:-}" ]] && [[ "${_config_choice}" == "${_opt_keep}" ]]; then
  success "Keeping existing localhost.yml"

elif [[ -n "${_opt_push:-}" ]] && [[ "${_config_choice}" == "${_opt_push}" ]]; then
  push_config_to_repo "$localhost_yml" "$config_repo" "$config_host_path" "$config_hostname"
  success "Configuration saved to github.com/${config_repo} (hosts/${config_hostname}.yml)"

elif [[ "${_config_choice}" == "${_opt_fresh}" ]]; then
  echo ""
  read -rp "   User login [$(whoami)]: " user_login
  user_login="${user_login:-$(whoami)}"
  if [[ ${#user_login} -lt 3 ]]; then
    error "User login must be at least 3 characters"
    exit 1
  fi
  read -rp "   Full name [${user_login}]: " user_name
  user_name="${user_name:-$user_login}"
  user_email="$(promptForValue 'email address' email)"

  {
    printf 'user_login: "%s"\n' "$user_login"
    printf 'user_name: "%s"\n' "$user_name"
    printf 'user_email: "%s"\n' "$user_email"
    prompt_github_accounts_yaml
  } > "$localhost_yml"

  success "Configuration written"
else
  error "Invalid choice: ${_config_choice}"
  exit 1
fi
completed

title "Ansible Vault Configuration"
vault_pass_file=~/Projects/fedora-desktop/vault-pass.secret
if grep -qF '!vault' "$localhost_yml" 2>/dev/null; then
  # localhost.yml has encrypted values — need the matching vault password
  if [[ -f "$vault_pass_file" ]] && [[ -s "$vault_pass_file" ]]; then
    # Test existing vault password against encrypted values
    if ansible localhost -c local -e "@$localhost_yml" -m debug -a "msg=vault_ok" \
       --vault-id "localhost@$vault_pass_file" 2>/dev/null | grep -q "vault_ok"; then
      success "Existing vault password verified"
    else
      error "Existing vault-pass.secret cannot decrypt localhost.yml"
      echo -e "   ${YELLOW}${ARROW}${NC} The file exists but the password is wrong."
      echo -e "   Enter the correct vault password (from your password manager):"
      read -rsp "   " vaultPass
      echo
      echo "$vaultPass" > "$vault_pass_file"
      chmod 600 "$vault_pass_file"
      success "Vault password updated"
    fi
  else
    echo -e "\n${CYAN}${ARROW}${NC} Your localhost.yml has vault-encrypted values."
    echo -e "   Enter your vault password (from your password manager):"
    read -rsp "   " vaultPass
    echo
    echo "$vaultPass" > "$vault_pass_file"
    chmod 600 "$vault_pass_file"
    success "Vault password configured"
  fi
elif [[ -f "$vault_pass_file" ]]; then
  success "Existing vault password found"
else
  info "Setting up Ansible vault"
  echo -e "\n${CYAN}${ARROW}${NC} Enter vault password (or leave blank to auto-generate):"
  read -rsp "   " vaultPass
  echo
  if [[ "" == "$vaultPass" ]]; then
    vaultPass="$(openssl rand -base64 32)"
    success "Generated new vault password"
  else
    success "Vault password configured"
  fi
  echo "$vaultPass" > "$vault_pass_file"
fi
completed

title "GitHub SSH Key Passphrase"
# GitHub SSH keys (github_*) are full account keys — they must be passphrase-protected.
# The passphrase is stored in the vault so the Ansible playbook can manage keys idempotently.
_github_ssh_passphrase=""

if grep -q 'github_ssh_passphrase:' "$localhost_yml" 2>/dev/null; then
  success "github_ssh_passphrase already configured in localhost.yml"
else
  info "GitHub SSH keys require a passphrase (these are full account keys, not deploy keys)"
  echo
  if [[ -n "$_ssh_key_password" ]]; then
    echo -e "${CYAN}${ARROW}${NC} Use the same password as your main SSH key (~/.ssh/id) for all GitHub keys?"
    read -rsp "   Press 'y' to use same, 'n' to enter a different one: " -n 1 _yn
    echo
    if [[ "${_yn,,}" == "y" ]]; then
      _github_ssh_passphrase="$_ssh_key_password"
      success "Using same password as ~/.ssh/id"
    fi
  fi

  if [[ -z "$_github_ssh_passphrase" ]]; then
    info "Hint: your login password is a convenient choice"
    while true; do
      read -rsp "   GitHub SSH keys passphrase: " _github_ssh_passphrase
      echo
      read -rsp "   Confirm passphrase: " _confirm_passphrase
      echo
      [[ "$_github_ssh_passphrase" == "$_confirm_passphrase" ]] && break
      echo -e "${RED}${CROSS} Passphrases do not match — try again${NC}"
    done
  fi

  info "Encrypting github_ssh_passphrase and saving to vault..."
  # printf avoids trailing newline that echo adds — passphrase must be exact
  _encrypted=$(printf '%s' "$_github_ssh_passphrase" | ansible-vault encrypt_string \
    --stdin-name 'github_ssh_passphrase')
  printf '\n%s\n' "$_encrypted" >> "$localhost_yml"
  success "github_ssh_passphrase saved to localhost.yml (vault-encrypted)"
fi
completed

title "Setting Up GitHub Multi-Account Access"
if grep -q 'github_accounts' "$localhost_yml" 2>/dev/null; then
  # gh-account-setup.bash handles: per-account gh auth, OAuth scope audit,
  # SSH key generation, programmatic key upload (gh ssh-key add), and
  # isolated SSH verification. Pass passphrase via env if already in memory
  # (fresh-install path); otherwise the script decrypts from vault itself.
  GITHUB_SSH_PASSPHRASE="${_github_ssh_passphrase:-}" \
  LOCALHOST_YML="$localhost_yml" \
  VAULT_PASS_FILE="$vault_pass_file" \
    ./scripts/gh-account-setup.bash --setup-all
else
  success "Single account setup — no additional accounts to authenticate"
fi
completed

title "Running Ansible Playbooks"
info "Pulling latest changes before running playbooks"
assert_clean_worktree ~/Projects/fedora-desktop
# See note above on `command git` — bypass any sourced git() wrapper.
command git pull
success "Repository up to date"

info "Installing Ansible requirements"
ansible-galaxy install -r requirements.yml > /dev/null 2>&1
success "Requirements installed"

info "Executing main configuration playbook"
echo -e "${YELLOW}${INFO} This may take several minutes...${NC}\n"

# Run main playbook normally with full colors
main_exit_code=0

if sudo -n true 2>/dev/null; then
  ./playbooks/playbook-main.yml
  main_exit_code=$?
else
  echo -e "${YELLOW}${INFO} You will be prompted for your sudo password${NC}"
  ./playbooks/playbook-main.yml --ask-become-pass
  main_exit_code=$?
fi

if [[ $main_exit_code -eq 0 ]]; then
  completed
else
  error "Main playbook failed with exit code: $main_exit_code"
  
  # Offer to create GitHub issue
  if confirm "Would you like to create a GitHub issue for this failure?"; then
    create_github_issue "./playbooks/playbook-main.yml" "$main_exit_code"
  fi
  
  # Ask if user wants to continue despite failure
  if ! confirm "Do you want to continue with optional playbooks despite the main playbook failure?"; then
    error "Installation aborted due to main playbook failure"
    exit $main_exit_code
  fi
fi

## ── Restore Projects ─────────────────────────────────────────────────────────

title "Restoring Projects"
_pull_projects_script=~/Projects/fedora-desktop/fedora-install/pull-projects.bash
if [[ -f "$_pull_projects_script" ]]; then
  if confirm "Would you like to restore projects from your config repo manifest?"; then
    if ! "$_pull_projects_script" --account "$primary_gh_username"; then
      warning "Projects restore failed or no manifest found — continuing"
    fi
  fi
else
  warning "pull-projects.bash not found — skipping project restore"
fi

echo -e "\n${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║              MAIN INSTALLATION COMPLETE!                    ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

fi # end: OPTIONAL_ONLY skip block

## Optional Playbooks Menu System

# Function to run a playbook (wrapper for backward compatibility)
run_playbook() {
  run_playbook_with_issue_option "$1" "$2"
}

# Parse a space/comma-separated list of numbers; print valid ones (one per line)
_parse_number_list() {
  local input="$1"
  local max="$2"
  local -a tokens
  # Global IFS=$'\n\t' excludes spaces, so use explicit IFS for splitting
  IFS=' ,' read -ra tokens <<< "$input"
  for n in "${tokens[@]}"; do
    [[ -z "$n" ]] && continue
    if [[ "$n" =~ ^[0-9]+$ ]] && [[ "$n" -ge 1 ]] && [[ "$n" -le "$max" ]]; then
      echo "$n"
    else
      warning "Ignoring invalid number: $n (valid range: 1-$max)" >&2
    fi
  done
}

# Function to display menu
show_menu() {
  local category="$1"
  shift
  local playbooks=("$@")
  local choice

  while true; do
    echo -e "\n${CYAN}${BOLD}$category Playbooks${NC}"
    echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

    local i=1
    for pb in "${playbooks[@]}"; do
      local name
      name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
      echo -e "  ${BOLD}$i)${NC} $name"
      ((i++))
    done
    echo -e "  ${BOLD}A)${NC} Run all"
    echo -e "  ${BOLD}W)${NC} Whitelist — enter numbers to run (e.g. 1 3 5 or 1,3,5)"
    echo -e "  ${BOLD}B)${NC} Blacklist — enter numbers to skip, run all others"
    echo -e "  ${BOLD}S)${NC} Skip to next section"
    echo -e "  ${BOLD}Q)${NC} Quit optional installations"

    echo
    read -rp "Enter your choice: " choice

    case "$choice" in
      [1-9]|[1-9][0-9])
        if [[ $choice -le ${#playbooks[@]} ]]; then
          local selected="${playbooks[$((choice-1))]}"
          local name
          name=$(basename "$selected" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          run_playbook "$selected" "$name"
        else
          error "Invalid selection: $choice (choose 1-${#playbooks[@]})"
          echo -e "${YELLOW}${ARROW} Please try again${NC}\n"
          sleep 1
        fi
        ;;
      [Aa])
        for pb in "${playbooks[@]}"; do
          local name
          name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          run_playbook "$pb" "$name"
        done
        break
        ;;
      [Ww])
        read -rp "Enter numbers to run (space or comma separated): " _wl_input
        mapfile -t _wl_nums < <(_parse_number_list "$_wl_input" "${#playbooks[@]}")
        if [[ ${#_wl_nums[@]} -eq 0 ]]; then
          warning "No valid numbers entered — try again"
        else
          for n in "${_wl_nums[@]}"; do
            local pb="${playbooks[$((n-1))]}"
            local name
            name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
            run_playbook "$pb" "$name"
          done
          break
        fi
        ;;
      [Bb])
        read -rp "Enter numbers to skip (space or comma separated, Enter to run all): " _bl_input
        mapfile -t _bl_nums < <(_parse_number_list "$_bl_input" "${#playbooks[@]}")
        local _idx=1
        for pb in "${playbooks[@]}"; do
          local _skip=false
          for n in "${_bl_nums[@]}"; do
            if [[ "$_idx" -eq "$n" ]]; then
              _skip=true
              break
            fi
          done
          if [[ "$_skip" == "false" ]]; then
            local name
            name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
            run_playbook "$pb" "$name"
          fi
          ((_idx++))
        done
        break
        ;;
      [Ss])
        break
        ;;
      [Qq])
        return 1
        ;;
      *)
        error "Invalid choice: '$choice'"
        echo -e "${YELLOW}${ARROW} Please enter a number, A, W, B, S, or Q${NC}\n"
        sleep 1
        ;;
    esac
  done
  return 0
}

# Hardware detection function
check_hardware() {
  local playbook="$1"
  local pb_name
  pb_name=$(basename "$playbook")
  
  case "$pb_name" in
    *nvidia*)
      if lspci 2>/dev/null | grep -qi nvidia; then
        echo "${GREEN}[RECOMMENDED]${NC}"
      elif lsmod 2>/dev/null | grep -qi nouveau; then
        echo "${YELLOW}[MAYBE]${NC}"
      else
        echo "${RED}[NOT DETECTED]${NC}"
      fi
      ;;
    *displaylink*)
      if lsusb 2>/dev/null | grep -qi displaylink; then
        echo "${GREEN}[RECOMMENDED]${NC}"
      else
        echo "${YELLOW}[MANUAL CHECK]${NC}"
      fi
      ;;
    *tlp*|*battery*)
      if [[ -d /sys/class/power_supply/BAT0 ]] || [[ -d /sys/class/power_supply/BAT1 ]]; then
        echo "${GREEN}[RECOMMENDED]${NC}"
      else
        echo "${RED}[DESKTOP]${NC}"
      fi
      ;;
    *)
      echo "${YELLOW}[CHECK MANUALLY]${NC}"
      ;;
  esac
}

# Optional Playbooks Section
echo -e "\n${MAGENTA}${BOLD}Optional Configurations${NC}"
echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

if [[ "$OPTIONAL_ONLY" == "true" ]] || confirm "Would you like to install optional components?"; then
  cd ~/Projects/fedora-desktop

  # Playbooks that always run without prompting (auto-run before the interactive menu)
  # NOTE: play-speech-to-text.yml removed pending GPU/CPU split — see issue #11
  auto_run_common=()

  # Common optional playbooks
  if [[ -d playbooks/imports/optional/common ]]; then
    mapfile -t common_playbooks < <(find playbooks/imports/optional/common -name "*.yml" -type f | sort)
    if [[ ${#common_playbooks[@]} -gt 0 ]]; then

      # Auto-run whitelisted playbooks first (no prompt)
      menu_playbooks=()
      for pb in "${common_playbooks[@]}"; do
        pb_base=$(basename "$pb")
        _is_auto=false
        for _auto_name in "${auto_run_common[@]}"; do
          if [[ "$pb_base" == "$_auto_name" ]]; then
            _is_auto=true
            break
          fi
        done
        if [[ "$_is_auto" == "true" ]]; then
          name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          info "Auto-running: $name"
          run_playbook "$pb" "$name"
        else
          menu_playbooks+=("$pb")
        fi
      done

      # Interactive menu for the rest
      if [[ ${#menu_playbooks[@]} -gt 0 ]]; then
        info "Found ${#menu_playbooks[@]} common optional playbooks"
        if ! show_menu "Common Optional" "${menu_playbooks[@]}"; then
          info "Skipping remaining optional installations"
        fi
      fi
    fi
  fi

  # Hardware-specific playbooks — auto-run detected, skip undetected, prompt for uncertain
  if [[ -d playbooks/imports/optional/hardware-specific ]]; then
    mapfile -t hw_playbooks < <(find playbooks/imports/optional/hardware-specific -name "*.yml" -type f | sort)
    if [[ ${#hw_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${CYAN}${BOLD}Hardware-Specific Playbooks${NC}"
      echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      info "Analyzing your hardware..."

      hw_auto=()
      hw_prompt=()
      for pb in "${hw_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        hw_status=$(check_hardware "$pb")
        if echo "$hw_status" | grep -q "RECOMMENDED"; then
          hw_auto+=("$pb")
          success "Hardware detected — will auto-run: $name"
        elif echo "$hw_status" | grep -q "NOT DETECTED\|DESKTOP"; then
          info "Hardware not detected — skipping: $name"
        else
          hw_prompt+=("$pb")
          warning "Needs manual check: $name $hw_status"
        fi
      done

      # Auto-run recommended hardware playbooks
      for pb in "${hw_auto[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        run_playbook "$pb" "$name"
      done

      # Prompt for uncertain hardware playbooks
      if [[ ${#hw_prompt[@]} -gt 0 ]]; then
        if confirm "Would you like to configure hardware-specific components that need manual checking?"; then
          show_menu "Hardware-Specific" "${hw_prompt[@]}"
        fi
      fi
    fi
  fi
  
  # Untested playbooks warning
  if [[ -d playbooks/imports/optional/untested ]]; then
    mapfile -t untested_playbooks < <(find playbooks/imports/optional/untested -name "*.yml" -type f | sort)
    if [[ ${#untested_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${RED}${BOLD}⚠ UNTESTED Playbooks (Fedora $fedora_version)${NC}"
      echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      error "Found ${#untested_playbooks[@]} untested playbooks for Fedora $fedora_version"
      echo -e "${RED}${BOLD}These have NOT been tested on Fedora $fedora_version and may fail:${NC}"
      for pb in "${untested_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        echo -e "  ${RED}⚠${NC} $name"
      done
      echo -e "\n${YELLOW}These require careful manual testing before use.${NC}"
      
      if confirm "Would you like to attempt running untested playbooks? (NOT RECOMMENDED)"; then
        echo -e "${RED}${BOLD}WARNING: These playbooks may fail or cause issues!${NC}"
        show_menu "Untested (USE WITH CAUTION)" "${untested_playbooks[@]}"
      fi
    fi
  fi
  
  # Experimental playbooks warning
  if [[ -d playbooks/imports/optional/experimental ]]; then
    mapfile -t exp_playbooks < <(find playbooks/imports/optional/experimental -name "*.yml" -type f | sort)
    if [[ ${#exp_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${YELLOW}${BOLD}⚠ Experimental Playbooks${NC}"
      echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      warning "Found ${#exp_playbooks[@]} experimental playbooks"
      echo -e "${YELLOW}These are experimental and should only be run if you know what you're doing:${NC}"
      for pb in "${exp_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        echo -e "  ${YELLOW}•${NC} $name"
      done
      echo -e "\n${YELLOW}Run these manually if needed: ${BOLD}./playbooks/imports/optional/experimental/play-*.yml${NC}"
    fi
  fi
fi

# Final completion message
echo -e "\n${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║                    ALL DONE!                                ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

echo -e "${CYAN}${BOLD}Optional next steps${NC} (run after reboot):"
echo -e "  ${ARROW} Optional setup scripts (rclone cloud storage, etc.):"
echo -e "    ${BOLD}cd ~/Projects/fedora-desktop${NC}"
echo -e "    ${BOLD}./scripts/setup.bash${NC}"
echo -e "  ${ARROW} Python development environment (pyenv + pyenv versions):"
echo -e "    ${BOLD}./playbooks/imports/optional/common/play-python.yml${NC}"
echo

# Mark setup complete so the GNOME autostart doesn't re-fire after reboot
mkdir -p ~/.local/state
touch ~/.local/state/fedora-desktop-setup-complete

title "System Reboot"
warning "A reboot is recommended to complete the configuration"
if confirm "Ready to reboot now?"; then
  echo -e "${YELLOW}${INFO} Rebooting system...${NC}"
  sudo reboot now
else
  success "Installation complete!"
  echo -e "${YELLOW}${INFO} Remember to reboot your system when convenient${NC}"
  exit 0
fi
