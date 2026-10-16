# Plan 00206: deployed tools never run from checkout

**Status**: Not Started
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: High

## Overview

The fedora-desktop checkout is a working folder: agents and the owner develop in it,
switch branches in it and merge work in progress into F44 in it. The ccy container edits
the host's own checkout, so a merge into F44 changes the files on the host at once. Any
runtime tool that loads code from that tree therefore changes the moment a merge lands,
before QA has run on the result and before any play has deployed it. That has already
happened: the LXC btrfs fix in `helpers/agent_bus_lxc` went live on merge because
`ccy-teams` imports `helpers/agent_bus/declare.py` straight from the checkout.

The owner's rule: what runs on the desktop is what has been deployed, and is regarded
as ready to deploy; what is in the checkout is work in progress. A tool built from the
repo is installed by its play into a deployed location and runs only from there. A
merge then changes nothing at run time until the owner runs `meta-deploy.bash`.

This plan records the audit of every place that breaks the rule ([INVENTORY.md](INVENTORY.md)),
designs the deployed shape (versioned, drift-checked, guarded by a QA gate), and moves
each offender, riskiest first. Plays, `run.bash`, `meta-deploy.bash`, plan scripts, QA
and tests still run from the checkout: that is their job.

## Goals

- No deployed artefact (unit, wrapper, shell snippet, binary, launcher) loads code from
  the checkout at run time. The checkout may still be passed as **data** where the
  checkout is the subject (host_vars, plays to run, the freshness and pin judgement, the
  vmtest spool), always as an explicit argument, never as a code root.
- Every deployed tool answers `--version` with the source it was built from, and a
  deployed copy that lags the checkout is reported.
- Tests and acceptance run the checkout's code explicitly and never depend on what is
  deployed; acceptance of a deployed tool refuses to vouch for a copy whose version is
  not the one under test.
- A QA gate blocks a new runtime path into the checkout, so the class cannot come back.

## Non-Goals

- Moving plays, `run.bash`, `meta-deploy.bash` or plan scripts out of the checkout.
- Changing how the owner deploys: `meta-deploy.bash` from the checkout stays the one
  host command.
- The pingbus kit being built from two sources (`files/opt/claude-yolo/` and
  `helpers/pingbus`): Plan 00202 owns it.
- Porting the Python helpers to Go: Plan 00191 and the "Tooling Is Go" rule own that; this
  plan only changes where they run from.
- Changing the hooks daemon's `deployed_artefact_drift` handler: it is daemon-owned and
  covers daemon artefacts. Its design is reused, not its code.

## Context & Background

**Inventory** ([INVENTORY.md](INVENTORY.md), evidence in `subagent-reports/`):

| Class                          | Count | Items                                                                                                                                          |
| ------------------------------ | ----- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Runtime code from the checkout | 7     | R1 `ccy-teams` -> `declare.py`; R2-R5 the four `host_health` entry points; R6 the `gh-*-token-phpstorm` functions; R7 `ftp-camera --pass` (D6) |
| Borderline (owner decision)    | 2     | B1 the project's own ccy supervisor; B2 the project image and `ccy.env` (D7)                                                                   |
| Already deployed copies        | 22    | the ccy launcher and libs, base image, pingbus and agent-bus zipapps, kit and plugin dir, container-watch, vmtest, Go commands, extensions…    |
| Checkout by design             | 13    | plays, zipapp builds at play time, `run.bash`, `meta-deploy.bash`, plan scripts, QA, tests, the repo's own git and Claude hooks…               |

The ccy launcher (`/var/local/claude-yolo` and its `lib/`) is already a copy, the base
image is built from the deployed `/opt/claude-yolo`, the bus plugin dir is the deployed
kit, nothing symlinks into the repo, and there is no cron. Every runtime offender is a
**Python helper reached by `root_dir`**: one Go binary that shells out to it, three
`host_health` entry points, one wrapper and one shell function.

**What exists to reuse.**

- `/usr/local/lib/ccy-helpers/helpers/` already holds deployed copies of containerwatch,
  github443 and vmtest, run with `PYTHONPATH=<lib> python3 -P`.
- `pingbus.pyz` and `agent-bus.pyz` (`helpers/pingbus/bundle.py`) are single-file zipapps
  installed to `/usr/local/lib/agent-bus/`.
