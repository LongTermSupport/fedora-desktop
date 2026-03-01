#!/usr/bin/bash
# Build a custom Fedora installation ISO with the kickstart embedded.
#
# Usage: sudo bash ./fedora-install/build-iso.bash <fedora-iso> [output-iso]
#
# Requires the 'lorax' package (provides mkksiso).
# The output ISO can be flashed to USB with:
#   sudo dd if=output.iso of=/dev/sdX bs=4M status=progress oflag=sync

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
KS_FILE="${SCRIPT_DIR}/ks.cfg"

die() {
    echo "ERROR: $*" >&2
    exit 1
}

# --- Preflight checks ---

if [[ $EUID -ne 0 ]]; then
    die "This script must be run as root (use sudo)"
fi

if [[ $# -lt 1 ]]; then
    echo "Usage: sudo bash $0 <fedora-iso> [output-iso]"
    echo ""
    echo "Examples:"
    echo "  sudo bash $0 ~/Downloads/Fedora-Everything-netinst-x86_64-43-1.1.iso"
    echo "  sudo bash $0 ~/Downloads/Fedora-Workstation-Live-x86_64-43-1.1.iso custom.iso"
    exit 1
fi

FEDORA_ISO="$1"
OUTPUT_ISO="${2:-$(dirname "$FEDORA_ISO")/fedora-desktop-custom.iso}"

if [[ ! -f "$FEDORA_ISO" ]]; then
    die "Input ISO not found: $FEDORA_ISO"
fi

if [[ ! -f "$KS_FILE" ]]; then
    die "Kickstart file not found: $KS_FILE"
fi

if ! command -v mkksiso &>/dev/null; then
    echo "mkksiso not found. Installing lorax..."
    dnf -y install lorax
fi

# --- Build ISO ---

echo "Building custom Fedora ISO..."
echo "  Source ISO:  $FEDORA_ISO"
echo "  Kickstart:   $KS_FILE"
echo "  Output ISO:  $OUTPUT_ISO"
echo ""

mkksiso --ks "$KS_FILE" "$FEDORA_ISO" "$OUTPUT_ISO"

echo ""
echo "=== Build complete ==="
echo "Output: $OUTPUT_ISO"
echo ""
echo "To flash to USB:"
echo "  sudo dd if=$OUTPUT_ISO of=/dev/sdX bs=4M status=progress oflag=sync"
echo ""
echo "Replace /dev/sdX with your USB device (check with 'lsblk')."
