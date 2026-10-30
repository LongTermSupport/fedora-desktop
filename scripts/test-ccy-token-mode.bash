#!/usr/bin/env bash
# Unit-test select_token's per-mode answer to "no usable token" (Plan 00048, CCY 3.50.0).
#
# Sources the libraries from THIS repo (not the deployed /var/local copy) so a fix
# can be verified before running the playbook.
#
# WHY THIS TEST EXISTS. select_token is called by BOTH launchers — ccy passes
# "container", the host cc wrapper passes "host" — and the two modes must answer
# an unusable pool differently: ccy hard-stops, cc offers the Desktop account.
# A bug shipped because one branch could not tell "the pool is empty" (where
# falling through to Desktop is the DESIGN) from "the pool has tokens, all past
# their expiry stamp" (where falling through silently authenticates the user as a
# DIFFERENT Claude account, with no error, no non-zero exit and no prompt).
#
# The expiry stamp makes that distinction load-bearing rather than academic: it is
# a flat +90 days written into the filename at creation, because `claude
# setup-token` does not report real expiry, and is_token_valid counts "expires
# today" as expired. So a token that still authenticates perfectly drops out of
# the valid set on day 90 — and on the day this was found, an entire pool had.
#
# Most cases return before select_token reaches its `read -p` menu. The menu
# cases drive it through piped stdin under a hard timeout, which is how the
# unbounded-loop regression (CCY 3.51.0) is gated — a hang must fail the gate,
# not stall CI. Only the FILENAME is ever read (is_token_valid parses the date
# out of it and never opens the file), so the fixtures hold a placeholder string
# and no real token is involved.
#
# COVERAGE IS NOT TOTAL: 9 of select_token's 12 return points. The three left
# out need a real `claude` binary (container create/renew) or a billed API call
# (the post-usage-fetch path). The summary line restates this, because a green
# run must not read as "all of this code is good".
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the
# full picture, and each result is checked explicitly. (Same reason, same shape as
# scripts/test-ccy-rootless-guard.bash.)
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
PURE_LIB="$LIB_DIR/common-pure.bash"
TOKEN_LIB="$LIB_DIR/token-management.bash"

for lib in "$PURE_LIB" "$TOKEN_LIB"; do
    if [ ! -f "$lib" ]; then
        echo "FAIL: library not found at $lib" >&2
        exit 1
    fi
done

# The container-mode create/renew paths reference these; cc pre-exports the same
# empty stubs. Set here so `set -u` inside the library cannot trip on them.
: "${GH_TOKEN:=}" "${IMAGE_NAME:=}"

# source-path makes the relative source= resolve from THIS script's directory rather
# than the caller's cwd — without it shellcheck -x reports SC1091 "does not exist".
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$PURE_LIB"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/token-management.bash
source "$TOKEN_LIB"

for fn in select_token is_token_valid; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the libraries" >&2
        echo "      (the function is absent, not merely broken)" >&2
        exit 1
    fi
done

# Fixtures live under the repo's own gitignored scratch area rather than /tmp: the
# tree is tracked (untracked/.gitignore ignores its contents), so it exists in a
# fresh clone, and nothing is written outside the repository.
# mktemp, not "$$": this repo is bind-mounted into containers, and a PID in one
# namespace collides with the same PID in another, so two concurrent runs could
# share a fixture dir and delete each other's on exit.
WORK="$(mktemp -d "$REPO_ROOT/untracked/ccy-token-mode-fixtures.XXXXXX")"
BANNERS="$WORK/banners.log"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

mkdir -p "$WORK/empty-pool" "$WORK/expired-only" "$WORK/has-valid"
: >"$BANNERS"

