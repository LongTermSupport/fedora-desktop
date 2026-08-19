#!/usr/bin/env bash
#
# Plan 00076 — confirm the rewritten rclone helpers still work. HOST ONLY.
#
# The parser fix was not cosmetic: `query_stats` (rclone-tail) and `query_cache`
# (rclone-cache-status) used to let Python's stdout flow straight out of the `if`
# condition. They now capture it and re-emit it. If that re-emission is wrong the
# scripts do not crash — they print `ERR|parse failed`, or nothing, which is
# exactly the shape of failure this repo treats as worse than an error.
#
# So this exercises the DEPLOYED scripts, not a copy of the construct.
#
# Read-only: prints one snapshot from each, changes nothing.
#
# Usage: acceptance.bash [--help]

set -uo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00076 — acceptance for the rewritten rclone helpers (HOST ONLY)

Usage: acceptance.bash [--help]

Checks, in order:
  1. qa-deployed-drift.bash — the repo and ~/.local/bin/ agree
  2. rclone-tail --once     — exits 0, emits a snapshot, and does NOT say
                              "parse failed" (which would mean the captured
                              Python output never made it back out)
  3. rclone-cache-status    — same, plus a rendered byte figure, which is the
                              rewritten fmt_bytes() fallback path

If the rclone RC is not reachable the scripts legitimately report
"rc unreachable" — that is NOT a pass and NOT a failure, and this script says
INCONCLUSIVE rather than vouching for a path it could not exercise.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: acceptance.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "ERROR: this looks like a CCY container (/workspace)." >&2
    echo "  The deployed scripts live on the HOST. Run this there." >&2
    exit 1
fi

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/acceptance.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

FAIL=0
INCONCLUSIVE=0

echo "=============================================================="
echo "Plan 00076 — acceptance"
echo "=============================================================="

# --- 1. the repo and the host agree ------------------------------------------
echo
echo "### 1. deployed-drift gate"
if "$REPO_ROOT/scripts/qa-deployed-drift.bash"; then
    echo "  OK — deployed copies match the repo"
else
    echo "  FAIL — run deploy.bash first (play-rclone.yml)"
    FAIL=1
fi

# --- shared runner ------------------------------------------------------------
# Runs a deployed script, keeps its status AND its output, and judges the three
# outcomes separately. A non-zero exit and a zero exit that printed
# "parse failed" are different failures and are reported as such.
check_script() {
    local label="$1" bin="$2" must_match="$3"
    shift 3
    local out rc

    echo
    echo "### $label"
    if [ ! -x "$bin" ]; then
        echo "  FAIL — not deployed or not executable: $bin"
        FAIL=1
        return 0
    fi

    if out=$("$bin" "$@" 2>&1); then rc=0; else rc=$?; fi

    echo "  exit status: $rc"
    echo "  --- output ---"
    printf '%s\n' "$out" | sed 's/^/  | /'
    echo "  --------------"

    if [ "$rc" -ne 0 ]; then
        echo "  FAIL — non-zero exit"
        FAIL=1
        return 0
    fi
    if [ -z "$out" ]; then
        echo "  FAIL — exited 0 with no output at all"
        FAIL=1
        return 0
    fi
    # The specific way the rewrite could be wrong: the captured stdout never
    # reaches the caller, so the failure branch fires on a successful call.
    if printf '%s' "$out" | grep -q "parse failed"; then
        echo "  FAIL — 'parse failed' present: the captured Python output did"
        echo "         not make it back out of the rewritten function"
        FAIL=1
        return 0
    fi
    if printf '%s' "$out" | grep -qi "rc unreachable\|rejected credentials\|credential missing"; then
        echo "  INCONCLUSIVE — the RC did not answer, so the rewritten success"
        echo "                 path was never exercised. Start the mounts and"
        echo "                 re-run; do not read this as a pass."
        INCONCLUSIVE=1
        return 0
    fi
    if ! printf '%s' "$out" | grep -qE "$must_match"; then
        echo "  FAIL — output did not contain the expected rendered value"
        echo "         (pattern: $must_match)"
        FAIL=1
        return 0
    fi
    echo "  OK — success path exercised and rendered"
}

# `--once` prints one snapshot. A healthy line carries a byte figure from the
# rewritten query_stats payload, e.g. "12MiB/40MiB", or reports the mount idle.
check_script "2. rclone-tail --once" "$HOME/.local/bin/rclone-tail" \
    "[0-9]+([.][0-9]+)?[KMGT]?i?B|idle" --once

# rclone-cache-status renders byte figures through the rewritten fmt_bytes().
check_script "3. rclone-cache-status" "$HOME/.local/bin/rclone-cache-status" \
    "[0-9]+([.][0-9]+)?[KMGT]?i?B"

echo
echo "=============================================================="
if [ "$FAIL" -ne 0 ]; then
    echo "VERDICT: FAIL — see the sections marked FAIL above."
    echo "=============================================================="
    exit 1
fi
if [ "$INCONCLUSIVE" -ne 0 ]; then
    echo "VERDICT: INCONCLUSIVE — nothing failed, but the rewritten success"
    echo "         path was not exercised because the RC did not answer."
    echo "=============================================================="
    exit 2
fi
echo "VERDICT: PASS — both rewritten functions emit their payload correctly."
echo "=============================================================="
