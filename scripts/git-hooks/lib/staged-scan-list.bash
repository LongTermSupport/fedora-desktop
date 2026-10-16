#!/usr/bin/env bash
# staged-scan-list.bash — the staged paths the pre-commit secret scan reads, one per line on
# stdout. Run (not sourced) by scripts/git-hooks/pre-commit from the repository root.
#
# Every added, copied, modified, renamed or type-changed path (ACMRT: a `git mv` plus an edit is
# R, and must be scanned; see the hook) except src-go/vendor. That tree is third-party Go source
# exactly as `go mod vendor` writes it (Plan 00204), whose authors' contact addresses are not
# ours to replace. The exemption holds only for a verified tree: when anything under
# src-go/vendor is staged (a deletion included), qa-go-deps.bash judges the STAGED src-go,
# checked out of the index into a scratch directory, so a working copy put back after staging
# cannot pass a tampered file. A failing gate fails this script, and so the commit.
#
# stdout: the paths. stderr: the scan banner and the gate's own output.
# Exit: 0 the list is complete, non-zero the gate failed or git did.

set -euo pipefail

echo "🔍 Scanning staged changes for sensitive information..." >&2
gate="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/qa-go-deps.bash"
if ! git diff --cached --quiet -- src-go/vendor; then
    staged="$(mktemp -d)"
    trap 'rm -rf "$staged"' EXIT
    git ls-files -z -- src-go | git checkout-index -z --stdin --prefix="$staged/"
    if ! "$gate" --root "$staged" >&2; then
        echo "✗ src-go/vendor is staged, but the staged tree fails qa-go-deps (above), so the" >&2
        echo "  secret scan will not exempt it. Fix: cd src-go && go mod tidy && go mod vendor" >&2
        exit 1
    fi
fi
git diff --cached --name-only --diff-filter=ACMRT -- . ':(exclude)src-go/vendor'
