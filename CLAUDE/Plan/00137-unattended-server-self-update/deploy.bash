#!/usr/bin/env bash
# Plan 00137 — deploy.bash
#
# PURPOSE: put the unattended self-update cycle on its two machines. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2): Ansible never runs in the CCY container.
#
# TWO ROLES, BECAUSE THE TRUST MODEL HAS TWO ENDS:
#
#   --role desktop  the machine the owner commits from. play-git-configure-and-tools.yml
#                   configures the passphrase-protected SSH signing key
#                   (`git_signing_key` in host_vars). Nothing signs by default; a release
#                   is a deliberate `git sign-deploy` or `git commit -S`.
#   --role server   the always-on server hosting ccy sessions, in this order:
#                   1. play-claude-yolo.yml: `ccy-sessions` (warn, verify-restore) and the
#                      restore unit. The cycle reboots, so `ccy_restore_sessions: true`
#                      must be declared first, or the sessions do not come back.
#                   2. play-self-update.yml: the deploy clone, the root sbin entry point,
#                      its sudoers drop-in, the system units, root-owned ansible-core and
#                      collections, and ptrace_scope=1. It refuses to run with
#                      `self_update_enabled: true` and incomplete inputs.
#
# Usage: ./deploy.bash --role desktop|server [-h|--help] [-y|--yes]
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

PLAN_USAGE="usage: deploy.bash --role desktop|server [-h|--help] [-y|--yes]

  --role desktop   playbooks/imports/play-git-configure-and-tools.yml   (commit signing)
  --role server    playbooks/imports/play-claude-yolo.yml               (sessions + restore)
                   playbooks/imports/optional/common/play-self-update.yml (the cycle)

Declare the inputs in host_vars first; host_vars/localhost.yml.dist lists them. For
the server that means self_update_* (the password vault-encrypted) and
ccy_restore_sessions: true.

-y/--yes is accepted; this script asks nothing, so it is a no-op here.
--check is REFUSED: both server plays read a command task's registered stdout in a
later task, which check mode skips.

AFTERWARDS (server): CLAUDE/Plan/00137-unattended-server-self-update/acceptance.bash."

plan_mode deploy
plan_parse_common_flags "$@"

role=""
set -- "${PLAN_REMAINING_ARGS[@]}"
while [[ $# -gt 0 ]]; do
    case "$1" in
        --role)
            [[ $# -ge 2 ]] || { printf '[FATAL] --role needs desktop or server\n' >&2; exit 64; }
            role="$2"
            shift 2
            ;;
        --role=*)
            role="${1#--role=}"
            shift
            ;;
        *)
            printf '[FATAL] unknown argument: %s\n' "$1" >&2
            printf '%s\n' "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
case "${role}" in
    desktop|server) ;;
    *)
        printf '[FATAL] --role must be desktop or server (got %s)\n' "${role:-nothing}" >&2
        printf '%s\n' "${PLAN_USAGE}" >&2
        exit 64
        ;;
esac

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check is not supported: the plays read registered command output in\n' >&2
    printf '        later tasks, and check mode skips command tasks.\n' >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's git config, launchers, systemd units and sudoers"
plan_prime_sudo
plan_start_log auto

if [[ "${role}" == "desktop" ]]; then
    plan_deploy_leg "play-git-configure-and-tools.yml" \
        plan_ansible_playbook playbooks/imports/play-git-configure-and-tools.yml
    printf '\n==> NEXT: release with "git sign-deploy" (an empty signed commit) and push.\n'
    printf '    The server moves only to the newest commit your key signed.\n\n'
else
    plan_deploy_leg "play-claude-yolo.yml" \
        plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
    plan_deploy_leg "play-self-update.yml" \
        plan_ansible_playbook playbooks/imports/optional/common/play-self-update.yml
    printf '\n==> NEXT: run acceptance.bash in this folder. It dry-runs the cycle and reads\n'
    printf '    back every piece the plays installed.\n\n'
fi

plan_finish
