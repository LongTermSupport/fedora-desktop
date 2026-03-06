# Fedora Installation

This directory contains tools for a fully automated Fedora desktop install
using a kickstart file — no USB key or manual package selection required.
The installer runs from a GRUB boot entry on the existing system, using
ISOs downloaded to a local partition.

---

## How it works

```
┌──────────────────────────────────────────────────────────────────┐
│  Existing Fedora system (current install)                        │
│                                                                  │
│  1. setup-netinstall-boot.bash                                   │
│     ├── Creates 4 GiB FDINST partition (shrinks LUKS from end)  │
│     ├── Downloads netinstall ISO → FDINST                        │
│     ├── Downloads Workstation Live ISO → extracts squashfs.img   │
│     ├── Copies vmlinuz + initrd.img → /boot/fedora-netinstall/  │
│     ├── Copies ks.cfg → /boot/fedora-netinstall/                │
│     └── Creates GRUB menu entry "Fedora XX Install (ISO)"        │
│                                                                  │
│  2. Reboot → select install entry from GRUB                      │
└──────────────────────────────────────────────────────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────────────┐
│  Anaconda installer (boots from FDINST partition)                │
│                                                                  │
│  3. ks.cfg %pre — interactive TUI on tty6                        │
│     ├── WiFi: auto-detects or prompts for SSID/password          │
│     ├── Prompts: username, full name, email, hostname            │
│     ├── Prompts: LUKS disk encryption passphrase                 │
│     ├── Prompts: user password                                   │
│     └── Prompts: shell prompt colour                             │
│                                                                  │
│  4. Anaconda installs Fedora                                     │
│     ├── Partitioning: EFI + /boot + LUKS2 Btrfs (auto)          │
│     ├── Filesystem: Workstation Live image via liveimg           │
│     └── Base install: fully offline (no network needed)          │
│                                                                  │
│  5. ks.cfg %post (chroot) — system configuration                 │
│     ├── Configures passwordless sudo (removed after first boot)  │
│     ├── Pre-clones fedora-desktop repo via HTTPS                 │
│     ├── Generates vault password                                 │
│     ├── Writes minimal localhost.yml (login, name, email)        │
│     ├── Persists WiFi credentials as NetworkManager connection   │
│     └── Installs GNOME autostart entry for setup wizard          │
│                                                                  │
│  6. System reboots into new Fedora install                       │
└──────────────────────────────────────────────────────────────────┘
           │
           ▼
┌──────────────────────────────────────────────────────────────────┐
│  First login — GNOME autostart launches setup wizard             │
│                                                                  │
│  7. run.bash (in a terminal, automatically)                      │
│     ├── Installs Ansible via pipx                                │
│     ├── Creates SSH key (~/.ssh/id)                              │
│     ├── Authenticates with GitHub CLI (gh auth login)            │
│     ├── Adds SSH key to GitHub account                           │
│     ├── Pulls localhost.yml from private config repo             │
│     │   (github.com/<username>/fedora-desktop-config)           │
│     │   OR prompts manually if no config repo exists             │
│     ├── Runs main Ansible playbook                               │
│     └── Runs GitHub multi-account setup playbook                 │
│                                                                  │
│  8. System reboots — fully configured                            │
└──────────────────────────────────────────────────────────────────┘
```

---

## Files

| File | Purpose |
|------|---------|
| `setup-netinstall-boot.bash` | Prepares existing system for install (partition, ISOs, GRUB) |
| `ks.cfg` | Kickstart config — drives the Anaconda installer |
| `build-iso.bash` | Alternative: embed kickstart into an ISO for USB install |

---

## Prerequisites

- An existing Fedora system with a LUKS-encrypted root partition
- At least 7 GiB free on the Btrfs root filesystem (for the 4 GiB FDINST partition)
- At least 300 MB free on `/boot`
- Internet access (for downloading ISOs)
- `sudo` access

---

## Step 1 — Prepare the install boot entry

```bash
# From the fedora-desktop repo root:
sudo bash ./fedora-install/setup-netinstall-boot.bash
```

This will:
1. Create a 4 GiB `FDINST` ext4 partition by shrinking your LUKS container
   (you will be prompted for your LUKS passphrase)
