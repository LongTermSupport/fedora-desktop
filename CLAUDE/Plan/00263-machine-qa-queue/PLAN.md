# Plan 00263: machine qa queue

**Status**: In Progress (design done; blocked on the owner decisions D1-D4, a host window, and the heavy-work pause)
**Created**: 2026-11-23
**Owner**: joseph
**Priority**: High

**Target repository**: `LongTermSupport/qa-queue`, a standalone repository (owner, Q62). It is not built inside fedora-desktop. This plan stays here only to track it
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

- [x] ✅ **Task 1.1**: The owner's answers (Q62, relayed by the coordinator), verbatim: "3 and yes
  LongTermSupport - please first though lets fully check for prior art - even if we dont get a
  full match, surely there's some good ideas out there that we can crib from. the container
  mounted lock file sounds like a quick win, the downside being that it would enforce full
  single threaded across the whole machine. I think we'd need to mount N lock files and allow
  agents to try to lock any one of them - so we have a simple concurrency limit. And yes I think
  any single container should be capped at (100/N+1)% of machine CPU". That gives:
  - **Option 3, staged.** First N lock files mounted into each container (an agent takes any free
    one, so at most N heavy jobs run at once), plus a CPU cap per container. Then the queue, on
    the home machine and in the data centre.
  - **Repository:** `LongTermSupport/qa-queue`.
  - **The prior-art survey (Task 1.2) comes first, and in full.**
  - The coordinator proposed refinements that the owner has not yet confirmed. They are inputs to
    the design, not decisions:
    - a low cgroup `cpu.weight`, with the hard cap as a backstop;
    - an I/O weight and a memory ceiling per container;
    - the wrapper exports the slot's share (`GOMAXPROCS`, `go test -p`, `pytest -n`);
    - ~~host seats get the same caps through a systemd slice, and the same lock~~ (dropped by the
      owner's third answer, below);
    - N=2 on the laptop to start.
  - **Owner, second answer on Q62 (verbatim):** "3 - this relies on codebases/agents being honest
    and aware of the slots. Is there a way to cap this at container level?" So the cap must hold
    whatever an agent does. The coordinator proposed an answer, not yet confirmed by the owner:
    **kernel enforcement first.**
    - One parent `agents.slice` holds every ccy container (`podman --cgroup-parent`). It has a
      total `CPUQuota`, a low `CPUWeight`, an `IOWeight` and a `MemoryMax`. Host sessions are left
      out (the owner's third answer, below).
    - Each container gets `--cpus` and `--cpuset-cpus`, so nproc, affinity and GOMAXPROCS size
      themselves, plus `--memory` and `--pids-limit`.
    - The slot lock becomes an optimisation for fairness and throughput, not the safety.
    - The order is: the slice and caps, then the lock, then the queue. Task 1.2 checks rootless
      podman's cgroup v2 delegation on F44, and Go's cgroup-aware GOMAXPROCS.
  - **Owner, third answer on Q62 (verbatim):** "host sessions should not be running tests - host
    seats are for running deployments or triage at desktop level - for now we can leave them out
    but we do need to think about ways to ensure they stay in their lane - they are not workers".
    So host sessions are not in the slice and do not take the lock.
  - **Open design item, the host seat's lane:** host seats run deployments and desktop-level
    triage, never tests or QA. How is that enforced, not just asked? Candidates:
    - a hooks-daemon rule that refuses QA, test and build commands in a `+local.host` seat;
    - the `cc` launcher's deny rules (the settings `cc` already passes to Claude Code).
      Task 1.3 weighs them; the owner decides.
- [x] ✅ **Task 1.2**: A survey of existing tools that already do this: job queues and locks
  such as `flock`, `ts` (task-spooler), `nq`, systemd transient units with slices,
  `pueue`, and self-hosted CI runners. For each: fit, gaps and licence. Build only what no
  existing tool covers. Done: [RESEARCH-prior-art.md](RESEARCH-prior-art.md). In short:
  - Kernel caps can hold the limit at container level: a parent slice plus per-container caps.
    Rootless `--cgroup-parent` into a user slice is unproven and is tested first.
  - flock in a bind mount is the right slot lock.
  - No existing tool fits whole, but five small agent tools have ideas worth taking.
  - The recommendation is to build thin: stage 1 as a slice, caps, a slotted-flock wrapper and
    a hook; stage 2 as a small queue whose runners start each job as a systemd transient unit,
    with pueue and Woodpecker weighed as the alternatives.
- [x] ✅ **Task 1.3**: The design: [DESIGN.md](DESIGN.md). It covers the kernel caps, the slot
  lock, the host seat's lane and the queue, with four owner decisions (D1-D4, DESIGN.md section 6)
  and the build in four phases (DESIGN.md section 5).
- [ ] ⬜ **Task 1.4**: The owner decides D1-D4 (N; the CPU and memory budget; the lane guard;
  stage 2 built or adopted).

### Phase A: stage 1a, kernel caps (fedora-desktop)

- [ ] ⬜ **Task A.1**: The read-only probe of cgroup delegation and the CPU topology on the host
  (`triage.bash`, DESIGN.md 1.6, round 1), in a window the owner picks.
- [ ] ⬜ **Task A.2**: The proof that a rootless container goes under the user slice
  (`acceptance.bash`, round 2), in the owner's window.
- [ ] ⬜ **Task A.3**: The `cpuset` Delegate drop-in in `play-podman.yml`, only if A.1 shows
  `cpuset` is missing.
- [ ] ⬜ **Task A.4**: The rules first: extend the pids-limit rules to require the slice and the
  caps, proven red on today's container runs.
- [ ] ⬜ **Task A.5**: The container-limits helper (Go, test first), wired into every run site,
  with the version bump.
- [ ] ⬜ **Task A.6**: `agents.slice` and its limits file in `play-claude-yolo.yml`.
- [ ] ⬜ **Task A.7**: A plan script applies `podman update` to the running containers.
- [ ] ⬜ **Task A.8**: Acceptance: two containers under load, and the owner says the desktop
  stays usable.

### Phase B: stage 1b, slots (`qa-queue`, then fedora-desktop)

- [ ] ⬜ **Task B.1**: Create the `LongTermSupport/qa-queue` repository and its CI.
- [ ] ⬜ **Task B.2**: `qa-slot run -- <cmd>`, test first, plus a test across two containers,
  then a release.
- [ ] ⬜ **Task B.3**: The slot directory and its mount, and the pinned install in the image
  (version bump).
- [ ] ⬜ **Task B.4**: `qa-all.bash` and `qa-changed.bash` wrap themselves in a slot; a PreToolUse
  handler refuses known-heavy commands run outside the wrapper.

### Phase C: the host seat's lane

- [ ] ⬜ **Task C.1**: The guard chosen in D3 (DESIGN.md section 3), test first.

### Phase D: stage 2, the queue

- [ ] ⬜ **Task D.1**: The job format, the spool, the runner, the checkout cache, results and
  logs, as D4 decides (DESIGN.md section 4).
- [ ] ⬜ **Task D.2**: A play installs the pinned runner; `qa-changed.bash` and the agents' QA
  instructions submit through the queue.

## Success Criteria

- [ ] Five agent sessions asking for QA at once run at most N jobs at a time, and the owner's
  desktop stays responsive.
- [ ] A job's result names the commit it tested and matches a direct run of the same commands.
- [ ] Pausing the queue stops new jobs from starting within seconds.

## Delivery & Milestones

- Phase 1 research and design: RESEARCH-prior-art.md, DESIGN.md.
