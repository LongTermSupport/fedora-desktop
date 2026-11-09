#!/bin/bash
# The Docker side of ccy's cross-engine network cleanup: the launcher's network-mismatch menu,
# option 1, when CCY runs on Podman and the project's networks are Docker's. Every listing,
# stop and removal is asked of the Go front beside the bash (ccylaunch F20-F22), which says
# what failed in the engine's words, so a cleanup that could not finish stops there and never
# goes on to start compose on a half-cleaned engine. Needs SCRIPT_DIR.

# ccy_docker_network_cleanup project NETWORK...: stops the containers on each network, then
# removes it; a network already gone (the front's 5) goes on to the next.
# ccy_docker_network_cleanup all: stops every running Docker container, then prunes every
# unused Docker network.
# Returns 1 at the first step that failed, its cause on stderr; 64 for another mode.
ccy_docker_network_cleanup() {
    local mode="${1:-}" net listed
    local -a containers
    [ $# -eq 0 ] || shift
    case "$mode" in
        project)
            echo "Stopping containers and removing project networks..."
            for net in "$@"; do
                listed=$("$SCRIPT_DIR/claude-yolo" --list-containers docker "network=$net") || return 1
                if [ -n "$listed" ]; then
                    mapfile -t containers <<<"$listed"
                    echo "  Stopping containers on $net: ${containers[*]}"
                    "$SCRIPT_DIR/claude-yolo" --stop-containers docker "${containers[@]}" || return 1
                fi
                echo "  Removing network: $net"
                "$SCRIPT_DIR/claude-yolo" --remove-network docker "$net" || [ $? -eq 5 ] || return 1
            done
            ;;
        all)
            echo "Stopping all Docker containers..."
            listed=$("$SCRIPT_DIR/claude-yolo" --list-containers docker) || return 1
            if [ -n "$listed" ]; then
                mapfile -t containers <<<"$listed"
                "$SCRIPT_DIR/claude-yolo" --stop-containers docker "${containers[@]}" || return 1
            fi
            echo "Pruning all unused Docker networks..."
            docker network prune -f || { echo "✗ The unused Docker networks could not be pruned (docker's words above)" >&2; return 1; }
            echo "✓ Docker networks pruned"
            ;;
        *)
            echo "ccy_docker_network_cleanup: unknown mode '$mode' (project or all)" >&2
            return 64
            ;;
    esac
}
