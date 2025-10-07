#!/bin/bash
# Claude Code YOLO Container Entrypoint
# In rootless Docker, UID 0 = host user, so this is safe

set -e

# Verify GH_TOKEN is set
if [ -z "$GH_TOKEN" ]; then
    echo "ERROR: GH_TOKEN environment variable not set" >&2
    exit 1
fi

# Verify SSH directory exists
if [ ! -d ~/.ssh ]; then
    echo "ERROR: ~/.ssh directory not mounted" >&2
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
echo "$GH_TOKEN" | gh auth login --with-token

# Configure SSH for git operations
chmod 700 ~/.ssh
chmod 600 ~/.ssh/id_*
eval "$(ssh-agent -s)" > /dev/null
ssh-add ~/.ssh/id_*

# Set sandbox mode to bypass root detection
export IS_SANDBOX=1

# Execute the command
exec "$@"
