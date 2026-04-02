# UniFi Network Controller Setup Guide

Self-hosted UniFi Network Controller running as a Podman container, for managing UniFi access points with seamless whole-house WiFi roaming.

## Overview

Three wired UniFi APs, all sharing the same SSID, managed by a single controller. Your phone (and other devices) automatically roam to the strongest AP as you move around the house — no disconnects, no manual switching.

## Quick Start

```bash
cd ~/Projects/fedora-desktop
./playbooks/imports/optional/common/play-unifi-controller.yml
```

Then open **https://localhost:8443** (accept the self-signed certificate warning). First startup takes 1-2 minutes while the controller initialises.

## What the Playbook Deploys

| Component | Container | Purpose |
|-----------|-----------|---------|
| MongoDB 7.0 | `unifi-mongodb` | Database backend (required by UniFi) |
| UniFi Network Application | `unifi-network-controller` | AP management, WiFi config, roaming |

Both containers run in a shared Podman pod (`unifi-pod`) so they communicate over localhost. Data persists in `~/.local/share/unifi/`.

The playbook also:
- Opens required firewall ports (see [Ports Reference](#ports-reference))
- Installs a `unifi-controller` CLI command for on-demand start/stop
- Installs a GNOME desktop launcher (search "UniFi" in Activities)

## Network Setup (3 Wired APs)

### Prerequisites

- 3 UniFi APs, each connected via ethernet to your router/switch
- Each AP powered via PoE (from a PoE switch or injector) or USB-C (model dependent)
- Your Fedora machine on the same LAN

### Step 1: Physical Setup

Connect all three APs via ethernet to your router or switch and power them. That's it — no special wiring topology needed. Each AP has a direct wired backhaul to your network.

### Step 2: Controller First Run

1. Open **https://localhost:8443**
2. Create an admin account
3. Select **Advanced Setup** to stay local-only (skip Ubiquiti cloud account)
4. The controller will scan for devices on your LAN

### Step 3: Adopt All Three APs

All three APs should appear in **Devices** since they're wired to the same network.

1. Click **Adopt** on each AP
2. Wait for each to provision and come online (status changes from "Adopting" to "Connected")
3. If an AP was previously managed by another controller, factory reset it first (hold reset button 10+ seconds)

### Step 4: Create Your WiFi Network

1. Go to **Settings > WiFi**
2. Create a single WiFi network (one SSID, one password)
3. All three APs will broadcast the same network automatically

### Step 5: Enable Seamless Roaming

These settings ensure your phone switches APs smoothly without drops:

| Setting | Location | What It Does |
|---------|----------|--------------|
| **Fast Roaming (802.11r)** | Settings > WiFi > [network] > Advanced | Pre-authenticates with the next AP before switching — near-zero handoff time |
| **BSS Transition (802.11v)** | Settings > WiFi > [network] > Advanced | Controller tells clients to move to a better AP |
| **Minimum RSSI** | Settings > WiFi > [network] > Advanced | Forces clients off a weak AP so they roam to a stronger one (try -75 dBm to start) |

**802.11r note:** Most modern phones and laptops support it. Some older IoT devices (smart bulbs, old printers) may have trouble connecting with it enabled. If that happens, create a second SSID without 802.11r for those devices.

### Step 6: Set Controller Hostname

**Important** — APs need to reach the controller over the network:

1. Go to **Settings > System > Advanced**
2. Set **Controller Hostname/IP** to your machine's LAN IP (e.g., `192.168.1.x`)
3. Do NOT use `localhost` or `127.0.0.1` — the APs can't reach that

### Step 7: Verify

- Check each AP shows **"Connected"** with an **ethernet uplink** in **Devices > [AP] > Uplink**
- Connect your phone to the WiFi network
- Walk around the house — the **Clients** view shows which AP your phone is connected to
- You should see it switch APs seamlessly as you move

## AP Placement Tips

- **Spread evenly** — one per floor, or one at each end plus one in the middle
- **Avoid co-location** — APs too close together cause interference and confused roaming
- **Central mounting** — ceiling or high wall mount gives best coverage
- **Check coverage** in the controller's **WiFi Insights** or **Map** view after setup
- If you see clients "sticking" to a far AP, lower the **Minimum RSSI** threshold

## Starting and Stopping

The controller runs on-demand — it is **not** auto-started on boot.

### Desktop Launcher

Search **"UniFi"** in GNOME Activities to launch. This opens a terminal that starts the pod, opens the web UI in your browser, and keeps running until you close it or press Ctrl+C.

### Command Line

```bash
# Start controller and open web UI
unifi-controller start

# Stop controller
unifi-controller stop

# Check status
unifi-controller status
```

When launched with `start`, the controller keeps running in the foreground. Press **Ctrl+C** to stop it cleanly.

### Direct Pod Management

```bash
# View individual containers
podman ps --pod

# View UniFi logs
podman logs -f unifi-network-controller

# View MongoDB logs
podman logs -f unifi-mongodb
```

## Ports Reference

These ports are opened in the firewall by the playbook:

| Port | Protocol | Purpose | Required |
|------|----------|---------|----------|
| 8443 | TCP | Web UI (HTTPS) | Yes |
| 8080 | TCP | Device communication (inform URL) | Yes |
| 3478 | UDP | STUN (AP discovery) | Yes |
| 10001 | UDP | AP discovery (L2) | Yes |
| 1900 | UDP | UPnP/DLNA | Optional |
| 8843 | TCP | Guest portal HTTPS | Optional |
| 6789 | TCP | Speed test | Optional |

## Backup and Restore

### Backup

Controller backups are configured in **Settings > System > Backup**. Auto-backups are stored inside the container at `/config/data/backup/autobackup/`, which maps to:

```
~/.local/share/unifi/config/data/backup/autobackup/
```

You can also trigger a manual backup from the UI and download the `.unf` file.

### Restore

1. Open the controller UI
2. Go to **Settings > System > Backup**
3. Upload the `.unf` backup file

### Full Reset

To completely remove and redeploy:

```bash
unifi-controller stop
podman pod rm unifi-pod
rm -rf ~/.local/share/unifi

# Re-run the playbook
./playbooks/imports/optional/common/play-unifi-controller.yml
```

## Troubleshooting

### Controller won't start

```bash
# Check pod status
podman pod ps --no-trunc

# Check container logs
podman logs unifi-network-controller 2>&1 | tail -30
podman logs unifi-mongodb 2>&1 | tail -30

# Check if ports are already in use
ss -tlnp | grep -E '8443|8080|3478'
```

### APs not appearing in controller

1. Verify the AP and controller are on the same LAN subnet
2. Check that port 8080 is reachable from the AP's network:
   ```bash
   # From another machine on the same network
   curl -k http://<controller-ip>:8080/inform
   ```
3. Check firewall rules are active:
   ```bash
   firewall-cmd --list-ports --no-pager | cat
   ```
4. If the AP was previously adopted by another controller, factory reset it (hold reset button 10+ seconds)

### Phone not roaming between APs

- Enable **802.11r** and **802.11v** (see [Step 5](#step-5-enable-seamless-roaming))
- Set **Minimum RSSI** to force clients off weak APs (start with -75 dBm, lower if too aggressive)
- Ensure APs are far enough apart — overlapping coverage should be ~20%, not 80%
- Check your phone supports 802.11r (most modern phones do)
- Some Android phones are "sticky" by design — Minimum RSSI helps override this

### Controller accessible only on localhost

Set the controller hostname to your machine's LAN IP:
1. **Settings > System > Advanced**
2. Set **Controller Hostname/IP** to your LAN IP (e.g., `192.168.1.x`)
3. APs need this to communicate with the controller

## Notes

- **Controller is on-demand** — launch it when you need to manage your network. Basic WiFi continues when the controller is stopped, but you cannot make changes or view stats.
- **MongoDB credentials** (`unifi`/`unifi`) are internal to the pod and not exposed externally.
- **Timezone** is set to `Europe/London` in the playbook — edit `TZ` variable before deploying if needed.
- **Memory limit** is set to 1024MB — increase `MEM_LIMIT` in the playbook if managing many clients.
