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
# verify-restore's probes: the pane table, and one session's screen from TEST_SCREENS/<name>.
*list-panes*) if [ -n "${TEST_PANES:-}" ]; then cat "$TEST_PANES"; fi ;;
# Real tmux takes `=name` as a SESSION target but not as a pane target: a pane command
# needs `=name:` and fails "can't find pane" without the colon (#69). The fake does the same.
*capture-pane*)
    target=""
    while [ $# -gt 0 ]; do
        if [ "$1" = "-t" ]; then target="$2"; fi
        shift
    done
    case "$target" in
    =*:*) ;;
    =*)
        echo "can't find pane: $target" >&2
        exit 1
        ;;
    esac
    target="${target#=}"
    cat "$TEST_SCREENS/${target%:}"
    ;;
# set-going's keys: logged as `send-keys <target> <keys...>`, refused for a session named in
# TEST_SENDKEYS_BROKEN, and held to the same `=name:` pane target as capture-pane.
*send-keys*)
    while [ "$1" != send-keys ]; do shift; done
    shift
    target=""
    keys=()
    while [ $# -gt 0 ]; do
        case "$1" in
        -t) target="$2"; shift 2 ;;
        -l) keys+=("[literal]"); shift ;;
        *) keys+=("$1"); shift ;;
        esac
    done
    case "$target" in
    =*:) ;;
    *)
        echo "can't find pane: $target" >&2
        exit 1
        ;;
    esac
    if [ -n "${TEST_SENDKEYS_BROKEN:-}" ] && [ "$target" = "=${TEST_SENDKEYS_BROKEN}:" ]; then
        echo "pane is dead" >&2
        exit 1
    fi
    printf 'send-keys %s %s\n' "$target" "${keys[*]}" >>"$TEST_LOG"
    ;;
*)
    echo "fake tmux: unexpected call: $*" >&2
    exit 97
    ;;
esac
EOF
# ps and podman: the process table and the running CCY containers, from files a case writes.
# Only verify-restore asks either of them anything in this suite.
cat >"$BIN/ps" <<'EOF'
#!/usr/bin/env bash
if [ -n "${TEST_PS:-}" ]; then cat "$TEST_PS"; fi
EOF
# `podman exec <container> <cli> <args>` runs a ccy project's daemon CLI inside its container:
# logged as exec[<container>] <cli> <args>, and failing for a container named in
# TEST_EXEC_BROKEN (its CLI cannot run) or for arguments matching TEST_EXEC_FAIL_MATCH.
cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" = exec ]; then
    container="$2"
    shift 2
    printf 'exec[%s] %s\n' "$container" "$*" >>"$TEST_LOG"
    if [ -n "${TEST_EXEC_BROKEN:-}" ] && [ "$container" = "$TEST_EXEC_BROKEN" ]; then
        echo "exec: no usable venv in the container" >&2
        exit 1
    fi
    if [ -n "${TEST_EXEC_FAIL_MATCH:-}" ] && [[ "$*" == *"$TEST_EXEC_FAIL_MATCH"* ]]; then
        exit 1
    fi
    exit 0
fi
case "$*" in
"ps --filter label=ccy=true --format {{.Names}}") if [ -n "${TEST_PODMAN_NAMES:-}" ]; then cat "$TEST_PODMAN_NAMES"; fi ;;
# The list's query: name, networks and the token and key labels.
'ps --filter label=ccy=true --format {{.Names}}|{{.Networks}}|{{.Label "ccy-token"}}|{{.Label "ccy-ssh-keys"}}')
    if [ -n "${TEST_PODMAN_DETAILS:-}" ]; then cat "$TEST_PODMAN_DETAILS"; fi
    ;;
# ... and the host pid of each listed container's first process, for the CPU column.
'inspect --format {{.Name}} {{.State.Pid}} a_yolo') printf 'a_yolo 120\n' ;;
*)
    echo "fake podman: unexpected call: $*" >&2
    exit 97
    ;;
esac
EOF
chmod 755 "$BIN/ps" "$BIN/podman"
# systemctl: records the request and does nothing, or fails when TEST_SYSTEMCTL_RC says so.
# TEST_SYSTEMCTL_TERM_CALLER=1 makes `reboot` and `poweroff` send TERM to their caller first,
# as a real shutdown does to every process while the power action is under way.
cat >"$BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'systemctl %s\n' "$*" >>"$TEST_LOG"
if [ -n "${TEST_SYSTEMCTL_TERM_CALLER:-}" ] && [[ "$*" == reboot* || "$*" == poweroff* ]]; then
    kill -TERM "$PPID"
    sleep 0.2
fi
exit "${TEST_SYSTEMCTL_RC:-0}"
EOF
# pgrep: shutdown-with-update's akmods probe is `pgrep -f "[a]kmods"`, which matches any
# process whose command line mentions akmods. A real pgrep let an unrelated shell on the
# machine running this test hold every case in the five-minute akmods wait.
cat >"$BIN/pgrep" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
# busctl: logind's CanReboot answer for this account, from TEST_CAN_REBOOT (default yes), or
# a failure to reach the bus when TEST_BUSCTL_RC is set. Any other question is an error.
cat >"$BIN/busctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'busctl %s\n' "$*" >>"$TEST_LOG"
if [ "$*" != "--system call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanReboot" ]; then
    echo "fake busctl: unexpected call: $*" >&2
    exit 97
fi
if [ -n "${TEST_BUSCTL_RC:-}" ]; then
    echo "Failed to connect to bus: No such file or directory" >&2
    exit "$TEST_BUSCTL_RC"
fi
printf 's "%s"\n' "${TEST_CAN_REBOOT:-yes}"
EOF
chmod 755 "$BIN/tmux" "$BIN/systemctl" "$BIN/pgrep" "$BIN/busctl"

