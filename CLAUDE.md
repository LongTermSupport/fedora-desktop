# Claude Code Configuration

## Project Overview

This is a **Fedora Desktop Configuration Management Project** that automates the setup of a freshly installed Fedora system for development work using Ansible. The project uses a branching strategy where each Fedora version has its own branch, with the target version defined in `vars/fedora-version.yml`, and provides a comprehensive desktop environment setup with development tools, customizations, and optional components.

### Purpose
- Automate Fedora desktop environment setup for development
- Provide consistent, reproducible development environment
- Support both essential and optional software installations
- Maintain secure configuration with encrypted secrets

## Project Architecture

### Core Structure
```
fedora-desktop/
├── ansible.cfg                    # Ansible configuration
├── requirements.yml               # Ansible collections/roles
├── run.bash                      # Bootstrap installation script
├── vault-pass.secret             # Encrypted vault password file
├── vars/                        # Configuration variables
│   └── fedora-version.yml       # Centralized Fedora version config
├── environment/localhost/        # Ansible inventory configuration
│   ├── hosts.yml                # Host definitions
│   └── host_vars/localhost.yml   # Host-specific variables
├── playbooks/                   # Ansible playbooks
│   ├── playbook-main.yml        # Main playbook orchestrator
│   └── imports/                 # Individual task playbooks
│       ├── play-*.yml           # Core system playbooks
│       └── optional/            # Optional feature playbooks
├── files/                       # Static configuration files
│   ├── etc/                    # System configuration templates
│   └── var/local/              # Custom scripts and configs
└── untracked/                   # Runtime data (facts cache)
```

### Execution Flow
1. **Bootstrap Phase** (`run.bash`):
   - Validates Fedora version against `vars/fedora-version.yml`
   - Installs system dependencies (git, ansible, python3)
   - Configures GitHub CLI and SSH keys
   - Clones project repository
   - Collects user configuration
   - Initializes Ansible vault

2. **Configuration Phase** (`playbook-main.yml`):
   - Runs preflight system checks (including Fedora version validation)
   - Applies base system configurations
   - Installs and configures development tools
   - Sets up user environment

3. **Optional Components**:
   - Manual execution of optional playbooks
   - Hardware-specific configurations
   - Experimental features

## Ansible Configuration Patterns

### Host Management
- **Target**: `desktop` group (localhost only)
- **Connection**: Local transport (not SSH)
- **Privilege Escalation**: sudo with `-HE` flags
- **Inventory**: YAML-based localhost configuration

### Variable Management
- **Version Configuration**: `vars/fedora-version.yml` (centralized Fedora version)
- **Host Variables**: `environment/localhost/host_vars/localhost.yml`
- **Vault Encryption**: Uses `localhost` vault ID
- **Required Variables**:
  - `fedora_version`: Target Fedora version (from `vars/fedora-version.yml`)
  - `user_login`: System username
  - `user_name`: Full display name  
  - `user_email`: Email address
  - `lastfm_api_key`/`lastfm_api_secret`: Encrypted API credentials

### Playbook Patterns
- **Modular Design**: Each feature as separate playbook
- **Consistent Structure**:
  ```yaml
  - hosts: desktop
    name: [Descriptive Name]
    become: [true/false]
    vars:
      root_dir: "{{ inventory_dir }}/../../"
    vars_files:
      - "{{ root_dir }}/vars/fedora-version.yml"  # For version-dependent playbooks
    tasks: [...]
  ```
- **Categorization**:
  - Core playbooks: Essential system setup
  - Optional/common: General development tools  
  - Optional/hardware-specific: Hardware drivers/configs
  - Optional/experimental: Bleeding-edge features

## Technology Stack

### Core Technologies
- **Configuration Management**: Ansible 2.9.9+
- **Target OS**: Fedora (version per branch, defined in `vars/fedora-version.yml`)
- **Package Manager**: DNF with parallel downloads
- **Shell Environment**: Bash with custom prompts
- **Version Control**: Git with GitHub CLI integration
- **Branching Strategy**: Version-specific branches (F42, F43, etc.)

### Development Tools Supported
- **Languages**: Python 3, Node.js 20, Golang, Docker
- **Editors**: Vim (customized), VS Code, PyCharm Community
- **Containers**: LXC, Docker, Toolbox
- **Security**: Ansible Vault, SSH key management
- **Package Sources**: DNF repos, Flatpak, RPM Fusion

