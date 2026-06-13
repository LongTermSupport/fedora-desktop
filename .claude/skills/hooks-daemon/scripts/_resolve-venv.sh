#!/bin/bash
#
# _resolve-venv.sh — Plan 00104 Phase 5 Task 5.4 thin shim.
#
# Delegates to the canonical bash library at
# ``$DAEMON_DIR/scripts/lib/resolve_venv.sh`` (Plan 00104 Phase 4 Task 4.1).
# Preserves the historical contract:
#   - REQUIRES: DAEMON_DIR is set before sourcing.
#   - SETS:     PYTHON — path to the venv's bin/python.
#   - On failure: stderr directive + non-zero return/exit (5 = no usable venv,
#     1 = misuse, 2 = invalid daemon_dir).
#
# Sourced by daemon-cli.sh, health-check.sh, init-handlers.sh.

if [ -z "${DAEMON_DIR:-}" ]; then
    echo "❌ _resolve-venv.sh: DAEMON_DIR must be set before sourcing" >&2
    # shellcheck disable=SC2317  # unreachable when sourced, intentional exec fallback
    return 1 2>/dev/null || exit 1
fi

_rv_lib="$DAEMON_DIR/scripts/lib/resolve_venv.sh"
if [ ! -f "$_rv_lib" ]; then
    echo "❌ _resolve-venv.sh: canonical library missing at $_rv_lib" >&2
    echo "   Reinstall the daemon so scripts/lib/resolve_venv.sh is present." >&2
    unset _rv_lib
    # shellcheck disable=SC2317  # unreachable when sourced, intentional exec fallback
    return 5 2>/dev/null || exit 5
fi

# shellcheck disable=SC1090  # path is computed at runtime from DAEMON_DIR
source "$_rv_lib"

if PYTHON=$(resolve_venv_python "$DAEMON_DIR"); then
    # shellcheck disable=SC2034  # PYTHON is exported for the sourcing caller.
    export PYTHON
    unset _rv_lib
else
    _rv_rc=$?
    unset _rv_lib
    # shellcheck disable=SC2317  # unreachable when sourced, intentional exec fallback
    return "$_rv_rc" 2>/dev/null || exit "$_rv_rc"
fi
