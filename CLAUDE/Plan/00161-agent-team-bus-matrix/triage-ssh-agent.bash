#!/usr/bin/env bash
# Plan 00161 — triage-ssh-agent.bash: why a key ccy forwards from the ssh-agent does not sign.
#
# PURPOSE: the 2026-10-10 acceptance stopped at U20, and the owner's own launches fail the same
# way: ccy forwards the saved key from the ssh-agent through its one-key agent, and the GitHub
# probe then gets no answer. This records, on the host:
#   - which agent SSH_AUTH_SOCK is (the process listening on it, the user units), the OpenSSH
#     version, and how many keys the agent lists;
#   - for each key this checkout's Quick Launch saved: whether it needs a passphrase and
#     whether the agent lists it;
#   - for each such key, four timed attempts, so a hang reads as exit 124: sign a scratch file
#     (ssh-keygen -Y sign) directly through the agent and through the deployed one-key agent,
#     and ssh -v -T to github.com both ways, then the one-key agent's log;
#   - the host's route to GitHub: on 2026-10-10 the host's TCP connect to github.com:22 timed
#     out while a container on the same host connected, so its addresses, routes, rules and
#     active connections, timed TCP connects from the host and from a rootless container, and
#     ssh -v over port 443.
# Fact-finding only: it renders no verdict (PlanScriptStandards R9). The probes are in
# triage-ssh-agent-probe.bash beside this script.
#
# RUN ON THE HOST, as the desktop user, in a terminal whose SSH_AUTH_SOCK is the one ccy uses:
# through CLAUDE/Plan/meta-deploy.bash, or
#   ./CLAUDE/Plan/00161-agent-team-bus-matrix/triage-ssh-agent.bash
# It never prompts (BatchMode) and needs no sudo.
#
# EFFECT ON THE HOST: nothing persistent. A one-key agent runs for the length of the run, in
# an owner-only directory under the run directory, and is stopped at the end. Nothing is
# added to or removed from the ssh-agent. Signatures are of a scratch file and are deleted.
#
# EXIT CODES: 0 every leg established its facts; 1 at least one did not; 64 usage.
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

PLAN_USAGE="usage: triage-ssh-agent.bash [-h|--help]

Read-only, on the HOST: which ssh-agent SSH_AUTH_SOCK is, and whether each key this checkout's
Quick Launch saved signs, directly and through ccy's one-key agent, offline and to GitHub."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it probes the desktop's ssh-agent and the deployed one-key agent"
plan_start_log auto

PROBE="${PLAN_SCRIPT_DIR}/triage-ssh-agent-probe.bash"
WORK="${PLAN_RUN_DIR}/ssh-agent"
mkdir -p "${WORK}"
chmod 700 "${WORK}"

plan_gather_leg "the ssh-agent and OpenSSH" bash "${PROBE}" agent "${PLAN_REPO_ROOT}" "${WORK}"
plan_gather_leg "the keys Quick Launch saved" bash "${PROBE}" keys "${PLAN_REPO_ROOT}" "${WORK}"
plan_gather_leg "the host's route to GitHub" bash "${PROBE}" network "${PLAN_REPO_ROOT}" "${WORK}"
plan_gather_leg "signing: directly and through the one-key agent" bash "${PROBE}" sign "${PLAN_REPO_ROOT}" "${WORK}"

plan_finish
