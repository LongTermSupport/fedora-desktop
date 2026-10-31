#!/bin/bash
# Claude YOLO Docker Health Library
# Handles zombie container detection, overlay2 migration, and docker health checks
#
# Version: 1.2.0 - Plan 00075: get_container_stats() made a total contract
#                  (returns 0 with an "unknown" sentinel instead of a status that
#                  errexit turned into a dead `ccy --top`), and the three bare
#                  inspect/ps captures given explicit fallbacks so a container
#                  exiting mid-scan can no longer truncate the zombie list or
#                  kill the status table.
#          1.1.0 - Container engine abstraction (docker/podman support)

# Wrong answers a container menu takes before it gives up (return 2), so a menu whose input
# is not a person cannot loop for ever.
CCY_CONTAINER_MENU_TRIES=3

# ═══════════════════════════════════════════════════════════════════════════════
# ZOMBIE CONTAINER DETECTION
# ═══════════════════════════════════════════════════════════════════════════════

# Find CCY containers that have lost their controlling terminal
# These are containers started with -it but whose docker run process has died
# Returns: newline-separated list of container names; 1 when the running containers could not
# be listed (the Go front's cause on stderr), never "none found"
find_zombie_containers() {
    local suffix="${1:-yolo}"  # Default to "yolo" containers
    local zombies=()

    local containers
    containers=$("$SCRIPT_DIR/claude-yolo" --list-containers "$CONTAINER_ENGINE" "name=_${suffix}") || return 1

    if [ -z "$containers" ]; then
        return 0  # No containers found
    fi
    local inspect_err
    inspect_err=$(mktemp) || return 1

    while IFS= read -r container_name; do
        [ -z "$container_name" ] && continue

        # Check if this container was started with -it (TTY mode)
        local has_tty still why
        # A container can exit between the `ps` above and this inspect, so an inspect that
        # failed asks the listing again: a container gone from it is that race, skipped; one
        # still listed is the engine failing, and the scan fails with the engine's words
        # (kept apart from the answer, so a warning is never read as one).
        if ! has_tty=$(container_cmd inspect --format '{{.Config.Tty}}' "$container_name" 2>"$inspect_err"); then
            why=$(<"$inspect_err")
            still=$("$SCRIPT_DIR/claude-yolo" --list-containers "$CONTAINER_ENGINE" "name=${container_name}") || { rm -f "$inspect_err"; return 1; }
            grep -qxF -- "$container_name" <<<"$still" || continue
            print_error "Container $container_name could not be inspected: $why"
            rm -f "$inspect_err"
            return 1
        fi

        if [ "$has_tty" = "true" ]; then
            # Check if there's a container run process still attached
            # The run process would have the container name in its command line
            if ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${container_name}[^_]" >/dev/null 2>&1 && \
               ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${container_name}$" >/dev/null 2>&1; then
                zombies+=("$container_name")
            fi
        fi
    done <<< "$containers"
    rm -f "$inspect_err"

    printf '%s\n' "${zombies[@]}"
}

# Get detailed stats for a container
# Args: container_name
# Returns: ALWAYS 0, echoing either real stats or "unknown|unknown|unknown".
#
# The contract is deliberately total, because all three callers are table
# renderers that need a row for every container:
#
#   stats=$(get_container_stats "$container")
#
# That is a plain assignment inside a plainly-called function, so `errexit` is
# live. The previous `return 1` therefore did not produce the "unknown" row the
# fallback string exists for — it killed the caller. `ccy --top` printed its
# header and exited silently the first time podman could not report on a
# container (one that exited between the `ps` and the `stats`, or a cgroup setup
# where stats are unavailable).
#
# An earlier pass through this file annotated the line below FAIL-FAST-OK on the
# reasoning that "emptiness is handled on the next line, so the failure is
# surfaced". That reasoning was wrong: it read the function in isolation and
# missed that the caller's context turns the return into an abort. Recorded
# because the annotation looked perfectly sound.
get_container_stats() {
    local container_name="$1"

    # Get stats in one call
    local stats
    # Belt and braces, and NOT the thing that fixed the dead `ccy --top`.
    # Measured, not assumed: a failing bare capture inside a function invoked as
    # `$(fn)` does NOT abort it — errexit bites on the CALLER's plain assignment
    # when this function propagates a non-zero, which is why removing the old
    # `return 1` is what actually fixed the symptom. The fallback is here so the
    # total contract above holds however this function comes to be called.
    stats=$(container_cmd stats --no-stream --format '{{.CPUPerc}}|{{.MemUsage}}|{{.PIDs}}' "$container_name" 2>/dev/null) || stats=""

    if [ -z "$stats" ]; then
        echo "unknown|unknown|unknown"
        return 0
    fi

    echo "$stats"
}

