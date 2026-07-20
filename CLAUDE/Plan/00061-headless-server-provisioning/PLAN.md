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

### Phase 2: Design commit — **COMPLETE (proposal converged, design gate passed)**

- [x] ✅ **Task 2.1**: Record the chosen mechanism (native play-level `tags:` + `--skip-tags`, Sonnet + Fable grafts) as **Technical Decision 1** below.
- [x] ✅ **Task 2.2**: Exhaustive per-play task sweep done in `PROPOSAL.md` §1 (all 31 core plays task-by-task; the sweep found a **3rd** mixed play — `play-prevent-ssh-suspend`'s gsettings task, a real latent bug — that a one-pass read missed). Ambiguous calls settled: `rpm-fusion`→general (owner), `ms-fonts`→gnome, `terminal-emulators`→gnome, `systemd-user-tweaks`→general, `python`→general. Optional tree fast-pass-classified (Medium/Low rows flagged for verify-on-implement).
- [x] ✅ **Task 2.3**: Sonnet↔Fable refinement loop run to **convergence** — **6 rounds across 3 mechanisms**, each mechanism forced by a sharpened owner requirement, each preserving the classification verbatim:
  - **Tags** (Decision 1): r1→`AUDIT-round-1.md` (1 blocker `set -e` + 4 should-fix)→r2→`AUDIT-round-2.md` (zero findings).
  - **`when:`-on-import** (Decision 4, auto-detect requirement): r3→`AUDIT-round-3.md` (1 blocker: 4b flagged all 31 core plays; 1 should-fix: orphaned-`when:`)→r4→judge independently re-ran final Check 4a (clean).
  - **Self-guard** (Decision 5, standalone requirement): r5→`AUDIT-round-5.md` (0 blockers; 1 should-fix: guard-field comment-strip; Fable verified the canonical guard passes its own gate byte-for-byte)→r6 judge applied + independently re-verified the fix (canonical matches, comment-suffixed guard now matches). **CONVERGED.**
  - `PROPOSAL.md` (round 6) is the implementation-ready spec.

### Phase 3: Implementation (self-guarding plays + auto-detected `provisioning_profile`) — **ready; per `PROPOSAL.md` §8 checklist**

- [ ] ⬜ **Task 3.1**: Add `environment/localhost/group_vars/desktop.yml` computing `provisioning_profile` from `systemctl get-default` (server-biased; `-e` overridable) + the batch-run fail-fast assert in `play-AA-preflight-sanity.yml` (`PROPOSAL.md` §2).
- [ ] ⬜ **Task 3.2**: Add `vars: {scope: general|gnome|server}` to **every** play (all 31 core + 41 optional) per `PROPOSAL.md` §1; add the exact 2-task self-guard (assert + `meta: end_play`) as the first two tasks of every **gnome/server** play (general plays carry no guard).
- [ ] ⬜ **Task 3.3**: Apply the 3 mixed-play task-level `when:` edits + the container-watch list-form `when:` override (`PROPOSAL.md` §6); split `play-virtualbox-windows.yml` (two `- hosts:` plays) first.
- [ ] ⬜ **Task 3.4**: Add the uniform Check 4 (`vars.scope` + guard-presence per play; rejects a guard on a general play) to `scripts/qa-ansible.bash` (`PROPOSAL.md` §5); no `qa-all.bash` changes. `playbook-main.yml` stays bare imports (no `when:`).
- [ ] ⬜ **Task 3.5**: **Read Medium/Low-confidence optional rows** (`PROPOSAL.md` §1.3) before trusting the fast-pass `scope:` value.
- [ ] ⬜ **Task 3.6**: Document the zero-flag command (`ansible-playbook playbook-main.yml` auto-detects) + standalone runs (`ansible-playbook playbooks/imports/play-X.yml` also auto-gate) + the `-e provisioning_profile=…` override in `docs/playbooks.md`; note the `scope:`/guard grammar in `CLAUDE/AnsibleStyle.md` (`PROPOSAL.md` §7).
- [ ] ⬜ **Task 3.7**: Run QA: `./scripts/qa-all.bash`; fix findings. In-container static checks only (`--syntax-check` + the QA gate) — `--list-tasks` can NOT prove guard skips.
- [ ] ⬜ **Task 3.8**: (On HOST) verify gating end-to-end via a real/`--check` run, **batch AND standalone**: server profile ends every gnome play; desktop runs them; a standalone gnome play ends on server (`PROPOSAL.md` §7).

**Open owner decisions before/within Phase 3** (genuine judgment calls the design deferred to a human, not blockers to the mechanism):

- `play-virtualbox-windows.yml` scope value(s) — VirtualBox has a real headless mode (same category as the rpm-fusion call); the file must be split into two plays and each scoped.
- The Medium/Low-confidence optional-tree rows in `PROPOSAL.md` §1.3 — verify against the real task list at implementation time.

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

### Decision 4: Mechanism PIVOT — `when:` + auto-detected `provisioning_profile` (supersedes Decision 1's selection layer)

**Context**: A new owner requirement landed after Decision 1 converged: the
system should **auto-detect** the target (desktop vs headless server) and adjust
itself — **zero config, zero runtime flags** — "a hook that checks up front."

**Key technical fact**: `--skip-tags` is resolved by the CLI *before* any fact
exists, so a tag-based design can **never** self-configure — it always needs an
external driver (a flag or a wrapper script). `when:` is evaluated at runtime
against variables, so it *can* consume an auto-detected `provisioning_profile`
and gate itself with no flag and no wrapper. The requirement moved; the right
mechanism moves with it. (The owner also ruled the "convert a Server install
into a GNOME desktop" case out of scope — a desktop comes from the Workstation
ISO, a server from the Server ISO — so `systemctl get-default`
\[`graphical.target` vs `multi-user.target`\] reflects genuine intent, and the
desired-vs-current-state circularity worry is moot.)

**Decision**: Pivot the *selection layer* from `tags:`/`--skip-tags` to `when:`
on the core `import_playbook:` lines, gated by a `provisioning_profile` variable
that is **auto-computed** (default `desktop`; detected from `systemctl get-default`, server-biased when uncertain; `-e provisioning_profile=…`
overrides for testing/CI). `ansible-playbook playbook-main.yml` then
auto-detects and gates with no flags. Everything mechanism-*independent* from
the converged `PROPOSAL.md` is **reused verbatim** — the exhaustive
classification (21 general / 10 gnome), the three mixed-play discoveries, the
`prevent-ssh-suspend` gsettings latent-bug find, the container-watch
`register`/`when` hazard. Only *how we select* changes.

**Prototype evidence** (`prototype-when-import.md`, ansible-core 2.19.11):
`when:` on `import_playbook` gated by `provisioning_profile` **works at runtime**
(server → gnome play `skipping`, `skipped=1`; desktop → runs). **Caveat found:**
`--list-tasks` does **not** evaluate `when:`, so the cheap in-container static
skip-proof that worked for tags is **not** available for `when:` — end-to-end
skip verification is host-side (`--check`/real run; Task 3.8 already is).

**Accepted trade-offs vs Decision 1**: (a) core-play scope now lives at the
import site in `playbook-main.yml` (plays less self-describing — acceptable, as
core plays are batch-only; optional plays keep an in-file scope marker for the
QA gate + docs); (b) weaker in-container static verification (host-side instead).
**Won**: true zero-flag auto-detection, no wrapper, explicit readable gate logic,
and it's arguably *simpler* overall than tags+wrapper.

**Follow-up**: re-run the Decision-3 loop (Sonnet revises `PROPOSAL.md` for the
`when:` mechanism, reusing the classification; Fable audits the deltas) — the
QA gate now validates each core import carries exactly one recognized
profile-gate (unguarded = general, `!= 'server'` = gnome, `== 'server'` =
server) rather than reading a tag.
**Date**: 2026-07-20

### Decision 5: Gate lives IN each play (self-guard), not on the import — every play standalone-runnable + auto-gating (supersedes Decision 4's import-site `when:`)

**Context**: New owner requirement — every play must be runnable **standalone**
(`ansible-playbook playbooks/imports/play-X.yml`), not only via the
`playbook-main.yml` batch. Decision 4's `when:`-on-`import_playbook:` gate lives
in `playbook-main.yml`, so it does **not** fire on a standalone run — a gnome
core play run by itself would execute on a server. The import-site design's
"core plays are batch-only" assumption is void.

**Decision**: Move the profile gate from the import site **into each play** as a
top-of-play guard task (`ansible.builtin.meta: end_play` — or `end_host` —
`when: <play does not match provisioning_profile>`), driven by a per-play
`scope:` var. The `when:`-on-import lines in `playbook-main.yml` are **dropped**.
This unifies core + optional plays (one identical self-gating mechanism — no
more split import-parser [4a] vs `vars.scope` [4b] in the QA gate), makes every
play **self-describing and standalone-runnable**, and a single boilerplate guard
expression referencing `scope:` covers all three buckets (general never ends;
gnome ends when `provisioning_profile == 'server'`; server ends when
`!= 'server'`). The `group_vars/desktop.yml` auto-detect layer (Decision 4 §2)
is **unchanged** — verified it also loads on a standalone run. Classification,
mixed-task handling, and the two latent-bug finds carry over verbatim.

**Prototype evidence** (`prototype-self-guard.md`, ansible-core 2.19.11, run
STANDALONE): (1) `group_vars/desktop.yml` loads on a bare single-play run
(profile auto-computed, gnome play ran on `desktop`); (2) `meta: end_play` and
`meta: end_host` both honor `when:` — with `-e provisioning_profile=server` the
guard fires and the play ends before its real tasks. Both load-bearing
assumptions confirmed.

**Open design questions for the loop**: the exact guard expression (one uniform
guard referencing `scope:` vs a per-scope hardcoded condition); whether general
plays carry the boilerplate guard too (uniformity) or omit it; the QA-gate
rewrite to a **uniform per-play** check (every play has a valid `scope:` var +,
for gnome/server, the matching guard) replacing the 4a/4b split; and one gap — a
typo'd `-e provisioning_profile=` on a **standalone** run bypasses `play-AA`'s
assert (§2.3), so profile validation may need to live in the guard/group_vars
itself.
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
