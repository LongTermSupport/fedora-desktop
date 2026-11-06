# Plan 00243: arm64 (aarch64) support

**Status**: In Progress
**Created**: 2026-11-01
**Owner**: joseph
**Priority**: High

Addresses the owner's request, passed on by the example-team coordinator: fedora-desktop must
fully support arm64. The tracking issue is linked under Delivery & Milestones.

## Overview

Today the repository assumes x86_64 in places: download URLs, RPM and binary arch pins, repo
files, Flatpaks, container images, headless browsers, static binaries, firmware, UEFI and GPU
assumptions, and the ccy install path. This plan finds every one and, for each, either makes
it work on aarch64 or records that no aarch64 build exists, with the alternative, and makes
the play say so instead of failing.

The proof is external. The example-team team owns a disposable Fedora aarch64 test bed VM
(their Plan 00182). Each cycle is a clean install, then `dnf upgrade`, then a snapshot, then
an unattended `run.bash`. They raise every failure as an issue on this repository with its
logs, and re-test each fix from the snapshot.

## Branching (the owner's direction, 2026-11-01)

This plan folder lives on F44, which claims its number. The work itself collects on the
feature branch `F44-arm`, branched from F44: each item is a worktree branched off `F44-arm`
and merged back into it. None of the work goes to F44 for now. The plan's own progress is
recorded on F44, and `F44-arm` merges F44 in to stay current. The end state is full arm64 support in the default version branch (F44 today), and
on every version branch after it. So `F44-arm` is merged into F44 once the test bed proves
it, and arm64 support is a standing requirement from then on: the architecture rule
(Task 1.2) keeps it from regressing.

## Goals

- Full arm64 support in the default version branch, and on every version branch going
  forward, kept by QA rather than by memory.
- Every x86_64 assumption is found by a sweep that can be repeated, and kept out by a QA rule
  (Defence Before Fix): a hardcoded architecture is a defect class, not a one-off.
- Each one is fixed for aarch64, or declared unavailable there, with the alternative named
  and the play skipping it loudly for that architecture.
- An unattended `run.bash` completes on the example-team aarch64 test bed with no manual step.

## Non-Goals

- The test bed itself (example-team Plan 00182).
- 32-bit ARM, or other architectures.

## Tasks

### Phase 1: sweep and defence

- [x] ✅ **Task 1.1**: The sweep. Every architecture assumption in playbooks, tasks, vars,
  files, scripts, src-go, helpers, the ccy image and the docs is listed in
  `SWEEP.md` with what it is, whether an aarch64 build exists, and the fix or alternative.
  `SWEEP.md` is on `F44-arm`.
- [x] ✅ **Task 1.2**: The detector rule for a hardcoded architecture (semgrep, or a Go
  analyzer for src-go), proved red on the sweep's instances and made blocking.
  `arch-pinned-artefact` on `F44-arm`; its allowlist is empty, so it blocks every instance.

### Phase 2: fixes

- [x] ✅ **Task 2.1**: Fix every instance from `SWEEP.md`, in batches by area, tests first.
  Add a `changed-plays` entry to the deploy queue for the x86_64 host, which must keep
  working. All four batches are on `F44-arm` at 9a05de5e3 (full QA green). The
  `changed-plays` entry goes in with the merge into `F44`, since the host runs `F44`.
- [x] ✅ **Task 2.2**: A CI lane on real aarch64 before the test bed: `.github/workflows/arm64-install.yml`
  runs the headless server-profile `run.bash` on GitHub's `ubuntu-24.04-arm` runner, in a
  Fedora 44 container with systemd as PID 1. Green on `F44-arm` with no architecture fault;
  every failure it met was the container's and was fixed in the workflow, not the plays. The
  desktop profile, optional plays, a reboot and a real boot loader stay for Task 3.1.
- [x] ✅ **Task 2.3**: The playbook, ccy image, ISO, VM lab and CI lane fixes ported from
  `F44-arm` onto `F44` as x86_64-neutral changes, so an aarch64 host following the `F44` tip
  gets them. Pins `F44-arm` added that change x86_64 (yq 4.54.1, hifi-rs 0.3.8, pinned
  shellcheck and golangci-lint digests) and the `arch-pinned-artefact` gate stay on `F44-arm`.

### Phase 3: the test loop with example-team

- [ ] ⬜ **Task 3.1**: Each issue the example-team test bed raises is fixed here and handed back
  for a re-test from the snapshot.

## Success Criteria

- [ ] An unattended `run.bash` completes on the clean aarch64 test bed, proven from the
  snapshot by example-team.
- [ ] The same run still passes on the x86_64 desktop host.
- [ ] The architecture rule blocks in QA (it does on `F44-arm`; ticked when merged into `F44`).

## Delivery & Milestones

- Plan created.
