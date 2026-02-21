#!/bin/bash
# Network Management Library
# Shared Docker network operations for claude-yolo (ccy)
#
# Version: 1.5.0 - Auto-add DNS servers for WARP/localhost DNS compatibility

# Get the expected network name for the current project
# Returns: network-name based on folder name, or repo name as fallback
# Priority: parent-project format > project-only (with warning) > git remote
get_expected_network_name() {
    local project_name
    project_name=$(basename "$PWD")
    local parent_folder
    parent_folder=$(basename "$(dirname "$PWD")")
    local generic_folders="projects|repos|work|src|code|dev|home"

    # PRIORITY 1: Try parent-folder-project-folder naming (unless parent is generic)
    # This is the preferred format to avoid collisions (e.g., "ec-site" vs "other-site")
    if ! echo "$parent_folder" | grep -qiE "^($generic_folders)$"; then
        local parent_project_network="${parent_folder}-${project_name}-network"
        if container_cmd network ls --format '{{.Name}}' | grep -q "^${parent_project_network}$"; then
            echo "$parent_project_network"
            return 0
        fi
    fi

    # PRIORITY 2: Fallback to project-only naming with collision warning
    local project_only_network="${project_name}-network"
    if container_cmd network ls --format '{{.Name}}' | grep -q "^${project_only_network}$"; then
        echo "⚠️  Warning: Using project-only network name '${project_only_network}'" >&2
        echo "   Risk of collision if multiple projects share the same directory name." >&2
        echo "   Consider renaming network to include parent directory: ${parent_folder}-${project_name}-network" >&2
        echo "$project_only_network"
        return 0
    fi

    # PRIORITY 3: Try to get repo name from git remote
    if git rev-parse --git-dir > /dev/null 2>&1; then
        local repo_url
        repo_url=$(git config --get remote.origin.url 2>/dev/null || echo "")
        if [ -n "$repo_url" ]; then
            # Extract repo name from URL (handles both HTTP and SSH formats)
            local repo_name
            repo_name=$(basename "$repo_url" .git)
            local repo_network="${repo_name}-network"

            if container_cmd network ls --format '{{.Name}}' | grep -q "^${repo_network}$"; then
                echo "$repo_network"
                return 0
            fi
        fi
    fi

    # No matching network found - return the preferred parent-project format
    # (or project-only if parent is generic)
    if ! echo "$parent_folder" | grep -qiE "^($generic_folders)$"; then
        echo "${parent_folder}-${project_name}-network"
    else
        echo "$project_only_network"
    fi
    return 1
}

# Get the persisted network file path for current project
get_network_persistence_file() {
    local project_path
    project_path=$(pwd)
    local project_hash
    project_hash=$(echo -n "$project_path" | sha256sum | cut -d' ' -f1 | cut -c1-16)
    echo "$HOME/.claude-tokens/ccy/projects/$project_hash/network"
}

# Save network name to persistence file
save_network_preference() {
    local network_name="$1"
    local network_file
    network_file=$(get_network_persistence_file)

    mkdir -p "$(dirname "$network_file")"
    echo "$network_name" > "$network_file"
}

# Load network name from persistence file
load_network_preference() {
    local network_file
    network_file=$(get_network_persistence_file)

    if [ -f "$network_file" ]; then
        cat "$network_file"
        return 0
    fi

    return 1
}

