#!/bin/bash
# UI Helpers Library
# Shared display and UI functions for claude-yolo and claude-browser
#
# Version: 1.0.0

# Function to display quick launch command preview
# Args: $1 = tool_name, $2 = token_name, $3+ = ssh_keys (array)
show_quick_launch_command() {
    local tool_name="$1"
    local token_name="$2"
    shift 2
    local ssh_keys=("$@")

    echo "╔══════════════════════════════════════════════════════════════════════════════╗"
    echo "║ Quick Launch Command (copy to skip prompts next time):                      ║"
    echo "╠══════════════════════════════════════════════════════════════════════════════╣"
    echo "║                                                                              ║"

    local cmd="  $tool_name"
    [[ -n "$token_name" ]] && cmd+=" --token $token_name"

    if [ ${#ssh_keys[@]} -gt 0 ]; then
        for key in "${ssh_keys[@]}"; do
            cmd+=" \\"
            printf "║ %-76s ║\n" "$cmd"
            cmd="      --ssh-key $key"
        done
    fi

    if [[ -n "$SAVED_NETWORK" ]]; then
        cmd+=" \\"
        printf "║ %-76s ║\n" "$cmd"
        cmd="      --network $SAVED_NETWORK"
    fi

    printf "║ %-76s ║\n" "$cmd"
    echo "║                                                                              ║"
    echo "╚══════════════════════════════════════════════════════════════════════════════╝"
    echo ""
}

# Export functions
export -f show_quick_launch_command
