#!/bin/bash
# CCY agent team bus membership: what a launch tells its container about the bus (Plan 00203
# SPEC-hot-control.md D1, D5, H1, H2, H33b, H35).
#
# A session whose own pane ccy's tmux started has a registry name (the one ccy-sessions lists;
# ccy_tmux_current_session, never the name of a pane it was merely run from). It is
# labelled ccy-session=<name>, the host's only map from a session to its container, bus or not.
# Unless launched with --no-bus it also carries the agent team bus kit, even on no team, so the
# owner can put it in one later: `ccy-teams internal membership-init` writes generation 1 of its
# desired membership (the launch's seats, or none) into a directory only the owner's tools
# write, $XDG_RUNTIME_DIR/ccy/membership/<name>/, and prints it; the container gets it mounted
# read-only at /run/ccy/membership, relabelled per session on SELinux, and
# PINGBUS_MEMBERSHIP=/run/ccy/membership. A launch in a plain terminal has no name: with --teams
# its membership directory is named after its container (it gets its seats but no label, so it
# cannot be changed while it runs); without, it carries no kit. --no-bus, and that last case,
# pass PINGBUS_MEMBERSHIP=off. The entrypoint refuses a launch that states neither.
#
# Once the container has gone, the launcher's cleanup runs ccy_bus_disconnected (Plan 00194):
# a session that ended without its goodbye (a closed terminal, podman stop, a crash: the
# container ends at once, so no hook runs) is told to its teams by the host's pingbus.
#
# Requires print_error (common-pure.bash), ccy_tmux_current_session (tmux-session.bash) and
# the launcher's CCY_MOUNT_RELABEL.

# The host's pingbus, which play-agent-bus.yml installs; CCY_TEAMS_PINGBUS overrides it, as for
# ccy-teams.
CCY_PINGBUS_COMMAND="${CCY_TEAMS_PINGBUS:-/usr/local/share/agent-bus/kit/pingbus}"

# ccy_no_bus_check <no-bus> <teams> — refuse (64) --no-bus beside any team (<teams> not empty):
# the flag leaves the bus out. The launcher asks right after its parsing, and again for the
# seats it is about to take, which a Quick Launch may have brought back.
ccy_no_bus_check() {
    [ "${1:?ccy_no_bus_check requires true or false}" != true ] || [ -z "${2?ccy_no_bus_check requires the teams}" ] || {
        print_error "--no-bus leaves the agent team bus out, so it cannot be used with a team (--teams, or Quick Launch's). Drop one."
        return 64
    }
}

# ccy_membership_args <canonical-list> <no-bus> <container> — fill CCY_SEAT_RUN_ARGS with the
# bus's container arguments, and, for a session carrying the kit, CCY_BUS_DISCONNECT with what
# ccy_bus_disconnected needs: the checkout's seats, the session's name and the launch's start
# in epoch milliseconds.
#   status 0   done
#   64         --no-bus with seats to take
#   otherwise  the session could not be named, or membership-init's own code; the reason on
#              stderr
ccy_membership_args() {
    local canonical="${1?ccy_membership_args requires the canonical list, or an empty one}"
    local no_bus="${2:?ccy_membership_args requires true or false}"
    local container="${3:?ccy_membership_args requires the container name}" session dir rc
    CCY_SEAT_RUN_ARGS=() CCY_BUS_DISCONNECT=()
    ccy_no_bus_check "$no_bus" "$canonical" || return
    session=$(ccy_tmux_current_session) || return 1
    [ -z "$session" ] || CCY_SEAT_RUN_ARGS+=(--label "ccy-session=$session")
    if [ "$no_bus" = true ] || [ -z "$session$canonical" ]; then
        CCY_SEAT_RUN_ARGS+=(-e PINGBUS_MEMBERSHIP=off)
        # tmux reuses a name once its session ends: the membership an earlier session left under
        # this one is not this launch's (Plan 00203, Fable's M6 review B1).
        [ -z "$session" ] || rm -rf -- "${XDG_RUNTIME_DIR:?}/ccy/membership/$session"
        return
    fi
    dir=$(ccy-teams internal membership-init "${session:-$container}" "$PWD" "$canonical") || {
        rc=$?
        print_error "Not starting: ccy-teams internal membership-init failed (exit $rc; the reason is above)."
        return "$rc"
    }
    CCY_SEAT_RUN_ARGS+=(-v "$dir:/run/ccy/membership:ro${CCY_MOUNT_RELABEL:+,Z}" -e PINGBUS_MEMBERSHIP=/run/ccy/membership)
    CCY_BUS_DISCONNECT=("$PWD/.claude/ccy/pingbus/seats" "${session:-$container}" "$(date +%s%3N)" "$container")
}

