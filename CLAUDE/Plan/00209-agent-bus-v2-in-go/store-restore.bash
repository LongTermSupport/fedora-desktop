#!/usr/bin/env bash
# store-restore.bash — Plan 00209 rollback: put back the seat stores store-backup.bash copied,
# once the host's member program is the old one again, so the old program's strict reader finds
# a store it accepts and resumes from that store's sync position.
#
# WHAT IT CHANGES: for each seat in <backup dir>/manifest.tsv whose directory still exists, under
# that seat's lock (an exclusive flock on seat.lock, as the member takes it), the current
# store.json is renamed aside to store.json.aside-<ms> (the name `teamchat seat set-aside` uses,
# kept for inspection, never deleted) and the backup copy is put in its place (mode 0600, renamed
# into place, synced). Nothing else in the seat directory is touched; seat.lock keeps its inode.
#
# IT REFUSES, before changing anything: a backup directory without manifest.tsv and kit.txt; a
# copy that is missing or is not a whole old-format store; a host whose kit current link is not
# the one kit.txt recorded (roll the program back first); and any seat a session holds now (end
# that session first: a store put back under a running link would be overwritten by it).
# A seat whose directory is gone (its session released) is listed as such: nothing reads it.
#
# WHAT A RESTORED STORE COSTS: the old program resumes from the position the backup recorded,
# so the items since then are fetched again and delivered as repeats; none is lost.
#
# WHERE TO RUN: on the HOST, as the owner, after the old member program is deployed and before
# any teamchat session starts again (meta-deploy.list orders it).
#
# Usage: ./CLAUDE/Plan/00209-agent-bus-v2-in-go/store-restore.bash <backup dir> [-h|--help]
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

PLAN_USAGE="usage: store-restore.bash <backup dir> [-h|--help]"
plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 1 ]]; then
    printf '[FATAL] %s\n' "${PLAN_USAGE}" >&2
    exit 1
fi
backupDir="${PLAN_REMAINING_ARGS[0]}"

plan_require_host "the seat stores it puts back and the kit link it checks are the host owner's"
plan_start_log auto

umask 077
kitCurrent="/usr/local/lib/teamchat/kit/current"
manifest="${backupDir}/manifest.tsv"
kitRecord="${backupDir}/kit.txt"

for tool in jq flock; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not installed; that is an IaC gap, not something to skip\n' "${tool}" >&2
        exit 1
    fi
done
for f in "${manifest}" "${kitRecord}"; do
    if [[ ! -f "${f}" ]]; then
        printf '[FATAL] %s is missing: %s is not a store-backup.bash directory\n' "${f}" "${backupDir}" >&2
        exit 1
    fi
done

# The program first: the old kit must be current again, or the new program would rewrite these.
recordedKit="$(awk -F'\t' '$1 == "kit_current" {print $2}' "${kitRecord}")"
kitNow="none"
if [[ -L "${kitCurrent}" ]]; then
    kitNow="$(readlink "${kitCurrent}")"
fi
if [[ "${recordedKit}" != "${kitNow}" ]]; then
    printf '[FATAL] the kit current link is %s, not the %s recorded at the backup: deploy the old member program first\n' "${kitNow}" "${recordedKit}" >&2
    exit 1
fi
printf 'kit current link is the recorded old kit: %s\n' "${kitNow}"

# Pass 1 judges every seat and changes nothing; pass 2 runs only when every seat passed.
pass=1
while [[ "${pass}" -le 2 ]]; do
    restored=0
    gone=0
    while IFS=$'\t' read -r src copy _held; do
        if [[ "${src}" == "source" ]]; then
            continue
        fi
        seat="$(dirname "${src}")"
        if [[ ! -d "${seat}" ]]; then
            if [[ "${pass}" -eq 2 ]]; then
                printf 'seat directory gone, nothing reads it: %s\n' "${seat}"
            fi
            gone=$((gone + 1))
            continue
        fi
        if [[ ! -f "${copy}" ]] || ! jq -e 'type == "object" and .v == 1 and (.sync | type == "object") and (.sync | has("floor_ids") | not)' "${copy}" >/dev/null; then
            printf '[FATAL] the copy %s is missing or not a whole old-format store\n' "${copy}" >&2
            exit 1
        fi
        lock="${seat}/seat.lock"
        if [[ ! -f "${lock}" ]]; then
            printf '[FATAL] %s has no seat.lock, so the store cannot be put back under its lock\n' "${seat}" >&2
            exit 1
        fi
        exec {lockFd}<"${lock}"
        if ! flock -x -n "${lockFd}"; then
            exec {lockFd}<&-
            printf '[FATAL] a session holds %s: end that session, then run this again\n' "${seat}" >&2
            exit 1
        fi
        if [[ "${pass}" -eq 2 ]]; then
            if [[ -e "${seat}/store.json" ]]; then
                aside="${seat}/store.json.aside-$(date '+%s%3N')"
                mv -T "${seat}/store.json" "${aside}"
                printf 'set aside %s\n    -> %s\n' "${seat}/store.json" "${aside}"
            fi
            cp --preserve=mode,timestamps "${copy}" "${seat}/store.json.restoring"
            chmod 0600 "${seat}/store.json.restoring"
            mv -T "${seat}/store.json.restoring" "${seat}/store.json"
            sync "${seat}/store.json" "${seat}"
            printf 'restored %s\n    <- %s\n' "${seat}/store.json" "${copy}"
            printf '%s\t%s\n' "${seat}/store.json" "${copy}" >>"${PLAN_RUN_DIR}/store-restore-report.tsv"
            restored=$((restored + 1))
        fi
        exec {lockFd}<&-
    done <"${manifest}"
    pass=$((pass + 1))
done

printf '%s store(s) restored, %s seat directory(ies) gone, from %s\n' "${restored}" "${gone}" "${backupDir}"
plan_finish
