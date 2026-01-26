# Claude Code Configuration

## ⚠️ CRITICAL: CCY CONTAINER ENVIRONMENT DETECTION

**IF THE PROJECT PATH IS `/workspace/` - YOU ARE IN A CCY CONTAINER**

When working in the CCY container environment:

### What CCY Container IS:
- ✅ **Development environment** for editing files
- ✅ **Git operations** (commit, push, pull)
- ✅ **File manipulation** (read, write, edit)
- ✅ **Code review and analysis**
- ✅ **Testing bash syntax** with `bash -n`

### What CCY Container IS NOT:
- ❌ **Target system** for Ansible playbooks
- ❌ **Fedora host** with user `joseph`
- ❌ **System with systemd services**
- ❌ **Environment with real users/groups**

### ABSOLUTE RULES FOR CCY CONTAINER:

1. **NEVER run Ansible playbooks**
   - ❌ `ansible-playbook playbooks/...`
   - The container does NOT have the target users, groups, or system state
   - Playbooks are configured for the HOST system, not the container

2. **Only edit and commit**
   - ✅ Edit playbook files
   - ✅ Commit changes to git
   - ✅ Push to remote
   - Then tell the USER to run the playbook on their HOST system

3. **Correct workflow from CCY container**:
   ```bash
   # In CCY container (/workspace/):
   vim playbooks/imports/play-something.yml    # Edit
   git add playbooks/imports/play-something.yml
   git commit -m "Update playbook"
   git push

   # Then instruct USER to run on HOST:
   # "On your host system, run:"
   # ansible-playbook ~/Projects/fedora-desktop/playbooks/imports/play-something.yml
   ```

4. **How to detect you're in CCY container**:
   - Project path is `/workspace/`
   - Running as `root` user in container
   - User `joseph` does not exist as a real user (only as mount point)
   - No systemd, no real Fedora environment

**REMEMBER: In CCY container = EDIT ONLY, DEPLOY ON HOST**

### ⚠️ CRITICAL: CCY VERSION BUMP REQUIREMENT

**ALWAYS bump CCY_VERSION when modifying `files/var/local/claude-yolo/claude-yolo`**

The CCY script has hash validation to detect modifications without version bumps. A pre-commit hook enforces this requirement.

**Rules:**
1. **ANY code change requires a version bump** (patches are fine for small fixes)
2. **Update the version comment** to describe what changed
3. **Never commit CCY changes without bumping the version**

**Version numbering (Semantic Versioning):**
- **Patch (x.y.Z)**: Bug fixes, minor improvements, documentation
- **Minor (x.Y.0)**: New features, backward compatible changes
- **Major (X.0.0)**: Breaking changes, major refactoring

**Example:**
```bash
# Before (version 3.0.0)
CCY_VERSION="3.0.0"  # Removed sessions, simplified state management

# After making a fix (bump to 3.0.1)
CCY_VERSION="3.0.1"  # Fix: persist sessions in .claude/ccy/
```

**What happens if you forget:**
- Pre-commit hook will **REJECT** the commit
- Users will see "DEVELOPER ERROR: CCY script modified without version bump"
- Deployment issues and confusion about what version is running

**This applies to:**
- `files/var/local/claude-yolo/claude-yolo` (main CCY wrapper)
- Any file with version tracking

### ⚠️ MANDATORY: Run QA Scripts Before Committing

**ALWAYS run QA scripts before committing changes to Bash or Python files.**

The project includes QA scripts that validate syntax and catch common errors:

```bash
# Run ALL QA checks (recommended)
./scripts/qa-all.bash

# Or run individual checks
./scripts/qa-bash.bash    # Bash syntax validation
./scripts/qa-python.bash  # Python syntax + linting
```

**Rules:**
1. **Run `./scripts/qa-all.bash` before EVERY commit** that touches Bash or Python files
2. **Fix all errors** before committing - QA failures indicate broken code
3. **Do not skip QA** - even for "small" changes

