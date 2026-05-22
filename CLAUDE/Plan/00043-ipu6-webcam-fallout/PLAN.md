# Plan 00043: IPU6 Webcam Playbook Fallout

**Status**: Complete
**Created**: 2026-05-21
**Owner**: joseph (with Claude assistance)
**Priority**: High

## Overview

The IPU6 webcam playbook (`play-ipu6-webcam.yml`, commit `e5d0e33`) caused major boot instability on the X1 Carbon Gen 10 (i7-1260P, Alder Lake-P — confirmed via DMI in Plan 00044 audit):

- Kernel **7.0.9-104** (newest): no WiFi, no Bluetooth — all radios dead.
- Kernel **7.0.4-100**: WiFi works, GNOME login then **hard freeze**.
- Kernel **6.19.14-200** (currently running): working normally.

Root cause: pulling `akmod-intel-ipu6` triggered installation of `kernel-devel-matched` → `kernel-devel-7.0.9` → `kernel-core-7.0.9` + `kernel-modules-core-7.0.9` only — but **not** the full `kernel-7.0.9` metapackage. Kernel 7.0.9 has no `kernel-modules` / `kernel-modules-extra` installed, so `iwlwifi.ko`, `btusb.ko`, `btintel.ko` and the rest of the wireless stack literally do not exist on disk for that kernel.

Secondary: on Fedora 43+ kernel ≥ 6.19, mainline already ships `intel-ipu6` + `intel-ipu6-isys` + `intel-ipu6-psys` in-tree. The RPM Fusion akmod is redundant (only builds a partial `psys` overlay with 210 depmod symbol-mismatch warnings) and was the sole reason kernel-devel-matched got pulled in.

The 7.0.4 freeze appears unrelated — no fingerprint in logs (no oops, no lockup, no GPU hang, no panic) and the same kernel + stack worked perfectly during the original playbook run earlier the same day. Likely a one-off hardware/firmware-level hang.

## Goals

- Restore radios on kernel 7.0.9 (or remove the partially-installed 7.0.9 entirely).
- Update `play-ipu6-webcam.yml` so it cannot reproduce this failure mode.
- Document the incident with enough evidence that a future maintainer can recognise the trap.

## Non-Goals

- Shipping a real `v4l2-relayd` `setup.cfg` so the icamerasrc bridge creates a `/dev/video` capture node. Pipewire-libcamera already covers browsers / Wayland-native apps. The cosmetic crash-loop is silenced by masking `v4l2-relayd@icamerasrc.service`.
- Investigating the 7.0.4 freeze further unless it recurs — current evidence doesn't justify rollback.

## Context & Background

- Original playbook commit: `e5d0e33` (2026-05-21).
- Detailed evidence and three sub-agent investigations are captured in `01-findings.md` and the `02-/03-/04-agent-*.md` reports in this directory.
- Direct verification on the running system:
  ```
  $ rpm -qa | grep '^kernel-modules-7.0.9'
  kernel-modules-core-7.0.9-104.fc43.x86_64
  # kernel-modules-7.0.9-104 is ABSENT
  $ ls /lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/net/wireless/intel/iwlwifi/
  dvm  mld  mvm  tests          # no iwlwifi.ko.xz
  $ ls /lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/bluetooth/
  No such file or directory
  ```

## Tasks

### Phase 1: Investigation ✅

- ✅ Capture system state, kernel inventory, boot history.
- ✅ Sub-agent 1: investigate kernel 7.0.9 radio loss (see `02-agent-1-no-radio.md`).
- ✅ Sub-agent 2: investigate kernel 7.0.4 freeze (see `03-agent-2-freeze.md`).
- ✅ Sub-agent 3: audit akmod build state + dnf history (see `04-agent-3-akmod-state.md`).
- ✅ Synthesise findings (see `01-findings.md`).

### Phase 2: Playbook Fix ✅

- ✅ Rewrite `playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml`:
  - ✅ Remove `akmod-intel-ipu6`, `kernel-devel`, `kernel-headers`, `mokutil` from packages.
  - ✅ Remove the entire Secure Boot / MOK enrollment block.
  - ✅ Remove the `akmods --force` task.
  - ✅ Add explicit task to mask `v4l2-relayd@icamerasrc.service` (silences SPLASHSRC crash-loop).
  - ✅ Update header comment with the WHY-NOT-AKMOD rationale and a pointer to this plan.
  - ✅ Simplify final pause prompt (drop MOK branch).
