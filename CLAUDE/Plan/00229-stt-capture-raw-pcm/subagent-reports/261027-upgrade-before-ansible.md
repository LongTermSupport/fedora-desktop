# Task 2.2: every package upgraded before any Ansible process starts

Branch `fix/00229-upgrade-before-ansible`, cut from F44 at 76b5b9094. Not pushed, not merged.

## The defect

The main playbook runs as one `ansible-playbook` process. Its second play,
`play-AB-dnf-upgrade.yml`, upgraded python3 (3.14.7 to 3.14.8) under that process, which
replaced the interpreter's `lib-dynload` extension modules. The first later lazy import of a C
extension failed: `lookup('url')` in `play-toolbox-install.yml` gave "name 'ssl' is not
defined". Every fresh install whose python3 was behind the repos died there.

## The shape chosen, and why

run.bash's dependency step and `consumer-install` both run `dnf -y upgrade --refresh` before
any Ansible process starts. The headless path shares the run.bash step. A failed upgrade stops
the run before anything else is installed (`set -e` in run.bash, `failf` in Go).

The preferred shape, a separate `ansible-playbook` run of the upgrade play before the main
playbook, was not chosen:

- That process would itself outlive the upgrade, for the play's remaining tasks.
- run.bash's bash-limits ratchet would have to pay for a second three-branch become block.

A plain dnf run leaves no Ansible process alive across the upgrade. It also covers
python3-libs, openssl-libs and every other library Ansible loads, without listing them.

`play-AB-dnf-upgrade.yml` stays in `playbook-main.yml`, at the same position:

- Its upgrade is a cheap no-op there.
- `--changed`, `--rerun` and the play ledger keep it as a play.
- A single run of the play still upgrades.
- Its half-installed kernel cleanup stays.
- Its reboot notice now also fires when the running kernel is not the newest installed. The
  kernel now changes before the play takes its snapshot, so the old check would miss it.

## Changes

- `run.bash` 1.35.0: the upgrade comes before the install, and the package list is on one
  line. The redundant `unset _main_sudo_probe` was removed so the statement count stays at
  2811\. Code lines went from 2846 to 2838 (`scripts/bash-limits.allowlist`).
- `src-go/internal/consumer/app.go`: `install()` upgrades first.
- `playbooks/imports/play-AB-dnf-upgrade.yml`: added `newest_kernel_version`, and the reboot
  notice condition uses it.
- New suite `scripts/test-run-bash-upgrade-first.bash` (gate `run-bash-upgrade-first` in
  `scripts/qa-gates.tsv`, `src-go/qa/all/addedgates_test.go`, and a row in `CLAUDE/QA.md`).
- Docs: `docs/run-bash-changelog.md`, `docs/playbooks.md`, `docs/architecture.md` and
  `docs/installation.md`.
- `CLAUDE/Plan/meta-deploy.list`: the existing 00228 entry's VM runs are named as this task's
  host check.

## Tests (red, then green)

- `scripts/test-run-bash-upgrade-first.bash`: 8 of 13 failed on the old run.bash; all 13 pass
  now. It checks that the upgrade runs before the install and that a failed upgrade stops the
  run. On the real file, it checks that the upgrade comes before pipx, `ansible-galaxy` and the
  main playbook.
- Consumer tests: `TestTheUpgradeFinishesBeforeAnyAnsibleProcessStarts` (order across Run and
  Foreground) and `TestAFailedUpgradeStopsTheRunBeforeAnsible` both failed before the change
  and pass now.

## Host check (pending)

The VM lab's desktop-fresh-install and desktop-consumer-install on the 10-06 base. These are
the 00228 queue entry, run from the pushed HEAD after this branch is merged.