- `tasks/build-go-command.yml` builds every Go command reproducibly (`-trimpath -buildvcs=false`) and installs a copy.
- `qa-deployed-drift` (`src-go/qa/deployeddrift`, Plan 00192) compares repo files with
  their deployed copies on the host, skipped in ccy and in linked worktrees.
- Plan 00109's play ledger records which play ran here at which commit, and its
  login report and panel tell the owner when a play has changed since.
- The daemon's `deployed_artefact_drift` handler's two rules: presence is the signal
  (compare only what is deployed), and a copy the deploy overwrites unconditionally is
  drifted whenever it differs, with no ledger needed.
- Plan 00079's podfreeze acceptance already refuses a deployed binary that differs from
  the repo.

**Related plans.** 00109 (drift, the panel; superset of the drift clause), 00191 (Go
tooling; builds ccy-teams), 00202 (pingbus kit built twice), 00137 (server self-update
from a signed deploy clone; the same question for servers, already answered there),
00199 (md-viewer: a pinned module with a `--version` check), 00184 (a menu of deployed
tools; this plan decides what counts as deployed).

## Design

### 1. One deployed shape per kind of tool

- **Python helpers become zipapps**, built at play time by one shared task file
  (`tasks/build-python-zipapp.yml`, the twin of `build-go-command.yml`) from an explicit
  list of packages, and installed to `/usr/local/lib/fedora-desktop/<tool>.pyz` (root-owned,
  0755, run with `python3 -I`). A zipapp is one file, so the install is an atomic
  replace: a running tool never sees half a tree, which a directory copy cannot promise.
  The build imports every module inside the zip before install (an import smoke test),
  so a missing package fails the play, not the next login. `pingbus.bundle` is
  generalised rather than copied (D2).
- **Go commands** stay on `build-go-command.yml`. What they may not do is reach back:
  `-X main.RootDir` is no longer a code root. ccy-teams is given the deployed declare
  zipapp's path (`-X main.DeclarePyz=/usr/local/lib/fedora-desktop/agent-bus-declare.pyz`)
  and keeps the checkout only for the host_vars file and the plays it runs.
- **Wrappers, units and shell snippets** name the deployed zipapp and pass the checkout
  with an explicit `--repo-root` where the checkout is the data.

### 2. Version: what is deployed, and from what

- The version of a deployed tool is the **git tree hash of its inputs** at deploy time
  (for a zipapp, the packages it bundles; for Go, `src-go`), plus `+dirty` when the
  working tree had uncommitted changes in those paths. A tree hash changes only when the
  tool's source does, so the reproducible build stays byte-identical across unrelated
  commits and `copy` still reports `ok`.
- It is stamped in (`-X main.Version` for Go, a `VERSION` member in the zipapp) and
  printed by `--version` as `<tool> <tree-hash>[+dirty] (commit <sha>)`.
- Each deploy writes one line per artefact to a manifest,
  `/usr/local/lib/fedora-desktop/deployed.json` (tool, path, version, commit, play),
  the one place the drift check and the panel read. A `+dirty` deploy is allowed or
  refused according to D1.

### 3. Drift detection, reusing what exists

- **Repo vs host**: `qa-deployed-drift` gains a "tool version" check: for each manifest
  entry, the checkout's current tree hash of that tool's inputs against the deployed
  version. Different means "merged, not deployed". Same skips as today (ccy, worktrees).
- **On the desktop**: Plan 00109's login report and panel gain one axis, "deployed tools
  behind the checkout", read from the manifest, phrased as "N tools are behind F44: run
  meta-deploy". This lands in 00109's surfaces; this plan supplies the manifest and the
  comparison.
- Presence is the signal: a tool never deployed on this host is not reported.

### 4. Tests and acceptance run the checkout explicitly

- Unit tests already import the checkout (`PYTHONPATH=$REPO_ROOT`, Go in-tree) and stay so.
- Deployed tools get **no** environment override that points them back at the checkout:
  a developer runs checkout code by its module (`python3 -m helpers.x` from the repo,
  `go run ./cmd/x`), never through a deployed entry point. An override would be a
  runtime path into the checkout by another name.
- Plan acceptance that exercises a deployed tool first compares the deployed `--version`
  with the checkout's tree hash and stops, naming `meta-deploy.bash`, when they differ,
  the podfreeze rule from Plan 00079 generalised into a `_planlib` helper.

### 5. Stopping regressions: a gate, Defence Before Fix