**What QA catches:**
- ✅ Bash syntax errors (`bash -n` validation)
- ✅ Python syntax errors (`python3 -m py_compile`)
- ✅ Common Python issues (via `ruff` if installed)

**What QA does NOT catch (known limitations):**
- ❌ **Runtime API incompatibilities** - e.g., calling a library method with parameters it no longer accepts
- ❌ **Import errors** - missing dependencies only fail at runtime
- ❌ **Logic errors** - code that runs but produces wrong results

**For Python files that use external libraries** (like `wsi-stream` using RealtimeSTT):
- After editing, **manually test the script** to verify it works
- Library APIs can change between versions
- Syntax checking alone is not sufficient for integration code

**Example workflow:**
```bash
# 1. Make changes
vim files/home/.local/bin/wsi-stream

# 2. Run QA
./scripts/qa-all.bash

# 3. If QA passes, deploy and TEST the actual script
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
~/.local/bin/wsi-stream --help  # Verify it imports/runs

# 4. Only then commit
git add files/home/.local/bin/wsi-stream
git commit -m "fix: update wsi-stream"
```

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

### ⚠️ PUBLIC REPOSITORY WARNING

**THIS IS A PUBLIC REPOSITORY - EXTREME CAUTION REQUIRED**

This repository is publicly accessible on GitHub. **NEVER** commit:

- ❌ **Personal information** - Names, email addresses, usernames, account IDs
- ❌ **Local configuration** - File paths with usernames, home directories, hostnames
- ❌ **Credentials** - API keys, tokens, passwords, SSH keys, certificates
- ❌ **Private data** - IP addresses, internal URLs, company information
- ❌ **Sensitive examples** - Real usernames in code examples or comments
- ❌ **Vault passwords** - vault-pass.secret is gitignored for a reason
- ❌ **Debug output** - Logs or error messages containing sensitive data
- ❌ **Account mappings** - Hardcoded user-to-account associations

**ALWAYS use:**
- ✅ **Generic placeholders** - `user`, `example.com`, `<username>`, `{{ user_login }}`
- ✅ **Ansible variables** - Reference variables instead of hardcoded values
- ✅ **Ansible Vault** - Encrypt ALL sensitive data in host_vars/localhost.yml
- ✅ **Dynamic detection** - Query systems at runtime (e.g., `gh api user`, `ssh -T`)
- ✅ **Documentation variables** - Use `{{ user_login }}` in examples, never real usernames
- ✅ **Gitignore** - Keep sensitive files out of git (.credentials, .secret, etc.)

**Before committing:**
1. Review ALL changes with `git diff`
2. Search for usernames: `git diff | grep -i "yourname"`
3. Check for email addresses: `git diff | grep -E "[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}"`
4. Verify no hardcoded paths: `git diff | grep "/home/"`
5. Confirm no tokens/keys visible: `git diff | grep -E "(token|key|password|secret)"`

**If accidentally committed:**
1. DO NOT just delete in next commit - it's still in git history
2. Use `git filter-branch` or BFG Repo-Cleaner to purge from history
3. Rotate ALL exposed credentials immediately
4. Inform team/users if credentials were pushed to remote

This is not paranoia - it's basic security hygiene for public repositories.

**Automated Protection - Git Hooks:**

This repository uses version-controlled git hooks to prevent accidental leaks:
- **`scripts/git-hooks/pre-commit`**: Scans staged files for sensitive patterns
- **`scripts/git-hooks/commit-msg`**: Validates commit messages for sensitive information

The hooks are automatically configured and enforced by:
- **Initial Setup**: `run.bash` calls `play-git-hooks-security.yml` during bootstrap
- **Ongoing Enforcement**: `playbook-main.yml` includes `play-git-hooks-security.yml`
- **Verification**: Ansible validates hooks exist, are executable, and are configured

