# Plan 00263, Task 1.2: prior-art survey

Scope: many agent sessions in rootless podman containers on one Linux workstation each run
heavy QA. Stage 1 is a machine-wide limit (N slot locks plus kernel caps). Stage 2 is a job
queue with runners, clean checkouts from a TTL cache, results, logs, pause, resume and
cancel, at home and in a data centre.

Owner corrections folded in:

- Host sessions are not workers. They never run tests, QA or builds. So the slice and the
  lock cover containers only, and host sessions need a lane guard instead (section 9).
- The owner asked whether the cap can be held at container level, not by agents being
  honest about slots. Section 8 checks the kernel-enforced design.

Method: web search and fetch, `gh api` reads of public repositories, and reading this repo.
Nothing was installed, cloned, built or run. Each claim is marked **FACT** (with a source),
**INFERENCE** or **UNKNOWN**. Star counts and push dates are from `gh api` on 2026-11-23.

---

## 1. Counting semaphores and locks

### flock(1) and flock(2)

- What: takes an advisory lock on an open file; `flock -n` fails at once if it is held,
  `flock -w N` waits up to N seconds. FACT: <https://man7.org/linux/man-pages/man1/flock.1.html>
- The kernel drops the lock when the last file descriptor for that open file is closed,
  which includes the process dying. So there are no stale locks after a crash. FACT:
  <https://man7.org/linux/man-pages/man2/flock.2.html>
- Across a container boundary: a flock lock is attached to the open file on the inode, and a
  bind mount exposes the same inode from the same kernel. So a lock taken in one container is
  seen in every other container and on the host. INFERENCE (from flock(2); no source found that
  tests rootless podman directly). Proof is cheap: one test in the new repository.
- The lock file must sit in a bind-mounted directory, never in an image layer. Overlayfs may
  change which underlying inode a file is (copy-up), so two containers each "locking the same
  path" in their own overlay hold different locks. FACT on identity changing:
  <https://docs.kernel.org/filesystems/overlayfs.html>; the lock consequence is INFERENCE.
- NFS and other network file systems emulate flock with byte-range locks, with known edge
  cases. Not relevant on one machine. FACT: flock(2) man page above (NOTES section).
- Fit, stage 1: **strong.** This is the primitive. Licence: util-linux, GPL-2.0+.
  Maintained: yes.

### Slotted flock (N files, take any free one)

- Pattern: files `slot.1` to `slot.N`; try `flock -n` on each in turn; if none is free, wait
  and retry, or block on one at random. INFERENCE (a common shell idiom; no single canonical
  source).
- Gaps: no fairness (no first-come order), a waiter polling with sleep wastes wake-ups, and
  a blocked waiter on slot 3 does not notice that slot 1 became free. INFERENCE.
- Crib: hold the lock on a file descriptor that the heavy command inherits, so the slot is
  held exactly as long as the command tree lives. INFERENCE.
- Fit, stage 1: **this is the design**, with a small Go helper doing the polling fairly
  (for example a ticket file under one extra lock to give first-come order).

### GNU parallel `sem`

- What: a counting semaphore; `sem -j N --id name cmd` runs cmd when one of N slots is free.
  FACT: <https://www.gnu.org/software/parallel/sem.html>
- Stores its state as lock files under the user's `~/.parallel/semaphores`. INFERENCE from the
  docs and source; a container with a different home sees a different semaphore unless that
  directory is shared. It runs as a Perl program. Licence GPL-3.0+. Maintained: yes (monthly
  releases). FACT on licence and releases: <https://www.gnu.org/software/parallel/>
- Fit: workable for stage 1 if the semaphore directory is bind-mounted, but it brings Perl and
  GNU parallel into every image. Crib: the `--id` naming and the `--fg`/`--wait` split.

### lockrun, lckdo, "chg"

- lockrun: a small C wrapper for cron jobs that refuses to start a second copy, using a lock
  file. FACT: <https://unixwiz.net/tools/lockrun.html>. Only a mutex (N=1). Not maintained in any
  active sense. Superseded by flock(1). INFERENCE.
- lckdo (moreutils) is a similar wrapper; its own man page points to flock(1) instead. INFERENCE
  (from the moreutils docs as remembered; not re-fetched).
- "chg": UNKNOWN. No locking tool by this name was found; the best-known `chg` is Mercurial's
  command server client, which is unrelated.

### POSIX named semaphores

