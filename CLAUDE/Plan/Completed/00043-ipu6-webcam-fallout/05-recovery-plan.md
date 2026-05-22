# Recovery Plan

**Current state**: running 6.19.14 (working). 7.0.4 freezes once-off (probably unrelated). 7.0.9 has no radios (kernel-modules missing).

**Goal**: get all installed kernels healthy, prevent recurrence, and align the playbook with reality.

Two paths — choose one. **Path A is recommended** (less destructive, faster, addresses the actual bug).

---

## Path A — Repair (recommended)

Repair the half-installed 7.0.9 kernel by adding the missing module RPMs. Keep akmod for now (it's not actively hurting anything; mainline overrides it). Fix the playbook so the next run doesn't repeat the trap.

### A1. Add the missing module RPMs for 7.0.9

```bash
sudo dnf install \
    kernel-7.0.9-104.fc43 \
    kernel-modules-7.0.9-104.fc43 \
    kernel-modules-extra-7.0.9-104.fc43
```

Including `kernel-7.0.9` (the metapackage) ensures dnf treats this as a "proper" kernel install going forward — future upgrades won't repeat the partial-install pattern.

### A2. Regenerate initramfs for 7.0.9

```bash
sudo dracut -f --kver 7.0.9-104.fc43.x86_64
```

### A3. Verify the modules are now present

```bash
ls /lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/net/wireless/intel/iwlwifi/iwlwifi.ko.xz
ls /lib/modules/7.0.9-104.fc43.x86_64/kernel/drivers/bluetooth/btusb.ko.xz
```

Both should now exist.

### A4. Regenerate grub (defensive)

```bash
# BIOS:
sudo grub2-mkconfig -o /boot/grub2/grub.cfg
# UEFI (Fedora):
sudo grub2-mkconfig -o /boot/efi/EFI/fedora/grub.cfg
```

### A5. Reboot and test

Reboot into 7.0.9. Confirm:

- `ip link` shows the wifi interface (`wlp0s20f3` or similar)
- `bluetoothctl list` shows the controller
- `nmcli device status` shows wifi as `available` or `connected`
- The IPU6 camera still works: `wpctl status | grep -i camera`

If 7.0.9 boots clean: stay there. If problems recur: boot back to 6.19.14 or 7.0.4 and proceed to Path B.

### A6. Update the playbook (mandatory follow-up)

See `06-playbook-fix-proposal.md`. Even if A1–A5 work, the playbook will reproduce the partial-install bug on the next clean machine. Fix it before merging back.

---

## Path B — Roll back the kmod overlay entirely

If you'd rather not trust the akmod overlay at all (given the 210 depmod warnings on 7.0.9), tear it out:

```bash
sudo dnf remove akmod-intel-ipu6 \
                kmod-intel-ipu6-7.0.9-104.fc43.x86_64

# Also remove the (broken) 7.0.9 if you don't want to repair it:
sudo dnf remove kernel-core-7.0.9-104.fc43.x86_64 \
                kernel-modules-core-7.0.9-104.fc43.x86_64 \
                kernel-devel-7.0.9-104.fc43.x86_64 \
                kernel-devel-matched-7.0.9-104.fc43.x86_64

# OR repair it as in Path A1.

sudo grub2-mkconfig -o /boot/grub2/grub.cfg
```

Keep `ipu6-camera-bins`, `ipu6-camera-hal`, `gstreamer1-plugins-icamerasrc`, `v4l2-relayd`, `libcamera-tools`, `v4l-utils` — these are userspace and work with mainline IPU6.

Reboot to 7.0.4 (or 6.19.14) and verify camera still works via mainline kernel + userspace stack.

---

## Optional cosmetic cleanup (either path)

Stop the v4l2-relayd@icamerasrc.service from spinning on `SPLASHSRC` until/unless we ship a real `setup.cfg`:

```bash
sudo systemctl mask v4l2-relayd@icamerasrc.service
```

This kills the 5× start-limit crash loop and the 29× pipewire `/dev/videoN: Permission denied` spam seen on the freeze boot. The base `v4l2-relayd.service` is a stub (`ExecStart=/bin/true`) anyway — masking the template instance is harmless.

---

## Decision points

| Question                                     | If yes                                                     | If no                |
| -------------------------------------------- | ---------------------------------------------------------- | -------------------- |
| Do you want to keep kernel 7.0.9 available?  | Path A (repair).                                           | Path B (remove).     |
| Do you want the akmod overlay on Fedora 43+? | Keep as-is (Path A); it's harmless if mainline is healthy. | Path B `dnf remove`. |
| Should the playbook be fixed?                | Yes — always. See `06-playbook-fix-proposal.md`.           | —                    |
