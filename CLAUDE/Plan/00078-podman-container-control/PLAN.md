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
tool, `podman-freeze`, deployed to `~/.local/bin/` by a new optional play. CCY
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
- Simple TUI: fzf multi-select picker with a numbered-menu fallback (same
  pattern as the deployed `open` command).
- Safe by design: preview of the exact affected set, confirmation before bulk
  action, refusal to run inside a container, `-y` for non-interactive use.
- CCY containers carry an explicit label so "all CCY containers" is a filter,
  not a name regex.
- Deployed via Ansible (`files/home/.local/bin/podman-freeze` + new play).

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

Confirmed facts (per `CLAUDE/PlanTriage.md`, source cited for each):

| #   | Fact                                                                                                                                                                          | Source                                                                                                  |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- |
| F1  | `podman pause`/`unpause` use the cgroup freezer and support `--all` and `--filter`, with filters including `network=`, `label=`, `name=`, `status=`, `ancestor=`              | podman-pause(1), docs.podman.io stable (fetched 2026-08-19)                                             |
| F2  | `podman container checkpoint`/`restore` (CRIU) is **not supported rootless** — CRIU needs elevated capabilities; root/sudo required                                           | podman.io/docs/checkpoint, criu.org/Podman, docs.podman.io (web research 2026-08-19, logged in JOURNAL) |
| F3  | This repo's default engine is **rootless Podman**; CCY runs under it                                                                                                          | `CLAUDE/ContainerEngines.md`                                                                            |
| F4  | CCY containers are named `<project>_<suffix>[_N]` where suffix is `yolo` or `browser` (e.g. `myproject_yolo`, `myproject_yolo_2`)                                             | `files/var/local/claude-yolo/lib/common.bash` `get_next_container_name()` (lines 598–631)               |
| F5  | The CCY `run` invocation passes **no `--label`**; the image carries `LABEL claude-yolo-version=` and `claude-yolo-dockerfile-hash=`                                           | `files/var/local/claude-yolo/claude-yolo:2944`; `files/var/local/claude-yolo/Dockerfile:36,326`         |
| F6  | CCY containers run with `--rm`                                                                                                                                                | `files/var/local/claude-yolo/claude-yolo:2944`                                                          |
| F7  | fzf is already deployed by this repo, with a numbered-menu fallback pattern when absent                                                                                       | `playbooks/imports/optional/common/play-open-command.yml:53`; `files/home/.local/bin/open`              |
| F8  | No container-management UI tool is currently deployed by any play                                                                                                             | grep of `playbooks/` (2026-08-19)                                                                       |
| F9  | podman-tui is packaged in Fedora (`dnf install podman-tui`) and requires the user `podman.socket` service                                                                     | packages.fedoraproject.org; github.com/containers/podman-tui README (fetched 2026-08-19)                |
| F10 | The user's four verbs (freeze/unfreeze, suspend/unsuspend) map to **one** rootless-capable mechanism: pause/unpause. Checkpoint is the only second mechanism and is root-only | F1 + F2 + F3                                                                                            |

Facts established by the **Phase 0 host triage** (raw log in this plan's
gitignored `logs/`; full write-up in the journal):

| #   | Fact                                                                                                                                                                 | Source             |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ |
| F11 | podman 5.8.4, `rootless=true`, `cgroupVersion=v2`, `cgroupManager=systemd`, `runtime=crun` — **rootless pause is viable; the Task 0.3 decision gate passes** (H1)    | Phase 0 triage log |
| F12 | `ps --filter label=claude-yolo-version` matches **exactly** the running CCY containers — the group is selectable today, with no launcher change (H2, revises D4)     | Phase 0 triage log |
| F13 | `podman pause` accepts `-f, --filter`; `ps --filter network=` partitions every network correctly, reporting empty sets rather than erroring (H3)                     | Phase 0 triage log |
| F14 | `podman-tui 1.11.3-1.fc44` is in `updates` and the user `podman.socket` it needs is already active; `fzf` is present at `/usr/bin/fzf` (H4 packaging; D3 dependency) | Phase 0 triage log |
| F15 | **A CCY container shares a user-defined network with a ten-container compose stack**, and **seven CCY containers share the default `podman` network**                | Phase 0 triage log |