- What: `sem_open("/name")` creates a counting semaphore in `/dev/shm`. FACT:
  <https://man7.org/linux/man-pages/man7/sem_overview.7.html>
- A container gets its own `/dev/shm` by default, so sharing needs `--ipc=host` or a shared
  mount. INFERENCE from podman's IPC namespace defaults.
- Fatal gap: if a holder dies, the count is not given back. There is no owner and no
  automatic release. FACT: sem_overview(7) above (no robust mode exists for named semaphores;
  the "robust" option is only for pthread mutexes).
- Fit: **poor.** Stale counts after a crash are the exact failure we must avoid.

---

## 2. Job queues and spoolers

| Tool                       | What it does                                                                                                      | Licence                               | Maintained                                                                              | Container boundary                                                                                      |
| -------------------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| task-spooler (`ts`, `tsp`) | One queue per user; a server process runs jobs with a slot count (`-S N`); `ts -w`, `-c` to wait and read output. | GPL-2.0                               | Original is old; the GPU fork `justanhduc/task-spooler` (449 stars) last pushed 2024-03 | Socket in `/tmp` keyed by user; sharing needs the socket bind-mounted (`TS_SOCKET`).                    |
| nq                         | Each job is a file in a directory; a job waits on `flock` of all earlier job files. No daemon.                    | CC0 / public domain (gh: NOASSERTION) | 3137 stars, last push 2025-10                                                           | The queue is a directory, so a bind mount shares it; strictly serial (N=1).                             |
| pueue                      | A daemon with a client; groups, each with its own parallel limit; pause, resume, kill, logs, dependencies.        | Apache-2.0 (per gh)                   | 6438 stars, pushed 2026-11-18                                                           | Talks over a Unix socket or TCP with TLS and a shared secret, so a container can reach the host daemon. |
| at / batch                 | `batch` runs a job when the load average falls below a threshold.                                                 | GPL-2.0+                              | Maintained by distros                                                                   | Needs `atd`; no result return.                                                                          |
| systemd-run                | Starts a transient service or scope with resource properties and a `--slice`.                                     | LGPL-2.1+                             | Yes                                                                                     | Host only; a container would need the user bus.                                                         |
| Slurm                      | Cluster batch scheduler: partitions, queues, fair share, cgroup limits per job.                                   | GPL-2.0 (gh: NOASSERTION)             | 4536 stars, pushed 2026-11-21                                                           | Far too big for one laptop.                                                                             |
| HTCondor                   | High-throughput batch system, matchmaking of jobs to machines.                                                    | Apache-2.0                            | 339 stars, pushed 2026-11-23                                                            | Too big.                                                                                                |
| Nomad (batch jobs)         | Cluster scheduler; batch job type; podman driver; resource limits per task.                                       | BUSL-1.1 since 2023 (gh: NOASSERTION) | 17117 stars, pushed 2026-11-18                                                          | Agent runs on the host; jobs submitted over HTTP API.                                                   |

Sources (FACT): task-spooler <https://github.com/justanhduc/task-spooler> and
<https://viric.name/soft/ts/>; nq <https://github.com/leahneukirchen/nq>; pueue
<https://github.com/Nukesor/pueue>; batch <https://man7.org/linux/man-pages/man1/batch.1p.html>;
systemd-run <https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html>; Slurm
<https://github.com/SchedMD/slurm>; HTCondor <https://github.com/htcondor/htcondor>; Nomad
<https://github.com/hashicorp/nomad> and the licence change
<https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license>.

Notes per tool:

- **pueue** is the closest single fit to the stage 2 local queue: groups with parallel
  limits, pause and resume per group, cancel, logs, and a remote client. Gaps: it runs a
  shell command where the daemon lives; it has no idea of a repository, a commit, a clean
  checkout or a cache; and it has no allow-list of commands, so whoever can reach the socket
  can run anything as the daemon user. INFERENCE from its README.
- **nq** is worth cribbing for its design: a queue that is just a directory of lock files,
  where the job's order is its file name and "is it running" is "is its file locked". No
  daemon to crash. INFERENCE.
- **task-spooler** crib: `ts -S N` to change the slot count at run time, and a client that
  can block on a job and get its exit status. FACT: <https://viric.name/soft/ts/>
- **batch** crib: start only when load is below a threshold. A useful extra gate on top of
  slots, not a replacement. INFERENCE.
