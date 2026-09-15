# Plan 00122: freeze and thaw LXC containers, on machinery podfreeze already has

**Status**: In Progress
**Created**: 2026-09-15
**Owner**: joseph
**Priority**: Medium

## Overview

`podfreeze` (Plan 00079) freezes and thaws **Podman** containers: a menu of groups
rather than of containers, a verb derived from current state rather than asked for, and
a dry run. This repo also runs **LXC** as a first-class engine — `lxc` and
`lxc-templates` from the `ganto/lxc4` Copr, installed by
`playbooks/imports/play-lxc-install-config.yml`, which `playbook-main.yml` imports — and
there is no equivalent for it. `lxc-freeze` and `lxc-unfreeze` exist; nothing groups
them, previews them, or derives the verb.

The two engines are **not** interchangeable, which is why this is a sibling tool and not
a flag on `podfreeze`:

| Axis      | Podman here                                          | LXC here                                      |
| --------- | ---------------------------------------------------- | --------------------------------------------- |
| Privilege | rootless, no `sudo`                                  | **rootful** — every query and action needs it |
| Inventory | `podman ps`                                          | `sudo lxc-ls -f` / `sudo lxc-info`            |
| States    | `running` / `paused`                                 | `RUNNING` / `STOPPED` / `FROZEN`              |
| Groups    | CCY session, network, GitHub account, token, SSH key | none of those exist                           |

Folding LXC into `podfreeze` would make a rootless tool prompt for `sudo` on every menu
open — including for a user who only wanted Podman, because a group row cannot say
`FREEZE 3` without first reading `/var/lib/lxc`. The owner chose a separate `lxcfreeze`
over that, and over the misnaming a `podfreeze` that does LXC would carry.

What the two **do** share is everything that is not the engine: the group menu, the
drill-down, the derived verb, the dry run, the bounded retry on a bad keypress, the
refusal to run inside a container, and the per-target act-and-report loop. That goes
into a sourced library, so `lxcfreeze` is the LXC-specific parts and nothing else.

## Goals

- `lxcfreeze` freezes and thaws LXC containers singly, by bridge, or all of them, with
  the same derived-verb and dry-run behaviour `podfreeze` has.
- The shared machinery lives in **one** place, sourced by both tools.
- `podfreeze`'s behaviour is **unchanged** by the extraction — proven, not asserted.
- Both tools and the library are deployed by one play, since neither has a lifecycle of
  its own and copying the deploy tasks is how the pair would drift.

## Non-Goals

- **Renaming or re-scoping `podfreeze`.** It keeps its name, its groups and its rootless
  behaviour. This plan adds a sibling and extracts what they share.
- **CCY groups for LXC.** `ccy=true`, `ccy-github`, `ccy-token` and `ccy-ssh-keys` are
  labels CCY stamps on Podman containers at launch. LXC sessions have no equivalent, and
  inventing one is not this plan's business.
- **`lxc-checkpoint` / suspend-to-disk.** `lxc-freeze` is the cgroup freezer, exactly as
  `podman pause` is: the processes stay in RAM and stop being scheduled, and a frozen
  container does not survive a reboot.
- **Stopped containers.** As with `podfreeze`, only containers that can be frozen or
  thawed are inventoried. A `STOPPED` container is neither.
- **Unprivileged/user LXC** under `~/.local/share/lxc`. This repo's LXC is rootful; a
  second search path would double every query for a configuration nothing here creates.

## Tasks

### Phase 1: The shared library, with `podfreeze` unchanged

- [ ] ⬜ **Task 1.1**: A test suite for the machinery **before** it moves. There is none
  today, so the extraction has no safety net: `podfreeze` is 1,261 lines, it works, and it
  is used daily. The suite drives the real functions against stub engines
- [ ] ⬜ **Task 1.2**: Extract the engine-agnostic functions into
  `files/home/.local/lib/freeze/freezelib.bash`. Engine-specific behaviour enters through
  a named hook, not through an `if` on the engine inside shared code
- [ ] ⬜ **Task 1.3**: `podfreeze` sources the library and keeps its own Podman inventory,
  CCY groups and identity axes. The suite from 1.1 must pass unchanged across the move — a
  test written after the refactor proves the refactor's own idea of correct

### Phase 2: `lxcfreeze`

- [ ] ⬜ **Task 2.1**: The inventory. `sudo lxc-ls -f`, parsed into the shared arrays;
  `FROZEN` maps to the frozen state and `STOPPED` is excluded
- [ ] ⬜ **Task 2.2**: The actions — `sudo lxc-freeze -n` / `sudo lxc-unfreeze -n`, one
  container at a time so a single failure is named rather than collapsing the batch
- [ ] ⬜ **Task 2.3**: Groups worth having: all containers, and by bridge. LXC has no
  session identity, so those axes are absent rather than empty
- [ ] ⬜ **Task 2.4**: The `sudo` story. A tool that needs root for a read-only inventory
  must say so before prompting, and a refused `sudo` is a named failure — never an empty
  set read as "nothing to freeze"
- [ ] ⬜ **Task 2.5**: `lxc` absent is its own answer, distinct from "LXC installed, no
  containers" — the same distinction `probe.dkms_registry` had to carry in Plan 00109, and
  for the same reason: one of them is a fault and the other is not

### Phase 3: Deploy, QA, docs

- [ ] ⬜ **Task 3.1**: Extend `play-podfreeze.yml` to deploy the library and `lxcfreeze`
  alongside `podfreeze`. One play: same `hosts`, same `become`, same `scope`, and both
  engines are core imports of `playbook-main.yml`, so neither tool needs a guard the other
  does not. A second play would copy four deploy tasks, which is the shape that drifts
- [ ] ⬜ **Task 3.2**: Wire the suite into `qa-all.bash`; update `CLAUDE/QA.md`'s gate
  count and table
- [ ] ⬜ **Task 3.3**: `docs/playbooks.md` — the catalogue entry covers both tools, and the
  `#play-podfreezeyml` anchor `docs/ccy.md` links must keep resolving
- [ ] ⬜ **Task 3.4**: **HOST** — run the play, then `lxcfreeze list`, freeze a container,
  confirm `sudo lxc-info -n NAME -s` reports `FROZEN`, thaw it, and confirm `podfreeze`
  still behaves exactly as before on the same machine
- [ ] ⬜ **Task 3.5**: `qa-reviewer` over the diff

## Success Criteria

- [ ] `lxcfreeze` freezes a running LXC container and thaws it again, verified with
  `lxc-info -s` rather than by the tool's own report
- [ ] Running `lxcfreeze` twice on the same target toggles it, as `podfreeze` does
- [ ] `podfreeze` behaves identically before and after the extraction — the same suite
  passes against both
- [ ] A refused or absent `sudo` produces a named failure, never an empty selection
- [ ] `lxc` not installed is reported as such, and is distinguishable from zero containers
- [ ] `./scripts/qa-all.bash` passes

## Dependencies

- **Builds on**: Plan 00079 (podman container control) — owns `podfreeze` and the play
  this plan extends. Not reopened; this adds a sibling and a shared library
- **Requires**: `playbooks/imports/play-lxc-install-config.yml` (core import) for the
  `lxc-freeze` / `lxc-unfreeze` binaries
- **Related**: `CLAUDE/ContainerEngines.md` — the Podman/Docker/LXC role split this plan
  takes as given

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00122-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
