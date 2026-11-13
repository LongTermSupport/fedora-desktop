#!/usr/bin/env bash
# probe-instances.bash — Plan 00254 Task 3.1: the facts behind "one Lightning per team".
#
# Called by triage.bash, one leg per mode; runnable on its own on the HOST for one mode.
# Appends what it finds to REPORT and exits non-zero when the fact could not be established.
# Renders no verdict (PlanScriptStandards.md R9).
#
#   env      does `flatpak run --env=XDG_*_HOME=...` reach the sandbox? And the fallback,
#            `--command=env XDG_*_HOME=... printenv`?
#   runtime  is the runtime's CA bundle at /etc/ssl/certs/ca-certificates.crt, and is there a
#            python3 (Phase 4's handshake check needs both)?
#   two      start two instances on two probe roots; after a settle period, is each still
#            running and holding its own instance lock? Then launch a third on the FIRST root:
#            does it exit by itself while the first keeps running?
#
# EFFECT ON THE HOST: mode `two` opens up to three Lightning windows for under a minute, with
# their data under ~/.var/app/org.lightning_matrix.Lightning/teams-probe/, then stops the
# processes it started and removes that directory. It never touches the default profile or an
# instance it did not start (no `flatpak kill`, which would end every instance of the app).
# The launch logs are kept beside REPORT.
#
# Usage: probe-instances.bash env|runtime|two REPORT
set -euo pipefail

readonly APP="org.lightning_matrix.Lightning"
readonly SETTLE_SECONDS=20
readonly RAISE_WAIT_SECONDS=20
readonly USAGE="usage: probe-instances.bash env|runtime|two REPORT"

mode="${1:?${USAGE}}"
report="${2:?${USAGE}}"
reportDir="$(dirname "${report}")"
probeBase="${HOME}/.var/app/${APP}/teams-probe"
started=()

say() { printf '%s\n' "$*" | tee -a "${report}"; }

# alive PID: the process exists and is not a zombie waiting to be reaped.
alive() {
  local state
  state="$(ps -p "$1" -o stat=)" || return 1
  [[ "${state}" != Z* ]]
}

requireApp() {
  local infoOut
  if ! infoOut="$(flatpak info "${APP}" 2>&1)"; then
    say "FACT ${mode}: COULD NOT ESTABLISH: ${APP} is not installed: ${infoOut}"
    exit 1
  fi
  say "FACT ${mode}: ${APP}: $(printf '%s\n' "${infoOut}" | awk '/^ *(Version|Commit|Runtime):/ {$1=$1; printf "%s; ", $0}')"
}

probeEnv() {
  local root="${probeBase}/env" want got
  want="${root}/data|${root}/config|${root}/cache"
  got="$(flatpak run --env=XDG_DATA_HOME="${root}/data" --env=XDG_CONFIG_HOME="${root}/config" \
    --env=XDG_CACHE_HOME="${root}/cache" --command=printenv "${APP}" \
    XDG_DATA_HOME XDG_CONFIG_HOME XDG_CACHE_HOME | paste -sd '|')"
  say "FACT env, --env form: sandbox sees [${got}], given [${want}]: $([[ "${got}" == "${want}" ]] && echo SAME || echo DIFFERENT)"
  got="$(flatpak run --command=env "${APP}" XDG_DATA_HOME="${root}/data" \
    XDG_CONFIG_HOME="${root}/config" XDG_CACHE_HOME="${root}/cache" \
    printenv XDG_DATA_HOME XDG_CONFIG_HOME XDG_CACHE_HOME | paste -sd '|')"
  say "FACT env, --command=env form: sandbox sees [${got}], given [${want}]: $([[ "${got}" == "${want}" ]] && echo SAME || echo DIFFERENT)"
}

probeRuntime() {
  local out rc
  if out="$(flatpak run --command=ls "${APP}" -l /etc/ssl/certs/ca-certificates.crt 2>&1)"; then rc=0; else rc=$?; fi
  say "FACT runtime CA bundle: exit ${rc}: ${out}"
  if out="$(flatpak run --command=python3 "${APP}" --version 2>&1)"; then rc=0; else rc=$?; fi
  say "FACT runtime python3: exit ${rc}: ${out}"
}

# launch ROOT NAME: start one instance on ROOT in the background; prints its pid.
launch() {
  local root="$1" name="$2"
  mkdir -p "${root}/data" "${root}/config" "${root}/cache"
  flatpak run --env=XDG_DATA_HOME="${root}/data" --env=XDG_CONFIG_HOME="${root}/config" \
    --env=XDG_CACHE_HOME="${root}/cache" "${APP}" >"${reportDir}/lightning-probe-${name}.log" 2>&1 &
  printf '%s\n' "$!"
}

stopStarted() {
  local pid
  for pid in "${started[@]}"; do
    if alive "${pid}"; then kill "${pid}"; fi
  done
  sleep 3
  for pid in "${started[@]}"; do
    if alive "${pid}"; then say "FACT two: pid ${pid} did not stop on SIGTERM; left running"; fi
  done
  rm -rf -- "${probeBase}"
}

countInstances() { flatpak ps --columns=application | awk -v a="${APP}" '$1 == a {n++} END {print n + 0}'; }

probeTwo() {
  if [[ -z "${WAYLAND_DISPLAY:-}" && -z "${DISPLAY:-}" ]]; then
    say "FACT two: COULD NOT ESTABLISH: no WAYLAND_DISPLAY or DISPLAY; run from a terminal in the desktop session"
    exit 1
  fi
  trap stopStarted EXIT
  local a="${probeBase}/team-a" b="${probeBase}/team-b" pidA pidB pidC name pid root lock waited=0
  say "FACT two: instances of ${APP} running before the probe: $(countInstances)"
  pidA="$(launch "${a}" a)"
  started+=("${pidA}")
  pidB="$(launch "${b}" b)"
  started+=("${pidB}")
  sleep "${SETTLE_SECONDS}"
  for name in A B; do
    if [[ "${name}" == A ]]; then pid="${pidA}" root="${a}"; else pid="${pidB}" root="${b}"; fi
    lock="${root}/data/MatrixClient/matrix-client/instance-matrix-client.lock"
    say "FACT two: instance ${name} after ${SETTLE_SECONDS}s: $(alive "${pid}" && echo running || echo EXITED); its lock $([[ -e "${lock}" ]] && echo present || echo ABSENT) at ${lock}"
  done
  say "FACT two: instances of ${APP} running with both probes up: $(countInstances)"
  pidC="$(launch "${a}" c)"
  started+=("${pidC}")
  while alive "${pidC}" && [[ "${waited}" -lt "${RAISE_WAIT_SECONDS}" ]]; do
    sleep 1
    waited=$((waited + 1))
  done
  say "FACT two: a second launch on root A: $(alive "${pidC}" && echo "STILL RUNNING after ${RAISE_WAIT_SECONDS}s" || echo "exited after ${waited}s"); instance A $(alive "${pidA}" && echo "still running" || echo EXITED)"
  say "FACT two: launch logs: ${reportDir}/lightning-probe-{a,b,c}.log"
  say "NOT ESTABLISHABLE by script: that the two windows each showed their own login page (the owner looks)"
}

mkdir -p "${reportDir}"
requireApp
case "${mode}" in
  env) probeEnv ;;
  runtime) probeRuntime ;;
  two) probeTwo ;;
  *)
    printf 'probe-instances.bash: unknown mode %s\n%s\n' "${mode}" "${USAGE}" >&2
    exit 2
    ;;
esac