# The most `pingbus seat disconnected` may take: its wait for each seat (15 s) and the posts.
CCY_BUS_DISCONNECT_TIMEOUT_S="${CCY_BUS_DISCONNECT_TIMEOUT_S:-60}"

# _ccy_bus_log <text> [--error] — a line in the checkout's disconnect.log, which pingbus seat
# disconnected writes too (cli.DISCONNECT_LOG), owner-only; with --error, on stderr first. A
# closed terminal takes stderr with it (a write fails with EIO), so the log is the record then,
# and a failed write to either never stops the cleanup: the teams are still to be told.
_ccy_bus_log() {
    local log="${CCY_BUS_DISCONNECT[0]%/*}/disconnect.log" gone=""
    if [ "${2:-}" = --error ] && ! print_error "$1"; then gone=" (stderr has gone)"; fi
    if ! (umask 077 && printf '%s launcher session=%s %s%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        "${CCY_BUS_DISCONNECT[1]}" "$1" "$gone" >>"$log"); then
        if ! print_error "could not write $log"; then return 0; fi
    fi
}

# ccy_bus_disconnected — at the launcher's exit: `pingbus seat disconnected` tells each team of
# a seat this launch held that ended without its goodbye. Nothing for a launch without the kit
# or a checkout with no seats. A launcher leaving on a signal (a closed terminal's SIGHUP, a
# SIGTERM) gets here with its container still running and holding the seats: bash runs the
# EXIT trap without waiting for podman, which hears of the hangup only once the launcher has
# gone. So a container still running is stopped first, as `podman stop` would. Each outcome is
# a line in the disconnect log. A failure is not the launcher's: the session has ended, and a
# restart still goes ahead. The launcher's cleanup ignores SIGHUP and SIGINT meanwhile;
# `timeout` bounds pingbus and gives it a process group of its own.
ccy_bus_disconnected() {
    [ -n "${CCY_BUS_DISCONNECT[0]:-}" ] && [ -d "${CCY_BUS_DISCONNECT[0]}" ] || return 0
    local rc=0 running container="${CCY_BUS_DISCONNECT[3]}"
    if ! running=$(container_cmd ps -q --filter "name=^$container\$"); then
        _ccy_bus_log "could not ask whether the container $container still runs (the reason is above); telling the teams anyway" --error
    elif [ -n "$running" ]; then
        _ccy_bus_log "the container $container still ran at the launcher's exit (a hangup or a TERM): stopping it first"
        container_cmd stop -t 10 "$container" >/dev/null || _ccy_bus_log "podman stop $container failed (exit $?)" --error
    fi
    timeout "$CCY_BUS_DISCONNECT_TIMEOUT_S" "$CCY_PINGBUS_COMMAND" seat disconnected \
        --root "${CCY_BUS_DISCONNECT[0]}" --session "${CCY_BUS_DISCONNECT[1]}" \
        --since "${CCY_BUS_DISCONNECT[2]}" || rc=$?
    if [ "$rc" -eq 0 ]; then
        _ccy_bus_log "pingbus seat disconnected exited 0"
    elif [ "$rc" -eq 124 ]; then
        _ccy_bus_log "pingbus seat disconnected was stopped after $CCY_BUS_DISCONNECT_TIMEOUT_S s: the team may not have been told this session ended without a goodbye." --error
    else
        _ccy_bus_log "pingbus seat disconnected failed (exit $rc; the reason is above): the team was not told this session ended without a goodbye." --error
    fi
}