# Get container uptime/age
# Args: container_name
# Returns: human-readable uptime string, or "unknown" if the engine cannot say.
# CONTRACT: total — always exits 0. Callers use it as `uptime=$(...)` in a plain
# assignment under errexit, so a propagated non-zero would kill the caller rather
# than produce a row. Same reasoning as get_container_stats() above.
get_container_uptime() {
    local container_name="$1"
    local uptime
    uptime=$(container_cmd ps --filter "name=^${container_name}$" --format '{{.RunningFor}}' 2>/dev/null) || uptime=""
    printf '%s\n' "${uptime:-unknown}"
}

# Show zombie container management TUI
# Args: the zombie containers' names
# Returns: 0 if zombies were handled, 1 if user cancelled, 2 when no answer came or a stop
# failed (the Go front's cause on stderr)
show_zombie_container_tui() {
    local zombies=("$@")

    local rule="════════════════════════════════════════════════════════════════════════════════"
    printf '\n%s\n%s\n%s\n\n' "$rule" "⚠️  Orphaned Containers Detected" "$rule"
    printf '%s\n' "Found ${#zombies[@]} container(s) running without an attached terminal." \
        "These containers are consuming resources but have no active session." "" "This typically happens when:" \
        "  • The terminal running ccy was closed" "  • The SSH connection was lost" "  • The parent process was killed" ""

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

    printf '\n%s\n\n%s\n' "$rule" "Options:"
    printf '%s\n' "  [a] Stop ALL orphaned containers (recommended)" "  [s] Select specific containers to stop" \
        "  [i] Ignore and continue (containers will keep running)" "  [q] Quit without starting new session" ""

    local attempts=0
    while true; do
        if ! read -rp "$CCY_PROMPT_ZOMBIE_MENU " choice; then
            echo "No answer: the input closed. Not starting a session." >&2
            return 2
        fi
        echo ""

        case "$choice" in
            a|A)
                echo "Stopping all orphaned containers..."
                "$SCRIPT_DIR/claude-yolo" --stop-containers "$CONTAINER_ENGINE" "${zombies[@]}" || return 2
                echo ""
                echo "✓ All orphaned containers stopped"
                return 0
                ;;
            s|S)
                echo "Enter container numbers to stop (space-separated, e.g., '1 3 4'):"
                read -rp "> " selections

                local chosen=()
                for sel in $selections; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#zombies[@]} ]; then
                        chosen+=("${zombies[$((sel-1))]}")
                    else
                        echo "  Invalid selection: $sel (skipped)"
                    fi
                done
                [ ${#chosen[@]} -eq 0 ] || "$SCRIPT_DIR/claude-yolo" --stop-containers "$CONTAINER_ENGINE" "${chosen[@]}" || return 2
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
                attempts=$((attempts + 1))
                if [ "$attempts" -ge "$CCY_CONTAINER_MENU_TRIES" ]; then
                    echo "No valid answer in $CCY_CONTAINER_MENU_TRIES tries. Not starting a session." >&2
                    return 2
                fi
                echo "Invalid choice. Please enter a, s, i, or q."
                echo ""
                ;;
        esac
    done
}

# Quick check for zombie containers at startup
# This is meant to be called early in ccy startup
# Args: suffix (default: "yolo"), restoring (true when this launch is a session restore)
# Returns: 0 to continue, 1 when the person quit, 2 when no answer came or the containers
# could not be listed or stopped
#
# A restore answers "ignore" without asking: nobody is at a restored session to answer, and
# ignoring is the one choice that destroys nothing. A container still running after a boot
# was started after it, by a sibling restore or by a person, and its state cannot be
# proved dead from here. (Stopped containers are a different, provable case:
# clean_stale_containers_startup removes those for every launch.)
check_zombie_containers_startup() {
    local suffix="${1:-yolo}" restoring="${2:-false}"

    # A scan that could not list the containers fails the launch (2), never "none found".
    local zombies=() found
    found=$(find_zombie_containers "$suffix") || return 2
    while IFS= read -r line; do
        [[ -n "$line" ]] && zombies+=("$line")
    done <<<"$found"

    local zombie_count=${#zombies[@]}

    if [ "$zombie_count" -gt 0 ]; then
        if [ "$restoring" = true ]; then
            echo "Unattended launch: leaving ${zombie_count} container(s) without a terminal running (${zombies[*]}); a launch nobody answers never stops a container it cannot prove dead." >&2
            return 0
        fi
        show_zombie_container_tui "${zombies[@]}"
        return $?
    fi

    return 0
}

# Clean up stale (stopped/dead) CCY containers at startup
# These are leftover from unclean shutdowns where --rm didn't fire
# Auto-removes without prompting since these containers are unrecoverable
# (exited or created: the states docker and podman share; the engine ORs a repeated filter).
# Args: suffix (default: "yolo")
# Returns: 0 when listed (each removal's failure is shown with its cause), 1 when the stopped
# containers could not be listed (the Go front's cause on stderr), never "none found"
clean_stale_containers_startup() {
    local suffix="${1:-yolo}"
    local stale=() found

    found=$("$SCRIPT_DIR/claude-yolo" --list-containers "$CONTAINER_ENGINE" --all "name=_${suffix}" status=exited status=created) || return 1
    while IFS= read -r name; do
        [ -n "$name" ] && stale+=("$name")
    done <<<"$found"

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
    clean_stale_containers_startup "$suffix" || return 1

    # Get all containers matching the suffix; a listing that failed ends --top with its cause.
    local containers=() found
    found=$("$SCRIPT_DIR/claude-yolo" --list-containers "$CONTAINER_ENGINE" "name=_${suffix}") || return 1
    while IFS= read -r line; do
        [[ -n "$line" ]] && containers+=("$line")
    done <<<"$found"

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
        # Same race as find_zombie_containers(), but this one is NOT in a
        # subshell — it renders the `ccy --top` table directly. A bare capture
        # here killed the whole table the first time a container exited while it
        # was being listed. With the fallback the row simply reads "active".
        has_tty=$(container_cmd inspect --format '{{.Config.Tty}}' "$container" 2>/dev/null) || has_tty=""
        if [ "$has_tty" = "true" ]; then
            if ! pgrep -f "${CONTAINER_ENGINE} run.*--name[= ]${container}( |$)" >/dev/null 2>&1; then
                status="orphan?"
            fi
        fi

        printf "%-3s %-28s %-8s %-18s %-12s %-8s\n" "$i)" "$container" "$cpu" "$mem" "$uptime" "$status"
        i=$((i + 1))
    done

    printf '\n%s\n\n%s\n' "════════════════════════════════════════════════════════════════════════════════" "Actions:"
    printf '%s\n' "  [number]  Stop specific container (e.g., '1' or '1 2 3')" "  [a]       Stop ALL containers" \
        "  [r]       Refresh stats" "  [q]       Quit" ""

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
                "$SCRIPT_DIR/claude-yolo" --stop-containers "$CONTAINER_ENGINE" "${containers[@]}" || return 1
                echo ""
                echo "✓ All containers stopped"
                return 0
                ;;
            *)
                # Try to parse as space-separated numbers
                local chosen=()
                for sel in $action; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#containers[@]} ]; then
                        chosen+=("${containers[$((sel-1))]}")
                    elif [[ "$sel" =~ ^[0-9]+$ ]]; then
                        echo "Invalid number: $sel (valid range: 1-${#containers[@]})"
                    fi
                done

                if [ ${#chosen[@]} -eq 0 ]; then
                    echo "Invalid input. Enter container numbers, 'a' for all, 'r' to refresh, or 'q' to quit."
                else
                    "$SCRIPT_DIR/claude-yolo" --stop-containers "$CONTAINER_ENGINE" "${chosen[@]}" || return 1
                    echo ""
                    # Show updated list; one that could not be listed is a failure, never "all stopped".
                    local remaining remaining_out
                    remaining_out=$("$SCRIPT_DIR/claude-yolo" --list-containers "$CONTAINER_ENGINE" "name=_${suffix}") || return 1
                    # Not `grep -c .`: grep exits 1 on zero matches, so under
                    # `set -e` an empty (correct, all-stopped) result would abort
                    # the function — the same defect class, reintroduced.
                    if [ -z "$remaining_out" ]; then
                        remaining=0
                    else
                        remaining=$(printf '%s\n' "$remaining_out" | wc -l)
                    fi
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
# Args: project_name, suffix, unattended (true when nobody can answer: a restore, a
# restart or a headless launch)
# Returns: 0 to continue, 1 when the person quit, 2 when no answer came or the containers
# could not be listed or stopped
#
# Unattended, it answers "continue alongside" without asking. Sessions that ran together in
# one project are restored together, and several seats of one team run headless in one
# checkout, so a sibling's container is expected, and stopping it would kill that session.
check_project_containers_startup() {
    local project_name="$1"
    local suffix="${2:-yolo}"
    local restoring="${3:-false}"

    # Get containers for this project; a listing that failed fails the launch (2).
    local containers=() found
    found=$("$SCRIPT_DIR/claude-yolo" --list-containers "$CONTAINER_ENGINE" "name=${project_name}_${suffix}") || return 2
    while IFS= read -r line; do
        [[ -n "$line" ]] && containers+=("$line")
    done <<<"$found"

    if [ ${#containers[@]} -eq 0 ]; then
        return 0  # No containers, continue normally
    fi

    if [ "$restoring" = true ]; then
        echo "Unattended launch: starting alongside ${#containers[@]} running container(s) for this project (${containers[*]})." >&2
        return 0
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

    local attempts=0
    while true; do
        if ! read -rp "$CCY_PROMPT_EXISTING_CONTAINERS " choice; then
            echo "No answer: the input closed. Not starting a session." >&2
            return 2
        fi
        echo ""

        case "$choice" in
            c|C)
                echo "Continuing with new container..."
                return 0
                ;;
            s|S)
                echo "Stopping existing containers..."
                "$SCRIPT_DIR/claude-yolo" --stop-containers "$CONTAINER_ENGINE" "${containers[@]}" || return 2
                echo ""
                return 0
                ;;
            m|M)
                echo "Enter container numbers to stop (space-separated), or 'b' to go back:"
                read -rp "> " selections

                if [ "$selections" = "b" ] || [ "$selections" = "B" ]; then
                    continue
                fi

                local chosen=()
                for sel in $selections; do
                    if [[ "$sel" =~ ^[0-9]+$ ]] && [ "$sel" -ge 1 ] && [ "$sel" -le ${#containers[@]} ]; then
                        chosen+=("${containers[$((sel-1))]}")
                    fi
                done
                [ ${#chosen[@]} -eq 0 ] || "$SCRIPT_DIR/claude-yolo" --stop-containers "$CONTAINER_ENGINE" "${chosen[@]}" || return 2
                echo ""
                return 0
                ;;
            q|Q)
                return 1
                ;;
            *)
                attempts=$((attempts + 1))
                if [ "$attempts" -ge "$CCY_CONTAINER_MENU_TRIES" ]; then
                    echo "No valid answer in $CCY_CONTAINER_MENU_TRIES tries. Not starting a session." >&2
                    return 2
                fi
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
