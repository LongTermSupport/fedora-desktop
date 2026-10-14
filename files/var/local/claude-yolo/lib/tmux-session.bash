#!/bin/bash
# CCY terminal-death insulation: an interactive session runs inside tmux, on CCY's own
# server, so losing the terminal emulator detaches the session instead of destroying it.
#
# WHY: `podman run -it` allocates the session's pty inside whatever terminal tab ran ccy. When
# the emulator dies — a Wayland protocol error, a GPU reset, an upgrade restarting it, a
# window closed by accident — every tab's pty goes with it, the podman client gets SIGHUP and
# runs its --rm teardown, and with a single-process emulator every session dies at once.
# Container survival does not help: a container whose claude has no pty cannot be reached.
#
# HOW: before the first prompt, the launcher re-executes itself inside a tmux session. The
# tmux SERVER then owns the pty and the emulator only holds a tmux CLIENT, which is
# disposable. Three details make the guarantee unconditional:
#   - a dedicated socket (`tmux -L ccy`), so the server is always one this code started and
#     never one a user happened to start from a plain tab;
#   - the client that creates that server runs under `systemd-run --user --scope`, so the
#     server's cgroup is a ccy-tmux-*.scope under `systemd --user`, not the tab's scope;
#   - a server-wide client-attached hook that detaches any client attaching to a session
#     that already has one. A session is on ONE terminal or none; two terminals mirroring
#     the same claude cannot happen, whoever races whom.
#
# Session names mirror container names: ccy-<project>, then ccy-<project>-2, and so on.
# Running ccy in a project that has a detached session — the one a dead terminal left
# behind — offers to re-attach it; a session open in another terminal is never offered
# there. ccy_tmux_take_over (with ccy_tmux_client_words and ccy_tmux_idle_words) can take one
# over, detaching the other terminal, but nothing calls them now: the Go ccy-sessions does its
# own take-over. They stay until the owner decides when dead bash goes (decision D8 in
# ASSESS-ccy-launcher.md of the Go tooling plan).
#
# Requires print_error (common-pure.bash, always loaded first) and lib/session-registry.bash:
# every session started here is recorded so a reboot can be undone, and the record's removal
# rides in the pane's trampoline. The interactive parts follow CLAUDE/InteractiveScripts.md:
# strict validation, bounded re-prompt, EOF is a clean exit.

CCY_TMUX_SOCKET="ccy"
# The launcher this library is serving, which names its sessions: ccy-<project> for the
# container launcher, cc-<project> for the host wrapper (cc sets this before calling).
# Both live on the one server, so ccy-sessions shows them side by side.
: "${CCY_TMUX_SESSION_PREFIX:=ccy}"

# The same tmux invocation everywhere, so no call can land on the default server by mistake.
ccy_tmux() {
    tmux -L "$CCY_TMUX_SOCKET" "$@"
}

# ccy_tmux_list — every session on CCY's server as "<name> <attached-count> <directory>",
# one per line. No server yet is the normal first-run state and prints nothing; any other
# failure is real.
ccy_tmux_list() {
    local listing
    if listing=$(ccy_tmux list-sessions -F '#{session_name} #{session_attached} #{session_path}' 2>&1); then
        printf '%s\n' "$listing"
        return 0
    fi
    if [[ "$listing" == *"no server running"* ]] || [[ "$listing" == *"No such file or directory"* ]]; then
        return 0
    fi
    print_error "tmux list-sessions failed: $listing"
    return 1
}

# ccy_tmux_project_sessions — the sessions started from THIS directory. Matched on the
# directory, not the session name: two checkouts of one repo share a project name, and
# project "app" would otherwise claim "ccy-app-2", which is project "app-2"'s first session.
# A listing failure is propagated, never read as "no sessions".
ccy_tmux_project_sessions() {
    local listing name attached dir
    listing=$(ccy_tmux_list) || return 1
    while read -r name attached dir; do
        [[ -n "$name" ]] || continue
        if [[ "$dir" == "$PWD" && "$name" == "${CCY_TMUX_SESSION_PREFIX}-"* ]]; then
            printf '%s %s %s\n' "$name" "$attached" "$dir"
        fi
    done <<<"$listing"
}

