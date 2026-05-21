# Sub-Agent 3 Report — akmod Build State & dnf-history Audit

**Scope**: Inventory what akmod-intel-ipu6 actually built, for which kernels, and trace how kernel 7.0.9 ended up half-installed.

## Build state per kernel

| Kernel        | akmod result                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **7.0.9-104** | **PARTIAL / BROKEN** — RPM built and installed, but ships only `psys` + i2c sensor stubs. **No `intel-ipu6-isys.ko`** in /extra/. depmod emitted **210 WARNING lines** about the /extra/intel-ipu6/ tree (every line truncated identically to 80 chars — per-symbol unresolved-reference warnings against the mainline `intel-ipu6` it `depends:` on. modinfo confirms `depends: intel-ipu6`.) |
| **7.0.4-100** | **BUILD NEVER ATTEMPTED** — at the boot into 7.0.4 (21:45:37 BST), akmods reported `Files needed for building modules against kernel 7.0.4-100.fc43.x86_64 could not be found ... /usr/src/kernels/7.0.4-100.fc43.x86_64/ ... missing. Is the correct kernel-devel package installed? [FAILED]`. Only `kernel-devel-7.0.9-104` is installed.                                                   |
| **6.19.14**   | **BUILD NEVER ATTEMPTED** — same failure: missing `/usr/src/kernels/6.19.14-200.fc43.x86_64/` (21:48:56 BST).                                                                                                                                                                                                                                                                                  |

## Was 7.0.9 pulled by the playbook?

**Yes.** dnf-history transaction 81 (2026-05-21 18:21:30, the playbook run) explicitly installed:

- `kernel-core-7.0.9-104.fc43`
- `kernel-modules-core-7.0.9-104.fc43`
- `kernel-devel-7.0.9-104.fc43`
- `kernel-devel-matched-7.0.9-104.fc43`

**but did NOT install `kernel-7.0.9` or `kernel-modules-7.0.9` or `kernel-modules-extra-7.0.9`.**

The trigger was `kernel-devel-matched` — pulled by `akmod-intel-ipu6` via the `akmods` package which `Requires: kernel-devel-matched`. `kernel-devel-matched` always pulls the latest available `kernel-devel`, which in turn pulls the matching `kernel-core` + `kernel-modules-core`.

The same transaction removed `kernel-6.19.13` (replaced by what should have been a full 7.0.9 but wasn't).

Txn 82/83 then installed the freshly built `kmod-intel-ipu6-7.0.9-104` and `kmod-v4l2loopback-7.0.9-104` RPMs from `/tmp/akmods.*/results/`.

## Modprobe blacklist conflict?

**No.** `grep -r ipu6 /etc/modprobe.d/` returned empty. Only generic non-ipu6 blacklists (appletalk, ax25, etc.). The kmod-intel-ipu6 RPM also ships no modprobe.d entry.

The out-of-tree `intel_ipu6_psys` declares `depends: intel-ipu6` and binds via `alias: auxiliary:intel_ipu6.psys` — designed to load *alongside* the in-tree `intel-ipu6` core, not replace it. So strictly, no conflict at the modprobe layer.

But the **210 depmod WARNINGs** strongly suggest symbol-version mismatches against the mainline `intel-ipu6` it depends on. On a kernel where mainline already provides isys+psys+core in-tree, the akmod's redundant psys on /extra/ adds no value and risks misbehaviour.

## Remediation recommendation

The RPM-Fusion akmod is the **wrong tool** for kernel ≥ 7.0 — mainline already has isys+psys+core in-tree, and the akmod's partial psys layered on top breaks depmod and adds no value. Recommended HOST sequence:

```bash
# 1. Stop the akmod / kmod from being re-pulled
sudo dnf remove akmod-intel-ipu6 \
                kmod-intel-ipu6-7.0.9-104.fc43.x86_64 \
                akmods \
                kernel-devel \
                kernel-devel-matched \
                kernel-srpm-macros

# 2. Decide on 7.0.9: either repair it or remove it
#    Repair (preferred — radios back, IPU6 still works via mainline):
sudo dnf install kernel-modules-7.0.9-104.fc43 kernel-modules-extra-7.0.9-104.fc43
sudo dracut -f --kver 7.0.9-104.fc43.x86_64
#    OR remove if you'd rather pin to 7.0.4:
# sudo dnf remove kernel-core-7.0.9-104.fc43.x86_64 kernel-modules-core-7.0.9-104.fc43.x86_64

# 3. Keep ipu6-camera-bins, ipu6-camera-hal, gstreamer1-plugins-icamerasrc,
#    v4l2-relayd — userspace, not the problem.

# 4. Regenerate grub
sudo grub2-mkconfig -o /boot/grub2/grub.cfg
#    (or /boot/efi/EFI/fedora/grub.cfg on UEFI)

# 5. Update Ansible play to drop akmod-intel-ipu6 on F43+.
```

## Key files for follow-up

- `/var/cache/akmods/intel-ipu6/.last.log` (54 KB, 599 lines — same as `0.0-24...-for-7.0.9-104.fc43.x86_64.log`)
- `/var/cache/akmods/intel-ipu6/kmod-intel-ipu6-7.0.9-104.fc43.x86_64-0.0-24.20250909git4bb5b4d.fc43.x86_64.rpm`
- `/lib/modules/7.0.9-104.fc43.x86_64/extra/intel-ipu6/` (out-of-tree tree)
- `playbooks/imports/optional/hardware-specific/play-ipu6-webcam.yml` (the play to update)
