# Plan 00077: Top-level `ansible_*` fact variables are deprecated

**Status**: In Progress
**Created**: 2026-08-19
**Owner**: joseph
**Priority**: Medium

## Overview

Ansible auto-injects every gathered fact as a top-level variable, so
`ansible_facts['env']` is also reachable as `ansible_env`. That injection
(`INJECT_FACTS_AS_VARS`) is deprecated and **will be removed in ansible-core
2.24**. The host runs **2.19.12** today, and `ansible.cfg` does not set the
option, so it currently defaults to `True` and everything works.

When it goes, every top-level reference becomes an **undefined variable** at
runtime. That is not a soft failure: the task errors mid-play, on the machine,
after earlier tasks have already changed system state.

Surfaced by a real deploy — `play-claude-yolo.yml` printed the deprecation
against line 291 while deploying Plan 00074. It was visible only because that
play happened to be run with output on screen.

## Goals

- Replace every top-level `ansible_*` fact reference with `ansible_facts[...]`.
- Prove the repo is clean rather than assert it: a QA check that fails if a new
  one is introduced.

## Non-Goals

- Not setting `inject_facts_as_vars = False` in `ansible.cfg` as the fix. That
  would be the *test*, not the remedy, and flipping it before the references are
  gone breaks every play at once. Considered as a verification step instead —
  see Task 2.2.
- Not touching `ansible_facts[...]` usages that are already correct.
- Not renaming this repo's **own** variables that begin with `ansible_` (there
  are none, but the QA check must not invent any).

## Context & Background

### Facts

| ID  | Fact                                                                                                                                                                                                     | Source                       |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| F1  | Host runs `ansible [core 2.19.12]`                                                                                                                                                                       | `ansible --version`          |
| F2  | `INJECT_FACTS_AS_VARS` is **not set** in `ansible.cfg`, so it defaults to `True`                                                                                                                         | `ansible.cfg`                |
| F3  | Removal is scheduled for **ansible-core 2.24**                                                                                                                                                           | deprecation text, deploy log |
| F4  | **11 live references** across 7 playbooks, over 3 distinct fact names — `ansible_env` ×3, `ansible_distribution` ×3, `ansible_distribution_major_version` ×4, plus 2 in comments that are not references | repo grep                    |

F4 is the reason this is a small job rather than a migration: the surface is 11
lines, and every one is a mechanical substitution with an exact replacement.

### The files

- `playbooks/imports/play-claude-yolo.yml` — `ansible_env.HOME`
- `playbooks/imports/play-claude-code.yml` — `ansible_env.HOME`
- `playbooks/imports/optional/hardware-specific/play-darktable-ai-gpu.yml` — `ansible_env.HOME`
- `playbooks/imports/play-rpm-fusion.yml` — `ansible_distribution_major_version` ×2
- `playbooks/imports/optional/common/play-darktable-ai-appimage.yml` — `ansible_distribution` ×2
- `playbooks/imports/optional/common/play-darktable-ai-build.yml` — `ansible_distribution` ×2, `ansible_distribution_major_version` ×2

## Tasks

### Phase 1: Replace the references

- [x] ✅ **Task 1.1**: `ansible_env.HOME` → `ansible_facts['env']['HOME']` (3 sites)
- [x] ✅ **Task 1.2**: `ansible_distribution` → `ansible_facts['distribution']` and
  `ansible_distribution_major_version` → `ansible_facts['distribution_major_version']`
  (8 sites). Note the two `assert:` `that:` sites are bare expressions, not
  `{{ }}` templates — both forms need the same substitution
- [x] ✅ **Task 1.3**: `./scripts/qa-all.bash` green, including
  `qa-ansible-syntax.bash`. Also fixed `CLAUDE/AnsibleStyle.md`, whose own
  Preflight Assertions example taught the deprecated form — the style guide and
  the new gate would otherwise contradict each other

### Phase 2: Stop it coming back

- [x] ✅ **Task 2.1**: Add a check to `scripts/qa-ansible.bash` that fails on a
  top-level `ansible_<factname>` reference. It must key on a **known fact-name
  list**, not on the `ansible_` prefix alone — this repo's own `ansible_facts`
  usages and any future `ansible_`-prefixed local var must not trip it
- [ ] ⬜ **Task 2.2**: Prove the fix rather than assume it — run one converted
  play with `ANSIBLE_INJECT_FACTS_AS_VARS=False`, which is exactly the post-2.24
  world. A play that passes under that flag is proven, not argued.
  **HOST action**: `CLAUDE/Plan/00077-…/acceptance.bash`
  - **First run: the negative control did its job and the harness was broken.**
    The environment variable is **`ANSIBLE_INJECT_FACT_VARS`** — no "AS", "FACT"
    singular — not `ANSIBLE_INJECT_FACTS_AS_VARS`, which is what the setting is
    *called* (`INJECT_FACTS_AS_VARS`) and what the ini key resembles
    (`inject_facts_as_vars`). Ansible ignored the non-existent variable in
    silence; the control play printed `distribution is Fedora` and passed.
    Without the control, the positive case would have been run and reported as
    proof of nothing. Verified with `ansible-config list`, and F2 re-confirmed:
    `ansible.cfg` does not set it
  - Script now runs a **cheaper check 0 first** — `ansible-config dump --only-changed` must report the setting as `False`. That catches a wrong
    variable name in one command, before any play runs
  - Runtime coverage is 2 of the 11 sites (the default play is
    `play-rpm-fusion.yml`, idempotent and cheap). The other 9 are held
    statically by `qa-ansible.bash` Check 5 — stated in the script's own output
    rather than left to be assumed

## Dependencies

- Surfaced while deploying Plan 00074; no code dependency on it.

## Technical Decisions

### Decision 1: Fix the references, do not disable the injection

**Context**: The obvious "fix" is `inject_facts_as_vars = False` in `ansible.cfg`.
**Decision**: No. That flag is the **verification**, not the remedy — turning it
off before the references are converted breaks every affected play at once, and
turning it off *after* adds nothing that 2.24 will not do anyway. Task 2.2 uses
it as a one-shot proof via the environment variable, leaving `ansible.cfg`
alone.
**Date**: 2026-08-19

## Success Criteria

- [x] Zero top-level `ansible_*` fact references in `playbooks/ tasks/ vars/ environment/`
- [x] `qa-all.bash` fails if one is reintroduced
- [ ] At least one converted play runs clean under `ANSIBLE_INJECT_FACTS_AS_VARS=False`

## Risks & Mitigations

| Risk                                                       | Impact | Probability | Mitigation                                                                       |
| ---------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------- |
| A substitution typo makes a var undefined only at runtime  | H      | M           | `--syntax-check` catches structure; Task 2.2 catches the semantics on the host   |
| The QA check over-matches and flags legitimate `ansible_*` | M      | M           | Key on a known fact-name list, never on the prefix alone                         |
| More references exist in files the grep did not cover      | M      | L           | The QA check runs over the same tree the other Ansible checks do, so it re-finds |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Found in the deploy output of Plan 00074, not by a gate — which is itself the
  argument for Task 2.1
