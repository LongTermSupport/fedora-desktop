# Plan 00243: arm64 (aarch64) support

**Status**: In Progress
**Created**: 2026-11-01
**Owner**: joseph
**Priority**: High

Addresses the owner's request, passed on by the lts-infra coordinator: fedora-desktop must
fully support arm64. The tracking issue is linked under Delivery & Milestones.

## Overview

Today the repository assumes x86_64 in places: download URLs, RPM and binary arch pins, repo
files, Flatpaks, container images, headless browsers, static binaries, firmware, UEFI and GPU
assumptions, and the ccy install path. This plan finds every one and, for each, either makes
it work on aarch64 or records that no aarch64 build exists, with the alternative, and makes
the play say so instead of failing.

The proof is external. The lts-infra team owns a disposable Fedora aarch64 test bed VM
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
- An unattended `run.bash` completes on the lts-infra aarch64 test bed with no manual step.

## Non-Goals

- The test bed itself (lts-infra Plan 00182).
- 32-bit ARM, or other architectures.

## Tasks

### Phase 1: sweep and defence

- [x] ✅ **Task 1.1**: The sweep. Every architecture assumption in playbooks, tasks, vars,
  files, scripts, src-go, helpers, the ccy image and the docs is listed in
  [SWEEP.md](SWEEP.md) with what it is, whether an aarch64 build exists, and the fix or alternative.
- [ ] 🔄 **Task 1.2**: The detector rule for a hardcoded architecture (semgrep, or a Go
  analyzer for src-go), proved red on the sweep's instances and made blocking. The rule
  `arch-pinned-artefact` (`.semgrep/arch-pin.yml`, gate `scripts/qa-arch-pin.bash`) is in
  qa-all, proved red on both ccy Dockerfiles; the remaining instances are the ratchet
  `scripts/arch-pin.allowlist`, a baseline for the owner to accept. Plain blocking once the
  allowlist is empty.
  - [x] ✅ **Task 1.2a**: The qa-reviewer's fixes to the gate: every QA script executable
    (held by a Go test), the ratchet comparing findings and the merge-base, qa-changed
    routing, the pin docs, the narrowed ARM exclusion, the yq pin (ccy 3.104.17).

### Phase 2: fixes

- [ ] ⬜ **Task 2.1**: Fix every instance from `SWEEP.md`, in batches by area, tests first.
  Add a `changed-plays` entry to the deploy queue for the x86_64 host, which must keep
  working.
  - [x] ✅ **Task 2.1a**: The two ccy images (SWEEP.md section A; issues 119, 120, 121).
    Edited and gated; the arm64 build itself is proved by lts-infra (Task 3.1).
  - [ ] 🔄 **Task 2.1b**: The ratchet's files (SWEEP.md section B) and the implicit ones
    (section C).
    - [x] ✅ **Task 2.1b-1**: What `playbook-main.yml` reaches: yq, markless, host shellcheck
      and golangci-lint by an arch-keyed map with a digest each; Chrome repo, kmod and kernel
      NEVRAs by the running arch; JetBrains `linuxARM64`; Slack and intel-media-driver,
      which have no aarch64 build, skipped by name there. The rule now accepts the
      non-deprecated `ansible_facts['architecture']` as the selection.
    - [x] ✅ **Task 2.1b-2**: The optional plays: ouch, Ookla, RapidRAW, ART, hifi-rs (now
      pinned with a digest) and DisplayLink by arch, each with a digest per arch; CUDA,
      rescrobbled, VirtualBox and IPU6, which have no aarch64 build, skipped by name there
      (CUDA fails if asked for by name); Element needs nothing.

### Phase 3: the test loop with lts-infra

- [ ] ⬜ **Task 3.1**: Each issue the lts-infra test bed raises is fixed here and handed back
  for a re-test from the snapshot.

## Success Criteria

- [ ] An unattended `run.bash` completes on the clean aarch64 test bed, proven from the
  snapshot by lts-infra.
- [ ] The same run still passes on the x86_64 desktop host.
- [ ] The architecture rule blocks in QA.

## Delivery & Milestones

- Plan created.
