#!/bin/bash
# cc's agent team bus: `cc --teams [<team>|<seat>@<team>[,...]]` and `cc --bus` (Plan 00177 Task
# 4.10, Plan 00201 M6, Plan 00203 M6).
#
# The desktop Claude Code session joins its teams as `host` members, `<repo>.<N>+<host>.host`,
# one seat of the checkout it runs in per team, as ccy's sessions do. The list grammar and the
# questions are ccy's: `ccy-teams choose-teams --type=host` shows the teams with what they are
# for, offers a team that does not cover the checkout its scope, chooses the seats, and asks the
# owner's once-per-user consent (below), then `agent-bus seat check` validates the list it
# prints and `agent-bus seat take --type=host` makes the seats (one sudo agent-bus add-member
# each). With no terminal a team alone or a bare `--teams` is refused, and so is a full list
# until the consent is recorded, before any seat is made. cc runs as the owner, with no
# dedicated agent user, by the owner's decision, so it sits beside their Element profile. The
# consent lives in ccy-teams (Go) and is recorded under the user's config
# (~/.config/pingbus/element-accepted, owner-only), the only thing that lets pingbus run a
# `host` member beside Element.
#
# The teams can be changed while the session runs (`ccy-teams session add|remove|move|role`,
# Plan 00203 H31): `ccy-teams internal membership-init --kind=cc` writes generation 1 of its
# desired membership into $XDG_RUNTIME_DIR/pingbus/membership/<session>/ and the seat keeper of
# the session follows it. `cc --bus` starts on no team, after the same consent, for the owner to
# add to teams later. The session runs as the owner, so it could write that file itself: the
# weaker authority of cc, accepted (Plan 00203 Decision 3), the same stance as towards Element.
# Its text names no team, since its teams may change (H32). A cc run outside the tmux of ccy has
# no session name; its membership is named cc.<pid> and nothing can change it while it runs.
#
# The guard is cc's alone (project settings are shared with ccy sessions) and it is
# instruction, not containment: the session's own text says only how to post (pingbus, as
# itself), and cc's settings file denies Claude's file tools Element's folders and the keyring,
# and Bash the plain routes to them (rules match command text, so a determined agent gets past
# them; the consent says ccy is the safe choice). Nothing the session reads mentions Element or
# posting as anyone else. `agent-bus seat take` drops sudo's cached credential before the
# session starts on this terminal.
#
# Sourced by cc after common-pure.bash (print_error) and tmux-session.bash
# (ccy_tmux_current_session). Status 64 is a usage mistake, 78 a state that stops the launch;
# stdout carries nothing, every message goes to stderr.

CC_TEAMS_KIT="/usr/local/share/agent-bus/kit"
CC_TEAMS_DENY_SETTINGS="/var/local/claude-code/cc-teams-settings.json"
CC_TEAMS_AGENT_BUS="agent-bus"
CC_TEAMS_CCY_TEAMS="ccy-teams"
CC_TEAMS_MEMBER_TYPE="host"
CC_TEAMS_SEATS_DIR=".claude/ccy/pingbus/seats"
CC_TEAMS_CCY_DIR=".claude/ccy"
# The line cc adds to the system prompt of the session: where its teams are said, and the one
# way it posts. Nothing else.
CC_TEAMS_PROMPT="Your agent-bus teams and seats are given in the session context and may change while you run; the latest agent-bus membership notice is current. Post to your teams only with pingbus, as yourself; the pingbus skill says how."

