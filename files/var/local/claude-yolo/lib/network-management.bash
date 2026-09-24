#!/bin/bash
# Network Management Library
# Shared Docker network operations for claude-yolo (ccy)
#
# Version: 1.9.3 - A hand-edited Quick Launch line that still names the network fails the
#                  disconnect, naming the line, instead of being reported as cleared.
#          1.9.2 - --disconnect forgets the network everywhere a later launch reads it:
#                  the saved default, Quick Launch's LAST_NETWORK and this project's
#                  restore records; --connect fails on an engine that cannot list networks.
#          1.9.1 - An engine that cannot list its containers is a failure, not "none";
#                  a container's last network is never detached; a bare --disconnect
#                  with no container asks before clearing the saved default.
#          1.9.0 - `ccy --disconnect`: detach a network and clear the saved default
#                  that names it; --connect says what it saved and how to undo it.
#          1.8.0 - Plan 00075: three errexit call-site fixes — `ccy --connect`
#                  with no argument no longer dies before listing the networks,
#                  the "already connected" branch and its error report are no
#                  longer dead code, and the connect-failure diagnostic survives
#                  a container that has gone away.
#          1.7.0 - Skip compose start prompt when services already running

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
        repo_url=$(git config --get remote.origin.url 2>/dev/null) || repo_url=""
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
        # A `cat` that cannot read the file must not return 0 with no output —
        # callers read that as "no preference recorded" and quietly move on.
        cat "$network_file" || return 1
        return 0
    fi

    return 1
}

# Remove the project's saved default network. Fails if a file that is there cannot be removed.
clear_network_preference() {
    local network_file
    network_file=$(get_network_persistence_file)
    [ -f "$network_file" ] || return 0
    rm -f -- "$network_file"
}

# The Quick Launch config, relative to the project directory: the launcher's save_launch_config
# writes it after every launch and load_launch_config offers it on the next plain one (and a
# session restore takes it unasked), joining its LAST_NETWORK.
CCY_QUICK_LAUNCH_CONFIG=".claude/ccy/.last-launch.conf"

# The network the Quick Launch config names, printed; nothing when there is no config or it
# names none. Read with awk rather than sourced: only the one key is wanted here.
_quick_launch_network() {
    [ -f "$CCY_QUICK_LAUNCH_CONFIG" ] || return 0
    awk -F'"' '/^LAST_NETWORK="/ { print $2; exit }' "$CCY_QUICK_LAUNCH_CONFIG"
}

# Blank LAST_NETWORK in the Quick Launch config when it names $1. Every other line is kept as
# it is, and the rewrite is made beside the file (private, as the launcher makes it) and then
# moved into place, so the launcher never reads half of one.
_forget_quick_launch_network() {
    local network="$1" file="$CCY_QUICK_LAUNCH_CONFIG" named tmp
    if ! named=$(_quick_launch_network); then
        print_error "Could not read the Quick Launch config $file, so whether it names $network is unknown."
        return 1
    fi
    [ "$named" = "$network" ] || return 0
    tmp="$file.tmp.$$"
    if ! (umask 077 && WANT="LAST_NETWORK=\"$network\"" awk '$0 == ENVIRON["WANT"] { print "LAST_NETWORK=\"\""; next } { print }' "$file" >"$tmp") ||
        ! mv -f -- "$tmp" "$file"; then
        [ ! -f "$tmp" ] || rm -f -- "$tmp"
        print_error "Could not rewrite the Quick Launch config $file, so it still names $network and accepting Quick Launch would join it again."
        return 1
    fi
    # The rewrite replaces only the exact line the launcher writes. A hand-edited one (a
    # trailing space or comment, a CR) still reads as naming the network, so it is read again.
    if ! named=$(_quick_launch_network); then
        print_error "Could not read the Quick Launch config $file back after rewriting it, so whether it still names $network is unknown."
        return 1
    fi
    if [ "$named" = "$network" ]; then
        print_error "$file still names $network on the line: $(awk '/^LAST_NETWORK="/ { print; exit }' "$file" | cat -A)"
        print_error "That line is not the one ccy writes, so it was left as it is. Edit or delete it, then run the disconnect again."
        return 1
    fi
    echo "Cleared $network from the Quick Launch configuration ($file)."
}

