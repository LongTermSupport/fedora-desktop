# Research 01: Kernel / dmesg / firmware audit

## Summary

The kernel is **clean** — `/proc/sys/kernel/tainted == 0`, no out-of-tree modules
loaded, no oops / NMI / soft-lockup / `Call Trace` events anywhere in the last
seven days, and no `Direct firmware load … failed` messages. The biggest concrete
finding is a **pending reboot**: the system is running `6.19.14-200.fc43` but
`kernel-7.0.9-104.fc43` is already installed (Fedora 43 has moved to a 7.x
series since this boot), so a reboot will pull in newer drivers (`i915`, `xe`,
`iwlwifi`, `mtk_t7xx`) and likely fix several of the cosmetic boot warnings
below. The remaining items are all low-impact: an Intel ADL-P `i915` "Selective
fetch area calculation failed in pipe A" PSR warning, an `mtk_t7xx` WWAN
"Port AT is not opened, drop packets" message (modem is rfkill-blocked anyway),
seven `i801_smbus … busy` messages during boot, and an unrecognised SPI-NOR
JEDEC ID. Bluetooth (`hci0`) firmware took ~1.5s to load — long but normal for
Intel AX211. The previously-tainting `akmod-intel-ipu6` from plan 00043 has
been fully replaced by the in-tree, Fedora-signed `intel-ipu6` driver
(`intree: Y`, `signer: Fedora kernel signing key`), and the IPU6 camera stack
authenticates cleanly. Note: the prompt described the box as a Gen 11 X1 Carbon,
but `thinkpad_acpi` reports `21CB007CUK` — this is a **Gen 10** (i7-1265U
Alder Lake-P, not 1365U / Gen 11).

## Findings

### Kernel taint & out-of-tree modules

- **Severity**: ⚪ harmless — taint is clean
- **Evidence**:
  ```
  /proc/sys/kernel/tainted = 0
  intel_ipu6_isys       159744  0
  intel_ipu6             94208  1 intel_ipu6_isys
  modinfo intel_ipu6 → filename: …/kernel/drivers/media/pci/intel/ipu6/intel-ipu6.ko.xz
                       intree:   Y
                       signer:   Fedora kernel signing key
  ```
  (source: `/tmp/lsmod.txt`, `/tmp/ipu6_modinfo.txt`)
- **Recommended action**: none — informational. The plan 00043 transition from
  `akmod-intel-ipu6` to in-tree `intel_ipu6` is confirmed complete and clean.

### Pending kernel upgrade (running kernel is stale)

- **Severity**: 🔴 actionable
- **Evidence**:
  ```
  uname -r         → 6.19.14-200.fc43.x86_64
  rpm -q kernel    → kernel-6.19.14-200.fc43.x86_64
                     kernel-7.0.4-100.fc43.x86_64
                     kernel-7.0.9-104.fc43.x86_64  (latest installed)
  ```
  (source: `/tmp/uname.txt`, host `rpm -q kernel-core` output)
- **Recommended action**: **reboot the host** to pick up `kernel-core-7.0.9-104`.
  That single action will (a) refresh `i915` / `xe` to the 7.x versions which
  have fixes for the ADL-P PSR / "Selective fetch" warning seen below, (b)
  refresh `iwlwifi` / `mtk_t7xx`, and (c) eliminate the version drift between
  loaded modules and on-disk `/lib/modules/*`. No Ansible change is needed —
  this is purely a user action. If two old kernels (`6.19.14`, `7.0.4`) are
  still installed alongside `7.0.9`, consider letting `dnf` prune them (default
  `installonly_limit=3` already handles that).

### i915 vs xe driver (Intel Iris Xe iGPU)

- **Severity**: 🟡 worth noting
- **Evidence**:
  ```
  i915 0000:00:02.0: [drm] Found alderlake_p (device ID 46a6) integrated display version 13.00 stepping D0
  i915 0000:00:02.0: [drm] Finished loading DMC firmware i915/adlp_dmc.bin (v2.20)
  i915 0000:00:02.0: [drm] GT0: GuC firmware i915/adlp_guc_70.bin version 70.49.4
  i915 0000:00:02.0: [drm] Selective fetch area calculation failed in pipe A
  lsmod: i915  5480448  18    xe  4689920  0
  ```
  (source: `/tmp/dk_gpu.txt`, `/tmp/lsmod.txt`)
