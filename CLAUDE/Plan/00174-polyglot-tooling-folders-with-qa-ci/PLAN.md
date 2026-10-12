# Plan 00174: Polyglot tooling folders, each with its first-party QA pipeline

**Status**: Not Started
**Created**: 2026-10-12
**Owner**: joseph
**Priority**: Medium

## Overview

The owner's direction: tooling in this repo is not bound to Python. Where efficiency
matters (a long-running service, a root daemon), write it in Rust, because the owner
does not read the code and the language should serve the job. To keep several
languages manageable, the repo takes a monorepo shape: one top-level folder per
language, `tooling-rust/`, `tooling-python/`, `tooling-go/` and so on, each with a
full QA process of its own.

QA comes from the owner's first-party pipelines on GitHub, LongTermSupport:
[`rust-qa-ci`](https://github.com/LongTermSupport/rust-qa-ci) ("One QA entry point for
Rust workspaces") and [`py-qa-ci`](https://github.com/LongTermSupport/py-qa-ci) ("One QA
entry point for Python projects"). Both are new and may have bugs. A defect found in
them is fixed upstream (an issue or a pull request against them), not worked around
here.

The first consumer is Plan 00155's headset key mapper, a root system service that
suits Rust.

## Goals

- `tooling-rust/` exists as a Cargo workspace, with `rust-qa-ci` as its single QA entry
  point, wired into `./scripts/qa-all.bash` so a Rust failure fails the repo gate.
- How a Rust binary reaches the host is settled in IaC: built by Ansible on the host
  with the toolchain `play-rust-dev.yml` installs (that play becomes a dependency of
  any play deploying Rust tooling), and installed root-owned.
- The convention (where each language's tooling lives, how its QA runs, how it is
  deployed) is documented in `CLAUDE/` and the docs index, so the next language
  follows it.

## Non-Goals

- `tooling-go/` or any other language before something needs it (YAGNI).

## Tasks

### Phase 1: Research

- [ ] ⬜ **Task 1.1**: Read `rust-qa-ci` (install, the entry point, what it requires of a
  workspace, how it pins its tools) and `py-qa-ci`. Record in `RESEARCH-qa-ci.md`:
  the exact adoption steps, the tools each needs (each a playbook dependency, never a
  manual install), and anything that looks like a bug, with an upstream issue.
- [ ] ⬜ **Task 1.2**: How the repo's QA gates discover files today
  (`scripts/qa-*.bash`, the hooks daemon's lint-on-edit), and what must change so
  `.rs` files are linted on edit and in `qa-all.bash`.

### Phase 2: `tooling-rust/`

- [ ] ⬜ **Task 2.1**: The workspace skeleton and `rust-qa-ci` adoption, test first: a
  first crate is created only with its test (Plan 00155's mapper is the first crate,
  built under that plan).
- [ ] ⬜ **Task 2.2**: `qa-all.bash` runs `rust-qa-ci`. A missing toolchain fails loudly
  and names `play-rust-dev.yml` (the repo's missing-dependency rule).
- [ ] ⬜ **Task 2.3**: The deploy pattern: an Ansible task that builds a named crate in
  release mode on the host and installs the binary root-owned under `/usr/local/`,
  idempotent (rebuilds only when the source changes).

### Phase 3: Convention

- [ ] ⬜ **Task 3.1**: A `CLAUDE/` topic file for the polyglot layout and its QA rules, a
  path-triggered rule under `.claude/rules/` for `tooling-*/**`, and the docs index
  entry.
- [ ] ⬜ **Task 3.2**: `./scripts/qa-all.bash` and the `qa-reviewer` agent over the plan's
  diff.

### Phase 4: Migrate historic tooling (owner: yes; not urgent, but to be done)

The owner's order: new tooling goes under `tooling-*/` from now on. Historic tooling
moves once the concept is proven by the first new crate or package.

- [ ] ⬜ **Task 4.1**: Inventory what moves: `helpers/` (stdlib Python, its own runner,
  invoked by plays through `command:` + `argv:`), the Python under `files/` that is
  tooling rather than a user-facing script, and the tests under `tests/helpers/`.
  Record each with its callers (plays, scripts, the hooks daemon's paths) in
  `RESEARCH-migration.md`.
- [ ] ⬜ **Task 4.2**: Move `helpers/` into `tooling-python/` on `py-qa-ci`, updating every
  caller in the same commit. `qa-all.bash` stays green before and after.
- [ ] ⬜ **Task 4.3**: Update `helpers/CLAUDE.md`, `.claude/rules/python-helpers.md`,
  `playbooks/CLAUDE.md` and the AnsibleStyle references that name `helpers/`.

## Dependencies

- Blocks: Plan 00155 (the headset key mapper is to be written in Rust).
- Uses: `play-rust-dev.yml` (the Rust toolchain, already in IaC).

## Success Criteria

- [ ] A Rust crate in `tooling-rust/` is checked by `rust-qa-ci` from `qa-all.bash`, and a
  deliberate defect in it fails the gate.
- [ ] A play builds and installs it on the host with no manual step.
- [ ] The convention is documented and path-triggered.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00174-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed
