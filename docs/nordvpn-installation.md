# NordVPN Installation Guide

This guide covers installing and configuring NordVPN on Fedora using OpenVPN and
NetworkManager. This is an **optional** playbook — it is not run by default.

## Architecture

This repo implements NordVPN connectivity via **OpenVPN + NetworkManager**, not the
official NordVPN CLI client. There is no `nordvpnd` daemon, no `nordvpn` group, and no
`nordvpn login` command. Instead:

- `openvpn`, `NetworkManager-openvpn`, and `NetworkManager-openvpn-gnome` are installed
- You download `.ovpn` server config files from NordVPN manually
- The `nord` command (a Go binary built from `src-go/cmd/nord`) manages import and connection via `nmcli`
- NordVPN **service credentials** (not your account login) are stored in
  `~/.config/nordvpn/.credentials` (mode 0600)

## Prerequisites

- Active NordVPN subscription
- Fedora desktop system
- Ansible configured with vault (see `CLAUDE/SecurityRules.md`)

## Installation

### Step 1: Configure Service Credentials

The playbook prompts for your NordVPN **service credentials** on first run. These are
separate from your NordVPN account login. To find them:

1. Log in to nordvpn.com
2. Go to Dashboard → Services → NordVPN
3. Click "Set up NordVPN manually"
4. Copy the **Service credentials** username and password

On first run the playbook appends them in plaintext to
`environment/localhost/host_vars/localhost.yml`. Encrypt them immediately afterwards:

```bash
./vault.bash replace nordvpn_username
./vault.bash replace nordvpn_password
```

`replace` encrypts each value **in place**, so there is nothing to paste by hand. Use
`replace` and not `set` — `set` appends a new variable and refuses when one already
exists, which it will, because the playbook just wrote both in plaintext.

The raw `ansible-vault encrypt_string 'value' --name 'nordvpn_username'` form still
works if you prefer it, but it puts the secret in your shell history and leaves you to
swap the `!vault |` block in yourself. See `CLAUDE/SecurityRules.md` for the full
variable-level vault workflow.

If credentials are already present in `localhost.yml` (as `nordvpn_username` /
`nordvpn_password`), the prompt is skipped.

### Step 2: Run the Playbook

```bash
./playbooks/imports/optional/common/play-nordvpn-openvpn.yml
```

This playbook:

- Installs `openvpn`, `NetworkManager-openvpn`, `NetworkManager-openvpn-gnome`
- Creates `~/.config/nordvpn/` (mode 0700) and `~/.config/nordvpn/configs/` (mode 0755)
- Creates `~/.local/share/nordvpn/` for logs
- Deploys credentials to `~/.config/nordvpn/.credentials` (mode 0600)
- Builds the `nord` command from this checkout and installs it at `~/.local/bin/nord`
  (it imports `play-golang.yml` first, for the Go toolchain)
- Adds the `openvpn` service to firewalld (if firewalld is running)

### Step 3: Download OpenVPN Configs

The playbook does not download `.ovpn` files — you must fetch them manually:

1. Go to nordvpn.com → Dashboard → Downloads → Linux
2. Choose the **OpenVPN** tab
3. Download the server configs you want (UDP recommended)
4. Move them into place:

```bash
mv ~/Downloads/*.ovpn ~/.config/nordvpn/configs/
```

## Usage: `nord` Helper

`~/.local/bin/nord` manages connections via NetworkManager. Configs are imported to
NetworkManager on first connect and named with a `nordvpn-` prefix.

### Commands

```bash
# Interactive chooser — lists available servers and prompts for selection
nord

# List available .ovpn configs in ~/.config/nordvpn/configs/
nord list

# List configs already imported into NetworkManager (the connected one marked)
nord list-active

# Connect to a server (imports .ovpn into NetworkManager if not yet imported)
nord connect uk1234.nordvpn.com.udp

# Show current connection status and public IP
nord status

# Switch to a different server: the same as connect, which brings any other NordVPN
# connection down first, once the new one is found and imported
nord switch us1234.nordvpn.com.udp

# Disconnect
nord disconnect

# Remove all nordvpn-* connections from NetworkManager (asks first; --yes does not)
nord cleanup

# Debug output
nord --debug connect <name>

# Version
nord --version
```

A config's name is its `.ovpn` file name without the extension and without a
`.nordvpn.com.udp` or `.nordvpn.com.tcp` suffix (`uk1234.nordvpn.com.udp.ovpn` is
`uk1234`); `nord list` shows the names. When the UDP and TCP files of one server are both
there, each is named in full (`uk1234.nordvpn.com.udp`) and the short name is refused as
ambiguous. The full file name without `.ovpn` always works as the argument too.

The password is never put on a command line: when `nord` imports a config it reads the
password from the credentials file and stores it in the profile through nmcli's editor, fed
on its standard input. The profile then holds both credentials, so it connects from GNOME's
network settings too. New credentials reach a profile when it is imported again
(`nord cleanup`, then `nord connect <name>`).

Connections persist in NetworkManager across reboots. Use GNOME Settings → Network to
view or remove them via the GUI.

## Security

- Credentials are stored at `~/.config/nordvpn/.credentials` with mode 0600
- The file contains two lines: service username on line 1, service password on line 2
- `nordvpn_username` and `nordvpn_password` in `localhost.yml` must be encrypted with
  `ansible-vault encrypt_string` before committing — the playbook prints a reminder if
  they were just written in plaintext
- This is a public repo — never commit plaintext credentials

## Troubleshooting

### `nord` reports missing credentials

Re-run the playbook to regenerate `~/.config/nordvpn/.credentials`:

```bash
./playbooks/imports/optional/common/play-nordvpn-openvpn.yml
```

### Connection fails

```bash
# Check NetworkManager logs for OpenVPN errors
journalctl -u NetworkManager --no-pager --since "5 minutes ago"

# Verify the credentials file exists and is non-empty
wc -l ~/.config/nordvpn/.credentials

# Remove the imported connection and re-import
nord cleanup
nord connect <name>

# Check NetworkManager-openvpn plugin is installed
rpm -q NetworkManager-openvpn NetworkManager-openvpn-gnome
```

### No `.ovpn` configs found

```bash
ls ~/.config/nordvpn/configs/
```

If empty, download configs from nordvpn.com (see Step 3 above).

### Firewall warning during playbook run

If the playbook prints "firewalld is not running — OpenVPN firewall rule was NOT
applied", start firewalld and re-run:

```bash
sudo systemctl start firewalld
./playbooks/imports/optional/common/play-nordvpn-openvpn.yml
```

## File Locations

| Path                                    | Purpose                                |
| --------------------------------------- | -------------------------------------- |
| `~/.config/nordvpn/configs/`            | Downloaded `.ovpn` server config files |
| `~/.config/nordvpn/.credentials`        | Service credentials (mode 0600)        |
| `~/.local/bin/nord`                     | Connection manager (Go binary)         |
| `~/.local/share/nordvpn/nord.log`       | Connection log (rotates at 1 MB)       |

**Source files in this repo:**

| Repo path                                                    | Deployed to           |
| ------------------------------------------------------------ | --------------------- |
| `src-go/cmd/nord`, `src-go/internal/nord` (built by the play) | `~/.local/bin/nord`   |
| `playbooks/imports/optional/common/play-nordvpn-openvpn.yml` | (runs the deployment) |
