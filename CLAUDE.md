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

## Development Principles

### Core Principles
All code in this project must adhere to these fundamental principles:

#### Fail Fast
- **Exit immediately on errors** - Use `set -e` in all bash scripts
- **Validate early** - Check prerequisites before starting work
- **No silent failures** - Every error must stop execution with clear message
- **Explicit error handling** - Don't hide errors or use fallback values
- **Exit codes matter** - Always check command success with `if !` or `&&`

Examples:
```bash
# GOOD: Fail fast with clear error
if [ -z "$REQUIRED_VAR" ]; then
    echo "ERROR: REQUIRED_VAR is not set" >&2
    exit 1
fi

# BAD: Silent failure with default
REQUIRED_VAR="${REQUIRED_VAR:-default}"
```

#### YAGNI (You Aren't Gonna Need It)
- **Don't add features** until they are actually needed
- **No speculative code** - Only solve current problems
- **Remove unused code** - Delete, don't comment out
- **Simple solutions first** - Complex solutions only when simple ones fail

#### DRY (Don't Repeat Yourself)
- **Extract common patterns** into reusable functions/tasks
- **Use variables** for repeated values
- **Create includes** for repeated task blocks
- **Reference, don't duplicate** documentation and configs

#### Idempotent Operations
- **Safe to run multiple times** - Same result every time
- **Check before change** - Use `creates`, `unless`, conditionals
- **Declarative over imperative** - Describe state, not steps
- **No side effects** on re-runs

Examples:
```yaml
# GOOD: Idempotent with creates
- name: Install from URL
  shell: wget https://example.com/install.sh && bash install.sh
  args:
    creates: /usr/bin/installed_binary

# BAD: Will fail on second run
- name: Install from URL
  shell: wget https://example.com/install.sh && bash install.sh
```

#### Security First
- **Never hardcode secrets** in version control
- **Use vault for sensitive data** - API keys, passwords, tokens
- **Validate inputs** before using them
- **Principle of least privilege** - Minimal permissions required
- **No credentials in logs** - Sanitize output

### Code Quality Standards
- **Self-documenting code** - Clear names over comments
- **Comments explain WHY** not what - Code shows what
- **Consistent formatting** - Follow project patterns
- **Meaningful error messages** - Tell user how to fix
- **Test critical paths** - Especially preflight checks

## Ansible Style Rules

### File Modification Preferences

#### blockinfile vs lineinfile Usage
- **Prefer `blockinfile` for all file content modifications**:
  ```yaml
  # PREFERRED: For complex configurations
  - name: Update ~/.bashrc File for the Bash Git Prompt
    blockinfile:
      path: /home/{{ user_login }}/.bashrc
      marker: "# {mark} ANSIBLE MANAGED: Git Bash Prompt"
      block: |
        GIT_PROMPT_ONLY_IN_REPO=1
        GIT_PROMPT_THEME=Solarized
        GIT_PROMPT_START=$PS1
        source ~/.bash-git-prompt/gitprompt.sh
  ```

#### Marker Patterns
- **Use descriptive markers with consistent format**:
```yaml
  marker: "# {mark} ANSIBLE MANAGED: [Purpose Description]"
  marker: "## {mark} [specific purpose] for {{ user_login}}"  # For sudoers
  marker: "\" {mark} [Purpose]"  # For vim configs
  marker: "-- {mark} ANSIBLE MANAGED: [Purpose]"  # For Lua configs
  ```

### Package Management Patterns

#### Package Module Usage
- **Use `package` module for simple installations**:
  ```yaml
  - name: Basic packages
    package:
      name: "{{ packages }}"
      state: present
    vars:
      packages:
        - vim
        - wget
        - bash-completion
  ```

- **Use `dnf` module for Fedora-specific features**:
  ```yaml
  - name: Install with DNF-specific options
    dnf:
      name:
        - docker-ce
        - docker-ce-cli
        - containerd.io
  ```

#### Package List Organization
- **Group packages logically with descriptive comments**:
  ```yaml
  packages:
    # Essential system tools
    - vim
    - wget
    - bash-completion
    # Development dependencies  
    - gcc
    - gcc-c++
    - cmake
  ```

### Service Management Patterns

#### SystemD Service Handling
- **Use consistent systemd service patterns**:
  ```yaml
  # For system services
  - name: Enable and Start Service
    systemd:
      name: service_name
      state: started
      enabled: yes

  # For user services
  - name: Enable User Service
    systemd:
      name: docker
      state: started
      enabled: yes
      scope: user
  ```

