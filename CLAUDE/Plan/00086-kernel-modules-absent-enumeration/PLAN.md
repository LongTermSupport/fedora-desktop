# Plan 00086: kernel modules absent enumeration

**Status**: In Progress
**Created**: 2026-08-24
**Owner**: joseph
**Priority**: Medium

## Overview

A downstream deployment repo's live proof of `playbook-main.yml` on a real guest
found that `playbooks/imports/play-AB-dnf-upgrade.yml`'s half-installed-kernel
detection hard-fails the whole play on a host where the `kernel-modules` package
was never installed at all (a minimal/cloud-style Fedora image carrying only
`kernel-core` + `kernel-modules-core`, confirmed live via `rpm -qa` — no
`kernel-modules`, `kernel-modules-extra`, `kernel-devel`, `kernel-tools`, or
`python3-perf` present).

Root cause: the "Enumerate installed kernel-modules versions" task
(`rpm -q kernel-modules --queryformat '%{VERSION}-%{RELEASE} '`) has
`changed_when: false` but no `failed_when: false`. `rpm -q` on a genuinely
absent package returns rc=1 with stdout `package kernel-modules is not installed` (confirmed live) — this is legitimate "zero installed versions"
data for the difference computation two tasks later, not a command failure,
but the task hard-fails the play before that computation ever runs.

This is a fourth, unrelated defect surfaced downstream while proving PR #33
(`RUN_BASH_GITHUB_ACCOUNTS=none`) + PR #34 (`RUN_BASH_SUDO_PASSWORD_FILE`) +
PR #35 (headless `PATH` fix, Plan 00085) end-to-end — none of those three
mechanisms are implicated here.

## Goals

- Make the kernel-modules enumeration tolerate "package not installed" as data,
  without masking any other kind of `rpm -q` failure.
- Keep the half-installed-kernel difference computation correct when
  `kernel-modules` is legitimately absent (must treat it as zero versions, not
  as the literal "package ... is not installed" string).

## Non-Goals

- Not changing the half-install detection/cleanup logic itself (lines
  124-207) — only the enumeration task that feeds it.
- Not relaxing `failed_when` on the sibling "Enumerate installed kernel-core
  versions" task — `kernel-core` is present on every bootable system, so that
  task genuinely should hard-fail if it ever errors.

## Tasks

### Phase 1: fix

- [x] ✅ **Task 1.1**: add `failed_when: false` (with a `# FAIL-FAST-OK:`
  annotation, per this repo's fail-fast rule) to the kernel-modules
  enumeration task, plus a follow-up `assert` that the only tolerated failure
  shape is rc=1 with the "is not installed" message — any other rc/stderr
  still hard-fails the play (probe-then-fail pattern).
- [x] ✅ **Task 1.2**: fix the difference computation so a not-installed
  result is treated as zero versions, not as the literal error string split
  into words.
- [x] ✅ **Task 1.3**: `bash -n`/`ansible-playbook --syntax-check` clean;
  `./scripts/qa-all.bash` passes (537 files checked, QA passed).
- [ ] ⬜ **Task 1.4**: commit, push on a branch, open PR against `F44`.
- [ ] ⬜ **Task 1.5**: independent review (qa-reviewer stand-in).
- [ ] ⬜ **Task 1.6**: merge.

## Success Criteria

- [ ] A downstream live proof re-run on a guest lacking `kernel-modules` gets
  past this task without a hard failure, and `half_installed_kernels` comes
  out as an accurate list (empty when `kernel-core` and `kernel-modules-core`
  are in step, per Plan 00045's proof run).

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00086-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: 28837729 (shared session-wide failsafe, already running).
