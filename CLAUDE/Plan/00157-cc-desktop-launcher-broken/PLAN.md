# Plan 00157: cc desktop launcher broken

**Status**: In Progress
**Created**: 2026-10-05
**Owner**: joseph
**Priority**: High

## Overview

`cc`, the host Claude Code launcher (`/var/local/claude-code/cc`, deployed by
`play-claude-yolo.yml` and aliased in `~/.bashrc`), has stopped working. Typing `cc`
reports that no Claude process was found (the owner's wording, by dictation) instead of
starting a session. The owner is working from `ccy` meanwhile.

The text of that error is not in this repository: not in `cc`, not in the ccy libraries it
sources (`common-pure`, `token-management`, `tmux-session`, `session-registry`), and not in
anything under `files/`. So it comes from something this checkout does not hold. That may be
the claude binary, a host startup file, a host hook, or a different `cc` than the alias.
Nothing is assumed about which until triage says so.

## Facts

| ID  | Fact                                                                                      | Source                                |
| --- | ----------------------------------------------------------------------------------------- | ------------------------------------- |
| F1  | No tracked or ignored file in the repo contains "claude process found" (case-insensitive) | `rg --no-ignore` in the CCY container |
| F2  | `cc` is `alias cc='/var/local/claude-code/cc'` in the managed block of `~/.bashrc`        | `play-claude-yolo.yml`                |

## Hypotheses

Each is settled by a section of `triage.bash`; none is a finding yet.

| ID  | Hypothesis                                                                      | Confirmed by / refuted by                                           |
| --- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| H1  | The claude binary itself prints the message (a newer Claude Code release)       | "the error text inside the claude binary" has a hit                 |
| H2  | `cc` no longer resolves to the wrapper (a function or other binary shadows it)  | "interactive bash: type -a cc" shows something other than the alias |
| H3  | The deployed wrapper or a library differs from this checkout, or fails to parse | "deployed cc and libs vs this checkout" / "bash -n"                 |
| H4  | Credential state cc refuses to walk past (parked backup beside a live file)     | "credential files cc parks and restores"                            |

## Tasks

### Phase 1: Triage

- [x] ✅ **Task 1.1**: Write `triage.bash`: read-only by default; `--trace` additionally runs
  `bash -x cc --version` under `script`, with token values redacted before the trace is written
- [x] ✅ **Task 1.1a**: The first host run hung in its first interactive-shell probe and
  Ctrl-C could not end it. `triage.bash` now runs that shell detached, into a file, killed
  at its limit; `reap-stuck-triage.bash` (first in meta-deploy) records what the old run
  was waiting on, then kills it and everything it started.
- [ ] 🔄 **Task 1.2**: Owner runs the triage on the host (through `meta-deploy.bash`); read the report
  under `untracked/plan-runs/00157-cc-desktop-launcher-broken/triage/`
- [ ] ⬜ **Task 1.3**: Record the facts the report establishes; settle H1 to H4. If the source is still
  unknown, run `triage.bash --trace` in a terminal

### Phase 2: Fix

- [ ] ⬜ **Task 2.1**: Fix in IaC once the cause is a fact (tasks to be written from the triage)

## Success Criteria

- [ ] `cc` in a fresh terminal shows the token chooser and starts a Claude Code session, with both
  a named token and Desktop

## Delivery & Milestones

- Triage script committed
