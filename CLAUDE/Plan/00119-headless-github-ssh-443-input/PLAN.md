# Plan 00119: `RUN_BASH_GITHUB_SSH_443` — a headless box declares the always-on GitHub 443 route

**Status**: In Progress
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

Headless provisioning with a real `RUN_BASH_GITHUB_ACCOUNTS` writes a fresh
`environment/localhost/host_vars/localhost.yml` holding the identity and `github_accounts`, and
`play-github-cli-multi.yml` then logs `gh` in, generates the account key and uploads it — all
over HTTPS. What the fresh file could not carry was `github_ssh_over_443: true`
(`docs/github-ssh-over-443.md` §2), so on a box whose egress allows 443 and blocks 22 the run
succeeds and every later SSH use of the key hangs on a firewalled port. The interactive answer
("set it in localhost.yml and re-run the play") needs a person at the box, which is exactly
what a headless run does not have.

`RUN_BASH_GITHUB_SSH_443=1` is that answer as an input: validated in preflight beside the other
`RUN_BASH_*` variables, written as `github_ssh_over_443: true` into the fresh file, and picked up
by the gh play in the same unattended run. It is refused together with
`RUN_BASH_GITHUB_ACCOUNTS=none`, where there is no key to route.

## Goals

- `RUN_BASH_GITHUB_SSH_443=0|1` (default `0`), strictly validated; `1` with `none` fails preflight.
- The fresh `localhost.yml` carries `github_ssh_over_443: true` when `1`; an already-configured
  file is kept untouched as before.
- A unit test drives the writer through on/off/unset/none/kept; wired into `qa-all.bash`.
- `RUN_BASH_VERSION` 1.20.0; usage text, `docs/headless-provisioning.md` table and
  `docs/run-bash-changelog.md` say so.

## Non-Goals

- Changing how `play-github-cli-multi.yml` applies the 443 route. It already does.
- Re-writing an already-configured `localhost.yml` to add the key. That file is the box's own.

## Tasks

### Phase 1: the input

- [x] ✅ **Task 1.1**: preflight parse + validation, the `none` contradiction, the writer branch.
- [x] ✅ **Task 1.2**: `scripts/test-run-bash-headless-localhost-yml.bash`, wired into `qa-all.bash`.
- [x] ✅ **Task 1.3**: version bump, usage text, docs table, changelog entry.

### Phase 2: proof on a real box — BLOCKED BY a consumer pinning this version

- [ ] ⬜ **Task 2.1**: a headless run with a GitHub account and the flag on a port-22-blocked box
  ends with `ssh -T git@github.com-<alias>` greeting the account over 443.

## Success Criteria

- [x] `qa-all.bash` green including the new leg.
- [ ] Task 2.1 observed on a real box.

## Delivery & Milestones

- Phase 1: see the delivery commit on F44.
