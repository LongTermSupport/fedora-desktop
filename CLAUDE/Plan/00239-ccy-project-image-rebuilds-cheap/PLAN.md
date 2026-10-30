# Plan 00239: ccy project image rebuilds cheap

**Status**: Not Started (research done; the owner's questions settled at their defaults; the
host measurement is the first work)
**Created**: 2026-10-29
**Owner**: joseph
**Priority**: Medium

## Overview

A project whose `.claude/ccy/Dockerfile` compiles a lot is expensive to rebuild, and almost
every ccy release rebuilds every project image in full. The owner asked whether podman or
Docker practice, layers for instance, would make a new ccy image much cheaper. The research
([RESEARCH-layers.md](RESEARCH-layers.md)) found three causes:

1. The launcher rebuilds a project image whenever the **image ID** of `claude-yolo:latest`
   changes (`claude-yolo:2096`, `:2120`). Claude Code's daily in-place update `podman commit`s a
   new `:latest` (`:1984-2015`). So the first launch on a given day by a project without its
   own Dockerfile makes every project that has one rebuild in full. ccy releases are not the
   only trigger.
2. `LABEL claude-yolo-version` is the second instruction of the base stage (`Dockerfile:36`),
   so a container version bump (78 commits on 24 days on F44 in the last three months) very
   likely rebuilds the whole base, then every project from its first line.
3. A project's layers sit on top of everything ccy bakes in, so any change to `:latest` misses
   every cached project layer. The templates' cache mounts are set up in a way that caches
   almost nothing.

The same research found that the base stays fresh only by accident. No build passes `--pull`
(default `missing`), so `node:lts-slim` and `rust:slim` are as old as their first pull, and
only the label's position makes the apt layers rerun. The owner's constraint is that cheaper
rebuilds must never lock ccy to an out-of-date or insecure base. So this plan makes the
refresh explicit (requirement R-FRESH) and lands it with, or before, anything that removes a
rebuild.

## Goals

- A ccy release and a Claude Code update no longer rebuild a heavy project's expensive layers;
  measured on the host, before and after, in time and bytes rebuilt.
- **R-FRESH**: no cached or pinned layer outlives a stated maximum age; every upstream `FROM`
  image is checked for a newer digest on a stated cadence; a base older than the maximum age,
  or behind its upstream digest, is reported at launch and in the host health report; and the
  events that force a full rebuild are written down and tested.
- The project-Dockerfile contract for heavy projects (multi-stage builder, cache retention,
  cache mounts that work) is documented in the template, examples, prompts and
  `CUSTOM-DOCKERFILES.txt`.

## Non-Goals

- Changing any owner project's Dockerfile (the owner migrates his own; the measurement uses a
  synthetic project-A).
- A shared registry or cross-host build cache.
- Vulnerability scanning of images (owner question Q7).
- Changing Plan 00235's prune before that plan finishes; the prune changes here are designed
  with it.

## Context & Background

- Research, options (0), (a)-(e), the freshness table, the recommendation and the measurement
  design: [RESEARCH-layers.md](RESEARCH-layers.md).
- Plan 00235 (self-prune, In Progress, prune held off by its Q6): its P1 would remove a
  multi-stage builder's cache a day after the build; its non-goal "changing what ccy builds or
  when it rebuilds" is this plan.
- Plan 00216 (image build lock): every build here, the refresh timer's included, takes it.

## Owner questions

