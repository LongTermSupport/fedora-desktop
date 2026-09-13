#!/usr/bin/bash
# vmtest-session-provision.bash — runs INSIDE the desktop guest, launched by gnome-session
# through the autostart entry the VM kickstart wrote (Plan 00110, DESIGN.md §5.3 item 3).
#
# It is the session. A transient `systemd-run --user` unit is not: it belongs to no logind
# session, so XDG_SESSION_TYPE, XDG_SESSION_ID and XDG_SEAT are absent there. This script
# inherits the real session environment by construction and records it as evidence.
#
# Protocol with the host (all under /var/lib/vmtest, owned by the lab user):
#   session.env   written at every session start: the environment this script inherited
#   run.env       the host writes it over SSH: RUN_BASH_* assignments, one per line
#   run.log       everything run.bash printed, appended as it runs
#   run.exit      written when run.bash returns, holding its exit status
# The script waits for run.env for up to an hour, runs run.bash from the checkout the
# host told it about, and exits. Nothing here is a check; the guest acceptance script and
# the host judge the results.
set -uo pipefail

STATE=/var/lib/vmtest
readonly STATE
WAIT_SECONDS=3600
readonly WAIT_SECONDS

mkdir -p "${STATE}"
env | sort >"${STATE}/session.env"

waited=0
while [[ ! -r "${STATE}/run.env" ]]; do
    if [[ "${waited}" -ge "${WAIT_SECONDS}" ]]; then
        printf 'no run.env within %ss; nothing to run\n' "${WAIT_SECONDS}" >>"${STATE}/run.log"
        exit 0
    fi
    sleep 2
    waited=$((waited + 2))
done

# One run per session: a second launch (a re-login) must not re-run provisioning.
if [[ -e "${STATE}/run.exit" ]]; then
    exit 0
fi

set -a
# shellcheck source=/dev/null
source "${STATE}/run.env"
set +a

rc=0
{
    printf '==> session runner: starting run.bash at %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    bash "${HOME}/run.bash"
} >>"${STATE}/run.log" 2>&1 || rc=$?
printf '%s\n' "${rc}" >"${STATE}/run.exit.tmp"
mv -f "${STATE}/run.exit.tmp" "${STATE}/run.exit"
exit 0
