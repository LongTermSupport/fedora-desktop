# Plan 00106: run bash git ref headless

**Status**: In Progress
**Created**: 2026-09-08
**Owner**: joseph
**Priority**: Medium

## Overview

The headless provisioner clones `fedora-desktop` and pulls whatever branch the clone landed on,
which is the repository's default branch. A downstream consumer that provisions machines from
this repo cannot choose what it provisions from: not a release branch, not a feature branch, not
a pinned commit. Pinning `run.bash` itself by digest, as such consumers do, pins only the script;
the content is always the default branch's tip.

This plan adds `RUN_BASH_GIT_REF` with composer-style semantics: a branch name means "track this
branch's tip", a 40-hex commit means "exactly this commit, detached". Unset keeps today's
behaviour. It is the mechanism a consumer needs to let one machine follow a branch while others
stay pinned, and it fails loud when the ref does not resolve rather than provisioning from
whatever happened to be checked out.

## Goals

- `RUN_BASH_GIT_REF=<branch>` leaves the checkout on that branch at its origin tip; a repeat run
  moves it to the new tip.
- `RUN_BASH_GIT_REF=<sha>` leaves the checkout detached at that commit; a repeat run does not
  fail on the detached state.
- An unresolvable ref aborts via `hl_abort` before Ansible runs.
- Unset: identical behaviour to 1.17.0.

## Non-Goals

- The interactive (GitHub-identity) clone path. A human at a keyboard checks out what they want.
- Choosing a ref for `run.bash` itself. The caller already fetches the script at the commit it
  trusts; this variable governs the checkout the script then provisions from.

## Tasks

### Phase 1: the variable

- [x] ✅ **Task 1.1**: `hl_checkout_ref` in `run.bash`; `git pull` replaced by it when the
  variable is set; help text; `RUN_BASH_VERSION` 1.18.0; changelog entry.
- [x] ✅ **Task 1.2**: `docs/headless-provisioning.md` table row and the
  `docs/headless-server-install.md` example.
- [ ] ⬜ **Task 1.3**: proven on a real headless box by a downstream consumer: a branch run
  lands on the tip, a second run is idempotent, and a bad ref aborts.

## Success Criteria

- [ ] `./scripts/qa-all.bash` green (shellcheck on `run.bash`).
- [ ] Task 1.3's three runs observed, with the closing `info` lines quoted in the journal.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00106-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1: see JOURNAL for the commit.
