# Plan 002: NordVPN OpenVPN Manager

## Overview

Create a bash script (`nord`) and Ansible playbook to manage NordVPN OpenVPN connections via NetworkManager CLI. User downloads .ovpn files, script handles import/connect/disconnect/switch operations.

**Key Design Decisions:**
- On-demand import (import configs when connecting, not all upfront)
- Persistent NetworkManager connections (visible in GNOME Settings)
- Two-tier credentials: Ansible Vault (git) + runtime file (mode 0600)
- No systemd services (use NetworkManager directly)
- Full GNOME NetworkManager GUI integration

---

## Files to Create

### 1. Main Script: `files/home/.local/bin/nord`

**Purpose:** Bash script to manage NordVPN OpenVPN connections

**Key Components:**

```bash
#!/usr/bin/bash
# NordVPN OpenVPN Connection Manager
# Version: 1.0.0

set -e  # Fail fast

# Configuration
CONFIG_DIR="$HOME/.config/nordvpn"
OVPN_DIR="$CONFIG_DIR/configs"
CREDENTIALS_FILE="$CONFIG_DIR/.credentials"
STATE_FILE="$CONFIG_DIR/.current-connection"
LOG_DIR="$HOME/.local/share/nordvpn"
LOG_FILE="$LOG_DIR/nord.log"
MAX_LOG_SIZE=1048576  # 1MB

# NetworkManager connection prefix
NM_PREFIX="nordvpn-"
```

**Core Functions to Implement:**

1. **Logging Functions** (following `wsi` patterns from `/workspace/files/home/.local/bin/wsi:74-152`)
   - `ensure_log_dir()` - Create log dir, rotate if > 1MB
   - `log_to_file()` - Write to log with ISO 8601 timestamp
   - `log()` - Info-level (stderr + file)
   - `log_debug()` - Debug-level (when DEBUG=true)
   - `log_error()` - Error-level with cleanup

2. **Cleanup & Signal Handling** (following `wsi` patterns)
   - `cleanup()` - Remove temp files, update state
   - `trap cleanup EXIT` - Always run cleanup
   - `trap 'cleanup; exit 130' TERM INT` - Handle signals

3. **Dependency Checking** (following `wsi` patterns from `wsi:290-335`)
   - `check_dependencies()` - Verify nmcli, openvpn available
   - Check NetworkManager is running
   - Check credentials file exists (if connections exist)

4. **Core Operations**
   - `list_configs()` - List available .ovpn files in configs/ directory
   - `list_connections()` - List imported NM connections (prefix: nordvpn-)
   - `import_config()` - Import .ovpn to NetworkManager with credentials
   - `connect_vpn()` - Connect to named VPN (imports if needed)
   - `disconnect_vpn()` - Disconnect current VPN
   - `switch_vpn()` - Disconnect current, connect new
   - `status_vpn()` - Show connection status + public IP
   - `cleanup_old()` - Remove all nordvpn-* connections from NM

5. **Command Interface** (following git-style subcommands)
   ```bash
   # Argument parsing with case/shift loop (following wsi:387-464)
   case "$1" in
       list) list_configs ;;
       list-active) list_connections ;;
       connect) connect_vpn "$2" ;;
       disconnect) disconnect_vpn ;;
       switch) switch_vpn "$2" ;;
       status) status_vpn ;;
       cleanup) cleanup_old ;;
       --help|-h) show_usage ;;
       --version|-v) echo "$NORD_VERSION" ;;
       --debug) DEBUG=true; shift; "$@" ;;
   esac
   ```

**Key nmcli Commands:**

```bash
# Import .ovpn to NetworkManager
nmcli connection import type openvpn file "$ovpn_file"

# Rename connection for consistency
nmcli connection modify "$old_name" connection.id "$NM_PREFIX$config_name"

# Set credentials
nmcli connection modify "$NM_PREFIX$config_name" \
    +vpn.data "username=$(head -1 "$CREDENTIALS_FILE")" \
    +vpn.secrets "password=$(tail -1 "$CREDENTIALS_FILE")"

# Connect/Disconnect
nmcli connection up "$NM_PREFIX$config_name"
nmcli connection down "$NM_PREFIX$config_name"

# Query active VPN connections
nmcli -t -f NAME,TYPE,STATE connection show --active | \
    grep ":vpn:activated" | grep "^$NM_PREFIX"

# Delete connection
nmcli connection delete "$NM_PREFIX$config_name"
```

**Error Handling Examples:**

