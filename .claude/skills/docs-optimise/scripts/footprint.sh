#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See the daemon clone's CLAUDE/LLM-INSTALL.md,
# "Which Files Under .claude/ Are Yours?", for the full list and the
# linter exclusions.
#
# footprint.sh - Thin wrapper over `hooks-daemon docs-qa --footprint --json`.
# Measures which instruction files load at session start and which on demand.
# It reads, never rewrites, and judges nothing. Feeds the /docs-optimise
# skill's proposal.
#
# Usage:
#   ./footprint.sh
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# The docs-qa skill's finder is the single resolver for the daemon CLI.
# shellcheck disable=SC1091  # resolved at runtime relative to this script
source "$SCRIPT_DIR/../../docs-qa/scripts/_locate-cli.sh"

"$DOCS_QA_CLI" docs-qa --footprint --json
