# Plan 00263, Task 1.3: design

Inputs: the owner's three answers (PLAN.md, Task 1.1) and the survey
([RESEARCH-prior-art.md](RESEARCH-prior-art.md), cited below as "survey §N"). Staged as the owner
chose (option 3): kernel caps first (1a), then slots (1b), then the host seat's lane guard, then
the queue (stage 2).

Marks: **FACT** (a source or `file:line`), **INFERENCE**, **UNKNOWN**. Line numbers are at the
plan branch's tip. Each piece says where it lives: **qa-queue**, the repository
`LongTermSupport/agentic-qa-queue` (called qa-queue below for short; its commands are `qa-slot` and
`qa-queue`), or **fedora-desktop** (fd).

**Naming convention (owner):** every AI and workflow tool under LongTermSupport takes the
`agentic-` prefix in its repository name.

Terms used throughout: **C** is the machine's logical CPU count (threads), **RAM** its memory,
**N** the number of slots (owner decision D1: computed per machine, one per 12 agent threads,
rounded up; section 6). The desktop keeps hard floors, **F_cpu = 4
threads** and **F_mem = 16 GB** (owner, on D2); the rest is shared by the agents. Every value is
computed from the host's own facts at deploy time, never hard-coded.

---

## 1. Stage 1a: kernel caps (the safety)

The kernel holds the cap, whatever an agent runs. This answers the owner's second Q62 answer.
INFERENCE (survey §8d): a delegatee cannot raise its parent's limits.

### 1.1 The parent slice: `agents.slice`

- A user unit under the owner's `user@.service`, beside `app.slice` and `session.slice`.
  Every ccy container goes in it through `podman run --cgroup-parent=agents.slice`. Host
  sessions stay out (owner, third answer).
- Limits (values are owner decision D2; the formula follows the owner's 100/(N+1)):

| Property                                     | Value                                | Why                                                                                                                                                                                                                                         |
| -------------------------------------------- | ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `CPUQuota`                                   | `(C − F_cpu) × 100%`                 | The agents share everything above the desktop's floor. systemd counts 100% as one CPU (FACT: systemd.resource-control).                                                                                                                     |
| `AllowedCPUs`                                | all threads but `F_cpu` (4) kept     | Keeps whole threads free, so the compositor never waits for a CFS period (survey §8d, INFERENCE). Needs `cpuset` delegated (§1.4). On a hybrid CPU, which threads to reserve is UNKNOWN until the probe reads the topology.                 |
| `CPUWeight`                                  | `10`                                 | Yields to `app.slice` and `session.slice` (both 100) when they want CPU; work-conserving when they don't (FACT, survey §8d).                                                                                                                |
| `MemoryHigh` / `MemoryMax`                   | a little below / `RAM − F_mem`       | Max keeps the desktop's 16 GB; High (for example Max − 1 GB) throttles and reclaims first (FACT, survey §6). A fixed percentage was rejected: on a machine with plenty of RAM it only wastes it (owner).                                    |
| `TasksMax`                                   | `N+2` × the per-container pids limit | A fork bomb across containers stops in the slice, not at the user's limit. INFERENCE.                                                                                                                                                       |
| `IOWeight`                                   | **not set**                          | Does nothing on NVMe with the `none` scheduler, and crun refuses `io.weight` rootless ("No such device") (FACT, survey §6). The bet is that the CPU cap limits IO enough (owner). The probe reads the scheduler; revisit only if it is BFQ. |
| `io.max` (`IOReadBandwidthMax` and the like) | **not set; an optional probe item**  | A hard bandwidth or IOPS cap works without a scheduler. The probe checks whether the `io` controller is delegated rootless; use it only if the CPU caps prove not to be enough. INFERENCE.                                                  |

- systemd-oomd is **not** turned on for the slice, against survey §8d. This repo deliberately
  set `ManagedOOMMemoryPressure=auto` on `user.slice` because PSI-based killing took out
  containers with free RAM (FACT: `playbooks/imports/play-systemd-user-tweaks.yml:57-79`,
  `:139-156`). MemoryHigh plus the kernel OOM killer inside the slice is enough. INFERENCE.
- Changing a limit is one `systemctl --user set-property agents.slice ...`, which reaches every
  running container under it. That is the owner's lever during a call (INFERENCE, survey §8b).
  The persistent value is set by the play, never by hand.

### 1.2 Per-container flags

Every ccy container gets an equal share of what is above the desktop's floors (owner, on D2;
this replaces the first formula of 100/(N+1)%):

