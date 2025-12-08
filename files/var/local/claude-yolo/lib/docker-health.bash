#!/bin/bash
# Claude YOLO Docker Health Library
# Handles zombie container detection, overlay2 migration, and docker health checks
#
# Version: 1.0.0

# Minimum kernel version for native overlay2 with rootless Docker + SELinux
readonly MIN_KERNEL_OVERLAY2="5.13.0"

# ═══════════════════════════════════════════════════════════════════════════════
# ZOMBIE CONTAINER DETECTION
# ═══════════════════════════════════════════════════════════════════════════════

# Find CCY containers that have lost their controlling terminal
# These are containers started with -it but whose docker run process has died
# Returns: newline-separated list of container names
find_zombie_containers() {
    local suffix="${1:-yolo}"  # Default to "yolo" containers
    local zombies=()

    # Get all running containers matching the suffix pattern
    local containers=$(docker ps --filter "name=_${suffix}" --format '{{.Names}}' 2>/dev/null)

    if [ -z "$containers" ]; then
        return 0  # No containers found
    fi

    while IFS= read -r container_name; do
        [ -z "$container_name" ] && continue

        # Check if this container was started with -it (TTY mode)
        local has_tty=$(docker inspect --format '{{.Config.Tty}}' "$container_name" 2>/dev/null)

        if [ "$has_tty" = "true" ]; then
            # Check if there's a docker run process still attached
            # The docker run process would have the container name in its command line
            if ! pgrep -f "docker run.*--name[= ]${container_name}[^_]" >/dev/null 2>&1 && \
               ! pgrep -f "docker run.*--name[= ]${container_name}$" >/dev/null 2>&1; then
                zombies+=("$container_name")
            fi
        fi
    done <<< "$containers"

    printf '%s\n' "${zombies[@]}"
}

# Get detailed stats for a container
# Args: container_name
# Returns: formatted stats string
get_container_stats() {
    local container_name="$1"

    # Get stats in one call
    local stats=$(docker stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}' "$container_name" 2>/dev/null)

    if [ -z "$stats" ]; then
        echo "unknown|unknown|unknown"
        return 1
    fi

    echo "$stats"
}

# Get container uptime/age
# Args: container_name
# Returns: human-readable uptime string
get_container_uptime() {
    local container_name="$1"
    docker ps --filter "name=^${container_name}$" --format '{{.RunningFor}}' 2>/dev/null
}

# Show zombie container management TUI
# Args: suffix (default: "yolo")
# Returns: 0 if zombies were handled or none found, 1 if user cancelled
show_zombie_container_tui() {
    local suffix="${1:-yolo}"
    local zombies=()

    # Find zombie containers
    while IFS= read -r zombie; do
        [ -n "$zombie" ] && zombies+=("$zombie")
    done < <(find_zombie_containers "$suffix")

    if [ ${#zombies[@]} -eq 0 ]; then
        return 0  # No zombies found
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "⚠️  Orphaned Containers Detected"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Found ${#zombies[@]} container(s) running without an attached terminal."
    echo "These containers are consuming resources but have no active session."
    echo ""
    echo "This typically happens when:"
    echo "  • The terminal running ccy was closed"
    echo "  • The SSH connection was lost"
    echo "  • The parent process was killed"
    echo ""

    # Build table header
    printf "%-4s %-30s %-10s %-20s %-10s\n" "#" "CONTAINER" "CPU" "MEMORY" "UPTIME"
    echo "────────────────────────────────────────────────────────────────────────────────"

    local i=1
    for container in "${zombies[@]}"; do
        local stats=$(get_container_stats "$container")
        local cpu=$(echo "$stats" | cut -d'|' -f1)
        local mem=$(echo "$stats" | cut -d'|' -f2)
        local uptime=$(get_container_uptime "$container")

        printf "%-4s %-30s %-10s %-20s %-10s\n" "$i)" "$container" "$cpu" "$mem" "$uptime"
        i=$((i + 1))
    done

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Options:"
    echo "  [a] Stop ALL orphaned containers (recommended)"
    echo "  [s] Select specific containers to stop"
    echo "  [i] Ignore and continue (containers will keep running)"
    echo "  [q] Quit without starting new session"
    echo ""

    while true; do
        read -p "Choice [a/s/i/q]: " choice
        echo ""

        case "$choice" in
            a|A)
                echo "Stopping all orphaned containers..."
                for container in "${zombies[@]}"; do
                    echo -n "  Stopping $container... "
                    if docker stop "$container" >/dev/null 2>&1; then
                        echo "✓"
                    else
                        echo "✗ (may already be stopped)"
                    fi
                done
                echo ""
                echo "✓ All orphaned containers stopped"
                return 0
                ;;
            s|S)
                echo "Enter container numbers to stop (space-separated, e.g., '1 3 4'):"
                read -p "> " selections

                for sel in $selections; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#zombies[@]} ]; then
                        local container="${zombies[$((sel-1))]}"
                        echo -n "  Stopping $container... "
                        if docker stop "$container" >/dev/null 2>&1; then
                            echo "✓"
                        else
                            echo "✗ (may already be stopped)"
                        fi
                    else
                        echo "  Invalid selection: $sel (skipped)"
                    fi
                done
                echo ""
                return 0
                ;;
            i|I)
                echo "Continuing without stopping orphaned containers."
                echo "Note: These containers will continue consuming CPU/memory."
                echo ""
                return 0
                ;;
            q|Q)
                echo "Exiting."
                return 1
                ;;
            *)
                echo "Invalid choice. Please enter a, s, i, or q."
                echo ""
                ;;
        esac
    done
}

