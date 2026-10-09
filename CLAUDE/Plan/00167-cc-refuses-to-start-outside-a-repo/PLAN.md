# Plan 00167: cc refuses to start outside a repo

**Status**: Not Started
**Created**: 2026-10-09
**Owner**: joseph
**Priority**: Medium

## Overview

`cc` (`files/var/local/claude-code/cc`, deployed by `play-claude-yolo.yml` and aliased in
`~/.bashrc`) starts Claude Code in whatever directory it is typed in. Typed in `~`, it
starts a session that loads no project's `.claude/settings.json`. That means no hooks
daemon, no status line, and an agent with no idea it is working in IaC territory. That is
exactly what happened in the session whose handover created this plan: it changed nothing
in this repo, but it worked without any of the project's safeguards and without knowing
they were missing.

This plan makes `cc` check that it is inside a git working tree before it does anything
else (before the tmux re-exec and the token chooser), and stop with a clear message if it
is not.

## Goals

- `cc` run outside a git working tree exits non-zero with a message naming the directory
  and the two ways forward: `cd` into a project, or run `claude` directly.
- It refuses before it creates a tmux session or shows the token chooser.
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
  is planned for it unless it would hang or be reported as success.

### Phase 2: Implement

- [ ] ⬜ **Task 2.1**: In `cc`, after `umask 077` and **before** the interactive-terminal check,
  run `git rev-parse --show-toplevel` and refuse with `print_error`-style output on stderr
  when it fails. Before the terminal check, so the refusal is testable without a pty and
  never reaches tmux. Here `git rev-parse` asking about the cwd is the point, unlike in plan
  scripts. `git` absent is its own error, not a refusal for the wrong reason.
- [ ] ⬜ **Task 2.2**: Apply Decision 2 for subdirectories, if Task 1.1 calls for anything.
- [ ] ⬜ **Task 2.3**: Document the behaviour in `docs/ccy.md` (the `cc` paragraph near line
  198\) and add a `cc` entry to `docs/ccy-changelog.md`. No `CCY_VERSION` bump: `cc` is not
  under `files/var/local/claude-yolo/`.
- [ ] ⬜ **Task 2.4**: Run QA: `./scripts/qa-all.bash`; fix any findings.

### Phase 3: Deploy and accept

- [ ] ⬜ **Task 3.1**: `deploy.bash` runs `play-claude-yolo.yml`.
- [ ] ⬜ **Task 3.2**: `acceptance.bash` runs the deployed `/var/local/claude-code/cc` with
  stdin not a terminal: from a fresh `mktemp -d` it must exit 1 with the repository
  refusal, and from the repository root it must get past that check and stop at the
  terminal check instead. A tmux session list taken before and after shows no new `cc-`
  session. Prints `COVERAGE: n of m`.
- [ ] ⬜ **Task 3.3**: Add the plan to `meta-deploy.bash`; the owner runs it, then types `cc`
  once in `~` and once in a project.

### Phase 4: Review and close

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan's diff; resolve every BLOCK
  and FIX-BEFORE-MERGE finding.
- [ ] ⬜ **Task 4.2**: Remove the plan from `meta-deploy.bash`, mark it Complete, and move it
  to `Completed/`.

## Dependencies

- Related: Plan 00048 (`cc` token-source parity), Plan 00135 (session restore), Completed
  Plan 00157 (`cc` launcher broken).

## Technical Decisions

### Decision 1: Hard refusal, no confirm prompt, no override

**Context**: The handover asked: hard fail, or warn and confirm? Escape hatch or not?
**Options considered**: (A) refuse and exit 1. (B) warn and ask "start anyway?". (C) refuse
unless a flag or variable is set.
**Decision**: A. A prompt gets answered "y" by reflex, which defeats the point. A flag is
YAGNI next to `claude`, which already starts a session anywhere. The refusal is not a
recoverable input mistake in the sense of `InteractiveScripts.md`: `cc` cannot change its
caller's directory, so there is nothing to re-prompt for.
**Date**: 2026-10-09

### Decision 2: Subdirectories of a repository

To be settled by Task 1.1.

## Success Criteria

- [ ] `cc` in a directory outside any git working tree exits 1 with the refusal, and starts
  no tmux session.
- [ ] `cc` at a repository root starts as before (owner check).
- [ ] Subdirectory behaviour matches Decision 2.
- [ ] QA passes (`./scripts/qa-all.bash`) and the `qa-reviewer` findings are resolved.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00167-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan written from the handover
