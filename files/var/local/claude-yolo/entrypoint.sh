#!/bin/bash
# Claude Code YOLO Container Entrypoint
# In rootless Docker, UID 0 = host user, so this is safe
# IMPORTANT: Uses ccy-specific tokens from ~/.claude-tokens/ccy/ (NOT desktop tokens)

set -e

# Enable debug mode if requested
if [ "$DEBUG_MODE" = "true" ]; then
    set -x
fi

# Verify GH_TOKEN is set
if [ -z "$GH_TOKEN" ]; then
    echo "ERROR: GH_TOKEN environment variable not set" >&2
    exit 1
fi

# Note: ~/.claude is already mounted with project-specific state
# It contains credentials, settings, and all conversation history
# We only need to set up git, gh CLI, and SSH

# Configure git
if [ -f /tmp/claude-config-import/gitconfig ]; then
    cp /tmp/claude-config-import/gitconfig ~/.gitconfig
fi

# Configure GitHub CLI with token
mkdir -p ~/.config/gh
TEMP_TOKEN="$GH_TOKEN"
unset GH_TOKEN

if ! echo "$TEMP_TOKEN" | gh auth login --with-token 2>&1; then
    echo "ERROR: gh auth login failed" >&2
    exit 1
fi

# Verify the authenticated account matches the expected GitHub username
if [ -n "$GITHUB_USERNAME" ]; then
    AUTHENTICATED_USER=$(gh api user --jq .login 2>/dev/null)
    if [ "$AUTHENTICATED_USER" != "$GITHUB_USERNAME" ]; then
        echo "ERROR: Token authentication mismatch" >&2
        echo "Expected: $GITHUB_USERNAME" >&2
        echo "Got: $AUTHENTICATED_USER" >&2
        echo "" >&2
        echo "This means the gh-token-<alias> function on the host returned the wrong token." >&2
        echo "Please ensure play-github-cli-multi.yml is properly configured." >&2
        exit 1
    fi
    echo "✓ Authenticated as GitHub account: $GITHUB_USERNAME"
fi

if ! gh auth status 2>&1; then
    echo "ERROR: GitHub CLI authentication failed" >&2
    exit 1
fi

# Configure SSH for git operations if keys provided
if [ -n "$SSH_KEY_PATHS" ]; then
    eval "$(ssh-agent -s)" > /dev/null 2>&1

    IFS=: read -ra KEYS <<< "$SSH_KEY_PATHS"
    for key in "${KEYS[@]}"; do
        if ! ssh-add "$key" 2>&1; then
            echo "ERROR: Failed to add SSH key: $key" >&2
            exit 1
        fi
    done
else
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo "⚠  WARNING: Running without SSH keys"
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
    echo "Git push operations will NOT work."
    echo ""
    echo "To add SSH keys, use one of these methods:"
    echo ""
    echo "  1. Use github_ keys (recommended):"
    echo "     ccy --ssh-key ~/.ssh/github_<alias>"
    echo ""
    echo "     Set up github_ keys with:"
    echo "     ansible-playbook playbooks/imports/optional/common/play-github-cli-multi.yml"
    echo ""
    echo "  2. Use existing SSH key:"
    echo "     ccy --ssh-key ~/.ssh/id_ed25519"
    echo ""
    echo "════════════════════════════════════════════════════════════════════════════════"
    echo ""
fi

# Add GitHub host keys to avoid SSH verification prompts
mkdir -p ~/.ssh
chmod 700 ~/.ssh
if ! curl -sL https://api.github.com/meta | jq -r '.ssh_keys | .[]' | sed -e 's/^/github.com /' >> ~/.ssh/known_hosts 2>&1; then
    echo "WARNING: Failed to fetch GitHub SSH host keys. SSH operations may require manual verification."
else
    chmod 600 ~/.ssh/known_hosts
fi

# Set sandbox mode to bypass root detection
export IS_SANDBOX=1

# Update Claude Code to latest version on every launch
echo "Checking for Claude Code updates..."
if npm update -g @anthropic-ai/claude-code 2>&1 | grep -v "npm WARN"; then
    echo "✓ Claude Code is up to date"
else
    echo "✓ Claude Code updated successfully"
fi
echo ""

# Execute the command
exec "$@"
