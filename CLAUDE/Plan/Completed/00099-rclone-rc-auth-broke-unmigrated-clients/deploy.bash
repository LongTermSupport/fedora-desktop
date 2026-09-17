#!/usr/bin/env bash
#
# Plan 00099 — deploy the RC credential helper and every migrated client.
# HOST ONLY (CLAUDE/PlanScriptStandards.md R2).
#
# Runs BOTH plays, and that pairing is the point. Plan 00094 changed
# files/home/.local/bin/ftp-camera but deployed only play-rclone.yml, so the
# repo fix never reached the host and the camera copy stayed broken for weeks.
# ftp-camera is deployed by play-ftp-camera.yml; the rclone helpers and the
# shared credential library by play-rclone.yml. Both, or the fix is partial.
#
# WHAT IT CHANGES ON THE HOST: the rclone RC credential file and the shared
# rclone-rc-auth.bash library are written; rclone-cache-status, rclone-cache-warm
# and rclone-tail are replaced with their migrated forms; ftp-camera likewise.
# play-rclone.yml REWRITES every rclone mount unit and RESTARTS the mounts, which
# interrupts the VFS write-back queue. This script refuses to run while an
# ftp-camera process is in flight.
#
# ON THE LIBRARY, since the plan's own handoff recorded NOT converting these
# scripts as an owner trade-off. That reasoning was about acceptance.bash, whose
# container harness plan_require_host would kill — it never reached this file,
# and this file had a live defect the standard exists to prevent: it ran
# `ansible-playbook` without cd-ing to the repo root, and every path in
# ansible.cfg is RELATIVE (the inventory, roles_path, the vault credential
# setting, and callback_plugins — Plan 00109's play ledger). Invoked by absolute
# path from anywhere but the root it would have run with none of them. The host
# run worked because the operator happened to be standing in the right place.
# plan_ansible_playbook subshells that cd, and closes stdin so a play cannot
# drain a later prompt's input.
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
# shellcheck source=../../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: deploy.bash [-h|--help] [-y|--yes] [--check]

Plan 00099 — deploy the rclone RC client migration (HOST ONLY)

Runs, in order:
  1. play-rclone.yml       — deploys rclone-rc-auth.bash and the migrated
                             rclone-cache-status / rclone-cache-warm /
                             rclone-tail; rewrites and restarts the mounts
  2. play-ftp-camera.yml   — deploys the migrated ftp-camera

REFUSES to run while an ftp-camera process is in flight, because restarting the
mount would interrupt the VFS write-back queue and can lose cached-but-not-yet-
uploaded data.

Run acceptance.bash afterwards to confirm the change landed."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it rewrites this machine's rclone mount units and restarts the mounts"

PLAY_RCLONE="playbooks/imports/optional/common/play-rclone.yml"
PLAY_CAMERA="playbooks/imports/optional/common/play-ftp-camera.yml"

for play in "${PLAY_RCLONE}" "${PLAY_CAMERA}"; do
    if [[ ! -f "${PLAN_REPO_ROOT}/${play}" ]]; then
        printf '[FATAL] playbook not found: %s\n' "${PLAN_REPO_ROOT}/${play}" >&2
        exit 1
    fi
done

# ── in-flight copy guard ─────────────────────────────────────────────────────────────────
# Restarting the mount mid-copy interrupts the write-back queue. Refuse rather than risk
# cached-but-unuploaded data.
#
# Anchored to a path component or start-of-line, NOT a bare substring: a plain
# `pgrep -f ftp-camera` also matches any shell whose command line merely MENTIONS
# ftp-camera — including the one that invoked this script, and any agent or terminal
# discussing the problem. That false positive makes the guard refuse a deploy with nothing
# actually running. Self and parent are excluded for the same reason.
camera_pids=""
raw_pids=""
if raw_pids=$(pgrep -f '(^|/)ftp-camera([[:space:]]|$)'); then
    # grep exits 1 when EVERY hit was filtered out — the normal case here (only this
    # script and its parent matched). That is a result, not an error, but under
    # `set -euo pipefail` an unguarded assignment from it aborts the whole deploy
    # silently, with no message and no plays run. Check the status.
    filtered=""
    if filtered=$(printf '%s\n' "${raw_pids}" | grep -v -x -e "$$" -e "${PPID}"); then
        camera_pids="${filtered}"
    fi
fi
if [[ -n "${camera_pids}" ]]; then
    printf '[FATAL] an ftp-camera process is running.\n' >&2
    printf '  Deploying now would restart the mount and interrupt the VFS\n' >&2
    printf '  write-back queue. Wait for the copy to finish, then re-run.\n\n' >&2
    printf '  Running processes:\n' >&2
    while IFS= read -r pid; do
        ps -o pid=,args= -p "${pid}" >&2
    done <<< "${camera_pids}"
    exit 1
fi

plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-rclone.yml" \
    plan_ansible_playbook "${PLAY_RCLONE}"

plan_deploy_leg "play-ftp-camera.yml" \
    plan_ansible_playbook "${PLAY_CAMERA}"

printf '\nDeploy finished. Now confirm it landed:\n'
printf '  %s/acceptance.bash\n' "${PLAN_SCRIPT_DIR}"

plan_finish
