#!/usr/bin/bash
# Set up a GRUB boot entry for Fedora install via local ISO partition
#
# Creates a 2GB ext4 partition (labeled FDINST) by shrinking the LUKS
# container from the end, downloads the Fedora netinstall ISO onto it,
# extracts vmlinuz + initrd to /boot, and creates a GRUB menu entry.
#
# Anaconda boots with inst.stage2=hd:LABEL=FDINST:/fedora-install.iso
# (loads stage2 from local disk — no network needed during dracut).
# WiFi connects interactively in the kickstart %pre for package downloads.
# No USB key or PXE server required.
#
# Usage: sudo bash ./fedora-install/setup-netinstall-boot.bash
#
# Uses the Fedora version from vars/fedora-version.yml to match
# the branch.
#
# After running, reboot and select "Fedora XX Install (ISO)" from GRUB.
# The kickstart TUI will prompt for WiFi, LUKS, and user details.
#
# To remove: sudo bash ./fedora-install/setup-netinstall-boot.bash --remove

set -euo pipefail

# --- constants ---

BOOT_DIR="/boot/fedora-netinstall"
GRUB_ENTRY="/etc/grub.d/40_fedora_netinstall"
BASE_URL="https://download.fedoraproject.org/pub/fedora/linux/releases"
FDINST_LABEL="FDINST"
FDINST_MOUNT="/mnt/fedora-install"
ISO_RELEASE_SUFFIX="1.1"  # Fedora GA release suffix (consistent across releases)
# 2 GiB in 512-byte sectors: 2 * 1024 * 1024 * 1024 / 512
SHRINK_SIZE_GIB=2
SHRINK_SIZE_SECTORS=4194304
MIN_BTRFS_FREE_GIB=5
MIN_ISO_SIZE=$((100 * 1024 * 1024))  # 100MB — sanity check for corrupt downloads

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
KS_SOURCE="${SCRIPT_DIR}/ks.cfg"
VERSION_FILE="${REPO_DIR}/vars/fedora-version.yml"

# --- helpers ---

die() {
    echo "ERROR: $*" >&2
    exit 1
}

get_version_from_file() {
    if [[ ! -f "$VERSION_FILE" ]]; then
        die "Version file not found: $VERSION_FILE"
    fi
    local version
    version=$(grep -E '^fedora_version:' "$VERSION_FILE" | awk '{print $2}' | tr -d "\"'")
    if [[ -z "$version" ]]; then
        die "Could not read fedora_version from $VERSION_FILE"
    fi
    echo "$version"
}

# --- device detection ---

# Find the device-mapper path for the root filesystem (e.g. /dev/mapper/luks-<uuid>)
find_root_dm_path() {
    local root_source
    root_source=$(findmnt -no SOURCE / 2>/dev/null) || die "Cannot determine root filesystem device"
    if [[ ! "$root_source" =~ ^/dev/mapper/ ]]; then
        die "Root filesystem is not on device-mapper (LUKS required). Got: $root_source"
    fi
    echo "$root_source"
}

# Extract dm name from path (e.g. /dev/mapper/luks-xxx → luks-xxx)
find_dm_name() {
    basename "$1"
}

# Find the physical partition backing a LUKS device (e.g. /dev/nvme0n1p3)
find_luks_backing_partition() {
    local dm_name="$1"
    local backing
    backing=$(cryptsetup status "$dm_name" 2>/dev/null | awk '/device:/{print $2}')
    if [[ -z "$backing" ]]; then
        die "Cannot determine backing device for LUKS: $dm_name"
    fi
    echo "$backing"
}

# Find the parent disk of a partition (e.g. /dev/nvme0n1p3 → /dev/nvme0n1)
find_parent_disk() {
    local partition="$1"
    local parent
    parent=$(lsblk -no PKNAME "$partition" 2>/dev/null | head -1)
    if [[ -z "$parent" ]]; then
        die "Cannot determine parent disk for: $partition"
    fi
    echo "/dev/${parent}"
}

# Get the partition number from a partition device (e.g. /dev/nvme0n1p3 → 3)
find_partition_number() {
    local partition="$1"
    local part_num
    part_num=$(cat "/sys/class/block/$(basename "$partition")/partition" 2>/dev/null) \
        || die "Cannot determine partition number for: $partition"
    echo "$part_num"
}

