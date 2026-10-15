#!/usr/bin/env bash
# qa-nokill-containerwatch.bash — L0 no-kill safety gate for the container-watch watchdog
# (Plan 00055 Phase 5, Plan 00132 Phase 6).
#
# The watchdog must never signal a process, and must never end a container, with ONE audited
# exception: helpers/containerwatch/containment.py may build a graceful `stop`. The gate fails when
# an executable termination call site (os.kill, send_signal, .kill(), a kill/pkill argv, a
# `podman stop` argv ...) appears in helpers/containerwatch/**/*.py or the panel's
# containerReport.js and sections/containers.js, or when containment.py learns a verb beyond stop.
# It matches call-site syntax, so the word "kill" stays free in guidance strings and prose.
#
# The tool is Go (src-go/qa/nokill, command src-go/qa/cmd/qa-nokill-containerwatch); lib/go-tool.bash
# builds it into a private directory under the cache and runs it, so the exit code reaches the
# caller. Specification: CLAUDE/Plan/00192-.../SPEC-qa-nokill-containerwatch.md.
#
# Exit codes: 0 clean, 1 a forbidden call site, 2 bad usage or the run could not complete (a
# declared JavaScript target missing, no Python, an unreadable file, git failing, a build failure).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/go-tool.bash
source "$SCRIPT_DIR/lib/go-tool.bash"
run_go_tool qa-nokill-containerwatch --scripts "$SCRIPT_DIR" "$@"
