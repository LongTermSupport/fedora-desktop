# Plan 00058: GitHub version pin updates

**Status**: In Progress
**Created**: 2026-07-06
**Owner**: joseph
**Priority**: Medium

## Overview

Several playbooks hardcode an upstream release version in a play var (e.g.
`marklessVersion`, `ouchVersion`, `rescrobbledVersion`). A drift review
(`scripts/check-pinned-versions.bash`, added alongside the `update-versions`
skill) found **every GitHub-tracked pin behind upstream**. This plan bumps each
pin to its current upstream release — including the adjacent sha256 checksum and
release-asset-filename changes each one requires — and provides a plan-local
`deploy.bash` so the affected playbooks can be re-run in one shot on the HOST.

The pins are independent projects with independent release cadences, so each is
its own task and its own commit. DisplayLink is the exception and the riskiest:
its version, evdi version, and packaging release interlock inside a
Fedora-version-specific rpm asset name and it cannot be runtime-tested without
DisplayLink dock hardware.

## Goals

- Bring every automated version pin found by `check-pinned-versions.bash` up to
  its current upstream release (where safe to do so).
- Update every adjacent sha256 checksum and asset-name pattern so downloads still
  verify and resolve.
- Provide `deploy.bash` (HOST-only) that re-runs all affected playbooks.

## Non-Goals

- Bumping `cudnn_version` (NVIDIA-hosted, not GitHub — manual, tracked as MANUAL
  by the checker; out of scope).
- Deploying/live-testing on the HOST — this is CCY-container edit-and-commit
  work; the user runs `deploy.bash` on the HOST.
- Changing install logic beyond the version/checksum/asset values (the separate
  qobuz-player asset-rename fix is tracked in Notes, not a pin bump).

## Tasks

### Phase 1: Simple pins (version only, no checksum)

- [x] ✅ **Task 1.1 — nvm** `play-nvm-install.yml`: `nvm_version` 0.40.1 → 0.40.5. Commit `410000d`.
- [x] ✅ **Task 1.2 — rescrobbled** `play-qobuz.yml`: `rescrobbledVersion` 0.8.0 → 0.10.0. Commit `bd487a8`.

### Phase 2: Checksum-pinned binaries

- [x] ✅ **Task 2.1 — markless** `play-markless.yml`: 0.9.6 → 0.9.29 + sha256 `7c79…599d`. Commit `1ea474a`.
- [x] ✅ **Task 2.2 — ouch** `play-compression-helpers.yml`: 0.6.1 → 0.8.1 (asset/layout unchanged, no checksum). Commit `d8e42a1`.
- [x] ✅ **Task 2.3 — RapidRAW** `play-photography.yml`: 1.5.5 → 1.5.8 + sha256 `6738…b17c` (rpm name unchanged). Commit `e9f273e`.
- [x] ✅ **Task 2.4 — ART** `play-photography.yml`: 1.26.3 → 1.26.6 + sha256 `a467…ae02` (AppImage name unchanged). Commit `e9f273e`.
- [ ] 🚫 **Task 2.5 — darktable** `play-darktable-ai-build.yml`: 5.4.1 → 5.6.0 **BLOCKED**.
  The build is Fedora dist-git-spec-driven; Fedora F44/rawhide still ship 5.4.1,
  so `darktable_distgit_commit` + the version-specific patch name cannot move to
  5.6.0 yet. Bumping the version/sha in isolation would break the build. Hold
  until Fedora packages 5.6.0. (New source tarball sha512 already captured for
  when it unblocks: `52fd…7189`.)

### Phase 3: Interlocking, hardware-specific

- [x] ✅ **Task 3.1 — DisplayLink/evdi** `play-displaylink.yml`: `displaylink_version`
  v6.3.0 → v6.3.0-1, `evdi_version` 1.14.16 → 1.15.0, `evdi_rpm_release` 2 → 1
  (read off the actual F44 asset; URL verified 200). Commit `588d1ae`.
  **Needs HOST verification with a real DisplayLink dock after deploy** (DKMS/evdi
  - Secure Boot MOK) — only asset reachability was checked in-container.

### Phase 4: Verify & deliver

- [x] ✅ **Task 4.1**: QA passed after each bump (`./scripts/qa-all.bash`).
- [ ] 🔄 **Task 4.2**: `scripts/check-pinned-versions.bash` — all automated pins current
  EXCEPT darktable (intentionally held) and cuDNN (MANUAL). Re-verify after HOST deploy.
- [x] ✅ **Task 4.3**: plan-local `deploy.bash` written (HOST-only, fail-fast).

## Success Criteria

- [x] Every safe automated pin bumped to current upstream (all but darktable).
- [x] All adjacent checksums/asset patterns updated so downloads verify/resolve.
- [x] `./scripts/qa-all.bash` passes.
- [ ] `deploy.bash` run on the HOST and pins verified current (user action).
- [x] `deploy.bash` present and runnable.

## Risks & Mitigations

| Risk                                            | Impact | Probability | Mitigation                                                                                          |
| ----------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------- |
| DisplayLink asset for F44 differs / absent      | H      | M           | Values derived from the actual F44 asset filename; URL verified 200; flagged for HOST hardware test |
| Asset filename pattern changed between releases | M      | M           | Each investigated + confirmed exact new asset name before editing                                   |
| Computed sha256 wrong                           | M      | L           | Recomputed from the exact release asset; a bad hash fails at HOST download time                     |
| darktable major bump breaks dist-git build      | H      | H           | Confirmed real → held back, not applied                                                             |

## Notes & Updates

### 2026-07-06

- Plan scaffolded; failsafe recovery cron `a37af71e` created (hourly, non-durable).
  Note: `mkplan.bash` refused at first because the git counter (56) lagged the
  highest plan on disk (57) — reconciled with the exact command it printed, then
  it allocated 00058. Expected fail-safe behaviour, not a bug.
- Change-sets gathered by per-playbook investigation agents (new version,
  published-or-computed checksum, verified asset URL).
- Bumped + committed: nvm, rescrobbled, markless, ouch, RapidRAW, ART, DisplayLink.
- darktable held back (Fedora still ships 5.4.1 — see Task 2.5).
- **Out-of-plan but related:** a HOST deploy of `play-qobuz.yml` hit an unrelated
  404 — `qobuz-player` upstream (SofusA/qobine) renamed its assets from
  `qobuz-player-*` to `qobine-*` and split per app; the old all-in-one binary is
  now shipped as `qobine-tui`. Fixed the asset URL + extraction in `play-qobuz.yml`.
  This is NOT a `check-pinned-versions.bash` pin (qobuz-player tracks "latest", not a
  hardcoded version). Traceability note: this fix was staged concurrently and got
  bundled into commit `e9f273e` (labelled RapidRAW/ART) via a `git add` index race
  rather than landing as its own commit — the change itself is correct and committed,
  the commit message just under-describes it.
