#!/bin/bash
# CCY session registry: one file per live session, so a reboot can be undone.
#
# WHY: a ccy session runs inside tmux on CCY's own server (lib/tmux-session.bash), which
# saves it from a dying terminal but not from a reboot — the server goes down with the
# machine and nothing knew what was running. This library is that knowledge. A record is
# written when a session STARTS and removed when its command RETURNS; a session that is
# killed — by a reboot, a power cut, a `kill-session` — never reaches the removal, so a
# record still present at boot means exactly "this was running when the machine went down".
# No shutdown hook, no timing assumption, and an unclean power loss is handled by the same
# rule as a clean reboot.
#
# WHERE: ~/.local/state/ccy/sessions/<session-name> (XDG_STATE_HOME honoured, CCY_STATE_DIR
# overrides for tests). The registry belongs to the TOOL, not to this repository's
# provisioning state under $XDG_STATE_HOME/fedora-desktop: it is written by the ccy and cc
# launchers, read by ccy-sessions, and means the same thing on a machine that installed ccy
# by other means. That is a decision, recorded in Plan 00135.
#
# FORMAT: one field per line, `key=value`, header line first, `arg=` repeated in order.
# Read with `read -r`, never sourced — a state file is data, not code — and a value that
# cannot be one line (a newline inside an argument) is refused at write time rather than
# written as two lines that read back as something else. `compose=` is optional: how the
# project's compose services stood when the session started (CCY_REGISTRY_COMPOSE_OUTCOMES),
# which a restore replays as --compose. A record without it is read as having no answer, so
# the header stays at 1; a ccy older than the key refuses a record that has it, by name.
#
# WHAT IS REPLAYED: the arguments a restore starts the launcher with are the ORIGINAL ones
# with the one-shot set removed. `--prevent` writes `never` into the project's
# allowed-hostnames file and would switch ccy off for the project being restored; `--rebuild`
# would rebuild the image every boot; `--prompt` and a bare first message would re-send a
# stale instruction. The set is enumerated in ccy_registry_replay_args and tested in
# scripts/test-ccy-session-registry.bash; anything after `--` is claude's and is kept
# verbatim. `--no-restore` is the opt-out: it is consumed here and the launcher never sees it.
#
# Requires print_error (common-pure.bash, always loaded first). ccy_registry_restore also
# needs ccy_tmux_list and ccy_tmux_start_detached from lib/tmux-session.bash.

CCY_REGISTRY_HEADER="ccy-session-record 1"

# How a session's compose services stood, as its record's compose= value:
#   started   ccy ran `up -d` for this session (asked, or --compose start)
#   declined  the answer was no (asked, or --compose skip)
#   running   they were running already, so nothing was asked
CCY_REGISTRY_COMPOSE_OUTCOMES=(started declined running)

# _ccy_registry_compose_outcome_ok <outcome> — whether it is one of the three.
_ccy_registry_compose_outcome_ok() {
    _ccy_registry_listed "${1-}" "${CCY_REGISTRY_COMPOSE_OUTCOMES[@]}"
}

# ccy_registry_dir — the registry directory, printed. A relative state home is refused for
# the reason helpers/play_ledger/ledger.py refuses one: the location would then depend on
# the cwd of whoever launched, and a restore would read an empty directory and report success.
ccy_registry_dir() {
    local base
    if [[ -n "${CCY_STATE_DIR:-}" ]]; then
        base="$CCY_STATE_DIR"
    else
        local state_home="${XDG_STATE_HOME:-}"
        if [[ -n "$state_home" && "$state_home" != /* ]]; then
            print_error "XDG_STATE_HOME='$state_home' is not absolute, so the session registry's location would depend on the current directory."
            return 1
        fi
        base="${state_home:-$HOME/.local/state}/ccy"
    fi
    if [[ "$base" != /* ]]; then
        print_error "session registry base '$base' is not an absolute path."
        return 1
    fi
    printf '%s/sessions\n' "$base"
}

# ccy_registry_wants_restore [args...] — false when `--no-restore` appears among the
# launcher's own arguments (before any `--`; after it every word is claude's).
ccy_registry_wants_restore() {
    local arg
    for arg in "$@"; do
        [[ "$arg" == "--" ]] && return 0
        [[ "$arg" == "--no-restore" ]] && return 1
    done
    return 0
}

# ccy_registry_launch_args [args...] — the argv the launcher is actually run with: the
# original with `--no-restore` removed (before `--` only), one per line, nothing else touched.
ccy_registry_launch_args() {
    local arg after_dd=false
    for arg in "$@"; do
        if [[ "$after_dd" == false ]]; then
            [[ "$arg" == "--" ]] && after_dd=true
            [[ "$arg" == "--no-restore" ]] && continue
        fi
        printf '%s\n' "$arg"
    done
}

# ccy_registry_replay_args <prefix> [args...] — the arguments a restore replays, one per
# line. PURE. For prefix `ccy` the one-shot set is removed; any other launcher (cc) forwards
# everything to claude, so ccy's flags mean nothing there and every flag is kept — but a
# bare opening instruction is as stale on a cc replay as on a ccy one, and is dropped by
# the same rule.
#
# The parser this mirrors is the launcher's own flat loop: a value-taking flag consumes the
# NEXT word whatever it is, `--` ends ccy's options. Three groups:
#   dropped, no value   modes that exit without a session, one-run switches, --ssh-agent
#                       (the agent socket is a different path after a reboot) and the opt-out
#   dropped, with value --update-token, --export-token, --connect, --disconnect, --prompt,
#                       --run-for, --until (a deadline is absolute: a replay would restart or
#                       miss it; a supervisor-requested relaunch carries it in the environment),
#                       --compose (the record's compose= is what a restore replays, so a
#                       replayed flag would say it twice, and differently once answered)
#   kept, with value    --token, --ssh-key, --network, --engine, --max-age, --teams (a
#                       session comes back in the same agent team bus seats; --teams=<list>
#                       is kept as one word)
# A word that is not a flag is a first message to claude — stale on replay — unless it
# follows a flag ccy does not know, when it is that flag's value (`--model opus`).
#
# The groups are the one list; scripts/test-ccy-session-registry.bash derives the
# launcher's flags from its parser and fails on any flag in none of them. --help, --version,
# -h and -v exit before a session exists, so they never reach a record; they are listed so
# the population is complete.
CCY_REGISTRY_DROP_FLAGS=(--rebuild --create-token --list-tokens --custom --custom-docker --top
    --prevent --debug --headless --disable-custom-docker --ssh-agent --no-restore
    --help --version -h -v)
CCY_REGISTRY_DROP_VALUE_FLAGS=(--update-token --export-token --connect --disconnect --prompt --run-for --until --compose)
CCY_REGISTRY_KEEP_VALUE_FLAGS=(--token --ssh-key --network --engine --max-age --teams)
CCY_REGISTRY_KEEP_FLAGS=(--no-ssh --github-443 --no-network --supervise --no-supervise)

# ccy_registry_flag_class <word> — drop, drop-value, keep-value, keep, or unknown.
ccy_registry_flag_class() {
    local word="${1?ccy_registry_flag_class requires a word}"
    case "$word" in
    --rebuild=* | --update-token=*)
        printf 'drop\n'
        ;;
    --teams=*)
        printf 'keep\n'
        ;;
    *)
        if _ccy_registry_listed "$word" "${CCY_REGISTRY_DROP_FLAGS[@]}"; then
            printf 'drop\n'
        elif _ccy_registry_listed "$word" "${CCY_REGISTRY_DROP_VALUE_FLAGS[@]}"; then
            printf 'drop-value\n'
        elif _ccy_registry_listed "$word" "${CCY_REGISTRY_KEEP_VALUE_FLAGS[@]}"; then
            printf 'keep-value\n'
        elif _ccy_registry_listed "$word" "${CCY_REGISTRY_KEEP_FLAGS[@]}"; then
            printf 'keep\n'
        else
            printf 'unknown\n'
        fi
        ;;
    esac
}

# _ccy_registry_listed <word> [members...] — whether the word is one of the members.
_ccy_registry_listed() {
    local word="$1" member
    shift
    for member in "$@"; do
        [[ "$member" == "$word" ]] && return 0
    done
    return 1
}

ccy_registry_replay_args() {
    local prefix="${1:?ccy_registry_replay_args requires a prefix}"
    shift
    local arg after_dd=false drop_next=false keep_next=false value_slot=false
    for arg in "$@"; do
        if [[ "$after_dd" == true ]]; then
            printf '%s\n' "$arg"
            continue
        fi
        if [[ "$arg" == "--" ]]; then
            after_dd=true
            printf '%s\n' "$arg"
            continue
        fi
        if [[ "$prefix" != "ccy" ]]; then
            case "$arg" in
            -*)
                value_slot=true
                printf '%s\n' "$arg"
                ;;
            *)
                if [[ "$value_slot" == true ]]; then
                    printf '%s\n' "$arg"
                fi
                value_slot=false
                ;;
            esac
            continue
        fi
        if [[ "$drop_next" == true ]]; then
            drop_next=false
            continue
        fi
        if [[ "$keep_next" == true ]]; then
            keep_next=false
            printf '%s\n' "$arg"
            continue
        fi
        case "$(ccy_registry_flag_class "$arg")" in
        drop)
            value_slot=false
            ;;
        drop-value)
            drop_next=true
            value_slot=false
            ;;
        keep-value)
            keep_next=true
            value_slot=false
            printf '%s\n' "$arg"
            ;;
        keep)
            value_slot=false
            printf '%s\n' "$arg"
            ;;
        *)
            if [[ "$arg" == -* ]]; then
                value_slot=true
                printf '%s\n' "$arg"
            else
                if [[ "$value_slot" == true ]]; then
                    printf '%s\n' "$arg"
                fi
                value_slot=false
            fi
            ;;
        esac
    done
}

# ccy_registry_write [--compose <outcome>] <name> <dir> <launcher> <prefix> <yes|no>
# [replay-args...] — write (or replace) the record for a session. Private to the user, written
# whole then moved into place, so a reader never sees half a record. Every rewrite of an
# existing record passes its REC_COMPOSE back, or the outcome is lost.
ccy_registry_write() {
    local compose=""
    if [[ "${1:-}" == "--compose" ]]; then
        compose="${2-}"
        shift 2 || {
            print_error "ccy_registry_write: --compose needs an outcome."
            return 1
        }
        if [[ -n "$compose" ]] && ! _ccy_registry_compose_outcome_ok "$compose"; then
            print_error "session record: compose outcome '$compose' is not one of ${CCY_REGISTRY_COMPOSE_OUTCOMES[*]}; the record was not written."
            return 1
        fi
    fi
    local name="${1:?ccy_registry_write requires a session name}"
    local dir="${2:?ccy_registry_write requires a directory}"
    local launcher="${3:?ccy_registry_write requires a launcher path}"
    local prefix="${4:?ccy_registry_write requires a prefix}"
    local restore="${5:?ccy_registry_write requires yes or no}"
    shift 5
    local regdir path tmp arg
    case "$restore" in
    yes | no) ;;
    *)
        print_error "session record for '$name': restore must be yes or no, not '$restore'."
        return 1
        ;;
    esac
    if [[ "$name" == */* ]]; then
        print_error "session record name '$name' contains a slash and cannot name a file."
        return 1
    fi
    for arg in "$name" "$dir" "$launcher" "$prefix" "$@"; do
        if [[ "$arg" == *$'\n'* ]]; then
            print_error "session record for '$name' cannot hold a value containing a newline; the record was not written."
            return 1
        fi
    done
    regdir=$(ccy_registry_dir) || return 1
    (umask 077 && mkdir -p "$regdir") || {
        print_error "could not create the session registry at $regdir"
        return 1
    }
    path="$regdir/$name"
    tmp="$path.tmp.$$"
    if ! (
        umask 077
        {
            printf '%s\n' "$CCY_REGISTRY_HEADER"
            printf 'name=%s\n' "$name"
            printf 'dir=%s\n' "$dir"
            printf 'launcher=%s\n' "$launcher"
            printf 'prefix=%s\n' "$prefix"
            printf 'restore=%s\n' "$restore"
            if [[ -n "$compose" ]]; then
                printf 'compose=%s\n' "$compose"
            fi
            for arg in "$@"; do
                printf 'arg=%s\n' "$arg"
            done
        } >"$tmp"
    ); then
        rm -f -- "$tmp"
        print_error "could not write the session record $path"
        return 1
    fi
    if ! mv -f -- "$tmp" "$path"; then
        rm -f -- "$tmp"
        print_error "could not move the session record into place at $path"
        return 1
    fi
}

