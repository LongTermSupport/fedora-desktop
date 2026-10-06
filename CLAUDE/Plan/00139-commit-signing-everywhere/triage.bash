#!/usr/bin/env bash
# triage.bash — report the owner's latest `vmtest run server-fast-provision` (Task: a
# headless `none` provisioning passes playbook-main.yml). vmtest keeps each run under
# ${VMTEST_HOME:-~/.local/share/vmtest}/runs/<UTC stamp>-<scenario>/, outside this
# checkout, so the container cannot read it; this prints it into the run log.
# Fact-finding only: renders no verdict of its own and changes nothing.
#
# WHERE TO RUN: on the HOST (plan_require_host), normally through meta-deploy.bash.
#
# Usage: ./CLAUDE/Plan/00139-commit-signing-everywhere/triage.bash [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

usage() {
    cat <<'EOF'
Usage: triage.bash [--help]

Prints the latest `vmtest run server-fast-provision` result (Plan 00139). Run on the HOST.

Options:
  --help    Show this help and exit (creates nothing).
EOF
}

for arg in "$@"; do
    case "$arg" in
        --help | -h)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $arg (see --help)" >&2
            exit 1
            ;;
    esac
done

plan_mode gather
plan_require_host "the vmtest run directories live in the host user's home"
plan_start_log auto

runsDir="${VMTEST_HOME:-${HOME}/.local/share/vmtest}/runs"
scenario="server-fast-provision"

[[ -d "${runsDir}" ]] || {
    printf '[FATAL] no vmtest runs directory at %s; has vmtest ever run here?\n' "${runsDir}" >&2
    exit 1
}

# Run ids start with a UTC stamp, so the last in name order is the newest.
latest=""
for dir in "${runsDir}"/*-"${scenario}"; do
    [[ -d "${dir}" ]] && latest="${dir}"
done
[[ -n "${latest}" ]] || {
    printf '[FATAL] no %s run under %s\n' "${scenario}" "${runsDir}" >&2
    exit 1
}

echo "== all ${scenario} runs"
for dir in "${runsDir}"/*-"${scenario}"; do
    [[ -d "${dir}" ]] && printf '  %s\n' "$(basename "${dir}")"
done
echo
echo "== latest: $(basename "${latest}")"
echo "-- files"
ls -la "${latest}"
echo
echo "-- response.json"
if [[ -f "${latest}/response.json" ]]; then
    cat "${latest}/response.json"
else
    echo "(none: the run did not reach its verdict)"
fi
echo
for log in "${latest}"/*.log; do
    [[ -f "${log}" ]] || continue
    echo "-- last 60 lines of $(basename "${log}")"
    awk '{l[NR]=$0} END {for (i = NR - 59; i <= NR; i++) if (i > 0) print l[i]}' "${log}"
    echo
done

plan_finish
