#!/bin/bash
#
# DAEMON-OWNED FILE - do not edit. Deployed into your project by the
# claude-code-hooks-daemon installer and refreshed on every upgrade, so local
# changes are discarded. See CLAUDE/LLM-INSTALL.md, "Which Files Under
# .claude/ Are Yours?", for the full list and the linter exclusions.
#
# _locate-cli.sh - Sourced helper: sets DOCS_QA_CLI to an executable that
# runs the daemon CLI (`"$DOCS_QA_CLI" <subcommand> [args...]`), or exits
# non-zero with a diagnostic. Not a bootstrap-tracked script (unlike
# upgrade.sh/daemon-cli.sh/health-check.sh/init-handlers.sh) — it is a thin
# finder wrapper, so it stays simple and delegates to whichever CLI entry
# point the project already has.
#

set -euo pipefail

_docs_qa_project_root() {
    local root
    root="$(pwd)"
    while [ "$root" != "/" ]; do
        if [ -f "$root/.claude/hooks-daemon.yaml" ]; then
            echo "$root"
            return 0
        fi
        root="$(dirname "$root")"
    done
    return 1
}

if ! DOCS_QA_PROJECT_ROOT="$(_docs_qa_project_root)"; then
    echo "Not in a hooks daemon project (no .claude/hooks-daemon.yaml found in any parent directory)" >&2
    exit 1
fi

# Self-install mode (this daemon's own repo): bin/hooks-daemon at the root.
if [ -x "$DOCS_QA_PROJECT_ROOT/bin/hooks-daemon" ]; then
    DOCS_QA_CLI="$DOCS_QA_PROJECT_ROOT/bin/hooks-daemon"
# Normal client install: delegate to the deployed daemon-cli.sh sibling.
elif [ -x "$DOCS_QA_PROJECT_ROOT/.claude/skills/hooks-daemon/scripts/daemon-cli.sh" ]; then
    DOCS_QA_CLI="$DOCS_QA_PROJECT_ROOT/.claude/skills/hooks-daemon/scripts/daemon-cli.sh"
else
    echo "Could not locate the hooks-daemon CLI (tried bin/hooks-daemon and .claude/skills/hooks-daemon/scripts/daemon-cli.sh)" >&2
    exit 1
fi

export DOCS_QA_CLI
