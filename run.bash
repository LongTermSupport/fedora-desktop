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
else
  echo -e "${YELLOW}${WARN} WARNING - Could not find vars/fedora-version.yml${NC}"
  echo -e "   Skipping version check, but this may cause issues\n"
fi

## Functions

title(){
  ((STEP_CURRENT++))
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
  local yn=n
  while [[ "$yn" != "y" ]]; do
    echo
    echo -e "${YELLOW}${ARROW}${NC} $msg"
    read -sp "   Press 'y' to confirm: " -n 1 yn
  done
  echo -e "\n${GREEN}${CHECK} Confirmed${NC}\n"
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
info "Installing: git, python3, grubby, jq, openssl, pipx"
sudo dnf -y install \
  git \
  python3 \
  python3-pip \
  grubby \
  jq \
  openssl \
  pipx > /dev/null 2>&1
completed

title "Configuring System Settings"
info "Updating GRUB configuration for cgroups"
sudo grubby --update-kernel=ALL --args="systemd.unified_cgroup_hierarchy=0" > /dev/null 2>&1
completed

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
ghCheckTokenPermission "admin:public_key" > /dev/null 2>&1
ssh_key_fingerprint=$(ssh-keygen -lf ~/.ssh/id.pub | awk '{print $2}')
if ! gh ssh-key list | grep -q "$ssh_key_fingerprint" 2>/dev/null; then
  gh ssh-key add ~/.ssh/id.pub --title="$(hostname) Added by fedora-desktop setup script on $(date +%Y-%m-%d)" --type=authentication > /dev/null 2>&1
  success "SSH key added to GitHub"
else
  success "SSH key already configured on GitHub"
fi
completed

title "Updating SSH Known Hosts"
info "Configuring GitHub host keys"
ssh-keygen -R github.com 2>/dev/null || true
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
if sudo -n true 2>/dev/null; then
  ./playbooks/playbook-main.yml
else
  echo -e "${YELLOW}${INFO} You will be prompted for your sudo password${NC}"
  ./playbooks/playbook-main.yml --ask-become-pass
fi
completed

echo -e "\n${GREEN}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}${BOLD}║              INSTALLATION COMPLETE!                         ║${NC}"
echo -e "${GREEN}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}\n"

title "System Reboot Required"
warning "Your system needs to be rebooted to complete the configuration"
confirm "Ready to reboot now?"
echo -e "${YELLOW}${INFO} Rebooting system...${NC}"
sudo reboot now
