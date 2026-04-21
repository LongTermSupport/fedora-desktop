# Container Engine Strategy — DDEV and the Three-Engine Role Split

**Document type**: Decision-gate research for Plan 033
**Created**: 2026-04-21
**Status**: Analysis — awaiting user decision before Plan 033 PLAN.md is updated again
**Supersedes**: none (supplements [PLAN.md](PLAN.md) v2)

## Why this document exists

Plan 033 has now gone through two approaches for DDEV:

- **v1** (committed to branch `ddev`, PR #18): install DDEV on rootless Docker (`play-docker.yml`'s existing setup).
- **v2** (current PLAN.md): pivot to rootless Podman via `docker context` pointing at `$XDG_RUNTIME_DIR/podman/podman.sock` — reuses the repo's existing Podman stack, requires sysctl + fuse-overlayfs + context plumbing.

A third option is now on the table, raised by the user:

> "another option is we just use plain docker for ddev — but it does not like rootless so we need to use normal docker for ddev. Can we do that? Can we have rootless and not rootless docker coexisting? Should we actually make docker our 'non rootless when needed' podman, the 'default container, rootless' and LXC for more VM like?"

That's two separate questions — a technical coexistence question, and a strategic architecture question. This document answers both, consolidates all the research from v1 and v2, and lays out an explicit recommendation for the user to sign off before PLAN.md is updated.

## Executive summary

- **Can rootful + rootless Docker coexist?** Yes, technically — but it's a support trap. Both daemons work, but which one the `docker` CLI talks to depends on `$DOCKER_HOST`, the active `docker context`, and socket-activation timing. Pick one, don't run both.
- **Can rootful Docker + rootless Podman coexist?** Yes, cleanly. They share no state, listen on different sockets, and use different networking stacks. CCY stays on Podman; DDEV uses Docker; they don't see each other's containers.
- **Is the proposed three-engine role split (Podman default / Docker when needed / LXC for VM-like) a good architecture?** Yes — it's actually how DDEV's upstream docs, Fedora's ecosystem, and this repo's existing defaults all naturally slot together. This doc formalises what the repo is already drifting towards.
- **Recommended approach**: Adopt the three-engine split. Convert `play-docker.yml` from rootless → rootful. Rewrite Plan 033 as the simplest of the three options — DDEV gets `docker` group membership and a running `docker.service`, nothing more.

## Part 1 — Coexistence questions

### 1.1 Rootless Docker + Rootful Docker on the same host

**The short answer**: both can run at once; the `docker` CLI picks one via `docker context`.

| Layer                  | Rootful Docker                               | Rootless Docker                                                   |
| ---------------------- | -------------------------------------------- | ----------------------------------------------------------------- |
| Daemon unit            | `docker.service` (system)                    | `docker.service` (user) — `~/.config/systemd/user/docker.service` |
| Socket                 | `/var/run/docker.sock` (root:docker 660)     | `$XDG_RUNTIME_DIR/docker.sock` (user 660)                         |
| Storage                | `/var/lib/docker`                            | `~/.local/share/docker`                                           |
| Networking             | iptables NAT on host                         | slirp4netns in user netns                                         |
| Privileged ports       | native                                       | blocked unless `ip_unprivileged_port_start=0`                     |
| `docker` CLI picks via | `docker context default` (or `$DOCKER_HOST`) | `docker context rootless` (or `$DOCKER_HOST`)                     |

**What breaks if you run both:**

- Nothing at the kernel level — different netns, different storage, different sockets.
- At the UX level: users lose track of which daemon has their containers. `docker ps` on context A is empty; context B has them. Three times a week someone files a bug report that "docker is broken."
- The `docker` group: users in `docker` group can talk to the rootful daemon (which is root-equivalent). Rootless doesn't need group membership. If a user has both set up, the group membership is dormant but the daemon is still running.

**Verdict**: technically coexists, operationally a mess. Pick one Docker mode. (This is also Docker upstream's advice.)

### 1.2 Rootful Docker + Rootless Podman on the same host

**The short answer**: clean coexistence. They share no state.

| Concern          | Status                                                                                                                                       |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| Sockets          | `/var/run/docker.sock` (Docker) vs `$XDG_RUNTIME_DIR/podman/podman.sock` (Podman) — no overlap                                               |
| Storage          | `/var/lib/docker` (Docker) vs `~/.local/share/containers` (Podman) — no overlap                                                              |
| Networking       | Docker uses iptables NAT; Podman rootless uses pasta/slirp4netns in user netns. Podman's user-space networking does not touch host iptables. |
| iptables rules   | Docker owns its `DOCKER`, `DOCKER-USER` chains. Podman rootless adds no host chains.                                                         |
| Container images | Separate registries in separate storage — both can pull `nginx:latest` independently without interference.                                   |
| cgroups          | Docker rootful is in system cgroups. Podman rootless is in `user.slice/user-$UID.slice/user@.service/…`.                                     |
| User perception  | `docker ps` shows Docker containers. `podman ps` shows Podman containers. Never the same list.                                               |
| CCY risk         | CCY uses `podman build` / `podman run` directly — it never touches `docker`. Docker daemon coming or going is invisible to CCY.              |

**One edge case**: if a container published to port 80 from both engines simultaneously, second one fails. That's expected single-port-binding behaviour — nothing engine-specific. (In practice: DDEV on Docker rootful binds 80/443; Podman rootless containers use different ports.)

**Verdict**: both can run, do not interact, and CCY is safe.

### 1.3 LXC + Podman/Docker on the same host

Already the status quo in this repo. `play-lxc-install-config.yml` sets up `lxcbr0` (10.0.x.x in firewalld trusted zone) and loads iptables kernel modules. LXC containers are full-system containers with their own init, kernel modules, and network stack via the bridge. Podman rootless and Docker rootful both ignore `lxcbr0` — they create their own networking.

The iptables modules loaded for LXC (`ip_tables`, `iptable_nat`, `iptable_mangle`, `iptable_filter` — `play-lxc-install-config.yml:142-154`) are also what Docker rootful uses for NAT. Having LXC in the stack **helps** Docker by pre-loading these modules.

**Verdict**: all three coexist cleanly. No changes needed.

## Part 2 — DDEV on each engine

### 2.1 Approach A: DDEV on rootless Docker (original v1)

What `play-docker.yml` currently sets up. DDEV needs:

- `ddev config global --no-bind-mounts=true` — because rootless Docker maps bind mounts to container root, which breaks MariaDB, Apache, PostgreSQL
- Mutagen file sync (implied by `--no-bind-mounts`) — perceivable lag between file save and container-side visibility
- `net.ipv4.ip_unprivileged_port_start=0` sysctl — otherwise ddev-router can't bind 80/443
- `DOCKERD_ROOTLESS_ROOTLESSKIT_DISABLE_HOST_LOOPBACK=false` in `~/.config/systemd/user/docker.service.d/override.conf` — otherwise Xdebug loopback fails

**Pros**: unprivileged daemon, contained blast radius.
**Cons**: ≥4 non-trivial config knobs, Mutagen overhead, non-default DDEV global state, upstream rates it as rougher than rootful.
**DDEV upstream verdict**: works but adds friction.

### 2.2 Approach B: DDEV on rootless Podman (current PLAN.md v2)

What PLAN.md v2 documents. DDEV needs:

- `docker context create podman-rootless --docker host=unix://…/podman.sock`
- `docker context use podman-rootless`
- `net.ipv4.ip_unprivileged_port_start=0` sysctl (same as rootless Docker)
- Optional (but recommended for perf) `fuse-overlayfs` + `~/.config/containers/storage.conf`
- No `--no-bind-mounts` needed (Podman uses `--userns=keep-id`)

**Pros**: reuses the Podman stack already deployed by `play-podman.yml`; cleaner than rootless Docker (no bind-mount mangling).
**Cons**: DDEV labels Podman support as "experimental"; fuse-overlayfs requires `podman system reset` (wipes CCY image) if applied after existing Podman state exists → guarded opt-in; docker-context indirection surprises users when `docker ps` shows DDEV containers but `podman ps` also shows them (it's the same daemon, viewed from two CLIs).
**DDEV upstream verdict**: "experimental" — their word, from `docs/content/users/install/docker-installation.md`.

### 2.3 Approach C: DDEV on rootful Docker (the new proposal)

What the user is now asking about. DDEV needs:

- `docker.service` enabled and started system-wide
- `{{ user_login }}` in `docker` group (gives root-equivalent access to Docker socket — standard dev-desktop trade-off)
- …nothing else

DDEV's own docs explicitly label this the **"recommended, free, open-source, best performance and stability"** option on Linux (per `docker-installation.md` fetched from upstream). No sysctl tweak. No bind-mount flag. No Mutagen. No storage driver fiddling. No docker context. DDEV's happy path.

**Pros**: simplest; upstream's recommended configuration; best performance; `.ddev/config.yaml` files shared with colleagues Just Work; no context indirection.
**Cons**: root-equivalent access via `docker` group (standard, accepted for dev machines); always-on root daemon (mitigable via `docker.socket` activation); security-wise weaker than rootless — but not catastrophically so for a developer workstation.

**Security reality check**: membership in the `docker` group means the user can do `docker run -v /:/host -it alpine chroot /host sh` and own the machine. This is well-known. It is not materially different from the fact that the user already has passwordless `sudo` (`CLAUDE/SecurityRules.md` → "Passwordless sudo configured for main user"). For this repo's threat model — a single-user developer workstation — docker-group membership is not an escalation over the existing baseline.

## Part 3 — Current state of this repo's container engines

Consolidated from the earlier research phase:

### What's installed by default (`playbooks/playbook-main.yml`)

| Playbook                       | Line | What it sets up                                                                                       |
| ------------------------------ | ---- | ----------------------------------------------------------------------------------------------------- |
| `play-lxc-install-config.yml`  | 14   | LXC + `lxcbr0` + iptables kernel modules + `lxc-bash` project clone                                   |
| `play-docker.yml`              | 18   | docker-ce + rootless setup via `dockerd-rootless-setuptool.sh install`, user `docker.service` enabled |
| `play-podman.yml`              | 19   | podman + podman-compose + user `podman.socket` enabled                                                |
| `play-systemd-user-tweaks.yml` | 8    | Disables systemd-oomd aggressive killing (benefits all container engines)                             |

### What uses what

| Tool                                         | Engine it uses                          | How                                                                                                                              |
| -------------------------------------------- | --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| CCY (`claude-yolo`)                          | **Podman rootless** (default)           | `container_engine: podman` in `vars/container-defaults.yml:10`; `podman build`/`podman run` directly — no context, no docker CLI |
| Claude devtools (`play-claude-devtools.yml`) | Either, via `container_engine` variable | Abstracted through `container_cmd()` in `lib/docker-health.bash`                                                                 |
| LXC / `lxc-bash`                             | **LXC**                                 | Dedicated tooling in `~/Projects/lxc-bash`                                                                                       |
| DDEV (v1 on PR branch)                       | **Docker rootless** (hardcoded)         | `docker --version` check in current playbook                                                                                     |
| Docker Compose helper scripts                | None currently                          | Podman-compose is installed but not enforced by any playbook                                                                     |
| Other                                        | None                                    | Nothing else in the repo depends on a container engine                                                                           |

### Orphaned setup

**Rootless Docker has no current consumer.** It was installed to support DDEV. If DDEV moves to either rootless Podman (B) or rootful Docker (C), the rootless Docker setup becomes unused. It still takes up:

- A user `docker.service` running
- `/etc/subuid` and `/etc/subgid` entries (100000:65536)
- Storage at `~/.local/share/docker`

Not broken, just orphaned. Cleanup is optional but aesthetically correct.

## Part 4 — The proposed three-engine role split

The user's suggested architecture:

| Engine     | Role                                                                              | Rootful/rootless | Primary users                                                                                                                        |
| ---------- | --------------------------------------------------------------------------------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| **Podman** | Default container engine, rootless, daily use                                     | rootless         | CCY, claude-devtools, ad-hoc `podman run`, anything where container security matters more than compatibility                         |
| **Docker** | "Non-rootless when needed" — compatibility mode for tools that expect full Docker | **rootful**      | DDEV, any project with a standard docker-compose.yml shared across a team, tools that assume Docker socket at `/var/run/docker.sock` |
| **LXC**    | VM-like full-system containers                                                    | rootful          | `lxc-bash` projects, long-lived system containers with systemd inside, "dev boxes"                                                   |

### Why this architecture works

Each engine plays to its strength:

- **Podman rootless** is what Fedora pushes as the modern default. Per-user daemon-less model fits CCY's ephemeral "container per invocation" pattern. CCY already uses it.
- **Docker rootful** is what *the industry at large* uses. DDEV, Testcontainers, docker-compose stacks published by upstream projects — they all assume this. Fighting that upstream current costs config complexity (v1's Mutagen and `--no-bind-mounts`, v2's context indirection and fuse-overlayfs reset). The "non-rootless when needed" framing is honest: we admit rootful is rootful, gate it on membership in the `docker` group, and get back to work.
- **LXC** is the only one of the three that gives you systemd-inside and a proper init. Docker and Podman containers are "a process and maybe its children"; LXC containers are "a tiny Fedora." Very different use case.

### Why this beats Approach B (rootless Podman for DDEV)

Approach B would work, but:

- Makes DDEV run on DDEV's "experimental" container engine.
- Adds a `docker context` indirection that will confuse users the first time `docker ps` and `podman ps` both show the same container.
- Requires the fuse-overlayfs guard gymnastics to avoid destroying CCY's image.
- Adds a sysctl change (`ip_unprivileged_port_start=0`) that's a security-relevant knob.

Approach C has *none* of these costs. And it aligns DDEV with DDEV's upstream recommendation.

### Why this keeps Podman as the default

Even with Docker rootful added, Podman remains the **default** for:

- CCY (explicit via `container_engine: podman`)
- Any script or playbook that wants a container without the root escalation surface
- Future work that uses containers for isolation rather than compatibility

Docker becomes the *opt-in compatibility* engine. You reach for it when a tool demands it (DDEV, a colleague's docker-compose stack). For everything else, Podman.

## Part 5 — What changes to implement Approach C

The changes are smaller than Approach B. Three files and one playbook-main re-run on the host.

### 5.1 `playbooks/imports/play-docker.yml` — rootless → rootful

Current (lines 41-65) sets up rootless. Replacement:

```yaml
# After dnf install of docker-ce packages (unchanged)

- name: Add {{ user_login }} to docker group
  become: true
  ansible.builtin.user:
    name: "{{ user_login }}"
    groups: docker
    append: true

- name: Enable and start docker.service (system-wide)
  become: true
  ansible.builtin.systemd:
    name: docker
    state: started
    enabled: true

- name: Enable docker.socket (socket activation)
  become: true
  ansible.builtin.systemd:
    name: docker.socket
    state: started
    enabled: true
```

**Delete**:

- `dockerd-rootless-setuptool.sh install` task
- User-scope `systemd` enable of `docker.service`
- (Optionally) `/etc/subuid` and `/etc/subgid` block — rootful Docker doesn't need it. Keeping it for now costs nothing; removing it is a separate cleanup.

### 5.2 `playbooks/imports/optional/common/play-ddev.yml` — dramatic simplification

Revert to something very close to the v1 playbook, but with cleaner prereq checks:

```yaml
- name: "Prereq — docker daemon running"
  ansible.builtin.command: docker info
  register: docker_info
  changed_when: false
  failed_when: false
- name: "Prereq — fail if docker daemon not accessible"
  ansible.builtin.fail:
    msg: "docker.service not running or user not in docker group. Run: ansible-playbook playbooks/imports/play-docker.yml"
  when: docker_info.rc != 0

# mkcert (keep v1)
# DDEV yum repo + dnf install (keep v1)
# Verify ddev version (keep v1)
```

**Delete** (relative to v2):

- Podman version probe
- podman.socket probe
- Sysctl for `ip_unprivileged_port_start`
- Docker-context creation/use
- fuse-overlayfs install and guard
- subuid/subgid probe

That's ~100 lines of playbook logic that simply vanishes.

### 5.3 `docs/ddev.md` — simplify prereqs

Replace the "Prerequisite: Docker" section to call `play-docker.yml` (which now sets up rootful), drop any rootless-specific troubleshooting, add one sentence explaining the three-engine role split so users aren't surprised that CCY is on Podman while DDEV is on Docker.

### 5.4 `CLAUDE.md` or a new `CLAUDE/ContainerEngines.md` — document the role split

The strategic decision deserves a short doc. One page explaining: "Podman is the default rootless engine; Docker (rootful) is available as a compatibility layer; LXC is for full-system containers." Future contributors know which to reach for.

Not strictly required for Plan 033 to land, but cheap to include.

### 5.5 What about the orphaned rootless Docker state on already-deployed hosts?

Users who already ran `play-docker.yml` in rootless mode have:

- `~/.config/systemd/user/docker.service` (from `dockerd-rootless-setuptool.sh`)
- `~/.local/share/docker` (rootless container storage)

On next `playbook-main.yml` run (after the `play-docker.yml` edit lands):

- New tasks will add them to `docker` group and enable system `docker.service`.
- Existing user `docker.service` still exists. Doesn't conflict (different socket), but wastes resources.

Cleanup options:

1. **Leave it alone.** User can run `dockerd-rootless-setuptool.sh uninstall` if they care. Documented in release notes.
2. **Active cleanup.** Add a one-time `dockerd-rootless-setuptool.sh uninstall` task with `creates`/`failed_when` guards. Low risk because the tool is idempotent.

Recommend option 1 for this plan — keeps the scope tight. Option 2 is a one-line follow-up plan.

## Part 6 — Implementation impact comparison

For each approach, the changes required to ship:

| Approach                     | `play-docker.yml`                      | `play-podman.yml` | `play-ddev.yml`                                            | Docs           | sysctl changes                       | Risk to CCY                                          |
| ---------------------------- | -------------------------------------- | ----------------- | ---------------------------------------------------------- | -------------- | ------------------------------------ | ---------------------------------------------------- |
| **A — rootless Docker (v1)** | No change                              | No change         | Keeps current Docker dependency                            | v1 docs        | Yes (`ip_unprivileged_port_start=0`) | None (Podman untouched)                              |
| **B — rootless Podman (v2)** | No change                              | No change         | Heavy refactor (+sysctl, +docker-context, ±fuse-overlayfs) | Refactor       | Yes (`ip_unprivileged_port_start=0`) | Medium (fuse-overlayfs + reset path must be guarded) |
| **C — rootful Docker (new)** | Rewrite rootless → rootful (~15 lines) | No change         | Simplify to v1 minus rootless gotchas                      | Light refactor | **None**                             | None (Podman untouched)                              |

Approach C is strictly less invasive than B for the same end-user experience (`ddev start` works), and it aligns with upstream DDEV's recommendation.

## Part 7 — Risks and caveats for Approach C

| Risk                                                                                    | Impact   | Mitigation                                                                                                |
| --------------------------------------------------------------------------------------- | -------- | --------------------------------------------------------------------------------------------------------- |
| Rootful Docker daemon always running (security posture)                                 | Low-Med  | Use `docker.socket` activation so `docker.service` starts on first socket access — minimal idle footprint |
| User in `docker` group ≈ root                                                           | Med      | Accepted — same threat model as existing passwordless sudo; document in `CLAUDE/ContainerEngines.md`      |
| Orphaned rootless Docker state on already-deployed hosts                                | Low      | Document cleanup in release notes (option 1); optional follow-up plan for automated cleanup               |
| Users with local projects still configured for rootless Docker (`--no-bind-mounts` set) | Very Low | DDEV's global config key is per-user; they can run `ddev config global --no-bind-mounts=false` once       |
| `docker` CLI conflict with `podman-docker` shim                                         | N/A      | We don't install `podman-docker`; no conflict                                                             |
| SELinux `:Z` / `:z` labelling on bind mounts                                            | Low      | Fedora standard; DDEV already sets `:rw` with context-aware labels                                        |
| First `docker run` after install needs fresh group membership shell                     | Very Low | Document: user logs out/in or runs `newgrp docker` once — one-time                                        |

## Part 8 — Open questions before commit

These are the decisions the user needs to make before PLAN.md is rewritten one more time:

1. **Approach**: A, B, or C?
   Recommended: **C** (rootful Docker for DDEV, Podman stays the default elsewhere).

2. **Rootless Docker cleanup**: active uninstall in the playbook, or leave orphaned on existing hosts?
   Recommended: **leave orphaned, document in release notes**. Active cleanup is a separate plan.

3. **Socket activation**: enable `docker.socket` alongside `docker.service`?
   Recommended: **yes** — minimal idle footprint, standard Fedora pattern.

4. **`CLAUDE/ContainerEngines.md`**: write it now as part of Plan 033, or defer?
   Recommended: **write now** — one page, cements the role split so future work doesn't re-litigate this.

5. **PR #18**: squash-merge v1/v2/v3 into one clean commit, or keep the history?
   Recommended: **keep history** (v1 → v2 → v3 is an interesting design trail for future readers).

## Recommendation

Adopt the three-engine role split. Rewrite Plan 033 as **Approach C** — DDEV on rootful Docker, keeping Podman as the default rootless engine and LXC for VM-like containers. This:

- Aligns this repo with DDEV's own upstream "best performance and stability" recommendation.
- Minimises the change to the most contentious file (`play-docker.yml` gets a rootless → rootful swap; `play-ddev.yml` gets *simpler*, not more complex).
- Zero risk to CCY's Podman container state.
- No sysctl changes.
- No storage-driver gymnastics.
- Formalises a role split (Podman default / Docker when needed / LXC VM-like) that the codebase is already drifting toward.

If the user concurs, the next plan revision (`PLAN.md` v3) will:

1. Collapse Phases 2 and 3 of current PLAN.md into a much smaller Phase 2: "convert `play-docker.yml` to rootful + simplify `play-ddev.yml`."
2. Drop the fuse-overlayfs guard, sysctl, and docker-context tasks entirely.
3. Add a small Phase 3: "write `CLAUDE/ContainerEngines.md` documenting the role split."
4. Update `docs/ddev.md` with the simplified prereqs.
5. Leave CCY, LXC, and Podman setup completely untouched.

## References

- [DDEV Podman + Docker Rootless blog post](https://ddev.com/blog/podman-and-docker-rootless/) — source for Approach B's setup steps
- [DDEV docker-installation.md (upstream)](https://raw.githubusercontent.com/ddev/ddev/main/docs/content/users/install/docker-installation.md) — states "Docker for Linux: Recommended, free, open-source, best performance and stability"
- [DDEV requirements.go](https://raw.githubusercontent.com/ddev/ddev/main/pkg/dockerutil/requirements.go) — Docker 25.0 / Podman 5.0 minimum versions
- `playbooks/imports/play-docker.yml` — current rootless setup (lines 41-65 are the target of the rewrite)
- `playbooks/imports/play-podman.yml` — current Podman setup (unchanged under all approaches)
- `playbooks/imports/play-lxc-install-config.yml` — LXC setup (unchanged)
- `vars/container-defaults.yml` — `container_engine: podman` repo default
- `files/var/local/claude-yolo/` — CCY, which uses Podman directly (unchanged under all approaches)