The defect class: **a deployed artefact resolves code from the checkout at run time.**
A new Go gate, `qa-checkout-runtime` (in `src-go/qa`, wired into `qa-all.bash`,
`qa-changed.bash` and CI), reads every source of a deployed artefact (templates and
files under `files/`, `content:`/`blockinfile:` blocks and inline units in plays,
`go_command_ldflags`) and fails on `root_dir` (or a variable rendered from it) used as a
code root:

- `WorkingDirectory={{ root_dir }}` in a unit;
- `PYTHONPATH` naming `root_dir`;
- `cd "{{ root_dir }}"` followed by an interpreter or `-m`;
- an exec or `source` of a path under `root_dir`, or a rendered `ROOT_DIR`/`REPO_PATH`;
- `-X main.RootDir` in ldflags.

Data uses (host_vars, plays, the vmtest spool, the vault) are rows in an allowlist that
names the data path and the reason, on the same ratchet as `bash-limits.allowlist`: rows
may go, never be added without review. Proved red first on the seven instances in the
inventory, which start on the allowlist and leave it one by one as each phase moves
them. A Go project rule (`project_checkout_exec` in `src-go/qa/rules`) covers the Go
side: a process started with an argument or `PYTHONPATH` derived from `RootDir`.

### 6. Migration order, riskiest first

1. R1 `ccy-teams` -> `declare.py` (every team session; already caused a live change).
2. R2-R5 `host_health` (every shell, every login, the panel), one package, one phase.
3. R6 the `gh-*-token-phpstorm` functions.
4. R7 `ftp-camera --pass`, per D6.

## Tasks

Each phase is shippable alone: it leaves the tree green and the host working, with its
own `meta-deploy.list` entry where a host run is needed.

### Phase 1: The gate (Defence Before Fix)

- [ ] ⬜ **Task 1.1**: Write `qa-checkout-runtime` with fixtures; prove it red on each of
  the seven inventory instances and on the ways round it (a variable rendered from
  `root_dir`, `chdir` in a `shell:` the play writes into a file)
- [ ] ⬜ **Task 1.2**: Ratchet allowlist seeded with the seven instances and the data uses,
  each with its reason; wire into `qa-all.bash`, `qa-changed.bash`, CI and `CLAUDE/QA.md`
- [ ] ⬜ **Task 1.3**: `project_checkout_exec` Go rule, red on the `declare.go` fixture,
  table row for the current instance until Phase 3

### Phase 2: Shared deploy mechanism and version

- [ ] ⬜ **Task 2.1**: `tasks/build-python-zipapp.yml` (generalised from `pingbus.bundle`),
  with the import smoke test, the `VERSION` member and atomic install
- [ ] ⬜ **Task 2.2**: Tree-hash version for Go commands in `build-go-command.yml`
  (`-X main.Version`), `--version` on every src-go command
- [ ] ⬜ **Task 2.3**: The deploy manifest written by both task files
- [ ] ⬜ **Task 2.4**: `_planlib` helper: deployed version equals checkout or stop

### Phase 3: ccy-teams and declare (R1)

- [ ] ⬜ **Task 3.1**: `agent-bus-declare.pyz` (declare plus `agent_bus`, `agent_bus_lxc`,
  `pingbus.bundle`/`protocol`) built and installed by `play-claude-yolo.yml`
- [ ] ⬜ **Task 3.2**: ccy-teams runs the deployed zipapp; `RootDir` kept only for host_vars
  and plays; refuses with a clear message naming `meta-deploy.bash` when the zipapp is
  missing or older than the binary expects
- [ ] ⬜ **Task 3.3**: D5's rule for `apply`/`remove` launching plays from the checkout
- [ ] ⬜ **Task 3.4**: Allowlist row and Go rule table row removed; acceptance through
  meta-deploy (a merge to `helpers/agent_bus_lxc` changes nothing until deployed)

### Phase 4: host_health (R2-R5)

- [ ] ⬜ **Task 4.1**: `host-health.pyz` (`host_health`, `play_ledger`,
  `self_update.published`, `version_pins`) built and installed by
  `play-host-health-login-report.yml`
- [ ] ⬜ **Task 4.2**: The two units, the bashrc snippet and `fedora-desktop-health` run the
  zipapp with `--repo-root` as data; `WorkingDirectory={{ root_dir }}` gone
- [ ] ⬜ **Task 4.3**: Allowlist rows removed; acceptance through meta-deploy

### Phase 5: The rest (R6, R7)