# Construct partition device path from disk + number
# NVMe: /dev/nvme0n1 + 4 → /dev/nvme0n1p4
# SATA: /dev/sda + 4 → /dev/sda4
partition_device_path() {
    local disk="$1"
    local num="$2"
    if [[ "$disk" =~ [0-9]$ ]]; then
        echo "${disk}p${num}"
    else
        echo "${disk}${num}"
    fi
}

# Find partition with FDINST filesystem label (returns device path or empty)
find_fdinst_partition() {
    blkid -L "$FDINST_LABEL" 2>/dev/null || true
}

# --- preflight ---

preflight_checks() {
    local mode="$1"

    if [[ $EUID -ne 0 ]]; then
        die "This script must be run as root (use sudo)"
    fi

    local required_cmds=(curl grub2-mkconfig blkid lsblk findmnt)
    if [[ "$mode" == "setup" ]]; then
        required_cmds+=(parted mkfs.ext4 cryptsetup btrfs mount umount partprobe blockdev)
    elif [[ "$mode" == "remove" ]]; then
        required_cmds+=(parted cryptsetup btrfs partprobe blockdev)
    fi

    for cmd in "${required_cmds[@]}"; do
        if ! command -v "$cmd" &>/dev/null; then
            die "Required command not found: $cmd"
        fi
    done

    if [[ ! -d /etc/grub.d ]]; then
        die "/etc/grub.d does not exist. Is GRUB installed?"
    fi

    if ! mountpoint -q /boot 2>/dev/null && [[ ! -d /boot/grub2 ]]; then
        die "/boot does not appear to be mounted or configured"
    fi

    if [[ "$mode" == "setup" ]]; then
        # Verify LUKS root exists
        local root_source
        root_source=$(findmnt -no SOURCE / 2>/dev/null) || true
        if [[ ! "$root_source" =~ ^/dev/mapper/ ]]; then
            die "Root filesystem is not on LUKS (device-mapper). Got: ${root_source:-unknown}"
        fi

        # Check Btrfs free space (need MIN_BTRFS_FREE_GIB before attempting SHRINK_SIZE_GIB shrink)
        local btrfs_avail_bytes
        btrfs_avail_bytes=$(btrfs filesystem usage -b / 2>/dev/null \
            | awk '/Free \(estimated\):/{print $3}') || true
        if [[ -z "$btrfs_avail_bytes" ]] || [[ ! "$btrfs_avail_bytes" =~ ^[0-9]+$ ]]; then
            local avail_kb
            avail_kb=$(df --output=avail / | tail -1 | tr -d ' ')
            btrfs_avail_bytes=$(( avail_kb * 1024 ))
        fi
        local min_bytes=$(( MIN_BTRFS_FREE_GIB * 1024 * 1024 * 1024 ))
        if [[ "$btrfs_avail_bytes" -lt "$min_bytes" ]]; then
            die "Btrfs has less than ${MIN_BTRFS_FREE_GIB}GiB free ($(( btrfs_avail_bytes / 1024 / 1024 / 1024 ))GiB). Need ${MIN_BTRFS_FREE_GIB}GiB minimum."
        fi

        # Remove previous boot files before /boot space check
        if [[ -d "$BOOT_DIR" ]]; then
            rm -rf "$BOOT_DIR"
        fi

        # Check /boot has enough space (~300MB for vmlinuz + initrd.img + ks.cfg)
        local boot_avail_kb
        boot_avail_kb=$(df --output=avail /boot | tail -1 | tr -d ' ')
        if [[ "$boot_avail_kb" -lt 307200 ]]; then
            die "/boot has less than 300MB free ($(( boot_avail_kb / 1024 ))MB available). Need ~300MB."
        fi
    fi
}

# --- partition operations ---

