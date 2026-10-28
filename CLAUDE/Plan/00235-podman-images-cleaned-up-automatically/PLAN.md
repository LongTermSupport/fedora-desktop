# Plan 00235: podman images cleaned up automatically

**Status**: In Progress (the first prune freed 299G, then broke the store; the repair and timer gate (4.5, 4.6, 4.8) are deployed or deploying; Phase 3 (3.1, 3.2) built and gated on a clean store; Q6 is the owner's)
**Created**: 2026-10-28
**Owner**: joseph
**Priority**: High

## Overview

The host's root filesystem (one btrfs for `/` and `/home`) reached 99%. Half the disk was the
rootless podman store: 694 images, 639 of them dangling (`<none>:<none>`), added at 100-250 a
month since July. Nothing in this repository ever removes an image. ccy builds and commits onto
fixed tags (`claude-yolo:base`, `:latest`, `:<project>`), so every rebuild and every daily
Claude Code update leaves the image the tag used to name behind, and no timer, play or
health check looks at the store. The owner's request: "we MUST have a proper podman cleanup
system ... with automated cleanup procedures"; "DO NOT JUST FIX THIS STUFF MANUALLY"; "the ccy
system itself needs pruning built in"; "and then some kind of catchall in the fedora health
report".

So four layers, each catching what the one before misses: ccy removes what its own build
replaced, the moment it replaces it; a daily user timer, installed by a play, removes dangling
images no container uses from any source; the host-health report says when the disk or the
store is over a threshold anyway; and a QA gate refuses a new image-building path that has no
cleanup. The existing backlog is cleared by that timer's first run on the host, through the
play. No step of this plan is a manual prune.

Research, with file:line for every claim below:
[subagent-reports/261028-plan-research.md](subagent-reports/261028-plan-research.md).

## Goals

- A ccy build or in-place update that replaces an image removes the replaced image when no
  tag names it and no container (running or stopped) uses it, and says so on stderr.
- A daily user timer removes dangling images no container uses, and buildah working containers
  an interrupted build left, logging each removal (id, size, labels) to the journal and to a
  retention log; it never removes a tagged image in use, nor any volume.
- The host-health report (panel and login message) names low disk free and an overgrown store.
- A QA gate fails on any image-building call site that is not paired with the cleanup.
- On the host, after the deploy: the backlog of unused dangling images is gone, and stays gone.

## Non-Goals

- Removing volumes. Dangling volumes are reported; the owner decides (Question Q2).
- Removing `claude-yolo:base` or `:latest`, or any image a container uses.
- Changing what ccy builds or when it rebuilds.
- The interactive `reclaim` tool (Plan 00062, Plan 00192's port): it stays the manual tool for
  everything else; this plan is the automation it lacks.
- Docker's store. The command and the timer hard-code podman, whose store is the subject (Docker is
  compatibility only, and DDEV owns its images).

## Context & Background

### Facts

| #   | Fact                                                                                                                                                                                                                | Source                  |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------- |
| F1  | One btrfs for `/` and `/home`, 1.6T; 146G free (91%) after the VM lab's prune freed 107.5 GiB; it had been at 99%                                                                                                   | host agent, 2026-10-28  |
| F2  | Rootless overlay storage 769G; 694 images, 639 dangling, 628 used by no container; 14 containers; 35 volumes, 30 dangling                                                                                           | host agent              |
| F3  | Dangling by month: 2025-12 1, 2026-01 4, 02 13, 03 7, 04 3, 05 4, 06 10, 07 85, 08 249, 09 141, 10 122                                                                                                              | host agent              |
| F4  | Of the 400 newest dangling: 314 carry no `org.opencontainers.image.title`, 84 are one project's custom-Dockerfile base (a PHP server image), 2 the unifi controller's                                               | host agent              |
| F5  | Eight image-making paths (B1-B8); B1-B6 all run inside `ccy_image_build_locked`; B7 (devtools build) and B8 (unifi `compose pull`) do not; none removes the replaced image                                          | research §1             |
| F6  | Nothing in `playbooks/`, `tasks/`, `files/` prunes images; no prune timer exists                                                                                                                                    | research §2, host agent |
| F7  | `helpers/host_health` has no disk or podman probe; a failed user unit is already a finding                                                                                                                          | research §4             |
| F8  | An orphaned volume owned outside the current subuid range makes `podman system df` exit 125; reclaim 2.0.x copes by listing as JSON                                                                                 | host agent; research §2 |
| F9  | `lib/image-build-lock.bash` has 57 code lines (limit 60); the launcher is on the bash-limits ratchet (`scripts/bash-limits.allowlist:50`)                                                                           | research §1             |
| F10 | The first prune removed 639 things (299G freed), then left 3 c/storage temp dirs holding files owned by old-range uids; c/storage retries them on every layer store load and fails, so every image operation failed | host agent, 2026-10-28  |

`triage.bash` re-measures F1-F4 and F6 (`./triage.bash --du` for the store's size on disk).

### Hypotheses

- **H1**: most dangling images are ccy's own, chiefly B5 (the daily in-place Claude Code
  update commits onto each image a launch uses). Confirmed if triage's `labels` leg shows most
  newest-dangling images carry a `claude-yolo-version` label (project images inherit it from
  the base); refuted if most carry none.
- **H2**: `podman image prune` (dangling, unused) frees most of F2's 769G. Confirmed by the
  `images` leg's summed size (an upper bound: shared layers counted again) and by `--du`
  before and after the first timer run.

### Unverified premises

- That a stopped container's image is reported by `podman ps -a --format {{.ImageID}}` (the
  probe and the timer both rely on it; the Go fake must mirror the real output, Task 3.1).
