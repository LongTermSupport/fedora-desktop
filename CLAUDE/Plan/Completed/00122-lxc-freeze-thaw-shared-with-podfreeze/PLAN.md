# Plan 00122: freeze and thaw LXC containers, as podfreeze does for Podman

**Status**: Complete
**Created**: 2026-09-15
**Completed**: 2026-09-17
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
>
> **SUPERSEDED by the owner on first use** — *"totally different UX to the podfreeze
> system though, maybe we can extract some DRY helpers"*. Phase 4 was reopened and done:
> the library exists, `podfreeze` was refactored onto it, and the safety net is the
> 187-case pin written against `podfreeze` BEFORE anything moved. Everything above this
> line is the reasoning as it stood when the plan was filed, kept because it is why the
> order was right — not because it still describes the code.

## Goals

- `lxcfreeze` freezes and thaws LXC containers singly, by bridge, or all of them, with
  the same derived-verb and dry-run behaviour `podfreeze` has.
- Its **decisions** are pure functions with a test suite, so the parts that cannot be
  exercised in a container are still falsifiable — the pattern
  `scripts/test-ccy-rootless-guard.bash` already establishes here.
- ~~`podfreeze` is **not touched**: not one line, so its behaviour cannot regress.~~
  **Superseded in Phase 4.** `podfreeze` was refactored onto the shared library. What
  replaces the guarantee is `scripts/test-podfreeze.bash`, 187 cases written against
  the tool as it was and passing unchanged across the extraction — a weaker promise
  than "not one line", and an actually checkable one.
- Deployed by its own play, `play-lxcfreeze.yml`. That is **not** what this plan first
  concluded — see Task 3.1 for why the owner's "don't touch podfreeze" outranks the
  one-play derivation, and where the two get reconciled.

## Non-Goals

- ~~**Touching `podfreeze` at all**, per the owner's instruction above. Not its name, not
  its groups, not its rootless behaviour, and not a library extracted out of it.~~
  **Superseded in Phase 4**, which the owner reopened. Its name, its groups and its
  rootless behaviour are still untouched; the library WAS extracted out of it.
- ~~**De-duplicating the two tools.** Deferred to Phase 4 by the same instruction. The
  duplication is deliberate and written down rather than discovered later.~~
  **Done, in Phase 4.** The duplication was deliberate for exactly as long as the
  deferral held, and is gone.
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
- [x] ✅ **Task 3.5**: `qa-reviewer` over the diff — FIX-BEFORE-MERGE, 11 findings, all
  acted on. Report:
  [subagent-reports/260915-qa-reviewer-opus-5.md](subagent-reports/260915-qa-reviewer-opus-5.md).
  The two that justified the whole pass: a `sudo lxc-info` failure was laundered into
  "this container does not exist", dropping it from the inventory silently while a
  comment asserted the preflight guards had ruled that out; and the extracted menu
  layer — the only thing Phase 4 exists to share — had **no behavioural coverage in any
  suite**, because `pick_target` and `drill_into_group` were stubbed out in the file
  whose own comment said this layer "is not left to a host to find out". Fixes below in
  Task 3.6
- [x] ✅ **Task 3.6**: Act on all 11 — done; per-finding detail in the journal. The
  shape of the fixes: statuses captured and unreadable containers DISCLOSED rather than
  dropped (`INV_UNREADABLE` / `warn_unreadable`); `FREEZE_SELECT_GONE` so "that group
  went away" and "the hook broke" stop sharing a status; `parse_member_choice` split
  out of `drill_into_group` so the menu grammar is testable at all; the table hooks pad
  their own columns; `fzf` moved into the shared task file. Suites 231 / 76 / 187
- [x] ✅ **Task 3.7**: **Carried to Plan 00131.** `.semgrep/bash-conventions.yml`'s
  `|| true` rule is line-anchored and cannot see the enclosed form (`$( cmd || true )`),
  which is how the two above shipped. Widening it found 18 further live sites in 8
  files, four of them the git hooks that gate secret scanning for this public repo —
  real risk, so its own plan rather than the tail of this one. The widening was reverted
  here; the sites and the reasoning are in this plan's journal and in Plan 00131. Closed
  rather than held open: a box that will never be ticked here makes a finished plan read
  as incomplete

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