- **systemd-run** crib: a job is a transient unit, so CPU, IO and memory limits, kill,
  pause (`systemctl freeze`), logs (journal) and cleanup come free. FACT on `freeze`:
  <https://www.freedesktop.org/software/systemd/man/latest/systemctl.html>. This is the
  natural runner back-end for a host-side stage 2 runner.
- **Slurm, HTCondor, Nomad**: too heavy to adopt. Crib Slurm's fair-share by user (here, by
  agent seat) and Nomad's job spec shape. INFERENCE.

---

## 3. Build and CI runners

| Runner                            | Concurrency model                                                             | Licence                           | Maintained                       |
| --------------------------------- | ----------------------------------------------------------------------------- | --------------------------------- | -------------------------------- |
| GitHub Actions self-hosted runner | One job at a time per runner process; run several runners for more.           | MIT (actions/runner, 6415 stars)  | Yes, pushed 2026-11-23           |
| Buildkite agent                   | `--spawn N` runs N workers in one agent; queues by tag.                       | MIT (buildkite/agent, 1109 stars) | Yes                              |
| Woodpecker                        | Agent setting `WOODPECKER_MAX_WORKFLOWS` (parallel workflows per agent).      | Apache-2.0 (8271 stars)           | Yes, pushed today                |
| Drone                             | Runner `DRONE_RUNNER_CAPACITY`. Drone is now folded into Harness Open Source. | Apache-2.0 for the open parts     | Repository now `harness/harness` |
| GitLab runner                     | Global `concurrent`, per-runner `limit`, per-runner `request_concurrency`.    | MIT                               | Yes                              |
| Jenkins                           | A fixed number of executors per node.                                         | MIT                               | Yes                              |
| act                               | Runs GitHub Actions workflows locally in containers.                          | MIT (nektos/act, 72766 stars)     | Yes                              |

Sources (FACT): <https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/about-self-hosted-runners>;
<https://buildkite.com/docs/agent/v3/configuration>;
<https://woodpecker-ci.org/docs/administration/configuration/agent>;
<https://docs.drone.io/runner/docker/configuration/reference/>;
<https://docs.gitlab.com/runner/configuration/advanced-configuration/>;
<https://www.jenkins.io/doc/book/managing/nodes/>; <https://github.com/nektos/act>.

- Fit, stage 1: none. They are servers plus agents, and they want a forge in the loop.
- Fit, stage 2: **Woodpecker or Buildkite are real "adopt" candidates** if the queue may
  talk to a forge. A local Woodpecker server plus an agent with a max-workflows of N gives a
  queue, logs, cancel and a web UI. Gaps: an agent must push a commit somewhere the server
  can see; a pipeline file must live in each repository; and the result comes back through a
  web API, not as a local exit status. INFERENCE.
- Crib from all of them: the split of **server (queue) / agent (capacity)**; the agent
  pulling work (no inbound port); labels or tags to route jobs; the job log streamed and kept
  by ID. INFERENCE.
- Crib from GitLab: the `concurrent` versus per-runner `limit` split maps onto our "machine
  N" versus "per-repository" limits. FACT (docs above).
- act: crib the idea of running the same workflow file locally, so the queue's job and CI's
  job are one definition. Running act needs a Docker-compatible socket; podman's socket works
  in practice. INFERENCE.

---

## 4. Remote and cached build executors

- **Bazel Remote Execution API** (REAPI): a client sends an "action" (command plus a digest
  of its inputs); the server runs it on a worker and stores outputs in a content-addressed
  store (CAS); identical actions hit an action cache. FACT:
  <https://github.com/bazelbuild/remote-apis>
- **Buildbarn** (`bb-remote-execution`, Apache-2.0, 171 stars, pushed 2026-11-20) and
  **BuildGrid** (Apache-2.0) implement it, with schedulers, worker pools, and per-platform
  queues. FACT: <https://github.com/buildbarn/bb-remote-execution>, <https://buildgrid.build/>
- Fit: none directly. Our QA is not Bazel actions. But crib two ideas:
  - **Key the result by input digest.** A job is (repository, commit tree hash, command,
    tool versions). If the same key has passed, return the cached result. INFERENCE. The
    agent-specific write-up below reports 83 to 85 percent of checks served from such a cache.
    FACT: <https://dev.to/mattbusel/i-ran-20-ai-coding-agents-on-one-pc-the-bottleneck-was-the-compiler-6j7>
  - **Platform properties** route a job to a worker that can run it. INFERENCE.