# Relative dates count from noon today: "+N days" from just after midnight lands on the day
# before when the span crosses the clocks going back (a 25-hour day).
TODAY="$(date +%Y-%m-%d)"
YESTERDAY="$(date -d "$TODAY 12:00 1 day ago" +%Y-%m-%d)"
NEXT_YEAR="$(date -d "$TODAY 12:00 365 days" +%Y-%m-%d)"
printf 'placeholder-not-a-real-token\n' >"$WORK/expired-only/stale.$YESTERDAY.token"
printf 'placeholder-not-a-real-token\n' >"$WORK/has-valid/fresh.$NEXT_YEAR.token"

PASSED=0
FAILED=0

# check <description> <token-dir> <mode> <expected-rc> <expected: EMPTY|NONEMPTY>
check() {
    local desc="$1" dir="$2" mode="$3" wantRc="$4" wantSel="$5"
    local rc=0 selState="EMPTY"
    SELECTED_TOKEN=""
    # The banners are the human-facing payload, not the thing under test, so they
    # are captured to a log that stays readable rather than discarded.
    printf '\n--- %s (mode=%s) ---\n' "$desc" "$mode" >>"$BANNERS"
    select_token "$dir" "$mode" >>"$BANNERS" 2>&1 || rc=$?
    [ -n "$SELECTED_TOKEN" ] && selState="NONEMPTY"

    if [ "$rc" -eq "$wantRc" ] && [ "$selState" = "$wantSel" ]; then
        printf '  PASS  %-54s -> rc=%s sel=%s\n' "$desc" "$rc" "$selState"
        PASSED=$((PASSED + 1))
    else
        printf '  FAIL  %-54s -> rc=%s (want %s) sel=%s (want %s)\n' \
            "$desc" "$rc" "$wantRc" "$selState" "$wantSel"
        printf '        banners for this case are in %s\n' "$BANNERS"
        FAILED=$((FAILED + 1))
    fi
}

echo ""
echo "=== container mode: ccy must NEVER launch without a named token ==="
# All three are non-zero, so claude-yolo's caller reports "No Valid Tokens
# Available" and exits rather than starting a session with no credential.
# 1 = usage error (the dir is not there at all); 3 = the pool yielded nothing.
check "missing token dir"   "$WORK/does-not-exist" container 1 EMPTY
check "empty pool"          "$WORK/empty-pool"     container 3 EMPTY
check "expired tokens only" "$WORK/expired-only"   container 3 EMPTY

echo ""
echo "=== host mode: the DESIGNED Desktop short-circuit ==="
# A genuinely empty pool is the one case where falling through to the host
# account is intended — there is nothing else cc could offer.
check "missing token dir -> Desktop" "$WORK/does-not-exist" host 0 EMPTY
check "empty pool -> Desktop"        "$WORK/empty-pool"     host 0 EMPTY

echo ""
echo "=== host mode: THE case this test exists for ==="
# Tokens are PRESENT but none passed the guessed stamp. Returning 0 here is the
# bug: cc reads the empty SELECTED_TOKEN as "Desktop" and silently authenticates
# as a different account. It must refuse instead.
check "expired tokens only -> refuse" "$WORK/expired-only" host 3 EMPTY