This ensures:
- ✓ Hooks are tracked in version control and distributed with the repository
- ✓ All contributors automatically get the latest hook versions
- ✓ Updates to hooks are pulled with normal `git pull`
- ✓ Hook configuration is verified every time the main playbook runs
- ✓ No way to accidentally disable the hooks

**What the hooks protect against:**
- ✓ API keys, tokens, passwords, SSH keys
- ✓ Private email domains (.dev, .internal, .corp, .local)
- ✓ Specific username patterns (e.g., acme-username.token)
- ✓ Hardcoded paths with usernames
- ✓ Private IP addresses

**Safe placeholders allowed:**
- ✓ example_user, test_user, {{ user_login }}
- ✓ user@example.com, admin@example.com
- ✓ 192.168.x.x, 10.x.x.x

**Manual installation** (for existing clones):
```bash
cd ~/Projects/fedora-desktop
git config core.hooksPath scripts/git-hooks
```

**Note:** Hooks can be bypassed with `git commit --no-verify`, but this is **strongly discouraged**.

### ⚠️ INFRASTRUCTURE AS CODE - ANSIBLE-ONLY DEPLOYMENT

**THIS IS AN ANSIBLE-MANAGED INFRASTRUCTURE PROJECT**

All system changes, deployments, and configurations MUST be performed through Ansible playbooks. Manual operations are PROHIBITED.

**NEVER recommend or perform manual actions:**
- ❌ **Manual file copies** - `sudo cp file /path/` or `cp file dest`
- ❌ **Manual installations** - `sudo dnf install`, `npm install -g`, `pip install`
- ❌ **Manual configuration edits** - Direct editing of system files
- ❌ **Manual service management** - `systemctl enable/start` commands
- ❌ **Manual downloads** - `curl | bash` or `wget` scripts
- ❌ **Manual symlinks** - `ln -s` operations

**ALWAYS use Ansible:**
- ✅ **Create/update playbooks** - Write or modify existing playbooks
- ✅ **Use Ansible modules** - `copy`, `package`, `service`, `file`, `get_url`
- ✅ **Ensure idempotency** - Playbooks must be safe to run multiple times
- ✅ **Test playbook changes** - Verify with `--check` or `--diff` flags
- ✅ **Version control** - All infrastructure changes tracked in git

**Deployment workflow:**
1. Modify files in `/workspace/files/` directory structure
2. Update or create playbook in `/workspace/playbooks/imports/`
3. Ensure playbook properly copies/deploys modified files
4. Test with `ansible-playbook playbook.yml --check`
5. Deploy with `ansible-playbook playbook.yml`
6. Document changes in commit message

**Why this matters:**
- **Reproducibility** - Entire system can be rebuilt from git
- **Auditability** - All changes tracked in version control
- **Consistency** - Same process works on fresh install or updates
- **Idempotency** - Safe to re-run without breaking existing setup
- **Documentation** - Playbooks serve as executable documentation

**Example - WRONG approach:**
```bash
# ❌ BAD - Manual deployment
sudo cp files/var/local/claude-yolo/claude-yolo /var/local/claude-yolo/
sudo chmod +x /var/local/claude-yolo/claude-yolo
```

**Example - CORRECT approach:**
```bash
# ✅ GOOD - Ansible deployment
ansible-playbook playbooks/imports/optional/common/play-install-claude-yolo.yml
```

**If you catch yourself or the user suggesting manual steps:**
1. STOP immediately
2. Identify the Ansible playbook that should handle this
3. Verify the playbook will correctly deploy the changes
4. If playbook is missing/incomplete, UPDATE THE PLAYBOOK FIRST
5. Then recommend running the playbook

This is a hard rule with NO exceptions.

### ⚠️ TESTING WORKFLOW - DEPLOY FIRST, TEST SECOND

**CRITICAL: Even for "quick tests", ALWAYS deploy through Ansible first.**

The correct workflow is:
1. **Edit source files** in the repository (e.g., `extensions/`, `files/`)
2. **Update/create playbook** to deploy those files
3. **Run the playbook** to deploy to the system
4. **Test the deployed result** on the system

