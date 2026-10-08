#!/bin/bash
# CCY restart-request library: acting on a supervisor's "restart this session" request.
#
# Claude Code is baked into the CCY image, so only a new container picks up a newer version.
# The supervisor inside the container (claude-supervise.py, run by entrypoint.sh as the claude
# wrapper) can decide a session should restart, for example because it has reached its maximum
# age. It then types /exit at an idle point, writes a JSON request file into the project and
# exits with status 75 (EX_TEMPFAIL). The container ends, and the launcher on the host decides
# whether that was a restart request, updates the image and relaunches with --resume.
#
# The contract (owned by the hooks daemon, CcySupervisor.md):
#   file    <project>/.claude/ccy/state/restart-request.json, mode 0600, written atomically
#   schema  {"session_id": "<id>", "reason": "<short text>", "plugin": "<name>",
#            "requested_at": <epoch seconds, float>}
#   rule    act on status 75 only when the file is present and fresh; delete it once read;
#           a genuine child exit of 75 with no file is not a request.
#
# The file is written by code running in the container, so it is untrusted input that ends up
# in the relaunch argv. Everything here refuses rather than guesses.
#
# Needs jq and ssh-keygen (installed by play-claude-yolo.yml and the base system). Needs
# session-registry.bash loaded first: the relaunch argv reuses its flag classification. The
# key functions need SSH_AGENT_SENTINEL, from ssh-handling.bash.
#
# Nobody is watching a relaunch, so it must never stop at a prompt. The launcher marks it
# (CCY_RESTART_RELAUNCH, read by ccy_restart_marker_take) and carries every launch choice in
# its arguments; the prompts that have one safe answer take it, and anything else stops the
# launch with ccy_restart_refuse, which names the manual `ccy --resume <id>`.

# The exit status the supervisor uses for "restart requested".
export CCY_RESTART_EXIT_STATUS=75

# The request file, relative to the project root.
export CCY_RESTART_REQUEST_REL=".claude/ccy/state/restart-request.json"

# A request this much older than now is stale; one this far in the future is a clock lie.
CCY_RESTART_MAX_AGE_SECONDS=300
CCY_RESTART_MAX_FUTURE_SECONDS=60

# A real request is a few hundred bytes. Bounded so a hostile file cannot make jq read a
# gigabyte.
CCY_RESTART_MAX_BYTES=4096

# At most this many restarts inside this many seconds, then the launcher stops and says so.
export CCY_RESTART_DEFAULT_MAX=3
export CCY_RESTART_DEFAULT_WINDOW_SECONDS=3600

# Claude Code session ids are UUIDs.
CCY_RESTART_SESSION_ID_PATTERN='^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$'

# ccy_restart_request_discard <file> — remove a request file, whatever it holds. A symlink is
# removed as the link it is (rm never follows it). A directory in its place is an error: it
# cannot be a request and rm -f would not remove it, so a loop could survive it.
ccy_restart_request_discard() {
    local file="${1:?ccy_restart_request_discard requires a file}"
    if [ -d "$file" ] && [ ! -L "$file" ]; then
        printf 'ERROR: %s is a directory, not a restart request; remove it.\n' "$file" >&2
        return 1
    fi
    if ! rm -f -- "$file"; then
        printf 'ERROR: could not remove the restart request %s.\n' "$file" >&2
        return 1
    fi
}

