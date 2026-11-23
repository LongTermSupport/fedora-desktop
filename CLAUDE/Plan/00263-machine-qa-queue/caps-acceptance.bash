#!/usr/bin/env bash
# Plan 00263 Task A.7b: the stage 1a caps hold on this host. Read only; it changes nothing.
# Run after play-systemd-user-tweaks.yml --tags oomd, play-claude-yolo.yml and the relaunch of
# the ccy sessions.
#
#   1. user@UID.service reports ManagedOOMMemoryPressure=auto: systemd-oomd no longer kills in
#      the user session on memory pressure (Fedora's default there is kill at 50% for 20 s).
#   2. oomctl dump lists no memory-pressure watch on user@UID.service: the live oomd, not the
#      unit file. oomctl needs root: run through sudo -n, a refusal is COULD NOT ESTABLISH.
#   3. agents.slice is loaded by the user manager with MemoryMax and MemoryHigh set.
#   4. every running ccy container (label ccy=true) and ccc container (ccc=true) has its
#      cgroup under user@UID.service/agents.slice/ (podman's live cgroup path), and its scope
#      has a MemoryMax and a TasksMax (the user manager's live values).
#
# Exit 0: all hold. Exit 1: one does not (named). Exit 2: COULD NOT ESTABLISH one (named), as
# when no ccy or ccc container is running.
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

PLAN_USAGE="usage: caps-acceptance [-h|--help]
Check, read only, that systemd-oomd no longer kills in the user session on memory pressure,
that agents.slice is loaded with its caps, and that every running ccy and ccc container is
under it with its memory and pids limits."
plan_parse_common_flags "$@"
plan_mode gather
plan_require_host "it reads this host's systemd managers, systemd-oomd and container cgroups"
plan_start_log auto

uid="$(id -u)"
userManager="user@${uid}.service"
slicePrefix="/user.slice/user-${uid}.slice/${userManager}/agents.slice/"
failed=0
unknown=0

ok() { printf 'OK %s\n' "$*"; }
bad() {
    printf 'FAIL %s\n' "$*" >&2
    failed=1
}
unsure() {
    printf 'COULD NOT ESTABLISH %s\n' "$*" >&2
    unknown=1
}

# 1. The unit's property, as pid 1 reports it.
pressure="$(systemctl show "${userManager}" --property=ManagedOOMMemoryPressure --property=DropInPaths)"
printf '%s\n' "${pressure}"
if grep -qx 'ManagedOOMMemoryPressure=auto' <<<"${pressure}"; then
    ok "1. ${userManager}: no memory-pressure kill"
else
    bad "1. ${userManager} is still set to be killed on memory pressure"
fi

# 2. The live oomd: the cgroups it watches for pressure are listed under one heading.
if dump="$(sudo -n oomctl dump 2>&1)"; then
    watched="$(awk '/^Memory Pressure Monitored CGroups:/ {m = 1; next} /^[A-Z].*:$/ {m = 0} m' <<<"${dump}")"
    if grep -qF "${userManager}" <<<"${watched}"; then
        bad "2. systemd-oomd still watches ${userManager} for memory pressure"
    else
        ok "2. systemd-oomd watches no memory pressure on ${userManager}"
    fi
else
    unsure "2. oomctl dump did not answer through sudo -n: ${dump}"
fi

# 3. The slice, as the user manager has it loaded.
slice="$(systemctl --user show agents.slice --property=LoadState --property=MemoryMax --property=MemoryHigh)"
printf '%s\n' "${slice}"
if grep -qx 'LoadState=loaded' <<<"${slice}" && ! grep -qx -e 'MemoryMax=infinity' -e 'MemoryHigh=infinity' <<<"${slice}"; then
    ok "3. agents.slice is loaded with its caps"
else
    bad "3. agents.slice is not loaded, or has no MemoryMax or MemoryHigh"
fi

# 4. Each running container's live cgroup and its scope's limits. The listings go to files, so
# a podman or awk failure stops the run instead of reading as no containers. Each line of the
# second is the container's name, its cgroup path, and that path's last part (its scope unit).
idFile="${PLAN_RUN_DIR}/container-ids.txt"
podman ps --quiet --no-trunc --filter label=ccy=true >"${idFile}"
podman ps --quiet --no-trunc --filter label=ccc=true >>"${idFile}"
listFile="${PLAN_RUN_DIR}/containers.txt"
: >"${listFile}"
if [[ -s "${idFile}" ]]; then
    xargs podman inspect --format '{{.Name}} {{.State.CgroupPath}}' <"${idFile}" >"${listFile}"
fi
scopeFile="${PLAN_RUN_DIR}/container-scopes.txt"
awk -F'[ /]' '{print $0, $NF}' "${listFile}" >"${scopeFile}"
found=0
while read -r name cg scope; do
    found=1
    limits="$(systemctl --user show "${scope}" --property=MemoryMax --property=TasksMax)"
    printf '%s cgroup %s %s\n' "${name}" "${cg}" "$(tr '\n' ' ' <<<"${limits}")"
    if [[ "${cg#"${slicePrefix}"}" == "${cg}" ]]; then
        bad "4. ${name} is not under agents.slice (started before the caps? relaunch it)"
    elif grep -qx -e 'MemoryMax=infinity' -e 'TasksMax=infinity' <<<"${limits}"; then
        bad "4. ${name} is under agents.slice with no memory or pids limit"
    else
        ok "4. ${name} is under agents.slice with its limits"
    fi
done <"${scopeFile}"
((found)) || unsure "4. no running ccy or ccc container to look at"

if ((failed)); then
    printf '==> FAIL: a cap does not hold (above)\n' >&2
    PLAN_FAILED_LEGS="caps"
    plan_finish
fi
if ((unknown)); then
    # plan_finish has no third status: COULD NOT ESTABLISH is exit 2, said here.
    plan_list_reports
    printf '==> COULD NOT ESTABLISH: see the lines above\n' >&2
    exit 2
fi
printf '==> PASS: the stage 1a caps hold\n'
plan_finish