- **Recommended action**: none — informational only. `i915` is the active
  driver and is the supported path for Alder Lake-P (`xe` is the experimental
  newer driver, currently *also* loaded but bound to no devices: refcount 0).
  Switching to `xe` for ADL-P is **not** recommended in the 6.x/7.x timeframe
  on this hardware — `xe` formally targets Lunar Lake / Battlemage and later.
  The "Selective fetch area calculation failed in pipe A" line is a known,
  benign PSR (Panel Self Refresh) edge-case on ADL-P and has been quietened
  upstream in newer kernels — the pending 7.0.9 kernel will most likely make
  it disappear.

### ACPI BIOS \_OSI(Linux) firmware bug

- **Severity**: ⚪ harmless boot chatter
- **Evidence**:
  ```
  May 21 21:48:42 kernel: ACPI: [Firmware Bug]: BIOS _OSI(Linux) query ignored
  May 21 21:48:55 kernel: thinkpad_acpi: ThinkPad BIOS N3AET89W (1.54 ), EC N3AHT54W
  ```
  (source: `/tmp/dk_bios.txt`, `/tmp/dk_biosver.txt`)
- **Recommended action**: none — informational only. Lenovo deliberately
  ignores the Linux \_OSI query so the BIOS exposes Windows-equivalent ACPI
  tables. This is the correct behaviour for Linux on a ThinkPad and is **not**
  something to fix via kernel boot params. (Adding `acpi_osi="Linux"` would
  break things, not fix them.) Latest ThinkPad X1C Gen 10 BIOS is in the
  N3AETxxW series; `1.54` is recent — leave alone or update via
  `fwupdmgr update` if a newer one ships.

### mtk_t7xx 5G modem chatter

- **Severity**: 🟡 worth noting
- **Evidence**:
  ```
  May 21 21:48:55 kernel: mtk_t7xx 0000:08:00.0: Port AT is not opened, drop packets
  May 21 21:48:55 kernel: thinkpad_acpi: rfkill switch tpacpi_wwan_sw: radio is blocked
  lspci: 08:00.0 Cellular controller/modem … MEDIATEK T700 5G Modem [14c3:4d75]
  ```
  (source: `/tmp/kerr7d.txt`, `/tmp/dk_storage.txt`, `/tmp/lspci.txt`)
- **Recommended action**: if the WWAN modem is unused (it is rfkill-blocked),
  consider blacklisting `mtk_t7xx` via an Ansible-managed
  `/etc/modprobe.d/blacklist-mtk-t7xx.conf` to eliminate the boot chatter and
  save ~0.1s of init. Keep the module unblacklisted if the modem is ever used.
  Low priority; cosmetic.

### i801 SMBus busy at boot

- **Severity**: ⚪ harmless boot chatter
- **Evidence**:
  ```
  May 21 21:48:55 kernel: i801_smbus 0000:00:1f.4: SMBus is busy, can't use it!
  (×7 in a tight cluster, all at the same second)
  ```
  (source: `/tmp/kerr7d.txt`)
- **Recommended action**: none — informational only. This is a long-standing
  ADL-P PCH quirk where the SMBus controller reports busy during the brief
  window when AML methods access it. The kernel recovers and SMBus works
  normally afterwards. No user-actionable fix.

### spi-nor unrecognised JEDEC ID

- **Severity**: ⚪ harmless boot chatter
- **Evidence**:
  ```
  May 21 21:48:55 kernel: spi-nor spi0.0: supply vcc not found, using dummy regulator
  May 21 21:48:55 kernel: spi-nor spi0.0: unrecognized JEDEC id bytes: f7 30 30 09 03 00
  ```
  (source: `/tmp/dk_spi.txt`)
- **Recommended action**: none. The Intel PCH SPI controller exposes the
  Management Engine's SPI flash chip; `spi-nor` is not the right driver for
  this (the ME flash is intentionally not user-accessible). The probe failure
  is expected. No fix needed.

### iwlwifi "Unhandled alg 0x707"

