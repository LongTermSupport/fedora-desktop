#!/bin/bash
# SSH Handling Library
# Shared SSH key operations for claude-yolo (ccy)
#
# Version: 1.8.0 - A selected key file that needs a passphrase and that the session's agent
#                  holds is forwarded from the agent through ccy's one-key agent, in place
#                  of the file (ccy_agent_filter_start, Plan 00163).
#          1.7.1 - configure_git_signing takes commit/tag signing on or off from the
#                  project's local config first (Plan 00161).
#          1.7.0 - configure_git_signing signs through an agent; no private key is staged
#                  (Plan 00139).
#          1.4.0 - Two identities a box may hold besides a github_<alias> key:
#                  the project remote's own key, reached through an ssh-config
#                  alias that `ssh -G` resolves to GitHub (a deploy key on a
#                  box provisioned with no GitHub account), and the session's
#                  forwarded ssh-agent (SSH_AGENT_SENTINEL in SSH_KEYS). The
#                  alias stanza and its known_hosts pin travel to the container
#                  as SSH_CONFIG_EXTRA_B64 / SSH_KNOWN_HOSTS_PINS; an exported
#                  GH_TOKEN is cross-checked against an account identity.
#          1.3.0 - GitHub probes now unlock passphrase keys into a PRIVATE
#                  throwaway ssh-agent BEFORE any connection is opened. A
#                  passphrase prompt left waiting used to outlive GitHub's
#                  ~2-minute sshd LoginGraceTime on the already-open port-22
#                  connection: the late-typed passphrase then failed instantly
#                  on the dead socket and the failure was misread as "port 22
#                  blocked", offering a spurious 443 fallback. ssh-add talks to
#                  no server, so the prompt can now wait indefinitely — and the
#                  whole validation asks for each passphrase once, not once per
#                  probe. The agent holds only ccy's selected keys and is
#                  killed when validation returns.
#          1.2.0 - The no-SSH-key fallback in build_ssh_mounts_and_validate()
#                  now honours a caller-supplied GH_TOKEN directly instead
#                  of routing it through `gh auth token`. Measured: gh
#                  already gives an exported GH_TOKEN precedence over its
#                  own stored credentials, so this is not a live-bug fix —
#                  it makes that precedence explicit in our own code and
#                  drops the `gh auth token` dependency (no local gh login
#                  required) for a runner authenticating purely by token.
#          1.1.0 - The token-owner cross-check no longer misreports a GitHub
#                  outage as a configuration error. `gh api user` is retried and
#                  its answer validated as a login before being compared; a
#                  failure now says GitHub is unavailable and offers
#                  CCY_SKIP_TOKEN_OWNER_CHECK=1 rather than telling the user to
#                  go and edit localhost.yml.
#          1.0.1

# Read the project's git remote URL — origin if present, else first remote.
# Echoes the URL on stdout (or empty when the cwd isn't a git repo or has no
# remote configured).
get_project_remote_url() {
    local repo_path="${1:-.}"

    local probe
    probe=$(git -C "$repo_path" rev-parse --git-dir 2>&1) || return 0
    : "${probe:=}"  # silence shellcheck SC2034 — we only need the exit code

    local url
    url=$(git -C "$repo_path" config --get remote.origin.url 2>&1) || url=""
    if [ -z "$url" ]; then
        local first
        first=$(git -C "$repo_path" remote 2>&1) || first=""
        first=$(echo "$first" | head -1)
        if [ -n "$first" ]; then
            url=$(git -C "$repo_path" config --get "remote.${first}.url" 2>&1) || url=""
        fi
    fi
    # An empty URL is a valid outcome (no remote configured), not an error.
    # Must return 0 explicitly — callers assign via `var=$(...)` under `set -e`,
    # where a non-zero command substitution would abort the whole script.
    if [ -n "$url" ]; then
        echo "$url"
    fi
    return 0
}

# The host part of an SSH remote URL: `git@HOST:path` or `ssh://git@HOST[:port]/path`.
# Echoes the host, or nothing for any other URL shape (https, empty, …).
remote_ssh_host() {
    local url="$1"
    if [[ "$url" =~ ^git@([^:/]+):.+$ ]]; then
        echo "${BASH_REMATCH[1]}"
    elif [[ "$url" =~ ^ssh://git@([^:/]+)(:[0-9]+)?/.+$ ]]; then
        echo "${BASH_REMATCH[1]}"
    fi
    return 0
}

# Ask ssh what an ssh-config alias means, and answer only if it is GitHub.
#
# A box provisioned without a GitHub account reaches its repositories through
# per-repository deploy keys bound to `Host <alias>` stanzas in ~/.ssh/config, and
# its remotes are `git@<alias>:owner/repo.git`. The alias's NAME is whatever the
# provisioning system chose, so it is never pattern-matched here: `ssh -G` prints
# the effective hostname, port and identity files for it from the user's own
# config, `~` already expanded, and that is the only authority consulted.
#
# Args: $1 = alias (the host part of the remote URL)
# Echoes "hostname<TAB>port<TAB>keyfile" when the alias resolves to github.com or
# ssh.github.com. keyfile is the FIRST identity file that exists on disk, or empty
# when none does — that case is still rc 0, because "the remote names a GitHub
# alias whose key is missing" must be reported, not treated as "no keys here".
# Returns 1 when the host is GitHub written literally, empty, or bound to
# something that is not GitHub.
resolve_github_ssh_alias() {
    local alias="$1"
    case "$alias" in
        ""|github.com|ssh.github.com) return 1 ;;
    esac
    local cfg
    cfg=$(ssh -G "$alias" 2>/dev/null) || return 1
    local hostname port
    hostname=$(awk '$1 == "hostname" { print $2; exit }' <<< "$cfg")
    port=$(awk '$1 == "port" { print $2; exit }' <<< "$cfg")
    case "$hostname" in
        github.com|ssh.github.com) ;;
        *) return 1 ;;
    esac
    # `ssh -G` prints IdentityFile values AS WRITTEN — a leading `~/` is not
    # expanded until ssh opens the file (measured: a config line
    # `IdentityFile ~/.ssh/deploy_keys/x` comes back verbatim), so it is
    # expanded here before the existence test.
    local keyfile="" candidate
    while IFS= read -r candidate; do
        [ -n "$candidate" ] || continue
        case "$candidate" in
            \~/*) candidate="$HOME/${candidate#\~/}" ;;
            \~)   candidate="$HOME" ;;
        esac
        if [ -f "$candidate" ]; then
            keyfile="$candidate"
            break
        fi
    done < <(awk '$1 == "identityfile" { print $2 }' <<< "$cfg")
    printf '%s\t%s\t%s\n' "$hostname" "${port:-22}" "$keyfile"
    return 0
}

# Parse owner/repo from a GitHub remote URL. Handles ssh, https, the
# `git@github.com-<alias>:` form, and any ssh-config alias that `ssh -G`
# resolves to GitHub (resolve_github_ssh_alias).
#
# Args: $1 = URL
# Echoes "owner/repo" on stdout, or empty if not a recognised GitHub URL.
parse_github_owner_repo() {
    local url="$1"
    url="${url%.git}"
    if [[ "$url" =~ ^git@github\.com(-[^:]+)?:(.+)$ ]]; then
        echo "${BASH_REMATCH[2]}"
        return 0
    fi
    if [[ "$url" =~ ^https?://github\.com/(.+)$ ]]; then
        echo "${BASH_REMATCH[1]}"
        return 0
    fi
    if [[ "$url" =~ ^ssh://git@github\.com(:[0-9]+)?/(.+)$ ]]; then
        echo "${BASH_REMATCH[2]}"
        return 0
    fi
    local host
    host=$(remote_ssh_host "$url")
    if [ -n "$host" ] && resolve_github_ssh_alias "$host" >/dev/null; then
        if [[ "$url" =~ ^git@[^:/]+:(.+)$ ]] || [[ "$url" =~ ^ssh://git@[^:/]+(:[0-9]+)?/(.+)$ ]]; then
            echo "${BASH_REMATCH[${#BASH_REMATCH[@]}-1]}"
            return 0
        fi
    fi
    return 1
}

# The project remote's GitHub alias, if it has one.
#
# Sets GITHUB_ALIAS_HOST (the alias as written in the remote URL),
# GITHUB_ALIAS_HOSTNAME, GITHUB_ALIAS_PORT and GITHUB_ALIAS_KEY (the existing
# identity file the alias binds). All four are cleared first.
#
# Args: $1 = repo path
# Returns 0 when the remote uses a GitHub alias with a key on disk; 1 when the
# project has no such remote (not a repo, no remote, literal GitHub host, alias
# bound elsewhere); 2 when the alias IS GitHub but its identity file does not
# exist — printed to stderr with the alias and the path, because that box was
# provisioned to reach this repo and the key it was given is gone.
GITHUB_ALIAS_HOST=""
GITHUB_ALIAS_HOSTNAME=""
GITHUB_ALIAS_PORT=""
GITHUB_ALIAS_KEY=""
detect_project_github_alias() {
    local repo_path="${1:-.}"
    GITHUB_ALIAS_HOST=""
    GITHUB_ALIAS_HOSTNAME=""
    GITHUB_ALIAS_PORT=""
    GITHUB_ALIAS_KEY=""

    local url host resolved
    url=$(get_project_remote_url "$repo_path")
    [ -n "$url" ] || return 1
    host=$(remote_ssh_host "$url")
    [ -n "$host" ] || return 1
    resolved=$(resolve_github_ssh_alias "$host") || return 1

    local hostname port keyfile
    IFS=$'\t' read -r hostname port keyfile <<< "$resolved"
    if [ -z "$keyfile" ]; then
        local declared=""
        if ! declared=$(ssh -G "$host" 2>&1 | awk '$1 == "identityfile" { print $2 }' | paste -sd ' '); then
            declared=""
        fi
        echo "ERROR: the remote $url uses the ssh alias '$host', which your ~/.ssh/config binds to" >&2
        echo "       $hostname:$port — but none of its IdentityFile entries exist on disk:" >&2
        echo "         ${declared:-(none declared)}" >&2
        echo "       The key this box was given for the repository is missing. Re-run the" >&2
        echo "       provisioning that writes it; ccy will not guess another identity." >&2
        return 2
    fi
    GITHUB_ALIAS_HOST="$host"
    GITHUB_ALIAS_HOSTNAME="$hostname"
    GITHUB_ALIAS_PORT="$port"
    GITHUB_ALIAS_KEY="$keyfile"
    return 0
}

# The `Host` stanza the container needs so the remote's alias resolves inside it.
#
# Args: $1 = alias, $2 = hostname, $3 = port,
#       $4 = key path INSIDE the container, or empty when the alias key is not
#            mounted — then no IdentityFile is named and the container's own
#            agent (holding whatever account key was chosen) answers for the alias,
#       $5 = "yes" to pin ssh to that key (IdentitiesOnly), "no" to leave the line
#            out — used when an agent is forwarded, so that ssh offers the agent's
#            identities first and a push authenticates as the person, while the
#            deploy key remains the fallback for a fetch.
render_ssh_alias_stanza() {
    local alias="$1" hostname="$2" port="$3" keypath="$4" identities_only="$5"
    printf 'Host %s\n    HostName %s\n    Port %s\n    User git\n' "$alias" "$hostname" "$port"
    if [ -n "$keypath" ]; then
        printf '    IdentityFile %s\n' "$keypath"
        if [ "$identities_only" = "yes" ]; then
            printf '    IdentitiesOnly yes\n'
        fi
    fi
}

# What the launcher hands the entrypoint for an alias: the stanza, base64 so a
# multi-line value survives `-e`, and the host:port the entrypoint must ALSO pin
# in known_hosts when it is not plain github.com:22 (which it pins already).
#
# Args: as render_ssh_alias_stanza
# Sets: SSH_CONFIG_EXTRA_B64, SSH_KNOWN_HOSTS_PINS
SSH_CONFIG_EXTRA_B64=""
SSH_KNOWN_HOSTS_PINS=""
compose_ssh_alias_exports() {
    local hostname="$2" port="$3"
    SSH_CONFIG_EXTRA_B64=$(render_ssh_alias_stanza "$@" | base64 -w0)
    if [ "$hostname:$port" != "github.com:22" ]; then
        SSH_KNOWN_HOSTS_PINS="$hostname:$port"
    else
        SSH_KNOWN_HOSTS_PINS=""
    fi
}

# Is there a forwarded (or otherwise live) ssh-agent holding at least one key?
# `ssh-add -l` is 0 with identities, 1 with an empty agent, 2 when it cannot
# connect; only the first is an agent worth mounting. What ssh-add said is kept
# in SSH_AGENT_PROBE_OUTPUT for the caller's message.
SSH_AGENT_PROBE_OUTPUT=""
ssh_agent_usable() {
    SSH_AGENT_PROBE_OUTPUT=""
    [ -n "${SSH_AUTH_SOCK:-}" ] || return 1
    [ -S "$SSH_AUTH_SOCK" ] || [ -e "$SSH_AUTH_SOCK" ] || return 1
    local rc=0
    SSH_AGENT_PROBE_OUTPUT=$(ssh-add -l 2>&1) || rc=$?
    [ "$rc" -eq 0 ]
}

# Is the key file's public half among the identities the session's agent holds?
# Matches by fingerprint against SSH_AGENT_PROBE_OUTPUT, which ssh_agent_usable
# filled; a key without its .pub beside it cannot be matched and counts as not held.
#
# Args: $1 = private key path
ssh_agent_holds_key() {
    local pub="$1.pub" listing fingerprint
    [ -f "$pub" ] || return 1
    listing=$(ssh-keygen -lf "$pub" 2>&1) || return 1
    fingerprint=$(awk 'NR == 1 { print $2 }' <<< "$listing")
    [ -n "$fingerprint" ] || return 1
    grep -qF -- " $fingerprint " <<< "$SSH_AGENT_PROBE_OUTPUT"
}

# A GitHub greeting names a LOGIN for an account key and OWNER/REPO for a
# deploy key. The slash is the whole distinction.
github_identity_is_deploy_key() {
    [[ "$1" == */* ]]
}

