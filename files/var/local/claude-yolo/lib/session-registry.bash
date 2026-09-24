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
#   dropped, with value --update-token, --export-token, --connect, --disconnect, --prompt
#   kept, with value    --token, --ssh-key, --network, --engine
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
CCY_REGISTRY_DROP_VALUE_FLAGS=(--update-token --export-token --connect --disconnect --prompt)
CCY_REGISTRY_KEEP_VALUE_FLAGS=(--token --ssh-key --network --engine)
CCY_REGISTRY_KEEP_FLAGS=(--no-ssh --github-443 --no-network --supervise --no-supervise)

# ccy_registry_flag_class <word> — drop, drop-value, keep-value, keep, or unknown.
ccy_registry_flag_class() {
    local word="${1?ccy_registry_flag_class requires a word}"
    case "$word" in
    --rebuild=* | --update-token=*)
        printf 'drop\n'
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
        if ! ccy_registry_write "$REC_NAME" "$REC_DIR" "$REC_LAUNCHER" "$REC_PREFIX" "$REC_RESTORE" "${kept[@]}"; then
            failures=$((failures + 1))
            continue
        fi
        printf 'Removed --network %s from the restore record of session %s.\n' "$network" "$REC_NAME"
    done
    [[ "$failures" -eq 0 ]]
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

# ccy_registry_restore_args <prefix> [replay-args...] — the arguments a restore starts the
# launcher with, one per line: the recorded set, with `--supervise` for ccy unless the record
# already says --supervise or --no-supervise, then `--continue` unless the record already
# continues or resumes a conversation. cc forwards every argument to claude, and --supervise
# is ccy's flag, so cc gets --continue only.
#
# After a recorded `--` every word is claude's, so --supervise goes in before it, and a
# --supervise after it is claude's word rather than ccy's. --continue is claude's flag and
# reaches claude from either side, so it is appended.
ccy_registry_restore_args() {
    local prefix="${1:?ccy_registry_restore_args requires a prefix}"
    shift
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
    local add_supervise=false
    [[ "$prefix" == "ccy" && "$has_supervise" == false ]] && add_supervise=true
    after_dd=false
    for arg in "$@"; do
        if [[ "$arg" == "--" && "$after_dd" == false ]]; then
            after_dd=true
            if [[ "$add_supervise" == true ]]; then
                printf '%s\n' "--supervise"
                add_supervise=false
            fi
        fi
        printf '%s\n' "$arg"
    done
    if [[ "$add_supervise" == true ]]; then
        printf '%s\n' "--supervise"
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
CCY_RESTORE_MANIFEST_HEADER="ccy-restore-manifest 1"

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

# ccy_restore_manifest_write [<name> <prefix> <dir>]... — record the sessions a restore
# started, stamped with this boot's id. Written whole then moved into place, like a record.
ccy_restore_manifest_write() {
    local path boot tmp
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
            while [[ $# -ge 3 ]]; do
                printf 'name=%s\nprefix=%s\ndir=%s\n' "$1" "$2" "$3"
                shift 3
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
# RM_NAMES, RM_PREFIXES and RM_DIRS. Strict, like ccy_registry_read: each entry is a name=,
# prefix=, dir= triple in that order, and anything else is a rejection. A missing manifest
# is a rejection too, with its own message: no restore has run.
ccy_restore_manifest_read() {
    local path line key value first=true expect=name
    RM_BOOT=""
    RM_NAMES=() RM_PREFIXES=() RM_DIRS=()
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
        if [[ "$key" == boot && -z "$RM_BOOT" && "$expect" == name && "${#RM_NAMES[@]}" -eq 0 ]]; then
            RM_BOOT="$value"
            continue
        fi
        if [[ "$key" != "$expect" ]]; then
            print_error "the restore manifest $path has '$key' where '$expect' was expected and is not read."
            return 1
        fi
        case "$key" in
        name) RM_NAMES+=("$value") expect=prefix ;;
        prefix) RM_PREFIXES+=("$value") expect=dir ;;
        dir) RM_DIRS+=("$value") expect=name ;;
        esac
    done <"$path"
    if [[ "$first" == true ]]; then
        print_error "the restore manifest $path is empty."
        return 1
    fi
    if [[ -z "$RM_BOOT" ]]; then
        print_error "the restore manifest $path names no boot."
        return 1
    fi
    if [[ "$expect" != name ]]; then
        print_error "the restore manifest $path ends part-way through an entry."
        return 1
    fi
}

# ccy_restore_verdict <prefix> <live 0|1> <screen> <container> — one restored session's
# state, printed as "<STATE>[ <detail>]". PURE: every probe arrives as text, so every
# shape is testable (scripts/test-ccy-session-registry.bash).
#   <screen>     the pane's visible text (tmux capture-pane -p)
#   <container>  for ccy: "up" when the session's container is running, "starting" when
#                its engine client exists and the container is not yet listed, "-" when
#                there is no engine client; ignored for cc, which runs claude on the host
#
# The screen is judged by its LAST non-blank line, where a prompt waiting for input sits:
#   DEAD launcher-exited            the trampoline's hold line: the launcher has returned
#   WAITING-AT-PROMPT <name>        a prompt from ccy_known_prompts
#   STARTING                        a ccy session with no running container yet
#   OK                              anything else, with the container up for ccy
# A session that is not live at all is "DEAD session-not-running".
ccy_restore_verdict() {
    local prefix="$1" live="$2" screen="$3" container="$4"
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
    cc)
        printf 'OK\n'
        ;;
    ccy)
        if [[ "$container" == up ]]; then
            printf 'OK\n'
        else
            printf 'STARTING\n'
        fi
        ;;
    *)
        printf 'DEAD unknown-launcher-%s\n' "$prefix"
        ;;
    esac
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
# answer the prompts that have one safe answer. The rest still ask, in the pane, and
# `ccy-sessions verify-restore` names them from the manifest written at the end.
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
    local regdir listing name file failures=0 started=0 seen=false
    local -A live=()
    local -a args=() manifest=()
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
            ccy_restore_manifest_write "${manifest[@]}" || return 1
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
        if [[ -n "${live[$REC_NAME]:-}" ]]; then
            echo "skip $REC_NAME: already running." >&2
            # Still one of the sessions that should be up, so still one to verify: a second
            # restore in the same boot must not hide a session stuck at a prompt since the first.
            manifest+=("$REC_NAME" "$REC_PREFIX" "$REC_DIR")
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
        if ccy_tmux_start_detached "$REC_NAME" "$REC_DIR" env CCY_SESSION_RESTORE=1 "$REC_LAUNCHER" "${args[@]}"; then
            echo "restored $REC_NAME in $REC_DIR." >&2
            started=$((started + 1))
            manifest+=("$REC_NAME" "$REC_PREFIX" "$REC_DIR")
        else
            print_error "could not start $REC_NAME in $REC_DIR (the record is kept at $file)."
            failures=$((failures + 1))
        fi
    done

    if [[ "$dry_run" == false ]]; then
        ccy_restore_manifest_write "${manifest[@]}" || failures=$((failures + 1))
    fi
    if [[ "$seen" == false ]]; then
        echo "ccy session registry: nothing to restore (no records in $regdir)." >&2
        [[ "$failures" -eq 0 ]]
        return
    fi
    echo "ccy session restore: $started started, $failures failed." >&2
    [[ "$failures" -eq 0 ]]
}
