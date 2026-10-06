#!/usr/bin/env bash
# Unit-test taking over a CCY session that is open in another terminal: the library's
# ccy_tmux_take_over and its helpers (files/var/local/claude-yolo/lib/tmux-session.bash),
# and the Ctrl-T key in the ccy-sessions picker (files/home/.local/bin/ccy-sessions).
#
# WHY THIS EXISTS. A session can be attached from one terminal only, so a terminal that
# dropped without letting go — an SSH connection that died while its tmux client stayed
# connected — holds the session hostage: every other terminal is refused it. Taking over
# detaches that other terminal and attaches this one. What must never happen is the
# session itself being harmed (a kill-session, or anything inside it), an attach that goes
# ahead when the other terminal did not let go, or a take-over the user did not confirm.
# None of that can be tried against a live tmux on the machine running the test, so a fake
# `tmux` holds the sessions and their clients in files and logs every call, and a fake
# `fzf` answers the picker from a queue. The real ccy-sessions is run under `script`,
# because the picker demands a terminal.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/home/.local/bin/ccy-sessions"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for lib in common-pure.bash tmux-session.bash; do
    if [ ! -f "$LIB_DIR/$lib" ]; then
        echo "FAIL: $lib not found at $LIB_DIR/$lib" >&2
        exit 1
    fi
done
if [ -z "$(command -v script)" ]; then
    echo "FAIL: script (util-linux) is needed to give ccy-sessions a terminal" >&2
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
# check_has <label> <needle> <haystack> — the haystack contains the needle.
check_has() {
    if [[ "$3" == *"$2"* ]]; then
        check "$1" yes yes
    else
        check "$1" "contains: $2" "$3"
    fi
}

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/sessions-take-over-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# ── the fakes ─────────────────────────────────────────────────────────────────────────
BIN="$SCRATCH/bin"
STATE="$SCRATCH/state"
LOG="$SCRATCH/calls.log"
mkdir -p "$BIN" "$STATE/clients"

# tmux: sessions are "<name> <dir>" lines in $STATE/sessions; a session's clients are
# "<tty> <activity-epoch>[ exiting|suspended]" lines in $STATE/clients/<name>. Every call
# is logged.
#
# It keeps tmux's two views of "who is on this session" apart, because they differ: the
# #{session_attached} count in list-sessions leaves out a client that has been told to leave
# and has not yet gone (flagged "exiting" here), or one that is suspended (flagged
# "suspended", gone only once detached), while list-clients — which the single-attach hook
# counts — still shows both.
#
# detach-client flags every client exiting. They are gone after TEST_TMUX_LINGER more
# list-clients calls (default 0: at once), or never with TEST_TMUX_STICKY. Other knobs:
#   TEST_TMUX_DETACH_FAIL  tmux refuses the detach
#   TEST_TMUX_DETACH_GONE  the client left just before the detach, which tmux then fails
#                          with "no current client"
#   TEST_TMUX_END_ON_DETACH  the session ends at the detach, its client still flagged
#   TEST_TMUX_RACE_ATTACH  another terminal attaches just before ours does
# attach-session models the hook: if list-clients would show any client on the session,
# the arriving terminal is detached again (tmux still exits 0); otherwise this terminal is
# on it until the user detaches, which here is at once. Which happened is $STATE/attached.
# An unknown call is an error, so a new tmux call in the code under test shows up as a
# failure rather than passing by accident.
cat >"$BIN/tmux" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = "-L" ] && shift 2
printf '%s\n' "$*" >>"$TEST_LOG"
target=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
    case "${args[i]}" in -t | -s) target="${args[i + 1]#=}" ;; esac
done
has_session() { awk -v want="$1" '$1 == want { found = 1 } END { exit found ? 0 : 1 }' "$TEST_STATE/sessions"; }
clients_of() { if [ -f "$TEST_STATE/clients/$1" ]; then cat "$TEST_STATE/clients/$1"; fi; }
case "$1" in
list-sessions)
    while read -r name dir; do
        [ -n "$name" ] || continue
        printf '%s %s %s\n' "$name" "$(clients_of "$name" | awk 'NF && $3 == ""' | wc -l)" "$dir"
    done <"$TEST_STATE/sessions"
    ;;