# The literal SSH_KEYS entry that means "the session's ssh-agent, not a file".
readonly SSH_AGENT_SENTINEL="ssh-agent"

# Probe each ~/.ssh/github_<alias> key by checking whether the matching
# `gh-token-<alias>` token (from play-github-cli-multi.yml) has PUSH
# permission on the remote repo. We check `.permissions.push` from
# `gh api repos/owner/repo` — read access is meaningless for public
# repos because every authenticated token can read them, which would
# mark every key as a match and defeat the auto-default.
#
# This avoids two SSH-probe pitfalls:
#   1. Passphrase-protected keys + ssh-agent isolation = false negatives
#   2. SSH handshake latency (gh API is faster)
#
# IMPORTANT: SEQUENTIAL by design. `gh-token-<alias>` calls `gh auth switch`
# which mutates the global gh active-account state. Running these in
# parallel would race on shared state and corrupt the user's session.
#
# Restores the originally-active account when done so the user's shell
# state is unchanged.
#
# Echoes one matching key path per line on stdout (sorted by key name).
# Sets PROBE_LOG_DIR for diagnostics on 0-match outcome.
#
# Args: $1 = remote URL
probe_gh_keys_for_remote() {
    local remote_url="$1"
    [ -z "$remote_url" ] && return 0

    local owner_repo
    owner_repo=$(parse_github_owner_repo "$remote_url") || return 0
    [ -z "$owner_repo" ] && return 0

    # Source the gh aliases file — required because this lib runs in a
    # subshell that doesn't inherit interactive bash function definitions.
    if [ -f "$HOME/.bashrc-includes/gh-aliases.inc.bash" ]; then
        # shellcheck source=/dev/null
        source "$HOME/.bashrc-includes/gh-aliases.inc.bash"
    fi

    # Per-probe logs for diagnosing a 0-match outcome.
    # BSH-10: mktemp -d (0700) rather than a predictable /tmp/ccy-gh-probe-$PID.
    PROBE_LOG_DIR=$(mktemp -d /tmp/ccy-gh-probe-XXXXXX)
    export PROBE_LOG_DIR

    # Capture the originally-active gh account so we can restore it after
    # probing (each gh-token-<alias> call switches the active account).
    local original_active=""
    original_active=$(gh api user --jq .login 2>"$PROBE_LOG_DIR/original.err")

    local key_path key_basename alias token_func token api_out api_rc type_check
    while IFS= read -r key_path; do
        [ -z "$key_path" ] && continue
        key_basename=$(basename "$key_path")
        if [[ "$key_basename" =~ ^github_(.+)$ ]]; then
            alias="${BASH_REMATCH[1]}"
            token_func="gh-token-${alias}"
            type_check=$(type -t "$token_func" 2>"$PROBE_LOG_DIR/${alias}.type.err")
            if [ "$type_check" = "function" ]; then
                token=$("$token_func" 2>"$PROBE_LOG_DIR/${alias}.token.err")
                if [ -n "$token" ]; then
                    api_out=$(GH_TOKEN="$token" gh api "repos/$owner_repo" --jq '.permissions.push' 2>&1)
                    api_rc=$?
                    if [ "$api_rc" -eq 0 ] && [ "$api_out" = "true" ]; then
                        echo "$key_path"
                    fi
                    printf "rc=%s push=%s\n" "$api_rc" "$api_out" > "$PROBE_LOG_DIR/${alias}.api.log"
                fi
            else
                echo "no gh-token-${alias} function" > "$PROBE_LOG_DIR/${alias}.token.err"
            fi
        fi
    done < <(find "$HOME/.ssh" -type f -name "github_*" ! -name "*.pub" 2>/dev/null | sort)

    # Restore the original active account so the user's shell state is
    # unaffected. Failure here goes to the log dir but does not fail the
    # function — the caller cannot do anything useful about it.
    if [ -n "$original_active" ]; then
        local restore_out
        restore_out=$(gh auth switch --hostname github.com --user "$original_active" 2>&1)
        printf "%s\n" "$restore_out" > "$PROBE_LOG_DIR/restore.log"
    fi
    return 0
}

# Print the key menu's current list. Called only from discover_and_select_ssh_keys,
# whose locals it reads through bash's dynamic scope: short_list, shown, labels,
# candidates, pushers, default_pick, tool_name.
_ssh_key_menu_list() {
    local i suffix
    echo ""
    if [ "$short_list" = true ]; then
        echo "Identities with push access to this remote:"
    else
        echo "Available identities:"
    fi
    echo ""
    echo "  0) Continue without SSH key (git push will NOT work)"
    echo ""
    for i in "${!shown[@]}"; do
        suffix=""
        [ "$((i+1))" = "$default_pick" ] && suffix=" ← default"
        echo "  $((i+1))) ${labels[${shown[$i]}]}${suffix}"
    done
    if [ "$short_list" = true ]; then
        echo ""
        echo "  a) Show every identity ($((${#candidates[@]} - ${#pushers[@]})) more, not confirmed to push here)"
    fi
    echo ""
    [ -n "$default_pick" ] && echo "Press ENTER to accept the default ($default_pick)."
    echo "You can also specify keys manually with: $tool_name --ssh-key <path>"
    echo ""
}