# project <dir> [with-cli|without-cli|broken-cli] — a project directory, optionally holding
# a daemon CLI that logs its arguments and the directory it was found in. A broken CLI is
# executable but cannot run at all, the way one without its venv fails on the host.
project() {
    local dir="$1" with_cli="${2:-with-cli}"
    mkdir -p "$dir"
    if [ "$with_cli" = "broken-cli" ]; then
        mkdir -p "$dir/.claude/hooks-daemon/bin"
        cat >"$dir/.claude/hooks-daemon/bin/hooks-daemon" <<'EOF'
#!/usr/bin/env bash
printf 'cli[%s] %s\n' "$(cd "$(dirname "$0")/../../.." && pwd)" "$*" >>"$TEST_LOG"
echo "resolve_venv: no usable venv found" >&2
exit 1
EOF
        chmod 755 "$dir/.claude/hooks-daemon/bin/hooks-daemon"
    fi
    if [ "$with_cli" = "with-cli" ]; then
        mkdir -p "$dir/.claude/hooks-daemon/bin"
        cat >"$dir/.claude/hooks-daemon/bin/hooks-daemon" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'cli[%s] %s\n' "$(cd "$(dirname "$0")/../../.." && pwd)" "$*" >>"$TEST_LOG"
# TEST_CLI_FAIL_MATCH / TEST_CLI_FAIL_MATCH_2: refuse any call whose arguments contain
# either (a warning, or a withdrawal, that fails).
if [ -n "${TEST_CLI_FAIL_MATCH:-}" ] && [[ "$*" == *"$TEST_CLI_FAIL_MATCH"* ]]; then
    exit 1
fi
if [ -n "${TEST_CLI_FAIL_MATCH_2:-}" ] && [[ "$*" == *"$TEST_CLI_FAIL_MATCH_2"* ]]; then
    exit 1
fi
EOF
        chmod 755 "$dir/.claude/hooks-daemon/bin/hooks-daemon"
    fi
}

A="$SCRATCH/project a"
B="$SCRATCH/project-b"
NOCLI="$SCRATCH/project-without-daemon"
BROKEN="$SCRATCH/project-with-broken-daemon"
project "$A"
project "$B"
project "$NOCLI" without-cli
project "$BROKEN" broken-cli

SESSIONS="$SCRATCH/sessions"
export TEST_SESSIONS="$SESSIONS" TEST_LOG="$LOG"

# fake_home <dir> <on|off> — a user home whose ccy session restore is enabled or not, the
# way play-claude-yolo.yml leaves it: the unit's wants-symlink present or absent. It also
# holds the user's ccy-sessions, which is where shutdown-with-update looks for it.
fake_home() {
    local home="$1" restore="$2"
    mkdir -p "$home/.config/systemd/user/default.target.wants" "$home/.local/bin"
    ln -s "$TOOL" "$home/.local/bin/ccy-sessions"
    # The link resolves only when the unit it points at exists, as on a deployed host.
    if [ "$restore" = on ] || [ "$restore" = dangling ]; then
        ln -s ../ccy-sessions-restore.service \
            "$home/.config/systemd/user/default.target.wants/ccy-sessions-restore.service"
    fi
    if [ "$restore" = on ]; then
        : >"$home/.config/systemd/user/ccy-sessions-restore.service"
    fi
}
HOME_ON="$SCRATCH/home-restore-on"
HOME_OFF="$SCRATCH/home-restore-off"
HOME_DANGLING="$SCRATCH/home-restore-dangling"
fake_home "$HOME_ON" on
fake_home "$HOME_OFF" off
fake_home "$HOME_DANGLING" dangling

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
echo "=== --list: the table on stdout, no terminal, nothing opened ==="
# One ccy session with a container, one cc session on the host.
printf '%s\n' "ccy-a 0 $A" "cc-b 1 $B" >"$SESSIONS"
printf '%s\n' "ccy-a 100" "cc-b 200" >"$SCRATCH/panes"
printf '%s\n' "100 1 bash -c trampoline" "110 100 podman run --rm -it --name a_yolo img claude" \
    "120 1 /entrypoint.sh" "200 1 bash -c trampoline" "210 200 claude" >"$SCRATCH/ps"
printf '%s\n' "a_yolo|[a-net]|work|key_0" >"$SCRATCH/details"
# The CPU sample reads this machine's /proc for the table's pids, so its figures are whatever
# those pids are here: only their shape is checked. No pause between the two samples.
list_stdout() {
    HOME="$HOME_ON" PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" TEST_PANES="$SCRATCH/panes" \
        TEST_PS="$SCRATCH/ps" TEST_PODMAN_DETAILS="$SCRATCH/details" CCY_CPU_SAMPLE_SECONDS=0 \
        "$TOOL" "$@" </dev/null 2>/dev/null
}
for flag in --list -l; do
    TEST_PANES="$SCRATCH/panes" TEST_PS="$SCRATCH/ps" TEST_PODMAN_DETAILS="$SCRATCH/details" \
        CCY_CPU_SAMPLE_SECONDS=0 run "$flag"
    check "$flag works with no terminal" "0" "$rc"
    listed="$(list_stdout "$flag")"
    check "$flag prints a heading and one row per session" "3" "$(grep -c . <<<"$listed")"
    check "$flag heading names the CPU, token and key columns" "yes" \
        "$([[ "$(awk 'NR == 1' <<<"$listed")" == *"CPU"*"TOKEN"*"SSH KEY"* ]] && echo yes || echo no)"
    check "$flag row gives the ccy session its CPU, network, token and key" \
        "ccy-a detached cpu a-net work key_0" \
        "$(awk '$1 == "ccy-a" { print $1, $2, ($3 ~ /^[0-9]+%$/ ? "cpu" : $3), $4, $5, $6 }' <<<"$listed")"
    check "$flag row gives the cc session its CPU, no container and - for token and key" \
        "cc-b open elsewhere cpu no container - -" \
        "$(awk '$1 == "cc-b" { print $1, $2, $3, ($4 ~ /^[0-9]+%$/ ? "cpu" : $4), $5, $6, $7, $8 }' <<<"$listed")"
