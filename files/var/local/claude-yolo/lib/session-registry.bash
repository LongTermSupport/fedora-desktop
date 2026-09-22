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
# written as two lines that read back as something else.
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
#   dropped, with value --update-token, --export-token, --connect, --prompt
#   kept, with value    --token, --ssh-key, --network, --engine
# A word that is not a flag is a first message to claude — stale on replay — unless it
# follows a flag ccy does not know, when it is that flag's value (`--model opus`).
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
        case "$arg" in
        --rebuild | --rebuild=* | --create-token | --update-token=* | --list-tokens | --custom | \
            --custom-docker | --top | --prevent | --debug | --headless | --disable-custom-docker | \
            --ssh-agent | --no-restore)
            value_slot=false
            ;;
        --update-token | --export-token | --connect | --prompt)
            drop_next=true
            value_slot=false
            ;;
        --token | --ssh-key | --network | --engine)
            keep_next=true
            value_slot=false
            printf '%s\n' "$arg"
            ;;
        --no-ssh | --github-443 | --no-network | --supervise | --no-supervise)
            value_slot=false
            printf '%s\n' "$arg"
            ;;
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
    done
}

# ccy_registry_write <name> <dir> <launcher> <prefix> <yes|no> [replay-args...] — write (or
# replace) the record for a session. Private to the user, written whole then moved into
# place, so a reader never sees half a record.
ccy_registry_write() {
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

# ccy_registry_remove <name> — delete a session's record. An absent record is not an error:
# the session may have been started before the registry existed.
ccy_registry_remove() {
    local name="${1:?ccy_registry_remove requires a session name}" regdir
    regdir=$(ccy_registry_dir) || return 1
    rm -f -- "$regdir/$name"
}

# ccy_registry_read <file> — parse a record into REC_NAME, REC_DIR, REC_LAUNCHER,
# REC_PREFIX, REC_RESTORE and the REC_ARGS array. Strict: a wrong header, an unknown key or a
# missing field is a rejection, never a guess — a guessed record starts the wrong thing in
# the wrong place.
ccy_registry_read() {
    local file="${1:?ccy_registry_read requires a record path}" line key value first=true
    REC_NAME="" REC_DIR="" REC_LAUNCHER="" REC_PREFIX="" REC_RESTORE=""
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
    printf '%s' "\"\$@\"; rc=\$?; rm -f -- ${quoted}; if [ \"\$rc\" -ne 0 ]; then printf '\\n${prefix} exited with status %s. Press Enter to close this session.\\n' \"\$rc\"; read -r; fi; exit \"\$rc\""
}

# ccy_registry_restore_args <prefix> [replay-args...] — the arguments a restore starts the
# launcher with, one per line: the recorded set, then `--supervise` for ccy unless the record
# already says --supervise or --no-supervise, then `--continue` unless the record already
# continues or resumes a conversation. cc forwards every argument to claude, and --supervise
# is ccy's flag, so cc gets --continue only.
ccy_registry_restore_args() {
    local prefix="${1:?ccy_registry_restore_args requires a prefix}"
    shift
    local arg has_supervise=false has_continue=false
    for arg in "$@"; do
        case "$arg" in
        --supervise | --no-supervise) has_supervise=true ;;
        --continue | -c | --resume | -r) has_continue=true ;;
        esac
    done
    if [[ $# -gt 0 ]]; then
        printf '%s\n' "$@"
    fi
    if [[ "$prefix" == "ccy" && "$has_supervise" == false ]]; then
        printf '%s\n' "--supervise"
    fi
    if [[ "$has_continue" == false ]]; then
        printf '%s\n' "--continue"
    fi
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
# Reports go to stderr, which under systemd is the journal. --dry-run prints the decisions
# and starts nothing.
ccy_registry_restore() {
    local dry_run=false
    if [[ "${1:-}" == "--dry-run" ]]; then
        dry_run=true
    elif [[ $# -gt 0 ]]; then
        print_error "ccy_registry_restore accepts only --dry-run, not '$1'."
        return 1
    fi
    local regdir listing name file failures=0 started=0 seen=false
    local -A live=()
    local -a args=()
    regdir=$(ccy_registry_dir) || return 1
    if [[ ! -d "$regdir" ]]; then
        echo "ccy session registry: nothing to restore ($regdir does not exist)." >&2
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
        if [[ -n "${live[$REC_NAME]:-}" ]]; then
            echo "skip $REC_NAME: already running." >&2
            continue
        fi
        if [[ ! -d "$REC_DIR" ]]; then
            print_error "cannot restore $REC_NAME: its directory no longer exists: $REC_DIR (the record is kept at $file; remove it if the project is gone)."
            failures=$((failures + 1))
            continue
        fi
        mapfile -t args < <(ccy_registry_restore_args "$REC_PREFIX" "${REC_ARGS[@]}")
        if [[ "$dry_run" == true ]]; then
            echo "would start $REC_NAME in $REC_DIR: $REC_LAUNCHER ${args[*]}" >&2
            continue
        fi
        if ccy_tmux_start_detached "$REC_NAME" "$REC_DIR" "$REC_LAUNCHER" "${args[@]}"; then
            echo "restored $REC_NAME in $REC_DIR." >&2
            started=$((started + 1))
        else
            print_error "could not start $REC_NAME in $REC_DIR (the record is kept at $file)."
            failures=$((failures + 1))
        fi
    done

    if [[ "$seen" == false ]]; then
        echo "ccy session registry: nothing to restore (no records in $regdir)." >&2
        return 0
    fi
    echo "ccy session restore: $started started, $failures failed." >&2
    [[ "$failures" -eq 0 ]]
}
