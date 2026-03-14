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
MODE="${1:-full}"  # full | mounts

# ---------------------------------------------------------------------------
die()    { echo "ERROR: $*" >&2; exit 1; }
header() { echo ""; echo "━━━ $* ━━━"; echo ""; }
# ---------------------------------------------------------------------------

# --- Preflight checks -------------------------------------------------------

command -v rclone > /dev/null || die "rclone not installed.
Run the playbook first:
  ansible-playbook $PROJECT_ROOT/playbooks/imports/optional/common/play-rclone.yml"

command -v ansible-vault > /dev/null || die "ansible-vault not found. Is Ansible installed?"

[[ -f "$VAULT_PASS_FILE" ]] || die "Vault password file not found: $VAULT_PASS_FILE"
[[ -f "$HOST_VARS" ]]       || die "host_vars not found: $HOST_VARS"

# --- Step 1: rclone config --------------------------------------------------

if [[ "$MODE" != "mounts" ]]; then
    header "Step 1: Configure rclone remotes"
    echo "This opens the rclone configuration wizard."
    echo "Add or modify remotes, then choose 'q' (quit) when done."
    echo ""
    rclone config
fi

# --- Step 2: Vault the config -----------------------------------------------

VAULTED=""

if [[ "$MODE" != "mounts" ]]; then
    header "Step 2: Vault rclone config"

    [[ -f "$RCLONE_CONF" ]] || die "rclone config not found at $RCLONE_CONF
Run 'rclone config' to create it first."

    echo "Encrypting $RCLONE_CONF with ansible-vault..."
    VAULTED=$(ansible-vault encrypt_string \
        --vault-id "localhost@$VAULT_PASS_FILE" \
        --stdin-name rclone_config \
        < "$RCLONE_CONF") || die "ansible-vault encrypt_string failed"

    echo "Config vaulted successfully."
fi

# --- Step 3: Define mount points --------------------------------------------

header "Step 3: Configure mount points"

REMOTES=$(rclone listremotes | sed 's/:$//')

if [[ -n "$REMOTES" ]]; then
    echo "Available remotes:"
    while IFS= read -r remote; do echo "  $remote"; done <<< "$REMOTES"
else
    echo "(No remotes configured yet — run without 'mounts' argument to set them up)"
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

# --- Step 6: Deploy prompt --------------------------------------------------

header "Step 5: Deploy"

PLAYBOOK="$PROJECT_ROOT/playbooks/imports/optional/common/play-rclone.yml"

echo "Ready to deploy config and mount services."
read -rp "Run the playbook now? [y/N] " DEPLOY

if [[ "${DEPLOY,,}" == "y" ]]; then
    ansible-playbook "$PLAYBOOK"
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