echo ""
echo "=== the menu must terminate: EOF and bounded retries ==="
# These are the ONLY cases that reach the `read -p` menu, so they run in a
# subshell with piped stdin and a hard timeout — a regression here hangs, and a
# hung gate must fail rather than stall CI.
#
# WHY: both launchers call select_token in a condition context (`if !` in cc,
# `||` in ccy), and bash suppresses errexit for the whole dynamic extent of such
# a command. `read` failing on EOF was therefore NOT fatal, the empty-selection
# branch looped back to `read`, and the menu spun for ever at ~250k lines/second.
# A human pressing Ctrl-D got a hang instead of an exit.
# Two INDEPENDENT defences make the menu terminate, and asserting only the exit
# status cannot tell them apart: the retry cap alone ends an EOF run too, so a
# test checking just rc=2 still passes with the EOF check deleted (measured).
# Each case therefore also asserts the message naming the mechanism that fired.
menu_case() {
    local desc="$1" input="$2" wantRc="$3" wantMsg="$4" rc=0 out=""
    out="$(printf '%s' "$input" | timeout 10 bash -c "
        set -uo pipefail
        : \"\${GH_TOKEN:=}\" \"\${IMAGE_NAME:=}\"
        source '$PURE_LIB'
        source '$TOKEN_LIB'
        select_token '$WORK/has-valid' host
    " 2>&1)" || rc=$?
    printf '\n--- %s ---\n%s\n' "$desc" "$out" >>"$BANNERS"

    if [ "$rc" -eq 124 ]; then
        printf '  FAIL  %-54s -> TIMED OUT (unbounded loop)\n' "$desc"
        FAILED=$((FAILED + 1))
    elif [ "$rc" -ne "$wantRc" ]; then
        printf '  FAIL  %-54s -> rc=%s (want %s)\n' "$desc" "$rc" "$wantRc"
        printf '        output for this case is in %s\n' "$BANNERS"
        FAILED=$((FAILED + 1))
    elif ! grep -qF "$wantMsg" <<<"$out"; then
        printf '  FAIL  %-54s -> rc=%s but never said "%s"\n' "$desc" "$rc" "$wantMsg"
        printf '        the OTHER defence ended this run; this one is not working\n'
        FAILED=$((FAILED + 1))
    else
        printf '  PASS  %-54s -> rc=%s\n' "$desc" "$rc"
        PASSED=$((PASSED + 1))
    fi
}

menu_case "EOF at the prompt -> cancelled"    ''                    2 'Cancelled'
menu_case "runaway invalid input -> gives up" $'9\n9\n9\n9\n9\n9\n' 2 'Giving up'
menu_case "valid pick -> selects"             $'1\n'                0 'Selected token'
menu_case "valid pick -> names its expiry"    $'1\n'                0 'Selected token: fresh (expires: '
# The `d` keypress is the whole reason the hard stop is acceptable: it is how a
# human reaches the Desktop account deliberately when the pool is usable. If it
# regressed, cc would have no route to Desktop at all.
menu_case "Desktop keypress -> host OAuth"    $'d\n'                0 'Using Desktop'

echo ""
echo "=== container mode: a token with no usage left is refused (lib/token-usable.bash) ==="
# ccy-token-usable is stubbed: STUB_USED_UP names the token it calls used up, STUB_EXIT forces
# its exit. Its own reading of the usage is the Go command's, tested in internal/ccylaunch.
# Piped stdin makes ccy_nobody_to_ask true; the menu runs only where a person answers, so a
# case stands for that person by redefining it, unless it tests the nobody-there stop.
USABLE_LIB="$LIB_DIR/token-usable.bash"
mkdir -p "$WORK/two-valid" "$WORK/bin"
printf 'placeholder-not-a-real-token\n' >"$WORK/two-valid/alpha.$NEXT_YEAR.token"
printf 'placeholder-not-a-real-token\n' >"$WORK/two-valid/beta.$NEXT_YEAR.token"
cat >"$WORK/bin/ccy-token-usable" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$STUB_CALLS"
mark=false
[ "$1" = --mark ] && mark=true && shift
[ "$1" = -- ] && shift
name="${1##*/}" && name="${name%%.*}"
[ -z "${STUB_EXIT:-}" ] || exit "$STUB_EXIT"
[ "$name" = "${STUB_USED_UP:-}" ] || exit 0
if [ "$mark" = true ]; then
    echo "   ✗ no usage left: STUB-WINDOW"
    exit 0
fi
echo "✗ No usage left on token $name: STUB-WINDOW" >&2
exit 3
STUB
chmod +x "$WORK/bin/ccy-token-usable"