# Say what --connect saved and what it means: the default is invisible otherwise, and it
# brings the network back on every later launch.
# Args: $1 = network_name, $2 = tool_name, $3 = indent
_report_saved_network_preference() {
    echo "${3}📌 Saved as this project's default network: $1"
    echo "${3}   every plain $2 launch in this project connects to it."
    echo "${3}   Undo with: $2 --disconnect $1"
}

# The running containers of the current project, one name per line (none: no output).
# Args: $1 = container_suffix ("_yolo" or "_browser")
# The project name is derived the SAME way container creation does (get_project_name
# prefixes a non-generic parent dir); basename "$PWD" alone found no containers exactly
# when that collision-avoidance naming applied. An engine that cannot list is a failure,
# never an empty list: read as "no containers", it let --disconnect clear the saved default.
_project_running_containers() {
    local base_name listing
    base_name="$(get_project_name)${1}"
    if ! listing=$(container_cmd ps --format '{{.Names}}'); then
        print_error "$CONTAINER_ENGINE could not list the running containers (its message is above); nothing was changed"
        return 1
    fi
    awk -v prefix="$base_name" 'index($0, prefix) == 1' <<<"$listing"
}

# The engine's network names, one per line. Like the container list, an engine that cannot
# list its networks is a failure, never an empty list: read as one it becomes "not found",
# "no longer exists" or "none", each of them a wrong cause.
_engine_network_names() {
    local listing
    if ! listing=$(container_cmd network ls --format '{{.Name}}'); then
        print_error "$CONTAINER_ENGINE could not list its networks (its message is above); nothing was changed"
        return 1
    fi
    [ -z "$listing" ] || printf '%s\n' "$listing"
}

# Read _project_running_containers into the array named by $1. Fails, having printed why,
# when the engine cannot list.
# Args: $1 = array name, $2 = container_suffix
_read_project_running_containers() {
    local -n _into="$1"
    local _listing _name
    _listing=$(_project_running_containers "$2") || return 1
    _into=()
    while IFS= read -r _name; do
        [ -n "$_name" ] && _into+=("$_name")
    done <<<"$_listing"
    return 0
}