# Function to discover and interactively select SSH keys
#
# Three sources, in the order they are offered:
#   1. the project remote's own key — an ssh-config alias `ssh -G` resolves to
#      GitHub (a deploy key on a box with no GitHub account);
#   2. the ~/.ssh/github_<alias> account keys play-github-cli-multi.yml writes;
#   3. the session's ssh-agent, when it holds a key (typically forwarded by
#      `ssh -A`; pushes then authenticate as that person).
# When the remote's key is the ONLY candidate it is selected without a prompt:
# there is nothing to choose between, and that is exactly the headless-box case.
# A session agent that already holds an account key asks for no passphrase, which
# a key file does twice (here and in the container): such keys are marked, and with
# no push-verified key and no remote key the agent is the default.
#
# Args: $1 = tool_name (for display)
# Modifies: SSH_KEYS global array
# Returns: 0 on success (possibly with no key chosen), 1 when the remote names a
#          GitHub alias whose key is missing — a provisioning fault, not a menu.
discover_and_select_ssh_keys() {
    local tool_name="$1"

    # These keys are managed by play-github-cli-multi.yml which creates keys with
    # the pattern ~/.ssh/github_<alias> for each configured GitHub account.
    # See: playbooks/imports/optional/common/play-github-cli-multi.yml:163-183
    mapfile -t GITHUB_KEYS < <(find "$HOME/.ssh" -type f -name "github_*" ! -name "*.pub" 2>/dev/null | sort)

    local alias_rc=0
    detect_project_github_alias "." || alias_rc=$?
    if [ "$alias_rc" -eq 2 ]; then
        return 1
    fi

    # agent_held: the account key files whose public half the agent already holds. Those
    # are unlocked, so the agent route asks for no passphrase where a key file asks twice
    # (here, then again in the container).
    local agent_ok=false agent_key_count=0 held_key
    local -a agent_held=()
    if ssh_agent_usable; then
        agent_ok=true
        agent_key_count=$(grep -c . <<< "$SSH_AGENT_PROBE_OUTPUT")
        for held_key in "${GITHUB_KEYS[@]}"; do
            if ssh_agent_holds_key "$held_key"; then
                agent_held+=("$held_key")
            fi
        done
    fi

    local remote_url=""
    remote_url=$(get_project_remote_url ".")

    # The remote's key, alone: nothing to choose, so nothing to ask.
    if [ "$alias_rc" -eq 0 ] && [ ${#GITHUB_KEYS[@]} -eq 0 ] && [ "$agent_ok" = false ]; then
        SSH_KEYS+=("$GITHUB_ALIAS_KEY")
        echo ""
        echo "✓ Using the project remote's key: $GITHUB_ALIAS_KEY"
        echo "  (alias $GITHUB_ALIAS_HOST → $GITHUB_ALIAS_HOSTNAME:$GITHUB_ALIAS_PORT in ~/.ssh/config; no github_ key, no agent)"
        echo ""
        return 0
    fi

    if [ "$alias_rc" -ne 0 ] && [ ${#GITHUB_KEYS[@]} -eq 0 ] && [ "$agent_ok" = false ]; then
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "⚠  WARNING: No SSH Keys Available"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        echo "No github_ SSH key in ~/.ssh/, no ssh-agent holding a key, and the project"
        echo "remote does not use an ssh-config alias bound to GitHub."
        echo "Git push operations will NOT work without SSH keys."
        echo ""
        echo "To set up GitHub SSH keys, run:"
        echo "  ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
        echo ""
        echo "Or specify a key manually:"
        echo "  $tool_name --ssh-key ~/.ssh/<your-key>"
        echo ""
        echo "Or log in with agent forwarding (ssh -A) and pass:"
        echo "  $tool_name --ssh-agent"
        echo ""
        # Enter is the only answer that starts a session, and with nobody to press it (a
        # restore, a restart, no terminal) it is taken: the session starts as it did before.
        if ccy_launch_unattended; then
            echo "Nobody to ask (a restore, a restart or no terminal): continuing WITHOUT an SSH key."
        else
            read -rp "$CCY_PROMPT_SSH_NO_KEY " _unused
        fi
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        return 0
    fi

    # Probe every account key against the project's remote, so the menu can steer
    # towards the key(s) that actually have push access. Picking the wrong key here
    # mis-routes git push to an account that is refused, so steering the user toward
    # a verified-working key is the primary purpose of this prompt.
    local working_keys=""
    local probe_status="skipped (not a git repo or no remote)"
    local suggested_key=""
    if [ ${#GITHUB_KEYS[@]} -gt 0 ] && [ -n "$remote_url" ]; then
        echo ""
        echo "Probing GitHub accounts against remote: $remote_url"
        echo "(checks .permissions.push via gh-token-<alias> — sequential, ~1-3 seconds)"

        working_keys=$(probe_gh_keys_for_remote "$remote_url")

        local match_count=0
        if [ -n "$working_keys" ]; then
            match_count=$(echo "$working_keys" | grep -c .)
        fi

        case "$match_count" in
            0)  probe_status="no account keys have push access to this remote (logs: $PROBE_LOG_DIR/)" ;;
            1)  suggested_key=$(echo "$working_keys" | head -1)
                probe_status="1 account key has push access" ;;
            *)  probe_status="$match_count account keys have push access" ;;
        esac
    elif [ ${#GITHUB_KEYS[@]} -eq 0 ]; then
        probe_status="no github_ account keys to probe"
    fi
    # With no verified account key, the remote's own key is the natural default.
    if [ -z "$suggested_key" ] && [ "$alias_rc" -eq 0 ]; then
        suggested_key="$GITHUB_ALIAS_KEY"
    fi
    # With nothing else to steer by, an agent that already holds an account key is the
    # default: choosing a key file instead means typing its passphrase.
    if [ -z "$suggested_key" ] && [ ${#agent_held[@]} -gt 0 ]; then
        suggested_key="$SSH_AGENT_SENTINEL"
    fi

    # probed[i] says whether the push probe checked candidates[i]: only the github_
    # account keys are; the remote's own key and the agent are offered unchecked.
    local -a candidates=() labels=() probed=()
    if [ "$alias_rc" -eq 0 ]; then
        candidates+=("$GITHUB_ALIAS_KEY")
        labels+=("$GITHUB_ALIAS_KEY  — the project remote's key (alias $GITHUB_ALIAS_HOST → $GITHUB_ALIAS_HOSTNAME:$GITHUB_ALIAS_PORT)")
        probed+=(no)
    fi
    local i
    for i in "${!GITHUB_KEYS[@]}"; do
        local marker=""
        if [ -n "$working_keys" ] && grep -qxF "${GITHUB_KEYS[$i]}" <<< "$working_keys"; then
            marker="  ✓ has push access to this remote"
        fi
        if [[ " ${agent_held[*]:-} " == *" ${GITHUB_KEYS[$i]} "* ]]; then
            marker="$marker  (also in your ssh-agent, which asks no passphrase)"
        fi
        # The remote's alias key can be one of the account keys: one file, one line. The
        # alias entry (first in the list) takes the account key's markers and its probe.
        if [ "$alias_rc" -eq 0 ] && [ "${GITHUB_KEYS[$i]}" = "$GITHUB_ALIAS_KEY" ]; then
            labels[0]="${labels[0]}${marker}"
            if [ -n "$remote_url" ]; then probed[0]=yes; fi
            continue
        fi
        candidates+=("${GITHUB_KEYS[$i]}")
        labels+=("${GITHUB_KEYS[$i]}${marker}")
        if [ -n "$remote_url" ]; then probed+=(yes); else probed+=(no); fi
    done
    if [ "$agent_ok" = true ]; then
        local agent_label="the session's ssh-agent ($agent_key_count key(s))"
        if [ ${#agent_held[@]} -gt 0 ]; then
            local held_names="" held_path
            for held_path in "${agent_held[@]}"; do
                held_names="${held_names:+$held_names, }$(basename "$held_path")"
            done
            agent_label="$agent_label holding $held_names — already unlocked, no passphrase asked"
        fi
        candidates+=("$SSH_AGENT_SENTINEL")
        labels+=("$agent_label; the container can use EVERY key the agent holds, and signs as whichever it picks; runs the container with SELinux labelling off")
        probed+=(no)
    fi

    # The candidates the probe found push access for. When there are any, the first
    # menu offers only them: a key that cannot push, picked by mistake, shows up only
    # at the session's first refused push. The full list is one keystroke (a) away,
    # and a key in it that cannot push needs an explicit yes. With none, there is
    # nothing better to steer towards, so the full list is the menu.
    local -a pushers=() shown=()
    for i in "${!candidates[@]}"; do
        if [ -n "$working_keys" ] && grep -qxF "${candidates[$i]}" <<< "$working_keys"; then
            pushers+=("$i")
        fi
    done
    # The agent joins the short list, after the keys that can push, when it holds one of
    # them: it is the route that asks for no passphrase.
    if [ ${#pushers[@]} -gt 0 ] && [ "$agent_ok" = true ]; then
        for held_key in "${agent_held[@]}"; do
            if grep -qxF "$held_key" <<< "$working_keys"; then
                pushers+=("$((${#candidates[@]} - 1))")
                break
            fi
        done
    fi
    local short_list=false
    if [ ${#pushers[@]} -gt 0 ]; then
        short_list=true
        shown=("${pushers[@]}")
        # The default in both lists is the first key that can push, so ENTER after `a`
        # never lands on a key that needs the "use it anyway" question.
        suggested_key="${candidates[${pushers[0]}]}"
    else
        shown=("${!candidates[@]}")
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "SSH Key Selection for Claude YOLO"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "No SSH key was specified with --ssh-key flag."
    echo "Probe result: $probe_status"

    local list_pending=true default_pick="" selection confirm picked p is_pusher
    local -r max_tries=3
    local mistakes=0
    while true; do
        if [ "$mistakes" -ge "$max_tries" ]; then
            echo "✗ Giving up after $max_tries invalid selections; no SSH key chosen." >&2
            return 1
        fi
        if [ "$list_pending" = true ]; then
            list_pending=false
            default_pick=""
            if [ -n "$suggested_key" ]; then
                for i in "${!shown[@]}"; do
                    if [ "${candidates[${shown[$i]}]}" = "$suggested_key" ]; then
                        default_pick=$((i+1))
                        break
                    fi
                done
            fi
            _ssh_key_menu_list
        fi

        local prompt_text="${CCY_PROMPT_SSH_KEY} [0-${#shown[@]}"
        [ "$short_list" = true ] && prompt_text="$prompt_text, a"
        prompt_text="$prompt_text]"
        [ -n "$default_pick" ] && prompt_text="$prompt_text (default: $default_pick)"
        prompt_text="$prompt_text: "
        if ! read -rp "$prompt_text" selection; then
            echo "✗ Input closed before an SSH key was chosen." >&2
            return 1
        fi
        echo ""

        if [ -z "$selection" ]; then
            if [ -z "$default_pick" ]; then
                echo "No default available — please enter a number between 0 and ${#shown[@]}"
                echo ""
                mistakes=$((mistakes + 1))
                continue
            fi
            selection="$default_pick"
        fi

        if [ "$short_list" = true ] && { [ "$selection" = "a" ] || [ "$selection" = "A" ]; }; then
            short_list=false
            shown=("${!candidates[@]}")
            list_pending=true
            continue
        fi

        if [ "$selection" = "0" ]; then
            echo "⚠  Continuing WITHOUT SSH key - git push operations will fail"
            echo ""
            break
        fi

        # At most three digits, so the range test never meets a number bash overflows.
        if [[ ! "$selection" =~ ^[1-9][0-9]{0,2}$ ]] || [ "$selection" -gt ${#shown[@]} ]; then
            echo "Invalid selection: $selection"
            echo "Please enter a number between 0 and ${#shown[@]}"
            echo ""
            mistakes=$((mistakes + 1))
            continue
        fi

        picked="${shown[$((selection-1))]}"
        is_pusher=false
        for p in "${pushers[@]}"; do
            [ "$p" = "$picked" ] && is_pusher=true
        done
        if [ ${#pushers[@]} -gt 0 ] && [ "$is_pusher" = false ]; then
            if [ "${probed[$picked]}" = yes ]; then
                echo "⚠  ${candidates[$picked]} cannot push to this remote ($remote_url):"
                echo "   the probe found no push access for it, so git push from this session will be refused."
            else
                echo "⚠  ${candidates[$picked]} was not checked for push access to this remote ($remote_url);"
                echo "   only github_ account keys are probed, and the probe found others that can push."
            fi
            if ! read -rp "Use it anyway? [y/N] " confirm; then
                echo "✗ Input closed before an SSH key was chosen." >&2
                return 1
            fi
            echo ""
            if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
                echo "Not using it — choose again."
                echo ""
                continue
            fi
        fi
        SSH_KEYS+=("${candidates[$picked]}")
        echo "✓ Selected: ${labels[$picked]}"
        echo ""
        break
    done

    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    return 0
}

# Probe GitHub for the identity a key (or the session's agent) authenticates as.
# Echoes it on success — a login for an account key, `owner/repo` for a deploy
# key — and on failure nothing on stdout, ssh's reply on stderr (returns 1, but callers run inside
# build_ssh_mounts_and_validate which is invoked as `|| exit 1`, so set -e is
# disabled — an empty result does not abort).
# CRITICAL isolation flags — without them the probe falls through to ~/.ssh/config's
# default `Host github.com` entry and/or the USER'S ssh-agent, returning the
# wrong account:
#   -F /dev/null          → ignore ~/.ssh/config
#   -o IdentitiesOnly=yes → only try the -i key
#   -o IdentityAgent=…    → ONLY ccy's private probe agent (below), never the
#                           user's; `none` when no probe agent is running
# The one deliberate exception is the SSH_AGENT_SENTINEL: then the user's agent
# IS the identity under test, so the probe signs through $SSH_AUTH_SOCK with
# whatever it holds and no -i at all.
# ConnectTimeout bounds the wait so a DROP-firewalled port 22 fails fast (~10s)
# instead of hanging on the default TCP timeout before any 443 fallback can run.
# It does not bound what follows the connection: an agent that asks before it
# signs (ssh-add -c, an expired gpg-agent cache) waits for a person. On an
# unattended launch (CCY_UNATTENDED_LAUNCH: a restart or a restore) there is
# nobody, so the whole probe is bounded and an expiry is reported as such.
_github_probe_identity() {
    local key="$1" host="$2" port="$3"
    local -a identity_opts guard=()
    if [ "$key" = "$SSH_AGENT_SENTINEL" ]; then
        identity_opts=(-o IdentitiesOnly=no -o IdentityAgent="${SSH_AUTH_SOCK:-none}")
    elif ccy_agent_filter_forwards "$key"; then
        # A key ccy forwards from the agent signs through the one-key agent; BatchMode so
        # that, should the agent refuse, ssh fails rather than asking for the file's passphrase.
        identity_opts=(-i "$key" -o IdentitiesOnly=yes -o IdentityAgent="$CCY_AGENT_FILTER_SOCK" -o BatchMode=yes)
    else
        identity_opts=(-i "$key" -o IdentitiesOnly=yes -o IdentityAgent="${CCY_PROBE_AGENT_SOCK:-none}")
    fi
    if [ "${CCY_UNATTENDED_LAUNCH:-false}" = true ]; then
        guard=(timeout --kill-after=5 "${CCY_UNATTENDED_PROBE_SECONDS:-60}")
    fi
    local out rc=0
    out=$("${guard[@]}" ssh -T "${identity_opts[@]}" \
        -F /dev/null \
        -o StrictHostKeyChecking=no \
        -o ConnectTimeout=10 \
        -p "$port" \
        "git@${host}" 2>&1) || rc=$?
    if [ "${#guard[@]}" -gt 0 ] && { [ "$rc" -eq 124 ] || [ "$rc" -eq 137 ]; }; then
        echo "  GitHub probe for ${key} got no answer within ${CCY_UNATTENDED_PROBE_SECONDS:-60}s; an agent that asks before signing cannot be answered on an unattended launch." >&2
        return 1
    fi
    if ! printf '%s\n' "$out" | grep -oP "Hi \K[^!]+"; then
        # A refused key, a dropped connection and an agent that would not sign all end
        # here; only ssh's own reply tells them apart.
        echo "  GitHub probe for ${key} at ${host}:${port} (ssh exit ${rc}): ${out:-no output}" >&2
        return 1
    fi
}

# ── Private probe agent: unlock passphrase keys BEFORE any connection exists ──
#
# ssh opens the TCP connection to GitHub FIRST and prompts for the key
# passphrase second, while the connection sits open. GitHub's sshd enforces a
# LoginGraceTime of ~2 minutes, so a prompt left waiting (user away from the
# keyboard at launch) outlives the connection: the passphrase is then accepted
# locally, auth fails instantly on the dead socket, and the port-22 failure is
# misread as "port 22 firewall-blocked" — triggering a spurious 443 fallback
# offer even though a prompt relaunch works fine.
#
# The fix is to collect the passphrase while NO connection is open: load each
# key into a private throwaway ssh-agent via ssh-add (which talks to no server,
# so the prompt can wait indefinitely), then let the probes sign via that
# agent. Bonus: ONE passphrase prompt per key for the whole validation instead
# of one per probe (the 22-then-443 fallback path used to prompt twice).
#
# The agent is PRIVATE — a fresh process holding only ccy's selected keys, on
# its own socket, never the user's SSH_AUTH_SOCK — so the account-isolation
# guarantee of IdentitiesOnly/-i is preserved. It is killed as soon as
# validation finishes (RETURN trap in build_ssh_mounts_and_validate).
#
# Sets: CCY_PROBE_AGENT_SOCK, CCY_PROBE_AGENT_PID (empty when unavailable)
CCY_PROBE_AGENT_SOCK=""
CCY_PROBE_AGENT_PID=""
_probe_agent_start() {
    CCY_PROBE_AGENT_SOCK=""
    CCY_PROBE_AGENT_PID=""
    command_exists ssh-agent || return 0

    local out
    if ! out=$(ssh-agent -s 2>&1); then
        # Not fatal: probes fall back to direct -i (pre-agent behaviour). Say
        # why, so a recurrence of the timeout misdiagnosis is explicable.
        echo "⚠ Could not start probe ssh-agent — passphrase prompts will hold a live"
        echo "  GitHub connection open. It said: $out"
        return 0
    fi
    CCY_PROBE_AGENT_SOCK=$(echo "$out" | grep -oP 'SSH_AUTH_SOCK=\K[^;]+')
    CCY_PROBE_AGENT_PID=$(echo "$out" | grep -oP 'SSH_AGENT_PID=\K[0-9]+')
    if [ -z "$CCY_PROBE_AGENT_SOCK" ] || [ -z "$CCY_PROBE_AGENT_PID" ]; then
        echo "⚠ Unrecognised ssh-agent output — probing without an agent."
        _probe_agent_stop
        return 0
    fi
    return 0
}

_probe_agent_stop() {
    local kill_out
    # A restore's probe passphrase copy is removed with the agent it unlocked keys into.
    ccy_restore_askpass_discard_probe
    if [ -n "$CCY_PROBE_AGENT_PID" ]; then
        if ! kill_out=$(kill "$CCY_PROBE_AGENT_PID" 2>&1); then
            # An already-gone agent is a normal teardown outcome, but say so
            # rather than swallowing it — this path must never mask the real
            # exit status of the function being torn down.
            echo "note: probe ssh-agent (pid $CCY_PROBE_AGENT_PID) was already gone: $kill_out"
        fi
    fi
    CCY_PROBE_AGENT_SOCK=""
    CCY_PROBE_AGENT_PID=""
    return 0
}

# Load one key into the probe agent, prompting for its passphrase with NO
# GitHub connection open. ssh-add itself allows 3 passphrase attempts per
# invocation; per the interactive-script rules a mistyped passphrase is a
# recoverable input error, so we re-offer the whole ssh-add up to 3 rounds
# before failing.
#
# With an askpass stage (a session restore on a headless server, see
# ccy_restore_askpass_stage) there is no person to ask: ssh-add is run once, through the
# stage's helper, with stdin closed, and a key the passphrase does not open fails at once.
_probe_agent_add_key() {
    local key="$1" askpass_dir="${2:-}" round
    if [ -n "$askpass_dir" ]; then
        SSH_AUTH_SOCK="$CCY_PROBE_AGENT_SOCK" SSH_ASKPASS="$askpass_dir/askpass" \
            SSH_ASKPASS_REQUIRE=force CCY_RESTORE_PP_FILE="$askpass_dir/pp" \
            ssh-add "$key" </dev/null
        return
    fi
    for round in 1 2 3; do
        if SSH_AUTH_SOCK="$CCY_PROBE_AGENT_SOCK" ssh-add "$key"; then
            return 0
        fi
        if [ "$round" -lt 3 ]; then
            echo ""
            echo "Key not unlocked: $key"
            if ccy_nobody_to_ask; then
                ccy_prompt_refuse ssh-passphrase-retry "Load the key into your ssh-agent first (ssh-add), or launch with a key that has no passphrase, or --no-ssh."
                return 1
            fi
            read -rp "$CCY_PROMPT_SSH_PASSPHRASE_RETRY (round $round of 3), or Ctrl+C to abort: " _unused
        fi
    done
    return 1
}

# ── Restore-only SSH_ASKPASS: a restored session on a headless server unlocks unattended ──
#
# After a reboot nobody is at a server to type a key's passphrase, so a restored session
# would stop at ssh-add's prompt twice: here, and in the container's entrypoint. ccy-sessions
# restore names a passphrase file (written by play-claude-yolo.yml from the vault's
# github_ssh_passphrase) and ssh-add is fed from it through SSH_ASKPASS, the way
# `run.bash --headless` loads the same key. ONLY then: an ordinary launch never sets
# SSH_ASKPASS (ccy_restore_passphrase_take refuses the file outside a restore).
#
# Each use gets its own copy in a fresh owner-only directory on XDG_RUNTIME_DIR (tmpfs), and
# each copy is removed once its keys are added: the probe's right after its unlock loop, the
# container's by the entrypoint (restore_askpass_finish) before Claude starts, with the
# launcher's cleanup as the backstop. The helper's text holds no secret, only reads
# $CCY_RESTORE_PP_FILE when ssh-add runs it, so the passphrase is never in argv or a log.
#
# The helper answers ONLY "Enter passphrase for ...", ssh-add's first question for a key. Real
# ssh-add asks "Bad passphrase, try again" of a wrong answer for ever (measured: OpenSSH 9.2
# asked 24 times in 5 seconds), so refusing the retry is what makes a wrong passphrase a
# failure instead of a hang. Any other question (a host key confirmation) is refused too.
CCY_RESTORE_ASKPASS_MOUNT="/run/ccy/restore-askpass"
CCY_PROBE_ASKPASS_DIR=""
CCY_RESTORE_ASKPASS_DIR=""

# ccy_restore_askpass_stage <passphrase-file> — stdout: a new stage directory holding `pp`
# (the copy, 0600) and `askpass` (the helper, 0700).
ccy_restore_askpass_stage() {
    local source_file="$1" dir
    if ! dir=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/ccy-askpass.XXXXXX"); then
        print_error "could not create a directory for the session-restore askpass under ${XDG_RUNTIME_DIR:-/tmp}"
        return 1
    fi
    if ! install -m 0600 -- "$source_file" "$dir/pp"; then
        print_error "could not copy the session-restore passphrase file $source_file"
        rm -rf -- "$dir"
        return 1
    fi
    if ! cat >"$dir/askpass" <<'CCY_ASKPASS_BODY'; then
#!/bin/sh
# ccy session-restore askpass. Answers ssh-add's first passphrase question for a key from
# $CCY_RESTORE_PP_FILE, and nothing else: a retry means the passphrase was wrong, and asking
# again would loop for ever.
case "$1" in
"Enter passphrase for "*) exec cat -- "${CCY_RESTORE_PP_FILE:?ccy restore askpass: no passphrase file named}" ;;
esac
echo "ccy restore askpass: not answering: $1" >&2
exit 1
CCY_ASKPASS_BODY
        print_error "could not write the session-restore askpass helper in $dir"
        rm -rf -- "$dir"
        return 1
    fi
    if ! chmod 0700 "$dir/askpass"; then
        print_error "could not make the session-restore askpass helper executable"
        rm -rf -- "$dir"
        return 1
    fi
    printf '%s\n' "$dir"
}

# ccy_restore_askpass_discard_probe — remove the probe's stage, if there is one.
ccy_restore_askpass_discard_probe() {
    [ -n "$CCY_PROBE_ASKPASS_DIR" ] || return 0
    if ! rm -rf -- "$CCY_PROBE_ASKPASS_DIR"; then
        print_error "could not remove the session-restore passphrase copy in $CCY_PROBE_ASKPASS_DIR"
        return 1
    fi
    CCY_PROBE_ASKPASS_DIR=""
}

# ccy_restore_askpass_container <passphrase-file> — stage the container's copy, record it in
# CCY_RESTORE_ASKPASS_DIR (the launcher's cleanup removes it), and append its mount to
# SSH_RUN_OPTS. The mount alone is the signal: the entrypoint finds the helper there and
# sets SSH_ASKPASS for its own ssh-add only. No variable goes into the container's
# configuration, which every later `podman exec` would inherit. Paths only; the passphrase
# never reaches the engine's argv. Call after build_ssh_mounts_and_validate, which resets
# SSH_RUN_OPTS.
ccy_restore_askpass_container() {
    local relabel=""
    CCY_RESTORE_ASKPASS_DIR=$(ccy_restore_askpass_stage "$1") || return 1
    # Read-write: the entrypoint removes the copy and the helper once its keys are added.
    if [ "${CCY_SELINUX_MODE:-off}" != "off" ]; then
        relabel=":Z"
    fi
    SSH_RUN_OPTS+=(-v "$CCY_RESTORE_ASKPASS_DIR:$CCY_RESTORE_ASKPASS_MOUNT$relabel")
}

# ── An ordinary launch: the user's own SSH_ASKPASS answers for ONE key ──────────────────
#
# A key file is unlocked twice, here and again in the container, and a person at the keyboard
# types the passphrase both times. When SSH_ASKPASS names an executable (the one ssh-add itself
# would run) and exactly one encrypted key file is selected, that program is asked once and its
# answer goes through the same stage a session restore uses: the host's unlock, then the
# container's, with nobody asked. The answer lives in an owner-only file on the runtime
# directory for the length of the launch and is never in argv or the environment. Several keys,
# a forwarded agent, a passphrase-less key, a headless launch or a helper that gives no answer
# leave the ordinary prompting exactly as it was.
CCY_SUPPLIED_PP_FILE=""

# Whether a person can answer on this launch's terminal; a function so a test can say.
ccy_has_terminal() { [ -t 0 ]; }

# ccy_askpass_passphrase_supply — sets RESTORE_SSH_PASSPHRASE_FILE when the helper answered.
# Call after the keys are selected and before build_ssh_mounts_and_validate.
ccy_askpass_passphrase_supply() {
    [ -z "$RESTORE_SSH_PASSPHRASE_FILE" ] || return 0
    { [ -n "${SSH_ASKPASS:-}" ] && [ -x "$SSH_ASKPASS" ]; } || return 0
    { ccy_has_terminal && [ "${HEADLESS_MODE:-false}" != "true" ]; } || return 0
    [ ${#SSH_KEYS[@]} -eq 1 ] || return 0
    local key="${SSH_KEYS[0]}"
    [ "$key" != "$SSH_AGENT_SENTINEL" ] || return 0
    # A key ccy forwards from the agent is unlocked there already.
    ! ccy_agent_filter_forwards "$key" || return 0

    # An empty passphrase opens a key that has none: nothing to supply.
    local unlock_probe
    if unlock_probe=$(ssh-keygen -y -P '' -f "$key" 2>&1); then
        return 0
    fi
    # Only an encrypted key is worth asking for: a missing or unreadable file fails differently.
    [[ "$unlock_probe" == *passphrase* ]] || return 0

    local pp_file pp_dir="${XDG_RUNTIME_DIR:-/tmp}" gpg_tty="${GPG_TTY:-}"
    if ! pp_file=$(mktemp "$pp_dir/ccy-pp.XXXXXX"); then
        print_error "could not create a file for the passphrase under $pp_dir"
        return 1
    fi
    # A helper that asks through gpg needs the terminal named; tmux panes do not inherit it.
    [ -n "$gpg_tty" ] || gpg_tty=$(tty) || gpg_tty=""
    if ! GPG_TTY="$gpg_tty" SSH_ASKPASS_REQUIRE=force "$SSH_ASKPASS" "Enter passphrase for $key: " \
            >"$pp_file" </dev/null || [ ! -s "$pp_file" ]; then
        rm -f -- "$pp_file"
        echo "note: your SSH_ASKPASS helper gave no passphrase for $key; ssh-add will ask."
        return 0
    fi
    CCY_SUPPLIED_PP_FILE="$pp_file"
    RESTORE_SSH_PASSPHRASE_FILE="$pp_file"
    echo "✓ SSH key passphrase supplied by your SSH_ASKPASS helper — no prompt, here or in the container"
}

# ccy_askpass_passphrase_discard — remove the supplied answer, if there is one. Safe to repeat.
ccy_askpass_passphrase_discard() {
    [ -n "$CCY_SUPPLIED_PP_FILE" ] || return 0
    if ! rm -f -- "$CCY_SUPPLIED_PP_FILE"; then
        print_error "could not remove the supplied passphrase file $CCY_SUPPLIED_PP_FILE"
        return 1
    fi
    if [ "$RESTORE_SSH_PASSPHRASE_FILE" = "$CCY_SUPPLIED_PP_FILE" ]; then
        RESTORE_SSH_PASSPHRASE_FILE=""
    fi
    CCY_SUPPLIED_PP_FILE=""
}

# ── Key files the person's agent already holds: forward those keys alone (Plan 00163) ──
#
# A key file with a passphrase is unlocked by a person, twice; a headless launch, a restart
# and a restore have nobody to ask. When the session's ssh-agent already holds that key,
# ccy forwards it from the agent instead of mounting the file: through ccy's one-key agent
# (ssh_agent_filter.py, beside this library), which lists and signs with the selected keys
# only and refuses everything else, so the container never reaches the agent's other keys,
# and can neither add, remove nor lock any. It is mounted where --ssh-agent mounts the whole
# agent. SSH_KEYS keeps the key files, so Quick Launch, the session record and a restart name
# them as before, and each launch decides afresh from what the agent holds then.
#
# Which keys: ccy_agent_forward_select, the one decision the launch, its unattended checks
# (ccy_restart_keys_unattended) and Plan 00161's acceptance all take.
#
# The filter lives as long as the launcher: started here, stopped by ccy_agent_filter_stop
# from the launcher's EXIT trap and cleanup (a restart runs cleanup before its exec), and it
# stops by itself when the launcher is gone (a SIGKILL runs no trap).
CCY_AGENT_FILTER_HELPER="$(dirname "${BASH_SOURCE[0]}")/ssh_agent_filter.py"
CCY_AGENT_FILTER_KEYS=()
CCY_AGENT_FILTER_DIR=""
CCY_AGENT_FILTER_SOCK=""
CCY_AGENT_FILTER_PID=""
CCY_AGENT_KEY_FINGERPRINT=""
CCY_AGENT_FORWARD_KEYS=()
CCY_AGENT_FORWARD_FPS=()
CCY_AGENT_FILTER_START_TRIES=100

# _ssh_fingerprint_of <file> — stdout: the SHA256 fingerprint ssh-keygen -l prints for it.
_ssh_fingerprint_of() {
    local listing
    listing=$(ssh-keygen -E sha256 -lf "$1" 2>&1) || return 1
    awk 'NR == 1 && $2 ~ /^SHA256:/ { print $2; found = 1 } END { exit !found }' <<<"$listing"
}

# ssh_key_fingerprint <key-file> — stdout: its SHA256 fingerprint, from the private key file
# itself: an OpenSSH private key keeps its public half unencrypted, so no passphrase is asked.
# ssh-keygen -l prefers a .pub beside the file it is given, so it is given a link to the key
# in a directory holding nothing else. A .pub beside the key must agree: one that names
# another key would forward that other key, so the key is then not matched at all.
ssh_key_fingerprint() {
    local key="$1" dir target fingerprint public_fp rc=0
    target=$(realpath -e -- "$key" 2>&1) || return 1
    dir=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/ccy-keyfp.XXXXXX") || return 1
    if ln -s -- "$target" "$dir/key"; then
        fingerprint=$(_ssh_fingerprint_of "$dir/key") || rc=1
    else
        rc=1
    fi
    rm -rf -- "$dir" || return 1
    [ "$rc" -eq 0 ] || return 1
    if [ -e "$key.pub" ]; then
        if ! public_fp=$(_ssh_fingerprint_of "$key.pub") || [ "$public_fp" != "$fingerprint" ]; then
            echo "note: $key.pub is not the public half of $key (the key file is $fingerprint), so ccy does not look for $key in your ssh-agent" >&2
            return 1
        fi
    fi
    printf '%s\n' "$fingerprint"
}

# ssh_key_needs_passphrase <key-file> — status 0 when the empty passphrase is refused as a
# wrong one. A key with none, or a missing or unreadable file, is not one that needs it.
ssh_key_needs_passphrase() {
    local probe
    if probe=$(ssh-keygen -y -P '' -f "$1" 2>&1); then
        return 1
    fi
    [[ "$probe" == *passphrase* ]]
}

# ccy_agent_forwards_key <key-file> — whether this key could be forwarded from the agent at
# SSH_AUTH_SOCK instead of mounting the file: it needs a passphrase, and the agent holds a
# key with its fingerprint. Sets CCY_AGENT_KEY_FINGERPRINT. One key's half of the decision;
# ccy_agent_forward_select makes it for the selection.
ccy_agent_forwards_key() {
    local key="$1" fingerprint listed rc=0
    CCY_AGENT_KEY_FINGERPRINT=""
    [ "$key" != "$SSH_AGENT_SENTINEL" ] || return 1
    [ -n "${SSH_AUTH_SOCK:-}" ] || return 1
    ssh_key_needs_passphrase "$key" || return 1
    fingerprint=$(ssh_key_fingerprint "$key") || return 1
    listed=$(ssh-add -l -E sha256 2>&1) || rc=$?
    [ "$rc" -eq 0 ] || return 1
    awk -v fp="$fingerprint" '$2 == fp { found = 1 } END { exit !found }' <<<"$listed" || return 1
    CCY_AGENT_KEY_FINGERPRINT="$fingerprint"
}

# ccy_agent_forward_select <ssh-keys...> — THE decision, for the whole selection: which key
# files are forwarded from the agent. Each one that needs a passphrase and that the agent
# holds is, PROVIDED that leaves no selected key needing a passphrase: every other key is
# mounted as a file, and with an agent forwarded nothing in the container unlocks a file, so
# a mixed selection would still stop at a prompt. Then nothing is forwarded and the launch
# unlocks every file as it always has. A selection naming the whole agent (SSH_AGENT_SENTINEL)
# forwards no key this way. Sets CCY_AGENT_FORWARD_KEYS and CCY_AGENT_FORWARD_FPS.
ccy_agent_forward_select() {
    CCY_AGENT_FORWARD_KEYS=()
    CCY_AGENT_FORWARD_FPS=()
    local key
    local -a keys=() fps=()
    for key in "$@"; do
        [ "$key" != "$SSH_AGENT_SENTINEL" ] || return 0
    done
    for key in "$@"; do
        if ccy_agent_forwards_key "$key"; then
            keys+=("$key")
            fps+=("$CCY_AGENT_KEY_FINGERPRINT")
        elif ssh_key_needs_passphrase "$key"; then
            return 0
        fi
    done
    CCY_AGENT_FORWARD_KEYS=("${keys[@]}")
    CCY_AGENT_FORWARD_FPS=("${fps[@]}")
}

# ccy_agent_filter_forwards <key-file> — status 0 when the running one-key agent forwards it.
ccy_agent_filter_forwards() {
    local forwarded
    for forwarded in "${CCY_AGENT_FILTER_KEYS[@]}"; do
        [ "$forwarded" = "$1" ] && return 0
    done
    return 1
}

# ccy_agent_filter_start — when ccy_agent_forward_select forwards any selected key, start the
# one-key agent for those keys and set CCY_AGENT_FILTER_KEYS/_SOCK/_PID/_DIR. Otherwise sets
# nothing and returns 0. A filter that was wanted and did not start fails.
ccy_agent_filter_start() {
    [ -z "$CCY_AGENT_FILTER_PID" ] || return 0
    ccy_agent_forward_select "${SSH_KEYS[@]}"
    [ ${#CCY_AGENT_FORWARD_KEYS[@]} -gt 0 ] || return 0
    local key names="" fingerprint
    local -a allow=()
    for key in "${CCY_AGENT_FORWARD_KEYS[@]}"; do
        names+="${names:+, }$(basename "$key")"
    done
    for fingerprint in "${CCY_AGENT_FORWARD_FPS[@]}"; do
        allow+=(--allow "$fingerprint")
    done

    if [ ! -f "$CCY_AGENT_FILTER_HELPER" ]; then
        print_error "ccy's one-key agent is not installed at $CCY_AGENT_FILTER_HELPER"
        echo "  Re-run playbooks/imports/play-claude-yolo.yml, which installs it." >&2
        return 1
    fi
    if ! command -v python3 >/dev/null; then
        print_error "python3 not found: ccy's one-key agent needs it to forward $names from your ssh-agent"
        return 1
    fi
    if ! CCY_AGENT_FILTER_DIR=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/ccy-agent.XXXXXX"); then
        print_error "could not create a directory for ccy's one-key agent under ${XDG_RUNTIME_DIR:-/tmp}"
        CCY_AGENT_FILTER_DIR=""
        return 1
    fi
    CCY_AGENT_FILTER_SOCK="$CCY_AGENT_FILTER_DIR/agent.sock"
    python3 -I "$CCY_AGENT_FILTER_HELPER" --listen "$CCY_AGENT_FILTER_SOCK" \
        --upstream "$SSH_AUTH_SOCK" "${allow[@]}" --parent-pid "$$" \
        </dev/null >/dev/null 2>"$CCY_AGENT_FILTER_DIR/log" &
    CCY_AGENT_FILTER_PID=$!

    local tries=0 alive listed rc=0
    while [ ! -S "$CCY_AGENT_FILTER_SOCK" ]; do
        if ! alive=$(kill -0 "$CCY_AGENT_FILTER_PID" 2>&1) || [ "$tries" -ge "$CCY_AGENT_FILTER_START_TRIES" ]; then
            print_error "ccy's one-key agent did not start for $names${alive:+ ($alive)}. It said:"
            cat -- "$CCY_AGENT_FILTER_DIR/log" >&2
            ccy_agent_filter_stop
            return 1
        fi
        tries=$((tries + 1))
        sleep 0.1
    done
    listed=$(SSH_AUTH_SOCK="$CCY_AGENT_FILTER_SOCK" ssh-add -l -E sha256 2>&1) || rc=$?
    for fingerprint in "${CCY_AGENT_FORWARD_FPS[@]}"; do
        if [ "$rc" -ne 0 ] || ! awk -v fp="$fingerprint" '$2 == fp { found = 1 } END { exit !found }' <<<"$listed"; then
            print_error "ccy's one-key agent does not offer every key of $names (ssh-add -l through it: $listed)"
            cat -- "$CCY_AGENT_FILTER_DIR/log" >&2
            ccy_agent_filter_stop
            return 1
        fi
    done
    CCY_AGENT_FILTER_KEYS=("${CCY_AGENT_FORWARD_KEYS[@]}")
    if [ ${#CCY_AGENT_FILTER_KEYS[@]} -eq 1 ]; then
        echo "✓ $names needs a passphrase and your ssh-agent holds it: forwarding that one key from the agent (no prompt; the agent's other keys stay out of the container)"
    else
        echo "✓ $names need passphrases and your ssh-agent holds them: forwarding those keys alone from the agent (no prompt; the agent's other keys stay out of the container)"
    fi
}

# ccy_agent_filter_stop — stop the one-key agent and remove its directory. Safe to repeat.
ccy_agent_filter_stop() {
    local out rc=0
    if [ -n "$CCY_AGENT_FILTER_PID" ]; then
        if out=$(kill "$CCY_AGENT_FILTER_PID" 2>&1); then
            wait "$CCY_AGENT_FILTER_PID" || rc=$?
            if [ "$rc" -ne 0 ]; then
                echo "note: ccy's one-key agent (pid $CCY_AGENT_FILTER_PID) exited with status $rc" >&2
            fi
        else
            echo "note: ccy's one-key agent (pid $CCY_AGENT_FILTER_PID) was already gone: $out" >&2
        fi
    fi
    if [ -n "$CCY_AGENT_FILTER_DIR" ] && ! rm -rf -- "$CCY_AGENT_FILTER_DIR"; then
        print_error "could not remove ccy's one-key agent directory $CCY_AGENT_FILTER_DIR"
        return 1
    fi
    CCY_AGENT_FILTER_PID=""
    CCY_AGENT_FILTER_DIR=""
    CCY_AGENT_FILTER_SOCK=""
    CCY_AGENT_FILTER_KEYS=()
}

# _agent_public_for_key <agent-socket> <key-file> — stdout: "type base64" of the identity the
# agent lists with the key file's fingerprint; status 1 when it lists none.
_agent_public_for_key() {
    local sock="$1" key="$2" fingerprint listed line line_fp
    fingerprint=$(ssh_key_fingerprint "$key") || return 1
    listed=$(SSH_AUTH_SOCK="$sock" ssh-add -L 2>&1) || return 1
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        if ! line_fp=$(ssh-keygen -E sha256 -lf - <<<"$line" 2>&1); then
            echo "note: skipping an identity the agent lists that ssh-keygen cannot read: $line_fp" >&2
            continue
        fi
        read -r _ line_fp _ <<<"$line_fp"
        if [ "$line_fp" = "$fingerprint" ]; then
            read -r line_fp line _ <<<"$line"
            printf '%s %s' "$line_fp" "$line"
            return 0
        fi
    done <<<"$listed"
    return 1
}

# _probe_unlock_keys <tool_name> — unlock every selected key file into the private probe
# agent, BEFORE any GitHub connection is opened (see _probe_agent_start). Requires SSH_KEYS;
# RESTORE_SSH_PASSPHRASE_FILE is set only on a server's session restore. Ordinarily it asks
# the person, and is skipped when no terminal can answer (headless/CI): a passphrase-less key
# needs no agent and an encrypted one could not be unlocked anyway. On a restore it asks
# nobody: every key unlocks through askpass, or the launch fails here, loudly.
_probe_unlock_keys() {
    local tool_name="$1" unlock_key restore=false
    [ -n "${RESTORE_SSH_PASSPHRASE_FILE:-}" ] && restore=true
    if [ "$restore" = false ] && { [ ! -t 0 ] || [ "${HEADLESS_MODE:-false}" = "true" ]; }; then
        return 0
    fi
    if [ "$restore" = true ] && ! ccy_restore_passphrase_check "$RESTORE_SSH_PASSPHRASE_FILE"; then
        return 1
    fi
    _probe_agent_start
    if [ -z "$CCY_PROBE_AGENT_SOCK" ]; then
        if [ "$restore" = true ]; then
            print_error "no probe ssh-agent, so this restored session cannot unlock its SSH key unattended."
            return 1
        fi
        return 0
    fi
    if [ "$restore" = true ]; then
        CCY_PROBE_ASKPASS_DIR=$(ccy_restore_askpass_stage "$RESTORE_SSH_PASSPHRASE_FILE") || return 1
    fi
    for unlock_key in "${SSH_KEYS[@]}"; do
        # The session's agent is already unlocked by definition.
        [ "$unlock_key" = "$SSH_AGENT_SENTINEL" ] && continue
        # So is a key ccy forwards from it.
        ccy_agent_filter_forwards "$unlock_key" && continue
        if ! _probe_agent_add_key "$unlock_key" "$CCY_PROBE_ASKPASS_DIR"; then
            print_error "Could not unlock SSH key: $unlock_key"
            if [ -n "$CCY_SUPPLIED_PP_FILE" ]; then
                echo "The passphrase your SSH_ASKPASS helper gave does not open this key." >&2
                ccy_restore_askpass_discard_probe
            elif [ "$restore" = true ]; then
                echo "The session-restore passphrase (github_ssh_passphrase in host_vars) does not open this key." >&2
                ccy_restore_askpass_discard_probe
            else
                echo "The passphrase was not accepted. Re-run $tool_name to try again."
            fi
            return 1
        fi
    done
    ccy_restore_askpass_discard_probe
}

# Echoes the GitHub login that a token belongs to. Retries, and VALIDATES that
# the answer actually looks like a login.
#
# Both halves matter, and neither was here before. The caller used to run
#
#     token_user=$(GH_TOKEN=… gh api user --jq .login 2>/dev/null)
#
# and compare whatever came back — discarding the exit status entirely. During a
# GitHub blip the API answers 502 with a JSON body, gh writes that body to
# stdout, and the blob was then treated as an account name. The result was a
# mangled report telling the user their github_accounts mapping was wrong and to
# go and edit localhost.yml. The mapping was fine; GitHub was down. A check that
# misdiagnoses an outage as a config error is worse than no check.
#
# Results come back in GLOBALS, not on stdout, and deliberately so: a caller
# using `login=$(resolve_token_owner_login …)` would run this in a subshell,
# where the failure detail assigned below could never reach it — the caller
# would print "(no output)" in place of the diagnosis, which is the whole point
# of the message. Globals match how the rest of this file returns values
# (SSH_KEYS, GITHUB_USERNAME, GH_TOKEN).
#
# Args: $1 = token
# Sets: TOKEN_OWNER_LOGIN on success, TOKEN_OWNER_LOOKUP_ERROR on failure
# Returns: 0 on success, 1 on failure
TOKEN_OWNER_LOGIN=""
TOKEN_OWNER_LOOKUP_ERROR=""
resolve_token_owner_login() {
    local token="$1"
    local out attempt
    local attempts="${CCY_TOKEN_OWNER_ATTEMPTS:-3}"

    TOKEN_OWNER_LOGIN=""
    TOKEN_OWNER_LOOKUP_ERROR=""

    for (( attempt = 1; attempt <= attempts; attempt++ )); do
        if out="$(GH_TOKEN="$token" gh api user --jq .login 2>&1)"; then
            # A login is the only acceptable answer. A JSON error body, an HTML
            # error page, or empty output all mean the lookup did not succeed —
            # whatever the exit status claimed.
            if [[ "$out" =~ ^[A-Za-z0-9][A-Za-z0-9-]*$ ]]; then
                TOKEN_OWNER_LOGIN="$out"
                return 0
            fi
        fi
        TOKEN_OWNER_LOOKUP_ERROR="$out"
        if [ "$attempt" -lt "$attempts" ]; then
            sleep "${CCY_TOKEN_OWNER_RETRY_DELAY:-$attempt}"
        fi
    done

    return 1
}

# ccy_github_443_answer — whether to route GitHub SSH over 443 for this session, when port 22
# failed and 443 works: status 0 for yes. With nobody to ask (ccy_launch_unattended) it is
# yes, the only way the launch can go on; a person is asked, and only n declines.
ccy_github_443_answer() {
    if ccy_launch_unattended; then
        echo "  Nobody to ask (headless, a restore, a restart or no terminal) — enabling 443 automatically (the only way to proceed)."
        return 0
    fi
    local reply_443=""
    if ! read -rp "$CCY_PROMPT_GITHUB_443 " reply_443; then
        echo "" >&2
        print_error "No answer: the input closed at the GitHub-over-443 question."
        return 1
    fi
    case "$reply_443" in
    [Nn]*) return 1 ;;
    *) return 0 ;;
    esac
}

# Function to build SSH mounts and validate GitHub connection
# Args: $1 = tool_name (for display)
# Requires: SSH_KEYS global array (paths, or SSH_AGENT_SENTINEL for the session's agent)
# Sets: SSH_MOUNTS, SSH_KEY_PATHS, SSH_RUN_OPTS, SSH_AGENT_FORWARDED,
#       SSH_CONFIG_EXTRA_B64, SSH_KNOWN_HOSTS_PINS, GITHUB_USERNAME, GH_TOKEN
# Returns: 0 on success, exits on error
build_ssh_mounts_and_validate() {
    local tool_name="$1"

    # Build SSH key mount arguments and extract GitHub account
    # This needs to happen early so GH_TOKEN is available for create_token
    SSH_MOUNTS=()
    SSH_KEY_PATHS=()
    SSH_RUN_OPTS=()
    SSH_AGENT_FORWARDED=0
    SSH_CONFIG_EXTRA_B64=""
    SSH_KNOWN_HOSTS_PINS=""
    export SSH_AGENT_FORWARDED SSH_CONFIG_EXTRA_B64 SSH_KNOWN_HOSTS_PINS
    GITHUB_USERNAME=""

    # BSH-06: the PRIMARY key (index 0) defines the container identity — both
    # GITHUB_USERNAME and the gh-token alias below are derived from it. Additional
    # keys are still mounted and connectivity-verified, but they must NOT overwrite
    # GITHUB_USERNAME: GH_TOKEN comes from key 0's alias, so letting a later key win
    # would pair a key-0 token with a key-N username and the container entrypoint
    # would hard-fail on a token/identity mismatch.
    local primary_user=""

    # Unlock every selected key into the private probe agent BEFORE any GitHub
    # connection is opened — see _probe_agent_start for why (GitHub's ~2-minute
    # LoginGraceTime vs a passphrase prompt left waiting). The RETURN trap
    # guarantees teardown on every exit path from this function, success or
    # failure. _probe_unlock_keys says when nothing is unlocked, and how a
    # restored session on a server unlocks with nobody to ask.
    if [ ${#SSH_KEYS[@]} -gt 0 ]; then
        trap '_probe_agent_stop' RETURN
        _probe_unlock_keys "$tool_name" || return 1
    fi

    # The project's alias, if any: its stanza must reach the container whichever
    # identity is chosen, or the remote URL cannot even be fetched inside.
    local alias_rc=0
    detect_project_github_alias "." || alias_rc=$?
    if [ "$alias_rc" -eq 2 ]; then
        return 1
    fi
    local alias_container_key=""
    local primary_key=""

    for i in "${!SSH_KEYS[@]}"; do
        local key="${SSH_KEYS[$i]}"
        local key_label
        if [ "$key" = "$SSH_AGENT_SENTINEL" ]; then
            if ! ssh_agent_usable; then
                print_error "--ssh-agent: no usable ssh-agent in this session"
                echo "  SSH_AUTH_SOCK=${SSH_AUTH_SOCK:-(unset)}"
                echo "  ssh-add -l said: ${SSH_AGENT_PROBE_OUTPUT:-(nothing)}"
                echo "Log in with agent forwarding (ssh -A), or load a key with ssh-add, then retry."
                return 1
            fi
            # SELinux: container_t may not connect to a socket served by the
            # unconfined agent process, and relabelling the socket file does not
            # change that (measured: `:z` moved it to container_file_t, connect
            # still denied). Labelling is disabled for THIS container only.
            SSH_AGENT_FORWARDED=1
            SSH_RUN_OPTS+=("-v" "$SSH_AUTH_SOCK:/run/ccy/ssh-agent"
                           "-e" "SSH_AUTH_SOCK=/run/ccy/ssh-agent"
                           "--security-opt" "label=disable")
            key_label="ssh-agent"
        elif ccy_agent_filter_forwards "$key"; then
            # The one-key agent, mounted once however many keys it forwards, as the whole
            # agent is above, and for the same SELinux reason: an unconfined process serves it.
            if [ "$SSH_AGENT_FORWARDED" != "1" ]; then
                SSH_AGENT_FORWARDED=1
                SSH_RUN_OPTS+=("-v" "$CCY_AGENT_FILTER_SOCK:/run/ccy/ssh-agent"
                               "-e" "SSH_AUTH_SOCK=/run/ccy/ssh-agent"
                               "--security-opt" "label=disable")
            fi
            key_label="key $(basename "$key"), from your ssh-agent"
        else
            local container_key_path="/root/.ssh/key_$i"
            if [ "${CCY_SELINUX_MODE:-off}" != "off" ]; then
                # container_t may not read ssh_home_t, and a relabel IN PLACE would
                # change the label of the person's own key file. So the key is
                # copied into a per-session tmpfs directory (owner-only, removed by
                # cleanup) and THAT directory is mounted with the private relabel.
                if [ -z "${CCY_KEY_STAGE_DIR:-}" ]; then
                    if ! CCY_KEY_STAGE_DIR=$(mktemp -d "${XDG_RUNTIME_DIR:-/tmp}/ccy-keys.XXXXXX"); then
                        print_error "Could not create a staging directory for SSH keys under ${XDG_RUNTIME_DIR:-/tmp}"
                        return 1
                    fi
                    export CCY_KEY_STAGE_DIR
                    SSH_MOUNTS+=("-v" "$CCY_KEY_STAGE_DIR:/root/.ssh/ccy-keys:ro,Z")
                fi
                if ! install -m 0600 "$key" "$CCY_KEY_STAGE_DIR/key_$i"; then
                    print_error "Could not stage SSH key $key into $CCY_KEY_STAGE_DIR"
                    return 1
                fi
                container_key_path="/root/.ssh/ccy-keys/key_$i"
            else
                SSH_MOUNTS+=("-v" "$key:$container_key_path:ro")
            fi
            SSH_KEY_PATHS+=("$container_key_path")
            key_label="key $(basename "$key")"
            if [ "$alias_rc" -eq 0 ] && [ "$key" = "$GITHUB_ALIAS_KEY" ]; then
                alias_container_key="$container_key_path"
            fi
        fi

        # Probe for the identity. GITHUB_SSH_443=1 (the --github-443 flag, or an
        # accepted auto-fallback below) routes over ssh.github.com:443; otherwise
        # the standard github.com:22. ssh.github.com:443 serves the same host keys.
        # The project's alias key is probed where its alias points, because that
        # is the only endpoint the box was configured to reach it on.
        local gh_ssh_host="github.com" gh_ssh_port="22"
        if [ "${GITHUB_SSH_443:-0}" = "1" ]; then
            gh_ssh_host="ssh.github.com"
            gh_ssh_port="443"
        fi
        local key_is_alias=false
        if [ "$alias_rc" -eq 0 ] && [ "$key" = "$GITHUB_ALIAS_KEY" ]; then
            key_is_alias=true
            gh_ssh_host="$GITHUB_ALIAS_HOSTNAME"
            gh_ssh_port="$GITHUB_ALIAS_PORT"
        fi
        local detected_user
        detected_user=$(_github_probe_identity "$key" "$gh_ssh_host" "$gh_ssh_port")

        # Auto-fallback: only on the PRIMARY key, only when not already on 443.
        # If port 22 failed but ssh.github.com:443 authenticates, port 22 is
        # firewall-blocked — offer to enable 443 for THIS session. GITHUB_SSH_443
        # propagates to the container env + entrypoint, and to subsequent keys in
        # this loop (they recompute the endpoint from it each iteration).
        if [ -z "$detected_user" ] && [ "$i" -eq 0 ] && [ "$key_is_alias" = false ] && [ "${GITHUB_SSH_443:-0}" != "1" ]; then
            local user_443
            user_443=$(_github_probe_identity "$key" "ssh.github.com" "443")
            if [ -n "$user_443" ]; then
                echo ""
                echo "⚠ GitHub SSH on port 22 failed, but ssh.github.com:443 works (authenticated as $user_443)."
                echo "  Port 22 is likely blocked on this network."
                echo "  443 mode routes all GitHub SSH over ssh.github.com:443 for THIS ccy session."
                if ccy_github_443_answer; then
                    export GITHUB_SSH_443=1
                    gh_ssh_host="ssh.github.com"
                    gh_ssh_port="443"
                    detected_user="$user_443"
                    echo "✓ GitHub SSH over 443 enabled for this session"
                else
                    print_error "GitHub SSH over port 22 is blocked and 443 mode was declined."
                    echo "Re-run with 443 enabled:  ccy --github-443"
                    return 1
                fi
            fi
        fi

        if [ -z "$detected_user" ]; then
            if [ "$key" = "$SSH_AGENT_SENTINEL" ]; then
                print_error "None of the ssh-agent's keys authenticates to GitHub ($gh_ssh_host:$gh_ssh_port)"
                echo ""
                echo "  ssh-add -l: ${SSH_AGENT_PROBE_OUTPUT}"
                echo ""
                echo "Load the key GitHub knows into the agent, or pick a key file instead."
                return 1
            fi
            print_error "SSH key authentication to GitHub failed: $key"
            if ccy_agent_filter_forwards "$key" && [ -f "$CCY_AGENT_FILTER_DIR/log" ]; then
                # Removed when ccy stops, and the only record of what the agent answered.
                echo "  ccy's one-key agent, which signed for $(basename "$key") through your ssh-agent, logged:"
                cat -- "$CCY_AGENT_FILTER_DIR/log"
            fi
            echo ""
            echo "ssh's reply is printed above. 'Permission denied (publickey)' means GitHub does"
            echo "not know this key; a timeout or a closed connection means the network, so try again."
            echo ""
            echo "If GitHub does not know the key:"
            echo "  1. Go to https://github.com/settings/keys"
            echo "  2. Click 'New SSH key'"
            echo "  3. Add the public key from: $key.pub"
            echo ""
            echo "Or set up GitHub keys with:"
            echo "  ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
            return 1
        fi

        if github_identity_is_deploy_key "$detected_user"; then
            # Repository-scoped; it names no account, so it can never be the
            # container's identity and never maps to a gh-token-<alias>.
            echo "✓ Deploy key for $detected_user ($key_label) — repository-scoped, no account identity"
        elif [ -z "$primary_user" ]; then
            primary_user="$detected_user"
            primary_key="$key"
            echo "✓ GitHub account ($key_label): $detected_user"
        else
            echo "✓ Additional identity ($key_label) authenticates as: $detected_user"
            if [ "$detected_user" != "$primary_user" ]; then
                echo "  note: this maps to a different account; the container"
                echo "        will use the primary account ($primary_user)."
            fi
        fi
    done

    # Identity is the first ACCOUNT identity's login (see BSH-06 note above); a
    # deploy key ahead of it in the list does not count.
    GITHUB_USERNAME="$primary_user"

    # The alias stanza for the container. With the alias key mounted it names the
    # key; with an agent forwarded it leaves IdentitiesOnly out so the agent's
    # identities are tried first and a push authenticates as the person; with
    # neither it carries no IdentityFile and the container's own agent (holding
    # the mounted account key) answers for the alias.
    if [ "$alias_rc" -eq 0 ]; then
        local identities_only="yes"
        if [ "$SSH_AGENT_FORWARDED" = "1" ]; then
            identities_only="no"
        fi
        compose_ssh_alias_exports "$GITHUB_ALIAS_HOST" "$GITHUB_ALIAS_HOSTNAME" "$GITHUB_ALIAS_PORT" \
            "$alias_container_key" "$identities_only"
    fi

    # Get GitHub token from gh CLI
    if ! command_exists gh; then
        print_error "gh (GitHub CLI) not found"
        echo "Install it with: ansible-playbook playbooks/imports/play-git-configure-and-tools.yml"
        return 1
    fi

    # An account identity from a github_<alias> key has an account-specific token
    # function; this requires play-github-cli-multi.yml to be configured and the
    # shell reloaded. Any other identity (agent, deploy key, none) takes the
    # caller's GH_TOKEN, else the active gh login's token.
    local key_basename=""
    if [ -n "$primary_key" ] && [ "$primary_key" != "$SSH_AGENT_SENTINEL" ]; then
        key_basename=$(basename "$primary_key")
    fi
    if [ -n "$GITHUB_USERNAME" ] && [[ "$key_basename" =~ ^github_(.+)$ ]]; then
        {
            local alias
            alias="${BASH_REMATCH[1]}"
            local token_func
            token_func="gh-token-${alias}"

            # Load gh aliases if not already loaded (script runs in subshell)
            if ! type "$token_func" &>/dev/null; then
                if [ -f "$HOME/.bashrc-includes/gh-aliases.inc.bash" ]; then
                    # shellcheck source=/dev/null
                    source "$HOME/.bashrc-includes/gh-aliases.inc.bash"
                fi
            fi

            # Check if the gh-token-<alias> function exists (from play-github-cli-multi.yml)
            if ! type "$token_func" &>/dev/null; then
                print_error "GitHub multi-account function not found: $token_func"
                echo ""
                echo "Selected SSH key: ${SSH_KEYS[0]}"
                echo "Expected file: ~/.bashrc-includes/gh-aliases.inc.bash"
                echo "Expected function: $token_func"
                echo ""
                echo "Required: gh-token-<alias> functions from play-github-cli-multi.yml"
                echo ""
                echo "To fix:"
                echo "  1. Run: ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
                echo "  2. Verify: ls -la ~/.bashrc-includes/gh-aliases.inc.bash"
                echo "  3. Verify: grep $token_func ~/.bashrc-includes/gh-aliases.inc.bash"
                return 1
            fi

            # Get the token for the specific account.
            #
            # The status is checked, not discarded: if the function fails and
            # writes its complaint to stdout, an emptiness test alone would let
            # that complaint through AS THE TOKEN, and the failure would surface
            # later as a baffling auth error instead of here as a clear one.
            local token_err=""
            if ! GH_TOKEN="$("$token_func" 2>&1)"; then
                token_err="$GH_TOKEN"
                GH_TOKEN=""
            fi
            if [ -z "$GH_TOKEN" ] || [ -n "$token_err" ]; then
                GH_TOKEN=""
                print_error "Failed to retrieve token for account: $GITHUB_USERNAME"
                echo ""
                echo "Function $token_func returned no usable token."
                if [ -n "$token_err" ]; then
                    echo "It said: $token_err"
                fi
                echo "Account is not authenticated with gh CLI."
                echo ""
                echo "Fix: ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
                return 1
            fi

            # Cross-check: the token we just got should belong to the same
            # account that the SSH key authenticates as. A mismatch means the
            # github_accounts mapping (alias → username) is inconsistent with
            # the SSH key registrations. Fail here on the host with a clear
            # error rather than letting the container entrypoint surface it
            # after image build, which is slower and less obvious.
            local token_user=""
            if resolve_token_owner_login "$GH_TOKEN"; then
                token_user="$TOKEN_OWNER_LOGIN"
            fi

            if [ -z "$token_user" ]; then
                print_error "Could not verify which account this token belongs to"
                echo ""
                echo "  GitHub's API did not return a usable answer after 3 attempts."
                echo "  What it said:"
                echo ""
                printf '    %s\n' "${TOKEN_OWNER_LOOKUP_ERROR:-(no output)}"
                echo ""
                echo "This is almost always GitHub being briefly unavailable, NOT a"
                echo "problem with your keys or your configuration. Check"
                echo "https://www.githubstatus.com/ and try again shortly."
                echo ""
                echo "The cross-check being skipped here only confirms that the token"
                echo "from ${token_func} belongs to the same account as the SSH key."
                echo "To launch anyway without it:"
                echo ""
                echo "  CCY_SKIP_TOKEN_OWNER_CHECK=1 ccy"
                echo ""
                if [ -z "${CCY_SKIP_TOKEN_OWNER_CHECK:-}" ]; then
                    return 1
                fi
                echo "CCY_SKIP_TOKEN_OWNER_CHECK is set — continuing unverified."
                token_user="(unverified)"
            elif [ "$token_user" != "$GITHUB_USERNAME" ]; then
                print_error "Token owner does not match SSH-detected account"
                echo ""
                echo "  SSH key ${SSH_KEYS[0]} authenticates as: $GITHUB_USERNAME"
                echo "  But ${token_func} returned a token owned by: $token_user"
                echo ""
                echo "This means the github_accounts mapping for alias '$alias'"
                echo "points at '$token_user', but the SSH key ~/.ssh/github_${alias}"
                echo "is registered on GitHub as '$GITHUB_USERNAME'."
                echo ""
                echo "Fix one of:"
                echo "  - update github_accounts[${alias}] in localhost.yml to match the SSH key, or"
                echo "  - move the SSH key registration to match the alias mapping"
                return 1
            fi

            echo "✓ SSH key → $GITHUB_USERNAME ✓ gh token → $token_user (via $token_func)"
        }
    elif [ -n "${GH_TOKEN:-}" ]; then
        # The caller exported GH_TOKEN (a CI runner with --no-ssh and a pre-set
        # token, or a session on a box with no gh login that received the token
        # from the person's own machine). `gh help environment` documents that
        # GH_TOKEN takes precedence over gh's stored credentials, so this is the
        # token the container would end up with anyway; using it directly drops
        # the "gh must already be logged in" requirement from this path.
        #
        # With an ACCOUNT identity in hand (an agent, or a key that is not a
        # github_<alias> one) the token is cross-checked against it, exactly as
        # the gh-token-<alias> path does: a token for one account paired with a
        # key that pushes as another would otherwise surface later, inside the
        # container, as a baffling identity mismatch. With no account identity
        # there is nothing to check against and the token is used unverified.
        if [ -n "$GITHUB_USERNAME" ]; then
            local env_token_user=""
            if resolve_token_owner_login "$GH_TOKEN"; then
                env_token_user="$TOKEN_OWNER_LOGIN"
            fi
            if [ -z "$env_token_user" ]; then
                print_error "Could not verify which account the exported GH_TOKEN belongs to"
                echo ""
                echo "  GitHub's API did not return a usable answer after 3 attempts."
                echo "  What it said:"
                echo ""
                printf '    %s\n' "${TOKEN_OWNER_LOOKUP_ERROR:-(no output)}"
                echo ""
                echo "This is almost always GitHub being briefly unavailable. To launch anyway:"
                echo ""
                echo "  CCY_SKIP_TOKEN_OWNER_CHECK=1 ccy"
                echo ""
                if [ -z "${CCY_SKIP_TOKEN_OWNER_CHECK:-}" ]; then
                    return 1
                fi
                echo "CCY_SKIP_TOKEN_OWNER_CHECK is set — continuing unverified."
            elif [ "$env_token_user" != "$GITHUB_USERNAME" ]; then
                print_error "Exported GH_TOKEN belongs to a different account than the SSH identity"
                echo ""
                echo "  SSH identity authenticates as: $GITHUB_USERNAME"
                echo "  GH_TOKEN is owned by:           $env_token_user"
                echo ""
                echo "Export the token for $GITHUB_USERNAME, or select that account's key."
                return 1
            else
                echo "✓ SSH identity → $GITHUB_USERNAME ✓ exported GH_TOKEN → $env_token_user"
            fi
        else
            echo "✓ Using caller-supplied GH_TOKEN (no account identity to cross-check; unverified)"
        fi
    else
        # No account-specific token function and no caller-supplied token - fall
        # back to the active gh CLI account's default token.
        # Status checked rather than discarded: `gh auth token` prints its
        # complaint on failure, and an emptiness test alone would accept that
        # complaint as the token.
        local auth_err=""
        if ! GH_TOKEN="$(gh auth token 2>&1)"; then
            auth_err="$GH_TOKEN"
            GH_TOKEN=""
        fi

        if [ -z "$GH_TOKEN" ] || [ -n "$auth_err" ]; then
            GH_TOKEN=""
            print_error "No GitHub token: gh is not logged in and GH_TOKEN is not exported"
            echo ""
            echo "The container's gh needs a token. Either:"
            echo "  - log this box in:        gh auth login"
            echo "  - or export one for THIS session (a box that deliberately holds no"
            echo "    GitHub login gets it from the person's own machine):"
            echo "                            export GH_TOKEN=…   then re-run ccy"
            echo ""
            echo "For multi-account setup with github_ SSH keys, run:"
            echo "  ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
            return 1
        fi
    fi
}

# The key git on the host picks for the project, printed with ~/ expanded, or nothing when
# none is set. Used for a session whose only SSH identity is a forwarded agent. It is taken
# only from ~/.gitconfig, what it includes, and the system config: the project's own
# .git/config is writable from inside the container, so a key named there would let the
# container choose what the next session signs as. That refuses, naming the file to fix.
#   $1 the project directory
_host_signing_key_for() {
    local project="$1" key scope origin rc

    # Scope, origin and value, NUL-separated: the plain output quotes an unusual path. An
    # include reports the scope of the file including it.
    local fields=() probe config_file top
    probe=$(mktemp) || { print_error "Could not create a temporary file"; return 1; }
    git -C "$project" config -z --show-scope --show-origin --get user.signingkey >"$probe" && rc=0 || rc=$?
    mapfile -d '' -t fields <"$probe"
    rm -f "$probe"
    if [ "$rc" -gt 1 ]; then
        print_error "Could not read the signing key git uses in $project (git config exit $rc)"
        return 1
    fi
    scope="${fields[0]:-}"
    origin="${fields[1]:-}"
    key="${fields[2]:-}"
    if [ "$rc" -eq 0 ] && [ "$scope" != global ] && [ "$scope" != system ]; then
        print_error "user.signingkey for $project is set in its $scope git config, which the container can write."
        echo "  ccy picks the key the container signs with, so it takes it only from" >&2
        echo "  ~/.gitconfig and the system config. Remove the setting:" >&2
        case "$origin" in
            file:*)
                config_file="${origin#file:}"
                # A relative origin is relative to the repository's top level or, with no
                # work tree (a bare repository, or ccy started inside .git), to the git
                # directory. The failed probe's message is replaced by the next one's.
                if [ "${config_file#/}" = "$config_file" ]; then
                    if top=$(git -C "$project" rev-parse --show-toplevel 2>&1); then
                        config_file="$top/$config_file"
                    elif top=$(git -C "$project" rev-parse --absolute-git-dir 2>&1); then
                        config_file="$top/$config_file"
                    else
                        echo "    (relative to the git directory of $project: $top)" >&2
                    fi
                fi
                printf '    git config --file %q --unset user.signingkey\n' "$config_file" >&2
                ;;
            *) echo "    it comes from GIT_CONFIG_COUNT or GIT_CONFIG_PARAMETERS in this environment ($origin)" >&2 ;;
        esac
        return 1
    fi
    case "$key" in
        \~/*) key="$HOME/${key#\~/}" ;;
    esac
    printf '%s' "$key"
}

# The refusal for signing that is on but cannot work in the container. It names where
# signing is on from configure_git_signing's signing_on_in, which it is only called from.
#   $1 what is wrong   $2 the remedy
_git_signing_refusal() {
    print_error "Commit signing is on ${signing_on_in:-in ~/.gitconfig}, but $1."
    echo "  Every commit in the container would fail. $2" >&2
    return 1
}

# How a refusal or the launch line names a signing key: a path by its file name, a key::
# literal by its type, since its base64 is no name.
_signing_key_name() {
    case "$1" in
        key::*)
            local type
            read -r type _ <<<"${1#key::}"
            printf 'the literal %s key in user.signingkey' "$type"
            ;;
        *) basename "$1" ;;
    esac
}

# Commit signing in the container (Plan 00139 D5).
# play-git-configure-and-tools.yml signs every commit and tag with the machine's login key,
# and play-github-cli-multi.yml each account's repositories with that account's login key,
# all through the ssh-agent that holds them unlocked. The container signs the same way, so
# no private key is copied in for it:
#   - a session whose primary SSH identity is a key file signs with that key. The
#     entrypoint adds it to the container's own agent, or the forwarded agent holds it;
#   - a session whose only identity is a forwarded agent signs with the key git on the
#     host picks for the project, passed in as its public half, if that agent holds it.
# A [user] section naming it is appended to the ~/.gitconfig copy: the last value wins
# over the host's key and every account include, whose host paths the container cannot
# read. Signing that is on with no key the container can use would fail every commit made
# in it, so that refuses the launch. Signing that is off leaves the copy as it is.
# Whether commits and tags are signed is read from the project's local config first, as git
# in the container reads it, and from the copy where the project does not set it: a
# repository that turns signing off makes no signed commits, so needs no key, and one that
# turns it on needs one even when ~/.gitconfig does not.
#   $1 the gitconfig copy   $2 the project directory
#   $3 the primary SSH identity: a host key path, $SSH_AGENT_SENTINEL, or empty
#   $4 where a key-file identity is mounted in the container
#   $5 1 when the session's ssh-agent is forwarded into the container
#   $6 the one-key agent's socket when it forwards the key-file identity in place of the
#      file (ccy_agent_filter_start), else empty: the copy then names that key's public half
configure_git_signing() {
    local gitconfig="$1" project="$2" primary="$3" in_container="$4" forwarded="$5" filter_sock="${6:-}"
    local name value rc format key signingkey label public="" signing=false
    local in_repo=false probe signing_on_in="in ~/.gitconfig"

    # A directory outside any repository has no local config. Any other failure is not that.
    if probe=$(LC_ALL=C git -C "$project" rev-parse --git-dir 2>&1); then
        in_repo=true
    elif [[ "$probe" != *"not a git repository"* ]]; then
        print_error "Could not tell whether $project is a git repository: $probe"
        return 1
    fi

    for name in commit.gpgsign tag.gpgsign; do
        rc=1
        if [ "$in_repo" = true ]; then
            value=$(git -C "$project" config --local --type=bool --get "$name" 2>&1) && rc=0 || rc=$?
            if [ "$rc" -gt 1 ]; then
                print_error "Could not read $name from the git config of $project (git config exit $rc): $value"
                return 1
            fi
            if [ "$rc" -eq 0 ] && [ "$value" = "true" ]; then
                signing_on_in="in this project's git config"
            fi
        fi
        if [ "$rc" -eq 1 ]; then
            value=$(git config --file "$gitconfig" --type=bool --get "$name") && rc=0 || rc=$?
            if [ "$rc" -gt 1 ]; then
                print_error "Could not read $name from $gitconfig (git config exit $rc)"
                return 1
            fi
        fi
        if [ "$value" = "true" ]; then
            signing=true
        fi
    done
    [ "$signing" = true ] || return 0

    format=$(git config --file "$gitconfig" --get gpg.format) && rc=0 || rc=$?
    if [ "$rc" -gt 1 ]; then
        print_error "Could not read gpg.format from $gitconfig (git config exit $rc)"
        return 1
    fi
    if [ "$format" != "ssh" ]; then
        _git_signing_refusal "gpg.format is '${format:-openpgp}'; only SSH signing works inside the container" \
            "Re-run playbooks/imports/play-git-configure-and-tools.yml, which sets up SSH signing."
        return 1
    fi

    if [ -n "$primary" ] && [ "$primary" != "$SSH_AGENT_SENTINEL" ]; then
        key="$primary"
        signingkey="$in_container"
        label="$(basename "$key"), the session's SSH identity"
    elif [ "$primary" = "$SSH_AGENT_SENTINEL" ]; then
        key=$(_host_signing_key_for "$project") || return 1
        if [ -z "$key" ]; then
            _git_signing_refusal "user.signingkey is not set" \
                "Re-run playbooks/imports/play-git-configure-and-tools.yml, which sets it."
            return 1
        fi
        label="$(_signing_key_name "$key"), the key git picks for this project, from the forwarded agent"
    else
        _git_signing_refusal "the session has no SSH identity to sign with" \
            "Choose an SSH key at launch, or pass --ssh-agent with an agent holding one."
        return 1
    fi

    # A forwarded agent must hold the key: nothing in the container can unlock it. The
    # one-key agent offers only the identity's own key, and no file of it is mounted, so the
    # copy names its public half as the agent gives it.
    if [ -n "$filter_sock" ] && [ "$primary" != "$SSH_AGENT_SENTINEL" ]; then
        local offered
        if ! offered=$(_agent_public_for_key "$filter_sock" "$key"); then
            _git_signing_refusal "ccy's one-key agent does not offer $(basename "$key")" \
                "Load it into your ssh-agent with: ssh-add $key"
            return 1
        fi
        signingkey="key::$offered"
        label="$(basename "$key"), the session's SSH identity, from your ssh-agent"
    elif [ "$forwarded" = "1" ] || [ "$primary" = "$SSH_AGENT_SENTINEL" ]; then
        case "$key" in
            key::*) public="${key#key::}" ;;
            *)
                if [ ! -f "$key.pub" ]; then
                    _git_signing_refusal "$key.pub does not exist, so ccy cannot tell whether the forwarded agent holds it" \
                        "Restore the public half beside the key."
                    return 1
                fi
                public=$(<"$key.pub")
                ;;
        esac
        read -r _ public _ <<<"$public"
        local listed
        listed=$(ssh-add -L 2>&1) && rc=0 || rc=$?
        if [ "$rc" -gt 1 ]; then
            _git_signing_refusal "the forwarded ssh-agent cannot be read (ssh-add -L: $listed)" \
                "Check SSH_AUTH_SOCK, then retry."
            return 1
        fi
        if ! awk -v blob="$public" '$2 == blob { found = 1 } END { exit !found }' <<<"$listed"; then
            local load="Load it with: ssh-add $key"
            case "$key" in
                key::*) load="Load the private key whose public half user.signingkey names." ;;
            esac
            if [ "$rc" -eq 1 ]; then
                _git_signing_refusal "the forwarded ssh-agent holds no keys, or is locked, so it cannot sign with $(_signing_key_name "$key")" \
                    "Unlock it with: ssh-add -X. Or: $load"
            else
                _git_signing_refusal "the forwarded ssh-agent does not hold $(_signing_key_name "$key")" "$load"
            fi
            return 1
        fi
        if [ "$primary" = "$SSH_AGENT_SENTINEL" ]; then
            signingkey="key::$(awk -v blob="$public" '$2 == blob { print $1 " " $2; exit }' <<<"$listed")"
        fi
    fi

    # The leading newline ends a last line the copy may have left unterminated.
    if ! printf '\n[user]\n\tsigningkey = %s\n' "$signingkey" >>"$gitconfig"; then
        print_error "Could not point user.signingkey in $gitconfig at the session's key"
        return 1
    fi
    echo "✓ Commit signing: $label"
}

# Export functions
export -f _host_signing_key_for
export -f _git_signing_refusal
export -f _signing_key_name
export -f configure_git_signing
export -f _agent_public_for_key
export -f ssh_key_fingerprint
export -f ssh_key_needs_passphrase
export -f ccy_agent_forwards_key
export -f discover_and_select_ssh_keys
export -f build_ssh_mounts_and_validate
export -f resolve_github_ssh_alias
export -f remote_ssh_host
export -f detect_project_github_alias
export -f render_ssh_alias_stanza
export -f compose_ssh_alias_exports
export -f ssh_agent_usable
export -f github_identity_is_deploy_key
export -f _github_probe_identity
