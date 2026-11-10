#!/usr/bin/env bash
# A stand-in for one of the pre-push hook's gates, linked into a test's scratch scripts directory
# under the gate's name: it records its arguments and stdin beside itself and exits with
# STUB_EXIT_<name> (qa_private_names, qa_push_gate), 0 by default.
set -euo pipefail

name="$(basename "$0" .bash)"
dir="$(dirname "$0")"
printf '%s\n' "$*" >"$dir/$name.args"
cat >"$dir/$name.stdin"
var="STUB_EXIT_${name//-/_}"
exit "${!var:-0}"