- **Go `GOCACHEPROG`**: since Go 1.24 the go command can hand its build and test cache to an
  external program over a JSON protocol on stdin and stdout. FACT:
  <https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching> and the Go 1.24 release notes
  <https://go.dev/doc/go1.24>. Example: `saracen/gobuildcache` (MIT, 5 stars). Fit: a shared
  read-mostly Go cache across containers would cut repeated compile work, which is most of the
  load. A plain shared `GOCACHE` directory is the cheaper first step. INFERENCE: the Go cache
  is designed to be safe for concurrent use by several go commands, FACT:
  <https://pkg.go.dev/cmd/go#hdr-Build_and_test_caching>; across user namespaces the file
  owners must map, UNKNOWN until tested.

---

## 5. Merge queues and test schedulers

- **bors-ng**: archived 2024, Apache-2.0, 1527 stars; GitHub's own merge queue replaced it.
  FACT: `gh api repos/bors-ng/bors-ng` (archived: true) and
  <https://bors.tech/newsletter/2023/05/24/tmib-76/>.
- **GitHub merge queue**: builds a temporary branch of the base plus queued pull requests and
  runs CI on it; groups and a build concurrency setting. FACT:
  <https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue>
- **Mergify**: hosted merge queue with batching and speculative checks. FACT:
  <https://docs.mergify.com/merge-queue/>
- **Zuul**: speculative gating; tests each change as if all changes ahead of it had merged,
  with a window that grows and shrinks. Apache-2.0. FACT:
  <https://zuul-ci.org/docs/zuul/latest/gating.html>
- Fit: none for stage 1 or the stage 2 runner. Crib: **batching**. When five agents each ask
  for the full suite on near-identical commits, run once on the newest and report to all, or
  bisect only on failure (Zuul and Mergify both do this). INFERENCE. Also Zuul's
  **adaptive window**: shrink concurrency after failures. INFERENCE.

---

## 6. Load-aware throttles and cgroup v2

- **nice / ionice**: relative only, per process. `nice 19` still lets a job eat every idle
  core; the problem is contention, memory and IO, which nice does not bound. ionice only
  works with an IO scheduler that honours it (BFQ). INFERENCE; FACT on ionice needing a
  scheduler that supports classes: <https://man7.org/linux/man-pages/man1/ionice.1.html>
- **cgroup v2 `cpu.weight`**: 1 to 10000, default 100; CPU is shared among sibling groups by
  weight ratio and is work-conserving (idle share is lent out). FACT:
  <https://docs.kernel.org/admin-guide/cgroup-v2.html>
- **cgroup v2 `cpu.max`**: "$MAX $PERIOD" in microseconds; "max" means no limit. FACT: same.
- **`io.weight`** only works with the iocost model (off by default, enabled via
  `io.cost.qos`) or with the BFQ scheduler. FACT: same. On NVMe with the `none` scheduler,
  io.weight does nothing. INFERENCE. A Fedora user got "write io.weight: No such device" from
  crun in a rootless container. FACT:
  <https://discussion.fedoraproject.org/t/io-resource-limits-not-supported-in-rootless-containers/80669>
- **`memory.high`** throttles and reclaims, never kills; **`memory.max`** is hard and invokes
  the OOM killer in that group. FACT: kernel doc above; systemd advises MemoryHigh as the main
  control and MemoryMax as the last line. FACT:
  <https://www.freedesktop.org/software/systemd/man/latest/systemd.resource-control.html>
- **systemd CPUQuota / CPUWeight / IOWeight / MemoryMax**: unit properties that write the files
  above; weights compare only among siblings in a slice. FACT: resource-control page above.
- **podman `--cpus`, `--cpu-shares`, `--cpuset-cpus`, `--memory`, `--pids-limit`,
  `--blkio-weight`**: all documented; "not supported on cgroups V1 rootless systems", so
  supported on v2 rootless when the controller is delegated. FACT:
  <https://docs.podman.io/en/latest/markdown/podman-run.1.html>. On v2, `--cpu-shares` is
  converted to `cpu.weight`. INFERENCE (podman and crun do the conversion; not re-checked in
  source).
- Delegation and `podman update`: see section 8.

---

## 7. Agent-specific prior art

This problem is now common and several small tools exist. None is mature.