- [x] ✅ **Task 4.1**: Pin `podfreeze`'s behaviour with a suite written against it **as it
  is now**, before any extraction touches it. This is not optional and it is not
  ceremony: the tool is 1,261 lines, has no test anywhere in the repo, is used daily, and
  a suite written after the move proves only that the refactor agrees with itself.
  `scripts/test-podfreeze.bash`, 187 cases once Task 4.1b added four, wired into
  `qa-all.bash`. `podfreeze` itself is
  unchanged: the suite sources only the definitions above the tool's argument loop, with
  the boundary derived from the file's own content. 15 mutants were each killed by a named
  case — see the journal
- [x] ✅ **Task 4.1b**: Fix the two defects Task 4.1 found while pinning, BEFORE the
  extraction moves the functions holding them. Separately from the extraction, so that
  the extraction's guarantee can be "no behaviour change at all" rather than "no
  behaviour change except these two". Both were pinned as-shipped first, then the pins
  flipped to assert the fix, so each is a traceable before/after
  - [x] ✅ `select_network` matched with `grep -qx` and no `-F`, making the
    network-existence check a **regex** match — `podma.` passed because `podman`
    exists, and the user then got "Nothing in that group" rather than the unknown-network
    error listing the real ones. `select_identity` already did this correctly
  - [x] ✅ `identity_matches` split its ssh-key haystack with an unquoted
    `for word in $have` — word splitting **and** pathname expansion, so a label value
    of `*` expanded against the working directory and every file there became a key
    that session appeared to hold. A container label is not the tool's to trust that
    far. Now `read -ra`, with cases proving multi-key matching still works
- [x] ✅ **Task 4.2**: `files/home/.local/lib/freeze/freeze-common.bash` — the decisions
  **and** the menu layer, deployed to `~/.local/lib/freeze/` and sourced by both tools.
  User-scope tools get a user-scope library, and because `files/` mirrors the target
  filesystem one relative path (`../lib/freeze/…`) resolves from the checkout and from
  `~/.local/bin` alike. Seven named hooks and six declared settings — five the contract
  requires, plus the optional `FREEZE_LIST_NOTE`; **no `if` on an
  engine name anywhere in the shared half**. Its own suite,
  `scripts/test-freezelib.bash` (231 cases), drives every decision under BOTH engines'
  state vocabularies — a hardcoded `running` passes one pass and fails the other — and
  20 mutants were each killed by a named case
- [x] ✅ **Task 4.3**: `lxcfreeze` adopted it and gained `fzf`, the two-level
  drill-down, `TAB`/`2,4,5` member selection, and the `ENTER`/`1`/`b`/`q` keys. Its own
  suite is 76 cases: the ~25 that drove the now-shared decisions MOVED to the library
  suite with their assertions intact, and the cases that replaced them cover what is
  genuinely LXC's — the bridge group axis, and the hooks
- [x] ✅ **Task 4.4**: `scripts/test-podfreeze.bash` passes **187/187 with the SUITE
  FILE byte-identical** — `git diff` touches not one line of the suite across the
  extraction commit. That is the guarantee it was written first to be able to give.
  Said precisely because "the file" read as `podfreeze` to at least one reader, and
  `podfreeze` is 698 lines changed: what is unchanged is the yardstick, not the tool
