#!/bin/bash
# Interactive rclone remote config, vault setup, and mount management
#
# Interactive helper for rclone setup and host_vars vault management.
# Runs rclone config, vaults the result, collects mount definitions,
# and patches host_vars/localhost.yml in-place.
#
# Usage:
#   ./scripts/setup-rclone.bash           # Full setup (config + mounts)
#   ./scripts/setup-rclone.bash mounts    # Reconfigure mounts only (skip rclone config)
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
# Colours
BOLD='\033[1m'
DIM='\033[2m'
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'  # No Colour

die()    { echo ""; echo -e "  ${RED}${BOLD}✗ FATAL:${NC} $*" >&2; echo ""; exit 1; }
ok()     { echo -e "  ${GREEN}✓${NC} $*"; }
warn()   { echo -e "  ${YELLOW}⚠${NC}  $*"; }
info()   { echo -e "  ${CYAN}→${NC}  $*"; }
header() { echo ""; echo -e "${BOLD}━━━ $* ━━━${NC}"; echo ""; }
check()  { echo -ne "  Checking ${DIM}$1${NC} ... "; }
# ---------------------------------------------------------------------------

# --- Preflight checks -------------------------------------------------------

header "Preflight checks"

# Guard: must not run inside CCY container
check "environment (not CCY container)"
if [[ "$PROJECT_ROOT" == "/workspace" && "$(id -u)" == "0" ]]; then
    echo -e "${RED}FAIL${NC}"
    die "Running inside CCY container as root.
This script must run on the HOST system where your Fedora desktop lives.
Exit the container and run from your host project directory:
  ~/Projects/fedora-desktop/scripts/setup-rclone.bash"
fi
ok "host environment (not container)"

# Required tools
check "ansible-playbook"
if ! command -v ansible-playbook > /dev/null; then
    echo -e "${RED}FAIL${NC}"
    die "ansible-playbook not found.
Install Ansible first:
  sudo dnf install ansible"
fi
ok "ansible-playbook ($(ansible-playbook --version | head -1))"

check "ansible-vault"
if ! command -v ansible-vault > /dev/null; then
    echo -e "${RED}FAIL${NC}"
    die "ansible-vault not found. Should be installed with Ansible."
fi
ok "ansible-vault"

check "python3"
if ! command -v python3 > /dev/null; then
    echo -e "${RED}FAIL${NC}"
    die "python3 not found. Required for host_vars patching."
fi
ok "python3 ($(python3 --version))"

# Required files
check "vault password ($VAULT_PASS_FILE)"
if [[ ! -f "$VAULT_PASS_FILE" ]]; then
    echo -e "${RED}FAIL${NC}"
    die "Vault password file not found: $VAULT_PASS_FILE
This file is gitignored and must exist on your host.
If you're starting fresh, re-run the bootstrap:
  $PROJECT_ROOT/run.bash"
fi
ok "vault password file exists"

check "host_vars ($HOST_VARS)"
if [[ ! -f "$HOST_VARS" ]]; then
    echo -e "${RED}FAIL${NC}"
    die "host_vars not found: $HOST_VARS
Unexpected — check that the repository is properly cloned."
fi
ok "host_vars/localhost.yml exists"

check "rclone playbook"
if [[ ! -f "$PLAYBOOK" ]]; then
    echo -e "${RED}FAIL${NC}"
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

# Fedora version matches repo expectation
check "Fedora version"
if [[ ! -f /etc/redhat-release ]]; then
    echo -e "${RED}FAIL${NC}"
    die "/etc/redhat-release not found — this script must run on a Fedora host."
fi
if ! grep -qi "^Fedora release" /etc/redhat-release; then
    echo -e "${RED}FAIL${NC}"
    die "Not running on Fedora: $(cat /etc/redhat-release)
This repository targets Fedora only."
fi
RUNNING_VERSION=$(grep -oP 'Fedora release \K[0-9]+' /etc/redhat-release)
EXPECTED_VERSION=$(grep 'fedora_version:' "$PROJECT_ROOT/vars/fedora-version.yml" | awk '{print $2}')
if [[ "$RUNNING_VERSION" != "$EXPECTED_VERSION" ]]; then
    echo -e "${YELLOW}WARN${NC}"
    warn "Fedora version mismatch: running Fedora $RUNNING_VERSION but repo targets Fedora $EXPECTED_VERSION."
    warn "The correct branch for this host may be: git checkout F${RUNNING_VERSION}"
    echo ""
    read -rp "  Continue anyway? [y/N] " CONTINUE_ANYWAY
    if [[ "${CONTINUE_ANYWAY,,}" != "y" ]]; then
        die "Aborted due to Fedora version mismatch."
    fi
else
    ok "Fedora $RUNNING_VERSION matches repo (vars/fedora-version.yml)"
fi