# ═══════════════════════════════════════════════════════════════════════════════
# OVERLAY2 MIGRATION SUPPORT
# ═══════════════════════════════════════════════════════════════════════════════

# Check if kernel supports native overlay2 for rootless containers
# Returns: 0 if supported, 1 if not
check_overlay2_kernel_support() {
    local kernel_version=$(uname -r | cut -d'-' -f1)

    # Compare versions
    if version_greater_than "$kernel_version" "$MIN_KERNEL_OVERLAY2" || \
       [ "$kernel_version" = "$MIN_KERNEL_OVERLAY2" ]; then
        return 0
    fi

    return 1
}

# Check if overlay kernel module is loaded
# Returns: 0 if loaded, 1 if not
check_overlay_module() {
    lsmod | grep -q "^overlay " 2>/dev/null
}

# Check current docker storage driver
# Returns: storage driver name (e.g., "overlay2", "fuse-overlayfs")
get_docker_storage_driver() {
    docker info --format '{{.Driver}}' 2>/dev/null || echo "unknown"
}

# Check if rootless docker is using native overlay2
# Returns: 0 if using native overlay2, 1 otherwise
is_using_native_overlay2() {
    local driver=$(get_docker_storage_driver)
    [ "$driver" = "overlay2" ]
}

# Check if user has any existing docker data that would need migration
# Returns: 0 if data exists, 1 if clean
has_docker_data() {
    local docker_root="${HOME}/.local/share/docker"

    # Check for any storage directory
    if [ -d "${docker_root}/overlay2" ] || [ -d "${docker_root}/fuse-overlayfs" ]; then
        # Check if there are actually images/containers
        local image_count=$(docker images -q 2>/dev/null | wc -l)
        local container_count=$(docker ps -aq 2>/dev/null | wc -l)

        if [ "$image_count" -gt 0 ] || [ "$container_count" -gt 0 ]; then
            return 0  # Has data
        fi
    fi

    return 1  # No significant data
}

# Get size of docker data directory
# Returns: human-readable size string
get_docker_data_size() {
    local docker_root="${HOME}/.local/share/docker"
    du -sh "$docker_root" 2>/dev/null | cut -f1 || echo "unknown"
}

