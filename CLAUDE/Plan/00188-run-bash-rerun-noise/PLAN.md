# Plan 00188: run.bash re-run prints no spurious pipx warnings

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #54.

## Overview

On a re-run, `run.bash`'s Ansible step prints three ⚠️ pipx warnings, one each for
`jmespath`, `passlib` and `ansible-lint`, saying the package "already seems to be injected"
(#54). Nothing is wrong: `run.bash` runs `pipx inject ansible <pkg>` for each one
unconditionally, so a partial earlier install gets repaired, and pipx warns when the package
is already there. But warning lines on a normal run teach the reader to skim past warnings.

#54 sets the constraints. Inject only what is missing, read from the venv's injected
packages (for example `pipx list --json`, `venvs.ansible.metadata.injected_packages`). Print
one line in `run.bash`'s own style. Keep the repair, so a missing dependency is still
injected and a failed inject still stops the run. Never hide pipx's output with `2>/dev/null`
or `--quiet`.

## Goals

- A re-run with every dependency present prints one `✓` line and no pipx warning.
- A missing dependency is injected; a failed inject stops the run with pipx's output visible.

## Non-Goals

- Changing which packages are injected, or how Ansible itself is installed.
- Silencing pipx's output.

## Tasks

### Phase 1: Build (tests first)

- [ ] ⬜ **Task 1.1**: A test drives the inject step with a stub `pipx`: all present (no
  inject, one line), one missing (only it injected), an inject failure (run stops), and
  unreadable `pipx list --json` output (run stops, never assumes present).
- [ ] ⬜ **Task 1.2**: `run.bash` reads the injected set and injects only the missing ones.
  `RUN_BASH_VERSION` patch bump and `docs/run-bash-changelog.md` entry.
- [ ] ⬜ **Task 1.3**: `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] A re-run on a complete install shows no ⚠️ line from pipx.
- [ ] Deleting one injected package and re-running injects exactly that one.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00188-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #54.
