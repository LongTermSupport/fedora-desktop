# Codebase Analysis: fedora-desktop Setup Flow

> Compiled: 2026-03-01
> Purpose: Comprehensive reference of everything run.bash and the Ansible playbooks do, organized to help plan kickstart integration.

---

## Table of Contents

1. [run.bash: Complete Step-by-Step Analysis](#1-runbash-complete-step-by-step-analysis)
2. [playbook-main.yml: Import Chain](#2-playbook-mainyml-import-chain)
3. [Core Playbook Details](#3-core-playbook-details)
4. [Optional Playbook Inventory](#4-optional-playbook-inventory)
5. [Variables That Must Be Collected](#5-variables-that-must-be-collected)
6. [Configuration Files (ansible.cfg, vars/, requirements.yml)](#6-configuration-files)
7. [files/ Directory Structure](#7-files-directory-structure)
8. [Ansible Inventory Structure](#8-ansible-inventory-structure)
9. [Dependency Graph](#9-dependency-graph)
10. [Kickstart Integration Analysis](#10-kickstart-integration-analysis)

---

## 1. run.bash: Complete Step-by-Step Analysis

`run.bash` is the bootstrap entry point. It has 15 numbered steps (tracked by `STEP_TOTAL=15`), plus additional logic for optional playbooks and a reboot prompt.

### Pre-Step: Assertions and Environment

- **Must not be root**: Exits with error if `whoami` is `root`.
- **Bash strict mode**: `set -e`, `set -u`, `set -o pipefail`, IFS set to `\n\t`.
- **Fedora version check**: Reads `vars/fedora-version.yml` for `fedora_version`, compares against `/etc/os-release` `VERSION_ID`. Exits on mismatch. This check only works when run from within the repo (checks `./vars/fedora-version.yml`).

### Step 1: Installing System Dependencies

```
sudo dnf -y install git python3 python3-pip python3-libdnf5 grubby jq openssl pipx
```

Packages installed:
- `git` - Version control
- `python3` - Python runtime
- `python3-pip` - Python package installer
- `python3-libdnf5` - Required by Ansible's dnf module on Fedora
- `grubby` - Bootloader configuration tool
- `jq` - JSON processor
- `openssl` - Cryptographic toolkit (used for vault password generation)
- `pipx` - Python application installer (used to install Ansible)

### Step 2: Checking for Legacy Grub Configurations

Checks for and removes old `systemd.unified_cgroup_hierarchy` kernel arguments from all kernels using `grubby`. This is cleanup from older Docker configurations.

### Step 3: Setting up Ansible Environment

```bash
pipx install --include-deps ansible
pipx inject ansible jmespath
pipx inject ansible passlib
pipx inject ansible ansible-lint
```

Also creates a symlink for `ansible-lint` from `~/.local/share/pipx/venvs/ansible/bin/ansible-lint` to `~/.local/bin/`.

**Important for kickstart**: Ansible is installed via `pipx`, not via `dnf`. In a kickstart `%post` or firstboot context, `dnf -y install ansible-core` would be the alternative. The `pipx` approach installs the full `ansible` package (not just `ansible-core`), which includes more modules.

### Step 4: Creating SSH Key Pair

- Checks if `~/.ssh/id` exists.
- If not, prompts for a password (with confirmation).
- Creates an Ed25519 key: `ssh-keygen -t ed25519 -f ~/.ssh/id -P "$password"`
- **REQUIRES interactive input**: Password must be entered twice.

### Step 5: Set Custom Hostname

- Checks if hostname is the default `fedora`.
- If so, prompts for a new hostname and sets it with `hostnamectl set-hostname`.
- **REQUIRES interactive input** (only if hostname is default).

### Step 6: Installing GitHub CLI

```bash
sudo dnf -y install 'dnf-command(config-manager)'
sudo dnf config-manager addrepo --from-repofile=https://cli.github.com/packages/rpm/gh-cli.repo
sudo dnf -y install gh
```

Checks if the `gh-cli` repo already exists before adding.

### Step 7: GitHub Authentication Setup

- Adds `export GH_HOST="github.com"` to `~/.bashrc` (if not present).
- Checks `gh auth status`. If not authenticated, runs `gh auth login`.
- **REQUIRES interactive input**: Browser-based OAuth flow. User is instructed to choose SSH authentication method.
- Includes a `ghCheckTokenPermission` helper function for verifying OAuth scopes.

### Step 8: Verifying GitHub Account Configuration

- Gets GitHub username from `gh api user` (CLI auth).
- Gets GitHub username from `ssh -T git@github.com -i ~/.ssh/id` (SSH key).
- Compares the two. Exits with error if they do not match.
- **Requires**: Both `gh` auth and SSH key to be configured and pointing at the same GitHub account.

### Step 9: Configuring GitHub SSH Access

- Checks for `admin:public_key` OAuth permission, requests it if missing.
- Gets SSH key fingerprint from `~/.ssh/id.pub`.
- Checks if the key is already on GitHub via `gh api user/keys`.
- If not, adds it with `gh ssh-key add` (authentication type, not signing).
- **Requires network and GitHub authentication**.

### Step 10: Updating SSH Known Hosts

- Removes existing GitHub entries from `known_hosts`.
- Fetches fresh GitHub host keys from `https://api.github.com/meta`.
- Appends them to `~/.ssh/known_hosts`.

### Step 11: Setting up Project Directory

Creates `~/Projects` if it does not exist.

### Step 12: Cloning Configuration Repository

```bash
cd ~/Projects
git clone git@github.com:LongTermSupport/fedora-desktop.git
```

- Uses SSH (not HTTPS) to clone.
- **Requires**: SSH key on GitHub and known_hosts configured.
- Skips if `~/Projects/fedora-desktop` already exists.

### Step 13: User Configuration Setup (conditional)

Only runs if `~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml` does not exist.

Prompts for:
- **user_login** - System username (e.g., `joseph`)
- **user_name** - Full display name (e.g., `Joseph`)
- **user_email** - Email address (e.g., `joseph@example.com`)

Writes a minimal `localhost.yml`:
```yaml
user_login: "joseph"
user_name: "Joseph"
user_email: "joseph@example.com"
```

### Step 14 (unnumbered): Updating Repository

```bash
cd ~/Projects/fedora-desktop
git pull
```

### Step 14 (title): Configuring Git Security Hooks

Runs `play-git-hooks-security.yml` with `--ask-become-pass`.

### Step 15: Ansible Vault Configuration

- Checks if `vault-pass.secret` exists.
- If not, prompts for a vault password.
- If left blank, auto-generates one: `openssl rand -base64 32`.
- Writes the password to `vault-pass.secret`.

### Step 15 (continued): Running Ansible Playbooks

```bash
ansible-galaxy install -r requirements.yml
./playbooks/playbook-main.yml [--ask-become-pass]
```

- Runs `ansible-galaxy` to install role and collection dependencies.
- Runs the main playbook. If `sudo -n true` works (passwordless sudo already configured), runs without `--ask-become-pass`. Otherwise asks for the become password.

### Post-Main: Optional Playbooks Menu

After the main playbook completes, presents an interactive menu system for optional playbooks:

1. **Common optional playbooks** - Scanned from `playbooks/imports/optional/common/`
2. **Hardware-specific playbooks** - Scanned from `playbooks/imports/optional/hardware-specific/` with hardware detection (NVIDIA, DisplayLink, battery/TLP)
3. **Untested playbooks** - Scanned from `playbooks/imports/optional/untested/` with a strong warning
4. **Experimental playbooks** - Scanned from `playbooks/imports/optional/experimental/` (listed but not run from menu)

Each playbook can be run individually, all at once, or skipped. Failed playbooks offer to create a GitHub issue automatically (with log sanitization and optional Claude Code AI sanitization).

### Final: System Reboot

Prompts user to reboot. If confirmed, runs `sudo reboot now`.

---

## 2. playbook-main.yml: Import Chain

File: `/home/joseph/Projects/fedora-desktop/playbooks/playbook-main.yml`

The main playbook imports 16 playbooks in order:

```
1.  imports/play-AA-preflight-sanity.yml
2.  imports/play-basic-configs.yml
3.  imports/play-systemd-user-tweaks.yml
4.  imports/play-nvm-install.yml
5.  imports/play-git-configure-and-tools.yml
6.  imports/optional/common/play-github-cli-multi.yml
7.  imports/play-git-hooks-security.yml
8.  imports/play-lxc-install-config.yml
9.  imports/play-ms-fonts.yml
10. imports/play-rpm-fusion.yml
11. imports/play-toolbox-install.yml
12. imports/optional/common/play-docker.yml
13. imports/play-podman.yml
14. imports/optional/common/play-python.yml
15. imports/play-claude-code.yml
16. imports/optional/common/play-install-claude-yolo.yml
```

Note: Two optional playbooks (`play-github-cli-multi.yml`, `play-docker.yml`, `play-python.yml`, `play-install-claude-yolo.yml`) are imported directly in the main chain, not just available in the optional menu. This means they run as part of the standard setup.

---

## 3. Core Playbook Details

### 3.1 play-AA-preflight-sanity.yml

- **Hosts**: desktop
- **Become**: true
- **Vars files**: `vars/fedora-version.yml`
- **Purpose**: Validates that the environment meets requirements.
- **Checks**:
  - Ansible version >= 2.9.9
  - OS is Fedora
  - Distribution major version matches `fedora_version` from vars
- **Kickstart compatibility**: Fully compatible. No interactive input, no service management.

### 3.2 play-basic-configs.yml

- **Hosts**: desktop
- **Become**: true
- **Purpose**: Core system configuration. This is the most comprehensive core playbook.
- **Tasks**:
  1. **PS1 prompt colour**: Checks `/var/local/ps1-prompt-colour`. If not present, pauses with an interactive prompt to select colour. Options: white, whiteBold, red, redBold, green, greenBold, yellow, yellowBold, blue, blueBold, purple, purpleBold, lightblue, lightblueBold. Default: `lightblueBold`.
  2. **Ensure jq**: Includes `tasks/ensure-jq.yml` (installs jq via dnf).
  3. **Basic packages**: vim, wget, bash-completion, htop, python3-libdnf5, multitail, figlet.
  4. **Passwordless sudo**: Adds `user_login` to sudoers with NOPASSWD.
  5. **Vim configuration**: Downloads deus colourscheme, configures `/etc/vimrc.local`.
  6. **Bash tweaks**: Copies three files from `files/`:
     - `/etc/profile.d/zz_lts-fedora-desktop.bash` (aliases, history, prompt, docker-node helpers)
     - `/var/local/colours` (colour function library)
     - `/var/local/ps1-prompt` (PS1 prompt rendering logic)
  7. **Prompt colour file**: Writes `/var/local/ps1-prompt-colour` with `export PS1_COLOUR=<chosen>`.
  8. **Bashrc includes directories**: Creates `~/.bashrc-includes` for both root and user.
  9. **Source bash tweaks**: Adds `source /etc/profile.d/zz_lts-fedora-desktop.bash` to `.bashrc` and `.bash_profile` for both root and user.
  10. **Bashrc includes sourcing**: Adds a block to `.bashrc` that sources all files in `~/.bashrc-includes/`.
  11. **shutdown-with-update script**: Copies to `/usr/local/bin/` and deploys alias bashrc include.
  12. **USB audio fix**: Deploys bashrc include for both root and user.
  13. **Copy SSH keys to root**: Copies `~/.ssh/id` and `~/.ssh/id.pub` to `/root/.ssh/`.
  14. **Install YQ**: Downloads from GitHub releases to `/usr/bin/yq`.
  15. **DNF parallel downloads**: Adds `max_parallel_downloads=10` to `/etc/dnf/dnf.conf`.
  16. **Hardware firmware update**: Runs `fwupdmgr` commands (get-devices, refresh, get-updates, update).

- **Variables used**: `user_login`
- **Interactive input**: PS1 colour prompt (only on first run, then cached in `/var/local/ps1-prompt-colour`)
- **Requires existing SSH keys**: Yes (copies `~/.ssh/id` to root)
- **Kickstart concerns**:
  - PS1 colour prompt requires interaction (or pre-configuration of `/var/local/ps1-prompt-colour`)
  - SSH key copy requires keys to exist
  - fwupdmgr may fail in chroot/firstboot without full hardware access

### 3.3 play-systemd-user-tweaks.yml

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Disables aggressive systemd-oomd memory pressure killing for user services.
- **Tasks**:
  - Creates `~/.config/systemd/user/user.slice.d/50-oom-override.conf`
  - Sets `ManagedOOMMemoryPressure=auto`
  - Verifies configuration with `systemctl --user show user.slice`
- **Variables used**: `user_login`
- **Kickstart concerns**: Verification step requires running systemd user session. File creation is fine in any context.

### 3.4 play-nvm-install.yml

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Installs NVM (Node Version Manager) and Node.js LTS.
- **Tasks**:
  - Installs curl, wget, bash-completion via dnf
  - Downloads NVM install script
  - Configures NVM in bashrc and bash_profile
  - Installs Node.js LTS and sets it as default
- **Variables used**: `user_login`, `nvm_version` (hardcoded 0.40.1), `node_version` (hardcoded `lts/*`)
- **Kickstart concerns**: Needs network access. All tasks are file/download operations, should work in firstboot.

### 3.5 play-git-configure-and-tools.yml

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Configures git and installs git tools.
- **Tasks**:
  - Sets git global config: user.email, user.name, color.ui, push.default, core.editor (vim), init.defaultBranch (main), fetch.prune, pull.rebase false, etc.
  - Creates global gitignore at `~/.config/git/ignore`
  - Clones bash-git-prompt
  - Configures bashrc for git prompt
  - Installs GitHub CLI (dnf) - with `creates: /usr/bin/gh` guard
  - Installs git-filter-repo (dnf)
  - Adds git aliases (gu, gd, gs, c) to bashrc
- **Variables used**: `user_login`, `user_email`, `user_name`
- **Kickstart concerns**: Network needed for cloning bash-git-prompt. GitHub CLI install duplicates run.bash step 6.

### 3.6 play-github-cli-multi.yml (in main chain)

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Multi-account GitHub CLI setup with SSH keys, aliases, and account switching.
- **Variables used**: `user_login`, `github_accounts` (dict from localhost.yml, may not exist on first run)
- **Heavy interactive input**: Multiple pause prompts for:
  - Configuration type choice (single/multi/skip)
  - Account details (usernames, aliases)
  - SSH key addition instructions (requires user to open browser)
  - GitHub CLI authentication (requires browser-based OAuth in another terminal)
- **Kickstart concerns**: **Cannot be automated**. Requires interactive browser-based authentication. Must remain in Phase 3 (interactive session).

### 3.7 play-git-hooks-security.yml

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Configures git to use tracked hooks from `scripts/git-hooks/`.
- **Tasks**: Verifies repo structure, sets `core.hooksPath` to `scripts/git-hooks`.
- **Variables used**: None (uses `$HOME` env var)
- **Kickstart concerns**: Requires the repo to be cloned. Otherwise compatible.

### 3.8 play-lxc-install-config.yml

- **Hosts**: desktop
- **Become**: true
- **Purpose**: Installs and configures LXC containers.
- **Tasks**:
  - Enables `ganto/lxc4` Copr repository
  - Installs lxc, lxc-templates
  - Sets SELinux to permissive
  - Starts and enables lxc service
  - Configures firewall (lxcbr0 to trusted zone)
  - Creates insecure SSH key for container access (`~/.ssh/id_lxc`)
  - Configures SSH for LXC containers
  - Configures DHCP for lxcbr0
  - Clones `lxc-bash` repo via SSH (git@github.com:LongTermSupport/lxc-bash.git)
  - Adds bash completion
  - Increases inotify limits
  - Loads kernel modules (ip_tables, iptable_filter, iptable_nat, iptable_mangle)
- **Variables used**: `user_login`
- **Kickstart concerns**:
  - Starts services (lxc) - needs systemd
  - Clones via SSH - needs GitHub SSH access
  - SELinux change is significant

### 3.9 play-ms-fonts.yml

- **Hosts**: desktop
- **Become**: true
- **Purpose**: Installs Microsoft core fonts.
- **Tasks**:
  - Installs curl, cabextract, xorg-x11-font-utils, fontconfig
  - Installs msttcore-fonts-installer from SourceForge via `rpm -i`
- **Kickstart concerns**: Network needed. Compatible with firstboot.

### 3.10 play-rpm-fusion.yml

- **Hosts**: desktop
- **Become**: true (within tasks)
- **Purpose**: Installs RPM Fusion free and nonfree repos, multimedia codecs.
- **Tasks**: Single shell block that:
  - Installs RPM Fusion free and nonfree release RPMs
  - Enables fedora-cisco-openh264
  - Updates @core
  - Installs multimedia group (with `--allowerasing`)
  - Installs intel-media-driver
- **Kickstart concerns**: Network needed. Can work in %post or firstboot.

### 3.11 play-toolbox-install.yml (actually JetBrains Toolbox)

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Despite the filename, installs JetBrains Toolbox (not Fedora Toolbox).
- **Tasks**:
  - Checks if JetBrains Toolbox is installed
  - Downloads latest release from JetBrains API
  - Unpacks and runs installer
  - Launches Toolbox for system integration
- **Kickstart concerns**: Needs network. Launches a GUI application (`nohup ... &`). Should be deferred to interactive session.

### 3.12 play-docker.yml (in main chain)

- **Hosts**: desktop
- **Become**: false (with selective become: true)
- **Purpose**: Installs Docker CE in rootless mode.
- **Tasks**:
  - Adds Docker CE repo
  - Installs docker-ce, docker-ce-cli, containerd.io, buildx, compose
  - Sets up UID/GID maps in `/etc/subuid` and `/etc/subgid`
  - Runs `dockerd-rootless-setuptool.sh install`
  - Enables and starts user-level docker systemd service
- **Variables used**: `user_login`
- **Kickstart concerns**: Needs systemd for service start. Rootless setup needs running system.

### 3.13 play-podman.yml

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Installs Podman and podman-compose.
- **Tasks**:
  - Installs podman via dnf
  - Installs podman-compose via pip (--user)
  - Enables and starts podman.socket (user scope)
  - Verifies with `podman info`
- **Variables used**: `user_login`
- **Kickstart concerns**: User-level systemd service needs running session.

### 3.14 play-python.yml (in main chain)

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Installs Python development tools and multiple Python versions.
- **Tasks**:
  - Installs build dependencies (gcc, cmake, zlib-devel, etc.)
  - Installs PDM (via pipx)
  - Installs Hugging Face CLI (via pipx)
  - Installs pyenv
  - Configures pyenv in bashrc/bash_profile
  - Installs Python versions: 3.11.13, 3.12.11, 3.13.1
- **Variables used**: `user_login`, `pyenv_versions` (hardcoded)
- **Kickstart concerns**: Heavy compilation (building Python from source). Network needed. Time-consuming.

### 3.15 play-claude-code.yml

- **Hosts**: desktop
- **Become**: false
- **Purpose**: Installs Claude Code CLI.
- **Tasks**:
  - Installs system dependencies (ripgrep, curl, wget, git, openssh-clients)
  - Installs Claude Code via official installer script
  - Configures bashrc integration (PATH, alias `cc`)
- **Variables used**: `user_login`
- **Kickstart concerns**: Network needed. Compatible with firstboot.

### 3.16 play-install-claude-yolo.yml (in main chain)

- **Hosts**: desktop
- **Become**: false
- **Vars files**: `vars/container-defaults.yml`
- **Purpose**: Installs Claude Code YOLO container-based mode.
- **Tasks**:
  - Verifies container engine (podman or docker) is installed and running
  - Creates directory structure under `/opt/claude-yolo/`
  - Copies Dockerfile, entrypoint script, documentation, skills, library scripts
  - Copies wrapper script to `/var/local/claude-yolo/`
  - Deploys bashrc includes
  - Creates token directories
  - Builds container image
  - Cleans up retired artifacts
- **Variables used**: `user_login`, `container_engine` (from `vars/container-defaults.yml`, default: `podman`)
- **Kickstart concerns**: Needs container engine running. Heavy (container build).

---

## 4. Optional Playbook Inventory

### 4.1 optional/common/ (28 playbooks)

| Playbook | Purpose |
|----------|---------|
| `play-advanced-kernel-management.yml` | Kernel version management |
| `play-cloudflare-warp.yml` | Cloudflare WARP VPN client |
| `play-docker.yml` | Docker CE rootless install (also in main chain) |
| `play-fast-file-manager.yml` | Fast file manager installation |
| `play-firefox.yml` | Firefox policy configuration |
| `play-github-cli-multi.yml` | GitHub multi-account setup (also in main chain) |
| `play-gnome-shell-dev.yml` | GNOME Shell development tools |
| `play-gnome-shell-extensions.yml` | GNOME Shell extensions |
| `play-gnome-shell.yml` | GNOME Shell configuration |
| `play-golang.yml` | Golang development environment |
| `play-gsettings.yml` | GNOME gsettings configuration |
| `play-hd-audio.yml` | HD audio configuration |
| `play-install-claude-devtools.yml` | Claude Code development tools |
| `play-install-claude-yolo.yml` | Claude Code YOLO mode (also in main chain) |
| `play-install-distrobox.yml` | Distrobox container manager |
| `play-install-flatpaks.yml` | Flatpak application installation |
| `play-install-lightweight-ides.yml` | Lightweight IDE installation |
| `play-install-markless.yml` | Markless tool installation |
| `play-install-terminal-emulators.yml` | Terminal emulator installation |
| `play-lastpass.yml` | LastPass CLI |
| `play-nordvpn-openvpn.yml` | NordVPN OpenVPN configuration |
| `play-python.yml` | Python development setup (also in main chain) |
| `play-qobuz-cli.yml` | Qobuz music CLI |
| `play-rust-dev.yml` | Rust development environment |
| `play-speech-to-text.yml` | Speech to text tooling |
| `play-vpn.yml` | VPN configuration |
| `play-vscode.yml` | VS Code installation |

### 4.2 optional/hardware-specific/ (3 playbooks)

| Playbook | Purpose | Detection |
|----------|---------|-----------|
| `play-displaylink.yml` | DisplayLink driver | USB device detection |
| `play-laptop-lid-power-management.yml` | Laptop power management | Battery detection |
| `play-nvidia.yml` | NVIDIA driver | PCI device detection |

### 4.3 optional/experimental/ (4 playbooks)

| Playbook | Purpose |
|----------|---------|
| `play-docker-in-lxc-support.yml` | Docker in LXC containers |
| `play-docker-overlay2-migration.yml` | Docker storage migration |
| `play-lxde-install.yml` | LXDE desktop environment |
| `play-virtualbox-windows.yml` | VirtualBox for Windows VMs |

### 4.4 optional/archived/ (1 playbook)

| Playbook | Purpose |
|----------|---------|
| `play-tlp-battery-optimisation.yml` | TLP battery optimization (archived) |

---

## 5. Variables That Must Be Collected

### 5.1 Variables Collected by run.bash (Interactive)

| Variable | Source | When Collected | Required For | Can Be Pre-Set |
|----------|--------|---------------|--------------|----------------|
| **user_login** | Interactive prompt | Step 13 (if localhost.yml missing) | All playbooks | Yes - write to localhost.yml |
| **user_name** | Interactive prompt | Step 13 (if localhost.yml missing) | Git config, user creation | Yes - write to localhost.yml |
| **user_email** | Interactive prompt | Step 13 (if localhost.yml missing) | Git config | Yes - write to localhost.yml |
| **SSH key password** | Interactive prompt | Step 4 (if ~/.ssh/id missing) | SSH key generation | Cannot be pre-set without security risk |
| **hostname** | Interactive prompt | Step 5 (if hostname is "fedora") | System hostname | Yes - use `hostnamectl` in kickstart |
| **GitHub auth** | Browser OAuth | Step 7 (if not authenticated) | GitHub operations | Cannot be automated |
| **vault password** | Interactive prompt or auto-generated | Step 15 (if vault-pass.secret missing) | Ansible vault decryption | Yes - auto-generate |

### 5.2 Variables Collected by Ansible Playbooks (Interactive)

| Variable | Source | Playbook | Can Be Pre-Set |
|----------|--------|----------|----------------|
| **PS1_Colour** | Interactive pause prompt | play-basic-configs.yml | Yes - write `/var/local/ps1-prompt-colour` with `export PS1_COLOUR=lightblueBold` |
| **become password** | `--ask-become-pass` | Any playbook with become: true | Yes - configure passwordless sudo first |
| **GitHub account config** | Interactive pause prompt | play-github-cli-multi.yml | Yes - write to localhost.yml (but auth still requires interaction) |

### 5.3 Variables in localhost.yml (host_vars)

```yaml
# Required (unencrypted)
user_login: "joseph"
user_name: "Joseph"
user_email: "joseph@ltscommerce.dev"

# Optional (unencrypted)
lastpass_accounts:
  balli: "joseph@ballicom.co.uk"
  ec: "joseph@edmondscommerce.co.uk"

github_accounts:
  balli: "ballidev"
  lts: "LTSCommerce"
  joseph: "joseph-uk"

# Optional (vault-encrypted)
lastfm_api_key: !vault |
  $ANSIBLE_VAULT;1.2;AES256;localhost
  ...

lastfm_api_secret: !vault |
  $ANSIBLE_VAULT;1.2;AES256;localhost
  ...
```

### 5.4 Variables in vars/ Files

**vars/fedora-version.yml:**
```yaml
fedora_version: 43
```

**vars/container-defaults.yml:**
```yaml
container_engine: podman
```

### 5.5 Complete List of Information Needed Upfront for Full Automation

For a fully pre-configured kickstart setup, these need to be known before installation:

1. **LUKS passphrase** - Disk encryption password
2. **User password** - Login password
3. **user_login** - System username
4. **user_name** - Display name
5. **user_email** - Email address
6. **hostname** - Machine hostname
7. **PS1_Colour** - Prompt colour preference (can use default: `lightblueBold`)
8. **Fedora version / branch** - Which branch to clone (e.g., `F42`, `F43`)

Everything else can either be auto-generated (vault password) or deferred to interactive session (SSH keys, GitHub auth, vault-encrypted secrets).

---

## 6. Configuration Files

### 6.1 ansible.cfg

Location: `/home/joseph/Projects/fedora-desktop/ansible.cfg`

Key settings:
```ini
[defaults]
inventory = ./environment/localhost       # YAML-based localhost inventory
roles_path = ./roles/vendor              # Galaxy/GitHub roles
gathering = smart                        # Fact caching
fact_caching = jsonfile
fact_caching_connection = ./untracked/facts/
retry_files_enabled = False
error_on_undefined_vars = true
any_errors_fatal = true                  # Stop on first error
sudo_flags = -HE                         # Preserve HOME and environment
transport = local                        # Local connection, not SSH

# Vault
ask_vault_pass = False
vault_password_file = ./vault-pass.secret
vault_identity = localhost
vault_id_match = true                    # Only decrypt matching vault ID
```

**Kickstart implications**:
- The `vault_password_file` must exist at `./vault-pass.secret` relative to the project root.
- `transport=local` means no SSH is needed for Ansible execution.
- `any_errors_fatal=true` means any task failure stops the entire playbook run.

### 6.2 vars/fedora-version.yml

```yaml
fedora_version: 43
```

Currently set to 43 on the F42 branch (note: this appears to be the target for the next version, or was recently updated). The kickstart must match this version.

### 6.3 requirements.yml

```yaml
roles:
  - src: https://github.com/LongTermSupport/ansible-role-vault-scripts
    scm: git
    name: lts.vault-scripts
    version: master
collections:
  - name: community.general
  - name: ansible.posix
```

Dependencies:
- **lts.vault-scripts** role from GitHub (via git)
- **community.general** collection (for copr module, pipx module, modprobe, git_config)
- **ansible.posix** collection (for selinux module)

For kickstart, these can be installed via:
```bash
# Collections as RPMs (avoids Galaxy network dependency)
dnf -y install ansible-collection-community-general ansible-collection-ansible-posix

# Role still needs git clone or Galaxy
ansible-galaxy install -r requirements.yml
```

---

## 7. files/ Directory Structure

The `files/` directory contains static configuration files that are copied to the target system by various playbooks.

```
files/
├── etc/
│   ├── firefox/
│   │   └── policies/
│   │       └── policies.json               # Firefox enterprise policies
│   ├── profile.d/
│   │   └── zz_lts-fedora-desktop.bash      # Main bash customization (aliases, history, prompt, docker-node)
│   └── systemd/
│       └── system/
│           ├── kernel-version-manager.path  # Path unit for kernel management
│           └── kernel-version-manager.service # Service for kernel management
├── home/
│   ├── .config/
│   │   └── speech-to-text/
│   │       ├── claude-prompt-article.txt    # Claude prompt templates
│   │       ├── claude-prompt-corporate.txt
│   │       ├── claude-prompt-humanize.txt
│   │       └── claude-prompt-natural.txt
│   ├── .local/
│   │   └── bin/
│   │       ├── ccdt                        # Claude Code dev tools script
│   │       ├── git-account-helper.j2       # Jinja2 template for git multi-account
│   │       ├── gshell-nested              # GNOME Shell nested session
│   │       ├── nord                       # NordVPN helper
│   │       ├── wsi                        # Whisper speech-to-text
│   │       ├── wsi-article                # Whisper article mode
│   │       ├── wsi-article-window         # Whisper article window
│   │       ├── wsi-claude-process         # Whisper Claude processing
│   │       ├── wsi-model-manager          # Whisper model manager
│   │       ├── wsi-server-manager         # Whisper server manager
│   │       └── wsi-stream                 # Whisper streaming
│   │       └── wsi-stream-server          # Whisper stream server
│   └── bashrc-includes/
│       ├── claude-devtools.bash            # Claude dev tools bash functions
│       ├── claude-yolo.bash                # Claude YOLO bash functions
│       ├── claude-yolo.bash.j2             # Jinja2 template version
│       ├── shutdown-with-update.bash       # Shutdown alias include
│       └── usb-audio-fix.bash             # USB audio fix functions
├── opt/
│   └── claude-yolo/
│       ├── ccy-startup-info.txt           # CCY startup information
│       ├── docs/
│       │   ├── CCY-GUIDE.txt              # CCY usage guide
│       │   └── CUSTOM-DOCKERFILES.txt     # Custom Dockerfile docs
│       └── skills/
│           └── browsing/
│               ├── COMMANDLINE-USAGE.md   # Browsing skill CLI docs
│               ├── EXAMPLES.md            # Browsing skill examples
│               └── SKILL.md               # Browsing skill definition
├── usr/
│   └── local/
│       └── bin/
│           ├── debug-pipewire.bash        # PipeWire debug script
│           ├── manage-kernel-versions.py  # Kernel version management
│           ├── qp                         # Quick play (Qobuz)
│           └── shutdown-with-update       # Shutdown with update script
└── var/
    └── local/
        ├── claude-yolo/
        │   ├── .shellcheckrc              # ShellCheck config
        │   ├── Dockerfile                 # Main CCY Dockerfile
        │   ├── Dockerfile.example-ansible # Ansible example Dockerfile
        │   ├── Dockerfile.example-golang  # Golang example Dockerfile
        │   ├── Dockerfile.project-template # Template Dockerfile
        │   ├── ccy-ctrl-z-patch.js        # Ctrl+Z patch for Claude Code
        │   ├── claude-yolo                # CCY wrapper script
        │   ├── entrypoint.sh              # Container entrypoint
        │   └── lib/
        │       ├── common.bash            # Common helper functions
        │       ├── docker-health.bash     # Docker health checks
        │       ├── dockerfile-custom.bash # Custom Dockerfile handling
        │       ├── network-management.bash # Network management
        │       ├── ssh-handling.bash       # SSH handling
        │       ├── token-management.bash  # Token management
        │       └── ui-helpers.bash        # UI helper functions
        ├── colours                        # Colour function library
        ├── docker-in-lxc                  # Docker in LXC support
        └── ps1-prompt                     # PS1 prompt rendering logic
```

### Key files deployed by playbooks in the main chain:

| File (destination) | Deployed By | Purpose |
|---------------------|-------------|---------|
| `/etc/profile.d/zz_lts-fedora-desktop.bash` | play-basic-configs | Main bash customization |
| `/var/local/colours` | play-basic-configs | Colour function library |
| `/var/local/ps1-prompt` | play-basic-configs | PS1 prompt logic |
| `/var/local/ps1-prompt-colour` | play-basic-configs | PS1 colour setting (generated) |
| `/usr/local/bin/shutdown-with-update` | play-basic-configs | System script |
| `~/.bashrc-includes/shutdown-with-update.bash` | play-basic-configs | User alias |
| `~/.bashrc-includes/usb-audio-fix.bash` | play-basic-configs | USB audio fix |
| `/opt/claude-yolo/*` | play-install-claude-yolo | Container build context |
| `/var/local/claude-yolo/*` | play-install-claude-yolo | CCY wrapper and libraries |
| `~/.bashrc-includes/claude-yolo.bash` | play-install-claude-yolo | CCY bash integration |
| `~/.local/bin/git-account-helper` | play-github-cli-multi | Multi-account git helper |
| `~/.bashrc-includes/gh-aliases.inc.bash` | play-github-cli-multi | GitHub CLI aliases |

---

## 8. Ansible Inventory Structure

```
environment/localhost/
├── hosts.yml                          # Host definitions
└── host_vars/
    └── localhost.yml                  # Per-host variables (contains vault-encrypted data)
```

### hosts.yml

```yaml
desktop:
  hosts:
    localhost:
      vars:
        ansible_host: localhost
        connection: local
        ansible_connection: local
        ansible_python_interpreter: "{{ansible_playbook_python}}"
```

All playbooks target the `desktop` group, which contains only `localhost` with local transport.

### localhost.yml (host_vars)

Contains:
- `user_login` - unencrypted
- `user_name` - unencrypted
- `user_email` - unencrypted
- `lastpass_accounts` - unencrypted dict
- `github_accounts` - unencrypted dict
- `lastfm_api_key` - vault-encrypted
- `lastfm_api_secret` - vault-encrypted

---

## 9. Dependency Graph

### What must happen before what:

```
PHASE: Pre-Ansible (run.bash steps 1-12)
  1. System packages (git, python3, pipx, etc.)
  2. Legacy grub cleanup
  3. Ansible installation (pipx)
  4. SSH key generation [INTERACTIVE]
  5. Hostname setting [INTERACTIVE if default]
  6. GitHub CLI installation
  7. GitHub authentication [INTERACTIVE, BROWSER]
  8. GitHub account verification
  9. GitHub SSH key upload
  10. SSH known hosts
  11. Projects directory
  12. Clone repository [REQUIRES SSH]

PHASE: Configuration (run.bash steps 13-15)
  13. User variable collection [INTERACTIVE, if first run]
  14. Git security hooks
  15a. Vault password [INTERACTIVE or auto-generated]
  15b. ansible-galaxy requirements
  15c. Main playbook execution

PHASE: Main Playbook Chain
  AA. Preflight sanity (Fedora version check)
  ├── Requires: vars/fedora-version.yml
  │
  Basic configs
  ├── Requires: user_login, PS1_Colour [INTERACTIVE first time], SSH keys exist
  │
  Systemd user tweaks
  ├── Requires: user_login, systemd user session for verification
  │
  NVM install
  ├── Requires: user_login, network
  │
  Git configure
  ├── Requires: user_login, user_name, user_email, network
  │
  GitHub CLI multi
  ├── Requires: user_login, github_accounts (optional), [HEAVY INTERACTIVE]
  │
  Git hooks security
  ├── Requires: repo cloned
  │
  LXC install
  ├── Requires: user_login, systemd, GitHub SSH access, network
  │
  MS Fonts
  ├── Requires: network
  │
  RPM Fusion
  ├── Requires: network
  │
  Toolbox (JetBrains)
  ├── Requires: network, GUI for system integration
  │
  Docker
  ├── Requires: user_login, systemd user services
  │
  Podman
  ├── Requires: user_login, systemd user services
  │
  Python
  ├── Requires: user_login, network, build tools
  │
  Claude Code
  ├── Requires: user_login, network
  │
  Claude YOLO
  ├── Requires: user_login, container engine (podman/docker), network
```

### Critical Interactive Touchpoints

The following points require a human at the keyboard and/or a browser:

1. **SSH key password** (run.bash step 4)
2. **Hostname** (run.bash step 5, only if default)
3. **GitHub OAuth** (run.bash step 7, requires browser)
4. **User variables** (run.bash step 13, first run only)
5. **Vault password** (run.bash step 15, or auto-generate)
6. **PS1 colour** (play-basic-configs, first run only)
7. **GitHub multi-account setup** (play-github-cli-multi, heavy interaction)

---

## 10. Kickstart Integration Analysis

### What Can Be Collected in Kickstart %pre

| Data | Collection Method | Notes |
|------|-------------------|-------|
| LUKS passphrase | `read -s` on tty6 | Must be collected interactively |
| User login | `read` on tty6 | Or hardcode/pass via kernel param |
| User password | `read -s` on tty6 | Hash with python crypt |
| Full name | `read` on tty6 | Or derive from GECOS |
| Email address | `read` on tty6 | Store for Ansible |
| Hostname | `read` on tty6 | Or pass via kernel param |
| PS1 colour | `read` on tty6 | Or use default (lightblueBold) |

### What Can Be Done in Kickstart %post

| Action | Feasibility | Notes |
|--------|-------------|-------|
| Install system packages | Yes | dnf works in chroot |
| Add third-party repos (RPM Fusion, Docker, GH CLI) | Yes | dnf works in chroot |
| Install Ansible (via dnf, not pipx) | Yes | `ansible-core` package |
| Install Ansible collections (via dnf RPMs) | Yes | Avoids Galaxy network |
| Clone repo via HTTPS | Yes | Public repo, no auth needed |
| Write localhost.yml | Yes | File operations work |
| Write vault-pass.secret | Yes | Auto-generate with openssl |
| Write PS1 colour file | Yes | Simple file write |
| Configure passwordless sudo | Yes | File operations work |
| Set hostname | Yes | `hostnamectl` works |
| Copy files | Yes | Standard operations |
| Create firstboot service | Yes | `systemctl enable` works |
| Start services | No | systemd is not PID 1 in chroot |
| Run full Ansible playbook | Partial | Many tasks will fail (systemd, services) |
| SSH key generation | Yes | `ssh-keygen` works, but password entry is problematic |
| GitHub authentication | No | Requires browser |
| dconf/gsettings | No | No D-Bus session |
| Build containers | No | No container engine running |

### What Must Be Deferred to Firstboot Service

| Action | Why |
|--------|-----|
| Start/enable services (lxc, docker, podman) | Needs running systemd |
| Build container images (Claude YOLO) | Needs container engine running |
| Install NVM/Node.js | Works but needs user environment |
| Install pyenv/Python versions | Works but time-consuming compilation |
| JetBrains Toolbox | Needs GUI |
| fwupdmgr | Needs hardware access |

### What Must Be Deferred to Interactive Session (Phase 3)

| Action | Why |
|--------|-----|
| SSH key generation with password | Requires interactive password entry |
| GitHub OAuth authentication | Requires browser |
| GitHub SSH key upload | Requires authenticated gh CLI |
| GitHub multi-account setup | Heavy interactive prompts |
| Vault secret encryption | Requires knowing secrets (API keys) |
| LXC repo clone (SSH) | Requires GitHub SSH access |

### Mapping run.bash Steps to Kickstart Phases

| run.bash Step | Kickstart Phase | Method |
|---------------|----------------|--------|
| 1. System deps | %post or %packages | dnf/packages section |
| 2. Grub cleanup | %post | grubby commands |
| 3. Ansible install | %post | `dnf -y install ansible-core` |
| 4. SSH key | Phase 3 (interactive) | Cannot automate password |
| 5. Hostname | %pre or kickstart command | `network --hostname=` or %pre input |
| 6. GitHub CLI | %post or %packages | dnf install |
| 7. GitHub auth | Phase 3 (interactive) | Browser required |
| 8. GitHub verify | Phase 3 (interactive) | Depends on step 7 |
| 9. GitHub SSH | Phase 3 (interactive) | Depends on steps 4, 7 |
| 10. SSH known hosts | Firstboot or Phase 3 | curl + file write |
| 11. Projects dir | %post | mkdir |
| 12. Clone repo | %post (HTTPS) or Firstboot | `git clone https://...` |
| 13. User vars | %pre (collected) + %post (written) | Collected in %pre, written in %post |
| 14. Git hooks | Firstboot | Needs repo cloned |
| 15a. Vault password | %post | Auto-generate |
| 15b. Galaxy requirements | Firstboot | Needs network |
| 15c. Main playbook | Firstboot (partial) + Phase 3 | Split by compatibility |

### Playbook Compatibility Matrix for Kickstart Phases

| # | Playbook | %post | Firstboot | Interactive | Blockers |
|---|----------|-------|-----------|-------------|----------|
| 1 | play-AA-preflight-sanity | Yes | Yes | Yes | None |
| 2 | play-basic-configs | Partial | Mostly | Yes | PS1 prompt (pre-set file), SSH keys (skip copy), fwupdmgr (skip) |
| 3 | play-systemd-user-tweaks | File only | Yes | Yes | Verification needs user session |
| 4 | play-nvm-install | No | Yes | Yes | Needs user env |
| 5 | play-git-configure-and-tools | No | Yes | Yes | Needs user_email, user_name |
| 6 | play-github-cli-multi | No | No | Yes | Heavy interactive, browser auth |
| 7 | play-git-hooks-security | No | Yes | Yes | Needs repo cloned |
| 8 | play-lxc-install-config | No | Partial | Yes | Needs systemd, SSH for clone |
| 9 | play-ms-fonts | Yes | Yes | Yes | Network only |
| 10 | play-rpm-fusion | Yes | Yes | Yes | Network only |
| 11 | play-toolbox-install | No | No | Yes | Needs GUI |
| 12 | play-docker | No | Yes | Yes | Needs systemd |
| 13 | play-podman | No | Yes | Yes | Needs systemd |
| 14 | play-python | No | Yes | Yes | Heavy compilation |
| 15 | play-claude-code | No | Yes | Yes | Needs network |
| 16 | play-install-claude-yolo | No | Yes | Yes | Needs container engine |

### Pre-Configuration Files to Create in %post

To avoid interactive prompts during Ansible runs, these files should be pre-created:

1. **`/var/local/ps1-prompt-colour`** with content: `export PS1_COLOUR=lightblueBold`
2. **`vault-pass.secret`** with auto-generated content: `openssl rand -base64 32`
3. **`environment/localhost/host_vars/localhost.yml`** with user_login, user_name, user_email
4. **`/etc/sudoers.d/99-user-nopasswd`** with passwordless sudo for the user

### Repository Clone Strategy

- **%post**: Clone via HTTPS (public repo, no auth needed): `git clone -b F42 https://github.com/LongTermSupport/fedora-desktop.git`
- **Phase 3 (interactive)**: Switch remote to SSH after GitHub auth: `git remote set-url origin git@github.com:LongTermSupport/fedora-desktop.git`

### Vault Strategy

- **%post/Firstboot**: Auto-generate vault password, create localhost.yml with ONLY unencrypted values. No vault-encrypted data needed for core playbooks.
- **Phase 3 (interactive)**: User encrypts sensitive data (lastfm_api_key, lastfm_api_secret) with `ansible-vault encrypt_string`.

### Tasks to Skip in Automated Context

The following tasks from the main playbook chain will fail or are inappropriate in an automated context and should be skipped or handled differently:

1. **play-basic-configs**: Skip SSH key copy to root (keys do not exist yet), skip fwupdmgr (may timeout or fail without hardware access)
2. **play-github-cli-multi**: Skip entirely (requires interactive browser auth)
3. **play-lxc-install-config**: Skip the `lxc-bash` git clone via SSH (change to HTTPS or defer)
4. **play-toolbox-install**: Skip entirely (JetBrains Toolbox needs GUI)
5. **play-basic-configs PS1 prompt**: Pre-create the colour file to avoid interactive prompt
