#!/usr/bin/env bash
# export.bash — Plan 00178: export and verify the room history of every v1 agent team on this
# host, before v1 is retired (SPEC-v1-export.md).
#
# WHAT IT CHANGES: nothing in v1. For each v1 team TEAM whose homeserver is running it runs, as
# root, `teamchat-admin migrate export --team TEAM` (a new stamped record under
# /var/lib/teamchat-admin/_v1-export/TEAM/), then `teamchat-admin migrate export-verify --team
# TEAM` (the record re-hashed against its digest). With --human-copy, each export also leaves
# the owner a copy under ~/agent-team-archives/.
#
# WHICH TEAMS: a v1 team is a directory under /var/lib/agent-bus/ holding team.json (listed
# through sudo). A team whose homeserver unit (agent-bus-hs@TEAM.service) is active is exported. A team
# directory whose unit is not active belongs to a team declared absent: v1's play stopped its
# server and blocked it, so it cannot be read. The run names each such team and fails at the
# end. The remedy is to declare it present again, run v1's play, run this again, and then
# declare it absent again. A purged team has no directory and nothing to export.
#
# WHERE TO RUN: on the HOST, as the owner, through meta-deploy.bash after play-teamchat.yml has
# installed the teamchat-admin that has the export verbs.
#
# Usage: ./CLAUDE/Plan/00178-agent-team-history-export/export.bash [-h|--help] [--human-copy]
set -euo pipefail
scriptDir="$(realpath "$(dirname "${BASH_SOURCE[0]}")")"
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

PLAN_USAGE="usage: export.bash [-h|--help] [--human-copy]
Export and verify the history of every v1 agent team on this host (teamchat-admin migrate
export and export-verify, as root). --human-copy also leaves the owner a copy under
~/agent-team-archives/."
plan_mode deploy
plan_parse_common_flags "$@"
humanCopy=0
for arg in "${PLAN_REMAINING_ARGS[@]}"; do
    if [[ "${arg}" == "--human-copy" ]]; then
        humanCopy=1
    else
        printf '[FATAL] unknown argument %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
        exit 64
    fi
done

plan_require_host "the v1 teams, their homeservers and teamchat-admin are this host's"
plan_prime_sudo
plan_start_log auto

if ! command -v teamchat-admin >/dev/null; then
    printf '[FATAL] teamchat-admin is not installed: run playbooks/imports/play-teamchat.yml\n' >&2
    exit 1
fi

# v1's state root and homeserver unit names are read from this checkout's agent_bus helpers
# through Plan 00201's product reader, never copied, so they cannot drift from the product.
productValue=(python3 -I "${PLAN_REPO_ROOT}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py" product-value)
stateRoot="$("${productValue[@]}" helpers/agent_bus/render.py STATE_ROOT)"
unitGlob="$("${productValue[@]}" helpers/agent_bus/health.py hs_unit '*')"

# Each listing goes to a file, not a process substitution, so its failure stops the run.
dirsFile="${PLAN_RUN_DIR}/v1-team-dirs.txt"
unitsFile="${PLAN_RUN_DIR}/v1-active-units.txt"
# A team is a directory holding team.json, as v1 finds them; the directory is also the v1 user's
# home, so other entries are not teams. The tests here need no sudo, so a sudo failure stops.
teamDirs=""
if [[ -d "${stateRoot}" ]]; then
    teamDirs="$(sudo find "${stateRoot}" -mindepth 2 -maxdepth 2 -name team.json -type f -printf '%h\n')"
fi
if [[ -n "${teamDirs}" ]]; then
    printf '%s\n' "${teamDirs}" >"${dirsFile}"
else
    : >"${dirsFile}"
fi
systemctl list-units --no-legend --plain --state=active "${unitGlob}" >"${unitsFile}"

copyArgs=()
if ((humanCopy)); then
    archive="${HOME}/agent-team-archives"
    if [[ ! -d "${archive}" ]]; then
        mkdir -m 0700 "${archive}"
    fi
    copyArgs=(--copy-to "${archive}" --copy-owner "$(id -un)")
fi

exported=0
found=0
stopped=()
# The list is read on descriptor 3, and each sudo's stdin is /dev/null, so no command can
# consume the rest of the list.
while IFS= read -r -u 3 teamDir; do
    team="${teamDir##*/}"
    found=$((found + 1))
    unit="$("${productValue[@]}" helpers/agent_bus/health.py hs_unit "${team}" </dev/null)"
    # The unit is the first field of a listed line, compared whole, not as a pattern.
    if ! awk -v unit="${unit}" '$1 == unit { found = 1 } END { exit !found }' "${unitsFile}"; then
        stopped+=("${team}")
        printf '==> %s: homeserver not running (declared absent, stopped or failed): not exported\n' "${team}"
        continue
    fi
    printf '==> %s: exporting\n' "${team}"
    sudo teamchat-admin migrate export --team "${team}" "${copyArgs[@]}" </dev/null
    printf '==> %s: verifying\n' "${team}"
    sudo teamchat-admin migrate export-verify --team "${team}" </dev/null
    exported=$((exported + 1))
done 3<"${dirsFile}"

printf '%s of %s v1 team(s) exported and verified\n' "${exported}" "${found}"
if ((${#stopped[@]} > 0)); then
    printf '[FAIL] not exported, homeserver not running: %s\n' "${stopped[*]}" >&2
    printf 'Declare each present again (v1 play), run this again, then declare it absent again.\n' >&2
    exit 1
fi
plan_finish
