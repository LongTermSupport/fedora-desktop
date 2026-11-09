#!/usr/bin/env bash
# Plan 00203 — lxc-throwaways-up.bash: two throwaway LXC containers, left running, for Task
# 3.23's host acceptance of two LXC members in one team (`ccy-teams lxc-add` offers only a
# running container, and the host's own containers are the owner's). The containers are made as
# Plan 00161's U23 made its LXC member (_acceptance-u23.inc.bash, u23_prepare_lxc).
# lxc-throwaways-down.bash removes them.
#
# RUN ON THE HOST, as the desktop user (not root), by hand from Task 3.23's steps:
#   ./CLAUDE/Plan/00203-teams-hot-control-and-robustness/lxc-throwaways-up.bash
# The one prompt is sudo's, before the log opens (R3); every root step after it uses `sudo -n`.
#
# WHAT IT CHANGES: two containers in root's LXC, t00203-coord and t00203-worker, from lxc-create's
# download template (Fedora, this host's release: an image download), started. In each, by dnf,
# python3, git-core, util-linux-core, shadow-utils and gh; the agent user `agent` with its
# checkout ~/t00203-work (git init, no remote); and that user's gh logged in to github.com with
# the desktop user's token (README.lxc's requirement, which the join play checks), kept in the
# agent's home and gone with the container. The token is held in this shell only: never in an
# argv, a log or a file. Nothing of the bus is set up: `ccy-teams lxc-add` does that. A run that
# fails part-way destroys the containers it made.
#
# EXIT CODES: 0 both containers are up (what to run next is printed); 1 a step failed and names
# itself (what it made is removed); 64 usage.
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
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

PLAN_USAGE="usage: lxc-throwaways-up.bash [-h|--help]

Plan 00203 Task 3.23: two throwaway LXC containers (t00203-coord, t00203-worker), each with the
agent user 'agent', its checkout ~/t00203-work and gh logged in, left running for
'ccy-teams lxc-add'. lxc-throwaways-down.bash removes them.
Exit 0 up, 1 a step failed (what it made is removed), 64 usage. Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
    exit 64
done
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: the containers are real\n' >&2
    exit 64
fi

plan_require_host "it creates LXC containers in root's LXC on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: it uses sudo for each root step\n' >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

# shellcheck source-path=SCRIPTDIR
# shellcheck source=_lxc-throwaways.inc.bash
source "${PLAN_SCRIPT_DIR}/_lxc-throwaways.inc.bash"
readonly REPORT="${PLAN_RUN_DIR}/lxc-throwaways-report.md"

plan_on_cleanup up_after_stop

printf '# Plan 00203 Task 3.23 throwaway LXC containers\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
plan_deploy_leg "LXC with its download template; the desktop user's gh login" up_prerequisites
plan_deploy_leg "no throwaway container of these names is here already" lt_none_left
plan_deploy_leg "make, start and prepare ${LT_NAMES[*]}" up_all

printf '\nUP: %s are running, each with user %s and checkout %s.\n' "${LT_NAMES[*]}" "${LT_AGENT}" "${LT_CHECKOUT}" | tee -a "${REPORT}"
cat <<EOF | tee -a "${REPORT}"

Next: Task 3.23's steps in CLAUDE/Plan/00203-teams-hot-control-and-robustness/PLAN.md
(ccy-teams lxc-add, once per container, user ${LT_AGENT}, checkout ${LT_CHECKOUT}).

When done: ./CLAUDE/Plan/00203-teams-hot-control-and-robustness/lxc-throwaways-down.bash
EOF
plan_finish
