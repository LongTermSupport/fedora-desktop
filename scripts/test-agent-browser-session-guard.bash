#!/usr/bin/env bash
# Unit-test the ccy browser session guard (Plan 00140).
#
# Every agent-browser session name is its own daemon and its own browser, so an agent
# that starts a new session per task while the old ones still run piles up Chromium
# after Chromium. The guard sits between each of the three browser commands and the
# real binary and refuses a command that would start another session once the cap is
# reached.
#
# Runs the guard from THIS checkout against a fake agent-browser that keeps its
# "live sessions" in a state file. No browser, no daemon, no network. The real-binary
# check is the plan's acceptance.bash, which needs a ccy container.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the
# full picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
GUARD="$CCY_DIR/agent-browser-session-guard"

if [ ! -f "$GUARD" ]; then
    echo "FAIL: guard not found at $GUARD" >&2
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# The fake binary. It understands the handful of flags the wrappers and the guard
# pass, answers `session` and `session list` from $FAKE_STATE, and treats every other
# command as one that starts (or reuses) the target session, as the real CLI does.
FAKE="$WORK/fake-agent-browser"
cat > "$FAKE" <<'FAKE_EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%q ' "$@" >> "$FAKE_LOG"
echo >> "$FAKE_LOG"
json=0
session="${AGENT_BROWSER_SESSION:-default}"
session_set=0
rest=()
while (( $# )); do
    case "$1" in
        --namespace|--headed|--config) shift 2 ;;
        --json) json=1; shift ;;
        --session)
            if (( ! session_set )); then session="$2"; session_set=1; fi
            shift 2 ;;
        --session=*)
            if (( ! session_set )); then session="${1#--session=}"; session_set=1; fi
            shift ;;
        *) rest+=("$1"); shift ;;
    esac
