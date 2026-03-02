# Plan 018: Fedora Kickstart Automated Install

> Status: In Progress
> Created: 2026-03-01
> Branch: F43
> Target: Fedora 43

## Goal

Build a fully automated Fedora installation pipeline that:

1. Boots from a GRUB network install entry (already built: `fedora-install/setup-netinstall-boot.bash`)
2. Collects user info upfront via a %pre TUI (WiFi, LUKS passphrase, username, password, email, hostname)
3. Installs Fedora 43 with LUKS2 + Btrfs (single encrypted volume, no separate /home partition)
4. Chains into the existing Ansible playbooks via a firstboot systemd service
5. Achieves zero manual intervention after the initial TUI prompts until first login

## Architecture: Three-Phase Pipeline

```
Phase 1: Kickstart              Phase 2: Firstboot             Phase 3: Interactive
(Anaconda Installer)            (systemd oneshot)               (User's First Login)

┌─────────────────────┐        ┌─────────────────────┐        ┌─────────────────────┐
│ %pre: TUI collects  │        │ Wait for network    │        │ SSH key generation   │
│   - WiFi SSID+pass  │        │ Clone repo (HTTPS)  │        │ gh auth login        │
│   - LUKS passphrase │        │ Install Galaxy reqs │        │ Vault secret entry   │
│   - username/pass   │        │ Run Ansible (core)  │        │ Optional playbooks   │
│   - email/hostname  │        │ Self-disable service│        │ Switch git to SSH    │
│   - PS1 colour      │        │                     │        │                     │
│                     │        │                     │        │                     │
│ Partitioning:       │───────>│ Playbooks that run: │───────>│ Playbooks deferred: │
│   EFI + /boot + LUKS│        │   preflight         │        │   github-cli-multi  │
│                     │        │   basic-configs*    │        │   lxc (SSH clone)   │
│ %post: minimal      │        │   systemd-tweaks    │        │   toolbox (GUI)     │
│   - Fix DNS         │        │   nvm-install       │        │                     │
│   - Write config    │        │   git-configure     │        │ Interactive tasks:  │
│   - Create firstboot│        │   git-hooks         │        │   Vault encryption  │
│   - Pre-set PS1     │        │   ms-fonts          │        │   GitHub OAuth      │
│   - Auto-gen vault  │        │   rpm-fusion        │        │   run.bash (SSH)    │
└─────────────────────┘        │   docker            │        └─────────────────────┘
                               │   podman            │
                               │   python            │
                               │   claude-code       │
                               │   claude-yolo       │
                               └─────────────────────┘
```

## Research Documents

Detailed research supporting this plan:

- [kickstart-research.md](./kickstart-research.md) — Kickstart syntax, %pre TUI, LUKS+Btrfs partitioning, serving methods, package groups
- [post-install-research.md](./post-install-research.md) — %post chroot vs nochroot, firstboot service pattern, vault handling, dconf, Flatpak/RPM Fusion
- [codebase-analysis.md](./codebase-analysis.md) — Full analysis of run.bash, all playbooks, variables, files/ structure, dependency graph

## File Structure

All files in `fedora-install/`:

```
fedora-install/
├── setup-netinstall-boot.bash    # Creates ISO partition, downloads netinstall ISO, configures GRUB
├── ks.cfg                        # Main kickstart file (TUI %pre, partitioning, firstboot service)
└── build-iso.bash                # FUTURE — Embed ks.cfg into Fedora ISO via mkksiso for USB boot
```

## Implementation Steps

### Step 1: Create `fedora-install/ks.cfg`

The main kickstart file with these sections:

#### %pre — Interactive TUI

Switch to tty6, collect all user input via plain `read` commands (dialog/whiptail not available in Anaconda):

