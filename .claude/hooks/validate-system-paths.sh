#!/bin/bash
# Claude Code Pre-Tool Hook: Validate System Paths
#
# Prevents direct editing of deployed system files.
# Claude should only edit project files and use Ansible to deploy.
#
# Exit Code 2 = Block operation (stderr fed to Claude)
# Exit Code 0 = Allow operation

set -e

# Read tool use JSON from stdin
TOOL_USE=$(cat)

# Extract tool name
TOOL_NAME=$(echo "$TOOL_USE" | jq -r '.tool // empty')

# Only validate Edit and Write operations
if [[ "$TOOL_NAME" != "Edit" ]] && [[ "$TOOL_NAME" != "Write" ]]; then
    exit 0  # Allow - not a file modification tool
fi

# Extract file path from tool parameters
FILE_PATH=$(echo "$TOOL_USE" | jq -r '.params.file_path // empty')

if [[ -z "$FILE_PATH" ]]; then
    exit 0  # Allow - no file path to validate
fi

# System paths that should NOT be edited directly
BLOCKED_PATHS=(
    "/etc/"
    "/usr/"
    "/var/"
    "/opt/"
    "/root/"
    "/home/"
)

# Check if file path matches any blocked pattern
for BLOCKED_PATH in "${BLOCKED_PATHS[@]}"; do
    if [[ "$FILE_PATH" == "$BLOCKED_PATH"* ]]; then
        # Extract just the path portion after /workspace for clearer messaging
        RELATIVE_PATH="${FILE_PATH#$CLAUDE_PROJECT_DIR/}"

        # Determine project equivalent path
        PROJECT_PATH=""
        if [[ "$FILE_PATH" == /var/local/* ]]; then
            PROJECT_PATH="files/var/local/..."
        elif [[ "$FILE_PATH" == /etc/* ]]; then
            PROJECT_PATH="files/etc/..."
        elif [[ "$FILE_PATH" == /usr/local/* ]]; then
            PROJECT_PATH="files/usr/local/..."
        fi

        # Return blocking JSON response
        cat >&2 <<EOF
{
  "hookSpecificOutput": {
    "permissionDecision": "deny",
    "permissionDecisionReason": "❌ BLOCKED: Direct editing of deployed system files is not allowed.

Target: $FILE_PATH

This is a deployed file on the actual system filesystem. Directly editing
deployed files bypasses version control and creates configuration drift.

✓ CORRECT APPROACH:
1. Edit the project file in: $PROJECT_PATH
2. Deploy using Ansible:
   ansible-playbook playbooks/imports/optional/common/play-distrobox-playwright.yml

This ensures:
- Changes are version controlled
- Changes can be reviewed and tested
- Changes are reproducible across environments
- Configuration remains consistent

See CLAUDE.md 'Development Principles' section for more details."
  }
}
EOF
        exit 2  # Exit code 2 blocks the operation
    fi
done

# File path is within project directory - allow operation
exit 0
