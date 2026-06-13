# shellcheck shell=bash
# ssh-with-agent.bash — shared agent-forwarding helpers for the with-* wrappers
#
# Sourced (never executed) by ssh-with-password, scp-with-password,
# ssh-with-key and scp-with-key. It backs their -A (agent forwarding) support.
#
# Policy: when the caller passes -A we ALWAYS start a fresh, dedicated ssh-agent
# scoped to this one invocation and ask which keys to add — we never trust
# whatever agent (if any) the surrounding environment happens to provide. The
# dedicated agent is killed again when the calling wrapper exits, so no stray
# agent or forwarded keys are left behind.

SWA_SSH_DIR="${SSH_DIR:-$HOME/.ssh}"

# swa_wants_agent_forward ARGS... — succeed (return 0) when the forwarded args
# request agent forwarding: a short-flag cluster containing A (-A, -AX, -tA, …)
# or an explicit -o ForwardAgent=yes.
swa_wants_agent_forward() {
    local arg
    for arg in "$@"; do
        if [[ "$arg" == -[!-]* && "$arg" == *A* ]] || [[ "$arg" == "ForwardAgent=yes" ]]; then
            return 0
        fi
    done
    return 1
}

# swa_remote_git_push_init — print the remote-shell init run on an interactive
# forwarded login. It presets GIT_SSH_COMMAND so `git push` on the remote uses
# the FORWARDED agent and ignores that machine's ~/.ssh/config (its
# `IdentitiesOnly yes` + custom-named key would otherwise shadow the forwarded
# key and authenticate as the wrong, push-less account). `-F /dev/null` reads no
# config, so only the forwarded agent key is offered, then a login shell is
# exec'd. The \$SHELL is deliberately left for the REMOTE shell to expand.
swa_remote_git_push_init() {
    printf '%s' "export GIT_SSH_COMMAND=\"ssh -F /dev/null\"; exec \"\$SHELL\" -l"
}

# swa_has_remote_command ARGS... — succeed (0) when the ssh args include a remote
# command (a non-option token after the destination), i.e. NOT an interactive
# login. Mirrors ssh's own option parsing: the short options below take a
# following argument, everything else is a boolean flag or the destination.
swa_has_remote_command() {
    local argopts="bBcDEeFIiJLlmOopQRSWw"
    local t cl ch n i skip_next=0 seen_host=0
    for t in "$@"; do
        if [ "$skip_next" -eq 1 ]; then
            skip_next=0
            continue
        fi
        case "$t" in
            -*)
                cl="${t#-}"
                n=${#cl}
                i=0
                while [ "$i" -lt "$n" ]; do
                    ch="${cl:$i:1}"
                    if [[ "$argopts" == *"$ch"* ]]; then
                        # Arg-taking option: it consumes the rest of this token as
                        # its value, or the next token when it is the cluster's tail.
                        [ "$i" -eq $((n - 1)) ] && skip_next=1
                        break
                    fi
                    i=$((i + 1))
                done
                ;;
            *)
                if [ "$seen_host" -eq 0 ]; then
                    seen_host=1
                else
                    return 0
                fi
                ;;
        esac
    done
    return 1
}

# swa_discover_keys — print candidate private keys in SWA_SSH_DIR, one per line.
# Skips public keys and the usual non-key files.
swa_discover_keys() {
    local f name
    for f in "$SWA_SSH_DIR"/*; do
        [ -f "$f" ] || continue
        name="$(basename "$f")"
        case "$name" in
            *.pub|config|known_hosts|known_hosts.old|authorized_keys|*.swp|*.bak) continue ;;
        esac
        printf '%s\n' "$f"
    done
}

# swa_cleanup_agent — EXIT trap: kill the dedicated agent this session started.
swa_cleanup_agent() {
    if [ -n "${SWA_AGENT_STARTED:-}" ] && [ -n "${SSH_AGENT_PID:-}" ]; then
        ssh-agent -k > /dev/null
    fi
}

# swa_start_ephemeral_agent — start a dedicated ssh-agent, register its teardown,
# and interactively add the chosen keys. Exports SSH_AUTH_SOCK / SSH_AGENT_PID so
# the subsequent ssh/scp picks the agent up and can forward it. All prompts go to
# stderr; the chooser reads /dev/tty.
swa_start_ephemeral_agent() {
    local -a keys=() chosen=()
    local i key num choice
    mapfile -t keys < <(swa_discover_keys)

    echo "Agent forwarding (-A) requested — starting a dedicated ssh-agent." >&2
    eval "$(ssh-agent -s)"
    SWA_AGENT_STARTED=1
    trap swa_cleanup_agent EXIT

    if [ "${#keys[@]}" -eq 0 ]; then
        echo "No private keys found in $SWA_SSH_DIR — forwarding an empty agent." >&2
        return 0
    fi

    echo "Private keys in $SWA_SSH_DIR:" >&2
    i=1
    for key in "${keys[@]}"; do
        echo "  $i) $(basename "$key")" >&2
        i=$((i + 1))
    done
    echo "  a) Add all" >&2
    echo "  q) Add none" >&2

    read -rp "Keys to add to the forwarded agent (numbers, 'a', or 'q'): " choice < /dev/tty

    case "$choice" in
        q|Q) echo "Adding no keys." >&2; return 0 ;;
        a|A) chosen=("${keys[@]}") ;;
        *)
            for num in $choice; do
                if [[ "$num" =~ ^[0-9]+$ ]] && [ "$num" -ge 1 ] && [ "$num" -le "${#keys[@]}" ]; then
                    chosen+=("${keys[$((num - 1))]}")
                else
                    echo "  Skipping invalid selection: $num" >&2
                fi
            done
            ;;
    esac

    for key in "${chosen[@]}"; do
        if ssh-add "$key"; then
            echo "  Added: $(basename "$key")" >&2
        else
            echo "  FAILED: $(basename "$key")" >&2
        fi
    done

    echo "Keys loaded into the forwarded agent:" >&2
    if ! ssh-add -l >&2; then
        echo "  (agent has no identities loaded)" >&2
    fi
}
