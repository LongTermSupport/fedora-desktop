# Plan 00216: ccy image build lock

**Status**: In Progress
**Created**: 2026-10-22
**Owner**: joseph
**Priority**: Medium

## Overview

Two ccy sessions started at the same moment each rebuilt the container image. The owner's
requirement: only one process builds an image at a time; every other launcher waits for that
build to finish and then uses its result without building again; and stale locks must be
dealt with.

Every image build and in-place Claude Code update now runs through `ccy_image_build_locked`
(`files/var/local/claude-yolo/lib/image-build-lock.bash`): a kernel `flock` on
`$XDG_RUNTIME_DIR/ccy/image-builds/<image>.lock` (`:` and `/` written as `_`), with the
launcher's checks in `lib/image-build.bash`. The Go front answers `ccy --build-lock`
(`src-go/internal/ccylaunch/buildlock.go`), which shows the builds in progress and stops a hung
one after a `[y/N]`.

## Goals

- One build per image at a time across every ccy launch on the host and the play.
- A launch that waited checks again under the lock, with the same check the launcher makes,
  and uses the holder's build instead of building again.
- A waiter is told, at intervals, whose build it waits for, its age, and how to stop it.
- A stale record is named and ignored; nothing is ever deleted by hand.

## Non-Goals

- Serialising builds of different images: each image (each build context) has its own lock.
- A timeout on the wait (decision D3).

## Decisions

- **D1. Where the build sites are.** `build_container_with_hash` (lib/common.bash, both base
  stages), the project image's `run_project_build` and `update_claude_inplace` (`podman commit`, called by `--rebuild=claude|project`, the daily auto-update, the project fast path
  and the restart path) in the launcher, and play-claude-yolo.yml's `podman build`. Nothing in
  lib/ssh-handling.bash, lib/dockerfile-custom.bash or the Go code builds an image. All take
  the lock; the play runs its build through the library with `bash -c`.
- **D2. Lock per build context.** `claude-yolo:latest`'s lock covers `claude-yolo:base` (one
  context, built together, and both updated in place under it); each project image
  `claude-yolo:<project>` has its own. The directory is the user's runtime directory, as the
  container-name claims use (`$XDG_RUNTIME_DIR/ccy/`, else `/run/user/<uid>`): shared by every
  launch of the user, mode 0700, refused when it is a link or not the user's.
- **D3. No timeout.** A real build takes many minutes, so a waiter waits for as long as the
  build takes and says every `CCY_IMAGE_BUILD_REPORT_SECONDS` (30) on stderr whose build it
  waits for, how long it has run, and to run `ccy --build-lock` if it has hung. A timeout would
  either start a second build beside a slow first one (the defect) or fail a launch that only
  had to wait.
- **D4. What a waiter re-checks.** The base image: its labels (`validate_container_version`),
  atomic with the image tag. The project image: its hash files, which `run_project_build` now
  writes itself, inside the lock (they were written after it returned, so a waiter re-checking
  at once would rebuild). An in-place update: `--unless-changed`, the image's ID taken before
  the wait; another launch's commit changes it. An explicit `--rebuild=full` (check `false`)
  builds again after waiting: it asked for a fresh build.
- **D5. Stale data, never a stale lock.** The kernel releases the flock when its holder exits
  or dies. The holder's descriptor is closed in every program the build starts (`{fd}>&-`), so
  a child that outlives the build (podman's pause process) cannot keep it; the cost is that a
  holder killed by SIGKILL alone leaves its podman build running beside the waiter's, which
  podman's own storage locking makes safe. The record (`<pid> <epoch began> <image>`) is
  emptied before release; one whose pid has gone is said to be stale and ignored by both
  readers, and `ccy --build-lock` also refuses a pid whose process started after the build
  began (a reused pid), so it never offers to stop an unrelated process.
- **D6. `--build-lock` is the Go front's own flag.** The bash launcher has no answer for it,
  so the front answers whatever the bash's state; only `CCY_LEGACY` hands it over, and the
  bash refuses it naming the front. The stop sends SIGTERM to the holder and every process
  under it, then SIGKILL after 10 s if the lock is still held, and fails naming it after 5 s
  more.
- **D7. Bash within the limits.** The launcher and lib/common.bash are on the ratchet
  allowlist; both shrank (the three duplicated Dockerfile checks moved into
  `ccy_base_image_build`). The two new libraries are within every limit.

## Tasks

### Phase 1: The lock

- [x] ✅ **Task 1.1**: `scripts/test-ccy-image-build-lock.bash` first (red: 6 of 66), then
  `lib/image-build-lock.bash` and `lib/image-build.bash` (green). Real races: separate bash
  processes, a real flock, a slow fake podman; exactly one build or commit per race.
- [x] ✅ **Task 1.2**: Every build site under the lock (D1): the launcher, lib/common.bash, the
  play (and its library list); `test-ccy-restart-request.bash` and `test-ccy-lifecycle.bash`
  stub the lock around the update they already stub.
- [x] ✅ **Task 1.3**: `ccy --build-lock` in the Go front, `buildlock_test.go` first (F14), help
  text in both launchers, difftest goldens re-recorded (one build where the missing-image
  scenario built twice; the lock file in the run directory).
- [x] ✅ **Task 1.4**: CCY 3.99.15.0 with its changelog entry, docs/ccy.md, QA.md and the gate
  row, and the play-claude-yolo.yml entry of meta-deploy.list.

### Phase 2: Review and deploy

- [ ] ⬜ **Task 2.1**: Adversarial Opus review rounds until MERGE (subagent-reports/).
- [ ] ⬜ **Task 2.2**: The qa-reviewer agent, and its findings fixed.
- [ ] ⬜ **Task 2.3**: The owner runs meta-deploy (the play-claude-yolo.yml entry) and its check.

## Success Criteria

- [ ] Two launches started together build a missing or stale image once, and both run it.
- [ ] A waiter says whose build it waits for and how to stop it; `ccy --build-lock` names the
  holder and stops it after a yes.
- [ ] A holder killed mid-build leaves the waiter to build; its record is named stale.

## Delivery & Milestones

- <!-- delivery commit hashes -->
