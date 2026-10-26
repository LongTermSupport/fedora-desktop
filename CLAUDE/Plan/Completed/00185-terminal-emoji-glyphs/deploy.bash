#!/usr/bin/env bash
# Plan 00185 — deploy.bash (Task 2.2)
#
# PURPOSE: make Noto Color Emoji the emoji fallback after the text fonts, so a terminal draws
# colour emoji instead of Symbola's or Noto Emoji's monochrome ones (#60). HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2): Ansible never runs in the CCY container.
#
# WHAT IT CHANGES: one leg, playbooks/imports/play-fonts.yml (the renamed play-ms-fonts.yml).
# It installs google-noto-color-emoji-fonts if missing, deploys
# /etc/fonts/conf.d/69-colour-emoji-fallback.conf and, when that file changes, rebuilds the
# system font caches (fc-cache -f, under umask 022) and prints a notice to reopen terminal
# windows. On every run it makes the system font cache files 0644, which repairs caches an
# earlier rebuild left root-only. The Microsoft core fonts task is unchanged and skips on a
# host that has them.
#
# Then acceptance.bash, as the last leg (R9), unless --check.
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

Runs, on the HOST:

  playbooks/imports/play-fonts.yml   (Noto Color Emoji as the emoji fallback; the font
                                      caches rebuilt when the fallback file changes, and
                                      made readable by every user on every run)

then acceptance.bash. --check previews the play and skips the acceptance."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's fontconfig"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-fonts.yml" plan_ansible_playbook playbooks/imports/play-fonts.yml

if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '\n==> --check: nothing was deployed, so acceptance.bash is not run.\n'
else
    plan_deploy_leg "acceptance.bash" bash "${PLAN_SCRIPT_DIR}/acceptance.bash"
    printf '\n==> Close and reopen terminal windows: a running program keeps the font setup it\n'
    printf '    started with, so only a new window shows the colour emoji.\n'
fi
plan_finish