# Show overlay2 migration TUI
# This is called from Ansible or manually to guide migration
# Returns: 0 if migration completed/skipped, 1 if cancelled
show_overlay2_migration_tui() {
    local current_driver=$(get_docker_storage_driver)

    # Already on overlay2
    if [ "$current_driver" = "overlay2" ]; then
        echo "✓ Docker is already using native overlay2 storage driver"
        return 0
    fi

    # Check kernel support
    if ! check_overlay2_kernel_support; then
        local kernel_version=$(uname -r)
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "⚠️  Native overlay2 Not Supported"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        echo "Your kernel ($kernel_version) does not support native overlay2 for rootless Docker."
        echo "Required: kernel $MIN_KERNEL_OVERLAY2 or later"
        echo ""
        echo "You will continue using fuse-overlayfs, which works but has higher CPU overhead."
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        return 0
    fi

    # Check overlay module
    if ! check_overlay_module; then
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "⚠️  Overlay Kernel Module Not Loaded"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        echo "The overlay kernel module is not loaded."
        echo "This may require running: sudo modprobe overlay"
        echo ""
        echo "You will continue using fuse-overlayfs for now."
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        return 0
    fi

    # Kernel supports overlay2 but docker is using fuse-overlayfs
    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "🚀 Native overlay2 Available"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Your system supports native overlay2, which is faster than fuse-overlayfs."
    echo ""
    echo "Current driver:  $current_driver"
    echo "Recommended:     overlay2"
    echo ""
    echo "Benefits of native overlay2:"
    echo "  • Significantly lower CPU usage (no FUSE overhead)"
    echo "  • Faster container startup"
    echo "  • Better I/O performance"
    echo ""

    if has_docker_data; then
        local data_size=$(get_docker_data_size)
        local image_count=$(docker images -q 2>/dev/null | wc -l)
        local container_count=$(docker ps -aq 2>/dev/null | wc -l)

        echo "⚠️  Migration Required"
        echo ""
        echo "Switching storage drivers requires removing existing Docker data:"
        echo "  • Images: $image_count"
        echo "  • Containers: $container_count (including stopped)"
        echo "  • Data size: $data_size"
        echo ""
        echo "After migration, you will need to:"
        echo "  • Rebuild container images (ccy --rebuild)"
        echo "  • Pull any Docker Hub images again"
        echo ""
        echo "Options:"
        echo "  [m] Migrate to overlay2 (will delete all Docker data)"
        echo "  [s] Skip migration (continue using $current_driver)"
        echo "  [q] Quit"
        echo ""

        while true; do
            read -p "Choice [m/s/q]: " choice
            echo ""

            case "$choice" in
                m|M)
                    echo "Starting migration to overlay2..."
                    echo ""

                    # Stop docker
                    echo "Stopping rootless Docker..."
                    systemctl --user stop docker.service docker.socket 2>/dev/null || true

                    # Remove docker data
                    echo "Removing Docker data..."
                    rm -rf "${HOME}/.local/share/docker"

                    # Create daemon.json with overlay2
                    mkdir -p "${HOME}/.config/docker"
                    cat > "${HOME}/.config/docker/daemon.json" <<'EOF'
{
  "storage-driver": "overlay2"
}
EOF
                    echo "Created ~/.config/docker/daemon.json with overlay2 driver"

                    # Start docker
                    echo "Starting rootless Docker..."
                    systemctl --user start docker.socket docker.service

                    # Verify
                    sleep 2
                    local new_driver=$(get_docker_storage_driver)
                    if [ "$new_driver" = "overlay2" ]; then
                        echo ""
                        echo "✓ Migration successful! Docker is now using native overlay2"
                        echo ""
                        echo "Next steps:"
                        echo "  • Run 'ccy --rebuild' to rebuild the CCY container"
                        echo "  • Pull any other images you need"
                    else
                        echo ""
                        echo "⚠️  Migration may have failed. Current driver: $new_driver"
                        echo "Check: docker info | grep 'Storage Driver'"
                    fi

                    return 0
                    ;;
                s|S)
                    echo "Skipping migration. Continuing with $current_driver."
                    return 0
                    ;;
                q|Q)
                    echo "Cancelled."
                    return 1
                    ;;
                *)
                    echo "Invalid choice. Please enter m, s, or q."
                    echo ""
                    ;;
            esac
        done
    else
        # No data to migrate - easy path
        echo "No existing Docker data found. Clean migration is possible."
        echo ""
        echo "Options:"
        echo "  [m] Switch to overlay2 (recommended)"
        echo "  [s] Skip (continue using $current_driver)"
        echo ""

        while true; do
            read -p "Choice [m/s]: " choice
            echo ""

            case "$choice" in
                m|M)
                    echo "Configuring overlay2..."

                    # Stop docker
                    systemctl --user stop docker.service docker.socket 2>/dev/null || true

                    # Remove any existing storage dir
                    rm -rf "${HOME}/.local/share/docker"

                    # Create daemon.json
                    mkdir -p "${HOME}/.config/docker"
                    cat > "${HOME}/.config/docker/daemon.json" <<'EOF'
{
  "storage-driver": "overlay2"
}
EOF

                    # Start docker
                    systemctl --user start docker.socket docker.service

                    sleep 2
                    local new_driver=$(get_docker_storage_driver)
                    if [ "$new_driver" = "overlay2" ]; then
                        echo "✓ Docker configured with native overlay2"
                    else
                        echo "⚠️  Configuration may have failed. Current driver: $new_driver"
                    fi

                    return 0
                    ;;
                s|S)
                    echo "Skipping. Continuing with $current_driver."
                    return 0
                    ;;
                *)
                    echo "Invalid choice. Please enter m or s."
                    echo ""
                    ;;
            esac
        done
    fi
}