# usable_case <description> <path-prefix> <mode> <attended: yes|no> <stdin> <want-rc> <env> <want-text>...
usable_case() {
    local desc="$1" prefix="$2" mode="$3" attended="$4" input="$5" wantRc="$6" env="$7" rc=0 out="" want
    shift 7
    : >"$WORK/calls"
    out="$(printf '%s' "$input" | env STUB_CALLS="$WORK/calls" "$env" PATH="$prefix$PATH" timeout 10 bash -c "
        set -uo pipefail
        : \"\${GH_TOKEN:=}\" \"\${IMAGE_NAME:=}\"
        source '$PURE_LIB'
        source '$TOKEN_LIB'
        source '$USABLE_LIB'
        [ '$attended' = no ] || ccy_nobody_to_ask() { return 1; }
        select_token '$WORK/two-valid' $mode && echo \"SELECTED=\$SELECTED_TOKEN\"
    " 2>&1)" || rc=$?
    printf '\n--- %s ---\n%s\n' "$desc" "$out" >>"$BANNERS"
    if [ "$rc" -ne "$wantRc" ]; then
        printf '  FAIL  %-54s -> rc=%s (want %s)\n' "$desc" "$rc" "$wantRc"
        FAILED=$((FAILED + 1))
        return
    fi
    for want in "$@"; do
        if ! grep -qF -- "$want" <<<"$out$(cat "$WORK/calls")"; then
            printf '  FAIL  %-54s -> rc=%s but never said "%s"\n' "$desc" "$rc" "$want"
            FAILED=$((FAILED + 1))
            return
        fi
    done
    printf '  PASS  %-54s -> rc=%s\n' "$desc" "$rc"
    PASSED=$((PASSED + 1))
}

STUB_BIN="$WORK/bin:"
usable_case "used up -> refused, marked, another chosen" "$STUB_BIN" container yes $'1\n2\n' 0 STUB_USED_UP=alpha \
    "✗ No usage left on token alpha: STUB-WINDOW" "alpha   expires" "✗ no usage left: STUB-WINDOW" \
    "Selected token: beta" "SELECTED=$WORK/two-valid/beta.$NEXT_YEAR.token" "--mark -- $WORK/two-valid/alpha"
usable_case "usage left -> chosen at once" "$STUB_BIN" container yes $'1\n' 0 STUB_USED_UP=none \
    "Selected token: alpha" "-- $WORK/two-valid/alpha.$NEXT_YEAR.token"
usable_case "only used-up picks -> bounded, gives up" "$STUB_BIN" container yes $'1\n1\n1\n1\n' 2 STUB_USED_UP=alpha \
    "Giving up after 3 invalid selections"
usable_case "used up, nobody to choose -> stops" "$STUB_BIN" container no $'1\n' 1 STUB_USED_UP=alpha \
    "Nobody is here to choose another token"
usable_case "check fails -> stops, naming it" "$STUB_BIN" container yes $'1\n' 1 STUB_EXIT=2 \
    "ccy-token-usable failed (exit 2)"
usable_case "check not installed -> stops, naming the play" "" container yes $'1\n' 1 STUB_USED_UP=none \
    "ccy-token-usable is not installed" "run play-claude-yolo.yml"
usable_case "host mode (cc) never asks it" "$STUB_BIN" host yes $'1\n' 0 STUB_EXIT=2 \
    "Selected token: alpha"
if [ -s "$WORK/calls" ]; then
    echo "  FAIL  host mode ran ccy-token-usable: $(cat "$WORK/calls")"
    FAILED=$((FAILED + 1))
else
    echo "  PASS  host mode ran no ccy-token-usable"
    PASSED=$((PASSED + 1))
fi

echo ""
echo "=== usage errors ==="
bad_mode_rc=0
SELECTED_TOKEN=""
select_token "$WORK/has-valid" nonsense >>"$BANNERS" 2>&1 || bad_mode_rc=$?
if [ "$bad_mode_rc" -eq 1 ]; then
    echo "  PASS  an invalid mode is a usage error (rc=1), not a verdict on the pool"
    PASSED=$((PASSED + 1))