```bash
# Missing config file
if [ ! -f "$OVPN_DIR/$config_name.ovpn" ]; then
    log_error "Config not found: $config_name.ovpn"
    echo "Available configs:"
    list_configs
    exit 1
fi

# Connection failure
if ! nmcli connection up "$nm_conn" 2>/dev/null; then
    log_error "Failed to connect to $config_name"
    echo "Troubleshooting:"
    echo "  1. Check credentials: cat ~/.config/nordvpn/.credentials"
    echo "  2. Check logs: journalctl -u NetworkManager -f"
    echo "  3. Re-import: nord cleanup && nord connect $config_name"
    return 1
fi
```

**Script Size:** ~400-500 lines (reference: `wsi` is 863 lines)

---

### 2. Ansible Playbook: `playbooks/imports/optional/common/play-nordvpn-openvpn.yml`

**Purpose:** Deploy nord script, install dependencies, collect credentials

**Structure:**

```yaml
---
#!/usr/bin/env ansible-playbook
- hosts: desktop
  name: NordVPN OpenVPN Manager
  become: false
  vars:
    root_dir: "{{ inventory_dir }}/../../"
  vars_files:
    - "{{ root_dir }}/environment/localhost/host_vars/localhost.yml"
  tasks:
```

**Task Breakdown:**

1. **System Dependencies**
   ```yaml
   - name: Install OpenVPN and NetworkManager Support
     become: true
     ansible.builtin.dnf:
       name:
         - openvpn
         - NetworkManager-openvpn
         - NetworkManager-openvpn-gnome  # GUI integration
       state: present
   ```

2. **Directory Creation** (following `play-speech-to-text.yml:215-274`)
   ```yaml
   - name: Create NordVPN Configuration Directory
     ansible.builtin.file:
       path: "/home/{{ user_login }}/.config/nordvpn"
       state: directory
       owner: "{{ user_login }}"
       group: "{{ user_login }}"
       mode: '0700'

   - name: Create OpenVPN Configs Directory
     ansible.builtin.file:
       path: "/home/{{ user_login }}/.config/nordvpn/configs"
       state: directory
       owner: "{{ user_login }}"
       group: "{{ user_login }}"
       mode: '0755'

   - name: Create Log Directory
     ansible.builtin.file:
       path: "/home/{{ user_login }}/.local/share/nordvpn"
       state: directory
       owner: "{{ user_login }}"
       group: "{{ user_login }}"
       mode: '0755'
   ```

3. **Credential Collection** (only if not already configured)
   ```yaml
   - name: Check if NordVPN Credentials Already Configured
     ansible.builtin.set_fact:
       nordvpn_credentials_exist: "{{ nordvpn_username is defined and nordvpn_password is defined }}"

   - block:
       - name: Prompt for NordVPN Service Username
         ansible.builtin.pause:
           prompt: |
             ========================================================================
             NORDVPN OPENVPN CREDENTIALS
             ========================================================================

             You need your NordVPN SERVICE credentials (not your account login).

             To get them:
             1. Log into nordvpn.com
             2. Go to Dashboard → Services → NordVPN
             3. Click "Set up NordVPN manually"
             4. Copy "Service credentials" (username/password)

             These credentials will be encrypted with ansible-vault.

             Enter NordVPN service username (format: aBcD1eF2gH3iJ4kL)
         register: vpn_username_prompt

       - name: Prompt for NordVPN Service Password
         ansible.builtin.pause:
           prompt: "Enter NordVPN service password"
           echo: false
         register: vpn_password_prompt

       - name: Save Credentials to localhost.yml
         ansible.builtin.lineinfile:
           path: "{{ root_dir }}/environment/localhost/host_vars/localhost.yml"
           line: "{{ item.line }}"
           create: false
         loop:
           - line: "nordvpn_username: {{ vpn_username_prompt.user_input }}"
           - line: "nordvpn_password: {{ vpn_password_prompt.user_input }}"
         delegate_to: localhost
         no_log: true

     when: not nordvpn_credentials_exist
   ```

4. **Deploy Credentials File**
   ```yaml
   - name: Create Credentials File
     ansible.builtin.copy:
       dest: "/home/{{ user_login }}/.config/nordvpn/.credentials"
       owner: "{{ user_login }}"
       group: "{{ user_login }}"
       mode: '0600'
       content: |
         {{ nordvpn_username }}
         {{ nordvpn_password }}
   ```

5. **Deploy Script**
   ```yaml
   - name: Deploy nord Script
     ansible.builtin.copy:
       src: "{{ root_dir }}/files/home/.local/bin/nord"
       dest: "/home/{{ user_login }}/.local/bin/nord"
       owner: "{{ user_login }}"
       group: "{{ user_login }}"
       mode: '0755'
   ```

