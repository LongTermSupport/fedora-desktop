# Plan 00183: the prompt shows up-to-date when the tracking ref is stale

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #52.

## Overview

In a checkout not fetched for two weeks, the prompt showed the clean, up-to-date marker
(`[main|✔]`). A `git pull` a second later fast-forwarded through two weeks of commits (#52).
The prompt compares `HEAD` with the local tracking ref, which moves only on a fetch, so it
reports "not behind" truthfully against stale information.

The prompt is `bash-git-prompt`, cloned and sourced by `play-git-configure-and-tools.yml`.
Upstream it has a periodic remote fetch (`GIT_PROMPT_FETCH_REMOTE_STATUS`, with a timeout
between fetches), which the play does not set. So the first question is why that fetch did
not keep the ref fresh: it may be off, or it may be failing silently. The remote is reached
over SSH with a passphrase-protected key held in the agent, and an SSH session may have no
agent at all. The cause is unknown, so this plan triages first.

## Goals

- The ahead/behind counts in the prompt are fresh, or visibly marked stale.
- A fetch never blocks the prompt and never asks for a passphrase.
- The behaviour is set by the play, not by hand.

## Non-Goals

- Replacing `bash-git-prompt`.
- Fetching repositories nobody has `cd`'d into.

## Tasks

### Phase 1: Triage

- [ ] ⬜ **Task 1.1**: `triage.bash`: the installed `bash-git-prompt` version and its fetch
  settings as sourced, `.git/FETCH_HEAD` age in the checkout, and whether a fetch with the
  agent present, and with none, succeeds, fails or prompts.

### Phase 2: Decide and build

- [ ] ⬜ **Task 2.1**: Owner picks from the triage: turn on and tune the upstream fetch, add a
  staleness marker from `FETCH_HEAD`'s age, or both.
- [ ] ⬜ **Task 2.2**: The play sets the chosen behaviour in its `.bashrc` block; a test
  shows a stale ref is marked or refreshed and a missing agent neither blocks nor prompts.
- [ ] ⬜ **Task 2.3**: `deploy.bash`, `acceptance.bash`, an entry in `meta-deploy.bash`; docs;
  `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] A checkout behind its remote never shows the up-to-date marker without a fresh fetch.
- [ ] With no agent, the prompt draws at once and asks for nothing.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00183-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #52.