# ccy_restart_request_read <file> [now] — validate a request and print its session id.
#   status 0  valid; the session id is the only thing on stdout
#   status 1  present but refused; the reason is on stderr
#   status 2  no such file (not a request, and not an error)
# `now` is epoch seconds, injectable for tests. The file is left in place: the caller
# consumes it, so a refusal can still be reported and removed by the same code path.
ccy_restart_request_read() {
    local file="${1:?ccy_restart_request_read requires a file}"
    local now="${2:-}"
    [ -n "$now" ] || now=$(date +%s)

    if [ ! -e "$file" ] && [ ! -L "$file" ]; then
        return 2
    fi
    if [ -L "$file" ]; then
        printf 'restart request %s is a symlink; refusing it.\n' "$file" >&2
        return 1
    fi
    if [ ! -f "$file" ]; then
        printf 'restart request %s is not a regular file; refusing it.\n' "$file" >&2
        return 1
    fi

    local size
    if ! size=$(stat -c %s -- "$file"); then
        printf 'could not read the size of restart request %s; refusing it.\n' "$file" >&2
        return 1
    fi
    if [ "$size" -eq 0 ] || [ "$size" -gt "$CCY_RESTART_MAX_BYTES" ]; then
        printf 'restart request %s is %s bytes (allowed: 1 to %s); refusing it.\n' \
            "$file" "$size" "$CCY_RESTART_MAX_BYTES" >&2
        return 1
    fi

    local fresh_filter="type == \"object\"
        and (.session_id | type == \"string\")
        and (.requested_at | type == \"number\")
        and ((\$now - .requested_at) as \$age
             | \$age <= \$max_age and \$age >= (0 - \$max_future))"
    local verdict
    if ! verdict=$(jq -e --argjson now "$now" \
        --argjson max_age "$CCY_RESTART_MAX_AGE_SECONDS" \
        --argjson max_future "$CCY_RESTART_MAX_FUTURE_SECONDS" \
        "$fresh_filter" -- "$file" 2>&1); then
        if [ "$verdict" = "false" ] || [ "$verdict" = "null" ]; then
            printf 'restart request %s is not a fresh, well-formed request (needs a string session_id and a numeric requested_at within %ss of now); refusing it.\n' \
                "$file" "$CCY_RESTART_MAX_AGE_SECONDS" >&2
        else
            printf 'restart request %s is not valid JSON: %s\n' "$file" "$verdict" >&2
        fi
        return 1
    fi

    # -j and a trailing marker: the value arrives byte for byte, so a newline smuggled into
    # the id is still there for the pattern to refuse (command substitution would eat it).
    local raw sid
    if ! raw=$(jq -j '.session_id, "x"' -- "$file"); then
        printf 'could not read the session id from %s; refusing it.\n' "$file" >&2
        return 1
    fi
    sid="${raw%x}"
    if ! [[ "$sid" =~ $CCY_RESTART_SESSION_ID_PATTERN ]]; then
        printf 'restart request %s carries a session id that is not a Claude session id; refusing it.\n' \
            "$file" >&2
        return 1
    fi

    # The reason is log text written by the container: printable characters only, short.
    local reason
    if reason=$(jq -j 'if (.reason | type) == "string" then .reason else "" end' -- "$file"); then
        reason=$(printf '%s' "$reason" | tr -cd '[:print:]')
        reason="${reason:0:120}"
        [ -z "$reason" ] || printf 'restart requested by the session supervisor: %s\n' "$reason" >&2
    fi

    printf '%s\n' "$sid"
}

# ccy_restart_budget_take <history-file> <now> <max> <window-seconds> — record one restart if
# fewer than <max> were recorded inside the last <window-seconds>; otherwise refuse and record
# nothing. The history is one epoch per line. A history that is not exactly that is an error,
# because treating garbage as "no history" would let a corrupt file switch the bound off.
#   status 0  allowed and recorded
#   status 1  refused, or the history could not be read or written; the reason is on stderr
ccy_restart_budget_take() {
    local file="${1:?ccy_restart_budget_take requires a history file}"
    local now="${2:?ccy_restart_budget_take requires the time}"
    local max="${3:?ccy_restart_budget_take requires a limit}"
    local window="${4:?ccy_restart_budget_take requires a window}"
    local value
    for value in "$now" "$max" "$window"; do
        if ! [[ "$value" =~ ^[0-9]+$ ]]; then
            printf 'restart budget: "%s" is not a whole number.\n' "$value" >&2
            return 1
        fi
    done
    if [ "$window" -eq 0 ]; then
        printf 'restart budget: the window must be at least 1 second.\n' >&2
        return 1
    fi

    local kept=() stamp
    if [ -e "$file" ]; then
        if [ ! -f "$file" ] || [ -L "$file" ]; then
            printf 'restart budget: %s is not a regular file.\n' "$file" >&2
            return 1
        fi
        while IFS= read -r stamp || [ -n "$stamp" ]; do
            if ! [[ "$stamp" =~ ^[0-9]+$ ]]; then
                printf 'restart budget: %s is corrupt (a line is not an epoch); remove it to reset the count.\n' \
                    "$file" >&2
                return 1
            fi
            if [ $((now - stamp)) -lt "$window" ]; then
                kept+=("$stamp")
            fi
        done <"$file"
    fi

    if [ "${#kept[@]}" -ge "$max" ]; then
        printf 'restart budget spent: %s restart(s) in the last %ss, the limit is %s in %ss.\n' \
            "${#kept[@]}" "$window" "$max" "$window" >&2
        return 1
    fi

    local tmp="$file.tmp.$$"
    if ! { printf '%s\n' "${kept[@]}" "$now" | grep -v '^$' >"$tmp"; }; then
        rm -f -- "$tmp"
        printf 'restart budget: could not write %s.\n' "$file" >&2
        return 1
    fi
    if ! mv -f -- "$tmp" "$file"; then
        rm -f -- "$tmp"
        printf 'restart budget: could not move the history into place at %s.\n' "$file" >&2
        return 1
    fi
}

# ccy_restart_relaunch_args <session-id> [original launcher args...] — the arguments for the
# relaunch, NUL separated (an argument may hold a newline). The ccy options the session was
# started with are kept, except the launch choices (--token, --ssh-key, --ssh-agent, --no-ssh,
# --network, --no-network, --github-443): those are dropped, and the caller puts the choices
# the session actually ran with in front, from ccy_restart_choice_args. A choice made at a
# prompt is in no argument, so the original arguments alone would send the relaunch back to
# that prompt with nobody there to answer it. Also dropped is what only made sense once (an
# opening message, --prompt, --rebuild, the debug chooser). Claude's own session selectors
# (-c, --continue, -r, --resume, --session-id) are removed and replaced by one
# `--resume <session-id>`.
#
# The walk mirrors ccy_registry_replay_args and uses its flag classification.
#   status 0  arguments printed
#   status 1  the session id is not a Claude session id; nothing printed
ccy_restart_relaunch_args() {
    local sid="${1:?ccy_restart_relaunch_args requires a session id}"
    shift
    if ! [[ "$sid" =~ $CCY_RESTART_SESSION_ID_PATTERN ]]; then
        printf 'refusing to build a relaunch for a session id that is not a Claude session id.\n' >&2
        return 1
    fi
    if ! declare -F ccy_registry_flag_class >/dev/null; then
        printf 'ccy_restart_relaunch_args needs lib/session-registry.bash loaded first.\n' >&2
        return 1
    fi

    local arg after_dd=false drop_next=false keep_next=false value_slot=false skip_resume_value=false
    for arg in "$@"; do
        if [[ "$skip_resume_value" == true ]]; then
            skip_resume_value=false
            if [[ "$arg" != -* ]]; then
                continue
            fi
        fi
        if [[ "$drop_next" == true ]]; then
            drop_next=false
            continue
        fi
        if [[ "$keep_next" == true ]]; then
            keep_next=false
            printf '%s\0' "$arg"
            continue
        fi
        if [[ "$after_dd" == false ]]; then
            if [[ "$arg" == "--" ]]; then
                after_dd=true
                printf '%s\0' "$arg"
                continue
            fi
            case "$arg" in
            --token | --ssh-key | --network)
                drop_next=true
                value_slot=false
                continue
                ;;
            --ssh-agent | --no-ssh | --no-network | --github-443)
                value_slot=false
                continue
                ;;
            esac
            case "$(ccy_registry_flag_class "$arg")" in
            drop)
                value_slot=false
                continue
                ;;
            drop-value)
                drop_next=true
                value_slot=false
                continue
                ;;
            keep-value)
                keep_next=true
                value_slot=false
                printf '%s\0' "$arg"
                continue
                ;;
            keep)
                value_slot=false
                printf '%s\0' "$arg"
                continue
                ;;
            esac
        fi
        # Claude's side: before `--` an option ccy does not know, and everything after it.
        case "$arg" in
        -c | --continue | --resume=* | --session-id=*)
            value_slot=false
            continue
            ;;
        -r | --resume)
            skip_resume_value=true
            value_slot=false
            continue
            ;;
        --session-id)
            drop_next=true
            value_slot=false
            continue
            ;;
        esac
        if [[ "$after_dd" == true ]]; then
            printf '%s\0' "$arg"
        elif [[ "$arg" == -* ]]; then
            value_slot=true
            printf '%s\0' "$arg"
        else
            # A bare word is the value of the unknown option before it (`--model opus`);
            # on its own it is an opening message, which is stale on a resume.
            if [[ "$value_slot" == true ]]; then
                printf '%s\0' "$arg"
            fi
            value_slot=false
        fi
    done
    printf '%s\0' "--resume" "$sid"
}