| Input | Method | Notes |
|-------|--------|-------|
| WiFi SSID | `read` | Prompted first — needed for network install |
| WiFi password | `read -s` | Connects via `nmcli device wifi connect` |
| Username | `read` | Validates non-empty |
| Full name | `read` | For git config + GECOS |
| Email | `read` | For git config |
| Hostname | `read` | Default: `fedora-desktop` |
| LUKS passphrase | `read -s` with confirmation | Min length check |
| User password | `read -s` with confirmation | Hashed via python crypt |
| PS1 colour | `read` with list of options | Default: `lightblueBold` |

**WiFi connection flow** (first thing in %pre, before other prompts):

1. Prompt for SSID and password
2. Run `nmcli device wifi connect "$SSID" password "$PASS"`
3. Wait up to 15 seconds, verify with `nmcli -t -f STATE general` or ping
4. **On failure**: Show error, loop back to prompt — user can re-enter credentials or try a different network
5. **Only proceed** to remaining prompts once WiFi is confirmed connected

This ensures the network is available for the rest of the install (package downloads, inst.repo).

Generates three temp files:
- `/tmp/part-include` — Partitioning directives with LUKS passphrase
- `/tmp/user-include` — User creation with hashed password
- `/tmp/install-vars` — All variables for %post/firstboot (includes WiFi SSID for %post nmcli config)

#### Command Section

```
lang en_GB.UTF-8
keyboard --xlayouts='gb'
timezone Europe/London --utc
rootpw --lock
selinux --enforcing
firewall --enabled --service=ssh
network --bootproto=dhcp --device=link --activate
bootloader --location=mbr --append="rhgb quiet"
%include /tmp/part-include
%include /tmp/user-include
reboot --eject
```