| Tool                                          | Mechanism                                                                                                                                                                                                                         | Licence | Signals                    | Container-aware                             |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- | -------------------------- | ------------------------------------------- |
| agent-throttle (`lucasrosati/agent-throttle`) | `solo` wrapper takes a slot; slot = `mkdir` lock dir with `pid` and `pid_start`; reaps dead owners by PID plus start time; watchdog timeout 20 min; a PreToolUse hook blocks test runs without a worker count or above a ceiling. | MIT     | 2 stars, pushed 2026-10    | No. PID checks break across PID namespaces. |
| agentgc (`JohnWayneeee/agentgc`)              | Rust CLI; a Claude Code hook rewrites heavy commands to `agentgc run --`; default slots = a quarter of logical cores; waiters in no fixed order.                                                                                  | MIT     | 0 stars, pushed 2026-11    | Not stated.                                 |
| agent-slots (`staterijndael/agent-slots`)     | JSON file under `flock`, no daemon; capacity follows free memory and load; dead or hung holders lose slots; a hook blocks direct heavy commands. macOS.                                                                           | MIT     | 0 stars, pushed 2026-11    | Not stated.                                 |
| Clearway (`AW-HTK/clearway`)                  | Machine-wide queue over MCP; measures RAM and CPU; grants slots in arrival order; a hook refuses heavy commands without a slot. Windows and WSL only.                                                                             | MIT     | 0 stars, pushed 2026-11-22 | WSL boundary only.                          |
| waitlet (`kriasoft/waitlet`)                  | Limits how many agents run at once, with a queue.                                                                                                                                                                                 | UNKNOWN | 0 stars                    | UNKNOWN                                     |

Sources (FACT): <https://github.com/lucasrosati/agent-throttle> and its
`docs/how-it-works.md`; <https://dev.to/ghost_wayne/queue-heavy-builds-for-parallel-claude-code-agents-3832>;
<https://github.com/anthropics/claude-code/issues/101193> (which names agent-slots and
Clearway); `gh api` for each repository.

- Anthropic's own tracker has an open feature request for exactly this: 11 agents on an
  11-core laptop pushed load to about 726; seven agents queued on one cargo lock turned a
  17-minute gate into over two hours. No maintainer reply. FACT: issue 101193 above.
- Crib from agent-throttle: **set the worker count once at slot acquisition** (it fixes
  W = min(ceil(C/2), floor(C/occupied_slots))) and pass it as tool flags (pytest `-n`, Jest
  `--maxWorkers`). FACT: its README. Also its **watchdog timeout that kills the whole tree**,
  and logging each run for a weekly report.
- Crib from agentgc and agent-slots: **a PreToolUse hook that blocks a heavy command not run
  through the wrapper**. This makes the slot rule enforced, not hoped for. INFERENCE.
- Crib from agent-slots and Clearway: **admission by live memory and load**, not only a fixed
  N. INFERENCE.
- Pitfall seen: `mkdir` plus PID files need stale-lock reaping, and PID checks are wrong
  across containers (each container has its own PID namespace). flock avoids both.
  INFERENCE from agent-throttle's own doc, which says containers are not covered.

---

## 8. Container-level enforcement (owner question: "is there a way to cap this at container level?")

Proposal under test: one parent slice holds every agent container; the slice has a total
CPUQuota, a low CPUWeight, an IOWeight and a MemoryMax; each container also gets `--cpus`,
`--cpuset-cpus`, `--memory` and `--pids-limit`; the slot lock becomes a fairness and
throughput aid, not the safety. Host sessions are out (owner correction).

### (a) Rootless delegation on Fedora 44

- Upstream systemd's `user@.service` ships `Delegate=pids memory cpu`. So **cpu, memory and
  pids are delegated; cpuset and io are not.** FACT:
  <https://github.com/systemd/systemd/blob/main/units/user%40.service.in>
- Fedora ships the upstream unit with no extra Delegate drop-in. INFERENCE (no Fedora drop-in
  found; one 2022 book says Fedora delegated only memory and pids then, which predates
  upstream adding cpu). Check on the machine: `cat /sys/fs/cgroup/user.slice/user-$(id -u).slice/user@$(id -u).service/cgroup.controllers`.
  UNKNOWN until read.
- Podman's troubleshooting guide gives the fix as a drop-in
  `/etc/systemd/system/user@.service.d/delegate.conf` with `Delegate=memory pids cpu cpuset`,
  then log out and in. FACT: <https://github.com/containers/podman/blob/main/troubleshooting.md>
  (entry 26). In this repository that drop-in must be an Ansible task. Adding `io` is possible
  but see the io.weight caveat in section 6.
- Without `cpuset` delegated, `--cpuset-cpus` fails with "the requested cgroup controller
  `cpuset` is not available". FACT: same entry.
