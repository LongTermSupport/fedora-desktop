# Hardware & Kernel Modules — Fedora 44 Migration Research (Plan 00050)

This dimension assesses the repo's out-of-tree kernel modules (akmod-nvidia, evdi/DisplayLink via DKMS, akmod-VirtualBox) and hardware enablement (Intel IPU6 webcam, MOK/Secure Boot signing) against Fedora 44. Confirmed baseline: **Fedora 44 was released on 28 April 2026** and shipped with **kernel 6.19.14-300.fc44** at GA ([Fedora Magazine](https://fedoramagazine.org/announcing-fedora-linux-44/)); **kernel 7.0** arrived in the F44 stable updates repository in mid-to-late May 2026 ([Fedora Discussion](https://discussion.fedoraproject.org/t/release-date-of-kernel-version-7-0/189300)), so a June 2026 migration lands directly onto a 7.0.x kernel. RPM Fusion's F44 branch is live with **akmod-nvidia on the 595 driver branch by default** (Maxwell/Pascal moved to the legacy 580xx branch) ([LinuxCapable](https://linuxcapable.com/how-to-install-nvidia-drivers-on-fedora-linux/)). NVIDIA's **CUDA repo for fedora44 exists** (CUDA 13.3, repo dated 2026-05-19, and now also carries `nvidia-driver-610.43.02-1.fc44` packages) ([NVIDIA repo index](https://developer.download.nvidia.com/compute/cuda/repos/fedora44/x86_64/)). The pinned **displaylink-rpm v6.2.0-1 release ships fedora-44 assets with evdi 1.14.16** ([GitHub API](https://api.github.com/repos/displaylink-rpm/displaylink-rpm/releases/latest)), and evdi gained preliminary kernel 7.0 support in 1.14.15 ([evdi releases](https://github.com/DisplayLink/evdi/releases)). RPM Fusion ships **fc44 builds of the IPU6 userspace stack** (`ipu6-camera-bins 0.0-20`, `ipu6-camera-hal 0.0-28`, built Feb 2026) ([RPM Fusion F44 mirror](https://muug.ca/mirror/rpmfusion/nonfree/fedora/releases/44/Everything/x86_64/os/Packages/i/)). Separately, the **Microsoft 2011 Secure Boot CA expires in June 2026**, with shim signing transitioning to the 2023 key ([Fedora Magazine](https://fedoramagazine.org/expiration-of-microsoft-secure-boot-keys/)).

## HW-01: Kernel versionlock from previous release can block or half-install the F44 kernel

**Severity**: high

**Area**: kernel management

**Files**:

- `files/usr/local/bin/manage-kernel-versions.py:188-205` (locks `kernel-0:<full_version>.x86_64`, e.g. an fc43 NEVRA), `:335-345` (previous-minor lock logic)
- `playbooks/imports/play-AB-dnf-upgrade.yml:10-23` and `:100-105` (documents the 2026-05-21 incident where a versionlocked `kernel` metapackage caused a half-installed 7.0.9 kernel — `kernel-core` without `kernel-modules`, all radios dead on next boot)
- `playbooks/imports/optional/common/play-advanced-kernel-management.yml:78-83` (runs the lock script on every play run)

**Concern**: The kernel-version-manager deliberately versionlocks the highest patch of the previous-minor kernel as a hardware-compat fallback. At F43→F44 migration time those locks reference fc43 NEVRAs. The repo's own incident record (play-AB-dnf-upgrade.yml lines 18-23) proves that a versionlock on the `kernel` metapackage can cause DNF to install only part of a new kernel's sub-package set — the exact failure mode that produced a bootable-but-driverless kernel (no iwlwifi.ko/btusb.ko) on F43. During the `dnf system-upgrade` to F44 (which immediately pulls a 6.19.x or 7.0.x fc44 kernel), stale fc43 kernel locks risk either blocking the fc44 kernel from depsolving or reproducing the half-install. This breaks the core migration flow itself, not just a hardware play.

**Recommendation**: Add an explicit pre-upgrade step to the F44 migration runbook: list and remove all `kernel*` versionlock entries (`dnf versionlock list` / `delete`) before launching the release upgrade, then re-run `play-advanced-kernel-management.yml` after first boot into F44 so the lock regime re-establishes itself with fc44 NEVRAs. Consider teaching `manage-kernel-versions.py` to drop locks whose release tag (`fcNN`) does not match the running distro release.

## HW-02: akmod-nvidia build breakage window on F44 kernels — gate the upgrade timing

**Severity**: medium

**Area**: nvidia

**Files**:

- `playbooks/imports/optional/hardware-specific/play-nvidia.yml:37-47` (installs `akmod-nvidia`), `:265-302` (5-minute poll for the built `.ko` then `akmods --force` fallback), `:349-373` (reboot gating on built vs loaded version)
- `files/usr/local/bin/shutdown-with-update:22-37` (waits up to 5 minutes for akmods at shutdown)

**Concern**: NVIDIA's akmod is historically the first out-of-tree module to break on a Fedora kernel bump, and F44 already has a documented instance: driver 590.48.01 failed to build against kernel 6.19.x at the F44 GA window, fixed only via an RPM Fusion koji patch ([Fedora Discussion](https://discussion.fedoraproject.org/t/fedora-44-kernel-6-19-x-nvidia-akmod-cannot-be-built/181035)). The legacy 470xx akmod also fails against kernel 7.0.4 (RPM Fusion bug 7454, [Fedora Discussion](https://discussion.fedoraproject.org/t/akmods-fails-to-build-nvidia-470xx-kernel-7-0-4-100/190994)). Since F44 stable moved to kernel 7.0.x in May 2026, the migration must land on an RPM Fusion driver build (595/610 branch) confirmed to compile against 7.0.x. The playbook's poll/force/halt logic will correctly fail fast on a build failure, but that still leaves the host without the driver mid-migration.

**Recommendation**: Before migrating, check the RPM Fusion F44 updates repo for the current akmod-nvidia version and confirm (via RPM Fusion bugzilla/koji) that it builds against the F44 kernel the host will receive (7.0.x as of June 2026). Keep the previous-minor versionlock fallback kernel (HW-01 re-lock step) as the rollback path if the akmod fails post-upgrade. No playbook code change is required for the RTX 500 Ada target — it remains on the current 595+ branch; only Maxwell/Pascal-era GPUs would need the new `akmod-nvidia-580xx` legacy split.

## HW-03: NVIDIA CUDA fedora44 repo now ships driver 610.x packages — re-verify the exclude fence

**Severity**: medium

**Area**: nvidia/cuda

**Files**:

- `playbooks/imports/optional/hardware-specific/play-nvidia.yml:78-87` (CUDA repo templated on `fedora_version`, with `exclude:` list at line 86), `:89-99` (installs `cuda-toolkit` and asserts `akmod-nvidia` survived)
- `vars/fedora-version.yml:6` (`fedora_version: 43`)

**Concern**: Good news first: `https://developer.download.nvidia.com/compute/cuda/repos/fedora44/x86_64/` exists and serves CUDA 13.3 (verified 2026-06-12), so the `fedora_version` bump will not 404 — unlike some previous releases where NVIDIA lagged Fedora. However, the fedora44 repo carries `nvidia-driver-610.43.02-1.fc44` driver packages. The repo definition's `exclude:` fence (line 86) is what stops this repo from ever replacing the RPM Fusion akmod driver; any new package naming NVIDIA introduces in the 610 era (e.g. new `nvidia-open*`/kmod spellings) that slips past the globs would let a `dnf upgrade` swap the driver stack out from under akmods. The play's `rpm -q akmod-nvidia` assert (lines 95-99) only checks the akmod package is present, not that no NVIDIA-repo driver got co-installed.

**Recommendation**: At migration time, diff the fedora44 repo's driver-related package names against the `exclude:` globs and extend the list if NVIDIA added new names. Consider strengthening the post-install assert to also check that no `nvidia-driver`/`nvidia-open` package from the cuda-fedora44 repo is installed. Also confirm `cuda-toolkit` 13.3 remains compatible with the cuDNN 9.22 cu13 wheel pinned at lines 16-18.

## HW-04: DisplayLink fedora-44 RPM exists, but evdi 1.14.16 vs kernel 7.0 needs build verification

**Severity**: low

**Area**: displaylink

**Files**:

- `playbooks/imports/optional/hardware-specific/play-displaylink.yml:11-12` (pins `displaylink_version: v6.2.0-1`, `evdi_version: 1.14.16`), `:127` (download URL templated on `fedora_version` — auto-flips to `fedora-44-…` on the vars bump), `:49-102` (latest-release assert against GitHub)
- `scripts/check-displaylink-status.sh` (post-install verification invoked at play line 289)

**Concern**: The pinned v6.2.0-1 release publishes a `fedora-44-displaylink-1.14.16-1.github_evdi.x86_64.rpm` asset (verified via the GitHub releases API, 2026-06-12), so the existing pins carry over to F44 unchanged. The residual risk is the DKMS build of evdi against the F44 kernel: evdi only gained *preliminary* kernel 7.0 support in 1.14.15, and F44 stable is on 7.0.x as of late May 2026. A DKMS build failure at install time (or on the next kernel update) would leave dock monitors dead until upstream patches land. The play's version-currency assert (lines 73-102) will also start failing the play as soon as displaylink-rpm tags a release newer than v6.2.0-1, forcing a pin bump as part of the migration.

**Recommendation**: During migration, run the play after first boot into the F44 kernel and watch the DKMS build; if displaylink-rpm has tagged a newer release by then, bump `displaylink_version`/`evdi_version` together (the assert will force this anyway). Verify `dkms status` shows evdi built against the running 7.0.x kernel before relying on docked displays.

## HW-05: IPU6 userspace stack carries over to F44, but upstream stagnation is a watch-item

**Severity**: low

**Area**: ipu6 webcam

**Files**:

- `playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml:17-19` (comment asserts the in-tree driver + pipewire-libcamera path on "Fedora 43+ kernel ≥ 6.19"), `:21-31` (hard rule: do NOT install `akmod-intel-ipu6` — the 2026-05-21 fallout), `:61-76` (installs `ipu6-camera-bins`, `ipu6-camera-hal`, `gstreamer1-plugins-icamerasrc`, `v4l2-relayd` from RPM Fusion), `:78-92` (masks the broken `v4l2-relayd@icamerasrc` instance)

**Concern**: RPM Fusion ships fc44 builds of the full userspace set (`ipu6-camera-bins 0.0-20.20250627git…fc44`, `ipu6-camera-hal 0.0-28.20250627git…fc44`, verified on the F44 mirror), and the in-tree `intel_ipu6` driver remains the supported kernel path on 6.19/7.0 — so the play should work on F44 as-is once `fedora_version` is bumped. Two residual concerns: (1) the RPM Fusion IPU6 userspace packages are reportedly no longer actively updated upstream ([intel/ipu6-drivers#375](https://github.com/intel/ipu6-drivers/issues/375)), so the HAL/icamerasrc path may bit-rot against F44's newer libcamera/pipewire (GNOME 50 stack); (2) the play's guard comments are F43-specific and the masked `v4l2-relayd@icamerasrc` workaround needs re-testing — F44 may have fixed or further broken that template unit.

**Recommendation**: After migration, re-run the play's verification steps (`cam -l`, `wpctl status`) on F44; if the libcamera software-ISP path now enumerates the camera without the proprietary HAL, consider slimming the play to the libcamera/pipewire path. Keep the akmod-intel-ipu6 prohibition — it remains correct on F44's in-tree driver. Refresh the F43-specific comments to say F43/F44.

## HW-06: MOK signing flows are unaffected by the June 2026 Microsoft cert expiry — but verify shim updates land

**Severity**: info

**Area**: secure boot

**Files**:

- `playbooks/imports/optional/hardware-specific/play-nvidia.yml:158-251` (akmods MOK: `kmodgenca`, `mokutil --import`, enrolment pause)
- `playbooks/imports/optional/hardware-specific/play-displaylink.yml:18-37`, `:131-194` (DKMS MOK: `/var/lib/dkms/mok.pub` enrolment)

**Concern**: The Microsoft 2011 Secure Boot UEFI CA expires in June 2026; Microsoft now signs shim with the 2023 key, and Fedora is rolling out multi-key-signed first-stage boot loaders (already in Rawhide/F45) ([Fedora Magazine](https://fedoramagazine.org/expiration-of-microsoft-secure-boot-keys/)). This does **not** affect the repo's MOK flows: locally generated akmods/DKMS keys are enrolled in the machine's MOK database and chain to shim's MOK verification, not to Microsoft's CA, and already-booting machines continue to boot after expiry. This is a forward-looking watch-item, not a migration blocker — no repo change is required.

**Recommendation**: No action for the F44 migration itself. As polish, after migrating confirm `shim`/`grub2` updates apply cleanly on F44 and that firmware vendors' 2023 KEK/db updates (where offered via fwupd) do not invalidate the enrolled MOK (they do not by design, but a `mokutil --test-key` check post-update is cheap). Existing MOK enrolment playbook logic carries over unchanged.

## HW-07: akmod-VirtualBox is the most fragile module on a new kernel series

**Severity**: low

**Area**: virtualbox

**Files**:

- `playbooks/imports/optional/experimental/play-virtualbox-windows.yml:15-18` (installs `kernel-headers`, `kernel-devel`, `dkms`, `akmod-VirtualBox`), `:42-44` (handler runs `akmods --kernels $(uname -r)` then restarts `vboxdrv.service`)

**Concern**: Of the out-of-tree modules this repo manages, VirtualBox's kmod historically breaks first on a new kernel minor (it needs upstream VirtualBox patches for each kernel API change, then RPM Fusion packaging). With F44 on kernel 7.0.x, `akmod-VirtualBox` may fail to build until RPM Fusion catches up, which would fail this play's handler. This is an experimental, optional play, so impact is contained.

**Recommendation**: Treat as verify-on-F44: after migration, run the play and confirm `akmods --kernels $(uname -r)` succeeds and `vboxdrv` loads. If the build fails, hold the play until RPM Fusion publishes a VirtualBox build for the running F44 kernel rather than pinning an older kernel for it.

## HW-08: Stale F43-era comments in hardware plays

**Severity**: low

**Area**: documentation hygiene

**Files**:

- `playbooks/imports/optional/hardware-specific/play-nvidia.yml:4` ("recommended method for Fedora 43"), `:44` ("F43 rename of nvidia-vaapi-driver")
- `playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml:17`, `:21`, `:29` (F43-specific guard comments)

**Concern**: Nothing functional — these plays template on `fedora_version` correctly (verified: `rpmfusion-*-release-44.noarch.rpm` resolves on the RPM Fusion mirror network, and the CUDA/displaylink URLs flip automatically). But the header comments hard-code "Fedora 43"/"F43", which will mislead future readers about what has been validated on F44 and when the akmod-intel-ipu6 prohibition was last re-confirmed.

**Recommendation**: When the F44 bump lands, update these comments to reference F44 (or make them release-agnostic, e.g. "Fedora 43+"), and note the date each hardware play was last verified on the F44 kernel.

## Sources

- https://fedoramagazine.org/announcing-fedora-linux-44/ — F44 release 28 April 2026, kernel 6.19.14-300.fc44, GNOME 50
- https://discussion.fedoraproject.org/t/release-date-of-kernel-version-7-0/189300 — kernel 7.0 to F44 updates mid-to-late May 2026
- https://9to5linux.com/fedora-linux-44-is-now-available-for-download-heres-whats-new — F44 release overview
- https://linuxcapable.com/how-to-install-nvidia-drivers-on-fedora-linux/ — F44 NVIDIA 595 default branch, 580xx legacy split, akmod-nvidia availability
- https://discussion.fedoraproject.org/t/fedora-44-kernel-6-19-x-nvidia-akmod-cannot-be-built/181035 — akmod-nvidia 590.48.01 build failure on F44/6.19, koji fix
- https://discussion.fedoraproject.org/t/akmods-fails-to-build-nvidia-470xx-kernel-7-0-4-100/190994 — nvidia-470xx akmod failure vs kernel 7.0.4 (RPM Fusion bug 7454)
- https://developer.download.nvidia.com/compute/cuda/repos/fedora44/x86_64/ — CUDA fedora44 repo exists (CUDA 13.3, nvidia-driver-610.43.02-1.fc44)
- https://api.github.com/repos/displaylink-rpm/displaylink-rpm/releases/latest — v6.2.0-1 with fedora-44 asset, evdi 1.14.16
- https://github.com/DisplayLink/evdi/releases — evdi 1.14.15 preliminary kernel 7.0 support, 1.14.16 build fixes
- https://muug.ca/mirror/rpmfusion/nonfree/fedora/releases/44/Everything/x86_64/os/Packages/i/ — fc44 ipu6-camera-bins/hal packages
- https://github.com/intel/ipu6-drivers/issues/375 — RPM Fusion IPU6 userspace packages no longer actively updated
- https://fedoramagazine.org/expiration-of-microsoft-secure-boot-keys/ — Microsoft 2011 Secure Boot CA expiry June 2026, 2023 key transition, MOK unaffected
