#!/usr/bin/env bash
# Plan 00214 — check-changed-plays.bash: one acceptance check per call, on this host's real
# ledger and checkout.
#
# Usage: check-changed-plays.bash <check> <repo root> <run dir>
#   same      the health report's play-freshness check and run.bash --changed's helper name
#             the same plays: the report's changed and failed plays are the helper's RUN
#             lines, and its gone plays the GONE lines (issue #110, the 10 against 25)
#   report    after a collection, fedora-desktop-health prints this checkout's
#             run.bash --changed when a play is to run, and does not when none is
#   reminder  the login shell's second report of the day names the same command
#
# Exit 0 when the check passed, 1 when it failed (the reason on stderr). With no play to
# run, `report` and `reminder` cannot judge the case they exist for: they say so and add
# their name to <run dir>/not-judged, which acceptance.bash turns into exit 2.
set -euo pipefail

check="${1:?usage: check-changed-plays.bash <same|report|reminder> <repo root> <run dir>}"
root="${2:?usage: check-changed-plays.bash <check> <repo root> <run dir>}"
runDir="${3:?usage: check-changed-plays.bash <check> <repo root> <run dir>}"
fail() {
    printf '[FAIL] %s\n' "$1" >&2
    exit 1
}

# The checkout's own helper, as the installed commands run it: PYTHONPATH, -P, and the
# system interpreter by path.
helper() {
    PYTHONPATH="${root}" /usr/bin/python3 -P -m "$@"
}

# The marker lines of run.bash --changed's helper, failing if it gave no answer.
changed_plays() {
    helper helpers.play_ledger.changed_plays --repo-root "${root}" || fail "changed_plays gave no answer (exit $?); its reason is above"
}

# What login_message says, read from this checkout's helper, never a copy: on the first line
# the words it names run.bash --changed's work with (_RUNS_CHANGED), on the second this
# checkout's command as run_changed_command quotes it for a document with a play to run.
login_words() {
    PYTHONPATH="${root}" /usr/bin/python3 -P -c 'import sys
from helpers.host_health import login_message
from helpers.play_ledger import freshness
command = login_message.run_changed_command({"plays": [{"state": freshness.STALE}]}, checkout=sys.argv[1])
if not command or "\n" in command or "\n" in login_message._RUNS_CHANGED:
    sys.exit(f"login_message gave no one-line command or words: {command!r}")
print(login_message._RUNS_CHANGED)
print(command)' "${root}"
}

# Sets runs and command from login_words, failing if they cannot be read.
read_login_words() {
    local text
    text="$(login_words)" || fail "cannot read _RUNS_CHANGED and run_changed_command from helpers/host_health/login_message.py (above)"
    mapfile -t words <<<"${text}"
    [[ "${#words[@]}" -eq 2 && -n "${words[0]}" && -n "${words[1]}" ]] || fail "want two lines from login_message, got: ${text}"
    runs="${words[0]}" command="${words[1]}"
}

check_same() {
    local report status=0 helped
    report="$(helper helpers.play_ledger.check_freshness --repo-root "${root}")" || status=$?
    [[ "${status}" -le 1 ]] || fail "the report's play-freshness check gave no answer (exit ${status}); its reason is above"
    helped="$(changed_plays)"
    printf '%s\n' "${report}" >"${runDir}/check-freshness.out"
    printf '%s\n' "${helped}" >"${runDir}/changed-plays.out"
    awk -F ' — ' '/ — (changed since it was run here|its last run here did not succeed)$/ { print "RUN " $1 } / — no longer in the checkout$/ { print "GONE " $1 }' \
        "${runDir}/check-freshness.out" | sort >"${runDir}/report-plays.txt"
    awk '$1 == "RUN" || $1 == "GONE" { print $1 " " $2 }' "${runDir}/changed-plays.out" | sort >"${runDir}/helper-plays.txt"
    diff "${runDir}/report-plays.txt" "${runDir}/helper-plays.txt" >&2 || fail "the report (<) and run.bash --changed (>) name different plays"
    printf '  ok: both name the same %s plays (%s to run)\n' \
        "$(awk 'END { print NR }' "${runDir}/helper-plays.txt")" \
        "$(awk '$1 == "RUN" { n++ } END { print n + 0 }' "${runDir}/helper-plays.txt")"
}

# Whether run.bash --changed has a play to run now; records the check as not judged if not.
plays_to_run() {
    local helped
    # Explicit: called as an `if` condition, so set -e does not stop it here.
    helped="$(changed_plays)" || exit 1
    if [[ "${helped}" == *"RUN "* ]]; then
        return 0
    fi
    printf '%s\n' "${check}" >>"${runDir}/not-judged"
    printf '  not judged: no play has to run here, so there is no command to look for\n'
    return 1
}

check_report() {
    local printed runs command remedy
    local -a words
    read_login_words
    remedy="To run ${runs}: ${command}"
    # A collection, as host-health.service runs it at login, so the document is this code's.
    helper helpers.host_health.login_report --repo-root "${root}" --no-notify >"${runDir}/collection.out" ||
        fail "the collection exited $?; its output is in ${runDir}/collection.out"
    [[ -x "${HOME}/.local/bin/fedora-desktop-health" ]] || fail "fedora-desktop-health is not installed; play-host-health-login-report.yml installs it"
    printed="$("${HOME}/.local/bin/fedora-desktop-health")"
    printf '%s\n' "${printed}" >"${runDir}/fedora-desktop-health.out"
    if plays_to_run; then
        grep -qxF "  ${remedy}" "${runDir}/fedora-desktop-health.out" || fail "fedora-desktop-health does not print: ${remedy}"
        printf '  ok: it prints %s\n' "${command}"
    elif grep -qF 'run.bash --changed' "${runDir}/fedora-desktop-health.out"; then
        fail "fedora-desktop-health names run.bash --changed with no play to run"
    fi
}

check_reminder() {
    local second runs command
    local -a words
    read_login_words
    # Global, not local: the EXIT trap runs after this function has returned.
    stamps="$(mktemp -d)"
    trap 'rm -rf "${stamps}"' EXIT
    helper helpers.host_health.login_message --once-a-day --stamp-dir "${stamps}" >"${runDir}/login-first.out"
    second="$(helper helpers.host_health.login_message --once-a-day --stamp-dir "${stamps}")"
    printf '%s\n' "${second}" >"${runDir}/login-second.out"
    if plays_to_run; then
        [[ "${second}" == *", or ${command} to run ${runs}"* ]] || fail "the reminder does not name ${command}: ${second}"
        printf '  ok: the reminder names %s\n' "${command}"
    fi
}

case "${check}" in
same) check_same ;;
report) check_report ;;
reminder) check_reminder ;;
*) fail "unknown check: ${check}" ;;
esac