- **`--cgroup-parent` into a user slice, rootless**: podman documents `--cgroup-parent` only as
  the parent path, with no rootless note. FACT: podman-run page above. With the systemd cgroup
  manager the parent is a slice name (for example `agents.slice`), created under the user's
  `user@.service`. INFERENCE. No source confirms it end to end for rootless. UNKNOWN; this is the
  first thing to prove in the new repository. The cgroupfs manager is a known fallback when
  the systemd manager misbehaves, but rootless cgroupfs cannot create groups outside the
  delegated subtree. FACT on the fallback: <https://bugzilla.redhat.com/show_bug.cgi?id=2156620>;
  the rest INFERENCE.
- In this repository today, ccy sets only `--pids-limit` (seen in
  `files/var/local/claude-yolo/lib/image-build.bash` and `lib/token-management.bash`); there is
  no Delegate drop-in, no `--cgroup-parent`, no `--cpus`. FACT (grep of the repo).

### (b) `podman update` on a running rootless container

- `podman update` can change `--cpus`, `--cpu-quota`, `--cpu-period`, `--cpu-shares`,
  `--cpuset-cpus`, `--memory`, `--memory-reservation`, `--pids-limit`, the blkio
  options and more. Most are "not supported on cgroups V1 rootless systems". FACT:
  <https://docs.podman.io/en/latest/markdown/podman-update.1.html>
- So on cgroup v2 rootless it should work for each delegated controller. INFERENCE.
- Whether the change survives a container restart: the page does not say. UNKNOWN.
- Use: the owner can tighten every running container at once (for example during a call)
  without restarting agents. INFERENCE. Changing the parent slice
  (`systemctl --user set-property agents.slice CPUQuota=...`) does the same in one step.
  INFERENCE.

### (c) Go's container-aware GOMAXPROCS

- From Go 1.25 the default GOMAXPROCS is the smallest of: logical CPUs, the CPU affinity mask,
  and the cgroup CPU quota divided by the period, rounded up. It never goes below 2 unless the
  CPU count or affinity mask is below 2. It is re-checked up to once a second. FACT:
  <https://pkg.go.dev/runtime#GOMAXPROCS> and <https://go.dev/blog/container-aware-gomaxprocs>
- So it reads **both** `cpu.max` (via the quota) **and** cpuset (via the affinity mask that
  `--cpuset-cpus` sets). The coordinator's note "cpu.max, not cpuset" is half right: cpuset
  counts too, through affinity. FACT (same page). It ignores `cpu.weight`. FACT: blog above.
- Whether it reads the quota of parent cgroups too (a quota only on `agents.slice`): UNKNOWN.
  The Go docs say "cgroup CPU quota", not which levels. Safer to set `--cpus` on each container,
  which is in the container's own cgroup. INFERENCE.
- Gating: the old behaviour (`containermaxprocs=0`, `updatemaxprocs=0`) is the default only for
  language version 1.24 and below. FACT: runtime doc above. Our modules: `src-go/go.mod` has
  `go 1.26.0` and `toolchain go1.26.11`; `src-go/qa/go.mod` has `go 1.26.10`. FACT (read). So
  **the new default applies to our binaries and test binaries.** INFERENCE (the go directive of
  the main module sets the GODEBUG defaults).
- `go test -p` and `go build -p` default to GOMAXPROCS of the go command itself, which is built
  by the 1.26 toolchain, so they auto-size too. INFERENCE (`-p` default is documented as
  GOMAXPROCS: <https://pkg.go.dev/cmd/go#hdr-Build_flags>; that the go command's own GOMAXPROCS
  follows the container rule is inference).
- `nproc` and Python's `os.process_cpu_count()` read the affinity mask, so `--cpuset-cpus`
  sizes them; a quota alone does not. INFERENCE. pytest-xdist `-n auto` therefore needs
  cpuset, or the explicit `-n` the wrapper exports.

### (d) Does a parent slice's CPUQuota bound every child; how does CPUWeight compare

- cgroup v2 resources are distributed top-down: a child can only use what its parent was
  given, and a delegatee cannot escape the parent's limits. FACT:
  <https://docs.kernel.org/admin-guide/cgroup-v2.html> (resource distribution and delegation
  sections). So a `cpu.max` on `agents.slice` bounds the sum of all containers under it, and a
  container cannot raise it. INFERENCE from those rules (CFS bandwidth throttles the parent's
  whole subtree); the systemd page does not state it for slices. FACT that it does not:
  resource-control page above.
