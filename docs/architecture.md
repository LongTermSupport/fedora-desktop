# Project Architecture

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
│           └── archived/         # Deprecated playbooks
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

`playbook-main.yml` orchestrates these playbooks in order:

1. **play-AA-preflight-sanity.yml**: Version and dependency checks
2. **play-basic-configs.yml**: System packages and configurations
3. **play-nvm-install.yml**: Node Version Manager setup
4. **play-claude-code.yml**: Claude Code CLI installation
5. **play-git-configure-and-tools.yml**: Git configuration
6. **play-lxc-install-config.yml**: LXC container support
7. **play-ms-fonts.yml**: Microsoft fonts installation
8. **play-rpm-fusion.yml**: Third-party repository setup
9. **play-toolbox-install.yml**: JetBrains Toolbox

### 3. Optional Components

Manually executed based on needs:
- **common/**: Development tools, applications
- **hardware-specific/**: NVIDIA, DisplayLink, TLP
- **experimental/**: LXDE, VirtualBox

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