#!/usr/bin/bash
# Set up a GRUB boot entry for Fedora network install
# Downloads vmlinuz + initrd to /boot and creates a GRUB menu entry
# so you can reinstall Fedora without a USB key.
#
# Usage: sudo bash ./fedora-install/setup-netinstall-boot.bash
#
# Automatically fetches the latest released Fedora version from
# fedoraproject.org. Requires curl and jq.
#
# After running, reboot and select "Fedora XX Network Install" from GRUB.
# Connect to WiFi in Anaconda's Network & Host Name screen, then proceed.
#
# To remove: sudo bash ./fedora-install/setup-netinstall-boot.bash --remove

set -euo pipefail

BOOT_DIR="/boot/fedora-netinstall"
GRUB_ENTRY="/etc/grub.d/40_fedora_netinstall"
BASE_URL="https://download.fedoraproject.org/pub/fedora/linux/releases"

# --- helpers ---

die() {
    echo "ERROR: $*" >&2
    exit 1
}

preflight_checks() {
    local mode="$1"

    # Must be root
    if [[ $EUID -ne 0 ]]; then
        die "This script must be run as root (use sudo)"
    fi

    # Required commands
    for cmd in curl jq grub2-mkconfig df; do
        if ! command -v "$cmd" &>/dev/null; then
            die "Required command not found: $cmd"
        fi
    done

    # GRUB directory must exist
    if [[ ! -d /etc/grub.d ]]; then
        die "/etc/grub.d does not exist. Is GRUB installed?"
    fi

    # /boot must be mounted
    if ! mountpoint -q /boot 2>/dev/null && [[ ! -d /boot/grub2 ]]; then
        die "/boot does not appear to be mounted or configured"
    fi

    if [[ "$mode" == "setup" ]]; then
        # Check /boot has enough space (~300MB needed)
        local avail_kb
        avail_kb=$(df --output=avail /boot | tail -1 | tr -d ' ')
        if [[ "$avail_kb" -lt 307200 ]]; then
            die "/boot has less than 300MB free ($(( avail_kb / 1024 ))MB available). Need ~300MB."
        fi
    fi
}

get_latest_version() {
    local version
    version=$(curl -sfL https://fedoraproject.org/releases.json \
        | jq -r '[.[] .version | select(test("^[0-9]+$"))] | max')
    if [[ -z "$version" || "$version" == "null" ]]; then
        die "Failed to fetch latest Fedora version from fedoraproject.org"
    fi
    echo "$version"
}

verify_remote_files() {
    local target_version="$1"
    local vmlinuz_url="${BASE_URL}/${target_version}/Everything/x86_64/os/images/pxeboot/vmlinuz"

    echo "Verifying Fedora ${target_version} files exist on server..."
    local http_code
    http_code=$(curl -sfL -o /dev/null -w '%{http_code}' "$vmlinuz_url" 2>/dev/null || true)
    if [[ "$http_code" != "200" ]]; then
        die "Fedora ${target_version} netinstall files not found on server (HTTP ${http_code})"
    fi
}

# --- actions ---

do_remove() {
    preflight_checks "remove"

    echo "Removing network install boot entry..."

    if [[ -d "$BOOT_DIR" ]]; then
        rm -rf "$BOOT_DIR"
        echo "  Removed $BOOT_DIR"
    else
        echo "  $BOOT_DIR not found (already clean)"
    fi

    if [[ -f "$GRUB_ENTRY" ]]; then
        rm -f "$GRUB_ENTRY"
        echo "  Removed $GRUB_ENTRY"
    else
        echo "  $GRUB_ENTRY not found (already clean)"
    fi

    grub2-mkconfig -o /boot/grub2/grub.cfg 2>/dev/null
    echo "  GRUB config regenerated"
    echo "Done. Network install entry removed."
}

do_setup() {
    local target_version="$1"
    local vmlinuz_url="${BASE_URL}/${target_version}/Everything/x86_64/os/images/pxeboot/vmlinuz"
    local initrd_url="${BASE_URL}/${target_version}/Everything/x86_64/os/images/pxeboot/initrd.img"
    local repo_url="${BASE_URL}/${target_version}/Everything/x86_64/os/"

    # Clean up any previous install
    if [[ -d "$BOOT_DIR" ]]; then
        echo "Removing previous netinstall files..."
        rm -rf "$BOOT_DIR"
    fi

    mkdir -p "$BOOT_DIR"

    echo "Downloading Fedora ${target_version} kernel and initrd to ${BOOT_DIR}..."
    echo "  vmlinuz..."
    if ! curl -fL -o "${BOOT_DIR}/vmlinuz" "$vmlinuz_url"; then
        rm -rf "$BOOT_DIR"
        die "Failed to download vmlinuz"
    fi

    echo "  initrd.img..."
    if ! curl -fL -o "${BOOT_DIR}/initrd.img" "$initrd_url"; then
        rm -rf "$BOOT_DIR"
        die "Failed to download initrd.img"
    fi

    # Sanity check downloaded files have non-zero size
    if [[ ! -s "${BOOT_DIR}/vmlinuz" ]]; then
        rm -rf "$BOOT_DIR"
        die "Downloaded vmlinuz is empty"
    fi
    if [[ ! -s "${BOOT_DIR}/initrd.img" ]]; then
        rm -rf "$BOOT_DIR"
        die "Downloaded initrd.img is empty"
    fi

    echo "Creating GRUB entry..."
    cat > "$GRUB_ENTRY" << GRUBEOF
#!/bin/bash
cat << 'EOF'
menuentry "Fedora ${target_version} Network Install" {
    linux /fedora-netinstall/vmlinuz inst.repo=${repo_url}
    initrd /fedora-netinstall/initrd.img
}
EOF
GRUBEOF
    chmod +x "$GRUB_ENTRY"

    echo "Regenerating GRUB config..."
    grub2-mkconfig -o /boot/grub2/grub.cfg 2>/dev/null

    echo ""
    echo "=== Setup complete ==="
    echo "Fedora ${target_version} network install boot entry is ready."
    echo ""
    echo "Next steps:"
    echo "  1. Reboot your system"
    echo "  2. In GRUB menu, select 'Fedora ${target_version} Network Install'"
    echo "  3. In Anaconda, go to Network & Host Name to connect WiFi"
    echo "  4. Proceed with installation (you can wipe the entire disk)"
    echo ""
    echo "To remove this boot entry later:"
    echo "  sudo bash $0 --remove"
}

# --- main ---

case "${1:-}" in
    --remove|-r)
        do_remove
        ;;
    --help|-h)
        echo "Usage: sudo bash $0"
        echo "       sudo bash $0 --remove"
        echo ""
        echo "Sets up a GRUB boot entry for the latest Fedora network install."
        exit 0
        ;;
    *)
        if [[ -n "${1:-}" ]]; then
            die "Unknown argument: $1 (use --help for usage)"
        fi

        preflight_checks "setup"

        echo "Fetching latest Fedora version from fedoraproject.org..."
        TARGET_VERSION=$(get_latest_version)
        echo "Latest release: Fedora ${TARGET_VERSION}"

        verify_remote_files "$TARGET_VERSION"
        do_setup "$TARGET_VERSION"
        ;;
esac