- CPUWeight only compares siblings in the same parent. FACT: systemd resource-control page.
- Rootless containers sit inside `user@UID.service`, next to the desktop's own slices. Upstream
  user slices: `session.slice` CPUWeight=100, `app.slice` CPUWeight=100, `background.slice`
  CPUWeight=30. FACT: <https://github.com/systemd/systemd/tree/main/units/user> (read via gh).
  GNOME runs its shell and core services in `session.slice` and apps in `app.slice`. INFERENCE.
- So an `agents.slice` with CPUWeight=10 under the user manager gets about 10/(100+100+30+10)
  of the CPU when everything is busy, and all of it when the desktop is idle. INFERENCE.
- But a terminal or IDE the owner opens is in `app.slice` with weight 100, and so is a
  browser. Weight protects them only when they actually want CPU. The hard CPUQuota is what
  keeps cores free for latency (a compositor frame cannot wait for a 100 ms CFS period).
  INFERENCE.
- Memory: MemoryMax on `agents.slice` bounds the sum; the effective limit is the strictest of
  the unit and its parents. FACT: systemd resource-control page. Use MemoryHigh below it so the
  group slows before the OOM killer fires. FACT (same page advises this).
- Desktop protection also needs systemd-oomd watching `agents.slice` (ManagedOOMMemoryPressure)
  so a swap storm kills agent work first. INFERENCE.
- Conclusion for the owner's question: **yes, the cap can be held by the kernel at container
  level**, and an agent cannot opt out of it, provided the cpu (and for cpuset, cpuset)
  controller is delegated and the parent is a slice the agents do not control. The slot lock
  then only decides who waits. INFERENCE, with the `--cgroup-parent` step UNKNOWN until proved.

---

## 9. Keeping a host session to its lane (refusing tests, QA and builds)

- **Claude Code permission deny rules**: `Bash(go test *)` style rules; deny is checked before
  ask and allow, and an allow cannot carve an exception out of a deny. Deny rules match every
  subcommand of a compound command, inside subshells, command substitutions and loops, and
  past any leading variable assignment. FACT: <https://code.claude.com/docs/en/permissions>
- Limits: a deny rule "doesn't match the same program invoked in a different form", nor by
  path or inside `sh -c`; so `./scripts/qa-all.bash` or a Makefile target still runs unless
  named. FACT: same page. Managed settings can make the rules non-overridable. FACT: same page.
- **PreToolUse hooks** see the full command text and can refuse it with a reason; the docs
  recommend them for checks a pattern cannot express. FACT: same page and
  <https://code.claude.com/docs/en/hooks>. This repository already runs a hooks daemon with
  PreToolUse handlers, so a "host seat: no test/QA/build" handler fits there. INFERENCE.
- Similar guardrails: agent-throttle's `throttle-guard`, agentgc's rewrite hook and
  agent-slots' plugin all block heavy commands by text at PreToolUse. FACT: section 7 sources.
- Kernel backstop: put host agent sessions in a slice with a small CPUQuota anyway, so a
  command that slips past the text check cannot swamp the machine. INFERENCE.

---

## 10. Pitfalls others hit

01. **Stale locks after a crash.** `mkdir` and PID-file locks need reaping, and PID reuse makes
    it hard (agent-throttle checks PID plus start time). POSIX named semaphores never give a
    count back. flock is released by the kernel. FACT/INFERENCE as above.
02. **PID namespaces.** A PID written by one container means nothing in another. Any
    liveness check by PID fails across containers. INFERENCE.
03. **Lock file on overlay.** A lock path inside the image layer is a different file per
    container. Use a bind mount of one host directory. INFERENCE from overlayfs docs.
04. **Rootless file ownership.** The lock files must be openable by the mapped UID in every
    container; with `--userns=keep-id` this is the host user. INFERENCE.
05. **Delegation gaps.** cpuset and io are not delegated by default; io.weight needs iocost or
    BFQ; crun reports "No such device". FACT (sections 6 and 8).
06. **Weights do not protect latency.** cpu.weight is work-conserving; the desktop still waits
    for a busy core until the scheduler switches. Hard caps leave cores idle for the owner.
    INFERENCE.
07. **Tools that size by core count.** Before Go 1.25 and with tools that read `nproc`, a
    capped container still starts one worker per host core, then thrashes inside its quota.
    FACT for Go (blog above); INFERENCE for others.