# ccy_tmux_next_name <project> — the first free session name for a project.
ccy_tmux_next_name() {
    local base="${CCY_TMUX_SESSION_PREFIX}-$1" listing n=1 candidate
    listing=$(ccy_tmux_list) || return 1
    candidate="$base"
    while awk -v want="$candidate" '$1 == want { found = 1 } END { exit found ? 0 : 1 }' <<<"$listing"; do
        n=$((n + 1))
        candidate="${base}-${n}"
    done
    printf '%s\n' "$candidate"
}

# ccy_tmux_is_detached <name> — true only if the session exists and no client is on it, as
# list-clients counts them: the single-attach hook's own view. #{session_attached} is not
# used here because it drops a client that has been told to leave but has not yet gone,
# which the hook still counts — attaching in that gap is kicked straight back off.
# A tmux failure is a failure (return 2 after the error), not "gone".
ccy_tmux_is_detached() {
    local clients rc=0
    clients=$(_ccy_tmux_clients "$1") || rc=$?
    if [[ "$rc" -eq 2 ]]; then
        return 1
    elif [[ "$rc" -ne 0 ]]; then
        return 2
    fi
    [[ -z "$clients" ]]
}

# The hook that enforces one terminal per session. It runs on the server for every attach;
# if the session then has more than one client, the client that just arrived is detached.
# tmux's own #{session_attached} is not reliable inside this hook, hence list-clients.
ccy_tmux_single_attach_hook() {
    printf "if-shell 'test \$(tmux -L %s list-clients -t \"#{session_name}\" | wc -l) -gt 1' 'detach-client'" \
        "$CCY_TMUX_SOCKET"
}

# ccy_tmux_attach <name> — attach the current terminal to a detached session and, when the
# user detaches again, say what state they left it in. Refuses (return 2) a session that has
# a client at the moment of asking; the hook covers the race after that. Does not return to
# a launcher: attaching IS the session, so the caller exits afterwards. Returns 3 when
# another terminal is on the session afterwards: the hook detached this one on arrival.
ccy_tmux_attach() {
    local name="${1:?ccy_tmux_attach requires a session name}" clients rc=0
    if ! ccy_tmux_is_detached "$name"; then
        print_error "'$name' is open in another terminal, or gone. It can be attached from one terminal only."
        return 2
    fi
    ccy_tmux set-hook -g client-attached "$(ccy_tmux_single_attach_hook)" || return 1
    echo "Attaching to '$name'." >&2
    if ! ccy_tmux attach-session -t "=$name"; then
        print_error "could not attach to '$name'"
        return 1
    fi
    clients=$(_ccy_tmux_clients "$name") || rc=$?
    if [[ "$rc" -eq 2 ]]; then
        echo "'$name' has ended." >&2
        return 0
    elif [[ "$rc" -ne 0 ]]; then
        return 1
    fi
    if [[ -n "$clients" ]]; then
        print_error "'$name' is open in another terminal, so this terminal was not attached to it."
        return 3
    fi
    echo "Detached. '$name' keeps running; run ccy in its project directory to return to it." >&2
    return 0
}

# ── taking a session over from another terminal ──────────────────────────────────────────
#
# A terminal that dropped without letting go — an SSH connection that died while its tmux
# client stayed connected — holds its session, and the one-terminal rule then refuses it to
# every other terminal. Taking over detaches that other terminal and attaches this one. Only
# the client is detached: the session and everything in it keep running.

# How long a detached terminal is given to let go, in tenths of a second. tmux detaches a
# client by telling it to leave, so the session shows no client only once it has.
: "${CCY_TMUX_TAKE_OVER_WAIT_TENTHS:=50}"

# ccy_tmux_idle_words <seconds> — "idle 40 s", "idle 12 min", "idle 3 h", "idle 2 d",
# rounded down. PURE. A negative age (the clock moved back) reads as 0.
ccy_tmux_idle_words() {
    local s="$1"
    if [[ "$s" -lt 0 ]]; then
        s=0
    fi
    if [[ "$s" -lt 60 ]]; then
        printf 'idle %s s' "$s"
    elif [[ "$s" -lt 3600 ]]; then
        printf 'idle %s min' "$((s / 60))"
    elif [[ "$s" -lt 86400 ]]; then
        printf 'idle %s h' "$((s / 3600))"
    else
        printf 'idle %s d' "$((s / 86400))"
    fi
}

