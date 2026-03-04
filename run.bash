#!/usr/bin/env bash

## Setup
set -e
set -u
set -o pipefail
IFS=$'\n\t'

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
echo -e "${BLUE}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

# Check Fedora version matches expected version from config
echo -e "${CYAN}${INFO} Checking system requirements...${NC}"
if [[ -f ./vars/fedora-version.yml ]]; then
  expected_version=$(grep "fedora_version:" ./vars/fedora-version.yml | cut -d: -f2 | tr -d ' ')
  actual_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
  
  if [[ "$actual_version" != "$expected_version" ]]; then
    echo -e "${RED}${BOLD}${CROSS} ERROR - Fedora version mismatch${NC}"
    echo -e "   Expected: Fedora ${BOLD}$expected_version${NC}"
    echo -e "   Actual:   Fedora ${BOLD}$actual_version${NC}\n"
    echo -e "${YELLOW}${ARROW} Please check out the correct branch for your Fedora version${NC}\n"
    exit 1
  fi
  echo -e "${GREEN}${CHECK} Fedora version check passed: $actual_version${NC}"
  fedora_version="$actual_version"
else
  echo -e "${YELLOW}${WARN} WARNING - Could not find vars/fedora-version.yml${NC}"
  echo -e "   Skipping version check, but this may cause issues\n"
  fedora_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
fi

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
    if issue_url=$(gh issue create \
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
  local item v yn
  item="$1"
  yn=n
  while [[ "$yn" != "y" ]]; do
    echo -e "\n${CYAN}${ARROW}${NC} Please enter your ${BOLD}$item${NC}:" 1>&2
    read -rp "   " v
    echo -e "\n   You entered: ${BOLD}$v${NC}" 1>&2
    read -rsp "   Is this correct? (y/n): " -n 1 yn 1>&2
    echo 1>&2
  done
  echo "$v"
}

## Process

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
else
  echo " - found existing key"
fi
completed

title "Set Custom Hostname"
if [[ "$(hostname)" == "fedora" ]]; then
  echo "found default hostname, please choose a new one:"
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

# When setting up the github token, some required permissions might be missed out
# This function allows us to check for the required permissions
function ghCheckTokenPermission(){
  local permission="$1"
  local failSilent="${2:-false}"
  local scopes
  scopes="$(gh api -i user | grep 'X-Oauth-Scopes')"
  if [[ "$scopes" == *"$permission"* ]]; then
    echo " - found $permission permission"
    return 0
  else
    if [[ "$failSilent" == "true" ]]; then
      return 1
    fi
    echo " - missing $permission permission"
    echo "Please run this command ON THE MACHINE ITSELF, NOT REMOTELY
    gh auth refresh -h github.com -s '$permission'
    "
    return 1
  fi
}

if ! gh auth status > /dev/null 2>&1; then
  echo -e "\n${YELLOW}${BOLD}┌─────────────────────────────────────────────────┐${NC}"
  echo -e "${YELLOW}${BOLD}│                    IMPORTANT                    │${NC}"
  echo -e "${YELLOW}${BOLD}│   PLEASE CHOOSE SSH AS THE AUTHENTICATION      │${NC}"
  echo -e "${YELLOW}${BOLD}│                    METHOD!                      │${NC}"
  echo -e "${YELLOW}${BOLD}└─────────────────────────────────────────────────┘${NC}\n"
  
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

title "Configuring GitHub SSH Access"
# Check if we have the required permission
if ! ghCheckTokenPermission "admin:public_key" > /dev/null 2>&1; then
  warning "Missing admin:public_key permission - requesting it now"
  gh auth refresh -h github.com -s admin:public_key
fi

ssh_key_fingerprint=$(ssh-keygen -lf ~/.ssh/id.pub | awk '{print $2}')
# Use gh api to check for SSH keys without triggering signing key scope warning
if ! gh api user/keys 2>/dev/null | grep -q "$ssh_key_fingerprint"; then
  # Add SSH key for authentication only (not signing)
  if gh ssh-key add ~/.ssh/id.pub --title="$(hostname) Added by fedora-desktop setup script on $(date +%Y-%m-%d)" --type=authentication 2>&1; then
    success "SSH authentication key added to GitHub"
  else
    error "Failed to add SSH key to GitHub"
    echo -e "${YELLOW}${ARROW} Try manually adding your SSH key:${NC}"
    echo -e "   cat ~/.ssh/id.pub | gh ssh-key add --title='$(hostname)' --type=authentication"
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
  git -C ~/Projects/fedora-desktop pull
  success "Repository updated"
fi
cd ~/Projects/fedora-desktop
completed


title "Loading Personal Configuration"
localhost_yml=~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml
config_repo="${primary_gh_username}/fedora-desktop-config"

