#!/usr/bin/env bash
# Claude Code Hook: Auto-lint Ansible Playbooks
# Triggers: PostToolUse on Edit|Write operations
# Purpose: Automatically lint Ansible playbook files after editing

set -euo pipefail

# Read JSON input from stdin
INPUT=$(cat)

# Extract file path using jq
FILE_PATH=$(echo "$INPUT" | jq -r '.tool_input.file_path // empty')

# Exit successfully if no file path
if [ -z "$FILE_PATH" ]; then
    exit 0
fi

# Only process YAML files in playbooks directory
if [[ ! "$FILE_PATH" =~ /playbooks/.*\.ya?ml$ ]]; then
    exit 0
fi

# Get the project directory
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-.}"

# Change to project directory to ensure relative paths work
cd "$PROJECT_DIR" || exit 0

# Check if the lint script exists
if [ ! -x "./scripts/lint" ]; then
    # Silently skip if lint script doesn't exist (project may not be fully set up)
    exit 0
fi

# Get relative path for cleaner output
REL_PATH="${FILE_PATH#$PROJECT_DIR/}"

# Run lint on the specific file
echo "🔍 Linting $REL_PATH..." >&2

# Run ansible-lint using our project's lint script
# Capture output and exit code
if LINT_OUTPUT=$(./scripts/lint "$FILE_PATH" 2>&1); then
    # Linting passed
    echo "✓ No ansible-lint issues found in $REL_PATH" >&2
    exit 0
else
    LINT_EXIT_CODE=$?

    # Linting failed - show the output
    echo "" >&2
    echo "❌ Ansible-lint found issues in $REL_PATH:" >&2
    echo "" >&2
    echo "$LINT_OUTPUT" >&2
    echo "" >&2
    echo "Please fix the linting issues above before proceeding." >&2
    echo "Run './scripts/lint $FILE_PATH' to see detailed violations." >&2

    # Exit with code 2 to block the operation
    exit 2
fi