# cc_teams_split_args [args...] — CC_TEAMS_GIVEN (true when `--teams` or `--bus` was: the bus is
# wanted), CC_TEAMS_BUS (true for `--bus` with no `--teams`: a session on no team),
# CC_TEAMS_LIST (the `--teams` value, "" for a bare one) and CC_TEAMS_REST, every other
# argument, for claude. An option where the value belongs means it was left out, as in ccy.
# Both flags together are refused, here or (--teams first) by the chooser; a record never holds
# both, since ccy-teams drops --bus from it when it gives the session a team.
cc_teams_split_args() {
    CC_TEAMS_GIVEN=false CC_TEAMS_BUS=false CC_TEAMS_LIST="" CC_TEAMS_SEATS=""
    CC_TEAMS_REST=()
    local expect=false arg
    for arg in "$@"; do
        if [ "$expect" = true ]; then
            expect=false
            [[ "$arg" == -* ]] || { CC_TEAMS_LIST=$arg; continue; }
        fi
        case "$arg" in
            --bus) CC_TEAMS_GIVEN=true CC_TEAMS_BUS=true ;;
            --teams | --teams=*)
                if [ "$CC_TEAMS_GIVEN" = true ]; then
                    print_error "--teams is given once, with every seat in it, and not with --bus: cc --teams <seat>@<team>,<seat>@<team>"
                    return 64
                fi
                CC_TEAMS_GIVEN=true
                if [ "$arg" = "--teams" ]; then expect=true; else CC_TEAMS_LIST=${arg#--teams=}; fi
                ;;
            *) CC_TEAMS_REST+=("$arg") ;;
        esac
    done
}

# cc_teams_choose — CC_TEAMS_SEATS, the canonical list `agent-bus seat check` prints for the
# list `ccy-teams choose-teams` prints, or none for `--bus`, which asks only the consent. The
# chooser is always asked: it knows whether a person can answer, asks the Element consent of the
# owner, and refuses (78) before any seat is made when there is none to ask and no record.
# Leaving it ends cc quietly.
cc_teams_choose() {
    local list rc=0
    [ "$CC_TEAMS_BUS" = false ] || { "$CC_TEAMS_CCY_TEAMS" choose-teams "--type=$CC_TEAMS_MEMBER_TYPE" --bus "$CC_TEAMS_LIST"; return; }
    list=$("$CC_TEAMS_CCY_TEAMS" choose-teams "--type=$CC_TEAMS_MEMBER_TYPE" "$CC_TEAMS_LIST") || return $?
    [ -n "$list" ] || exit 0
    CC_TEAMS_SEATS=$("$CC_TEAMS_AGENT_BUS" seat check "$list") || { rc=$?; print_error "--teams $list refused by agent-bus seat check (exit $rc; the reason is above)."; return "$rc"; }
}

# cc_teams_take — make CC_TEAMS_SEATS (sudo asks for a password for a new one) and refuse one
# another session holds, then show the launch block. Their output goes to stderr.
cc_teams_take() {
    local rc=0
    "$CC_TEAMS_AGENT_BUS" seat take "$CC_TEAMS_SEATS" "--type=$CC_TEAMS_MEMBER_TYPE" >&2 || { rc=$?; print_error "Not starting: agent-bus could not seat this session in $CC_TEAMS_SEATS (exit $rc; the reason is above)."; return "$rc"; }
    "$CC_TEAMS_CCY_TEAMS" describe-seats --block "$CC_TEAMS_SEATS" >&2 || { rc=$?; print_error "Not starting: ccy-teams describe-seats --block failed (exit $rc; the reason is above)."; return "$rc"; }
}

# cc_teams_ccy_dir — the working directory's checkout has the `.claude/ccy/` that `agent-bus
# seat take` needs, as ccy makes it, so cc --teams works in a repo never used with ccy. A new
# one (0700) gets a .gitignore for the seats' tree alone, which is all the seats need; ccy
# replaces it with its own full one the next time it runs there (it has no `*` rule). An
# existing one is left as it is.
cc_teams_ccy_dir() {
    local dir="${1:?cc_teams_ccy_dir requires the checkout}/$CC_TEAMS_CCY_DIR"
    if [ -L "$dir" ] || { [ -e "$dir" ] && [ ! -d "$dir" ]; }; then
        print_error "$dir is not a real directory; remove it and start cc again."
        return 78
    fi
    [ -d "$dir" ] && return 0
    (umask 077 && mkdir -p "$dir" && printf '%s\n' \
        '# Created by cc --teams: the team seats hold tokens and are never committed.' \
        'pingbus/' >"$dir/.gitignore") || { print_error "Could not make $dir."; return 78; }
}