- `--cpus=(C − F_cpu)/N`: writes the container's own `cpu.max`, which Go 1.25+ reads for
  GOMAXPROCS. Our modules are on go 1.26, so Go builds and tests size themselves (FACT, survey
  §8c).
- `--memory=(RAM − F_mem)/N`: the slice still bounds the sum.
- `--pids-limit`: unchanged, as today (`files/var/local/claude-yolo/lib/container-limits.bash:9-24`).
- `--cpuset-cpus`: **not per container.** Pinning each container to (C − F_cpu)/N named threads puts
  two busy containers on the same cores while others sit idle. The slice's `AllowedCPUs` does
  the core reserve instead. INFERENCE. The cost: `nproc` and pytest `-n auto` inside the
  container still see the slice's CPUs, so the slot wrapper exports the worker count (§2.3).

### 1.3 Where ccy builds its `podman run`, and how the caps get there

FACTS from the code:

- The session container starts at `files/var/local/claude-yolo/claude-yolo:3803-3810`
  (`ccy_agent_container run $DOCKER_FLAGS --rm --pids-limit "$CCY_PIDS_LIMIT" ...`), through
  `ccy_agent_container` (`lib/agent-choice.bash:87-99`).
- `ccy_container_limits` settles the pids limit (`claude-yolo:101`,
  `lib/container-limits.bash:18-24`).
- The Go front does not start the session; it hands every other run to the bash launcher
  (`src-go/internal/ccylaunch/front.go:15-20`, rule F2). It keeps a copy of the pids bounds
  (`src-go/internal/ccylaunch/pidslimit.go:11-47`).
- Short helper containers in Go pass `--pids-limit` too: `src-go/internal/engine/engine.go:163`,
  `:188`, `:201`, `:210`. So does `ccc`, a second session launcher:
  `src-go/internal/ccc/run.go:53`, `:85`.
- Helper-built mount arguments already exist as a pattern: `ccy-teams internal go-cache`
  prints the Go cache mounts (`claude-yolo:2488-2489`).
- `claude-yolo` is on the bash-limits ratchet, so it may not grow (CLAUDE.md, "Tooling Is Go").

Design (fd):

- A Go helper, `ccy-teams internal container-limits`, reads the host's limits file (written by
  the play: N, the budget) and the machine (C, RAM), and prints one argument per line:
  `--cgroup-parent=agents.slice --cpus=... --memory=... --pids-limit=...`. It refuses a missing
  or bad file with a message naming the play. Same shape as the go-cache helper.
- `lib/container-limits.bash` fills an array `CCY_LIMIT_ARGS` from it. Each
  `--pids-limit "$CCY_PIDS_LIMIT"` in the launcher becomes `"${CCY_LIMIT_ARGS[@]}"`. Same line
  count, so the ratchet holds. INFERENCE (the gate counts code lines and statements).
- Go: `engine` and `ccc` take the same arguments from one Go function. `pidslimit.go` becomes
  `limits.go`.
