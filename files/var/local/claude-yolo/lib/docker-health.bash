#!/bin/bash
# Claude YOLO Docker Health Library
# Handles zombie container detection, overlay2 migration, and docker health checks
#
# Version: 1.1.0 - Container engine abstraction (docker/podman support)

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
    local containers
    containers=$(container_cmd ps --filter "name=_${suffix}" --format '{{.Names}}' 2>/dev/null)

    if [ -z "$containers" ]; then
        return 0  # No containers found
    fi

    while IFS= read -r container_name; do
        [ -z "$container_name" ] && continue

        # Check if this container was started with -it (TTY mode)
        local has_tty
        has_tty=$(container_cmd inspect --format '{{.Config.Tty}}' "$container_name" 2>/dev/null)

        if [ "$has_tty" = "true" ]; then
            # Check if there's a container run process still attached
            # The run process would have the container name in its command line
            if ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${container_name}[^_]" >/dev/null 2>&1 && \
               ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${container_name}$" >/dev/null 2>&1; then
                zombies+=("$container_name")
            fi
        fi
    done <<< "$containers"

    printf '%s\n' "${zombies[@]}"
}

# Find CCY containers that are stopped or in created state
# These are leftover from unclean shutdowns (battery death, crash, kill -9)
# where the --rm flag never fired
# Args: suffix (default: "yolo")
# Returns: newline-separated list of container names
find_stale_containers() {
    local suffix="${1:-yolo}"
    local stale=()

    # Query all non-running containers matching CCY naming pattern
    # Valid states for both Docker and Podman: exited, created
    # (Docker also has "dead" but Podman does not)
    local status
    for status in exited created; do
        local containers
        containers=$(container_cmd ps -a \
            --filter "name=_${suffix}" \
            --filter "status=${status}" \
            --format '{{.Names}}' 2>/dev/null)

        while IFS= read -r name; do
            [ -n "$name" ] && stale+=("$name")
        done <<< "$containers"
    done

    if [ ${#stale[@]} -gt 0 ]; then
        printf '%s\n' "${stale[@]}" | sort -u
    fi
}

# Get detailed stats for a container
# Args: container_name
# Returns: formatted stats string
get_container_stats() {
    local container_name="$1"

    # Get stats in one call
    local stats
    stats=$(container_cmd stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}' "$container_name" 2>/dev/null)

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
    container_cmd ps --filter "name=^${container_name}$" --format '{{.RunningFor}}' 2>/dev/null
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
        local stats
        stats=$(get_container_stats "$container")
        local cpu
        cpu=$(echo "$stats" | cut -d'|' -f1)
        local mem
        mem=$(echo "$stats" | cut -d'|' -f2)
        local uptime
        uptime=$(get_container_uptime "$container")

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
        read -rp "Choice [a/s/i/q]: " choice
        echo ""

        case "$choice" in
            a|A)
                echo "Stopping all orphaned containers..."
                for container in "${zombies[@]}"; do
                    echo -n "  Stopping $container... "
                    if container_cmd stop "$container" >/dev/null 2>&1; then
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
                read -rp "> " selections

                for sel in $selections; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#zombies[@]} ]; then
                        local container="${zombies[$((sel-1))]}"
                        echo -n "  Stopping $container... "
                        if container_cmd stop "$container" >/dev/null 2>&1; then
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

# Clean up stale (stopped/dead) CCY containers at startup
# These are leftover from unclean shutdowns where --rm didn't fire
# Auto-removes without prompting since these containers are unrecoverable
# Args: suffix (default: "yolo")
# Returns: 0 always (cleanup is best-effort)
clean_stale_containers_startup() {
    local suffix="${1:-yolo}"
    local stale=()

    while IFS= read -r name; do
        [ -n "$name" ] && stale+=("$name")
    done < <(find_stale_containers "$suffix")

    if [ ${#stale[@]} -eq 0 ]; then
        return 0
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "Cleaning Up Stale Containers"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Found ${#stale[@]} container(s) left over from an unclean shutdown."
    echo "These containers are stopped/dead and unrecoverable — removing automatically."
    echo ""

    local cleaned=0
    local failed=0
    for container in "${stale[@]}"; do
        echo -n "  Removing $container... "
        local rm_output
        if rm_output=$(container_cmd rm -f "$container" 2>&1); then
            echo "✓"
            cleaned=$((cleaned + 1))
        else
            echo "✗ ($rm_output)"
            failed=$((failed + 1))
        fi
    done

    echo ""
    if [ "$failed" -eq 0 ]; then
        echo "✓ Cleaned up $cleaned stale container(s)"
    else
        echo "Cleaned $cleaned, failed to remove $failed container(s)"
        echo "  Manual cleanup: $CONTAINER_ENGINE rm -f <container_name>"
    fi
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
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

    while true; do
    # Clean up stale containers first (stopped/dead from unclean shutdowns)
    clean_stale_containers_startup "$suffix"

    # Get all containers matching the suffix
    local containers=()
    while IFS= read -r line; do
        [[ -n "$line" ]] && containers+=("$line")
    done < <(container_cmd ps --filter "name=_${suffix}" --format '{{.Names}}' 2>/dev/null)

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
        local stats
        stats=$(get_container_stats "$container")
        local cpu
        cpu=$(echo "$stats" | cut -d'|' -f1)
        local mem
        mem=$(echo "$stats" | cut -d'|' -f2 | cut -d'/' -f1)  # Just usage, not limit
        local uptime
        uptime=$(get_container_uptime "$container")

        # Check if zombie (no container run process attached)
        local status="active"
        local has_tty
        has_tty=$(container_cmd inspect --format '{{.Config.Tty}}' "$container" 2>/dev/null)
        if [ "$has_tty" = "true" ]; then
            if ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${container}( |$)" >/dev/null 2>&1; then
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
        read -rp "Action: " action
        echo ""

        case "$action" in
            q|Q|quit|exit)
                return 0
                ;;
            r|R|refresh)
                continue 2
                ;;
            a|A|all)
                echo "Stopping all containers..."
                for container in "${containers[@]}"; do
                    echo -n "  Stopping $container... "
                    if container_cmd stop "$container" >/dev/null 2>&1; then
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
                        if container_cmd stop "$container" >/dev/null 2>&1; then
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
                    local remaining
                    remaining=$(container_cmd ps --filter "name=_${suffix}" --format '{{.Names}}' 2>/dev/null | wc -l)
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
    done < <(container_cmd ps --filter "name=${project_name}_${suffix}" --format '{{.Names}}' 2>/dev/null)

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
        local stats
        stats=$(get_container_stats "$container")
        local cpu
        cpu=$(echo "$stats" | cut -d'|' -f1)
        local uptime
        uptime=$(get_container_uptime "$container")
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
        read -rp "Choice [c/s/m/q]: " choice
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
                    if container_cmd stop "$container" >/dev/null 2>&1; then
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
                read -rp "> " selections

                if [ "$selections" = "b" ] || [ "$selections" = "B" ]; then
                    continue
                fi

                for sel in $selections; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#containers[@]} ]; then
                        local container="${containers[$((sel-1))]}"
                        echo -n "  Stopping $container... "
                        if container_cmd stop "$container" >/dev/null 2>&1; then
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
export -f check_zombie_containers_startup
export -f clean_stale_containers_startup
export -f show_container_top
export -f check_project_containers_startup
