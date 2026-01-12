#!/bin/bash
# Network Management Library
# Shared Docker network operations for claude-yolo and claude-browser
#
# Version: 1.3.0 - Added parent-folder-repo-folder naming, improved error handling

# Get the expected network name for the current project
# Returns: network-name based on folder name, or repo name as fallback
get_expected_network_name() {
    local project_name=$(basename "$PWD")
    local expected_network="${project_name}-network"

    # Check if this network exists
    if container_cmd network ls --format '{{.Name}}' | grep -q "^${expected_network}$"; then
        echo "$expected_network"
        return 0
    fi

    # Try parent-folder-repo-folder naming (unless parent is generic)
    local parent_folder=$(basename "$(dirname "$PWD")")
    local generic_folders="projects|repos|work|src|code|dev|home"

    # Check if parent folder is NOT a generic name (case insensitive)
    if ! echo "$parent_folder" | grep -qiE "^($generic_folders)$"; then
        local parent_repo_network="${parent_folder}-${project_name}-network"
        if container_cmd network ls --format '{{.Name}}' | grep -q "^${parent_repo_network}$"; then
            echo "$parent_repo_network"
            return 0
        fi
    fi

    # Fallback: Try to get repo name from git remote
    if git rev-parse --git-dir > /dev/null 2>&1; then
        local repo_url=$(git config --get remote.origin.url 2>/dev/null || echo "")
        if [ -n "$repo_url" ]; then
            # Extract repo name from URL (handles both HTTP and SSH formats)
            local repo_name=$(basename "$repo_url" .git)
            local repo_network="${repo_name}-network"

            if container_cmd network ls --format '{{.Name}}' | grep -q "^${repo_network}$"; then
                echo "$repo_network"
                return 0
            fi
        fi
    fi

    # No matching network found - return the expected name anyway
    echo "$expected_network"
    return 1
}

# Get the persisted network file path for current project
get_network_persistence_file() {
    local project_path=$(pwd)
    local project_hash=$(echo -n "$project_path" | sha256sum | cut -d' ' -f1 | cut -c1-16)
    echo "$HOME/.claude-tokens/ccy/projects/$project_hash/network"
}

# Save network name to persistence file
save_network_preference() {
    local network_name="$1"
    local network_file=$(get_network_persistence_file)

    mkdir -p "$(dirname "$network_file")"
    echo "$network_name" > "$network_file"
}

# Load network name from persistence file
load_network_preference() {
    local network_file=$(get_network_persistence_file)

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
    local project_name=$(basename "$PWD")
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
                read -p "Select container [0-${#matching_containers[@]}] (0): " selection
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
        local persisted_network=$(load_network_preference 2>/dev/null || echo "")

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
        local expected_network=$(get_expected_network_name)

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
        local start_index=1
        if [ -n "$best_match" ]; then
            echo "  0) $best_match (default - expected network for this project)"
            echo ""
            start_index=1
        fi

        for i in "${!networks[@]}"; do
            if [ "$i" != "$best_match_index" ]; then
                echo "  $((i + 1))) ${networks[$i]}"
            fi
        done

        echo ""

        while true; do
            if [ -n "$best_match" ]; then
                read -p "Select network [0-${#networks[@]}] (0): " selection
                selection=${selection:-0}  # Default to 0 if empty
            else
                read -p "Select network [1-${#networks[@]}]: " selection
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
            local error_output=$(container_cmd network connect "$network_name" "$container" 2>&1)
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

        local error_output=$(container_cmd network connect "$network_name" "$container_name" 2>&1)
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
            local networks=$(container_cmd inspect "$container_name" --format '{{json .NetworkSettings.Networks}}' 2>/dev/null)
            if [ "$networks" = "null" ] || [ -z "$networks" ]; then
                echo "  (none - container has no network connections)"
                echo ""
                echo "This is unusual. The container may need to be restarted."
                echo "Try: $tool_name (restart the container)"
            else
                echo "$networks" | jq -r 'keys[]' | sed 's/^/  /'
            fi
            echo ""
            exit 1
        fi
    fi

    exit 0
}

# Export functions
export -f get_expected_network_name
export -f get_network_persistence_file
export -f save_network_preference
export -f load_network_preference
export -f connect_to_network
