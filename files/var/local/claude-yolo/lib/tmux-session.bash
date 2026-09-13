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
# behind — offers to re-attach it; a session open in another terminal is never offered.
#
# Requires print_error (common-pure.bash, always loaded first). The interactive parts follow
# CLAUDE/InteractiveScripts.md: strict validation, bounded re-prompt, EOF is a clean exit.

CCY_TMUX_SOCKET="ccy"
CCY_TMUX_MAX_TRIES=3
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

# ccy_tmux_is_detached <name> — true only if the session exists and no client is on it.
# A listing failure is a failure (return 2 after the error), not "gone".
ccy_tmux_is_detached() {
    local listing
    listing=$(ccy_tmux_list) || return 2
    [[ "$(awk -v want="$1" '$1 == want { print $2 }' <<<"$listing")" == "0" ]]
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
# a launcher: attaching IS the session, so the caller exits afterwards.
ccy_tmux_attach() {
    local name="${1:?ccy_tmux_attach requires a session name}"
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
    if ccy_tmux_is_detached "$name"; then
        echo "Detached. '$name' keeps running; run ccy in its project directory to return to it." >&2
    elif [[ -n "$(ccy_tmux_list | awk -v want="$name" '$1 == want')" ]]; then
        echo "'$name' is open in another terminal, so this terminal was not attached to it." >&2
    else
        echo "'$name' has ended." >&2
    fi
    return 0
}

# ccy_tmux_offer <project> — when the project has detached sessions, ask what to do. Prints
# exactly one line on stdout: "attach <name>", "create", or "quit". Prompts go to stderr.
# EOF or a cancelled prompt is "quit" (InteractiveScripts rule 3); invalid input re-prompts
# up to CCY_TMUX_MAX_TRIES (rule 2).
ccy_tmux_offer() {
    local project="$1" listing name attached
    local -a detached=() in_use=()
    listing=$(ccy_tmux_project_sessions "$project") || return 1
    while read -r name attached _; do
        [[ -n "$name" ]] || continue
        if [[ "$attached" == "0" ]]; then
            detached+=("$name")
        else
            in_use+=("$name")
        fi
    done <<<"$listing"

    if [[ ${#in_use[@]} -gt 0 ]]; then
        echo "Open in other terminals (not offered): ${in_use[*]}" >&2
    fi
    if [[ ${#detached[@]} -eq 0 ]]; then
        printf 'create\n'
        return 0
    fi

    echo "Detached CCY session(s) started from ${PWD/#${HOME}/\~}:" >&2
    local i
    for i in "${!detached[@]}"; do
        printf '  %d) %s\n' "$((i + 1))" "${detached[$i]}" >&2
    done

    local attempt=1 reply range=""
    if [[ ${#detached[@]} -gt 1 ]]; then
        range=" or a number up to ${#detached[@]}"
    fi
    while :; do
        printf "Attach [1]%s, 'n' for a new session, 'q' to quit: " "$range" >&2
        if ! read -r reply </dev/tty; then
            echo >&2
            echo "Cancelled. Nothing started." >&2
            printf 'quit\n'
            return 0
        fi
        case "$reply" in
        "" | 1)
            printf 'attach %s\n' "${detached[0]}"
            return 0
            ;;
        n | N)
            printf 'create\n'
            return 0
            ;;
        q | Q)
            echo "Nothing started." >&2
            printf 'quit\n'
            return 0
            ;;
        *)
            if [[ "$reply" =~ ^[0-9]+$ ]] && ((reply >= 1 && reply <= ${#detached[@]})); then
                printf 'attach %s\n' "${detached[$((reply - 1))]}"
                return 0
            fi
            echo "  '$reply' is not a listed number, 'n' or 'q' — let's try again." >&2
            ;;
        esac
        attempt=$((attempt + 1))
        if [[ "$attempt" -gt "$CCY_TMUX_MAX_TRIES" ]]; then
            echo "Giving up after $CCY_TMUX_MAX_TRIES attempts. Nothing started." >&2
            printf 'quit\n'
            return 0
        fi
    done
}

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

    local decision action name
    decision=$(ccy_tmux_offer "$project") || return 1
    read -r action name <<<"$decision"
    case "$action" in
    quit)
        exit 0
        ;;
    attach)
        # A refusal here means the session was taken between the offer and the answer.
        # Not a retry loop: the honest move is to show the offer again from the top.
        if ccy_tmux_attach "$name"; then
            exit 0
        fi
        exec "$@"
        ;;
    esac

    name=$(ccy_tmux_next_name "$project") || return 1
    echo "Starting session '$name' under tmux. If this terminal dies, run ${CCY_TMUX_SESSION_PREFIX} here again to re-attach." >&2
    # The trampoline's dollars are escaped: they expand in the bash tmux starts, not here.
    local hold_on_failure
    hold_on_failure="\"\$@\"; rc=\$?; if [ \"\$rc\" -ne 0 ]; then printf '\\n${CCY_TMUX_SESSION_PREFIX} exited with status %s. Press Enter to close this session.\\n' \"\$rc\"; read -r; fi; exit \"\$rc\""
    # The scope keeps the server out of the terminal's cgroup; --collect lets systemd forget
    # it once empty, whatever its exit status. The session is created detached, the
    # single-attach hook is installed, and only then is this client attached — one server
    # round trip, so no client can reach the session before the hook exists.
    exec systemd-run --user --scope --quiet --collect \
        --unit "ccy-tmux-$$" --description "CCY tmux session $name" \
        -- tmux -L "$CCY_TMUX_SOCKET" \
        new-session -d -s "$name" -- bash -c "$hold_on_failure" ccy-tmux "$@" \; \
        set-hook -g client-attached "$(ccy_tmux_single_attach_hook)" \; \
        attach-session -t "=$name"
}

# ccy_tmux_banner — inside a CCY session, one line on how to leave and come back. Silent in
# a user's own tmux, whose sessions ccy does not manage.
ccy_tmux_banner() {
    [[ -n "${TMUX:-}" ]] || return 0
    local socket="${TMUX%%,*}"
    [[ "$(basename "$socket")" == "$CCY_TMUX_SOCKET" ]] || return 0
    local name
    name=$(tmux display-message -p '#S') || return 1
    echo "tmux session '$name': F12 then Detach leaves it running; ${CCY_TMUX_SESSION_PREFIX} here or ccy-sessions brings it back." >&2
}
