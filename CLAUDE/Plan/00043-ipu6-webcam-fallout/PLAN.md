# Plan 00043: IPU6 Webcam Playbook Fallout

**Status**: In Progress
**Created**: 2026-05-21
**Owner**: joseph (with Claude assistance)
**Priority**: High

## Overview

The IPU6 webcam playbook (`play-ipu6-webcam.yml`, commit `e5d0e33`) caused major boot instability on the X1 Carbon Gen 11:

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

### Phase 3: Host Recovery 🚫

**BLOCKED** on user backing up important data before any system changes.

Recommended approach — Path A from `05-recovery-plan.md` (least destructive):

- 🚫 Run `sudo dnf install kernel-7.0.9-104.fc43 kernel-modules-7.0.9-104.fc43 kernel-modules-extra-7.0.9-104.fc43` to repair the half-installed 7.0.9.
- 🚫 Regenerate initramfs: `sudo dracut -f --kver 7.0.9-104.fc43.x86_64`.
- 🚫 Regenerate grub: `sudo grub2-mkconfig -o /boot/efi/EFI/fedora/grub.cfg` (or `/boot/grub2/grub.cfg`).
- 🚫 Reboot to 7.0.9, verify `nmcli device status` shows wifi, `bluetoothctl list` shows controller, `wpctl status` shows "Built-in Front Camera".

**Alternative — full `dnf upgrade`**: also acceptable. Will refresh all packages; if a newer kernel (≥ 7.0.10) is in repos, it'll be pulled with the full module set, sidelining the broken 7.0.9. Won't *repair* 7.0.9 in place though — the half-install stays as cruft unless explicitly removed.

**Alternative — Path B from `05-recovery-plan.md`**: rip out `akmod-intel-ipu6` and `kmod-intel-ipu6-7.0.9` entirely; optionally remove the partial 7.0.9. More invasive; only worth it if depmod warnings or future akmod misbuilds become a real problem.

### Phase 4: Verification 🚫

Blocked on Phase 3. Once recovery is run:

- 🚫 Confirm radios work on 7.0.9.
- 🚫 Confirm camera still works on whichever kernel is current (`wpctl status` shows pipewire video source).
- 🚫 Confirm `dnf history info` for any subsequent transaction does not pull `kernel-devel-matched`.
- 🚫 Smoke-test the updated playbook on a clean run (idempotent, exits cleanly, mask survives).

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