# ccy_restart_choice_args <token-file> <github-443> <no-network> <network> [ssh-keys...] — the
# launch choices the session ran with, as launcher options, NUL separated. They go in front
# of ccy_restart_relaunch_args, so they come before any `--`.
#   token-file  the token file the session used: its path, so the relaunch opens that exact
#               file. The token itself never enters argv.
#   github-443  0 or 1, the session's GITHUB_SSH_443
#   no-network  true or false, whether --no-network was in force
#   network     the project network it joined, or empty for none
#   ssh-keys    the session's SSH_KEYS; none means --no-ssh, SSH_AGENT_SENTINEL means --ssh-agent
#   status 0  options printed
#   status 1  an input is not one of the above; nothing printed, the reason on stderr
ccy_restart_choice_args() {
    local token="${1?ccy_restart_choice_args requires the token file}"
    local gh443="${2?ccy_restart_choice_args requires the 443 state}"
    local no_network="${3?ccy_restart_choice_args requires the no-network state}"
    local network="${4?ccy_restart_choice_args requires the network}"
    shift 4
    local sentinel="${SSH_AGENT_SENTINEL:?ccy_restart_choice_args needs lib/ssh-handling.bash loaded first}"
    if [ -z "$token" ]; then
        printf 'the session has no token file on record, so the relaunch could not name one.\n' >&2
        return 1
    fi
    if [ "$gh443" != 0 ] && [ "$gh443" != 1 ]; then
        printf 'GITHUB_SSH_443 is "%s", not 0 or 1.\n' "$gh443" >&2
        return 1
    fi
    if [ "$no_network" != true ] && [ "$no_network" != false ]; then
        printf 'the no-network state is "%s", not true or false.\n' "$no_network" >&2
        return 1
    fi
    local key
    for key in "$@"; do
        if [ -z "$key" ]; then
            printf 'the session has an empty SSH key entry, so the relaunch could not name it.\n' >&2
            return 1
        fi
    done

    local out=(--token "$token")
    if [ "$#" -eq 0 ]; then
        out+=(--no-ssh)
    fi
    for key in "$@"; do
        if [ "$key" = "$sentinel" ]; then
            out+=(--ssh-agent)
        else
            out+=(--ssh-key "$key")
        fi
    done
    if [ "$gh443" = 1 ]; then
        out+=(--github-443)
    fi
    if [ "$no_network" = true ]; then
        out+=(--no-network)
    elif [ -n "$network" ]; then
        out+=(--network "$network")
    fi
    printf '%s\0' "${out[@]}"
}

