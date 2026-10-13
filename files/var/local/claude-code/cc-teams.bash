#!/bin/bash
# cc's agent team bus: `cc --teams <team>` (Plan 00177 Task 4.10).
#
# The desktop Claude Code session joins the team as a `host` member, `<repo>.<N>+<host>.host`,
# in a seat of the checkout it runs in, as ccy's sessions do: agent-bus picks the next free
# seat (`seat choose --type=host`) and makes it the first time (`seat take --type=host`, one
# sudo agent-bus add-member). It runs as the owner, with no dedicated agent user, by the
# owner's decision, so it sits beside their Element profile. The owner accepts that once per
# user at a confirm the session never sees; cc records it under the user's config
# (~/.config/pingbus/element-accepted, owner-only), the only thing that lets pingbus run a
# `host` member beside Element.
#
# The guard is cc's alone (project settings are shared with ccy sessions) and it is
# instruction, not containment: the session's own text says only how to post (pingbus, as
# itself), and cc's settings file denies Claude's file tools Element's folders. Nothing the
# session reads mentions Element or posting as anyone else.
#
# Sourced by cc after common-pure.bash (print_error). Status 64 is a usage mistake, 78 a
# state that stops the launch; stdout carries nothing, every message goes to stderr.

CC_TEAMS_KIT="/usr/local/share/agent-bus/kit"
CC_TEAMS_DENY_SETTINGS="/var/local/claude-code/cc-teams-settings.json"
CC_TEAMS_AGENT_BUS="agent-bus"
CC_TEAMS_MEMBER_TYPE="host"
CC_TEAMS_SEATS_DIR=".claude/ccy/pingbus/seats"
CC_TEAMS_ACCEPTED_DIR="$HOME/.config/pingbus"
CC_TEAMS_ACCEPTED_FILE="element-accepted"
CC_TEAMS_CCY_DIR=".claude/ccy"
# Where the confirm is asked: the terminal itself, so nothing the session can capture sees it.
CC_TEAMS_TTY="/dev/tty"

# cc_teams_split_args [args...] — CC_TEAM, the team `--teams` names ("" without one), and
# CC_TEAMS_REST, every other argument, for claude. One team, given once, with no seat.
cc_teams_split_args() {
    CC_TEAM=""
    CC_TEAMS_REST=()
    local seen=false
    while [ "$#" -gt 0 ]; do
        local value
        case "$1" in
            --teams | --teams=*)
                if [ "$seen" = true ]; then
                    print_error "--teams is given once: cc --teams <team>"
                    return 64
                fi
                seen=true
                if [ "$1" = "--teams" ]; then
                    if [ "$#" -lt 2 ]; then
                        print_error "--teams needs a team: cc --teams <team>"
                        return 64
                    fi
                    value=$2
                    shift
                else
                    value=${1#--teams=}
                fi
                if [ -z "$value" ] || [[ "$value" == *[@,]* ]]; then
                    print_error "cc --teams takes one team, and picks the seat itself: cc --teams <team>"
                    return 64
                fi
                CC_TEAM=$value
                ;;
            *) CC_TEAMS_REST+=("$1") ;;
        esac
        shift
    done
}

# cc_teams_seat <team> — CC_SEAT and CC_HANDLE: the seat agent-bus offers by default for a
# host member of this checkout in <team> (the lowest free one, else a new number).
cc_teams_seat() {
    local team="${1:?cc_teams_seat requires a team}" choices rc=0
    if ! command -v "$CC_TEAMS_AGENT_BUS" >/dev/null; then
        print_error "--teams needs $CC_TEAMS_AGENT_BUS on this host, and it is not installed."
        echo "  Install it with play-agent-bus.yml, or start cc without --teams." >&2
        return 78
    fi
    choices=$("$CC_TEAMS_AGENT_BUS" seat choose "$team" "--type=$CC_TEAMS_MEMBER_TYPE") || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "agent-bus could not offer a seat in team $team (exit $rc; the reason is above)."
        return "$rc"
    fi
    CC_SEAT=$(awk -F'\t' '$1 == "DEFAULT" { print $2 }' <<<"$choices")
    if [ -z "$CC_SEAT" ] || [[ "$CC_SEAT" == *$'\n'* ]]; then
        print_error "agent-bus seat choose named no single default seat; update agent-bus (play-agent-bus.yml)."
        return 78
    fi
    CC_HANDLE=$(awk -F'\t' -v seat="$CC_SEAT" '$1 == "CHOICE" && $2 == seat { print $4 }' <<<"$choices")
    if [ -z "$CC_HANDLE" ] || [[ "$CC_HANDLE" == *$'\n'* ]]; then
        print_error "agent-bus seat choose gave no handle for seat $CC_SEAT; update agent-bus (play-agent-bus.yml)."
        return 78
    fi
}