done
if [[ "${rest[*]:-}" == "session list" ]]; then
    if [[ -n "${FAKE_LIST_FAIL:-}" ]]; then echo "daemon socket dir unreadable" >&2; exit 1; fi
    if [[ -n "${FAKE_LIST_GARBAGE:-}" ]]; then echo "Active sessions:"; exit 0; fi
    # A just-closed session stays listed for FAKE_LINGER more list calls.
    lingering=()
    if [[ -f "$FAKE_STATE.linger" ]]; then
        n="$(cat "$FAKE_STATE.linger")"
        if (( n > 0 )); then
            lingering=(closing-one)
            echo $((n - 1)) > "$FAKE_STATE.linger"
        fi
    fi
    mapfile -t live < "$FAKE_STATE"
    all=("${live[@]}" "${lingering[@]}")
    if (( ${#all[@]} )); then
        printf '%s\n' "${all[@]}" | jq -R . | jq -cs '{success:true,data:{sessions:.}}'
    else
        echo '{"success":true,"data":{"sessions":[]}}'
    fi
    exit 0
fi
if [[ "${rest[*]:-}" == "session" ]]; then
    if (( json )); then jq -cn --arg s "$session" '{data:{session:$s},success:true}'; else echo "$session"; fi
    exit 0
fi
if ! grep -qxF -- "$session" "$FAKE_STATE"; then echo "$session" >> "$FAKE_STATE"; fi
echo "$session ${rest[*]:-}" >> "$FAKE_LOG.launch"
echo "ran: ${rest[*]:-}"
exit "${FAKE_EXIT:-0}"
FAKE_EOF
chmod 755 "$FAKE"

PASSED=0
FAILED=0
OUT=""
ERR=""
RC=0

# reset <live-session>... : start a case with exactly these sessions live.
reset() {
    : > "$WORK/state"
    rm -f "$WORK/state.linger"
    local s
    for s in "$@"; do echo "$s" >> "$WORK/state"; done
    : > "$WORK/log"
    : > "$WORK/log.launch"
}

# launched : did the fake run anything other than a session probe?
launched() { [[ -s "$WORK/log.launch" ]]; }

# run_guard [VAR=value ...] -- <caller args>... : run the guard as agent-browser-headless.
run_guard() {
    local envs=()
    while (( $# )) && [[ "$1" != -- ]]; do envs+=("$1"); shift; done
    shift
    OUT="$(env FAKE_STATE="$WORK/state" FAKE_LOG="$WORK/log" "${envs[@]}" \
        bash "$GUARD" agent-browser-headless "$FAKE" --namespace headless --headed false -- "$@" \
        2> "$WORK/stderr")"
    RC=$?
    ERR="$(cat "$WORK/stderr")"
}

pass() { printf '  PASS  %s\n' "$1"; PASSED=$((PASSED + 1)); }
fail() {
    printf '  FAIL  %s\n' "$1"
    printf '        rc=%s stdout=%q\n        stderr=%q\n' "$RC" "$OUT" "$ERR"
    FAILED=$((FAILED + 1))
}

# ran <description> <expected-command-text> : the guard handed the command to the binary.
ran() {
    if [[ "$RC" == 0 && "$OUT" == "ran: $2" ]]; then pass "$1"; else fail "$1"; fi
}

# refused <description> <text the refusal must contain>... : exit 3, nothing launched,
# nothing on stdout, and the message names what it must.
refused() {
    local desc="$1"; shift
    local ok=1 needle
    [[ "$RC" == 3 ]] || ok=0
    [[ -z "$OUT" ]] || ok=0
    if launched; then ok=0; fi
    for needle in "$@"; do [[ "$ERR" == *"$needle"* ]] || ok=0; done
    if (( ok )); then pass "$desc"; else fail "$desc"; fi
}

echo ""
echo "=== nothing live: anything may start the first session ==="
reset
run_guard -- open https://example.com
ran "first open on the default session" "open https://example.com"
reset
run_guard -- --session task-a open https://example.com
ran "first open on a named session" "open https://example.com"

echo ""
echo "=== the session is already live: reuse is never refused ==="
reset default
run_guard -- open https://example.com
ran "second open on default reuses it" "open https://example.com"
reset task-a
run_guard -- --session task-a snapshot -i
ran "--session naming the live session" "snapshot -i"
reset task-a
run_guard AGENT_BROWSER_SESSION=task-a -- get url
ran "AGENT_BROWSER_SESSION naming the live session" "get url"

echo ""
echo "=== THE leak: a new session while one is live is refused ==="
reset task-a
run_guard -- open https://example.com
refused "export lost, command falls back to default" "task-a" "agent-browser-headless --session task-a" "agent-browser-headless close --all"
reset default
run_guard -- --session task-b open https://example.com
refused "a new --session name" "default"
reset default
run_guard -- --session=task-b open https://example.com
refused "a new --session=name" "default"
reset default
run_guard -- open https://example.com --session task-b
refused "--session after the command" "default"
reset default
run_guard AGENT_BROWSER_SESSION=task-c -- open https://example.com
refused "a new AGENT_BROWSER_SESSION" "default"
reset default
run_guard -- --session task-b get url
refused "a non-open command also starts a browser" "default"
reset default
run_guard -- --json --session task-b snapshot
refused "a boolean flag before the command" "default"
reset default
run_guard -- --session task-b --profile /tmp/p open https://example.com
refused "a value flag before the command" "default"

echo ""
echo "=== commands that start no browser are never refused ==="
reset task-a
run_guard -- close --all
ran "close --all" "close --all"
reset task-a
run_guard -- close
ran "bare close on a session that is not live" "close"
reset task-a
run_guard -- --session task-b close
ran "close naming another session" "close"
reset task-a
run_guard -- session list
if [[ "$RC" == 0 && "$OUT" == *task-a* ]]; then pass "session list"; else fail "session list"; fi
reset task-a
run_guard -- skills get core --full
ran "skills get core --full" "skills get core --full"
reset task-a
run_guard -- --version
ran "--version" "--version"
reset task-a
run_guard -- --help
ran "--help" "--help"
reset task-a
run_guard --
ran "no arguments at all" ""
reset task-a
run_guard -- --session task-b doctor
ran "doctor" "doctor"

echo ""
echo "=== a value that happens to be a command word does not open the guard ==="
reset default
run_guard -- --session close open https://example.com
refused "--session close is a session name, not the close command" "default"
reset default
run_guard -- --profile skills open https://example.com --session task-b
refused "a --profile value named skills" "default"

echo ""
echo "=== the cap is configurable, and validated ==="
reset task-a
run_guard CCY_BROWSER_MAX_SESSIONS=2 -- --session task-b open https://example.com
ran "cap 2 allows a second session" "open https://example.com"
reset task-a task-b
run_guard CCY_BROWSER_MAX_SESSIONS=2 -- --session task-c open https://example.com
refused "cap 2 refuses a third" "task-a" "task-b"
for bad in 0 -1 abc 1.5 ""; do
    reset
    run_guard "CCY_BROWSER_MAX_SESSIONS=$bad" -- open https://example.com
    if [[ "$RC" != 0 && "$RC" != 3 && -z "$OUT" && "$ERR" == *CCY_BROWSER_MAX_SESSIONS* ]] && ! launched; then
        pass "invalid cap '$bad' fails loudly and runs nothing"
    else
        fail "invalid cap '$bad' fails loudly and runs nothing"
    fi
done

echo ""
echo "=== a just-closed session still listed for a moment is waited out ==="
reset
echo 3 > "$WORK/state.linger"
run_guard -- --session task-b open https://example.com
ran "close --all then open: the lingering session is waited out" "open https://example.com"
reset
echo 1000 > "$WORK/state.linger"
run_guard -- --session task-b open https://example.com
refused "a session that never goes away is still refused" "closing-one"

echo ""
echo "=== probe failures fail loudly, and run nothing ==="
reset
run_guard FAKE_LIST_FAIL=1 -- open https://example.com
if [[ "$RC" != 0 && "$RC" != 3 && -z "$OUT" && "$ERR" == *"session list"* ]] && ! launched; then
    pass "session list failing"
else
    fail "session list failing"
fi
reset
run_guard FAKE_LIST_GARBAGE=1 -- open https://example.com
if [[ "$RC" != 0 && "$RC" != 3 && -z "$OUT" && -n "$ERR" ]] && ! launched; then
    pass "session list output that is not the expected JSON"
else
    fail "session list output that is not the expected JSON"
fi

echo ""
echo "=== pass-through is exact ==="
reset
run_guard FAKE_EXIT=7 -- open https://example.com
if [[ "$RC" == 7 ]]; then pass "the binary's exit code is preserved"; else fail "the binary's exit code is preserved"; fi
reset
run_guard -- fill @e1 'two words' --session default
if grep -qxF -- '--namespace headless --headed false fill @e1 two\ words --session default ' "$WORK/log"; then
    pass "wrapper flags first, caller args unchanged"
else
    fail "wrapper flags first, caller args unchanged"
fi

echo ""
echo "=== the guard is wired into the image ==="
DOCKERFILE="$CCY_DIR/Dockerfile"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"
for mode in headed headless lite-headless; do
    line="$(grep -E "^[[:space:]]*\"exec /opt/claude-yolo/agent-browser-session-guard agent-browser-$mode \\\$REAL " "$DOCKERFILE")"
    if [[ -n "$line" && "$line" == *' -- \"\$@\""'* ]]; then
        pass "agent-browser-$mode execs the guard with -- before the caller's args"
    else
        fail "agent-browser-$mode execs the guard with -- before the caller's args"
    fi
done
if grep -qE '^COPY agent-browser-session-guard /opt/claude-yolo/agent-browser-session-guard$' "$DOCKERFILE"; then
    pass "Dockerfile copies the guard into the image"
else
    fail "Dockerfile copies the guard into the image"
fi
if grep -qF 'files/var/local/claude-yolo/agent-browser-session-guard"' "$PLAY"; then
    pass "play-claude-yolo.yml stages the guard into the build context"
else
    fail "play-claude-yolo.yml stages the guard into the build context"
fi

echo ""
echo "──────────────────────────────────────────────────────────────"
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"

if [ "$PASSED" -eq 0 ]; then
    echo "ERROR: zero tests ran — the suite is broken, not the guard clean" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
echo "OK"
