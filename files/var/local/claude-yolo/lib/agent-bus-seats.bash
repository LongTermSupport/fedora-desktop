#!/bin/bash
# CCY agent team bus seats: the host side of `ccy --teams <seat>@<team>[,...]`.
#
# A ccy session is in a team only when its launch names it, in one seat per team (Plan 00161
# DESIGN.md sections 5.3, 5.5 and 5.6). The seat and team grammar is pingbus's own, so this
# library holds no copy of it: `agent-bus seat check` validates a list and prints its
# canonical form, and `agent-bus seat take` creates each named seat that does not exist yet
# (one `sudo agent-bus add-member`) and refuses a seat another session holds. The launcher
# calls check right after its argument parsing and take just before the container starts, and
# exits with their code when either refuses: 64 a malformed list, 75 a held seat, 78 anything
# else that stops the seats being had (a team not running here, a seat that cannot be made).
# A launch without --teams calls neither and passes the container no bus variable.
#
# Requires print_error (common-pure.bash, always loaded first).

# The agent-bus wrapper, which play-agent-bus.yml installs on every desktop.
CCY_AGENT_BUS_COMMAND="agent-bus"

# _ccy_seats_need_agent_bus — refuse (78) when agent-bus is not installed.
_ccy_seats_need_agent_bus() {
    if ! command -v "$CCY_AGENT_BUS_COMMAND" >/dev/null; then
        print_error "--teams needs $CCY_AGENT_BUS_COMMAND on this host, and it is not installed."
        echo "  Install it with play-agent-bus.yml (it runs agent-bus-install software), or launch without --teams." >&2
        return 78
    fi
}

# ccy_seats_check <list> — the canonical form of a --teams list, printed on stdout, as
# `agent-bus seat check` gives it. Changes nothing and runs no sudo.
#   status 0   the list is usable here; its canonical form printed
#   otherwise  agent-bus's own code (64 usage, 78 a team not running here), or 78 when
#              agent-bus is missing or printed no single list; the reason on stderr
ccy_seats_check() {
    local list="${1?ccy_seats_check requires a list}" canonical rc=0
    _ccy_seats_need_agent_bus || return
    canonical=$("$CCY_AGENT_BUS_COMMAND" seat check "$list") || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "--teams '$list' refused by agent-bus seat check (exit $rc; the reason is above)."
        return "$rc"
    fi
    if [ -z "$canonical" ] || [[ "$canonical" == *$'\n'* ]]; then
        print_error "agent-bus seat check accepted '$list' but did not print one canonical list; update agent-bus (play-agent-bus.yml)."
        return 78
    fi
    printf '%s\n' "$canonical"
}

# ccy_seats_take <canonical-list> <unattended> — claim the launch's seats on the host, creating
# any that do not exist, and fill CCY_SEAT_RUN_ARGS with what the container then gets:
# PINGBUS_SEATS and the ccy-seats label. An empty list (a plain launch) calls nothing and
# leaves CCY_SEAT_RUN_ARGS empty. Unattended (true: headless, a restore or a restart), sudo
# never prompts. take's own report goes to stderr, so a headless session's stdout stays its own.
#   status 0   the seats are ready
#   otherwise  agent-bus seat take's code (75 a held seat, 78 a seat that cannot be made), or
#              78 when agent-bus is missing; the reason on stderr
ccy_seats_take() {
    local canonical="${1?ccy_seats_take requires the canonical list, or an empty one}"
    local unattended="${2:?ccy_seats_take requires true or false}" rc=0
    CCY_SEAT_RUN_ARGS=()
    [ -n "$canonical" ] || return 0
    _ccy_seats_need_agent_bus || return
    local take=("$CCY_AGENT_BUS_COMMAND" seat take "$canonical")
    if [ "$unattended" = true ]; then
        take+=(--no-prompt)
    fi
    "${take[@]}" >&2 || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "Not starting: agent-bus seat take could not seat this session in $canonical (exit $rc; the reason is above)."
        echo "  End the session that holds a seat, choose another seat, or launch without --teams." >&2
        return "$rc"
    fi
    CCY_SEAT_RUN_ARGS+=(-e "PINGBUS_SEATS=$canonical" --label "ccy-seats=$canonical")
}

# ccy_teams_canonical_args <flag-pos> <value-pos> <canonical> [args...] — the launcher's
# arguments with its --teams flag (at flag-pos, 1-based) and its value (at value-pos, the same
# position for --teams=<list>) replaced by `--teams <canonical>`, NUL separated. A restart and
# a restore replay these, so they come back in the same seats, spelled one way.
ccy_teams_canonical_args() {
    local flag_pos="${1:?ccy_teams_canonical_args requires the flag position}"
    local value_pos="${2:?ccy_teams_canonical_args requires the value position}"
    local canonical="${3:?ccy_teams_canonical_args requires the canonical list}"
    shift 3
    local pos=0 arg
    for arg in "$@"; do
        pos=$((pos + 1))
        if [ "$pos" -eq "$flag_pos" ]; then
            printf '%s\0' --teams "$canonical"
        elif [ "$pos" -gt "$flag_pos" ] && [ "$pos" -le "$value_pos" ]; then
            continue
        else
            printf '%s\0' "$arg"
        fi
    done
}
