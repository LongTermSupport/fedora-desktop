# Plan 00122: freeze and thaw LXC containers, as podfreeze does for Podman

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
| Inventory | `podman ps`                                          | `sudo lxc-ls -1` + `sudo lxc-info -s`         |
| States    | `running` / `paused`                                 | `RUNNING` / `STOPPED` / `FROZEN`              |
| Groups    | CCY session, network, GitHub account, token, SSH key | none of those exist                           |

Folding LXC into `podfreeze` would make a rootless tool prompt for `sudo` on every menu
open — including for a user who only wanted Podman, because a group row cannot say
`FREEZE 3` without first reading `/var/lib/lxc`. The owner chose a separate `lxcfreeze`
over that, and over the misnaming a `podfreeze` that does LXC would carry.

What the two **do** share is everything that is not the engine: the group menu, the
drill-down, the derived verb, the dry run, the bounded retry on a bad keypress, the
refusal to run inside a container, and the per-target act-and-report loop.

> **SCOPE, set by the owner after the plan was filed**: *"for now dont touch podfreeze
> just make lxcfreeze"*, and *"we can worry about DRY later"*. So the shared library is
> **not** built here and `podfreeze` is **not** refactored. `lxcfreeze` is standalone and
> will re-implement the menu, the derived verb and the dry run for itself.
>
> That duplication is a **recorded decision, not an oversight**. The reason it is the
> right call today: extracting a library from a 1,261-line tool that has no test anywhere
> in the repo and is used daily means the refactor's only safety net would be a suite
> written for the occasion — and a suite written after a refactor proves the refactor's
> own idea of correct. Building `lxcfreeze` first produces the second caller that shows
> which seams are actually shared, rather than guessing them from one. The extraction is
> [Phase 4](#phase-4-the-shared-library-deferred), left explicit so it is findable.

## Goals

- `lxcfreeze` freezes and thaws LXC containers singly, by bridge, or all of them, with
  the same derived-verb and dry-run behaviour `podfreeze` has.
- Its **decisions** are pure functions with a test suite, so the parts that cannot be
  exercised in a container are still falsifiable — the pattern
  `scripts/test-ccy-rootless-guard.bash` already establishes here.
- `podfreeze` is **not touched**: not one line, so its behaviour cannot regress.
- Deployed by its own play, `play-lxcfreeze.yml`. That is **not** what this plan first
  concluded — see Task 3.1 for why the owner's "don't touch podfreeze" outranks the
  one-play derivation, and where the two get reconciled.

## Non-Goals

- **Touching `podfreeze` at all**, per the owner's instruction above. Not its name, not
  its groups, not its rootless behaviour, and not a library extracted out of it.
- **De-duplicating the two tools.** Deferred to Phase 4 by the same instruction. The
  duplication is deliberate and written down rather than discovered later.
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

### Phase 1: The decisions, and a suite that can falsify them

The queries and the guards cannot run here — this container has no `lxc`, and a
freeze-tool's own guard refuses to run inside a container by design. So the **decisions**
are pure functions over fabricated inventories and are tested directly, while the query
that feeds them is not. That split is the repo's existing pattern, stated in
`scripts/test-ccy-rootless-guard.bash`: *"you cannot ask a real engine to be rootful just
to prove the guard notices."*

- [x] ✅ **Task 1.1**: `scripts/test-lxcfreeze.bash` — written first, against the pure
  decision functions. Every state list arrives **out of order and non-uniform**, or a
  mutant that drops the sort or reads the wrong list survives every case
- [x] ✅ **Task 1.2**: The decisions themselves, sourceable without executing the tool:
  the derived verb, the menu label, the state counts, and the partition of a selection
  into act / skip / vanished

### Phase 2: `lxcfreeze`

- [x] ✅ **Task 2.1**: The inventory. `sudo lxc-ls -1` for names and `sudo lxc-info -n NAME -s` for each state — **not** `lxc-ls -f`, whose aligned human columns are the
  fragile thing to parse. `FROZEN` and `RUNNING` are inventoried; `STOPPED` is excluded
- [x] ✅ **Task 2.2**: The actions — `sudo lxc-freeze -n` / `sudo lxc-unfreeze -n`, one
  container at a time so a single failure is named rather than collapsing the batch
- [x] ✅ **Task 2.3**: Groups worth having: all containers, and by bridge (read from
  `lxc.net.0.link` in `/var/lib/lxc/NAME/config`). LXC has no session identity, so those
  axes are absent rather than empty
- [x] ✅ **Task 2.4**: The `sudo` story. A tool that needs root for a read-only inventory
  says so before prompting, and a refused `sudo` is a named failure — never an empty set
  read as "nothing to freeze"
- [x] ✅ **Task 2.5**: `lxc` absent is its own answer, distinct from "LXC installed, no
  containers" — the same distinction `probe.dkms_registry` had to carry in Plan 00109, and
  for the same reason: one of them is a fault and the other is not
- [x] ✅ **Task 2.6**: Refuse to run inside a container, as `podfreeze` does. The host's
  LXC is not reachable from in here, so every probe would report an empty machine — and a
  misleading empty answer is worse than an error

### Phase 3: Deploy, QA, docs

- [x] ✅ **Task 3.1**: A new `play-lxcfreeze.yml`.
  **This reverses the plan's first answer, and the reason is the owner's instruction.**
  The repo's own test — a play earns its own file when it has "a lifecycle of its own"
  (`docs/playbooks.md`) — says `lxcfreeze` does *not*: same `hosts`, same `become`, same
  `scope`, and both engines are core imports of `playbook-main.yml`, so one play should
  own both. But extending `play-podfreeze.yml` means either its name stops matching what
  it does, or it gets renamed — and a rename churns the catalogue heading, the
  `#play-podfreezeyml` anchor `docs/ccy.md` links, and Plan 00079's `PLAN.md` and
  `deploy.bash`. Against *"for now dont touch podfreeze"* and *"we can worry about DRY
  later"*, the three duplicated deploy tasks are the cheaper debt, and Phase 4 is where
  both plays and both tools get reconciled together
- [x] ✅ **Task 3.2**: Wire the suite into `qa-all.bash`; update `CLAUDE/QA.md`'s gate
  count and table
- [x] ✅ **Task 3.3**: `docs/playbooks.md` gains a `play-lxcfreeze.yml` entry;
  `CLAUDE/ContainerEngines.md` gains the pair. The existing `#play-podfreezeyml` anchor
  is untouched, so `docs/ccy.md`'s link to it keeps resolving
- [x] ✅ **Task 3.4**: **HOST** — the owner ran it: *"i ran it and it seems to work"*. So
  `sudo lxc-ls -1` and `sudo lxc-info -n NAME -s` do emit what the parsers expect, which
  is the one thing the suite structurally could not establish
- [ ] ⬜ **Task 3.5**: `qa-reviewer` over the diff

### Phase 4: The shared library — no longer deferred, and not for DRY

**Reopened by the owner on first use**: *"totally different UX to the podfreeze system
though, maybe we can extract some DRY helpers"*. They are right, and the cause is mine:
Phase 2 **simplified** podfreeze's menu rather than reproducing it, so the two tools now
teach different habits for the same job.

| Axis          | `podfreeze`                                 | `lxcfreeze` as shipped      |
| ------------- | ------------------------------------------- | --------------------------- |
| Picker        | `fzf` when present, numbered menu otherwise | numbered menu only, always  |
| Structure     | **two-level** — group, then its members     | **flat** — both in one list |
| Member select | `TAB` in fzf, or `2,4,5` in the menu        | not possible                |
| Keys          | `ENTER`/`1` = all, `b` = back, `q` = quit   | `q` only                    |
| Retry budget  | 3 wrong answers, dies on the 4th prompt     | 3 prompts total             |

**So the shared thing is the menu LAYER, not a few helpers**, because that layer is
where the UX lives. Extracting it makes the two behave identically as a consequence.
Extracting only the pure decisions would be tidier and would leave the UX exactly as
divergent as it is now — which is the half that was actually complained about.

- [ ] ⬜ **Task 4.1**: Pin `podfreeze`'s behaviour with a suite written against it **as it
  is now**, before any extraction touches it. This is not optional and it is not
  ceremony: the tool is 1,261 lines, has no test anywhere in the repo, is used daily, and
  a suite written after the move proves only that the refactor agrees with itself
- [ ] ⬜ **Task 4.2**: Extract the decisions **and** the menu layer into a library both
  tools source. Engine differences enter through named hooks — the inventory query, the
  act call, the availability guard, the two state words, the extra table columns — never
  an `if` on the engine inside shared code
- [ ] ⬜ **Task 4.3**: `lxcfreeze` adopts it, gaining `fzf`, the drill-down, the member
  selection and the keys. `scripts/test-lxcfreeze.bash`'s 69 cases must still pass
- [ ] ⬜ **Task 4.4**: Task 4.1's suite must still pass against `podfreeze`, unchanged —
  that is the whole point of writing it first
- [ ] ⬜ **Task 4.5**: Reconcile the two plays, which Task 3.1 deferred to exactly here.
  With a shared library there is a third artefact to deploy, and two plays each copying
  its deploy task is the drift shape that argument was about
- [ ] ⬜ **Task 4.6**: **HOST** — both tools still behave as before, and `podfreeze`'s
  fzf path in particular, which no suite here can exercise

## Success Criteria

- [ ] `lxcfreeze` freezes a running LXC container and thaws it again, verified with
  `lxc-info -s` rather than by the tool's own report
- [ ] Running `lxcfreeze` twice on the same target toggles it, as `podfreeze` does
- [ ] `git diff` touches **no** line of `files/home/.local/bin/podfreeze`
- [ ] A refused or absent `sudo` produces a named failure, never an empty selection
- [ ] `lxc` not installed is reported as such, and is distinguishable from zero containers
- [ ] Every decision the suite covers has a mutant that kills it
- [ ] `./scripts/qa-all.bash` passes

## Dependencies

- **Builds on**: Plan 00079 (podman container control) — owns `podfreeze` and the play
  this plan extends. Not reopened, and `podfreeze` itself is not modified
- **Requires**: `playbooks/imports/play-lxc-install-config.yml` (core import) for the
  `lxc-freeze` / `lxc-unfreeze` binaries
- **Related**: `CLAUDE/ContainerEngines.md` — the Podman/Docker/LXC role split this plan
  takes as given

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00122-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
