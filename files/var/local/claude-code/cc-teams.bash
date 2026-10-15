#!/bin/bash
# cc's agent team bus: `cc --teams [<team>|<seat>@<team>[,...]]` (Plan 00177 Task 4.10,
# Plan 00201 M6).
#
# The desktop Claude Code session joins its teams as `host` members, `<repo>.<N>+<host>.host`,
# one seat of the checkout it runs in per team, as ccy's sessions do. The list grammar and the
# questions are ccy's: on a terminal `ccy-teams choose-teams --type=host` shows the teams with
# what they are for, offers a team that does not cover the checkout its scope, chooses the
# seats, and asks the owner's once-per-user consent (below), then `agent-bus seat check`
# validates the list it prints and `agent-bus seat take --type=host` makes the seats (one sudo
# agent-bus add-member each). With no terminal nobody can be asked: a full list is checked
# `--resolved` and a team alone or a bare `--teams` is refused. cc runs as the owner, with no
# dedicated agent user, by the owner's decision, so it sits beside their Element profile. The
# consent lives in ccy-teams (Go) and is recorded under the user's config
# (~/.config/pingbus/element-accepted, owner-only), the only thing that lets pingbus run a
# `host` member beside Element; the no-terminal path cannot ask it, so pingbus refuses the
# session there until an attended launch has recorded it.
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
CC_TEAMS_CCY_TEAMS="ccy-teams"
CC_TEAMS_MEMBER_TYPE="host"
CC_TEAMS_SEATS_DIR=".claude/ccy/pingbus/seats"
CC_TEAMS_CCY_DIR=".claude/ccy"

# cc_teams_split_args [args...] — CC_TEAMS_GIVEN (true when `--teams` was), CC_TEAMS_LIST (its
# value, "" for a bare one) and CC_TEAMS_REST, every other argument, for claude. An option
# where the value belongs means it was left out, as in ccy.
cc_teams_split_args() {
    CC_TEAMS_GIVEN=false
    CC_TEAMS_LIST=""
    CC_TEAMS_REST=()
    local expect=false arg
    for arg in "$@"; do
        if [ "$expect" = true ]; then
            expect=false
            [[ "$arg" == -* ]] || { CC_TEAMS_LIST=$arg; continue; }
        fi
        case "$arg" in
            --teams | --teams=*)
                if [ "$CC_TEAMS_GIVEN" = true ]; then
                    print_error "--teams is given once, with every seat in it: cc --teams <seat>@<team>,<seat>@<team>"
                    return 64
                fi
                CC_TEAMS_GIVEN=true
                if [ "$arg" = "--teams" ]; then expect=true; else CC_TEAMS_LIST=${arg#--teams=}; fi
                ;;
            *) CC_TEAMS_REST+=("$arg") ;;
        esac
    done
}

# cc_teams_attended — a person is at a terminal, so the chooser can ask.
cc_teams_attended() { [ -t 0 ] && [ -t 2 ]; }

# cc_teams_choose — CC_TEAMS_SEATS, the canonical list `agent-bus seat check` prints for
# CC_TEAMS_LIST. Attended, the list is the one `ccy-teams choose-teams` prints (which asks the
# owner's Element consent); leaving it ends cc quietly. Unattended it is checked `--resolved`.
cc_teams_choose() {
    local list=$CC_TEAMS_LIST resolved=(--resolved) rc=0
    if cc_teams_attended; then
        list=$("$CC_TEAMS_CCY_TEAMS" choose-teams "--type=$CC_TEAMS_MEMBER_TYPE" "$list") || return $?
        [ -n "$list" ] || exit 0
        resolved=()
    fi
    CC_TEAMS_SEATS=$("$CC_TEAMS_AGENT_BUS" seat check "$list" "${resolved[@]}") || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "--teams '$list' refused by agent-bus seat check (exit $rc; the reason is above)."
        if [ -z "$list" ]; then echo "  Choosing a team needs a terminal: name the seat instead, --teams <seat>@<team>." >&2; fi
        return "$rc"
    fi
}

# cc_teams_take — make CC_TEAMS_SEATS (sudo asks for a password for a new one) and refuse one
# another session holds, then show the launch block and make CC_TEAMS_SENTENCE, the line that
# names the session's handles. Their output goes to stderr.
cc_teams_take() {
    local rc=0
    "$CC_TEAMS_AGENT_BUS" seat take "$CC_TEAMS_SEATS" "--type=$CC_TEAMS_MEMBER_TYPE" >&2 || rc=$?
    if [ "$rc" -ne 0 ]; then
        print_error "Not starting: agent-bus could not seat this session in $CC_TEAMS_SEATS (exit $rc; the reason is above)."
        return "$rc"
    fi
    "$CC_TEAMS_CCY_TEAMS" describe-seats --block "$CC_TEAMS_SEATS" >&2 || { rc=$?; print_error "Not starting: ccy-teams describe-seats --block failed (exit $rc; the reason is above)."; return "$rc"; }
    CC_TEAMS_SENTENCE=$("$CC_TEAMS_CCY_TEAMS" describe-seats --sentence "$CC_TEAMS_SEATS") || { rc=$?; print_error "Not starting: ccy-teams describe-seats --sentence failed (exit $rc; the reason is above)."; return "$rc"; }
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

# cc_teams_prompt <sentence> — the line cc adds to the session's system prompt: who it is on
# each team (the sentence ccy-teams made) and the one way it posts. Nothing else.
cc_teams_prompt() {
    printf '%s Post to your teams only with pingbus, as yourself; the pingbus skill says how.\n' "${1:?cc_teams_prompt requires the sentence}"
}

# cc_teams_command <tmp> <checkout> <seats> <sentence> [claude args...] — CC_TEAMS_CMD, the
# session's command line, with the seats as PINGBUS_SEATS. pingbus claims the checkout's seats
# and builds the session home in <tmp> (private to this cc), then becomes claude with the
# plugin, the settings and the prompt line.
cc_teams_command() {
    local tmp="${1:?}" checkout="${2:?}" seats="${3:?}" sentence="${4:?}"
    shift 4
    CC_TEAMS_CMD=()
    CC_TEAMS_CMD+=(
        env "PINGBUS_SEATS=$seats" "$CC_TEAMS_KIT/pingbus" seat exec
        --root "$checkout/$CC_TEAMS_SEATS_DIR" --home "$tmp/pingbus-home" --
        claude --plugin-dir "$CC_TEAMS_KIT/plugin/pingbus" --settings "$tmp/settings.json"
        --append-system-prompt "$(cc_teams_prompt "$sentence")" "$@"
    )
}

# cc_teams_prepare <tmp> [claude args...] — everything above, in order, in the working
# directory's checkout; fills CC_TEAMS_CMD.
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
    for need in "$CC_TEAMS_AGENT_BUS" "$CC_TEAMS_CCY_TEAMS"; do
        command -v "$need" >/dev/null || { print_error "--teams needs $need on this host, and it is not installed: run play-agent-bus.yml and play-claude-yolo.yml."; return 78; }
    done
    checkout=$(git rev-parse --show-toplevel) || { print_error "cc --teams runs in a git checkout: its seat lives there."; return 78; }
    cc_teams_ccy_dir || return
    # The owner's consent is asked inside the chooser, so a declined start leaves no member.
    cc_teams_choose || return
    cc_teams_take || return
    cc_teams_settings "$tmp/settings.json" || return
    cc_teams_command "$tmp" "$checkout" "$CC_TEAMS_SEATS" "$CC_TEAMS_SENTENCE" "$@"
}