list-panes) ;;
list-clients)
    if ! has_session "$target"; then
        echo "can't find session: $target" >&2
        exit 1
    fi
    clients_of "$target" | awk '{ print $1, $2 }'
    linger="$(cat "$TEST_STATE/linger")"
    if [ -z "${TEST_TMUX_STICKY:-}" ] && [ -f "$TEST_STATE/clients/$target" ] &&
        grep -q ' exiting$' "$TEST_STATE/clients/$target"; then
        if [ "$linger" -gt 0 ]; then
            printf '%s\n' "$((linger - 1))" >"$TEST_STATE/linger"
        else
            awk '$3 != "exiting"' "$TEST_STATE/clients/$target" >"$TEST_STATE/clients/$target.new"
            mv "$TEST_STATE/clients/$target.new" "$TEST_STATE/clients/$target"
        fi
    fi
    ;;
detach-client)
    if [ -n "${TEST_TMUX_DETACH_FAIL:-}" ]; then
        echo "server exited unexpectedly" >&2
        exit 1
    fi
    if [ -n "${TEST_TMUX_DETACH_GONE:-}" ]; then
        : >"$TEST_STATE/clients/$target"
        echo "no current client" >&2
        exit 1
    fi
    awk 'NF { print $1, $2, "exiting" }' "$TEST_STATE/clients/$target" >"$TEST_STATE/clients/$target.new"
    mv "$TEST_STATE/clients/$target.new" "$TEST_STATE/clients/$target"
    if [ -n "${TEST_TMUX_END_ON_DETACH:-}" ]; then
        awk -v gone="$target" '$1 != gone' "$TEST_STATE/sessions" >"$TEST_STATE/sessions.new"
        mv "$TEST_STATE/sessions.new" "$TEST_STATE/sessions"
    fi
    if [ -z "${TEST_TMUX_STICKY:-}" ] && [ "$(cat "$TEST_STATE/linger")" -eq 0 ]; then
        : >"$TEST_STATE/clients/$target"
    fi
    ;;
attach-session)
    if [ -n "${TEST_TMUX_RACE_ATTACH:-}" ]; then
        printf '/dev/pts/9 %s\n' "$(date +%s)" >>"$TEST_STATE/clients/$target"
    fi
    if [ -n "$(clients_of "$target" | awk 'NF')" ]; then
        printf 'kicked\n' >"$TEST_STATE/attached"
    else
        printf 'yes\n' >"$TEST_STATE/attached"
    fi
    ;;
set-hook) ;;
# The session the asking process runs in, which tmux finds from the TMUX the process carries.
display-message) printf '%s\n' "$TEST_TMUX_CURRENT" ;;
*)
    echo "fake tmux: unexpected call: $*" >&2
    exit 97
    ;;
esac
EOF

# fzf: answers from $TEST_ANSWERS, one line per call, "<key>|<choice>": <choice> is a session
# name (the row starting with it), YES (the "Yes, " row), EXIT (the Exit row) or ABORT (Esc).
# Each call's arguments are appended to $TEST_FZF_LOG, so a test can read the header shown.
cat >"$BIN/fzf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
rows="$(cat -)"
printf '%s\n' "$*" >>"$TEST_FZF_LOG"
n=$(($(cat "$TEST_STATE/fzf-calls") + 1))
printf '%s\n' "$n" >"$TEST_STATE/fzf-calls"
answer="$(awk -v n="$n" 'NR == n' "$TEST_ANSWERS")"
key="${answer%%|*}"
choice="${answer#*|}"
case "$choice" in
ABORT) exit 130 ;;
EXIT) row="Exit" ;;
YES) row="$(awk '/^Yes, / { print; exit }' <<<"$rows")" ;;
"") echo "fake fzf: no answer queued for call $n" >&2; exit 2 ;;
*) row="$(awk -v want="$choice" '$1 == want { print; exit }' <<<"$rows")" ;;
esac
if [[ "$*" == *--expect* ]]; then
    printf '%s\n' "$key"
fi
printf '%s\n' "$row"
EOF
chmod 755 "$BIN/tmux" "$BIN/fzf"

export TEST_LOG="$LOG" TEST_STATE="$STATE" TEST_FZF_LOG="$SCRATCH/fzf.log" TEST_ANSWERS="$SCRATCH/answers"
export PATH="$BIN:$PATH" CCY_CPU_SAMPLE_SECONDS=0

