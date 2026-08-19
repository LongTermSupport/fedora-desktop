# Plan 00078: Podman container control — freeze/thaw by container, network, and CCY group

**Status**: Not Started
**Created**: 2026-08-19
**Owner**: joseph
**Priority**: Medium

## Overview

The user wants an easy way to freeze/unfreeze ("suspend/unsuspend") Podman
containers — individually, all CCY containers as a group, and by network — with
a simple interactive UI. Research (see Facts) shows the four verbs map to **one
mechanism** that works on this repo's rootless Podman: `podman pause`/`unpause`
(the cgroup freezer). The other candidate mechanism, `podman container checkpoint`/`restore` (CRIU), **requires root** and so is out of scope on this
rootless setup — there is no real four-way split to build.

No surveyed existing tool (podman-tui, lazydocker, ctop, Cockpit's podman
module, dockge) offers network-scoped bulk freeze, CCY-group operations, or
self-freeze protection, so the plan is a small purpose-built interactive bash
tool, `podfreeze`, deployed to `~/.local/bin/` by a new optional play. CCY
containers turn out to be selectable **today** via the inherited
`claude-yolo-version` image label (F12), so the tool ships on that plus the
`<project>_yolo[_N]` name pattern as a fallback; an explicit run-time `--label`
in the CCY launcher remains worth having, but as an enhancement rather than a
prerequisite (see D4).

This plan was authored inside a CCY container where `podman` is not installed,
so every host-runtime claim below is either doc-sourced or listed as a
hypothesis with a `triage.bash` probe to confirm on the HOST before build.

## Goals

- One command to freeze/thaw: a named container, all CCY containers, all
  containers on a named network, or an interactively picked set.
- Simple TUI: a menu of **groups** (fzf, numbered-menu fallback, same pattern
  as the deployed `open` command) that acts, refreshes and re-offers itself
  until you quit — the group is the common target, not the individual.
- **No verb to choose and nothing to confirm**: the state decides the verb, so
  the same choice twice toggles; freezing is undone by doing it again (D7).
- Prints the exact set it touched, with CCY rows marked; `-n` to look without
  acting; refuses to run inside a container.
- CCY containers carry an explicit label so "all CCY containers" is a filter,
  not a name regex.
- Deployed via Ansible (`files/home/.local/bin/podfreeze` + new play).

## Non-Goals

- **Checkpoint/restore (CRIU)** — root-only (F2), and CCY containers run
  `--rm` (F6) which checkpoint rejects without `--export` (H5). If a
  reboot-surviving suspend is ever wanted, that is a separate root-mode plan.
- A GUI — the user accepts a TUI; a GUI buys nothing here.
- Docker/LXC support — Podman-first per `CLAUDE/ContainerEngines.md`; the
  `container_engine` variable is respected only insofar as the play declares
  the tool Podman-only.
- Replacing podman-tui/Cockpit for general container *browsing* — anyone who
  wants a full dashboard can install those; this tool does the verbs they lack.

## Facts

Confirmed facts (per `CLAUDE/PlanTriage.md`), each with its source in italics.
A list rather than a table on purpose — the table's column padding was costing
~2 KB of whitespace in a document read in full every session.

**From documentation and repo source:**

- **F1** — `podman pause`/`unpause` use the cgroup freezer and take `--all` and
  `--filter` (`network=`, `label=`, `name=`, `status=`, `ancestor=`).
  *podman-pause(1), docs.podman.io, 2026-08-19*
- **F2** — `podman container checkpoint`/`restore` (CRIU) is **not supported
  rootless**; CRIU needs elevated capabilities. *podman.io/docs/checkpoint,
  criu.org/Podman, 2026-08-19; research logged in JOURNAL*
- **F3** — this repo's default engine is **rootless Podman**, and CCY runs
  under it. *`CLAUDE/ContainerEngines.md`*
- **F4** — CCY containers are named `<project>_<suffix>[_N]`, suffix `yolo` or
  `browser`. *`claude-yolo/lib/common.bash`, `get_next_container_name()` 598–631*
- **F5** — the CCY `run` invocation passes **no `--label`**; the image carries
  `claude-yolo-version` and `claude-yolo-dockerfile-hash`.
  *`claude-yolo:2944`; `Dockerfile:36,326`*
- **F6** — CCY containers run with `--rm`. *`claude-yolo:2944`*
- **F7** — fzf is already deployed here, with a numbered-menu fallback when
  absent. *`play-open-command.yml:53`; `files/home/.local/bin/open`*
- **F8** — no container-management UI tool is deployed by any play.
  *grep of `playbooks/`, 2026-08-19*
- **F9** — podman-tui is packaged in Fedora and needs the user `podman.socket`.
  *packages.fedoraproject.org; podman-tui README, 2026-08-19*
