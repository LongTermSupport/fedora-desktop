# Ansible Playbook Audit — Full Repository

**Date**: 2026-04-03
**Method**: Manual review of all playbooks, vars, and environment files
**Scope**: `playbooks/`, `environment/`, `vars/`
**Total findings**: 20 (4 High, 5 Medium, 4 Low, 7 Informational)

## High Severity (4)

### H1: Curl piped to bash (3 playbooks)
- **File**: `playbooks/imports/play-nvm-install.yml:24`
- **File**: `playbooks/imports/play-python.yml:62`
- **File**: `playbooks/imports/optional/common/play-rust-dev.yml:97`
- **Description**: Downloads and executes scripts directly from the network without inspection. Violates security best practices.
- **Fix**: Use `ansible.builtin.get_url` to download first, verify checksums, then execute.

### H2: Duplicate shebang lines (7 playbooks)
- **Files**:
  - `playbooks/imports/play-claude-yolo.yml:1-3`
  - `playbooks/imports/play-podman.yml:1-3`
  - `playbooks/imports/play-python.yml:1-3`
  - `playbooks/imports/play-vscode.yml:1-3`
  - `playbooks/imports/play-gnome-shell.yml:1-3`
  - `playbooks/imports/play-systemd-user-tweaks.yml:1-3`
  - `playbooks/imports/play-github-cli-multi.yml:1-3`
- **Description**: `#!/usr/bin/env ansible-playbook` appears twice (lines 1 and 3). Could cause parsing issues.
- **Fix**: Remove the duplicate shebang on line 3.

### H3: Non-reproducible `state: latest` (4 tasks)
- **File**: `playbooks/imports/play-python.yml:51` — pipx pdm
- **File**: `playbooks/imports/play-python.yml:58` — pipx huggingface_hub
- **File**: `playbooks/imports/optional/hardware-specific/play-nvidia.yml:29` — dnf
- **File**: `playbooks/imports/optional/common/play-cloudflare-warp.yml:21` — dnf
- **Description**: `state: latest` prevents reproducible deployments — different versions installed on each run.
- **Fix**: Use `state: present` (accept whatever is installed or install default version).

### H4: Personal information in public repository
- **File**: `environment/localhost/host_vars/localhost.yml`
- **Lines**: 1-3, 21, 39-48
- **Description**: Real usernames, email addresses, and account mappings committed to public repo:
  - `user_login`, `user_name`, `user_email` — real personal details
  - `qobuz_username` — real email address (unencrypted)
  - `lastpass_accounts` — real email-to-account mappings
  - `github_accounts` — real GitHub usernames
  - NordVPN username (partially exposed)
- **Note**: Passwords and API keys are correctly vault-encrypted. Only the non-secret identifiers are exposed. The project's own SecurityRules.md prohibits "Personal information", "Account mappings", and "Sensitive examples" in version control.
- **Fix**: Either vault-encrypt the sensitive identifiers, move to a gitignored file, or accept as known risk and document the exception.

## Medium Severity (5)

### M1: Missing file permissions on copy/blockinfile tasks
- **Files**: Multiple playbooks
- **Description**: Several `blockinfile` and file modification tasks don't explicitly set `owner:`, `group:`, `mode:`. While these may inherit, AnsibleStyle.md requires explicit specification.
- **Fix**: Add explicit `owner`, `group`, `mode` to all file modification tasks.

### M2: Non-idempotent shell commands without `creates:` guard
- **File**: `playbooks/imports/play-basic-configs.yml:219` — `grub2-editenv` with `changed_when: true`
- **File**: `playbooks/imports/play-git-configure-and-tools.yml:39` — Git clone without checking if already cloned
- **File**: `playbooks/imports/play-vscode.yml:12-21` — Complex shell adding repos/installing
- **Description**: Shell commands that run every time without idempotency checks.
- **Fix**: Add `creates:` parameter or `when:` conditionals.

### M3: Overly permissive socket mode
- **File**: `playbooks/imports/optional/common/play-speech-to-text.yml:67`
- **Description**: ydotool socket configured with `socket-perm=0666` (world-writable).
- **Fix**: Use more restrictive `0660` or `0600`.

### M4: Inconsistent `changed_when` usage
- **File**: `playbooks/imports/play-basic-configs.yml:220` — GRUB menu command
- **File**: `playbooks/imports/play-basic-configs.yml:240` — fwupd update command
- **Description**: `changed_when: true` on commands that don't necessarily change anything. Makes idempotency checks unreliable.
- **Fix**: Properly detect actual changes or use `changed_when: false` for read-only operations.

### M5: Complex conditionals that could be simplified
- **File**: `playbooks/imports/play-basic-configs.yml:123-124`
- **File**: `playbooks/imports/play-prevent-ssh-suspend.yml:54-61`
- **Description**: Complex inline calculations and multi-line gsettings commands that could be clearer.
- **Fix**: Split into multiple tasks or use variables for clarity.

## Low Severity (4)

| ID | File | Description |
|----|------|-------------|
| L1 | Various older playbooks | Some bare module names instead of FQCN (`copy:` vs `ansible.builtin.copy:`) |
| L2 | Various blockinfile tasks | Some marker comments could be more descriptive |
| L3 | play-git-configure-and-tools.yml:70 | Marker block mixes git prompt and SSH agent concerns |
| L4 | Various | Inconsistent task naming style (some use Title Case, others don't) |

## Informational (7)

| ID | Description |
|----|-------------|
| I1 | All reviewed tasks have explicit `name:` attributes — good compliance |
| I2 | Most copy tasks correctly set owner/group/mode — good baseline |
| I3 | Vault encryption correctly used for all passwords and API keys |
| I4 | Proper `become: true` usage throughout |
| I5 | Good use of `vars_files` for version-dependent playbooks |
| I6 | Consistent `hosts: desktop` pattern across playbooks |
| I7 | Good use of `blockinfile` with descriptive markers in most cases |