# ccy_restart_keys_unattended <passphrase-file> [ssh-keys...] — whether every SSH key can be
# unlocked with nobody at the keyboard. A forwarded agent is unlocked already. A key file opens
# unattended when it has no passphrase, when the session's ssh-agent holds it (ccy then
# forwards that one key from the agent, ccy_agent_forwards_key in ssh-handling.bash), or when
# a passphrase file is named (a server's session restore; ssh-handling.bash feeds it to
# ssh-add through askpass). Anything else would stop the relaunch at ssh-add's prompt, on the
# host or inside the container.
#   status 0  every key opens unattended
#   status 1  a key does not; which one and why on stderr
ccy_restart_keys_unattended() {
    local passphrase_file="${1?ccy_restart_keys_unattended requires the passphrase file, or an empty one}"
    shift
    local sentinel="${SSH_AGENT_SENTINEL:?ccy_restart_keys_unattended needs lib/ssh-handling.bash loaded first}"
    [ -z "$passphrase_file" ] || return 0
    local key why
    for key in "$@"; do
        [ "$key" = "$sentinel" ] && continue
        # -P '' tries the empty passphrase and never asks; the public key on stdout is not wanted.
        if ! why=$(ssh-keygen -y -P '' -f "$key" 2>&1 >/dev/null); then
            ccy_agent_forwards_key "$key" && continue
            why=$(printf '%s' "$why" | tr '\n' ' ')
            printf 'SSH key %s does not open without a passphrase (%s), nobody is at the keyboard to type one, and your ssh-agent does not hold it (ssh-add %s loads it).\n' \
                "$key" "$why" "$key" >&2
            return 1
        fi
    done
}