# ccy_tmux_client_words <clients> <now> — "<tty> (idle …), <tty> (idle …)". PURE.
#   <clients> "<tty> <last-activity-epoch>" per line, as _ccy_tmux_clients asks tmux
ccy_tmux_client_words() {
    local clients="$1" now="$2" tty activity out=""
    while read -r tty activity; do
        [[ -n "$tty" ]] || continue
        out+="${out:+, }${tty} ($(ccy_tmux_idle_words "$((now - activity))"))"
    done <<<"$clients"
    printf '%s' "$out"
}

# _ccy_tmux_clients <name> — the "<tty> <last-activity-epoch>" lines for every client on a
# session, as list-clients — and therefore the single-attach hook — sees them. Returns 2,
# silently, when the session (or the whole server) is gone; 1 after any other error.
_ccy_tmux_clients() {
    local out
    if ! out=$(ccy_tmux list-clients -t "=$1" -F '#{client_tty} #{client_activity}' 2>&1); then
        if [[ "$out" == *"can't find session"* || "$out" == *"no server running"* ||
            "$out" == *"No such file or directory"* ]]; then
            return 2
        fi
        print_error "could not list the terminals on '$1': $out"
        return 1
    fi
    printf '%s\n' "$out"
}

# ccy_tmux_take_over <name> — detach every other terminal from the session, wait until
# list-clients shows none (the hook's view; see ccy_tmux_is_detached), and attach this one
# through ccy_tmux_attach. Returns 2 when the session has ended, or was taken again just
# before the attach; 1 when tmux refuses the detach or the other terminal does not let go in
# time; 3 when the attach was kicked off again (ccy_tmux_attach). Nothing is attached on 1
# or 2. Never kills the session or anything in it.
ccy_tmux_take_over() {
    local name="${1:?ccy_tmux_take_over requires a session name}" clients words out rc=0 waited=0 detach_failed=false
    clients=$(_ccy_tmux_clients "$name") || rc=$?
    if [[ "$rc" -eq 2 ]]; then
        print_error "'$name' has ended; there is nothing to take over."
        return 2
    elif [[ "$rc" -ne 0 ]]; then
        return 1
    fi
    if [[ -n "$clients" ]]; then
        words=$(ccy_tmux_client_words "$clients" "$EPOCHSECONDS")
        if out=$(ccy_tmux detach-client -s "=$name" 2>&1); then
            echo "Detached '$name' from ${words}; the session keeps running." >&2
        else
            # The other terminal may have left between the listing and the detach, and tmux
            # then fails the detach for want of a client. That is the outcome wanted, so it
            # is only a failure if a client is still there.
            detach_failed=true
            echo "The detach from ${words} failed (${out}); checking whether '$name' is free anyway." >&2
        fi
        while :; do
            rc=0
            clients=$(_ccy_tmux_clients "$name") || rc=$?
            if [[ "$rc" -eq 2 ]]; then
                print_error "'$name' ended while it was being taken over; nothing was attached."
                return 2
            elif [[ "$rc" -ne 0 ]]; then
                return 1
            fi
            [[ -n "$clients" ]] || break
            if [[ "$detach_failed" == true ]]; then
                print_error "could not detach '$name' from ${words}: $out"
                return 1
            fi
            if [[ "$waited" -ge "$CCY_TMUX_TAKE_OVER_WAIT_TENTHS" ]]; then
                print_error "'$name' was told to leave ${words}, but it is still open there after $((CCY_TMUX_TAKE_OVER_WAIT_TENTHS / 10)) s. Nothing was attached; the session keeps running."
                return 1
            fi
            sleep 0.1
            waited=$((waited + 1))
        done
    fi
    ccy_tmux_attach "$name"
}

