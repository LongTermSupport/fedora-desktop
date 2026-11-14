#!/usr/bin/env bash
# Unit-test scripts/secret-screen.bash, the shell's way to show a secret (rule
# secret-on-normal-screen, CLAUDE/QA.md): piped or redirected, what it reads is written out byte
# for byte (it is the payload a caller captures); on a terminal it is shown on the alternate
# screen, which keeps no scrollback, and the screen is left once Enter is pressed.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TOOL="$SCRIPT_DIR/secret-screen.bash"
if [ ! -x "$TOOL" ]; then
    echo "FAIL: $TOOL does not exist or is not executable" >&2
    exit 1
fi
if ! command -v script >/dev/null; then
    echo "FAIL: script (util-linux) is needed for the terminal cases" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "ok: $label"
    else
        failed=$((failed + 1))
        echo "FAIL: $label: want [$want], got [$got]"
    fi
}

# Piped: byte for byte, trailing newlines and a missing last newline kept.
for sample in $'one line\n' $'two\nlines\n\n' 'no newline' ''; do
    got="$(printf '%s' "$sample" | "$TOOL" | od -An -c)"
    check "piped output is the input byte for byte: $(printf '%q' "$sample")" "$(printf '%s' "$sample" | od -An -c)" "$got"
done

printf 'to a file\n' | "$TOOL" >"$WORK_DIR/redirected"
check "redirected to a file, nothing but the input" "to a file" "$(cat "$WORK_DIR/redirected")"

# On a terminal: script(1) gives the helper a pseudo-terminal as stdout and /dev/tty; the Enter
# comes from script's own stdin.
printf 'SAMPLE-VALUE\n' >"$WORK_DIR/value"
printf '\n' | script -q -e -c "\"$TOOL\" <\"$WORK_DIR/value\"" "$WORK_DIR/typescript" >/dev/null
status=$?
check "on a terminal, exit 0 once Enter is pressed" "0" "$status"
screen="$(cat "$WORK_DIR/typescript")"
on=$'\e[?1049h'
off=$'\e[?1049l'
shown="${screen#*"$on"}"
check "the alternate screen is entered" "yes" "$([ "$shown" != "$screen" ] && echo yes || echo no)"
inside="${shown%%"$off"*}"
check "the value is shown on the alternate screen" "yes" "$([[ $inside == *SAMPLE-VALUE* ]] && echo yes || echo no)"
check "the prompt says to press Enter" "yes" "$([[ $inside == *"press Enter"* ]] && echo yes || echo no)"
check "the alternate screen is left" "yes" "$([ "$inside" != "$shown" ] && echo yes || echo no)"
before="${screen%%"$on"*}"
after="${shown#*"$off"}"
check "nothing of the value reaches the normal screen" "no" "$([[ $before$after == *SAMPLE-VALUE* ]] && echo yes || echo no)"

# Input closed before Enter: the alternate screen is still left, and the run fails.
script -q -e -c "\"$TOOL\" <\"$WORK_DIR/value\"" "$WORK_DIR/typescript2" </dev/null >/dev/null
status=$?
check "no Enter (input closed): the run fails" "yes" "$([ "$status" -ne 0 ] && echo yes || echo no)"
check "no Enter (input closed): the alternate screen is still left" "yes" \
    "$([[ $(cat "$WORK_DIR/typescript2") == *"$off"* ]] && echo yes || echo no)"

echo ""
echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
