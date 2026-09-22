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
# It is the same picker as ccy-sessions — one look for the whole human layer — shown only
# when there is something to offer. Esc is "quit" (InteractiveScripts rule 3).
ccy_tmux_offer() {
    local project="$1" listing name attached dir rows="" in_use=""
    listing=$(ccy_tmux_project_sessions "$project") || return 1
    while read -r name attached dir; do
        [[ -n "$name" ]] || continue
        if [[ "$attached" == "0" ]]; then
            rows+="$(ccy_tmux_row "$name" "$attached" "$dir")"$'\n'
        else
            in_use+="${in_use:+, }$name"
        fi
    done <<<"$listing"

    if [[ -z "$rows" ]]; then
        if [[ -n "$in_use" ]]; then
            echo "Open in other terminals: ${in_use}. Starting a new session." >&2
        fi
        printf 'create\n'
        return 0
    fi

    local header picked key row
    header="$(ccy_tmux_header "Enter attach   Ctrl-N new session   Esc or q quit" \
        "Detached sessions started from ${PWD/#${HOME}/\~}" \
        "${in_use:+Open in other terminals (cannot be attached): ${in_use}}")"
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

# ── which network a session's container is on ────────────────────────────────────────────
#
# A tmux session and a container know nothing about each other, so the link has to be found.
# It is the PROCESS TREE: the engine client running the container is a descendant of the
# session's pane, and it carries the container's name on its own command line
# (`--name <container>`) — the same fact lib/docker-health.bash already matches on. Name
# arithmetic would not do: sessions are numbered `-2` and containers `_1`, independently, so
# `ccy-app-2` and `app_yolo_1` cannot be paired by shape.
#
# The network itself is then asked of the ENGINE — not of the saved launch config, and not of
# the `--network` flag sitting on that same command line. Both of those record what was
# REQUESTED at launch, while `ccy --connect <net>` attaches a network to a container that is
# already running, so only the engine knows what a session is connected to now.

# ccy_session_containers <panes> <processes> — "<session> <engine> <container>" for every
# session in <panes>, in pane order, with "- -" when its tree holds no engine client. PURE:
# both tables arrive as text, so every shape is testable
# (scripts/test-ccy-session-network.bash).
#   <panes>     "<session> <pane-pid>" per line
#   <processes> "<pid> <ppid> <command line>" per line
ccy_session_containers() {
    local panes="$1" processes="$2"
    local -A parent=() client=() session_of=() seen=() owner=()
    local -a order=() client_pids=() fields=()
    local pid ppid args engine container session walk hops i

    while read -r pid ppid args; do
        [[ -n "$pid" ]] || continue
        parent["$pid"]="$ppid"
        # --name is not an engine-only flag, so the command itself has to be the engine.
        engine="${args%% *}"
        engine="${engine##*/}"
        case "$engine" in
        podman | docker) ;;
        *) continue ;;
        esac
        read -r -a fields <<<"$args"
        container=""
        for ((i = 1; i < ${#fields[@]}; i++)); do
            case "${fields[i]}" in
            --name=*)
                container="${fields[i]#--name=}"
                break
                ;;
            --name)
                container="${fields[i + 1]:-}"
                break
                ;;
            esac
        done
        [[ -n "$container" ]] || continue
        client["$pid"]="${engine} ${container}"
        client_pids+=("$pid")
    done <<<"$processes"

    while read -r session pid; do
        [[ -n "$session" && -n "$pid" ]] || continue
        session_of["$pid"]="$session"
        if [[ -z "${seen[$session]:-}" ]]; then
            seen["$session"]=1
            order+=("$session")
        fi
    done <<<"$panes"

    # Climb from each client to the pane that owns it. Rootless podman re-executes itself in
    # a user namespace, so one container legitimately appears twice in one tree; the first
    # client found wins and both name the same container anyway. The hop cap is what stops a
    # malformed table — a cycle, a pid that is its own parent — from spinning here.
    if [[ "${#client_pids[@]}" -gt 0 ]]; then
        for pid in "${client_pids[@]}"; do
            walk="$pid"
            hops=0
            while [[ -n "$walk" && "$walk" != "0" && "$hops" -lt 32 ]]; do
                if [[ -n "${session_of[$walk]:-}" ]]; then
                    session="${session_of[$walk]}"
                    if [[ -z "${owner[$session]:-}" ]]; then
                        owner["$session"]="${client[$pid]}"
                    fi
                    break
                fi
                walk="${parent[$walk]:-}"
                hops=$((hops + 1))
            done
        done
    fi

    if [[ "${#order[@]}" -gt 0 ]]; then
        for session in "${order[@]}"; do
            printf '%s %s\n' "$session" "${owner[$session]:-"- -"}"
        done
    fi
}