2. Download the Fedora netinstall ISO to FDINST
3. Download the Fedora Workstation Live ISO, extract `squashfs.img` to FDINST,
   then delete the ISO (only squashfs.img is kept)
4. Copy `vmlinuz` and `initrd.img` to `/boot/fedora-netinstall/`
5. Copy `ks.cfg` to `/boot/fedora-netinstall/`
6. Add a GRUB menu entry: **"Fedora XX Install (ISO)"**

If you need to re-run after a failed setup, use `--clean` to reformat FDINST
and start fresh:

```bash
sudo bash ./fedora-install/setup-netinstall-boot.bash --clean
sudo bash ./fedora-install/setup-netinstall-boot.bash
```

To remove the install entry and reclaim disk space afterwards:

```bash
sudo bash ./fedora-install/setup-netinstall-boot.bash --remove
```

---

## Step 2 — Reboot and install

1. Reboot your system
2. At the GRUB menu, select **"Fedora XX Install (ISO)"**
3. The installer switches to **tty6** for interactive input (tty1 shows Anaconda progress)
4. Answer the prompts:
   - **WiFi**: SSID and password (auto-detected if already connected)
   - **Username**: system login name (lowercase, no spaces)
   - **Full name**: display name
   - **Email**: used for git configuration
   - **Hostname**: machine name (default: `fedora-desktop`)
   - **LUKS passphrase**: disk encryption passphrase (min 8 chars, no `"`, `\`, or `` ` ``)
   - **User password**: login password
   - **Shell prompt colour**: one of the listed options
5. Confirm and the installation proceeds automatically
6. System reboots into the new install

> **Keyboard layout**: The kickstart auto-detects your current host keyboard layout
> (VC keymap and X11 layout) and applies it to the new install.

---

## Step 3 — First login

On first GNOME login, a terminal opens automatically running `run.bash`.

The wizard:
1. Installs Ansible via pipx
2. Generates an SSH key (`~/.ssh/id`)
3. Authenticates with GitHub (`gh auth login` — choose SSH method when prompted)
4. Adds the SSH key to your GitHub account
5. **Pulls `localhost.yml`** from your private config repo
   (`github.com/<username>/fedora-desktop-config`) if it exists — skipping
   manual prompts entirely
6. Falls back to manual prompts if no config repo is found
7. Runs the main Ansible playbook (full system configuration)
8. Runs the GitHub multi-account setup playbook

> The setup wizard only runs once. After completion, a sentinel file is written
> at `~/.local/state/fedora-desktop-setup-complete` which prevents re-runs.

---

## Personal config repo

The setup wizard can restore your full `localhost.yml` automatically on a fresh
install if you keep a private backup at:

```
github.com/<your-primary-github-username>/fedora-desktop-config
```

### Backing up your config

After your first install (once Ansible vault secrets are set up):

```bash
cd ~/Projects/fedora-desktop
./fedora-install/push.bash config
```

This will:
- Select your GitHub account interactively (if multiple)
- Create the `fedora-desktop-config` private repo if it doesn't exist
- Validate that all sensitive values are Ansible Vault-encrypted
- Push `localhost.yml` via the GitHub API
- **Verify** the remote content matches your local file

### What's in localhost.yml

```yaml
user_login: "yourusername"
user_name: "Your Name"
user_email: "you@example.com"

# GitHub CLI accounts
github_accounts:
  personal: "yourusername"
  work: "yourusername-work"

# Encrypted secrets (ansible-vault encrypt_string)
lastfm_api_key: !vault |
  $ANSIBLE_VAULT;1.2;AES256;localhost
  ...
```

> **Vault password is NOT stored in the config repo.** Keep it in your
> password manager (Bitwarden, 1Password, etc.). You will be prompted for
> it during setup.

---

## Moving to a new machine or reinstalling Fedora

Both scenarios follow the same pattern: **push everything before**, do the
install, then **pull everything after**. The main difference is whether
you're keeping the same hardware or starting fresh.

### Before you wipe / before you move

Run these two commands on your **current machine** to back up everything
to GitHub:

