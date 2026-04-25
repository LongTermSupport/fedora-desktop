# Plan 034: Track Config GitHub Account in localhost.yml

**Status**: Not Started
**Created**: 2026-04-23
**Owner**: joseph
**Priority**: Medium
**Estimated Effort**: 2-3 hours

## Overview

`run.bash` currently resolves "which GitHub account owns my
`fedora-desktop-config` repo" by calling `gh api user --jq .login`. That
returns whatever gh account happens to be active at the moment, which is
volatile — `gh auth switch` flips it. The config repo lookup and the
`pull-projects --account` call both depend on this, so they silently
target the wrong account whenever the active gh default drifts.

The fix is to make `localhost.yml` declare the config-owning account
authoritatively, and treat `gh api user` as a first-install bootstrap
only.

## Goals

- `localhost.yml` carries a single source of truth for the config repo account
- `run.bash` prefers the declared value over `gh api user`
- First-install flow writes the chosen account back into the new
  `localhost.yml` so subsequent runs are stable
- `pull-projects` path uses the same value

## Non-Goals

- Not changing how per-account SSH keys are resolved (that's already driven
  by `github_accounts` in the vault)
- Not adding multi-config-repo support
- Not touching the vault encryption format

## Context & Background

Relevant current code in `run.bash`:

- line 544 — `primary_gh_username="$(gh api user --jq '.login')"`
- line 636 — `config_repo="${primary_gh_username}/fedora-desktop-config"`
- line 954 — `"$_pull_projects_script" --account "$primary_gh_username"`

Today's incident: user had the LTS account as their config host, but the
active gh default was a different account (`ballidev`). run.bash looked
for `ballidev/fedora-desktop-config`, didn't find it, and offered to
configure fresh — which would have wiped the existing vault.

## Tasks

### Phase 1: Design

- [ ] ⬜ Pick the variable name (proposed: `config_github_account`)
- [ ] ⬜ Decide fallback order:
  1\. value from `localhost.yml` if present
  2\. value from `gh api user` (first-install bootstrap only)
  3\. explicit prompt if the yml exists but lacks the key (migration path)
- [ ] ⬜ Decide how first-install writes the value back (plain unencrypted
  line, written before any vault content — this is not secret)

### Phase 2: Implementation

- [ ] ⬜ Add variable read in `run.bash` before line 544
- [ ] ⬜ Change line 636 to use the resolved value (same var name for least churn)
- [ ] ⬜ Change line 954 call site
- [ ] ⬜ On first-install path, append `config_github_account: <value>` to
  the new `localhost.yml` before any vault entries
- [ ] ⬜ On migration path (existing yml without the key), prompt once and
  append to yml so next run is clean

### Phase 3: Docs & QA

- [ ] ⬜ Update `docs/post-upgrade.md` — note that F42→F43 migrations
  should set `config_github_account` explicitly
- [ ] ⬜ Update any `localhost.yml` example in docs
- [ ] ⬜ `./scripts/qa-all.bash` passes
- [ ] ⬜ Bump `RUN_BASH_VERSION` with a descriptive comment

### Phase 4: Verification

- [ ] ⬜ Fresh install flow still works (no existing yml)
- [ ] ⬜ Migration flow: existing yml without the key → prompts once, writes back
- [ ] ⬜ Stable flow: existing yml with the key → no prompt, uses that account
- [ ] ⬜ Active gh account differs from declared account → still picks declared

## Dependencies

- None — self-contained change to `run.bash` and doc

## Technical Decisions

### Decision 1: Unencrypted variable, not vaulted

**Context**: Should `config_github_account` be encrypted?
**Decision**: No. It's a GitHub org/username and the repo name is
discoverable from any push; it's not secret. Keep it unencrypted so the
bootstrap flow can write it without the vault password being available.
**Date**: 2026-04-23

### Decision 2: Single variable, not per-function

**Context**: Should we have separate `config_repo_account` and
`projects_account`?
**Decision**: Single variable for now. In practice both are the same
account. If that changes, split later (YAGNI).
**Date**: 2026-04-23

## Success Criteria

- [ ] `run.bash` never again asks "configure fresh?" because of an
  unrelated `gh auth switch`
- [ ] Active gh account can change freely without affecting which
  config repo is looked up
- [ ] Clean migration path for existing F42→F43 installs
- [ ] QA passes, new run.bash version bumped

## Risks & Mitigations

| Risk                                                                                | Impact | Probability | Mitigation                                          |
| ----------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------- |
| Migration prompt annoys users with existing yml                                     | Low    | High        | One-time, writes back so it's not asked again       |
| Users hand-edit the value to wrong account                                          | Low    | Low         | Error from `gh api repos/.../contents/...` is clear |
| First-install bootstrap writes wrong account if gh default is wrong at install time | Medium | Medium      | Show value and confirm before writing               |

## Notes & Updates

### 2026-04-23

- Plan created after F42→F43 migration surfaced the issue. User ran
  `./run.bash` while the active gh default was `ballidev`, not the LTS
  account that owns the config repo. Existing workaround: `gh auth switch` before running. Proper fix: this plan.
