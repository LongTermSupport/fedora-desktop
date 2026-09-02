# Plan 00061: Headless / Server Provisioning Support

**Status**: Dormant (blocked on Task 3.8: the plan-local `acceptance.bash` must run on the laptop HOST; it refuses to run in the CCY container)
**Created**: 2026-07-20
**Owner**: joseph
**Priority**: Medium

## Overview

This repo provisions a **Fedora desktop workstation**: every core play in
`playbooks/playbook-main.yml` targets `hosts: desktop`, and the imported set mixes
GNOME/GUI software, genuinely general tooling (git, python, podman, docker, nvm,
claude tooling), and host-hygiene plays that straddle both.

We want the same source tree to provision a **headless Fedora Server** (no GNOME,
no GUI) — installing the general and server-appropriate pieces while cleanly
skipping everything that assumes a graphical session. The overriding constraint
is **lowest possible maintenance burden**: one play stays one play, no duplicated
playbook set, and the desktop path stays exactly as it is today.

The chosen mechanism is a **self-guarding play**: every play declares
`vars: {scope: general|gnome|server}`, gnome/server plays carry a canonical 2-task
guard that ends the play when the auto-detected `provisioning_profile` does not
match, and a QA gate in `scripts/qa-ansible.bash` enforces both. The design went
through two brainstorms, a decision gate, and six adversarial audit rounds across
three mechanisms — see [DECISIONS.md](DECISIONS.md) for the rationale and
[PROPOSAL.md](PROPOSAL.md) for the implementation-ready specification.

## Goals

- Provision a **headless Fedora Server** from the same source tree that
  provisions the desktop today.
- Achieve the split with the **lowest ongoing maintenance burden** — no wholesale
  duplication of the playbook set.
- Introduce a **scope taxonomy** — every play is exactly one of `general`,
  `gnome`, or `server`.
- Add a **QA gate** (wired into `./scripts/qa-all.bash`) that **fails** if any
  play is missing a scope declaration or declares an invalid/multiple scope.
- Preserve existing desktop provisioning with **zero regression**.
- Provide a documented, discoverable way to run the **server subset**.

## Non-Goals

- **Not** building a distinct product or a second repo — one source tree, two
  provisioning profiles.
- **Not** hardening/CIS-benchmarking a server, nor adding server-app roles (web
  servers, databases, k8s). Those can come later on top of the taxonomy.
- **Not** rewriting the desktop plays' internals — only classifying them and,
  where a play genuinely mixes concerns, tagging, splitting, or gating tasks.
- **Not** changing the kickstart/bare-metal install pipeline (Plans 00018/00022).

## Supporting documents

- [DECISIONS.md](DECISIONS.md) — context, design axes, the Fable-vs-Sonnet
  decision gate, Decisions 1–5, and the owner calls made during implementation.
- [PROPOSAL.md](PROPOSAL.md) — round-6 implementation-ready spec (classification,
  detection layer, guard, QA Check 4, mixed-play edits, verification procedure).
- [brainstorm-fable.md](brainstorm-fable.md), [brainstorm-sonnet.md](brainstorm-sonnet.md)
  — the two independent design brainstorms.
- [AUDIT-round-1.md](AUDIT-round-1.md), [AUDIT-round-2.md](AUDIT-round-2.md),
  [AUDIT-round-3.md](AUDIT-round-3.md), [AUDIT-round-5.md](AUDIT-round-5.md) —
  Fable's adversarial audits of each proposal round.
- [prototype-when-import.md](prototype-when-import.md),
  [prototype-self-guard.md](prototype-self-guard.md) — prototype evidence for
  Decisions 4 and 5.
- [acceptance.bash](acceptance.bash) — HOST-run end-to-end gating suite (Task 3.8).
- [PLAN_archive.md](PLAN_archive.md) — the previous full-length plan, kept verbatim.
- `JOURNAL/` — the day-by-day activity log.

## Tasks

### Phase 1: Problem definition & brainstorm

- [x] ✅ **Task 1.1**: Scaffold plan, define the problem, goals, non-goals, and the open design axes ([DECISIONS.md](DECISIONS.md)).
- [x] ✅ **Task 1.2**: Commission two independent brainstorm passes into `brainstorm-fable.md` and `brainstorm-sonnet.md`, each proposing a full approach (mechanism + scope location + QA gate + mixed-play handling) with pros/cons and a concrete first slice.
- [x] ✅ **Task 1.3**: Compare the two brainstorms; pick the winning design at a decision gate — recorded in [DECISIONS.md](DECISIONS.md) ("Decision Gate").

### Phase 2: Design commit — proposal converged, design gate passed

