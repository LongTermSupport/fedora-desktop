# Plan 021: Firstboot Wizard Redesign

**Status**: 🔄 In Progress
**Created**: 2026-03-02
**Owner**: Agent
**Priority**: High

## Overview

The current firstboot approach uses a systemd service that runs the full Ansible
setup headlessly in the background. This is the wrong model:

- No interactivity — user cannot respond to prompts
- No easy retry — if a step fails, the service marks itself disabled
- Hard to debug — must tail a log file to see what's happening
- Runs as root, not as the user (pipx, ansible are user-local)
- The GNOME show-firstboot helper is a workaround for a fundamentally flawed design

The new approach: **%post clones the repo and sets up config, then a GNOME terminal
wizard handles everything interactively on first login.**

## Goals

- Remove the systemd firstboot service entirely
- Move git clone to %post (during install, while network is available)
- Create an interactive terminal wizard with per-step retry
- Wizard opens automatically in gnome-terminal on first GNOME login
- Wizard exits silently on subsequent logins (completion marker check)
- Every step visible to the user, with clear retry on failure
- Wizard is re-runnable at any time: `fedora-desktop-setup.bash`

## Non-Goals

- Redesigning the playbook-main.yml composition (tracked separately)
- Adding a TUI framework (bash is sufficient)
- Handling the case where GNOME is not available (console fallback)

## Context & Background

The original firstboot design was appropriate when the install was truly headless,
but the project now targets a desktop environment where the user is present for
first login. An interactive wizard is both simpler and more robust.

The wizard runs as the user (not root), which is correct for pipx/ansible setup.
Config files (vault-pass.secret, localhost.yml) are copied to user-readable
location (`~/.local/share/fedora-desktop-install/`) during %post so the wizard
can access them without sudo.

## Tasks

### Phase 1: ks.cfg Redesign

- [x] ✅ **Task 1.1**: Remove systemd service creation from %post
- [x] ✅ **Task 1.2**: Remove old firstboot.bash script embedding
- [x] ✅ **Task 1.3**: Remove show-firstboot.bash helper script
- [x] ✅ **Task 1.4**: Add git clone attempt in %post (non-fatal)
- [x] ✅ **Task 1.5**: Add config file copy to user-readable location
- [x] ✅ **Task 1.6**: Create fedora-desktop-setup.bash interactive wizard
- [x] ✅ **Task 1.7**: Create fedora-desktop-autostart.bash wrapper
- [x] ✅ **Task 1.8**: Update GNOME autostart .desktop entry

### Phase 2: Playbook Review

- [ ] ⬜ **Task 2.1**: Audit playbook-main.yml — remove interactive plays
  - play-github-cli-multi.yml has headless guard but is conceptually interactive
  - Consider moving it to wizard's "optional next steps" section
- [ ] ⬜ **Task 2.2**: Review "optional but essential" plays
  - play-python.yml (optional/common) — should it be in main?
  - Assess other optional plays that are commonly needed
- [ ] ⬜ **Task 2.3**: Add network wait to wizard (brief check before each network step)

### Phase 3: QA & Commit

- [ ] ⬜ **Task 3.1**: Run `./scripts/qa-all.bash`
- [ ] ⬜ **Task 3.2**: Commit with plan reference

## Technical Decisions

### Decision 1: Wizard as embedded bash script vs repo script
**Decision**: Embed wizard in ks.cfg (not sourced from repo) so it exists even
if the clone fails. The wizard handles clone failure with retry.
**Date**: 2026-03-02

### Decision 2: Completion marker location
**Decision**: `~/.local/state/fedora-desktop-setup-complete` — user-writable,
no sudo needed, standard XDG location.
**Date**: 2026-03-02

### Decision 3: Non-fatal pre-clone in %post
**Decision**: Clone attempt in %post is best-effort. If it fails (network blip),
the wizard clones on first login. This avoids failing the whole install for a
transient network issue.
**Date**: 2026-03-02

### Decision 4: Autostart wrapper vs inline Exec check
**Decision**: Separate `fedora-desktop-autostart.bash` wrapper handles completion
check before opening gnome-terminal. Avoids quoting complexity in .desktop Exec=
and keeps the .desktop file simple.
**Date**: 2026-03-02

## Success Criteria

- [ ] No systemd service in installed system
- [ ] On first GNOME login, gnome-terminal opens with wizard automatically
- [ ] On subsequent logins, nothing opens (completion marker present)
- [ ] Each wizard step has retry loop
- [ ] Wizard is re-runnable: `sudo /usr/local/bin/fedora-desktop-setup.bash`
- [ ] `./scripts/qa-all.bash` passes

## Notes & Updates

### 2026-03-02
- Plan created. User direction: "we're trying to do too much in firstboot"
- Key insight: boot into GNOME, run stuff in terminal, user-friendly with retry
- Previous approach: headless systemd service with tail-log GNOME popup workaround
- New approach: interactive wizard that IS the setup, not a monitor of hidden setup
