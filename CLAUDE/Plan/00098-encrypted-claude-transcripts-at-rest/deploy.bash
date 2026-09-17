#!/usr/bin/env bash
#
# Plan 00098 — deploy the Claude Code store containment.
# HOST ONLY (CLAUDE/PlanScriptStandards.md R2).
#
# WHY THIS FILE EXISTS. The plan shipped an acceptance.bash and no deploy.bash, so under
# the batch harness its check 5 ("desktop store contained") failed, printed the very play
# that fixes it, and nothing ever ran that play. A gate that names its own remedy and has
# no way to apply it fails identically forever, and each run looks like a fresh finding
# rather than the same unapplied fix.
#
# WHAT IT CHANGES ON THE HOST: play-claude-code.yml re-asserts ownership and the 0700/0600
# permission model over the Claude Code state directory, so files below it stop being
# reachable by other local users. It does not touch transcript content.
#
# Usage: ./deploy.bash [-h|--help] [-y|--yes] [--check]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [-y|--yes] [--check]

Plan 00098 — deploy the Claude Code store containment (HOST ONLY)

Runs:
  1. play-claude-code.yml — re-asserts the 0700/0600 permission model over the
                            Claude Code state directory

Run acceptance.bash afterwards to confirm the change landed."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it rewrites the ownership and permissions of this machine's Claude Code state directory"

PLAY_CLAUDE_CODE="playbooks/imports/play-claude-code.yml"

if [[ ! -f "${PLAN_REPO_ROOT}/${PLAY_CLAUDE_CODE}" ]]; then
    printf '[FATAL] playbook not found: %s\n' "${PLAN_REPO_ROOT}/${PLAY_CLAUDE_CODE}" >&2
    exit 1
fi

plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-code.yml" \
    plan_ansible_playbook "${PLAY_CLAUDE_CODE}"
