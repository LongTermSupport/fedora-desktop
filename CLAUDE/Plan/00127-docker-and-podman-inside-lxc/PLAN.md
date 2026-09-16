# Plan 00127: Docker and Podman inside LXC

**Status**: Dormant (parked by the owner; filed so the problem is tracked, no work scheduled)
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

Container engines inside this host's LXC system containers are not currently working.
The owner reports that Docker inside an LXC container does not run, and Podman inside
LXC has never been established here either. The repository already ships a host-side
play for the Docker case, `play-docker-in-lxc-support.yml` (kernel modules, sysctl,
user namespaces) and a `docker-in-lxc` wrapper that creates a container per project,
so the gap is between what that play sets up and what an engine needs today on
Fedora 44 with LXC 6 and cgroup v2.

This plan is parked. It exists so the symptom has a home, so `lxcfreeze`'s
suspend-to-disk work (Plan 00122 Phase 6) can point at it rather than reason about an
engine that is not running, and so the triage starts from a written question rather
than from memory. GitHub issue tracking: see the Delivery section.

## Goals

- A named cause for Docker not starting inside an LXC container on this host, with the
  evidence in the journal
- Docker running inside an LXC system container, deployed by the play, verified by a
  container build and run from inside
- Podman (rootless where the container allows it) running inside the same kind of
  container, so the repo's Podman-first rule holds inside LXC too
- The play, the wrapper and `docs/containerization.md` say what is required and why

## Non-Goals

- Docker and Podman coexistence on the HOST — that is GitHub issue #41 and stays there
- Rewriting the `docker-in-lxc` wrapper's project workflow; only what it needs to make
  the engine start
- Nested LXC or VMs

## Tasks

### Phase 1: Triage

- [ ] ⬜ **Task 1.1**: On the host, pick one LXC container the owner designates for
  testing, install Docker inside it as the wrapper would, and capture why the daemon
  fails: `journalctl -u docker` inside, the container's LXC config, the host's
  module and sysctl state from the play, and cgroup v2 delegation. Anonymise before
  it enters the journal
- [ ] ⬜ **Task 1.2**: The same for Podman inside the container, rootless first, rootful
  if rootless cannot work under the container's namespace setup
- [ ] ⬜ **Task 1.3**: Compare what `play-docker-in-lxc-support.yml` configures against
  what the two failures ask for. List the deltas as the Phase 2 tasks

### Phase 2: Fix, in IaC

- [ ] ⬜ **Task 2.1**: Host-side changes go in `play-docker-in-lxc-support.yml`;
  container-side changes go in the wrapper's container setup. No manual fixes in a
  running container survive this plan
- [ ] ⬜ **Task 2.2**: A plan-local `triage.bash` that reproduces Task 1.1's checks
  read-only, so the fix is verified by the same probe that found the fault
- [ ] ⬜ **Task 2.3**: Docs: `docs/containerization.md` and the play header state the
  requirements; `CLAUDE/ContainerEngines.md` gains the LXC-inside row if it is missing
- [ ] ⬜ **Task 2.4**: `qa-reviewer` over the diff before the plan is marked Complete

## Success Criteria

- [ ] `docker run --rm hello-world` succeeds inside an LXC container created by the
  wrapper, after the play alone
- [ ] `podman run --rm hello-world` succeeds inside the same container
- [ ] Both survive a container restart and a host reboot
- [ ] `./scripts/qa-all.bash` passes

## Dependencies

- **Related**: `playbooks/imports/optional/experimental/play-docker-in-lxc-support.yml`,
  `files/var/local/docker-in-lxc`, `playbooks/imports/play-lxc-install-config.yml`
- **Related**: Plan 00122 Phase 6 — suspend to disk for LXC; an engine's netfilter
  rules and overlay mounts are the hard case for CRIU, so that spike waits on nothing
  here but must be re-run once an engine is working inside a container
- **Related**: GitHub issue #41 — engine coexistence on the host, a different problem

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00127-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Tracked on GitHub as the issue linked from the journal's first entry
