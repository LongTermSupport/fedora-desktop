# Configuration Guide

Learn how to customize your Fedora desktop configuration.

## Quick Reference

**Common tasks:**

- [Change user settings](#user-configuration) - Name, email, vault password
- [Customize bash prompt](#prompt-color-configuration) - Color preferences
- [Manage secrets](#vault-configuration) - API keys and passwords
- [Add custom configurations](#adding-custom-configurations) - Your own playbooks
- [Debug issues](#troubleshooting-configuration) - Configuration problems

**Important files:**

- `environment/localhost/host_vars/localhost.yml` - Your settings (plain YAML with encrypted string values)
- `vault-pass.secret` - Vault password (gitignored)
- `/etc/profile.d/zz_lts-fedora-desktop.bash` - Custom bash configs
- `~/.ssh/config` - SSH configuration

## User Configuration

### Host Variables

Edit `environment/localhost/host_vars/localhost.yml` to customize:

```yaml
user_login: "your-username"
user_name: "Your Full Name"
user_email: "your.email@example.com"
```

### Prompt Color Configuration

During installation, you'll be prompted to choose a PS1 color:

- Red
- Green
- Yellow
- Blue
- Magenta
- Cyan
- White

This is stored in `/var/local/ps1-prompt-colour` and used by the bash prompt system.

### Vault Configuration

This project uses **variable-level** encryption, not file-level encryption.
`environment/localhost/host_vars/localhost.yml` is a **plain YAML file** containing
`!vault |` encrypted string values — `ansible-vault view/edit` will error on it.

Edit the file in any normal text editor. To encrypt a new secret value:

```bash
# Encrypt a single value and print the !vault block to paste into localhost.yml
ansible-vault encrypt_string 'sensitive-value' --name 'variable_name'
```

The vault password is stored in `vault-pass.secret` (gitignored).

See `CLAUDE/SecurityRules.md` ("Vault Management") for the full workflow.

## System Configuration

### DNF Optimization

Automatically configured in `/etc/dnf/dnf.conf`:

```ini
max_parallel_downloads=10
```

### Bash Environment

Custom configurations in `/etc/profile.d/zz_lts-fedora-desktop.bash`:

- Enhanced history (20K file size, 10K memory)
- Custom aliases
- Docker helper functions
- Error state prompt indicators

User-specific includes in `~/.bashrc-includes/`:

- Custom scripts and functions
- Per-user overrides

### SSH Configuration

Ed25519 keys generated at:

- `~/.ssh/id` (private key)
- `~/.ssh/id.pub` (public key)

SSH config for LXC containers in `~/.ssh/config`:

```
Host "10.0.*.*"
    IdentityFile ~/.ssh/id_lxc
    UserKnownHostsFile=/dev/null
    StrictHostKeyChecking=no
```

The `Host` pattern matches the LXC bridge subnet (`10.0.x.x`), and the containers are
reached with the dedicated `~/.ssh/id_lxc` key.

### Git Configuration

Automatically configured from host variables:

```bash
git config --global user.name "Your Name"
git config --global user.email "your.email@example.com"
```

Bash Git Prompt with Solarized theme in:

- `~/.bash-git-prompt/`
- Loaded in `.bashrc`

## Core Feature Configuration

These plays are imported by `playbook-main.yml` and run automatically on every
provisioning run — there is nothing to enable.

### Docker (optional — not imported by the main playbook)

`playbooks/imports/optional/common/play-docker.yml` installs Docker as a **rootful**
compatibility engine when a tool needs it (e.g. DDEV). Podman remains the rootless
default — see [Container Engines](../CLAUDE/ContainerEngines.md):

- User added to the `docker` group
- Systemd service enabled
- `docker-compose-plugin` installed, providing `docker compose`

### GitHub Multi-Account

Configure in `host_vars/localhost.yml`:

```yaml
github_accounts:
  personal: "your-personal-username"
  work: "your-work-username"
```

To authenticate a new account (with the required OAuth scopes) and deploy:

```bash
./scripts/gh-account-setup.bash --add=alias:username
ansible-playbook playbooks/imports/play-github-cli-multi.yml
```

See the full guide for the complete workflow, commands, and troubleshooting:
[GitHub Multi-Account Management](github-multi-account.md).

### GNOME Settings

`play-gsettings.yml` applies:

- Disable the Caps Lock key (via xkb-options)
- Disable middle-click closing tabs in the Ptyxis terminal

## Optional Features Configuration

These require running their playbook explicitly.

### LastPass Accounts

Configure in `host_vars/localhost.yml`:

```yaml
lastpass_accounts:
  personal: "you@example.com"
  work: "work@example.com"
```

### Audio Configuration

HD audio setup (`play-hd-audio.yml`) configures:

- PipeWire default sample rate: 48000 Hz, with dynamic switching allowed up to 192000 Hz
- Bluetooth codecs: LDAC, aptX HD
- Low latency settings

## Adding Custom Configurations

### Custom Playbooks

Create in `playbooks/imports/optional/` under the appropriate category (`common/`, `hardware-specific/`, or `experimental/`):

```yaml
- hosts: desktop
  name: My Custom Configuration
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
  tasks:
    - name: My task
      # Your tasks here
```

### Custom Files

Place static files in:

- `files/etc/` for system configs
- `files/home/` for user configs
- `files/var/` for variable data

Use in playbooks:

```yaml
- name: Copy custom config
  copy:
    src: "{{ root_dir }}/files/etc/myconfig"
    dest: /etc/myconfig
    owner: root
    group: root
    mode: '0644'
```

### Custom Variables

Add to `environment/localhost/host_vars/localhost.yml`:

```yaml
my_custom_var: "value"
my_secret: !vault |
  $ANSIBLE_VAULT;1.2;AES256;localhost
  [encrypted content]
```

## Ansible Patterns

### File Modifications

Preferred method using `blockinfile`:

```yaml
- name: Update config file
  blockinfile:
    path: /path/to/file
    marker: "# {mark} ANSIBLE MANAGED: Description"
    block: |
      configuration line 1
      configuration line 2
```

### Service Management

```yaml
- name: Enable and start service
  systemd:
    name: service-name
    state: started
    enabled: yes
    daemon_reload: yes
```

### Package Installation

```yaml
- name: Install packages
  package:
    name:
      - package1
      - package2
    state: present
```

## Troubleshooting Configuration

### Check Applied Configuration

```bash
# View Ansible facts
ansible desktop -m setup

# Check specific configuration
ansible desktop -m shell -a "grep max_parallel /etc/dnf/dnf.conf"

# List installed packages
ansible desktop -m package_facts
```

### Reset Configuration

To reset a configuration managed by `blockinfile`:

1. Remove the marked block from the file
2. Re-run the playbook

### Debug Playbook Execution

```bash
# Verbose output
ansible-playbook playbook.yml -vvv

# Check mode (dry run)
ansible-playbook playbook.yml --check

# Step through tasks
ansible-playbook playbook.yml --step
```