# cc_teams_take <seat> <team> — make the seat when it is new (sudo asks for a password) and
# refuse one another session holds. agent-bus's report goes to stderr.
cc_teams_take() {
    local seat="${1:?cc_teams_take requires a seat}" team="${2:?cc_teams_take requires a team}" rc=0
    "$CC_TEAMS_AGENT_BUS" seat take "$seat@$team" "--type=$CC_TEAMS_MEMBER_TYPE" >&2 || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "Not starting: agent-bus could not seat this session in $seat@$team (exit $rc; the reason is above)."
        return "$rc"
    fi
}

# cc_teams_ccy_dir — the working directory's checkout has the `.claude/ccy/` that `agent-bus
# seat take` needs, as ccy makes it, so cc --teams works in a repo never used with ccy. A new
# one (0700) gets a .gitignore for the seats' tree alone, which is all the seats need; ccy
# replaces it with its own full one the next time it runs there (it has no `*` rule). An
# existing one is left as it is.
cc_teams_ccy_dir() {
    local top dir
    top=$(git rev-parse --show-toplevel) || return 78
    dir="$top/$CC_TEAMS_CCY_DIR"
    if [ -L "$dir" ] || { [ -e "$dir" ] && [ ! -d "$dir" ]; }; then
        print_error "$dir is not a real directory; remove it and start cc again."
        return 78
    fi
    [ -d "$dir" ] && return 0
    if ! (umask 077 && mkdir -p "$dir" && printf '%s\n' \
        '# Created by cc --teams: the team seats hold tokens and are never committed.' \
        'pingbus/' >"$dir/.gitignore"); then
        print_error "Could not make $dir."
        return 78
    fi
}

