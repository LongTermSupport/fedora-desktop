#!/usr/bin/env bash
# probe-functional.bash — prove a child claude actually authenticates and answers.
#
# This is the one probe that COSTS: it makes a real API call on the session's own
# subscription. Everything else in this plan's acceptance run is free. It is worth the call,
# because every other probe checks that the token did not leak, and none of them checks that
# the feature does the thing it exists to do. A suite that only proves absence can pass while
# the capability is entirely broken.
#
# WHERE TO RUN: INSIDE the CCY container, with the mode enabled. Called as a leg by
# acceptance.bash, which enforces both.
set -euo pipefail

WRAPPER_NAME="ccy-claude"
SENTINEL="child-claude-probe-ok"

if ! command -v "${WRAPPER_NAME}" >/dev/null; then
    printf '[FAIL] %s is not on PATH, so the capability cannot be exercised.\n' "${WRAPPER_NAME}" >&2
    exit 1
fi

printf '== functional: a child claude authenticates and returns a completion\n'
printf '   (this leg spends one real API call on the session subscription)\n'

# `< /dev/null` is not optional. Without it the child waits on stdin for several seconds and
# then warns; inside a leg with no terminal it is the difference between a clean run and a
# confusing stall. The skill teaches the same thing.
OUT=""
if ! OUT="$("${WRAPPER_NAME}" -p "reply with exactly: ${SENTINEL}" --model haiku </dev/null 2>&1)"; then
    printf '[FAIL] the child exited non-zero. Output:\n%s\n' "${OUT}" >&2
    exit 1
fi

if [[ "${OUT}" == *"Not logged in"* ]]; then
    printf '[FAIL] the child was not authenticated — the token did not reach it:\n%s\n' "${OUT}" >&2
    exit 1
fi

if [[ "${OUT}" != *"${SENTINEL}"* ]]; then
    printf '[FAIL] the child answered, but not with the sentinel. Output:\n%s\n' "${OUT}" >&2
    exit 1
fi

printf 'the child authenticated and returned the expected answer\n'