- **F10** — the user's four verbs map to **one** rootless-capable mechanism,
  pause/unpause; checkpoint is the only other and is root-only. *F1 + F2 + F3*

**From the Phase 0 host triage** (raw log in this plan's gitignored `logs/`,
write-up in the journal):

- **F11** — podman 5.8.4, `rootless=true`, cgroups v2, `cgroupManager=systemd`,
  `runtime=crun` — **rootless pause is viable, so the Task 0.3 gate passes** (H1)
- **F12** — `ps --filter label=claude-yolo-version` matched exactly the running
  CCY containers, so the group is selectable with no launcher change (H2)
- **F13** — `podman pause` accepts `-f, --filter`; `ps --filter network=`
  partitions every network correctly, reporting empty sets not errors (H3)
- **F14** — `podman-tui 1.11.3-1.fc44` is in `updates`, its `podman.socket` is
  already active, and `fzf` is at `/usr/bin/fzf` (H4 packaging; D3 dependency)
- **F15** — **a CCY container shares a user-defined network with a
  ten-container compose stack**, and **seven CCY containers share the default
  `podman` network**

**From the first host acceptance run** (Phase 3):

- **F16** — **the inherited `claude-yolo-version` label over-matches**: it marks
  anything BUILT FROM the CCY image, session or not. Demonstrated — `--ccy`
  selected the gate's own throwaway, built from `localhost/claude-yolo:*`