**Note**: `lang`, `keyboard`, `timezone` should be parameterised in the TUI or set to sensible defaults. For now hardcode to GB/London (user's locale).

#### Partitioning (generated by %pre into `/tmp/part-include`)

```
clearpart --all --initlabel --disklabel=gpt
zerombr
part /boot/efi --fstype=efi --size=600
part /boot --fstype=ext4 --size=1024
part btrfs.01 --fstype=btrfs --size=1 --grow --encrypted --luks-version=luks2 --passphrase=${LUKSPASS}
btrfs none --label=fedora btrfs.01
btrfs / --subvol --name=root LABEL=fedora
btrfs /home --subvol --name=home LABEL=fedora
```

Key decisions:
- **Separate unencrypted /boot** — Avoids GRUB/LUKS2/Argon2id incompatibility. LUKS passphrase prompt happens via Plymouth/initramfs (better UX).
- **Single Btrfs volume** with root + home subvolumes — User requested no separate /home partition.
- **No /var subvolume** — Keeps it simple. Can add later if needed.
- **600MB EFI + 1GB /boot** — Matches current disk layout.

#### %packages

```
%packages --excludeWeakdeps
@^workstation-product-environment
vim-enhanced
git
gh
ansible-core
ansible-collection-community-general
ansible-collection-ansible-posix
python3-libdnf5
python3-pip
htop
wget
bash-completion
ripgrep
jq
openssl
grubby
curl
pipx
-gnome-boxes
-gnome-tour
%end
```

Key decisions:
- Install `ansible-core` + collections via dnf RPMs (avoids Galaxy network issues in %post)
- Install `pipx` for later Ansible full install in firstboot
- Include `gh` (GitHub CLI) directly in packages
- Exclude `gnome-boxes` and `gnome-tour` (noise)

#### %post --nochroot — Bridge installer to installed system

```bash
# Copy DNS resolution
cp /etc/resolv.conf /mnt/sysimage/etc/resolv.conf

# Copy install variables to installed system
cp /tmp/install-vars /mnt/sysimage/var/lib/fedora-desktop-install/install-vars
```

#### %post (chroot) — Minimal system prep

What this section does:
1. Read install-vars
2. Set hostname via `hostnamectl`
3. Configure passwordless sudo for the user
4. Write `/var/local/ps1-prompt-colour` (pre-set to avoid interactive prompt in Ansible)
5. Generate vault password: `openssl rand -base64 32`
6. Write minimal `localhost.yml` (unencrypted values only)
7. Enable DNF parallel downloads
8. Configure WiFi as a persistent NetworkManager connection (`nmcli connection add` with autoconnect)
9. Deploy the firstboot script and systemd service
10. Enable the firstboot service

What this section does NOT do:
- Run Ansible playbooks (chroot limitations)
- Start services (no systemd PID 1)
- Clone git repos (defer to firstboot for reliability)
- Handle SSH keys or GitHub auth
- Install Flatpak apps (D-Bus issues)

### Step 2: Create `fedora-install/firstboot-setup.bash`

Deployed to `/usr/local/bin/fedora-desktop-firstboot.bash` by %post.

This runs as a systemd oneshot service on first boot with full systemd, networking, and D-Bus:

1. **Wait for network** — Poll `curl -sf https://github.com` with timeout
2. **Clone repo** — `git clone -b F43 https://github.com/LongTermSupport/fedora-desktop.git` as the user into `~/Projects/fedora-desktop`
3. **Copy config into repo** — Move vault-pass.secret and localhost.yml into the cloned repo
4. **Install Galaxy requirements** — `ansible-galaxy install -r requirements.yml`
5. **Upgrade Ansible** — `pipx install --include-deps ansible && pipx inject ansible jmespath passlib ansible-lint` (matches run.bash step 3)
6. **Run main playbook** — `ansible-playbook playbooks/playbook-main.yml --connection=local`
7. **Handle failures gracefully** — Log everything, don't halt on non-critical failures
8. **Self-disable** — Touch marker file, disable service

The systemd service unit:

```ini
[Unit]
Description=Fedora Desktop First Boot Configuration
After=network-online.target
Wants=network-online.target
Before=display-manager.service
ConditionPathExists=!/var/lib/fedora-desktop-setup-complete

[Service]
Type=oneshot
RemainAfterExit=no
ExecStart=/usr/local/bin/fedora-desktop-firstboot.bash
ExecStartPost=/usr/bin/touch /var/lib/fedora-desktop-setup-complete
ExecStartPost=/usr/bin/systemctl disable fedora-desktop-setup.service
TimeoutStartSec=3600
StandardOutput=journal+console
StandardError=journal+console

[Install]
WantedBy=multi-user.target
```

### Step 3: Create `fedora-install/build-iso.bash`

Script to embed ks.cfg into a Fedora ISO using `mkksiso` (from the `lorax` package):

```bash
sudo mkksiso --ks fedora-install/ks.cfg <input-iso> <output-iso>
```

This creates a self-contained bootable ISO where kickstart is automatically applied.

### Step 4: Rewrite `fedora-install/setup-netinstall-boot.bash` (ISO partition approach)

**Problem**: The original PXE approach downloaded vmlinuz + initrd from the netinstall mirror and created a WiFi initrd overlay. This failed because the PXE initrd lacks Intel iwlwifi drivers — those live in install.img (stage2, ~820MB), which itself must be downloaded over WiFi. Chicken-and-egg.

**Solution**: Create a 2GB ext4 partition (FDINST) by shrinking the LUKS container from the end, download the full Fedora netinstall ISO onto it, and boot via GRUB with `inst.stage2=hd:LABEL=FDINST:/fedora-install.iso`. Anaconda loads stage2 from local disk (no network needed during dracut). WiFi connects in %pre for package downloads.

**Setup flow**:
1. Preflight checks (root, commands, GRUB, LUKS exists, Btrfs >= 5GB free)
2. Read version from `vars/fedora-version.yml`, construct ISO URL, verify with HTTP HEAD
3. Detect disk layout: root dm-crypt → LUKS backing partition → parent disk
4. Find or create FDINST partition (shrink Btrfs→LUKS→partition, create p4, mkfs.ext4)
5. Download ISO with `curl -z` freshness check, verify >= 100MB
6. Loop-mount ISO, extract vmlinuz + initrd.img to `/boot/fedora-netinstall/`
7. Copy ks.cfg with version substitution
8. Create GRUB entry: `inst.stage2=hd:LABEL=FDINST:/fedora-install.iso inst.ks=hd:UUID=<boot>:/fedora-netinstall/ks.cfg`
9. Disable GRUB auto-hide, regenerate GRUB config, unmount FDINST

**Remove flow** (reverse): rm boot files → rm GRUB entry → parted rm p4 → grow p3 100% → cryptsetup resize → btrfs resize max → regenerate GRUB

**Partition operations** (shrink from END — safe, no data movement):
- Create: btrfs resize -2g → cryptsetup resize --size → parted resizepart → parted mkpart → mkfs.ext4
- Remove: parted rm → parted resizepart 100% → cryptsetup resize (no --size = fill) → btrfs resize max
- Each step has rollback on failure

**File layout after setup**:
```
/boot/fedora-netinstall/        (on /boot, ext4)
├── vmlinuz      (17MB)
├── initrd.img   (252MB)
└── ks.cfg       (20KB)

/mnt/fedora-install/            (FDINST partition, 2GB ext4)
└── fedora-install.iso  (~800MB)
```

### Step 5: Handle Ansible Playbook Compatibility

Some playbooks in the main chain need modifications or guards for automated context:

| Playbook | Issue | Solution |
|----------|-------|----------|
| `play-basic-configs.yml` | PS1 colour prompt is interactive | Pre-create `/var/local/ps1-prompt-colour` in %post — **already handled** by the existing conditional check |
| `play-basic-configs.yml` | SSH key copy to root | Add `when: ssh_key_stat.stat.exists` guard (keys may not exist yet) |
| `play-basic-configs.yml` | fwupdmgr may timeout | Add `ignore_errors: true` or conditional |
| `play-github-cli-multi.yml` | Requires interactive browser auth | Add a skip condition (e.g., check if `gh auth status` succeeds, skip if not) |
| `play-lxc-install-config.yml` | Clones via SSH (needs GitHub auth) | Change to HTTPS clone, or add fallback |
| `play-toolbox-install.yml` | Launches GUI (JetBrains Toolbox) | Add headless detection guard |

**Approach**: Add conditional guards to existing playbooks rather than creating separate "headless" versions. Each guard checks whether the required precondition exists (e.g., SSH keys, gh auth, display server).

### Step 6: Validate and Test

1. **Syntax validation**: `ksvalidator fedora-install/ks.cfg` (from `pykickstart` package)
2. **VM test**: Test in virt-manager/QEMU with a Fedora 43 netinstall ISO
3. **Iterate**: Fix issues found in VM testing
4. **Real hardware**: Test on actual target machine

## Key Design Decisions

### Why firstboot service instead of running Ansible in %post?

- systemctl commands fail in chroot (no running systemd PID 1)
- No D-Bus session (dconf/gsettings won't work)
- No user sessions (user-level systemd services can't start)
- Container engines not running (can't build Claude YOLO image)
- Better error handling — system is bootable even if firstboot fails, can retry

### Why HTTPS clone instead of SSH?

- SSH keys don't exist yet at install time
- Public repo — HTTPS works without authentication
- Phase 3 (interactive) switches remote to SSH after GitHub auth

### Why install Ansible collections via dnf RPMs?

- Avoids Galaxy network dependency in %post (DNS can be unreliable)
- Faster than downloading from Galaxy
- Available in Fedora repos: `ansible-collection-community-general`, `ansible-collection-ansible-posix`

### Why auto-generate vault password?

- Core playbooks don't need vault-encrypted data (lastfm API keys are optional)
- User can add vault-encrypted secrets later in Phase 3
- Avoids needing to pass vault password through the install pipeline

### WiFi via nmcli in %pre

- Kickstart's built-in WiFi options (`--essid`, `--wpakey`) are deprecated/unreliable
- Instead, we connect WiFi programmatically via `nmcli` in the %pre script
- NetworkManager is running in the Anaconda environment, so `nmcli device wifi connect` works
- Credentials are collected interactively in the TUI with a retry loop on failure
- WiFi SSID is saved to install-vars so %post can configure it as an autoconnect profile via `nmcli connection add` for post-install use

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| DNS fails in %post | Can't install extra packages | Copy resolv.conf in --nochroot phase |
| Network unavailable at firstboot | Clone fails | Retry loop with 120s timeout, fail gracefully |
| Ansible playbook fails partway | Incomplete config | Log everything, system still boots, user can re-run manually |
| Kickstart syntax errors | Install fails | Validate with `ksvalidator` before deploying |
| LUKS2/GRUB incompatibility | Can't boot | Separate unencrypted /boot avoids this entirely |
| Fedora version mismatch | Preflight fails | ks.cfg version tag matches branch, fedora-version.yml already set to 43 |
| Missing packages in Fedora repos | %packages fails | Use `--ignoremissing` or verify all packages exist first |

## Out of Scope (Future Work)

- Multiple kickstart profiles (e.g., minimal vs full)
- Private repo support (PAT/deploy key)
- Multi-disk / RAID configurations
- USB ISO embedding via mkksiso (build-iso.bash)

## Notes & Updates

### 2026-03-02 — v3: liveimg approach
- **Problem with v2**: netinstall ISO + `url` + `%packages` downloads ~2GB packages over WiFi each reinstall
- **v3 solution**: Use `liveimg` kickstart directive to deploy the Workstation Live filesystem from a local squashfs.img
- `setup-netinstall-boot.bash` now downloads **two ISOs**: netinstall (~1.1GB) and Workstation Live (~2.2GB)
- Workstation Live ISO is temporary — squashfs.img is extracted, ISO deleted to save space
- FDINST partition holds: `netinstall.iso` (~1.1GB) + `squashfs.img` (~1.8GB) = ~2.9GB total (fits in 4GB)
- Anaconda boots from netinstall ISO (`inst.stage2`), deploys Workstation filesystem via `liveimg --url=file:///mnt/fdinst/squashfs.img`
- Base install is fully offline; WiFi only needed for `dnf install` in `%post` (extra packages) and firstboot Ansible
- `%packages` section removed entirely (liveimg ignores it); replaced with `dnf install` in `%post --chroot`
- FDINST partition mounted read-only in `%pre` so liveimg can access squashfs.img

### 2026-03-02 — Installer UX: keyboard layout + font size
- **Keyboard layout**: GRUB entry passes `vconsole.keymap=<detected>` from host's `localectl`/`/etc/vconsole.conf` so `%pre` TUI gets correct keymap (e.g. `gb` not `us` — @ is in the wrong place on UK keyboards)
- **Font size**: GRUB entry passes `vconsole.font=latarcyrheb-sun32` so installer console uses 32px font instead of the tiny default
- **Installed system keyboard**: `setup-netinstall-boot.bash` sed-substitutes detected X11 layout into the copied `ks.cfg` `keyboard --xlayouts=` directive
- `ks.cfg` `%pre` calls `setfont latarcyrheb-sun32 2>/dev/null || true` after switching to tty6 as belt-and-suspenders fallback

### 2026-03-01
- Rewrote `setup-netinstall-boot.bash` from PXE download to ISO partition approach
- PXE initrd lacks iwlwifi drivers; netinstall ISO's stage2 (install.img) has full driver support
- ISO partition approach eliminates chicken-and-egg problem: stage2 loads from disk, WiFi connects later in %pre
- Updated ks.cfg comments (3 lines) to reference ISO approach instead of initrd overlay
