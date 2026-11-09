# Plan 00237: qa runs one at a time

**Status**: In Progress
**Created**: 2026-10-29
**Owner**: joseph
**Priority**: High

## Overview

The owner's rule: QA MUST NOT run in parallel, enforced rather than left to habit. A full
`./scripts/qa-all.bash` in one worktree and a worker's `./scripts/qa-changed.bash`, whose Go
gate ran the whole `go test -race -shuffle=on -covermode=atomic -coverpkg=./... ./...`, ran
together in one CCY container and pushed the host's load to 63 on 22 cores while the owner
was working. The hooks-daemon rule R-SUBAGENT-FULL-QA covers only a sub-agent's full run; it
cannot stop two coordinators, a coordinator and a worker's diff run, or a run on the host
beside one in a container.

So every QA run takes one lock, shared by every view of the checkout: a file in the
repository's git common directory, held with flock(2). A second run waits for the first,
saying on stderr whose run it waits for, instead of failing; `--no-wait` fails at once for a
caller that prefers that. A run started inside a run (qa-all and qa-changed both run
`scripts/qa-go-qa.bash`, which takes the lock when run on its own) inherits the lock and
never waits for its own parent.

And every QA run is a background job: the whole process tree runs at nice 19 and in the idle
I/O class, set once at the top, so even a single run gives way to the person at the desk.

## Goals

- `qa-all`, `qa-changed` and `scripts/qa-go-qa.bash` run alone take one lock,
  `<git common dir>/fedora-desktop-qa.lock`, shared by the main checkout, every git worktree,
  every CCY container that bind-mounts the checkout, and the host.
- A second run waits, printing `qa: waiting for the QA run <pid> (<command>, started <time>, in <worktree>)` once and a short line every minute; `--no-wait` exits 2 at once naming the
  holder; `QA_LOCK_TIMEOUT` bounds the wait (exit 2 when it passes).
- A nested run (a child of the holder) inherits the lock: no deadlock.
- The QA process tree runs at nice 19, ionice idle; `QA_FULL_PRIORITY=1` opts out.
- CLAUDE/QA.md and CLAUDE/AgentNotes.md say all of it, so a coordinator knows a second run
  waits rather than fails.

## Non-Goals

- Serialising anything that is not a QA entry point (a plan's own test script, `go test` run
  by hand). The suites qa-all runs are covered because qa-all holds the lock around them.
- Fairness between waiters: whoever polls first after the holder ends takes the lock.
- Locking the build of the tool by `scripts/lib/go-tool.bash` (a cached `go build`, seconds):
  the lock is taken by the built command, which is where the run's flags are known.

## Tasks

### Phase 1: the lock and the priority, tests first

- [x] ✅ **Task 1.1**: `qa/internal/qalock`: the lock path from `git rev-parse --git-common-dir` (absolute), flock(2) with a poll on a clock seam, the holder record
  written by the holder, the wait line and the minute line, `--no-wait`, `QA_LOCK_TIMEOUT`,
  nesting decided from the process tree (the record's pid namespace is this run's and its pid
  an ancestor), never the environment. Unit tests on fakes; a two-process test of the real
  flock; a hard-link test that the lock is the inode's.
- [x] ✅ **Task 1.2**: low priority: `qa/internal/proc` lowers the calling thread (nice 19,
  `ioprio_set` idle) and re-execs the command, so every thread and child inherits it; skipped
  when already low or with `QA_FULL_PRIORITY=1`. Tests on a fake and a real child.
- [x] ✅ **Task 1.3**: wire both into `qa-all` and `qa-changed` (lock after the run is known
  to run something; `--no-wait` in both usages) and a `qa-lock` command that
  `scripts/qa-go-qa.bash` runs go-qa under.

### Phase 2: documentation and review

- [x] ✅ **Task 2.1**: CLAUDE/QA.md (the lock, why it is shared, the wait line, `--no-wait`,
  `QA_LOCK_TIMEOUT`, nice/ionice, `QA_FULL_PRIORITY`), CLAUDE/AgentNotes.md (a second run waits).
- [x] ✅ **Task 2.2**: qa-changed passes; the qa-reviewer agent reviews the branch (its
  findings fixed: nesting no longer trusts the environment, the real nested chain and a child's
  I/O class are tested, the bind-mount claim is stated as what the tests prove).
- [ ] ⬜ **Task 2.3**: on the host, with this merged: start `./scripts/qa-changed.bash` on the
  host and one in a CCY container on the same checkout; the second prints the wait line naming
  the first. This is the one check of sharing through a bind mount no unit test can set up.

## Success Criteria

- [x] Two real processes on one lock file: the second waits and names the first, then runs.
- [x] A qa-changed whose Go gate runs `qa-go-qa.bash` does not wait for itself (the real run; qa-all takes the same path).
- [x] `--no-wait` with the lock held exits 2 naming the holder.
- [x] A QA run's children run at nice 19, ionice idle, unless `QA_FULL_PRIORITY=1`.
- [ ] A run in a CCY container waits for a run on the host (Task 2.3).

## Delivery & Milestones

- `395457460` the lock, low priority, qa-lock, docs.
- The review fixes: the commit after it on this branch.