- **Severity**: ⚪ harmless
- **Evidence**:
  ```
  May 21 21:49:01 kernel: iwlwifi 0000:00:14.3: Unhandled alg: 0x707  (×3)
  iwlwifi: Detected Intel(R) Wi-Fi 6E AX211 160MHz
  iwlwifi: loaded firmware version 89.735b75a4.0 so-a0-gf-a0-89.ucode
  ```
  (source: `/tmp/dk_wlbt.txt`)
- **Recommended action**: none. AX211 firmware reports debug events with
  algorithm IDs that the in-kernel driver intentionally ignores. Cosmetic.
  Newer firmware shipped with `linux-firmware-20260410` should already be
  installed; the 7.0.9 kernel may have a newer `iwlmvm` that filters these.

### Bluetooth firmware slow load (~1.5s)

- **Severity**: ⚪ harmless boot chatter
- **Evidence**:
  ```
  May 21 21:48:56 kernel: Bluetooth: hci0: Found device firmware: intel/ibt-0040-0041.sfi
  May 21 21:48:57 kernel: Bluetooth: hci0: Firmware loaded in 1477195 usecs
  May 21 21:48:57 kernel: Bluetooth: hci0: Device booted in 16491 usecs
  ```
  (source: `/tmp/dk_wlbt.txt`)
- **Recommended action**: none. 1.48 seconds is normal for the AX211 BT
  combo's `ibt-0040-0041.sfi` blob. Successful load + clean Fseq execution.

### NVMe storage

- **Severity**: ⚪ harmless
- **Evidence**:
  ```
  May 21 21:48:43 kernel: nvme 0000:04:00.0: platform quirk: setting simple suspend
  May 21 21:48:43 kernel: nvme nvme0: D3 entry latency set to 10 seconds
  May 21 21:48:43 kernel: nvme nvme0: 16/0/0 default/read/poll queues
  May 21 21:48:56 kernel: nvme nvme0: using unchecked data buffer
  ```
  (source: `/tmp/dk_storage.txt`)
- **Recommended action**: none. The "simple suspend" quirk and 10s D3 latency
  are platform quirks Fedora applies to known Samsung / SK hynix OEM NVMe
  drives that misbehave under aggressive APST — this is the *fix*, not a bug.
  No write errors, no temperature warnings, no queue stalls.

### SOF audio (Alder Lake-P + ALC287)

- **Severity**: ⚪ harmless
- **Evidence**:
  ```
  May 21 21:48:56 kernel: sof-audio-pci-intel-tgl 0000:00:1f.3: Firmware info: version 2:2:0-57864
  May 21 21:48:56 kernel: sof-audio-pci-intel-tgl 0000:00:1f.3: Firmware: ABI 3:22:1 Kernel ABI 3:23:1
  May 21 21:48:56 kernel: snd_hda_codec_alc269 ehdaudio0D0: ALC287: picked fixup  (pin match)
  May 21 21:48:56 kernel: sof-audio-pci-intel-tgl: hda_dsp_hdmi_build_controls: no PCM in topology for HDMI converter 3
  ```
  (source: `/tmp/dk_audio.txt`)
- **Recommended action**: none. `sof-firmware` is *not* installed as a
  separate RPM (the SOF blobs ship inside `linux-firmware` on Fedora 43, which
  is at 20260410-1). ALC287 fixup applied cleanly. The "no PCM in topology
  for HDMI converter 3" line is benign topology trimming. Sound works.

### IPU6 camera (Intel CSI ISP)

- **Severity**: ⚪ harmless
- **Evidence**:
  ```
  May 21 21:48:55 kernel: intel-ipu6 0000:00:05.0: enabling device (0000 -> 0002)
  May 21 21:48:55 kernel: intel-ipu6 0000:00:05.0: Found supported sensor INT3474:01
  May 21 21:48:55 kernel: intel-ipu6 0000:00:05.0: Connected 1 cameras
  May 21 21:48:55 kernel: intel-ipu6 0000:00:05.0: CSE authenticate_run done
  May 21 21:48:55 kernel: intel-ipu6 0000:00:05.0: IPU6-v3[465d] hardware version 5
  May 21 21:48:56 kernel: ov2740 i2c-INT3474:01: supply AVDD not found, using dummy regulator (×3)
  ```
  (source: `/tmp/dk_camera.txt`, `/tmp/dk_audio.txt`)
