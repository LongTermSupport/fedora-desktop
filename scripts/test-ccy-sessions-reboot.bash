#!/usr/bin/env bash
# Unit-test the ccy-sessions subcommands that warn sessions before a reboot and restore them
# after one (files/home/.local/bin/ccy-sessions: notify, reboot, restore).
#
# WHY THIS EXISTS. `ccy-sessions reboot --in N` is the deliberate way to take a machine
# down with agents running: it signals every live session's project through that
# project's own hooks-daemon CLI, again at one minute, and only then reboots. Each of those
# is a thing that can silently not happen — a project signalled twice, a project with no
# daemon CLI skipped with a warning, a `--dry-run` that still reboots — and none of them
# can be tried on the machine running the test. So the executable is run for real, against
# a fake `tmux` on PATH that answers with a canned session list, project directories that
# hold a logging stand-in for the daemon CLI, and a `systemctl` that records what it was
# asked instead of doing it. What is asserted is the real script's behaviour, byte for byte.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/home/.local/bin/ccy-sessions"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

if [ ! -x "$TOOL" ]; then
    echo "FAIL: $TOOL is missing or not executable" >&2
    exit 1
fi

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/sessions-reboot-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# ── the fakes ─────────────────────────────────────────────────────────────────────────
BIN="$SCRATCH/bin"
LOG="$SCRATCH/calls.log"
mkdir -p "$BIN"

# tmux: the one query the tool makes is list-sessions with a format; it answers from a
# file the test rewrites per case. Anything else is an error, so a new tmux call in the
# tool shows up here as a failure rather than passing by accident.
cat >"$BIN/tmux" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
case "$*" in
*list-sessions*) cat "$TEST_SESSIONS" ;;
*)
    echo "fake tmux: unexpected call: $*" >&2
    exit 97
    ;;
esac
EOF
# systemctl: records the request and does nothing.
cat >"$BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'systemctl %s\n' "$*" >>"$TEST_LOG"
EOF
chmod 755 "$BIN/tmux" "$BIN/systemctl"

# project <dir> [with-cli] — a project directory, optionally holding a daemon CLI that logs
# its arguments and the directory it was found in.
project() {
    local dir="$1" with_cli="${2:-with-cli}"
    mkdir -p "$dir"
    if [ "$with_cli" = "with-cli" ]; then
        mkdir -p "$dir/.claude/hooks-daemon/bin"
        cat >"$dir/.claude/hooks-daemon/bin/hooks-daemon" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'cli[%s] %s\n' "$(cd "$(dirname "$0")/../../.." && pwd)" "$*" >>"$TEST_LOG"
EOF
        chmod 755 "$dir/.claude/hooks-daemon/bin/hooks-daemon"
    fi
}

A="$SCRATCH/project a"
B="$SCRATCH/project-b"
NOCLI="$SCRATCH/project-without-daemon"
project "$A"
project "$B"
project "$NOCLI" without-cli

SESSIONS="$SCRATCH/sessions"
export TEST_SESSIONS="$SESSIONS" TEST_LOG="$LOG"

# run <args...> — the tool under the fakes, minutes shortened to nothing so a countdown
# completes inside a test. stdout+stderr captured; status in $rc.
run() {
    : >"$LOG"
    out="$(PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" CCY_SESSIONS_MINUTE_SECONDS=0 CCY_STATE_DIR="$SCRATCH/state" "$TOOL" "$@" </dev/null 2>&1)"
    rc=$?
}
calls() { if [ -f "$LOG" ]; then cat "$LOG"; fi; }

echo ""
echo "=== the picker path is untouched ==="
run --help
check "--help works with no terminal" "0" "$rc"
check "--help names the new subcommands" "yes" "$([[ "$out" == *"notify"* && "$out" == *"reboot"* && "$out" == *"restore"* ]] && echo yes || echo no)"
run
check "bare ccy-sessions still wants a terminal" "1" "$rc"
check "and says so" "yes" "$([[ "$out" == *"needs a terminal"* ]] && echo yes || echo no)"
run --bogus
check "an unknown option is still exit 64" "64" "$rc"

echo ""
echo "=== notify: every live project, exactly once ==="
# Two sessions in project a (a second terminal, a second session), one in project b:
# two projects, so two signals. The sessions are "<name> <attached> <dir>" as tmux
# prints them.
printf '%s\n' "ccy-a 1 $A" "ccy-a-2 0 $A" "cc-b 0 $B" >"$SESSIONS"
run notify reboot-warning --minutes 5
check "notify succeeds" "0" "$rc"
check "each project signalled exactly once" "2" "$(calls | grep -c '^cli')"
check "project a signalled with its own root, all sessions, the minutes" "yes" \
    "$([[ "$(calls)" == *"cli[$A] signal reboot-warning --minutes 5 --all-sessions --project-root $A"* ]] && echo yes || echo no)"
