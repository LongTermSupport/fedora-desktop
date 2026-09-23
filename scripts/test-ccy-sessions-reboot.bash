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
if [ -n "${TEST_TMUX_BROKEN:-}" ]; then
    echo "lost server" >&2
    exit 1
fi
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

# fake_home <dir> <on|off> — a user home whose ccy session restore is enabled or not, the
# way play-claude-yolo.yml leaves it: the unit's wants-symlink present or absent. It also
# holds the user's ccy-sessions, which is where shutdown-with-update looks for it.
fake_home() {
    local home="$1" restore="$2"
    mkdir -p "$home/.config/systemd/user/default.target.wants" "$home/.local/bin"
    ln -s "$TOOL" "$home/.local/bin/ccy-sessions"
    if [ "$restore" = on ]; then
        ln -s ../ccy-sessions-restore.service \
            "$home/.config/systemd/user/default.target.wants/ccy-sessions-restore.service"
    fi
}
HOME_ON="$SCRATCH/home-restore-on"
HOME_OFF="$SCRATCH/home-restore-off"
fake_home "$HOME_ON" on
fake_home "$HOME_OFF" off

# run <args...> — the tool under the fakes, minutes shortened to nothing so a countdown
# completes inside a test. stdout+stderr captured; status in $rc. The home is one with
# restore ON unless TEST_HOME says otherwise.
run() {
    : >"$LOG"
    out="$(HOME="${TEST_HOME:-$HOME_ON}" PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" CCY_SESSIONS_MINUTE_SECONDS=0 CCY_STATE_DIR="$SCRATCH/state" "$TOOL" "$@" </dev/null 2>&1)"
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
echo "=== notify going-down: the kind follows the restore opt-in, not the power action ==="
# The daemon's two warning texts differ on one thing an agent acts on: reboot-warning
# says a session restore will follow, shutdown-warning says none will and asks for a
# handoff. So the truthful kind is decided by whether restore is enabled here.
printf '%s\n' "ccy-a 1 $A" "cc-b 0 $B" >"$SESSIONS"
TEST_HOME="$HOME_ON" run notify going-down --minutes 5
check "restore on: going-down succeeds" "0" "$rc"
check "restore on: each project is told reboot-warning (a restore follows)" "2" \
    "$(calls | grep -c 'signal reboot-warning --minutes 5 --all-sessions')"
TEST_HOME="$HOME_OFF" run notify going-down --minutes 5
check "restore off: each project is told shutdown-warning (no restore, hand off)" "2" \
    "$(calls | grep -c 'signal shutdown-warning --minutes 5 --all-sessions')"
check "restore off: and no reboot-warning" "0" "$(calls | grep -c 'signal reboot-warning')"
TEST_HOME="$HOME_OFF" run notify going-down --minutes 5 --dry-run
check "going-down --dry-run succeeds" "0" "$rc"
check "and invokes no daemon CLI" "0" "$(calls | grep -c '^cli')"
check "and names the kind it would send" "yes" \
    "$([[ "$out" == *"would signal shutdown-warning --minutes 5"* ]] && echo yes || echo no)"
run notify going-down
check "going-down without --minutes is refused" "64" "$rc"
run notify shutdown-warning --minutes 4
check "an explicit shutdown-warning is still delivered" "2" \
    "$(calls | grep -c 'signal shutdown-warning --minutes 4 --all-sessions')"

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

# With restore off a reboot brings nothing back, so the agents must be told to hand off.
TEST_HOME="$HOME_OFF" run reboot --in 3
check "restore off: reboot warns with shutdown-warning, both times" "4" \
    "$(calls | grep -c 'signal shutdown-warning --minutes [31] ')"