# ccy_restart_marker_take — read and remove CCY_RESTART_RELAUNCH, the mark ccy_handle_restart_exit
# puts on the launcher it execs. Sets CCY_RESTART_RELAUNCH_SESSION to the session id being
# restarted, or to empty on any other launch. Removed from the environment, so nothing this
# launch starts inherits it and a ccy run by hand inside the session is an ordinary launch.
# On a restart it also says on stderr what is happening, and how to resume by hand if the
# launch stops before Claude starts.
#   status 1  the mark is set but is not a Claude session id
CCY_RESTART_RELAUNCH_SESSION=""
ccy_restart_marker_take() {
    local mark="${CCY_RESTART_RELAUNCH:-}"
    unset CCY_RESTART_RELAUNCH
    CCY_RESTART_RELAUNCH_SESSION=""
    [ -n "$mark" ] || return 0
    if ! [[ "$mark" =~ $CCY_RESTART_SESSION_ID_PATTERN ]]; then
        printf 'CCY_RESTART_RELAUNCH is set, but not to a Claude session id; only a supervisor-requested restart sets it.\n' >&2
        return 1
    fi
    CCY_RESTART_RELAUNCH_SESSION="$mark"
    printf 'Restarting session %s with nobody at the keyboard: nothing will be asked, and a choice only a person can make stops the launch.\n' \
        "$CCY_RESTART_RELAUNCH_SESSION" >&2
    printf '  If it stops before Claude starts, resume it by hand: ccy --resume %s\n' \
        "$CCY_RESTART_RELAUNCH_SESSION" >&2
}

# ccy_restart_refuse <session-id> <reason> — say on stderr why a restart stops here and how to
# resume the session by hand. The caller exits.
ccy_restart_refuse() {
    local sid="${1:?ccy_restart_refuse requires a session id}"
    local reason="${2:?ccy_restart_refuse requires a reason}"
    printf 'ERROR: Not restarting session %s: %s\n' "$sid" "$reason" >&2
    printf '  Resume it by hand when you are ready: ccy --resume %s\n' "$sid" >&2
}
