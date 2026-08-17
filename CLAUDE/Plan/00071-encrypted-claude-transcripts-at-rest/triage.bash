#!/usr/bin/env bash
#
# Plan 00071 — Claude Code state posture triage.
#
# READ-ONLY. Changes nothing. Safe to re-run at any time, on a live system,
# with sessions running. Gathers facts only — it renders NO verdict; the
# pass/fail gate is acceptance.bash.
#
# Writes its report to <plan folder>/logs/claude-state-triage.log.
#
# Runs in either place, and says which it is:
#   - inside a CCY container -> sees the project store at /workspace/.claude/ccy
#   - on the HOST            -> additionally sees the desktop store at ~/.claude
#
set -euo pipefail

# --- Argument parsing happens BEFORE any environment resolution, so --help
#     still works on exactly the machine that is too broken to resolve paths.
usage() {
    cat <<'EOF'
Plan 00071 triage — report the on-disk posture of Claude Code state.

USAGE:
    triage.bash [--help]

WHAT IT REPORTS:
    - which store(s) are visible from here (CCY container vs HOST)
    - group/other-readable file and directory counts per store
    - per-subdirectory permission census (the leak is not in projects/)
    - transcript corpus size and age spread vs the retention setting
    - whether a umask is set anywhere in the CCY launch path
    - whether CACHEDIR.TAG is present (backup-tool exclusion marker)
    - a secret-pattern census run with the real grep binary (see SCANNER TRAP)

SCANNER TRAP (Plan 00071, F9):
    In the CCY container, `grep` is a shell FUNCTION wrapping `ugrep
    --ignore-files`, which honours .gitignore. `.claude/ccy/.gitignore` is a
    bare `*`, so every recursive grep of the state tree returns ZERO hits with
    exit 0 — indistinguishable from "clean". This script resolves the real grep
    binary with `type -P` and self-tests that exact code path against a planted
    canary before any census result is believed.

READ-ONLY: no file is created, moved, deleted or chmod'd anywhere outside the
report directory.
EOF
}

case "${1:-}" in
    -h | --help)
        usage
        exit 0
        ;;
    "") ;;
    *)
        echo "ERROR: unknown option '$1'. See --help." >&2
        exit 1
        ;;
esac

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/claude-state-triage.log"
exec > >(tee "$LOG") 2>&1

# Resolve the real grep EXECUTABLE, bypassing any function/alias of the same
# name. `type -P` searches $PATH only, so an interactive shell's ugrep wrapper
# (which honours .gitignore, and would silently see nothing here) cannot win.
# `command grep` is not usable for this: it is a shell builtin construct, so
# neither find -exec nor xargs can invoke it.
GREP_BIN="$(type -P grep)"
if [ -z "$GREP_BIN" ]; then
    echo "ERROR: no grep executable found in PATH. Cannot census the state tree." >&2
    exit 1
fi

# THE census primitive. Everything that greps the state tree goes through this
# one function, including the scanner self-test — a self-test that exercises a
# different code path than the real census proves nothing. (First revision of
# this script learned that the hard way: the self-test passed while the census
# silently reported 0 hits for every pattern, because `find -exec command grep`
# cannot work — `command` is a shell builtin, not an executable.)
#
# Prints the count of MATCHING FILES on stdout. Never prints a matched value,
# so the report can never become a fresh copy of the secret it is hunting.
scan_count_files() {
    local root="$1" pattern="$2" glob="${3:-*}"
    find "$root" -type f -name "$glob" -print0 |
        xargs -0 -r "$GREP_BIN" -lE "$pattern" |
        wc -l
}

# A non-zero exit status is DATA, not a failure. Capture it and carry on.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

echo "=========================================================="
echo " Plan 00071 — Claude Code state posture triage"
echo " generated: $(date -Is)"
echo " host:      $(uname -srm)"
echo "=========================================================="
echo

# ---------------------------------------------------------------------------
# Locate the stores
# ---------------------------------------------------------------------------
CCY_STORE=""
DESKTOP_STORE=""

if [ -d /workspace/.claude/ccy ]; then
    CCY_STORE="/workspace/.claude/ccy"
    CONTEXT="CCY container (project store visible; host ~/.claude is NOT)"
