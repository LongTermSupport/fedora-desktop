# Plan 00061: Headless / Server Provisioning Support

**Status**: In Progress
**Created**: 2026-07-20
**Owner**: joseph
**Priority**: Medium

## Overview

Today this repo provisions a **Fedora desktop workstation**. Every core play in
`playbooks/playbook-main.yml` targets `hosts: desktop` and the imported set mixes
three very different concerns without separation: GNOME/desktop GUI software
(`play-firefox`, `play-browsers`, `play-gnome-shell`, `play-gnome-shell-extensions`,
`play-gsettings`, `play-terminal-emulators`, `play-vscode`, `play-comms`,
`play-speech-to-text`), genuinely general tooling that is equally useful on a
server (`play-git-configure-and-tools`, `play-python`, `play-podman`,
`play-docker`, `play-nvm-install`, `play-claude-code`, `play-claude-yolo`), and
host-hygiene plays that straddle both.

We want the same repo to be usable to provision a **headless Fedora Server** (no
GNOME, no GUI, likely a remote/SSH box or VM) — installing the general and
server-appropriate pieces while cleanly skipping everything that assumes a
graphical desktop session. The overriding constraint from the requester is
**lowest possible maintenance burden**: whatever mechanism we choose must not
double the number of playbooks, must not require every future play to be written
twice, and must keep the desktop path exactly as it is today.

This plan first **defines the problem and the constraints**, then evaluates
competing design approaches (two independent brainstorm passes — one by Fable,
one by Sonnet — are commissioned as the first step) before committing to a
single design. A key part of the ask is a **QA gate that forces every play to
declare its scope** (`scope-gnome` | `scope-general` | `scope-server`) so the
desktop/server split can never silently drift.

## Goals

- Enable this repo to provision a **headless Fedora Server** (no GNOME / no GUI)
  from the same source tree that provisions the desktop today.
- Achieve the split with the **lowest ongoing maintenance burden** — ideally one
  play stays one play; no wholesale duplication of the playbook set.
- Introduce a **scope taxonomy** — every play is classified as exactly one of
  `scope-gnome`, `scope-general`, or `scope-server` (final tag names TBD by the
  design decision).
- Add a **QA gate** (wired into `./scripts/qa-all.bash`) that **fails** if any
  play is missing a scope declaration or declares an invalid/multiple scope —
  making correct scoping mandatory and drift-proof.
- Preserve the existing desktop provisioning behaviour with **zero regression**
  (the default `playbook-main.yml` run on a desktop must produce the same result).
- Provide a documented, discoverable way to run the **server subset**
  (e.g. a tag selector, a separate entry playbook, or an inventory/host-group
  mechanism — the winning design decides).

## Non-Goals

- **Not** building a distinct product or a second repo — this is one source tree,
  two provisioning profiles.
- **Not** hardening/CIS-benchmarking a server, nor adding server-app roles
  (web servers, databases, k8s). This plan is about the **split mechanism + QA**,
  not new server workloads. Those can come later on top of the taxonomy.
- **Not** rewriting the desktop plays' internals — only classifying them and,
  where a play genuinely mixes concerns, the design decides whether to tag,
  split, or gate individual tasks.
- **Not** changing the kickstart/bare-metal install pipeline (Plans 00018/00022) —
  that is upstream of provisioning; this plan starts once a minimal Fedora is up.

## Context & Background

- **Current entry point**: `playbooks/playbook-main.yml` imports ~31 core plays
  unconditionally, all `hosts: desktop`, plus a large `imports/optional/**` tree.
- **Existing tag usage**: only ~11 of 31 core plays declare any `tags:` today, and
  those tags are task-level (`packages`, `pyenv`, …), not scope-level. There is no
  consistent play-level classification to build on yet.
- **IaC discipline** (`CLAUDE/InfrastructureAsCode.md`): the split must be
  declarative and reproducible — no runtime "am I a server?" probing that creates
  drift. Prefer a host-group / tag / variable mechanism the repo *declares*.
- **Fail-fast + QA mandate** (`CLAUDE.md`, `CLAUDE/QA.md`): the scope QA gate must
  be a hard failure, consistent with the project's #1 rule, and run inside
  `qa-all.bash` like the other Ansible gates.
- **Reference gates to mirror**: `scripts/qa-ansible.bash` (grep-based playbook
  hygiene) and `scripts/qa-ansible-syntax.bash` (per-playbook `--syntax-check`)
  are the pattern a new `scope` gate should follow.

## Candidate Design Axes (to be resolved by the brainstorm → decision gate)

These are the open questions the two brainstorm docs must address — not yet
decided:

1. **Selection mechanism** — Ansible `--tags/--skip-tags`? A separate
   `playbook-server.yml` entry point? Two host groups (`desktop` / `server`) in
   inventory? A `profile` variable gating imports? Trade-offs on maintenance.
2. **Where scope lives** — a play-level `tags:` on the `- hosts:` block? A
   sidecar manifest? A naming convention? A variable in each play? Which is
   least effort to keep correct and easiest for the QA gate to parse.
