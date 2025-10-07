#!/bin/bash
# Claude Code YOLO Container Entrypoint
# In rootless Docker, UID 0 = host user, so this is safe

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


# Copy Claude Code config files
mkdir -p ~/.claude
cp /tmp/claude-config-import/credentials.json ~/.claude/.credentials.json
cp /tmp/claude-config-import/settings.json ~/.claude/settings.json
cp /tmp/claude-config-import/config.json ~/.claude.json
chmod 600 ~/.claude/.credentials.json

# Configure git
cp /tmp/claude-config-import/gitconfig ~/.gitconfig

# Configure GitHub CLI with token
mkdir -p ~/.config/gh
TEMP_TOKEN="$GH_TOKEN"
unset GH_TOKEN
if ! echo "$TEMP_TOKEN" | gh auth login --with-token 2>&1; then
    echo "ERROR: gh auth login failed" >&2
    exit 1
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
    echo "WARNING: No SSH keys provided. Git push operations will not work."
    echo "To add SSH keys: ccy --ssh-key ~/.ssh/id_ed25519"
fi

# Set sandbox mode to bypass root detection
export IS_SANDBOX=1

# Execute the command
exec "$@"