- [x] ✅ **Task 2.1**: Record the chosen mechanism (native play-level `tags:` + `--skip-tags`, Sonnet + Fable grafts) as Decision 1 in [DECISIONS.md](DECISIONS.md).
- [x] ✅ **Task 2.2**: Exhaustive per-play task sweep in `PROPOSAL.md` §1 (all 31 core plays task-by-task; found a 3rd mixed play, `play-prevent-ssh-suspend`'s gsettings task, a real latent bug). Ambiguous calls settled per Decision 2. Optional tree fast-pass-classified.
- [x] ✅ **Task 2.3**: Sonnet↔Fable refinement loop run to convergence — 6 rounds across 3 mechanisms (tags → `when:`-on-import → self-guard), each forced by a sharpened owner requirement (Decisions 3, 4, 5), each preserving the classification verbatim. Audit trail: `AUDIT-round-{1,2,3,5}.md`; `PROPOSAL.md` (round 6) is the implementation-ready spec.

### Phase 3: Implementation (self-guarding plays + auto-detected `provisioning_profile`)

- [x] ✅ **Task 3.1**: Added `environment/localhost/group_vars/desktop.yml` computing `provisioning_profile` from `systemctl get-default` (server-biased; `-e` overridable) + the batch-run fail-fast assert in `play-AA-preflight-sanity.yml` (`PROPOSAL.md` §2).
- [x] ✅ **Task 3.2**: Added `vars: {scope: …}` to every play (73 plays: 31 core + 41 optional + `dev/play-collect-diagnostics.yml`) per `PROPOSAL.md` §1; added the exact 2-task self-guard as the first two tasks of every gnome play. The byte-exact Check 4 gate verified every one.
- [x] ✅ **Task 3.3**: Applied the mixed-play task-level `when:` edits + the container-watch list-form `when:` override (`PROPOSAL.md` §6); split `play-virtualbox-windows.yml` into engine install (`general`) + `play-virtualbox-windows-vm-setup.yml` (`gnome`). Two further mixed plays found and handled: `play-nordvpn-openvpn.yml` and `play-rclone.yml`; `play-qobuz.yml` kept `general` with its GUI-Flatpak tasks gated.
- [x] ✅ **Task 3.4**: Added the uniform Check 4 to `scripts/qa-ansible.bash` (`PROPOSAL.md` §5); zero `qa-all.bash` changes; `playbook-main.yml` untouched.
- [x] ✅ **Task 3.5**: Read every Medium/Low-confidence optional row and classified from real task lists — corrections recorded in [DECISIONS.md](DECISIONS.md) ("Open owner calls").
- [x] ✅ **Task 3.6**: Documented the zero-flag + standalone + `-e` override in `docs/playbooks.md` ("Desktop vs. Headless Server Provisioning") and the `scope:`/guard grammar in `CLAUDE/AnsibleStyle.md` ("Provisioning Profile Self-Guard").
- [x] ✅ **Task 3.7**: `./scripts/qa-all.bash` → PASS: every playbook scope+guard OK, every `--syntax-check` OK, all six stages green.
- [ ] ⬜ **Task 3.8**: (On HOST) verify gating end-to-end via the plan-local [acceptance.bash](acceptance.bash) — server-path guard proof (no `--check` needed because `meta: end_play` ends each gnome play before its first real task), desktop path via `-e provisioning_profile=desktop --check`, read-only detection + typo checks. It refuses to run in the CCY container. Run on the laptop host: `./CLAUDE/Plan/00061-headless-server-provisioning/acceptance.bash`.

**Owner review items** (defensible calls made during implementation, may be
revisited): the `play-virtualbox-windows` split scopes and the `qobuz`
general+mixed classification — see [DECISIONS.md](DECISIONS.md).

## Success Criteria

- [ ] A documented command provisions a headless Fedora Server with no GNOME/GUI
  plays running, from this repo.
- [ ] Every play declares exactly one scope; the QA gate **fails** on a missing,
  invalid, or multiple-scope declaration.
- [ ] The scope gate is part of `./scripts/qa-all.bash`.
- [ ] Desktop provisioning is unchanged (zero regression).
- [ ] The maintenance burden is demonstrably low: adding a future play requires
  only a single scope declaration, not a duplicated playbook.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Plan created; problem defined; Fable + Sonnet brainstorms commissioned.
- Design converged after six audit rounds; `PROPOSAL.md` round 6 is the spec.
- Phase 3 implemented on `feat/00061-headless-server-provisioning`; QA green.
- Plan slimmed and marked Dormant pending the HOST-run `acceptance.bash` (Task 3.8).
