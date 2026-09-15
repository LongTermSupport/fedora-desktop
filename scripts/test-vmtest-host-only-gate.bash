#!/usr/bin/env bash
# Unit-test host_only_preflight (files/home/.local/bin/vmtest, Plan 00121 Task 2.2).
#
# Extracts the ONE function under test out of `vmtest` with awk into a temp file and sources
# that — `vmtest` calls main "$@" on load and would try to drive libvirt. The function is
# bounded by its `host_only_preflight() {` line and the first `^}` after it; a refactor that
# moves it keeps working, one that renames it fails loudly at extraction.
#
# WHY THIS EXISTS. host_only_preflight is the host-CLI gate on a scenario that puts a real
# GitHub PAT into a guest. Two other gates cover the bridge and are tested in
# tests/helpers/vmtest/test_bridge_run.py; this is the one a human types past, and the one
# that has to refuse a run which looks almost right — the wrong file mode, an empty token
# file, the scenario on both deployed enumerations at once.
#
# A permission check that only ever permits is this repo's cardinal defect, so most of what
# follows is refusals — with the acceptance case FIRST, so every refusal below is a change
# from a known-good baseline rather than an assertion about a gate that might refuse
# everything.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VMTEST="$REPO_ROOT/files/home/.local/bin/vmtest"

if [ ! -f "$VMTEST" ]; then
    echo "FAIL: vmtest not found at $VMTEST" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

awk '$0 == "host_only_preflight() {" {p=1} p {print} p && /^\}/ {exit}' "$VMTEST" > "$work/fn.bash"
if ! grep -q '^host_only_preflight() {' "$work/fn.bash"; then
    echo "FAIL: could not extract host_only_preflight from vmtest" >&2
    exit 1
fi

# `die` belongs to the CLI and EXITS. The stub must exit too: a stub that merely returned
# would let the function run on past its own refusal, so every later gate would be exercised
# in a state the real CLI can never reach — and a test built that way reports on code that
# does not exist.
#
# Exiting means the call has to happen in a subshell to stay observable, and a subshell
# cannot assign back to the parent. So the message travels through a file, written at the
# moment of refusal.
DIE_FILE="$work/die-message"
DIE_MESSAGE=""
die() {
    printf '%s' "$*" > "$DIE_FILE"
    exit 1
}

# run_gate — the function under test, with its exit status and its refusal message both
# recovered. Every case below goes through this rather than calling the function directly.
run_gate() {
    local rc=0
    : > "$DIE_FILE"
    ( host_only_preflight "$@" ) || rc=$?
    DIE_MESSAGE="$(cat "$DIE_FILE")"
    return "$rc"
}

# shellcheck source=/dev/null
source "$work/fn.bash"

if ! declare -F host_only_preflight >/dev/null; then
    echo "FAIL: host_only_preflight is not defined after sourcing the extract" >&2
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

SCENARIO="server-github-token"
# Distinctive values, so a leak into a refusal message is unmistakable rather than a
# substring of something innocent.
SECRET_VALUE="ghp_UNIQUEtokenVALUE0123456789"
PASSPHRASE_VALUE="correct-horse-battery-staple-UNIQUE"

ALLOWLIST="$work/scenarios.allowlist"
HOST_ONLY_LIST="$work/scenarios.host-only"

# reset — the state of a run that SHOULD be permitted. Each case spoils exactly one thing, so
# a refusal can only be attributed to what that case changed.
reset() {
    DIE_MESSAGE=""
    printf 'server-fast-provision\n' > "$ALLOWLIST"
    printf '%s\n' "$SCENARIO" > "$HOST_ONLY_LIST"
    TOKEN_FILE="$work/gh-token"
    SSH_PP_FILE="$work/ssh-pass"
    printf '%s\n' "$SECRET_VALUE" > "$TOKEN_FILE"
    printf '%s\n' "$PASSPHRASE_VALUE" > "$SSH_PP_FILE"
    chmod 600 "$TOKEN_FILE" "$SSH_PP_FILE"
    GITHUB_ACCOUNT="throwaway-lab-user"
    unset VMTEST_FROM_BRIDGE
}

# ---------------------------------------------------------------------------
# The acceptance case, first
# ---------------------------------------------------------------------------

reset
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a correctly prepared host-only run is permitted" "0" "$rc"
check "a permitted run says nothing" "" "$DIE_MESSAGE"

