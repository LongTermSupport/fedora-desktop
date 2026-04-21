# Container Engines in this Repo

This repo sets up three container technologies. They are not interchangeable — each has a role. This document is the canonical source of truth for which one to reach for and why.

## The one-line rule

> **Use Podman wherever possible — it is the better system. Reach for Docker only when a tool genuinely needs it for compatibility or legacy reasons, and understand that Docker is significantly less secure than Podman.**

## The role split

| Engine     | Mode         | Role                                                             | Installed by                                    |
| ---------- | ------------ | ---------------------------------------------------------------- | ----------------------------------------------- |
| **Podman** | **rootless** | Default container engine for everything. Daily use.              | `playbooks/imports/play-podman.yml`             |
| **Docker** | **rootful**  | Compatibility mode for tools that require full Docker semantics. | `playbooks/imports/play-docker.yml`             |
| **LXC**    | rootful      | Full-system, VM-like containers with systemd inside.             | `playbooks/imports/play-lxc-install-config.yml` |

All three are installed by `playbook-main.yml`. They coexist cleanly — different sockets, different storage, different networking stacks. See "Coexistence" below.

## Podman — the default, use this first

**Podman is the default container engine.** The repo variable `container_engine: podman` in `vars/container-defaults.yml` drives this: playbooks that need a container engine abstract over this variable and default to Podman.

### Why Podman is preferred

- **Rootless by default.** Containers run in your user's namespace. No root-equivalent daemon, no `docker` group, no privilege escalation pathway from a compromised container to the host.
- **Daemon-less.** Podman is a fork-exec CLI — `podman run` spawns the container directly via its own fork. There's no always-on daemon to crash, leak memory, or become an attack surface.
- **OCI-compliant.** Podman containers are the same format Docker produces. Images built with `podman build` run under Docker, and vice versa.
- **Fedora's blessed path.** Red Hat maintains Podman. Fedora ships it pre-installed. Security updates and kernel feature support land here first.
- **`podman-compose`** covers most `docker-compose.yml` use cases.

### What uses Podman in this repo

- **CCY (`claude-yolo`)** — the main daily-driver container
- **Claude devtools** (`play-claude-devtools.yml`)
- Any new container work should default here

### Rules of thumb

- Reach for Podman first. Always.
- If you're writing a new playbook that needs a container, use the `container_engine` variable — do not hardcode `podman` or `docker`.
- If Podman does not work for your use case, that is a concrete signal that you need the Docker compatibility path. Document the reason.

## Docker — compatibility mode only

**Docker is installed as rootful (system-level daemon).** Not rootless. This is a deliberate choice — see `CLAUDE/Plan/033-ddev-installation/container-engine-strategy.md` for the full analysis.

### Why Docker is rootful, not rootless

Rootless Docker exists but is a bad experience:

- Bind mounts are mapped to container root, which breaks MariaDB, Apache, PostgreSQL
- Workarounds involve Mutagen file sync with perceptible lag
- Requires `ddev config global --no-bind-mounts=true` and similar per-tool workarounds
- Privileged ports (80/443) need sysctl tweaks
- Upstream tools (DDEV included) rate it as "rougher than rootful"

When we need Docker at all, we need the Docker that tools expect — rootful.

### What uses Docker in this repo

- **DDEV** (`playbooks/imports/optional/common/play-ddev.yml`) — the canonical compatibility use case. DDEV's upstream docs explicitly recommend rootful Docker on Linux as the "best performance and stability" path.
- Any future tool that publishes a `docker-compose.yml` assuming standard Docker semantics and cannot be adapted to Podman.

### Security trade-offs — read these before using Docker

Docker rootful has a well-known security caveat: **membership in the `docker` group is root-equivalent.** A user in the `docker` group can run:

```bash
docker run -v /:/host -it alpine chroot /host sh
```

…and own the host. This is not a bug. It is the inherent design of a root daemon accessed via a Unix socket.

