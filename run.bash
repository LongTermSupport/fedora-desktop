#!/usr/bin/env bash

## Setup
set -e
set -u
set -o pipefail
standardIFS="$IFS"
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
SPINNER="⣾⣽⣻⢿⡿⣟⣯⣷"
BUG="🐛"

## Step counter
STEP_CURRENT=0
STEP_TOTAL=15

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

confirm(){
  local msg="$1"
  local yn=""
  echo
  echo -e "${YELLOW}${ARROW}${NC} $msg"
  while true; do
    read -sp "   Press 'y' to confirm, 'n' to skip: " -n 1 yn
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
  local current_user="$(whoami)"
  sanitized=$(echo "$sanitized" | sed "s|/home/$current_user|/home/***USER***|g")
  
  # Remove vault passwords and encrypted content
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
  local error_log="$2"
  local exit_code="$3"
  
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
  local fedora_version=$(grep "^VERSION_ID=" /etc/os-release | cut -d= -f2)
  local branch=$(git branch --show-current)
  local commit=$(git rev-parse --short HEAD)
  local hostname=$(hostname)
  local kernel=$(uname -r)
  
  # Sanitize the error log
  info "Sanitizing error log for sensitive data..."
  local sanitized_log=$(sanitize_error_log "$error_log")
  
  # If Claude Code is available, use it for enhanced sanitization
  if check_claude_code; then
    info "Using Claude Code for enhanced sensitive data removal..."
    local temp_file=$(mktemp)
    echo "$sanitized_log" > "$temp_file"
    
    # Ask Claude to sanitize the log further
    local claude_sanitized=$(claude "Please remove any potentially sensitive information from this error log including passwords, API keys, tokens, personal data, private URLs, or system-specific paths that shouldn't be shared publicly. Return ONLY the sanitized version of the log, preserving the error messages and structure but with sensitive data replaced with placeholders like ***REDACTED***:\n\n$(cat $temp_file)" 2>/dev/null || echo "")
    
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
  local issue_title="[Automated] Playbook failure: $(basename $playbook_name) on Fedora $fedora_version"
  
  local issue_body="## Playbook Failure Report

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
    local issue_url=$(gh issue create \
      --title "$issue_title" \
      --body "$issue_body" \
      --label "bug" \
      --label "automated" \
      2>&1)
    
    if [[ $? -eq 0 ]]; then
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

# Function to run playbook with error capture
run_playbook_with_capture(){
  local playbook="$1"
  local name="$2"
  local temp_log=$(mktemp)
  local exit_code=0
  
  echo -e "\n${CYAN}${ARROW} Running: $name${NC}"
  
  # Run playbook and capture output
  if sudo -n true 2>/dev/null; then
    "$playbook" 2>&1 | tee "$temp_log"
    exit_code=${PIPESTATUS[0]}
  else
    "$playbook" --ask-become-pass 2>&1 | tee "$temp_log"
    exit_code=${PIPESTATUS[0]}
  fi
  
  if [[ $exit_code -eq 0 ]]; then
    success "Completed: $name"
    rm -f "$temp_log"
    return 0
  else
    error "Failed: $name (exit code: $exit_code)"
    
    # Extract last 100 lines of error for issue
    local error_excerpt=$(tail -n 100 "$temp_log")
    
    # Offer to create GitHub issue
    if confirm "Would you like to create a GitHub issue for this failure?"; then
      create_github_issue "$playbook" "$error_excerpt" "$exit_code"
    fi
    
    rm -f "$temp_log"
    return $exit_code
  fi
}

promptForValue(){
  local item v yn
  item="$1"
  yn=n
  while [[ "$yn" != "y" ]]; do
    echo -e "\n${CYAN}${ARROW}${NC} Please enter your ${BOLD}$item${NC}:" 1>&2
    read -p "   " v
    echo -e "\n   You entered: ${BOLD}$v${NC}" 1>&2
    read -sp "   Is this correct? (y/n): " -n 1 yn 1>&2
    echo 1>&2
  done
  echo "$v"
}

## Process

echo -e "\n${MAGENTA}${BOLD}Installation Process${NC}"
echo -e "${MAGENTA}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
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
if sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy=0"; then
  warning "Found legacy cgroup configuration, removing..."
  sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=0"
  success "Legacy cgroup configuration removed"
else
  success "No legacy cgroup configuration found"
fi

title "Setting up Ansible Environment"
info "Installing Ansible and dependencies"
pipx install --include-deps ansible > /dev/null 2>&1 || success "Ansible already installed"
pipx inject ansible jmespath > /dev/null 2>&1 || success "jmespath already configured"
pipx inject ansible passlib > /dev/null 2>&1 || success "passlib already configured"
pipx inject ansible ansible-lint > /dev/null 2>&1 || success "ansible-lint already configured"
if [ ! -L ~/.local/bin/ansible-lint ]; then
    ln -s ~/.local/share/pipx/venvs/ansible/bin/ansible-lint ~/.local/bin/
    success "Created ansible-lint symlink"
else
    success "ansible-lint symlink exists"
fi
completed

title "Creating SSH Key Pair\n\nNOTE - you must set a password\n\nSuggest you use your login password"
if [[ ! -f ~/.ssh/id ]]; then
  while true; do
    read -s -p "Password: " password
    echo
    read -s -p "Password (confirm): " password2
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
  read -p "Hostname: " hostname
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
completed


title "Verifying GitHub Account Configuration"
info "Checking account consistency"
ghAuthLoginName="$(gh api user | jq -r '.login')"
set +e
sshGithubName="$(ssh -T git@github.com -i ~/.ssh/id |& grep -Po '(?<=Hi ).*(?=! You)')"
set -e
if [[ "$ghAuthLoginName" != "$sshGithubName" ]]; then
  error "GitHub account mismatch detected"
  echo -e "   ${RED}CLI account: $ghAuthLoginName${NC}"
  echo -e "   ${RED}SSH account: $sshGithubName${NC}\n"
  echo -e "${YELLOW}${ARROW} To fix this issue:${NC}"
  echo -e "   1. Run 'gh auth logout'"
  echo -e "   2. Login again with the correct account"
  echo -e "   3. Ensure SSH key upload is selected\n"
  exit 1
fi
success "Account verification passed: $ghAuthLoginName"
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

title "Setting up Project Directory"
if [ ! -d ~/Projects ]; then
  mkdir -p ~/Projects
  success "Projects directory created"
else
  success "Projects directory exists"
fi
completed

title "Cloning Configuration Repository"
cd ~/Projects
if [[ ! -d ~/Projects/fedora-desktop ]]; then
  info "Cloning fedora-desktop repository"
  git clone git@github.com:LongTermSupport/fedora-desktop.git > /dev/null 2>&1
  success "Repository cloned successfully"
else
  success "Repository already exists"
fi
completed


if [[ ! -f ~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml ]]; then
  title "User Configuration Setup"
  info "Please provide your user information"
  
  echo -e "\n${CYAN}Current system user: ${BOLD}$(whoami)${NC}"
  user_login="$(promptForValue 'user login')"
  
  user_name="$(promptForValue 'full name')"
  
  user_email="$(promptForValue 'email address')"
  completed

  title "Generating Ansible Configuration"
  cat <<EOF > ~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml
user_login: "$user_login"
user_name: "$user_name"
user_email: "$user_email"
EOF
  completed
fi

title "Updating Repository"
cd ~/Projects/fedora-desktop
info "Pulling latest changes"
git pull > /dev/null 2>&1
success "Repository updated"
completed

title "Ansible Vault Configuration"
if [[ -f ~/Projects/fedora-desktop/vault-pass.secret ]]; then
  success "Existing vault password found"
else
  info "Setting up Ansible vault"
  echo -e "\n${CYAN}${ARROW}${NC} Enter vault password (or leave blank to auto-generate):"
  read -sp "   " vaultPass
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

title "Running Ansible Playbooks"
info "Installing Ansible requirements"
ansible-galaxy install -r requirements.yml > /dev/null 2>&1
success "Requirements installed"

info "Executing main configuration playbook"
echo -e "${YELLOW}${INFO} This may take several minutes...${NC}\n"

# Run main playbook with error capture
temp_main_log=$(mktemp)
main_exit_code=0

if sudo -n true 2>/dev/null; then
  ./playbooks/playbook-main.yml 2>&1 | tee "$temp_main_log"
  main_exit_code=${PIPESTATUS[0]}
else
  echo -e "${YELLOW}${INFO} You will be prompted for your sudo password${NC}"
  ./playbooks/playbook-main.yml --ask-become-pass 2>&1 | tee "$temp_main_log"
  main_exit_code=${PIPESTATUS[0]}
fi

if [[ $main_exit_code -eq 0 ]]; then
  completed
  rm -f "$temp_main_log"
else
  error "Main playbook failed with exit code: $main_exit_code"
  
  # Extract last 200 lines for main playbook errors (they tend to be longer)
  main_error_excerpt=$(tail -n 200 "$temp_main_log")
  
  # Offer to create GitHub issue
  if confirm "Would you like to create a GitHub issue for this failure?"; then
    create_github_issue "./playbooks/playbook-main.yml" "$main_error_excerpt" "$main_exit_code"
  fi
  
  rm -f "$temp_main_log"
  
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
  run_playbook_with_capture "$1" "$2"
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
      local name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
      echo -e "  ${BOLD}$i)${NC} $name"
      ((i++))
    done
    echo -e "  ${BOLD}A)${NC} Run all"
    echo -e "  ${BOLD}S)${NC} Skip to next section"
    echo -e "  ${BOLD}Q)${NC} Quit optional installations"
    
    echo
    read -p "Enter your choice: " choice
    
    case "$choice" in
      [1-9]|[1-9][0-9])
        if [[ $choice -le ${#playbooks[@]} ]]; then
          local selected="${playbooks[$((choice-1))]}"
          local name=$(basename "$selected" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          run_playbook "$selected" "$name"
        else
          error "Invalid selection: $choice (choose 1-${#playbooks[@]})"
          echo -e "${YELLOW}${ARROW} Please try again${NC}\n"
          sleep 1
        fi
        ;;
      [Aa])
        for pb in "${playbooks[@]}"; do
          local name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
          run_playbook "$pb" "$name"
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
        echo -e "${YELLOW}${ARROW} Please enter a number (1-${#playbooks[@]}), A, S, or Q${NC}\n"
        sleep 1
        ;;
    esac
  done
  return 0
}

# Hardware detection function
check_hardware() {
  local playbook="$1"
  local pb_name=$(basename "$playbook")
  
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
  
  # Common optional playbooks
  if [[ -d playbooks/imports/optional/common ]]; then
    mapfile -t common_playbooks < <(find playbooks/imports/optional/common -name "*.yml" -type f | sort)
    if [[ ${#common_playbooks[@]} -gt 0 ]]; then
      info "Found ${#common_playbooks[@]} common optional playbooks"
      if ! show_menu "Common Optional" "${common_playbooks[@]}"; then
        info "Skipping remaining optional installations"
      fi
    fi
  fi
  
  # Hardware-specific playbooks
  if [[ -d playbooks/imports/optional/hardware-specific ]]; then
    mapfile -t hw_playbooks < <(find playbooks/imports/optional/hardware-specific -name "*.yml" -type f | sort)
    if [[ ${#hw_playbooks[@]} -gt 0 ]]; then
      echo -e "\n${CYAN}${BOLD}Hardware-Specific Playbooks${NC}"
      echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
      info "Analyzing your hardware..."
      
      i=1
      for pb in "${hw_playbooks[@]}"; do
        name=$(basename "$pb" .yml | sed 's/^play-//' | tr '-' ' ' | sed 's/\b\(.\)/\u\1/g')
        status=$(check_hardware "$pb")
        echo -e "  ${BOLD}$i)${NC} $name $status"
        ((i++))
      done
      echo
      
      if confirm "Would you like to configure hardware-specific components?"; then
        show_menu "Hardware-Specific" "${hw_playbooks[@]}"
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

# Reboot section
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