# cc_teams_accept <handle> — the owner's acceptance, asked once per user. The words are for
# the human at the terminal; the session starts only after them, and never sees them.
#   0  accepted, now or before   1  declined   78  the record cannot be written safely
cc_teams_accept() {
    local handle="${1:?cc_teams_accept requires the handle}"
    local dir="$CC_TEAMS_ACCEPTED_DIR" record="$CC_TEAMS_ACCEPTED_DIR/$CC_TEAMS_ACCEPTED_FILE"
    if [ -L "$dir" ] || [ -L "$record" ] || { [ -e "$record" ] && [ ! -f "$record" ]; }; then
        print_error "$record, or its folder, is not a plain file or directory; remove it and start cc again."
        return 78
    fi
    [ -f "$record" ] && return 0
    if ! command -v gum >/dev/null; then
        print_error "gum is needed to ask you this, and it is not installed: run play-claude-yolo.yml."
        return 78
    fi
    {
        echo
        echo "  $handle joins the team as its own member, and runs as you."
        echo
        echo "  That means it can reach what you can, including the Element"
        echo "  session you are signed in to. A misbehaving agent could use it."
        echo
        echo "  It is told to post only through pingbus, as $handle, and its"
        echo "  file tools are kept out of Element's folders. That is guidance,"
        echo "  not a wall. You are asked once, for every cc --teams seat."
        echo
    } >&2
    if ! gum confirm --default=false "Accept, and start $handle?" 0<>"$CC_TEAMS_TTY" >&0; then
        echo "Not started. Nothing was recorded; cc --teams asks again next time." >&2
        return 1
    fi
    if ! (umask 077 && mkdir -p "$dir" && chmod 700 "$dir" && set -C &&
        printf 'accepted %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$record"); then
        print_error "Could not record your answer in $record."
        return 78
    fi
}

# cc_teams_settings <out> — the session's settings: the kit's (the bus may wake an idle
# session) with cc's deny rules on Element's folders, written owner-only.
cc_teams_settings() {
    local out="${1:?cc_teams_settings requires an output path}" merged
    if ! merged=$(jq -s '.[0] * .[1]' "$CC_TEAMS_KIT/settings.json" "$CC_TEAMS_DENY_SETTINGS"); then
        print_error "Could not merge $CC_TEAMS_KIT/settings.json and $CC_TEAMS_DENY_SETTINGS."
        return 78
    fi
    (umask 077 && printf '%s\n' "$merged" >"$out")
}

# cc_teams_prompt <handle> <team> — the one line cc adds to the session's system prompt:
# who it is on the bus and the one way it posts. Nothing else.
cc_teams_prompt() {
    local handle="${1:?cc_teams_prompt requires a handle}" team="${2:?cc_teams_prompt requires a team}"
    printf 'You are %s, a member of the agent team %s. Post to the team only with pingbus, as yourself; the pingbus skill says how.\n' \
        "$handle" "$team"
}

# cc_teams_command <tmp> <checkout> <seat@team> <handle> <team> [claude args...] —
# CC_TEAMS_CMD, the session's command line, with the seat as PINGBUS_SEATS. pingbus
# claims the checkout's seat and builds the session home in <tmp> (private to this cc),
# then becomes claude with the plugin, the settings and the prompt line.
cc_teams_command() {
    local tmp="${1:?}" checkout="${2:?}" ref="${3:?}" handle="${4:?}" team="${5:?}"
    shift 5
    CC_TEAMS_CMD=()
    CC_TEAMS_CMD+=(
        env "PINGBUS_SEATS=$ref" "$CC_TEAMS_KIT/pingbus" seat exec
        --root "$checkout/$CC_TEAMS_SEATS_DIR" --home "$tmp/pingbus-home" --
        claude --plugin-dir "$CC_TEAMS_KIT/plugin/pingbus" --settings "$tmp/settings.json"
        --append-system-prompt "$(cc_teams_prompt "$handle" "$team")" "$@"
    )
}

# cc_teams_prepare <tmp> [claude args...] — everything above, in order, for CC_TEAM in the
# working directory's checkout; fills CC_TEAMS_CMD.
cc_teams_prepare() {
    local tmp="${1:?cc_teams_prepare requires a private directory}" checkout need
    shift
    for need in "$CC_TEAMS_KIT/pingbus" "$CC_TEAMS_KIT/settings.json" \
        "$CC_TEAMS_KIT/plugin/pingbus/.claude-plugin/plugin.json" "$CC_TEAMS_DENY_SETTINGS"; do
        if [ ! -f "$need" ]; then
            print_error "--teams needs $need, and it is missing: run play-agent-bus.yml and play-claude-yolo.yml."
            return 78
        fi
    done
    if ! checkout=$(git rev-parse --show-toplevel); then
        print_error "cc --teams runs in a git checkout: its seat lives there."
        return 78
    fi
    cc_teams_ccy_dir || return
    cc_teams_seat "$CC_TEAM" || return
    echo "Agent team $CC_TEAM: seat $CC_SEAT, as $CC_HANDLE" >&2
    # The owner's answer comes first: a declined start leaves no member behind.
    cc_teams_accept "$CC_HANDLE" || return
    cc_teams_take "$CC_SEAT" "$CC_TEAM" || return
    cc_teams_settings "$tmp/settings.json" || return
    cc_teams_command "$tmp" "$checkout" "$CC_SEAT@$CC_TEAM" "$CC_HANDLE" "$CC_TEAM" "$@"
}
