#!/bin/bash
# CCY agent team bus seats: the host side of `ccy --teams <team>|<seat>@<team>[,...]`.
#
# A ccy session is in a team only when its launch names it, in one seat per team (Plan 00161
# DESIGN.md sections 5.3, 5.5 and 5.6). The seat and team grammar is pingbus's own, so this
# library holds no copy of it: `agent-bus seat check` validates a list and prints its
# canonical form, and `agent-bus seat take` creates each named seat that does not exist yet
# (one `sudo agent-bus add-member`) and refuses a seat another session holds. Both are run by
# `ccy-teams internal seat-check` and `seat-take`, which answer a teamchat list themselves
# (Plan 00209 Task 4.2). The launcher
# calls check right after its argument parsing and take just before the container starts, and
# exits with their code when either refuses: 64 a malformed list, 75 a held seat, 78 anything
# else that stops the seats being had (a team not running here, a seat that cannot be made).
# A launch without --teams calls neither. What the container is told about the bus, seats or
# none, is lib/agent-bus-membership.bash's (Plan 00203).
#
# A team named alone (`ccy --teams long-term-support`, Plan 00177 Task 4.8) is the normal
# form on a terminal: the launcher has `ccy-teams choose-teams` show the checkout's seats in
# it in a gum chooser, offer the seat's role (keep it, make it the team's coordinator, stop it
# being one; Plan 00196), and print the chosen `<seat>@<team>` list, which then replaces the
# given one in the launch's arguments like any canonical list, so a restart or a restore
# replays the seat. A launch with nobody to choose (headless, no terminal, a restore, a
# restart) checks with `--resolved`, which refuses a team alone.
#
# Requires print_error (common-pure.bash, always loaded first) and ccy_membership_args
# (agent-bus-membership.bash). agent-bus itself is needed only for a list on agent-bus: ccy-teams
# decides each list's system and refuses (78) an agent-bus list when agent-bus is missing, so a
# teamchat launch runs on a host without it.

# ccy_seats_check <list> <attended> [claude-args...] — the canonical form of a --teams list,
# printed on stdout, as `agent-bus seat check` gives it. Attended (true: a person at a
# terminal), a team named alone is kept for the chooser; otherwise it is refused (64). A launch
# that takes seats with claude's -c/--continue is refused (64, the owner's ruling Q67): it
# resumes the checkout's newest conversation, which in a team checkout may be another seat's.
# The arguments are never rewritten; the refusal names `ccy --resume`. Changes nothing and
# runs no sudo.
#   status 0   the list is usable here; its canonical form printed
#   otherwise  ccy-teams' or agent-bus's own code (64 usage, 78 a team not running here or
#              agent-bus missing for an agent-bus list), 64 for --continue, or 78 when no
#              single list was printed; the reason on stderr
ccy_seats_check() {
    local list="${1?ccy_seats_check requires a list}" canonical rc=0 arg
    local attended="${2:?ccy_seats_check requires true or false}"
    shift 2
    local check=(ccy-teams internal seat-check "$list")
    if [ "$attended" != true ]; then
        check+=(--resolved)
    fi
    canonical=$("${check[@]}") || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "--teams '$list' refused by agent-bus seat check (exit $rc; the reason is above)."
        return "$rc"
    fi
    if [ -z "$canonical" ] || [[ "$canonical" == *$'\n'* ]]; then
        print_error "agent-bus seat check accepted '$list' but did not print one canonical list; update agent-bus (play-agent-bus.yml)."
        return 78
    fi
    for arg in "$@"; do
        [ "$arg" = --continue ] || [ "$arg" = -c ] || continue
        print_error "ccy refuses --continue in a team seat: it resumes this checkout's newest conversation, which may be another seat's. Resume the seat's own conversation instead:
    ccy --resume <conversation id> --teams $canonical
  The id is the name (without .jsonl) of the seat's last transcript under .claude/ccy/projects/-workspace/ in this checkout, or ask the team's coordinator."
        return 64
    done
    printf '%s\n' "$canonical"
}

# ccy_seats_take <canonical-list> <unattended> <no-bus> <container> — fill CCY_SEAT_RUN_ARGS
# with what the container gets about the bus (ccy_membership_args, lib/agent-bus-membership.bash:
# the ccy-session label, the read-only membership mount and PINGBUS_MEMBERSHIP), then claim
# the launch's seats on the host, creating any that do not exist. An empty list (a plain
# launch) takes nothing and calls no agent-bus. Unattended (true: headless, a restore or a
# restart), sudo never prompts. take's own report goes to stderr, so a headless session's stdout
# stays its own; so does the launch block `ccy-teams describe-seats --block` prints after it
# (every team the session is on, with its seat, role and what the team is for; a failure of it
# stops the launch).
#   status 0   the seats are ready
#   otherwise  ccy_membership_args's code, agent-bus seat take's (75 a held seat, 78 a seat
#              that cannot be made, or agent-bus missing for an agent-bus list), or ccy-teams describe-seats
#              --block's own code (the seats are then already taken); the reason on stderr
ccy_seats_take() {
    local canonical="${1?ccy_seats_take requires the canonical list, or an empty one}"
    local unattended="${2:?ccy_seats_take requires true or false}" rc
    ccy_membership_args "$canonical" "${3:?ccy_seats_take requires --no-bus true or false}" \
        "${4:?ccy_seats_take requires the container name}" || return
    [ -n "$canonical" ] || return 0
    local take=(ccy-teams internal seat-take "$canonical")
    [ "$unattended" != true ] || take+=(--no-prompt)
    "${take[@]}" >&2 || { rc=$?; print_error "Not starting: agent-bus seat take could not seat this session in $canonical (exit $rc; the reason is above).
  End the session that holds a seat, choose another seat, or launch without --teams."; return "$rc"; }
    ccy-teams describe-seats --block "$canonical" >&2 || { rc=$?; print_error "Not starting: ccy-teams describe-seats --block failed (exit $rc; the reason is above)."; return "$rc"; }
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
