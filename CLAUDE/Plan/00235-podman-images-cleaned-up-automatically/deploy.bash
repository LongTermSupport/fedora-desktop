#!/usr/bin/env bash
# Plan 00235 — deploy.bash
#
# PURPOSE: deploy the daily prune of the rootless podman store. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2): Ansible never runs in the CCY container.
#
# EFFECT ON THE HOST: play-podman.yml first repairs the rootless store as root
# (tasks/podman-store-repair.yml): each c/storage temporary directory left in
# overlay/tempdirs, and its lock file, is removed and named with its size, unless a podman or
# buildah of the user is removing or building or a lock there is held, which fails the play
# instead. It repairs again after the first prune, whatever that prune's result.
# play-podman.yml (which imports play-golang.yml) then builds container-images
# into ~/.local/bin, installs container-images-prune.service and .timer in the user manager
# and enables the timer. Until a prune has succeeded once it also starts the service and waits
# for it: that run REMOVES the backlog (dangling images no container uses, over a day old;
# tagged images no container uses and none is built on, over 28 days old (never claude-yolo:base
# or :latest, nor an image built here that nothing builds again);
# buildah working containers over a day old; nothing while a ccy build runs), each removal logged to
# ~/.local/state/fedora-desktop/container-images.log. No volume and no image in use is removed.
# The owner agreed to the first run clearing the backlog with no dry run read first (Q4).
# Before the timer, a root probe (tasks/podman-store-ownership-probe.yml) looks for a store
# file owned outside the user's uid and current subuid range (the old range). If it finds one
# the timer is STOPPED and DISABLED, the first prune is not run, and the play FAILS ON PURPOSE
# at its end: "the podman store holds files owned outside your subuid range (old range); the
# daily prune is held off until they are re-owned (Plan 00235 Q6)". The store is repaired by
# then, so podman works; the failure is the owner's Q6 decision. A clean probe writes
# ~/.local/state/fedora-desktop/container-images.store-clean, a failing one removes it.
# Then play-claude-yolo.yml installs CCY 3.102.1 (lib/image-build-prune.bash) and
# container-images again, and rebuilds claude-yolo:latest under the build lock: that build
# removes the :latest and :base images it replaced when no container uses them, and from then
# on every ccy build and in-place update does the same, while the store-clean marker is
# present. If play-podman.yml fails on purpose (Q6), play-claude-yolo.yml does not run in this
# deploy; CCY 3.102.1 then arrives through changed-plays on a later run.
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

Plan 00235, the deploy half: runs play-podman.yml on the HOST, which repairs the rootless store
as root (leftover c/storage temporary directories), builds container-images,
installs and enables the daily container-images-prune timer and, the first time, runs the
prune once (it removes the backlog of unused dangling images). If the store holds files owned
outside the user's subuid range (the old range), the timer is held off (stopped, disabled), no
prune runs, and the play fails on purpose with that message (Plan 00235 Q6), so the next leg
does not run. Then play-claude-yolo.yml, whose image build, and every ccy build after it,
removes the images it replaced while the store is confirmed clean.
  --check   the play as a dry run
Prompts for sudo once, before the run log opens."

plan_mode deploy
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -ne 0 ]]; then
    printf '[FATAL] unknown argument: %s\n%s\n' "${PLAN_REMAINING_ARGS[0]}" "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it installs the prune timer in the host user's systemd manager"
plan_prime_sudo
plan_start_log auto

plan_deploy_leg "play-podman.yml (container-images, its daily timer, the first prune)" \
    plan_ansible_playbook playbooks/imports/play-podman.yml
plan_deploy_leg "play-claude-yolo.yml (CCY 3.102.1: a build removes the images it replaced)" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml
# STANDARD-EXCEPTION(R9): no acceptance.bash leg here, where R9 puts it. meta-deploy.bash
# runs acceptance.bash after this script.
plan_finish