**F15 is the safety case, and no hypothesis anticipated it.** The blast radius of
a network-scoped freeze is not guessable from the network's name: the obvious
first use (freeze an app's network) also freezes a live Claude session, and
freezing the default network stops nearly every session on the machine. This
makes the resolved-set preview in Task 2.1 the feature the network verb is
unusable without, not a nicety.

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
**Decision**: build `podman-freeze` for the verbs; do **not** bundle podman-tui
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

### D4: CCY identification — explicit run-time label, name-pattern fallback

**Context**: CCY sets no container label (F5); name pattern `*_yolo*` (F4) is
the only certain handle today, and inherited image labels (H2) are unverified.
**Decision**: add `--label ccy=true --label ccy-project=<project>` to the CCY
`run` invocation (CCY_VERSION **minor** bump per `CLAUDE/ContainerRules.md`).
The tool selects CCY containers by `label=ccy=true` **or** the F4 name pattern,
so pre-existing unlabelled containers still match during the transition.
**Date**: 2026-08-19

**REVISED after Phase 0** (2026-08-19): H2 is **confirmed** — the inherited
`claude-yolo-version` image label already selects exactly the CCY containers, so
the launcher change is no longer a prerequisite and Phase 1 is demoted to an
enhancement. The tool ships selecting on `label=claude-yolo-version` **or** the
F4 name pattern. The explicit label is still wanted later for two reasons the
inherited one cannot serve: it distinguishes "is a CCY session" from "was built
from the CCY image", and it carries the project name for per-project verbs.

### D5: Naming — `podman-freeze`

Behaviour-descriptive, no mood: `podman-freeze` with verbs
`freeze` / `thaw` / `list` (no-arg = interactive picker). "freeze/thaw" is the
user's own vocabulary; help text states it equals pause/unpause.
**Date**: 2026-08-19

## Tasks

### Phase 0: Host triage + decision gate

- [x] ✅ **Task 0.1**: Plan-local `triage.bash` written (read-only, HOST-only,
  logs to this plan's gitignored `logs/`)
- [x] ✅ **Task 0.2**: Run on the HOST. H1–H3 confirmed, H4 partly; Facts table
  updated above and the outcome journalled
- [x] ✅ **Task 0.3**: **Decision gate PASSED.** Rootless pause is viable
  (cgroups v2 + systemd + crun), so the plan proceeds as designed

### Phase 1: CCY container labelling — enhancement, not a prerequisite

Demoted by the Phase 0 triage: H2 is confirmed, so the CCY group is selectable
today with no launcher change and no CCY version bump. What the explicit label
still buys, and why the phase is kept rather than dropped: see D4's REVISED
note above.

- [ ] ⬜ **Task 1.1**: Add `--label ccy=true --label ccy-project=<project>` to
  the `container_cmd run` invocation in `files/var/local/claude-yolo/claude-yolo`
- [ ] ⬜ **Task 1.2**: Bump `CCY_VERSION` (minor) with a comment describing the
  change; update `docs/ccy-changelog.md`
- [ ] ⬜ **Task 1.3**: Run `./scripts/qa-all.bash`; commit

### Phase 2: The `podman-freeze` tool

- [x] ✅ **Task 2.1**: Implement `files/home/.local/bin/podman-freeze`:
  - verbs: `freeze`/`thaw` with targets `<name>…`, `--network <net>`, `--ccy`,
    `--all`; `list` report; no-arg = interactive fzf multi-select picker
    (numbered-menu fallback) showing name, status, networks, CCY marker
  - `--dry-run` prints the affected set and exits; every bulk action previews
    the exact set and confirms `[y/N]` (default No, bounded retries, EOF =
    clean abort); `-y`/`--yes` escape hatch
  - refuses to run inside a container (`/run/.containerenv`) — the host's
    podman is not reachable there and this also prevents freezing yourself
  - freeze skips already-paused containers; thaw targets only paused ones
    (`--filter status=`); empty selection is a clear message, not a silent pass
  - prompts/progress/errors → stderr; `list` output is the human payload
    (stdout) per `CLAUDE/StderrHygiene.md`
- [x] ✅ **Task 2.2**: New play
  `playbooks/imports/optional/common/play-podman-freeze.yml` (`scope: general`,
  repo playbook structure per `CLAUDE/AnsibleStyle.md`): deploy the script
  `0755`, install `fzf` (optional dependency — tool degrades without it).
  Documented in `docs/playbooks.md`
- [x] ✅ **Task 2.3**: Run `./scripts/qa-all.bash` (includes ansible syntax +
  shebang/exec-bit checks); commit

### Phase 3: Deploy, acceptance, review

- [x] ✅ **Task 3.1**: Write plan-local `deploy.bash` + `acceptance.bash`
  (HOST): deploy chains straight into acceptance and exits with its verdict;
  acceptance freezes/thaws a **throwaway** container on a **throwaway** network
  (so a selection bug cannot reach a real container), checks `--ccy --dry-run`
  against the live CCY set as a contract, and checks the in-container refusal,
  the unknown-network and unknown-name failures, and mutually-exclusive
  targets. `--all` is never run for real. Both log to the plan's `logs/`
- [ ] ⬜ **Task 3.2**: User runs `CLAUDE/Plan/00078-podman-container-control/deploy.bash`
  on the HOST (it deploys, then runs acceptance itself); journal the verdict
- [ ] ⬜ **Task 3.3**: Run the `qa-reviewer` agent over the plan's full diff;
  resolve all BLOCK/FIX-BEFORE-MERGE findings
- [ ] ⬜ **Task 3.4**: Mark plan Complete, move to `Completed/`, update README
  index + statistics in the same commit

## Dependencies

- Depends on: nothing
- Blocks: nothing

## Success Criteria

- [ ] `podman-freeze freeze --network <net>` pauses exactly the containers on
  that network after an accurate preview + confirmation
- [ ] `podman-freeze freeze --ccy` / `thaw --ccy` operates on all CCY
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

| Risk                                                                               | Impact | Probability | Mitigation                                                                       |
| ---------------------------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------- |
| Freezing a live CCY session mid-write (agent stalls, SSH/API connections time out) | M      | M           | Preview marks CCY containers; confirmation default No; freeze is non-destructive |
| H1 wrong — rootless pause blocked on this host                                     | H      | L           | Phase 0 decision gate before any build                                           |
| Name-pattern CCY match catches an unrelated `*_yolo` container                     | L      | L           | Explicit labels (D4) make the pattern a transition fallback only                 |
| User forgets containers are frozen (a paused container looks hung)                 | M      | M           | `podman-freeze list` surfaces paused set; freeze prints the exact thaw command   |
| `--rm` interaction: `podman stop` on a paused `--rm` container removes it          | M      | L           | Tool never stops; docs warn to thaw before stopping                              |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00078-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created + research logged
- Phase 0 host triage run; decision gate passed, Phase 1 demoted
- Phase 2 built: `podman-freeze`, `play-podman-freeze.yml`, docs; plus the
  plan's `deploy.bash` + `acceptance.bash` (Task 3.1)