**NEVER do this:**
- ❌ `pip install package` to "test if it works"
- ❌ Creating files directly in `~/.local/bin/` or `/usr/local/bin/`
- ❌ Running `dnf install` to "check if the package exists"
- ❌ Manually copying files to "see if the config works"
- ❌ Any command that modifies system state outside of Ansible

**Why "just testing" manually is still WRONG:**
- Creates drift between repo and system state
- Makes debugging harder (is the bug in the playbook or the manual config?)
- Builds bad habits that lead to undocumented changes
- Defeats the entire purpose of Infrastructure as Code
- The "quick test" often becomes the permanent state

**Correct testing workflow:**
```bash
# 1. Edit files in repo
vim extensions/my-extension/script.sh

# 2. Update playbook to deploy
vim playbooks/imports/optional/common/play-my-feature.yml

# 3. Deploy via Ansible
ansible-playbook playbooks/imports/optional/common/play-my-feature.yml

# 4. Test the deployed result
~/.local/bin/script.sh --test
```

**For package availability checks:**
```bash
# ✅ GOOD - Just query, don't install
dnf info package-name
pip index versions package-name

# ❌ BAD - Actually installs
pip install package-name
dnf install package-name
```

**If something doesn't work after Ansible deployment:**
1. Fix the PLAYBOOK, not the system
2. Re-run the playbook
3. Test again
4. Repeat until working

**The playbook IS the source of truth. The system state is just a reflection of it.**

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
- **Languages**: Python 3, Node.js 20, Golang
- **Editors**: Vim (customized), VS Code, PyCharm Community
- **Containers**: Podman (recommended), Docker (optional), LXC, Toolbox
  - **Podman**: Rootless by default, better performance and security on Linux, fast filesystem
  - **Docker**: Available as optional install for legacy project compatibility
- **Security**: Ansible Vault, SSH key management
- **Package Sources**: DNF repos, Flatpak, RPM Fusion

### Dependencies
- **Ansible Collections**:
  - `community.general`
  - `ansible.posix`
- **System Packages**:
  - Base: `vim`, `wget`, `bash-completion`, `htop`, `python3-libdnf5`
  - Development: `git`, `gh`, `ripgrep`, `jq`, `openssl`
  - Containers: `podman`, `podman-compose`
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
**IMPORTANT: This project uses VARIABLE-level encryption, NOT file-level encryption**

- **Vault password**: Stored in `vault-pass.secret` (gitignored)
- **Vault ID**: Uses `localhost` vault ID with matching enforced (`vault_id_match=true`)
- **Encryption method**: Individual sensitive values encrypted with `ansible-vault encrypt_string`
- **File format**: `environment/localhost/host_vars/localhost.yml` is a regular YAML file with encrypted string values
- **Editing**: Use a regular text editor (vim, nano, etc.) - DO NOT use `ansible-vault edit`
- **Encrypting new values**: `ansible-vault encrypt_string 'secret' --name 'var_name'`
- **SSH keys**: Copied to root user for system operations

**Example of variable-level encryption:**
```yaml
# Regular unencrypted variables
user_login: example_user
user_name: Example User

# Encrypted variables (created with ansible-vault encrypt_string)
lastfm_api_key: !vault |
  $ANSIBLE_VAULT;1.2;AES256;localhost
  66386439653162636163623333...
```

### Permissions
- Passwordless sudo configured for main user
- SSH key authentication for GitHub
- Proper file permissions on sensitive files (`mode: 0600`)

## Special Features

### Custom Shell Environment
- **Prompt System**: Dynamic PS1 with color coding and error states
- **Git Integration**: bash-git-prompt with Solarized theme
- **Container Helpers**: Node.js container wrapper functions (Podman/Docker)
- **Aliases**: Comprehensive set for development workflow
- **History**: Enhanced history management (20K file size, 10K memory)

### GitHub Multi-Account Management
**Location**: `playbooks/imports/optional/common/play-github-cli-multi.yml:1`