done
run --list extra
check "--list takes no argument" "64" "$rc"
: >"$SESSIONS"
run --list
check "--list with no sessions succeeds" "0" "$rc"
check "and prints nothing on stdout" "" "$(list_stdout --list)"

echo ""
echo "=== notify: every live project, exactly once ==="
# Two sessions in project a (a second terminal, a second session), one in project b:
# two projects, so two signals. The sessions are "<name> <attached> <dir>" as tmux
# prints them.
printf '%s\n' "ccy-a 1 $A" "ccy-a-2 0 $A" "cc-b 0 $B" >"$SESSIONS"
run notify reboot-warning --minutes 5
check "notify succeeds" "0" "$rc"
check "each project signalled exactly once" "2" "$(calls | grep -c '^cli.* signal reboot-warning')"
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
TEST_HOME="$HOME_DANGLING" run notify going-down --minutes 5
check "a dangling wants-symlink is restore OFF: it pulls in nothing at boot" "2" \
    "$(calls | grep -c 'signal shutdown-warning --minutes 5 --all-sessions')"
TEST_HOME="$HOME_OFF" run notify going-down --minutes 5 --dry-run
check "going-down --dry-run succeeds" "0" "$rc"
check "and signals no daemon" "0" "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"
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
# reboot that is not going to happen. (The one with a CLI is asked `signal --help`, which
# changes nothing, so that a CLI that cannot run is listed in the same refusal.)
check "and NO project was signalled, not even the one with a CLI" "0" \
    "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"

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
check "dry run signals no daemon" "0" "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"
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
check "and signals nothing" "0" "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"

# The CLI is there but cannot run (on the host, a project whose daemon has no venv). Found
# only when signalling, the first project had already been warned; it must refuse first.
printf '%s\n' "ccy-a 1 $A" "ccy-y 0 $BROKEN" >"$SESSIONS"
run reboot --in 2
check "a daemon CLI that cannot run refuses the reboot" "1" "$rc"
check "  and REBOOTS NOTHING" "0" "$(calls | grep -c '^systemctl')"
check "  and warns no project, not even the one before it" "0" \
    "$(calls | grep -c '^cli.* signal [a-z-]*warning')"
check "  naming the project" "yes" "$([[ "$out" == *"$BROKEN"* ]] && echo yes || echo no)"
check "  and what its CLI said" "yes" "$([[ "$out" == *"no usable venv"* ]] && echo yes || echo no)"
run reboot --in 2 --dry-run
check "the dry run finds it too" "1" "$rc"
check "  and signals nothing" "0" "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"

echo ""
echo "=== a ccy project's daemon is reached inside its container ==="
# Most projects run only ccy, so their daemon has a venv only inside the container; the
# host CLI is the broken one above. The session's container is found as verify-restore
# finds it: the engine client in its pane's process tree.
CT="$SCRATCH/container-route"
mkdir -p "$CT"
CCYONLY="$SCRATCH/project-ccy-only"
project "$CCYONLY" broken-cli
IN_CT="/workspace/.claude/hooks-daemon/bin/hooks-daemon"
printf '%s\n' "ccy-a 1 $A" "ccy-c 0 $CCYONLY" >"$SESSIONS"
printf '%s\n' "ccy-c 500" >"$CT/panes"
printf '%s\n' "500 1 bash" "600 500 podman run --rm --name c_yolo claude-yolo:latest" >"$CT/ps"
TEST_PANES="$CT/panes" TEST_PS="$CT/ps" run reboot --in 2
check "a ccy-only project with no host venv: the reboot goes ahead" "0" "$rc"
check "  its warning is sent inside its container, to its /workspace" "1" \
    "$(calls | grep -cF "exec[c_yolo] $IN_CT signal reboot-warning --minutes 2 --all-sessions --project-root /workspace")"
check "  and again at one minute" "1" \
    "$(calls | grep -cF "exec[c_yolo] $IN_CT signal reboot-warning --minutes 1 --all-sessions --project-root /workspace")"
check "  its host CLI is never asked to signal" "0" "$(calls | grep -cF "cli[$CCYONLY] signal reboot")"
check "  a project with no container is still reached on the host" "1" \
    "$(calls | grep -cF "cli[$A] signal reboot-warning --minutes 2 --all-sessions --project-root $A")"
check "  the output names the container" "yes" "$([[ "$out" == *"(in container c_yolo)"* ]] && echo yes || echo no)"
check "  and the machine reboots" "1" "$(calls | grep -c '^systemctl reboot$')"

TEST_EXEC_BROKEN=c_yolo TEST_PANES="$CT/panes" TEST_PS="$CT/ps" run reboot --in 2
check "a daemon that cannot run inside the container refuses the reboot" "1" "$rc"
check "  warning no project" "0" "$(calls | grep -c 'signal [a-z-]*warning')"
check "  and rebooting nothing" "0" "$(calls | grep -c '^systemctl')"
check "  naming the container" "yes" "$([[ "$out" == *"(in container c_yolo)"* ]] && echo yes || echo no)"