On a developer workstation configured by this repo, the user already has passwordless sudo (see `CLAUDE/SecurityRules.md`). Docker-group membership is not a meaningful escalation over that baseline in this context. It **is** meaningful on shared or production systems, where this repo is not intended to run.

### Rules of thumb

- Reach for Docker only when Podman has been tried and genuinely does not work.
- When you do, document the reason in the playbook that installs the tool.
- Do not install `podman-docker` (the alias package) — it conflicts with `docker-ce-cli` installed by `play-docker.yml`, and it masks the engine choice from the user.

## LXC — VM-like system containers

**LXC is a different beast.** It's not an alternative to Podman or Docker — it's an alternative to a VM.

- LXC containers run a full init (systemd inside), not just a single process
- They're long-lived, persistent
- They have their own network bridge (`lxcbr0` on `10.0.x.x`) via the trusted firewalld zone
- They're configured via the `lxc-bash` tooling cloned to `~/Projects/lxc-bash`

### What to use LXC for

- Running multiple services together as one "dev box"
- Experiments that need real system-level features (cron, systemd units, package managers inside)
- Isolating a development environment from the host more deeply than a container can

### What NOT to use LXC for

- Running a single app — that's Podman's job
- Anything you'd share as a `docker-compose.yml` — that's Docker's job

## Coexistence — all three can run at once

This repo's `playbook-main.yml` installs all three by default. They coexist cleanly:

- **Podman rootless** uses `$XDG_RUNTIME_DIR/podman/podman.sock` and `~/.local/share/containers` per-user. No daemon, no system state.
- **Docker rootful** uses `/var/run/docker.sock` and `/var/lib/docker`. System daemon, accessed via `docker` group.
- **LXC** uses `lxcbr0` bridge and `/var/lib/lxc`. Independent networking.

There is no shared state, no socket contention, and no iptables chain conflict. `podman ps` never shows Docker containers; `docker ps` never shows Podman containers; `lxc-ls` is its own universe.

**What to avoid**: running both *rootful* and *rootless* Docker on the same host. Technically possible, operationally a disaster — users lose track of which daemon owns their containers. This repo picks rootful Docker and stops there.

## FAQ

### Q: Can I build an image once and use it in both Podman and Docker?

Yes. Both are OCI-compliant. A `Dockerfile` builds to the same format. You can `podman build` and `docker build` the same file with equivalent results. Images pulled from Docker Hub work under both.

### Q: Why not use `podman-docker` to alias `docker` → `podman`?

Because it conflicts with `docker-ce-cli` installed by `play-docker.yml`. We keep them separate: Podman for Podman work, Docker for Docker work, `docker` CLI talks to the Docker daemon.

### Q: Do I need to be in the `docker` group to use Podman?

No. Podman is rootless and does not need group membership. Only Docker (rootful) needs `docker` group.

### Q: Can CCY use Docker instead of Podman?

Yes — override `container_engine: docker` in `host_vars/localhost.yml`. Not recommended (you lose the rootless isolation CCY benefits from) but supported.

### Q: Do I need to restart my shell after `play-docker.yml`?

Yes — once, to pick up the new `docker` group membership. Log out and back in, or run `newgrp docker` in the current shell.

### Q: What about `podman-compose` vs `docker-compose`?

`podman-compose` is installed by `play-podman.yml` and handles most `docker-compose.yml` files. Use it for Podman-based compose workflows. `docker compose` (the Docker CLI plugin, installed by `play-docker.yml`) handles the Docker-based ones.

## See also

- `CLAUDE/Plan/033-ddev-installation/PLAN.md` — DDEV-specific implementation plan
- `CLAUDE/Plan/033-ddev-installation/container-engine-strategy.md` — the decision-gate analysis that led to this split
- `vars/container-defaults.yml` — the `container_engine` default
- `playbooks/imports/play-podman.yml` — Podman setup
- `playbooks/imports/play-docker.yml` — Docker setup
- `playbooks/imports/play-lxc-install-config.yml` — LXC setup
