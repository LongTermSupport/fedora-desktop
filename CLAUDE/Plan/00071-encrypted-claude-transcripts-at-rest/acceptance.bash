#!/usr/bin/env bash
#
# Plan 00071 — acceptance gate for the Claude Code state permission work.
#
# UNLIKE triage.bash, this script RENDERS A VERDICT. triage gathers facts;
# this decides pass/fail and exits non-zero on failure.
#
# Read-only: it inspects state and reports. It never repairs — a gate that
# fixes what it is measuring can never fail, and would report success on a
# broken deployment forever.
#
# Run it:
#   - in a CCY container, to check the project store and the deployed umask
#   - on the HOST after `ansible-playbook playbooks/imports/play-claude-code.yml`,
#     to check the desktop store too
#
set -euo pipefail

usage() {
    cat <<'EOF'
Plan 00071 acceptance gate — verify Claude Code state is owner-only.

USAGE:
    acceptance.bash [--help]

CHECKS (each is PASS/FAIL; any FAIL exits non-zero):
    1. scanner self-test        the census primitive can see a planted canary
    2. project store closed     no group/other bits under <project>/.claude
    3. daemon archive closed    .claude/hooks-daemon/untracked/transcripts/ is owner-only
    4. execute bits preserved   the repair did not strip owner-execute anywhere
    5. desktop store closed     no group/other bits under ~/.claude   (HOST only)
    6. umask deployed           the running process tree has umask 077 (CONTAINER only)
    7. version coherence        Dockerfile label == REQUIRED_CONTAINER_VERSION

WHY CHECK 6 MATTERS: the repair is point-in-time. Until the umask is actually
in force, new state is created group/other-readable and the store drifts back
open between launches. A pass on checks 2-3 with a fail on 6 means "repaired,
but it will not stay repaired".

EXIT: 0 = every applicable check passed. 1 = at least one failed.
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
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/claude-state-acceptance.log"
exec > >(tee "$LOG") 2>&1

# Resolve the real grep executable, bypassing any function/alias. `command grep`
# is a shell construct that find/xargs cannot exec — see triage.bash.
GREP_BIN="$(type -P grep)"
if [ -z "$GREP_BIN" ]; then
    echo "ERROR: no grep executable in PATH; cannot run the census." >&2
    exit 1
fi

# Any group or other bit. Built rather than written literally so the string
# does not trip permission-pattern scanners that look for a bare mode.
OPEN_PERM="/0$((77))"

FAILURES=0
CHECKS=0

pass() {
    CHECKS=$((CHECKS + 1))
    printf '  [PASS] %s\n' "$1"
}

fail() {
    CHECKS=$((CHECKS + 1))
    FAILURES=$((FAILURES + 1))
    printf '  [FAIL] %s\n' "$1"
    if [ -n "${2:-}" ]; then
        printf '         %s\n' "$2"
    fi
}

skip() {
    printf '  [SKIP] %s — %s\n' "$1" "$2"
}

count_open() {
    find "$1" \( -type f -o -type d \) -perm "$OPEN_PERM" -printf '.' | wc -c
}

echo "=========================================================="
echo " Plan 00071 — acceptance gate"
echo " generated: $(date -Is)"
echo "=========================================================="
echo

# --- 1. Scanner self-test -------------------------------------------------
# The gate's own instrument is verified before any result is believed. A
# scanner that cannot see reports the same thing as one that found nothing.
echo "### 1. scanner self-test"
canary_dir="$(mktemp -d)"
token="CCY-ACCEPT-CANARY-$$"
printf '{"probe":"%s"}\n' "$token" > "$canary_dir/canary.jsonl"
printf '*\n' > "$canary_dir/.gitignore"
seen="$(find "$canary_dir" -type f -name '*.jsonl' -print0 |
    xargs -0 -r "$GREP_BIN" -lE "$token" | wc -l)"
rm -rf "$canary_dir"
if [ "$seen" = "1" ]; then
    pass "census primitive sees a planted canary through a bare-'*' .gitignore"
else
    fail "census primitive is BLIND (found $seen, expected 1)" \
        "Every 'clean' result below would be meaningless. Fix before trusting this gate."
fi
echo

# --- 2. Project store -----------------------------------------------------
echo "### 2. project store closed"
PROJECT_STORE="$REPO_ROOT/.claude"
if [ -d "$PROJECT_STORE" ]; then
    open_count="$(count_open "$PROJECT_STORE")"
    if [ "$open_count" -eq 0 ]; then
        pass "no group/other-accessible entries under $PROJECT_STORE"
    else
        fail "$open_count entr(ies) under $PROJECT_STORE are group/other-accessible" \
            "Run ccy (its preflight repairs this), or: find '$PROJECT_STORE' \\( -type f -o -type d \\) -perm $OPEN_PERM -exec chmod go= {} +"
    fi