- **Recommended action**: none. IPU6 + OV2740 sensor probe clean. The "supply
  AVDD/DOVDD/DVDD not found, using dummy regulator" warnings are ACPI-level
  power-rail descriptions that the kernel can't introspect but doesn't need
  to (the BIOS already powered the sensor); webcam works.

### USB device reset (one-shot)

- **Severity**: ⚪ harmless
- **Evidence**:
  ```
  May 21 21:49:05 kernel: usb 3-6: reset full-speed USB device number 2 using xhci_hcd
  ```
  (source: `/tmp/dk_usb.txt`)
- **Recommended action**: none. Single reset at boot, no repeat loop. Likely
  a quirk during driver bind for one of the internal USB devices (BT, IPU6
  IVSC, fingerprint, or NFC). Not a btusb reset loop.

### i2c_hid_acpi (ELAN touchpad) incomplete report

- **Severity**: 🟡 worth noting
- **Evidence**:
  ```
  May 21 21:49:18 kernel: i2c_hid_acpi i2c-ELAN067C:00: i2c_hid_get_input: incomplete report (14/60419)
  ```
  (source: `/tmp/kerr7d.txt`)
- **Recommended action**: monitor only. A single occurrence is normal noise
  during initial HID descriptor read; recurrence on resume from suspend would
  indicate the well-known ELAN i2c-hid bug that needs the `i2c_hid_core`
  module to be unloaded/loaded around suspend. If the user reports the
  touchpad cursor freezing after wake, revisit with an Ansible
  `/etc/systemd/system/elan-i2c-hid-reset.service` workaround. No action
  needed currently.

### Suspend / resume

- **Severity**: ⚪ harmless — not exercised this boot
- **Evidence**:
  ```
  May 21 21:48:42 kernel: Low-power S0 idle used by default for system suspend
  May 21 21:48:42 kernel: ACPI: PM: (supports S0 S4 S5)
  /tmp/pm.txt: 11 lines — all hibernation-region registration, no actual suspend cycle
  ```
  (source: `/tmp/pm.txt`, `/tmp/dk_susp.txt`)
- **Recommended action**: none from this layer — the laptop has not been
  suspended during the current uptime, so there's nothing to analyse. The
  power-management agent will go deeper. Note that the platform supports
  **S0ix only** (no S3 / "deep" sleep), which is correct and intentional for
  Alder Lake-P ThinkPads; do not attempt to enable S3 via `mem_sleep_default=deep`
  unless the user is willing to investigate downsides separately.

### Watchdog / NMI / soft-lockups (7-day history)

- **Severity**: ⚪ none
- **Evidence**:
  ```
  journalctl --since "7 days ago" -g 'lockup|NMI|watchdog|oops|BUG:|RIP:|Call Trace'
  → 14 "-- Boot … --" markers only, no actual hits
  ```
  (source: `/tmp/lockups.txt`)
- **Recommended action**: none. Stable. (14 boots over 7 days indicates
  routine daily reboot/shutdown behaviour, no crash boots.)

### Firmware loader failures

- **Severity**: ⚪ none
- **Evidence**:
  ```
  grep -iE 'firmware load.*fail|Direct firmware load|firmware:.*fail' /tmp/dk.txt
  → 0 hits
  rpm -q linux-firmware → linux-firmware-20260410-1.fc43.noarch
  ```
  (source: `/tmp/dk_fw_fail.txt`, host `rpm -q`)
- **Recommended action**: none.

### ThinkPad ACPI / EC / thermal

- **Severity**: ⚪ harmless
- **Evidence**:
  ```
  May 21 21:48:42 kernel: ACPI: thermal: Thermal Zone [THM0] (53 C)
  May 21 21:48:42 kernel: intel_pstate: HWP enabled
  May 21 21:48:55 kernel: thinkpad_acpi: secondary fan control detected & enabled
  May 21 21:48:55 kernel: thinkpad_acpi: battery 1 registered (start 75, stop 80, behaviours: 0xb)
  ```
  (source: `/tmp/dk_thinkpad.txt`, `/tmp/dk_platform_filtered.txt`)
- **Recommended action**: none. Charge thresholds (75/80) and dual-fan control
  are active. `intel_pstate` is in HWP mode (the correct mode for Alder Lake-P).