# ccy_network_word <container> <networks> — the one word a picker row shows for a session.
# PURE. <container> is "-" when no container was found for that session.
#   <networks> "<container> <the engine's rendering of its network list>" per line
#
# Three ordinary states get three distinct words, because a reader cannot act on an
# ambiguity:
#   <name>[,<name>]  what the ENGINE says the container is connected to right now
#   none             the container runs on no named network — `ccy --no-network`, or an
#                    engine default that is not a named network
#   no container     there is nothing to ask about: a `cc` session runs claude on the host,
#                    and a ccy session outlives a container that has exited
# The fourth word, "unknown", belongs to the caller: it is what a FAILED probe shows, and it
# is deliberately none of these.
ccy_network_word() {
    local container="$1" networks="$2" name rest raw
    if [[ "$container" == "-" ]]; then
        printf 'no container'
        return 0
    fi
    while read -r name rest; do
        [[ "$name" == "$container" ]] || continue
        # Podman's ps template renders a Go slice — "[a b]", "[]" — where Docker renders a
        # comma string. Both reduce to the same list.
        raw="$rest"
        if [[ "$raw" == \[*\] ]]; then
            raw="${raw:1:${#raw}-2}"
        fi
        raw="${raw//,/ }"
        local -a nets=()
        read -r -a nets <<<"$raw"
        if [[ "${#nets[@]}" -eq 0 ]]; then
            printf 'none'
        else
            local IFS=,
            printf '%s' "${nets[*]}"
        fi
        return 0
    done <<<"$networks"
    printf 'no container'
}

# ccy_tmux_network_rows — "<session> <network word>" for every session on CCY's server.
#
# Three probes whatever the number of sessions: tmux for the panes, one process table, and
# one query per engine that is actually running a CCY container — none at all when no session
# has one. The picker rebuilds its rows on every loop, so this has to stay flat in the
# session count rather than shelling out per row.
#
# Returns 1, having said why, when a probe fails. The caller then shows "unknown" on every
# row rather than a blank, because a blank would read as "no network".
ccy_tmux_network_rows() {
    local panes processes containers networks="" listing session engine container
    local -A engines=()

    # No server yet is the normal first-run state and means no sessions to report, exactly as
    # in ccy_tmux_list. It must not surface as an error: the caller prints one for every
    # failure, and a host with nothing running would be met with a complaint.
    if ! panes=$(ccy_tmux list-panes -a -F '#{session_name} #{pane_pid}' 2>&1); then
        if [[ "$panes" == *"no server running"* ]] || [[ "$panes" == *"No such file or directory"* ]]; then
            return 0
        fi
        print_error "tmux list-panes failed: $panes"
        return 1
    fi
    if ! processes=$(ps -ww -eo pid=,ppid=,args= 2>&1); then
        print_error "the process table could not be read, so no session can be matched to a container: $processes"
        return 1
    fi

    containers=$(ccy_session_containers "$panes" "$processes")

    # A here-string over empty text still yields one blank line, so the session guard is what
    # stops an empty engine name being collected and then run as a command.
    while read -r session engine container; do
        [[ -n "$session" && "$container" != "-" ]] || continue
        engines["$engine"]=1
    done <<<"$containers"

    for engine in "${!engines[@]}"; do
        # Every CCY container carries `--label ccy=true`, so this asks about those and
        # nothing else on the host.
        if ! listing=$("$engine" ps --filter label=ccy=true --format '{{.Names}} {{.Networks}}' 2>&1); then
            print_error "$engine ps failed: $listing"
            return 1
        fi
        networks+="${listing}"$'\n'
    done

    while read -r session engine container; do
        [[ -n "$session" ]] || continue
        printf '%s %s\n' "$session" "$(ccy_network_word "$container" "$networks")"
    done <<<"$containers"
}

# ── the shared picker: one look for ccy, cc and ccy-sessions ─────────────────────────────

# ccy_tmux_row <name> <attached> <dir> [network] — one aligned picker row. The state words are
# what the pickers test for, so they are defined once here. [network] adds a column before the
# directory; with no network given the column is left out altogether rather than padded,
# because a blank one would read as "no network" to the picker that never asked.
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

# ccy_tmux_confirm <title> <question> <yes-label> — a yes/no question in the same picker,
# so no prompt in the human layer is a bare read. The safe answer is the first row and
# therefore the default; Esc or q is "no" too. Returns 0 only when the yes row is chosen.
ccy_tmux_confirm() {
    local picked
    if ! picked="$(ccy_tmux_pick "$1" "$(ccy_tmux_header "Enter choose   Esc or q exit" "$2")" "" last \
        <<<"Yes, $3")"; then
        return 1
    fi
    [[ "${picked#*$'\n'}" == "Yes, "* ]]
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
    out=$(printf '%s\n%s\n' "${rows%$'\n'}" "$CCY_TMUX_EXIT_ROW" | fzf --height=~70% --layout=reverse --no-multi --no-sort --no-info \
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
