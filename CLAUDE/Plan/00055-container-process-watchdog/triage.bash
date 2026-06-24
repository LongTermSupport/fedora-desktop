#!/usr/bin/env bash
# Plan 00055 — post-deploy triage for the container-process watchdog.
#
# HOST-ONLY: confirms the deployed watchdog is healthy on the Fedora host. Run it
# after deploy.bash (and any time you want to re-check). It forces one scan and
# prints the timer schedule + current findings. The watchdog is reporting-only, so
# "forcing a scan" only writes a report + emits a signal — it never kills anything.
#
# Healthy output with no runaway containers is "OK - 0 findings": that is a PASS,
# not a problem — it means nothing in any container is currently long-running AND
# CPU-pinned.
#
#   ./CLAUDE/Plan/00055-container-process-watchdog/triage.bash
set -euo pipefail

if ! command -v container-watch >/dev/null; then
    echo "container-watch not on PATH — has deploy.bash run, and is ~/.local/bin on PATH?" >&2
    exit 1
fi

echo "== run a scan now (oneshot service) =="
systemctl --user start container-watch.service

echo
echo "== timer schedule =="
systemctl --user list-timers container-watch.timer --no-pager --all

echo
echo "== current findings =="
container-watch status
container-watch list

echo
echo "Triage OK. (0 findings is the healthy steady state — a finding only appears"
echo "when a container process is BOTH >= CW_AGE_S old AND CPU-pinned.)"
