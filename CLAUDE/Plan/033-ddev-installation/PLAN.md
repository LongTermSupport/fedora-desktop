# Plan 033: DDEV on Rootless Podman

**Status**: In Progress (Pivoting — v2)
**Created**: 2026-04-17
**Revised**: 2026-04-21
**Owner**: Claude
**Priority**: Medium

## Overview

Add an Ansible playbook to install [DDEV](https://ddev.com/) — a container-based local development environment for PHP/CMS projects — configured to use the **rootless Podman** stack this repo already provides, not Docker.

The v1 draft of this plan wired DDEV to rootless Docker (installed by `playbooks/imports/play-docker.yml`). PR feedback from @ballidev on [#18](https://github.com/LongTermSupport/fedora-desktop/pull/18) redirected the work:

> please pivot to podman rootless — ie standard podman set up with this repo
> https://ddev.com/blog/podman-and-docker-rootless/
> be VERY CAREFUL you don't bugger up the normal podman stuff

This revision delivers that pivot, while deliberately avoiding changes that would destabilise the existing rootless Podman state used by CCY (`claude-yolo`).

## Goals

- Install DDEV and configure it to run against the **existing** rootless Podman stack
- Add only the Podman-side configuration DDEV actually needs (sysctl port-start, `docker context`, fuse-overlayfs storage) — nothing else
- Keep the DDEV playbook self-contained: it may read state installed by `play-podman.yml` but must not silently mutate it
- Preserve coexistence with: Docker (still installed by default), CCY's Podman usage, LXC
- Fail-fast with actionable messages when prerequisites are missing

## Non-Goals

- Removing Docker from the repo (`play-docker.yml` stays in `playbook-main.yml`; users who want Docker still have it)
- Switching the repo default away from `container_engine: podman` — it's already podman
- Migrating CCY's container storage — it uses whatever `podman system info` says and must not be disturbed
- Supporting Podman < 5.0 (Fedora 42/43 ship 5.x — no back-compat needed)
- Supporting DDEV on macOS/Windows (Linux-only repo)
- Configuring specific DDEV projects (project-level `.ddev/config.yaml` is out of scope)

## Context & Background

### Repo already has most of what DDEV needs

| Requirement (from DDEV Podman blog) | Status in repo | Evidence |
| ------------------------------------ | -------------- | -------- |
| Podman ≥ 5.0 installed | ✅ Fedora 42/43 ship podman 5.x; `play-podman.yml` installs it | `playbooks/imports/play-podman.yml:9-14` |
| `podman.socket` enabled user-scope | ✅ Already enabled (D-Bus probe guarded) | `playbooks/imports/play-podman.yml:22-34` |
| podman-compose installed | ✅ pip `--user` install | `playbooks/imports/play-podman.yml:16-20` |
| subuid/subgid range for user | ⚠️ Set by `play-docker.yml` (100000:65536) — same range DDEV wants, but ownership is Docker's, not Podman's | `playbooks/imports/play-docker.yml:31-39` |
| `user.max_user_namespaces` ≥ 28633 | ✅ Fedora default is 28633 | kernel default |
| `net.ipv4.ip_unprivileged_port_start=0` | ❌ **Missing** — Fedora default is 1024; ddev-router needs 80/443 | — |
| `fuse-overlayfs` + `storage.conf` | ❌ **Missing** — perf optimisation; CCY currently uses default driver | — |
| `docker` CLI binary | ✅ Installed by `play-docker.yml` (docker-ce-cli) | `playbooks/imports/play-docker.yml:21-29` |
| `docker context` → podman socket | ❌ **Missing** — DDEV talks to podman via `unix://$XDG_RUNTIME_DIR/podman/podman.sock` through the docker CLI | — |
| systemd-oomd tuned for containers | ✅ Already done repo-wide | `playbooks/imports/play-systemd-user-tweaks.yml:95-106` |
| mkcert installed + CA trusted | ✅ In current v1 playbook (keep) | `playbooks/imports/optional/common/play-ddev.yml:22-33` |

**Three things are missing**: a sysctl for privileged ports, an optional fuse-overlayfs storage driver, and a `docker context` pointing at the podman socket.

### Why DDEV still needs the `docker` CLI

DDEV is written to shell out to `docker` / `docker compose` commands. It does not link against libpodman or have a podman-native code path. On Linux the supported integration (per the DDEV blog post) is:

1. Keep `docker` CLI installed (already true — `docker-ce-cli` ships with `play-docker.yml`)
2. Create a docker context called `podman-rootless` whose endpoint is `unix://$XDG_RUNTIME_DIR/podman/podman.sock`
3. Set that context as the active one for the user: `docker context use podman-rootless`
4. DDEV discovers the active context and talks to podman's socket — no DDEV-side config flags needed

This is why we **don't** need `podman-docker` (the alias shim). We also do **not** need `--no-bind-mounts`: that flag is for Docker Rootless only — Podman handles bind-mount UID mapping via `--userns=keep-id`, so bind mounts work.

### CCY coexistence — the hard constraint

CCY uses podman directly (`podman build`, `podman run`, `podman inspect` — see `files/var/local/claude-yolo/lib/docker-health.bash`). It does **not** use the docker CLI or docker context. Changing the `docker context` setting therefore has zero effect on CCY.

The one way to break CCY is `podman system reset` (wipes containers, images, volumes). DDEV's blog says reset is required when switching storage drivers to fuse-overlayfs. That means fuse-overlayfs configuration is **only safe** when there's no existing Podman state. We handle this by making fuse-overlayfs opt-in and guarded (see Decision 3).

### LXC coexistence

`play-lxc-install-config.yml` sets up an `lxcbr0` bridge on `10.0.x.x` in the trusted firewall zone. Podman rootless uses pasta/slirp4netns networking inside the user namespace — no bridge on `lxcbr0` and no firewalld zone overlap. The iptables kernel modules loaded by `play-lxc-install-config.yml` (`ip_tables`, `iptable_nat`, etc. — lines 142-154) are also what rootless Podman networking uses, so LXC's setup incidentally helps Podman.

### What v1 got right and we're keeping

- mkcert via `dnf install mkcert` + `mkcert -install` as user (idempotent via `creates:`)
- DDEV via yum repo at `pkg.ddev.com/yum/` + `dnf install ddev`
- Playbook is executable with `#!/usr/bin/env ansible-playbook` shebang
- Standard header: `hosts: desktop`, `become: false` by default with per-task `become: true` where needed

## Tasks

Legend: ✅ done (v1), 🔄 in progress, ⬜ not started, ♻️ replace/refactor v1 task.

### Phase 1: v1 scaffolding (already complete — retain)

- [x] ✅ **Task 1.1**: Initial playbook created with shebang, header, mkcert, DDEV yum repo, `ddev version` verification — `playbooks/imports/optional/common/play-ddev.yml`
- [x] ✅ **Task 1.2**: User documentation created — `docs/ddev.md`
- [x] ✅ **Task 1.3**: `./scripts/qa-all.bash` passed for v1

### Phase 2: Pivot playbook from Docker to Podman

- [ ] ♻️ **Task 2.1**: Replace Docker dependency check with Podman check
  - [ ] ⬜ Probe `podman --version` (fail-fast with pointer to `playbooks/imports/play-podman.yml` if missing)
  - [ ] ⬜ Probe `systemctl --user is-active podman.socket` (fail-fast if not running — but only when a user D-Bus session exists; use the same D-Bus probe pattern as `play-podman.yml:22-26`)
  - [ ] ⬜ Probe `docker --version` (fail-fast with pointer to `play-docker.yml` — needed for the `docker context` shim)
  - [ ] ⬜ Remove the v1 `docker --version` check and its fail task
- [ ] ⬜ **Task 2.2**: Ensure Podman version ≥ 5.0 — parse `podman --version` and fail if < 5.0 (Fedora 42/43 always pass, but the check documents the requirement)
- [ ] ⬜ **Task 2.3**: Apply sysctl `net.ipv4.ip_unprivileged_port_start=0`
  - [ ] ⬜ Write to `/etc/sysctl.d/60-rootless-ports.conf` via `blockinfile` with ANSIBLE MANAGED marker
  - [ ] ⬜ Reload with `sysctl --system` via handler
  - [ ] ⬜ Document: required for ddev-router to bind 80/443 as rootless user
- [ ] ⬜ **Task 2.4**: Create `docker context` for podman socket
  - [ ] ⬜ Compute `XDG_RUNTIME_DIR` for `{{ user_login }}` (`/run/user/{{ user_uid }}`) — look up `user_uid` via `getent passwd`
  - [ ] ⬜ Run `docker context create podman-rootless --description "Podman (rootless)" --docker host="unix:///run/user/{{ user_uid }}/podman/podman.sock"` via `ansible.builtin.command` with `creates:` pointing at `~/.docker/contexts/meta/…` OR register-check-then-create (probe pattern)
  - [ ] ⬜ Run `docker context use podman-rootless` (idempotent — rerunning with same value is a no-op)
  - [ ] ⬜ Verify: `docker context show` → `podman-rootless`
- [ ] ⬜ **Task 2.5**: Keep mkcert steps from v1 unchanged — they're engine-agnostic
- [ ] ⬜ **Task 2.6**: Keep DDEV yum repo + dnf install from v1 unchanged
- [ ] ⬜ **Task 2.7**: Post-install smoke test
  - [ ] ⬜ Run `ddev version` and show stdout
  - [ ] ⬜ Run `docker context show` and assert stdout == `podman-rootless`
  - [ ] ⬜ Run `docker info --format '{{.Host.RemoteSocket.Path | default .Name}}'` — expect output referencing podman socket (document the expected substring, not an exact match)

### Phase 3: Optional fuse-overlayfs performance config (GUARDED)

**Risk**: switching storage driver on a host with existing Podman state needs `podman system reset`, which destroys CCY's container image. Must be guarded.

- [ ] ⬜ **Task 3.1**: Probe for existing Podman state
  - [ ] ⬜ Check if `/home/{{ user_login }}/.local/share/containers/storage/` exists and is non-empty
  - [ ] ⬜ If non-empty: **skip** fuse-overlayfs config, print a debug message telling the user that it's a manual opt-in (see Task 3.3)
- [ ] ⬜ **Task 3.2**: If no existing state: install `fuse-overlayfs` + write `~/.config/containers/storage.conf`
  - [ ] ⬜ `dnf install fuse-overlayfs`
  - [ ] ⬜ `copy:` `~/.config/containers/storage.conf` with overlay + `mount_program = /usr/bin/fuse-overlayfs`
- [ ] ⬜ **Task 3.3**: Document the manual opt-in path in `docs/ddev.md` — how to rebuild storage with fuse-overlayfs after the fact, including the explicit warning that it destroys existing containers/images (CCY included) and requires re-running the CCY image build

### Phase 4: Update docs/ddev.md

- [ ] ⬜ **Task 4.1**: Replace "Prerequisite: Docker" with "Prerequisite: rootless Podman (installed by `play-podman.yml`) + `docker` CLI (installed by `play-docker.yml`)"
- [ ] ⬜ **Task 4.2**: Update Troubleshooting section
  - [ ] ⬜ Replace `systemctl --user status docker` with `systemctl --user status podman.socket`
  - [ ] ⬜ Add: how to verify `docker context show` reports `podman-rootless`
  - [ ] ⬜ Add: how to roll back (`docker context use default`) if something goes wrong
  - [ ] ⬜ Add: `podman ps` to see DDEV containers (not `docker ps` — both work, but `podman ps` is authoritative)
- [ ] ⬜ **Task 4.3**: Add a "How DDEV finds Podman" sub-section explaining the `docker context` → podman.sock indirection (one short paragraph)
- [ ] ⬜ **Task 4.4**: Add a coexistence note: CCY and DDEV share the same Podman daemon-per-user; running both simultaneously is fine; `podman ps -a` shows containers from both

### Phase 5: QA and commit

- [ ] ⬜ **Task 5.1**: `./scripts/qa-all.bash` must pass
- [ ] ⬜ **Task 5.2**: `chmod +x` preserved on the playbook; `head -n1` is still the shebang
- [ ] ⬜ **Task 5.3**: Ansible style review against `CLAUDE/AnsibleStyle.md`
  - [ ] ⬜ `blockinfile` for sysctl, with `# {mark} ANSIBLE MANAGED:` marker
  - [ ] ⬜ Probe-then-check pattern (`failed_when: false # FAIL-FAST-OK:`) only on the read-only probes
  - [ ] ⬜ No `ignore_errors`; no silent failures
- [ ] ⬜ **Task 5.4**: Commit plan + playbook + docs together (plan-code locked-step per CLAUDE.md Plan Commit Rule)
- [ ] ⬜ **Task 5.5**: Update PR #18 description with a "What changed in v2" summary and push

### Phase 6: User deployment verification (on host, not in CCY)

- [ ] ⬜ **Task 6.1**: User runs `ansible-playbook playbooks/imports/optional/common/play-ddev.yml` on their host
- [ ] ⬜ **Task 6.2**: `docker context show` → `podman-rootless`
- [ ] ⬜ **Task 6.3**: `ddev version` succeeds
- [ ] ⬜ **Task 6.4**: In a PHP project, `ddev start` succeeds; `podman ps` shows `ddev-*` containers
- [ ] ⬜ **Task 6.5**: `ddev-router` binds 80/443 (check `ss -tlnp`)
- [ ] ⬜ **Task 6.6**: CCY regression check — `claude-yolo` still starts; its container image is unchanged (`podman image inspect claude-yolo:latest` still works)
- [ ] ⬜ **Task 6.7**: Idempotency — second run of the playbook produces zero changes

## Technical Decisions

### Decision 1 (v2): `docker context` vs `DOCKER_HOST` env var vs `podman-docker`

**Context**: DDEV shells out to the `docker` CLI. We need to get its calls to hit the podman socket.

**Options considered**:

1. **`docker context create podman-rootless`** + `docker context use podman-rootless`
   - Pro: officially documented by DDEV upstream ([podman-and-docker-rootless](https://ddev.com/blog/podman-and-docker-rootless/))
   - Pro: per-user, persistent, survives reboot
   - Pro: easy to roll back — `docker context use default`
   - Con: needs docker CLI (we have it via `play-docker.yml`)
2. **`DOCKER_HOST=unix://$XDG_RUNTIME_DIR/podman/podman.sock` in user's `.bashrc`**
   - Pro: no docker-context state
   - Con: affects every `docker` invocation globally — can break user's expectation of docker-ce usage
   - Con: env var injection into interactive shells is fragile (GNOME desktop launches don't always source `.bashrc`)
3. **`podman-docker` package** (installs `/usr/bin/docker` → `/usr/bin/podman` symlink)
   - Pro: zero-config
   - Con: **conflicts with `docker-ce-cli` installed by `play-docker.yml`** — dnf would refuse to install alongside
   - Con: all docker usage on the system goes to podman — too aggressive

**Decision**: Option 1 (docker context). It's scoped to DDEV's use case (via active context), officially recommended, and trivially reversible. Option 3 is off the table because of the CLI conflict.

**Date**: 2026-04-21

### Decision 2 (v2): Require both `play-docker.yml` and `play-podman.yml`, don't remove Docker

**Context**: `playbook-main.yml` currently runs both `play-docker.yml` (line 18) and `play-podman.yml` (line 19) by default. We could remove Docker to go podman-only.

**Decision**: Keep the current `playbook-main.yml` intact. The DDEV playbook will require `docker` CLI (from `docker-ce-cli`) and active rootless Podman. Removing Docker is a separate, bigger decision outside this plan's scope, and the user's guidance was to pivot DDEV, not the whole repo.

**Side effect**: both docker-ce rootless daemon and podman user daemon are installed. They don't conflict because DDEV's `docker` calls go through the `podman-rootless` context to podman's socket, not to docker-ce's daemon. Users who want pure podman can stop docker-ce with `systemctl --user stop docker`, but that's their call.

**Date**: 2026-04-21

### Decision 3 (v2): fuse-overlayfs is optional and guarded

**Context**: DDEV's blog recommends `fuse-overlayfs` for performance. Applying it requires writing `~/.config/containers/storage.conf`. If existing Podman containers/images already exist in the default storage driver, switching requires `podman system reset`, which wipes **everything** — including CCY's container image (`claude-yolo:latest`) that takes minutes to rebuild.

**Options considered**:

1. Always apply fuse-overlayfs config and run `podman system reset`
   - ❌ Destroys CCY state — violates "don't bugger up the normal podman stuff"
2. Never apply fuse-overlayfs, accept the performance hit
   - Safe, but leaves ~20-30% DDEV performance on the table per the blog
3. **Apply only if `~/.local/share/containers/storage/` is empty; otherwise skip and document manual opt-in**
   - Safe on fresh installs
   - Existing users keep their CCY state
   - Manual opt-in path documented for users who want perf and are willing to rebuild CCY

**Decision**: Option 3. The probe is simple (`stat` on the directory), the failure mode is clear (skip + debug message), and the manual opt-in is a one-paragraph doc section.

**Date**: 2026-04-21

### Decision 4 (v2): subuid/subgid — don't duplicate what `play-docker.yml` already does

**Context**: Both Docker rootless and Podman rootless need `/etc/subuid` and `/etc/subgid` entries for the user. `play-docker.yml:31-39` already writes `{{ user_login }}:100000:65536` to both. The DDEV blog's Podman setup also writes `100000:165535` (same range, different notation).

**Decision**: Do not duplicate the subuid/subgid task in `play-ddev.yml`. Instead, probe the mappings (`getent subuid {{ user_login }}`) in the prereq phase; fail-fast with a pointer to `play-docker.yml` or `play-podman.yml` if missing. This avoids the risk of conflicting blockinfile markers and keeps responsibility where it lives.

**Follow-up (not in this plan)**: consider moving the subuid/subgid task out of `play-docker.yml` into a new `play-container-subids.yml` that both `play-docker.yml` and `play-podman.yml` import. That's a repo-wide refactor — separate plan.

**Date**: 2026-04-21

### Decision 5 (v2): No DDEV `--no-bind-mounts`, no Mutagen

**Context**: DDEV's Docker Rootless path needs `ddev config global --no-bind-mounts=true` because Docker Rootless maps bind mounts to root. Podman Rootless uses `--userns=keep-id` which maps the host user to a matching UID inside the container — bind mounts work correctly.

**Decision**: Don't set `--no-bind-mounts`. Don't enable Mutagen. DDEV defaults are correct for Podman rootless on Linux.

**Date**: 2026-04-21

### Decision 6 (v2): DDEV yum repo stays unsigned for now

Unchanged from v1. DDEV repo is `gpgcheck=0`; upstream plans to sign. Accept for this plan.

**Date**: 2026-04-17 (unchanged)

## Playbook Sketch (v2)

Annotated: **keep** = unchanged from v1, **new** = added, **replace** = refactored.

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  name: DDEV on Rootless Podman
  become: false
  vars:
    root_dir: "{{ inventory_dir }}/../../"
  tasks:

    # ── REPLACE: Prereq checks (Podman, podman.socket, docker CLI, subuid) ──
    - name: "Prereq — podman installed"
      ansible.builtin.command: podman --version
      register: podman_check
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe
    - name: "Prereq — fail if podman missing"
      ansible.builtin.fail:
        msg: "Podman required. Run: ansible-playbook playbooks/imports/play-podman.yml"
      when: podman_check.rc != 0

    - name: "Prereq — parse podman version"
      ansible.builtin.set_fact:
        podman_version: "{{ podman_check.stdout | regex_search('([0-9]+\\.[0-9]+\\.[0-9]+)', '\\1') | first }}"
    - name: "Prereq — fail if podman < 5.0"
      ansible.builtin.fail:
        msg: "Podman >= 5.0 required (DDEV requirement). Found: {{ podman_version }}"
      when: podman_version is version('5.0.0', '<')

    - name: "Prereq — docker CLI (needed for docker context)"
      ansible.builtin.command: docker --version
      register: docker_cli_check
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe
    - name: "Prereq — fail if docker CLI missing"
      ansible.builtin.fail:
        msg: "docker CLI required (for docker context pointing at podman.sock). Run: ansible-playbook playbooks/imports/play-docker.yml"
      when: docker_cli_check.rc != 0

    - name: "Prereq — D-Bus user session"
      ansible.builtin.command: systemctl --user status
      register: dbus_check
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe
    - name: "Prereq — podman.socket active"
      ansible.builtin.command: systemctl --user is-active podman.socket
      register: podman_socket_check
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe
      when: dbus_check.rc == 0
    - name: "Prereq — fail if podman.socket not active"
      ansible.builtin.fail:
        msg: "podman.socket not running. Run: ansible-playbook playbooks/imports/play-podman.yml"
      when: dbus_check.rc == 0 and podman_socket_check.stdout != 'active'

    - name: "Prereq — subuid mapping exists for {{ user_login }}"
      ansible.builtin.command: "getent subuid {{ user_login }}"
      register: subuid_check
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe
    - name: "Prereq — fail if subuid missing"
      ansible.builtin.fail:
        msg: "subuid mapping missing for {{ user_login }}. Run: ansible-playbook playbooks/imports/play-docker.yml (which sets it)"
      when: subuid_check.rc != 0

    # ── NEW: sysctl for privileged ports ──
    - name: "sysctl — allow unprivileged port binding from 0"
      become: true
      ansible.builtin.blockinfile:
        path: /etc/sysctl.d/60-rootless-ports.conf
        create: true
        marker: "# {mark} ANSIBLE MANAGED: DDEV ddev-router ports 80/443 (rootless)"
        mode: "0644"
        block: |
          net.ipv4.ip_unprivileged_port_start=0
      notify: sysctl reload

    # ── KEEP: mkcert ──
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

    # ── NEW: docker context → podman socket ──
    - name: "Look up uid for {{ user_login }}"
      ansible.builtin.getent:
        database: passwd
        key: "{{ user_login }}"
    - name: "Set user_uid fact"
      ansible.builtin.set_fact:
        user_uid: "{{ getent_passwd[user_login][1] }}"

    - name: "docker context — check if podman-rootless exists"
      ansible.builtin.command: docker context inspect podman-rootless
      become: true
      become_user: "{{ user_login }}"
      register: context_check
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe

    - name: "docker context — create podman-rootless"
      ansible.builtin.command: >
        docker context create podman-rootless
        --description "Podman (rootless) for DDEV"
        --docker host=unix:///run/user/{{ user_uid }}/podman/podman.sock
      become: true
      become_user: "{{ user_login }}"
      when: context_check.rc != 0

    - name: "docker context — use podman-rootless"
      ansible.builtin.command: docker context use podman-rootless
      become: true
      become_user: "{{ user_login }}"
      register: context_use
      changed_when: "'podman-rootless' not in context_use.stdout"

    # ── KEEP: DDEV repo + install ──
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

    # ── NEW: optional fuse-overlayfs (guarded) ──
    - name: "fuse-overlayfs — detect existing podman storage"
      ansible.builtin.stat:
        path: "/home/{{ user_login }}/.local/share/containers/storage"
      register: podman_storage_stat

    - name: "fuse-overlayfs — skip if existing podman state present"
      ansible.builtin.debug:
        msg: >
          Existing Podman storage detected at ~/.local/share/containers/storage —
          skipping fuse-overlayfs config to preserve CCY state.
          See docs/ddev.md for manual opt-in (requires podman system reset).
      when: podman_storage_stat.stat.exists

    - name: "fuse-overlayfs — install package"
      become: true
      ansible.builtin.dnf:
        name: fuse-overlayfs
        state: present
      when: not podman_storage_stat.stat.exists

    - name: "fuse-overlayfs — write storage.conf"
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.copy:
        dest: "/home/{{ user_login }}/.config/containers/storage.conf"
        mode: "0644"
        content: |
          [storage]
          driver = "overlay"
          [storage.options.overlay]
          mount_program = "/usr/bin/fuse-overlayfs"
      when: not podman_storage_stat.stat.exists

    # ── Verification ──
    - name: "Verify — ddev version"
      ansible.builtin.command: ddev version
      register: ddev_version
      changed_when: false

    - name: "Show DDEV version"
      ansible.builtin.debug:
        var: ddev_version.stdout_lines

    - name: "Verify — active docker context is podman-rootless"
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command: docker context show
      register: ctx_show
      changed_when: false
      failed_when: ctx_show.stdout != 'podman-rootless'

  handlers:
    - name: sysctl reload
      become: true
      ansible.builtin.command: sysctl --system
```

## Success Criteria

- [ ] Playbook installs DDEV against rootless Podman with no manual post-steps
- [ ] `docker context show` reports `podman-rootless` after the playbook runs
- [ ] `ddev start` on a PHP project brings up containers visible in `podman ps`
- [ ] CCY (`claude-yolo`) still launches and its container image is untouched
- [ ] `./scripts/qa-all.bash` passes
- [ ] Playbook is idempotent — second run reports zero changes
- [ ] Fail-fast errors point users at the correct prereq playbook (podman, docker)
- [ ] `docs/ddev.md` reflects the Podman-first setup

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
| ---- | ------ | ----------- | ---------- |
| **`podman system reset` destroys CCY image** | 🔴 High | Low (guarded) | fuse-overlayfs is opt-in only on empty storage; existing-state probe + skip; documented manual opt-in with warning |
| docker-ce rootless daemon conflicts with DDEV using podman socket | Medium | Low | `docker context` isolation — DDEV's docker calls hit podman.sock, docker-ce stays available on `default` context |
| `net.ipv4.ip_unprivileged_port_start=0` is a security-sensitive kernel setting | Low | Med | Standard for dev desktops (matches DDEV blog guidance); scoped to sysctl.d file with ANSIBLE MANAGED marker so it's auditable/reversible |
| Podman < 5.0 on host | Low | Very Low | Fedora 42/43 ship 5.x; explicit version probe in playbook fails fast with the requirement |
| `docker context create` not idempotent on re-run | Low | Med | Probe-then-create pattern using `docker context inspect` rc as the condition |
| User's XDG_RUNTIME_DIR path differs | Low | Low | Computed from `getent passwd` uid, not `$XDG_RUNTIME_DIR` — survives sudo/become |
| DDEV yum repo unsigned | Low | High | Accepted from v1; upstream plans signing |
| Plan 033 merging out of order w.r.t. other branches | Low | Low | v2 is a pure refactor of the same three files the PR already touches — no new files, no reordering of `playbook-main.yml` |

## Notes & Updates

### 2026-04-17 (v1)

- Original plan created; Docker-based DDEV playbook committed to `ddev` branch; PR #18 opened.

### 2026-04-21 (v2 — this revision)

- @ballidev feedback on PR #18 requested pivot to rootless Podman.
- Researched: `play-podman.yml`, `play-docker.yml`, `play-lxc-install-config.yml`, `vars/container-defaults.yml`, CCY's `files/var/local/claude-yolo/` (Dockerfile, wrapper, docker-health.bash).
- Fetched DDEV docker-installation.md from upstream (redirects to blog) and the podman-and-docker-rootless blog post for the exact Podman setup steps.
- Discovered the repo already has most Podman prerequisites; only three concrete additions needed (sysctl, docker context, optional fuse-overlayfs).
- Identified the CCY storage-driver risk and designed the Phase 3 guard.
- Identified no-op on LXC coexistence (different network stack) and on CCY (uses podman CLI directly, unaffected by docker context).
- Rewrote plan: phases, decisions 1–6, risks table, playbook sketch.