# Function to connect running container to a Docker network
# Args: $1 = network_name (optional), $2 = container_suffix ("_yolo" or "_browser"), $3 = tool_name (for display)
connect_to_network() {
    local network_name="$1"
    local container_suffix="$2"
    local tool_name="${3:-ccy}"
    local project_name
    project_name=$(get_project_name)
    local container_name=""

    local matching_containers=()
    _read_project_running_containers matching_containers "$container_suffix" || return 1
    # Listed once, before anything is asked: every check below is judged against this list.
    local known_networks
    known_networks=$(_engine_network_names) || return 1

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
        return 1
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
                read -rp "${CCY_PROMPT_NETWORK_CONTAINER}0-${#matching_containers[@]}] (0): " selection
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
        if ! persisted_network=$(load_network_preference 2>/dev/null); then
            persisted_network=""
        fi

        if [ -n "$persisted_network" ]; then
            # Verify the persisted network still exists
            if grep -qxF -- "$persisted_network" <<<"$known_networks"; then
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

        # Get expected network name (folder-name-network or repo-name-network).
        #
        # get_expected_network_name() returns 1 to mean "this name does not exist
        # yet" while still echoing the name — a documented, expected outcome. As a
        # plain assignment under errexit that status ABORTED the function, so
        # `ccy --connect` with no argument printed its banner and "Available
        # networks:" and then died without listing one, on exactly the projects
        # someone runs --connect to set up. Take the name, read the status.
        local expected_network=""
        if ! expected_network=$(get_expected_network_name); then
            : # name still echoed; a non-zero status only means "not created yet"
        fi

        local sorted_networks
        sorted_networks=$(sort <<<"$known_networks")
        while IFS= read -r net; do
            [ -n "$net" ] || continue
            # Skip default networks
            if [[ "$net" != "bridge" ]] && [[ "$net" != "host" ]] && [[ "$net" != "none" ]]; then
                networks+=("$net")

                # Check for best match (exact match with expected network name)
                if [[ "$net" == "$expected_network" ]] && [ -z "$best_match" ]; then
                    best_match="$net"
                    best_match_index=$((${#networks[@]} - 1))
                fi
            fi
        done <<<"$sorted_networks"

        if [ ${#networks[@]} -eq 0 ]; then
            echo "No user-defined networks found."
            echo ""
            echo "Create a network first:"
            echo "  $CONTAINER_ENGINE network create ${project_name}_network"
            return 1
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
                read -rp "${CCY_PROMPT_NETWORK_SELECT}0-${#networks[@]}] (0): " selection
                selection=${selection:-0}  # Default to 0 if empty
            else
                read -rp "${CCY_PROMPT_NETWORK_SELECT}1-${#networks[@]}]: " selection
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
    if ! grep -qxF -- "$network_name" <<<"$known_networks"; then
        print_error "Network not found: $network_name"
        echo ""
        echo "Available networks:"
        awk 'NF { print "  " $0 }' <<<"$known_networks"
        return 1
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
            # Put the call in a condition so errexit is suspended and the
            # status survives. Previously the assignment failed the moment
            # `network connect` returned non-zero and the shell exited right
            # there — making the "already connected" branch below, the error
            # report, and the whole summary unreachable. Re-running
            # `ccy --connect` on an already-connected container is the most
            # common repeat invocation, and it exited silently.
            local error_output exit_code
            if error_output=$(container_cmd network connect "$network_name" "$container" 2>&1); then
                exit_code=0
            else
                exit_code=$?
            fi

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
            _report_saved_network_preference "$network_name" "$tool_name" "  "
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

        # Same as the multi-container path above: condition, not bare
        # assignment, so a non-zero status is data rather than a silent exit.
        local error_output exit_code
        if error_output=$(container_cmd network connect "$network_name" "$container_name" 2>&1); then
            exit_code=0
        else
            exit_code=$?
        fi

        if [ $exit_code -eq 0 ]; then
            echo "✓ Connected successfully!"
            echo ""

            # Save network preference for future sessions
            save_network_preference "$network_name"
            _report_saved_network_preference "$network_name" "$tool_name" ""
            echo ""

            echo "You can now access project containers from inside $tool_name."
            echo "Example: curl http://container-name:port"
        elif echo "$error_output" | grep -q "already attached\|already connected"; then
            echo "⚠ Container already connected to this network"
            echo ""

            # Save network preference anyway since it's correct
            save_network_preference "$network_name"
            _report_saved_network_preference "$network_name" "$tool_name" ""
            echo ""
        else
            echo "✗ Failed to connect container"
            echo ""
            echo "Error: $error_output"
            echo ""
            echo "Container networks:"
            local networks_json
            # This is the ERROR-REPORTING path — we are here because the connect
            # failed. A bare capture aborted the function under errexit exactly
            # when the container had gone away, swallowing the diagnostic at the
            # moment it was needed. The fallback prints "(none)", which is the
            # right prompt whether the container has no networks or cannot be
            # inspected at all.
            networks_json=$(container_cmd inspect "$container_name" --format '{{json .NetworkSettings.Networks}}' 2>/dev/null) || networks_json=""
            if [ "$networks_json" = "null" ] || [ -z "$networks_json" ]; then
                echo "  (none - container has no network connections)"
                echo ""
                echo "This is unusual. The container may need to be restarted."
                echo "Try: $tool_name (restart the container)"
            else
                echo "$networks_json" | jq -r 'keys[]' | awk '{ print "  " $0 }'
            fi
            echo ""
            return 1
        fi
    fi

    return 0
}

# The networks the engine attaches every container to by default. --disconnect never offers
# them: detaching one cuts the session's own internet access, which is never what undoing a
# wrong --connect means. Named explicitly, one is still detached.
_is_engine_default_network() {
    case "$1" in
        bridge | host | none | podman) return 0 ;;
    esac
    return 1
}

# The networks one container is attached to, one per line. Fails, with the engine's own
# message on stderr, when the container cannot be inspected.
_container_networks() {
    local out words=()
    # \$ is a literal $ here: these are Go template variables, not shell ones.
    out=$(container_cmd container inspect "$1" --format "{{range \$k, \$v := .NetworkSettings.Networks}}{{\$k}} {{end}}") || return 1
    read -r -a words <<<"${out//$'\n'/ }"
    [ ${#words[@]} -eq 0 ] || printf '%s\n' "${words[@]}"
}

# Three things bring a network back on a later launch of this project, and all three are
# cleared when they name it: the saved default (written by --connect), the Quick Launch
# config's LAST_NETWORK (written by a launch onto it), and the --network of this project's
# restore records (replayed after a reboot). A saved default naming another network stays,
# and is named. Fails, having said which, if any of them could not be cleared.
# Needs ccy_registry_forget_network from lib/session-registry.bash.
# Args: $1 = network_name, $2 = saved default ("" for none), $3 = tool_name
_forget_network_for_later_launches() {
    local network="$1" saved="$2" tool="$3" failed=0
    if ! declare -F ccy_registry_forget_network >/dev/null; then
        print_error "ccy_registry_forget_network is not defined: lib/session-registry.bash must be sourced before forgetting a network."
        return 1
    fi
    if [ -z "$saved" ]; then
        echo "No saved default network for this project."
    elif [ "$saved" = "$network" ]; then
        if clear_network_preference; then
            echo "Cleared the saved default network: $saved"
        else
            print_error "Could not remove the saved default network file: $(get_network_persistence_file)"
            failed=1
        fi
    else
        echo "The saved default network stays: $saved (it is not $network)."
    fi
    _forget_quick_launch_network "$network" || failed=1
    ccy_registry_forget_network "$PWD" "$network" || failed=1
    if [ "$failed" -ne 0 ]; then
        print_error "A later launch of this project may still join $network: see the error above."
        return 1
    fi
    echo "Nothing saved for this project names $network now: a plain $tool, and a restore after a reboot, will not join it without asking."
}

# Whether anything saved for this project would bring network $1 back ($2 = the saved default).
# A store that cannot be read counts as naming it, so the forget that follows reports the error.
_network_remembered() {
    local named listing
    [ -n "$2" ] && [ "$2" = "$1" ] && return 0
    named=$(_quick_launch_network) || return 0
    [ "$named" = "$1" ] && return 0
    listing=$(ccy_registry_forget_network "$PWD" "$1" --check) || return 0
    [ -n "$listing" ]
}

# Ask, on the terminal, whether to clear the saved default network $1 ($2 = tool name).
# Succeeds only on a yes: Enter, a no, end of input, three unreadable answers or no terminal
# at all keep it. Reads CCY_TTY (default /dev/tty), not stdin, so a piped stdin cannot answer
# for the user.
_confirm_clear_saved_network() {
    local tty="${CCY_TTY:-/dev/tty}" open_error
    echo "The saved default network is $1: every plain launch in this project joins it." >&2
    # Opened once to see whether it can be: a redirection that fails prints bash's own line,
    # which names a device path and not what to do.
    if ! open_error=$({ : <"$tty"; } 2>&1); then
        echo "There is no terminal to ask on (${open_error##*: }), so the saved default network was kept: $1" >&2
        echo "To clear it without being asked, name it: ${2:-ccy} --disconnect $1" >&2
        return 1
    fi
    if ! _ask_clear_saved_network "$1" <"$tty"; then
        echo "Kept the saved default network: $1" >&2
        return 1
    fi
}

# The y/N loop itself, reading stdin, which _confirm_clear_saved_network points at the
# terminal. A terminal that cannot be opened fails that redirection, so this never runs.
_ask_clear_saved_network() {
    local reply attempt=1 max_tries=3
    while :; do
        if ! read -rp "Clear the saved default network $1? [y/N] " reply; then
            echo "" >&2
            echo "No answer (end of input)." >&2
            return 1
        fi
        case "$reply" in
            y | Y | yes | YES) return 0 ;;
            "" | n | N | no | NO) return 1 ;;
        esac
        echo "  '$reply' is not y or n." >&2
        attempt=$((attempt + 1))
        if [ "$attempt" -gt "$max_tries" ]; then
            echo "Giving up after $max_tries attempts." >&2
            return 1
        fi
    done
}

# Detach a network from the current project's running container(s): the undo for a wrong
# `--connect`. That also saved the network as the project's default, so the default is
# cleared too when it names this network, or the network would return on the next launch.
# Args: $1 = network_name (optional: pick from the attached project networks when empty),
#       $2 = container_suffix ("_yolo" or "_browser"), $3 = tool_name (for display)
disconnect_from_network() {
    local network_name="$1"
    local container_suffix="$2"
    local tool_name="${3:-ccy}"
    local project_name saved
    project_name=$(get_project_name)
    if ! saved=$(load_network_preference); then
        saved=""
    fi

    local containers=()
    _read_project_running_containers containers "$container_suffix" || return 1

    # No container to detach, but what is saved for later launches outlives it. Named, the
    # user said which network; bare, nothing did, so clearing the saved default is asked first.
    if [ ${#containers[@]} -eq 0 ]; then
        if [ -n "$network_name" ] && _network_remembered "$network_name" "$saved"; then
            echo "No running container for project $project_name, so nothing was detached."
            _forget_network_for_later_launches "$network_name" "$saved" "$tool_name"
            return $?
        fi
        if [ -n "$saved" ] && [ -z "$network_name" ]; then
            echo "No running container for project $project_name, so there is nothing to detach." >&2
            _confirm_clear_saved_network "$saved" "$tool_name" || return 1
            _forget_network_for_later_launches "$saved" "$saved" "$tool_name"
            return $?
        fi
        print_error "No running containers found for project: $project_name"
        echo "Start $tool_name first, then run this in another terminal:" >&2
        echo "  $tool_name --disconnect <network_name>" >&2
        if [ -n "$saved" ]; then
            echo "The saved default network is $saved; '$tool_name --disconnect $saved' clears it." >&2
        fi
        local quick_launch
        if quick_launch=$(_quick_launch_network) && [ -n "$quick_launch" ] && [ "$quick_launch" != "$saved" ]; then
            echo "The Quick Launch configuration joins $quick_launch; '$tool_name --disconnect $quick_launch' clears it." >&2
        fi
        return 1
    fi

    local -A attached=() seen=()
    local all_attached=() candidates=() container nets net
    for container in "${containers[@]}"; do
        if ! nets=$(_container_networks "$container"); then
            print_error "Could not read the networks of $container (the engine's message is above)"
            return 1
        fi
        attached[$container]="$nets"
        while IFS= read -r net; do
            [ -n "$net" ] || continue
            [ -z "${seen[$net]:-}" ] || continue
            seen[$net]=1
            all_attached+=("$net")
            _is_engine_default_network "$net" || candidates+=("$net")
        done <<<"$nets"
    done

    if [ -n "$network_name" ]; then
        if [ -z "${seen[$network_name]:-}" ]; then
            if _network_remembered "$network_name" "$saved"; then
                echo "$network_name is not attached to any running container of $project_name, so nothing was detached."
                _forget_network_for_later_launches "$network_name" "$saved" "$tool_name"
                return $?
            fi
            print_error "$network_name is not attached to any running container of project $project_name"
            echo "Attached: ${all_attached[*]:-(none)}" >&2
            return 1
        fi
    else
        if [ ${#candidates[@]} -eq 0 ]; then
            echo "The running container(s) of $project_name are on no project network to disconnect."
            echo "Attached: ${all_attached[*]:-(none)}"
            if [ -z "$saved" ]; then
                echo "No saved default network for this project."
            else
                echo "The saved default network is $saved; '$tool_name --disconnect $saved' clears it."
            fi
            return 0
        fi

        {
            echo ""
            echo "════════════════════════════════════════════════════════════════════════════════"
            echo "Disconnect YOLO Container from a Network"
            echo "════════════════════════════════════════════════════════════════════════════════"
            echo ""
            echo "Networks the running container(s) of $project_name are on:"
            echo ""
            local i
            for i in "${!candidates[@]}"; do
                echo "  $((i + 1))) ${candidates[$i]}"
            done
            echo ""
        } >&2

        local attempt=1 max_tries=3 selection
        while :; do
            if ! read -rp "Select network to disconnect [1-${#candidates[@]}]: " selection; then
                echo "" >&2
                echo "Cancelled, no input. Nothing was disconnected." >&2
                return 1
            fi
            if [[ "$selection" =~ ^[0-9]+$ ]] && [ "$selection" -ge 1 ] && [ "$selection" -le ${#candidates[@]} ]; then
                network_name="${candidates[$((selection - 1))]}"
                break
            fi
            echo "  '$selection' is not a choice. Enter a number from 1 to ${#candidates[@]}." >&2
            attempt=$((attempt + 1))
            if [ "$attempt" -gt "$max_tries" ]; then
                echo "Giving up after $max_tries attempts. Nothing was disconnected." >&2
                return 1
            fi
        done
    fi

    # A container's LAST network is never detached. That is the usual undo: a session
    # launched with --network <saved> is on that network alone, and detaching it would cut
    # the session off from everything, the Claude API included. Checked for every container
    # before any is touched, so a refusal changes no container. What would bring the network
    # back on a later launch is still cleared: that is the half a relaunch depends on.
    local stranded=()
    for container in "${containers[@]}"; do
        grep -qxF -- "$network_name" <<<"${attached[$container]}" || continue
        [ "$(grep -c . <<<"${attached[$container]}")" -gt 1 ] || stranded+=("$container")
    done
    if [ ${#stranded[@]} -gt 0 ]; then
        print_error "Not disconnecting from $network_name: it is the only network of ${stranded[*]}, so the session would lose all networking, the Claude API included. No container was changed."
        if _forget_network_for_later_launches "$network_name" "$saved" "$tool_name"; then
            echo "The running session stays on $network_name until it ends. To take it off, end it and start it again: a plain $tool_name, or $tool_name --no-network." >&2
        fi
        return 1
    fi

    # Stop at the first refusal: the saved default is only cleared once the network is
    # really gone from every container that had it.
    local detached=0 error_output
    for container in "${containers[@]}"; do
        grep -qxF -- "$network_name" <<<"${attached[$container]}" || continue
        echo "Disconnecting $container from $network_name..."
        if ! error_output=$(container_cmd network disconnect "$network_name" "$container" 2>&1); then
            print_error "$CONTAINER_ENGINE refused to disconnect $container from $network_name:"
            echo "  ${error_output:-(no output)}" >&2
            if [ "$detached" -gt 0 ]; then
                echo "  $detached container(s) were disconnected before this one." >&2
            fi
            echo "Nothing saved for later launches was changed: the saved default, Quick Launch and restore records are as they were." >&2
            return 1
        fi
        echo "  ✓ Disconnected"
        detached=$((detached + 1))
    done
    echo ""
    _forget_network_for_later_launches "$network_name" "$saved" "$tool_name"
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
    # Explicit fallback rather than `|| echo`: a failed inspect and a genuinely
    # empty network both yield 0 here, but the assignment now says so plainly
    # instead of laundering the failure through a substituted value.
    container_count=$(container_cmd network inspect "$network_name" --format '{{len .Containers}}' 2>/dev/null) || container_count=0

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
        read -rp "$CCY_PROMPT_COMPOSE_START $compose_name up -d? [Y/n]: " start_choice
        start_choice=${start_choice:-Y}
        echo ""

        case "$start_choice" in
            Y|y|Yes|yes)
                echo "Starting $compose_name..."
                if $compose_cmd up -d; then
                    echo ""
                    echo "✓ Compose services started"
                    echo ""
                    # Track for session-end teardown offer (read by claude-yolo after container exits)
                    export CCY_COMPOSE_WAS_STARTED=true
                    export CCY_COMPOSE_CMD="$compose_cmd"
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

# Check if any compose services are currently running in this directory
# Sets COMPOSE_NETWORK if a running container is found on a non-default network
# Returns: 0 if at least one service is running, 1 if not or undetermined
_compose_already_running() {
    # Determine available compose command (mirrors _do_compose_start logic)
    local compose_cmd=""
    if [[ "$CONTAINER_ENGINE" = "podman" ]]; then
        local podman_compose_path
        if podman_compose_path=$(command -v podman-compose 2>/dev/null) && [[ -n "$podman_compose_path" ]]; then
            compose_cmd="podman-compose"
        fi
    else
        local docker_compose_path
        if docker_compose_path=$(command -v docker-compose 2>/dev/null) && [[ -n "$docker_compose_path" ]]; then
            compose_cmd="docker-compose"
        else
            local dc_version
            if dc_version=$(docker compose version 2>/dev/null) && [[ -n "$dc_version" ]]; then
                compose_cmd="docker compose"
            fi
        fi
    fi
    [[ -z "$compose_cmd" ]] && return 1

    # Get container IDs reported by compose (includes stopped containers)
    local compose_output
    local container_ids=()
    if compose_output=$($compose_cmd ps -q 2>/dev/null) && [[ -n "$compose_output" ]]; then
        mapfile -t container_ids < <(echo "$compose_output" | grep -v '^$')
    fi
    [[ ${#container_ids[@]} -eq 0 ]] && return 1

    # Check each container ID to confirm at least one is actually running
    local cid
    for cid in "${container_ids[@]}"; do
        local running_state
        if running_state=$(container_cmd inspect "$cid" --format '{{.State.Running}}' 2>/dev/null) \
            && [[ "$running_state" == "true" ]]; then
            # Extract its non-default network so CCY can join it
            # \$ produces a literal $ in double-quoted strings (Go template vars, not shell vars)
            local net_fmt net_output net=""
            net_fmt="{{range \$k, \$v := .NetworkSettings.Networks}}{{\$k}} {{end}}"
            if net_output=$(container_cmd inspect "$cid" --format "$net_fmt" 2>/dev/null); then
                net=$(echo "$net_output" | tr ' ' '\n' | grep -vE '^(bridge|host|none|podman|)$' | head -1)
            fi
            [[ -n "$net" ]] && COMPOSE_NETWORK="$net"
            return 0
        fi
    done

    return 1
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

    # If services are already running, use their network without prompting
    if _compose_already_running; then
        return 0
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

    # Check if network has dns_enabled (only those use aardvark-dns).
    #
    # The status is checked rather than discarded. Previously a failed inspect
    # produced empty output, which is != "true", so the function returned 0 —
    # reporting "no fix needed" when in truth it had been unable to look. That is
    # a silent skip of a repair, which this repo's first rule prohibits.
    local dns_enabled inspect_err
    if ! dns_enabled="$(container_cmd network inspect "$network_name" --format '{{.DNSEnabled}}' 2>&1)"; then
        inspect_err="$dns_enabled"
        echo "  Warning: could not inspect network '$network_name' — DNS check skipped." >&2
        echo "  ${CONTAINER_ENGINE} said: ${inspect_err:-(no output)}" >&2
        return 1
    fi

    if [[ "$dns_enabled" != "true" ]]; then
        # Network doesn't use aardvark-dns, no fix needed
        return 0
    fi

    # Check current DNS servers on the network.
    # Status checked: a failed inspect used to read as "no DNS servers
    # configured", which sent the function on to CHANGE the network based on a
    # read that never happened.
    local current_dns
    if ! current_dns="$(container_cmd network inspect "$network_name" --format '{{json .NetworkDNSServers}}' 2>&1)"; then
        echo "  Warning: could not read DNS servers for '$network_name' — not modifying it." >&2
        echo "  ${CONTAINER_ENGINE} said: ${current_dns:-(no output)}" >&2
        return 1
    fi

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
export -f load_network_preference
export -f connect_to_network
export -f disconnect_from_network
export -f check_and_start_compose_services
export -f offer_compose_start