info "Checking for personal config repo: github.com/${config_repo}"
if raw_content=$(gh api "repos/${config_repo}/contents/localhost.yml" --jq '.content' 2>/dev/null); then
  # Config repo has a file — check if local copy already has real data
  local_has_data=false
  if [[ -f "$localhost_yml" ]] && grep -qE '(!vault|github_accounts)' "$localhost_yml"; then
    local_has_data=true
  fi
  if [[ "$local_has_data" == "true" ]]; then
    echo -e "\n${YELLOW}${WARN} localhost.yml already contains real configuration.${NC}"
    echo -e "   1) Pull from config repo (recommended — overwrites local)"
    echo -e "   2) Keep existing local file"
    read -rp "   Choice [1/2]: " _choice
    if [[ "${_choice}" != "2" ]]; then
      printf '%s' "$raw_content" | base64 -d > "$localhost_yml"
      success "Configuration pulled from github.com/${config_repo}"
    else
      success "Keeping existing localhost.yml"
    fi
  else
    printf '%s' "$raw_content" | base64 -d > "$localhost_yml"
    success "Configuration pulled from github.com/${config_repo}"
  fi
elif [[ -f "$localhost_yml" ]]; then
  success "Config repo not found — using existing localhost.yml"
else
  info "No config repo found — entering configuration manually"
    echo -e "\n${CYAN}Current system user: ${BOLD}$(whoami)${NC}"
    user_login="$(promptForValue 'user login')"
    user_name="$(promptForValue 'full name')"
    user_email="$(promptForValue 'email address')"

    echo -e "\n${CYAN}${ARROW}${NC} Enter GitHub accounts (alias:username, comma-separated)"
    echo -e "   Single account:  ${BOLD}johndoe${NC}"
    echo -e "   Multi-account:   ${BOLD}personal:johndoe,work:johndoe-work${NC}"
    github_accounts_raw="$(promptForValue 'GitHub accounts')"

    {
      printf 'user_login: "%s"\n' "$user_login"
      printf 'user_name: "%s"\n' "$user_name"
      printf 'user_email: "%s"\n' "$user_email"
      printf '# GitHub CLI accounts\n'
      printf 'github_accounts:\n'
      while IFS= read -r pair; do
        pair="${pair// /}"
        if [[ "$pair" == *":"* ]]; then
          printf '  %s: "%s"\n' "${pair%%:*}" "${pair##*:}"
        elif [[ -n "$pair" ]]; then
          printf '  personal: "%s"\n' "$pair"
        fi
      done < <(printf '%s' "$github_accounts_raw" | tr ',' '\n')
    } > "$localhost_yml"

    success "Configuration written"
fi
completed

title "Ansible Vault Configuration"
if [[ -f ~/Projects/fedora-desktop/vault-pass.secret ]]; then
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
  echo "$vaultPass" > ~/Projects/fedora-desktop/vault-pass.secret
fi
completed

title "Preparing SSH Keys for GitHub Accounts"
if grep -q 'github_accounts' "$localhost_yml" 2>/dev/null; then
  info "Generating any missing per-account SSH keys"
  # Parse github_accounts from localhost.yml — use Python to ignore !vault tags
  mapfile -t _gh_account_pairs < <(python3 - "$localhost_yml" <<'PYEOF'
import sys, yaml

def _ignore_vault(loader, tag_suffix, node):
    return None

_loader = yaml.SafeLoader
yaml.add_multi_constructor('', _ignore_vault, Loader=_loader)

with open(sys.argv[1]) as f:
    data = yaml.load(f, Loader=_loader)

for alias, username in (data.get('github_accounts') or {}).items():
    print(f"{alias}:{username}")
PYEOF
  )

  if [[ ${#_gh_account_pairs[@]} -eq 0 ]]; then
    warning "No github_accounts entries parsed — skipping"
  else
    for _pair in "${_gh_account_pairs[@]}"; do
      _alias="${_pair%%:*}"
      _username="${_pair##*:}"

      # Ensure per-account SSH key exists — playbook will handle GitHub upload
      _key_private="$HOME/.ssh/github_${_alias}"
      if [[ ! -f "$_key_private" ]]; then
        info "Generating SSH key for $_alias ($_username)"
        ssh-keygen -t ed25519 -C "${_username}@github" -f "$_key_private" -N ""
      fi

      success "SSH key ready: $_alias ($_username)"
    done
  fi
else
  success "Single account setup — no additional accounts to authenticate"
fi
completed

title "Running Ansible Playbooks"
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

echo -e "\n${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║              MAIN INSTALLATION COMPLETE!                    ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

## Optional Playbooks Menu System

# Function to run a playbook (wrapper for backward compatibility)
run_playbook() {
  run_playbook_with_issue_option "$1" "$2"
}

# Parse a space/comma-separated list of numbers; print valid ones (one per line)
_parse_number_list() {
  local input="$1"
  local max="$2"
  local normalized="${input//,/ }"
  for n in $normalized; do
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

if confirm "Would you like to install optional components?"; then
  cd ~/Projects/fedora-desktop

  # Playbooks that always run without prompting (auto-run before the interactive menu)
  auto_run_common=(
    "play-speech-to-text.yml"
  )

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
echo -e "  ${ARROW} Python development environment (pyenv + pyenv versions):"
echo -e "    ${BOLD}cd ~/Projects/fedora-desktop${NC}"
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