#### Service Restart via Handlers
- **Use handlers for service restarts triggered by config changes**:
  ```yaml
  tasks:
    - name: Update Config
      lineinfile:
        path: /path/to/config
        line: "setting = value"
      notify: restart-service

  handlers:
    - name: restart-service
      systemd:
        name: service_name
        state: restarted
  ```

### User vs System Configuration Patterns

#### Privilege Escalation
- **Be explicit about become usage**:
  ```yaml
  # System-level changes
  - name: System Configuration
    become: true
    blockinfile:
      path: /etc/config

  # User-level changes with specific user
  - name: User Configuration  
    become: true
    become_user: "{{ user_login }}"
    command: user_specific_command
  ```

#### File Ownership and Permissions
- **Always set appropriate ownership for user files**:
  ```yaml
  - name: User SSH Config
    blockinfile:
      path: "/home/{{ user_login }}/.ssh/config"
      create: true
      owner: "{{ user_login }}"
      group: "{{ user_login }}"
      mode: '0600'
  ```

### Error Handling and Validation Patterns

#### Idempotency with creates
- **Use `creates` parameter for shell commands that should run once**:
  ```yaml
  - name: Install from URL
    shell: |
      wget https://example.com/installer.sh
      chmod +x installer.sh && ./installer.sh
    args:
      creates: /usr/bin/installed_binary
  ```

#### Validation and Assertions
- **Include preflight checks for critical requirements**:
  ```yaml
  - name: Check System Requirements
    assert:
      that:
        - ansible_version.full is version_compare('2.9.9', '>=')
        - ansible_distribution == 'Fedora'
      fail_msg: 'System requirements not met'
  ```

### Variable Usage Patterns

#### Variable Naming Conventions
- **Use descriptive, consistent variable names**:
  ```yaml
  vars:
    root_dir: "{{ inventory_dir }}/../../"
    pyenv_versions:
      - 3.11.9
      - 3.12.4
  ```

#### Template References
- **Consistently reference template variables**:
  ```yaml
  # Always use the root_dir pattern for file references
  copy:
    src: "{{ root_dir }}/files{{ item }}"
    dest: "{{ item }}"
  ```

### Task Organization and Naming

#### Task Names
- **Use descriptive, action-oriented task names**:
  ```yaml
  - name: Install YQ Binary from GitHub
  - name: Set up SSH Config for LXC Containers  
  - name: Enable Flathub Repository
  - name: Copy SSH ID to root User
  ```

#### Task Grouping
- **Group related tasks with `block` when appropriate**:
  ```yaml
  - name: Vim Configuration Setup
    block:
      - name: Get Colourscheme
        get_url:
          url: https://raw.githubusercontent.com/ajmwagar/vim-deus/master/colors/deus.vim
          dest: /usr/share/vim/vimfiles/colors/deus.vim

      - name: Vim Configs
        blockinfile:
          marker: "\" {mark} Vim Colourscheme"  
          block: colors deus
          path: /etc/vimrc.local
          create: true
  ```

#### Tagging Strategy
- **Use meaningful tags for selective execution**:
  ```yaml
  tags:
    - packages      # Package installation tasks
    - yq            # Specific tool installation
    - sysctl        # System configuration changes
    - pyenv         # Python environment setup
  ```

### Special Configuration Patterns

#### Multi-file Loop Operations
- **Use loops for applying same operation to multiple files**:
  ```yaml
  - name: Ensure Bash Tweaks are Loaded
    blockinfile:
      marker: "# {mark} ANSIBLE MANAGED: Bash Tweaks"
      block: source /etc/profile.d/zz_lts-fedora-desktop.bash
      path: "{{ item }}"
      create: false
    loop:
      - /root/.bashrc
      - /root/.bash_profile
      - /home/{{ user_login }}/.bashrc
      - /home/{{ user_login }}/.bash_profile
  ```

#### External Repository Integration
- **Use shell modules with proper error handling for external installs**:
  ```yaml
  - name: Install from External Source
    shell: |
      set -x  # Enable command echoing for debugging
      command1 && \
      command2 && \
      command3
    args:
      executable: /bin/bash
      creates: /expected/result/file
  ```

### Code Quality Guidelines

#### Comments and Documentation
- **Include relevant comments for complex operations**:
  ```yaml
  # Set Controller Mode to bredr to Fix Bluetooth Headphones
  # @see https://gitlab.freedesktop.org/pipewire/pipewire/-/wikis/Performance-tuning
  ```

#### Conditional Logic
- **Keep conditional logic simple and readable**:
  ```yaml
  # Use when conditions sparingly and clearly
  when: ansible_distribution == 'Fedora'
  ```

These style rules ensure consistency across the project, improve maintainability, and follow Ansible best practices while accommodating the specific needs of Fedora desktop configuration management.

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