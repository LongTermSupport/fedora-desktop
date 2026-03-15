#!/bin/bash
# setup.bash
#
# Optional post-install setup dispatcher.
# Discovers all scripts/setup-*.bash scripts and offers to run them.
#
# Usage:
#   ./scripts/setup.bash
#
# Run this on the HOST system after the main playbook has been applied.
# Each setup-*.bash script handles its own full preflight checks.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# ---------------------------------------------------------------------------
die()    { echo ""; echo "  ✗ FATAL: $*" >&2; echo ""; exit 1; }
ok()     { echo "  ✓ $*"; }
header() { echo ""; echo "━━━ $* ━━━"; echo ""; }
check()  { echo -n "  Checking $1 ... "; }
# ---------------------------------------------------------------------------

header "Preflight checks"

check "environment (not CCY container)"
if [[ "$PROJECT_ROOT" == "/workspace" && "$(id -u)" == "0" ]]; then
    echo "FAIL"
    die "Running inside CCY container as root.
This script must run on the HOST system.
Exit the container and run: ~/Projects/fedora-desktop/scripts/setup.bash"
fi
ok "host environment"

check "Fedora version"
if [[ ! -f /etc/redhat-release ]]; then
    echo "FAIL"
    die "/etc/redhat-release not found — must run on a Fedora host."
fi
if ! grep -qi "^Fedora release" /etc/redhat-release; then
    echo "FAIL"
    die "Not running on Fedora: $(cat /etc/redhat-release)"
fi
RUNNING_VERSION=$(grep -oP 'Fedora release \K[0-9]+' /etc/redhat-release)
EXPECTED_VERSION=$(grep 'fedora_version:' "$PROJECT_ROOT/vars/fedora-version.yml" | awk '{print $2}')
if [[ "$RUNNING_VERSION" != "$EXPECTED_VERSION" ]]; then
    echo "FAIL"
    die "Fedora version mismatch: running $RUNNING_VERSION, repo targets $EXPECTED_VERSION.
Switch branch: git checkout F${RUNNING_VERSION}"
fi
ok "Fedora $RUNNING_VERSION"

echo ""
echo "All preflight checks passed."

# --- Discover setup-*.bash scripts ------------------------------------------

header "Available setup scripts"

mapfile -t SETUP_SCRIPTS < <(find "$SCRIPT_DIR" -maxdepth 1 -name "setup-*.bash" | sort)

if [[ ${#SETUP_SCRIPTS[@]} -eq 0 ]]; then
    echo "No setup-*.bash scripts found in $SCRIPT_DIR"
    exit 0
fi

for script in "${SETUP_SCRIPTS[@]}"; do
    name="$(basename "$script" .bash | sed 's/^setup-//')"
    # Pull description from second line (line after shebang)
    desc=$(sed -n '2p' "$script" | sed 's/^# *//')
    printf "  %-20s %s\n" "setup-${name}" "$desc"
done

# --- Offer to run each one --------------------------------------------------

header "Run setup scripts"

RAN_ANY=false

for script in "${SETUP_SCRIPTS[@]}"; do
    name="$(basename "$script" .bash | sed 's/^setup-//')"
    echo ""
    read -rp "Run setup-${name}.bash? [y/N] " ANSWER
    if [[ "${ANSWER,,}" == "y" ]]; then
        echo ""
        bash "$script"
        RAN_ANY=true
    fi
done

echo ""
if [[ "$RAN_ANY" == "true" ]]; then
    echo "Setup complete."
else
    echo "Nothing run. Execute individual scripts directly:"
    for script in "${SETUP_SCRIPTS[@]}"; do
        echo "  $script"
    done
fi
