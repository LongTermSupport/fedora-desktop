#!/usr/bin/env bash
# qa-hook-handlers.bash — the project-owned hooks-daemon handlers pass their own tests.
#
# Runs every test under .claude/hooks/handlers/**/tests with the daemon's interpreter: the
# handlers import claude_code_hooks_daemon, which only the daemon's own venv has. That venv is
# found the way the daemon finds it (<daemon>/scripts/lib/resolve_venv.sh python), never by a
# hardcoded venv name. The daemon is gitignored, so a linked worktree reads the main checkout's.
#
# Fails, naming the fix, when the daemon, its venv or its pytest (a dev extra the daemon does not
# install by default) is missing. The one skip is GitHub Actions, whose checkout has no daemon.
#
# stdout: HOOK-HANDLERS-OK <n> test(s) ..., or HOOK-HANDLERS-SKIPPED ...; the rest is stderr.
# Exit: 0 passed or skipped on GitHub Actions, 1 a test failed, 2 the run could not complete.

set -euo pipefail

die() { printf 'qa-hook-handlers: %s\n' "$1" >&2; exit 2; }

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    echo "HOOK-HANDLERS-SKIPPED GitHub Actions has no hooks daemon (it is gitignored); run qa-all locally"
    exit 0
fi

DAEMON="${HOOKS_DAEMON_ROOT_DIR:-$ROOT/.claude/hooks-daemon}"
if [[ ! -d "$DAEMON" ]]; then
    COMMON="$(git -C "$ROOT" rev-parse --path-format=absolute --git-common-dir)"
    DAEMON="$(dirname "$COMMON")/.claude/hooks-daemon"
fi
FIX="provision the hooks daemon (the hooks-daemon skill: provision) so $DAEMON exists with its venv"
[[ -x "$DAEMON/scripts/lib/resolve_venv.sh" ]] || die "no hooks daemon at $DAEMON: $FIX"
PY="$("$DAEMON/scripts/lib/resolve_venv.sh" python "$DAEMON")" || die "the daemon has no venv: $FIX"
"$PY" -c 'import claude_code_hooks_daemon' || die "$PY cannot import claude_code_hooks_daemon: $FIX"
"$PY" -c 'import pytest' 2> /dev/null ||
    die "pytest (the daemon's dev extra) is missing from its venv $PY: provision the daemon with its dev extra, then re-run"

OUT="$(mktemp)"
trap 'rm -f "$OUT"' EXIT
cd "$ROOT"
status=0
"$PY" -m pytest .claude/hooks/handlers -p no:cacheprovider -q --color=no > "$OUT" 2>&1 || status=$?
cat "$OUT" >&2
[[ "$status" -eq 0 ]] || { echo "qa-hook-handlers: a handler test failed (pytest exit $status)" >&2; exit 1; }
COUNT="$(grep -Eo '^[0-9]+ passed' "$OUT" | cut -d' ' -f1)" || die "pytest passed but reported no count"
echo "HOOK-HANDLERS-OK $COUNT test(s) under .claude/hooks/handlers"
