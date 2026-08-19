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
    # Project root, anchored on ansible.cfg's location (never moves from the repo root).
    # Do NOT use "{{ inventory_dir }}/../../" — inventory_dir is a host-scoped magic var
    # that is UNDEFINED during the early vars_files evaluation pass, so any
    # `vars_files: - "{{ root_dir }}/..."` entry silently skips (the "skipping vars_files
    # item due to an undefined variable" warning) before being re-evaluated. The config
    # lookup is host-independent and resolves on the first pass — 0 warnings, no skip.
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
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
      - ansible_facts['distribution'] == 'Fedora'
    fail_msg: 'System requirements not met'
```

### Facts: always `ansible_facts[...]`, never the top-level `ansible_<fact>` alias

Ansible also injects every gathered fact as a top-level variable, so
`ansible_facts['distribution']` is reachable as `ansible_distribution`. That
injection (`INJECT_FACTS_AS_VARS`) is **deprecated and removed in ansible-core
2.24**, at which point every such reference becomes an undefined variable — an
error mid-play, on the machine, after earlier tasks have already changed state.

Use the dictionary form everywhere:

```yaml
{{ ansible_facts['env']['HOME'] }}                    # not ansible_env.HOME
{{ ansible_facts['distribution'] }}                   # not ansible_distribution
{{ ansible_facts['distribution_major_version'] }}     # not ansible_distribution_major_version
```

This applies to bare expressions in `when:` and `assert: that:` exactly as it
does inside `{{ }}`. Enforced by `scripts/qa-ansible.bash` (Check 5), which keys
on a known fact-name list rather than the `ansible_` prefix, so `ansible_facts`
itself and `ansible_version` are unaffected.

---

## Variable Naming and Templates

### Naming Conventions

```yaml
vars:
  root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
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

### Provisioning Profile Self-Guard (Plan 00061)

This repo provisions **both** a Fedora desktop and a headless Fedora server from
the same source tree. Which one is being built is auto-detected — with zero
flags — into the `provisioning_profile` variable
(`environment/localhost/group_vars/desktop.yml`), from `systemctl get-default`:
`graphical.target` → `desktop`, anything else → `server` (server-biased when
uncertain).

**Every playbook declares a `scope`** in its play-level `vars:` block — one of
`general | gnome | server`:

```yaml
- hosts: desktop
  name: <Play Name>
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
    scope: general   # general | gnome | server — see CLAUDE/AnsibleStyle.md
  tasks:
    ...
```

- **`general`** — no task needs a GUI session, a display server, or a GUI app.
  Carries **no** guard; its first task is its first real task.
- **`gnome`** — needs a GUI session (the bucket means "needs a desktop", not
  literally GNOME — e.g. an LXDE install is `gnome`-bucketed).
- **`server`** — headless-only (no core/optional play is `server` today, but the
  value exists for symmetry).

A `gnome`- or `server`-scoped play **must** carry this exact 2-task guard as its
**first two tasks** (byte-identical everywhere — the QA gate checks the text):

```yaml
    - name: Scope guard — assert provisioning_profile is recognised
      ansible.builtin.assert:
        that:
          - provisioning_profile in ['desktop', 'server']
        fail_msg: |
          provisioning_profile={{ provisioning_profile }} is not recognised.
          Valid values: desktop, server.
          Auto-detected from `systemctl get-default`
          (see environment/localhost/group_vars/desktop.yml) or overridden
          via -e provisioning_profile=desktop|server.

    - name: Scope guard — end play if provisioning_profile does not match declared scope
      ansible.builtin.meta: end_play
      when: (scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')
```

The guard makes **every play safe to run standalone** — `ansible-playbook playbooks/imports/play-firefox.yml` auto-detects and self-gates, no
`playbook-main.yml` needed. Override detection with
`-e provisioning_profile=desktop|server`.

**A `general` play with one or two GUI-only tasks** stays `general` and gates
just those tasks with `when: provisioning_profile != 'server'` (see
`play-vpn.yml`, `play-basic-configs.yml`, `play-container-watch.yml`) rather than
scoping the whole play `gnome`.

All of the above — the `scope` value, the exact guard text on `gnome`/`server`
plays, and the *absence* of a guard on `general` plays — is enforced by
`scripts/qa-ansible.bash` (Check 4), which runs inside `./scripts/qa-all.bash`.

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

Every multi-command `shell: |` block **must** begin with `set -euo pipefail` and declare `args: executable: /bin/bash`. The shell module only propagates the **last** command's exit code — without strict mode, any earlier failure is silently swallowed, violating the project's #1 fail-fast rule.

```yaml
- name: Install Tool from External Repo
  ansible.builtin.shell: |
    set -euo pipefail
    curl -fsSL https://example.com/installer.sh -o /tmp/installer.sh
    bash /tmp/installer.sh
  args:
    executable: /bin/bash
    creates: /usr/bin/tool-binary
```

`set -x` (tracing) is optional and additive — it does not substitute for `set -e`. Use `creates:` or `changed_when:` for idempotency.

---

## Code Quality

- **Include relevant comments** for complex operations, especially `@see` links for non-obvious choices
- **Keep conditional logic simple** and readable (e.g., `when: ansible_distribution == 'Fedora'`)
- **Self-documenting code** — clear names over comments
- **Comments explain WHY** not what
- **Meaningful error messages** — tell user how to fix
