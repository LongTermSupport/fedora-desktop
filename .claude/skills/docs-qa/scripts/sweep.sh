#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
# sweep.sh - Thin wrapper over `hooks-daemon docs-qa --sweep --json`.
# A DETERMINISTIC finder: it lists candidate SSoT violations, it never
# judges content and never gates anything. Feeds the hooks-daemon-docs-qa
# agent's worklist.
#
# Usage:
#   ./sweep.sh
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091  # resolved at runtime relative to this script
source "$SCRIPT_DIR/_locate-cli.sh"

"$DOCS_QA_CLI" docs-qa --sweep --json