6. **Firewall Configuration** (following `play-vpn.yml:26-29`)
   ```yaml
   - name: Add OpenVPN to Firewalld
     become: true
     ansible.builtin.shell: |
       firewall-cmd --add-service openvpn --permanent
       firewall-cmd --reload
     register: firewall_result
     changed_when: "'ALREADY_ENABLED' not in firewall_result.stderr"
     failed_when: false
   ```

7. **Usage Instructions**
   ```yaml
   - name: Display Setup Instructions
     ansible.builtin.debug:
       msg:
         - "════════════════════════════════════════════════════════════════"
         - "NordVPN OpenVPN Manager Installation Complete"
         - "════════════════════════════════════════════════════════════════"
         - ""
         - "📥 NEXT STEPS - Download OpenVPN Configs"
         - ""
         - "  1. Go to nordvpn.com → Dashboard → Downloads → Linux"
         - "  2. Choose 'OpenVPN' tab"
         - "  3. Download desired server configs (.ovpn files)"
         - "  4. Move them to: ~/.config/nordvpn/configs/"
         - ""
         - "  Example:"
         - "    mv ~/Downloads/*.ovpn ~/.config/nordvpn/configs/"
         - ""
         - "🚀 USAGE"
         - ""
         - "  nord list              # List available configs"
         - "  nord connect uk-london # Connect to VPN"
         - "  nord status            # Check status"
         - "  nord switch us-newyork # Switch servers"
         - "  nord disconnect        # Disconnect"
         - "  nord --help            # View all commands"
   ```

**Playbook Size:** ~150-200 lines

---

### 3. Update Host Variables: `environment/localhost/host_vars/localhost.yml`

**Purpose:** Store encrypted NordVPN credentials

**Changes:** Playbook will append these lines (user will manually encrypt later):

```yaml
# NordVPN OpenVPN Credentials
nordvpn_username: serviceusername123
nordvpn_password: servicepassword456
```

**Post-deployment:** User must encrypt these values with:
```bash
# Encrypt username
ansible-vault encrypt_string 'actual_username' --name 'nordvpn_username' --vault-id localhost@vault-pass.secret

# Encrypt password
ansible-vault encrypt_string 'actual_password' --name 'nordvpn_password' --vault-id localhost@vault-pass.secret

# Replace plaintext values in localhost.yml with encrypted versions
```

---

## Directory Structure

```
~/.config/nordvpn/                   # Main config directory (mode 0700)
├── configs/                         # User-placed .ovpn files (mode 0755)
│   ├── uk-london.ovpn
│   ├── us-newyork.ovpn
│   └── de-berlin.ovpn
├── .credentials                     # NordVPN username/password (mode 0600)
└── .current-connection              # State tracking (contains connection name)

~/.local/share/nordvpn/              # Runtime data (mode 0755)
└── nord.log                         # Log file (rotates at 1MB)

~/.local/bin/                        # Script location
└── nord                             # Main script (mode 0755)
```

---

## Implementation Steps

### Step 1: Create Main Script
1. Create `files/home/.local/bin/nord`
2. Implement logging functions (from wsi patterns)
3. Implement dependency checking
4. Implement core operations (import, connect, disconnect, status)
5. Implement command interface with argument parsing
6. Add error handling and cleanup
7. Test script locally: `bash -n files/home/.local/bin/nord`

### Step 2: Create Ansible Playbook
1. Create `playbooks/imports/optional/common/play-nordvpn-openvpn.yml`
2. Add system dependencies installation
3. Add directory creation tasks
4. Add credential collection (with conditional check)
5. Add credentials file deployment
6. Add script deployment
7. Add firewall configuration
8. Add usage instructions

### Step 3: Test Deployment
1. Run playbook: `ansible-playbook playbooks/imports/optional/common/play-nordvpn-openvpn.yml`
2. Verify directories created
3. Verify nord script deployed and executable
4. Verify credentials file created (mode 0600)

### Step 4: Test Functionality
1. Download sample .ovpn file from NordVPN
2. Place in `~/.config/nordvpn/configs/`
3. Test: `nord list` - should show config
4. Test: `nord connect <name>` - should import and connect
5. Test: `nord status` - should show connected
6. Test: `nord disconnect` - should disconnect
7. Test: GNOME Settings → Network - should see connection
8. Test: GUI connect/disconnect

### Step 5: Encrypt Credentials
1. Test nord with plaintext credentials first
2. Once working, encrypt credentials in `localhost.yml`
3. Re-run playbook to deploy encrypted credentials
4. Verify nord still works with encrypted credentials

---

## Verification & Testing

### Manual Test Checklist

**Installation:**
- [ ] Playbook runs without errors
- [ ] OpenVPN and NetworkManager packages installed
- [ ] Directories created with correct permissions (0700, 0755, 0600)
- [ ] nord script deployed and executable (`which nord`)
- [ ] Credentials file exists and secured (`ls -la ~/.config/nordvpn/.credentials`)