# Shrink Btrfs→LUKS→partition from the end, create new FDINST partition in freed space
create_fdinst_partition() {
    local disk="$1"
    local luks_part_num="$2"
    local dm_name="$3"
    local new_part_num=$(( luks_part_num + 1 ))

    # Verify no partition exists after the LUKS partition
    local max_part
    max_part=$(parted -ms "$disk" unit s print 2>/dev/null \
        | grep -E '^[0-9]+:' | tail -1 | cut -d: -f1)
    if [[ "$max_part" -gt "$luks_part_num" ]]; then
        die "Partition ${max_part} already exists after LUKS partition ${luks_part_num}. Cannot create FDINST."
    fi

    # Get current LUKS partition end sector
    local part_line
    part_line=$(parted -ms "$disk" unit s print 2>/dev/null | grep "^${luks_part_num}:")
    if [[ -z "$part_line" ]]; then
        die "Cannot find partition ${luks_part_num} in parted output"
    fi
    local old_end
    old_end=$(echo "$part_line" | cut -d: -f3 | tr -d 's')

    local new_end=$(( old_end - SHRINK_SIZE_SECTORS ))
    local new_start=$(( new_end + 1 ))

    echo "  Partition layout change:"
    echo "    Partition ${luks_part_num}: end ${old_end}s -> ${new_end}s (shrink ${SHRINK_SIZE_GIB}GiB)"
    echo "    Partition ${new_part_num}: ${new_start}s -> end of disk (new, ${SHRINK_SIZE_GIB}GiB, ext4)"

    # Step 1: Shrink Btrfs (innermost layer)
    echo "  Step 1/5: Shrinking Btrfs by ${SHRINK_SIZE_GIB}GiB..."
    if ! btrfs filesystem resize "-${SHRINK_SIZE_GIB}g" /; then
        die "Failed to shrink Btrfs filesystem"
    fi

    # Step 2: Shrink LUKS dm device
    echo "  Step 2/5: Shrinking LUKS container..."
    local current_dm_sectors
    current_dm_sectors=$(blockdev --getsz "/dev/mapper/${dm_name}")
    local new_dm_sectors=$(( current_dm_sectors - SHRINK_SIZE_SECTORS ))
    if ! cryptsetup resize "$dm_name" --size "$new_dm_sectors"; then
        echo "  ROLLBACK: Restoring Btrfs size..."
        btrfs filesystem resize max / 2>/dev/null || true
        die "Failed to shrink LUKS container"
    fi

    # Step 3: Shrink partition (outermost layer)
    echo "  Step 3/5: Shrinking partition ${luks_part_num}..."
    if ! parted -s "$disk" resizepart "$luks_part_num" "${new_end}s"; then
        echo "  ROLLBACK: Restoring LUKS and Btrfs..."
        cryptsetup resize "$dm_name" 2>/dev/null || true
        btrfs filesystem resize max / 2>/dev/null || true
        die "Failed to shrink partition ${luks_part_num}"
    fi

    # Step 4: Create new partition in freed space
    echo "  Step 4/5: Creating partition ${new_part_num}..."
    if ! parted -s "$disk" mkpart "$FDINST_LABEL" ext4 "${new_start}s" 100%; then
        echo "  ROLLBACK: Restoring partition, LUKS, and Btrfs..."
        parted -s "$disk" resizepart "$luks_part_num" "${old_end}s" 2>/dev/null || true
        cryptsetup resize "$dm_name" 2>/dev/null || true
        btrfs filesystem resize max / 2>/dev/null || true
        die "Failed to create partition ${new_part_num}"
    fi
    partprobe "$disk"
    udevadm settle

    # Step 5: Format as ext4 with FDINST label
    local new_part_dev
    new_part_dev=$(partition_device_path "$disk" "$new_part_num")
    echo "  Step 5/5: Formatting ${new_part_dev} as ext4 (label=${FDINST_LABEL})..."
    if ! mkfs.ext4 -L "$FDINST_LABEL" -q "$new_part_dev"; then
        echo "  ROLLBACK: Removing partition and restoring..."
        parted -s "$disk" rm "$new_part_num" 2>/dev/null || true
        partprobe "$disk" 2>/dev/null || true
        parted -s "$disk" resizepart "$luks_part_num" "${old_end}s" 2>/dev/null || true
        cryptsetup resize "$dm_name" 2>/dev/null || true
        btrfs filesystem resize max / 2>/dev/null || true
        die "Failed to format ${new_part_dev}"
    fi

    echo "  FDINST partition created: ${new_part_dev}"
}