# ccy_tmux_teams_arg [args...] — the value of the launcher's --teams (`--teams <list>` or
# `--teams=<list>`, before any `--`), printed; nothing when there is none. PURE.
ccy_tmux_teams_arg() {
    local arg next=false
    for arg in "$@"; do
        if [[ "$next" == true ]]; then
            printf '%s\n' "$arg"
            return 0
        fi
        case "$arg" in
        --) return 0 ;;
        --teams) next=true ;;
        --teams=*)
            printf '%s\n' "${arg#--teams=}"
            return 0
            ;;
        esac
    done
}

# ccy_tmux_seats_shared <wanted> <held> — true when the two canonical lists
# (`<seat>@<team>,...`) name a seat in common. PURE.
ccy_tmux_seats_shared() {
    local item
    local -a wanted=()
    IFS=, read -r -a wanted <<<"$1"
    for item in "${wanted[@]}"; do
        [[ ",$2," == *",$item,"* ]] && return 0
    done
    return 1
}

# ccy_tmux_session_seats <name> — the --teams list a session was started with, read from its
# registry record; nothing for a session with no record or no --teams. A record that cannot
# be read is an error (said, return 1), never "no seats".
ccy_tmux_session_seats() {
    local regdir
    regdir=$(ccy_registry_dir) || return 1
    [[ -e "$regdir/$1" ]] || return 0
    (
        ccy_registry_read "$regdir/$1" || exit 1
        ccy_tmux_teams_arg "${REC_ARGS[@]}"
    )
}

# ccy_tmux_offer <project> [seats] — when the project has detached sessions, ask what to do.
# Prints exactly one line on stdout: "attach <name>", "create", or "quit". Prompts go to
# stderr. It is the same picker as ccy-sessions — one look for the whole human layer — shown
# only when there is something to offer. Esc is "quit" (InteractiveScripts rule 3).
#
# [seats] is the launch's canonical --teams list (Plan 00177 Task 4.6). With it, only the
# sessions that hold one of those seats are offered, each with the seats it holds, so Enter
# never drops --teams into a session that is in other seats or none; when there are detached
# sessions but none holds them, one line says a new session is starting and why.
ccy_tmux_offer() {
    local project="$1" want="${2:-}" listing name attached dir rows="" in_use="" seats skipped=0
    listing=$(ccy_tmux_project_sessions "$project") || return 1
    while read -r name attached dir; do
        [[ -n "$name" ]] || continue
        seats=""
        if [[ -n "$want" ]]; then
            seats=$(ccy_tmux_session_seats "$name") || return 1
            if ! ccy_tmux_seats_shared "$want" "$seats"; then
                [[ "$attached" == "0" ]] && skipped=$((skipped + 1))
                continue
            fi
        fi
        if [[ "$attached" == "0" ]]; then
            rows+="$(ccy_tmux_row "$name" "$attached" "$dir" ${seats:+"seats $seats"})"$'\n'
        else
            in_use+="${in_use:+, }$name"
        fi
    done <<<"$listing"

    if [[ -z "$rows" ]]; then
        if [[ -n "$in_use" ]]; then
            echo "Open in other terminals: ${in_use} (ccy-sessions, Ctrl-T, takes one over). Starting a new session." >&2
        elif [[ "$skipped" -gt 0 ]]; then
            echo "No detached session here holds ${want} (${skipped} detached in other seats or none), so a new session is starting in it." >&2
        fi
        printf 'create\n'
        return 0
    fi

    local header picked key row
    header="$(ccy_tmux_header "Enter attach   Ctrl-N new session   Esc or q quit" \
        "Detached sessions started from ${PWD/#${HOME}/\~}${want:+ that hold a seat of ${want}}" \
        "${in_use:+Open in other terminals: ${in_use} — ccy-sessions, Ctrl-T, takes one over}")"
    if ! picked="$(ccy_tmux_pick "${CCY_TMUX_SESSION_PREFIX}: a session is detached here" "$header" "ctrl-n" <<<"${rows%$'\n'}")"; then
        echo "Nothing started." >&2
        printf 'quit\n'
        return 0
    fi
    key="${picked%%$'\n'*}"
    row="${picked#*$'\n'}"
    if [[ "$key" == "ctrl-n" ]]; then
        printf 'create\n'
        return 0
    fi
    printf 'attach %s\n' "${row%% *}"
}