NOW="$(date +%s)"
# reset <sessions...> — a fresh server: each argument "<name>[:<tty>]" is a session in the
# scratch directory, attached to <tty> (last active three hours ago) when one is given.
reset() {
    local spec name
    : >"$STATE/sessions"
    rm -f "$STATE/clients/"*
    : >"$LOG"
    : >"$TEST_FZF_LOG"
    printf '0\n' >"$STATE/fzf-calls"
    printf '0\n' >"$STATE/linger"
    printf 'no\n' >"$STATE/attached"
    for spec in "$@"; do
        name="${spec%%:*}"
        printf '%s %s\n' "$name" "$SCRATCH" >>"$STATE/sessions"
        if [[ "$spec" == *:* ]]; then
            printf '%s %s\n' "${spec#*:}" "$((NOW - 3 * 3600 - 120))" >"$STATE/clients/$name"
        fi
    done
}
# detaches — how many detach-client calls were made. Anchored: the single-attach hook that
# set-hook installs carries the words detach-client too.
detaches() { grep -c '^detach-client' "$LOG"; }

# ── the library, sourced ──────────────────────────────────────────────────────────────
# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/tmux-session.bash"
# How long a detached terminal is waited for, in tenths of a second: short, for the case
# where it never lets go.
export CCY_TMUX_TAKE_OVER_WAIT_TENTHS=3

for fn in ccy_tmux_idle_words ccy_tmux_client_words ccy_tmux_other_terminals ccy_tmux_take_over; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the libraries" >&2
        exit 1
    fi
done

echo "== how long a terminal has been idle, in one word a person reads"
check "seconds" "idle 40 s" "$(ccy_tmux_idle_words 40)"
check "minutes, rounded down" "idle 12 min" "$(ccy_tmux_idle_words 779)"
check "hours, rounded down" "idle 3 h" "$(ccy_tmux_idle_words $((3 * 3600 + 3599)))"
check "days" "idle 2 d" "$(ccy_tmux_idle_words $((2 * 86400 + 5)))"
check "a clock that moved backwards reads as just now, not negative" "idle 0 s" "$(ccy_tmux_idle_words -5)"

echo "== the other terminals, named"
check "one terminal" "/dev/pts/3 (idle 3 h)" \
    "$(ccy_tmux_client_words "/dev/pts/3 1000" $((1000 + 3 * 3600)))"
check "two terminals" "/dev/pts/3 (idle 3 h), /dev/pts/7 (idle 2 min)" \
    "$(ccy_tmux_client_words $'/dev/pts/3 1000\n/dev/pts/7 11680' $((1000 + 3 * 3600)))"

reset "ccy-a:/dev/pts/3"
check "asked of tmux for a live session" "/dev/pts/3 (idle 3 h)" "$(ccy_tmux_other_terminals ccy-a)"
reset
err="$(ccy_tmux_other_terminals ccy-gone 2>&1)"
check "a session that has ended is return 2, not a failure" "2" "$?"
check "and says nothing about it: the caller words that" "" "$err"

echo "== take over: the other terminal is detached, this one attached, nothing killed"
reset "ccy-a:/dev/pts/3"
err="$(ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "taking over an open session succeeds" "0" "$?"
check "the other terminal is detached, then this one attaches" "detach-client attach-session" \
    "$(awk '$1 == "detach-client" || $1 == "attach-session" { print $1 }' "$LOG" | paste -sd' ' -)"
check "this terminal is the one left on the session" "yes" "$(cat "$STATE/attached")"
check "the detach names the session exactly" "1" "$(grep -cx 'detach-client -s =ccy-a' "$LOG")"
check "no session is killed" "0" "$(grep -c 'kill' "$LOG")"
check_has "it says which terminal it detached" "/dev/pts/3" "$err"

echo "== take over: a session that ended meanwhile is reported, not attached"
reset
err="$(ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "return 2" "2" "$?"
check "nothing is detached or attached" "0 0" "$(detaches) $(grep -c '^attach-session' "$LOG")"
check_has "it says the session has ended" "has ended" "$err"

echo "== take over: tmux refusing the detach stops it"
reset "ccy-a:/dev/pts/3"
err="$(TEST_TMUX_DETACH_FAIL=1 ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "return 1" "1" "$?"
check "nothing is attached" "0" "$(grep -c 'attach-session' "$LOG")"
check_has "tmux's own reason is shown" "server exited unexpectedly" "$err"

echo "== take over: a terminal that does not let go stops it"
reset "ccy-a:/dev/pts/3"
err="$(TEST_TMUX_STICKY=1 ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "return 1" "1" "$?"
check "nothing is attached" "0" "$(grep -c 'attach-session' "$LOG")"
check_has "it names the terminal that held on" "/dev/pts/3" "$err"

echo "== take over: a session nobody holds is simply attached"
reset "ccy-a"
err="$(ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "return 0" "0" "$?"
check "no detach is sent" "0" "$(detaches)"
check "it attaches" "1" "$(grep -cx 'attach-session -t =ccy-a' "$LOG")"