**Functionality:**
- [ ] `nord --help` shows usage
- [ ] `nord --version` shows version
- [ ] `nord list` shows "no configs" message when empty
- [ ] Download .ovpn file and place in configs/
- [ ] `nord list` shows the config
- [ ] `nord connect <name>` imports and connects (check output)
- [ ] `nord status` shows connected + public IP
- [ ] `nmcli connection show` lists nordvpn-* connection
- [ ] GNOME Settings → Network shows VPN connection
- [ ] `nord switch <other-name>` disconnects first, connects second
- [ ] `nord disconnect` disconnects
- [ ] `nord status` shows not connected

**Error Handling:**
- [ ] `nord connect nonexistent` shows error + lists available configs
- [ ] `nord connect` (no arg) shows usage
- [ ] Missing credentials file shows clear error
- [ ] NetworkManager stopped shows clear error

**Persistence:**
- [ ] Connection persists in NetworkManager (survives reboot)
- [ ] Can reconnect without re-import
- [ ] `nord cleanup` removes all connections
- [ ] Next connect re-imports successfully

**Integration:**
- [ ] GUI connect works (GNOME Settings → Network → VPN → Connect)
- [ ] GUI disconnect works
- [ ] System tray shows VPN icon when connected
- [ ] Log file created and rotates (test with large log)

### Expected Command Output Examples

```bash
$ nord list
Available VPN configs:

  uk-london
  us-newyork

Total: 2 configs

$ nord connect uk-london
[NORD] Importing uk-london to NetworkManager...
[NORD] Imported: nordvpn-uk-london
[NORD] Connecting to uk-london...
[NORD] Connected to uk-london

$ nord status
Connected: uk-london
Public IP: 185.xxx.xxx.xxx

$ nord disconnect
[NORD] Disconnecting uk-london
[NORD] Disconnected

$ nord status
Not connected
```

---

## Security Considerations

### Credential Protection

**In Git Repository:**
- ✅ Credentials encrypted in `localhost.yml` with ansible-vault
- ✅ Vault password in `vault-pass.secret` (gitignored)
- ❌ Never commit plaintext credentials

**On Filesystem:**
- ✅ `~/.config/nordvpn/.credentials` - mode 0600 (owner read/write only)
- ✅ `~/.config/nordvpn/` - mode 0700 (owner access only)
- ✅ NetworkManager stores credentials in keyring (protected by session)

**In NetworkManager:**
- ✅ Credentials stored in GNOME keyring
- ✅ Protected by user's session authentication
- ✅ Visible in GNOME Settings (by owner only)

### File Permissions Summary

```
~/.config/nordvpn/              0700 (drwx------)
~/.config/nordvpn/.credentials  0600 (-rw-------)
~/.config/nordvpn/configs/      0755 (drwxr-xr-x)
~/.local/bin/nord               0755 (-rwxr-xr-x)
~/.local/share/nordvpn/         0755 (drwxr-xr-x)
~/.local/share/nordvpn/nord.log 0644 (-rw-r--r--)
```

---

## Critical Files Reference

**Implementation:**
- `files/home/.local/bin/nord` - Main script (NEW)
- `playbooks/imports/optional/common/play-nordvpn-openvpn.yml` - Deployment playbook (NEW)
- `environment/localhost/host_vars/localhost.yml` - Credentials storage (APPEND)

**Pattern References:**
- `files/home/.local/bin/wsi:74-152` - Logging functions
- `files/home/.local/bin/wsi:290-335` - Dependency checking
- `files/home/.local/bin/wsi:387-464` - Argument parsing
- `playbooks/imports/optional/common/play-speech-to-text.yml:215-274` - Directory creation
- `playbooks/imports/optional/common/play-vpn.yml:14-29` - NetworkManager import + firewall

---

## Design Rationale

**Why On-Demand Import?**
- NordVPN has 5000+ servers, users may download 50+ configs
- Importing all would clutter NetworkManager
- Import-on-connect is fast (<2 seconds) and keeps NM clean

**Why Persistent Connections?**
- Faster subsequent connections (no re-import)
- Visible in GNOME Settings (better UX)
- User can manually manage via GUI
- Can be cleaned up with `nord cleanup` if needed

**Why No Systemd?**
- User preference stated in requirements
- NetworkManager already manages VPN connections
- No background daemon needed
- Simpler architecture

**Credentials Note:**
- NordVPN service credentials ≠ account login credentials
- Service credentials: alphanumeric string + password (for OpenVPN auth)
- Account credentials: email + password (for website login)
- Must use SERVICE credentials for OpenVPN connections
