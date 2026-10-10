# Plan 00170: unifi network tooling

**Status**: In Progress
**Created**: 2026-10-10
**Owner**: joseph
**Priority**: Medium

## Overview

The UniFi Network controller this repo deploys (`play-unifi-controller.yml`) is run on
demand, so its state is easy to lose track of between uses. A Wi-Fi fault showed the cost:
ssh timed out on 6 GHz because the access points' radios had stopped carrying DSCP-marked
packets, and finding that out took the controller's login, its API and a hand-run ping
matrix, none of which existed as tooling.

This plan builds that tooling in the plan folder first (`unifi-api.bash`,
`wifi-dscp-probe.bash`), uses it until its shape settles, then moves it to durable tracked
tooling deployed by the UniFi play.

## Goals

- The controller's admin login is owned by IaC: name `unifi-admin`, password vaulted as
  `unifi_admin_password`, applied to the controller's database by the play.
- A scripted, read-only API client that logs in with the vaulted credential and never
  prints a secret.
- A probe that shows, per DSCP marking, whether the Wi-Fi link carries the packet.
- Both promoted to `files/` and deployed by `play-unifi-controller.yml`.

## Non-Goals

- Changing UniFi network settings from scripts. The tooling reads; changes go through the
  controller UI until a concrete need appears.
- Anything specific to one install (device names, addresses, SSIDs) in tracked files.

## Tasks

### Phase 1: Plan-folder tooling

- [x] ✅ **Task 1.1**: Vaulted admin login, generated on first run and applied by
  `play-unifi-controller.yml`; documented in `docs/UnifiSetupGuide.md`

- [x] ✅ **Task 1.2**: `unifi-api.bash`: GET any `/api/s/<site>/<path>` with the vaulted
  login, `x_*` fields removed

- [x] ✅ **Task 1.3**: `wifi-dscp-probe.bash`: loss per DSCP marking to each target

- [x] ✅ **Task 1.4**: The play stops resetting the owner of the container-owned data
  directories, which aborted MongoDB under a running stack

- [ ] ⬜ **Task 1.5**: Events and firmware history through the `/v2` API (`stat/event` and
  `stat/alarm` return 404 on Network 10.x)

- [x] ✅ **Task 1.6**: The admin login runs last in the play, so a new install (no admin
  until the setup wizard) still gets the firewall rules, CLI and launcher; the orphan
  reclaim scans only what it fixes, refuses under a running MongoDB, re-checks afterwards,
  and parses `/etc/subuid` without shell

- [ ] ⬜ **Task 1.7**: The controller stays on demand: every tool that needs it starts it,
  waits until the API answers, and stops it afterwards unless it was already running.
  Measured on the host: a cold start answers an API login after about 15 s, and a stop
  takes about 5 s, so a scheduled check can bring it up and down each time

- [x] ✅ **Task 1.8**: `helpers/unifi/` (stdlib, tested): compare desired access-point
  radio state and a list of devices to adopt with the controller, `--check` reports,
  `--apply` adopts and sends each changed radio table. A private play drives it with the
  controller started on demand; check mode run against the live controller with no Wi-Fi
  drop

### Phase 2: Durable tooling

- [ ] ⬜ **Task 2.1**: Move both scripts to `files/home/.local/bin/`, deployed by
  `play-unifi-controller.yml`, with tests
- [ ] ⬜ **Task 2.2**: Point `docs/UnifiSetupGuide.md` troubleshooting at them

## Success Criteria

- [x] `unifi-api.bash stat/device` lists the access points without a password ever being
  shown or typed
- [x] A second run of the play changes nothing about the admin login
- [ ] The tools are installed on `PATH` by the play and covered by `qa-all.bash`

## Delivery & Milestones

- Phase 1 tooling and the vaulted login: this plan's first commit
