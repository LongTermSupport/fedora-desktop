# Plan 00167: cc refuses to start outside a repo

**Status**: Not Started
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: Medium
**Issue**: [#90](https://github.com/LongTermSupport/fedora-desktop/issues/90)

## Overview

`cc` (`files/var/local/claude-code/cc`, deployed by `play-claude-yolo.yml` and aliased in
`~/.bashrc`) starts Claude Code in whatever directory it is typed in. Typed in `~`, it
starts a session that loads no project's `.claude/settings.json`. That means no hooks
daemon, no status line, and an agent with no idea it is working in IaC territory. That is
exactly what happened in the session whose handover created this plan: it changed nothing
in this repo, but it worked without any of the project's safeguards and without knowing
they were missing.

This plan makes `cc` check that it is inside a git working tree before it does anything
else (before the tmux re-exec and the token chooser). If it is not, `cc` offers to switch to
this machine's fedora-desktop checkout, which is almost always what was meant, and carries
on from there. If the offer is declined, or there is no terminal to ask on, it stops with a
clear message.

## Goals

- `cc` run outside a git working tree, in a terminal, names the directory and offers to
  switch to the fedora-desktop checkout. Accepting starts the session there as if `cc` had
  been typed in it. Declining exits non-zero with the ways forward: `cd` into a project, or
  run `claude` directly.
- With no terminal, `cc` outside a repository exits non-zero with the same message and
  asks nothing.
- The check happens before `cc` creates a tmux session or shows the token chooser.
- `cc` inside a repository behaves exactly as it does now.

## Non-Goals

- No escape-hatch flag or environment variable. `claude` already is the escape hatch, and
  `cc`'s other refusals already point to it (YAGNI).
- `ccy`. It mounts the directory it is started in and has its own launch checks.
- Requiring `.claude/` in the repository. Plenty of repositories worth a session have none.

## Tasks

### Phase 1: Settle the open questions

- [ ] ⬜ **Task 1.1**: Establish from Claude Code's documentation whether a session started
  in a **subdirectory** of a repository loads that repository's `.claude/settings.json`.
  If it does, a subdirectory needs nothing more. If it does not, decide between `cd`-ing to
  the repository root and refusing. Record the answer and its source as Decision 2.
- [ ] ⬜ **Task 1.2**: Establish what a reboot restore (`ccy-sessions restore`, Plan 00135)
  does when a recorded `cc` session's directory is no longer a repository, and how
  `verify-restore` reports that `cc` exiting. Record it in the journal; no behaviour change
  is planned for it unless it would hang or be reported as success. A restore has no
  terminal answer to give, so it must take the no-terminal path, never the offer.
- [ ] ⬜ **Task 1.3**: Decide how `cc` finds the fedora-desktop checkout. `cc` is copied, not
  templated, so the path has to reach it from the play, for example as a small file the
  play writes beside `cc` from its `root_dir`. Nothing install-specific is hardcoded in the
  repo. Record it as Decision 3.

### Phase 2: Implement

- [ ] ⬜ **Task 2.1**: In `cc`, after `umask 077` and **before** the interactive-terminal check,
  run `git rev-parse --show-toplevel`. Here `git rev-parse` asking about the cwd is the
  point, unlike in plan scripts. `git` absent is its own error, not a refusal for the wrong
  reason. Outside a repository with no terminal: refuse on stderr and exit 1. With a
  terminal: name the directory and offer the fedora-desktop checkout, `[Y/n]`. Validate the
  answer strictly, re-prompt a bounded number of times on anything else
  (`InteractiveScripts.md`), `cd` there on yes, and refuse on no. The `cd` happens before the
  tmux re-exec, so the session name and the restore record both use the checkout.
- [ ] ⬜ **Task 2.2**: Apply Decision 2 for subdirectories, if Task 1.1 calls for anything.
- [ ] ⬜ **Task 2.3**: Document the behaviour in `docs/ccy.md` (the `cc` paragraph near line
  198\) and add a `cc` entry to `docs/ccy-changelog.md`. No `CCY_VERSION` bump: `cc` is not
  under `files/var/local/claude-yolo/`.
- [ ] ⬜ **Task 2.4**: Run QA: `./scripts/qa-all.bash`; fix any findings.

### Phase 3: Deploy and accept

- [ ] ⬜ **Task 3.1**: `deploy.bash` runs `play-claude-yolo.yml`.
- [ ] ⬜ **Task 3.2**: `acceptance.bash` runs the deployed `/var/local/claude-code/cc` with
  stdin not a terminal: from a fresh `mktemp -d` it must exit 1 with the repository
  refusal and ask nothing, and from the repository root it must get past that check and
  stop at the terminal check instead. A tmux session list taken before and after shows no
  new `cc-` session. Prints `COVERAGE: n of m`. The offer itself needs a terminal and is
  listed as NOT ESTABLISHABLE for the owner's check.
- [ ] ⬜ **Task 3.3**: Add the plan to `meta-deploy.bash`. The owner runs it, then types `cc`
  in `~` (accepts the offer once and declines it once) and once in a project.

### Phase 4: Review and close

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan's diff; resolve every BLOCK
  and FIX-BEFORE-MERGE finding.
- [ ] ⬜ **Task 4.2**: Remove the plan from `meta-deploy.bash`, mark it Complete, and move it
  to `Completed/`.

## Dependencies

- Related: Plan 00048 (`cc` token-source parity), Plan 00135 (session restore), Completed
  Plan 00157 (`cc` launcher broken).

## Technical Decisions

### Decision 1: Offer the fedora-desktop checkout; otherwise refuse, with no override

**Context**: The handover asked: hard fail, or warn and confirm? Escape hatch or not?
**Options considered**: (A) refuse and exit 1. (B) warn and ask "start anyway?". (C) refuse
unless a flag or variable is set. (D) offer to switch to the fedora-desktop checkout and
refuse if that is declined.
**Decision**: D, at the owner's request. They want `cc` in line with `ccy`, and starting
`cc` outside a repo almost always meant starting it in this one. Every answer to the offer
ends in a project session or no session, so unlike (B) a reflexive "y" is harmless. There is
no flag: plain `claude` already starts a session anywhere (YAGNI). `cc` cannot change its
caller's directory, but it can `cd` in its own process before it starts `claude`, which is
all the offer needs.
**Date**: 2026-10-09

### Decision 2: Subdirectories of a repository

To be settled by Task 1.1.

### Decision 3: How `cc` finds the checkout

To be settled by Task 1.3.

## Success Criteria

- [ ] `cc` outside any git working tree with no terminal exits 1 with the refusal and starts
  no tmux session.
- [ ] `cc` outside any git working tree in a terminal offers the fedora-desktop checkout;
  yes starts the session there, no exits 1 (owner check).
- [ ] `cc` at a repository root starts as before (owner check).
- [ ] Subdirectory behaviour matches Decision 2.
- [ ] QA passes (`./scripts/qa-all.bash`) and the `qa-reviewer` findings are resolved.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00167-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan written from the handover
