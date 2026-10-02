# Plan 00142: QA tool pin does not converge under pipx's uv backend

**Status**: Complete (2026-10-02; closed by the owner without the real-host converge run,
Task 1.5)
**Created**: 2026-09-29
**Owner**: joseph
**Priority**: High

## Overview

`playbooks/imports/play-python.yml` installs ruff and semgrep at the versions `/.qa-versions`
pins. It probes each tool's version and, for a tool at another version, ran
`community.general.pipx` with `state: install` and `force: true`. On Fedora 44 (pipx 1.15.0,
with uv installed by the same play) pipx uses its uv backend, and `pipx install --force` onto
an existing venv re-runs `uv venv` on that directory. uv refuses: *"A virtual environment
already exists"*. pipx then declines to remove a venv it did not create in that session, so
the task fails and the play stops. It was seen on a live headless provisioning run at F44
`fadc1eca`, where semgrep was installed at a newer version than the pin.

Any machine whose installed QA tool differs from its pin hits this: a pin change, or
`pipx upgrade-all` having moved the tool before the pin task existed. The play's purpose is
to converge the version, and in exactly that case it cannot.

The fix removes an off-pin tool (`state: absent`) and then installs it without `force`. The
pin task already runs after the install; a venv made afresh is unpinned, so it re-pins.

Search terms, if this comes back: `A virtual environment already exists`, `uv venv`,
`pipx install --force`, `pipx pin`, `pipx upgrade-all`, pipx uv backend, `UV_VENV_CLEAR`,
`community.general.pipx`, `state: absent`, ruff, semgrep, `.qa-versions`, off-pin, QA tool
version drift, `qa-toolchain.bash`, `play-python.yml`, "Remove QA tools that are not at
their pinned versions". Start with `reproduce-pipx-uv.bash` in this folder.

## Goals

- An off-pin QA tool, pinned or not, converges to `/.qa-versions` under pipx's uv backend.
- A tool already at its pin is left alone: a second run changes nothing.
- A permanent `qa-all.bash` gate holds the play to the remove-then-install-then-pin shape.

## Non-Goals

- Making `pipx install --force` work under uv (for example with `UV_VENV_CLEAR`). Removal
  does not depend on which backend recorded the venv.
- The CCY image and CI installs of the same pins. Each installs into a fresh environment,
  where no venv exists to replace.

## Tasks

### Phase 1: Reproduce, fix, gate

- [x] ✅ **Task 1.1**: Establish the cause from pipx and community.general source, and
  reproduce it
  - [x] ✅ `reproduce-pipx-uv.bash` runs the play's own QA-tool tasks, lifted unchanged,
    through ansible and community.general.pipx against a real pipx 1.15.0 and uv, with fake
    ruff/semgrep wheels served offline
  - [x] ✅ Against `fadc1eca`: both off-pin cases fail with uv's "A virtual environment
    already exists"
- [x] ✅ **Task 1.2**: Remove off-pin tools before installing; drop `force: true`
- [x] ✅ **Task 1.3**: `scripts/test-qa-tool-pin-converges.bash`, hard gate
  `qa-tool-pin-converges` in `qa-all.bash`; row in `CLAUDE/QA.md`
- [x] ✅ **Task 1.4**: `./scripts/qa-all.bash`, the qa-reviewer, and a PR to `F44`
- [ ] ❌ **Task 1.5**: On a host with an off-pin QA tool, `./playbooks/imports/play-python.yml`
  converges it, and a second run reports no change for the QA-tool tasks. The owner's
  meta-deploy run passed with ruff and semgrep already on their pins: the remove and
  install tasks skipped and the verify task passed, so the no-change half holds. The
  converge half needs a host whose tools have drifted, and none has. Cancelled: the owner
  closed the plan on the container reproduction (`reproduce-pipx-uv.bash`) and the gate.

## Success Criteria

- [x] `reproduce-pipx-uv.bash`: the off-pin cases fail before the change and pass after it;
  every case's second run reports `changed=0`
- [x] `test-qa-tool-pin-converges.bash` passes on the change and fails against `fadc1eca`
- [x] QA passes (`./scripts/qa-all.bash`)
- [ ] A real host run converges an off-pin tool (Task 1.5; not run, the owner closed the plan)

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00142-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Fix, reproduction and gate: branch `fix-qa-tool-pin-converges-under-uv`
