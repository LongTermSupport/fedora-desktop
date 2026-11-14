#!/bin/bash
# How a ccy launch ends where only a person could go on and nobody is there: a boot restore or a
# restart's relaunch in the session's tmux pane, or a headless launch (fedora-desktop#283). Such a
# stop is not the end of the session: the pane's trampoline (ccy_registry_trampoline,
# registry.Trampoline) keeps the session's record for CCY_EXIT_UNATTENDED_STOP, so the next
# restore starts it again, and verify-restore reports it NOT-STARTED <reason>. A launch fault
# (a failed build, an unreadable file) exits 1, and the trampoline deletes the record as for any
# ended session. The rule ccy-unattended-refusal-status (CLAUDE/QA.md) holds every such stop to
# this function.
#
# Requires print_error, and ccy_restart_refuse (restart-request.bash) for a restart's stop.

# registry.ExitUnattendedStop is the same number.
CCY_EXIT_UNATTENDED_STOP=88

# ccy_unattended_stop <reason> <sentence> [<restart-session-id>] — end the launch with
# CCY_EXIT_UNATTENDED_STOP. <reason> is one word (lower case, digits, hyphens: a launch
# question's ccy_known_prompts name, or token-used-up, restart-budget-spent, ...), <sentence>
# what a person must do. Both go on one line into CCY_UNATTENDED_STOP_FILE, which the trampoline
# names and clears when the session is next started, for verify-restore to read. A restart (the
# session id given, or CCY_RESTART_RELAUNCH_SESSION) also says how to resume it by hand.
ccy_unattended_stop() {
    local reason="${1:?ccy_unattended_stop requires a reason}" sentence="${2:?ccy_unattended_stop requires a sentence}"
    local sid="${3:-${CCY_RESTART_RELAUNCH_SESSION:-}}" file="${CCY_UNATTENDED_STOP_FILE:-}"
    [ -z "$sid" ] || ccy_restart_refuse "$sid" "$sentence"
    if [ -n "$file" ] && ! { mkdir -p -- "${file%/*}" && printf '%s %s\n' "$reason" "$sentence" >"$file.tmp" && mv -f -- "$file.tmp" "$file"; }; then
        print_error "Could not write why the launch stopped to $file, so verify-restore cannot name it ($reason)."
    fi
    exit "$CCY_EXIT_UNATTENDED_STOP"
}