- ✅ Verify playbook syntax (`ansible-playbook --syntax-check`).
- ✅ QA pass (`./scripts/qa-all.bash`; bash + python green; semgrep unavailable locally — environmental).

### Phase 3: Host Recovery ✅

Recovery was performed via a new durable Ansible play `playbooks/imports/play-AB-dnf-upgrade.yml` (slotted into `playbook-main.yml` right after `play-AA-preflight-sanity.yml`), NOT via the original surgical commands. Rationale: a `dnf upgrade` playbook is something the project wanted anyway for fresh-install workflows, and it doubles as the recovery mechanism here. The new play:

1. Refreshes dnf metadata.
2. Runs `dnf upgrade *` (respects existing versionlocks — the `kernel = 6.19.14-200` lock on the previous-minor fallback stays intact).
3. Auto-detects half-installed kernels (kernel-core without matching kernel-modules) and cleans them up, including dependent akmod-built kmods (e.g. `kmod-intel-ipu6-<v>`, `kmod-v4l2loopback-<v>`) which embed the kernel version in their RPM name.
4. Prompts for reboot if the kernel package set changed.

- ✅ Wrote `playbooks/imports/play-AB-dnf-upgrade.yml` with kernel-pre/post reporting, idempotent half-install cleanup, and reboot prompt.
- ✅ Wired into `playbooks/playbook-main.yml` after preflight-sanity (runs early on fresh installs).
- ✅ Ran the play. Result: kernel-core 7.0.9-105 installed full set (incl. `iwlwifi.ko.xz` + `btusb.ko.xz`); 7.0.4-100 trimmed by `installonly_limit=3` (acceptable — 6.19.14 is the locked fallback); 7.0.9-104 half-install and its dependent kmods removed cleanly.
- ✅ Verified `grubby --default-kernel` = `/boot/vmlinuz-7.0.9-105.fc43.x86_64`. `/boot` clean: only 6.19.14, 7.0.9-105, rescue. No 7.0.9-104 trace in `rpm -qa`.
- ⬜ Reboot to load 7.0.9-105 and verify radios + camera *(user action)*.

### Phase 4: Verification ✅

- ✅ `kernel-modules-7.0.9-105` and `kernel-modules-extra-7.0.9-105` present on disk; `iwlwifi.ko.xz` and `btusb.ko.xz` exist for the new kernel.
- ✅ `rpm -qa | grep 7.0.9-104` returns empty.
- ✅ `dnf versionlock list` shows `kernel = 6.19.14-200` lock preserved (policy intact).
- ✅ Booted into 7.0.9-105 on 2026-05-22 11:40. `uname -r` = `7.0.9-105.fc43.x86_64`.
- ✅ WiFi: `nmcli device status` shows `wlp0s20f3` connected. iwlwifi loaded firmware `89.735b75a4.0`. No firmware loader failures.
- ✅ Bluetooth: `bluetoothctl list` shows controller `74:04:F1:43:C3:3E`. `btusb`, `btintel`, `btbcm`, `btmtk`, `btrtl` modules all loaded. rfkill clean.
- ✅ Camera: `cam -l` finds `Internal front camera (\_SB_.PC00.LNK1)`; `wpctl status` shows `* 191. Intel MIPI Camera (V4L2)` as default video source plus a libcamera-managed `Intel MIPI Camera` device.
- ⬜ Smoke-test fresh-install behaviour of `play-AB-dnf-upgrade.yml` next time it runs in `playbook-main.yml` end-to-end *(future work — not blocking)*.

## Dependencies

- None inbound.
- Blocks: any future use of `play-ipu6-webcam.yml` on fresh installs (must apply the fix first).

## Technical Decisions

### Decision 1: Repair 7.0.9 vs Remove It

**Context**: kernel 7.0.9 is half-installed and unbootable for normal use (no radios). We can repair it or remove it.

**Options**:

1. **Repair** (Path A): `dnf install` the missing kernel-modules + kernel-modules-extra. Keeps a newer kernel available; future security fixes get tested sooner.
2. **Remove** (Path B): `dnf remove kernel-core-7.0.9 …`. Pins us to 7.0.4 as newest. Cleaner if the user doesn't want bleeding-edge kernels.

