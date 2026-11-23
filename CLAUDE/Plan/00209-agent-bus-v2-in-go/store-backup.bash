#!/usr/bin/env bash
# store-backup.bash — Plan 00209 cut-over: copy every teamchat seat store of this host's owner
# aside BEFORE any session runs on the new member program, so that a rollback to the old program
# can put back a store its strict reader accepts. The new program may write sync.floor_ids into
# store.json, a field the old program's strict decoder refuses (member/state/strict.go).
#
# WHAT IT CHANGES: nothing it reads. It creates one directory outside every checkout,
#   <XDG state home>/teamchat-store-backup/<UTC stamp>/      (mode 0700, umask 077 throughout)
# holding a copy of each store.json (mode 0600) at its path relative to the home directory, a
# manifest.tsv (source, copy, whether a session held the seat at copy time) and kit.txt (the host
# kit's current link target, which is the old program's digest, and the member link's target).
# Each source and destination is printed. store-restore.bash reads that directory on a rollback.
#
# WHICH STORES: the owner's seats on this host. The seat store's per-session seats,
#   <XDG state home>/teamchat/seats/sessions/<session>/<team>/store.json
# (ccy sessions, bound into the container at /run/teamchat/seats, and host cc --teams sessions,
# run with --state on the same directory), and the default host layout,
#   <home>/.config/teamchat/seats/<team>/store.json.
# Not an LXC member's: its store is inside the guest, in the agent user's
# <home>/.config/teamchat/lxc/<key>/seats, which this owner-run script cannot reach.
#
# WHY A RUNNING SESSION IS SAFE TO COPY: the member replaces store.json only by an atomic
# rename, so a copy reads one whole document; and a session running now runs the kit it was
# launched with (kits are resolved once at launch, DESIGN-v2-C-host.md C33), so its store is an
# old-format one. The copy may be older than that session's last write before it restarts on
# the new program; an older store only costs redelivery (an earlier sync position, items marked
# repeat), never a lost item. Each copy is checked to be a whole store with no sync.floor_ids;
# one that has floor_ids was already written by the new program, and the run fails naming it.
#
# WHERE TO RUN: on the HOST, as the owner, before playbooks/imports/play-teamchat.yml and the
# ccy image rebuild (meta-deploy.list orders it).
#
# Usage: ./CLAUDE/Plan/00209-agent-bus-v2-in-go/store-backup.bash [-h|--help]
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

PLAN_USAGE="usage: store-backup.bash [-h|--help]"
plan_mode deploy
plan_parse_common_flags "$@"

plan_require_host "the seat stores and the kit links it copies and records are the host owner's"
plan_start_log auto

umask 077
kitCurrent="/usr/local/lib/teamchat/kit/current"
memberLink="/usr/local/bin/teamchat"

stateHome="${XDG_STATE_HOME:-}"
if [[ "${stateHome}" != /* ]]; then
    stateHome="${HOME}/.local/state"
fi
backupRoot="${stateHome}/teamchat-store-backup"
backupDir="${backupRoot}/$(date -u '+%Y%m%dT%H%M%SZ')"

for tool in jq flock find; do
    if ! command -v "${tool}" >/dev/null; then
        printf '[FATAL] %s is not installed; that is an IaC gap, not something to skip\n' "${tool}" >&2
        exit 1
    fi
done

if [[ -e "${backupDir}" ]]; then
    printf '[FATAL] %s already exists; run again in a second\n' "${backupDir}" >&2
    exit 1
fi
mkdir -p "${backupRoot}"
chmod 0700 "${backupRoot}"
mkdir -m 0700 "${backupDir}"
printf 'backup directory: %s\n' "${backupDir}"

# The kit links before the play moves them: the current target is the old program's digest.
kitTarget="none"
if [[ -L "${kitCurrent}" ]]; then
    kitTarget="$(readlink "${kitCurrent}")"
fi
memberTarget="none"
if [[ -L "${memberLink}" ]]; then
    memberTarget="$(readlink -f "${memberLink}")"
fi
printf 'kit_current\t%s\nmember_program\t%s\n' "${kitTarget}" "${memberTarget}" >"${backupDir}/kit.txt"
printf 'old kit digest (kit current link): %s\nold member program: %s\n' "${kitTarget}" "${memberTarget}"

# find writes to a file, not a process substitution, so its failure stops the run.
listFile="${PLAN_RUN_DIR}/store-list.bin"
: >"${listFile}"
for root in "${stateHome}/teamchat/seats/sessions" "${HOME}/.config/teamchat/seats"; do
    if [[ ! -d "${root}" ]]; then
        printf 'no %s: no seats of that kind on this host\n' "${root}"
        continue
    fi
    # sessions/<session>/<team>/store.json is three levels down; seats/<team>/store.json two.
    find "${root}" -mindepth 2 -maxdepth 3 -name store.json -type f -print0 >>"${listFile}"
done

manifest="${backupDir}/manifest.tsv"
printf 'source\tcopy\theld\n' >"${manifest}"
count=0
while IFS= read -r -d '' src; do
    if [[ "${src}" != "${HOME}"/* ]]; then
        printf '[FATAL] %s is not under the home directory\n' "${src}" >&2
        exit 1
    fi
    copy="${backupDir}/${src#"${HOME}"/}"
    mkdir -p "$(dirname "${copy}")"
    cp --preserve=mode,timestamps "${src}" "${copy}"
    if ! jq -e 'type == "object" and .v == 1 and (.sync | type == "object") and (.sync | has("floor_ids") | not)' "${copy}" >/dev/null; then
        printf '[FATAL] %s is not a whole old-format store (unparseable, or sync.floor_ids present: the new program already wrote it, so this backup is too late for it)\n' "${src}" >&2
        exit 1
    fi
    # A momentary LOCK_SH probe, as the host's probe takes (member/lock package doc): no = no
    # session held the seat at the copy; yes = one did, so its store may move on before it restarts.
    held="no-lock-file"
    lock="$(dirname "${src}")/seat.lock"
    if [[ -f "${lock}" ]]; then
        exec {lockFd}<"${lock}"
        if flock -s -n "${lockFd}"; then
            held="no"
        else
            held="yes"
        fi
        exec {lockFd}<&-
    fi
    printf '%s\t%s\t%s\n' "${src}" "${copy}" "${held}" >>"${manifest}"
    printf 'copied %s\n    -> %s (session holding it: %s)\n' "${src}" "${copy}" "${held}"
    count=$((count + 1))
done <"${listFile}"

cp "${manifest}" "${PLAN_RUN_DIR}/store-backup-report.tsv"
printf '%s store(s) copied to %s\n' "${count}" "${backupDir}"
printf 'A rollback runs: CLAUDE/Plan/00209-agent-bus-v2-in-go/store-restore.bash %s\n' "${backupDir}"
plan_finish