- That `podman image rm <id>` refuses, rather than forces, an image a container uses (the
  self-prune leans on podman's own refusal as a second guard, never as the first).

### Precedent

Plan 00192 Task 3.10: the VM lab kept every failed run's overlay for ever. Its fix had the
shape this plan follows: a Go rule with one home, a dry run unless `--yes`, live things
(libvirt domains, `run.lock`) left, every eviction logged once it has happened, the evidence
the plans cite kept.

## Tasks

### Phase 1: Research

- [x] ✅ **Task 1.1**: Every image-building path, the removal paths, the timer's home, the
  health probe's route, the Defence Before Fix rule
  ([report](subagent-reports/261028-plan-research.md))

### Phase 2: Triage on the host

- [ ] ⬜ **Task 2.1**: The host agent runs meta-deploy (this plan's `triage.bash --du`,
  read-only); F1-F4 re-measured, H1 and H2 settled from the report, the facts table updated

### Phase 3: ccy removes what its build replaced

- [x] ✅ **Task 3.1**: Tests first, then a Go command (working name `container-images`, Decision
  1. in `src-go/cmd/` over `internal/engine`: `before <repository>` prints the id of each of the
     repository's tags; `replaced <repository> <ids-file>` removes each recorded id that no tag now
     names and no container (`ps -a`) uses, naming each removal and each kept image (with why) on
     stderr. A self-prune failure after the build (a refused removal, a list or log it cannot
     get) is reported and not fatal to the launch; while another ccy build lock is held,
     `replaced --lock <own lock>` removes nothing (review F1, F2). Fakes mirror the real
     engine's output (`execx.ErrUnexpected` for anything unscripted)
  - [x] ✅ cases: two containers of one project on the old image (agent teams) keep it; a stopped
    container keeps it; `:base` re-tagged under the `:latest` key is covered; a failed build
    removes nothing; an id another tag still names is kept
- [x] ✅ **Task 3.2**: `ccy_image_build_locked` (`lib/image-build-lock.bash:28`) records before the
  build and calls `replaced` after a successful one, inside the lock; within the 60-line limit
  (measured by `qa-bash-limits`), else the lock's own lines shrink. This covers B1-B6. CCY
  version bump; play-claude-yolo.yml builds and installs the command. Built:
  `lib/image-build-prune.bash` (sourced by the lock, which stays at 58 lines and 90
  statements, and passes its lock file to it); CCY 3.102.0, 3.102.1 with the review fixes; `tasks/claude-yolo-image.yml`
  installs the command before it copies the libs that call it
- [ ] ⬜ **Task 3.3**: B7: `play-claude-devtools.yml` runs the same `before`/`replaced` around its
  build
- [x] ✅ **Task 3.4**: Stale project images: `claude-yolo:<project>` tags no container uses and
  unused for longer than Q1's age are removed by the timer (Phase 4), recorded in its log; the
  next launch of that project rebuilds it as it does today. Built into `prune` (rule P2 in
  `src-go/internal/containerimages`): stale only when both its creation and its last ccy launch (the mtime of
  ~/.cache/claude-yolo-update-checks/claude-yolo\_<project>; creation alone when absent) are over 30 days old

### Phase 4: The daily catch-all

- [x] ✅ **Task 4.1**: Tests first, then the same command's `prune [--yes]`: a dry run unless
  `--yes`; removes dangling images used by no container and older than a day (so a build in
  flight is never touched), buildah working containers (command "buildah", never another "storage" container) older than a day; nothing at all while a ccy build lock is held; one --yes run at a time (a run lock); a success stamp `container-images.succeeded`, and Task 3.4's stale project tags; lists dangling volumes (count, names) and removes
  none; never calls `podman system df` (F8); appends each removal to
  `~/.local/state/fedora-desktop/container-images.log` (time, id, size, labels, why) only after
  the removal succeeded, and reports a failed one by name, exiting non-zero. `src-go/cmd/container-images`
  over `src-go/internal/containerimages` (P1-P8 in its package doc) and `internal/engine/store.go`;
  the podman output shapes it reads are in [subagent-reports/261028-prune-timer-built.md](subagent-reports/261028-prune-timer-built.md)
- [x] ✅ **Task 4.2**: `play-podman.yml` (Decision 2) imports `play-golang.yml`, builds the
  command with `tasks/build-go-command.yml`, and installs `container-images-prune.service` and
  `.timer` (user scope, daily, `Persistent=true`; podman hard-coded on purpose, see below), modelled on
  `playbooks/imports/optional/common/play-container-watch.yml`; the service runs `prune --yes`. Until a prune has succeeded (its stamp) the play starts the service once, so the deploy itself clears the backlog (Q4).
  The timer prunes podman's store, which this play installs whatever `container_engine` names
- [x] ✅ **Task 4.3**: `docs/` and `CLAUDE/ContainerEngines.md`: what is removed, when, where the
  log is, and how to see what the next run would remove (`container-images prune`)
- [ ] ⬜ **Task 4.4**: The orphaned volume (Q2b), removed once by a root step in `play-podman.yml`,
  probe then remove. The probe finds it by its property, never by a hard-coded name: a volume of
  the user's rootless store whose data is owned by a uid that is neither the user's nor inside
  the user's current subuid range (read from `/etc/subuid`), and that no container (`ps -a`)
  mounts. The step removes each such volume's data as root and then its record as the user
  (`podman volume rm`), logs each removal (name, owner uid, size) to the play output and the
  retention log of Task 4.1, and fails on a removal that does not succeed; with no such volume it
  changes nothing. Tests first for the probe's judgement (a volume in range, out of range, in
  use). Acceptance (Task 7.1): `podman system df` exits 0 afterwards. NOT BUILT: Plan 00062
  found 9 such volumes, one holding real database data, so "removed once" needs the owner's
  confirmation (Q2b, and Q6's option A would make them usable again instead)
- [x] ✅ **Task 4.5**: `tasks/podman-store-repair.yml`, imported by play-podman.yml before its
  first podman question and after the first prune: as root, removes each
  `overlay/tempdirs/temp-dir-N` and its `lock-N`, logging path and size; fails instead while a
  podman or buildah of the user removes or builds, or a lock there is held (`/proc/locks`)
  ([report](subagent-reports/261028-store-repair.md))
- [x] ✅ **Task 4.6**: P6a: the prune stops at the first removal refused with permission denied
  in c/storage's tempdirs, names the cause and the repair once, and exits 1 (tests first,
  journalled)
- [ ] ⬜ **Task 4.7** (FIX, Q6): layers still in the store with files owned by host uids outside
  the user's uid and current subuid range (the old range, 100000+k) cannot be deleted rootless;
  each prune meeting one stops (P6a) and needs the repair. The same open question as Plan 00062
  Task 4.8
- [x] ✅ **Task 4.8**: `tasks/podman-store-ownership-probe.yml`, imported by play-podman.yml after
  the repair and before the timer: as root, `find -xdev -ignore_readdir_race -quit` looks in the
  store's overlay for a file owned by a uid other than 0, the user's, or the current
  `/etc/subuid` range. If one is found, the timer is stopped and disabled, the first prune is
  not run, and the play fails at its end ("the podman store holds files owned outside your
  subuid range (old range); the daily prune is held off until they are re-owned (Plan 00235
  Q6)"), with the store already repaired

### Phase 5: The health report says so

- [ ] ⬜ **Task 5.1**: Tests first (`tests/helpers/host_health/test_probe.py`, a fake runner),
  then two findings in `helpers/host_health/probe.py` `collect`: free space on `/` and `/home`
  under Q3's thresholds; unused dangling images over Q3's count, or the prune timer not run
  successfully for three days. Counted with `podman images`/`ps`, never `system df`; a probe that
  cannot run is an `unchecked` finding, never silence. A failing prune service is already a
  failed-user-unit finding (F7)
- [ ] ⬜ **Task 5.2**: play-host-health-login-report.yml installs them; the panel and the login
  message show them like the other findings

### Phase 6: Defence Before Fix

- [ ] ⬜ **Task 6.1**: DBF: the rule `image-build-without-cleanup` (QA.md entry, in the patterns
  gate under `src-go/qa`): every `build`/`commit`/`bud`/`compose pull` of a container engine in
  tracked non-plan bash, YAML, Jinja or Go either runs inside `ccy_image_build_locked` or is
  followed in the same task or function by the command's `replaced`; red on the fixture and on
  B7/B8 before Tasks 3.3 and 6.2, green after; the independent searcher dispatched before the
  rule is written
- [ ] ⬜ **Task 6.2**: B8: the unifi play's `compose pull` paired with `before`/`replaced` (or, if
  `compose` re-tags across repositories the command cannot name, recorded in the rule as the
  timer's to sweep, with why)
- [ ] ⬜ **Task 6.3**: Plan 00192's defect-classes catalogue: class 17 gets this narrower rule
  as its defence for images

### Phase 7: On the host

- [ ] ⬜ **Task 7.1**: (`deploy.bash` runs play-podman.yml and `acceptance.bash` judges the timer
  parts; both queued) `deploy.bash` (changed-plays for play-podman.yml, play-claude-yolo.yml,
  play-claude-devtools.yml where installed, play-host-health-login-report.yml) and
  `acceptance.bash`, queued in meta-deploy.list; acceptance: the timer is enabled with a next
  run; its first run succeeded and logged its removals; unused dangling images older than a day
  are 0; every container that existed before still has its image; `claude-yolo:base`/`:latest`
  exist; a `--rebuild=claude` launch of one project leaves no new unused dangling image; the
  health report's new findings are present or clean, judged against Q3; free space recorded
  before and after
- [ ] ⬜ **Task 7.2**: `triage.bash --du` again a week after Task 7.1: the dangling count stays
  near zero (F3's rate absorbed)

### Phase 8: Review

- [ ] ⬜ **Task 8.1**: qa-reviewer over the branch diff; every BLOCK and FIX-BEFORE-MERGE resolved
- [ ] FOLLOW-UP (review U3): acceptance prints podman's `until=24h` count beside a `before=` count, to confirm the filter's meaning
- [ ] FOLLOW-UP (review U5): decide whether a dead-code change to a shared package needs a CCY version bump
- [ ] FOLLOW-UP: the `StoreContainer.External` comment in engine/store.go names only buildah (left so as not to bump CCY here)
- [ ] FOLLOW-UP (self-prune review U2): on docker an in-place update's old `:latest` is the new one's parent, so every daily update prints "could not remove" and the image stays until a full rebuild; decide whether `replaced` should say so once instead of as a refusal

## Questions for the owner

Each has the default this plan proceeds on unless the owner says otherwise.

- **Q1**: How long may a `claude-yolo:<project>` image go unused before the timer removes it?
  Default: **30 days** since it was created or last used by a container, and never while any
  container uses it. Cost of a wrong guess: one rebuild on that project's next launch.
- **Q2**: Dangling volumes (30 now): remove automatically, or report only? Default: **report
  only** (count and names, in the timer's log and the health report). A volume is data; a
  dangling one may be a stopped project's only copy.
- **Q3**: Health thresholds. Default: a finding when `/` or `/home` has under **10% or under 50
  GiB** free, or there are over **50** unused dangling images, or the prune timer has not run
  successfully for **3 days**.
- **Q4**: May the deploy's first timer run clear the whole backlog without a dry run being
  read first? Default: **yes**: it removes only dangling, unused, rebuildable images, and the
  triage (Task 2.1) is the dry run's list.
- **Q5**: host_health is Python, and CLAUDE.md ports "a standalone Python CLI ... when it is next
  reworked". Default: **two probes added to the existing function are not a rework**; it stays
  Python. If the owner disagrees, the port is a plan of its own and Phase 5 waits for it.
- **Q6**: Old-range files in the store (F10, Task 4.7). (A) A root step shifting files owned by
  100000+k to the current range's start+k, with the containers stopped, and setuid bits and
  file capabilities restored after the chown. (B) `podman system reset`, which destroys every
  container, image and volume, including the 9 volumes Plan 00062 found. Recommendation: **A**,
  after a read-only triage counts the affected layers and volumes. It keeps the containers'
  state and the volumes, and every later deletion works. Until then the play-podman.yml repair
  keeps the store usable, and the timer is held off (Task 4.8).

## Technical Decisions

### Decision 1: one Go command, three uses

**Context**: the self-prune, the timer and the gate's pairing all need "remove what nothing
uses", and the bash under `files/var/local/claude-yolo/` cannot grow (F9).
**Decision**: one Go command (`container-images`, a working name) with `before`, `replaced`
and `prune`; the bash calls it, the timer runs it, the gate recognises it. One rule, one home,
tested through fakes, as Plan 00192 did for vmtest's `prune-runs`.

### Decision 2: the timer lives in play-podman.yml

**Context**: the store is the engine's, shared by ccy, ccc, devtools and unifi.
**Decision**: `play-podman.yml`, which installs the engine for every audience, rather than
`play-claude-yolo.yml`, which a consumer install does not run. It imports `play-golang.yml`
for the build (the order `tasks/build-go-command.yml` requires).

### Decision 3: targeted removal after a build, a filtered prune on the timer

**Context**: `podman image prune` right after a build could take another build's in-flight
images.
**Decision**: after a build, remove only the ids that build's tags named before it; on the
timer, prune dangling images older than a day. Both leave anything a container uses.

## Success Criteria

- [ ] The tests above pass, and `./scripts/qa-all.bash` passes with the new rule
- [ ] `image-build-without-cleanup` red on its fixture and on B7/B8 before their fixes, green after
- [ ] `acceptance.bash` ACCEPTED on the host (Task 7.1), and Task 7.2's triage shows the count held
- [ ] No container lost its image; agent teams still launch (acceptance's container check)
- [ ] qa-reviewer findings resolved (Task 8.1)

## Risks & Mitigations

| Risk                                                         | Impact | Probability | Mitigation                                                                                                                                                                               |
| ------------------------------------------------------------ | ------ | ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| An image an agent-team member needs is removed               | H      | L           | `ps -a` checked first, podman's own refusal second; tests with two containers on one image                                                                                               |
| A build in flight loses its intermediate images to the timer | M      | L           | the timer's one-day age filter; nothing removed while a build lock is held                                                                                                               |
| A prune meets an old-range layer and breaks the store        | H      | M           | P6a stops at once; play-podman.yml repairs as root before and after, and holds the timer off (stopped, disabled) and fails while any such file remains (Task 4.8); Q6 is the lasting fix |
| The orphaned volume breaks the timer or the probe            | M      | M           | `system df` never called (F8); a test with that error shape                                                                                                                              |
| The lock's bash passes the 60-line limit                     | L      | M           | the Go command takes the logic; the lock's lines shrink if needed (Task 3.2)                                                                                                             |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00235-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan, research and triage: branch `plan/podman-cleanup`