The project supports multiple GitHub accounts with separate SSH keys and convenient shell functions.

#### Initial Setup
Run the playbook to configure accounts for the first time:
```bash
ansible-playbook ./playbooks/imports/optional/common/play-github-cli-multi.yml
```

The playbook will prompt you to enter accounts in format: `alias:username,alias:username`
Example: `work:johndoe-work,personal:johndoe,oss:johndoe-oss`

#### Adding a New Account to Existing Setup

**IMPORTANT**: The playbook skips configuration prompts if accounts already exist. To add a new account:

1. **Edit the host vars file directly** (it's a regular YAML file, not encrypted):
   ```bash
   vim environment/localhost/host_vars/localhost.yml
   ```

2. **Add the new account** to the `github_accounts` section:
   ```yaml
   # GitHub CLI accounts
   github_accounts:
     work: "johndoe-work"      # existing
     personal: "johndoe"       # existing
     oss: "johndoe-oss"        # <-- ADD NEW ACCOUNT HERE
   ```

3. **Re-run the playbook** to complete setup:
   ```bash
   ansible-playbook ./playbooks/imports/optional/common/play-github-cli-multi.yml
   ```

The playbook will:
- Generate SSH key for the new account (`~/.ssh/github_oss`)
- Add SSH config entry for `github.com-oss`
- Regenerate bash aliases/functions with all accounts
- Prompt for `gh auth login` for the new account

#### Available Commands
After setup, these functions are available in your shell:

```bash
# Account management
gh-list                    # List all configured accounts
gh-whoami                  # Show currently active account
gh-status                  # Check authentication status for all accounts
gh-switch work             # Switch to a specific account
github-test-ssh            # Test SSH connections for all accounts

# Account-specific commands (using work account as example)
gh-work pr list            # Run gh command as work account
clone-work owner/repo      # Clone repo using work account
remote-work owner/repo     # Set git remote for work account
gh-token-work              # Get GitHub token for work account
gh-work-make-default       # Set work as default account
```

#### Configuration Files
- **Account definitions**: `environment/localhost/host_vars/localhost.yml` (under `github_accounts`)
- **SSH keys**: `~/.ssh/github_<alias>` and `~/.ssh/github_<alias>.pub`
- **SSH config**: `~/.ssh/config` (separate blocks per account)
- **Bash functions**: `~/.bashrc-includes/gh-aliases.inc.bash` (regenerated on each run)

#### Removing an Account
1. Edit `environment/localhost/host_vars/localhost.yml` and remove the account from `github_accounts`
2. Re-run the playbook - bash functions will be regenerated without that account
3. Manually remove SSH keys and config if desired:
   ```bash
   rm ~/.ssh/github_<alias>*
   # Edit ~/.ssh/config to remove the account's block
   ```

### Hardware Support
- **Graphics**: NVIDIA driver support
- **Audio**: HD audio configurations
- **Power**: TLP battery optimization
- **Display**: DisplayLink driver support
- **Firmware**: Automatic fwupd updates

### Optional Components
- **Development**: VS Code, Python environments, Golang
- **Containers**: Docker (legacy project support)
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
- **Distrobox issues**: Check container logs with `podman logs <container-id>` or recreate container with playbook

### Debug Commands
```bash
# Check Ansible connectivity
ansible desktop -m ping

# View host variables (file is not encrypted, safe to use vim/cat)
cat environment/localhost/host_vars/localhost.yml
# Or decrypt individual encrypted string values:
# ansible localhost -m debug -a var="variable_name"

# Test specific component
ansible desktop -m setup --tree untracked/facts/

# Check DNF configuration
cat /etc/dnf/dnf.conf | grep max_parallel

# Debug distrobox containers
distrobox list                           # List all containers
podman logs <container-id>               # View container logs
journalctl --user -u podman              # View podman service logs
podman ps -a                             # Show all containers including stopped
podman inspect <container-name>          # Detailed container configuration
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