# The wait must use the hook's own view. A client told to leave drops out of
# #{session_attached} at once but stays in list-clients until it has gone; attaching in
# between is kicked straight back off by the hook.
echo "== take over: it waits until the old terminal is gone from list-clients, the hook's view"
reset "ccy-a:/dev/pts/3"
printf '3\n' >"$STATE/linger"
err="$(CCY_TMUX_TAKE_OVER_WAIT_TENTHS=20 ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "return 0" "0" "$?"
check "this terminal is the one left on the session, not kicked by the hook" "yes" "$(cat "$STATE/attached")"

echo "== take over: a terminal flagged leaving that never goes stops it"
reset "ccy-a:/dev/pts/3"
err="$(TEST_TMUX_STICKY=1 ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "return 1" "1" "$?"
check "nothing is attached" "0" "$(grep -c '^attach-session' "$LOG")"

echo "== take over: the old terminal left just before the detach"
reset "ccy-a:/dev/pts/3"
err="$(TEST_TMUX_DETACH_GONE=1 ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "the failed detach is not a failure: the session is free, so return 0" "0" "$?"
check "this terminal is attached" "yes" "$(cat "$STATE/attached")"

echo "== take over: a session that ends during the wait is reported at once"
reset "ccy-a:/dev/pts/3"
start="${EPOCHREALTIME/[.,]/}"
err="$(TEST_TMUX_STICKY=1 TEST_TMUX_END_ON_DETACH=1 CCY_TMUX_TAKE_OVER_WAIT_TENTHS=50 \
    ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
rc=$?
elapsed_ms=$(((${EPOCHREALTIME/[.,]/} - start) / 1000))
check "return 2" "2" "$rc"
check_has "it says the session ended" "ended" "$err"
check "it does not sit out the five-second wait" "yes" "$([ "$elapsed_ms" -lt 2000 ] && echo yes || echo "no: ${elapsed_ms} ms")"
check "nothing is attached" "0" "$(grep -c '^attach-session' "$LOG")"

echo "== attach: kicked by the hook is not success"
reset "ccy-a"
err="$(TEST_TMUX_RACE_ATTACH=1 ccy_tmux_attach ccy-a 2>&1 >/dev/null)"
check "ccy_tmux_attach returns 3 when this terminal was not left attached" "3" "$?"
check_has "and says so" "was not attached" "$err"
reset "ccy-a:/dev/pts/3"
err="$(TEST_TMUX_RACE_ATTACH=1 ccy_tmux_take_over ccy-a 2>&1 >/dev/null)"
check "a take-over whose attach was kicked returns 3, not 0" "3" "$?"

echo "== which CCY session this process runs in (where a launcher records its SSH key)"
reset "ccy-a"
check "inside CCY's server: the session's name" "ccy-a" \
    "$(TMUX="/tmp/tmux-1000/$CCY_TMUX_SOCKET,123,0" TEST_TMUX_CURRENT=ccy-a ccy_tmux_current_session)"
reset "ccy-a"
check "inside a user's own tmux: nothing, and tmux is not asked" ":0" \
    "$(TMUX="/tmp/tmux-1000/default,123,0" TEST_TMUX_CURRENT=ccy-a ccy_tmux_current_session):$(grep -c '^display-message' "$LOG")"
check "outside tmux: nothing" "" "$(env -u TMUX TEST_TMUX_CURRENT=ccy-a bash -c "$(printf 'source %q; source %q; ccy_tmux_current_session' "$LIB_DIR/common-pure.bash" "$LIB_DIR/tmux-session.bash")")"

# ── the picker: ccy-sessions under a terminal ─────────────────────────────────────────
# run_picker <answers...> — run the real ccy-sessions in the scratch directory with the
# fake fzf answering in order; prints its output (\r stripped), then "rc=<status>".
run_picker() {
    printf '%s\n' "$@" >"$TEST_ANSWERS"
    (cd "$SCRATCH" && CCY_LIB="$LIB_DIR" script -qec "$(printf '%q' "$TOOL")" /dev/null </dev/null 2>&1; echo "rc=$?") |
        tr -d '\r'
}
rc_of() { awk '/^rc=/ { rc = $0 } END { print rc }' <<<"$1"; }

echo "== ccy-sessions: the header offers Ctrl-T"
reset "ccy-a:/dev/pts/3"
out="$(run_picker "|ABORT")"
check "Esc leaves" "rc=0" "$(rc_of "$out")"
check_has "the key legend names Ctrl-T" "Ctrl-T take over" "$(cat "$TEST_FZF_LOG")"
check_has "the picker listens for ctrl-t" "ctrl-t" "$(cat "$TEST_FZF_LOG")"