# _ccy_registry_args_without_network <out-array> <network> [args...] — fill the named array
# with the ccy replay args, every `--network <network>` pair removed, walked the way
# ccy_registry_replay_args walks them: a value-taking flag consumes the next word, and after
# `--` every word is claude's. An array, not printed lines, so an empty argument survives.
_ccy_registry_args_without_network() {
    local -n _ccy_kept_out="$1"
    local network="$2"
    shift 2
    local after_dd=false
    _ccy_kept_out=()
    while [[ $# -gt 0 ]]; do
        if [[ "$after_dd" == true ]]; then
            _ccy_kept_out+=("$1")
            shift
            continue
        fi
        if [[ "$1" == "--" ]]; then
            after_dd=true
        elif [[ "$1" == "--network" && $# -ge 2 && "$2" == "$network" ]]; then
            shift 2
            continue
        else
            case "$(ccy_registry_flag_class "$1")" in
            keep-value | drop-value)
                if [[ $# -ge 2 ]]; then
                    _ccy_kept_out+=("$1" "$2")
                    shift 2
                    continue
                fi
                ;;
            esac
        fi
        _ccy_kept_out+=("$1")
        shift
    done
}

# ccy_registry_forget_network <dir> <network> [--check] — take `--network <network>` out of
# every ccy record for <dir>, so a restore after a reboot does not rejoin a network
# `ccy --disconnect` was asked to drop. Prints one line per record changed; with --check it
# changes nothing and prints the names of the records that would change. A record that
# cannot be read may name the network too, so it fails the call, after every other record
# has been handled. The rewrite goes through ccy_registry_write (whole, then moved into
# place). A record that is gone by the time it would be rewritten belongs to a session that
# ended meanwhile, and is not written back. A session that ends in the instant between that
# check and the move still gets its record back, and the next restore would start it again.
ccy_registry_forget_network() {
    local dir="${1:?ccy_registry_forget_network requires a directory}"
    local network="${2:?ccy_registry_forget_network requires a network}"
    local check=false regdir file failures=0
    local -a kept=()
    [[ "${3:-}" == "--check" ]] && check=true
    regdir=$(ccy_registry_dir) || return 1
    [[ -e "$regdir" ]] || return 0
    if [[ ! (-d "$regdir" && -r "$regdir" && -x "$regdir") ]]; then
        print_error "the session registry $regdir is not a readable directory, so its restore records cannot be checked for $network."
        return 1
    fi
    for file in "$regdir"/*; do
        [[ -e "$file" ]] || continue
        [[ "$file" == *.tmp.* ]] && continue
        if ! ccy_registry_read "$file"; then
            print_error "the restore record $file could not be read, so whether it rejoins $network is unknown."
            failures=$((failures + 1))
            continue
        fi
        [[ "$REC_PREFIX" == "ccy" && "$REC_DIR" == "$dir" ]] || continue
        _ccy_registry_args_without_network kept "$network" "${REC_ARGS[@]}"
        # Only whole pairs are removed, so a record that names the network comes back shorter.
        [[ ${#kept[@]} -ne ${#REC_ARGS[@]} ]] || continue
        if [[ "$check" == true ]]; then
            printf '%s\n' "$REC_NAME"
            continue
        fi
        [[ -e "$file" ]] || continue
        if ! ccy_registry_write --compose "$REC_COMPOSE" "$REC_NAME" "$REC_DIR" "$REC_LAUNCHER" "$REC_PREFIX" "$REC_RESTORE" "${kept[@]}"; then
            failures=$((failures + 1))
            continue
        fi
        printf 'Removed --network %s from the restore record of session %s.\n' "$network" "$REC_NAME"
    done
    [[ "$failures" -eq 0 ]]
}

# _ccy_registry_args_with_ssh_key <out-array> <key> [args...] — fill the named array with the
# ccy replay args plus `--ssh-key <key>`, put before any `--`, and return 0. Return 1, the
# array empty, when the args need no key: they already make the SSH choice (--ssh-key,
# --no-ssh), or they make no launch choice at all (no --token, --network or --no-network)
# and so are replayed through Quick Launch, whose saved configuration holds the key. An
# --ssh-key there would switch Quick Launch off and bring the token and network prompts back.
# Walked as ccy_registry_replay_args walks: a value-taking flag consumes the next word, and
# after `--` every word is claude's.
_ccy_registry_args_with_ssh_key() {
    local -n _ccy_with_key_out="$1"
    local key="$2"
    shift 2
    local -a args=("$@")
    local i end=${#args[@]} skips_quick_launch=false
    _ccy_with_key_out=()
    for ((i = 0; i < ${#args[@]}; i++)); do
        case "${args[i]}" in
        --)
            end=$i
            break
            ;;
        --ssh-key | --no-ssh) return 1 ;;
        --token | --network | --no-network) skips_quick_launch=true ;;
        esac
        case "$(ccy_registry_flag_class "${args[i]}")" in
        keep-value | drop-value) i=$((i + 1)) ;;
        esac
    done
    [[ "$skips_quick_launch" == true ]] || return 1
    _ccy_with_key_out=("${args[@]:0:end}" --ssh-key "$key" "${args[@]:end}")
}

# ccy_registry_record_ssh_key <name> <key> — add the SSH key chosen at ccy's key prompt to
# session <name>'s record, so its restore after a reboot starts with --ssh-key and never
# waits at that prompt with nobody there (fedora-desktop#69). The launcher calls it once the
# key is chosen. Only a ccy record whose arguments skip Quick Launch takes it; see
# _ccy_registry_args_with_ssh_key for why the others do not need it. No record (a session ccy
# did not start under tmux) is nothing to do; a record that cannot be read or rewritten is a
# failure. The rewrite goes through ccy_registry_write (whole, then moved into place).
ccy_registry_record_ssh_key() {
    local name="${1:?ccy_registry_record_ssh_key requires a session name}"
    local key="${2:?ccy_registry_record_ssh_key requires a key}"
    local regdir file
    local -a with_key=()
    regdir=$(ccy_registry_dir) || return 1
    file="$regdir/$name"
    [[ -e "$file" ]] || return 0
    if ! ccy_registry_read "$file"; then
        print_error "the restore record of session $name could not be read, so the SSH key chosen for it cannot be recorded."
        return 1
    fi
    [[ "$REC_PREFIX" == "ccy" ]] || return 0
    _ccy_registry_args_with_ssh_key with_key "$key" "${REC_ARGS[@]}" || return 0
    ccy_registry_write --compose "$REC_COMPOSE" "$REC_NAME" "$REC_DIR" "$REC_LAUNCHER" "$REC_PREFIX" "$REC_RESTORE" "${with_key[@]}" || return 1
    printf 'Recorded --ssh-key %s for session %s: a restore after a reboot uses it without asking.\n' \
        "$key" "$name" >&2
}

# ccy_registry_record_compose <name> <outcome> — put how the project's compose services stood
# (CCY_REGISTRY_COMPOSE_OUTCOMES) into session <name>'s record, so its restore after a reboot
# starts with --compose and never waits at the compose question with nobody there
# (fedora-desktop#87). The launcher calls it once the network and compose decisions are made.
# No record (a session ccy did not start under tmux) is nothing to do; a record that cannot be
# read or rewritten is a failure. The rewrite goes through ccy_registry_write.
ccy_registry_record_compose() {
    local name="${1:?ccy_registry_record_compose requires a session name}"
    local outcome="${2:?ccy_registry_record_compose requires an outcome}"
    local regdir file
    if ! _ccy_registry_compose_outcome_ok "$outcome"; then
        print_error "compose outcome '$outcome' is not one of ${CCY_REGISTRY_COMPOSE_OUTCOMES[*]}; nothing recorded for session $name."
        return 1
    fi
    regdir=$(ccy_registry_dir) || return 1
    file="$regdir/$name"
    [[ -e "$file" ]] || return 0
    if ! ccy_registry_read "$file"; then
        print_error "the restore record of session $name could not be read, so how its compose services stood cannot be recorded."
        return 1
    fi
    ccy_registry_write --compose "$outcome" "$REC_NAME" "$REC_DIR" "$REC_LAUNCHER" "$REC_PREFIX" "$REC_RESTORE" "${REC_ARGS[@]}" || return 1
    printf 'Recorded compose=%s for session %s: a restore after a reboot answers the compose question the same way.\n' \
        "$outcome" "$name" >&2
}

# ccy_registry_remove <name> — delete a session's record. An absent record is not an error:
# the session may have been started before the registry existed.
ccy_registry_remove() {
    local name="${1:?ccy_registry_remove requires a session name}" regdir
    regdir=$(ccy_registry_dir) || return 1
    rm -f -- "$regdir/$name"
}

# ccy_registry_read <file> — parse a record into REC_NAME, REC_DIR, REC_LAUNCHER,
# REC_PREFIX, REC_RESTORE, REC_COMPOSE (empty when the record has none) and the REC_ARGS
# array. Strict: a wrong header, an unknown key, an unknown compose outcome or a missing
# field is a rejection, never a guess — a guessed record starts the wrong thing in the wrong
# place.
ccy_registry_read() {
    local file="${1:?ccy_registry_read requires a record path}" line key value first=true
    REC_NAME="" REC_DIR="" REC_LAUNCHER="" REC_PREFIX="" REC_RESTORE="" REC_COMPOSE=""
    REC_ARGS=()
    if [[ ! -r "$file" ]]; then
        print_error "session record $file cannot be read."
        return 1
    fi
    while IFS= read -r line || [[ -n "$line" ]]; do
        if [[ "$first" == true ]]; then
            first=false
            if [[ "$line" != "$CCY_REGISTRY_HEADER" ]]; then
                print_error "session record $file does not start with '$CCY_REGISTRY_HEADER' and is not read."
                return 1
            fi
            continue
        fi
        [[ -n "$line" ]] || continue
        if [[ "$line" != *=* ]]; then
            print_error "session record $file has a line without '=': $line"
            return 1
        fi
        key="${line%%=*}"
        value="${line#*=}"
        case "$key" in
        name) REC_NAME="$value" ;;
        dir) REC_DIR="$value" ;;
        launcher) REC_LAUNCHER="$value" ;;
        prefix) REC_PREFIX="$value" ;;
        restore) REC_RESTORE="$value" ;;
        compose)
            if ! _ccy_registry_compose_outcome_ok "$value"; then
                print_error "session record $file has compose='$value'; expected one of ${CCY_REGISTRY_COMPOSE_OUTCOMES[*]}."
                return 1
            fi
            REC_COMPOSE="$value"
            ;;
        arg) REC_ARGS+=("$value") ;;
        *)
            print_error "session record $file has an unknown key '$key' and is not read."
            return 1
            ;;
        esac
    done <"$file"
    if [[ "$first" == true ]]; then
        print_error "session record $file is empty."
        return 1
    fi
    for key in NAME DIR LAUNCHER PREFIX RESTORE; do
        local -n field="REC_$key"
        if [[ -z "$field" ]]; then
            print_error "session record $file is missing its ${key,,} field."
            return 1
        fi
    done
    case "$REC_RESTORE" in
    yes | no) ;;
    *)
        print_error "session record $file has restore='$REC_RESTORE'; expected yes or no."
        return 1
        ;;
    esac
}

# The line a failed session's window is held on, which is how verify-restore tells a pane
# whose launcher has already exited from one that is still starting. It is spliced into a
# single-quoted printf format in the trampoline, so it must hold no quote and no percent.
CCY_SESSION_ENDED_TEXT="Press Enter to close this session."

# ccy_registry_trampoline <record-path> <prefix> — the `bash -c` string a session's tmux
# pane runs, printed. It runs the launcher (the pane's remaining arguments), removes the
# record the moment the launcher RETURNS — a returned launcher is an ended session, whatever
# its status — and on a non-zero status holds the window so the error can be read.
#
# The removal is here and nowhere else, because this is the one place that runs after the
# session's command and does not run when the session is killed. That asymmetry is the
# whole feature: a reboot kills the pane's bash before it gets here, and the record stays.
# The dollars are literal: they expand in the bash tmux starts, not in the caller.
ccy_registry_trampoline() {
    local record="${1:?ccy_registry_trampoline requires a record path}"
    local prefix="${2:?ccy_registry_trampoline requires a prefix}" quoted
    printf -v quoted '%q' "$record"
    printf '%s' "\"\$@\"; rc=\$?; rm -f -- ${quoted}; if [ \"\$rc\" -ne 0 ]; then printf '\\n${prefix} exited with status %s. ${CCY_SESSION_ENDED_TEXT}\\n' \"\$rc\"; read -r; fi; exit \"\$rc\""
}

# ccy_registry_restore_args [--compose <outcome>] <prefix> [replay-args...] — the arguments a
# restore starts the launcher with, one per line: the recorded set, with `--compose start`
# (outcome started or running: `up -d` leaves running services alone) or `--compose skip`
# (declined) for ccy when the record holds an outcome, and `--supervise` for ccy unless the
# record already says --supervise or --no-supervise, then `--continue` unless the record
# already continues or resumes a conversation. cc forwards every argument to claude and has
# no compose question, and --compose and --supervise are ccy's flags, so cc gets --continue
# only. A record with no outcome gets no --compose: the launcher then asks, as for a person.
#
# After a recorded `--` every word is claude's, so ccy's flags go in before it, and a
# --supervise after it is claude's word rather than ccy's. --continue is claude's flag and
# reaches claude from either side, so it is appended.
ccy_registry_restore_args() {
    local outcome=""
    if [[ "${1:-}" == "--compose" ]]; then
        outcome="${2-}"
        shift 2 || {
            print_error "ccy_registry_restore_args: --compose needs an outcome."
            return 1
        }
    fi
    local prefix="${1:?ccy_registry_restore_args requires a prefix}"
    shift
    local -a ccy_flags=()
    if [[ "$prefix" == "ccy" && -n "$outcome" ]]; then
        case "$outcome" in
        started | running) ccy_flags+=(--compose start) ;;
        declined) ccy_flags+=(--compose skip) ;;
        *)
            print_error "compose outcome '$outcome' is not one of ${CCY_REGISTRY_COMPOSE_OUTCOMES[*]}."
            return 1
            ;;
        esac
    fi
    local arg has_supervise=false has_continue=false after_dd=false
    for arg in "$@"; do
        if [[ "$arg" == "--" ]]; then
            after_dd=true
            continue
        fi
        case "$arg" in
        --supervise | --no-supervise) [[ "$after_dd" == true ]] || has_supervise=true ;;
        --continue | -c | --resume | -r) has_continue=true ;;
        esac
    done
    [[ "$prefix" == "ccy" && "$has_supervise" == false ]] && ccy_flags+=(--supervise)
    after_dd=false
    for arg in "$@"; do
        if [[ "$arg" == "--" && "$after_dd" == false ]]; then
            after_dd=true
            if [[ ${#ccy_flags[@]} -gt 0 ]]; then
                printf '%s\n' "${ccy_flags[@]}"
                ccy_flags=()
            fi
        fi
        printf '%s\n' "$arg"
    done
    if [[ ${#ccy_flags[@]} -gt 0 ]]; then
        printf '%s\n' "${ccy_flags[@]}"
    fi
    if [[ "$has_continue" == false ]]; then
        printf '%s\n' "--continue"
    fi
}

# ── the restore manifest: what the last restore brought up, for verify-restore ──────────
#
# A restored session's record is removed by its trampoline the moment its launcher returns,
# so a session that failed to come back leaves no record behind to be checked. The manifest
# is the restore's own account of every session that should now be up (the ones it started
# and the ones it found already running), written when it finishes, and it names the boot
# it ran in: a manifest from an earlier boot says nothing about this one.
# It lives beside the registry, never inside it: restore reads every file in the registry.
#
# Each entry also carries what `ccy-sessions set-going` needs and did (Plan 00135 Phase 8):
#   resume       the conversation id the launch names with --resume, or empty: --continue,
#                whose conversation is the newest transcript in the project
#   supervised   yes when a ccy supervisor carries the session on after a compaction (a ccy
#                launch without --no-supervise), else no
#   going        none (found already running: a person started it, nothing is typed into
#                it), pending (restored, not yet decided), compacting (no supervisor:
#                /compact typed at `at`, `continue` to follow once it ends), compact or
#                continue (the last thing typed, at `at`, epoch seconds), or untouched (left
#                alone, `detail` says why)
#   detail       the context size in tokens, or the reason it was left alone
#   transcript   the conversation's file, which verify-restore reads to see the input taken
# Every write holds CCY_RESTORE_MANIFEST_LOCK (ccy_restore_manifest_locked), so a second
# restore in the same boot and a set-going run cannot drop each other's entries.
CCY_RESTORE_MANIFEST_HEADER="ccy-restore-manifest 2"
CCY_RESTORE_MANIFEST_KEYS=(name prefix dir resume supervised going at detail transcript)

# ccy_registry_manifest_path — the manifest's path, printed.
ccy_registry_manifest_path() {
    local regdir
    regdir=$(ccy_registry_dir) || return 1
    printf '%s/last-restore\n' "${regdir%/sessions}"
}

# ccy_boot_id — this boot's id, printed. CCY_BOOT_ID_FILE overrides the source for tests.
ccy_boot_id() {
    local file="${CCY_BOOT_ID_FILE:-/proc/sys/kernel/random/boot_id}" id=""
    if ! IFS= read -r id <"$file" || [[ -z "$id" ]]; then
        print_error "this boot's id could not be read from $file."
        return 1
    fi
    printf '%s\n' "$id"
}

# ccy_restore_manifest_locked <command> [args...] — run the command holding the manifest's
# lock (flock on last-restore.lock beside it, waited for up to 60 s). Every read-modify-write
# of the manifest goes through here. The command runs in this shell, so what it sets stays.
ccy_restore_manifest_locked() {
    local path lock fd rc=0
    path=$(ccy_registry_manifest_path) || return 1
    lock="$path.lock"
    (umask 077 && mkdir -p "$(dirname "$path")") || {
        print_error "could not create the directory for the restore manifest $path"
        return 1
    }
    if ! exec {fd}>>"$lock"; then
        print_error "could not open the restore manifest's lock $lock"
        return 1
    fi
    if ! flock -w 60 "$fd"; then
        exec {fd}>&-
        print_error "the restore manifest's lock $lock was not released within 60 s; nothing was written."
        return 1
    fi
    "$@" || rc=$?
    exec {fd}>&-
    return "$rc"
}

# ccy_restore_manifest_index <name> — the index of <name> in RM_NAMES, printed; 1 if absent.
ccy_restore_manifest_index() {
    local i
    for i in "${!RM_NAMES[@]}"; do
        if [[ "${RM_NAMES[i]}" == "$1" ]]; then
            printf '%s\n' "$i"
            return 0
        fi
    done
    return 1
}

# ccy_restore_manifest_update <boot> <name> <expect-going> <going> <at> <detail> <transcript>
# — under the lock, re-read the manifest and set one entry's outcome, but only while it is
# still this boot's and the entry still says <expect-going>: a second restore may have
# rewritten it meanwhile. Returns 3, changing nothing, when it no longer applies; RM_* then
# hold the manifest as it is.
ccy_restore_manifest_update() {
    ccy_restore_manifest_locked _ccy_restore_manifest_update "$@"
}
_ccy_restore_manifest_update() {
    local boot="$1" name="$2" expect="$3" i
    ccy_restore_manifest_read || return 1
    if [[ "$RM_BOOT" != "$boot" ]] || ! i=$(ccy_restore_manifest_index "$name") ||
        [[ "${RM_GOING[i]}" != "$expect" ]]; then
        return 3
    fi
    RM_GOING[i]="$4" RM_AT[i]="$5" RM_DETAIL[i]="$6" RM_TRANSCRIPT[i]="$7"
    ccy_restore_manifest_rewrite
}

# _ccy_restore_manifest_merge [<entry groups>]... — the restore's write, run under the lock.
# An entry whose going is "keep" (a session found already running) takes the entry this
# boot's manifest already holds for that name, so a second restore leaves an earlier one's
# set-going state alone; with none there, it is written as none. A manifest that cannot be
# read is replaced: the entries this restore knows are the ones that should be up.
_ccy_restore_manifest_merge() {
    local width="${#CCY_RESTORE_MANIFEST_KEYS[@]}" boot i n
    local -a out=() group=()
    boot=$(ccy_boot_id) || return 1
    local path
    path=$(ccy_registry_manifest_path) || return 1
    # No manifest is the first restore of a boot. One that cannot be read says why on
    # stderr and is replaced; one from an earlier boot has nothing to keep.
    if [[ ! -e "$path" ]] || ! ccy_restore_manifest_read || [[ "$RM_BOOT" != "$boot" ]]; then
        RM_NAMES=()
    fi
    while [[ $# -ge "$width" ]]; do
        group=("${@:1:width}")
        shift "$width"
        if [[ "${group[5]}" == keep ]]; then
            if i=$(ccy_restore_manifest_index "${group[0]}"); then
                group=("${RM_NAMES[i]}" "${RM_PREFIXES[i]}" "${RM_DIRS[i]}" "${RM_RESUME[i]}"
                    "${RM_SUPERVISED[i]}" "${RM_GOING[i]}" "${RM_AT[i]}" "${RM_DETAIL[i]}" "${RM_TRANSCRIPT[i]}")
            else
                group[5]=none
            fi
        fi
        out+=("${group[@]}")
    done
    n=$#
    [[ "$n" -eq 0 ]] || {
        print_error "the restore manifest was handed $n values that make no whole entry."
        return 1
    }
    ccy_restore_manifest_write "${out[@]}"
}

# ccy_restore_manifest_write [<a group of values, one per CCY_RESTORE_MANIFEST_KEYS>]... —
# record the sessions a restore brought up, stamped with this boot's id. Written whole then
# moved into place, like a record. A value holding a newline is refused: it would read back
# as two lines. A caller that read the manifest first holds the lock around both.
ccy_restore_manifest_write() {
    local path boot tmp value width="${#CCY_RESTORE_MANIFEST_KEYS[@]}"
    if [[ $(($# % width)) -ne 0 ]]; then
        print_error "ccy_restore_manifest_write takes groups of $width values, not $#."
        return 1
    fi
    for value in "$@"; do
        if [[ "$value" == *$'\n'* ]]; then
            print_error "the restore manifest cannot hold a value with a newline in it: $value"
            return 1
        fi
    done
    path=$(ccy_registry_manifest_path) || return 1
    boot=$(ccy_boot_id) || return 1
    (umask 077 && mkdir -p "$(dirname "$path")") || {
        print_error "could not create the directory for the restore manifest $path"
        return 1
    }
    tmp="$path.tmp.$$"
    if ! (
        umask 077
        {
            printf '%s\n' "$CCY_RESTORE_MANIFEST_HEADER"
            printf 'boot=%s\n' "$boot"
            local key
            while [[ $# -ge "$width" ]]; do
                for key in "${CCY_RESTORE_MANIFEST_KEYS[@]}"; do
                    printf '%s=%s\n' "$key" "$1"
                    shift
                done
            done
        } >"$tmp"
    ); then
        rm -f -- "$tmp"
        print_error "could not write the restore manifest $path"
        return 1
    fi
    if ! mv -f -- "$tmp" "$path"; then
        rm -f -- "$tmp"
        print_error "could not move the restore manifest into place at $path"
        return 1
    fi
}

# ccy_restore_manifest_read — parse the manifest into RM_BOOT and the parallel arrays
# RM_NAMES, RM_PREFIXES, RM_DIRS, RM_RESUME, RM_SUPERVISED, RM_GOING, RM_AT, RM_DETAIL and
# RM_TRANSCRIPT. Strict, like ccy_registry_read: each entry is every key of
# CCY_RESTORE_MANIFEST_KEYS in that order, `going` one of its words, `supervised` yes or
# no, `at` digits or empty, and anything else is a rejection. A missing manifest is a
# rejection too, with its own message: no restore has run.
ccy_restore_manifest_read() {
    local path line key value first=true expect_at=0 expect
    local width="${#CCY_RESTORE_MANIFEST_KEYS[@]}"
    expect="${CCY_RESTORE_MANIFEST_KEYS[0]}"
    RM_BOOT=""
    RM_NAMES=() RM_PREFIXES=() RM_DIRS=() RM_RESUME=() RM_SUPERVISED=()
    RM_GOING=() RM_AT=() RM_DETAIL=() RM_TRANSCRIPT=()
    path=$(ccy_registry_manifest_path) || return 1
    if [[ ! -e "$path" ]]; then
        print_error "no session restore has been recorded ($path does not exist)."
        return 1
    fi
    if [[ ! -r "$path" ]]; then
        print_error "the restore manifest $path cannot be read."
        return 1
    fi
    while IFS= read -r line || [[ -n "$line" ]]; do
        if [[ "$first" == true ]]; then
            first=false
            if [[ "$line" == "ccy-restore-manifest "* && "$line" != "$CCY_RESTORE_MANIFEST_HEADER" ]]; then
                print_error "the restore manifest $path is '$line', written by another version of ccy-sessions; this one reads '$CCY_RESTORE_MANIFEST_HEADER'. The next restore (at the next boot) writes it again."
                return 1
            fi
            if [[ "$line" != "$CCY_RESTORE_MANIFEST_HEADER" ]]; then
                print_error "the restore manifest $path does not start with '$CCY_RESTORE_MANIFEST_HEADER' and is not read."
                return 1
            fi
            continue
        fi
        [[ -n "$line" ]] || continue
        key="${line%%=*}"
        value="${line#*=}"
        if [[ "$line" != *=* ]]; then
            print_error "the restore manifest $path has a line without '=': $line"
            return 1
        fi
        if [[ "$key" == boot && -z "$RM_BOOT" && "$expect_at" -eq 0 && "${#RM_NAMES[@]}" -eq 0 ]]; then
            RM_BOOT="$value"
            continue
        fi
        if [[ "$key" != "$expect" ]]; then
            print_error "the restore manifest $path has '$key' where '$expect' was expected and is not read."
            return 1
        fi
        case "$key" in
        name) RM_NAMES+=("$value") ;;
        prefix) RM_PREFIXES+=("$value") ;;
        dir) RM_DIRS+=("$value") ;;
        resume) RM_RESUME+=("$value") ;;
        supervised)
            if [[ "$value" != yes && "$value" != no ]]; then
                print_error "the restore manifest $path has supervised=$value, which is neither yes nor no, and is not read."
                return 1
            fi
            RM_SUPERVISED+=("$value")
            ;;
        going)
            case "$value" in
            none | pending | compacting | compact | continue | untouched) RM_GOING+=("$value") ;;
            *)
                print_error "the restore manifest $path has going=$value, which is none of none, pending, compacting, compact, continue or untouched, and is not read."
                return 1
                ;;
            esac
            ;;
        at)
            if [[ ! "$value" =~ ^[0-9]*$ ]]; then
                print_error "the restore manifest $path has at=$value, which is not a time in seconds, and is not read."
                return 1
            fi
            RM_AT+=("$value")
            ;;
        detail) RM_DETAIL+=("$value") ;;
        transcript) RM_TRANSCRIPT+=("$value") ;;
        esac
        expect_at=$(((expect_at + 1) % width))
        expect="${CCY_RESTORE_MANIFEST_KEYS[expect_at]}"
    done <"$path"
    if [[ "$first" == true ]]; then
        print_error "the restore manifest $path is empty."
        return 1
    fi
    if [[ -z "$RM_BOOT" ]]; then
        print_error "the restore manifest $path names no boot."
        return 1
    fi
    if [[ "$expect_at" -ne 0 ]]; then
        print_error "the restore manifest $path ends part-way through an entry."
        return 1
    fi
}

# ccy_restore_manifest_rewrite — write RM_* back as the manifest. The boot is re-stamped
# from ccy_boot_id, so a caller checks RM_BOOT first, and holds the lock.
ccy_restore_manifest_rewrite() {
    local i
    local -a entries=()
    for i in "${!RM_NAMES[@]}"; do
        entries+=("${RM_NAMES[i]}" "${RM_PREFIXES[i]}" "${RM_DIRS[i]}" "${RM_RESUME[i]}"
            "${RM_SUPERVISED[i]}" "${RM_GOING[i]}" "${RM_AT[i]}" "${RM_DETAIL[i]}" "${RM_TRANSCRIPT[i]}")
    done
    ccy_restore_manifest_write "${entries[@]}"
}

# ccy_registry_resume_id [restore-args...] — the conversation id the launch resumes, printed:
# the value of --resume <id>, --resume=<id> or -r <id>; nothing for --continue or no flag.
# PURE. A ccy flag that takes a value consumes the next word, as the launcher's parser does,
# so `--token -r` names a token, not a resume.
ccy_registry_resume_id() {
    local word id="" skip=false after_dd=false
    while [[ $# -gt 0 ]]; do
        word="$1"
        shift
        if [[ "$skip" == true ]]; then
            skip=false
            continue
        fi
        if [[ "$word" == "--" ]]; then
            after_dd=true
            continue
        fi
        case "$word" in
        --resume=*) id="${word#--resume=}" ;;
        --resume | -r)
            id="${1:-}"
            if [[ $# -gt 0 ]]; then
                shift
            fi
            ;;
        *)
            if [[ "$after_dd" == false ]]; then
                case "$(ccy_registry_flag_class "$word")" in
                keep-value | drop-value) skip=true ;;
                esac
            fi
            ;;
        esac
    done
    [[ -z "$id" ]] || printf '%s\n' "$id"
}

# ccy_registry_supervised <prefix> [restore-args...] — "yes" when a ccy supervisor carries
# the session on after a compaction, else "no". PURE. Only ccy has one; it is on unless the
# launch says --no-supervise (the last of --supervise/--no-supervise before `--` wins, as
# in the launcher's flat loop).
ccy_registry_supervised() {
    local prefix="$1" word answer=yes
    shift
    if [[ "$prefix" != ccy ]]; then
        printf 'no\n'
        return 0
    fi
    for word in "$@"; do
        [[ "$word" != "--" ]] || break
        case "$word" in
        --supervise) answer=yes ;;
        --no-supervise) answer=no ;;
        esac
    done
    printf '%s\n' "$answer"
}

# ccy_restore_verdict <prefix> <live 0|1> <screen> <container> [<going>] — one restored
# session's state, printed as "<STATE>[ <detail>]". PURE: every probe arrives as text, so
# every shape is testable (scripts/test-ccy-session-registry.bash).
#   <screen>     the pane's visible text (tmux capture-pane -p)
#   <container>  for ccy: "up" when the session's container is running, "starting" when
#                its engine client exists and the container is not yet listed, "-" when
#                there is no engine client; ignored for cc, which runs claude on the host
#   <going>      whether set-going has set it going (ccy_restore_going_state): "-" or
#                "started" (nothing more to wait for), "setting-going", or
#                "failed:<reason>"; default "-"
#
# The screen is judged by its LAST non-blank line, where a prompt waiting for input sits:
#   DEAD launcher-exited            the trampoline's hold line: the launcher has returned
#   WAITING-AT-PROMPT <name>        a prompt from ccy_known_prompts
#   STARTING                        a ccy session with no running container yet
#   SETTING-GOING                   running, and set-going has not yet seen it take input
#   NOT-SET-GOING <reason>          running, and set-going left it alone or it never
#                                   took what was typed
#   OK                              anything else, with the container up for ccy
# A session that is not live at all is "DEAD session-not-running".
ccy_restore_verdict() {
    local prefix="$1" live="$2" screen="$3" container="$4" going="${5:--}"
    local last="" line name text
    if [[ "$live" != 1 ]]; then
        printf 'DEAD session-not-running\n'
        return 0
    fi
    while IFS= read -r line; do
        line="${line%"${line##*[![:space:]]}"}"
        [[ -n "$line" ]] && last="$line"
    done <<<"$screen"
    last="${last#"${last%%[![:space:]]*}"}"
    if [[ "$last" == *"$CCY_SESSION_ENDED_TEXT" ]]; then
        printf 'DEAD launcher-exited\n'
        return 0
    fi
    while IFS=$'\t' read -r name text; do
        [[ -n "$name" ]] || continue
        if [[ "$last" == "$text"* ]]; then
            printf 'WAITING-AT-PROMPT %s\n' "$name"
            return 0
        fi
    done < <(ccy_known_prompts)
    case "$prefix" in
    cc) ;;
    ccy)
        if [[ "$container" != up ]]; then
            printf 'STARTING\n'
            return 0
        fi
        ;;
    *)
        printf 'DEAD unknown-launcher-%s\n' "$prefix"
        return 0
        ;;
    esac
    case "$going" in
    - | started) printf 'OK\n' ;;
    setting-going) printf 'SETTING-GOING\n' ;;
    failed:*) printf 'NOT-SET-GOING %s\n' "${going#failed:}" ;;
    *) printf 'NOT-SET-GOING unknown-state-%s\n' "$going" ;;
    esac
}

# ── setting a restored session going (Plan 00135 Task 8.2, fedora-desktop#88) ──────────
#
# A session restored with --continue comes back at an empty prompt with its conversation
# reloaded and a cold prompt cache. `ccy-sessions set-going` types the first thing into it:
# `/compact` when its context is at or above the floor, so the first cold turn is the one
# that shrinks it, else `continue`. What follows reads the two things that decision needs:
# whether Claude's prompt has drawn (the pane) and how big the context is (the transcript).
# The reasoning, and what each was measured against, is in Plan 00135's journal (26-10-08).

# ccy_screen_plain <screen> — the screen with its escape sequences removed, printed. PURE.
# set-going captures the pane with `capture-pane -e`, so the input box's placeholder can be
# told from typed text by its attributes; everything else reads the plain text.
ccy_screen_plain() {
    local text="$1" csi=$'\e\\[[0-9;:?]*[A-Za-z]'
    while [[ "$text" =~ $csi ]]; do
        text="${text//"${BASH_REMATCH[0]}"/}"
    done
    printf '%s\n' "$text"
}

# _ccy_input_line_empty <raw-input-line> — whether the input box line holds no typed text.
# Claude draws `❯`, a (no-break) space, and when the box is empty a placeholder in dim (SGR 2),
# which tmux -e reproduces as an SGR sequence carrying a 2. Typed text has no dim on it.
_ccy_input_line_empty() {
    local rest="${1#*❯}" sgr=$'^\e\\[([0-9;:]*)m'
    while [[ -n "$rest" ]]; do
        case "$rest" in
        " "* | $'\xc2\xa0'*)
            rest="${rest#" "}"
            rest="${rest#$'\xc2\xa0'}"
            ;;
        $'\e'*)
            [[ "$rest" =~ $sgr ]] || return 1
            if [[ ";${BASH_REMATCH[1]};" == *";2;"* ]]; then
                return 0
            fi
            rest="${rest#"${BASH_REMATCH[0]}"}"
            ;;
        *) return 1 ;;
        esac
    done
    return 0
}

# ccy_claude_screen_state <screen> — "ready", "busy", "typed" or "not-drawn". PURE.
# <screen> is `capture-pane -p -e` output (plain text works too, minus the placeholder test).
# Claude Code's input box is a line beginning with ❯ framed by rule lines of ─ above and
# below it. The same ❯ marks the cursor in Claude's selection dialogs, which are not framed
# that way, so only a framed ❯ counts.
#   busy    "esc to interrupt" in the few lines just above the box, where Claude's spinner
#           sits while a turn or a compaction runs: typing then would queue behind it. The
#           conversation above can quote the phrase; only the status area counts.
#   typed   the box already holds text someone typed: Enter would submit it with ours.
ccy_claude_screen_state() {
    local screen="$1" i j k seen plain
    local -a lines=() raw=()
    # Ten rule characters as a literal prefix, not a regex repeat: in the C locale a unit may
    # run in, a repeat would apply to the last byte of the three-byte ─ only.
    local rule='──────────'
    plain="$(ccy_screen_plain "$screen")"
    mapfile -t lines <<<"$plain"
    mapfile -t raw <<<"$screen"
    for ((i = 1; i < ${#lines[@]}; i++)); do
        [[ "${lines[i]}" == ❯* && "${lines[i - 1]}" == "$rule"* ]] || continue
        for ((j = i + 1; j < ${#lines[@]}; j++)); do
            [[ "${lines[j]}" == "$rule"* ]] || continue
            seen=0
            for ((k = i - 2; k >= 0 && seen < 4; k--)); do
                [[ -n "${lines[k]// /}" ]] || continue
                seen=$((seen + 1))
                if [[ "${lines[k]}" == *"esc to interrupt"* ]]; then
                    printf 'busy\n'
                    return 0
                fi
            done
            if [[ "${#raw[@]}" -eq "${#lines[@]}" ]] && ! _ccy_input_line_empty "${raw[i]}"; then
                printf 'typed\n'
            else
                printf 'ready\n'
            fi
            return 0
        done
    done
    printf 'not-drawn\n'
}

# ccy_transcript_dir <prefix> <dir> <config-home> — the directory holding a session's
# Claude transcripts, printed. PURE. ccy runs claude in its container at /workspace with
# /root/.claude linked to the project's .claude/ccy, so its transcripts are always under
# .claude/ccy/projects/-workspace. cc runs claude on the host, which names the directory for
# the working directory with every character that is not a letter or digit made a '-'.
ccy_transcript_dir() {
    local prefix="$1" dir="$2" config_home="$3"
    case "$prefix" in
    ccy) printf '%s/.claude/ccy/projects/-workspace\n' "$dir" ;;
    cc) printf '%s/projects/%s\n' "$config_home" "${dir//[^A-Za-z0-9]/-}" ;;
    *)
        print_error "no transcript directory is known for launcher '$prefix'."
        return 1
        ;;
    esac
}

# ccy_transcript_newest <transcript-dir> — the newest *.jsonl directly in it, printed: the
# conversation `claude --continue` resumes. Subdirectories hold sub-agent transcripts and
# are not looked in. None there is a failure, with the reason on stderr.
ccy_transcript_newest() {
    local tdir="$1" newest="" file
    if [[ ! -d "$tdir" ]]; then
        print_error "there is no transcript directory $tdir"
        return 1
    fi
    for file in "$tdir"/*.jsonl; do
        [[ -f "$file" ]] || continue
        if [[ -z "$newest" || "$file" -nt "$newest" ]]; then
            newest="$file"
        fi
    done
    if [[ -z "$newest" ]]; then
        print_error "there is no transcript in $tdir"
        return 1
    fi
    printf '%s\n' "$newest"
}

# ccy_transcript_for <transcript-dir> <resume-id> — the conversation a restored session
# resumes, printed: <id>.jsonl for a launch with --resume <id>, else the newest
# (ccy_transcript_newest). An id that is not a plain conversation id, or whose file is not
# there, is a failure: reading another conversation would decide on the wrong one.
ccy_transcript_for() {
    local tdir="$1" id="$2"
    if [[ -z "$id" ]]; then
        ccy_transcript_newest "$tdir"
        return
    fi
    if [[ ! "$id" =~ ^[A-Za-z0-9-]+$ ]]; then
        print_error "the resumed conversation id '$id' is not a conversation id."
        return 1
    fi
    if [[ ! -f "$tdir/$id.jsonl" ]]; then
        print_error "the resumed conversation $tdir/$id.jsonl is not there."
        return 1
    fi
    printf '%s\n' "$tdir/$id.jsonl"
}

# ccy_transcript_context_tokens <file> — the conversation's context size in tokens, printed:
# that of the LAST main-thread entry carrying one. An assistant message's input, cache-write
# and cache-read tokens together are what the next turn re-sends (Claude Code's own context
# figure); a compaction boundary's postTokens is the size a compaction left. Sub-agent
# (sidechain) entries are another context, and a <synthetic> assistant message is an error
# placeholder with zero usage, so neither counts. A line that is not JSON is skipped: the
# last one can be half-written while Claude appends to it. No size anywhere is a failure.
ccy_transcript_context_tokens() {
    local file="$1" tokens
    if ! tokens=$(jq -R -n -r '
        [inputs | fromjson? | objects | select((.isSidechain // false) | not)
         | if .type == "assistant" and (.message.usage | type) == "object"
              and (.message.model // "") != "<synthetic>" then
             .message.usage | (.input_tokens // 0) + (.cache_creation_input_tokens // 0)
                 + (.cache_read_input_tokens // 0)
           elif .type == "system" and .subtype == "compact_boundary"
              and (.compactMetadata.postTokens | type) == "number" then
             .compactMetadata.postTokens
           else empty end] | last // "none"' "$file" 2>&1); then
        print_error "the transcript $file could not be read: $tokens"
        return 1
    fi
    if [[ ! "$tokens" =~ ^[0-9]+$ ]]; then
        print_error "the transcript $file records no context size."
        return 1
    fi
    printf '%s\n' "$tokens"
}

# ccy_transcript_took_input_since <file> <epoch> — "yes" when the conversation took input at
# or after <epoch>, else "no". Input that is submitted is written to the transcript as a
# main-thread user entry the moment it is taken (a typed /compact, its expansion, or
# `continue`); a line left sitting in the input box writes nothing. Meta entries are
# Claude's own notes, not input.
ccy_transcript_took_input_since() {
    local file="$1" since="$2" answer
    if ! answer=$(jq -R -n -r --argjson since "$since" '
        [inputs | fromjson? | objects
         | select(.type == "user" and ((.isSidechain // false) | not) and ((.isMeta // false) | not))
         | .timestamp | strings | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601
         | select(. >= $since)] | if length > 0 then "yes" else "no" end' "$file" 2>&1); then
        print_error "the transcript $file could not be read: $answer"
        return 1
    fi
    printf '%s\n' "$answer"
}

# ccy_transcript_compacted_since <file> <epoch> — "yes" when a compaction of the
# conversation finished at or after <epoch> (a main-thread compact_boundary entry), else
# "no". A session with no supervisor is typed `continue` only once this says yes.
ccy_transcript_compacted_since() {
    local file="$1" since="$2" answer
    if ! answer=$(jq -R -n -r --argjson since "$since" '
        [inputs | fromjson? | objects
         | select(.type == "system" and .subtype == "compact_boundary" and ((.isSidechain // false) | not))
         | .timestamp | strings | sub("\\.[0-9]+Z$"; "Z") | fromdateiso8601
         | select(. >= $since)] | if length > 0 then "yes" else "no" end' "$file" 2>&1); then
        print_error "the transcript $file could not be read: $answer"
        return 1
    fi
    printf '%s\n' "$answer"
}

# ccy_restore_going_state <going> <at> <detail> <now> <window> <took-input yes|no|-> — the
# <going> word ccy_restore_verdict takes, from one manifest entry. PURE.
#   none                          "-": not this restore's to set going
#   pending                       "setting-going"
#   untouched                     "failed:<detail>"
#   compacting                    "setting-going" while set-going waits to type continue
#                                 after the compaction; "failed:compact-not-started" if the
#                                 /compact was not taken within <window>
#   compact | continue            "started" once it took input; "setting-going" until
#                                 <window> seconds after <at>; then "failed:<what>-not-started"
ccy_restore_going_state() {
    local going="$1" at="$2" detail="$3" now="$4" window="$5" took="$6"
    case "$going" in
    none) printf -- '-\n' ;;
    pending) printf 'setting-going\n' ;;
    untouched) printf 'failed:%s\n' "${detail:-no-reason-recorded}" ;;
    compacting)
        if [[ "$took" != yes && $((now - at)) -ge "$window" ]]; then
            printf 'failed:compact-not-started\n'
        else
            printf 'setting-going\n'
        fi
        ;;
    compact | continue)
        if [[ "$took" == yes ]]; then
            printf 'started\n'
        elif [[ $((now - at)) -lt "$window" ]]; then
            printf 'setting-going\n'
        else
            printf 'failed:%s-not-started\n' "$going"
        fi
        ;;
    *) printf 'failed:unknown-going-%s\n' "$going" ;;
    esac
}

# ccy_restore_passphrase_check <file> — is this a usable SSH key passphrase file for a
# restore? On a headless server play-claude-yolo.yml writes the vault's github_ssh_passphrase
# to it, and names it to ccy-sessions-restore.service through a drop-in, so a restored ccy
# session can unlock its key with nobody at the keyboard. A regular file, owned by this user,
# readable by nobody else, not empty. Says why not on stderr and returns 1; a session that
# went ahead without it would stop at ssh-add's passphrase prompt with nobody to answer.
ccy_restore_passphrase_check() {
    local file="$1" owner mode
    local fix="Re-run play-claude-yolo.yml on this server (it writes the file from github_ssh_passphrase in host_vars)."
    if [[ ! -f "$file" ]]; then
        print_error "the SSH key passphrase file for session restore is missing: $file. $fix"
        return 1
    fi
    if ! owner=$(stat -c %u -- "$file") || ! mode=$(stat -c %a -- "$file"); then
        print_error "could not read the owner and mode of the session-restore passphrase file $file."
        return 1
    fi
    if [[ "$owner" != "$(id -u)" ]]; then
        print_error "the session-restore passphrase file $file is owned by uid $owner, not by you. $fix"
        return 1
    fi
    if [[ "$mode" != 600 && "$mode" != 400 ]]; then
        print_error "the session-restore passphrase file $file has mode $mode; it must be readable by you alone (600). $fix"
        return 1
    fi
    if [[ ! -r "$file" || ! -s "$file" ]]; then
        print_error "the session-restore passphrase file $file is unreadable or empty. $fix"
        return 1
    fi
}

# ccy_restore_passphrase_take <session-restore true|false> — the launcher's half. Moves
# CCY_RESTORE_SSH_PASSPHRASE_FILE out of the environment into RESTORE_SSH_PASSPHRASE_FILE, so
# nothing this launch starts inherits it. Only ccy-sessions restore sets it, and only beside
# CCY_SESSION_RESTORE=1: on any other launch it is refused, so an ordinary launch can never
# unlock a key through askpass. On a restore the file is checked before anything else runs.
RESTORE_SSH_PASSPHRASE_FILE=""
ccy_restore_passphrase_take() {
    local session_restore="$1"
    RESTORE_SSH_PASSPHRASE_FILE="${CCY_RESTORE_SSH_PASSPHRASE_FILE:-}"
    unset CCY_RESTORE_SSH_PASSPHRASE_FILE
    [[ -n "$RESTORE_SSH_PASSPHRASE_FILE" ]] || return 0
    if [[ "$session_restore" != true ]]; then
        print_error "CCY_RESTORE_SSH_PASSPHRASE_FILE is set on a launch that is not a session restore; only ccy-sessions restore sets it."
        RESTORE_SSH_PASSPHRASE_FILE=""
        return 1
    fi
    ccy_restore_passphrase_check "$RESTORE_SSH_PASSPHRASE_FILE"
}

# ccy_registry_restore [--dry-run] — start every recorded session that is not running.
#
# Per record: marked no-restore → skipped; its session name already live → skipped (a
# hand-started session is ordinary, and starting a second under the same name is impossible
# anyway); its directory gone → an ERROR, the record kept for the operator, and the run
# continues to the next record so one deleted checkout does not hold back every other
# session. The run's exit status is non-zero if anything failed. Nothing is started at all if
# the live set cannot be read: restoring on top of an unknown set could double every session.
#
# Each session is started with CCY_SESSION_RESTORE=1 on its command, which lets the launcher
# answer the prompts that have one safe answer, and a ccy record's compose outcome becomes
# --compose (ccy_registry_restore_args). The rest still ask, in the pane, and
# `ccy-sessions verify-restore` names them from the manifest written at the end. Each
# session started here is entered there as pending; `ccy-sessions set-going`, run by its own
# unit after this one, waits for it and types its first input. This returns as soon as the
# sessions exist, because a boot's user manager waits for it.
#
# On a headless server the unit's drop-in also sets CCY_RESTORE_SSH_PASSPHRASE_FILE. It is
# checked before anything starts, and a bad one starts nothing: every ccy session would only
# stop at a passphrase prompt. It is then dropped from the environment, so tmux never holds
# it. Each ccy session is given its path on its own command (never its contents); a cc
# session starts no container and loads no key, so it is not.
#
# Reports go to stderr, which under systemd is the journal. --dry-run prints the decisions
# and starts nothing, and writes no manifest.
ccy_registry_restore() {
    local dry_run=false
    if [[ "${1:-}" == "--dry-run" ]]; then
        dry_run=true
    elif [[ $# -gt 0 ]]; then
        print_error "ccy_registry_restore accepts only --dry-run, not '$1'."
        return 1
    fi
    local regdir listing name file failures=0 started=0 seen=false resume supervised
    local passphrase_file="${CCY_RESTORE_SSH_PASSPHRASE_FILE:-}"
    # Out of the environment before tmux runs: the first session started here starts ccy's
    # tmux server, whose global environment every later pane inherits, and an ordinary ccy
    # given the file refuses it and exits. Only the restored commands below get the path.
    unset CCY_RESTORE_SSH_PASSPHRASE_FILE
    local -A live=()
    local -a args=() manifest=() marker=()
    if [[ -n "$passphrase_file" ]] && ! ccy_restore_passphrase_check "$passphrase_file"; then
        print_error "nothing is restored: every ccy session would stop at its SSH key passphrase prompt."
        return 1
    fi
    regdir=$(ccy_registry_dir) || return 1
    # Only an ABSENT registry is an empty one. A path that is there but cannot be listed
    # would glob to nothing and read as "nothing to restore" on a boot that restored nothing.
    if [[ -e "$regdir" && ! (-d "$regdir" && -r "$regdir" && -x "$regdir") ]]; then
        print_error "the session registry $regdir is not a readable directory, so the sessions recorded there cannot be listed; nothing is restored."
        return 1
    fi
    if [[ ! -e "$regdir" ]]; then
        echo "ccy session registry: nothing to restore ($regdir does not exist)." >&2
        if [[ "$dry_run" == false ]]; then
            ccy_restore_manifest_locked _ccy_restore_manifest_merge || return 1
        fi
        return 0
    fi
    if ! declare -F ccy_tmux_start_detached >/dev/null; then
        print_error "ccy_tmux_start_detached is not defined: lib/tmux-session.bash must be sourced before restoring."
        return 1
    fi
    if ! listing=$(ccy_tmux_list); then
        print_error "the live session list could not be read, so nothing is restored: starting sessions on top of an unknown live set could double every one of them."
        return 1
    fi
    while read -r name _; do
        [[ -n "$name" ]] && live["$name"]=1
    done <<<"$listing"

    for file in "$regdir"/*; do
        [[ -e "$file" ]] || continue
        [[ "$file" == *.tmp.* ]] && continue
        seen=true
        if ! ccy_registry_read "$file"; then
            failures=$((failures + 1))
            continue
        fi
        if [[ "$REC_RESTORE" == "no" ]]; then
            echo "skip $REC_NAME: marked no-restore." >&2
            continue
        fi
        if ! mapfile -t args < <(ccy_registry_restore_args --compose "$REC_COMPOSE" "$REC_PREFIX" "${REC_ARGS[@]}") \
            || ! wait "$!"; then
            print_error "could not work out the arguments to restore $REC_NAME with (the record is kept at $file)."
            failures=$((failures + 1))
            continue
        fi
        resume="$(ccy_registry_resume_id "${args[@]}")"
        supervised="$(ccy_registry_supervised "$REC_PREFIX" "${args[@]}")"
        if [[ -n "${live[$REC_NAME]:-}" ]]; then
            echo "skip $REC_NAME: already running." >&2
            # Still one of the sessions that should be up, so still one to verify: a second
            # restore in the same boot must not hide a session stuck at a prompt since the first.
            # Not this restore's to set going: whoever started it is driving it. An entry an
            # earlier restore in this boot made for it is kept as it is ("keep"), so set-going
            # can finish with it.
            manifest+=("$REC_NAME" "$REC_PREFIX" "$REC_DIR" "$resume" "$supervised" keep "" "" "")
            continue
        fi
        if [[ ! -d "$REC_DIR" ]]; then
            print_error "cannot restore $REC_NAME: its directory no longer exists: $REC_DIR (the record is kept at $file; remove it if the project is gone)."
            failures=$((failures + 1))
            continue
        fi
        if [[ "$dry_run" == true ]]; then
            echo "would start $REC_NAME in $REC_DIR: $REC_LAUNCHER ${args[*]}" >&2
            continue
        fi
        marker=(CCY_SESSION_RESTORE=1)
        if [[ -n "$passphrase_file" && "$REC_PREFIX" == ccy ]]; then
            marker+=("CCY_RESTORE_SSH_PASSPHRASE_FILE=$passphrase_file")
        fi
        if ccy_tmux_start_detached "$REC_NAME" "$REC_DIR" env "${marker[@]}" "$REC_LAUNCHER" "${args[@]}"; then
            echo "restored $REC_NAME in $REC_DIR." >&2
            started=$((started + 1))
            manifest+=("$REC_NAME" "$REC_PREFIX" "$REC_DIR" "$resume" "$supervised" pending "" "" "")
        else
            print_error "could not start $REC_NAME in $REC_DIR (the record is kept at $file)."
            failures=$((failures + 1))
        fi
    done

    if [[ "$dry_run" == false ]]; then
        ccy_restore_manifest_locked _ccy_restore_manifest_merge "${manifest[@]}" || failures=$((failures + 1))
    fi
    if [[ "$seen" == false ]]; then
        echo "ccy session registry: nothing to restore (no records in $regdir)." >&2
        [[ "$failures" -eq 0 ]]
        return
    fi
    echo "ccy session restore: $started started, $failures failed." >&2
    [[ "$failures" -eq 0 ]]
}