08. **Memory, not CPU, kills the machine.** agent-throttle's author and issue 101193 both
    report swap and load spikes from validation. FACT: sources in section 7.
09. **Shared tool locks serialise silently.** Seven agents on one cargo lock: a 17-minute gate
    took two hours. A queue that hides waits makes this worse; show the wait. FACT: issue 101193.
10. **No fairness.** agentgc's waiters run "in no fixed order"; a polling slotted flock is the
    same. A starving agent retries forever. FACT for agentgc (dev.to post); INFERENCE otherwise.

---

## 11. Ranked shortlist, stage 1

1. **Kernel caps first**: an `agents.slice` (CPUQuota, low CPUWeight, MemoryHigh and
   MemoryMax, later IOWeight) as each container's `--cgroup-parent`, plus per-container
   `--cpus`, `--cpuset-cpus`, `--memory`, `--pids-limit`, and a Delegate drop-in for cpuset.
   Enforced, cannot be skipped. Pending proof of rootless `--cgroup-parent`.
2. **Slotted flock** on N files in one bind-mounted directory, through a small Go wrapper that
   gives first-come order, exports the slot's share (GOMAXPROCS, `-p`, `pytest -n`), and logs.
3. **A PreToolUse hook** (in the existing hooks daemon) that refuses heavy commands not run
   through the wrapper, and refuses them outright on host seats.
4. GNU parallel `sem` with a shared semaphore directory: workable fallback, not preferred.
5. agent-throttle or agentgc: crib, do not adopt (tiny, single-author, not container-aware).

## 12. Ranked shortlist, stage 2

1. **Build a small Go queue whose runners start each job as a systemd transient unit**
   (`systemd-run --user --slice=agents.slice`), giving limits, freeze/thaw for pause, kill,
   and journal logs for free.
2. **pueue** as a stop-gap or as the runner core: groups with parallel limits, pause, resume,
   kill, logs, remote client. Missing: repositories, commits, checkout cache, command allow-list.
3. **Woodpecker** (server plus agent, Apache-2.0) if a forge-shaped flow with a web UI is
   acceptable, especially for the data-centre side.
4. Buildkite agent (MIT agent, but a hosted control plane) or GitHub self-hosted runners
   (one job per runner): fine for the data centre, too heavy and too remote for the laptop.
5. Nomad batch, Slurm, HTCondor: no; too big, and Nomad's licence is BUSL.

## 13. Ideas worth cribbing

- nq: the queue as a directory of lock files; order by file name; no daemon to crash.
- task-spooler: change the slot count at run time; a client that blocks for the exit status.
- pueue: groups with their own parallel limit; pause per group; a remote client with a secret.
- agent-throttle: fix the worker count when the slot is taken; kill the whole tree on timeout;
  weekly usage report.
- agentgc and agent-slots: a hook that rewrites or blocks heavy commands, so agents need not be
  honest; admission by live free memory and load.
- Bazel REAPI and Go's cache: key a result by (tree hash, command, tool versions) and return a
  cached pass; share the Go build cache (`GOCACHE`, later `GOCACHEPROG`).
- Merge queues and Zuul: batch identical requests on near-identical commits; shrink concurrency
  after failures.
- CI runners: server and pulling agent split; labels for routing; logs kept by job ID.
- `batch`: start only below a load threshold, as an extra gate.
- systemd: transient units, `freeze`/`thaw` for pause, oomd on the agents slice.

## 14. Recommendation: wrap and build small

- **Stage 1: build, thin.** No existing tool covers rootless containers with a kernel cap.
  The pieces are standard (flock, systemd slices, podman flags, the existing hooks daemon). The
  new code is a small Go wrapper (slot, share export, log) and the Ansible for the slice, the
  Delegate drop-in and the ccy run flags. Make the kernel caps the safety and the slot the
  fairness.
- **Stage 2: build the queue, wrap systemd for execution; keep pueue and Woodpecker as the
  "adopt" alternatives** to weigh in Task 1.3. What none of them gives is the part that matters
  most here: a job tied to a repository and a commit, run in a clean cached checkout, with an
  allow-list of commands, and a result keyed for reuse.
- First proofs for the new repository (none run here): flock across two rootless containers
  through a bind mount; rootless `--cgroup-parent=agents.slice` under the user manager; the
  Delegate drop-in giving cpuset; `podman update --cpus` on a running rootless container; and
  GOMAXPROCS inside a container with a quota only on the parent slice.