3. **Mixed-concern plays** — how to handle a play that installs both general and
   GNOME pieces (split it, tag tasks within it, or gate blocks by scope var).
4. **QA gate shape** — static grep vs. a small parser; how it enforces
   "exactly one scope per play"; how it handles `imports/optional/**`.
5. **Server host baseline** — what does "server" even include here, minimally
   (git, python, podman/docker, claude tooling), and what desktop-hygiene plays
   are actually GUI-coupled vs. safe-anywhere.

## Tasks

### Phase 1: Problem definition & brainstorm (this session)

- [x] ✅ **Task 1.1**: Scaffold plan, define the problem, goals, non-goals, and the open design axes above.
- [x] ✅ **Task 1.2**: Commission two independent brainstorm passes into `brainstorm-fable.md` and `brainstorm-sonnet.md`, each proposing a full approach (mechanism + scope location + QA gate + mixed-play handling) with pros/cons and a concrete first-slice.
- [x] ✅ **Task 1.3**: Compare the two brainstorms; pick the winning design (or a synthesis) at a decision gate recorded in this PLAN.md — see **Decision Gate** below.

### Phase 2: Design commit (decision gate PASSED — awaiting user go-ahead to implement)

- [x] ✅ **Task 2.1**: Record the chosen mechanism (native play-level `tags:` + `--skip-tags`, Sonnet + Fable grafts) as **Technical Decision 1** below.
- [ ] ⬜ **Task 2.2**: **Exhaustive per-play task sweep** (not a one-pass read — the two agents found different single mixed plays). Classify every core play into exactly one `scope-*` bucket, catalogue every mixed-concern task, and resolve the flagged ambiguous calls (`play-rpm-fusion`, `play-ms-fonts`, `play-terminal-emulators`, `play-systemd-user-tweaks`, `play-python`).

### Phase 3: Implementation (native tags + `--skip-tags` design)

- [ ] ⬜ **Task 3.1**: Add a play-level `tags: [scope-*]` block to every core play per the Task 2.2 catalogue.
- [ ] ⬜ **Task 3.2**: Handle mixed plays — task-level `scope-gnome` tag for trivial single-item exceptions; split into a single-scope file when the mixed concern is non-trivial.
- [ ] ⬜ **Task 3.3**: Add the scope check as a 4th grep/awk check **inside `scripts/qa-ansible.bash`** (no `qa-all.bash` positional-merge changes); it must fail on missing/invalid/multiple play-level scope tags.
- [ ] ⬜ **Task 3.4**: Document both canonical commands (desktop `--skip-tags scope-server`, server `--skip-tags scope-gnome`) in `docs/`, and note the scope-tag grammar in `CLAUDE/AnsibleStyle.md`.
- [ ] ⬜ **Task 3.5**: Run QA: `./scripts/qa-all.bash`; fix findings.
- [ ] ⬜ **Task 3.6**: Prove zero regression — `ansible-playbook playbook-main.yml --skip-tags scope-server --check --list-tasks` must be identical to today's unscoped `--list-tasks`; the server command must visibly drop every GNOME play.
- [ ] ⬜ **Task 3.7**: (On HOST) validate a headless run in a VM/container; confirm no GNOME/GUI plays execute.

## Decision Gate: Fable vs Sonnet

Two independent brainstorms were commissioned (`brainstorm-fable.md`,
`brainstorm-sonnet.md`). Both are well-grounded in the real repo; both correctly
reject a second `playbook-server.yml` (duplication) and inventory host groups
(the repo is one host, `localhost`, local transport). They diverge on the two
things that matter most: **the selection mechanism** and **where scope lives**.

| Axis                | Fable                                                                     | Sonnet                                                                            |
| ------------------- | ------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| Mechanism           | New `provisioning_profile` extra-var + `when:` on each `import_playbook:` | Native Ansible play-level `tags:` + `--skip-tags` at invocation                   |
| Scope declared in   | A `# scope: X` **comment** on the import line in `playbook-main.yml`      | The play's own `tags:` block, inside each play file                               |
| Desktop command     | Bare `ansible-playbook playbook-main.yml` (literally unchanged)           | `ansible-playbook playbook-main.yml --skip-tags scope-server`                     |
| Server command      | `-e provisioning_profile=server`                                          | `--skip-tags scope-gnome`                                                         |
| Mixed-concern plays | Split into a new single-scope file (found `play-basic-configs` USB-audio) | Task-level tag override, keep play scope (found `play-vpn` NM-openvpn-gnome)      |
| QA gate             | New `qa-ansible-scope.bash` as a **7th stage** in `qa-all.bash`           | 4th check folded into existing `qa-ansible.bash` — **zero `qa-all.bash` changes** |

### Verdict — Sonnet's design is the primary, with two grafts from Fable

**Chosen: Sonnet's approach** — play-level native `tags:` selected with
`--skip-tags`, QA gate folded into `qa-ansible.bash`. Rationale:

1. **Scope and selection are the same object.** With native tags, the thing that
   declares scope *is* the thing that drives selection. Fable's design carries an
   inert `# scope:` comment **plus** a separate `when:` guard that must agree — so
   it needs a gate cross-check just to police drift between two artifacts that
   Sonnet's design collapses into one. Fewer moving parts = lower burden.
