# Plan 00114: Running one play by hand fails on password sudo — run.bash gets a single-play mode

**Status**: In Progress
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

The documented way to run one play is to execute it directly
(`./playbooks/imports/play-<name>.yml` or `ansible-playbook playbooks/imports/play-<name>.yml`).
On a box whose user holds ordinary password sudo, which is every server-profile box and any
desktop that was not given NOPASSWD, the first `become: true` task dies with
`Premature end of stream waiting for become success … sudo: a password is required`. Ansible
runs become in its own pty, Fedora's `tty_tickets` means a ticket from the caller's shell is
never visible to it, and nothing told Ansible to prompt. Running the play under `sudo` is the
natural next guess and fails differently: root has no pipx `ansible-playbook`.

`run.bash` already solves this for every play it drives. `run_playbook_with_issue_option`
branches on the box: a headless run that resolved a sudo password hands Ansible the 0600 file
via `--become-password-file`, a NOPASSWD box runs the play bare after `sudo -k -n true`, and a
password-sudo box runs it with `--ask-become-pass`. The gap is that this logic is unreachable
from outside the installer flow.

This plan exposes it: `./run.bash --play <playbook> [ansible-playbook args…]` runs exactly one
play through that runner, from the repo checkout, and exits with the play's status. Every doc
that told the reader to invoke a play bare now names this instead, so there is one documented
way to run a play and it works on every sudo configuration.

## Goals

- `./run.bash --play playbooks/imports/play-<name>.yml` runs the play through the existing
  runner; extra arguments after the path pass through to `ansible-playbook`.
- Fail fast, with a message naming the fix, when: the path is missing or not a playbook under
  `playbooks/`; the script is streamed rather than run from a checkout; ansible is not
  installed; `--play` is combined with `--optional-only` or a headless run.
- The help text, `README.md`, `docs/playbooks.md` and every doc that shows a bare play
  invocation name `--play` instead.
- Proven: `--play` on a password-sudo box prompts once and converges; the failure modes above
  each abort with their message and a non-zero status.

## Non-Goals

- Supporting `--play` inside a headless run. Headless already has
  `RUN_BASH_OPTIONAL_PLAYBOOKS` with the same become contract.
- Changing the become logic itself. The runner's three branches are proven and untouched.
- Setting `become_ask_pass` in `ansible.cfg`. It fires on NOPASSWD boxes too, and the CLI
  consumes it before `--become-password-file`, so a headless run would hang on a prompt.

## Tasks

### Phase 1: The mode

- [x] ✅ **Task 1.1**: Argument parsing accepts `--play <path>` and captures everything after
  the path as pass-through arguments; `--play` with no path, with `--optional-only`, or
  under headless aborts.
- [x] ✅ **Task 1.2**: After the runner function is defined and before the install steps,
  dispatch: resolve the repo root from the script's own location, validate the play,
  assert `ansible-playbook` is on PATH, run it through
  `run_playbook_with_issue_option`, exit with its status.
- [x] ✅ **Task 1.3**: `--help` documents the mode.

### Phase 2: One documented way — BLOCKED BY Phase 1

- [x] ✅ **Task 2.1**: `README.md` and `docs/playbooks.md` state the mode and why a bare
  invocation fails on password sudo.
- [x] ✅ **Task 2.2**: Every doc line invoking a play bare is rewritten to `--play`, keeping
  any trailing ansible flags.

### Phase 3: Proof — BLOCKED BY Phase 1

- [x] ✅ **Task 3.1**: `scripts/qa-all.bash` green.
- [x] ✅ **Task 3.2**: Failure modes exercised: missing path, path outside `playbooks/`,
  `--optional-only` combination, `--headless` combination.
- [ ] 🔄 **Task 3.3**: Live: on a password-sudo box, `./run.bash --play` on a real play prompts
  for the become password once and the play completes. Prompt half proven from the
  controller; the typed-password completion is the operator's, at a terminal.

## Success Criteria

- [ ] One play runs to completion on a password-sudo box via `--play` with no flags the user
  had to know.
- [x] No tracked doc invokes a play bare.
- [x] Every abort path names its fix and exits non-zero.

## Delivery & Milestones

- `b0f9c76` the mode; `ba6baaf` the docs sweep.