- [x] ✅ **Task 4.5**: Reconciled, and the answer was **not** to merge the plays. What
  they genuinely share is one artefact, so `tasks/deploy-freeze-lib.yml` is included by
  both — the pattern `tasks/ensure-jq.yml` already establishes here. A third play owning
  the library would break `ansible-playbook play-podfreeze.yml` on a fresh host, since a
  tool without its library does not start; copying the tasks into both plays is the
  drift this task existed to remove. Each play keeps its own name, anchor and
  dependencies (`fzf` is podfreeze's alone)
- [x] ✅ **Task 4.6**: **HOST** — done by the batch run of `deploy.bash` then
  `acceptance.bash`: 12 of 12 checks, 33 assertions, 0 failed, ACCEPTED. Both deployed
  tools start against the shared library and list containers (check [4]), both resolve it
  by the same relative hop (check [2]), and all three artefacts are byte-identical to
  their repo copies at the declared mode (check [1]). **The fzf path itself is still
  unexercised** — check [3] asserts only that `fzf` is installed, so "podfreeze behaves as
  before" is established for the start, library-resolution and list paths, and is inferred
  rather than measured for the picker

### Phase 5: A thawed container must be reachable

First overnight use found the gap the suites cannot: every container thawed cleanly and
none answered ssh. The freezer stops the DHCP client with everything else, the one-hour
lease from the host's dnsmasq expires mid-freeze, the kernel drops the address, and
NetworkManager renews only on its own retry timer — two to eight minutes after the thaw.
Nothing was broken; it was slow, and slow reads as broken when the ssh wrapper gives up
after ten seconds. Evidence in the 26-09-16 journal.

- [x] ✅ **Task 5.1**: `renew_dhcp_lease` in `lxcfreeze`, called from the thaw branch of
  `freeze_hook_act` after `lxc-unfreeze`. Runs inside the container via `lxc-attach`,
  reconnects every ethernet device NetworkManager reports rather than assuming `eth0`,
  and a failed renewal is a named failure whose message says the container IS thawed.
  The suite pins the renewal to the thaw branch and that freeze does not touch the
  network; header comment, play ready message and `docs/playbooks.md` say what thaw
  now does
  - [x] ✅ **The thaw's status gates the renewal** (`|| return $?`), found by Task 6.4's
    review. Appending the renewal had silently transferred the hook's exit status from
    the unfreeze to it, and the library calls the hook inside `if out="$( … )"` where
    bash suspends errexit — so a failed `lxc-unfreeze` followed by a reconnect that
    happened to succeed printed `✓ name` and exited 0 for a container still frozen. The
    suite now RUNS the hook against a stubbed `sudo` rather than reading its text: line
    adjacency was what the old assertions checked, and they were green for both the
    broken and the fixed form
  - [x] ✅ Thaw now requires NetworkManager in every container it thaws — a container on
    `dhclient`, `systemd-networkd` or a static address fails the renewal. Loud rather
    than silent, and `docs/playbooks.md` says so
- [x] ✅ **Task 5.2**: **HOST** — both plays run, then the deployed tool froze and
  thawed one container: the host's `journalctl -t dnsmasq-dhcp` shows `DHCPDISCOVER`
  through `DHCPACK` in the same second as the thaw. The renewal is unconditional, so a
  freeze longer than the lease takes the same path; the overnight case is confirmed
  the next time it happens rather than staged for an hour
- [x] ✅ **Task 5.3**: `IPV4` column in `lxcfreeze`'s table, from `lxc-info -n NAME -iH`
  via `lxcf_parse_ipv4`, blank when there is no address. The address is matched by
  SHAPE, not by line position, so a dual-stack container cannot put an IPv6 address
  under a column headed IPV4, and a failure message from `lxc-info` cannot be printed
  where an address goes. A failed read keeps the container in the list — state is what
  gates the verbs — and prints `IPV4_UNREADABLE`, not a blank: the blank cell is the
  load-bearing signal, so a probe that could not answer must not produce it, or an
  `lxc-info` without `-i` makes every running container look like it lost its lease. The
  suite reads the column by POSITION, since trimming a two-column row cannot tell a
  blank address from a short row
- [x] ✅ **Task 5.4**: `FREEZE_FREEZE_NOTE` — a library slot, printed beside the "Thaw
  them with:" line at freeze time, where the cost is still avoidable. `lxcfreeze` fills
  it: thaw renews the lease so the address returns, but every ssh session into the
  container, and any agent socket forwarded over one, dies with the frozen TCP
  connection — reconnect rather than trust an old session. The library carries neither
  the text nor the assumption that there is one, because DHCP leases and severed ssh
  sessions say nothing about a Podman container; an empty note prints nothing at all,
  not a blank line
- [x] ✅ **Task 5.5**: `lxc-attach` chowns the file its stderr points at (a triage probe
  with stderr unredirected left a root-owned capture). Recorded in
  `CLAUDE/AgentNotes.md` under Project Gotchas

### Phase 6: Suspend to disk — closed: not realistic for a systemd container

**Decision (owner, from the spike): no suspend-to-disk verb, and no stop/start verb.**
CRIU refuses before writing an image, and Proxmox avoids container hibernation entirely.
Reasoning, the comparison table and the alternatives:
[DECISION-no-suspend-to-disk.md](DECISION-no-suspend-to-disk.md). Evidence in the
26-09-16 journal.

- [x] ✅ **Task 6.1**: Spike on the designated container — conclusive at the first
  step: CRIU refused before writing an image (nested UTS namespace from logind's
  `ProtectHostname=yes`). Not pursued past that, since the fix is a systemd drop-in in
  every container and only buys the next blocker
- [x] ✅ **Task 6.2**: **DECISION**: neither verb. Stop/start is what `lxc-stop` and
  `lxc-start` already are, and wrapping them in `lxcfreeze` would put a shutdown behind
  a tool whose name says otherwise
- [ ] ❌ **Task 6.3**: Cancelled by Task 6.2 — nothing to implement
- [x] ✅ **Task 6.4**: `qa-reviewer` over Phases 5–6 — verdict **BLOCK**, 2 blocking, 4
  should-fix, 8 nits, all acted on. Report:
  [subagent-reports/260916-qa-reviewer-phase56-opus-5.md](subagent-reports/260916-qa-reviewer-phase56-opus-5.md).
  The one that mattered: a failed `lxc-unfreeze` reported success (Task 5.1 above), and
  the suite could not tell the broken form from the fix because it read source text
  rather than running the hook. Nit 14 (two `sudo lxc-info` calls per container, which
  `lxc-info -si` would merge) is knowingly **not** taken: it is a cost worth knowing on
  a host with many containers and not worth a larger parse on a small one

## Success Criteria

**Every criterion below that needs a host is now a script, not an instruction.** Run
`deploy.bash` then `acceptance.bash` in this folder — or `untracked/meta-deploy.bash` to
run this plan alongside the others waiting. `acceptance.bash` carries twelve
COVERAGE-registered checks, creates and destroys its own throwaway container, and prints
the two claims no gate can settle rather than letting a green verdict imply them.

- [x] ✅ `lxcfreeze` freezes a running LXC container and thaws it again, verified with
  `lxc-info -s` rather than by the tool's own report — checks [7] and [8] of the host run
- [x] ✅ Running `lxcfreeze` twice on the same target toggles it, as `podfreeze` does —
  check \[8\]: the same command run again returned the container to RUNNING
- [x] **Phases 2–3 only**: `git diff` touches **no** line of
  `files/home/.local/bin/podfreeze`. Held through Task 4.1, which is why that
  suite pins the tool as shipped rather than a version adjusted to be testable.
  **Phase 4 supersedes it**: extracting a menu layer that both tools source
  necessarily edits `podfreeze`, and Task 4.4 — "Task 4.1's suite must still pass
  against `podfreeze`" — is the criterion that replaces it. The suite, not the
  absence of a diff, is what now protects the tool.
- [x] ✅ A refused or absent `sudo` produces a named failure, never an empty selection —
  check [10]
- [x] ✅ `lxc` not installed is reported as such, and is distinguishable from zero
  containers — check [11], which also asserts the message names the play that installs it
- [x] Every decision the suites cover has a mutant that kills it — 18 for `lxcfreeze`,
  15 for `podfreeze`, 20 for the shared library, each killed by a NAMED case
- [x] `./scripts/qa-all.bash` passes
- [x] ✅ After `lxcfreeze thaw`, ssh into the container succeeds on the first attempt,
  after a freeze longer than the one-hour lease — **closed on an argument plus a partial
  measurement, not on the stated experiment.** What was measured (Task 5.2) is a real
  freeze/thaw whose `journalctl -t dnsmasq-dhcp` shows `DHCPDISCOVER` through `DHCPACK` in
  the same second as the thaw. The renewal is unconditional, so a freeze longer than the
  lease reaches that identical code path and the only untested variable is whether the
  address had already been dropped. The owner declined to stage an hour-long freeze for
  it; the real overnight case confirms itself the next time it occurs. **If a first-attempt
  ssh ever fails after a long freeze, this criterion — not the renewal code — is where the
  gap was accepted.**
- [x] The suspend-to-disk decision is recorded with the spike's evidence — the decision
  in Phase 6's heading, the evidence in the 26-09-16 journal (10:00, 10:02, 10:10)

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
