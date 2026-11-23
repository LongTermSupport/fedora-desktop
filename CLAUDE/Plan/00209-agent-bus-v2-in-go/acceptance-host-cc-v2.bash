#!/usr/bin/env bash
# Plan 00209 Task 5.1 acceptance, the read-only part (DESIGN-host-cc-v2.md section 8).
#
# Before a host cc session on a teamchat team (no session named):
#   - the member program answers `teamchat version`, and the host kit
#     (/usr/local/lib/teamchat/kit/current/) holds settings.json and the teamchat plugin;
#   - the deployed ccy is this checkout's CCY_VERSION;
#   - `ccy-teams system <team>`, run in <checkout>, says teamchat.
# After the attended launch (a session named): its record in the seat store,
# <store>/sessions/<session>/conversation.json, names a transcript Claude Code wrote.
#
# The launch, the round trip, the stop, the restore and the kill (section 8 steps 2-4, 6-8) need
# a person at the session; the script lists them and does not judge them.
#
# Exit 0: every check it can make holds. Exit 1: one fails. Exit 2: COULD NOT ESTABLISH (no team
# or checkout given and no terminal to ask, or no session named yet: the session steps remain).
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

PLAN_USAGE="usage: $(basename "$0") [-h|--help] [<team> <checkout> [<session>]]
Read-only checks of a host cc session on teamchat team <team>, started in <checkout>. With no
<session> the before checks run and the script exits 2 (the session steps remain); with the
tmux session name of the running or stopped session, its conversation record is checked too.
Without <team> and <checkout> they are asked at the terminal."
plan_parse_common_flags "$@"
plan_mode gather
plan_require_host "it reads this host's teamchat kit, ccy and seat store"
plan_start_log auto

team="" checkout="" session=""
if ((${#PLAN_REMAINING_ARGS[@]} > 0)); then team="${PLAN_REMAINING_ARGS[0]}"; fi
if ((${#PLAN_REMAINING_ARGS[@]} > 1)); then checkout="${PLAN_REMAINING_ARGS[1]}"; fi
if ((${#PLAN_REMAINING_ARGS[@]} > 2)); then session="${PLAN_REMAINING_ARGS[2]}"; fi
failed=0
unknown=0
pass() { printf 'EVIDENCE %s\n' "$1"; }
bad() {
    printf 'FAIL %s\n' "$1" >&2
    failed=1
}
unsure() {
    printf 'COULD NOT ESTABLISH %s\n' "$1" >&2
    unknown=1
}

if [[ -z "${team}" || -z "${checkout}" ]]; then
    if ! { : </dev/tty; } 2>/dev/null; then
        unsure "no <team> and <checkout> given, and no terminal to ask them on"
        exit 2
    fi
    read -r -p 'teamchat team the host seat joins: ' team </dev/tty
    read -r -p 'checkout cc is started in (absolute path): ' checkout </dev/tty
fi
if [[ "${checkout:0:1}" != "/" || ! -d "${checkout}/" ]]; then
    bad "checkout '${checkout}' is not an absolute path to a directory"
    exit 1
fi

# 1. The member program and the host kit.
if version="$(teamchat version 2>&1)"; then
    pass "teamchat version: ${version}"
else
    bad "teamchat version failed: ${version} (run play-teamchat.yml --tags member)"
fi
kit=/usr/local/lib/teamchat/kit/current
for f in settings.json plugin/teamchat/.claude-plugin/plugin.json; do
    if [[ -f "${kit}/${f}" ]]; then
        pass "the host kit holds ${f}"
    else
        bad "${kit}/${f} is missing (play-teamchat.yml --tags member assembles it)"
    fi
done

# 2. The deployed ccy is this checkout's (the version appears in what --version prints).
want="$(grep -m1 '^CCY_VERSION=' "${PLAN_REPO_ROOT}/files/var/local/claude-yolo/claude-yolo" | cut -d'"' -f2)"
if got="$(ccy --version 2>&1)" && [[ -n "${want}" && "${got/"${want}"/}" != "${got}" ]]; then
    pass "ccy --version names ${want}"
else
    bad "ccy --version said '${got}', want ${want} (run play-claude-yolo.yml)"
fi

# 3. The team is on teamchat for the checkout: the third word of its line is the system.
line="" sys=""
if line="$(cd "${checkout}/" && ccy-teams system "${team}" 2>&1)"; then
    read -r _ _ sys <<<"${line}"
fi
if [[ "${sys}" == "teamchat" ]]; then
    pass "ccy-teams system in the checkout: ${line}"
else
    bad "ccy-teams system ${team} in the checkout said '${line}', want teamchat (is a pool seat placed there?)"
fi

# 4. The session's conversation record, in the seat store.
if [[ -z "${session}" ]]; then
    unsure "no session named: launch it (section 8 steps 2-3), then run again with its tmux session name"
else
    record="${XDG_STATE_HOME:-${HOME}/.local/state}/teamchat/seats/sessions/${session}/conversation.json"
    if [[ ! -f "${record}" ]]; then
        bad "${record} is missing: the v2 session wrote no conversation record"
    elif ! sid="$(jq -er '.session_id' "${record}")"; then
        bad "${record} names no session_id"
    elif [[ -n "$(find "${HOME}/.claude/projects" -mindepth 2 -maxdepth 2 -name "${sid}.jsonl" -print -quit)" ]]; then
        pass "the record names transcript ${sid}"
    else
        bad "the record names ${sid}, and no transcript ${sid}.jsonl is under ~/.claude/projects"
    fi
fi

cat <<'EOF'
The attended steps (DESIGN-host-cc-v2.md section 8), judged by the person at the session:
  2. seat: member add --prefix <repo>+<site>.host ... | teamchat seat place-issued --checkout <checkout>
  3. launch: cc --teams 1@<team> in <checkout>; teamchat reach --team <team> from its Bash tool exits 0
  4. round trip with a ccy seat, seen in teamchat history on both sides
  6. stop: ccy --stop <session>; the seat is released and the registry record gone
  7. restore: ccy-sessions declare/restore; the session recalls a fact and posts again
  8. kill: tmux kill-session; the seat is kept while the record remains
EOF

if ((failed)); then
    exit 1
fi
if ((unknown)); then
    exit 2
fi
printf '==> every check this script makes holds\n'
