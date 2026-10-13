# Plan 00190: Docker and Podman coexistence on the host, the remaining concerns

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #41.

## Overview

#41 began when `play-docker.yml` was a core import, so every converge installed rootful
Docker CE, against the repo's Podman-first rule. A host carrying `podman-docker` could not
converge at all. Plan 00091 (Completed) made Docker optional: `play-docker.yml` now lives in
`playbooks/imports/optional/common/`, and `playbook-main.yml` no longer imports it. 00091
left the remaining coexistence concerns on #41, and Plan 00127 (Docker and Podman inside LXC)
explicitly excludes them. So no active plan owns them.

The issue lists four leftovers. DDEV needs rootful Docker, so `play-docker.yml` must stay
working as an optional play, and `play-ddev.yml` must keep failing fast with a pointer when
Docker is absent. Docker group membership is root-equivalent; whether DDEV setups can be
contained further is open. `podman-docker` and `docker-ce-cli` can never coexist, and nothing
asserts the choice or explains the conflict when someone selects `play-docker.yml` on a host
that carries `podman-docker`. The play still carries the legacy cleanup of hand-managed
`/etc/subuid` and `/etc/subgid` blocks, and no future engine work may reintroduce
hand-managed ranges.

## Goals

- `play-docker.yml` on a host with `podman-docker` stops before dnf, with a message that
  names the conflict and the choice.
- `play-ddev.yml`'s fail-fast pointer when Docker is absent is confirmed and tested.
- A guard stops any play reintroducing hand-managed subuid/subgid ranges; the legacy cleanup
  is kept or retired on evidence.
- The containment question for DDEV's Docker is answered and recorded.

## Non-Goals

- Engines inside LXC (Plan 00127).
- Removing Docker support while DDEV needs it.

## Tasks

### Phase 1: Triage

- [ ] ⬜ **Task 1.1**: `triage.bash`: which `docker` CLI provider is installed, Docker group
  members, the subuid/subgid files against Fedora's allocations, and whether the legacy
  marker blocks are still present on this host.

### Phase 2: Build

- [ ] ⬜ **Task 2.1**: A pre-flight in `play-docker.yml` that refuses on `podman-docker` with
  the explanation; the same check stated in `CLAUDE/ContainerEngines.md`.
- [ ] ⬜ **Task 2.2**: Confirm and, if missing, test `play-ddev.yml`'s Docker-absent refusal.
- [ ] ⬜ **Task 2.3**: A QA pattern that flags hand-managed subuid/subgid blocks in plays;
  decide on the legacy cleanup from the triage.
- [ ] ⬜ **Task 2.4**: Research DDEV containment options; owner decides whether anything is
  built.
- [ ] ⬜ **Task 2.5**: `deploy.bash`, `acceptance.bash`, an entry in `meta-deploy.bash`;
  `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] Selecting `play-docker.yml` on a `podman-docker` host stops with a clear explanation
  before any package change.
- [ ] No play can reintroduce hand-managed subuid/subgid ranges unnoticed.
- [ ] Each of #41's four concerns has a recorded outcome.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00190-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #41's remaining concerns.
