#!/usr/bin/env bash
# Plan 00211 — deploy-stage-2.bash: deploy.bash --stage 2, as a meta-deploy.list entry, which
# passes no arguments to a script it runs. Everything is in deploy.bash.
set -euo pipefail
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/deploy.bash" --stage 2 "$@"
