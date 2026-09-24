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

This plan is **read-only research for now**: review the configuration, research the
optimal settings, and propose improvements, especially to the Ctrl+R experience.
Nothing is changed until the owner has chosen between the proposals.

- Current-state review with evidence: [RESEARCH-current-config.md](RESEARCH-current-config.md)
- Optimal settings and Ctrl+R tool comparison: [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md)
- Proposed changes and the decisions they need: [PROPOSAL.md](PROPOSAL.md)

## Goals

- Every command is in `~/.bash_history` within one prompt of running, across every
  terminal, tmux pane and login/non-login shell
- Every history entry carries a timestamp
- Ctrl+R searches the whole history, fuzzily, as a list, without duplicates
- `PROMPT_COMMAND` holds each hook exactly once in every kind of shell
- A stray unconfigured shell can no longer truncate the history file

## Non-Goals

- zsh/fish history
- Syncing history between machines (Plan 027's Phase 3 territory)
- Rewriting or de-duplicating the existing history file in place

## Tasks

### Phase 1: Research (read-only)

- [x] ✅ **Task 1.1**: Triage the live configuration — [`triage.bash`](triage.bash)
- [x] ✅ **Task 1.2**: Review the configuration — [RESEARCH-current-config.md](RESEARCH-current-config.md)
- [x] ✅ **Task 1.3**: Research optimal settings and Ctrl+R tools — [RESEARCH-optimal-config.md](RESEARCH-optimal-config.md)
- [x] ✅ **Task 1.4**: Write the proposal with decisions for the owner — [PROPOSAL.md](PROPOSAL.md)

### Phase 2: Owner decisions

- [ ] ⬜ **Task 2.1**: Owner decides D1–D3 in [PROPOSAL.md](PROPOSAL.md) (which of P1–P7 to adopt)
- [ ] ⬜ **Task 2.2**: Owner decides D4 — the relationship with Plan 027 (cancel, or keep as the Atuin follow-up)

Implementation phases are added once Phase 2 is settled.

## Dependencies

- Related: [Plan 027](../027-contextual-shell-history/PLAN.md) (Atuin, Not Started) — overlaps on Ctrl+R

## Success Criteria

- [ ] The owner has a proposal they can decide on, grounded in host evidence and cited docs
- [ ] (Later phases) acceptance checks for each goal above

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00138-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research and proposal complete; awaiting owner decisions (Phase 2)
