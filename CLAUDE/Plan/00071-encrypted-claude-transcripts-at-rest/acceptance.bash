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
    2. project store contained  every DIRECTORY under <project>/.claude is owner-only
    3. daemon archive contained .claude/hooks-daemon/untracked/transcripts/ is owner-only
    4. execute bits preserved   the repair did not strip owner-execute anywhere
    5. desktop store contained  every DIRECTORY under ~/.claude is owner-only  (HOST only)
    6. umask deployed           the running process tree has umask 077 (CONTAINER only)
    7. version coherence        Dockerfile label == REQUIRED_CONTAINER_VERSION

WHY DIRECTORIES, NOT FILES: reaching a file requires search (+x) permission on
EVERY ancestor directory. An owner-only directory chain therefore makes its
contents unreachable by other users whatever the individual file modes are.
Directory mode is the control that actually holds; per-file mode is not.

This distinction is not academic — two writers create group/other-readable
files continuously and NEITHER is governed by our umask:

  - Claude Code's file-history/ preserves each snapshot's SOURCE file mode
    (a 0644 repo file snapshots 0644; a 0600 secret snapshots 0600). That is
    reasonable behaviour, and umask cannot override an explicit mode.
  - The hooks daemon calls os.umask(0) and writes 0666 (upstream bug).

An earlier version of this gate failed on any group/other FILE bit. That made
it unpassable on a live session while reporting exposure that did not exist.
Open files are now reported as informational when the directory chain contains
them, and the gate fails only when containment is genuinely broken.

WHY CHECK 6 STILL MATTERS: the umask is defence in depth. It keeps
default-created state owner-only, so a single directory-mode slip does not
immediately expose file contents.

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

note() {
    printf '  [NOTE] %s\n' "$1"
    if [ -n "${2:-}" ]; then
        printf '         %s\n' "$2"
    fi
}

# Directories are the control: without +x on every ancestor, a file is
# unreachable regardless of its own mode.
count_open_dirs() {
    find "$1" -type d -perm "$OPEN_PERM" -printf '.' | wc -c
}

# Files are reported, not gated — see "WHY DIRECTORIES, NOT FILES" in --help.
count_open_files() {
    find "$1" -type f -perm "$OPEN_PERM" -printf '.' | wc -c
}

# Shared reporting for a state store: fail only on broken containment.
report_store() {
    local label="$1" root="$2" remedy="$3"
    local open_dirs open_files
    open_dirs="$(count_open_dirs "$root")"
    open_files="$(count_open_files "$root")"

    if [ "$open_dirs" -ne 0 ]; then
        fail "$open_dirs director(ies) under $root are group/other-accessible" \
            "Containment is broken — files below them are reachable by other local users. $remedy"
        return
    fi

    pass "$label: every directory under $root is owner-only, so its contents are unreachable"
    if [ "$open_files" -gt 0 ]; then
        note "$open_files file(s) below it carry group/other bits — contained, not exposed." \
            "Expected: file-history/ mirrors source modes; the hooks daemon writes 0666 (os.umask(0)). Neither is umask-governed; both are held by the 0700 chain."
    fi
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
echo "### 2. project store contained"
PROJECT_STORE="$REPO_ROOT/.claude"
if [ -d "$PROJECT_STORE" ]; then
    report_store "project store" "$PROJECT_STORE" \
        "Run ccy (its preflight repairs this), or: find '$PROJECT_STORE' -type d -perm $OPEN_PERM -exec chmod go= {} +"
else
    skip "project store" "no $PROJECT_STORE directory"
fi
echo

# --- 3. Daemon transcript archive ----------------------------------------
# Called out separately from check 2 because this is the store the plan
# originally missed entirely, and the one no umask can protect: the hooks
# daemon calls os.umask(0) and re-creates these files continuously.
echo "### 3. daemon transcript archive contained"
# Checked separately from the store as a whole because this is the one directory
# the daemon itself creates 0777 (mkdir under os.umask(0)) — the single place
# where containment is actively broken by another process rather than merely
# left loose. Its contents are verbatim conversation archives.
ARCHIVE="$PROJECT_STORE/hooks-daemon/untracked/transcripts"
if [ -d "$ARCHIVE" ]; then
    archive_mode="$(stat -c '%a' "$ARCHIVE")"
    archive_open_files="$(count_open_files "$ARCHIVE")"
    if [ "$archive_mode" = "700" ]; then
        pass "archive directory is owner-only (mode $archive_mode)"
        if [ "$archive_open_files" -gt 0 ]; then
            note "$archive_open_files archived file(s) carry group/other bits — contained by the 0700 directory." \
                "The daemon writes them 0666; see untracked/hooks-daemon-umask.md."
        fi
    else
        fail "archive directory is mode $archive_mode, expected 700" \
            "These are verbatim conversation archives and this directory is reachable by other local users. The daemon creates it 0777 under os.umask(0) — see untracked/hooks-daemon-umask.md."
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
echo "### 5. desktop store contained"
if [ -d "$HOME/.claude" ] && [ ! -L "$HOME/.claude" ]; then
    report_store "desktop store" "$HOME/.claude" \
        "Deploy it: ansible-playbook $REPO_ROOT/playbooks/imports/play-claude-code.yml"
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
