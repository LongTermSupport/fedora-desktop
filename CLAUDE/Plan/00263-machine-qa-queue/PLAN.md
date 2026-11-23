# Plan 00263: machine qa queue

**Status**: Not Started (design questions with the owner)
**Created**: 2026-11-23
**Owner**: joseph
**Priority**: High

**Target repository**: `qa-queue`, a standalone repository under the same GitHub owner as
fedora-desktop. It is not built inside fedora-desktop. This plan stays here only to track it
until the repository exists; fedora-desktop then only pins and installs it.

## Overview

Several agent sessions on one machine each start their own QA run: Go builds, race tests,
semgrep, linters, test suites. When they run at once, the machine becomes barely usable for its
owner. That happened on 2026-11-23, and every agent's heavy work had to be stopped by hand.

A machine-level QA queue fixes this. An agent asks the queue before it runs QA, instead of
running QA itself. A runner takes queued jobs one at a time, or up to a set limit. A job names a
repository, a commit, the commands, and any pre and post steps. Runners keep a cache of
checkouts that expires after a set time (TTL), so a job does not clone the repository afresh.
The owner can pause the queue, so the laptop stays usable during a work day.

This is a general tool, not specific to fedora-desktop, so it lives in its own repository with
its own focused CI (see Plan 00262).

## Goals

- At most N heavy QA jobs run on a machine at once (N set by the owner, default 1), whatever
  the number of agent sessions.
- An agent submits a job and gets its result: exit status, a log path, and the commit tested.
  It never runs the commands itself.
- Jobs run in clean checkouts of the named commit, from a cache with a TTL, never in an agent's
  own working tree.
- The owner can see the queue, pause it, resume it and cancel a job from one command.
- Containers (ccy sessions) can submit jobs to the host's queue through a narrow, safe channel.

## Non-Goals

- Not a CI replacement: GitHub CI stays the gate for protected branches.
- Not distributed across machines in its first version.
- No change to what fedora-desktop's QA checks; Plan 00262 and its audit cover that.

## Tasks

### Phase 1: Design (owner questions first)

- [ ] ⬜ **Task 1.1**: The owner's answers on design and existing tools. The coordinator is
  running the questions thread; the answers are recorded here.
- [ ] ⬜ **Task 1.2**: A survey of existing tools that already do this: job queues and locks
  such as `flock`, `ts` (task-spooler), `nq`, systemd transient units with slices,
  `pueue`, and self-hosted CI runners. For each: fit, gaps and licence. Build only what no
  existing tool covers.
- [ ] ⬜ **Task 1.3**: The design: the job format, how a container submits (socket, spool
  directory or bus), the security of running a submitted command (allowed repositories and
  commands, the user it runs as), the checkout cache and its TTL, CPU and IO limits (cgroups),
  results and logs, pause and cancel.

### Phase 2: Build (in the standalone repository)

- [ ] ⬜ **Task 2.1**: Create the `qa-queue` repository with its own CI. Go, test first, under
  the same rules as fedora-desktop's `src-go`.
- [ ] ⬜ **Task 2.2**: The queue, the runner and the client command, as the design says.

### Phase 3: Adopt in fedora-desktop

- [ ] ⬜ **Task 3.1**: A play installs a pinned release and its unit.
- [ ] ⬜ **Task 3.2**: `qa-changed.bash` and the agents' QA instructions submit through the
  queue; the docs and the QA rule are updated.

## Success Criteria

- [ ] Five agent sessions asking for QA at once run at most N jobs at a time, and the owner's
  desktop stays responsive.
- [ ] A job's result names the commit it tested and matches a direct run of the same commands.
- [ ] Pausing the queue stops new jobs from starting within seconds.

## Delivery & Milestones

- Plan scaffolded; awaiting the owner's design answers.
