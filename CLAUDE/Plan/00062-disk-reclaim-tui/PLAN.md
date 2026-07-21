# Plan 00062: Disk Reclaim TUI

**Status**: In Progress
**Created**: 2026-07-21
**Owner**: joseph
**Priority**: Medium

## Overview

The repo previously provisioned no dedicated disk-usage or cleanup tooling into
Fedora — the only disk-reclaim command was the narrow `raw-prune` (orphaned RAW
photos). This plan adds a general-purpose disk-reclamation capability: an Ansible
play that installs disk analysers plus a custom pure-bash TUI, `reclaim`, that
reports what is using disk and runs targeted cleanup actions semi-automatically.

`reclaim` is a dependency-light, menu-driven tool following
`CLAUDE/InteractiveScripts.md` (strict validation, friendly recovery, bounded
retries, clean EOF exit, confirm-before-destructive with a safe default) and
`CLAUDE/StderrHygiene.md` (interactive UI → stderr; the `report` subcommand's
stdout is the payload). Every destructive action shows what it will do and
defaults to "No"; it can also launch richer analysers (`ncdu`, `baobab`).

## Goals

- Add `play-disk-reclaim.yml` installing `ncdu`, `duf`, `trash-cli`, and `baobab`
  (GUI, desktop-only).
- Ship `reclaim` — a pure-bash TUI covering: biggest directories, biggest files,
  dnf autoremove + clean, old-kernel removal, journal vacuum, container prune
  (podman/docker), flatpak unused runtimes, `~/.cache` clear, empty trash, and a
  safe sweep.
- Comply with the repo's interactive-script and stderr-hygiene standards and pass
  `./scripts/qa-all.bash`.

## Non-Goals

- No automatic/unattended destructive cleanup — every action confirms (default No).
- No systemd timer / scheduled cleanup (on-demand tool only).
- Not wired into `playbook-main.yml` — standalone opt-in like
  `play-container-watch.yml` (wiring it into the main run is a user decision, see
  Delivery notes).

## Tasks

### Phase 1: Custom tool

- [x] ✅ **Task 1.1**: Write `files/home/.local/bin/reclaim` (pure-bash TUI:
  report header, 10 actions, capability detection, bounded/EOF-safe input,
  confirm-before-destructive, stderr-hygiene, `report`/`--help`/`--version`).
- [x] ✅ **Task 1.2**: Non-interactive escape hatch — `reclaim report` prints a
  stdout payload; unknown args exit 2.

### Phase 2: Playbook

- [x] ✅ **Task 2.1**: Write `playbooks/imports/optional/common/play-disk-reclaim.yml`
  (`scope: general`, installs CLI tools; `baobab` gated `when:     provisioning_profile != 'server'`; deploys `reclaim`; shebang + exec bit).

### Phase 3: QA + deploy

- [x] ✅ **Task 3.1**: Run QA: `./scripts/qa-all.bash` (403 files, exit 0; 0
  findings in `reclaim`).
- [x] ✅ **Task 3.2**: Container smoke test (`--version`, bad-arg exit 2,
  `report` stdout payload, `bash -n`).
- [ ] ⬜ **Task 3.3**: (HOST, not CCY) deploy + live-test — run the play, then
  `reclaim report` and walk the menu actions on the real desktop.

### Phase 4: Incident — reclaim wedged the rootless podman store

Running `reclaim` on a host whose rootless podman store already held stale temp
dirs (fallout of the full disk) surfaced and aggravated store corruption:
v1.0.0's `act_containers` ran `podman system prune -af` even though `podman system df` had failed to load the store. Every podman command — including CCY
startup across multiple live sessions — then failed with `error removing stale temp dir … permission denied`. The temp-dir files are owned by mapped sub-UIDs,
so neither the user nor `podman unshare` (namespaced root, still bound to the
sub-UID map) can unlink them — only real host root can.

- [x] ✅ **Task 4.1**: Harden `reclaim` (v1.0.1) — `system df` is the reachability
  probe; a failed probe surfaces the reason and SKIPS the prune. Applied to docker too.
- [x] ✅ **Task 4.2**: `reclaim` v1.0.2 — detect the `stale temp dir` signature and
  point the user at the repair playbook instead of poking the store.
- [x] ✅ **Task 4.3**: New `playbooks/imports/play-podman-store-repair.yml` — removes
  `overlay/tempdirs` as **real root** (bypasses the sub-UID ownership that blocks the
  user and `podman unshare`), probes store health before/after, asserts it loads
  cleanly. Safe with live containers (only orphaned scratch is removed; no
  prune/reset). Optional `-e podman_repair_remove_image=` drops a flagged-corrupted
  image (e.g. CCY).
- [ ] ⬜ **Task 4.4**: (HOST) run the repair play; confirm store loads + CCY starts.

## Success Criteria

- [x] `./scripts/qa-all.bash` passes.
- [x] `reclaim --help` / `report` / bad-arg behave correctly.
- [ ] Play deploys the tool + packages on the HOST and the menu actions work.

## Delivery & Milestones

- Implementation: `reclaim` tool + `play-disk-reclaim.yml` + plan (this commit).
- HOST deploy/live-test pending (Task 3.3) — CCY container is edit-and-commit only.