check "restore off: never reboot-warning" "0" "$(calls | grep -c 'signal reboot-warning')"
check "restore off: still reboots" "1" "$(calls | grep -c '^systemctl reboot$')"

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
echo "=== a session list that cannot be read is not an empty one ==="
# The failure case prints the same "nothing to signal" as the empty case if the listing's
# exit status is dropped on the way — and then the machine reboots with every agent
# unwarned, exit 0. The fake tmux fails outright here.
printf '%s\n' "ccy-a 1 $A" >"$SESSIONS"
: >"$LOG"
out="$(HOME="$HOME_ON" PATH="$BIN:$PATH" TEST_TMUX_BROKEN=1 CCY_LIB="$LIB_DIR" CCY_SESSIONS_MINUTE_SECONDS=0 CCY_STATE_DIR="$SCRATCH/state" "$TOOL" reboot --in 1 </dev/null 2>&1)"
rc=$?
check "reboot refuses when tmux cannot list sessions" "1" "$rc"
check "and reboots nothing" "0" "$(calls | grep -c '^systemctl')"
check "and signals nothing" "0" "$(calls | grep -c '^cli')"
check "and says why" "yes" "$([[ "$out" == *"could not be read"* ]] && echo yes || echo no)"
: >"$LOG"
out="$(HOME="$HOME_ON" PATH="$BIN:$PATH" TEST_TMUX_BROKEN=1 CCY_LIB="$LIB_DIR" CCY_STATE_DIR="$SCRATCH/state" "$TOOL" notify reboot-warning --minutes 5 </dev/null 2>&1)"
rc=$?
check "notify refuses too" "1" "$rc"
# The rehearsal shutdown-with-update relies on must refuse as well, or it protects nothing.
: >"$LOG"
out="$(HOME="$HOME_ON" PATH="$BIN:$PATH" TEST_TMUX_BROKEN=1 CCY_LIB="$LIB_DIR" CCY_STATE_DIR="$SCRATCH/state" "$TOOL" notify going-down --minutes 2 --dry-run </dev/null 2>&1)"
rc=$?
check "and so does the dry run" "1" "$rc"

echo ""
echo "=== Ctrl-C during the countdown tells the sessions the reboot is off ==="
printf '%s\n' "ccy-a 1 $A" "cc-b 0 $B" >"$SESSIONS"
: >"$LOG"
# A real minute here, so the countdown is in progress when the interrupt arrives.
HOME="$HOME_ON" PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" CCY_SESSIONS_MINUTE_SECONDS=60 CCY_STATE_DIR="$SCRATCH/state" \
    "$TOOL" reboot --in 3 </dev/null >"$SCRATCH/cancel.out" 2>&1 &
reboot_pid=$!
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
    if [ "$(calls | grep -c 'signal reboot-warning --minutes 3 ')" -eq 2 ]; then break; fi
    sleep 0.1
done
# TERM rather than INT: a job started with & from a non-interactive shell has SIGINT
# ignored, and an ignored-at-entry signal cannot be trapped, so INT would prove nothing
# here. The tool traps both the same way; TERM is also what systemd sends.
kill -TERM "$reboot_pid"
if wait "$reboot_pid"; then rc=0; else rc=$?; fi
check "an interrupted reboot exits 130" "130" "$rc"
check "every warned project is told reboot-cancelled" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions --project-root')"
check "and nothing reboots" "0" "$(calls | grep -c '^systemctl')"
check "and it says so" "yes" "$(grep -q 'Reboot cancelled' "$SCRATCH/cancel.out" && echo yes || echo no)"

echo ""
echo "=== shutdown-with-update / reboot-with-update, driven for real under fakes ==="
# The real script, run by both of its names, against stubs for everything that would
# touch the machine: sudo (drops `-u USER -H` and runs the rest as this user, with the
# fake home), getent, fwupdmgr, dnf, flatpak, shutdown and systemd-inhibit, plus the
# systemctl above. `bash -lc` through sudo (the pipx and rustup probes) answers "not
# installed" so no real tool upgrade can run from a test.
WITH_UPDATE="$REPO_ROOT/files/usr/local/bin/shutdown-with-update"
WU_DIR="$SCRATCH/with-update"
mkdir -p "$WU_DIR"
ln -s "$WITH_UPDATE" "$WU_DIR/shutdown-with-update"
ln -s "$WITH_UPDATE" "$WU_DIR/reboot-with-update"
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "${1:-}" = "-u" ] && shift 2
[ "${1:-}" = "-H" ] && shift
if [ "${1:-}" = "bash" ]; then
    exit 1
fi
HOME="$TEST_USER_HOME" exec "$@"
EOF
cat >"$BIN/getent" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s:x:1000:1000::%s:/bin/bash\n' "$2" "$TEST_USER_HOME"
EOF
for stub in fwupdmgr dnf flatpak; do
    cat >"$BIN/$stub" <<'EOF'