# ── the shared picker: one look for ccy, cc and ccy-sessions ─────────────────────────────

# ccy_tmux_row <name> <attached> <dir> [extra] — one aligned picker row. The state words are
# what the pickers test for, so they are defined once here. [extra] adds a column before the
# directory (the seats a session holds); a column not given is left out altogether rather
# than padded.
ccy_tmux_row() {
    local state="detached"
    if [[ "$2" != "0" ]]; then
        state="open elsewhere"
    fi
    if [[ -n "${4:-}" ]]; then
        printf '%-28s  %-15s  %-22s  %s' "$1" "$state" "$4" "${3/#${HOME}/\~}"
        return 0
    fi
    printf '%-28s  %-15s  %s' "$1" "$state" "${3/#${HOME}/\~}"
}

# ccy_tmux_header <keys line> [more lines...] — the picker header: the key legend first,
# then any context lines; empty ones are dropped.
ccy_tmux_header() {
    local line out="↑↓ choose   $1"
    shift
    for line in "$@"; do
        [[ -n "$line" ]] && out+=$'\n'"$line"
    done
    printf '%s' "$out"
}

# ccy_tmux_pick <title> <header> <expect-keys> [cursor] — run fzf over the rows on stdin
# with the house style: a bordered, padded box with the title on its frame, the legend
# above the rows, one keystroke per action (q quits as well as Esc, so nothing needs
# Enter after it), and an "Exit" row always present as the last row so leaving is a
# visible choice, not only a key. [cursor]="last" starts on that Exit row — the safe
# default for a yes/no. Prints "<key>\n<row>" (key empty for Enter); non-zero on Esc, q
# or Exit.
ccy_tmux_pick() {
    if [[ -z "$(command -v fzf)" ]]; then
        print_error "fzf is not installed; playbooks/imports/play-claude-yolo.yml installs it."
        return 1
    fi
    # fzf refuses an empty --expect ("key names required"), so the flag is only passed when
    # there are keys; the "<key>\n<row>" shape is kept either way.
    local -a opts=()
    if [[ -n "$3" ]]; then
        opts+=(--expect="$3")
    fi
    if [[ "${4:-}" == "last" ]]; then
        opts+=(--bind='start:last')
    fi
    local rows out
    rows=$(cat -)
    # --ansi: a row or the header may carry colour; fzf hands the chosen row back as plain
    # text, so a caller matches it against its rows without their colour. --color: the
    # terminal's own colours (16 base, -1 its default), with one accent, amber (3), for what
    # is active; the same accent as ccy-teams' questions.
    out=$(printf '%s\n%s\n' "${rows%$'\n'}" "$CCY_TMUX_EXIT_ROW" | fzf --ansi --height=~70% --layout=reverse --no-multi --no-sort --no-info \
        --color='16,fg:-1,bg:-1,fg+:-1:bold,bg+:-1,hl:3,hl+:3:bold,gutter:-1,pointer:3,marker:3,prompt:3,spinner:3,info:-1,header:-1,border:-1,label:3:bold' \
        --border=rounded --border-label=" $1 " --border-label-pos=3 \
        --margin=1,2 --padding=1,2 --header-first --pointer='▶' \
        --prompt="filter > " --header="$2"$'\n' --bind='q:abort' "${opts[@]}") || return $?
    if [[ -z "$3" ]]; then
        out=$'\n'"$out"
    fi
    if [[ "${out#*$'\n'}" == "$CCY_TMUX_EXIT_ROW" ]]; then
        return 1
    fi
    printf '%s\n' "$out"
}
CCY_TMUX_EXIT_ROW="Exit"

