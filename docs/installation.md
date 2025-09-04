# Installation Guide

## Prerequisites

- Fresh Fedora installation (version defined in `vars/fedora-version.yml`)
- Current branch targets: **Fedora 42**
- Encrypted root filesystem strongly recommended
- Third-party repositories must be enabled during Fedora installation
- Regular user account (not root)

## Quick Install

Run as your normal desktop user:

```bash
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))
```

## What the Installation Does

The `run.bash` script performs these steps:

1. **Version Check**: Validates your Fedora version matches the branch target
2. **Dependency Installation**: Installs git, ansible, python3-libdnf5
3. **GitHub Setup**: Configures GitHub CLI and generates SSH keys
4. **Repository Clone**: Clones this project to `~/Projects/fedora-desktop`
5. **User Configuration**: Collects your name, email, and prompt color preference
6. **Vault Setup**: Creates ansible vault password file
7. **Ansible Execution**: Runs the main playbook

## Manual Installation

If you prefer to run steps manually:

```bash
# Install dependencies
sudo dnf install -y git ansible python3-libdnf5

# Clone repository
mkdir -p ~/Projects
git clone https://github.com/LongTermSupport/fedora-desktop.git ~/Projects/fedora-desktop
cd ~/Projects/fedora-desktop

# Check out correct branch for your Fedora version
git checkout F42  # or appropriate version

# Install Ansible requirements
ansible-galaxy install -r requirements.yml

# Create vault password file
echo "your-password-here" > vault-pass.secret
chmod 600 vault-pass.secret

# Run main playbook
ansible-playbook playbooks/playbook-main.yml --ask-become-pass
```

## Post-Installation

The main playbook automatically runs:
- Preflight sanity checks
- Basic system configurations
- NVM and Node.js setup
- Claude Code installation
- Git configuration and tools
- LXC container support
- Microsoft fonts
- RPM Fusion repositories
- JetBrains Toolbox

Optional components must be run manually - see [Playbooks Documentation](playbooks.md).

## Troubleshooting

### Version Mismatch
If you see "Fedora version mismatch":
- Check your version: `cat /etc/fedora-release`
- Check target version: `cat vars/fedora-version.yml`
- Switch branch: `git checkout F<your-version>`

### Third-Party Repository Issues
If RPM Fusion packages fail:
```bash
sudo dnf install -y \
  https://download1.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm \
  https://download1.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm
```

### Permission Issues
Ensure your user has sudo access:
```bash
sudo usermod -aG wheel $USER
# Then log out and back in
```