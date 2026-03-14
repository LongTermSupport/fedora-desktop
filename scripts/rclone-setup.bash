#!/bin/bash
# rclone-setup.bash
#
# Interactive helper for rclone setup and host_vars vault management.
# Runs rclone config, vaults the result, collects mount definitions,
# and patches host_vars/localhost.yml in-place.
#
# Usage:
#   ./scripts/rclone-setup.bash           # Full setup (config + mounts)
#   ./scripts/rclone-setup.bash mounts    # Reconfigure mounts only (skip rclone config)
#
# Run this on the HOST system, from the project root.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOST_VARS="$PROJECT_ROOT/environment/localhost/host_vars/localhost.yml"
VAULT_PASS_FILE="$PROJECT_ROOT/vault-pass.secret"
RCLONE_CONF="$HOME/.config/rclone/rclone.conf"
PLAYBOOK="$PROJECT_ROOT/playbooks/imports/optional/common/play-rclone.yml"
MODE="${1:-full}"  # full | mounts

# ---------------------------------------------------------------------------
die()     { echo ""; echo "  ✗ FATAL: $*" >&2; echo ""; exit 1; }
ok()      { echo "  ✓ $*"; }
warn()    { echo "  ⚠ $*"; }
header()  { echo ""; echo "━━━ $* ━━━"; echo ""; }
check()   { echo -n "  Checking $1 ... "; }
# ---------------------------------------------------------------------------

# --- Preflight checks -------------------------------------------------------

header "Preflight checks"

# Guard: must not run inside CCY container
check "environment (not CCY container)"
if [[ "$PROJECT_ROOT" == "/workspace" && "$(id -u)" == "0" ]]; then
    echo "FAIL"
    die "Running inside CCY container as root.
This script must run on the HOST system where your Fedora desktop lives.
Exit the container and run from your host project directory:
  ~/Projects/fedora-desktop/scripts/rclone-setup.bash"
fi
ok "host environment (not container)"

# Required tools
check "ansible-playbook"
if ! command -v ansible-playbook > /dev/null; then
    echo "FAIL"
    die "ansible-playbook not found.
Install Ansible first:
  sudo dnf install ansible"
fi
ok "ansible-playbook ($(ansible-playbook --version | head -1))"

check "ansible-vault"
if ! command -v ansible-vault > /dev/null; then
    echo "FAIL"
    die "ansible-vault not found. Should be installed with Ansible."
fi
ok "ansible-vault"

check "python3"
if ! command -v python3 > /dev/null; then
    echo "FAIL"
    die "python3 not found. Required for host_vars patching."
fi
ok "python3 ($(python3 --version))"

# Required files
check "vault password ($VAULT_PASS_FILE)"
if [[ ! -f "$VAULT_PASS_FILE" ]]; then
    echo "FAIL"
    die "Vault password file not found: $VAULT_PASS_FILE
This file is gitignored and must exist on your host.
If you're starting fresh, re-run the bootstrap:
  $PROJECT_ROOT/run.bash"
fi
ok "vault password file exists"

check "host_vars ($HOST_VARS)"
if [[ ! -f "$HOST_VARS" ]]; then
    echo "FAIL"
    die "host_vars not found: $HOST_VARS
Unexpected — check that the repository is properly cloned."
fi
ok "host_vars/localhost.yml exists"

check "rclone playbook"
if [[ ! -f "$PLAYBOOK" ]]; then
    echo "FAIL"
    die "Playbook not found: $PLAYBOOK
Try: git pull"
fi
ok "play-rclone.yml exists"

# Check rclone — install via playbook if missing
check "rclone binary"
if ! command -v rclone > /dev/null; then
    echo "not installed"
    warn "rclone not found — running playbook to install it first..."
    echo ""
    ansible-playbook "$PLAYBOOK" || die "Playbook failed during rclone install. Check output above."
    echo ""
    if ! command -v rclone > /dev/null; then
        die "rclone still not found after running playbook.
Check playbook output for errors."
    fi
    ok "rclone installed ($(rclone --version | head -1))"
else
    ok "rclone ($(rclone --version | head -1))"
fi

# Validate mode argument
check "mode argument"
if [[ "$MODE" != "full" && "$MODE" != "mounts" ]]; then
    echo "FAIL"
    die "Unknown mode: '$MODE'
Valid modes: full | mounts
Usage: $0 [full|mounts]"
fi
ok "mode = $MODE"

echo ""
echo "All preflight checks passed."

# --- Step 1: rclone config --------------------------------------------------

if [[ "$MODE" != "mounts" ]]; then
    header "Step 1: Configure rclone remotes"
    echo "This opens the rclone configuration wizard."
    echo "Add or modify remotes, then choose 'q' (quit) when done."
    echo ""
    rclone config

    # Verify at least one remote exists after config
    REMOTE_COUNT=$(rclone listremotes | wc -l)
    if [[ "$REMOTE_COUNT" -eq 0 ]]; then
        die "No remotes configured in rclone.
Re-run this script and add at least one remote in the rclone config wizard."
    fi
fi

# --- Step 2: Vault the config -----------------------------------------------

VAULTED=""