# ccy_tmux_insulate <project> <command> [args...] — the launcher's entry point.
#
# Returns 0, doing nothing, only when insulation does not apply: already inside a tmux
# session (whichever server — the user is protected either way), or no terminal on
# stdin/stdout. Returns 1 when a required tool is missing. Otherwise it never returns to the
# caller: it attaches, or exec's tmux to run <command>, or exits 0 on "quit".
#
# A command that fails inside the session would take its error message with it when the
# session closed, so a non-zero exit holds the window open until Enter is pressed.
ccy_tmux_insulate() {
    local project="${1:?ccy_tmux_insulate requires a project name}"
    shift
    [[ $# -gt 0 ]] || {
        print_error "ccy_tmux_insulate requires a command to run"
        return 1
    }

    if [[ -n "${TMUX:-}" ]]; then
        # Inside tmux already. On the ccy server that is the re-exec landing: nothing to
        # do. On another server, what matters is where THAT server lives: one forked from
        # a terminal tab sits in the tab's *-spawn-*.scope and dies with the tab — exactly
        # the exposure this exists to remove — so wrapping is refused with the way out.
        # Nesting tmux inside it would only hide the problem.
        local socket="${TMUX%%,*}" server_pid server_cgroup
        if [[ "$(basename "$socket")" == "$CCY_TMUX_SOCKET" ]]; then
            return 0
        fi
        server_pid="${TMUX#*,}"
        server_pid="${server_pid%%,*}"
        if ! server_cgroup=$(<"/proc/${server_pid}/cgroup"); then
            print_error "cannot read the cgroup of this tmux server (pid ${server_pid})"
            return 1
        fi
        if [[ "$server_cgroup" == *-spawn-*.scope* ]]; then
            print_error "this tmux server was started from a terminal tab and dies with it, so a session here would not survive the terminal."
            echo "Detach (F12 then Detach, or Ctrl-b d) and run ccy from the plain shell: it starts a session on its own protected server." >&2
            return 1
        fi
        echo "Already inside a tmux server outside any terminal's scope; not wrapping again." >&2
        return 0
    fi
    if [[ ! -t 0 ]]; then
        return 0
    fi
    if [[ ! -t 1 ]]; then
        echo "stdout is not a terminal (for example under --debug), so this session is NOT insulated from its terminal." >&2
        return 0
    fi
    local tool
    for tool in tmux systemd-run; do
        if [[ -z "$(command -v "$tool")" ]]; then
            print_error "$tool is not installed, so this session could not be insulated from its terminal."
            echo "Deploy it with playbooks/imports/play-tmux-sessions.yml (part of playbook-main.yml)." >&2
            return 1
        fi
    done

    # A launch with --teams is offered only the sessions in its seats (ccy_tmux_offer). By here
    # the launcher has replaced the list with its canonical form, every seat named.
    local decision action name teams
    teams=$(ccy_tmux_teams_arg "${@:2}")
    decision=$(ccy_tmux_offer "$project" "$teams") || return 1
    read -r action name <<<"$decision"
    case "$action" in
    quit)
        exit 0
        ;;
    attach)
        # A refusal here means the session was taken between the offer and the answer.
        # Not a retry loop: the honest move is to show the offer again from the top. Return 3
        # (another terminal holds it after the attach) has already been reported, and
        # the user may well have worked in it first, so it is not shown the offer again.
        local attach_rc=0
        ccy_tmux_attach "$name" || attach_rc=$?
        if [[ "$attach_rc" -eq 0 || "$attach_rc" -eq 3 ]]; then
            exit 0
        fi
        exec "$@"
        ;;
    esac

    name=$(ccy_tmux_next_name "$project") || return 1
    echo "Starting session '$name' under tmux. If this terminal dies, run ${CCY_TMUX_SESSION_PREFIX} here again to re-attach." >&2

    # THE registry write that creates a record: this is the single place a session is
    # created from a terminal, so the record cannot drift from the session. (The launcher
    # later adds a chosen --ssh-key to the same record through lib/session-registry.bash.) `--no-restore` is the
    # registry's opt-out and never reaches the launcher; what is recorded for replay is the
    # launch argv with the one-shot arguments removed (see lib/session-registry.bash).
    local launcher="$1" restore=yes
    shift
    local -a launch=() replay=()
    ccy_registry_wants_restore "$@" || restore=no
    mapfile -t launch < <(ccy_registry_launch_args "$@")
    mapfile -t replay < <(ccy_registry_replay_args "$CCY_TMUX_SESSION_PREFIX" "${launch[@]}")
    ccy_registry_write "$name" "$PWD" "$launcher" "$CCY_TMUX_SESSION_PREFIX" "$restore" "${replay[@]}" || return 1

    # A record with no session behind it would be restored at the next boot as a session
    # that was never running, so a failed start takes its record with it.
    if ! ccy_tmux_start_detached "$name" "$PWD" "$launcher" "${launch[@]}"; then
        ccy_registry_remove "$name"
        return 1
    fi
    # Attaching IS the session from here: this client is the terminal's, and disposable.
    exec tmux -L "$CCY_TMUX_SOCKET" attach-session -t "=$name"
}

