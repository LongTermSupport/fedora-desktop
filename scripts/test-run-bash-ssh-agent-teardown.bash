#!/usr/bin/env bash
# Unit-test hl_ssh_agent_stop (run.bash, Plan 00063 Task 3.4).
#
# Extracts the ONE function under test out of run.bash with awk into a temp file and
# sources that — run.bash itself refuses to be sourced (it provisions on load), and this
# test must never start provisioning. The function is bounded by its `hl_ssh_agent_stop() {`
# line and the first `^}` after it; a refactor that moves the function keeps working, one
# that renames it fails this test loudly at extraction.
#
# WHY THIS TEST EXISTS. `ssh-agent -k` returns non-zero for two states that are not alike:
# the agent was already gone (harmless), or the kill FAILED and the agent is still running
# with an unlocked key reachable through $SSH_AUTH_SOCK for every remaining step of the run
# — ansible-galaxy, the main playbook, each optional playbook, the reboot. Reporting the
# second as the first and continuing is the exposure the function exists to close, and it
# exited 0 while doing so.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="$REPO_ROOT/run.bash"

if [ ! -f "$RUN_BASH" ]; then
    echo "FAIL: run.bash not found at $RUN_BASH" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

: > "$work/fn.bash"
for fn in hl_ssh_agent_stop headless_fail; do
    awk -v fn="$fn" '$0 == fn "() {" {p=1} p {print} p && /^\}/ {exit}' "$RUN_BASH" >> "$work/fn.bash"
    if ! grep -q "^${fn}() {" "$work/fn.bash"; then
        echo "FAIL: could not extract ${fn} from run.bash" >&2
        exit 1
    fi
done

# headless_fail renders with the colour constants; they are presentation, not behaviour.
RED='' ; YELLOW='' ; BOLD='' ; NC='' ; CROSS='x' ; ARROW='>'
export RED YELLOW BOLD NC CROSS ARROW

# shellcheck source=/dev/null
source "$work/fn.bash"

for fn in hl_ssh_agent_stop headless_fail; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: ${fn} is not defined after sourcing the extract" >&2
        exit 1
    fi
done

# `ssh-agent` is stubbed on PATH rather than as a shell function: the function under test
# invokes it as a command, so this is the shape it really meets, and a shell function named
# with a hyphen reads to shellcheck as unreachable code.
mkdir -p "$work/bin"
cat > "$work/bin/ssh-agent" <<'STUB'
#!/usr/bin/env bash
if [ -n "${STUB_SSH_AGENT_OUT:-}" ]; then
    printf '%s\n' "$STUB_SSH_AGENT_OUT"
fi
exit "${STUB_SSH_AGENT_RC:-0}"
STUB
chmod +x "$work/bin/ssh-agent"
PATH="$work/bin:$PATH"
export PATH

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# The first pid with no /proc entry. Walked rather than guessed, so it is certainly dead
# and there is no redirect hiding a lookup that did not work.
dead_pid=2
while [ -d "/proc/${dead_pid}" ]; do dead_pid=$((dead_pid + 1)); done

# 1. A clean kill says nothing and returns 0.
export STUB_SSH_AGENT_RC=0 STUB_SSH_AGENT_OUT=''
out=$(HL_SSH_AGENT_PID="$dead_pid" hl_ssh_agent_stop 2>&1); rc=$?
check "a clean kill returns 0" "0" "$rc"
check "a clean kill is silent" "" "$out"

# 2. A failing kill on an agent that is ALREADY GONE is the harmless case: still silent,
#    still 0. This is the case the old warn-and-continue was written for, and the only one
#    it was right about.
export STUB_SSH_AGENT_RC=1 STUB_SSH_AGENT_OUT='no such process'
out=$(HL_SSH_AGENT_PID="$dead_pid" hl_ssh_agent_stop 2>&1); rc=$?
check "an already-gone agent returns 0" "0" "$rc"
check "an already-gone agent is silent" "" "$out"

# 3. A failing kill on an agent that is STILL RUNNING must abort. $$ is this very shell, so
#    /proc/$$ is guaranteed to exist — a live pid with no race and nothing to clean up.
out=$(HL_SSH_AGENT_PID="$$" hl_ssh_agent_stop 2>&1); rc=$?
check "a surviving agent aborts the run" "1" "$rc"
case "$out" in
    *"survived teardown"*"unlocked key"*) check "the abort says what is exposed" "yes" "yes" ;;
    *) check "the abort says what is exposed" "yes" "no: ${out}" ;;
esac
case "$out" in
    *"$$"*) check "the abort names the surviving pid" "yes" "yes" ;;
    *) check "the abort names the surviving pid" "yes" "no: ${out}" ;;
esac

# 4. The abort must NOT unset HL_SSH_AGENT_PID first: hl_cleanup reads it on EXIT and should
#    get its attempt at the agent this function could not kill.
#
#    Asserted by asking headless_fail what the variable held AT THE MOMENT IT WAS CALLED,
#    not by inspecting the parent afterwards. hl_ssh_agent_stop has to run in a subshell
#    because the real headless_fail exits, and in a subshell an `unset` cannot reach the
#    parent — so a parent-side check would report "still set" whatever the function did.
headless_fail() { printf 'PID_AT_FAILURE=%s\n' "${HL_SSH_AGENT_PID:-unset}"; exit 1; }
out=$(HL_SSH_AGENT_PID="$$" hl_ssh_agent_stop 2>&1)
check "the abort leaves the pid for hl_cleanup" "PID_AT_FAILURE=$$" "$out"

# 5. No agent recorded at all: nothing to do, and it must not fail.
unset HL_SSH_AGENT_PID
out=$(hl_ssh_agent_stop 2>&1); rc=$?
check "no recorded agent returns 0" "0" "$rc"
check "no recorded agent is silent" "" "$out"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
