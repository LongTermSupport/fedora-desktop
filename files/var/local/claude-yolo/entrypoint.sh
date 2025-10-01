#!/bin/bash
# Claude Code YOLO Container Entrypoint
# In rootless Docker, UID 0 = host user, so this is safe

set -e

# Create config directories
mkdir -p ~/.claude

# Copy credentials if available
if [ -f "/tmp/claude-config-import/credentials.json" ]; then
    cp "/tmp/claude-config-import/credentials.json" ~/.claude/.credentials.json
    chmod 600 ~/.claude/.credentials.json
fi

# Copy settings if available
if [ -f "/tmp/claude-config-import/settings.json" ]; then
    cp "/tmp/claude-config-import/settings.json" ~/.claude/settings.json
else
    echo "{}" > ~/.claude/settings.json
fi

# Copy global config if available
if [ -f "/tmp/claude-config-import/config.json" ]; then
    cp "/tmp/claude-config-import/config.json" ~/.claude.json
else
    echo '{"numStartups":0}' > ~/.claude.json
fi

# Set sandbox mode to bypass root detection
export IS_SANDBOX=1

# Execute the command
exec "$@"