# cc_teams_settings <out> — the session's settings: the kit's (the bus may wake an idle
# session, from the start, so a first join is never a relaunch) with cc's deny rules on
# Element's sign-in, written owner-only.
cc_teams_settings() {
    local out="${1:?cc_teams_settings requires an output path}" merged
    merged=$(jq -s '.[0] * .[1]' "$CC_TEAMS_KIT/settings.json" "$CC_TEAMS_DENY_SETTINGS") || { print_error "Could not merge $CC_TEAMS_KIT/settings.json and $CC_TEAMS_DENY_SETTINGS."; return 78; }
    (umask 077 && printf '%s\n' "$merged" >"$out")
}

# cc_teams_command <tmp> <checkout> [claude args...] — CC_TEAMS_CMD, the command line of the
# session, once generation 1 of its desired membership (CC_TEAMS_SEATS, none for --bus) is
# written under its session name. The seat keeper of pingbus takes the seats it names and
# follows it, the session home is built in <tmp> (private to this cc), and pingbus becomes
# claude with the plugin, the settings (which accept team messages, as the keeper is told) and
# the prompt line.
cc_teams_command() {
    local tmp="${1:?}" checkout="${2:?}" session dir
    shift 2
    session=$(ccy_tmux_current_session) || return 78
    # membership-init says why on stderr when it refuses; its exit is the launch failure.
    dir=$("$CC_TEAMS_CCY_TEAMS" internal membership-init --kind=cc "${session:-cc.$$}" "$checkout" "$CC_TEAMS_SEATS") || return
    CC_TEAMS_CMD=()
    CC_TEAMS_CMD+=(
        env -u PINGBUS_SEATS "PINGBUS_MEMBERSHIP=$dir" "$CC_TEAMS_KIT/pingbus" seat exec --accepts-inbound
        --root "$checkout/$CC_TEAMS_SEATS_DIR" --home "$tmp/pingbus-home" --
        claude --plugin-dir "$CC_TEAMS_KIT/plugin/pingbus" --settings "$tmp/settings.json"
        --append-system-prompt "$CC_TEAMS_PROMPT" "$@"
    )
}

# cc_teams_prepare <tmp> [claude args...] — everything above, in order, in the working
# directory's checkout; fills CC_TEAMS_CMD.
cc_teams_prepare() {
    local tmp="${1:?cc_teams_prepare requires a private directory}" checkout need
    shift
    for need in "$CC_TEAMS_KIT/pingbus" "$CC_TEAMS_KIT/settings.json" \
        "$CC_TEAMS_KIT/plugin/pingbus/.claude-plugin/plugin.json" "$CC_TEAMS_DENY_SETTINGS"; do
        [ -f "$need" ] || { print_error "--teams and --bus need $need, and it is missing: run play-agent-bus.yml and play-claude-yolo.yml."; return 78; }
    done
    type -P "$CC_TEAMS_AGENT_BUS" "$CC_TEAMS_CCY_TEAMS" >/dev/null || { print_error "--teams and --bus need $CC_TEAMS_AGENT_BUS and $CC_TEAMS_CCY_TEAMS on this host, and one is not installed: run play-agent-bus.yml and play-claude-yolo.yml."; return 78; }
    checkout=$(git rev-parse --show-toplevel) || { print_error "cc --teams and cc --bus run in a git checkout: the seats live there."; return 78; }
    # The owner's consent is asked inside the chooser, so a declined start makes nothing: no
    # member, and not even the checkout's .claude/ccy/ (S-E4).
    cc_teams_choose || return
    cc_teams_ccy_dir "$checkout" || return
    [ "$CC_TEAMS_BUS" = true ] || cc_teams_take || return
    cc_teams_settings "$tmp/settings.json" || return
    cc_teams_command "$tmp" "$checkout" "$@"
}
