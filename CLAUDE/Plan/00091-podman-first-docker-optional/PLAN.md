# Plan 00091: podman first docker optional

**Status**: In Progress
**Created**: 2026-09-01
**Owner**: joseph
**Priority**: Medium

## Overview

The repo's own container-engine policy (CLAUDE/ContainerEngines.md) is "Podman first, Docker
only for compatibility (e.g. DDEV)", yet `playbooks/playbook-main.yml` imports
`playbooks/imports/play-docker.yml` as a core, non-optional play. On a podman-first host that
carries `podman-docker` (the `docker` CLI shim — e.g. a headless podman-only CI host), the
`docker-ce-cli` package conflict makes the main playbook unable to converge at all.

This plan demotes rootful Docker from core to optional per the owner's direction: move
`play-docker.yml` to `playbooks/imports/optional/common/`, drop its import from
`playbook-main.yml`, and make `play-lxc-install-config.yml`'s Docker-coexistence handling
conditional on Docker actually being present (fail-fast is preserved for hosts with Docker
half-configured). The env-var opt-out of PR #40 (`RUN_BASH_SKIP_DOCKER`) is superseded and
that PR is closed. Remaining coexistence concerns are tracked in issue #41 (Refs #41).

## Goals

- `playbook-main.yml` converges on a podman-only host (no Docker CE install attempted).
- `play-docker.yml` remains fully functional as an optional play, discovered by both the
  interactive menu and headless `RUN_BASH_OPTIONAL_PLAYBOOKS`.
- `play-lxc-install-config.yml` works on hosts with and without Docker, with no error hiding.
- Docs and CLAUDE topic files describe Docker as optional; no functional reference to the old
  core path remains.

## Non-Goals

- Removing Docker support or the DDEV workflow (DDEV still requires rootful Docker).
- Adding `play-docker.yml` (or `play-ddev.yml`) to `server-recommended.bundle` — the bundle
  stays podman-first; DDEV users opt in explicitly.
- Solving the wider coexistence problems (podman-docker assertion, docker-group containment)
  — tracked in issue #41.

## Tasks

### Phase 1: Demotion

- [x] ✅ **Task 1.1**: `git mv playbooks/imports/play-docker.yml playbooks/imports/optional/common/play-docker.yml`; remove the import from `playbook-main.yml`.
- [x] ✅ **Task 1.2**: Make `play-lxc-install-config.yml` Docker-conditional: probe Docker presence; when present, keep the fail-fast DOCKER-USER assertion and reconcile tasks; when absent, skip the Docker-coexistence block (lxc-net's own FORWARD/MASQUERADE rules suffice without Docker).

### Phase 2: References

- [x] ✅ **Task 2.1**: Update all functional references to the old path (play-ddev.yml fix messages, docs/installation.md, docs/ddev.md, docs/playbooks.md, docs/architecture.md, docs/containerization.md, docs/configuration.md, docs/README.md, CLAUDE/ContainerEngines.md).

### Phase 3: QA and PR

- [x] ✅ **Task 3.1**: Run the repo QA (`scripts/qa-all.bash` relevant gates) and fix findings.
- [x] ✅ **Task 3.2**: Push branch, open PR (Refs #41), close PR #40 as superseded.

## Success Criteria

- [x] `ansible-playbook --syntax-check` passes for `playbook-main.yml` and the moved play.
- [x] No functional reference to `playbooks/imports/play-docker.yml` remains outside
  historical plan folders.
- [x] PR open with rationale, migration notes, and bundle decision; PR #40 closed.

## Delivery & Milestones

- Demotion delivered on branch podman-first-docker-optional (989457d, 67ee5a5); PR: https://github.com/LongTermSupport/fedora-desktop/pull/42
- Issue: https://github.com/LongTermSupport/fedora-desktop/issues/41