elif [ -d "$PLAN_DIR/../../../.claude/ccy" ]; then
    CCY_STORE="$(cd "$PLAN_DIR/../../../.claude/ccy" && pwd)"
    CONTEXT="HOST (project store visible via the repo working tree)"
else
    CONTEXT="neither store found at the expected paths"
fi

# The desktop store only exists as a real directory on the HOST. Inside CCY,
# /root/.claude is a symlink into the project store — reporting it as a second
# store would double-count, so it is deliberately excluded.
if [ -d "$HOME/.claude" ] && [ ! -L "$HOME/.claude" ]; then
    DESKTOP_STORE="$HOME/.claude"
fi

echo "### context"
echo "$CONTEXT"
echo "  CCY store:     ${CCY_STORE:-(not visible from here)}"
echo "  Desktop store: ${DESKTOP_STORE:-(not visible from here — run on the HOST)}"
echo

if [ -z "$CCY_STORE" ] && [ -z "$DESKTOP_STORE" ]; then
    echo "ERROR: no Claude Code state directory found. Nothing to report." >&2
    echo "  Expected /workspace/.claude/ccy (in CCY) or ~/.claude (on the HOST)." >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Scanner self-test — a blind scanner reports "clean", which is
# indistinguishable from safety. Prove it can see before believing a census.
# ---------------------------------------------------------------------------
scanner_self_test() {
    local canary_dir canary_file token rc
    canary_dir="$(mktemp -d)"
    token="CCY-CANARY-$$-DO-NOT-MATCH-ELSEWHERE"
    canary_file="$canary_dir/canary.jsonl"
    printf '{"probe":"%s"}\n' "$token" > "$canary_file"
    # Mimic the real census: a gitignore that would blind an ignore-aware grep.
    printf '*\n' > "$canary_dir/.gitignore"

    echo "planted canary in $canary_dir (gitignored, as the real store is)"
    # Runs the REAL census primitive against the canary, with the same *.jsonl
    # glob the real census uses. If this says 1, the census below can be believed.
    local seen
    seen="$(scan_count_files "$canary_dir" "$token" '*.jsonl')"
    echo "scan_count_files() found: $seen file(s) (expected exactly 1)"
    if [ "$seen" = "1" ]; then
        rc=0
        echo "RESULT: the census primitive SEES the canary — results are trustworthy."
    else
        rc=1
        echo "RESULT: the census primitive is BLIND to the canary — every 'clean'"
        echo "        result below is meaningless. Do not trust this report."
    fi

    # Show the trap explicitly, for the record.
    echo
    echo "for comparison, the shell's own grep resolves to:"
    type grep || echo "(grep is not a shell function here)"  # FAIL-FAST-OK: a report line — `type` failing IS the finding this probe exists to record

    rm -rf "$canary_dir"
    return "$rc"
}

echo "### scanner self-test (must pass before any census below is believed)"
if scanner_self_test; then
    SCANNER_OK=1
else
    SCANNER_OK=0
fi
echo