TEST_EXEC_FAIL_MATCH="signal reboot-warning" TEST_PANES="$CT/panes" TEST_PS="$CT/ps" run reboot --in 2
check "a warning that fails inside the container refuses the reboot" "1" "$rc"
check "  the project warned before it is told reboot-cancelled" "1" \
    "$(calls | grep -cF "cli[$A] signal reboot-cancelled --all-sessions --project-root $A")"
check "  and so is the container, inside it" "1" \
    "$(calls | grep -cF "exec[c_yolo] $IN_CT signal reboot-cancelled --all-sessions --project-root /workspace")"

TEST_PANES="$CT/panes" TEST_PS="$CT/ps" run reboot --in 2 --dry-run
check "the dry run says it would signal inside the container" "yes" \
    "$([[ "$out" == *"would signal reboot-warning --minutes 2 to $CCYONLY (in container c_yolo)"* ]] && echo yes || echo no)"
check "  and signals nothing" "0" "$(calls | grep -c 'signal [a-z-]*\(warning\|cancelled\)')"

: >"$SESSIONS"
run reboot --in 2
check "no sessions: nothing to warn, still reboots" "0" "$rc"
check "with no signals" "0" "$(calls | grep -c '^cli')"
check "and one systemctl reboot" "1" "$(calls | grep -c '^systemctl reboot$')"

echo ""
echo "=== a reboot that stops after warning anyone withdraws the warning ==="
# Projects are signalled in listing order, so project a is warned before project b's
# warning fails. Project a must not be left expecting a reboot that is not coming.
printf '%s\n' "ccy-a 1 $A" "cc-b 0 $B" >"$SESSIONS"
TEST_CLI_FAIL_MATCH="signal reboot-warning --minutes 3 --all-sessions --project-root $B" run reboot --in 3
check "a first warning that fails part-way refuses the reboot" "1" "$rc"
check "and reboots nothing" "0" "$(calls | grep -c '^systemctl')"
check "the project already warned is told reboot-cancelled" "1" \
    "$(calls | grep -cF "cli[$A] signal reboot-cancelled --all-sessions --project-root $A")"
check "and it says the machine is staying up" "yes" "$([[ "$out" == *"staying up"* ]] && echo yes || echo no)"

TEST_CLI_FAIL_MATCH="signal reboot-warning --minutes 3 --all-sessions --project-root $B" \
    TEST_CLI_FAIL_MATCH_2="signal reboot-cancelled --all-sessions --project-root $A" run reboot --in 3
check "a withdrawal that fails at one project still reaches the next" "1" \
    "$(calls | grep -cF "cli[$B] signal reboot-cancelled --all-sessions --project-root $B")"
check "and says some sessions still expect the reboot" "yes" \
    "$([[ "$out" == *"still expect the reboot"* ]] && echo yes || echo no)"
check "and still exits non-zero" "1" "$rc"

TEST_SYSTEMCTL_RC=1 run reboot --in 1
check "systemctl refusing the reboot is a failure" "1" "$rc"
check "and every warned project is told reboot-cancelled" "2" \
    "$(calls | grep -c 'signal reboot-cancelled --all-sessions --project-root')"
# It failed; nobody cancelled it. The operator is told which, in those words.
check "and it says the reboot FAILED" "yes" "$([[ "$out" == *"the reboot failed"* ]] && echo yes || echo "no: $out")"
check "  not that it was cancelled" "no" "$([[ "$out" == *"Reboot cancelled"* || "$out" == *"reboot is off"* ]] && echo yes || echo no)"

run reboot --in 1
check "a reboot that went ahead withdraws nothing" "0" "$(calls | grep -c 'reboot-cancelled')"

echo ""
echo "=== reboot: everything that would stop it is found before anyone is warned (#69) ==="
# From a login with no terminal polkit refuses `systemctl reboot` ("interactive
# authentication required"), which used to surface only after both warnings and the
# countdown, and was then reported to every session as a cancellation.
printf '%s\n' "ccy-a 1 $A" "cc-b 0 $B" >"$SESSIONS"
run reboot --in 2
check "logind is asked whether this account may reboot, once" "1" "$(calls | grep -c '^busctl .*CanReboot$')"
check "  before the first warning" "busctl" "$(calls | awk 'NR == 1 { print $1 }')"
for answer in challenge no; do
    TEST_CAN_REBOOT="$answer" run reboot --in 2
    check "logind answers '$answer': the reboot is refused" "1" "$rc"
    check "  before any project is warned, or told anything" "0" \
        "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"
    check "  and nothing reboots" "0" "$(calls | grep -c '^systemctl')"
    check "  naming logind's answer" "yes" "$([[ "$out" == *"CanReboot: $answer"* ]] && echo yes || echo "no: $out")"
    check "  and the way to reboot instead" "yes" "$([[ "$out" == *"sudo /usr/local/bin/reboot-with-update"* ]] && echo yes || echo no)"
    check "  and not calling it a cancellation" "no" \
        "$([[ "$out" == *"staying up"* || "$out" == *"cancelled"* ]] && echo yes || echo no)"
done
TEST_CAN_REBOOT=challenge run reboot --in 2 --dry-run
check "the dry run refuses too, instead of saying it would reboot" "1:no" \
    "$rc:$([[ "$out" == *"would reboot"* ]] && echo yes || echo no)"
TEST_BUSCTL_RC=1 run reboot --in 2
check "logind cannot be asked: refused, not assumed" "1" "$rc"
check "  warning nobody" "0" "$(calls | grep -c '^cli.* signal [a-z-]*warning')"
check "  and saying why" "yes" "$([[ "$out" == *"could not ask logind"* && "$out" == *"Failed to connect"* ]] && echo yes || echo no)"
TEST_CAN_REBOOT=na run reboot --in 2
check "an answer that is not yes is a refusal" "1:0" "$rc:$(calls | grep -c '^systemctl')"