## Out-of-scope

These came up but the right layer to address them is not this audit:

- **Power-management deep-dive (S0ix residency, package C-states, EPP / EPB
  tuning, suspend-then-hibernate)** — explicitly the power agent's territory.
  This audit only confirmed S0ix is the supported sleep mode and that
  `intel_pstate` HWP is enabled.
- **Firmware updates (BIOS / TBT / ME / NVMe)** — handled by `fwupdmgr`, not
  by the kernel/dmesg layer. ESRT exposes 15 firmware entries; defer to the
  firmware-update agent or a `fwupdmgr refresh && fwupdmgr update` user step.
- **GNOME / userspace audio / pipewire** — out of scope for kernel audit. The
  ALSA/SOF kernel side is healthy; if the user reports audio glitches it's
  almost certainly a pipewire/wireplumber configuration issue.
- **The fact that the prompt says "Gen 11 / i7-1365U" but `thinkpad_acpi`
  reports `21CB007CUK` (Gen 10, i7-1265U)** — purely an inventory
  documentation note; doesn't change any of the recommendations.

## Raw evidence tarball

Files created in `/tmp/` for attachment / reproduction:

- `/tmp/dk.txt` — full current-boot `journalctl -k -b 0` (1390 lines)
- `/tmp/dk_alerts.txt` — filtered to error/fail/warn/fault/reset/firmware (37 lines)
- `/tmp/dk_gpu.txt` — i915 / xe / drm lines (46 lines)
- `/tmp/dk_gpu_quirks.txt` — PSR / dmc / page-fault / pipe subset
- `/tmp/dk_wlbt.txt` — iwlwifi / btusb / bluetooth / hci0 lines (89 lines)
- `/tmp/dk_audio.txt` — snd / sof / hda / regulator lines (43 lines)
- `/tmp/dk_storage.txt` — nvme / ata / scsi / block lines (36 lines)
- `/tmp/dk_platform.txt` — acpi / pstate / mei / tpm / thunderbolt / thermal (221 lines)
- `/tmp/dk_platform_filtered.txt` — mei / tpm / thunderbolt / thermal / pstate subset (28 lines)
- `/tmp/dk_camera.txt` — intel-ipu6 / v4l / ov2740 lines (8 lines)
- `/tmp/dk_thinkpad.txt` — thinkpad_acpi lines (14 lines)
- `/tmp/dk_susp.txt` — suspend / resume / s2idle (4 lines)
- `/tmp/dk_usb.txt` — usb device reset / disconnect lines (1 line)
- `/tmp/dk_bios.txt` — Firmware Bug / ACPI BIOS Error lines (1 line)
- `/tmp/dk_biosver.txt` — BIOS / EC version lines
- `/tmp/dk_spi.txt` — spi-nor lines (2 lines)
- `/tmp/dk_aspm.txt` — ASPM / L1SS lines (2 lines)
- `/tmp/dk_fw_fail.txt` — firmware-load failures (0 lines — empty by design)
- `/tmp/dk_xe.txt` — xe driver mentions (1 line)
- `/tmp/pm.txt` — `journalctl -b 0 -g 'PM: '` (11 lines, no suspend cycle)
- `/tmp/kerr7d.txt` — `journalctl --since "7 days ago" -p err -k` (11 lines)
- `/tmp/lockups.txt` — lockup / NMI / oops / BUG / Call Trace, 7 days (14 boot markers, 0 actual events)
- `/tmp/lsmod.txt` — all loaded modules (239 lines)
- `/tmp/oot_candidates.txt` — likely out-of-tree module names (intel_ipu6 only — and it's in-tree)
- `/tmp/module_taint.txt` — per-module taint flags (empty — no tainted modules)
- `/tmp/ipu6_modinfo.txt` — modinfo for intel_ipu6 confirming in-tree + Fedora-signed
- `/tmp/lspci.txt` — lspci -nnk (98 lines)
- `/tmp/fwpkgs.txt` — installed firmware RPM versions
- `/tmp/tainted.txt` — `/proc/sys/kernel/tainted` (value: 0)
- `/tmp/version.txt` — `/proc/version`
- `/tmp/uname.txt` — `uname -a`