2. **Fable's central objection to tags is incorrect.** Fable rejects tags because
   "tag inheritance is additive, not subtractive." True for `--tags` (allow-list
   selection), but Sonnet uses `--skip-tags` (subtractive) — which *does* drop a
   `scope-gnome`-tagged task from within an otherwise-selected `scope-general`
   play. Sonnet demonstrates the exact working counter-example (`play-vpn.yml`).
   The mixed-play case Fable said tags "cannot express cleanly" is expressed
   cleanly by skip-tags. This also happens to be **the user's own instinct**
   ("using tags for things that are gnome only").
3. **QA integration is concretely lower-risk.** Folding the check into the
   existing `qa-ansible.bash` avoids touching `qa-all.bash`'s positional `jq -s`
   JSON merge — which `qa-all.bash`'s own comments explicitly warn is fragile to
   new stages. Fable's 7th-stage approach walks straight into that footgun.

**Grafted from Fable (adopted):**

- **A) File-split for *substantial* mixed concerns.** Sonnet's task-level tag
  override is right for a trivial one-package exception (`play-vpn`); Fable's
  "split into a single-scope file" is better when a play genuinely carries a
  meaningful block of the other scope, keeping "one file = one scope" airtight.
  Rule: default to play-level tag; task-tag override for a trivial in-play
  exception; split the file when the mixed concern is non-trivial.
- **B) Both canonical commands documented, always.** Adopt Sonnet's own point
  (which Fable's zero-regression framing reinforces): the desktop command must
  explicitly `--skip-tags scope-server` so a *future* server-only play never
  silently runs on a desktop. Zero-regression must hold structurally, not just
  for today's empty `scope-server` bucket.

**Synthesis finding for Phase 2:** the two agents found **different** single
mixed-concern plays (`play-basic-configs` USB-audio-fix vs `play-vpn`
NM-openvpn-gnome). That proves a one-pass classification is unreliable — Phase 2
must do an **exhaustive per-play task sweep**, not trust either single find. The
two disagreed classification calls to resolve at Phase 2:
`play-rpm-fusion` (Fable→general, Sonnet→gnome) and `play-ms-fonts`/
`play-terminal-emulators`/`play-systemd-user-tweaks`/`play-python`
(various ambiguous flags across both docs).

## Technical Decisions

### Decision 1: Scope mechanism = native play-level `tags:` + `--skip-tags`

**Context**: Need to run a server subset from the desktop source tree at lowest
maintenance burden, with mandatory per-play scope classification.
**Options considered**: (A) `provisioning_profile` var + `when:` on imports +
`# scope:` comment [Fable]; (B) native play-level `tags:` + `--skip-tags`
[Sonnet]; (C) second playbook / inventory host groups [both rejected].
**Decision**: **(B)**, because scope declaration and selection become one native
Ansible object (no comment/guard drift, no bespoke variable), `--skip-tags` is
subtractive so mixed plays work, and the QA gate needs no `qa-all.bash` surgery.
Graft Fable's file-split rule for non-trivial mixed plays and the dual-command
zero-regression discipline.
**Date**: 2026-07-20

### Decision 2: Ambiguous classifications resolved by the owner

**Context**: The two brainstorms disagreed on a few play classifications.
**Decision** (owner, 2026-07-20):

- `play-rpm-fusion` → **`scope-general`**. It only enables the RPM Fusion
  free/nonfree repositories — foundational plumbing many later plays depend on
  (codecs, ffmpeg, hardware drivers). It is not GNOME-specific in any way, and
  *omitting* it on a server risks breaking downstream package installs. (Fable's
  read; overrides Sonnet's `scope-gnome`.)
- Remaining flagged calls (`play-ms-fonts`, `play-terminal-emulators`,
  `play-systemd-user-tweaks`, `play-python`) to be settled during the Phase 2
  exhaustive sweep inside the full proposal.
  **Date**: 2026-07-20

### Decision 3: Refinement loop — Sonnet proposes, Fable audits, owner-agent judges

**Context**: The chosen design (Decision 1) needs to become an
implementation-ready proposal before Phase 3.
**Process**: Sonnet authors a full proposal (`PROPOSAL.md`); Fable adversarially
audits it (`AUDIT-round-N.md`); the orchestrating agent judges each round
(real must-fix vs nitpick), feeds real findings back to Sonnet, and loops until
convergence (Fable raises nothing material). Convergence + judge sign-off gates
entry to Phase 3 implementation.
**Date**: 2026-07-20

## Success Criteria

- [ ] A documented command provisions a headless Fedora Server with no GNOME/GUI
  plays running, from this repo.
- [ ] Every play declares exactly one scope; the QA gate **fails** on a missing,
  invalid, or multiple-scope declaration.
- [ ] The scope gate is part of `./scripts/qa-all.bash`.
- [ ] Desktop provisioning is unchanged (zero regression).
- [ ] The maintenance burden is demonstrably low: adding a future play requires
  only a single scope tag, not a duplicated playbook.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Plan created; problem defined; Fable + Sonnet brainstorms commissioned.
