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

During an interactive installation, you'll be prompted to choose a PS1 colour for the hostname
in your prompt. The choices are the functions `/var/local/colours` defines: `white`, `red`,
`green`, `yellow`, `blue`, `purple`, `lightblue`, each also as `…Bold` (default
`lightblueBold`).

Three ways to set it without the prompt, highest priority first:

- `RUN_BASH_PS1_COLOUR` in a headless run (see [headless-provisioning.md](headless-provisioning.md))
- `hostname_overrides.<hostname>.ps1_colour` in `localhost.yml`
- the existing `/var/local/ps1-prompt-colour` file, which the prompt system reads and a re-run keeps

Pick a different colour per machine and you can tell at a glance which box a shell is on.

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

- History that survives many open terminals: every command is written at the next prompt,
  timestamped and never truncated. It lives in `~/.local/state/bash/history`, so a shell that
  never read this file cannot cut it down. Start a command with a space to keep it out.
  History expansions such as `!!` and `!$` are shown on the line for review before they run
  (`histverify`).
- Custom aliases
- Docker helper functions
- Error state prompt indicators

User-specific includes in `~/.bashrc-includes/`:

- Custom scripts and functions
- Per-user overrides
- `history-search.bash` (desktop user only): Ctrl+R searches the history of every terminal.
  It lists commands run in the current directory first, then the current git repository, then
  everything else. The chosen command is put on the prompt for review, never run straight
  away. The directory each command ran in is kept in `~/.local/state/bash/context`, which is
  private to the user like the history file. A command removed from history with
  `history -d` stays in that file until it is edited out.

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
./playbooks/imports/play-github-cli-multi.yml
```

See the full guide for the complete workflow, commands, and troubleshooting:
[GitHub Multi-Account Management](github-multi-account.md).

### GNOME Settings

`play-gsettings.yml` applies:

- Disable the Caps Lock key (via xkb-options)
- Disable middle-click closing tabs in the Ptyxis terminal

### Crash Reporting (ABRT)

`play-basic-configs.yml` sets the ABRT policy instead of leaving Fedora's defaults, and
writes the values in effect into a managed block in `host_vars/localhost.yml` (created
with the project defaults on first run). This is the standard shape for every per-host
option: pass it once with `-e` and the play persists it, or edit the block directly
(pattern: `CLAUDE/AnsibleStyle.md`, *Per-host options*). For ABRT:

```bash
./playbooks/imports/play-basic-configs.yml --tags abrt -e abrt_auto_reporting=false -e abrt_retention_days=7
```

```yaml
abrt_auto_reporting: false   # default true — send anonymous µReports for packaged crashes
abrt_retention_days: 7       # default 30 — a daily timer removes older problem records
```

Auto-reporting only covers crashes in signed Fedora packages; records from unpackaged
or third-party-repo binaries can never be reported, which is why the retention timer
exists — without it they accumulate and `abrt-applet` re-announces them at every login.

The whole policy applies to the desktop profile only: the applet and µReports are
desktop concerns, so the tasks are skipped on the server profile and nothing is persisted
to `localhost.yml` there. On the desktop profile the play installs the two packages it
drives (`abrt` and `abrt-tui`) rather than relying on the image to ship them. A host
provisioned as desktop and later resolved as server keeps its units and its persisted
block; the play does not remove them.

### Commit Signing

Optional, on the desktop you commit from. A self-updating server (below) deploys only
commits signed by your key, so signing is a deliberate act: nothing signs by default.
Generate a signing-only key with a passphrase, and keep it out of `ssh-agent`:

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_signing -C "git signing"
gh ssh-key add ~/.ssh/id_ed25519_signing.pub --type signing --title "git signing"
```

Set `git_signing_key: /home/<user>/.ssh/id_ed25519_signing` in host_vars and run
`play-git-configure-and-tools.yml`. It refuses a key without a passphrase. To release,
run `git sign-deploy` (an empty signed commit) or `git commit -S`, then push. The
signature vouches for every commit below it.

### Unattended Server Self-Update

`playbooks/imports/optional/common/play-self-update.yml` (server profile, Plan 00137).
It is off unless `self_update_enabled: true`. The inputs are listed in
`host_vars/localhost.yml.dist`. `self_update_signing_public_key` is the `.pub` line of
the key above, and `self_update_become_password` is your sudo password, vault-encrypted.
Re-run the play after changing host_vars: the cycle runs with a root-owned copy.

The whole cycle is described in [playbooks.md](playbooks.md#play-self-updateyml). Running
it day to day:

- **Trust.** The server deploys only the newest commit your key signed. Anything pushed
  above that waits, and a bad signature refuses the cycle. The signature vouches for every
  commit below it, so check what you are releasing before `git sign-deploy`.
- **Pausing.** Stop signing. Nothing new is deployed until you do. To turn the cycle off
  entirely, set `self_update_enabled: false` and re-run the play. That removes what it
  installed, and setting it back to true restores it.
- **When it runs.** Nightly at 03:30, within a random 30 minutes. A night missed while
  the server was off runs at the next boot.
- **Reading it.** `sudo fedora-desktop-self-update status` shows the last result, and
  `sudo fedora-desktop-self-update run --dry-run` names the plays the next cycle would run
  while changing nothing. The log is
  `journalctl -u fedora-desktop-self-update -u fedora-desktop-self-update-verify --no-pager | cat`.
  A failure also appears in `fedora-desktop-health` and the login snippet.
- **Slack alerts (optional).** Each server can post its failures, and each completed
  deploy, to a Slack incoming webhook. Declare `self_update_slack_webhook_url`
  vault-encrypted in host_vars. That is the IaC route, and for a headless install it
  comes from the host file in your config repo. Left undeclared, the play asks for it
  when run from a terminal, whether directly or from `run.bash`'s optional-playbook menu,
  then saves it encrypted in `localhost.yml`. Press ENTER to go without; declaring it as
  `""` stops the question. A run with no terminal, such as `run.bash --headless`, cannot
  be asked: it prints one line naming `self_update_slack_webhook_url` and continues with
  no Slack alerts. The message carries the result
  (outcome, phase, time, detail, plays, commit), never a hostname or username, so give
  each server its own webhook or channel to tell them apart. A post Slack does not
  accept is logged in the journal and reported by `fedora-desktop-health`.
- **A clone that differs from its commit.** If the cycle refuses because the deploy clone
  has changed files, or holds files the commit does not, re-running the play will not
  clear it: the play never touches an existing clone. Run the play once with
  `self_update_enabled: false`, which removes the clone, then once with it true, which
  clones and anchors it afresh.

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
