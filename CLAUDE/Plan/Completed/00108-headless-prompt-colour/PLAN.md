# Plan 00108: A headless box gets a prompt colour

**Status**: Complete
**Created**: 2026-09-09
**Owner**: joseph
**Priority**: Medium

## Overview

`play-basic-configs.yml` decides the prompt colour in three tiers: a per-hostname override in
`localhost.yml`, an existing `/var/local/ps1-prompt-colour`, then an interactive prompt. A
headless run has neither of the first two on a fresh box and nobody to answer the third. The
`pause` returns an empty answer, the `default` filter does not treat empty as missing, and the
box is left with `export PS1_COLOUR=`. Every prompt on it then silently falls back to the prompt
script's own colour, which is indistinguishable from a desktop's default. Measured on three
headless boxes provisioned by this repo: all three carry the empty line.

The fix is the headless contract's own shape: `RUN_BASH_PS1_COLOUR`, forwarded to the main
playbook as the `PS1_Colour` extra-var, validated in `run.bash` against the functions
`/var/local/colours` defines. The empty-answer path takes the documented default.

## Goals

- `RUN_BASH_PS1_COLOUR=<colour>` on a headless run writes exactly that colour.
- An unset variable, headless or interactive-with-enter, writes `lightblueBold`, never empty.
- A name outside the colour set aborts before the main playbook runs.

## Non-Goals

- Changing the prompt script or the colour set.

## Tasks

### Phase 1

- [x] ✅ **Task 1.1**: `run.bash` 1.19.0 forwards and validates `RUN_BASH_PS1_COLOUR`; the
  prompt-answer default covers the empty string; usage text, headless docs, install example,
  changelog and configuration page updated; QA green apart from seven pre-existing rule
  pointers at an untracked daemon path.

## Success Criteria

- [x] A headless box provisioned with `RUN_BASH_PS1_COLOUR=purpleBold` reads back
  `export PS1_COLOUR=purpleBold` (proven by the consuming estate on a box that follows this
  branch's tip; its plan is cited in the journal).

## Delivery & Milestones

- Phase 1: `ad0ca02e`