# Function to connect running container to a Docker network
# Args: $1 = network_name (optional), $2 = container_suffix ("_yolo" or "_browser"), $3 = tool_name (for display)
connect_to_network() {
    local network_name="$1"
    local container_suffix="$2"
    local tool_name="${3:-ccy}"
    local project_name
    project_name=$(basename "$PWD")
    local base_name="${project_name}${container_suffix}"
    local container_name=""

    # Find all running containers matching this project
    local matching_containers=()
    while IFS= read -r name; do
        matching_containers+=("$name")
    done < <(container_cmd ps --format '{{.Names}}' | grep "^${base_name}" || true)

    # Check if any containers are running
    if [ ${#matching_containers[@]} -eq 0 ]; then
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "Connect YOLO Container to Docker Network"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        print_error "No running containers found for project: $project_name"
        echo ""
        echo "Start $tool_name first, then run this in another terminal:"
        echo "  $tool_name --connect <network_name>"
        echo ""
        exit 1
    fi

    # Determine if we should connect all or select one
    local connect_all=false
    if [ ${#matching_containers[@]} -eq 1 ]; then
        # Only one container, use it automatically
        container_name="${matching_containers[0]}"
    else
        # Multiple containers - show selection menu with "all" option
        if [ -z "$network_name" ]; then
            echo ""
            echo "════════════════════════════════════════════════════════════════════════════════"
            echo "Select YOLO Container(s)"
            echo "════════════════════════════════════════════════════════════════════════════════"
            echo ""
            echo "Multiple containers found for project: $project_name"
            echo ""
            echo "  0) All containers (default)"
            echo ""

            for i in "${!matching_containers[@]}"; do
                echo "  $((i + 1))) ${matching_containers[$i]}"
            done
            echo ""

            while true; do
                read -rp "Select container [0-${#matching_containers[@]}] (0): " selection
                selection=${selection:-0}  # Default to 0 if empty
                echo ""

                if [ "$selection" = "0" ]; then
                    connect_all=true
                    break
                elif [ "$selection" -ge 1 ] && [ "$selection" -le ${#matching_containers[@]} ] 2>/dev/null; then
                    container_name="${matching_containers[$((selection - 1))]}"
                    break
                else
                    echo "Invalid selection: $selection"
                    echo "Please enter a number between 0 and ${#matching_containers[@]}"
                    echo ""
                fi
            done
        else
            # Network name provided via command line - connect all by default
            connect_all=true
        fi
    fi

    if [ -z "$network_name" ]; then
        # Check for persisted network preference first
        local persisted_network
        persisted_network=$(load_network_preference 2>/dev/null || echo "")

        if [ -n "$persisted_network" ]; then
            # Verify the persisted network still exists
            if container_cmd network ls --format '{{.Name}}' | grep -q "^${persisted_network}$"; then
                network_name="$persisted_network"
                echo ""
                echo "════════════════════════════════════════════════════════════════════════════════"
                echo "Using Saved Network Preference"
                echo "════════════════════════════════════════════════════════════════════════════════"
                echo ""
                echo "Network: $network_name (from saved preference)"
                echo ""
                echo "To change network, use: $tool_name --connect <network-name>"
                echo ""
            else
                echo "⚠ Saved network '$persisted_network' no longer exists. Prompting for new network..."
                echo ""
            fi
        fi
    fi

    if [ -z "$network_name" ]; then
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "Connect YOLO Container to Docker Network"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""

        # Display what we're connecting
        if [ "$connect_all" = true ]; then
            echo "Containers: All ${#matching_containers[@]} running containers"
        else
            echo "Container: $container_name"
        fi
        echo ""

        echo "Available networks:"
        echo ""

        # Get list of networks (excluding bridge, host, none)
        local networks=()
        local best_match=""
        local best_match_index=""

        # Get expected network name (folder-name-network or repo-name-network)
        local expected_network
        expected_network=$(get_expected_network_name)

        while IFS= read -r net; do
            # Skip default networks
            if [[ "$net" != "bridge" ]] && [[ "$net" != "host" ]] && [[ "$net" != "none" ]]; then
                networks+=("$net")

                # Check for best match (exact match with expected network name)
                if [[ "$net" == "$expected_network" ]] && [ -z "$best_match" ]; then
                    best_match="$net"
                    best_match_index=$((${#networks[@]} - 1))
                fi
            fi
        done < <(container_cmd network ls --format "{{.Name}}" | sort)

        if [ ${#networks[@]} -eq 0 ]; then
            echo "No user-defined networks found."
            echo ""
            echo "Create a network first:"
            echo "  $CONTAINER_ENGINE network create ${project_name}_network"
            exit 1
        fi

        # Show networks with optional default
        if [ -n "$best_match" ]; then
            echo "  0) $best_match (default - expected network for this project)"
            echo ""
        fi

        for i in "${!networks[@]}"; do
            if [ "$i" != "$best_match_index" ]; then
                echo "  $((i + 1))) ${networks[$i]}"
            fi
        done

        echo ""

        while true; do
            if [ -n "$best_match" ]; then
                read -rp "Select network [0-${#networks[@]}] (0): " selection
                selection=${selection:-0}  # Default to 0 if empty
            else
                read -rp "Select network [1-${#networks[@]}]: " selection
            fi
            echo ""

            if [ -z "$selection" ]; then
                echo "Invalid selection: (empty)"
                if [ -n "$best_match" ]; then
                    echo "Please enter a number between 0 and ${#networks[@]}, or press Enter for default (0)"
                else
                    echo "Please enter a number between 1 and ${#networks[@]}"
                fi
                echo ""
                continue
            fi

            # Handle selection
            if [ -n "$best_match" ] && [ "$selection" = "0" ]; then
                network_name="$best_match"
                break
            elif [ "$selection" -ge 1 ] && [ "$selection" -le ${#networks[@]} ] 2>/dev/null; then
                network_name="${networks[$((selection - 1))]}"
                break
            else
                if [ -n "$best_match" ]; then
                    echo "Invalid selection: $selection"
                    echo "Please enter a number between 0 and ${#networks[@]}"
                else
                    echo "Invalid selection: $selection"
                    echo "Please enter a number between 1 and ${#networks[@]}"
                fi
                echo ""
            fi
        done

        echo "Selected: $network_name"
        echo ""
    fi

    # Check if network exists
    if ! container_cmd network ls --format '{{.Name}}' | grep -q "^${network_name}$"; then
        print_error "Network not found: $network_name"
        echo ""
        echo "Available networks:"
        container_cmd network ls --format "  {{.Name}}"
        exit 1
    fi

    # Connect container(s) to network
    if [ "$connect_all" = true ]; then
        # Connect all containers
        echo "Connecting all containers to $network_name..."
        echo ""

        local success_count=0
        local already_connected_count=0
        local error_count=0

        for container in "${matching_containers[@]}"; do
            echo "  → $container"
            local error_output
            error_output=$(container_cmd network connect "$network_name" "$container" 2>&1)
            local exit_code=$?

            if [ $exit_code -eq 0 ]; then
                echo "    ✓ Connected successfully!"
                success_count=$((success_count + 1))
            elif echo "$error_output" | grep -q "already attached\|already connected"; then
                echo "    ⚠ Already connected"
                already_connected_count=$((already_connected_count + 1))
            else
                echo "    ✗ Failed: $error_output"
                error_count=$((error_count + 1))
            fi
        done

        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "Summary:"
        echo "  ✓ Connected: $success_count"
        if [ $already_connected_count -gt 0 ]; then
            echo "  ⚠ Already connected: $already_connected_count"
        fi
        if [ $error_count -gt 0 ]; then
            echo "  ✗ Errors: $error_count"
        fi
        echo ""

        # Save network preference for future sessions
        if [ $success_count -gt 0 ]; then
            save_network_preference "$network_name"
            echo "  📌 Saved network preference: $network_name"
            echo ""
        fi

        if [ $success_count -gt 0 ] || [ $already_connected_count -gt 0 ]; then
            echo "You can now access project containers from inside $tool_name."
            echo "Example: curl http://container-name:port"
        fi
    else
        # Connect single container
        echo "Connecting $container_name to $network_name..."
        echo ""

        local error_output
        error_output=$(container_cmd network connect "$network_name" "$container_name" 2>&1)
        local exit_code=$?

        if [ $exit_code -eq 0 ]; then
            echo "✓ Connected successfully!"
            echo ""

            # Save network preference for future sessions
            save_network_preference "$network_name"
            echo "📌 Saved network preference: $network_name"
            echo ""

            echo "You can now access project containers from inside $tool_name."
            echo "Example: curl http://container-name:port"
        elif echo "$error_output" | grep -q "already attached\|already connected"; then
            echo "⚠ Container already connected to this network"
            echo ""

            # Save network preference anyway since it's correct
            save_network_preference "$network_name"
            echo "📌 Saved network preference: $network_name"
            echo ""
        else
            echo "✗ Failed to connect container"
            echo ""
            echo "Error: $error_output"
            echo ""
            echo "Container networks:"
            local networks_json
            networks_json=$(container_cmd inspect "$container_name" --format '{{json .NetworkSettings.Networks}}' 2>/dev/null)
            if [ "$networks_json" = "null" ] || [ -z "$networks_json" ]; then
                echo "  (none - container has no network connections)"
                echo ""
                echo "This is unusual. The container may need to be restarted."
                echo "Try: $tool_name (restart the container)"
            else
                echo "$networks_json" | jq -r 'keys[]' | awk '{ print "  " $0 }'
            fi
            echo ""
            exit 1
        fi
    fi

    exit 0
}

# Check if a network has running containers
# Args: $1 = network_name
# Returns: 0 if containers are running on the network, 1 otherwise
network_has_running_containers() {
    local network_name="$1"

    if [[ -z "$network_name" ]]; then
        return 1
    fi

    # Get containers attached to this network
    local container_count
    container_count=$(container_cmd network inspect "$network_name" --format '{{len .Containers}}' 2>/dev/null || echo "0")

    if [[ "$container_count" -gt 0 ]]; then
        return 0
    fi

    return 1
}

# Check for compose files in current directory
# Returns: 0 if compose files found, 1 otherwise
# Sets: COMPOSE_FILES array with found files
has_compose_files() {
    COMPOSE_FILES=()
    for pattern in "docker-compose.yml" "docker-compose.yaml" "podman-compose.yml" "podman-compose.yaml" "compose.yml" "compose.yaml"; do
        if [ -f "$pattern" ]; then
            COMPOSE_FILES+=("$pattern")
        fi
    done

    if [ ${#COMPOSE_FILES[@]} -gt 0 ]; then
        return 0
    fi
    return 1
}

# Check if compose services are running and offer to start if not
# Args: $1 = network_name (the network we want to connect to)
#       $2 = project_name (optional, defaults to basename of PWD)
# Returns: 0 if services are running (or were started), 1 if user declined or no compose
check_and_start_compose_services() {
    local network_name="$1"
    local project_name="${2:-$(basename "$PWD")}"

    # Check if network has running containers
    if network_has_running_containers "$network_name"; then
        # Services are running, nothing to do
        return 0
    fi

    # Network exists but no containers - check for compose files
    if ! has_compose_files; then
        # No compose files, can't auto-start
        return 1
    fi

    echo ""
    echo "────────────────────────────────────────────────────────────────────────────────"
    echo "⚠  Network exists but no containers are running"
    echo "────────────────────────────────────────────────────────────────────────────────"
    echo ""
    echo "Network: $network_name"
    echo ""
    echo "The network exists but appears to have no running containers."
    echo "This usually means compose services were stopped but not removed."
    echo ""
    echo "Found compose files:"
    for cf in "${COMPOSE_FILES[@]}"; do
        echo "  • $cf"
    done
    echo ""

    # Use the existing offer_compose_start logic
    _do_compose_start "$network_name" "$project_name"
    return $?
}

# Internal helper to start compose (shared between offer_compose_start and check_and_start_compose_services)
# Args: $1 = expected_network, $2 = project_name
_do_compose_start() {
    local expected_network="$1"
    local project_name="$2"

    # Determine compose command based on container engine
    local compose_cmd=""
    local compose_name=""

    if [[ "$CONTAINER_ENGINE" = "podman" ]]; then
        if command -v podman-compose &>/dev/null; then
            compose_cmd="podman-compose"
            compose_name="podman-compose"
        else
            echo "⚠ podman-compose not installed"
            echo ""
            echo "Install with:"
            echo "  pip install podman-compose"
            echo "  # Or: ansible-playbook playbooks/imports/optional/common/play-podman.yml"
            echo ""
            echo "Then run: podman-compose up -d"
            echo "────────────────────────────────────────────────────────────────────────────────"
            return 1
        fi
    else
        if command -v docker-compose &>/dev/null; then
            compose_cmd="docker-compose"
            compose_name="docker-compose"
        elif command -v docker &>/dev/null && docker compose version &>/dev/null 2>&1; then
            compose_cmd="docker compose"
            compose_name="docker compose"
        else
            echo "⚠ docker-compose not installed"
            echo ""
            echo "Install Docker Compose or use Podman instead."
            echo "────────────────────────────────────────────────────────────────────────────────"
            return 1
        fi
    fi

    # Offer to start compose
    while true; do
        read -rp "Start services with $compose_name up -d? [Y/n]: " start_choice
        start_choice=${start_choice:-Y}
        echo ""

        case "$start_choice" in
            Y|y|Yes|yes)
                echo "Starting $compose_name..."
                if $compose_cmd up -d; then
                    echo ""
                    echo "✓ Compose services started"
                    echo ""
                    echo "Waiting for containers..."
                    sleep 2

                    # Verify containers are now running
                    if network_has_running_containers "$expected_network"; then
                        echo "✓ Services running on network: $expected_network"
                        echo "────────────────────────────────────────────────────────────────────────────────"
                        return 0
                    else
                        # Check if any project networks now have containers
                        local found_networks=()
                        while IFS= read -r net; do
                            if [[ "$net" != "bridge" ]] && [[ "$net" != "host" ]] && [[ "$net" != "none" ]] && [[ "$net" != "podman" ]]; then
                                if [[ "$net" == *"$project_name"* ]] && network_has_running_containers "$net"; then
                                    found_networks+=("$net")
                                fi
                            fi
                        done < <(container_cmd network ls --format "{{.Name}}" 2>/dev/null)

                        if [ ${#found_networks[@]} -gt 0 ]; then
                            COMPOSE_NETWORK="${found_networks[0]}"
                            echo "✓ Services running on network: $COMPOSE_NETWORK"
                            echo "────────────────────────────────────────────────────────────────────────────────"
                            return 0
                        fi

                        echo "⚠ Services started but no containers found on expected network"
                        echo "────────────────────────────────────────────────────────────────────────────────"
                        return 1
                    fi
                else
                    echo "⚠ $compose_name failed. Check errors above."
                    echo "────────────────────────────────────────────────────────────────────────────────"
                    return 1
                fi
                ;;
            N|n|No|no)
                echo "Skipping compose startup"
                echo "Run '$compose_name up -d' manually when ready"
                echo "────────────────────────────────────────────────────────────────────────────────"
                return 1
                ;;
            *)
                echo "Invalid choice. Please enter y or n"
                echo ""
                ;;
        esac
    done
}

# Check for compose files and offer to start services (used when network doesn't exist)
# Args: $1 = expected_network (optional), $2 = project_name
# Sets: COMPOSE_NETWORK (the network created/found after starting compose)
# Returns: 0 if compose started and network found, 1 otherwise
offer_compose_start() {
    local expected_network="${1:-}"
    local project_name="${2:-$(basename "$PWD")}"

    # Reset output variable
    COMPOSE_NETWORK=""

    # Check for compose files using shared helper
    if ! has_compose_files; then
        return 1
    fi

    echo "────────────────────────────────────────────────────────────────────────────────"
    echo "Compose Files Detected"
    echo "────────────────────────────────────────────────────────────────────────────────"
    echo ""
    echo "Found compose files:"
    for cf in "${COMPOSE_FILES[@]}"; do
        echo "  • $cf"
    done
    echo ""

    # Use shared helper to start compose
    _do_compose_start "$expected_network" "$project_name"
    return $?
}

# Ensure network has DNS servers configured for external resolution
# This fixes issues where aardvark-dns can't reach localhost-based DNS (e.g., Cloudflare WARP)
# Args: $1 = network_name
# Returns: 0 if DNS configured (or added), 1 if failed
ensure_network_dns() {
    local network_name="$1"
    local default_dns_servers=("1.1.1.1" "8.8.8.8")

    if [[ -z "$network_name" ]]; then
        return 1
    fi

    # Skip for default podman network (uses pasta's DNS proxy, not aardvark-dns)
    if [[ "$network_name" == "podman" ]]; then
        return 0
    fi

    # Check if network has dns_enabled (only those use aardvark-dns)
    local dns_enabled
    dns_enabled=$(container_cmd network inspect "$network_name" --format '{{.DNSEnabled}}' 2>/dev/null)

    if [[ "$dns_enabled" != "true" ]]; then
        # Network doesn't use aardvark-dns, no fix needed
        return 0
    fi

    # Check current DNS servers on the network
    local current_dns
    current_dns=$(container_cmd network inspect "$network_name" --format '{{json .NetworkDNSServers}}' 2>/dev/null)

    # If DNS servers already configured, nothing to do
    if [[ -n "$current_dns" ]] && [[ "$current_dns" != "null" ]] && [[ "$current_dns" != "[]" ]]; then
        return 0
    fi

    # No DNS servers configured - add them
    echo "Adding DNS servers to network '$network_name' for external resolution..."

    for dns in "${default_dns_servers[@]}"; do
        if ! container_cmd network update "$network_name" --dns-add "$dns" >/dev/null 2>&1; then
            echo "  ⚠ Failed to add DNS server $dns" >&2
        fi
    done

    echo "  ✓ Added DNS servers: ${default_dns_servers[*]}"
    return 0
}

# Export functions
export -f ensure_network_dns
export -f get_expected_network_name
export -f get_network_persistence_file
export -f save_network_preference
export -f load_network_preference
export -f connect_to_network
export -f network_has_running_containers
export -f has_compose_files
export -f check_and_start_compose_services
export -f _do_compose_start
export -f offer_compose_start