else
    echo "  FAIL  invalid mode returned rc=$bad_mode_rc (want 1)"
    FAILED=$((FAILED + 1))
fi

echo ""
echo "=== validate_token: a check that could not run is never an invalid token ==="
# validate_token asks the real Go front (--check-token, ccylaunch F17), built here, which runs
# the check through a fake podman on PATH answering as the case says. The launcher reads 3
# alone as a refused token (recovery); any other failure is a check that did not happen.
export CHECK_DIR="$WORK/check"
mkdir -p "$CHECK_DIR/bin"
if ! front_build=$(cd "$REPO_ROOT/src-go" && go build -trimpath -buildvcs=false -o "$CHECK_DIR/claude-yolo" ./cmd/claude-yolo 2>&1); then
    echo "  FAIL  the Go front did not build: $front_build"
    FAILED=$((FAILED + 1))
fi
cat >"$CHECK_DIR/bin/podman" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$CHECK_DIR/argv"
printf '%s' "${CLAUDE_CODE_OAUTH_TOKEN:-}" >"$CHECK_DIR/env-token"
printf '%s' "$CHECK_STDOUT"
[ -z "$CHECK_STDERR" ] || printf '%s\n' "$CHECK_STDERR" >&2
exit "$CHECK_EXIT"
EOF
chmod +x "$CHECK_DIR/bin/podman"
CHECK_TOKEN="placeholder_not-a-real-token"
# check_case <desc> <want-rc> <stderr-word> <podman stdout> <podman stderr> <podman exit>
check_case() {
    local desc="$1" want="$2" word="$3" rc=0 err
    err=$(CHECK_STDOUT="$4" CHECK_STDERR="$5" CHECK_EXIT="$6" \
        PATH="$CHECK_DIR/bin:$PATH" SCRIPT_DIR="$CHECK_DIR" CONTAINER_ENGINE=podman \
        validate_token "$CHECK_TOKEN" claude-yolo:latest 2>&1 >/dev/null) || rc=$?
    if [ "$rc" = "$want" ] && [[ "$err" == *"$word"* ]] && ! grep -qF "$CHECK_TOKEN" "$CHECK_DIR/argv" &&
        [ "$(cat "$CHECK_DIR/env-token")" = "$CHECK_TOKEN" ]; then
        echo "  PASS  $desc (rc=$rc)"
        PASSED=$((PASSED + 1))
    else
        echo "  FAIL  $desc: rc=$rc (want $want), stderr '$err' (want '$word'), token by name only"
        FAILED=$((FAILED + 1))
    fi
}
check_case "a token the API accepts is 0" 0 "" "200" "" 0
check_case "a token the API refuses is 3, the recovery case" 3 "refused the token" "401" "" 0
check_case "an unreachable API is 1 with its cause, not a refusal" 1 "Could not resolve host" "" "curl: (6) Could not resolve host: api.anthropic.com" 6
check_case "a missing image is 1, said so" 1 "is not built" "" "Error: claude-yolo:latest: image not known" 125
check_case "an engine failure is 1 with the engine's words" 1 "cannot connect" "" "Error: cannot connect to Podman" 125

echo ""
echo "=== expiry-stamp boundary: 'expires today' counts as expired ==="
# is_token_valid's own contract, asserted directly because the host-mode branch
# above depends on it. A token stamped today is NOT valid, which is how a whole
# pool can lapse on a single day.
if is_token_valid "$WORK/expired-only/stale.$YESTERDAY.token"; then
    echo "  FAIL  a yesterday-stamped token was reported valid"
    FAILED=$((FAILED + 1))
else
    echo "  PASS  yesterday-stamped token is expired"
    PASSED=$((PASSED + 1))
