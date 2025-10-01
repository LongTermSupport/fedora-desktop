#!/bin/bash
# Claude Code YOLO Container Entrypoint
# Copies configuration files from mounted temp directory

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

# Execute the command passed to the container
exec "$@"
