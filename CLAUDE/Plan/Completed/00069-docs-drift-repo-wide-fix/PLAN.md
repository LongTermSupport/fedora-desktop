# Plan 00069: Repo-Wide Documentation Drift Fix

**Status**: Complete
**Created**: 2026-08-07
**Completed**: 2026-08-07
**Owner**: joseph
**Priority**: High

## Overview

Plan 00068 documented CCY and reconnected the front page to the docs tree. In doing so it
surfaced a factual defect that had nothing to do with CCY: the root `README.md` described
Docker as an **optional, rootless** add-on when it is in fact **core** (imported by
`playbook-main.yml`) and deliberately **rootful**. That is the same drift Plan 00049
corrected across the user docs — the root README was missed at the time.

One instance found by accident implies others not yet found. This plan audits **every**
user-facing document against the real source (playbooks, `files/`, vars) and fixes every
factual defect found: commands and flags that do not exist, paths that have moved,
core/optional misclassifications, wrong technical characterisations, and instructions
that would fail if followed.

## Goals

- Audit all documents under `docs/` plus the root `README.md` for factual drift.
- Fix every confirmed defect, with each fix traced to source evidence.
- Leave no user-facing doc making a claim the source contradicts.

## Non-Goals

- **No code changes.** Documentation only.
- Not a style, tone or restructuring pass — Plan 00068 already did the structural work.
  This plan changes text only where it is *wrong*.
- Not adding missing content; only correcting incorrect content.

## Tasks

### Phase 1: Audit

- [x] ✅ **Task 1.1**: Audit every doc against source (9 parallel verifiers, grouped by
  subject area) — 32 findings returned; 2 groups (headless, github) came back clean

### Phase 2: Fix

- [x] ✅ **Task 2.1**: Independently re-verify each reported finding against source
- [x] ✅ **Task 2.2**: Apply every confirmed fix — 32 reported + 4 found during
  verification that the audit missed
- [x] ✅ **Task 2.3**: Re-check all cross-reference links and anchors

### Phase 3: Verify

- [x] ✅ **Task 3.1**: Run QA: `./scripts/qa-all.bash` — passed (417 files)
- [x] ✅ **Task 3.2**: Confirm no doc is orphaned and no link is broken — scripted check
  across every `docs/**/*.md` plus the root `README.md`

## Findings Summary

| Category                           | Examples                                                                                                                   |
| ---------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Core listed as optional            | Python, VS Code, Firefox, Slack, Kitty, WireGuard, Docker, GitHub multi-account, gsettings                                 |
| Feature documented but absent      | `disable_tracker` in fast-file-manager (no such task/var anywhere in the repo)                                             |
| Variable documented but absent     | `claude_stt_model` (model is a GSettings key / CLI flag, not an Ansible var)                                               |
| Command that would fail            | `docker-compose` (only `docker-compose-plugin` is installed → `docker compose`)                                            |
| Retired feature advertised         | Playwright Distrobox — `play-claude-yolo.yml` actively *removes* that container                                            |
| Banned Ansible pattern in examples | `{{ inventory_dir }}/../../` in 4 places (banned by `CLAUDE/AnsibleStyle.md`)                                              |
| Wrong UI instructions              | speech-to-text menu items and panel-icon states that do not match `extension.js`                                           |
| Stale version pins                 | "Node.js 20" ×4 (host tracks NVM `lts/*`; CCY image is `node:lts-slim`)                                                    |
| Wrong config values                | `fastestmirror`/`deltarpm` (never set), PipeWire 192 kHz (actual default 48 kHz), LXC SSH `Host lxc-*` (actual `10.0.*.*`) |
| Misattributed actions              | ripgrep and LXC SSH config credited to `play-git-configure-and-tools.yml`                                                  |
| Broken anchors                     | `#playwright-distrobox-automated`, `CLAUDE.md#-public-repository-warning`                                                  |

## Success Criteria

- [x] Every doc under `docs/` and the root `README.md` audited against source
- [x] Every confirmed factual defect fixed
- [x] Every fix backed by source evidence, not inference
- [x] All internal links and anchors resolve
- [x] QA passes (`./scripts/qa-all.bash`)

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00069-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: `e85368fc` (carried over from Plan 00068, same session)
- Precursor: Plan 00068 (`d52fad1`) found the Docker core/rootful drift that motivated this