fi
printf 'placeholder-not-a-real-token\n' >"$WORK/expired-only/today.$TODAY.token"
if is_token_valid "$WORK/expired-only/today.$TODAY.token"; then
    echo "  FAIL  a today-stamped token was reported valid (contract says expiring today = expired)"
    FAILED=$((FAILED + 1))
else
    echo "  PASS  today-stamped token is expired"
    PASSED=$((PASSED + 1))
fi
rm -f "$WORK/expired-only/today.$TODAY.token"
if is_token_valid "$WORK/has-valid/fresh.$NEXT_YEAR.token"; then
    echo "  PASS  future-stamped token is valid"
    PASSED=$((PASSED + 1))
else
    echo "  FAIL  a future-stamped token was reported expired"
    FAILED=$((FAILED + 1))
fi

echo ""
echo "=== expiry colours: red when unusable, yellow within two weeks, green beyond ==="
# The bands follow is_token_valid: a token stamped today is already refused, so it is
# red with the expired ones, not a "renew soon" yellow.
RED=$'\033[31m'
YELLOW=$'\033[33m'
GREEN=$'\033[32m'
RESET=$'\033[0m'
expiry_case() {
    local desc="$1" got="$2" want="$3"
    if [ "$got" = "$want" ]; then
        printf '  PASS  %s\n' "$desc"
        PASSED=$((PASSED + 1))
    else
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$desc" "$want" "$got"
        FAILED=$((FAILED + 1))
    fi
}
IN_14="$(date -d "$TODAY 12:00 14 days" +%Y-%m-%d)"
IN_15="$(date -d "$TODAY 12:00 15 days" +%Y-%m-%d)"
expiry_case "expired is red" "$(colorize_expiry "$YESTERDAY")" "${RED}${YESTERDAY}${RESET}"
expiry_case "expiring today is red (already refused)" "$(colorize_expiry "$TODAY")" "${RED}${TODAY}${RESET}"
expiry_case "14 days left is yellow" "$(colorize_expiry "$IN_14")" "${YELLOW}${IN_14}${RESET}"
expiry_case "15 days left is green" "$(colorize_expiry "$IN_15")" "${GREEN}${IN_15}${RESET}"
expiry_case "an unparseable date is printed plain" "$(colorize_expiry not-a-date)" "not-a-date"
expiry_case "a dated token file is labelled with its coloured expiry" \
    "$(token_expiry_label "$WORK/has-valid/fresh.$NEXT_YEAR.token")" " (expires: ${GREEN}${NEXT_YEAR}${RESET})"
expiry_case "an undated file gets no label" "$(token_expiry_label "$WORK/has-valid/undated.token")" ""

# A separate "discrimination check" re-invoking the empty-pool and expired-only
# calls used to sit here. It was TAUTOLOGICAL: those two fixtures are already
# asserted at rc=0 and rc=3 above, so while both pass it cannot fail, and when
# one breaks it fires in lockstep — two failure lines for one defect, and no
# independent signal. Removed rather than reworded; the count below is honest.

echo ""
echo "──────────────────────────────────────────────────────────────"
# COVERAGE, stated as a number rather than implied by the length of the list
# above. select_token has 12 return points; this suite exercises 9.
#
# NOT covered, and why — all three need something this container cannot give:
#   - container renew (r<N>) and container create (0) call create_token, which
#     needs a real `claude` binary and an interactive OAuth round trip
#   - the post-redraw-loop `return 1` is reachable only after a usage fetch,
#     which costs a billed API request against the allowance it reports
#
# A green run here means those three were NOT LOOKED AT, not that they are good.
printf 'coverage: 9 of 12 select_token return points (see header)\n'
printf 'passed: %d   failed: %d\n' "$PASSED" "$FAILED"

if [ "$PASSED" -eq 0 ]; then
    echo "ERROR: zero tests ran — discovery is broken, not the code clean" >&2
    exit 1
fi
if [ "$FAILED" -ne 0 ]; then
    exit 1
fi
echo "OK"
