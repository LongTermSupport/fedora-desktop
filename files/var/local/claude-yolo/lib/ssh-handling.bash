#!/bin/bash
# SSH Handling Library
# Shared SSH key operations for claude-yolo (ccy)
#
# Version: 1.0.0

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
        echo ""
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo "SSH Key Selection for Claude YOLO"
        echo "════════════════════════════════════════════════════════════════════════════════"
        echo ""
        echo "No SSH key was specified with --ssh-key flag."
        echo ""
        echo "Available GitHub SSH keys (managed by play-github-cli-multi.yml):"
        echo ""
        echo "  0) Continue without SSH key (git push will NOT work)"
        echo ""

        for i in "${!GITHUB_KEYS[@]}"; do
            echo "  $((i+1))) ${GITHUB_KEYS[$i]}"
        done

        echo ""
        echo "You can also specify keys manually with: $tool_name --ssh-key <path>"
        echo ""

        while true; do
            read -rp "Select SSH key [0-${#GITHUB_KEYS[@]}]: " selection
            echo ""

            if [ -z "$selection" ]; then
                echo "Invalid selection: (empty)"
                echo "Please enter a number between 0 and ${#GITHUB_KEYS[@]}"
                echo ""
                continue
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
