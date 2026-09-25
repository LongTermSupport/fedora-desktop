#!/usr/bin/env bash
# agent-mailbox-watch.bash — block until the agent mailbox has something for this side,
# print what it is, exit. The protocol is CLAUDE/AgentMailbox.md.
#
# Run it as a background command. The harness wakes the agent when a background command
# exits, so the exit IS the notification. Re-arm it after handling what it printed.
#
#   agent-mailbox-watch.bash ccy       wakes the ccy agent when:
#                                        - a from-desktop/NNNN-reply.md reaches STATE: done
#                                          or failed;
#                                        - a new to-ccy/*.md message arrives.
#                                      Each file wakes it once; what was printed goes into
#                                      the mailbox's .seen-ccy.
#   agent-mailbox-watch.bash desktop   wakes the desktop agent when a to-desktop/NNNN-*.md
#                                      request has no from-desktop/NNNN-reply.md yet.
#
# The mailbox is AGENT_MAILBOX_DIR, default untracked/agent-mailbox/ in this checkout. It
# can hold host details, so it is never tracked; it is made, mode 0700, on first use.
#
# Exit 0: something is waiting, and its paths are on stdout. Exit 3: nothing arrived before
# WATCH_MAX_SECONDS (default 21600, six hours), so re-arm. Exit 64: usage.
set -euo pipefail

side="${1:-}"
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
box="${AGENT_MAILBOX_DIR:-$repo_root/untracked/agent-mailbox}"
max="${WATCH_MAX_SECONDS:-21600}"
interval=5

pending_for_ccy() {
    local seen="$box/.seen-ccy" f
    touch "$seen"
    for f in "$box"/from-desktop/*-reply.md "$box"/to-ccy/*.md; do
        [[ -f "$f" ]] || continue
        if grep -qxF "$f" "$seen"; then
            continue
        fi
        # A reply counts only once it is finished; "STATE: running" is not news.
        if [[ "$f" == */from-desktop/* ]] && ! grep -qE '^STATE: (done|failed)' "$f"; then
            continue
        fi
        printf '%s\n' "$f"
    done
}

pending_for_desktop() {
    local f base
    for f in "$box"/to-desktop/[0-9]*.md; do
        [[ -f "$f" ]] || continue
        base="$(basename "$f")"
        [[ -e "$box/from-desktop/${base%%-*}-reply.md" ]] || printf '%s\n' "$f"
    done
}

case "$side" in
    ccy) probe=pending_for_ccy ;;
    desktop) probe=pending_for_desktop ;;
    *)
        echo "usage: agent-mailbox-watch.bash ccy|desktop" >&2
        exit 64
        ;;
esac

if [[ ! -d "$box" ]]; then
    mkdir -p "$box"
    chmod 0700 "$box"
fi
mkdir -p "$box/to-desktop" "$box/from-desktop" "$box/to-ccy"

waited=0
while true; do
    found="$("$probe")"
    if [[ -n "$found" ]]; then
        printf '%s\n' "$found"
        if [[ "$side" == ccy ]]; then
            printf '%s\n' "$found" >>"$box/.seen-ccy"
        fi
        exit 0
    fi
    if [[ "$waited" -ge "$max" ]]; then
        echo "agent-mailbox-watch.bash: nothing for ${side} in ${max}s; re-arm" >&2
        exit 3
    fi
    sleep "$interval"
    waited=$((waited + interval))
done