# ccy_tmux_start_detached <name> <dir> <launcher> [args...] — create a session on CCY's
# server running <launcher> in <dir>, detached, and return. Shared by the interactive start
# above and by the boot-time restore (ccy-sessions restore), so both produce the same
# session: same scope, same hook, same trampoline, and therefore the same record lifecycle.
#
# The scope keeps the server out of the caller's cgroup — a terminal tab's, or the restore
# unit's, which would otherwise take the server with it when the oneshot ended. --collect
# lets systemd forget the scope once empty, whatever its exit status. The session is created
# detached and the single-attach hook installed in one server round trip, so no client can
# reach the session before the hook exists. The pane runs the registry's trampoline, which
# removes the session's record when <launcher> returns and holds the window on a failure.
ccy_tmux_start_detached() {
    local name="${1:?ccy_tmux_start_detached requires a session name}"
    local dir="${2:?ccy_tmux_start_detached requires a directory}"
    local launcher="${3:?ccy_tmux_start_detached requires a launcher}"
    shift 3
    local tool regdir trampoline
    if [[ -z "$(command -v tmux)" ]]; then
        print_error "tmux is not installed, so session '$name' cannot be started. Deploy it with playbooks/imports/play-tmux-sessions.yml (part of playbook-main.yml)."
        return 1
    fi
    for tool in systemd-run systemd-escape; do
        if [[ -z "$(command -v "$tool")" ]]; then
            print_error "$tool is not available, so session '$name' cannot be started. It ships with systemd, which this host is expected to run; a user manager is what keeps the session out of the terminal's cgroup."
            return 1
        fi
    done
    regdir=$(ccy_registry_dir) || return 1
    trampoline=$(ccy_registry_trampoline "$regdir/$name" "$CCY_TMUX_SESSION_PREFIX")
    # The unit name carries the session name (escaped) as well as this pid: a restore starts
    # several sessions from one process, and two scopes may not share a name.
    systemd-run --user --scope --quiet --collect \
        --unit "ccy-tmux-$$-$(systemd-escape -- "$name")" --description "CCY tmux session $name" \
        -- tmux -L "$CCY_TMUX_SOCKET" \
        new-session -d -s "$name" -c "$dir" -- bash -c "$trampoline" ccy-tmux "$launcher" "$@" \; \
        set-hook -g client-attached "$(ccy_tmux_single_attach_hook)"
}

# ccy_tmux_current_session — the name of the CCY session this process runs in, printed;
# nothing when it runs in none (a user's own tmux, or no tmux at all), and tmux is then not
# asked. tmux finds the session from the TMUX this process inherited from its pane.
ccy_tmux_current_session() {
    [[ -n "${TMUX:-}" ]] || return 0
    local socket="${TMUX%%,*}"
    [[ "$(basename "$socket")" == "$CCY_TMUX_SOCKET" ]] || return 0
    tmux display-message -p '#S'
}

# ccy_tmux_banner — inside a CCY session, one line on how to leave and come back. Silent in
# a user's own tmux, whose sessions ccy does not manage.
ccy_tmux_banner() {
    local name
    name=$(ccy_tmux_current_session) || return 1
    [[ -n "$name" ]] || return 0
    echo "tmux session '$name': F12 then Detach leaves it running; ${CCY_TMUX_SESSION_PREFIX} here or ccy-sessions brings it back." >&2
}