# Remove FDINST partition and grow LUKS back to fill the disk
remove_fdinst_partition() {
    local fdinst_dev="$1"
    local disk="$2"
    local luks_part_num="$3"
    local dm_name="$4"

    local fdinst_part_num
    fdinst_part_num=$(find_partition_number "$fdinst_dev")

    echo "  Removing FDINST partition (${fdinst_dev})..."

    # Unmount if mounted
    if mountpoint -q "$FDINST_MOUNT" 2>/dev/null; then
        umount "$FDINST_MOUNT"
    fi
    if findmnt -rno TARGET "$fdinst_dev" &>/dev/null; then
        umount "$fdinst_dev" 2>/dev/null || true
    fi

    # Step 1: Remove FDINST partition
    echo "  Step 1/4: Removing partition ${fdinst_part_num}..."
    parted -s "$disk" rm "$fdinst_part_num"
    partprobe "$disk"

    # Step 2: Grow LUKS partition to fill disk
    echo "  Step 2/4: Growing partition ${luks_part_num} to fill disk..."
    parted -s "$disk" resizepart "$luks_part_num" 100%
    partprobe "$disk"

    # Step 3: Grow LUKS dm device to fill partition (no --size = fill)
    echo "  Step 3/4: Growing LUKS container..."
    cryptsetup resize "$dm_name"

    # Step 4: Grow Btrfs to fill LUKS
    echo "  Step 4/4: Growing Btrfs filesystem..."
    btrfs filesystem resize max /

    echo "  Disk space restored."

    rmdir "$FDINST_MOUNT" 2>/dev/null || true
}

mount_fdinst() {
    local fdinst_dev="$1"
    mkdir -p "$FDINST_MOUNT"
    if ! mountpoint -q "$FDINST_MOUNT" 2>/dev/null; then
        mount "$fdinst_dev" "$FDINST_MOUNT"
    fi
}

unmount_fdinst() {
    if mountpoint -q "$FDINST_MOUNT" 2>/dev/null; then
        umount "$FDINST_MOUNT"
    fi
}

# --- ISO operations ---

construct_iso_url() {
    local version="$1"
    local filename="Fedora-Everything-netinst-x86_64-${version}-${ISO_RELEASE_SUFFIX}.iso"
    echo "${BASE_URL}/${version}/Everything/x86_64/iso/${filename}"
}

verify_iso_url() {
    local url="$1"
    echo "Verifying ISO exists on server..."
    local http_code
    http_code=$(curl -sIL -o /dev/null -w '%{http_code}' --max-time 30 "$url" 2>/dev/null || echo "000")
    if [[ "$http_code" != "200" ]]; then
        die "ISO not found at: $url (HTTP ${http_code}). Check Fedora version and release suffix."
    fi
}

download_iso() {
    local iso_url="$1"
    local iso_dest="${FDINST_MOUNT}/fedora-install.iso"
    local temp_iso="${iso_dest}.partial"

    echo "Downloading ISO (skips if local copy is current)..."
    rm -f "$temp_iso"

    # curl -z: sends If-Modified-Since header based on local file timestamp
    # If server returns 304 Not Modified, output file is empty (or not written)
    if ! curl -fL --progress-bar -z "$iso_dest" -o "$temp_iso" "$iso_url"; then
        rm -f "$temp_iso"
        die "Failed to download ISO from: $iso_url"
    fi

    if [[ -s "$temp_iso" ]]; then
        # New file downloaded — verify minimum size
        local size
        size=$(stat -c%s "$temp_iso")
        if [[ "$size" -lt "$MIN_ISO_SIZE" ]]; then
            rm -f "$temp_iso"
            die "Downloaded ISO too small (${size} bytes, expected >=100MB). Possible download error."
        fi
        mv "$temp_iso" "$iso_dest"
        echo "  Downloaded ISO: $(( size / 1024 / 1024 ))MB"
    else
        rm -f "$temp_iso"
        if [[ -f "$iso_dest" ]]; then
            echo "  ISO is up to date (not modified on server)"
        else
            die "ISO download produced no output and no local copy exists"
        fi
    fi
}

