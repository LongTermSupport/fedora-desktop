# Plan 00138: Bash history that survives many terminals, and a better Ctrl+R

**Status**: Complete
**Created**: 2026-09-24
**Completed**: 2026-09-24
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
- [x] ✅ **Task 2.3**: Prototype the ranker and time it — fixture ordering correct, 15k records ≈ 150–180 ms, 100k ≈ 440–700 ms ([`ranker-timing.bash`](ranker-timing.bash); results in [PROPOSAL.md](PROPOSAL.md))

### Phase 3: Implementation (IaC — edit only; deploy is Phase 4)

- [x] ✅ **Task 3.1**: P5 — `PROMPT_COMMAND` hygiene
  - [x] ✅ `files/var/local/ps1-prompt`: idempotent array append instead of the scalar assignment
  - [x] ✅ `play-basic-configs.yml`: tweaks block removed from user and root `~/.bash_profile` (`state: absent`), after asserting each sources `~/.bashrc`
- [x] ✅ **Task 3.2**: P1–P4 — the history block of `files/etc/profile.d/zz_lts-fedora-desktop.bash`
  - [x] ✅ `HISTFILE=~/.local/state/bash/history`, only when that directory is the current user's (`[[ -O ]]`), else a stderr warning in interactive shells
  - [x] ✅ `HISTSIZE=-1`, `HISTFILESIZE=-1`, `HISTCONTROL=ignoreboth`, `HISTTIMEFORMAT`, `lithist`, `histverify`
  - [x] ✅ `__history_append` hook (`history -a`), appended once. The space-prefixed-entry deletion was dropped: it only repaired bash-preexec, which is not installed
  - [x] ✅ Up-arrow holds only this terminal's commands (owner's request, 2026-09-24). Where the Ctrl+R search is bound, `history-search.bash` starts the shell with HISTFILE at `/dev/null`, so nothing is loaded. The first prompt, or an EXIT trap, points it back at the shared file. Root and shells without fzf keep stock bash. Gate: `scripts/test-bash-history-session.bash`
- [x] ✅ **Task 3.3**: `play-basic-configs.yml` — history directories and seed
  - [x] ✅ `~/.local/state/bash` `0700` for the user and root; parents created explicitly so they are the account's, not root's
  - [x] ✅ Seed `history` `0600` from `~/.bash_history` once (`force: false`), user and root
  - [x] ✅ Directory mode and ownership are enforced by the `file` task and asserted by `acceptance.bash` checks 3 and 11
- [x] ✅ **Task 3.4**: R2 recorder — `files/home/bashrc-includes/history-search.bash` (user only)
  - [x] ✅ Exit status: no capture function needed. Tested on bash 5.3: every `PROMPT_COMMAND` element receives the command's own `$?`, so the recorder reads it directly and is simply appended
  - [x] ✅ Records `epoch TAB exit TAB cwd TAB command`, NUL-terminated, to `~/.local/state/bash/context` (created `0600`), builtins only, only when the history number advanced; the directory is the one the command started in
- [x] ✅ **Task 3.5**: Ranker and Ctrl+R binding
  - [x] ✅ `files/home/.local/bin/bash-history-rank`; `bind -x` Ctrl+R in emacs and both vi keymaps, warning on stderr if fzf is missing; current line as query; ctrl-r inside toggles sort; the pick replaces the line and never runs
  - [x] ✅ 100k-row latency accepted for now; compaction is the remedy when it matters, not a compiled ranker (reasoning in [PROPOSAL.md](PROPOSAL.md))
- [x] ✅ **Task 3.6**: `scripts/test-bash-history-search.bash`, wired into `qa-all.bash` as the `bash-history-search` gate; three mutants each turned it red
- [x] ✅ **Task 3.7**: [`deploy.bash`](deploy.bash) (also runs `play-vm-test-lab.yml` where the lab is installed) and [`acceptance.bash`](acceptance.bash) (15 checks with a COVERAGE line; before the deploy it runs all 15 and rejects)
- [x] ✅ **Task 3.8**: Run QA (`./scripts/qa-all.bash`) and the `qa-reviewer` agent over the plan diff
  - [x] ✅ First review: BLOCK — [report](subagent-reports/260924-qa-reviewer-opus-5.md). The blocker (the typed query in fzf's argv) and the fix-before-merge findings fixed
  - [x] ✅ Confirming review: blocker resolved (verified with a real fzf under a pty); one new FIX-BEFORE-MERGE (the symlink fix mis-tiered stow-style links into a repository) and one should-fix (the gate did not assert the query bind) — [report](subagent-reports/260924-qa-reviewer-confirm-opus-5.md). Both fixed, each with a test a control mutant turns red

### Phase 4: Host deploy and verification

- [x] ✅ **Task 4.1**: Owner ran `deploy.bash` (both plays, no failures), then `acceptance.bash`: ACCEPTED, 15 of 15 checks, full coverage
- [x] ✅ **Task 4.2**: Re-ran `triage.bash`: the new history file is written at each prompt (last write 31 s before the probe) with timestamps. Terminals opened before the deploy keep the old settings until closed and write to `~/.bash_history` on exit

## Dependencies

- Supersedes: [Plan 027](../../Cancelled/027-contextual-shell-history/PLAN.md) (Atuin, Cancelled)

## Success Criteria

- [x] Every goal above is a passing check in `acceptance.bash`, or named there as NOT ESTABLISHABLE
- [x] `./scripts/qa-all.bash` passes for the plan's files (the `bash-history-search` gate); the `qa-reviewer`'s BLOCK and FIX-BEFORE-MERGE findings are all resolved. The remaining qa-all failures predate this plan
- [x] The owner reported "all done" after the deploy; the Ctrl+R feel in a real terminal is theirs to judge and was not raised as a problem

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00138-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research: a5950efc, 415ec978
- Decisions recorded, prototype timed
- Implementation on branch `worktree-plan-00138`: f3ec93cf, review fixes 01f4a90a and b31e455d; merged into F44 at 5a65961e
- Deployed on the host; acceptance ACCEPTED 15/15
