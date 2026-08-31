#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
# find-comment-blocks.sh - Thin wrapper over
# `hooks-daemon find-comment-blocks PATHS... --json`. A DETERMINISTIC
# finder feeding the hooks-daemon-docs-qa agent's Decision-7 comment hunt
# (verbose comments that function as documentation) — it lists candidates,
# it never judges content and never gates anything.
#
# Usage:
#   ./find-comment-blocks.sh <path> [path...]
#

set -euo pipefail

if [ "$#" -eq 0 ]; then
    echo "Usage: $0 <path> [path...]" >&2
    exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091  # resolved at runtime relative to this script
source "$SCRIPT_DIR/_locate-cli.sh"

"$DOCS_QA_CLI" find-comment-blocks "$@" --json