extract_boot_files() {
    local iso_path="${FDINST_MOUNT}/fedora-install.iso"
    local iso_mount
    iso_mount=$(mktemp -d)

    # Clean and recreate boot directory
    rm -rf "$BOOT_DIR"
    mkdir -p "$BOOT_DIR"

    echo "Extracting vmlinuz and initrd.img from ISO..."
    if ! mount -o loop,ro "$iso_path" "$iso_mount"; then
        rmdir "$iso_mount"
        die "Failed to loop-mount ISO: $iso_path"
    fi

    local vmlinuz_src="${iso_mount}/images/pxeboot/vmlinuz"
    local initrd_src="${iso_mount}/images/pxeboot/initrd.img"

    if [[ ! -f "$vmlinuz_src" ]]; then
        umount "$iso_mount"
        rmdir "$iso_mount"
        die "vmlinuz not found in ISO at images/pxeboot/vmlinuz"
    fi
    if [[ ! -f "$initrd_src" ]]; then
        umount "$iso_mount"
        rmdir "$iso_mount"
        die "initrd.img not found in ISO at images/pxeboot/initrd.img"
    fi

    cp "$vmlinuz_src" "${BOOT_DIR}/vmlinuz"
    cp "$initrd_src" "${BOOT_DIR}/initrd.img"

    umount "$iso_mount"
    rmdir "$iso_mount"

    # Verify copies are non-empty
    if [[ ! -s "${BOOT_DIR}/vmlinuz" ]]; then
        die "Extracted vmlinuz is empty"
    fi
    if [[ ! -s "${BOOT_DIR}/initrd.img" ]]; then
        die "Extracted initrd.img is empty"
    fi

    local vmlinuz_size initrd_size
    vmlinuz_size=$(stat -c%s "${BOOT_DIR}/vmlinuz")
    initrd_size=$(stat -c%s "${BOOT_DIR}/initrd.img")
    echo "  vmlinuz: $(( vmlinuz_size / 1024 / 1024 ))MB"
    echo "  initrd.img: $(( initrd_size / 1024 / 1024 ))MB"
}

# --- main actions ---

do_setup() {
    local target_version="$1"

    local iso_url
    iso_url=$(construct_iso_url "$target_version")
    verify_iso_url "$iso_url"

    # Detect disk layout: root dm → LUKS backing partition → parent disk
    local dm_path dm_name luks_part disk luks_part_num
    dm_path=$(find_root_dm_path)
    dm_name=$(find_dm_name "$dm_path")
    luks_part=$(find_luks_backing_partition "$dm_name")
    disk=$(find_parent_disk "$luks_part")
    luks_part_num=$(find_partition_number "$luks_part")

    echo "Detected layout:"
    echo "  Root DM:    $dm_path"
    echo "  LUKS part:  $luks_part (partition ${luks_part_num} on ${disk})"
    echo ""

    # Find or create FDINST partition
    local fdinst_dev
    fdinst_dev=$(find_fdinst_partition)

    if [[ -n "$fdinst_dev" ]]; then
        echo "FDINST partition found: $fdinst_dev (skipping creation)"
    else
        echo "FDINST partition not found. Creating..."
        create_fdinst_partition "$disk" "$luks_part_num" "$dm_name"
        fdinst_dev=$(find_fdinst_partition)
        if [[ -z "$fdinst_dev" ]]; then
            die "FDINST partition was created but cannot be found by label"
        fi
    fi

    # Mount FDINST partition
    mount_fdinst "$fdinst_dev"

    # Download ISO (curl -z skips if local copy is current)
    download_iso "$iso_url"

    # Extract vmlinuz + initrd.img from ISO to /boot
    extract_boot_files

    # Copy kickstart file to /boot
    if [[ ! -f "$KS_SOURCE" ]]; then
        die "Kickstart file not found: $KS_SOURCE"
    fi
    echo "Copying kickstart file..."
    cp "$KS_SOURCE" "${BOOT_DIR}/ks.cfg"
    sed -i "s/^SETUP_BRANCH=.*/SETUP_BRANCH=\"F${target_version}\"/" "${BOOT_DIR}/ks.cfg"
    sed -i "s|releases/[0-9]*/Everything|releases/${target_version}/Everything|" "${BOOT_DIR}/ks.cfg"

    # Detect /boot UUID for inst.ks= (Anaconda needs hd:UUID= to find local files)
    local boot_source boot_uuid
    boot_source=$(findmnt -no SOURCE /boot 2>/dev/null) || die "Cannot determine /boot device"
    boot_uuid=$(blkid -s UUID -o value "$boot_source" 2>/dev/null) || die "Cannot determine /boot UUID"

    # Create GRUB entry
    echo "Creating GRUB entry..."
    cat > "$GRUB_ENTRY" << GRUBEOF
#!/bin/bash
cat << 'EOF'
menuentry "Fedora ${target_version} Install (ISO)" {
    linux /fedora-netinstall/vmlinuz inst.stage2=hd:LABEL=${FDINST_LABEL}:/fedora-install.iso inst.ks=hd:UUID=${boot_uuid}:/fedora-netinstall/ks.cfg inst.text
    initrd /fedora-netinstall/initrd.img
}
EOF
GRUBEOF
    chmod +x "$GRUB_ENTRY"

    # Ensure GRUB menu is visible (not auto-hidden)
    echo "Disabling GRUB menu auto-hide..."
    grub2-editenv - unset menu_auto_hide
    grub2-editenv - unset menu_hide_ok

    echo "Regenerating GRUB config..."
    grub2-mkconfig -o /boot/grub2/grub.cfg 2>/dev/null

    # Unmount FDINST
    unmount_fdinst

    echo ""
    echo "=== Setup complete ==="
    echo "Fedora ${target_version} install boot entry is ready."
    echo ""
    echo "Disk layout:"
    echo "  ${BOOT_DIR}/vmlinuz     — kernel"
    echo "  ${BOOT_DIR}/initrd.img  — initial ramdisk"
    echo "  ${BOOT_DIR}/ks.cfg      — kickstart"
    echo "  ${fdinst_dev} (FDINST)  — ISO partition"
    echo ""
    echo "Next steps:"
    echo "  1. Reboot your system"
    echo "  2. In GRUB menu, select 'Fedora ${target_version} Install (ISO)'"
    echo "  3. The kickstart TUI will prompt for WiFi, LUKS, and user details"
    echo "  4. Installation proceeds automatically after the prompts"
    echo ""
    echo "To remove this boot entry and reclaim disk space:"
    echo "  sudo bash $0 --remove"
}

