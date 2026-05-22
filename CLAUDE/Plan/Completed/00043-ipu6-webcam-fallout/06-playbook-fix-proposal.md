# Playbook Fix Proposal — `play-ipu6-webcam.yml`

**Problem the playbook caused**: pulling `akmod-intel-ipu6` triggered installation of `kernel-devel-matched` → `kernel-devel-7.0.9` → `kernel-core-7.0.9` + `kernel-modules-core-7.0.9` — but **not** the full `kernel-7.0.9` metapackage. That left 7.0.9 with no `iwlwifi`/`btusb`/etc., killing WiFi and Bluetooth on the next boot.

**Secondary problem**: on Fedora 43+ (kernel 6.19+, 7.0+), mainline already ships `intel-ipu6` + `intel-ipu6-isys` + `intel-ipu6-psys` in-tree. The RPM Fusion akmod only adds a redundant `psys` overlay that emits 210 depmod symbol-mismatch warnings.

## Proposed changes

### Change 1 — drop `akmod-intel-ipu6` from the package list

In `playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml`, the package list currently is:

```yaml
- akmod-intel-ipu6
- ipu6-camera-bins
- ipu6-camera-hal
- gstreamer1-plugins-icamerasrc
- v4l2-relayd
- libcamera-tools
- v4l-utils
- kernel-devel
- kernel-headers
- mokutil
```

Update to:

```yaml
- ipu6-camera-bins
- ipu6-camera-hal
- gstreamer1-plugins-icamerasrc
- v4l2-relayd
- libcamera-tools
- v4l-utils
```

Removed:

- `akmod-intel-ipu6` — mainline kernel provides the same drivers in-tree; the akmod adds no value and trips depmod.
- `kernel-devel`, `kernel-headers` — only needed for out-of-tree module builds; no longer applicable.
- `mokutil` — was only needed because of Secure Boot signing for the akmod-built module.

### Change 2 — remove the MOK enrollment block

The entire Secure Boot / MOK block (`Check Secure Boot state`, `Validate MOK password is in vault when Secure Boot is on`, `Check if DKMS MOK key exists`, `Check if MOK is already enrolled`, `Import MOK key for enrollment`, plus the MOK-related branch of the final pause prompt) becomes dead code. Delete it.

### Change 3 — remove the `akmods --force` task

Currently:

```yaml
- name: Run DKMS / akmods autoinstall so the module is built and signed now
  ansible.builtin.command: akmods --force
  changed_when: true
```

Delete this task.

### Change 4 — adjust the precondition assertion

Currently asserts `intel_ipu6` is loaded. This is still correct, but the comment should clarify the playbook now only installs userspace.

### Change 5 — adjust the final pause message

The "ACTION REQUIRED: SECURE BOOT MOK ENROLLMENT" branch becomes dead. Simplify the final pause to:

```
✅ Userspace IPU6 stack installed.

To make the camera visible to apps RIGHT NOW (no reboot):

  systemctl --user restart wireplumber pipewire

Then test:

  cam -l                          # libcamera should list the camera
  wpctl status                    # pipewire should show "Built-in Front Camera"

If the camera does not show up, a reboot is the most reliable way to
ensure the mainline IPU6 module + pipewire come up in the right order.

NOTE: The v4l2-relayd@icamerasrc.service may crash-loop on the
'SPLASHSRC' env var. This is harmless (the base v4l2-relayd.service
is a Fedora stub) — mask it if it's noisy:
  sudo systemctl mask v4l2-relayd@icamerasrc.service
```

### Change 6 — add a defensive note in the playbook header

Add a comment explaining why we DON'T install the akmod:

```yaml
# NOTE: Do not install akmod-intel-ipu6 on Fedora 43+.
# Mainline kernel ships intel-ipu6 / intel-ipu6-isys / intel-ipu6-psys
# in-tree under /lib/modules/<kernel>/kernel/drivers/media/pci/intel/ipu6/.
# The RPM Fusion akmod adds a redundant /extra/intel-ipu6/.../psys/ overlay
# that emits ~210 depmod symbol-mismatch warnings on modern kernels.
# Worse, akmod-intel-ipu6 pulls kernel-devel-matched which can leave the
# system with a partially-installed newer kernel (kernel-core only, no
# kernel-modules) — observed 2026-05-21, see
# untracked/2026-05-21-ipu6-fallout/ for the incident report.
```

## Out-of-scope (for now)

- Shipping a real `v4l2-relayd` `/etc/v4l2-relayd/setup.cfg` so that the icamerasrc bridge actually creates a `/dev/video` capture node for legacy V4L2 apps. Pipewire-libcamera already handles browsers (Firefox, Chrome) and Wayland-native apps, so this is mostly cosmetic. Worth a separate plan if a legacy app needs it.
- A `play-ipu6-webcam-uninstall.yml` playbook that walks the cleanup commands from `05-recovery-plan.md`. Worth adding for users hitting this same trap.

## Verification after applying the fix

Fresh box (or after Path A/B recovery) running the updated playbook:

1. `dnf history info` of the resulting transaction should show **no** `kernel-*` packages installed by the transaction.
2. `rpm -q akmod-intel-ipu6` should return `package akmod-intel-ipu6 is not installed`.
3. `cam -l` should list the camera.
4. `wpctl status` should show "Built-in Front Camera" as a video source.