# Quick check for zombie containers at startup
# This is meant to be called early in ccy startup
# Returns: 0 to continue, 1 to abort
check_zombie_containers_startup() {
    local suffix="${1:-yolo}"

    # Get zombie list into array, filtering empty lines
    local zombies=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && zombies+=("$line")
    done < <(find_zombie_containers "$suffix")

    local zombie_count=${#zombies[@]}

    if [ "$zombie_count" -gt 0 ]; then
        show_zombie_container_tui "$suffix"
        return $?
    fi

    return 0
}

# ═══════════════════════════════════════════════════════════════════════════════
# CCY TOP - CONTAINER MANAGEMENT TUI
# ═══════════════════════════════════════════════════════════════════════════════

# Show all CCY containers with stats and management options
# This is a global command - works from anywhere, not just git repos
# Args: suffix (default: "yolo")
show_container_top() {
    local suffix="${1:-yolo}"

    # Get all containers matching the suffix
    local containers=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && containers+=("$line")
    done < <(docker ps --filter "name=_${suffix}" --format '{{.Names}}' 2>/dev/null)

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "CCY Container Manager"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""

    if [ ${#containers[@]} -eq 0 ]; then
        echo "No CCY containers currently running."
        echo ""
        echo "Start a new session with: ccy"
        echo "════════════════════════════════════════════════════════════════════════════════"
        return 0
    fi

    echo "Running containers: ${#containers[@]}"
    echo ""

    # Build table header
    printf "%-3s %-28s %-8s %-18s %-12s %-8s\n" "#" "CONTAINER" "CPU" "MEMORY" "UPTIME" "STATUS"
    echo "────────────────────────────────────────────────────────────────────────────────"

    local i=1
    for container in "${containers[@]}"; do
        local stats=$(get_container_stats "$container")
        local cpu=$(echo "$stats" | cut -d'|' -f1)
        local mem=$(echo "$stats" | cut -d'|' -f2 | cut -d'/' -f1)  # Just usage, not limit
        local uptime=$(get_container_uptime "$container")

        # Check if zombie (no docker run process attached)
        local status="active"
        local has_tty=$(docker inspect --format '{{.Config.Tty}}' "$container" 2>/dev/null)
        if [ "$has_tty" = "true" ]; then
            if ! pgrep -f "docker run.*--name[= ]${container}( |$)" >/dev/null 2>&1; then
                status="orphan?"
            fi
        fi

        printf "%-3s %-28s %-8s %-18s %-12s %-8s\n" "$i)" "$container" "$cpu" "$mem" "$uptime" "$status"
        i=$((i + 1))
    done

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Actions:"
    echo "  [number]  Stop specific container (e.g., '1' or '1 2 3')"
    echo "  [a]       Stop ALL containers"
    echo "  [r]       Refresh stats"
    echo "  [q]       Quit"
    echo ""

    while true; do
        read -p "Action: " action
        echo ""

        case "$action" in
            q|Q|quit|exit)
                return 0
                ;;
            r|R|refresh)
                # Recursive call to refresh
                show_container_top "$suffix"
                return $?
                ;;
            a|A|all)
                echo "Stopping all containers..."
                for container in "${containers[@]}"; do
                    echo -n "  Stopping $container... "
                    if docker stop "$container" >/dev/null 2>&1; then
                        echo "✓"
                    else
                        echo "✗"
                    fi
                done
                echo ""
                echo "✓ All containers stopped"
                return 0
                ;;
            *)
                # Try to parse as space-separated numbers
                local valid_selection=false
                for sel in $action; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#containers[@]} ]; then
                        valid_selection=true
                        local container="${containers[$((sel-1))]}"
                        echo -n "Stopping $container... "
                        if docker stop "$container" >/dev/null 2>&1; then
                            echo "✓"
                        else
                            echo "✗"
                        fi
                    elif [[ "$sel" =~ ^[0-9]+$ ]]; then
                        echo "Invalid number: $sel (valid range: 1-${#containers[@]})"
                    fi
                done

                if [ "$valid_selection" = false ]; then
                    echo "Invalid input. Enter container numbers, 'a' for all, 'r' to refresh, or 'q' to quit."
                else
                    echo ""
                    # Show updated list
                    local remaining=$(docker ps --filter "name=_${suffix}" --format '{{.Names}}' 2>/dev/null | wc -l)
                    if [ "$remaining" -eq 0 ]; then
                        echo "✓ All CCY containers stopped"
                        return 0
                    else
                        echo "Remaining containers: $remaining"
                        echo "Enter 'r' to refresh list, or 'q' to quit"
                    fi
                fi
                echo ""
                ;;
        esac
    done
}