do_remove() {
    preflight_checks "remove"

    echo "Removing install boot entry..."

    # Remove boot files
    if [[ -d "$BOOT_DIR" ]]; then
        rm -rf "$BOOT_DIR"
        echo "  Removed $BOOT_DIR"
    else
        echo "  $BOOT_DIR not found (already clean)"
    fi

    # Remove GRUB entry script
    if [[ -f "$GRUB_ENTRY" ]]; then
        rm -f "$GRUB_ENTRY"
        echo "  Removed $GRUB_ENTRY"
    else
        echo "  $GRUB_ENTRY not found (already clean)"
    fi

    # Remove FDINST partition and reclaim space
    local fdinst_dev
    fdinst_dev=$(find_fdinst_partition)

    if [[ -n "$fdinst_dev" ]]; then
        local dm_path dm_name luks_part disk luks_part_num
        dm_path=$(find_root_dm_path)
        dm_name=$(find_dm_name "$dm_path")
        luks_part=$(find_luks_backing_partition "$dm_name")
        disk=$(find_parent_disk "$luks_part")
        luks_part_num=$(find_partition_number "$luks_part")

        remove_fdinst_partition "$fdinst_dev" "$disk" "$luks_part_num" "$dm_name"
    else
        echo "  FDINST partition not found (already clean)"
    fi

    # Regenerate GRUB config
    grub2-mkconfig -o /boot/grub2/grub.cfg 2>/dev/null
    echo "  GRUB config regenerated"

    echo ""
    echo "Done. Install entry removed and disk space reclaimed."
}

# --- entry point ---

case "${1:-}" in
    --remove|-r)
        do_remove
        ;;
    --help|-h)
        echo "Usage: sudo bash $0"
        echo "       sudo bash $0 --remove"
        echo ""
        echo "Sets up a GRUB boot entry for Fedora install from a local ISO."
        echo "Creates a 2GB partition for the ISO, extracts boot files to /boot,"
        echo "and configures GRUB. No USB key or PXE server needed."
        echo ""
        echo "Uses the version from vars/fedora-version.yml."
        exit 0
        ;;
    *)
        if [[ -n "${1:-}" ]]; then
            die "Unknown argument: $1 (use --help for usage)"
        fi

        preflight_checks "setup"

        TARGET_VERSION=$(get_version_from_file)
        echo "Target version: Fedora ${TARGET_VERSION} (from vars/fedora-version.yml)"
        echo ""

        do_setup "$TARGET_VERSION"
        ;;
esac
