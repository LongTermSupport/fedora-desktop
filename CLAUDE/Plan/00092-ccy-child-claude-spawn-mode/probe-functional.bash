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
#
# The NEUTRAL WORKING DIRECTORY is not optional either, and this probe found out the hard
# way. Run from /workspace the child loads the project harness — CLAUDE.md, the hooks daemon,
# session-start hooks, project skills — and on the first real run that context so dominated
# the prompt that the child replied about this repo's stop-hook rules and never produced the
# sentinel at all. Authentication had worked perfectly; the harness drowned the question.
#
# This probe's claim is "the credential reaches the child and the child answers". A neutral
# cwd is what keeps it testing that, instead of accidentally testing the project's own
# configuration. The trap itself is documented in the skill, because it bites callers too.
WORKDIR="$(mktemp -d)"
trap 'rm -rf "${WORKDIR}"' EXIT

OUT=""
if ! OUT="$(cd "${WORKDIR}" && "${WRAPPER_NAME}" -p "reply with exactly: ${SENTINEL}" \
    --model haiku </dev/null 2>&1)"; then
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