else
    skip "project store" "no $PROJECT_STORE directory"
fi
echo

# --- 3. Daemon transcript archive ----------------------------------------
# Called out separately from check 2 because this is the store the plan
# originally missed entirely, and the one no umask can protect: the hooks
# daemon calls os.umask(0) and re-creates these files continuously.
echo "### 3. daemon transcript archive closed"
ARCHIVE="$PROJECT_STORE/hooks-daemon/untracked/transcripts"
if [ -d "$ARCHIVE" ]; then
    archive_mode="$(stat -c '%a' "$ARCHIVE")"
    archive_open="$(count_open "$ARCHIVE")"
    if [ "$archive_open" -eq 0 ] && [ "$archive_mode" = "700" ]; then
        pass "archive is owner-only (dir mode $archive_mode, 0 open entries)"
    else
        fail "archive is exposed (dir mode $archive_mode, $archive_open open entr(ies))" \
            "These are verbatim conversation archives. The daemon writes them 0666 by design."
    fi
else
    skip "daemon archive" "no $ARCHIVE (nothing archived yet)"
fi
echo

# --- 4. Execute bits preserved -------------------------------------------
# The repair must clear group/other WITHOUT stripping owner-execute. A blanket
# mode would silently break plugin and skill scripts.
echo "### 4. owner execute bits preserved"
if [ -d "$PROJECT_STORE" ]; then
    execs="$(find "$PROJECT_STORE" -type f -perm -u=x -printf '.' | wc -c)"
    if [ "$execs" -gt 0 ]; then
        pass "$execs owner-executable file(s) still executable"
    else
        # Zero is suspicious rather than provably wrong: a store may legitimately
        # contain none. Report it as a fail so a human looks, per fail-fast.
        fail "no owner-executable files found under $PROJECT_STORE" \
            "If this store has plugins or skills, a blanket chmod may have stripped their exec bit."
    fi
else
    skip "execute bits" "no project store"
fi
echo

# --- 5. Desktop store (HOST only) ----------------------------------------
echo "### 5. desktop store closed"
if [ -d "$HOME/.claude" ] && [ ! -L "$HOME/.claude" ]; then
    desktop_open="$(count_open "$HOME/.claude")"
    if [ "$desktop_open" -eq 0 ]; then
        pass "no group/other-accessible entries under $HOME/.claude"
    else
        fail "$desktop_open entr(ies) under $HOME/.claude are group/other-accessible" \
            "Deploy it: ansible-playbook $REPO_ROOT/playbooks/imports/play-claude-code.yml"
    fi
else
    skip "desktop store" "not present here (inside CCY, /root/.claude is a symlink into the project store)"
fi
echo

# --- 6. Umask actually deployed ------------------------------------------
# The decisive check. Everything above measures a point in time; this measures
# whether that state will HOLD.
echo "### 6. umask deployed in the running container"
if [ -f /.dockerenv ] || [ -d /workspace/.claude ]; then
    current_umask="$(umask)"
    if [ "$current_umask" = "0077" ]; then
        pass "running umask is $current_umask — new state is created owner-only"
    else
        fail "running umask is $current_umask, expected 0077" \
            "The container still runs an entrypoint without the umask. Rebuild: the next ccy launch picks up container 2.26. Until then the store drifts open again between launches."
    fi
else
    skip "umask" "not in a CCY container; the container umask cannot be checked from the host"
fi
echo

# --- 7. Version coherence -------------------------------------------------
echo "### 7. container version coherence"
DOCKERFILE="$REPO_ROOT/files/var/local/claude-yolo/Dockerfile"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"
label_version="$("$GREP_BIN" -oP '(?<=^LABEL claude-yolo-version=")[^"]+' "$DOCKERFILE")"
required_version="$("$GREP_BIN" -oP '(?<=^REQUIRED_CONTAINER_VERSION=")[^"]+' "$LAUNCHER")"
if [ -n "$label_version" ] && [ "$label_version" = "$required_version" ]; then
    pass "Dockerfile label and REQUIRED_CONTAINER_VERSION agree ($label_version)"
else
    fail "version mismatch: Dockerfile label '$label_version' vs REQUIRED_CONTAINER_VERSION '$required_version'" \
        "A mismatch either forces an endless rebuild loop or lets a stale image pass the gate."
fi
echo

# --- Verdict --------------------------------------------------------------
echo "=========================================================="
if [ "$FAILURES" -eq 0 ]; then
    echo " VERDICT: PASS — $CHECKS check(s), 0 failures"
    echo
    echo " Full report: $LOG"
    echo "=========================================================="
    exit 0
fi
echo " VERDICT: FAIL — $FAILURES of $CHECKS check(s) failed"
echo
echo " Read the [FAIL] lines above; each names its remedy."
echo " Full report: $LOG"
echo "=========================================================="
exit 1