# Every session that cannot be warned is listed in one go, with what to do about each, and
# alongside a permission refusal rather than one problem per attempt.
NOCLI2="$SCRATCH/second-project-without-daemon"
project "$NOCLI2" without-cli
printf '%s\n' "ccy-a 1 $A" "ccy-x 0 $NOCLI" "ccy-x-2 0 $NOCLI" "ccy-z 0 $NOCLI2" "ccy-y 0 $BROKEN" >"$SESSIONS"
TEST_CAN_REBOOT=challenge run reboot --in 2
check "sessions that cannot be warned, and no permission: refused" "1" "$rc"
for session in ccy-x ccy-x-2 ccy-z; do
    check "  $session (its project has no daemon CLI) is named" "1" "$(grep -c "^  $session  in " <<<"$out")"
done
check "  with its project" "yes" "$([[ "$out" == *"ccy-z  in $NOCLI2"* ]] && echo yes || echo no)"
check "  the session whose daemon cannot run is named too" "1" "$(grep -c '^  ccy-y  in ' <<<"$out")"
check "  the options: end the session, or install the daemon" "yes" \
    "$([[ "$out" == *"Ctrl-X"* && "$out" == *"'install'"* ]] && echo yes || echo no)"
check "  and the permission problem in the same run" "yes" "$([[ "$out" == *"CanReboot: challenge"* ]] && echo yes || echo no)"
check "  and no project is warned or told anything" "0" "$(calls | grep -c '^cli.* signal [a-z-]*\(warning\|cancelled\)')"
check "  a session in a project that can be warned is not listed" "0" "$(grep -c '^  ccy-a ' <<<"$out")"

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
    if kill -0 "$reboot_pid" 2>/dev/null; then alive=1; else alive=0; fi
    if [ "$(calls | grep -c 'signal reboot-warning --minutes 3 ')" -eq 2 ]; then break; fi
    if [ "$alive" -eq 0 ]; then
        status=0
        wait "$reboot_pid" || status=$?
        echo "FAIL: ccy-sessions reboot exited (status $status) before warning both sessions:" >&2
        cat "$SCRATCH/cancel.out" >&2
        exit 1
    fi
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

TEST_SYSTEMCTL_RC=1 with_update reboot-with-update "$HOME_OFF" "" --in 1
check "a failed reboot request: exits non-zero" "yes" "$([ "$rc" -ne 0 ] && echo yes || echo no)"
check "and every warned project is told reboot-cancelled" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions')"

TEST_SHUTDOWN_RC=1 TEST_SYSTEMCTL_RC=1 with_update shutdown-with-update "$HOME_OFF" "y" --in 1
check "a failed forced poweroff: exits non-zero" "yes" "$([ "$rc" -ne 0 ] && echo yes || echo no)"
check "and every warned project is told reboot-cancelled" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions')"

TEST_CLI_FAIL_MATCH="--minutes 1 " with_update reboot-with-update "$HOME_OFF" "" --in 2
check "a failed one-minute warning: exits non-zero" "yes" "$([ "$rc" -ne 0 ] && echo yes || echo no)"
check "and the two-minute warning is withdrawn" "2" "$(calls | grep -c 'signal reboot-cancelled --all-sessions')"
check "and nothing reboots" "0" "$(calls | grep -c '^systemctl reboot$')"

TEST_CLI_FAIL_MATCH="--minutes 1 " with_update shutdown-with-update "$HOME_OFF" "" --in 1
check "a first warning that fails part-way: exits non-zero" "yes" "$([ "$rc" -ne 0 ] && echo yes || echo no)"
check "and whatever was reached is withdrawn" "yes" \
    "$([ "$(calls | grep -c 'signal reboot-cancelled --all-sessions')" -ge 1 ] && echo yes || echo no)"
check "and nothing shuts down" "0" "$(calls | grep -c '^shutdown -h now$')"

TEST_SYSTEMCTL_TERM_CALLER=1 with_update reboot-with-update "$HOME_OFF" "" --in 1
check "TERM while the reboot is under way: not treated as a cancel" "0" "$rc"
check "and the sessions are not told the reboot is off" "0" "$(calls | grep -c 'reboot-cancelled')"

TEST_SHUTDOWN_RC=1 TEST_SYSTEMCTL_TERM_CALLER=1 with_update shutdown-with-update "$HOME_OFF" "y" --in 1
check "TERM while a forced poweroff is under way: exits 0" "0" "$rc"
check "TERM while a forced poweroff is under way: not treated as a cancel" "0" "$(calls | grep -c 'reboot-cancelled')"

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
echo "=== verify-restore: every session this boot's restore brought up, named by its state ==="
# The real tool reads the manifest the real library writes, and asks fake tmux, ps and podman.
# The screens are made from the prompt constants, so what is looked for is what is printed.
# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/session-registry.bash"
VR="$SCRATCH/verify"
mkdir -p "$VR/screens"
printf 'this-boot\n' >"$VR/boot_id"
export TEST_SCREENS="$VR/screens" TEST_PANES="$VR/panes" TEST_PS="$VR/ps" TEST_PODMAN_NAMES="$VR/podman"

