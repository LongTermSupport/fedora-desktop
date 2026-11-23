# Plan 00263: machine qa queue

**Status**: In Progress (design done; all owner decisions D1-D4 answered; stage 1a's caps are built and land with the v2 batch (Plan 00209); their host proof and Phases B-E wait on a host window)
**Created**: 2026-11-23
**Owner**: joseph
**Priority**: High

**Target repository**: `LongTermSupport/agentic-qa-queue`, a standalone repository (owner, Q62;
renamed by the owner's `agentic-` convention for AI and workflow tooling). It is not built inside fedora-desktop. This plan stays here only to track it
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
- [x] ✅ **Task 1.4**: The owner decides D1-D4 (N; the CPU and memory budget; the lane guard;
  stage 2 built or adopted). Owner, on the design (verbatim): "lets start a naming convention -
  "agentic-" prefix for all of our AI / workflow tooling under LongTermSupport - so make it
  "agentic-qa-queue". 50% is a dumb threshold - my laptop is not RAM constrained at all - a
  50% cap regardless of RAM will just mean under utilisation. I'd suggest desktop gets a floor of
  16GB and then the remainder is up for grabs. I wonder if we should do the same for CPU -
  maintain a hard floor of 4 threads and then we do percentage on the rest? ... OK so i think you
  are saying that IO weights are meaningless - we hope that limited CPU will naturally limit IO
  usage. I dont envisage building this ad hoc QA on the DC server - it is already acting as a
  github runner? The home server can act as QA runner and so can the laptop." DESIGN.md now has:
  - the repository `LongTermSupport/agentic-qa-queue`;
  - D2 as floors (4 threads and 16 GB kept for the desktop, the rest shared equally by N);
  - no IOWeight, with `io.max` as an optional probe item;
  - runners on the laptop and the home server only.
    D1 is answered (Q64): N is computed per machine at install time, one run per 12 agent
    threads, rounded up, capped by memory (DESIGN.md section 6, D1; the owner confirmed the
    memory term). D3 is answered (Q64): yes, the hooks-daemon guard (A) plus the Claude Code deny
    rules (B).

### Phase A: stage 1a, kernel caps (fedora-desktop)

- [ ] ⬜ **Task A.1**: The read-only probe of cgroup delegation and the CPU topology on the host
  (`triage.bash`, DESIGN.md 1.6, round 1), in a window the owner picks. It also reads one QA
  run's peak memory (`memory.peak` of its cgroup), the `per_run_peak_ram` term of D1's formula.
- [ ] ⬜ **Task A.2**: The proof that a rootless container goes under the user slice
  (`acceptance.bash`, round 2), in the owner's window.
- [ ] ⬜ **Task A.3**: The `cpuset` Delegate drop-in in `play-podman.yml`, only if A.1 shows
  `cpuset` is missing.
- [x] ✅ **Task A.4**: The rules first: `project_container_pids_limit` now requires the whole
  limit set (`--cgroup-parent agents.slice`, `--pids-limit`, `--memory`, `--memory-reservation`)
  on every Go container run (c19438c1d).
- [x] ✅ **Task A.5**: The container-limits helper: `internal/ccylimits` (Go, test first) and
  the bash launcher's `lib/container-limits.bash`, one limit set for every ccy and ccc run
  (memory = RAM less 16 GiB, pids 16384), with the version bump (dd5ed4743).
- [x] ✅ **Task A.6**: `agents.slice` (MemoryMax = RAM less 16 GiB, MemoryHigh 1 GiB below) in
  `play-claude-yolo.yml`, read back from the live user manager; a launch is refused until the
  slice is installed (22243504e). The same commit replaces the old oomd fix, which never
  applied: a system drop-in for `user@.service` stops systemd-oomd's pressure kill in the user
  session (`play-systemd-user-tweaks.yml --tags oomd`). Findings, including what
  `--memory-reservation` really sets and the LXC members' missing caps:
  [RESULTS-ccy-caps.md](RESULTS-ccy-caps.md).
- [ ] ⬜ **Task A.7**: The running containers take the caps. Replaced by a relaunch (`ccy --resume`), not `podman update`: `--cgroup-parent` cannot be changed on a running container.
  It is step 6 of the v2 cut-over.
- [ ] 🔄 **Task A.7b**: The caps probe as a plan script, written: `caps-acceptance.bash`
  (planlib, read only). It checks that `user@UID.service` reports no pressure kill, that
  `oomctl dump` watches no pressure on it, that `agents.slice` is loaded with its caps, and that
  every running ccy and ccc container's live cgroup is under it, with its scope's MemoryMax and
  TasksMax set. Exit 2 when it cannot establish one. Owed: its first host run, queued in
  `meta-deploy.list` once the v2 cut-over's list has run.
- [ ] ⬜ **Task A.9**: Caps for the LXC members (none has a memory or pids limit today; they are
  rootful and sit outside `agents.slice`): `lxc.cgroup2.memory.max`, `memory.high` and
  `pids.max` in the member config, and a system slice to cap them together. An owner decision
  first, since it limits running members.
- [ ] ⬜ **Task A.8**: Acceptance: two containers under load, and the owner says the desktop
  stays usable.

### Phase B: stage 1b, slots (`qa-queue`, then fedora-desktop)

- [ ] ⬜ **Task B.1**: Create the `LongTermSupport/agentic-qa-queue` repository and its CI.
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

### Phase E: later, auto-tuning and weighted jobs (owner direction, Q64)

The owner wants "something that monitors and auto tunes this config, and something that can
judge and weight specific job runs". The static D1 formula ships first; this phase comes after
Phase D.

- [ ] ⬜ **Task E.1**: Auto-tune N and the per-run caps from measured cgroup data
  (`memory.peak`, the `cpu.stat` and pressure figures, queue wait), never below the owner's
  desktop floors.
- [ ] ⬜ **Task E.2**: Per-job weights. A job declares or learns its cost (for example, a Go
  `-race` run weighs 3 slots and a lint weighs 1), and the queue packs slots by weight, not by
  count.

## Success Criteria

- [ ] Five agent sessions asking for QA at once run at most N jobs at a time, and the owner's
  desktop stays responsive.
- [ ] A job's result names the commit it tested and matches a direct run of the same commands.
- [ ] Pausing the queue stops new jobs from starting within seconds.

## Delivery & Milestones

- Phase 1 research and design: RESEARCH-prior-art.md, DESIGN.md.