# ---------------------------------------------------------------------------
# Per-store posture
# ---------------------------------------------------------------------------
posture_census() {
    local store="$1"
    local total_files total_dirs open_files open_dirs

    total_files="$(find "$store" -type f -printf '.' | wc -c)"
    total_dirs="$(find "$store" -type d -printf '.' | wc -c)"
    open_files="$(find "$store" -type f -perm /077 -printf '.' | wc -c)"
    open_dirs="$(find "$store" -type d -perm /077 -printf '.' | wc -c)"

    echo "store: $store"
    echo "  mode of store root: $(stat -c '%a' "$store")"
    echo "  files: $total_files total, $open_files with group/other bits set"
    echo "  dirs:  $total_dirs total, $open_dirs with group/other bits set"
    echo
    echo "  READ THIS FOR: the count of group/other-permissioned entries."
    echo "    0 / 0 = closed. Anything else is readable by every local user."
    echo
    echo "  per-subdirectory breakdown (mode of dir, then file modes within):"
    local sub
    for sub in "$store"/*/; do
        [ -d "$sub" ] || continue
        printf '    %-22s dir=%s  files: ' "$(basename "$sub")" "$(stat -c '%a' "$sub")"
        find "$sub" -type f -printf '%m\n' | sort | uniq -c | tr '\n' ' '
        printf '\n'
    done
}

size_and_age_census() {
    local store="$1"
    local proj="$store/projects"
    if [ ! -d "$proj" ]; then
        echo "no projects/ directory under $store"
        return 0
    fi
    echo "transcript corpus under $proj:"
    echo "  files: $(find "$proj" -name '*.jsonl' -printf '.' | wc -c)"
    echo "  bytes: $(find "$proj" -name '*.jsonl' -printf '%s\n' | awk '{t += $1} END {print t + 0}')"
    echo
    echo "  age spread (count by mtime date — compare against cleanupPeriodDays):"
    find "$proj" -name '*.jsonl' -printf '%TY-%Tm-%Td\n' | sort | uniq -c
    echo
    echo "  READ THIS FOR: whether the oldest transcript is inside the retention"
    echo "    window. An entry older than cleanupPeriodDays means the sweep is"
    echo "    not running. Files NEVER swept (history.jsonl, stats-cache.json)"
    echo "    are reported separately below."
    echo
    local never
    for never in history.jsonl stats-cache.json; do
        if [ -f "$store/$never" ]; then
            echo "  never-swept: $never  $(stat -c '%s bytes, mode %a' "$store/$never")"
        fi
    done
}

secret_census() {
    local store="$1"
    if [ "$SCANNER_OK" -ne 1 ]; then
        echo "SKIPPED: scanner self-test failed, so a 'clean' result would be a lie."
        return 0
    fi
    echo "pattern census (counts only — no matched value is ever printed):"
    local pattern
    # Deliberately counts FILES, never printing a match, so the report itself
    # never becomes a new copy of the secret it is looking for.
    # ERE, because scan_count_files() uses `grep -lE`. A BRE-style \{20\} here
    # would match literally and silently report 0 — the same false-clean class
    # of bug the self-test above exists to catch.
    for pattern in 'ANSIBLE_VAULT' 'BEGIN [A-Z ]*PRIVATE KEY' 'ghp_[A-Za-z0-9]{20}' \
        'github_pat_[A-Za-z0-9_]{20}' 'sk-ant-[A-Za-z0-9-]{20}' 'AKIA[0-9A-Z]{16}'; do
        local hits
        hits="$(scan_count_files "$store" "$pattern" '*.jsonl')"
        printf '    %-34s %s file(s)\n' "$pattern" "$hits"
    done
    echo
    echo "  READ THIS FOR: whether the capture mechanism is live. A non-zero"
    echo "    count for any pattern means file contents reach the transcript"
    echo "    verbatim — even if today's hits are ciphertext rather than a"
    echo "    live plaintext credential."
}

for STORE in "$CCY_STORE" "$DESKTOP_STORE"; do
    [ -n "$STORE" ] || continue
    echo "=========================================================="
    echo " STORE: $STORE"
    echo "=========================================================="
    probe "permission posture" posture_census "$STORE"
    probe "corpus size and age" size_and_age_census "$STORE"
    probe "secret pattern census" secret_census "$STORE"
    probe "CACHEDIR.TAG present?" ls -l "$STORE/CACHEDIR.TAG"
done

# ---------------------------------------------------------------------------
# Launch-path facts (repo source, not live state)
# ---------------------------------------------------------------------------
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"

echo "=========================================================="
echo " LAUNCH PATH (repo source)"
echo "=========================================================="

umask_census() {
    local f
    for f in "$REPO_ROOT/files/var/local/claude-yolo/entrypoint.sh" \
        "$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"; do
        if [ -f "$f" ]; then
            printf '  %-58s umask lines: %s\n' \
                "${f#"$REPO_ROOT"/}" "$(command grep -c 'umask' "$f")"
        fi
    done
    echo
    echo "  READ THIS FOR: a 0 means every file Claude Code creates inherits the"
    echo "    default umask (022), i.e. group/other-readable, forever."
}
probe "umask set anywhere in the CCY launch path?" umask_census
probe "current shell umask" umask

echo "=========================================================="
echo " END OF REPORT"
echo
echo " READ FIRST: the 'permission posture' section of each store, and the"
echo " scanner self-test at the top. Everything else is supporting detail."
echo
echo " Full report saved to: $LOG"
echo "=========================================================="