# verify <args...> — verify-restore under the fakes; its stdout (the report) in $report,
# its stderr in $why, its status in $rc. SCRATCH/state holds the manifest.
verify() {
    report="$(HOME="$HOME_ON" PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" CCY_STATE_DIR="$SCRATCH/state" \
        CCY_BOOT_ID_FILE="$VR/boot_id" CCY_SESSIONS_POLL_SECONDS=0 \
        "$TOOL" verify-restore "$@" </dev/null 2>"$VR/stderr")"
    rc=$?
    why="$(cat "$VR/stderr")"
}
# manifest <boot> [name prefix dir]... — the restore manifest as the library writes it, each
# session one the restore found running (nothing for set-going to do).
manifest() {
    local boot="$1"
    shift
    local -a entries=()
    while [ $# -ge 3 ]; do
        entries+=("$1" "$2" "$3" none "" "" "")
        shift 3
    done
    manifest_full "$boot" "${entries[@]}"
}
# manifest_full <boot> [name prefix dir going at detail transcript]... — every field given.
manifest_full() {
    local boot="$1"
    shift
    printf '%s\n' "$boot" >"$VR/manifest-boot"
    CCY_STATE_DIR="$SCRATCH/state" CCY_BOOT_ID_FILE="$VR/manifest-boot" ccy_restore_manifest_write "$@"
}

rm -rf "$SCRATCH/state"
verify
check "no restore recorded: refused, not reported as fine" "1" "$rc"
check "and says so" "yes" "$([[ "$why" == *"no session restore has been recorded"* ]] && echo yes || echo no)"

manifest earlier-boot ccy-a ccy "$A"
verify
check "a manifest from an earlier boot is refused" "1" "$rc"
check "and names the reason" "yes" "$([[ "$why" == *"earlier boot"* ]] && echo yes || echo no)"

manifest this-boot
verify
check "a restore that brought nothing up this boot passes" "0" "$rc"
check "with nothing on stdout" "" "$report"

# Four sessions, one of each state. ccy-a runs its container; ccy-b waits at Quick Launch;
# cc-c waits at the token chooser; ccy-d's launcher exited and holds its window; ccy-e is gone.
manifest this-boot ccy-a ccy "$A" ccy-b ccy "$B" cc-c cc "$A" ccy-d ccy "$B" ccy-e ccy "$A"
printf '%s\n' "ccy-a 0 $A" "ccy-b 0 $B" "cc-c 0 $A" "ccy-d 0 $B" >"$SESSIONS"
printf '%s\n' "ccy-a 100" "ccy-b 200" "cc-c 300" "ccy-d 400" >"$VR/panes"
printf '%s\n' "100 1 bash -c trampoline" "101 100 /var/local/claude-yolo/claude-yolo" \
    "102 101 podman run --rm -it --name a_yolo_1 claude-yolo:latest" \
    "200 1 bash -c trampoline" "300 1 bash -c trampoline" "400 1 bash -c trampoline" >"$VR/ps"
printf 'a_yolo_1\n' >"$VR/podman"
printf 'claude is running\n' >"$VR/screens/ccy-a"
printf 'Found previous launch configuration\n%s \n\n\n' "$CCY_PROMPT_QUICK_LAUNCH" >"$VR/screens/ccy-b"
printf '%s [0-2]: \n' "$CCY_PROMPT_TOKEN_SELECT" >"$VR/screens/cc-c"
printf 'ccy exited with status 1. %s\n' "$CCY_SESSION_ENDED_TEXT" >"$VR/screens/ccy-d"
verify
check "any session not OK fails the check" "1" "$rc"
check "the running one is OK" "yes" "$([[ "$report" == *"ccy-a OK"* ]] && echo yes || echo no)"
check "the one at Quick Launch is named, with the prompt" "yes" \
    "$([[ "$report" == *"ccy-b WAITING-AT-PROMPT quick-launch"* ]] && echo yes || echo no)"
check "cc at its token chooser is named" "yes" \
    "$([[ "$report" == *"cc-c WAITING-AT-PROMPT token-select"* ]] && echo yes || echo no)"
check "a launcher that exited is dead" "yes" "$([[ "$report" == *"ccy-d DEAD launcher-exited"* ]] && echo yes || echo no)"
check "a session that is gone is dead" "yes" "$([[ "$report" == *"ccy-e DEAD session-not-running"* ]] && echo yes || echo no)"
check "one line per restored session" "5" "$(printf '%s\n' "$report" | grep -c .)"
check "and the reason for failing goes to stderr" "yes" "$([[ "$why" == *"Not every restored session"* ]] && echo yes || echo no)"

# Once the prompts are answered and the containers run, the same sessions pass.
manifest this-boot ccy-a ccy "$A" cc-c cc "$A"
printf 'claude is running\n' >"$VR/screens/cc-c"
verify
check "everything past its prompts, containers up: passes" "0" "$rc"
check "and reports each as OK" "ccy-a OK|cc-c OK" "$(printf '%s' "${report//$'\n'/|}")"

# A ccy session whose container is not up yet is STARTING, and --wait gives up on it at the
# deadline rather than waiting for ever.
manifest this-boot ccy-b ccy "$B"
printf 'Building image...\n' >"$VR/screens/ccy-b"
verify --wait 0
check "a container not yet up is starting, and not OK" "ccy-b STARTING:1" "$report:$rc"
verify --wait soon
check "--wait needs a number" "64" "$rc"

# A probe that fails is a failure, not a state.
: >"$VR/screens/.keep"
rm -f "$VR/screens/ccy-b"
verify
check "an unreadable screen fails the check instead of guessing" "1" "$rc"
check "and says whose" "yes" "$([[ "$why" == *"screen of ccy-b could not be read"* ]] && echo yes || echo no)"

echo ""
echo "=== set-going: each restored session's first input, once its prompt has drawn (#88) ==="
# The real tool, the real library, the fake tmux: what is typed is what the log records.
FLOOR=150000
# The line the tool types for a compact, read from the tool, so the case drives the constant.
SET_GOING_COMPACT_EXPECTED="$(awk -F'"' '/^SET_GOING_COMPACT=/ { print $2 }' "$TOOL")"
check "the compact line is found in the tool" "yes" "$([[ "$SET_GOING_COMPACT_EXPECTED" == "/compact "* ]] && echo yes || echo no)"
RULE="$(printf '─%.0s' $(seq 1 60))"
PROMPT_SCREEN="$(printf '%s\n' "  Claude Code" "$RULE" "❯ " "$RULE" "  ⏵⏵ bypass permissions on (shift+tab to cycle)")"
# set_going [env assignments...] — set-going under the fakes, no waiting and no submit pause;
# stderr in $why, status in $rc, the fake tmux's log in $LOG.
set_going() {
    : >"$LOG"
    env -u CLAUDE_CONFIG_DIR HOME="$HOME_ON" PATH="$BIN:$PATH" CCY_LIB="$LIB_DIR" CCY_STATE_DIR="$SCRATCH/state" \
        CCY_BOOT_ID_FILE="$VR/boot_id" CCY_SESSIONS_POLL_SECONDS=0 CCY_SESSIONS_SET_GOING_WAIT=0 \
        CCY_SESSIONS_SUBMIT_DELAY=0 CCY_RESTORE_COMPACT_FLOOR_TOKENS="$FLOOR" "$@" \
        "$TOOL" set-going </dev/null >"$VR/set-going-out" 2>"$VR/stderr"
    rc=$?
    why="$(cat "$VR/stderr")"
}
# transcript <file> <tokens> — a conversation whose last assistant message re-sends <tokens>.
transcript() {
    mkdir -p "$(dirname "$1")"
    printf '{"type":"assistant","message":{"model":"claude-opus-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":0,"cache_read_input_tokens":%s}}}\n' \
        "$(($2 - 2))" >"$1"
}
# took_input <file> — the conversation took a line just now, as a submitted one is written.
took_input() {
    printf '{"type":"user","timestamp":"%s","message":{"content":"continue"}}\n' \
        "$(date -u +%Y-%m-%dT%H:%M:%S.000Z)" >>"$1"
}
T_A="$A/.claude/ccy/projects/-workspace/conversation.jsonl"
T_C="$(ccy_transcript_dir cc "$B" "$HOME_ON/.claude")/conversation.jsonl"
transcript "$T_A" 208525
transcript "$T_C" 5000

# ccy-a: big context, prompt drawn. cc-c: small context, prompt drawn. ccy-b: already
# running when the restore looked (a person's), drawn too. ccy-d: launcher exited. ccy-w:
# waits at Quick Launch.
manifest_full this-boot ccy-a ccy "$A" pending "" "" "" cc-c cc "$B" pending "" "" "" \
    ccy-b ccy "$B" none "" "" "" ccy-d ccy "$B" pending "" "" "" ccy-w ccy "$A" pending "" "" ""
printf '%s\n' "ccy-a 0 $A" "cc-c 0 $B" "ccy-b 0 $B" "ccy-d 0 $B" "ccy-w 0 $A" >"$SESSIONS"
printf '%s\n' "$PROMPT_SCREEN" >"$VR/screens/ccy-a"
printf '%s\n' "$PROMPT_SCREEN" >"$VR/screens/cc-c"
printf '%s\n' "$PROMPT_SCREEN" >"$VR/screens/ccy-b"
printf 'ccy exited with status 1. %s\n' "$CCY_SESSION_ENDED_TEXT" >"$VR/screens/ccy-d"
printf 'Found previous launch configuration\n%s \n' "$CCY_PROMPT_QUICK_LAUNCH" >"$VR/screens/ccy-w"
set_going
check "a session left alone fails the run" "1" "$rc"
check "nothing on stdout" "" "$(cat "$VR/set-going-out")"
check "at or above the floor: /compact, typed, then Enter on its own" \
    "send-keys =ccy-a: [literal] $SET_GOING_COMPACT_EXPECTED|send-keys =ccy-a: Enter" \
    "$(grep -F '=ccy-a:' "$LOG" | paste -sd '|')"
check "below it: continue" "send-keys =cc-c: [literal] continue|send-keys =cc-c: Enter" \
    "$(grep -F '=cc-c:' "$LOG" | paste -sd '|')"
check "a session the restore found running is never typed into" "" "$(grep -F '=ccy-b:' "$LOG")"
check "nor one whose launcher exited, or that waits at a ccy prompt" "" "$(grep -E '=ccy-(d|w):' "$LOG")"
check "each left alone is named, with why" "yes" \
    "$([[ "$why" == *"left ccy-d untouched: launcher-exited"* && "$why" == *"left ccy-w untouched: waiting-at-prompt-quick-launch-after-0s"* ]] && echo yes || echo "no: $why")"
check "and each set going says what it typed and why" "yes" \
    "$([[ "$why" == *"set ccy-a going: compact (context 208525 tokens, compact floor $FLOOR)"* && "$why" == *"set cc-c going: continue (context 5000 tokens"* ]] && echo yes || echo "no: $why")"
CCY_STATE_DIR="$SCRATCH/state" ccy_restore_manifest_read
check "the manifest records what each got" "compact|continue|none|untouched|untouched" \
    "$(IFS='|' && printf '%s' "${RM_GOING[*]}")"
check "with the context size, or the reason" "208525|5000||launcher-exited|waiting-at-prompt-quick-launch-after-0s" \
    "$(IFS='|' && printf '%s' "${RM_DETAIL[*]}")"
check "and the transcript it was read from" "$T_A|$T_C" "${RM_TRANSCRIPT[0]}|${RM_TRANSCRIPT[1]}"
set_going
check "a second run finds nothing pending and types nothing" "0:" "$rc:$(cat "$LOG")"

# verify-restore: what was typed has to be taken. ccy-a's container runs; cc-c needs none.
manifest_full this-boot ccy-a ccy "$A" compact "$EPOCHSECONDS" 208525 "$T_A" \
    cc-c cc "$B" continue "$EPOCHSECONDS" 5000 "$T_C"
printf '%s\n' "ccy-a 100" "cc-c 300" >"$VR/panes"
printf '%s\n' "100 1 bash -c trampoline" "101 100 /var/local/claude-yolo/claude-yolo" \
    "102 101 podman run --rm -it --name a_yolo_1 claude-yolo:latest" "300 1 bash -c trampoline" >"$VR/ps"
printf 'a_yolo_1\n' >"$VR/podman"
CCY_SESSIONS_START_WINDOW=3600 verify
check "typed, not yet taken, inside the window: setting going, not OK" \
    "ccy-a SETTING-GOING|cc-c SETTING-GOING:1" "${report//$'\n'/|}:$rc"
CCY_SESSIONS_START_WINDOW=0 verify
check "not taken by the end of the window: reported, not counted OK" \
    "ccy-a NOT-SET-GOING compact-not-started|cc-c NOT-SET-GOING continue-not-started:1" "${report//$'\n'/|}:$rc"
took_input "$T_A"
took_input "$T_C"
CCY_SESSIONS_START_WINDOW=0 verify
check "taken: OK" "ccy-a OK|cc-c OK:0" "${report//$'\n'/|}:$rc"
manifest_full this-boot ccy-a ccy "$A" untouched "" busy-before-set-going "" cc-c cc "$B" pending "" "" ""
verify
check "left alone, or still pending: named, and not OK" \
    "ccy-a NOT-SET-GOING busy-before-set-going|cc-c SETTING-GOING:1" "${report//$'\n'/|}:$rc"

# --wait stops once the restore has settled: nothing transient, the same answer twice.
manifest_full this-boot ccy-a ccy "$A" untouched "" no-transcript ""
started_at=$SECONDS
verify --wait 60
check "a settled failure ends the wait at once, not at the ceiling" "1:fast" \
    "$rc:$([ $((SECONDS - started_at)) -lt 30 ] && echo fast || echo slow)"
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
started_at=$SECONDS
verify --wait 1
check "a pending one keeps it waiting to the end" "ccy-a SETTING-GOING:1:waited" \
    "$report:$rc:$([ $((SECONDS - started_at)) -ge 1 ] && echo waited || echo early)"

# Cases that leave a session alone, each one named.
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
printf '%s\n' "ccy-a 0 $A" >"$SESSIONS"
set_going CCY_RESTORE_COMPACT_FLOOR_TOKENS=
check "no floor: nothing typed, the run fails" "1:" "$rc:$(cat "$LOG")"
check "and says where the floor comes from" "yes" \
    "$([[ "$why" == *"ccy_restore_compact_floor_tokens"* && "$why" == *"left ccy-a untouched: no-compact-floor"* ]] && echo yes || echo "no: $why")"
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
printf '%s\n' "✻ Thinking… (3s · esc to interrupt)" "$PROMPT_SCREEN" >"$VR/screens/ccy-a"
set_going
check "already busy when its prompt drew: left alone" "1:" "$rc:$(cat "$LOG")"
check "and named" "yes" "$([[ "$why" == *"left ccy-a untouched: busy-before-set-going"* ]] && echo yes || echo "no: $why")"
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
printf 'Building image...\n' >"$VR/screens/ccy-a"
set_going
check "a prompt that never draws in the time: left alone, saying so" "1::yes" \
    "$rc:$(cat "$LOG"):$([[ "$why" == *"left ccy-a untouched: prompt-not-drawn-after-0s"* ]] && echo yes || echo "no: $why")"
printf '%s\n' "$PROMPT_SCREEN" >"$VR/screens/ccy-a"
manifest_full this-boot ccy-a ccy "$A" pending "" "" "" cc-c cc "$B" pending "" "" ""
printf '%s\n' "ccy-a 0 $A" "cc-c 0 $B" >"$SESSIONS"
mv "$T_A" "$T_A.away"
set_going
check "no transcript: left alone, and the next session still set going" "1:yes:yes" \
    "$rc:$([[ "$why" == *"left ccy-a untouched: no-transcript"* ]] && echo yes || echo no):$(grep -qF 'send-keys =cc-c: [literal] continue' "$LOG" && echo yes || echo no)"
mv "$T_A.away" "$T_A"
printf '{"type":"user","message":{"content":"hi"}}\n' >"$T_A"
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
set_going
check "no context size in it: left alone, nothing typed" "1::yes" \
    "$rc:$(cat "$LOG"):$([[ "$why" == *"left ccy-a untouched: context-unreadable"* ]] && echo yes || echo no)"
transcript "$T_A" 208525
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
set_going TEST_SENDKEYS_BROKEN=ccy-a
check "keys that cannot be sent: left alone, and named" "1:yes" \
    "$rc:$([[ "$why" == *"left ccy-a untouched: send-failed"* ]] && echo yes || echo "no: $why")"
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
: >"$SESSIONS"
set_going
check "gone before its prompt drew: left alone, and named" "1:yes" \
    "$rc:$([[ "$why" == *"left ccy-a untouched: session-not-running"* ]] && echo yes || echo "no: $why")"
manifest_full earlier-boot ccy-a ccy "$A" pending "" "" ""
set_going
check "a manifest from an earlier boot sets nothing going" "1:" "$rc:$(cat "$LOG")"
manifest_full this-boot ccy-a ccy "$A" pending "" "" ""
set_going TEST_TMUX_BROKEN=1
check "a session list that cannot be read: each pending one left alone, and named" "1:yes" \
    "$rc:$([[ "$why" == *"left ccy-a untouched: session-list-unreadable"* ]] && echo yes || echo "no: $why")"

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
