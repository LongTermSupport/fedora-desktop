#!/usr/bin/env bash
# Unit-test hl_ssh_agent_start (run.bash --headless): the login key is unlocked through a
# transient SSH_ASKPASS, and a WRONG passphrase fails instead of hanging.
#
# ssh-add asks a wrong askpass answer "Bad passphrase, try again" for ever (measured for
# Plan 00135: OpenSSH asked 24 times in 5 seconds), so a helper that answers every question
# turns a wrong RUN_BASH_GITHUB_SSH_PASSPHRASE into a run that never ends. The helper must
# answer only ssh-add's first question for the key.
#
# Uses the real ssh-keygen, ssh-agent and ssh-add with a throwaway encrypted key under a
# scratch HOME. The functions are extracted with awk and sourced, as the other run.bash gates
# do: run.bash provisions on load and must never be sourced whole.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="${RUN_BASH_UNDER_TEST:-$REPO_ROOT/run.bash}"

for tool in ssh-keygen ssh-agent ssh-add timeout; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is required (installed by the playbooks)" >&2
        exit 1
    fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

extract() {
    local fn="$1"
    awk -v fn="$fn" '$0 ~ "^" fn "\\(\\) ?\\{" {p=1} p {print} p && /^\}/ {exit}' "$RUN_BASH" >>"$work/fn.bash"
    if ! grep -qE "^${fn}\(\) ?\{" "$work/fn.bash"; then
        echo "FAIL: could not extract ${fn} from ${RUN_BASH}" >&2
        exit 1
    fi
}
: >"$work/fn.bash"
for fn in hl_abort hl_ssh_agent_start; do
    extract "$fn"
done

PASSPHRASE='correct horse battery'
mkdir -p "$work/home/.ssh"
if ! ssh-keygen -q -t ed25519 -N "$PASSPHRASE" -C test-key -f "$work/home/.ssh/id" >"$work/keygen.out" 2>&1; then
    echo "FAIL: could not make the test key: $(cat "$work/keygen.out")" >&2
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
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

# start <passphrase> — hl_ssh_agent_start in a fresh shell with nobody at a terminal (setsid
# detaches it, so ssh-add cannot fall back to asking on a tty). Writes its output to
# $work/out and the agent's listing to $work/listed, kills the agent, and prints "returned",
# "stopped" (hl_abort) or "hung".
cat >"$work/driver.bash" <<'DRIVER'
# driver.bash <fn.bash> <passphrase> <work dir>
source "$1"
HL_SECRET_FILES=()
HL_GITHUB_SSH_PASSPHRASE="$2"
cleanup() {
    if [ -n "${SSH_AGENT_PID:-}" ]; then kill "$SSH_AGENT_PID"; fi
    rm -f "${HL_SECRET_FILES[@]}"
}
trap cleanup EXIT
hl_ssh_agent_start
cp "$HL_ASKPASS" "$3/helper.copy"
ssh-add -l >"$3/listed" 2>&1
echo returned
DRIVER

start() {
    local verdict code=0
    timeout 15 setsid bash "$work/driver.bash" "$work/fn.bash" "$1" "$work" >"$work/out" 2>&1 </dev/null || code=$?
    if [ "$code" -eq 124 ]; then
        verdict=hung
    elif grep -qx returned "$work/out"; then
        verdict=returned
    else
        verdict=stopped
    fi
    echo "$verdict"
}

echo "== the right passphrase"
rm -f "$work/listed" "$work/helper.copy"
check "the run carries on" "returned" "$(HOME="$work/home" start "$PASSPHRASE")"
check "  with the key in the agent" "yes" "$(grep -qF test-key "$work/listed" 2>/dev/null && echo yes || echo no)"
check "  and the helper's text holds no passphrase" "no" "$(grep -qF "$PASSPHRASE" "$work/helper.copy" && echo yes || echo no)"

echo "== a wrong passphrase"
check "the run stops, and does not hang" "stopped" "$(HOME="$work/home" start 'not the passphrase')"
check "  saying why" "yes" "$(grep -qF 'could not be loaded' "$work/out" && echo yes || echo no)"
check "  and never prints the passphrase" "no" "$(grep -qF 'not the passphrase' "$work/out" && echo yes || echo no)"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
