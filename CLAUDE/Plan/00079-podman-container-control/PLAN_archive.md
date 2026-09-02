> Archived on 2026-09-02: superseded by the lean PLAN.md alongside. Kept verbatim as the historical record.

# Plan 00079: Podman container control — freeze/thaw by container, network, and CCY group

**Status**: In Progress
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
sessions are identified by the run-time `ccy=true` label the launcher sets from
CCY 3.40.0, plus the `<project>_yolo[_N]` name pattern for sessions started by
an older CCY. The inherited `claude-yolo-version` image label was the first
handle tried and is now deliberately unused — it marks a lineage, not a session
(F12, F16, D4, D6).

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

> **F1–F14 are settled Phase 0 research and host triage.** Compacted here for
> size; the verbatim originals with all their sources are in
> `JOURNAL/00079-Journal-26-08-20.md`. The claims are unchanged.

- **F1/F2/F10** — `pause`/`unpause` (cgroup freezer, takes `--all` and
  `--filter network=|label=|name=|status=|ancestor=`) is the **only**
  rootless-capable mechanism for the user's four verbs; CRIU
  `checkpoint`/`restore` is root-only
- **F3/F11** — rootless Podman 5.8.4, cgroups v2, `systemd`/`crun`: **rootless
  pause is viable, Task 0.3's gate passes** (H1)
- **F4** — CCY containers are named `<project>_<suffix>[_N]`, suffix `yolo` or
  `browser` (`common.bash:598-631`) — the fallback selector, see F19
- **F5/F12** — the `run` invocation passed **no `--label`**; only the *image*
  carried `claude-yolo-version`, which matched the running set at the time and
  so looked sufficient. F16 is why it was not
