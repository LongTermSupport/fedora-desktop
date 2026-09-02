# Plan 00061 — Design Decisions

Durable decision record for the headless-server provisioning mechanism. The
narrative of how each decision was reached is in `JOURNAL/`; the
implementation-ready specification is `PROPOSAL.md` (round 6); the verbatim
historical plan text is `PLAN_archive.md`.

## Context & Background

- **Entry point**: `playbooks/playbook-main.yml` imports ~31 core plays
  unconditionally, all `hosts: desktop`, plus a large `imports/optional/**` tree.
- **Pre-plan tag usage**: only ~11 of 31 core plays declared any `tags:`, all
  task-level (`packages`, `pyenv`, …) — no play-level classification existed.
- **IaC discipline** (`CLAUDE/InfrastructureAsCode.md`): the split must be
  declarative and reproducible — a host-group / tag / variable mechanism the repo
  *declares*, not ad-hoc runtime probing that creates drift.
- **Fail-fast + QA mandate** (`CLAUDE.md`, `CLAUDE/QA.md`): the scope gate is a
  hard failure inside `qa-all.bash`, mirroring `scripts/qa-ansible.bash` and
  `scripts/qa-ansible-syntax.bash`.

## Design axes the brainstorms had to resolve

1. **Selection mechanism** — `--tags/--skip-tags`, a separate `playbook-server.yml`,
   inventory host groups, or a `profile` variable gating imports.
2. **Where scope lives** — play-level `tags:`, a sidecar manifest, a naming
   convention, or a variable in each play.
3. **Mixed-concern plays** — split, tag tasks within, or gate blocks by scope var.
4. **QA gate shape** — static grep vs. a small parser; enforcing "exactly one
   scope per play"; handling `imports/optional/**`.
5. **Server host baseline** — what "server" minimally includes (git, python,
   podman/docker, claude tooling) and which hygiene plays are GUI-coupled.

## Decision Gate: Fable vs Sonnet brainstorms

Two independent brainstorms (`brainstorm-fable.md`, `brainstorm-sonnet.md`). Both
rejected a second `playbook-server.yml` (duplication) and inventory host groups
(the repo is one host, `localhost`, local transport). They diverged on the
selection mechanism and where scope lives.

| Axis                | Fable                                                                     | Sonnet                                                                            |
| ------------------- | ------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| Mechanism           | New `provisioning_profile` extra-var + `when:` on each `import_playbook:` | Native Ansible play-level `tags:` + `--skip-tags` at invocation                   |
| Scope declared in   | A `# scope: X` **comment** on the import line in `playbook-main.yml`      | The play's own `tags:` block, inside each play file                               |
| Desktop command     | Bare `ansible-playbook playbook-main.yml` (literally unchanged)           | `ansible-playbook playbook-main.yml --skip-tags scope-server`                     |
| Server command      | `-e provisioning_profile=server`                                          | `--skip-tags scope-gnome`                                                         |
| Mixed-concern plays | Split into a new single-scope file (found `play-basic-configs` USB-audio) | Task-level tag override, keep play scope (found `play-vpn` NM-openvpn-gnome)      |
| QA gate             | New `qa-ansible-scope.bash` as a **7th stage** in `qa-all.bash`           | 4th check folded into existing `qa-ansible.bash` — **zero `qa-all.bash` changes** |

**Verdict**: Sonnet's design primary, with two Fable grafts. Rationale:

1. **Scope and selection are the same object.** Native tags make the declaration
   drive the selection; Fable's inert `# scope:` comment plus a separate `when:`
   guard needs a cross-check just to police drift between two artifacts.
