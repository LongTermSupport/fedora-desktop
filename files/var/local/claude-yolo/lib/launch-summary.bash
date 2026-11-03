# shellcheck shell=bash
# Quick Launch's summary: one screen of every saved default (token, SSH key, network, and each
# team with its seat and role) drawn by `ccy-teams launch-summary` (Go). Enter launches with all of
# them; a line picked changes only that one. Sourced by claude-yolo.

# ccy_quick_set NAME VALUE — a launcher variable set from here (the launcher reads it).
ccy_quick_set() { printf -v "$1" %s "$2"; }

# ccy_quick_mode — what a Quick Launch may ask. A restore takes the saved configuration without
# asking: it is the one the person last chose in this project. So does a headless launch, whose
# stdin is the session's own input. Unasked (end of input included), the saved team is not taken:
# only --teams or a person's yes joins one (Plan 00161 M2.9). --no-bus/--opencode: on no team, the
# saved one kept. Sets quick_unasked, quick_team, quick_kept_team, quick_roles and quick_summary
# (a person at a terminal with gum and ccy-teams is shown the summary instead of the Y/n prompt).
#
# ccy_quick_mode headless — the headless launch's saved choices, once taken: an empty saved choice
# is that choice (no key; no project network, so the default one, as the launch that saved it had),
# never the picker or the network detection it would otherwise fall through to; a saved launch with
# no token cannot be replayed. Not --no-network: a session on pasta can join no network later.
ccy_quick_mode() {
    if [ "${1:-}" = headless ]; then
        [ "$HEADLESS_MODE" = true ] || return 0
        if [[ -z "$LAST_TOKEN" ]]; then
            print_error "Headless launch: the saved Quick Launch choices name no token, and a headless launch never asks for one."
            echo "  Launch with --token NAME (and --ssh-key, --ssh-agent or --no-ssh)." >&2
            exit 1
        fi
        [[ -n "$LAST_SSH_KEYS" ]] || ccy_quick_set NO_SSH_MODE true
        [[ -n "$LAST_NETWORK" ]] || ccy_quick_set CCY_NO_PROJECT_NETWORK true
        return 0
    fi
    quick_unasked="" quick_team="$LAST_TEAMS" quick_roles="${LAST_TEAM_ROLES:-}"
    if [ "$SESSION_RESTORE" = true ]; then quick_unasked="Session restore" quick_team=""
    elif [ "$HEADLESS_MODE" = true ]; then quick_unasked="Headless launch" quick_team=""
    elif ccy_nobody_to_ask; then quick_unasked="No terminal to ask on" quick_team=""
    elif [ "$NO_BUS_MODE" = true ]; then quick_team="" && ccy_quick_set quick_kept_team "$LAST_TEAMS"
    fi
    if [ -z "$quick_unasked" ] && [ -t 2 ] && command -v gum >/dev/null && command -v ccy-teams >/dev/null; then
        ccy_quick_set quick_summary true
    fi
}

# ccy_summary_ask TOKEN|SSH — the one question of that line, asked here so the summary comes
# back; a token not chosen leaves the saved one.
ccy_summary_ask() {
    if [ "$1" = ssh ]; then
        discover_and_select_ssh_keys "ccy" || exit 1
        LAST_SSH_KEYS="${SSH_KEYS[*]}"
    elif select_token "$TOKEN_DIR" "container"; then
        LAST_TOKEN="${SELECTED_TOKEN##*/}"
        LAST_TOKEN="${LAST_TOKEN%.token}" LAST_TOKEN="${LAST_TOKEN%.*}"
    else
        echo "No token chosen: the saved choice stays." >&2
    fi
    SELECTED_TOKEN="" SSH_KEYS=()
}

# ccy_launch_summary — shows the summary until the person accepts it, setting LAST_TOKEN,
# LAST_SSH_KEYS, LAST_NETWORK and, unless --teams or --no-bus decides the teams, quick_team and
# quick_roles. A network picked to change is cleared: its question is asked at its own step of the
# launch. Leaving the summary ends the launch (the reason is already on stderr).
ccy_launch_summary() {
    local out key value ask teams="" roles="" skip=()
    [ "$TEAMS_GIVEN" != true ] && [ "$NO_BUS_MODE" != true ] || skip=(--skip-teams)
    while true; do
        out=$(ccy-teams launch-summary --type=podman "--token=$LAST_TOKEN" "--ssh=$LAST_SSH_KEYS" \
            "--network=$LAST_NETWORK" "--teams=$quick_team" "--roles=$quick_roles" "${skip[@]}") || exit $?
        [ -n "$out" ] || exit 0
        while IFS=$'\t' read -r key value; do
            case "$key" in TEAMS) teams="$value" ;; ROLES) roles="$value" ;; ASK) ask="$value" ;; *) ;; esac
        done <<<"$out"
        if [ ${#skip[@]} -eq 0 ]; then quick_team="$teams" quick_roles="$roles"; fi
        case "$ask" in
            token) ccy_summary_ask token ;;
            ssh) ccy_summary_ask ssh ;;
            network) LAST_NETWORK="" && return 0 ;;
            *) return 0 ;;
        esac
    done
}
