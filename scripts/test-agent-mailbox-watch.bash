#!/usr/bin/env bash
# Test scripts/agent-mailbox-watch.bash, the watcher CLAUDE/AgentMailbox.md describes.
#
# Every case runs against a scratch mailbox (AGENT_MAILBOX_DIR) with WATCH_MAX_SECONDS=0,
# so the watcher probes exactly once and then either reports what is waiting (exit 0) or
# gives up (exit 3). Nothing sleeps.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WATCH="$SCRIPT_DIR/agent-mailbox-watch.bash"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label"
        echo "        wanted: $(printf '%q' "$want")"
        echo "        got:    $(printf '%q' "$got")"
    fi
}

# watch <box> <side>: sets out and rc from one probe of the watcher.
out=""
rc=0
watch() {
    if out="$(AGENT_MAILBOX_DIR="$1" WATCH_MAX_SECONDS=0 bash "$WATCH" "$2" 2>/dev/null)"; then
        rc=0
    else
        rc=$?
    fi
}

echo "== usage"
out="$(bash "$WATCH" 2>&1)"
rc=$?
check "no side is a usage error" "64" "$rc"
out="$(bash "$WATCH" sideways 2>&1)"
rc=$?
check "an unknown side is a usage error" "64" "$rc"

echo "== a missing mailbox is made, private"
box="$WORK/new-box"
watch "$box" desktop
check "nothing waiting: exit 3" "3" "$rc"
check "  and nothing on stdout" "" "$out"
check "the mailbox is created mode 0700" "700" "$(stat -c %a "$box")"
check "  with its three folders" "yes" \
    "$([ -d "$box/to-desktop" ] && [ -d "$box/from-desktop" ] && [ -d "$box/to-ccy" ] && echo yes || echo no)"

echo "== desktop side"
box="$WORK/desktop"
mkdir -p "$box"/{to-desktop,from-desktop,to-ccy}
printf 'do a thing\n' >"$box/to-desktop/0001-run-meta.md"
watch "$box" desktop
check "a request with no reply wakes the desktop" "0" "$rc"
check "  and names the request" "$box/to-desktop/0001-run-meta.md" "$out"
printf 'STATE: running\n' >"$box/from-desktop/0001-reply.md"
watch "$box" desktop
check "a request with a reply, even a running one, does not" "3" "$rc"
printf 'next\n' >"$box/to-desktop/0002-deploy.md"
printf 'later\n' >"$box/to-desktop/0003-deploy.md"
watch "$box" desktop
check "two waiting requests are both named, lowest first" \
    "$box/to-desktop/0002-deploy.md"$'\n'"$box/to-desktop/0003-deploy.md" "$out"

echo "== ccy side"
box="$WORK/ccy"
mkdir -p "$box"/{to-desktop,from-desktop,to-ccy}
printf 'STATE: running\n' >"$box/from-desktop/0001-reply.md"
watch "$box" ccy
check "a reply still running does not wake ccy" "3" "$rc"
printf 'STATE: running\nSTATE: done\nEXIT: 0\n' >"$box/from-desktop/0001-reply.md"
watch "$box" ccy
check "a reply that reaches STATE: done wakes ccy" "0" "$rc"
check "  and names the reply" "$box/from-desktop/0001-reply.md" "$out"
watch "$box" ccy
check "  once only" "3" "$rc"
printf 'STATE: failed\nEXIT: 1\n' >"$box/from-desktop/0002-reply.md"
watch "$box" ccy
check "a reply that reaches STATE: failed wakes ccy" "0" "$rc"
printf 'a question\n' >"$box/to-ccy/D-0001-question.md"
watch "$box" ccy
check "a message the desktop starts wakes ccy" "0" "$rc"
check "  and names it" "$box/to-ccy/D-0001-question.md" "$out"
watch "$box" ccy
check "  once only" "3" "$rc"
check "the seen record stays inside the mailbox" "yes" "$([ -f "$box/.seen-ccy" ] && echo yes || echo no)"

echo ""
echo "agent-mailbox-watch: passed: $passed  failed: $failed"
if [ "$failed" -ne 0 ]; then
    exit 1
fi
