# Plan 00085: headless path ~/.local/bin PATH gap

**Status**: In Progress
**Created**: 2026-08-24
**Owner**: joseph
**Priority**: High

## Overview

A downstream deployment repo's live proof of the headless `RUN_BASH_GITHUB_ACCOUNTS=none`
path (run via Ansible `become`/`become_user`, not an interactive login shell) found that
`run.bash` fails partway through provisioning with `ansible-galaxy: command not found`,
even though the preceding pipx install step reported `ansible-galaxy` as one of the apps
it made available.

Root cause: `pipx install --include-deps ansible` creates its app shims under
`~/.local/bin`, but `run.bash` never adds that directory to `PATH` within its own process.
An interactive terminal session already has `~/.local/bin` on `PATH` (Fedora's default
`.bash_profile`/`.bashrc` add it at login), so the desktop-interactive path has always
worked and this was never seen there. A non-interactive invocation via `sudo -u <user>`
(what both this repo's headless mode and any Ansible `become`-based caller use) resets
`PATH` to sudoers' `secure_path`, which never includes `~/.local/bin` — so the later bare
`ansible-galaxy install -r requirements.yml` call, and the `./playbooks/playbook-main.yml`
invocation right after it (shebang `#!/usr/bin/env ansible-playbook`), cannot resolve.

This is a pre-existing gap, unrelated to the RUN_BASH_GITHUB_ACCOUNTS=none (PR #33) or
RUN_BASH_SUDO_PASSWORD_FILE (PR #34) mechanisms — the run log confirms both of those
worked correctly (`sudo=password (RUN_BASH_SUDO_PASSWORD_FILE)` authenticated; the HTTPS
self-clone succeeded; GitHub setup was correctly skipped) before hitting this third,
independent blocker at the first bare `ansible-galaxy` call.

## Goals

- Make `run.bash` headless provisioning succeed regardless of the caller's PATH/shell
  context (login shell, non-login shell, `sudo -u`, SSH `command`), by having the script
  put `~/.local/bin` on its own `PATH` once pipx-installed tools are available.

## Non-Goals

- Not changing the interactive path's behaviour (already works).
- Not working around this in the downstream repo's Ansible invocation (e.g. passing
  `environment: PATH=...`) — the fix belongs in `run.bash` itself so every caller
  benefits, not just this one call site.

## Tasks

### Phase 1: fix

- [x] ✅ **Task 1.1**: add `export PATH="$HOME/.local/bin:$PATH"` to `run.bash`
  immediately after the pipx-based ansible install block, before any later bare
  `ansible-galaxy`/`ansible-playbook`/`ansible` invocation.
- [x] ✅ **Task 1.2**: `bash -n` + `shellcheck -x` clean; bump `RUN_BASH_VERSION`.
- [x] ✅ **Task 1.3**: commit, push on a branch, open PR against `F44` (PR #35).
- [x] ✅ **Task 1.4**: independent review (qa-reviewer stand-in) — verdict MERGE-READY
  WITH NITS; nits addressed (README index row added, Task 1.3 ticked).
- [ ] ⬜ **Task 1.5**: merge.

## Success Criteria

- [ ] A downstream live proof re-run (via `sudo -u`/Ansible `become`, the exact context
  that found this) gets past the `ansible-galaxy install -r requirements.yml` step and
  the subsequent `playbook-main.yml` invocation without a PATH-related failure.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00085-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Recovery cron: 28837729 (shared session-wide failsafe, already running).