#!/usr/bin/env bash
printf '%s %s\n' "$(basename "$0")" "$*" >>"$TEST_LOG"
EOF
done
cat >"$BIN/shutdown" <<'EOF'
#!/usr/bin/env bash
printf 'shutdown %s\n' "$*" >>"$TEST_LOG"
exit "${TEST_SHUTDOWN_RC:-0}"
EOF
cat >"$BIN/systemd-inhibit" <<'EOF'
#!/usr/bin/env bash
printf 'WHO WHAT\nsomeone sleep\n'
EOF
chmod 755 "$BIN/sudo" "$BIN/getent" "$BIN/fwupdmgr" "$BIN/dnf" "$BIN/flatpak" \
    "$BIN/shutdown" "$BIN/systemd-inhibit"

# with_update <name> <home> <stdin-text-or-empty> <args...> — status in $rc.
with_update() {
    local name="$1" home="$2" answer="$3"
    shift 3
    : >"$LOG"
    out="$(printf '%s' "$answer" | SUDO_USER="${TEST_SUDO_USER:-tester}" TEST_USER_HOME="$home" \
        PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" CCY_SESSIONS_MINUTE_SECONDS=0 \
        WITH_UPDATE_MINUTE_SECONDS=0 CCY_STATE_DIR="$SCRATCH/state" \
        bash "$WU_DIR/$name" "$@" 2>&1)"
    rc=$?
}

printf '%s\n' "ccy-a 1 $A" "cc-b 0 $B" >"$SESSIONS"
TEST_SHUTDOWN_RC=0 with_update shutdown-with-update "$HOME_OFF" "" --in 1
check "shutdown, restore off: succeeds" "0" "$rc"
check "the rehearsal signals nothing and names the real kind" "yes" \
    "$([[ "$out" == *"would signal shutdown-warning --minutes 1"* && "$out" != *"would reboot"* ]] && echo yes || echo no)"
check "restore off: the sessions are told shutdown-warning" "2" "$(calls | grep -c 'signal shutdown-warning --minutes 1 ')"
check "and then the machine shuts down" "1" "$(calls | grep -c '^shutdown -h now$')"
check "a shutdown that went ahead withdraws nothing" "0" "$(calls | grep -c 'reboot-cancelled')"

TEST_SHUTDOWN_RC=0 with_update shutdown-with-update "$HOME_ON" "" --in 1
check "shutdown, restore on: the sessions are told reboot-warning (a restore follows)" "2" \
    "$(calls | grep -c 'signal reboot-warning --minutes 1 ')"

TEST_SHUTDOWN_RC=1 with_update shutdown-with-update "$HOME_OFF" "n" --in 1
check "shutdown refused, answer N: exits non-zero" "1" "$rc"
check "and every warned project is told reboot-cancelled" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions')"
check "and nothing is forced" "0" "$(calls | grep -c '^systemctl poweroff')"
check "and it says so" "yes" "$([[ "$out" == *"Shutdown cancelled"* ]] && echo yes || echo no)"

TEST_SHUTDOWN_RC=1 with_update shutdown-with-update "$HOME_OFF" "" --in 1
check "shutdown refused, no answer possible (no tty): exits non-zero" "yes" "$([ "$rc" -ne 0 ] && echo yes || echo no)"
check "and the warning is still withdrawn" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions')"

TEST_SHUTDOWN_RC=1 with_update shutdown-with-update "$HOME_OFF" "y" --in 1
check "shutdown refused, answer Y: forces the poweroff" "1" "$(calls | grep -c '^systemctl poweroff -i$')"
check "and withdraws nothing" "0" "$(calls | grep -c 'reboot-cancelled')"

with_update reboot-with-update "$HOME_OFF" "" --in 1
check "reboot-with-update, restore off: shutdown-warning, since nothing comes back" "2" \
    "$(calls | grep -c 'signal shutdown-warning --minutes 1 ')"
check "and reboots" "1" "$(calls | grep -c '^systemctl reboot$')"

TEST_SUDO_USER=root with_update shutdown-with-update "$HOME_OFF" "" --in 1
check "run from a root shell (SUDO_USER=root): refused" "1" "$rc"
check "and says to run it through sudo as the desktop user" "yes" \
    "$([[ "$out" == *"not from a root shell"* ]] && echo yes || echo no)"
check "and nothing was updated" "0" "$(calls | grep -c '^dnf')"

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
