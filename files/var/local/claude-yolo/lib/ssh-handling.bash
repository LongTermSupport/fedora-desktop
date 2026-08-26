#!/bin/bash
# SSH Handling Library
# Shared SSH key operations for claude-yolo (ccy)
#
# Version: 1.3.0 - GitHub probes now unlock passphrase keys into a PRIVATE
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

# Parse owner/repo from a GitHub remote URL. Handles ssh, https, and the
# alias form (git@github.com-<alias>:owner/repo).
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
    return 1
}

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
# Args: $1 = tool_name (for display)
# Modifies: SSH_KEYS global array
# Returns: 0 on success, exits on error
discover_and_select_ssh_keys() {
    local tool_name="$1"

    # These keys are managed by play-github-cli-multi.yml which creates keys with
    # the pattern ~/.ssh/github_<alias> for each configured GitHub account.
    # See: playbooks/imports/optional/common/play-github-cli-multi.yml:163-183
    mapfile -t GITHUB_KEYS < <(find "$HOME/.ssh" -type f -name "github_*" ! -name "*.pub" 2>/dev/null | sort)

    if [ ${#GITHUB_KEYS[@]} -gt 0 ]; then
        # Probe every key against the project's remote in parallel so we can
        # default the selection to the key(s) that actually have access.
        # Picking the wrong key here silently mis-routes git push to the
        # wrong account, so steering the user toward a verified-working key
        # is the primary purpose of this prompt.
        local remote_url=""
        local suggested_index=""
        local probe_status="skipped (not a git repo or no remote)"

        remote_url=$(get_project_remote_url ".")
        if [ -n "$remote_url" ]; then
            echo ""
            echo "Probing GitHub accounts against remote: $remote_url"
            echo "(checks .permissions.push via gh-token-<alias> — sequential, ~1-3 seconds)"

            local working_keys
            working_keys=$(probe_gh_keys_for_remote "$remote_url")

            local match_count=0
            if [ -n "$working_keys" ]; then
                match_count=$(echo "$working_keys" | grep -c .)
            fi

            case "$match_count" in
                0)  probe_status="no keys have push access to this remote (logs: $PROBE_LOG_DIR/)" ;;
                1)
                    local winner
                    winner=$(echo "$working_keys" | head -1)
                    for i in "${!GITHUB_KEYS[@]}"; do
                        if [ "${GITHUB_KEYS[$i]}" = "$winner" ]; then
                            suggested_index=$((i+1))
                            break
                        fi
                    done
                    probe_status="1 key has push access"
                    ;;
                *)  probe_status="$match_count keys have push access — pick manually" ;;
            esac
        fi

        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "SSH Key Selection for Claude YOLO"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        echo "No SSH key was specified with --ssh-key flag."
        echo "Probe result: $probe_status"
        echo ""
        echo "Available GitHub SSH keys (managed by play-github-cli-multi.yml):"
        echo ""
        echo "  0) Continue without SSH key (git push will NOT work)"
        echo ""

        for i in "${!GITHUB_KEYS[@]}"; do
            local marker=""
            if [ -n "$working_keys" ] && grep -qxF "${GITHUB_KEYS[$i]}" <<< "$working_keys"; then
                marker="  ✓ has push access to this remote"
            fi
            if [ -n "$suggested_index" ] && [ "$((i+1))" = "$suggested_index" ]; then
                marker="${marker} ← default"
            fi
            echo "  $((i+1))) ${GITHUB_KEYS[$i]}${marker}"
        done

        echo ""
        if [ -n "$suggested_index" ]; then
            echo "Press ENTER to accept the verified default ($suggested_index)."
        fi
        echo "You can also specify keys manually with: $tool_name --ssh-key <path>"
        echo ""

        local prompt_text="Select SSH key [0-${#GITHUB_KEYS[@]}]"
        [ -n "$suggested_index" ] && prompt_text="$prompt_text (default: $suggested_index)"
        prompt_text="$prompt_text: "

        while true; do
            read -rp "$prompt_text" selection
            echo ""

            # Empty input → accept the verified default if we have one
            if [ -z "$selection" ]; then
                if [ -n "$suggested_index" ]; then
                    selection="$suggested_index"
                else
                    echo "No default available — please enter a number between 0 and ${#GITHUB_KEYS[@]}"
                    echo ""
                    continue
                fi
            fi

            if [ "$selection" = "0" ]; then
                echo "⚠  Continuing WITHOUT SSH key - git push operations will fail"
                echo ""
                break
            elif [ "$selection" -ge 1 ] && [ "$selection" -le ${#GITHUB_KEYS[@]} ] 2>/dev/null; then
                SSH_KEYS+=("${GITHUB_KEYS[$((selection-1))]}")
                echo "✓ Selected: ${GITHUB_KEYS[$((selection-1))]}"
                echo ""
                break
            else
                echo "Invalid selection: $selection"
                echo "Please enter a number between 0 and ${#GITHUB_KEYS[@]}"
                echo ""
            fi
        done

        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
    else
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "⚠  WARNING: No SSH Keys Available"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        echo "No github_ SSH keys found in ~/.ssh/"
        echo "Git push operations will NOT work without SSH keys."
        echo ""
        echo "To set up GitHub SSH keys, run:"
        echo "  ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
        echo ""
        echo "Or specify a key manually:"
        echo "  $tool_name --ssh-key ~/.ssh/id_ed25519"
        echo ""
        read -rp "Press Enter to continue WITHOUT SSH key, or Ctrl+C to cancel: " _unused
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
    fi
}

# Probe GitHub for the authenticated username using a specific key + endpoint.
# Echoes the username on success, nothing on failure (grep returns 1, but callers
# run inside build_ssh_mounts_and_validate which is invoked as `|| exit 1`, so
# set -e is disabled — an empty result does not abort).
# CRITICAL isolation flags — without them the probe falls through to ~/.ssh/config's
# default `Host github.com` entry and/or the USER'S ssh-agent, returning the
# wrong account:
#   -F /dev/null          → ignore ~/.ssh/config
#   -o IdentitiesOnly=yes → only try the -i key
#   -o IdentityAgent=…    → ONLY ccy's private probe agent (below), never the
#                           user's; `none` when no probe agent is running
# ConnectTimeout bounds the wait so a DROP-firewalled port 22 fails fast (~10s)
# instead of hanging on the default TCP timeout before any 443 fallback can run.
_github_probe_user() {
    local key="$1" host="$2" port="$3"
    local agent="${CCY_PROBE_AGENT_SOCK:-none}"
    ssh -T -i "$key" \
        -F /dev/null \
        -o IdentitiesOnly=yes \
        -o IdentityAgent="$agent" \
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
            read -rp "Hit return to try the passphrase again (round $round of 3), or Ctrl+C to abort: " _unused
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
# Requires: SSH_KEYS global array
# Sets: SSH_MOUNTS, SSH_KEY_PATHS, GITHUB_USERNAME, GH_TOKEN global variables
# Returns: 0 on success, exits on error
build_ssh_mounts_and_validate() {
    local tool_name="$1"

    # Build SSH key mount arguments and extract GitHub account
    # This needs to happen early so GH_TOKEN is available for create_token
    SSH_MOUNTS=()
    SSH_KEY_PATHS=()
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
                if ! _probe_agent_add_key "$unlock_key"; then
                    print_error "Could not unlock SSH key: $unlock_key"
                    echo "The passphrase was not accepted. Re-run $tool_name to try again."
                    return 1
                fi
            done
        fi
    fi

    for i in "${!SSH_KEYS[@]}"; do
        SSH_MOUNTS+=("-v" "${SSH_KEYS[$i]}:/root/.ssh/key_$i:ro")
        SSH_KEY_PATHS+=("/root/.ssh/key_$i")

        # Probe for the username. GITHUB_SSH_443=1 (the --github-443 flag, or an
        # accepted auto-fallback below) routes over ssh.github.com:443; otherwise
        # the standard github.com:22. ssh.github.com:443 serves the same host keys.
        local gh_ssh_host="github.com" gh_ssh_port="22"
        if [ "${GITHUB_SSH_443:-0}" = "1" ]; then
            gh_ssh_host="ssh.github.com"
            gh_ssh_port="443"
        fi
        local detected_user
        detected_user=$(_github_probe_user "${SSH_KEYS[$i]}" "$gh_ssh_host" "$gh_ssh_port")

        # Auto-fallback: only on the PRIMARY key, only when not already on 443.
        # If port 22 failed but ssh.github.com:443 authenticates, port 22 is
        # firewall-blocked — offer to enable 443 for THIS session. GITHUB_SSH_443
        # propagates to the container env + entrypoint, and to subsequent keys in
        # this loop (they recompute the endpoint from it each iteration).
        if [ -z "$detected_user" ] && [ "$i" -eq 0 ] && [ "${GITHUB_SSH_443:-0}" != "1" ]; then
            local user_443
            user_443=$(_github_probe_user "${SSH_KEYS[$i]}" "ssh.github.com" "443")
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
                    read -rp "Enable GitHub SSH over 443 for this session? [Y/n] " reply_443
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
            print_error "SSH key authentication to GitHub failed: ${SSH_KEYS[$i]}"
            echo ""
            echo "The selected SSH key is not registered with any GitHub account."
            echo ""
            echo "To fix this:"
            echo "  1. Go to https://github.com/settings/keys"
            echo "  2. Click 'New SSH key'"
            echo "  3. Add the public key from: ${SSH_KEYS[$i]}.pub"
            echo ""
            echo "Or set up GitHub keys with:"
            echo "  ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
            return 1
        fi

        if [ "$i" -eq 0 ]; then
            primary_user="$detected_user"
            echo "✓ Primary GitHub account (key $(basename "${SSH_KEYS[0]}")): $detected_user"
        else
            echo "✓ Additional SSH key authenticates as: $detected_user"
            if [ "$detected_user" != "$primary_user" ]; then
                echo "  note: this key maps to a different account; the container"
                echo "        will use the primary account ($primary_user)."
            fi
        fi
    done

    # Identity is the primary key's account (see BSH-06 note above).
    GITHUB_USERNAME="$primary_user"

    # Get GitHub token from gh CLI
    if ! command_exists gh; then
        print_error "gh (GitHub CLI) not found"
        echo "Install it with: ansible-playbook playbooks/imports/play-git-configure-and-tools.yml"
        return 1
    fi

    # If we detected a GitHub username from SSH key, get the account-specific token
    # This requires play-github-cli-multi.yml to be configured and shell reloaded
    if [ -n "$GITHUB_USERNAME" ] && [ ${#SSH_KEYS[@]} -gt 0 ]; then
        # Extract alias from the first SSH key
        local key_basename
        key_basename=$(basename "${SSH_KEYS[0]}")
        if [[ "$key_basename" =~ ^github_(.+)$ ]]; then
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
        fi
    elif [ -n "${GH_TOKEN:-}" ]; then
        # No SSH key / GitHub username detected, but the caller already
        # exported GH_TOKEN (e.g. a CI runner launching with --no-ssh and a
        # pre-set token). `gh help environment` documents that GH_TOKEN
        # already takes precedence over gh's own stored credentials, so
        # `gh auth token` below would in practice echo this same value back
        # (measured: `GH_TOKEN=x gh auth token` -> x, rc=0) — this branch is
        # not fixing a live clobbering bug. It makes that precedence
        # explicit and self-documenting in OUR code, and drops the
        # `gh auth token` subprocess call (and its "gh must already be
        # logged in" requirement) from this path — the caller's token is
        # used directly. There is no SSH identity to cross-check it
        # against, so it is used unverified.
        echo "✓ Using caller-supplied GH_TOKEN (no SSH key / GitHub username detected, unverified)"
    else
        # No GitHub username detected and no caller-supplied token - fall
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
            print_error "Not authenticated with GitHub CLI"
            echo ""
            echo "Run: gh auth login"
            echo ""
            echo "For multi-account setup with github_ SSH keys, run:"
            echo "  ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
            return 1
        fi
    fi
}

# Export functions
export -f discover_and_select_ssh_keys
export -f build_ssh_mounts_and_validate