**Decision**: **Repair** (Path A). The user is on a personal dev workstation that benefits from newer kernels. Removing a kernel because it's been mis-installed teaches the wrong lesson — fix the install, then prevent the cause (Phase 2 already done).

**Date**: 2026-05-21

### Decision 2: Keep `akmod-intel-ipu6` Installed (Don't Aggressively Remove)

**Context**: On the current host, `akmod-intel-ipu6` and `kmod-intel-ipu6-7.0.9` are installed. The akmod is redundant on Fedora 43+ (mainline has the same drivers) but isn't actively breaking anything — mainline wins the modprobe race, the /extra/ overlay just sits there.

**Decision**: **Leave it for now.** Removing involves more dnf transactions that could surprise us. The playbook change prevents recurrence on new installs. If the user wants a clean state later, Path B is documented in `05-recovery-plan.md`.

**Date**: 2026-05-21

## Success Criteria

- ✅ Playbook no longer installs `akmod-intel-ipu6` or `kernel-devel*`.
- ✅ Incident documented with evidence and reproduction path.
- 🚫 Kernel 7.0.9 boots with WiFi, Bluetooth, and IPU6 camera working.
- 🚫 Camera still works on the kernel the user actually uses day-to-day.
- 🚫 Re-running the fixed playbook is idempotent and doesn't churn kernel packages.

## Risks & Mitigations

| Risk                                                                 | Impact | Probability | Mitigation                                                                                                                         |
| -------------------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| Path A `dnf install` itself fails or further breaks the box          | High   | Low         | User has confirmed they will back up first. Worst-case: boot to 6.19.14 (proven working) and recover from there.                   |
| 7.0.4 freeze is actually a kernel regression, not a one-off          | Med    | Low         | If it recurs, capture pstore/MCE data per `03-agent-2-freeze.md` step 3; downgrade or pin in grub.                                 |
| Camera stops working after recovery (mainline + userspace only)      | Low    | Low         | Userspace stack (`ipu6-camera-bins`, `…-hal`, `icamerasrc`, libcamera) is the same; only the redundant akmod overlay would change. |
| Future dnf upgrade pulls `kernel-devel-matched` from another package | Low    | Low         | Audit on each upgrade. Could add a dnf protected.d entry if it becomes recurrent.                                                  |

## Notes & Updates

### 2026-05-21

- Created plan after migrating draft incident docs from `untracked/2026-05-21-ipu6-fallout/`.
- Phases 1 + 2 complete.
- Phase 3 + 4 blocked on user backup. User explicitly instructed: "no further system changes until I confirm I have backed up important stuff."
- Sub-agent reports preserved verbatim under `02-*` / `03-*` / `04-*` for future-maintainer context.

### 2026-05-22

- Found the *cause* of the half-install: a versionlock on `kernel = 6.19.14-200` (set by `play-advanced-kernel-management.yml` as the previous-minor fallback) silently filtered the `kernel` metapackage out of the akmod transaction's depsolve. The sub-packages (`kernel-core`, `kernel-modules-core`) — which aren't locked — got installed individually, but `kernel-modules` and `kernel-modules-extra` (pulled by the missing `kernel` metapackage) did not. Hence the half-install.
- Recovery executed via new durable `play-AB-dnf-upgrade.yml`. End state matches Decision 1: full `kernel-7.0.9-105` set installed, `6.19.14-200` locked fallback intact, broken 7.0.9-104 cleaned up entirely.
- `kmod-intel-ipu6-7.0.9-105` was NOT auto-rebuilt by akmods (only `kmod-v4l2loopback-7.0.9-105` was). Not blocking — mainline IPU6 driver in `kernel-modules-7.0.9-105` covers it. May want to follow up if akmod overlay is desired post-reboot, or to action Decision 2 (remove `akmod-intel-ipu6` entirely since mainline supersedes it).
- Plan 00043 substantively complete. Awaiting reboot to verify radios + camera on 7.0.9-105.
- **Post-reboot (11:40 BST)**: all green. Booted to 7.0.9-105 cleanly, WiFi connected to home AP, Bluetooth controller online, IPU6 camera enumerated by both V4L2 (Intel MIPI Camera) and libcamera (simple pipeline with uncalibrated.yaml fallback — same cosmetic warnings as the original successful Plan 00043 run, no functional impact).
- Plan 00043 **closed**.