**F15 is the safety case, and no hypothesis anticipated it.** The blast radius
of a network-scoped freeze is not guessable from the network's name: the
obvious first use (freeze an app's network) also freezes a live Claude session,
and freezing the default network stops nearly every session at once. D7 records
where that protection ended up.

H5 (checkpoint refuses `--rm` without `--export`) was not probed — checkpoint is
a non-goal per F2 + F3, so it matters only if that is reopened.

## Technical Decisions

### D1: Mechanism — pause/unpause only

**Context**: user asked for freeze/unfreeze *and* suspend/unsuspend.
**Decision**: both pairs are the same operation here: `podman pause`/`unpause`.
CRIU checkpoint (the only genuinely different "suspend to disk") is root-only
(F2) and incompatible with CCY's `--rm` (F6, H5) — excluded as a non-goal. The
tool's help text says this plainly: *frozen containers do not survive a
reboot*. **Date**: 2026-08-19

### D2: Build vs adopt — build a small tool; existing TUIs don't cover the asks

**Context**: surveyed podman-tui, lazydocker, ctop, Cockpit (cockpit-podman),
dockge (research log in JOURNAL). podman-tui is the only Fedora-packaged,
Podman-native TUI (F9), but nothing surveyed offers network-scoped bulk
freeze, a CCY-group verb, or self-freeze protection; lazydocker/ctop/dockge are
Docker-oriented and/or unpackaged for Fedora.
**Decision**: build `podfreeze` for the verbs; do **not** bundle podman-tui
into this plan (anyone wanting a browsing dashboard can propose it separately —
YAGNI). **Date**: 2026-08-19

### D3: Implementation — bash + fzf, not Python/curses, not Go/Rust

**Context**: repo conventions — user-facing interactive tools under
`files/home/.local/bin/` are bash (`open`, `ftp-camera`, `nord`, ccy itself);
`helpers/` stdlib-Python is for Ansible-invoked logic, not interactive TUIs
(`helpers/CLAUDE.md`); a Go/Rust TUI would add a toolchain this repo does not
have.
**Decision**: single bash executable using `podman` CLI + fzf `--multi` picker,
numbered-menu fallback when fzf is absent (F7 precedent). Binds to
`CLAUDE/InteractiveScripts.md` and `CLAUDE/StderrHygiene.md` in full.
**Date**: 2026-08-19

### D4: CCY identification — inherited image label now, explicit label next

**Decision (current)**: `--ccy` selects on the inherited `claude-yolo-version`
image label **or** the F4 name pattern. Phase 1 then adds an explicit
`--label ccy=true --label ccy-project=<project>` at `podman run` time
(CCY_VERSION **minor** bump per `CLAUDE/ContainerRules.md`), after which the
tool prefers `ccy=true` and keeps the inherited label as the fallback for
pre-Phase-1 containers.

**This moved twice**, which is worth knowing before re-litigating it: H2's
confirmation demoted Phase 1 from prerequisite to enhancement, then F16
re-promoted it as the fix for a demonstrated over-match. The reasoning at each
step is in the journal; the standing consequence is D6.
**Date**: 2026-08-19

### D5: Naming — `podfreeze`

Behaviour-descriptive, no mood: `podfreeze` with verbs
`freeze` / `thaw` / `list` (no-arg = interactive picker). "freeze/thaw" is the
user's own vocabulary; help text states it equals pause/unpause.
**Date**: 2026-08-19

Originally `podman-freeze`; shortened at the user's request. The pre-rename
binary is removed by a `state: absent` task in the play rather than by hand —
host state is owned by Ansible, and a stale executable on PATH is exactly the
drift that leaves two versions of a confirmation prompt on one machine.

### D6: `--ccy` over-matches until the explicit label lands

**Context**: F16 — every container built from the CCY image carries
`claude-yolo-version`, so `--ccy` selects CCY-derived containers that are not
Claude sessions. Not hypothetical: the acceptance gate's own throwaway was one.
**Options considered**: (a) narrow `--ccy` to the F4 name pattern alone;
(b) ship as-is and rely on the preview; (c) land Phase 1's explicit `ccy=true`
label and prefer it when present.

**Decision**: (c), with (b) holding the line meanwhile — `--ccy` keeps
`label OR name-pattern` until Phase 1 lands, then prefers `ccy=true` and falls
back for pre-Phase-1 containers.

**Why not (a)** — the two failure modes are asymmetric. The union's over-match
is *visible*: the extra container is named in the printed set and undone by
repeating the command. A narrowed selector's under-match is *silent*: a CCY
session not matching the pattern is simply absent while you believe you froze
everything. An invisible false sense of completeness is the worse failure and
is this repo's own defect class, so the union stands until a signal exists that
is both exact and positive — Phase 1, not a narrowing. (Full working, including
why the first version of this rationale was wrong, is in the journal.)
**Date**: 2026-08-19

### D7: the interactive UX — groups, a derived verb, no confirm, and a loop

**Context**: user feedback on the first build — three prompts to freeze the CCY
group, with the group targets reachable only by knowing the flag names.

**Decisions**, each with the reason it is not merely a preference:

1. **The menu offers groups, not containers.** All CCY containers / all
   containers / each network, with per-container picking as the last entry.
   Acting on a whole group is the common case, and the previous first screen
   made it the hardest thing to reach.
2. **The verb is derived, not asked** — anything running is frozen, a set with
   nothing running is thawed, so the same choice twice toggles. For a given set
   there is only one sensible move, and offering the other one is offering a
   mistake. An explicit `freeze`/`thaw` still wins so scripts can say what they
   mean rather than depend on current state.
3. **No confirmation prompt.** This reverses the "confirm before destructive
   action" reflex on purpose: freezing is *not* destructive or irreversible —
   it is undone by running the same command again — and in the menu you have
   already chosen a row that named the verb and the count. `--dry-run` remains
   for looking without acting. `-y`/`--yes` is therefore removed rather than
   left as a no-op flag.
4. **The menu loops**: act, re-read the inventory, re-offer. The re-read is the
   load-bearing part — counts must describe the machine now, not at startup.
5. **No "frozen things stay frozen" warning.** It restated the definition of
   the verb. The table's CCY column marks which rows are Claude sessions; what
   that implies is left to the reader.

**Consequence for F15**: the resolved-set preview is no longer a *gate*, it is
a record printed as it acts. The protection against freezing a network that
contains a live session moved earlier — into the menu row, which names the
count before the choice is made — and rests on reversibility rather than on a
prompt. **Date**: 2026-08-19

## Tasks

### Phase 0: Host triage + decision gate

- [x] ✅ **Task 0.1**: Plan-local `triage.bash` written (read-only, HOST-only,
  logs to this plan's gitignored `logs/`)
- [x] ✅ **Task 0.2**: Run on the HOST. H1–H3 confirmed, H4 partly; Facts table
  updated above and the outcome journalled
- [x] ✅ **Task 0.3**: **Decision gate PASSED.** Rootless pause is viable
  (cgroups v2 + systemd + crun), so the plan proceeds as designed

### Phase 1: CCY container labelling — the fix for the F16 over-match

Not a prerequisite (the tool works without it) but no longer optional either:
F16 showed `--ccy` selecting a CCY-*derived* container that was not a session.
See D4 and D6.

- [ ] ⬜ **Task 1.1**: Add `--label ccy=true --label ccy-project=<project>` to
  the `container_cmd run` invocation in `files/var/local/claude-yolo/claude-yolo`
- [ ] ⬜ **Task 1.2**: Bump `CCY_VERSION` (minor) with a comment describing the
  change; update `docs/ccy-changelog.md`
- [ ] ⬜ **Task 1.3**: Run `./scripts/qa-all.bash`; commit

### Phase 2: The `podfreeze` tool

- [x] ✅ **Task 2.1**: Implement `files/home/.local/bin/podfreeze`:
  - optional verbs `freeze`/`thaw` with targets `<name>…`, `--network <net>`,
    `--ccy`, `--all`; `list` report; no target = interactive **group** menu
    (fzf, numbered-menu fallback) that loops until quit — see D7
  - verb derived from state when not given; `--dry-run` prints the affected set
    and exits; no confirmation prompt (D7)
  - refuses to run inside a container (`/run/.containerenv`) — the host's
    podman is not reachable there and this also prevents freezing yourself
  - freeze skips already-paused containers; thaw targets only paused ones
    (`--filter status=`); empty selection is a clear message, not a silent pass
  - prompts/progress/errors → stderr; `list` output is the human payload
    (stdout) per `CLAUDE/StderrHygiene.md`
- [x] ✅ **Task 2.2**: New play
  `playbooks/imports/optional/common/play-podfreeze.yml` (`scope: general`,
  repo playbook structure per `CLAUDE/AnsibleStyle.md`): deploy the script
  `0755`, install `fzf` (optional dependency — tool degrades without it).
  Documented in `docs/playbooks.md`
- [x] ✅ **Task 2.3**: Run `./scripts/qa-all.bash` (includes ansible syntax +
  shebang/exec-bit checks); commit
- [x] ✅ **Task 2.4**: Plan-local `unit-test-selection.bash` — sources the
  tool's function region verbatim (cut at the `# Argument parsing` marker) and
  drives it against a synthetic inventory, so the states integration testing
  cannot easily manufacture (multi-network container, half-frozen group, empty
  CCY group, explicit verb overriding the derived one) are covered. Needs no
  podman, so it runs in the CCY container; `acceptance.bash` runs it first

### Phase 3: Deploy, acceptance, review

- [x] ✅ **Task 3.1**: Write plan-local `deploy.bash` + `acceptance.bash`
  (HOST): deploy chains straight into acceptance and exits with its verdict;
  acceptance freezes/thaws a **throwaway** container on a **throwaway** network
  (so a selection bug cannot reach a real container), checks `--ccy --dry-run`
  against the live CCY set as a contract, and checks the in-container refusal,
  the unknown-network and unknown-name failures, and mutually-exclusive
  targets. `--all` is never run for real. Both log to the plan's `logs/`
- [ ] 🔄 **Task 3.2**: User runs `CLAUDE/Plan/00078-podman-container-control/deploy.bash`
  on the HOST (it deploys, then runs acceptance itself); journal the verdict.
  **Run 1**: deploy clean (`failed=0`); acceptance **FAIL**, 1 of 15 — and the
  failure was the gate's own fixture, not the tool (F16, D6). Fixture fixed;
  awaiting run 2
- [ ] ⬜ **Task 3.3**: Run the `qa-reviewer` agent over the plan's full diff;
  resolve all BLOCK/FIX-BEFORE-MERGE findings
- [ ] ⬜ **Task 3.4**: Mark plan Complete, move to `Completed/`, update README
  index + statistics in the same commit

## Dependencies

- Depends on: nothing
- Blocks: nothing

## Success Criteria

- [ ] `podfreeze freeze --network <net>` pauses exactly the containers on
  that network, and prints the exact set it touched
- [ ] `podfreeze freeze --ccy` / `thaw --ccy` operates on all CCY
  containers (labelled and legacy-named)
- [ ] Interactive picker works with and without fzf
- [ ] Refuses to run inside a container; `--dry-run` changes nothing
- [ ] `acceptance.bash` renders `VERDICT: PASS` on the HOST against the
  deployed copy (it refuses to vouch for a binary that differs from the repo)
- [ ] New CCY containers carry `ccy=true` and `ccy-project=` labels
  *(Phase 1 — enhancement; the tool does not depend on it, see D4)*
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` verdict is PASS (or PASS
  WITH NITS, nits addressed or accepted)

## Risks & Mitigations

| Risk                                                                               | Impact | Probability | Mitigation                                                                                         |
| ---------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------- |
| Freezing a live CCY session mid-write (agent stalls, SSH/API connections time out) | M      | M           | Menu row names the verb and count before the choice; CCY rows marked; reversible by repeating (D7) |
| H1 wrong — rootless pause blocked on this host                                     | H      | L           | Phase 0 decision gate before any build                                                             |
| Name-pattern CCY match catches an unrelated `*_yolo` container                     | L      | L           | Explicit labels (D4) make the pattern a transition fallback only                                   |
| User forgets containers are frozen (a paused container looks hung)                 | M      | M           | `podfreeze list` surfaces paused set; freeze prints the exact thaw command                         |
| `--rm` interaction: `podman stop` on a paused `--rm` container removes it          | M      | L           | Tool never stops; docs warn to thaw before stopping                                                |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00078-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created + research logged
- Phase 0 host triage run; decision gate passed, Phase 1 demoted
- Phase 2 built: `podfreeze`, `play-podfreeze.yml`, docs; plus the
  plan's `deploy.bash` + `acceptance.bash` (Task 3.1)
