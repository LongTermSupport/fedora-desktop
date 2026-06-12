# Project Architecture

Technical deep dive into how the project is structured and how it works.

## Quick Overview

**What you'll learn:**

- [Directory structure](#directory-structure) - Where everything lives
- [Execution flow](#execution-flow) - Bootstrap → main → optional
- [Configuration management](#configuration-management) - How settings work
- [Branching strategy](#branching-strategy) - Version-specific branches
- [Security model](#security-model) - Vault encryption and SSH keys

**Best for:** Contributors, system administrators, and curious developers who want to understand the internals.

**Time to read:** 8-10 minutes

## Directory Structure

```
fedora-desktop/
├── ansible.cfg                    # Ansible configuration
├── requirements.yml               # Ansible Galaxy dependencies
├── run.bash                      # Bootstrap installer script
├── vault-pass.secret             # Vault password (gitignored)
├── CLAUDE.md                     # Claude Code instructions
│
├── vars/
│   └── fedora-version.yml        # Target Fedora version
│
├── environment/
│   └── localhost/
│       ├── hosts.yml             # Inventory definition
│       └── host_vars/
│           └── localhost.yml     # User-specific variables
│
├── playbooks/
│   ├── playbook-main.yml         # Main orchestrator
│   └── imports/
│       ├── play-*.yml            # Core playbooks
│       └── optional/
│           ├── common/           # General optional features
│           ├── hardware-specific/# Hardware drivers/configs
│           ├── experimental/     # Bleeding-edge features
│           ├── untested/         # Playbooks not yet verified
│           └── archived/         # Deprecated playbooks
│
├── docs/                         # User-facing documentation
├── extensions/                   # GNOME Shell extensions source
├── tasks/                        # Standalone Ansible task files
├── tests/                        # Test suite
├── fedora-install/               # Fedora installation helpers (ISO/kickstart)
│
├── files/                        # Static configuration files
│   ├── etc/                     # System configs
│   ├── home/                    # User configs
│   └── var/                     # Variable data
│
├── scripts/                      # Utility scripts
├── roles/                        # Ansible roles
│   └── vendor/                  # Third-party roles (from requirements.yml)
│
└── untracked/                    # Runtime data (gitignored)
    └── facts/                    # Ansible fact cache
```

## Execution Flow

### 1. Bootstrap Phase (`run.bash`)

The bootstrap script:

- Validates system requirements
- Checks Fedora version against `vars/fedora-version.yml`
- Installs core dependencies
- Configures GitHub CLI authentication
- Generates SSH keys
- Clones the repository
- Collects user configuration
- Initializes Ansible vault
- Executes main playbook

### 2. Main Playbook Execution

`playbook-main.yml` orchestrates these playbooks in order (all run by default — none are optional):

01. **play-AA-preflight-sanity.yml**: Version and dependency checks
02. **play-AB-dnf-upgrade.yml**: Full system package upgrade
03. **play-basic-configs.yml**: System packages and base configuration
04. **play-prevent-ssh-suspend.yml**: Prevent SSH session suspend
05. **play-network-wait-tuning.yml**: Network startup tuning
06. **play-systemd-user-tweaks.yml**: Systemd user session tweaks
07. **play-nvm-install.yml**: Node Version Manager setup
08. **play-git-configure-and-tools.yml**: Git configuration and tools
09. **play-git-hooks-security.yml**: Security pre-commit hooks
10. **play-firefox.yml**: Firefox browser configuration
11. **play-github-cli-multi.yml**: GitHub CLI multi-account support
12. **play-ms-fonts.yml**: Microsoft fonts installation
13. **play-rpm-fusion.yml**: Third-party repository setup
14. **play-browsers.yml**: Additional browser setup
15. **play-toolbox-install.yml**: JetBrains Toolbox
16. **play-docker.yml**: Rootful Docker (compatibility engine for DDEV)
17. **play-lxc-install-config.yml**: LXC container support
    - **Ordering constraint**: LXC runs _after_ Docker so the `DOCKER-USER`
      iptables chain exists when LXC reconciles outbound connectivity.
      See the "Reconcile iptables" block in `play-lxc-install-config.yml`.
18. **play-podman.yml**: Rootless Podman (default container engine)
19. **play-python.yml**: Python/pyenv setup
20. **play-claude-yolo.yml**: CCY (Claude container wrapper) installation
    - **Ordering constraint**: CCY must run _before_ `play-claude-code.yml`
      because the `cc` wrapper sources CCY lib files at runtime, and
      `play-claude-code.yml` asserts the lib is present before deploying it.
      See `CLAUDE/Plan/00048-cc-token-source-parity`.
21. **play-claude-code.yml**: Claude Code CLI and `cc` wrapper
22. **play-comms.yml**: Communication applications
23. **play-gnome-shell.yml**: GNOME Shell configuration
24. **play-gnome-shell-extensions.yml**: GNOME Shell extensions
25. **play-markless.yml**: Markless tool setup
26. **play-terminal-emulators.yml**: Terminal emulator configuration
27. **play-vscode.yml**: Visual Studio Code
28. **play-vpn.yml**: VPN configuration
29. **play-gsettings.yml**: GNOME settings
30. **play-ZZ-repo-cleanup.yml**: Post-run repository cleanup

### 3. Optional Components

Manually executed based on needs:

- **common/**: Development tools, applications
- **hardware-specific/**: NVIDIA, DisplayLink, laptop power/thermal management
- **experimental/**: LXDE, VirtualBox
- **archived/**: Deprecated playbooks (e.g. `play-tlp-battery-optimisation.yml` moved here)

## Configuration Management

### Ansible Configuration (`ansible.cfg`)

Key settings:

- **Inventory**: `./environment/localhost`
- **Connection**: Local transport (not SSH)
- **Privilege Escalation**: sudo with `-HE` flags
- **Vault**: Password file at `./vault-pass.secret`
- **Fact Caching**: JSON files in `./untracked/facts/`

### Variable Hierarchy

1. **Global Variables**: `vars/fedora-version.yml`
2. **Host Variables**: `environment/localhost/host_vars/localhost.yml`
3. **Playbook Variables**: Defined in individual playbooks
4. **Vault-encrypted**: API keys and secrets

### File Management

Static files are organized by destination:

- `files/etc/`: System configuration files
- `files/home/`: User configuration files
- `files/var/`: Variable data and scripts

## Branching Strategy

- Each Fedora version has its own branch (F42, F43, etc.)
- Branch name corresponds to Fedora version
- `vars/fedora-version.yml` defines target version
- Default branch updated to current working version

## Security Model

- Vault encryption for sensitive data
- SSH key generation and management
- Passwordless sudo configuration
- GitHub CLI multi-account support
- Encrypted vault password file (gitignored)
