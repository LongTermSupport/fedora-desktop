#!/usr/bin/env bash
# Plan 00139 — deploy.bash
#
# PURPOSE: turn on commit signing everywhere on this machine. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY container.
#
# THE LEGS, IN ORDER:
#   1. play-claude-yolo.yml — the ccy launcher (CCY 3.66.0) that carries the key into
#      each container. First, because it is harmless on its own: while ~/.gitconfig does
#      not ask for signing it stages nothing and refuses nothing.
#   2. play-git-configure-and-tools.yml — generates this machine's SSH signing key
#      (~/.ssh/id_ed25519_git_signing, no passphrase) unless git_signing_key names
#      another, and sets gpg.format, user.signingkey, commit.gpgsign and tag.gpgsign in
#      ~/.gitconfig. It removes the opt-in settings Plan 00137 wrote to the XDG config.
#      Second, because from here on ~/.gitconfig asks for signing, and a launcher older
#      than 3.66.0 would start containers whose every commit fails. The deploy stops at
#      the first failing leg, so this order never leaves that state behind.
#
# ccy sessions already running keep the gitconfig they started with, so they do not
# sign until they are restarted.
#
# Usage: ./deploy.bash [-h|--help] [--check]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check]

Runs, on the HOST, in this order:

  playbooks/imports/play-claude-yolo.yml               (ccy carries the key into containers)
  playbooks/imports/play-git-configure-and-tools.yml   (the signing key, sign everything)

The launcher goes first: on its own it changes nothing, while signing switched on under
an older launcher would start containers that cannot commit.

--check previews without changing anything.

Then register the key with GitHub if it is new (docs/configuration.md \"Commit
Signing\"), restart any ccy sessions, and run acceptance.bash."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's git config and ccy launcher"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

plan_deploy_leg "play-git-configure-and-tools.yml" \
    plan_ansible_playbook playbooks/imports/play-git-configure-and-tools.yml

printf '\n==> NEXT:\n'
printf '    1. If the signing key is new, register it with GitHub (docs/configuration.md\n'
printf '       "Commit Signing"), on the account whose verified email your commits use:\n'
printf '       gh auth switch --user <that account>\n'
printf '       gh auth refresh --scopes admin:ssh_signing_key\n'
printf '       gh ssh-key add ~/.ssh/id_ed25519_git_signing.pub --type signing\n'
printf '    2. Restart ccy sessions; a running one keeps its old, unsigned gitconfig.\n'
printf '    3. ./acceptance.bash\n\n'

plan_finish
