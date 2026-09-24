# Plan 00138: Bash history that survives many terminals, and a better Ctrl+R

**Status**: In Progress
**Created**: 2026-09-24
**Owner**: joseph
**Priority**: Medium

## Overview

Bash history on this desktop "behaves weirdly", and triage confirms why: history is
written to `~/.bash_history` only when a shell exits. With dozens of long-lived terminals
and tmux panes open, most recent work is in memory only — invisible to Ctrl+R in any other
shell, lost if a shell dies uncleanly, and appended to the file in exit order rather than
the order it happened. There are no timestamps, a third of the file is beyond Ctrl+R's
reach, and `ps1-prompt` overwrites part of the `PROMPT_COMMAND` array, so anything added
there naively would be lost in exactly the tmux shells that need it.

Research is complete and the owner has decided (see [PROPOSAL.md](PROPOSAL.md)):

- fix bash history itself for the user and root (P1–P5);
- a **repo-owned recorder** (R2), built only from bash builtins, that notes each command's
  directory and exit status in a `0600` file. Atuin is rejected on security;
- a **repo-owned ranker** on Ctrl+R that searches every terminal's history but ranks this
  directory's commands, then this git repo's, above the rest. fzf only draws the list.

Supporting documents:

- Current-state review with evidence: [RESEARCH-current-config.md](RESEARCH-current-config.md)
- Optimal settings and Ctrl+R tool comparison: [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md)
- Directory/repo-weighted ranking and the security of each option: [RESEARCH-ranking-and-security.md](RESEARCH-ranking-and-security.md)
- Proposal and the recorded decisions: [PROPOSAL.md](PROPOSAL.md)

## Goals

- Every command is in the history file within one prompt of running, across every
  terminal, tmux pane and login/non-login shell
- Every history entry carries a timestamp
- Ctrl+R searches the whole history of every terminal, fuzzily, as a list, without
  duplicates, ranking this directory's and this repo's commands first — even before a
  character is typed
- No new security exposure: nothing leaves the machine, no command line appears in another
  process's argv, the leading-space escape hatch keeps working
- `PROMPT_COMMAND` holds each hook exactly once in every kind of shell
- A stray unconfigured shell can no longer truncate the history file

## Non-Goals

- zsh/fish history
- Syncing history between machines
- Rewriting or de-duplicating the existing history file in place
- Atuin, McFly or any other third-party history tool

## Tasks

### Phase 1: Research (read-only)

- [x] ✅ **Task 1.1**: Triage the live configuration — [`triage.bash`](triage.bash)
- [x] ✅ **Task 1.2**: Review the configuration — [RESEARCH-current-config.md](RESEARCH-current-config.md)
- [x] ✅ **Task 1.3**: Research optimal settings and Ctrl+R tools — [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md)
- [x] ✅ **Task 1.4**: Write the proposal with decisions for the owner — [PROPOSAL.md](PROPOSAL.md)
- [x] ✅ **Task 1.5**: Research directory/repo weighting and security after the owner rejected fzf — [RESEARCH-ranking-and-security.md](RESEARCH-ranking-and-security.md); proposal revised

### Phase 2: Owner decisions and prototype

- [x] ✅ **Task 2.1**: Owner decisions — D1 P1–P5 yes; D2 R2 (repo-owned recorder); D3 fzf as picker only
- [x] ✅ **Task 2.2**: Owner decision D4 — Plan 027 (Atuin) cancelled, moved to `Cancelled/`
- [x] ✅ **Task 2.3**: Prototype the ranker and time it — [`prototype-ranker.bash`](prototype-ranker.bash), [`prototype-timing.bash`](prototype-timing.bash); fixture ordering correct, 15k records ≈ 150 ms, 100k ≈ 440 ms (results in [PROPOSAL.md](PROPOSAL.md))

### Phase 3: Implementation (IaC — edit only; deploy is Phase 4)

