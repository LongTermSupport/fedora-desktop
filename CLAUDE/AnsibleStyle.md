# Ansible Style Rules

## Playbook Structure

### Host and Variable Patterns
- **Target**: `desktop` group (localhost only)
- **Connection**: Local transport (not SSH)
- **Privilege Escalation**: sudo with `-HE` flags
- **Inventory**: YAML-based localhost configuration

**Consistent playbook structure:**
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

**Categorisation:**
- Core playbooks: Essential system setup
- Optional/common: General development tools
- Optional/hardware-specific: Hardware drivers/configs
- Optional/experimental: Bleeding-edge features

### Required Variables
- `fedora_version`: Target Fedora version (from `vars/fedora-version.yml`)
- `user_login`: System username
- `user_name`: Full display name
- `user_email`: Email address
- `lastfm_api_key`/`lastfm_api_secret`: Encrypted API credentials

---

## File Modification Preferences

### blockinfile vs lineinfile
**Prefer `blockinfile` for all file content modifications:**
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

### Marker Patterns
Use descriptive markers with consistent format:
```yaml
marker: "# {mark} ANSIBLE MANAGED: [Purpose Description]"
marker: "## {mark} [specific purpose] for {{ user_login}}"  # For sudoers
marker: "\" {mark} [Purpose]"  # For vim configs
marker: "-- {mark} ANSIBLE MANAGED: [Purpose]"  # For Lua configs
```

---

## Package Management

- **Use `package` module** for simple installations
- **Use `dnf` module** for Fedora-specific features (repos, enablerepo, etc.)
- **Group packages logically** with descriptive comments

---

## Service Management

- **Use consistent systemd patterns**: `state: started`, `enabled: yes`; add `scope: user` for user services
- **Use handlers** for service restarts triggered by config changes: `notify:` in task + `handlers:` block with `state: restarted`

---

## User vs System Configuration

### Privilege Escalation
- `become: true` for system-level tasks
- Add `become_user: "{{ user_login }}"` for user-level tasks

### File Ownership and Permissions
- **Always set** `owner:`, `group:`, `mode:` on every file task

---

## Error Handling and Validation

### Idempotency with creates
```yaml
- name: Install from URL
  shell: |
    wget https://example.com/installer.sh
    chmod +x installer.sh && ./installer.sh
  args:
    creates: /usr/bin/installed_binary
```

### Preflight Assertions
```yaml
- name: Check System Requirements
  assert:
    that:
      - ansible_version.full is version_compare('2.9.9', '>=')
      - ansible_distribution == 'Fedora'
    fail_msg: 'System requirements not met'
```

---

## Variable Naming and Templates

### Naming Conventions
```yaml
vars:
  root_dir: "{{ inventory_dir }}/../../"
  pyenv_versions:
    - 3.11.9
    - 3.12.4
```

### Template References
```yaml
# Always use the root_dir pattern for file references
copy:
  src: "{{ root_dir }}/files{{ item }}"
  dest: "{{ item }}"
```

---

## Task Organisation

### Task Names
Use descriptive, action-oriented names: e.g., "Install YQ Binary from GitHub", "Enable Flathub Repository"

### Task Grouping
Group related tasks with `block` when appropriate

### Tagging Strategy
```yaml
tags:
  - packages      # Package installation tasks
  - yq            # Specific tool installation
  - sysctl        # System configuration changes
  - pyenv         # Python environment setup
```

---

## Special Patterns

### Multi-file Loop Operations
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

### External Repository Integration
Use shell modules with proper error handling: `set -x`, `args.executable: /bin/bash`, `creates:` for idempotency

---

## Code Quality

- **Include relevant comments** for complex operations, especially `@see` links for non-obvious choices
- **Keep conditional logic simple** and readable (e.g., `when: ansible_distribution == 'Fedora'`)
- **Self-documenting code** — clear names over comments
- **Comments explain WHY** not what
- **Meaningful error messages** — tell user how to fix
