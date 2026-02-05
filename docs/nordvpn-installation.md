# NordVPN Installation Guide

This guide covers installing and configuring NordVPN on Fedora with GNOME Shell integration.

## Overview

A single playbook provides complete NordVPN setup:

**play-nordvpn-cli.yml** - Installs the NordVPN CLI client and GNOME Shell extension

## Prerequisites

- Active NordVPN subscription
- Fedora desktop system
- GNOME Shell desktop environment

## Installation Steps

### Step 1: Install NordVPN

```bash
ansible-playbook playbooks/imports/optional/common/play-nordvpn-cli.yml
```

This will:
- Install the official NordVPN CLI client
- Add your user to the `nordvpn` group
- Enable and start the `nordvpnd` daemon
- Install the NordVPN Connect GNOME Shell extension

**IMPORTANT**: You must **reboot** after installation for group membership to take effect.

### Step 2: Authenticate and Enable Extension

After rebooting:

1. Log in to your NordVPN account:
   ```bash
   nordvpn login
   ```

2. Restart GNOME Shell to activate the extension:
   - Press `Alt+F2`
   - Type `r` and press Enter
   - The NordVPN icon will appear in your top bar

## Usage

### CLI Commands

```bash
# Connect to best server
nordvpn connect

# Connect to specific country
nordvpn connect United_States
nordvpn connect Germany

# Connect to specific city
nordvpn connect United_States New_York

# Disconnect
nordvpn disconnect

# Check status
nordvpn status

# List countries
nordvpn countries

# List cities in a country
nordvpn cities United_States

# View settings
nordvpn settings

# Enable features
nordvpn set killswitch on
nordvpn set cybersec on
nordvpn set autoconnect on

# Set protocol
nordvpn set technology nordlynx  # Recommended (WireGuard-based)
nordvpn set technology openvpn
```

### GNOME Extension Features

The extension provides:
- **Quick connect/disconnect toggle** in the system tray
- **Connection status indicator**
- **Server selection** by country
- **Settings access** for:
  - Protocol selection (UDP/TCP)
  - CyberSec toggle
  - AutoConnect toggle
  - Custom DNS configuration
  - Favorite servers

## Troubleshooting

### Permission Denied Error

If you see `Permission denied accessing /run/nordvpn/nordvpnd.sock`:

1. Verify you're in the nordvpn group: `groups $USER`
2. If not, re-run the CLI playbook
3. **Reboot** - group changes require a new login session

### Extension Not Appearing

1. Verify NordVPN CLI is installed: `which nordvpn`
2. Check you're in the nordvpn group: `groups $USER`
3. Restart GNOME Shell: `Alt+F2` → type `r` → Enter
4. Check extension status: `gnome-extensions list | grep NordVPN`
5. Enable manually: `gnome-extensions enable NordVPN_Connect@poilrouge.fr`

### Connection Issues

```bash
# Check daemon status
systemctl status nordvpnd

# Check account status
nordvpn account

# View logs
journalctl -u nordvpnd -f
```

## Technical Details

### Official Resources

- [NordVPN Linux Installation](https://support.nordvpn.com/hc/en-us/articles/20196094470929-Installing-NordVPN-on-Linux-distributions)
- [NordVPN Linux CLI](https://github.com/NordSecurity/nordvpn-linux)
- [GNOME Extension Repository](https://github.com/AlexPoilrouge/NordVPN-connect)

### What Gets Installed

**CLI Playbook:**
- NordVPN package from official source
- nordvpnd systemd service
- User added to nordvpn group

**Extension Playbook:**
- NordVPN Connect GNOME extension
- Extension installed to `~/.local/share/gnome-shell/extensions/`
- GSettings schema compiled

### Security Considerations

- The NordVPN CLI uses a local daemon (`nordvpnd`) that requires group membership
- Only users in the `nordvpn` group can control the VPN
- Authentication tokens are stored securely by the NordVPN client
- Kill switch and CyberSec features available for enhanced security

### Uninstallation

To remove NordVPN:

```bash
# Stop and disable service
sudo systemctl stop nordvpnd
sudo systemctl disable nordvpnd

# Remove package
sudo dnf remove nordvpn

# Remove extension
rm -rf ~/.local/share/gnome-shell/extensions/NordVPN_Connect@poilrouge.fr

# Restart GNOME Shell
# Alt+F2 → type 'r' → Enter
```

## Notes

- The GNOME extension is community-developed and not officially affiliated with NordVPN
- Extension requires systemd (already present on Fedora)
- NordVPN supports multiple VPN protocols (NordLynx/WireGuard recommended)
- The extension works by wrapping the official CLI commands