**Settled 2026-10-30 at the defaults below.** The owner ("push hard, break stuff - move
forwards", and "the full rebuild does make ccy updates expensive") wants this built, not held on
questions; each default stands until the owner overturns it. New tooling here (the refresh
timer's checker, the inputs comparison) is Go, per the project rule and Plan 00209 direction 4.

- **Q1. Refresh cadence.** Default: maximum age **7 days** for every cached layer (base, project
  and builder caches, enforced with `--cache-ttl=168h` on every ccy build), a **daily**
  upstream digest check, and an **immediate** rebuild of the slow base by the timer when an
  upstream digest has changed. Alternative: weekly only, no digest check.
- **Q2. Who rebuilds after a refresh.** Default: a user timer rebuilds the ccy base at a quiet
  time; each project image is rebuilt at its own next launch, with a one-line notice saying
  why. Alternative: the timer also rebuilds recently launched project images.
- **Q3. Reporting.** Default: ccy prints one stderr line at launch when the image it runs is
  past the maximum age or behind its upstream digest; the health report names any ccy image
  past the maximum age plus 2 days grace, or behind upstream for over 2 days.
- **Q4. Upstream tags.** Default: keep floating tags (`node:lts-slim`) with `--pull=newer`, and
  record the digest built from in a label. Alternative: pin by digest with an update-versions
  bump, which is reproducible but needs a person to move it.
- **Q5. The multi-stage contract.** Default: advisory (template, examples, prompts, docs, and
  the `--custom-docker` improvement prompt proposes it when it sees a compile); not enforced.
- **Q6. Run-time mount of ccy's own files (option b3), or the project-between-layers split
  (b2).** Default: neither now; decide after the measurement of Phase 4, only if heavy
  projects still pay too much per ccy release.
- **Q7. Image vulnerability scanning** (trivy or grype). Default: not in this plan; the
  maximum age and digest check bound staleness without it.
- **Q8. The measurement's heavy project.** Default: the synthetic project-A fixture only.
  Alternative: also one real project, named only in an untracked file the script reads.

## Tasks

### Phase 1: Measure the current cost on the host

- [ ] ⬜ **Task 1.1**: `triage.bash` (read only, `_planlib.inc.bash`, R1-R14): M1 and M6 of the
  research. In-place commit layers in `podman image history claude-yolo:latest` and their
  dates; each project's base stamp against the current `:latest` ID and the image creation
  dates; the upstream images' local creation dates; whether `node:lts-slim` carries
  `/etc/apt/apt.conf.d/docker-clean`. Project names go to the untracked run log only.
- [ ] ⬜ **Task 1.2**: the project-A fixture in the plan folder: a pinned, deterministic compile
  taking minutes, in a single-stage Dockerfile (`FROM claude-yolo-measure:latest`) and a
  multi-stage one (builder `FROM` a pinned upstream image, artefact `COPY --from`).
- [ ] ⬜ **Task 1.3**: `measure.bash` (plan script, builds only under the repository name
  `claude-yolo-measure`, takes the image build lock, removes its own tags at the end, never
  touches real images or stamps): M2 (label-only change near the top), M3 (base after a label
  bump, then project-A; project-A after an in-place commit), M4 (cached rebuild of an
  unchanged context keeps the ID), M7 (layers kept, cache-mount size). Each build's wall time
  and bytes rebuilt (new layers' sizes from `podman image inspect` and `podman image history`,
  never `podman system df`). Results to the run log and a summary line per measurement.
- [ ] ⬜ **Task 1.4**: queue the plan in `CLAUDE/Plan/meta-deploy.list` (triage, then measure as
  the plan's `deploy.bash`, which changes nothing outside the measurement repository; its
  header says so), and ask the host agent to run `./CLAUDE/Plan/meta-deploy.bash`.
- [ ] ⬜ **Task 1.5**: record the results in a supporting document and confirm or correct the
  research's claims (1.3 the daily commit trigger, M2 the label break).

### Phase 2: Freshness first (R-FRESH)

- [ ] ⬜ **Task 2.1**: `--pull=newer` and `--cache-ttl=<maximum age>` on every ccy build (the
  launcher's base and project builds, the play's build), `apt-get upgrade` in the first apt
  layer, and the inputs label (Task 3.1) carrying each upstream digest. Version bumps.
- [ ] ⬜ **Task 2.2**: the refresh: a user timer from `play-claude-yolo.yml` that checks the
  upstream digests daily and rebuilds the base under the build lock when a digest changed or
  the base is past the maximum age (Q1, Q2). In Go if it is more than a thin wrapper.
- [ ] ⬜ **Task 2.3**: detection: the launch notice (Q3) and a host health report check
  (`helpers/host_health/`), each tested with a stale and a current image.
- [ ] ⬜ **Task 2.4**: the forced-full-rebuild events (upstream digest changed, maximum age,
  `--rebuild=full`, a base Dockerfile change) tested one by one.

### Phase 3: Remove the rebuilds that buy nothing

- [ ] ⬜ **Task 3.1**: option (0): the base build writes `claude-yolo-inputs` (hash of the
  Dockerfile, the whole staged context, derived from the directory, and the upstream digests);
  the launcher compares a project against the inputs of the image its own `FROM` names, not
  the ID of `:latest`. Drop `DOCKER_BUILDKIT`, `BUILDKIT_INLINE_CACHE` and the BuildKit
  message. Version bump; launcher tests.
- [ ] ⬜ **Task 3.2**: (b1): the version label to the end of `base`, the base ordered slow to fast
  (ccy's own `COPY`s and the Claude Code install last). Container version bump.
- [ ] ⬜ **Task 3.3**: (d): `docker-clean` handled in ccy's base so project apt cache mounts
  keep packages; the templates, examples, prompts and `CUSTOM-DOCKERFILES.txt` corrected (no
  `rm -rf` in mounted directories, no `PIP_NO_CACHE_DIR`, `id=` per tool).
- [ ] ⬜ **Task 3.4**: (a): the multi-stage pattern in the template, a heavy-project example,
  the prompts and docs; ccy's runtime Debian release published as a label and in the docs;
  the `claude-yolo-build-cache` label.
- [ ] ⬜ **Task 3.5**: with Plan 00235: the prune keeps a dangling image labelled
  `claude-yolo-build-cache` until the maximum age, and removes cache-mount directories under
  `/var/tmp/buildah-cache-<uid>` untouched for the maximum age, while no build lock is held.
- [ ] ⬜ **Task 3.6**: docs: `docs/ccy.md`, `docs/containerization.md`, `CUSTOM-DOCKERFILES.txt`,
  `CLAUDE/ContainerRules.md` (what triggers a rebuild, the maximum age, the contract).

### Phase 4: Measure again and decide on (b2)/(b3)

- [ ] ⬜ **Task 4.1**: `measure.bash` M5: the same events after Phases 2 and 3; before and after
  in the results document.
- [ ] ⬜ **Task 4.2**: Q6 put to the owner with the residual cost.
- [ ] ⬜ **Task 4.3**: `acceptance.bash` for R-FRESH and the trigger changes, with its coverage
  line; the qa-reviewer agent over the plan's diff.

## Success Criteria

- [ ] After a ccy release that changes only ccy's own files, project-A (multi-stage) rebuilds
  without rerunning its compile; time and bytes rebuilt recorded before and after.
- [ ] After an in-place Claude Code update of `:latest`, no project image is rebuilt.
- [ ] **R-FRESH**: no ccy build reuses a layer older than the maximum age (`--cache-ttl` on every
  build, tested); the upstream digests are checked on the agreed cadence and a changed digest
  rebuilds the base; a base past the maximum age or behind upstream is named at launch and in
  the health report; each forced-full-rebuild event is tested.
- [ ] The builder cache of a multi-stage project survives the daily prune until the maximum age,
  and no longer.
- [ ] QA passes (`./scripts/qa-all.bash`) and the qa-reviewer agent's findings are resolved.

## Risks & Mitigations

| Risk                                                                 | Impact | Probability       | Mitigation                                                                             |
| -------------------------------------------------------------------- | ------ | ----------------- | -------------------------------------------------------------------------------------- |
| Cheaper rebuilds freeze the base                                     | H      | H without R-FRESH | Phase 2 lands before Phase 3; `--cache-ttl` on every build as the backstop             |
| An input missing from the inputs hash keeps a project on an old base | M      | M                 | the hash is derived from the whole context directory, never a list; a test adds a file |
| A builder artefact needs a newer glibc than ccy's runtime            | M      | M                 | publish ccy's Debian release; static builds in the example                             |
| The prune removes builder caches                                     | M      | H today           | Task 3.5, with Plan 00235                                                              |
| Cache-mount directory grows without bound                            | L      | H                 | Task 3.5 removes by age                                                                |
| The measurement disturbs real images                                 | M      | L                 | its own repository name, the build lock, removal of its own tags only                  |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00239-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research and plan committed
