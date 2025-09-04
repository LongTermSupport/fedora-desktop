# Configuration Guide

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

Sensitive data is encrypted using Ansible Vault:

```bash
# View encrypted variables
ansible-vault view environment/localhost/host_vars/localhost.yml

# Edit encrypted variables
ansible-vault edit environment/localhost/host_vars/localhost.yml

# Encrypt new data
ansible-vault encrypt_string 'sensitive-value' --name 'variable_name'
```

The vault password is stored in `vault-pass.secret` (gitignored).

## System Configuration

### DNF Optimization

Automatically configured in `/etc/dnf/dnf.conf`:
```ini
fastestmirror=True
max_parallel_downloads=10
deltarpm=True
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
Host lxc-*
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
    User root
```

### Git Configuration

Automatically configured from host variables:
```bash
git config --global user.name "Your Name"
git config --global user.email "your.email@example.com"
```

Bash Git Prompt with Solarized theme in:
- `~/.bash-git-prompt/`
- Loaded in `.bashrc`

## Optional Features Configuration

### Docker

After running `play-docker.yml`:
- User added to docker group
- Systemd service enabled
- Docker compose installed

### GitHub Multi-Account

Configure in `host_vars/localhost.yml`:
```yaml
github_accounts:
  personal: "your-personal-username"
  work: "your-work-username"
```

Then run:
```bash
ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml
```

### LastPass Accounts

Configure in `host_vars/localhost.yml`:
```yaml
lastpass_accounts:
  personal: "personal@email.com"
  work: "work@company.com"
```

### Audio Configuration

HD audio setup (`play-hd-audio.yml`) configures:
- PipeWire sample rate: 192000 Hz
- Bluetooth codecs: LDAC, aptX HD
- Low latency settings

### GNOME Settings

Custom settings via `play-gsettings.yml`:
- Window management
- Keyboard shortcuts
- Desktop behavior

## Adding Custom Configurations

### Custom Playbooks

Create in `playbooks/imports/optional/custom/`:
```yaml
- hosts: desktop
  name: My Custom Configuration
  vars:
    root_dir: "{{ inventory_dir }}/../../"
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