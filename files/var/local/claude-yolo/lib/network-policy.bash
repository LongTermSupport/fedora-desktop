#!/bin/bash
# A ccy session's network policy (Plan 00232): the Go command ccy-network-policy holds it
# (internal/netpolicy, installed by play-claude-yolo.yml into ~/.local/bin). The launch runs the
# session's container through it (ccy_agent_container, lib/agent-choice.bash), and --connect and
# --disconnect refresh every live session's allow sets after changing its networks; --connect
# first refuses, plainly, a session started without a network (_container_can_join_networks).
#
# Requires print_error (common-pure.bash), connect_to_network, disconnect_from_network and
# container_cmd (network-management.bash), CONTAINER_ENGINE (common.bash) and the launcher's
# CONTAINER_NAME and IMAGE_NAME.

# ccy_network_policy <args...> — the command, or 1 naming the play when it is not installed (the
# launch's own 127 would read as the container's).
ccy_network_policy() {
    command -v ccy-network-policy >/dev/null || {
        print_error "ccy-network-policy is not installed, so ccy cannot hold the session's network policy: run play-claude-yolo.yml"
        return 1
    }
    ccy-network-policy "$@"
}

# ccy_network_policy_launch <engine run args...> — the engine's run under the session's policy;
# the container's exit status.
ccy_network_policy_launch() {
    ccy_network_policy launch --name "$CONTAINER_NAME" --image "$IMAGE_NAME" -- "$CONTAINER_ENGINE" "$@"
}

# ccy_network_change <connect-mode> <disconnect-mode> <connect-network> <disconnect-network> —
# ccy --connect or --disconnect, then the live sessions' policies refreshed. Both at once would
# attach and detach in one breath, so it is refused (1) rather than one silently winning.
ccy_network_change() {
    if [ "$1" = true ] && [ "$2" = true ]; then
        print_error "--connect and --disconnect cannot be used together"
        return 1
    fi
    if [ "$2" = true ]; then
        disconnect_from_network "$4" "_yolo" "ccy" || return
    else
        connect_to_network "$3" "_yolo" "ccy" || return
    fi
    ccy_network_policy refresh
}

# _container_can_join_networks <container> <network> <indent> — fails, saying so and naming the
# way out, for a session started without a network (--no-network, or a headless replay of an
# empty saved one: pasta; or slirp4netns, none, host, another's namespace). The engine can attach
# no network to it, and its own refusal ("pasta" is not supported: invalid network mode) reads
# as a fault. Fails with the engine's message when the container cannot be inspected.
_container_can_join_networks() {
    local container="$1" network="$2" indent="$3" mode
    mode=$(container_cmd container inspect "$container" --format '{{.HostConfig.NetworkMode}}') || return 1
    case "$mode" in
        pasta | slirp4netns | none | host | container:* | ns:*)
            echo "${indent}✗ $container was started without a network ($mode), and a session started without one cannot join one."
            echo "${indent}  Relaunch it with --network $network to start it on that network."
            return 1
            ;;
    esac
}
