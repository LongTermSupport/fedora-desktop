# Sub-Agent 2 Report — Kernel 7.0.4 Post-Login Freeze Investigation

**Scope**: Why did 7.0.4-100 freeze after GNOME login on boot -1, when the same kernel + same IPU6 stack worked perfectly on boot -4 earlier in the day?

## Final log entries (boot -1, journal absolute end at 21:47:11)

- `b-1full.txt:5340-5343` — `warp-svc[1487]: route-change` + `systemd-localed deactivated` + `audit BPF prog-id=97 UNLOAD`
- **No `shutdown`**, no `Syncing filesystems`, no `Journal stopped` — abrupt halt, classic hard-hang signature (contrast boot -4 which ended cleanly).
- Kernel ring ends at 21:46:42 with a routine `usb 3-6 reset` — no oops, no `BUG:`, no soft/hard lockup, no GPU hang, no panic anywhere in 1401 kernel lines.

## IPU6 stack state on boot -1

- IPU6 PCI 00:05.0 enabled, INT3474 sensor found, CSE auth completed, `ov2740 i2c-INT3474:01` probe succeeded with dummy regulators. Identical to working boot -4.
- **`v4l2-relayd@icamerasrc` crashed 5 times in 1s** (21:45:37–21:45:39):
  - Each iteration logged `g_source_remove: assertion 'tag > 0' failed` and `gst_element_set_state: assertion 'GST_IS_ELEMENT (element)' failed`.
  - Root cause: missing `SPLASHSRC` env var (`Referenced but unset environment variable evaluates to an empty string: SPLASHSRC`).
  - This is the documented Fedora stub-on-Fedora behaviour with no `setup.cfg`.
  - Systemd hit start-limit-hit, gave up at 21:45:39 — service `failed`, did not restart further.
- Pipewire/wireplumber spammed `spa.v4l2: Cannot open '/dev/videoN': 13, Permission denied` 29× (pipewire trying all the IPU6 subdev nodes which aren't capture devices).
- libcamera ov2740 sensor probe warnings (`Inappropriate ioctl for device`, `IPA module ov2740.yaml not found, falling back to uncalibrated.yaml`).
- All concentrated at 21:46:41, then quiet.
- No further IPU6/camera/v4l2 activity in the final 30 seconds before the freeze.

## Most likely cause

**Not the IPU6 stack.** Evidence:

- v4l2-relayd loop terminated at 21:45:39 (90s before freeze).
- pipewire v4l2 errors stopped at 21:46:41 (30s before freeze).
- System survived doing routine GNOME idle work (xdg-portal, firefox D-Bus timeout, GJS disposed-object warnings 13s before halt).
- Freeze at 21:47:11 has **no preceding diagnostic in any log** — consistent with a silent hardware-level lockup (CPU/RAM/firmware), not a software bug that would have left a fingerprint.

Hypotheses ruled out:

- **A (v4l2-relayd/icamerasrc loop)**: ruled out — camera activity ceased well before freeze.
- **B (GNOME/mutter crash)**: ruled out — gnome-shell was still emitting JS warnings 13s before halt.
- **C (pipewire-libcamera starvation)**: ruled out — v4l2 errors stopped 30s before halt.
- **D (i915/xe GPU hang)**: ruled out — no `drm` errors post-startup, no `GPU HANG` line.

## Recommended remediation

1. **Do not roll back the IPU6 playbook on this evidence** — the camera stack was idle when the freeze occurred.
2. **Cosmetic cleanup** (low priority): mask the broken `v4l2-relayd@icamerasrc.service` until a `setup.cfg` is shipped, to silence the crash loop and pipewire v4l2 spam:
   ```bash
   systemctl mask v4l2-relayd@icamerasrc.service
   ```
3. **If the freeze recurs**, capture:
   ```bash
   sudo dmesg --ctime > /tmp/dmesg.txt
   ```
   immediately after the next freeze.
   Check `/sys/firmware/acpi/tables/ERST` and MCE entries.
   Consider enabling pstore + `kernel.panic_on_oops=1` to force a crashdump on the next event.
4. **Monitor** — one freeze on boot -1 with a clean boot -4 is not yet a pattern. If 7.0.4 freezes again, suspect kernel regression independent of the IPU6 work.