# ---------------------------------------------------------------------------
# The bridge must never reach this path
# ---------------------------------------------------------------------------

# bridge_run.py declares itself when it launches the CLI. This is the CLI-side half of a
# refusal the bridge already makes for itself, so neither is the only thing standing between
# a real credential and the shared mount.
reset
export VMTEST_FROM_BRIDGE=1
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a run launched by the bridge is refused" "1" "$rc"
case "$DIE_MESSAGE" in
    *bridge*) check "the refusal says it came from the bridge" "yes" "yes" ;;
    *) check "the refusal says it came from the bridge" "yes" "no: ${DIE_MESSAGE}" ;;
esac

# ---------------------------------------------------------------------------
# The two enumerations
# ---------------------------------------------------------------------------

reset
printf 'something-else\n' > "$HOST_ONLY_LIST"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a scenario absent from the host-only list is refused" "1" "$rc"

# Absent, not empty, is how the playbook leaves the file when no scenario is host-only. A
# missing file must not read as "no restriction".
reset
rm -f "$HOST_ONLY_LIST"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a missing host-only list is refused, not treated as permission" "1" "$rc"

# The two lists are disjoint by construction in scenarios.py. If they are not disjoint ON
# DISK then a deployed file has been hand-edited — and the file that would have been edited
# is the one keeping a PAT off the shared mount.
reset
printf 'server-fast-provision\n%s\n' "$SCENARIO" > "$ALLOWLIST"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a scenario on BOTH enumerations is refused" "1" "$rc"
case "$DIE_MESSAGE" in
    *both*) check "the refusal names the disjointness breach" "yes" "yes" ;;
    *) check "the refusal names the disjointness breach" "yes" "no: ${DIE_MESSAGE}" ;;
esac

# ---------------------------------------------------------------------------
# The credentials the run cannot proceed without
# ---------------------------------------------------------------------------

reset
GITHUB_ACCOUNT=""
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a missing --github-account is refused" "1" "$rc"

reset
TOKEN_FILE=""
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a missing --github-token-file is refused" "1" "$rc"

reset
SSH_PP_FILE=""
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a missing --github-ssh-passphrase-file is refused" "1" "$rc"

reset
TOKEN_FILE="$work/does-not-exist"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "an unreadable token file is refused" "1" "$rc"

# An empty secret file is the case the scrubber also refuses: a secret that is not there
# cannot be redacted from the transcript afterwards, and cannot be verified absent either.
reset
: > "$TOKEN_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "an empty token file is refused" "1" "$rc"

reset
: > "$SSH_PP_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "an empty passphrase file is refused" "1" "$rc"

# Mode matters on a multi-user host: a 0644 PAT is readable by every account on the box
# before this run even starts.
reset
chmod 644 "$TOKEN_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a group/world-readable token file is refused" "1" "$rc"

reset
chmod 640 "$SSH_PP_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a group-readable passphrase file is refused" "1" "$rc"

# The account name is interpolated into an SSH command line. The grammar is the guard.
reset
GITHUB_ACCOUNT='user; rm -rf ~'
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "an account name outside the grammar is refused" "1" "$rc"

# ---------------------------------------------------------------------------
# No refusal may echo a secret. These messages are read by a human and pasted into tickets.
# ---------------------------------------------------------------------------

leaked=0
did_not_refuse=0
for spoil in empty-token bad-mode from-bridge both-lists; do
    reset
    case "$spoil" in
        empty-token) : > "$TOKEN_FILE" ;;
        bad-mode) chmod 644 "$TOKEN_FILE" ;;
        from-bridge) export VMTEST_FROM_BRIDGE=1 ;;
        both-lists) printf '%s\n' "$SCENARIO" >> "$ALLOWLIST" ;;
    esac
    # A case that does NOT refuse produces no message, and "no message contains a secret"
    # would then be true for the emptiest of reasons. Counting it separately keeps this
    # check from passing vacuously.
    if run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"; then
        did_not_refuse=$((did_not_refuse + 1))
        printf '  note: the %s case did not refuse, so it has no message to inspect\n' "$spoil" >&2
    fi
    case "$DIE_MESSAGE" in
        *"$SECRET_VALUE"* | *"$PASSPHRASE_VALUE"*) leaked=$((leaked + 1)) ;;
    esac
done
check "every spoiled case actually refused" "0" "$did_not_refuse"
check "no refusal echoes a secret value" "0" "$leaked"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
