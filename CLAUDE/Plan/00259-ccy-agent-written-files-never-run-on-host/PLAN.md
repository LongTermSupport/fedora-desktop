# Plan 00259: ccy agent written files never run on host

**Status**: Not Started
**Created**: 2026-11-20
**Owner**: joseph
**Priority**: High

## Overview

ccy mounts the project, `.git` included, read-write, and files the container writes are owned by
the owner's uid. Git and Claude Code run commands named in files under the project: `.git/hooks`,
the directory `core.hooksPath` names, `.git/config` keys (`core.fsmonitor`, `core.pager`,
`core.sshCommand`, `filter.*`, `diff.*` drivers), `.git/info/attributes`, `.husky`,
`.claude/settings*.json` hooks and `.claude/hooks/handlers/`. An agent that writes one of these
gets code run as the owner, on the host, outside every ccy boundary, the next time the owner or a
host agent runs git or opens a host Claude Code session in that checkout. In this repository
`core.hooksPath` is the tracked `scripts/git-hooks/`, editable from inside.

Issue #356 names the risk in `docs/ccy.md`. This plan decides the structural protection. Rootless
Podman without `CAP_SYS_ADMIN` can hold read-only sub-mounts over the read-write project that the
container cannot remove, which a VM sandbox cannot do as cheaply. The open question is which paths,
given that this repository edits `scripts/git-hooks/` and `.claude/` from inside on purpose.

**The question to decide:** which host-auto-executing paths ccy protects (read-only sub-mount,
launch/exit check, or both), how tracked files that are edited from inside on purpose are treated,
and whether a project can opt out.

## Goals

- No file the container writes is run by the host without the owner's knowledge: each
  host-auto-executing path is either read-only inside the container or checked on the host before
  host git or a host Claude Code session can run it.
- Project configuration a cloned repository brings (a tracked `.claude/ccy/mounts`) may only
  tighten what ccy grants; a widening needs a host-side grant from the owner, keyed on content.
- Host helpers that run git or a helper by name inside a ccy checkout do so hardened (hooks,
  fsmonitor, filters, drivers off; `PATH` not taken from the checkout).

## Non-Goals

- Network egress (Plan 00232) and credentials in the container (Plans 00146, 00252).
- The host `cc` session sandbox (Plan 00255).
- A VM or Landlock layer for ccy (rejected by the run's verdicts).

## Tasks

### Phase 1: Facts and decision

- [ ] ⬜ **Task 1.1**: A plan-local `triage.bash` that proves, on a throwaway checkout, which paths
  the host runs from: a planted `core.fsmonitor`, a filter driver, a hook under `core.hooksPath`,
  a `.claude/settings.json` hook; and whether the host prompt's `git status` on `cd` triggers the
  fsmonitor (reasoned from reading, not yet tested)
- [ ] ⬜ **Task 1.2**: Inventory every path the host reads or runs from a ccy checkout (taint
  inventory), including ccy's own state under `.claude/ccy/`
- [ ] ⬜ **Task 1.3**: Prove a read-only bind over `.git/config` and `.git/hooks` inside the
  read-write project holds against rename-and-replace (Plan 00232's finding F29 shows a read-only
  bound file can be replaced by renaming its parent)
- [ ] ⬜ **Task 1.4**: Owner decision: paths, treatment of tracked hook and `.claude/` paths,
  per-project opt-out; recorded under Technical Decisions

### Phase 2: Build

- [ ] ⬜ **Task 2.1**: Launch-time and exit-time check of `.git/config` for command-bearing keys,
  refusing with the key named
- [ ] ⬜ **Task 2.2**: Read-only sub-mounts for the paths decided in Task 1.4
- [ ] ⬜ **Task 2.3**: Host-side grant for a project's declared mounts (`.claude/ccy/mounts`)
- [ ] ⬜ **Task 2.4**: Hardened host git in `src-go/internal/gitx` for every host call over a
  project (the run's "not filed (cap)" item 6), and `PATH` sanitising for host helpers
- [ ] ⬜ **Task 2.5**: Session-end report of changes to protected paths
- [ ] ⬜ **Task 2.6**: `docs/ccy.md` Security Model updated to the built state; CCY version bump

## Dependencies

- Related: issue #356 (docs), Plan 00225 (liaison seat's read-only executed config, a subset),
  Plan 00206 (host never runs tools from the checkout, same hazard from a different writer),
  Plan 00232 (finding F29)

## Success Criteria

- [ ] An acceptance script plants each command-bearing file from inside a ccy session and shows the
  host does not run it (refused, read-only, or reported before any host git)
- [ ] A tracked `.claude/ccy/mounts` that widens access is refused until the owner grants it
- [ ] QA passes; `qa-reviewer` run as the final step

## Sources

Watch-list run `baseline-20261120-1050` (Plan 00258), in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`:

- `SYNTHESIS.md` section 1, S1, S3, S5; section 4 "New plans" 1
- `vm-sandboxes.md` "Takeaways, ranked" 1 and 3; "coipond/coi" idea 1; "devvmhq/devvm" idea 1
- `local-sandboxes.md` "Takeaways, ranked" 1; "webcoyote/sandvault"; "kstenerud/yoloai" Y2; N4

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00259-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Not started