if [[ "$MODE" != "mounts" ]]; then
    header "Step 2: Vault rclone config"

    if [[ ! -f "$RCLONE_CONF" ]]; then
        die "rclone config not found at $RCLONE_CONF
This is unexpected after completing rclone config — check rclone output above."
    fi

    echo "Encrypting $RCLONE_CONF with ansible-vault..."
    VAULTED=$(ansible-vault encrypt_string \
        --vault-id "localhost@$VAULT_PASS_FILE" \
        --stdin-name rclone_config \
        < "$RCLONE_CONF") || die "ansible-vault encrypt_string failed.
Check that vault-pass.secret contains the correct vault password."

    echo "Config vaulted successfully."
fi

# --- Step 3: Define mount points --------------------------------------------

header "Step 3: Configure mount points"

REMOTES=$(rclone listremotes | sed 's/:$//')

if [[ -n "$REMOTES" ]]; then
    echo "Available remotes:"
    while IFS= read -r remote; do echo "  $remote"; done <<< "$REMOTES"
else
    echo "(No remotes configured — run without 'mounts' argument to set them up)"
fi

echo ""
echo "Define auto-mount points (press Enter with no name to finish)."
echo "Leave mountpoint blank to use ~/mnt/<name>."
echo ""

MOUNT_NAMES=()
MOUNT_REMOTES=()
MOUNT_POINTS=()

while true; do
    read -rp "  Mount name (e.g. gdrive-personal): " MOUNT_NAME
    [[ -z "$MOUNT_NAME" ]] && break

    read -rp "  Remote spec (e.g. gdrive-personal:/): " MOUNT_REMOTE
    if [[ -z "$MOUNT_REMOTE" ]]; then
        echo "  Remote spec required — skipping."
        continue
    fi

    DEFAULT_MOUNTPOINT="$HOME/mnt/$MOUNT_NAME"
    read -rp "  Mountpoint [$DEFAULT_MOUNTPOINT]: " MOUNT_POINT
    MOUNT_POINT="${MOUNT_POINT:-$DEFAULT_MOUNTPOINT}"

    MOUNT_NAMES+=("$MOUNT_NAME")
    MOUNT_REMOTES+=("$MOUNT_REMOTE")
    MOUNT_POINTS+=("$MOUNT_POINT")

    echo "  ✓ $MOUNT_NAME → $MOUNT_REMOTE at $MOUNT_POINT"
    echo ""
done

# --- Step 4: Build mounts YAML ----------------------------------------------

if [[ ${#MOUNT_NAMES[@]} -gt 0 ]]; then
    MOUNTS_YAML="rclone_mounts:"
    for i in "${!MOUNT_NAMES[@]}"; do
        MOUNTS_YAML+="
  - name: ${MOUNT_NAMES[$i]}
    remote: \"${MOUNT_REMOTES[$i]}\"
    mountpoint: \"${MOUNT_POINTS[$i]}\""
    done
else
    MOUNTS_YAML=""
fi

# --- Step 5: Patch host_vars ------------------------------------------------

header "Step 4: Updating host_vars"

# Use Python to safely strip existing rclone_config and rclone_mounts blocks,
# then append the new values. Works with mixed plain+vaulted YAML.
python3 - "$HOST_VARS" "$MODE" <<'PYEOF'
import sys
import re

path = sys.argv[1]
mode = sys.argv[2]

with open(path) as f:
    content = f.read()

# Pattern: match a top-level key and everything indented under it
# (reads until next top-level key or end of file)
block_pattern = r'^{key}:[ \t]*.*?(?=^\S|\Z)'

if mode != 'mounts':
    content = re.sub(
        block_pattern.format(key='rclone_config'),
        '',
        content,
        flags=re.MULTILINE | re.DOTALL,
    )

content = re.sub(
    block_pattern.format(key='rclone_mounts'),
    '',
    content,
    flags=re.MULTILINE | re.DOTALL,
)

# Normalise blank lines
content = re.sub(r'\n{3,}', '\n\n', content).rstrip() + '\n'

with open(path, 'w') as f:
    f.write(content)

print(f"Cleaned existing rclone entries from {path}")
PYEOF

# Append new values
{
    if [[ -n "$VAULTED" ]]; then
        printf '\n%s\n' "$VAULTED"
    fi
    if [[ -n "$MOUNTS_YAML" ]]; then
        printf '\n%s\n' "$MOUNTS_YAML"
    fi
} >> "$HOST_VARS"

echo "host_vars updated: $HOST_VARS"

# --- Step 6: Deploy ---------------------------------------------------------

header "Step 5: Deploy"

echo "Ready to deploy config and mount services."
read -rp "Run the playbook now? [y/N] " DEPLOY

if [[ "${DEPLOY,,}" == "y" ]]; then
    ansible-playbook "$PLAYBOOK" || die "Playbook failed. Check output above."
    echo ""
    if [[ ${#MOUNT_NAMES[@]} -gt 0 ]]; then
        echo "Mount services enabled. Start them now with:"
        for name in "${MOUNT_NAMES[@]}"; do
            echo "  systemctl --user start rclone-${name}.service"
        done
    fi
else
    echo "Skipped. Deploy when ready:"
    echo "  ansible-playbook $PLAYBOOK"
fi