- [ ] ⬜ **Task 3.1**: P5 — `PROMPT_COMMAND` hygiene
  - [ ] ⬜ `files/var/local/ps1-prompt`: idempotent array append instead of the scalar assignment
  - [ ] ⬜ `play-basic-configs.yml`: remove the tweaks `source` block from user and root `~/.bash_profile` (`state: absent`), after asserting each `~/.bash_profile` sources `~/.bashrc`
- [ ] ⬜ **Task 3.2**: P1–P4 — replace the `#History` block of `files/etc/profile.d/zz_lts-fedora-desktop.bash`
  - [ ] ⬜ `HISTFILE=~/.local/state/bash/history`, only when that directory is owned by the current user (`[[ -O ]]`), else a stderr warning and bash's default
  - [ ] ⬜ `HISTSIZE=-1`, `HISTFILESIZE=-1`, `HISTCONTROL=ignoreboth`, `HISTTIMEFORMAT`, `lithist`, `histverify`
  - [ ] ⬜ `__history_append` hook (drops a space-prefixed newest entry, then `history -a`), appended to the array once
- [ ] ⬜ **Task 3.3**: `play-basic-configs.yml` — history directories and seed
  - [ ] ⬜ `~/.local/state/bash` `0700` for the user and root
  - [ ] ⬜ Seed `history` `0600` from `~/.bash_history` once (`creates:`), user and root
  - [ ] ⬜ Assert the directory exists, is owned correctly and is writable
- [ ] ⬜ **Task 3.4**: R2 recorder — a `~/.bashrc-includes` snippet (user only, `EUID != 0`), sourced after bash-git-prompt
  - [ ] ⬜ Capture `$?` in a function **prepended** to the array that returns the same status, so bash-git-prompt's `setLastCommandState` still sees the command's own status
  - [ ] ⬜ Record `epoch TAB exit TAB cwd TAB command` NUL-terminated to `~/.local/state/bash/context` (`0600`), builtins only, only when the history number advanced
- [ ] ⬜ **Task 3.5**: Ranker and Ctrl+R binding — promote the prototype into a deployed file
  - [ ] ⬜ `bind -x` Ctrl+R for emacs and vi modes, guarded by fzf presence and `EUID != 0`; current line as query; ctrl-r inside toggles sort; the chosen line replaces the prompt line and never runs on Enter
  - [ ] ⬜ Decide the 100k-row behaviour: accept the latency, or compact the context file into one row per command and directory
- [ ] ⬜ **Task 3.6**: Permanent tests under `tests/` for the recorder and ranker (the fixture cases from the prototype plus the ignorespace/`HISTIGNORE` skip), wired into `qa-all.bash`
- [ ] ⬜ **Task 3.7**: `deploy.bash` (`play-basic-configs.yml`) and `acceptance.bash`
  - [ ] ⬜ Acceptance: in `bash -i` and `bash -l -i`, each hook appears once in `PROMPT_COMMAND`; `HISTFILE`, modes and ownership of the directory and both files; Ctrl+R is bound to the ranker for the user and not for root; root has P1–P4
  - [ ] ⬜ NOT ESTABLISHABLE by script, named for the human: the feel of Ctrl+R in a real terminal
- [ ] ⬜ **Task 3.8**: Run QA (`./scripts/qa-all.bash`) and the `qa-reviewer` agent over the plan diff

### Phase 4: Host deploy and verification

- [ ] ⬜ **Task 4.1**: Owner runs `deploy.bash`, then `acceptance.bash`, on the host
- [ ] ⬜ **Task 4.2**: Re-run `triage.bash`: no live shell holds unsaved history, timestamps present

## Dependencies

- Supersedes: [Plan 027](../Cancelled/027-contextual-shell-history/PLAN.md) (Atuin, Cancelled)

## Success Criteria

- [ ] Every goal above is a passing check in `acceptance.bash`, or named there as NOT ESTABLISHABLE
- [ ] `./scripts/qa-all.bash` passes for the plan's files; the `qa-reviewer` finds no BLOCK or FIX-BEFORE-MERGE items
- [ ] The owner confirms Ctrl+R ranks the current directory's commands first in a real terminal

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00138-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research: a5950efc, 415ec978
- Decisions recorded, prototype timed; implementation (Phase 3) not started
