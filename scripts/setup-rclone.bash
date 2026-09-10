#!/bin/bash
# Interactive rclone remote config, vault setup, and mount management
#
# Interactive helper for rclone setup and host_vars vault management.
# Runs rclone config, vaults the result, collects mount definitions,
# and patches host_vars/localhost.yml in-place.
#
# Usage:
#   ./scripts/setup-rclone.bash
#
# No arguments: every choice is a prompt. Step 1 asks whether to open the
# rclone wizard (answer no to keep the remotes as they are and go straight to
# mounts). Mounts already in host_vars are listed; selecting one offers edit
# or remove, so a mountpoint can be changed without hand-editing localhost.yml.
#
# Run this on the HOST system, from the project root.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
HOST_VARS="$PROJECT_ROOT/environment/localhost/host_vars/localhost.yml"
VAULT_PASS_FILE="$PROJECT_ROOT/vault-pass.secret"
RCLONE_CONF="$HOME/.config/rclone/rclone.conf"
PLAYBOOK="$PROJECT_ROOT/playbooks/imports/optional/common/play-rclone.yml"

if [[ $# -gt 0 ]]; then
    echo "This script takes no arguments — every choice is prompted for. Usage: $0" >&2
    exit 1
fi

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

# ansible.cfg (inventory, vault password, roles path) is only auto-loaded from
# the current directory, so run from the project root with it pinned. Ansible
# exits 0 when the host pattern matches nothing, so that case is caught here:
# a play that ran on no hosts deployed nothing.
run_playbook() {
    local log rc=0
    log=$(mktemp)
    (cd "$PROJECT_ROOT" && ANSIBLE_CONFIG="$PROJECT_ROOT/ansible.cfg" ansible-playbook "$PLAYBOOK") 2>&1 | tee "$log" || rc=$?
    if grep -q "skipping: no hosts matched" "$log"; then
        rm -f "$log"
        die "The playbook matched no hosts, so nothing was deployed.
Check that $PROJECT_ROOT/environment/localhost/ contains the inventory (try: git status)."
    fi
    rm -f "$log"
    return "$rc"
}

# set -e exits silently on any unhandled failure; name the command so a run
# that stops between steps says why instead of just dropping to the prompt.
trap 'echo ""; echo -e "  ${RED}${BOLD}✗ FATAL:${NC} command failed at line $LINENO: $BASH_COMMAND" >&2; echo ""' ERR

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
    run_playbook || die "Playbook failed during rclone install. Check output above."
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

echo ""
echo -e "${GREEN}${BOLD}All preflight checks passed.${NC}"

# --- Step 1: rclone config --------------------------------------------------

header "Step 1: Configure rclone remotes"

# Remotes already saved on this machine decide the default: first run opens
# the wizard unconditionally; later runs ask, so a mount-only change does not
# have to sit through the wizard again.
mapfile -t EXISTING_REMOTES < <(rclone listremotes)
RUN_WIZARD="y"
if [[ ${#EXISTING_REMOTES[@]} -gt 0 ]]; then
    echo -e "  ${CYAN}Remotes already configured on this machine:${NC}"
    for r in "${EXISTING_REMOTES[@]}"; do
        echo -e "    ${GREEN}✓${NC} ${r%:}"
    done
    echo ""
    for _attempt in 1 2 3; do
        read -rp "  Open the rclone wizard to add or edit remotes? [y/N] " RUN_WIZARD
        RUN_WIZARD="${RUN_WIZARD,,}"
        RUN_WIZARD="${RUN_WIZARD:-n}"
        [[ "$RUN_WIZARD" == "y" || "$RUN_WIZARD" == "n" ]] && break
        warn "Enter y or n."
        RUN_WIZARD=""
    done
    [[ -z "$RUN_WIZARD" ]] && die "No valid answer after 3 attempts."
    echo ""
fi

if [[ "$RUN_WIZARD" == "y" ]]; then
    echo -e "  The ${BOLD}rclone configuration wizard${NC} will open now."
    echo ""
    echo -e "  ${CYAN}What to do:${NC}"
    echo -e "    ${BOLD}n${NC}  → New remote   ${DIM}(add a cloud storage account)${NC}"
    echo -e "    ${BOLD}e${NC}  → Edit remote  ${DIM}(fix client ID/secret or reconnect)${NC}"
    echo -e "    ${BOLD}q${NC}  → Quit         ${DIM}(when you have finished — this script continues to mounts)${NC}"
    echo ""
    echo -e "  ${YELLOW}What to skip:${NC}"
    echo -e "    ${BOLD}s${NC}  → Set configuration password  ${DIM}(not needed — we use Ansible Vault instead)${NC}"
    echo ""
    echo -e "  ${DIM}Common providers: Google Drive, S3, Hetzner Storage Box (SFTP, port 23)${NC}"
    echo ""
    read -rp "  Press Enter to open rclone config..." _

    # rclone config can return non-zero after an edit/reconnect even though the
    # config was saved; only the saved remotes matter, so report and carry on.
    RCLONE_CONFIG_RC=0
    rclone config || RCLONE_CONFIG_RC=$?
    if [[ "$RCLONE_CONFIG_RC" -ne 0 ]]; then
        echo ""
        warn "rclone config exited with status $RCLONE_CONFIG_RC — checking whether remotes were saved anyway."
    fi

    # Verify at least one remote exists after config
    REMOTE_COUNT=$(rclone listremotes | wc -l)
    if [[ "$REMOTE_COUNT" -eq 0 ]]; then
        die "No remotes configured in rclone.
Re-run this script and add at least one remote in the rclone config wizard."
    fi
else
    info "Keeping existing remotes; rclone config in host_vars is left as-is."
fi

# --- Step 2: Vault the config -----------------------------------------------

VAULTED=""

if [[ "$RUN_WIZARD" == "y" ]]; then
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

# Build indexed array of remote names (strip trailing colon)
mapfile -t REMOTE_LIST < <(rclone listremotes | sed 's/:$//')

if [[ ${#REMOTE_LIST[@]} -eq 0 ]]; then
    echo "  (No remotes configured — re-run and answer yes to the rclone wizard prompt)"
    echo ""
fi

# Pre-populate from existing host_vars (handles !vault tags safely)
MOUNT_NAMES=()
MOUNT_REMOTES=()
MOUNT_POINTS=()

mapfile -t _EXISTING < <(python3 - "$HOST_VARS" <<'PYEOF'
import sys, yaml

class VaultLoader(yaml.SafeLoader):
    pass
VaultLoader.add_constructor('!vault', lambda loader, node: '__VAULTED__')

with open(sys.argv[1]) as f:
    data = yaml.load(f, Loader=VaultLoader)

for m in data.get('rclone_mounts') or []:
    print(f"{m['name']}|{m['remote']}|{m['mountpoint']}")
PYEOF
)

if [[ ${#_EXISTING[@]} -gt 0 ]]; then
    echo -e "  ${CYAN}Existing mounts (already in host_vars):${NC}"
    for entry in "${_EXISTING[@]}"; do
        IFS='|' read -r _n _r _p <<< "$entry"
        MOUNT_NAMES+=("$_n")
        MOUNT_REMOTES+=("$_r")
        MOUNT_POINTS+=("$_p")
        echo -e "    ${GREEN}✓${NC} $_n → $_r at $_p"
    done
    echo ""
    echo -e "  ${DIM}Select a mounted remote to edit or remove it; select an unmounted one to add it; 0 to finish.${NC}"
    echo ""
fi

# Index into MOUNT_* arrays of the entry for a remote, or empty if none.
existing_mount_index() {
    local remote="$1" i
    for i in "${!MOUNT_REMOTES[@]}"; do
        if [[ "${MOUNT_REMOTES[$i]}" == "${remote}:"* ]]; then
            echo "$i"
            return
        fi
    done
}

# Drop entry $1 from all three MOUNT_* arrays, keeping them contiguous.
remove_mount_index() {
    local idx="$1"
    unset "MOUNT_NAMES[$idx]" "MOUNT_REMOTES[$idx]" "MOUNT_POINTS[$idx]"
    MOUNT_NAMES=("${MOUNT_NAMES[@]+"${MOUNT_NAMES[@]}"}")
    MOUNT_REMOTES=("${MOUNT_REMOTES[@]+"${MOUNT_REMOTES[@]}"}")
    MOUNT_POINTS=("${MOUNT_POINTS[@]+"${MOUNT_POINTS[@]}"}")
}

while true; do
    # Show numbered remote picker (mark remotes that already have a mount configured)
    if [[ ${#REMOTE_LIST[@]} -gt 0 ]]; then
        echo -e "  ${CYAN}Available remotes:${NC}"
        for i in "${!REMOTE_LIST[@]}"; do
            REMOTE_ALREADY=""
            EXISTING_IDX=$(existing_mount_index "${REMOTE_LIST[$i]}")
            if [[ -n "$EXISTING_IDX" ]]; then
                REMOTE_ALREADY="  ${GREEN}✓ mounted${NC} at ${DIM}${MOUNT_POINTS[$EXISTING_IDX]}${NC}"
            fi
            echo -e "    ${BOLD}$((i+1))${NC}  ${REMOTE_LIST[$i]}${REMOTE_ALREADY}"
        done
        echo -e "    ${BOLD}0${NC}  Done — no more changes"
        echo ""
    fi

    read -rp "  Select remote [0-${#REMOTE_LIST[@]}]: " REMOTE_SEL
    [[ -z "$REMOTE_SEL" || "$REMOTE_SEL" == "0" ]] && break

    # Validate selection is a number in range
    if ! [[ "$REMOTE_SEL" =~ ^[0-9]+$ ]] || \
       (( REMOTE_SEL < 1 || REMOTE_SEL > ${#REMOTE_LIST[@]} )); then
        warn "Invalid selection — enter a number between 1 and ${#REMOTE_LIST[@]}, or 0 to finish."
        echo ""
        continue
    fi

    SELECTED_REMOTE="${REMOTE_LIST[$((REMOTE_SEL-1))]}"

    # Already mounted: offer edit (re-prompt with current values as defaults) or remove.
    # Either way the old entry is dropped; edit re-adds it below with the new answers.
    DEFAULT_SUBPATH="/"
    DEFAULT_MOUNT_NAME="${SELECTED_REMOTE,,}"
    DEFAULT_MOUNT_NAME="${DEFAULT_MOUNT_NAME// /-}"
    DEFAULT_MOUNTPOINT=""
    EXISTING_IDX=$(existing_mount_index "$SELECTED_REMOTE")
    if [[ -n "$EXISTING_IDX" ]]; then
        echo ""
        echo -e "  ${BOLD}$SELECTED_REMOTE${NC} is already mounted:"
        echo -e "    ${MOUNT_NAMES[$EXISTING_IDX]} → ${MOUNT_REMOTES[$EXISTING_IDX]} at ${MOUNT_POINTS[$EXISTING_IDX]}"
        echo ""
        ACTION=""
        for _attempt in 1 2 3; do
            read -rp "  [e]dit this mount, [r]emove it, or [c]ancel? [e/r/c]: " ACTION
            ACTION="${ACTION,,}"
            [[ "$ACTION" == "e" || "$ACTION" == "r" || "$ACTION" == "c" ]] && break
            warn "Enter e, r, or c."
            ACTION=""
        done
        echo ""
        case "$ACTION" in
            e)
                DEFAULT_SUBPATH="${MOUNT_REMOTES[$EXISTING_IDX]#*:}"
                DEFAULT_MOUNT_NAME="${MOUNT_NAMES[$EXISTING_IDX]}"
                DEFAULT_MOUNTPOINT="${MOUNT_POINTS[$EXISTING_IDX]}"
                remove_mount_index "$EXISTING_IDX"
                ;;
            r)
                remove_mount_index "$EXISTING_IDX"
                ok "Removed mount for $SELECTED_REMOTE"
                echo ""
                continue
                ;;
            *)
                continue
                ;;
        esac
    fi

    # List top-level folders — if this fails the remote is broken/unreachable
    echo ""
    echo -e "  ${CYAN}Connecting to ${BOLD}$SELECTED_REMOTE${NC}${CYAN}...${NC}"
    LSD_OUT=$(mktemp)
    LSD_ERR=$(mktemp)
    if rclone lsd "${SELECTED_REMOTE}:/" >"$LSD_OUT" 2>"$LSD_ERR"; then
        TOP_DIRS=$(awk '{print $NF}' "$LSD_OUT")
        rm -f "$LSD_OUT" "$LSD_ERR"
        echo -e "  ${CYAN}Top-level folders in ${BOLD}$SELECTED_REMOTE${NC}${CYAN}:${NC}"
        if [[ -n "$TOP_DIRS" ]]; then
            while IFS= read -r dir; do
                echo -e "    ${DIM}/$dir${NC}"
            done <<< "$TOP_DIRS"
        else
            echo -e "    ${DIM}(remote root is empty)${NC}"
        fi
    else
        LSD_ERR_MSG=$(cat "$LSD_ERR")
        rm -f "$LSD_OUT" "$LSD_ERR"
        echo ""
        warn "Could not connect to ${BOLD}$SELECTED_REMOTE${NC}."
        echo ""

        # Show the rclone error message (log lines only, skip the JSON Details blob)
        LSD_ERR_SUMMARY=$(echo "$LSD_ERR_MSG" | grep -E "^[0-9]{4}/" | sed 's/^[0-9/: ]*//')
        echo -e "  ${DIM}rclone: ${LSD_ERR_SUMMARY}${NC}"
        echo ""

        # Give targeted advice based on the error
        if echo "$LSD_ERR_MSG" | grep -q "SERVICE_DISABLED\|API has not been used\|accessNotConfigured"; then
            PROJECT_ID=$(echo "$LSD_ERR_MSG" | grep -oP 'project[= ]\K[0-9]+' | head -1)
            echo -e "  ${YELLOW}▶ The Google Drive API is not enabled on your GCP project.${NC}"
            echo ""
            echo -e "  Fix: visit this URL and click ${BOLD}Enable${NC}:"
            if [[ -n "$PROJECT_ID" ]]; then
                echo -e "    ${CYAN}https://console.developers.google.com/apis/api/drive.googleapis.com/overview?project=${PROJECT_ID}${NC}"
            else
                echo -e "    ${CYAN}https://console.developers.google.com/apis/api/drive.googleapis.com${NC}"
            fi
            echo ""
            echo -e "  Then wait ~1 minute and select this remote again to retry."
        elif echo "$LSD_ERR_MSG" | grep -q "AuthError\|oauth\|token\|401\|invalid_grant"; then
            echo -e "  ${YELLOW}▶ Authentication failed — the OAuth token may be missing or expired.${NC}"
            echo ""
            echo -e "  Fix: open another terminal and re-authenticate:"
            echo -e "    ${BOLD}rclone config reconnect ${SELECTED_REMOTE}:${NC}"
            echo ""
            echo -e "  Then select this remote again to retry."
        elif echo "$LSD_ERR_MSG" | grep -q "connection refused\|no such host\|network\|dial tcp"; then
            echo -e "  ${YELLOW}▶ Network error — could not reach the remote service.${NC}"
            echo ""
            echo -e "  Check your internet connection, then re-run this script."
        else
            echo -e "  ${YELLOW}▶ Unexpected error — see rclone output above.${NC}"
            echo ""
            echo -e "  Try: ${BOLD}rclone lsd ${SELECTED_REMOTE}:/${NC} to investigate further."
        fi
        echo -e "  Fix the issue above, then select this remote again to retry."
        echo ""
        continue
    fi
    echo ""

    # Subpath within the remote (/ = entire remote). Defaults come from the
    # existing entry when editing, otherwise the generic defaults set above.
    echo -e "  ${DIM}Enter a folder path to mount only part of the remote, or press Enter to keep the default.${NC}"
    read -rp "  Folder to mount [$DEFAULT_SUBPATH]: " REMOTE_SUBPATH
    REMOTE_SUBPATH="${REMOTE_SUBPATH:-$DEFAULT_SUBPATH}"

    read -rp "  Mount name [$DEFAULT_MOUNT_NAME]: " MOUNT_NAME
    MOUNT_NAME="${MOUNT_NAME:-$DEFAULT_MOUNT_NAME}"

    # Mountpoint: default to ~/mnt/<name> unless editing an existing entry
    DEFAULT_MOUNTPOINT="${DEFAULT_MOUNTPOINT:-$HOME/mnt/$MOUNT_NAME}"
    read -rp "  Local mountpoint [$DEFAULT_MOUNTPOINT]: " MOUNT_POINT
    MOUNT_POINT="${MOUNT_POINT:-$DEFAULT_MOUNTPOINT}"

    MOUNT_REMOTE="${SELECTED_REMOTE}:${REMOTE_SUBPATH}"

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
python3 - "$HOST_VARS" "$RUN_WIZARD" <<'PYEOF'
import sys
import re

path = sys.argv[1]
replace_config = sys.argv[2] == 'y'

with open(path) as f:
    content = f.read()

# Pattern: match a top-level key and everything indented under it
# (reads until next top-level key or end of file)
block_pattern = r'^{key}:[ \t]*.*?(?=^\S|\Z)'

if replace_config:
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
    run_playbook || die "Playbook failed. Check output above."
    echo ""
    if [[ ${#MOUNT_NAMES[@]} -gt 0 ]]; then
        ok "Mount services started and enabled."
        for name in "${MOUNT_NAMES[@]}"; do
            echo -e "    ${DIM}systemctl --user status rclone-${name}.service${NC}"
        done
    fi
else
    info "Skipped. Deploy when ready (from the project root, so ansible.cfg is found):"
    echo -e "    ${BOLD}cd $PROJECT_ROOT && ansible-playbook $PLAYBOOK${NC}"
fi