- [ ] ⬜ **Task 5.1**: `github_scopes` deployed; the generated shell functions call it
- [ ] ⬜ **Task 5.2**: `ftp-camera --pass` per D6 (deployed `vault.bash` copy, or an
  allowlist row naming the vault as data)

### Phase 6: Drift coverage and surfaces

- [ ] ⬜ **Task 6.1**: `qa-deployed-drift` reads the manifest; covers the existing
  `ccy-helpers` copies (containerwatch, github443) and displaylink-recovery
- [ ] ⬜ **Task 6.2**: Plan 00109's login report and panel show "deployed tools behind the
  checkout" (handed to 00109 as a task with the manifest contract)
- [ ] ⬜ **Task 6.3**: `play-git-hooks-security.yml` uses `root_dir`, not a hardcoded path
  (found during the audit)

### Phase 7: Documentation and review

- [ ] ⬜ **Task 7.1**: The rule written where it is enforced: a section in
  `CLAUDE/InfrastructureAsCode.md`, a pointer in the path-triggered rules for `files/`
  and `playbooks/`, the gate in `CLAUDE/QA.md`
- [ ] ⬜ **Task 7.2**: `qa-reviewer` over the whole plan

## Owner Decisions

- **D1. What a deploy builds from.** (a) the working tree as it is, uncommitted edits
  included (today's behaviour for every Go command); (b) the working tree, but a runtime
  artefact refuses to deploy `+dirty` unless asked; (c) the committed HEAD only, exported
  with `git archive`; (d) a release tag (`44.x.y`, Release.md). **Recommended: (b)**: it
  matches "regarded as ready to deploy" without making every deploy wait for a tag, and
  `+dirty` stays visible in `--version` when allowed.
- **D2. Where zipapps live.** `/usr/local/lib/fedora-desktop/` (new, one place for all),
  or alongside the existing `/usr/local/lib/ccy-helpers/` and `/usr/local/lib/agent-bus/`.
  **Recommended: the new directory** for new tools, with the existing ones moved there
  when next touched (not in this plan).
- **D3. One zipapp per tool or one shared helpers zipapp.** **Recommended: one per tool**
  (`agent-bus-declare.pyz`, `host-health.pyz`, …): each deploys and versions on its own,
  and a change to one package does not redeploy unrelated tools.
- **D4. A deploy clone for the desktop** (as servers have in Plan 00137, plays run from a
  clean clone of F44) is the stronger isolation, and would also move plays out of the
  working tree. **Recommended: not now**: it changes the owner's workflow, and the
  runtime rule above closes the gap the owner named.
- **D5. ccy-teams launching plays from the checkout** (`apply`, `remove`). Accept as is,
  warn when the checkout is dirty or not on F44, or refuse. **Recommended: warn and ask**.
- **D6. `ftp-camera --pass` and `vault.bash`.** The vault file only exists in the checkout.
  **Recommended: an allowlist row** naming the vault as data; a deployed copy of
  `vault.bash` would still read the checkout's vault file.
- **D7. The project's own ccy files** (B1 supervisor, B2 image and `ccy.env`). These are
  every ccy project's configuration, not desktop tools. **Recommended: out of scope**,
  recorded in INVENTORY.md.

## Dependencies

- Related: Plan 00109 (receives Task 6.2), 00191 (ccy-teams build), 00202 (pingbus kit)

## Success Criteria

- [ ] `qa-checkout-runtime` passes with no runtime rows left on its allowlist
- [ ] A merge into F44 that changes `helpers/agent_bus*` or `helpers/host_health` changes
  nothing a running tool loads until `meta-deploy.bash` is run (acceptance shows it)
- [ ] Every deployed src-go command and zipapp answers `--version` with its tree hash
- [ ] `qa-deployed-drift` reports a tool merged but not deployed, on the host
- [ ] `qa-reviewer` passes the plan

## Risks & Mitigations

| Risk                                                               | Impact | Probability | Mitigation                                                              |
| ------------------------------------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------- |
| A zipapp misses a module the tool imports at run time              | H      | M           | Import every module in the zip at build time; the play fails, not login |
| ccy-teams binary and declare zipapp deployed at different versions | H      | M           | Both come from one play; ccy-teams checks the zipapp version it expects |
| The gate misses a spelling, so the class returns                   | M      | M           | Red-tested against the ways round it; Go side held by a project rule    |
| Merged fixes now wait for a deploy and the owner forgets           | M      | M           | The "behind the checkout" axis in the login report and panel            |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00206-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Audit and design committed (this pass)
