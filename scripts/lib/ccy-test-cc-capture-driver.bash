#!/usr/bin/env bash
# Driver for scripts/test-ccy-launch-args-capture.bash: runs cc's capture lines, read out of cc
# into <capture file>, with [args...] as cc's argv, then prints the CC_ARGS they built as the
# count and each argument bracketed. The builder they call is whatever is first on PATH.
# Usage: ccy-test-cc-capture-driver.bash <capture file> [args...]
set -uo pipefail
capture="$1"
shift
# shellcheck source=/dev/null
source "$capture"
printf '%d:' "${#CC_ARGS[@]}"
printf '[%s]' "${CC_ARGS[@]}"
