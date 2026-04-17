# Plan 033: Add DDEV Installation Playbook

**Status**: In Progress
**Created**: 2026-04-17
**Owner**: Claude
**Priority**: Medium

## Overview

Add an Ansible playbook to install [DDEV](https://ddev.com/) — a Docker-based local development environment for PHP/CMS projects (Drupal, WordPress, Laravel, Magento, etc.). DDEV provides per-project containers with web server, database, and services, configured via a simple `.ddev/` directory in each project.

The playbook will follow the established "repo + dnf install" pattern used by Docker and VS Code, since DDEV publishes a yum repository for Fedora/RHEL.

## Goals

- Install DDEV via its official yum repository on Fedora
- Install mkcert (DDEV dependency for local HTTPS certificates)
- Verify installation and print version
- Ensure Docker dependency is checked before proceeding
- Follow existing playbook conventions (idempotent, fail-fast, executable)

## Non-Goals

- Configuring specific DDEV projects (that's per-project, not system-level)
- Installing Docker (already handled by `play-docker.yml`)
- Switching container provider (project defaults are in `vars/container-defaults.yml`)
- Installing DDEV add-on services

## Context & Background

### DDEV Installation Method

DDEV provides a yum repository for Fedora/RHEL:

```bash
# Repo file at /etc/yum.repos.d/ddev.repo
[ddev]
name=ddev
baseurl=https://pkg.ddev.com/yum/
gpgcheck=0
enabled=1
```

Then `dnf install ddev`. The repo is currently unsigned (`gpgcheck=0`); signed yum support is planned upstream.

Bash completions are auto-installed by the package to `/usr/share/bash-completion/completions/ddev`.

### mkcert

DDEV uses [mkcert](https://github.com/FiloSottile/mkcert) to create locally-trusted HTTPS certificates. After installing mkcert, `mkcert -install` must be run as the user to install the local CA into the system trust store. mkcert is available in Fedora repos (`dnf install mkcert`).

### Docker Dependency

Docker is already installed by `playbooks/imports/play-docker.yml` with rootless setup. DDEV auto-detects Docker. The playbook should verify Docker is present and fail-fast if not.

### Pattern Precedent

Closest existing patterns:

- **Repo + dnf**: `play-docker.yml` (adds repo file, then dnf install)
- **Dependency check**: `play-claude-devtools.yml` (checks container engine, fails if missing)
- **Simple package**: `play-golang.yml`, `play-distrobox.yml`

## Tasks

### Phase 1: Playbook Creation

- [x] ✅ **Task 1.1**: Create `playbooks/imports/optional/common/play-ddev.yml`
  - [x] ✅ Add shebang (`#!/usr/bin/env ansible-playbook`) and set executable
  - [x] ✅ Standard header: `hosts: desktop`, `name:`, `become: false`, `vars:` with `root_dir`
  - [x] ✅ Docker dependency check (probe-then-fail pattern)
  - [x] ✅ Install mkcert via dnf (system package, `become: true`)
  - [x] ✅ Run `mkcert -install` as user (creates local CA)
  - [x] ✅ Add DDEV yum repo file via `copy` (idempotent)
  - [x] ✅ Install ddev via dnf (`become: true`, `update_cache: true`)
  - [x] ✅ Verify installation: `ddev version` (register + debug output)

### Phase 2: QA and Testing

- [x] ✅ **Task 2.1**: Run `./scripts/qa-all.bash` — QA passed (213 files checked)
- [x] ✅ **Task 2.2**: Review playbook against Ansible style rules (CLAUDE/AnsibleStyle.md)
- [x] ✅ **Task 2.3**: Dry-run review: verify idempotency (every task safe to run twice)

### Phase 3: Integration

- [ ] ⬜ **Task 3.1**: Commit playbook with plan reference
- [ ] ⬜ **Task 3.2**: Instruct user to deploy on host: `ansible-playbook playbooks/imports/optional/common/play-ddev.yml`
- [ ] ⬜ **Task 3.3**: User verifies: `ddev version`, `mkcert -version`

## Technical Decisions

### Decision 1: Yum repo vs install script

**Options Considered**:

1. **Yum repo** — Add `/etc/yum.repos.d/ddev.repo`, then `dnf install ddev`
   - Pro: idempotent, dnf manages upgrades, matches Docker pattern
   - Pro: bash completions auto-installed by package
   - Con: repo currently unsigned (`gpgcheck=0`)
2. **Install script** — `curl | bash` installer
   - Pro: officially supported
   - Con: blocked by project's `curl_pipe_shell` hook
   - Con: less idempotent, harder to manage upgrades
3. **Direct binary download** — Download from GitHub releases
   - Pro: pinned version
   - Con: manual upgrade path, no completions

**Decision**: Yum repo (Option 1) — matches existing Docker playbook pattern, provides automatic upgrades via dnf, and auto-installs completions. Unsigned repo is acceptable as DDEV plans to add signing, and the repo URL is their official domain.

### Decision 2: mkcert installation

**Options Considered**:

1. **Fedora dnf package** — `dnf install mkcert` (available in Fedora repos)
2. **Go install** — `go install filippo.io/mkcert@latest`
3. **Binary download** — from GitHub releases

**Decision**: Fedora dnf package (Option 1) — simplest, idempotent, already in Fedora repos. No need for Go toolchain dependency.

## Playbook Sketch

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  name: DDEV Local Development Environment
  become: false
  vars:
    root_dir: "{{ inventory_dir }}/../../"
  tasks:
    # 1. Dependency check
    - name: Check if Docker is Installed
      ansible.builtin.command: docker --version
      register: docker_check
      failed_when: false  # FAIL-FAST-OK: probe check
      changed_when: false

    - name: Fail if Docker is Not Available
      ansible.builtin.fail:
        msg: >
          Docker is required for DDEV.
          Run: ansible-playbook playbooks/imports/play-docker.yml
      when: docker_check.rc != 0

    # 2. mkcert (local HTTPS certificates)
    - name: Install mkcert
      become: true
      ansible.builtin.dnf:
        name: mkcert
        state: present

    - name: Install Local CA via mkcert
      ansible.builtin.command: mkcert -install
      become: true
      become_user: "{{ user_login }}"
      args:
        creates: "/home/{{ user_login }}/.local/share/mkcert/rootCA.pem"

    # 3. DDEV repo and install
    - name: Add DDEV Yum Repository
      become: true
      ansible.builtin.copy:
        dest: /etc/yum.repos.d/ddev.repo
        content: |
          [ddev]
          name=DDEV
          baseurl=https://pkg.ddev.com/yum/
          gpgcheck=0
          enabled=1
        mode: "0644"

    - name: Install DDEV
      become: true
      ansible.builtin.dnf:
        name: ddev
        state: present
        update_cache: true

    # 4. Verify
    - name: Verify DDEV Installation
      ansible.builtin.command: ddev version
      register: ddev_version
      changed_when: false

    - name: Show DDEV Version
      ansible.builtin.debug:
        var: ddev_version.stdout_lines
```

## Success Criteria

- [ ] Playbook installs DDEV and mkcert from packages (no manual steps)
- [ ] Docker dependency checked with fail-fast
- [ ] All tasks idempotent (safe to run multiple times)
- [ ] `./scripts/qa-all.bash` passes
- [ ] Playbook is executable (`chmod +x`, has shebang)
- [ ] User can run `ddev version` and `mkcert -version` after deployment

## Risks & Mitigations

| Risk                                 | Impact | Probability | Mitigation                                                              |
| ------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------- |
| DDEV yum repo unsigned               | Low    | High        | Accept for now; DDEV plans to add signing. Repo is official domain.     |
| mkcert CA path varies                | Low    | Low         | Use `creates:` with standard XDG path; verify on first host deployment. |
| Rootless Docker socket path for DDEV | Medium | Low         | DDEV auto-detects Docker provider; rootless is supported upstream.      |

## Notes & Updates

### 2026-04-17

- Plan created. Research completed: DDEV yum repo method confirmed, mkcert in Fedora repos confirmed, Docker playbook reviewed.