2. **Fable's objection to tags was wrong.** "Tag inheritance is additive" holds for
   `--tags`, but `--skip-tags` is subtractive and does drop a `scope-gnome` task
   from an otherwise-selected general play (Sonnet's `play-vpn.yml` example).
3. **Lower QA-integration risk.** Folding into `qa-ansible.bash` avoids touching
   `qa-all.bash`'s fragile positional `jq -s` merge.

**Grafts from Fable**: (A) file-split for *substantial* mixed concerns (task-level
override only for a trivial in-play exception); (B) both canonical commands
always documented so a future server-only play never silently runs on a desktop.

**Synthesis finding**: the two agents found *different* single mixed plays, so a
one-pass classification is unreliable — Phase 2 had to do an exhaustive per-play
task sweep. Disagreed calls to settle: `play-rpm-fusion`, `play-ms-fonts`,
`play-terminal-emulators`, `play-systemd-user-tweaks`, `play-python`.

## Decision 1: Scope mechanism = native play-level `tags:` + `--skip-tags`

**Context**: run a server subset from the desktop tree at lowest maintenance
burden with mandatory per-play scope classification.
**Options**: (A) `provisioning_profile` var + `when:` on imports + `# scope:`
comment [Fable]; (B) native play-level `tags:` + `--skip-tags` [Sonnet];
(C) second playbook / inventory host groups [both rejected].
**Decision**: (B) — declaration and selection become one native object,
`--skip-tags` is subtractive so mixed plays work, and the gate needs no
`qa-all.bash` surgery. Graft Fable's file-split rule and dual-command discipline.
**Date**: 2026-07-20. *Selection layer later superseded by Decisions 4 and 5.*

## Decision 2: Ambiguous classifications resolved by the owner

- `play-rpm-fusion` → **general**: it only enables RPM Fusion repositories,
  foundational plumbing many later plays depend on; omitting it on a server risks
  breaking downstream installs. (Fable's read; overrides Sonnet's gnome.)
- `play-ms-fonts` → gnome, `play-terminal-emulators` → gnome,
  `play-systemd-user-tweaks` → general, `play-python` → general — settled in
  the Phase 2 exhaustive sweep (`PROPOSAL.md` §1).

**Date**: 2026-07-20

## Decision 3: Refinement loop — Sonnet proposes, Fable audits, owner-agent judges

Sonnet authors `PROPOSAL.md`; Fable adversarially audits (`AUDIT-round-N.md`);
the orchestrating agent judges each round (must-fix vs nitpick), feeds real
findings back, and loops until Fable raises nothing material. Convergence plus
judge sign-off gates entry to Phase 3. Six rounds were run across three
mechanisms; the audit files and `PROPOSAL.md`'s revision logs are the evidence.
**Date**: 2026-07-20

## Decision 4: Pivot to `when:` + auto-detected `provisioning_profile` (supersedes Decision 1's selection layer)

**Context**: new owner requirement — auto-detect desktop vs headless server with
zero config and zero runtime flags.

**Key fact**: `--skip-tags` is resolved by the CLI before any fact exists, so a
tag design can never self-configure; `when:` is evaluated at runtime and can
consume an auto-detected variable. The owner ruled "convert a Server install into
a GNOME desktop" out of scope, so `systemctl get-default` (`graphical.target` vs
`multi-user.target`) reflects genuine intent.

**Decision**: gate the core `import_playbook:` lines with `when:` on a
`provisioning_profile` variable, auto-computed from `systemctl get-default`
(server-biased when uncertain; `-e provisioning_profile=…` overrides for
testing/CI). Everything mechanism-independent from the converged proposal is
reused verbatim: the classification (21 general / 10 gnome), the three mixed-play
discoveries, the `prevent-ssh-suspend` gsettings latent bug, the container-watch
`register`/`when` hazard.

**Prototype evidence** (`prototype-when-import.md`, ansible-core 2.19.11): `when:`
on `import_playbook` gates correctly at runtime. Caveat: `--list-tasks` does not
evaluate `when:`, so the cheap in-container static skip-proof is gone —
end-to-end skip verification is host-side.

**Trade-offs accepted**: core-play scope lives at the import site (plays less
self-describing); weaker in-container static verification.
**Date**: 2026-07-20. *Selection layer superseded by Decision 5; detection layer kept.*

## Decision 5: Gate lives IN each play (self-guard) — every play standalone-runnable + auto-gating

**Context**: new owner requirement — every play must be runnable standalone
(`ansible-playbook playbooks/imports/play-X.yml`). Decision 4's import-site gate
does not fire on a standalone run, so a gnome core play run by itself would
execute on a server.

**Decision**: move the gate into each play as a top-of-play guard
(`ansible.builtin.meta: end_play` with a `when:` driven by a per-play `scope:`
var); drop the import-site `when:` lines. This unifies core and optional plays
under one self-gating mechanism, makes every play self-describing, and lets one
uniform guard expression cover all three buckets (general never ends; gnome ends
when profile is server; server ends when profile is not server). The
`group_vars/desktop.yml` auto-detect layer is unchanged and verified to load on a
standalone run.

**Prototype evidence** (`prototype-self-guard.md`, ansible-core 2.19.11, run
standalone): `group_vars/desktop.yml` loads on a bare single-play run; both
`meta: end_play` and `meta: end_host` honour `when:`.

**Resolved in the round-5/6 loop** (`PROPOSAL.md` §3, §5): a uniform 2-task guard
(assert the profile is valid, then `end_play`), `end_play` over `end_host`,
general plays carry no guard and the gate rejects one, the QA gate is a single
uniform per-play check, and the standalone-typo gap is closed by the guard's own
assert.
**Date**: 2026-07-20

## Open owner calls made during implementation (owner may revisit)

- `play-virtualbox-windows.yml` split: engine install scoped **general**
  (VirtualBox is headless-capable via `VBoxHeadless`/`VBoxManage`); the split-off
  `play-virtualbox-windows-vm-setup.yml` (downloads/imports a Windows 11 desktop
  VM) scoped **gnome**.
- Medium/Low optional rows read and classified from their real task lists:
  `collaboration`, `ftp-camera`, `lastpass`, `hd-audio`, `cloudflare-warp` are
  **general** (the fast pass had guessed gnome); `nordvpn-openvpn` and `rclone`
  are mixed; `qobuz` kept **general** with its GUI-Flatpak tasks gated so the
  headless CLI player and scrobbler survive on a server.
