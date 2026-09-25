#!/bin/bash
# SSH Handling Library
# Shared SSH key operations for claude-yolo (ccy)
#
# Version: 1.6.1 - stage_git_signing_key trusts only ~/.gitconfig and the system config
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

    local agent_ok=false agent_key_count=0
    if ssh_agent_usable; then
        agent_ok=true
        agent_key_count=$(grep -c . <<< "$SSH_AGENT_PROBE_OUTPUT")
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
        read -rp "$CCY_PROMPT_SSH_NO_KEY " _unused
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        return 0
    fi

    # Probe every account key against the project's remote so we can default
    # the selection to the key(s) that actually have access. Picking the wrong
    # key here silently mis-routes git push to the wrong account, so steering
    # the user toward a verified-working key is the primary purpose of this
    # prompt.
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
            *)  probe_status="$match_count account keys have push access — pick manually" ;;
        esac
    elif [ ${#GITHUB_KEYS[@]} -eq 0 ]; then
        probe_status="no github_ account keys to probe"
    fi
    # With no verified account key, the remote's own key is the natural default.
    if [ -z "$suggested_key" ] && [ "$alias_rc" -eq 0 ]; then
        suggested_key="$GITHUB_ALIAS_KEY"
    fi

    local -a candidates=() labels=()
    if [ "$alias_rc" -eq 0 ]; then
        candidates+=("$GITHUB_ALIAS_KEY")
        labels+=("$GITHUB_ALIAS_KEY  — the project remote's key (alias $GITHUB_ALIAS_HOST → $GITHUB_ALIAS_HOSTNAME:$GITHUB_ALIAS_PORT)")
    fi
    local i
    for i in "${!GITHUB_KEYS[@]}"; do
        local marker=""
        if [ -n "$working_keys" ] && grep -qxF "${GITHUB_KEYS[$i]}" <<< "$working_keys"; then
            marker="  ✓ has push access to this remote"
        fi
        candidates+=("${GITHUB_KEYS[$i]}")
        labels+=("${GITHUB_KEYS[$i]}${marker}")
    done
    if [ "$agent_ok" = true ]; then
        candidates+=("$SSH_AGENT_SENTINEL")
        labels+=("the session's ssh-agent ($agent_key_count key(s)) — pushes as whoever it signs as; runs the container with SELinux labelling off")
    fi

    local suggested_index=""
    if [ -n "$suggested_key" ]; then
        for i in "${!candidates[@]}"; do
            if [ "${candidates[$i]}" = "$suggested_key" ]; then
                suggested_index=$((i+1))
                break
            fi
        done
    fi

    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "SSH Key Selection for Claude YOLO"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "No SSH key was specified with --ssh-key flag."
    echo "Probe result: $probe_status"
    echo ""
    echo "Available identities:"
    echo ""
    echo "  0) Continue without SSH key (git push will NOT work)"
    echo ""

    for i in "${!candidates[@]}"; do
        local suffix=""
        if [ -n "$suggested_index" ] && [ "$((i+1))" = "$suggested_index" ]; then
            suffix=" ← default"
        fi
        echo "  $((i+1))) ${labels[$i]}${suffix}"
    done

    echo ""
    if [ -n "$suggested_index" ]; then
        echo "Press ENTER to accept the default ($suggested_index)."
    fi
    echo "You can also specify keys manually with: $tool_name --ssh-key <path>"
    echo ""

    local prompt_text="${CCY_PROMPT_SSH_KEY} [0-${#candidates[@]}]"
    [ -n "$suggested_index" ] && prompt_text="$prompt_text (default: $suggested_index)"
    prompt_text="$prompt_text: "

    while true; do
        read -rp "$prompt_text" selection
        echo ""

        # Empty input → accept the default if we have one
        if [ -z "$selection" ]; then
            if [ -n "$suggested_index" ]; then
                selection="$suggested_index"
            else
                echo "No default available — please enter a number between 0 and ${#candidates[@]}"
                echo ""
                continue
            fi
        fi

        if [ "$selection" = "0" ]; then
            echo "⚠  Continuing WITHOUT SSH key - git push operations will fail"
            echo ""
            break
        elif [ "$selection" -ge 1 ] && [ "$selection" -le ${#candidates[@]} ] 2>/dev/null; then
            SSH_KEYS+=("${candidates[$((selection-1))]}")
            echo "✓ Selected: ${labels[$((selection-1))]}"
            echo ""
            break
        else
            echo "Invalid selection: $selection"
            echo "Please enter a number between 0 and ${#candidates[@]}"
            echo ""
        fi
    done

    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    return 0
}

# Probe GitHub for the identity a key (or the session's agent) authenticates as.
# Echoes it on success — a login for an account key, `owner/repo` for a deploy
# key — and nothing on failure (grep returns 1, but callers run inside
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
_github_probe_identity() {
    local key="$1" host="$2" port="$3"
    local -a identity_opts
    if [ "$key" = "$SSH_AGENT_SENTINEL" ]; then
        identity_opts=(-o IdentitiesOnly=no -o IdentityAgent="${SSH_AUTH_SOCK:-none}")
    else
        identity_opts=(-i "$key" -o IdentitiesOnly=yes -o IdentityAgent="${CCY_PROBE_AGENT_SOCK:-none}")
    fi
    ssh -T "${identity_opts[@]}" \
        -F /dev/null \
        -o StrictHostKeyChecking=no \
        -o ConnectTimeout=10 \
        -p "$port" \
        "git@${host}" 2>&1 | grep -oP "Hi \K[^!]+"
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
_probe_agent_add_key() {
    local key="$1" round
    for round in 1 2 3; do
        if SSH_AUTH_SOCK="$CCY_PROBE_AGENT_SOCK" ssh-add "$key"; then
            return 0
        fi
        if [ "$round" -lt 3 ]; then
            echo ""
            echo "Key not unlocked: $key"
            read -rp "$CCY_PROMPT_SSH_PASSPHRASE_RETRY (round $round of 3), or Ctrl+C to abort: " _unused
        fi
    done
    return 1
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
    # failure. Skipped when no tty can answer a prompt (headless/CI): a
    # passphrase-less key needs no agent and an encrypted one could not be
    # unlocked anyway.
    if [ ${#SSH_KEYS[@]} -gt 0 ] && [ -t 0 ] && [ "${HEADLESS_MODE:-false}" != "true" ]; then
        trap '_probe_agent_stop' RETURN
        _probe_agent_start
        if [ -n "$CCY_PROBE_AGENT_SOCK" ]; then
            local unlock_key
            for unlock_key in "${SSH_KEYS[@]}"; do
                # The session's agent is already unlocked by definition.
                [ "$unlock_key" = "$SSH_AGENT_SENTINEL" ] && continue
                if ! _probe_agent_add_key "$unlock_key"; then
                    print_error "Could not unlock SSH key: $unlock_key"
                    echo "The passphrase was not accepted. Re-run $tool_name to try again."
                    return 1
                fi
            done
        fi
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
                local enable_443=false
                if [ "${HEADLESS_MODE:-false}" = "true" ] || [ ! -t 0 ]; then
                    echo "  Non-interactive launch — enabling 443 automatically (the only way to proceed)."
                    enable_443=true
                else
                    local reply_443
                    read -rp "$CCY_PROMPT_GITHUB_443 " reply_443
                    case "$reply_443" in
                        [Nn]*) enable_443=false ;;
                        *) enable_443=true ;;
                    esac
                fi
                if [ "$enable_443" = "true" ]; then
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
            echo ""
            echo "The selected SSH key is not registered with any GitHub account or repository."
            echo ""
            echo "To fix this:"
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

# Carry commit signing into the container (Plan 00139).
# play-git-configure-and-tools.yml signs every commit and tag with the machine's SSH key,
# and play-github-cli-multi.yml includes a key per GitHub account, which git picks in a
# repository whose remote is that account's github.com-<alias> host. The key is the one
# git on the host picks for the project, so a commit made in the container is signed as
# it would be outside. It is staged into the directory holding the ~/.gitconfig copy,
# which the launcher mounts read-only (privately relabelled where SELinux needs it), and
# a [user] section naming the mounted key is appended to the copy: the last value wins,
# so it overrides the machine key and every account include, whose host paths the
# container cannot read. Signing that is on with no usable key would fail every commit
# made in the container, so that refuses the launch instead.
# The key is taken only from ~/.gitconfig, what it includes, and the system config. The
# project's own .git/config is writable from inside the container, so a key named there
# could be any file this user can read, copied in on the next launch: that refuses too.
#   $1 the gitconfig copy   $2 the host directory it is in   $3 where $2 is mounted
#   $4 the project directory
stage_git_signing_key() {
    local gitconfig="$1" stage_dir="$2" mount_dir="$3" project="$4"
    local key scoped scope format name value rc signing=false

    for name in commit.gpgsign tag.gpgsign; do
        value=$(git config --file "$gitconfig" --type=bool --get "$name") && rc=0 || rc=$?
        if [ "$rc" -gt 1 ]; then
            print_error "Could not read $name from $gitconfig (git config exit $rc)"
            return 1
        fi
        if [ "$value" = "true" ]; then
            signing=true
        fi
    done

    scoped=$(git -C "$project" config --show-scope --get user.signingkey) && rc=0 || rc=$?
    if [ "$rc" -gt 1 ]; then
        print_error "Could not read the signing key git uses in $project (git config exit $rc)"
        return 1
    fi
    scope="${scoped%%$'\t'*}"
    key="${scoped#*$'\t'}"
    if [ -n "$scoped" ] && [ "$scope" != global ] && [ "$scope" != system ]; then
        print_error "user.signingkey for $project is set in its $scope git config, which the container can write."
        echo "  ccy copies the signing key into the container, so it takes the key only from" >&2
        echo "  ~/.gitconfig and the system config. Remove the $scope setting:" >&2
        if [ "$scope" = command ]; then
            echo "    it comes from GIT_CONFIG_COUNT or GIT_CONFIG_PARAMETERS in this environment" >&2
        else
            echo "    git -C '$project' config --$scope --unset user.signingkey" >&2
        fi
        return 1
    fi
    format=$(git config --file "$gitconfig" --get gpg.format) && rc=0 || rc=$?
    if [ "$rc" -gt 1 ]; then
        print_error "Could not read gpg.format from $gitconfig (git config exit $rc)"
        return 1
    fi

    local problem=""
    if [ -z "$key" ]; then
        problem="user.signingkey is not set"
    elif [ "$format" != "ssh" ]; then
        problem="gpg.format is '${format:-openpgp}'; only SSH signing works inside the container"
    else
        case "$key" in
            \~/*) key="$HOME/${key#\~/}" ;;
        esac
        case "$key" in
            key::*) problem="user.signingkey is a literal public key, which needs an ssh-agent the container does not have" ;;
            *) [ -f "$key" ] || problem="the signing key $key does not exist" ;;
        esac
    fi

    if [ -n "$problem" ]; then
        if [ "$signing" = true ]; then
            print_error "Commit signing is on in ~/.gitconfig, but $problem."
            echo "  Every commit in the container would fail. Re-run" >&2
            echo "  playbooks/imports/play-git-configure-and-tools.yml, then" >&2
            echo "  playbooks/imports/play-github-cli-multi.yml: they generate the machine's key" >&2
            echo "  and each GitHub account's, and set signing up." >&2
            return 1
        fi
        return 0
    fi

    if ! install -m 0600 "$key" "$stage_dir/git-signing-key"; then
        print_error "Could not stage the signing key $key into $stage_dir"
        return 1
    fi
    # The leading newline ends a last line the copy may have left unterminated.
    if ! printf '\n[user]\n\tsigningkey = %s\n' "$mount_dir/git-signing-key" >>"$gitconfig"; then
        print_error "Could not point user.signingkey in $gitconfig at the staged key"
        return 1
    fi
}

# Export functions
export -f stage_git_signing_key
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
