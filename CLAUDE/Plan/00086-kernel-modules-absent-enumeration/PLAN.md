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
  `kernel-modules` is legitimately absent — and correctly recognise that a
  host where `kernel-modules` is absent altogether is not "half-installed" at
  all (it is complete for its own package set), so the removal task must
  never fire on it.
- Never let the removal task target the currently-running kernel, regardless
  of what the detection computes.

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
- [x] ✅ **Task 1.4**: commit, push on a branch, open PR against `F44` (PR #36).
- [x] ✅ **Task 1.5**: independent review (peer review agent) — returned
  **BLOCK** on two findings, both addressed in Task 1.7 below.
- [x] ✅ **Task 1.7**: address the two BLOCK findings —
  1. The original fix diffed `kernel-core` only against `kernel-modules`
     (unchanged from before this plan). On a host where `kernel-modules` is
     genuinely absent altogether (the exact target host class), that made
     the difference computation return every installed `kernel-core`
     version, which then fed the unconditional `dnf state: absent` removal
     task — a destructive regression on precisely the host this plan set out
     to fix. Corrected: the half-install heuristic now only runs when
     `kernel-modules` exists on the host at all (`km_versions.rc == 0`); a
     host where it is genuinely not installed is treated as complete for its
     own package set, not half-installed, and `half_installed_kernels` is
     unconditionally `[]`.
  2. Added an unconditional exclusion of the running kernel's version from
     the removal candidate list, as defense-in-depth against ever targeting
     the booted kernel.
  3. The JOURNAL entry for this plan had committed a real private VM
     hostname from the downstream consumer estate — redacted in place to a
     generic description, per this repo's own established precedent for
     this exact leak class (`676a7d74`/`cae9a0ca`, "Redact internal infra
     hostnames from this public repo"): a content-only forward fix, not a
     history rewrite.
- [x] ✅ **Task 1.8**: re-review after the Task 1.7 fixes — a second review
  agent found the fix for finding 1 solid, but caught that the finding-2 fix
  commit had reintroduced the same hostname in its own description (a
  re-leak). Fixed in a follow-up commit before the re-review reported back;
  the reviewer independently re-verified the corrected tip and returned
  **PASS**.
- [x] ✅ **Task 1.6**: merged (PR #36, merge commit `e8686ec`). CI green
  (`qa-all.bash`, helper unit tests, gitleaks — all pass) at merge time.
- [ ] ⬜ **Task 1.9**: re-pin lts-infra's live-proof harness at this merged
  commit and re-run it — the first run that can actually exercise this fix
  (the harness's pin only ever covered `run.bash` itself, never the
  self-cloned repo's playbook content — see lts-infra Plan 00045's journal).

## Success Criteria

- [ ] A downstream live proof re-run on a guest lacking `kernel-modules` gets
  past this task without a hard failure, and `half_installed_kernels` comes
  out `[]` — not because `kernel-core` and `kernel-modules-core` happen to be
  in step, but because `kernel-modules` is genuinely absent from this host's
  package set, so the half-install heuristic does not apply to it at all.
  **Not yet run** — pending Task 1.9.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00086-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Merged: `e8686ec` (PR #36).
- Recovery cron: 28837729 (shared session-wide failsafe, already running).