```bash
cd ~/Projects/fedora-desktop

# Back up both config and projects in one command (recommended)
./fedora-install/push.bash

# Or individually:
./fedora-install/push.bash config    # localhost.yml only
./fedora-install/push.bash projects  # projects manifest only
```

`push.bash config` saves your `localhost.yml` (usernames, email, GitHub
accounts, encrypted API keys) to a private repo on GitHub.

`push.bash projects` scans `~/Projects`, records every git repo's remote URL
and the SSH key it needs, and saves that manifest to the same private repo.
It will prompt you to choose the right key if an org is accessible by more
than one of your GitHub accounts.

> Run both, even if you already have a recent backup — it takes under a
> minute and ensures nothing recent is missed.

---

### Scenario A — Moving to a new machine

1. Push config and projects (above)
2. Set up the new machine using the normal install process (Steps 1–3 above)
3. When `run.bash` finishes the main Ansible setup, it will offer to restore
   your projects:

   ```
   Would you like to restore projects from your config repo manifest? [Y/n]:
   ```

   Say **Y** — it clones all your repos into `~/Projects` using the correct
   SSH key for each one.

That's it. Your new machine comes up with your full config, dotfiles, and
all your repos in the right places.

---

### Scenario B — Reinstalling Fedora on the same machine

1. Push config and projects (above)
2. Run `setup-netinstall-boot.bash` to prepare the install entry (Step 1)
3. Reboot and install (Step 2)
4. On first login, `run.bash` runs automatically and restores everything —
   same as Scenario A above

Because `run.bash` pulls `localhost.yml` from your config repo, you won't
need to re-enter your email, GitHub accounts, or API keys. The vault
password is the one exception — keep that in your password manager (it's
never stored in the config repo).

---

### After the restore

Once `run.bash` completes and projects are restored, you may want to run
any optional playbooks you had set up previously:

```bash
cd ~/Projects/fedora-desktop

# Example: restore speech-to-text
ansible-playbook ./playbooks/imports/optional/common/play-speech-to-text.yml

# Or run the optional playbooks menu
./run.bash --optional-only
```

---

## Alternative: USB install via custom ISO

If you prefer a USB key install, you can embed the kickstart directly into
a Fedora ISO:

```bash
sudo bash ./fedora-install/build-iso.bash ~/Downloads/Fedora-Everything-netinst-x86_64-43-1.1.iso
# Output: ~/Downloads/fedora-desktop-custom.iso

# Flash to USB:
sudo dd if=~/Downloads/fedora-desktop-custom.iso of=/dev/sdX bs=4M status=progress oflag=sync
```

This requires the `lorax` package (`sudo dnf install lorax`).

Note: The USB method does not have access to the local squashfs.img, so the
installer downloads packages over the network instead of deploying the Live
filesystem. This is slower but works without an existing Fedora system.

---

## Troubleshooting

### GRUB menu not showing

Run: `sudo grub2-editenv - unset menu_auto_hide && sudo grub2-mkconfig -o /boot/grub2/grub.cfg`

### Installer hangs at boot

Check that `vmlinuz` and `initrd.img` are present in `/boot/fedora-netinstall/`:
```bash
ls -lh /boot/fedora-netinstall/
```

### WiFi not connecting in installer

The `%pre` TUI will show an error and let you retry. Common causes:
- Passphrase containing backslashes (not supported by NetworkManager config)
- Wrong SSID (check capitalisation)
- 5 GHz networks not supported on some older adapters

### squashfs.img / erofs detection

Fedora switched Live images from SquashFS to EROFS in newer releases. The
setup script accepts both formats — verification checks for either magic.

### FDINST partition lost after interrupted setup

If the partition table shows the FDINST partition but `blkid -L FDINST` returns
nothing (label lost), run `--clean` to reformat:

```bash
sudo bash ./fedora-install/setup-netinstall-boot.bash --clean
```

### localhost.yml not restored from config repo

Ensure your primary GitHub account (`gh api user --jq '.login'`) matches the
owner of the `fedora-desktop-config` repo. If you have multiple GitHub accounts,
check which one is active:

```bash
gh auth status
```