### Dependencies
- **Ansible Collections**:
  - `community.general`
  - `ansible.posix`
- **System Packages**: 
  - Base: `vim`, `wget`, `bash-completion`, `htop`, `python3-libdnf5`
  - Development: `git`, `gh`, `ripgrep`, `jq`, `openssl`
  - Optional: Hardware-specific drivers, development environments

## Development Workflow

### Common Commands
```bash
# Run main configuration
./playbooks/playbook-main.yml

# Run optional playbook
ansible-playbook ./playbooks/imports/optional/common/play-install-flatpaks.yml

# Check specific component
ansible-playbook ./playbooks/imports/play-basic-configs.yml --ask-become-pass

# Update project
git pull && ansible-galaxy install -r requirements.yml
```

### Key File Locations
- **Version config**: `vars/fedora-version.yml:1`
- **Main orchestration**: `playbooks/playbook-main.yml:1`
- **System basics**: `playbooks/imports/play-basic-configs.yml:1`
- **Preflight checks**: `playbooks/imports/play-AA-preflight-sanity.yml:1`
- **Git setup**: `playbooks/imports/play-git-configure-and-tools.yml:1`
- **User config**: `environment/localhost/host_vars/localhost.yml:1`
- **Custom bash**: `files/etc/profile.d/zz_lts-fedora-desktop.bash:1`

### Configuration Customization
- **Prompt Colors**: Configurable via `PS1_Colour` variable in basic-configs
- **Git Configuration**: Automated based on `user_name` and `user_email`
- **SSH Keys**: Auto-generated Ed25519 keys with GitHub integration
- **Vim**: Custom Deus colorscheme and system-wide configuration

## Security Considerations

### Vault Management
- Password stored in `vault-pass.secret` (gitignored)
- Vault ID matching enforced (`vault_id_match=true`)
- Sensitive data (API keys) encrypted with Ansible Vault
- SSH keys copied to root user for system operations

### Permissions
- Passwordless sudo configured for main user
- SSH key authentication for GitHub
- Proper file permissions on sensitive files (`mode: 0600`)

## Special Features

### Custom Shell Environment
- **Prompt System**: Dynamic PS1 with color coding and error states
- **Git Integration**: bash-git-prompt with Solarized theme
- **Docker Helpers**: Node.js container wrapper functions
- **Aliases**: Comprehensive set for development workflow
- **History**: Enhanced history management (20K file size, 10K memory)

### Hardware Support
- **Graphics**: NVIDIA driver support
- **Audio**: HD audio configurations
- **Power**: TLP battery optimization
- **Display**: DisplayLink driver support
- **Firmware**: Automatic fwupd updates

### Optional Components
- **Development**: VS Code, Docker, Python environments, Golang
- **Applications**: Flatpak management, Firefox policies, VPN clients
- **Desktop**: GNOME Shell extensions and settings
- **Experimental**: Claude Code, LXDE, VirtualBox Windows

## Troubleshooting

### Common Issues
- **Version Mismatch**: Ensure you're on the correct branch for your Fedora version
- **run.bash fails**: Check Fedora version matches `fedora_version` in `vars/fedora-version.yml:1`
- **Preflight checks fail**: Verify Fedora version and system requirements
- **Vault issues**: Check vault password file exists and is readable
- **Package installation**: Verify third-party repos are enabled during Fedora installation
- **Permissions**: Ensure user has sudo privileges for system modifications

### Debug Commands
```bash
# Check Ansible connectivity
ansible desktop -m ping

# Verify vault access  
ansible-vault view environment/localhost/host_vars/localhost.yml

# Test specific component
ansible desktop -m setup --tree untracked/facts/

# Check DNF configuration
cat /etc/dnf/dnf.conf | grep max_parallel
```

## Project Maintenance

### Regular Updates
- **New Fedora Version**: Update `fedora_version` in `vars/fedora-version.yml` and create new branch
- **Package Lists**: Review and update package lists in playbooks
- **External Resources**: Update URLs and repository configurations
- **Testing**: Test optional playbooks after major system updates
- **Branch Management**: Set new default branch when starting work on new Fedora version

### Development Setup
- **IDE**: PyCharm Community with Ansible plugin
- **Linting**: ansible-lint integration
- **Extensions**: Ansible Vault Editor for encrypted content
- **Version Control**: Conventional Git workflow with signed commits

This configuration provides a robust, automated solution for Fedora desktop development environment setup with extensive customization options and security best practices.