check "project b too" "yes" \
    "$([[ "$(calls)" == *"cli[$B] signal reboot-warning --minutes 5 --all-sessions --project-root $B"* ]] && echo yes || echo no)"
check "notify never reboots" "0" "$(calls | grep -c '^systemctl')"

run notify reboot-cancelled
check "cancelled needs no minutes" "0" "$rc"
check "cancelled reaches each project once" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions --project-root')"

run notify reboot-warning
check "a warning without --minutes is refused" "64" "$rc"
run notify reboot-warning --minutes 0
check "zero minutes is refused" "64" "$rc"
run notify reboot-warning --minutes soon
check "non-numeric minutes are refused" "64" "$rc"
run notify shutdown-soon --minutes 5
check "an unknown signal kind is refused" "64" "$rc"
check "and nothing was signalled" "0" "$(calls | grep -c '^cli')"

: >"$SESSIONS"
run notify reboot-warning --minutes 5
check "no live sessions: nothing to signal, success" "0" "$rc"
check "and says so" "yes" "$([[ "$out" == *"No CCY or cc sessions"* ]] && echo yes || echo no)"

echo ""
echo "=== notify: a project with no daemon CLI is a refusal, not a skip ==="
printf '%s\n' "ccy-a 1 $A" "ccy-x 0 $NOCLI" >"$SESSIONS"
run notify reboot-warning --minutes 5
check "exit is non-zero" "1" "$rc"
check "the project is named" "yes" "$([[ "$out" == *"$NOCLI"* ]] && echo yes || echo no)"
# Checked BEFORE anything is signalled, so a refusal leaves no project half-warned about a
# reboot that is not going to happen.
check "and NO project was signalled, not even the one with a CLI" "0" "$(calls | grep -c '^cli')"

echo ""
echo "=== reboot --in N: warn, count down, warn at one minute, reboot ==="
printf '%s\n' "ccy-a 1 $A" "cc-b 0 $B" >"$SESSIONS"
run reboot --in 3
check "reboot succeeds" "0" "$rc"
check "the first warning carries the full minutes" "2" "$(calls | grep -c 'signal reboot-warning --minutes 3 ')"
check "the one-minute warning fires" "2" "$(calls | grep -c 'signal reboot-warning --minutes 1 ')"
check "then systemctl reboot, once" "1" "$(calls | grep -c '^systemctl reboot$')"
check "and it is the LAST thing that happens" "systemctl reboot" "$(calls | awk 'END{print}')"
check "the countdown is printed" "yes" "$([[ "$out" == *"3 minute"* ]] && echo yes || echo no)"

run reboot --in 1
check "--in 1 warns once (the one-minute warning IS the warning)" "2" "$(calls | grep -c 'signal reboot-warning --minutes 1 ')"
check "and reboots" "1" "$(calls | grep -c '^systemctl reboot$')"

run reboot --in 3 --dry-run
check "dry run succeeds" "0" "$rc"
check "dry run invokes no daemon CLI" "0" "$(calls | grep -c '^cli')"
check "dry run invokes no systemctl" "0" "$(calls | grep -c '^systemctl')"
check "dry run says what it would signal, per project" "yes" \
    "$([[ "$out" == *"would signal"* && "$out" == *"$A"* && "$out" == *"$B"* ]] && echo yes || echo no)"
check "dry run says it would reboot" "yes" "$([[ "$out" == *"would reboot"* ]] && echo yes || echo no)"

run reboot
check "reboot without --in is refused" "64" "$rc"
run reboot --in 0
check "--in 0 is refused" "64" "$rc"
check "and nothing rebooted" "0" "$(calls | grep -c '^systemctl')"

printf '%s\n' "ccy-a 1 $A" "ccy-x 0 $NOCLI" >"$SESSIONS"
run reboot --in 2
check "a missing daemon CLI refuses the reboot" "1" "$rc"
check "and REBOOTS NOTHING" "0" "$(calls | grep -c '^systemctl')"
check "and signals nothing" "0" "$(calls | grep -c '^cli')"

: >"$SESSIONS"
run reboot --in 2
check "no sessions: nothing to warn, still reboots" "0" "$rc"
check "with no signals" "0" "$(calls | grep -c '^cli')"
check "and one systemctl reboot" "1" "$(calls | grep -c '^systemctl reboot$')"

echo ""
echo "=== restore is reachable as a subcommand ==="
# The decisions are tested in scripts/test-ccy-session-registry.bash; this proves the
# subcommand exists, runs headless, and reads the registry the launchers write.
: >"$SESSIONS"
run restore --dry-run
check "restore --dry-run runs without a terminal" "0" "$rc"
check "and reports an empty registry" "yes" "$([[ "$out" == *"nothing to restore"* ]] && echo yes || echo no)"
run restore --now
check "restore rejects an unknown flag" "64" "$rc"

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$passed" "$failed"
if [ "$passed" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$failed" -ne 0 ]; then
    exit 1
fi
echo "OK"
