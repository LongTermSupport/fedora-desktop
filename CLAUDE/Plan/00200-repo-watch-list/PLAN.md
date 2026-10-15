# Plan 00200: repo watch list

**Status**: In Progress
**Created**: 2026-10-15
**Owner**: joseph
**Priority**: Medium

## Overview

The owner wants a list of external public repositories that this project watches and scans at
intervals, looking for good ideas, tooling, concepts and inspiration for fedora-desktop (the
Fedora IaC, the ccy container, agent teams, the hooks daemon, the Go QA tooling). Each scan
ends in a GitHub issue for a clear, defined win, or a plan where more discussion or research is
needed.

Three parts: the list and its state (`CLAUDE/Watchlist.md`, human-readable; `CLAUDE/Watchlist.tsv`,
strictly loaded: URL, branch, last-checked commit, run that recorded it); a Go command
`watchlist` that mirrors the repositories and reports what changed since the last review
([SPEC-watchlist.md](SPEC-watchlist.md)); and a `watchlist-scout` agent with a `/watchlist` skill
that do the reviewing and the filing.

## Goals

- `watchlist sync | mark | add` built to SPEC-watchlist.md, test first, through the `execx` seam
  (git through `gitx`) with fakes; every failure loud.
- The list documented (`CLAUDE/Watchlist.md`, indexed in CLAUDE.md) with state in a strictly
  loaded data file.
- A `watchlist-scout` agent and a `/watchlist` skill (scan all, scan one, add and review).
- The list seeded with `cdknorow/coral`, recorded as reviewed at the commit Plan 00193's
  `AUDIT-coral.md` read.

## Non-Goals

- A scheduler. How scans recur is a decision for the owner (task 3.1).
- Installing `watchlist` on the host. It runs from the checkout with `go run`; nothing outside an
  agent run needs it. A play like `ccy-teams`'s (`tasks/build-go-command.yml`) is the route if
  that changes.
- A new scan of coral. Its full review is Plan 00193's audit.
- The scout implementing what it finds.

## Tasks

### Phase 1: The command

- [x] ✅ **Task 1.1**: SPEC-watchlist.md, numbered expectations W1 to W25.
- [x] ✅ **Task 1.2**: `gitx` mirror methods (default branch, clone, fetch, head, range), tests first.
- [x] ✅ **Task 1.3**: `internal/watchlist` and `cmd/watchlist`, tests first, one test group per
  expectation.

### Phase 2: The list, the agent, the skill

- [x] ✅ **Task 2.1**: `CLAUDE/Watchlist.md`, `CLAUDE/Watchlist.tsv` seeded with coral at the
  commit `AUDIT-coral.md` read; indexed in CLAUDE.md.
- [x] ✅ **Task 2.2**: `.claude/agents/watchlist-scout.md`.
- [x] ✅ **Task 2.3**: `.claude/skills/watchlist/SKILL.md`.
- [x] ✅ **Task 2.4**: The adversarial review
  ([subagent-reports/261015-fable-review-watchlist.md](subagent-reports/261015-fable-review-watchlist.md))
  is fixed: force-push detection and `mark` ancestry (W19, W23), the scout's definition of a
  clear win, issue cap, label, text scan and the recorded standing request, the qa-changed route
  for the list, tools, tests for W5, W13, W14, host pinned to github.com, hints for the two dead
  ends, comment note, go-binary-suites header.
- [ ] ⬜ **Task 2.5**: First live use: `/watchlist` on coral (changes since the audit), after the
  owner has seen this plan. It is a large first diff (the audit's commit is hundreds of commits
  behind coral's head), so expect the cap of 3 issues to bind.

### Phase 3: Recurrence (owner decision)

- [ ] ⬜ **Task 3.1**: The owner chooses how scans recur: a persistent cron in the hooks daemon
  configuration, a scheduled agent (`/schedule`), or a periodic prompt by hand. Until decided,
  scans are run by hand with `/watchlist`.

## Success Criteria

- [x] `go test -race` and `golangci-lint` pass for `src-go`, with a test per expectation.
- [x] The shipped list loads under the strict rules (W25).
- [x] `watchlist add` against a real remote read the default branch and refused a duplicate.
- [ ] A scan has produced at least one issue or plan, or a report saying why not (task 2.5).
- [ ] The recurrence mechanism is chosen (task 3.1).

## Delivery & Milestones

- Command, list, agent and skill built on branch `watchlist/build`.