echo "== ccy-sessions: Ctrl-T on an open-elsewhere session, confirmed"
reset "ccy-a:/dev/pts/3" "ccy-b"
out="$(run_picker "ctrl-t|ccy-a" "|YES")"
check "it exits 0 after attaching" "rc=0" "$(rc_of "$out")"
confirm="$(cat "$TEST_FZF_LOG")"
check_has "the question names the other terminal" "/dev/pts/3 (idle 3 h)" "$confirm"
check_has "the question says the session keeps running" "keeps running" "$confirm"
check "the other terminal is detached" "1" "$(grep -cx 'detach-client -s =ccy-a' "$LOG")"
check "this terminal is attached" "1" "$(grep -cx 'attach-session -t =ccy-a' "$LOG")"
check "no session is killed" "0" "$(grep -c 'kill' "$LOG")"

echo "== ccy-sessions: Ctrl-T declined changes nothing"
reset "ccy-a:/dev/pts/3"
out="$(run_picker "ctrl-t|ccy-a" "|EXIT" "|ABORT")"
check "it returns to the list, then Esc leaves" "rc=0" "$(rc_of "$out")"
check "the list was shown again" "3" "$(cat "$STATE/fzf-calls")"
check "nothing is detached" "0" "$(detaches)"
check "nothing is attached" "0" "$(grep -c 'attach-session' "$LOG")"

echo "== ccy-sessions: Enter on an open-elsewhere session points at Ctrl-T"
reset "ccy-a:/dev/pts/3"
out="$(run_picker "|ccy-a" "|ABORT")"
check_has "the refusal names Ctrl-T" "Ctrl-T takes it over" "$out"
check "nothing is attached" "0" "$(grep -c 'attach-session' "$LOG")"

echo "== ccy-sessions: Ctrl-T on a detached session just attaches it"
reset "ccy-b"
out="$(run_picker "ctrl-t|ccy-b")"
check "it exits 0 after attaching" "rc=0" "$(rc_of "$out")"
check "no question is asked" "1" "$(cat "$STATE/fzf-calls")"
check "no detach is sent" "0" "$(detaches)"
check "it attaches" "1" "$(grep -cx 'attach-session -t =ccy-b' "$LOG")"

# A client tmux leaves out of #{session_attached} (suspended, or told to leave and stuck) makes
# the row read "detached" while the hook still counts it, so Enter would be kicked off again
# every time. Ctrl-T asks list-clients, finds it, and takes the session over.
echo "== ccy-sessions: Ctrl-T on a row that reads detached but has a client the hook counts"
reset "ccy-c:/dev/pts/4"
printf '/dev/pts/4 %s suspended\n' "$((NOW - 600))" >"$STATE/clients/ccy-c"
out="$(run_picker "ctrl-t|ccy-c" "|YES")"
check "it exits 0 after attaching" "rc=0" "$(rc_of "$out")"
check_has "the question names that terminal" "/dev/pts/4 (idle 10 min)" "$(cat "$TEST_FZF_LOG")"
check "the session is detached from it" "1" "$(detaches)"
check "this terminal is the one left on the session" "yes" "$(cat "$STATE/attached")"

echo "== ccy-sessions: after three failed tries, the last word is true"
reset "ccy-a:/dev/pts/3"
out="$(run_picker "|ccy-a" "|ccy-a" "|ccy-a")"
check "it exits 1" "rc=1" "$(rc_of "$out")"
check_has "nothing was tried, so it says nothing changed" "Nothing changed" "$out"
reset "ccy-a:/dev/pts/3" "ccy-b:/dev/pts/4" "ccy-c:/dev/pts/5" "ccy-d:/dev/pts/6"
out="$(TEST_TMUX_STICKY=1 TEST_TMUX_END_ON_DETACH=1 run_picker \
    "ctrl-t|ccy-a" "|YES" "ctrl-t|ccy-b" "|YES" "ctrl-t|ccy-c" "|YES")"
check "it exits 1" "rc=1" "$(rc_of "$out")"
check "three take-overs detached three terminals" "3" "$(detaches)"
check "so it does not claim nothing changed" "no" "$([[ "$out" == *"Nothing changed"* ]] && echo yes || echo no)"
check_has "it names the sessions a take-over was tried on" "A take-over was tried on ccy-a ccy-b ccy-c" "$out"

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
