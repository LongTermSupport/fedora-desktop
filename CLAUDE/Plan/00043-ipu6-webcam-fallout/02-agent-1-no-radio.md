# Sub-Agent 1 Report — Kernel 7.0.9 No-Radio Investigation

**Scope**: Why did WiFi (Intel AX211) and Bluetooth both vanish on kernel 7.0.9-104 boots (-3, -2)?

## Root cause

The `kernel-modules-7.0.9-104.fc43` RPM is **not installed**. Only `kernel-core-7.0.9` and `kernel-modules-core-7.0.9` are present.

```
kernel-core-7.0.9-104.fc43.x86_64
kernel-modules-core-7.0.9-104.fc43.x86_64
kernel-devel-7.0.9-104.fc43.x86_64
kernel-devel-matched-7.0.9-104.fc43.x86_64
# NO kernel-modules-7.0.9-104, NO kernel-modules-extra-7.0.9-104
```

Compare 7.0.4 and 6.19.14, which both have the full quartet (core + modules-core + modules + modules-extra).

`iwlwifi.ko`, `iwlmvm.ko`, `iwlmld.ko`, `btusb.ko`, `btintel.ko`, and the in-tree `intel-ipu6.ko/intel-ipu6-isys.ko` all live in `kernel-modules`. Without it: `/lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/net/wireless/intel/iwlwifi/{mld,mvm}/` are empty directories, `drivers/bluetooth/` doesn't exist, and `drivers/media/pci/intel/ipu6/` is empty.

## Evidence in journals

Boot -3 (21:16:19–21:22:41) and boot -2 (21:23:00–21:44:58) on 7.0.9-104 show **ZERO** `iwlwifi`/`btusb`/`hci`/`ipu6`/`intel-ipu6` lines in the kernel ring buffer. Compare boot -1 (7.0.4) with 96 such lines, boot 0 (6.19.14) with 79.

NetworkManager started fine (`May 21 21:18:27`–`21:18:28`) but found no wifi/BT interfaces because the drivers don't exist on disk.

The cryptsetup "Failed to deactivate" line at `21:22:41` is a shutdown-time cgroup-busy nuisance, **not** the cause.

## akmod-intel-ipu6 is NOT implicated

akmods built successfully at `19:24:29`–`19:24:34` (`akmodsbuild: Successful`). The akmod only produces `intel-ipu6-psys.ko` plus sensor i2c modules, by design — the isys side is in mainline `kernel-modules`. The akmod install touched only its own `/extra/intel-ipu6/` tree.

## Remediation

```bash
sudo dnf install kernel-modules-7.0.9-104.fc43 kernel-modules-extra-7.0.9-104.fc43
sudo dracut -f --kver 7.0.9-104.fc43.x86_64
# Reboot to 7.0.9 — radios should return
```

Once `kernel-modules` is back, 7.0.9 should boot with WiFi + BT + IPU6 (and the akmod-built psys overlay will load alongside mainline).

## Why the dnf install was partial

Almost certainly the previous dnf transaction that pulled in 7.0.9 was filtered/excluded. Worth checking:

- `/etc/dnf/dnf.conf`
- `/etc/dnf/protected.d/`
- `dnf versionlock list`
- whether the previous install line was `dnf install kernel-core kernel-devel` (missing `kernel` and `kernel-modules`)

Recommendation: **do NOT uninstall akmod-intel-ipu6 to recover radios** — it's not the culprit.
