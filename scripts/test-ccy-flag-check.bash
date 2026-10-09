#!/usr/bin/env bash
# Unit-test which arguments ccy's fail-fast flag check judges (Plan 00161, CCY 3.88.2).
#
# WHY THIS TEST EXISTS. ccy checks every `--flag` meant for claude against `claude --help`
# before any container work. Its comment said arguments after `--` are forwarded raw and
# skipped, but they went into the same array and were judged too. In Plan 00161's U20 run,
# the third of three back-to-back headless launches was refused for `--input-format
# --verbose --model`, all after `--` and all accepted for the two launches before it: the
# host's `claude --help` answered with something that was not the help. Only arguments
# before `--` are judged now, and help text with no usage line counts as no help.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

if [ ! -f "$LAUNCHER" ]; then
    echo "FAIL: launcher not found at $LAUNCHER" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The launcher's own loop, from `suspect_flags=()` to the `done` that closes it.
SNIPPET="$work/suspect-flags.bash"
awk '/^suspect_flags=\(\)$/ {on=1} on {print} on && /^done$/ {exit}' "$LAUNCHER" >"$SNIPPET"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

# judged <before-separator-count|""> <args...> — the flags the loop would judge.
judged() {
    local CLAUDE_ARGS_BEFORE_SEPARATOR="$1"
    shift
    local -a CLAUDE_ARGS=("$@")
    local -a suspect_flags=()
    # A count past the end of the arguments is a broken case, not a launcher answer.
    [ "${CLAUDE_ARGS_BEFORE_SEPARATOR:-0}" -le "${#CLAUDE_ARGS[@]}" ] || return 1
    # shellcheck source=/dev/null
    source "$SNIPPET"
    printf '%s' "${suspect_flags[*]}"
}

echo "=== which arguments are judged ==="
check "the loop was found in the launcher" "yes" "$(if grep -q 'suspect_flags+=' "$SNIPPET"; then echo yes; else echo no; fi)"
check "with no --, every flag is judged" "--foo --model" "$(judged "" --foo --model haiku)"
check "flags after -- are not judged" "--foo" "$(judged 1 --foo --input-format stream-json --verbose --model haiku)"
check "a -- with nothing before it judges nothing" "" "$(judged 0 --input-format stream-json --verbose)"
check "a --flag=value is judged by its name" "--bar" "$(judged "" --bar=1)"

echo "=== help text with no usage line is no help ==="
check "the host's and the image's help both need a ^Usage: line" "2" \
    "$(grep -c -F "grep -q '^Usage:' <<< \"\$claude_help\" || claude_help=\"\"" "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
