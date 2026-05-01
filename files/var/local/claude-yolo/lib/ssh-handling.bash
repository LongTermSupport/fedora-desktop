#!/bin/bash
# SSH Handling Library
# Shared SSH key operations for claude-yolo (ccy)
#
# Version: 1.0.0

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
    [ -n "$url" ] && echo "$url"
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
    export PROBE_LOG_DIR="/tmp/ccy-gh-probe-$$"
    mkdir -p "$PROBE_LOG_DIR"

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

    for i in "${!SSH_KEYS[@]}"; do
        SSH_MOUNTS+=("-v" "${SSH_KEYS[$i]}:/root/.ssh/key_$i:ro")
        SSH_KEY_PATHS+=("/root/.ssh/key_$i")

        # Extract GitHub username by testing SSH connection with the key.
        # CRITICAL isolation flags — without them the probe falls through to
        # ~/.ssh/config's default `Host github.com` entry (which typically
        # points IdentityFile at ~/.ssh/id) and/or ssh-agent, so GitHub
        # returns "Hi <id-owner>!" regardless of which github_<alias> key
        # was passed via -i. That false positive silently mis-maps aliases
        # to accounts and fails downstream in the container's token check.
        #   -F /dev/null        → ignore ~/.ssh/config
        #   -o IdentityAgent=none → ignore ssh-agent
        #   -o IdentitiesOnly=yes → only try the -i key
        # ssh -T responds with: "Hi <username>! You've successfully authenticated..."
        GITHUB_USERNAME=$(ssh -T -i "${SSH_KEYS[$i]}" \
            -F /dev/null \
            -o IdentitiesOnly=yes \
            -o IdentityAgent=none \
            -o StrictHostKeyChecking=no \
            git@github.com 2>&1 | grep -oP "Hi \K[^!]+")

        if [ -z "$GITHUB_USERNAME" ]; then
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

        echo "✓ Detected GitHub account for SSH key: $GITHUB_USERNAME"
    done

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

            # Get the token for the specific account
            GH_TOKEN=$($token_func 2>/dev/null)
            if [ -z "$GH_TOKEN" ]; then
                print_error "Failed to retrieve token for account: $GITHUB_USERNAME"
                echo ""
                echo "Function $token_func returned no token."
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
            local token_user
            token_user=$(GH_TOKEN="$GH_TOKEN" gh api user --jq .login 2>/dev/null)
            if [ -n "$token_user" ] && [ "$token_user" != "$GITHUB_USERNAME" ]; then
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
    else
        # No GitHub username detected - fall back to default token
        GH_TOKEN=$(gh auth token 2>/dev/null)

        if [ -z "$GH_TOKEN" ]; then
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
