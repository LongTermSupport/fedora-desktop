# Plan 033: DDEV on Rootful Docker

**Status**: In Progress (v3 — approved approach)
**Created**: 2026-04-17
**Revised**: 2026-04-21 (v3)
**Owner**: Claude
**Priority**: Medium

## Overview

Add an Ansible playbook to install [DDEV](https://ddev.com/) — a container-based local development environment for PHP/CMS projects — configured against **rootful Docker**. This is a deliberate, scoped compatibility exception to the repo's Podman-first default.

### Evolution of this plan

- **v1** (branch `ddev`, PR #18 commits `d031de8..f1871a6`): DDEV on rootless Docker (what `play-docker.yml` currently deploys). Committed and pushed, then paused for review.
- **v2** (commit `126aac5`): DDEV on rootless Podman via `docker context` indirection. Rejected in favour of v3 because it made DDEV run on DDEV's own "experimental" engine, added sysctl + fuse-overlayfs + context plumbing, and carried the fuse-overlayfs→`podman system reset` risk to CCY.
- **v3** (this revision): DDEV on rootful Docker. Aligns with DDEV upstream's "Docker for Linux: recommended, best performance and stability" guidance. Simpler than both prior approaches. Zero risk to the Podman stack CCY runs on. Formalises the repo's three-engine role split.

Decision-gate analysis: [container-engine-strategy.md](container-engine-strategy.md). User approved Approach C on 2026-04-21.

## Goals

- Install DDEV against **rootful Docker** — DDEV's upstream happy path
- Convert `playbooks/imports/play-docker.yml` from rootless to rootful (group-based, system-wide `docker.service` with socket activation)
- Formalise the three-engine role split in top-level docs (`CLAUDE/ContainerEngines.md`) and `CLAUDE.md` Critical Rules
- Leave Podman and LXC setup completely untouched — CCY keeps working
- Fail-fast with actionable messages when prerequisites are missing

## Non-Goals

- Removing Docker entirely — keep it installed, just flip the mode
- Removing rootless Docker artefacts from already-deployed hosts (document cleanup in release notes; out of scope for this plan)
- Supporting rootless Docker as an alternative mode — we commit to rootful
- Per-project `.ddev/config.yaml` tuning — user responsibility
- macOS/Windows DDEV support — Linux-only repo

## Context & Background

### The architectural message

Top-level rule, now in `CLAUDE/ContainerEngines.md` and echoed in `CLAUDE.md` Critical Rules:

> **Use Podman wherever possible — it is the better system. Reach for Docker only when a tool genuinely needs it for compatibility or legacy reasons, and understand that Docker is significantly less secure than Podman.**

DDEV is the first concrete "Docker for compatibility" case. It's exactly what that rule is for.

### Why rootful, not rootless

Per the DDEV blog post and upstream `docker-installation.md` (fetched from `github.com/ddev/ddev/main`):

> "Docker for Linux: Recommended, free, open-source, best performance and stability."

Rootless Docker would require `--no-bind-mounts`, Mutagen, sysctl tweaks, and a loopback override — friction for every user, for every project. Rootful Docker needs: install, enable, add to `docker` group. Done.

### What's already in the repo we're using

| Capability | Source | Changes |
| ---------- | ------ | ------- |
| `docker-ce` + `docker-ce-cli` + `containerd.io` + compose plugin | `play-docker.yml:21-29` | unchanged |
| `/etc/subuid` + `/etc/subgid` for user | `play-docker.yml:31-39` | retained — harmless in rootful mode, other tools may use |
| `docker.service` / `docker.socket` system units | ships with `docker-ce` | **newly enabled** (replaces rootless user-service) |
| `docker` group | ships with `docker-ce` | **user newly added** to this group |
| Rootless user-service via `dockerd-rootless-setuptool.sh` | `play-docker.yml:49-65` | **removed** from fresh deploys; existing deploys untouched |
| Podman + `podman.socket` | `play-podman.yml` | **completely untouched** |
| LXC | `play-lxc-install-config.yml` | **completely untouched** |
| `container_engine: podman` default | `vars/container-defaults.yml:10` | **unchanged** — Podman remains the repo default |
| CCY using Podman directly | `files/var/local/claude-yolo/` | **unchanged** — CCY never sees Docker |

### Security note

User in `docker` group can escape containers trivially via `docker run -v /:/host …`. This is accepted for single-user developer workstations where the user already has passwordless sudo (`CLAUDE/SecurityRules.md`). Documented prominently in `CLAUDE/ContainerEngines.md`. Not acceptable on shared or production systems — this repo is not for those.

## Tasks

Legend: ✅ done, 🔄 in progress, ⬜ not started, ♻️ replace/refactor v1 task, 🗑️ delete v2 work (superseded).

### Phase 1: Scaffolding from v1 (retain)

- [x] ✅ **Task 1.1**: Playbook scaffolded with shebang, header, mkcert, DDEV yum repo, version verification
- [x] ✅ **Task 1.2**: `docs/ddev.md` user documentation created
- [x] ✅ **Task 1.3**: v1 `./scripts/qa-all.bash` pass

### Phase 2: Strategy & top-level docs (Approach C groundwork)

- [x] ✅ **Task 2.1**: Write `CLAUDE/Plan/033-ddev-installation/container-engine-strategy.md` decision doc
- [x] ✅ **Task 2.2**: Write `CLAUDE/ContainerEngines.md` — top-level role-split doc with Podman-first rule
- [x] ✅ **Task 2.3**: Add "Container Engines: Podman First" section to CLAUDE.md Critical Rules
- [x] ✅ **Task 2.4**: Add `@CLAUDE/ContainerEngines.md` row to CLAUDE.md Topic Files Index

### Phase 3: Convert `play-docker.yml` rootless → rootful

- [ ] ⬜ **Task 3.1**: Replace rootless setup block (lines 41-65 of current `play-docker.yml`) with rootful:
  - [ ] ⬜ Add `{{ user_login }}` to `docker` group (`user` module, `append: true`)
  - [ ] ⬜ Enable and start system `docker.service`
  - [ ] ⬜ Enable and start system `docker.socket` (socket activation — reduces idle daemon footprint)
- [ ] ⬜ **Task 3.2**: Remove the D-Bus probe (no longer needed without rootless user-service)
- [ ] ⬜ **Task 3.3**: Remove `dockerd-rootless-setuptool.sh install` task
- [ ] ⬜ **Task 3.4**: Remove user-scope `systemd` enable of `docker`
- [ ] ⬜ **Task 3.5**: Retain subuid/subgid block (harmless; other tools may depend)
- [ ] ⬜ **Task 3.6**: Add verification task: `docker info` (confirms user can talk to daemon; requires `newgrp docker` on first run — document)
- [ ] ⬜ **Task 3.7**: Update any references in `playbooks/imports/optional/experimental/play-docker-overlay2-migration.yml` that assume user-scope Docker (lines 114, 120, 256-297) — either mark experimental playbook as incompatible with rootful (add preflight assertion) OR migrate it

### Phase 4: Simplify `play-ddev.yml` (🗑️ drop v2 tasks)

- [ ] 🗑️ **v2 Task 2.3**: sysctl `ip_unprivileged_port_start=0` — **not needed** (rootful Docker binds 80/443 natively)
- [ ] 🗑️ **v2 Task 2.4**: `docker context create podman-rootless` — **not needed** (default context talks to the rootful socket at `/var/run/docker.sock`, which DDEV auto-discovers)
- [ ] 🗑️ **v2 Phase 3**: fuse-overlayfs install + guard + `storage.conf` — **not needed** (rootful Docker uses `/var/lib/docker`, not Podman storage)
- [ ] ⬜ **Task 4.1**: Replace v1 Docker-check with a simple daemon reachability probe:
  - [ ] ⬜ `docker info` — fail if non-zero with message "docker daemon not running or user not in docker group; run `play-docker.yml`"
  - [ ] ⬜ No need to probe `podman` — DDEV does not use it
- [ ] ⬜ **Task 4.2**: Retain mkcert install + `mkcert -install` as user (unchanged from v1)
- [ ] ⬜ **Task 4.3**: Retain DDEV yum repo + `dnf install ddev` (unchanged from v1)
- [ ] ⬜ **Task 4.4**: Retain `ddev version` smoke test (unchanged from v1)

### Phase 5: Update `docs/ddev.md`

- [ ] ⬜ **Task 5.1**: Replace "Prerequisite: Docker" with "Prerequisite: rootful Docker installed via `play-docker.yml` (user must be in `docker` group)"
- [ ] ⬜ **Task 5.2**: Replace "systemctl --user status docker" troubleshooting with system-scope equivalent (`sudo systemctl status docker --no-pager -l`)
- [ ] ⬜ **Task 5.3**: Add one-paragraph "Why Docker, not Podman?" cross-referencing `CLAUDE/ContainerEngines.md` (user-facing version of the rule)
- [ ] ⬜ **Task 5.4**: Document the one-time "log out and back in (or `newgrp docker`)" step after first deploy

### Phase 6: QA, commit, PR update

- [ ] ⬜ **Task 6.1**: `./scripts/qa-all.bash` passes on the modified Ansible playbooks
- [ ] ⬜ **Task 6.2**: `chmod +x` preserved on `play-ddev.yml`; shebang intact
- [ ] ⬜ **Task 6.3**: Style review against `CLAUDE/AnsibleStyle.md`
- [ ] ⬜ **Task 6.4**: Commit Phase 3 + 4 + 5 code changes **together** with PLAN.md v3 status updates (per CLAUDE.md Plan Commit Rule)
- [ ] ⬜ **Task 6.5**: Push and update PR #18 description to reflect Approach C (replace the Docker rootless narrative)

### Phase 7: Host deployment verification

- [ ] ⬜ **Task 7.1**: On fresh Fedora 43 host: run `playbook-main.yml` (picks up modified `play-docker.yml`)
- [ ] ⬜ **Task 7.2**: Log out + back in (picks up new `docker` group membership)
- [ ] ⬜ **Task 7.3**: `sudo systemctl status docker --no-pager -l` → active; `docker info` → reports rootful daemon
- [ ] ⬜ **Task 7.4**: Run `ansible-playbook playbooks/imports/optional/common/play-ddev.yml`
- [ ] ⬜ **Task 7.5**: `ddev version` succeeds; `mkcert -version` succeeds
- [ ] ⬜ **Task 7.6**: In a test project: `ddev start` → all containers `Running`; `docker ps` shows `ddev-*`
- [ ] ⬜ **Task 7.7**: Regression — `claude-yolo` still starts; `podman image inspect claude-yolo:latest` unchanged
- [ ] ⬜ **Task 7.8**: Regression — LXC still works (`lxc-ls`, `systemctl is-active lxc`)
- [ ] ⬜ **Task 7.9**: Idempotency — second run of both playbooks reports zero changes
- [ ] ⬜ **Task 7.10**: On a host previously deployed with rootless Docker: verify system `docker.service` takes over cleanly, user is in `docker` group, `docker info` works. Document what happens to orphaned user service (expected: still exists but unused).

### Phase 8: Mark plan complete

- [ ] ⬜ **Task 8.1**: User confirms deployment success on at least one host
- [ ] ⬜ **Task 8.2**: Mark plan Status: **Complete**, add completion date to "Notes & Updates"
- [ ] ⬜ **Task 8.3**: Merge PR #18

## Technical Decisions

### Decision 1 (v3): Rootful Docker chosen over rootless Docker (A) and rootless Podman (B)

**Context**: Three viable approaches for DDEV:

- **A — rootless Docker**: v1 approach. Needs `--no-bind-mounts`, Mutagen, sysctl, loopback override. DDEV upstream rates as "rougher than rootful."
- **B — rootless Podman**: v2 approach. Requires `docker context` indirection, `ip_unprivileged_port_start=0` sysctl, and fuse-overlayfs config (which risks destroying CCY's image via `podman system reset`).
- **C — rootful Docker**: this approach. DDEV upstream's explicit recommendation. Install, enable, add to `docker` group. No sysctl changes. No storage-driver gymnastics. Zero risk to CCY.

**Decision**: Approach C.

**Rationale**:

1. Aligns with DDEV upstream's "Docker for Linux: recommended, best performance and stability" guidance
2. Strictly less invasive than B — fewer files change, fewer sysctl tweaks, no CCY risk
3. Formalises the repo's three-engine role split in a way that pays dividends beyond DDEV
4. `docker` group = root-equivalent is an accepted trade-off on single-user dev workstations (already true via passwordless sudo)

**Date**: 2026-04-21 (user approved)

### Decision 2 (v3): Use `docker.socket` activation, not always-on `docker.service` alone

**Options**:

1. Enable `docker.service` only — daemon always running
2. Enable `docker.socket` only — daemon starts on first socket access
3. Enable **both** — daemon starts at boot AND socket activation triggers restart if daemon dies

**Decision**: Option 3 (both). Matches Fedora's default for docker-ce. Socket activation gives a low-cost recovery path if the daemon crashes. `docker.service` enabled guarantees DDEV works immediately after boot without requiring a client call to warm the socket.

**Date**: 2026-04-21

### Decision 3 (v3): No active cleanup of rootless Docker artefacts on already-deployed hosts

**Context**: Users who ran the previous `play-docker.yml` have:

- `~/.config/systemd/user/docker.service` (from `dockerd-rootless-setuptool.sh install`)
- `~/.local/share/docker` storage

**Options**:

1. Leave orphaned — document in release notes; users can run `dockerd-rootless-setuptool.sh uninstall` if they care
2. Add a cleanup task in `play-docker.yml` that runs `uninstall` on re-deploy
3. Spin off a separate cleanup plan

**Decision**: Option 1. Orphaned state is harmless — different socket, no contention with the system daemon. Users who want to free the disk space can run the uninstall tool manually. Adding a cleanup task risks interfering with a user who deliberately kept rootless Docker around for something else.

**Follow-up**: brief release note in PR #18 description.

**Date**: 2026-04-21

### Decision 4 (v3): Keep subuid/subgid block in `play-docker.yml`

**Context**: `play-docker.yml:31-39` writes `{{ user_login }}:100000:65536` to `/etc/subuid` and `/etc/subgid`. Needed for rootless Docker (obsolete in Approach C) but also useful for other user-namespace-using tools.

**Decision**: Keep the block. Benign; removing it is a separate cleanup that isn't this plan's scope. Tools that may want it: rootless Podman (already configured), distrobox (installed), future user-namespace experiments.

**Date**: 2026-04-21

### Decision 5 (v3): `play-docker-overlay2-migration.yml` — mark experimental rootless-only OR migrate

**Context**: `playbooks/imports/optional/experimental/play-docker-overlay2-migration.yml` uses user-scope Docker commands (`systemctl --user …`). These stop working after Approach C flips the user to rootful.

**Decision**: Add a preflight assertion at the top of that experimental playbook: "this targets rootless Docker; not compatible with the rootful setup installed by the current `play-docker.yml`. If you need this, restore rootless Docker manually." Do **not** rewrite it — it's experimental and unused by the main playbook flow.

**Date**: 2026-04-21

### Decision 6 (v3): Decisions 1, 2, 6 from PLAN.md v2 are superseded

- v2 Decision 1 (`docker context` vs `DOCKER_HOST` vs `podman-docker`): N/A — we don't use `docker context` in v3
- v2 Decision 2 (keep both Docker and Podman installed, don't remove Docker): still holds, but now Docker is the rootful kind
- v2 Decision 3 (fuse-overlayfs guarded opt-in): N/A — no fuse-overlayfs in v3
- v2 Decision 4 (don't duplicate subuid/subgid): still holds, and Decision 4 above complements it
- v2 Decision 5 (no `--no-bind-mounts`): still holds — rootful Docker needs no such flag
- v2 Decision 6 (DDEV repo unsigned): unchanged

## Playbook Sketches

### `playbooks/imports/play-docker.yml` (rewritten rootless → rootful)

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  become: false
  vars:
    root_dir: "{{ inventory_dir }}/../../"
  tasks:
    - name: Install DNF Plugins
      become: true
      ansible.builtin.dnf:
        name: dnf-plugins-core

    - name: Add Docker Repo
      become: true
      ansible.builtin.shell: |
        dnf config-manager addrepo \
          --from-repofile=https://download.docker.com/linux/fedora/docker-ce.repo
      args:
        creates: /etc/yum.repos.d/docker-ce.repo

    - name: Install Docker
      become: true
      ansible.builtin.dnf:
        name:
          - docker-ce
          - docker-ce-cli
          - containerd.io
          - docker-buildx-plugin
          - docker-compose-plugin

    - name: Setup ID Maps (kept for rootless tools like distrobox)
      become: true
      ansible.builtin.blockinfile:
        path: "{{ item }}"
        marker: "# {mark} ANSIBLE MANAGED: subuid/subgid for rootless containers"
        block: |
          {{ user_login }}:100000:65536
      loop:
        - /etc/subuid
        - /etc/subgid

    - name: Add {{ user_login }} to docker group
      become: true
      ansible.builtin.user:
        name: "{{ user_login }}"
        groups: docker
        append: true

    - name: Enable and start docker.socket (system-wide, socket activation)
      become: true
      ansible.builtin.systemd:
        name: docker.socket
        state: started
        enabled: true

    - name: Enable and start docker.service (system-wide)
      become: true
      ansible.builtin.systemd:
        name: docker
        state: started
        enabled: true

    - name: Verify Docker Install (probe; requires user to re-log for group — document)
      ansible.builtin.command: docker info
      register: docker_info
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe — fails harmlessly on first deploy before user re-logs
```

### `playbooks/imports/optional/common/play-ddev.yml` (simplified)

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  name: DDEV on Rootful Docker
  become: false
  vars:
    root_dir: "{{ inventory_dir }}/../../"
  tasks:
    - name: "Prereq — docker daemon reachable"
      ansible.builtin.command: docker info
      register: docker_info
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe
    - name: "Prereq — fail with actionable message if docker unavailable"
      ansible.builtin.fail:
        msg: >
          Docker daemon not reachable. Run:
            ansible-playbook playbooks/imports/play-docker.yml
          Then log out and back in (or run `newgrp docker`) to pick up group membership.
      when: docker_info.rc != 0

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

    - name: Verify DDEV Installation
      ansible.builtin.command: ddev version
      register: ddev_version
      changed_when: false

    - name: Show DDEV Version
      ansible.builtin.debug:
        var: ddev_version.stdout_lines
```

Compare to PLAN.md v2's sketch — ~100 lines of sysctl / context / fuse-overlayfs logic deleted.

## Success Criteria

- [ ] `play-docker.yml` installs rootful Docker with socket activation; user is in `docker` group
- [ ] `play-ddev.yml` probes the Docker daemon (not Podman, not rootless), installs mkcert + DDEV, verifies
- [ ] `CLAUDE/ContainerEngines.md` exists and is linked from `CLAUDE.md` Critical Rules and Topic Files Index
- [ ] `docs/ddev.md` reflects rootful Docker prereqs and references the role-split doc
- [ ] All three engines coexist — Podman, Docker, LXC — no regressions
- [ ] `ddev start` on a PHP project succeeds; `docker ps` shows the DDEV containers
- [ ] CCY continues to work — its container image and runtime are untouched
- [ ] `./scripts/qa-all.bash` passes
- [ ] PR #18 updated to reflect Approach C
- [ ] Both playbooks idempotent — second run reports zero changes

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
| ---- | ------ | ----------- | ---------- |
| User hits `docker info` fail on first deploy because group membership hasn't taken effect | Low | High | Documented in `docs/ddev.md` and in playbook fail message: log out + back in, or `newgrp docker` |
| Orphaned rootless Docker state on already-deployed hosts wastes disk / confuses users | Low | Med | Decision 3: document cleanup in release notes; `dockerd-rootless-setuptool.sh uninstall` is the one-command recovery |
| `play-docker-overlay2-migration.yml` (experimental) breaks because it assumes user-scope Docker | Very Low | Low | Decision 5: add preflight assertion flagging incompatibility |
| `docker` group = root-equivalent raises security concerns | Med | Low | Already documented prominently in `CLAUDE/ContainerEngines.md`; threat model unchanged (passwordless sudo already exists) |
| Rootful Docker daemon always running — idle resource use | Very Low | High | Socket-activation (Decision 2) minimises idle footprint |
| CCY Podman state impacted | Critical | Zero | No changes to Podman, no `podman system reset`, no storage-driver changes. CCY is never touched. |
| LXC regressions | Critical | Zero | No changes to LXC playbook or its dependencies |
| User on rootless Docker already has `--no-bind-mounts=true` set in DDEV global config | Very Low | Low | One-line note in `docs/ddev.md`: `ddev config global --no-bind-mounts=false` to reset |

## Notes & Updates

### 2026-04-17 (v1)

- Plan created; Docker-based DDEV playbook committed to `ddev` branch; PR #18 opened.

### 2026-04-21 (v2)

- PR #18 feedback from @ballidev requested pivot to rootless Podman
- Rewrote plan for rootless Podman via `docker context` — PLAN.md v2 committed in `126aac5`

### 2026-04-21 (v3 — this revision)

- Raised third option during review: rootful Docker for DDEV specifically, keeping Podman as default and LXC for VM-like use
- Wrote [container-engine-strategy.md](container-engine-strategy.md) comparing all three approaches
- User approved Approach C (rootful Docker for DDEV, with explicit top-level rule: "use Podman wherever possible; Docker for compat/legacy only; understand it is less secure")
- Phase 2 (strategy + top-level docs) completed: `CLAUDE/ContainerEngines.md` written; `CLAUDE.md` Critical Rules + Topic Files Index updated
- PLAN.md rewritten as v3 — Phases 3-8 remain to implement
