#!/bin/bash
# Network Management Library
# Shared Docker network operations for claude-yolo and claude-browser
#
# Version: 1.1.0 - Container engine abstraction (docker/podman support)

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

        while IFS= read -r net; do
            # Skip default networks
            if [[ "$net" != "bridge" ]] && [[ "$net" != "host" ]] && [[ "$net" != "none" ]]; then
                networks+=("$net")

                # Check for best match (network name contains project name)
                if [[ "$net" == *"$project_name"* ]] && [ -z "$best_match" ]; then
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
            echo "  0) $best_match (default - matches project name)"
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

        for container in "${matching_containers[@]}"; do
            echo "  → $container"
            if container_cmd network connect "$network_name" "$container" 2>/dev/null; then
                echo "    ✓ Connected successfully!"
                success_count=$((success_count + 1))
            else
                echo "    ⚠ Already connected or failed"
                already_connected_count=$((already_connected_count + 1))
            fi
        done

        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "Summary:"
        echo "  ✓ Connected: $success_count"
        if [ $already_connected_count -gt 0 ]; then
            echo "  ⚠ Already connected: $already_connected_count"
        fi
        echo ""
        echo "You can now access project containers from inside $tool_name."
        echo "Example: curl http://container-name:port"
    else
        # Connect single container
        echo "Connecting $container_name to $network_name..."
        if container_cmd network connect "$network_name" "$container_name" 2>/dev/null; then
            echo "✓ Connected successfully!"
            echo ""
            echo "You can now access project containers from inside $tool_name."
            echo "Example: curl http://container-name:port"
        else
            echo "⚠ Container may already be connected to this network"
            echo ""
            echo "Container networks:"
            container_cmd inspect "$container_name" --format '{{range $k, $v := .NetworkSettings.Networks}}  {{$k}}{{"\n"}}{{end}}'
        fi
    fi

    exit 0
}

# Export functions
export -f connect_to_network
