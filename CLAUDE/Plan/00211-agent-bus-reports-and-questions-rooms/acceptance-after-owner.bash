#!/usr/bin/env bash
# Plan 00211 — acceptance-after-owner.bash: acceptance.bash --after-owner (A5), as a
# meta-deploy.list entry, which passes no arguments to a script it runs. Queue it only once the
# owner has done the Element steps of ACCEPTANCE-owner.md. Everything is in acceptance.bash.
set -euo pipefail
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)/acceptance.bash" --after-owner "$@"
