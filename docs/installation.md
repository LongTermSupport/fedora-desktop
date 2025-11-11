# Installation Guide

Get your Fedora system configured for development in 10-30 minutes.

## Before You Begin

### System Requirements

You need:
- **Fresh Fedora installation** - Fedora 42 (check version: `cat /etc/fedora-release`)
- **Regular user account** - Do not run as root
- **Internet connection** - Stable connection required for downloading packages
- **Sudo privileges** - Your user must be in the `wheel` group

Strongly recommended:
- **Encrypted root filesystem** - Set during Fedora installation
- **Third-party repositories enabled** - Enable during Fedora installation setup

### Why Third-Party Repositories?

Many features require packages from RPM Fusion (free and non-free):
- Microsoft fonts
- Media codecs
- NVIDIA drivers (if using optional hardware playbooks)
- Various proprietary software

If you didn't enable them during installation, don't worry - the playbook will set them up, but some packages may fail initially.

## Quick Install

### One-Command Installation

From your Fedora desktop, open a terminal and run:

```bash
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))
```

**That's it!** Grab a coffee and come back in 10-30 minutes.

### What Happens During Installation

The bootstrap script (`run.bash`) performs these steps in order:

**Phase 1: Validation** (30 seconds)
- Checks Fedora version matches target (Fedora 42)
- Verifies you're running as regular user (not root)
- Checks internet connectivity

**Phase 2: Dependencies** (2-5 minutes)
- Installs git, ansible, python3-libdnf5
- May prompt for sudo password

**Phase 3: GitHub Setup** (1-2 minutes)
- Configures GitHub CLI (gh)
- Generates Ed25519 SSH keys (`~/.ssh/id`, `~/.ssh/id.pub`)
- Prompts for GitHub authentication

**Phase 4: Repository** (30 seconds)
- Clones project to `~/Projects/fedora-desktop`
- Checks out appropriate branch

**Phase 5: Configuration** (interactive)
- Asks for your full name
- Asks for your email address
- Asks for bash prompt color preference (red, green, blue, etc.)
- Creates Ansible vault password