# Validate mode argument
check "mode argument"
if [[ "$MODE" != "full" && "$MODE" != "mounts" ]]; then
    echo -e "${RED}FAIL${NC}"
    die "Unknown mode: '$MODE'
Valid modes: full | mounts
Usage: $0 [full|mounts]"
fi
ok "mode = $MODE"

echo ""
echo -e "${GREEN}${BOLD}All preflight checks passed.${NC}"

# --- Step 1: rclone config --------------------------------------------------

if [[ "$MODE" != "mounts" ]]; then
    header "Step 1: Configure rclone remotes"

    echo -e "  The ${BOLD}rclone configuration wizard${NC} will open now."
    echo ""
    echo -e "  ${CYAN}What to do:${NC}"
    echo -e "    ${BOLD}n${NC}  → New remote  ${DIM}(add a cloud storage account)${NC}"
    echo -e "    ${BOLD}q${NC}  → Quit         ${DIM}(when you have added all your remotes)${NC}"
    echo ""
    echo -e "  ${YELLOW}What to skip:${NC}"
    echo -e "    ${BOLD}s${NC}  → Set configuration password  ${DIM}(not needed — we use Ansible Vault instead)${NC}"
    echo ""
    echo -e "  ${DIM}Common providers: Google Drive, S3, Hetzner Storage Box (SFTP, port 23)${NC}"
    echo ""
    read -rp "  Press Enter to open rclone config..." _

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

    info "Encrypting $RCLONE_CONF with ansible-vault..."
    # Use ANSIBLE_CONFIG explicitly so ansible.cfg (which defines the localhost
    # vault-id and password file) is always found regardless of working directory.
    # --encrypt-vault-id selects which loaded id to encrypt with (no duplicate).
    VAULTED=$(ANSIBLE_CONFIG="$PROJECT_ROOT/ansible.cfg" ansible-vault encrypt_string \
        --encrypt-vault-id localhost \
        --stdin-name rclone_config \
        < "$RCLONE_CONF") || die "ansible-vault encrypt_string failed.
Check that vault-pass.secret contains the correct vault password."

    ok "Config vaulted successfully."
fi

# --- Step 3: Define mount points --------------------------------------------

header "Step 3: Configure mount points"

REMOTES=$(rclone listremotes | sed 's/:$//')

if [[ -n "$REMOTES" ]]; then
    echo -e "  ${CYAN}Available remotes:${NC}"
    while IFS= read -r remote; do echo -e "    ${BOLD}$remote${NC}"; done <<< "$REMOTES"
else
    echo "  (No remotes configured — run without 'mounts' argument to set them up)"
fi

echo ""
echo -e "  Define auto-mount points. ${DIM}Press Enter with no name to finish.${NC}"
echo -e "  ${DIM}Leave mountpoint blank to use ~/mnt/<name>.${NC}"
echo ""

MOUNT_NAMES=()
MOUNT_REMOTES=()
MOUNT_POINTS=()

while true; do
    read -rp "  Mount name (e.g. gdrive-personal): " MOUNT_NAME
    [[ -z "$MOUNT_NAME" ]] && break

    read -rp "  Remote spec (e.g. gdrive-personal:/): " MOUNT_REMOTE
    if [[ -z "$MOUNT_REMOTE" ]]; then
        warn "Remote spec required — skipping."
        continue
    fi

    DEFAULT_MOUNTPOINT="$HOME/mnt/$MOUNT_NAME"
    read -rp "  Mountpoint [$DEFAULT_MOUNTPOINT]: " MOUNT_POINT
    MOUNT_POINT="${MOUNT_POINT:-$DEFAULT_MOUNTPOINT}"

    MOUNT_NAMES+=("$MOUNT_NAME")
    MOUNT_REMOTES+=("$MOUNT_REMOTE")
    MOUNT_POINTS+=("$MOUNT_POINT")

    ok "$MOUNT_NAME → $MOUNT_REMOTE at $MOUNT_POINT"
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

ok "host_vars updated: $HOST_VARS"

# --- Step 6: Deploy ---------------------------------------------------------

header "Step 5: Deploy"

echo -e "  Ready to deploy config and mount services via Ansible."
echo ""
read -rp "  Run the playbook now? [y/N] " DEPLOY

if [[ "${DEPLOY,,}" == "y" ]]; then
    ansible-playbook "$PLAYBOOK" || die "Playbook failed. Check output above."
    echo ""
    if [[ ${#MOUNT_NAMES[@]} -gt 0 ]]; then
        ok "Mount services enabled. Start them now with:"
        for name in "${MOUNT_NAMES[@]}"; do
            echo -e "    ${BOLD}systemctl --user start rclone-${name}.service${NC}"
        done
    fi
else
    info "Skipped. Deploy when ready:"
    echo -e "    ${BOLD}ansible-playbook $PLAYBOOK${NC}"
fi