# Check for running containers for current project and offer to manage them
# Called at ccy startup after git repo check
# Args: project_name, suffix
# Returns: 0 to continue, 1 to abort
check_project_containers_startup() {
    local project_name="$1"
    local suffix="${2:-yolo}"

    # Get containers for this project
    local containers=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && containers+=("$line")
    done < <(docker ps --filter "name=${project_name}_${suffix}" --format '{{.Names}}' 2>/dev/null)

    if [ ${#containers[@]} -eq 0 ]; then
        return 0  # No containers, continue normally
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "⚠️  Existing Containers Detected"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Found ${#containers[@]} running container(s) for this project:"
    echo ""

    # Show brief list with stats
    printf "%-3s %-30s %-8s %-12s\n" "#" "CONTAINER" "CPU" "UPTIME"
    echo "────────────────────────────────────────────────────────────────────────────────"

    local i=1
    for container in "${containers[@]}"; do
        local stats=$(get_container_stats "$container")
        local cpu=$(echo "$stats" | cut -d'|' -f1)
        local uptime=$(get_container_uptime "$container")
        printf "%-3s %-30s %-8s %-12s\n" "$i)" "$container" "$cpu" "$uptime"
        i=$((i + 1))
    done

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Options:"
    echo "  [c] Continue (start new container alongside existing)"
    echo "  [s] Stop all and continue with fresh container"
    echo "  [m] Manage containers (stop specific ones)"
    echo "  [q] Quit"
    echo ""

    while true; do
        read -p "Choice [c/s/m/q]: " choice
        echo ""

        case "$choice" in
            c|C)
                echo "Continuing with new container..."
                return 0
                ;;
            s|S)
                echo "Stopping existing containers..."
                for container in "${containers[@]}"; do
                    echo -n "  Stopping $container... "
                    if docker stop "$container" >/dev/null 2>&1; then
                        echo "✓"
                    else
                        echo "✗"
                    fi
                done
                echo ""
                return 0
                ;;
            m|M)
                echo "Enter container numbers to stop (space-separated), or 'b' to go back:"
                read -p "> " selections

                if [ "$selections" = "b" ] || [ "$selections" = "B" ]; then
                    continue
                fi

                for sel in $selections; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#containers[@]} ]; then
                        local container="${containers[$((sel-1))]}"
                        echo -n "  Stopping $container... "
                        if docker stop "$container" >/dev/null 2>&1; then
                            echo "✓"
                        else
                            echo "✗"
                        fi
                    fi
                done
                echo ""
                return 0
                ;;
            q|Q)
                return 1
                ;;
            *)
                echo "Invalid choice. Please enter c, s, m, or q."
                echo ""
                ;;
        esac
    done
}

# Export functions
export -f find_zombie_containers
export -f get_container_stats
export -f get_container_uptime
export -f show_zombie_container_tui
export -f check_overlay2_kernel_support
export -f check_overlay_module
export -f get_docker_storage_driver
export -f is_using_native_overlay2
export -f has_docker_data
export -f get_docker_data_size
export -f show_overlay2_migration_tui
export -f check_zombie_containers_startup
export -f show_container_top
export -f check_project_containers_startup
