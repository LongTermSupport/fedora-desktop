# Plan 00087: gitleaks generic key false positive

**Status**: In Progress
**Created**: 2026-08-24
**Owner**: joseph
**Priority**: Medium

## Overview

`F44`'s `gitleaks secret scan` CI check has been failing for a while (confirmed: the
same failure appears on F44's own tip, `34fefcb` and `dac4f7c`, unrelated to any of
this session's other in-flight PRs) — one finding, `generic-api-key`, in
`CLAUDE/Plan/Completed/00070-lightweight-agent-browser-engine/research/scan-alt-engines.md:222`.
This is a real gitleaks CI job downgraded to background noise: every PR shows a red
`mergeStateStatus: UNSTABLE` for a reason unrelated to its own diff, which trains
reviewers to ignore CI red state — the exact failure mode this repo's own anti-pattern
doctrine warns about (a check that reports "problem" for a reason nobody acts on).

Root cause, confirmed by running the CI job's exact gitleaks version (8.30.1 — the repo
had 8.21.2 available locally, which does NOT flag this; the ruleset changed between
versions) against the flagged file: the string `Medium/byteiota` (a citation to two
external blog names joined by `/`) has just enough entropy and shape to trip the
`generic-api-key` rule. Not a credential — a false positive on a slash-joined mixed-case
proper-noun pair.

## Goals

- Make `gitleaks secret scan` pass clean on `F44` again.
- Fix at the ROOT — rephrase the flagged text so it no longer forms a
  high-entropy-looking token — rather than adding another entry to
  `.gitleaks.toml`'s allowlist. This repo's own anti-pattern catalogue flags "an
  allowlist that grows" as a smell; a one-word rephrase costs nothing and needs no
  allowlist maintenance at all.

## Non-Goals

- Not touching `.gitleaks.toml`'s existing `containerwatch` allowlist entry — that one
  guards a genuine reusable test fixture pattern, not a one-off prose citation.
- Not auditing the rest of the repo for other pre-8.30.1-only findings beyond this one —
  confirmed via a full-tree scan (scoped to git-tracked files only, matching CI's clean
  checkout) that this was the CI job's only reported leak.

## Tasks

### Phase 1: fix

- [x] ✅ **Task 1.1**: downloaded gitleaks 8.30.1 (the exact CI version) locally,
  reproduced the finding (`Medium/byteiota`, `generic-api-key`, line 222), confirmed
  8.21.2 does NOT catch it (ruleset drift between versions, not a repo regression).
- [x] ✅ **Task 1.2**: rephrased `Medium/byteiota` → `Medium (Byteiota)` in
  `scan-alt-engines.md`; re-ran gitleaks 8.30.1 against the file — clean.
- [x] ✅ **Task 1.3**: commit, push, open PR against `F44` (PR #37). All three CI
  checks pass, including `gitleaks secret scan` — confirmed green, not just locally.
- [x] ✅ **Task 1.4**: independent review (peer review agent) — verdict **PASS WITH
  NITS**; confirmed the fix, the rephrase-over-allowlist call, and CI green
  independently. One nit: `CLAUDE/Plan/README.md` had no index row for this plan —
  added.
- [ ] ⬜ **Task 1.5**: merge; confirm `gitleaks secret scan` goes green on `F44`.

## Success Criteria

- [ ] `gitleaks dir . --redact --no-banner --verbose` (gitleaks 8.30.1) reports zero
  leaks against the merged `F44` tree.
- [ ] The `gitleaks secret scan` GitHub Actions check on `F44` shows green on the next
  run after merge.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00087-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: 28837729 (shared session-wide failsafe, already running).