**Phase 6: Main Playbook** (5-20 minutes)
- Runs all core playbooks automatically
- Installs packages and configures system
- See [Core Playbooks](playbooks.md#core-playbooks-automatically-run) for details

**Expected output:** Ansible task results showing "ok", "changed", or "skipped" for each step. Green text is good, yellow is normal changes, red means errors.

## Manual Installation

If you prefer step-by-step control or the quick install failed:

### Step 1: Install Dependencies

```bash
sudo dnf install -y git ansible python3-libdnf5
```

**Why these packages?**
- `git` - Clone the repository
- `ansible` - Run the configuration playbooks
- `python3-libdnf5` - Modern DNF Python bindings for Ansible

### Step 2: Clone Repository

```bash
mkdir -p ~/Projects
git clone https://github.com/LongTermSupport/fedora-desktop.git ~/Projects/fedora-desktop
cd ~/Projects/fedora-desktop
```

### Step 3: Checkout Correct Branch

```bash
# Check your Fedora version
cat /etc/fedora-release

# Checkout matching branch
git checkout F42  # Replace with your version (F42, F43, etc.)
```

### Step 4: Install Ansible Requirements

```bash
ansible-galaxy install -r requirements.yml
```

This installs required Ansible collections (community.general, ansible.posix).

### Step 5: Configure User Variables

Edit the user configuration file:

```bash
ansible-vault edit environment/localhost/host_vars/localhost.yml
```

You'll be prompted to create a vault password. Set these variables:

```yaml
user_login: "your-username"
user_name: "Your Full Name"
user_email: "your.email@example.com"
```

Save and exit (`:wq` in vim).

### Step 6: Save Vault Password

```bash
# Create vault password file with the password you just set
echo "your-vault-password" > vault-pass.secret
chmod 600 vault-pass.secret
```

### Step 7: Run Main Playbook

```bash
ansible-playbook playbooks/playbook-main.yml --ask-become-pass
```

Enter your sudo password when prompted. This runs all core playbooks automatically.

## What Gets Installed

### Core Components (Automatic)

The main playbook installs and configures:

**System Setup**
- Preflight sanity checks (Fedora version validation)
- Basic packages (vim, wget, htop, bash-completion, ripgrep, etc.)
- DNF optimization (10 parallel downloads, fastest mirror)
- Passwordless sudo for your user
- RPM Fusion repositories

**Development Tools**
- Git configuration (name, email from host_vars)
- bash-git-prompt with Solarized theme
- GitHub CLI (gh)
- SSH keys (Ed25519 at `~/.ssh/id`)
- Node.js 20 via NVM
- Claude Code CLI
- JetBrains Toolbox

**Container Platform**
- LXC and LXD packages
- Container networking (lxcbr0 bridge)
- SSH configuration for containers
- Firewall rules for container access

**Enhancements**
- Custom bash prompt with error indicators
- Enhanced bash history (20K lines)
- Microsoft fonts
- Vim with Deus colorscheme

**Time required:** 5-20 minutes depending on internet speed

### Optional Components

Must be run manually after main playbook. See [Playbooks Reference](playbooks.md#optional-playbooks) for the complete catalog.

Popular options:
```bash
cd ~/Projects/fedora-desktop

# Docker (rootless)
ansible-playbook playbooks/imports/optional/common/play-docker.yml

# Distrobox
ansible-playbook playbooks/imports/optional/common/play-install-distrobox.yml

# Playwright testing environment
ansible-playbook playbooks/imports/optional/common/play-distrobox-playwright.yml

# Python development
ansible-playbook playbooks/imports/optional/common/play-python.yml

# VS Code
ansible-playbook playbooks/imports/optional/common/play-vscode.yml
```

## Verifying Installation

After installation completes, verify the setup:

```bash
# Check Ansible can connect
ansible desktop -m ping
# Expected: localhost | SUCCESS

# Check installed Node.js version
node --version
# Expected: v20.x.x

# Check Git configuration
git config --global user.name
git config --global user.email
# Expected: Your name and email

# Check LXC installation
sudo lxc-ls --version
# Expected: Version number

# Verify bash customizations
echo $PS1 | grep -q "01;3" && echo "Custom prompt configured"
# Expected: Custom prompt configured
```

## Troubleshooting

### Version Mismatch Error

**Symptom:** `Fedora version mismatch` error during preflight checks

**Cause:** Your Fedora version doesn't match the branch target

**Solution:**
```bash
# 1. Check your Fedora version
cat /etc/fedora-release
# Output: Fedora release 42 (Forty Two)

# 2. Check target version for current branch
cd ~/Projects/fedora-desktop
cat vars/fedora-version.yml
# Output: fedora_version: 42

# 3. If versions don't match, checkout correct branch
git fetch origin
git checkout F42  # Replace with your version

# 4. Re-run the playbook
ansible-playbook playbooks/playbook-main.yml --ask-become-pass
```

### Bootstrap Script Fails to Download

**Symptom:** `curl` command fails or hangs

**Solutions:**

**Check internet connection:**
```bash
ping -c 3 raw.githubusercontent.com
```

**Try alternative download method:**
```bash
wget -O run.bash https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash
chmod +x run.bash
./run.bash
```

**Manual installation:** See [Manual Installation](#manual-installation) section above.

### Third-Party Repository Issues

**Symptom:** Some packages fail to install with "No package available" errors

**Cause:** RPM Fusion repositories not enabled

**Solution:**
```bash
# Install RPM Fusion repositories manually
sudo dnf install -y \
  https://download1.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm \
  https://download1.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm

# Re-run the playbook
cd ~/Projects/fedora-desktop
ansible-playbook playbooks/playbook-main.yml --ask-become-pass
```

### Permission Denied Errors

**Symptom:** `sudo: <user> is not in the sudoers file`

**Cause:** User not in wheel group (doesn't have sudo privileges)

**Solution:**
```bash
# Switch to root
su -

# Add user to wheel group
usermod -aG wheel your-username

# Exit root
exit

# Log out and log back in for changes to take effect
# Test sudo access
sudo whoami
# Expected: root
```

### Ansible Not Found

**Symptom:** `ansible: command not found` after installation

**Cause:** Ansible not installed or not in PATH

**Solution:**
```bash
# Install Ansible
sudo dnf install -y ansible

# Verify installation
ansible --version
```

### GitHub CLI Authentication Fails

**Symptom:** GitHub CLI (gh) authentication fails during bootstrap

**Solutions:**

**Skip for now and configure manually later:**
```bash
# After installation completes
gh auth login
# Follow interactive prompts
```

**Use SSH key manually:**
```bash
# Add your SSH public key to GitHub
cat ~/.ssh/id.pub

# Go to https://github.com/settings/keys
# Click "New SSH key" and paste the contents
```

### Playbook Hangs or Takes Too Long

**Symptom:** Ansible playbook appears stuck on a task

**Common causes:**
- **Slow mirror:** DNF downloading from slow repository mirror
- **Large packages:** Installing large packages like JetBrains Toolbox
- **First run:** Package cache being built for first time

**What to do:**
- **Be patient:** Some tasks legitimately take 5-10 minutes
- **Check activity:** Look for disk I/O or network activity
- **Increase verbosity:** Run with `-v` flag to see what's happening:
  ```bash
  ansible-playbook playbooks/playbook-main.yml --ask-become-pass -v
  ```

**Force timeout if truly stuck:**
- Press `Ctrl+C` to stop
- Check the last task that ran
- Run playbook again (it's idempotent, safe to re-run)

### Vault Password Issues

**Symptom:** `ERROR! Attempting to decrypt but no vault secrets found`

**Cause:** Vault password file missing or incorrect

**Solution:**
```bash
# Check if vault password file exists
ls -la ~/Projects/fedora-desktop/vault-pass.secret

# If missing, recreate it
cd ~/Projects/fedora-desktop
echo "your-vault-password" > vault-pass.secret
chmod 600 vault-pass.secret

# Test vault access
ansible-vault view environment/localhost/host_vars/localhost.yml
```

### NVM Installation Fails

**Symptom:** Node.js not available after installation

**Cause:** NVM not properly loaded in current shell

**Solution:**
```bash
# Source NVM in current shell
export NVM_DIR="$HOME/.nvm"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh"

# Verify Node.js
node --version

# If still not working, check NVM installation
ls -la ~/.nvm/

# Re-run NVM playbook
cd ~/Projects/fedora-desktop
ansible-playbook playbooks/imports/play-nvm-install.yml
```

### Still Having Issues?

**Get help:**
1. **Check existing issues:** [GitHub Issues](https://github.com/LongTermSupport/fedora-desktop/issues)
2. **Search discussions:** [GitHub Discussions](https://github.com/LongTermSupport/fedora-desktop/discussions)
3. **Open new issue:** Include:
   - Fedora version (`cat /etc/fedora-release`)
   - Branch name (`git branch`)
   - Error message (full output)
   - Steps to reproduce

**Debug mode:**
```bash
# Run playbook with maximum verbosity
ansible-playbook playbooks/playbook-main.yml --ask-become-pass -vvv

# Check Ansible facts
ansible desktop -m setup

# Verify inventory
ansible-inventory --list
```