- **Defence Before Fix:** this is a class ("a ccy container started outside the agents slice
  or without a CPU and memory cap"). Extend the existing pair of rules that hold the pids limit,
  the semgrep rule `ccy-container-default-pids-limit` (`.semgrep/bash-conventions.yml:292`; the
  file has an uncommitted change in this worktree, so the line may move) and the Go analyzer
  `src-go/qa/rules/containerpids.go:31`. Prove them red on `claude-yolo:3803` and
  `ccc/run.go:53`, then fix every hit.
- The ccy version bump (`claude-yolo:17`) is required.

### 1.4 Ansible for cgroup delegation (fd)

- Upstream `user@.service` delegates `pids memory cpu`, not `cpuset` or `io` (FACT, survey
  §8a). Whether Fedora 44 adds more is UNKNOWN; the probe reads it.
- Drop-in `/etc/systemd/system/user@.service.d/delegate.conf`, `Delegate=pids memory cpu cpuset`
  (FACT: podman's troubleshooting guide, entry 26). Owner: **`play-podman.yml`**, since it is
  podman's need. It takes effect at the next login. The play says so and fails a check that
  reads `cgroup.controllers` until it does (probe, then fail; fail fast).
- The `agents.slice` unit file under the user's systemd directory, and the limits file the
  helper reads: **`play-claude-yolo.yml`**, which owns ccy. Not `play-systemd-user-tweaks.yml`:
  that play holds machine-wide user-session fixes, and this slice exists only for ccy.
  INFERENCE on the ownership split.

### 1.5 How running sessions get the caps

- The per-container caps: `podman update --cpus ... --memory ...` on each container labelled
  `ccy=true` (`claude-yolo:3805`). Documented for v2 rootless (FACT, survey §8b). Done by a
  plan-local script, run through meta-deploy.
- The slice: **only at the next launch.** `podman update` has no `--cgroup-parent`, so a
  running container cannot be moved under the slice. INFERENCE from the option list in survey
  §8b. Sessions move as they restart (ccy's own restart request, `lib/restart-request.bash`).
- Whether `podman update` survives a container restart is UNKNOWN; it does not matter, since a
  restarted ccy container is a new launch with the new flags.

### 1.6 The first proof: rootless `--cgroup-parent` into a user slice

UNKNOWN today (survey §8a). Two plan scripts in this plan folder, built on
`_planlib.inc.bash`. They are run on the host by the host seat through `meta-deploy.bash`, in a
window the owner picks. Nothing else in stage 1a is built until both pass.

**Round 1, `triage.bash` (read-only).** Prints, and fails on the first unreadable source:

1. `cgroup.controllers` and `cgroup.subtree_control` of `user@$UID.service`: is `cpuset` there?
2. `podman info`: cgroup version and cgroup manager (must be v2 and systemd).
3. Any existing `user@.service.d` drop-ins (`systemctl cat user@.service`, no pager).
4. `C`, RAM, CPU topology (`lscpu -e`), and each disk's `queue/scheduler`.
5. `systemctl --user show app.slice session.slice -p CPUWeight` (the siblings' weights).

Added to `CLAUDE/Plan/meta-deploy.list` alone. Its output decides whether round 2 needs the
Delegate drop-in first.

**Round 2, `acceptance.bash` (minimal, mutating, reversible).** Added to the list only after
round 1, in the owner's window. It uses a throwaway slice and the ccy image already present
(`--pull=never`), so nothing is downloaded:

1. Set runtime-only properties on `qa-proof.slice` (`set-property --runtime CPUQuota=100% MemoryMax=512M`). Runtime only, gone at the end or at a reboot; a test fixture, not
   configuration.
2. `podman run --rm --cgroup-parent=qa-proof.slice --cpus=1 --memory=256m --pids-limit=256`
   reading `/proc/self/cgroup` and its `cpu.max`. Pass: the path lies under
   `user@$UID.service/qa-proof.slice`, and `cpu.max` reads `100000 100000`.
3. Two such containers each spin on two cores for a few seconds. Pass: the slice's
   `cpu.stat` shows `nr_throttled` rising and total usage at about one CPU.
4. Inside one, a Go one-liner prints `runtime.GOMAXPROCS(0)`. Records whether a quota on the
   slice alone is seen (survey §8c UNKNOWN).
5. A `podman update --cpus=0.5` on a running proof container; read `cpu.max` again.
6. Stop the slice; confirm no proof container is left. Fail loudly at any step.

If step 2 fails under the systemd manager, the fallback is a slice created first
(`systemd-run --user --slice`) or the cgroupfs manager inside the delegated subtree. Both are
UNKNOWN, and either would be a design change brought back to the owner.

---

## 2. Stage 1b: slots (fairness)

The caps keep the machine usable. The slots decide who waits, so N heavy jobs run fast instead
of many running slowly.

### 2.1 The slot directory

- A host directory with `slot.1` to `slot.N`, plus `turnstile`. Made by
  `play-claude-yolo.yml` from N (fd). Changing N is changing the play's variable.
- Mounted into every container by a second helper line (the go-cache pattern), **read-only**.
  `flock(2)` takes a lock "regardless of the mode in which the file was opened" (FACT: flock(2)
  man page), so a read-only mount is enough. A container cannot delete or add a slot.
  INFERENCE: that is stronger than the read-write mount the brief assumed.
- One inode for every container, because it is a bind mount, never an image path (survey §1,
  overlay pitfall). The lock crossing containers is INFERENCE until the qa-queue test proves it.

### 2.2 `qa-slot run -- <cmd>` (qa-queue, Go)

1. If `QA_SLOT` is set, it already holds a slot: exec the command (no nested deadlock).
2. Take `turnstile` with a blocking flock. Holding it, try each slot with a non-blocking flock,
   then retry once a second. This lets one waiter in at a time. Kernel flock waiters are not
   promised to be first-come (INFERENCE), but nobody starves while slots free up.
3. Waiting: a line to stderr at once and each minute ("all N slots busy, waited 3m"). Gives up
   after `--wait` (default 30m) with exit 75 and a message that names the wait and the slot
   count. Shows the wait (survey §10, pitfall 9).
4. Holding: release the turnstile; export `QA_SLOT=i`, `QA_SLOTS=N`, `QA_JOBS=k` (k = the
   container's CPU quota, rounded down, at least 1), `GOMAXPROCS=k`, `GOFLAGS=-p=k` (added to
   any existing GOFLAGS), `PYTEST_XDIST_AUTO_NUM_WORKERS=k` (INFERENCE: read by pytest-xdist
   `-n auto`; checked in its test). Then run the command in its own process group, with the
   slot's file descriptor inherited (survey §1 crib).
5. `--max-run` (default 60m) kills the whole process group (agent-throttle crib). SIGINT and
   SIGTERM go to the group. Exit status is the command's.
6. The kernel frees the slot when the last descriptor closes, crash included. No reaping, no PIDs.
7. One line per run to a log in the slot directory's sibling state dir: slot, waited, ran,
   exit. For a weekly report later (YAGNI until asked).

### 2.3 Install and wiring

- **Image (fd):** the Dockerfile (`files/var/local/claude-yolo/Dockerfile`) fetches a pinned
  qa-queue release and checks its sha256, as other pins are (the `update-versions` skill covers
  the bump). ccy version bump.
- **`qa-all.bash` and `qa-changed.bash` (fd):** each wraps itself before it builds its Go tool,
  because the build is heavy too (`scripts/lib/go-tool.bash:93`). One line before
  `run_go_tool` (`scripts/qa-all.bash:30`, `scripts/qa-changed.bash:26`):
  `[ -n "${QA_SLOT:-}" ] || exec qa-slot run -- "$0" "$@"`. `qa-slot` missing fails with
  command-not-found, which is the wanted loud failure.
- **GitHub CI (fd):** `.github/workflows/qa.yml` sets `QA_SLOT=ci`, an explicit, named opt-out:
  the runner is not a shared machine. Not a skip-if-absent.
- **The guard (fd, then upstream):** a hooks-daemon PreToolUse handler refuses a known-heavy
  command (`go test`, `go build`, `go vet`, `golangci-lint`, `semgrep`, `pytest`,
  `ansible-lint`, `ansible-playbook --syntax-check`, `scripts/qa-*.bash` called directly by
  a gate, `npm test`) unless the command line starts with `qa-slot run --`. `qa-all.bash` and
  `qa-changed.bash` are allowed, as they wrap themselves. It sits beside the existing project
  handlers in `.claude/hooks/handlers/pre_tool_use/` (for example `ansible_enforcement.py`,
  which already judges raw command text through `_raw_command_judge.py`), tests first in
  `tests/`. It matches text, so a script that runs `go test` inside gets past it. That is
  accepted: the kernel caps are the safety; the guard only steers honest work into the slots.
  Since qa-queue is general, the handler moves upstream to hooks-daemon as a configurable
  library handler once it has settled. INFERENCE.

---

## 3. The host seat's lane (owner decision D3)

Host seats run deployments and desktop triage, never tests, QA or builds (owner, third
answer).

**FACTS on today's state:**

- `cc` passes a settings file with deny rules only on its teams path:
  `files/var/local/claude-code/cc-teams.bash:42` (`cc-teams-settings.json`), merged at `:126`,
  passed at `:145`, and only when teams are given (`files/var/local/claude-code/cc:193-196`).
  A plain `cc` gets none. `cc-claude` only sets environment defaults
  (`src-go/internal/ccclaude/app.go:1-19`).
- The file's own comment says these rules are "instruction, not containment"
  (`cc-teams.bash:28-33`).
- Deny rules miss the same program in another form, by path, and inside `sh -c`. So
  `./scripts/qa-all.bash` runs unless named (FACT, survey §9).

| Option                                   | For                                                                                                                                                          | Against                                                                                                                                         |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| A. hooks-daemon handler, host seats only | Sees the whole command text, so it catches `sh -c '...'`, paths and script names. Gives a reason ("host seats do not run QA; ask a container seat"). Tested. | Per project: covers only projects whose daemon carries it until it is upstream. How the handler knows it serves a host seat is UNKNOWN (below). |
| B. cc deny rules                         | Cheap; one JSON file cc already owns; applies in any project.                                                                                                | Pattern-only gaps above; today only on the teams path, so plain `cc` would need the settings file too.                                          |

**Recommendation: A**, written as a library handler in hooks-daemon so every project gets it,
switched on by config. Add **B** only as a cheap second layer for the obvious forms (`go test`,
`pytest`, `semgrep`), passed on every cc launch, not just teams. Neither is containment; the
owner chose to leave host seats outside the slice. INFERENCE.

**UNKNOWN to settle first:** the handler needs a trustworthy "this is a host seat" signal.
Candidates: an environment variable cc exports (cc already knows the member type,
`cc-teams.bash:45` sets `CC_TEAMS_MEMBER_TYPE="host"`), if the daemon's handler can see the
session's environment; or the absence of `/run/.containerenv`, if the daemon serving the hook
runs in the same namespace as the session. Which process the handler runs in, for a host and a
container session on the same checkout, is the first thing the guard's task establishes.

---

## 4. Stage 2: the queue

### 4.1 The job

The agent names **what** to run; the host decides **how**. A job (JSON, versioned):

```json
{
  "version": 1,
  "repo": "github.com/<owner>/<repo>",
  "commit": "<40-hex sha>",
  "recipe": "qa-changed",
  "args": ["--base", "<40-hex sha>"],
  "submitter": "<seat handle>"
}
```

- `recipe` names an entry in the host's per-repository config, never a command line. A recipe
  holds `pre` (argv list), `run` (argv list), `post` (argv list, runs whatever the result),
  `timeout`, the image, and an allow-list of `args` patterns. INFERENCE: free command text from
  an agent would make the queue a remote shell on the host.
- `commit` must be fetchable from the repository's remote. The runner never reads an agent's
  working tree. The repo's own rule is to push every commit at once (CLAUDE.md, "Always Push"),
  so this costs nothing new. An unknown commit is refused with "push it first".

### 4.2 Submitting: a spool directory

- Each container gets its own read-write subdirectory of a host spool, mounted by the same
  helper pattern: `spool/<container>/{tmp,new,done}`. It cannot see another container's jobs.
- Submit = write to `tmp/`, then `rename` into `new/` (atomic in one directory: the maildir
  pattern). The client `qa-queue submit --wait` then waits on `done/<id>.json`.
- Why a spool, not a socket or the bus:
  - It survives a runner restart; a socket needs the runner up at submit time.
  - The directory is the identity: the runner knows which container wrote a job without any
    auth protocol. A socket in a shared mount would need a secret, as pueue does.
  - Nothing new to secure beyond file permissions; the same mount mechanism as the slots.
  - The team bus (pingbus) is for agents talking, not for work; it would couple the queue to
    teams. INFERENCE throughout.

### 4.3 The runner (qa-queue, Go)

- A systemd user service on the host. It watches each `spool/*/new` (inotify, plus a periodic
  scan) and validates every job against the allow-list (§4.5). A refused job gets a `done`
  file saying why.
- Admission: it takes a slot from the **same** slot directory as stage 1b, so queued jobs and
  direct `qa-slot` runs share one N. Coalescing: a job equal to one queued or running
  (repository, commit, recipe, args) joins it, and both submitters get the result (merge-queue
  crib, survey §5).
- Checkout: one bare mirror per repository in a cache directory, fetched per job; a detached
  `git worktree` per job at the commit. Removed after a pass; kept for a TTL (default 24h) after
  a fail, for inspection. Mirrors unused for a TTL (default 7d) are removed. Per-repository
  build caches (`GOCACHE`) are kept, as the Go cache is safe for concurrent use (FACT, survey §4).
- Execution: each job is a **systemd transient unit** in `agents-qa.slice` (a child of the
  agents slice), started over D-Bus. `RuntimeMaxSec` is the recipe's timeout. The unit runs the
  recipe **inside a container** from the recipe's image, with only the worktree and the build
  cache mounted. No tokens, no SSH, no home. Limits, freeze, kill and journal logs come from the
  unit (survey §2); isolation comes from the container. The podman mode that puts the container
  under the unit's own cgroup (`--cgroups=split`) is UNKNOWN for rootless and is proved first.
- Result cache: a pass is keyed by (tree hash, recipe, args, image digest). The same key again
  returns the cached result, marked `cached` (Bazel crib, survey §4).

### 4.4 Results, logs, pause, resume, cancel

- `done/<id>.json`: id, repository, commit, recipe, exit status, start, end, `cached`, and the
  log's path. The log is kept on the host by job id and copied into `done/<id>.log`, so the
  container can read it.
- Owner command `qa-queue`: `status` (queue, running, waited), `pause` (admit nothing new),
  `pause --freeze` (also `systemctl --user freeze` the running units; FACT, survey §2),
  `resume`, `cancel <id>` (drop it if queued, stop the unit if running).
- Success criterion "pausing stops new jobs within seconds": admission checks the pause flag
  before every start. INFERENCE.

### 4.5 Security

- **Allow-list (host-only file, written by the play):** the repositories, and per repository
  the recipes. A job naming anything else is refused. Containers cannot write it.
- **Code is still untrusted.** A recipe runs the commit's own scripts, written by agents. So:
  inside a container, nothing secret mounted, network as ccy's network policy allows.
- **The user jobs run as:** recommended, a dedicated system user (`qa-runner`) with its own
  rootless podman. A container escape then lands in an account that owns nothing of the
  owner's. Its whole `user-<uid>.slice` can be capped by a **system** drop-in, which needs no
  user-level delegation. The runner then takes the shared slots through a group-readable slot
  directory. INFERENCE. The cheaper first step is the owner's own user plus the container; the
  owner decides inside D4.

### 4.6 Build, or adopt pueue or Woodpecker (owner decision D4)

| Choice                                         | Gives                                                                           | Missing for us                                                                                                                                                                                                         |
| ---------------------------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **pueue** (Apache-2.0, active; survey §2)      | Groups with parallel limits, pause, resume, kill, logs, a remote client.        | No repository, commit, checkout cache or allow-list; anyone who reaches its socket runs any command as the daemon's user. A validating front would have to be written, and that front is most of the queue. INFERENCE. |
| **Woodpecker** (Apache-2.0, active; survey §3) | Server plus agent with a max-workflows limit, logs, cancel, a web UI.           | Needs a forge-shaped flow: a pipeline file in each repository, a server that sees the commit, results through a web API rather than an exit status. Heavy for a laptop. INFERENCE.                                     |
| **Build small** (qa-queue)                     | Exactly §4.1 to §4.5: spool, allow-listed recipes, mirror cache, systemd units. | Ours to maintain. The parts are standard (flock, inotify, git, systemd's D-Bus API).                                                                                                                                   |

**Recommendation: build small.** The value is in what neither tool has: a job tied to a
repository and a commit, run in a clean cached checkout, from an allow-list, with a reusable
result. Woodpecker is not needed: the data centre server already acts as a GitHub runner, and
no ad hoc QA runs there (owner).

### 4.7 The runners: the laptop and the home server

- One binary, one design. Config differs: N, the floors, the allow-list.
- The laptop: N small, the desktop's floors matter, pause is used.
- The home server: no desktop to protect, so N is larger and the floors can be small. A
  headless machine has no login session, so the runner's user needs lingering (`loginctl enable-linger`, by a play) for its user manager to run. INFERENCE.
- Not the data centre (owner): it already acts as a GitHub runner, so it gets no queue.
- Not distributed (PLAN.md non-goal): each machine has its own spool for its own containers.
  Submitting from one machine to another is a later question.

---

## 5. Phases and tasks

Test first throughout. "qq" is qa-queue, "fd" is fedora-desktop.

**Phase A: stage 1a, caps**

1. fd: `triage.bash` (round 1, §1.6); meta-deploy entry. Record the output in JOURNAL.
2. fd: `acceptance.bash` (round 2, §1.6), in the owner's window. Gate: all steps pass.
3. fd: Delegate drop-in in `play-podman.yml`, with its read-back check (if round 1 shows
   `cpuset` missing).
4. fd: the rules first. Extend `ccy-container-default-pids-limit` and `containerpids.go`
   to require the slice and the caps; prove them red on today's runs.
5. fd: the `container-limits` helper (Go, tests first); `CCY_LIMIT_ARGS` in
   `lib/container-limits.bash`; every run site from §1.3; the rules turn green. Version bump.
6. fd: `agents.slice` and the limits file in `play-claude-yolo.yml`.
7. fd: a plan script applying `podman update` to running `ccy=true` containers.
8. fd: acceptance: two containers under load, slice throttled, the owner says the desktop
   stays usable.

**Phase B: stage 1b, slots**

1. qq: create the repository and its CI (PLAN Task 2.1, brought forward).
2. qq: `qa-slot`, tests first: lock across processes, turnstile, wait and its timeout,
   re-entry, group kill on `--max-run` and on signals, exit status passed through, the exports.
3. qq: a cross-container test (two rootless containers, one bind-mounted directory). Release.
4. fd: slot directory in `play-claude-yolo.yml`; read-only mount through the helper; pinned
   install in the Dockerfile; version bump.
5. fd: self-wrap in `qa-all.bash` and `qa-changed.bash`; `QA_SLOT=ci` in `qa.yml`.
6. fd: the heavy-command PreToolUse handler, tests first.

**Phase C: the lane guard**

1. fd: establish the host-seat signal (§3 UNKNOWN).
2. hooks-daemon (upstream) or fd project handler: the host-seat refusal, tests first.
3. fd (only if D3 includes B): deny rules on every cc launch.

**Phase D: stage 2, the queue**

1. qq: job and recipe formats, validation, tests first.
2. qq: spool reader, admission through the shared slots, coalescing.
3. qq: mirror cache and TTLs; transient units with a container inside (prove `--cgroups=split`
   rootless first); results, logs; the `qa-queue` owner command.
4. fd: a play installs the pinned runner, its user (per D4), its service, the allow-list; the
   spool mount in ccy (version bump).
5. fd: `qa-changed.bash` and the agents' QA instructions submit through the queue (PLAN
   Task 3.2).

---

## 6. Owner decisions

| #   | Decision                    | Recommended                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| --- | --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | N                           | **Owner's answer (Q64, relayed by the coordinator):** no fixed N; "1 run every 12 cores, rounded up", from each machine's own resources at install time. As specified: `N = max(1, min(ceil(agent_cpus / 12), floor(agent_ram / per_run_peak_ram)))`, where `agent_cpus = C − F_cpu` (no CPU floor on a headless server) and `agent_ram = RAM − F_mem`. The memory term is the coordinator's addition and is **provisional** until the owner confirms it. Per-run caps are the agents' share divided by N, as in D2. |
| D2  | Total CPU and memory budget | **Owner's answer (floors):** the desktop keeps 4 threads and 16 GB. The slice gets `CPUQuota (C − 4) × 100%`, `AllowedCPUs` all threads but 4, `MemoryMax RAM − 16 GB`, `MemoryHigh` a little below; each container `--cpus (C − 4)/N` and `--memory (RAM − 16 GB)/N`, from host facts. No IOWeight; `io.max` is an optional probe item.                                                                                                                                                                             |
| D3  | The host lane guard         | A hooks-daemon library handler (A), with cc deny rules (B) as a cheap second layer.                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| D4  | Stage 2: build or adopt     | Build small in agentic-qa-queue, jobs run as a dedicated runner user. **Owner:** the runners are the laptop and the home server, not the data centre (already a GitHub runner).                                                                                                                                                                                                                                                                                                                                      |