- **F6** — CCY containers run with `--rm` (`claude-yolo:2944`)
- **F7/F8/F9/F14** — `fzf` is already deployed with a numbered-menu fallback; no
  container UI tool is deployed by any play; podman-tui is packaged and its
  socket already active (D3's dependency question)
- **F13** — `pause` accepts `-f, --filter`, and `ps --filter network=`
  partitions every network correctly, reporting empty sets rather than errors
- **F15** — **a CCY container shares a user-defined network with a
  ten-container compose stack**, and **seven CCY containers share the default
  `podman` network**

**From the first host acceptance run** (Phase 3):

- **F16** — **the inherited `claude-yolo-version` label over-matches**: it marks
  anything BUILT FROM the CCY image, session or not. Demonstrated — `--ccy`
  selected the gate's own throwaway, built from `localhost/claude-yolo:*`.
  Fixed by the run-time `ccy=true` label (Phase 1); the image label is no longer
  consulted (D6)
- **F17** — **`podman` is ONE shared bridge network, not a per-container
  default.** `podman network ls` lists it with a single NETWORK ID and the
  `bridge` driver, and seven CCY sessions were attached to it simultaneously.
  So a session launched without `--network` joins the same L2 domain as every
  other one. That is podman's default rather than a CCY choice, but it makes
  "network: podman" in the menu approximately "every session that did not join
  a project network" — and it is why F15's hazard runs the other way too: an
  eighth session sat on the app network `mkt` instead
- **F18** — **the host was running a `podfreeze` older than the repo's**, and
  `acceptance.bash` refused rather than verify it. The D9 drill-down (`a678e31`,
  10:42) landed **10 minutes after** run 2's deploy (10:32), so run 2's "16
  passed" never exercised it. Plan 00099's defect class, caught by the check
  written for it. **`deploy.bash`, not `acceptance.bash`, is the entry point
  whenever the tool itself changed**
- **F19** — **check 9 verified only the labelled path.** It built its expected
  set from `label=ccy=true` alone, so the name-pattern fallback was never
  asserted — and **4 of the 6 live sessions are unlabelled**, so the majority of
  the real population reaches `--ccy` through the one path the gate was silent
  about. Had the pattern broken, check 9 would still have reported OK. Identical
  in shape to Plan 00080's P4. Closed by **9b**, which then **proved** the
  fallback works — all four named, all four resolved
- **F20** — **the identity axes silently under-match unlabelled sessions.** An
  unlabelled session is in `--ccy` but in **no** identity group, because its
  account cannot be inferred. `select_identity` refused the case where *nothing*
  is labelled and said nothing about the case where only *some* is — the state
  the machine is in until every session is relaunched. So
  `podfreeze freeze --github X` answered "every session for X" with a strict
  subset — the exact ask the axis was added for. Fixed by disclosing on
  **stderr**: the identity is genuinely unknowable, so blocking would be wrong;
  being quiet was the defect

**From the `qa-reviewer` pass** (Task 3.3) — three more of the same shape, found
in the diff that documents the shape:

- **F21** — **the identity menu row was gated on cardinality, not coverage.**
  `${#values[@]} -lt 2` is only a proxy for "this value covers every session",
  and it is wrong in exactly the partial-rollout state the axis is for: one
  account + four unlabelled sessions is one distinct value covering 2 of 6. The
  row vanished from the menu while `--github X` still worked on the CLI, leaving
  the *wider* "all CCY containers" as the only menu route — steering toward
  freezing sessions the user had not asked for — and making F20's disclosure
  unreachable, since `select_identity` was never called
- **F22** — **`none` and "unlabelled" were collapsed.** The inventory comment
  claimed they stayed distinct; the code dropped `none` into the same empty-map
  state as an absent label, so a 3.40.0 session with no identity on any axis was
  reported as pre-3.40.0 and its owner told to relaunch it — advice that cannot
  work. Fixed with `INV_HAS_LABELS`, keyed on the `ccy=true` query result
- **F23** — **acceptance check 13 could pass having asserted nothing.** `gh_one`
  comes from a query including paused sessions; the expectation narrowed to
  running. The only labelled session being paused — normal, since pausing is
  this tool's job — left an empty expected set, an unexecuted loop body, and a
  printed pass over a population of zero

**F15 is the safety case, and no hypothesis anticipated it** — a network-scoped
freeze's blast radius is not guessable from the network's name. D7 records where
that protection ended up.

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

### D4: CCY identification — run-time session label, name pattern for older CCY

**Decision (settled)**: `--ccy` selects on the run-time `ccy=true` label (set by
CCY ≥ 3.40.0, so it cannot be inherited) **or** the F4 session name pattern. The
inherited `claude-yolo-version` image label is **not** consulted — see D6.

This moved three times (H2 demoted Phase 1 to an enhancement, F16 re-promoted
it, landing it then made the image label pure liability); the working is in the
journal, so re-read that before re-litigating it. **Date**: 2026-08-20

### D5: Naming — `podfreeze`

Behaviour-descriptive, no mood: `podfreeze` with verbs
`freeze` / `thaw` / `list` (no-arg = interactive picker). "freeze/thaw" is the
user's own vocabulary; help text states it equals pause/unpause.
**Date**: 2026-08-19

Originally `podman-freeze`; shortened at the user's request. The pre-rename
binary is removed by this plan's `deploy.bash`, not by a `state: absent` task in
the play: the play describes the machine's steady state, and a one-off cleanup
for a name that only ever existed mid-plan does not belong in it permanently.
It is still not done by hand — the plan script owns it, and fails if the removal
does not succeed.

### D6: the image label is dropped, and that is a narrowing with no gap

**Context**: F16 — the image label marks anything built FROM the CCY image, so
it selected non-sessions. Not hypothetical: the gate's own throwaway was one.
**Decision**: once Phase 1 landed, drop the image label outright rather than
keep it as a fallback.

**Why that narrowing is safe.** Narrowing is normally the dangerous direction —
an over-match is *visible* (named in the printed set, undone by repeating) while
an under-match is *silent*, a session absent while you believe you froze
everything. That asymmetry is why the union was kept until an exact positive
signal existed, and it stops applying here because the image label covers
**nothing** the other two miss: ≥ 3.40.0 sessions carry `ccy=true`, older ones
are named `<project>_yolo[_N]` by construction (`get_next_container_name`). Its
only remaining contribution was F16's false positive. **Date**: 2026-08-20

### D7: the interactive UX — groups, a derived verb, no confirm, and a loop

**Context**: user feedback on the first build — three prompts to freeze the CCY
group, with the group targets reachable only by knowing the flag names.

**Decisions**, each with the reason it is not merely a preference:

1. **The menu offers groups, not containers** — per-container picking is the
   last entry. Acting on a whole group is the common case, and it had been the
   hardest thing to reach.
2. **The verb is derived, not asked**: anything running is frozen, a set with
   nothing running is thawed, so the same choice twice toggles. There is only
   one sensible move for a given set, and offering the other is offering a
   mistake. An explicit `freeze`/`thaw` still wins, so scripts can say what
   they mean instead of depending on current state.
3. **No confirmation prompt.** This reverses the "confirm before destructive
   action" reflex deliberately: freezing is *not* irreversible, and the chosen
   menu row already named the verb and the count. `-y`/`--yes` is removed
   rather than left as a no-op; `--dry-run` remains.
4. **The menu loops**: act, re-read the inventory, re-offer. The re-read is
   load-bearing — counts must describe the machine now, not at startup.
5. **No "frozen things stay frozen" warning** — it restated the verb.

**Consequence for F15**: the resolved-set preview is a record printed as it
acts, not a gate. Protection against freezing a network holding a live session
moved earlier, into the menu row that names the count before the choice, and
rests on reversibility rather than on a prompt. **Date**: 2026-08-19

### D8: group by session identity, not just by name and network

**Context**: user ask — *"label ccy containers by key label and anthropic token
as well? so could easily freeze all containers with github ID XXX"*. Once a
session is labelled at run time at all (D6), the same mechanism answers a
question the tool could not previously ask: **who** is this session running as.

**Decision**: CCY stamps `ccy-github`, `ccy-token` and `ccy-ssh-keys`;
`podfreeze` gains `--github` / `--token` / `--ssh-key` and one menu row per
distinct value in the live inventory.

- **Values drive the menu**, derived from what is running (as the network rows
  are), and an axis appears only at ≥ 2 distinct values — with one account,
  "github: \<id>" and "all CCY containers" are the same button.
- **`ccy-ssh-keys` is multi-valued**, so it matches by WORD while the
  single-valued axes compare whole. Being lenient there would silently widen a
  freeze, so the two are separate paths, each unit-tested.
- **`none`, not empty**, so "no GitHub identity" cannot read as "unlabelled".
- **An unknown value is an error** listing the known ones — never an empty set
  and exit 0, the trap `select_network` already guards.
- **Nothing in the container reads these.** Pre-3.40.0 sessions carry none and
  stay reachable by name or `--ccy`; `--github` says so rather than pretending
  they do not exist. **Date**: 2026-08-20

### D9: choosing a group opens it, rather than acting immediately

**Context**: user ask — *"can we have a way to drill into a grouping … first
option is 'all' or its then possible to pick specific ones … easiest and
clearest"*.

**Decision**: a group row leads to a second screen listing the group's members,
with **"act on all of it" as the first row**, each member showing the verb it
will get. The standalone "pick individual containers…" entry is removed:
drilling into "all containers" *is* that, so keeping it would be a second route
to one screen.

**Why this does not undo D7.** The earlier feedback was that per-container
tab-selection was *forced* and that group targets were unreachable without
knowing flag names. Here the whole group stays one keypress — ENTER on row 1 —
so the fast path is intact and the picking is optional. What it buys is
**sight of the members before acting**, which a group row structurally cannot
give: "network: podman — FREEZE 5" names nothing, and F15/F17 are exactly the
case where one of those five is a live Claude session on an app network.

The "all" row is keyed by a sentinel (`*all*`) rather than the word: podman
container names must start with an alphanumeric, so no container can collide
with it. **Date**: 2026-08-20

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

- [x] ✅ **Task 1.1**: Add `--label ccy=true --label ccy-project=<project>` to
  the `container_cmd run` invocation in `files/var/local/claude-yolo/claude-yolo`
- [x] ✅ **Task 1.2**: Bump `CCY_VERSION` (minor) with a comment describing the
  change; update `docs/ccy-changelog.md` — 3.39.0 → 3.40.0
- [x] ✅ **Task 1.3**: Point `podfreeze` at `ccy=true`, drop the image label
  (D6), and re-aim the acceptance gate's check 9 at the new contract
- [x] ✅ **Task 1.4**: Label the session's **identity** too —
  `ccy-github` / `ccy-token` / `ccy-ssh-keys` — and give `podfreeze` matching
  `--github` / `--token` / `--ssh-key` targets plus menu rows (D8)
- [x] ✅ **Task 1.5**: Run `./scripts/qa-all.bash`; commit

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
- [x] ✅ **Task 3.2**: User runs `CLAUDE/Plan/00079-podman-container-control/deploy.bash`
  on the HOST (it runs `play-claude-yolo.yml` **then** `play-podfreeze.yml`,
  then acceptance itself); journal the verdict.
  **Run 3: PASS, 21 checks, 0 skipped** — checks 9 and 13 became real
  assertions, `--ccy` and `--github` both resolving against live labelled
  sessions with the throwaway excluded from each. (Runs 1–2 and the refused
  `acceptance.bash`-alone attempt that produced F18 are in `JOURNAL/`.)
- [x] ✅ **Task 3.2b**: Re-run `deploy.bash` to exercise **check 9b** (the F19
  fix). **PASS, 22 checks, 0 skipped** — 9b named all four unlabelled sessions
  and confirmed each resolves via the name fallback, so `--ccy` provably covers
  the whole fleet rather than only the labelled part of it
- [x] ✅ **Task 3.2c**: Re-run `deploy.bash` to exercise **check 13b** and the
  `select_identity` disclosure it tests (F20). **PASS, 24 checks, 0 skipped** —
  both halves hold: the NOTE names the sessions the axis cannot cover, and it
  stays on stderr so a captured dry-run is still just names
- [x] ✅ **Task 3.2d**: Record the recurring defect class rather than only its
  instances — `CLAUDE/AgentNotes.md` gains *"A partial result read as a complete
  one"* with all five instances, `CLAUDE/QA.md`'s local note points at it, and
  the `qa-reviewer` agent gains the three things to look for
- [x] ✅ **Task 3.3**: Run the `qa-reviewer` agent over the plan's full diff;
  resolve all BLOCK/FIX-BEFORE-MERGE findings. **Verdict: FIX-BEFORE-MERGE, 9
  findings**, all resolved. Four were fresh instances of the partial-result
  class *inside the diff that documents it* — the identity menu row suppressed
  by a cardinality proxy instead of coverage (F21), `none` conflated with
  unlabelled (F22), check 13 able to pass over an empty population (F23), and
  the `COVERAGE: n of m` rule applied to Plan 00080's triage but not to this
  plan's own gate. Unit test 43 → 49 assertions with regressions for the first
  two. See `JOURNAL/` for the full account
- [ ] ⬜ **Task 3.3b**: Re-run `deploy.bash` — the review's fixes touch
  `podfreeze` and `claude-yolo` (3.40.1), so the host is stale again by design,
  and checks 13/13b plus the new COVERAGE line have not run against them
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
- [ ] New CCY containers carry `ccy`, `ccy-project`, `ccy-github`, `ccy-token`
  and `ccy-ssh-keys` labels, and `podfreeze --github <id>` resolves exactly the
  sessions on that account (D4, D8)
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
     JOURNAL/00079-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created + research logged
- Phase 0 host triage run; decision gate passed, Phase 1 demoted
- Phase 2 built: `podfreeze`, `play-podfreeze.yml`, docs; plus the
  plan's `deploy.bash` + `acceptance.bash` (Task 3.1)
