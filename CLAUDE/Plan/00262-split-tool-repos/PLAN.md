# Plan 00262: split tool repos

**Status**: Proposed (awaiting owner decisions)
**Created**: 2026-11-23
**Owner**: joseph
**Priority**: Medium

## Overview

The owner asked whether fedora-desktop has become a monorepo whose CI runs everything for any
change, and whether the tool projects (ccy, teamchat, and others) should move to their own
repositories with their own CI, fedora-desktop only pinning their versions.

[PROPOSAL.md](PROPOSAL.md) holds the evidence and the options: where CI time goes now (the Go
gate is about half of a full run, teamchat is half of the Go code, and CI never restores a Go
cache), the candidate repos with their import coupling, the pin model, history, sequencing with
Plan 00209, cheaper interim wins, and the owner decisions D1-D7.

The recommendation is: fix CI wiring first and measure; then split a Go base module, teamchat
and ccy, in that order, only after the v2 cut-over and v1 removal (Plan 00209 Task 6.5).

**Owner direction (Q65), a second reason for the split:** LXC seats are heavily used, and the
owner wants the cc, ccy and teamchat tooling in a standalone repository that ships pinned,
released binaries, downloaded straight into an LXC guest, with no fedora-desktop checkout or
play there. Nothing starts before v2 lands; Plan 00146's token restart is proven on cc and ccy
first, and LXC members are a near-future target, not a distant one.

## Goals

- Cut the wall time of the `ci/` diff gate and the full `F44` run.
- A change to one tool does not run every other tool's tests.
- fedora-desktop pins each split tool to one exact version, tracked by `vars/version-pins.yml`.
- The agent tooling (cc, ccy, teamchat) ships released binaries with checksums that an LXC
  guest installs directly, pinned, without fedora-desktop.

## Non-Goals

- No code moves while Plan 00209's v2 cut-over and v1 removal are open.
- No split of v1 (pingbus, agent_bus): it is deleted by Plan 00209 Task 6.5.
- No split of the QA tooling, the Python helpers, the GNOME extensions or the hooks-daemon
  project handlers.
- No history rewrite by an agent (`git filter-repo` is human-only).

## Tasks

### Phase 0: Owner decisions

- [ ] ⬜ **Task 0.1**: Owner answers D1-D7 in [PROPOSAL.md](PROPOSAL.md#9-owner-decisions).

### Phase 1: Interim CI wins (no split; can start now, touches no v2 code)

- [ ] ⬜ **Task 1.1**: Measure the Go gate per package and per stage in CI (teamchat versus the
  rest), and why a small Go diff still runs the Go gate for most of a diff run.
- [ ] ⬜ **Task 1.2**: Turn the Go build and test cache on in `qa.yml`
  (`cache-dependency-path` for `src-go/go.sum` and `src-go/qa/go.sum`); confirm go-qa lets
  cached test results stand.
- [ ] ⬜ **Task 1.3**: Split the single `qa` job into parallel jobs (Go gate, helper tests,
  the rest) with a gate-subset option in `qa-all`, and a final job requiring all.
- [ ] ⬜ **Task 1.4**: Run the helper unit suite in parallel processes.
- [ ] ⬜ **Task 1.5**: Re-measure; owner decides whether the split is still wanted for CI time.

### Phase 2: Go base module (after Plan 00209 Task 6.5)

- [ ] ⬜ **Task 2.1**: Design the base module (`execx`, `term`, `fsx`, `gitx`, `shellword`,
  `stdinread`, `ui`, `sudo`, `systemd`, `duration`, `clock`); fix the `stdinread` to
  `teamchat/clock` inversion.
- [ ] ⬜ **Task 2.2**: New repo (fresh, pointer commit), its CI and tagged release; fedora-desktop
  imports it by version.

### Phase 3: teamchat repo

- [ ] ⬜ **Task 3.1**: Public API for what `internal/teams` uses; the teamchat project rules move
  with it.
- [ ] ⬜ **Task 3.2**: New repo, CI (with the `busfailpoints` stages), releases with checksums.
- [ ] ⬜ **Task 3.3**: `play-teamchat.yml`, the ccy image and `vars/version-pins.yml` consume the
  pinned release; `SourceDigest` becomes the release version.

### Phase 4: ccy repo

- [ ] ⬜ **Task 4.1**: Move the launcher, image, ccy Go commands and the `ccy-*` suites; the CCY
  version-bump and merge-commit gates move with them.
- [ ] ⬜ **Task 4.2**: `play-claude-yolo.yml` builds the pinned ccy commands; pins listed in
  `vars/version-pins.yml`.

## Success Criteria

- [ ] A change confined to fedora-desktop's plays does not run teamchat or ccy tests.
- [ ] Every split tool is pinned to one version and reported by `scripts/check-pinned-versions.bash`.
- [ ] Each new repo has its own green CI and tagged releases.

## Delivery & Milestones

- Proposal written: [PROPOSAL.md](PROPOSAL.md)
